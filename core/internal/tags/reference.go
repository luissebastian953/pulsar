// Consensus reference selection for post-cutoff characters: the top-scored
// Danbooru solo post whose OWN tags agree with the character's consensus
// design. Score-ranking alone favors popular alt-outfit/mistagged art (a
// pink-haired outlier taught us this); requiring agreement with the
// dominant design tags keeps the pick canonical (docs/tagdb.md#anchors).
package tags

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// forbiddenRefFloor is the hardcoded baseline for the reference-fetch
// gate. The runtime gate is this set ∪ FORBIDDEN_TAG_UC (built in NewLive)
// so config can only add tags, never remove these; the ingest compliance
// hook remains the real gate downstream (CLAUDE.md non-negotiables).
var forbiddenRefFloor = map[string]bool{
	"loli": true, "shota": true, "child": true, "toddler": true,
	"aged_down": true, "baby": true,
}

type refPost struct {
	ID       int    `json:"id"`
	Score    int    `json:"score"`
	Rating   string `json:"rating"`
	FileExt  string `json:"file_ext"`
	Width    int    `json:"image_width"`
	TagAll   string `json:"tag_string"`
	TagGen   string `json:"tag_string_general"`
	TagChar  string `json:"tag_string_character"`
	FileURL  string `json:"file_url"`
	LargeURL string `json:"large_file_url"`
}

// pickConsensusReference: candidates must carry every hair-design tag that
// ≥50% of the sample carries (the consensus design), pass rating/format
// gates, and depict the character alone (single character tag). Highest
// score wins.
func pickConsensusReference(posts []refPost, canonical string, forbidden map[string]bool) *refPost {
	if len(posts) == 0 {
		return nil
	}
	freq := map[string]int{}
	for _, p := range posts {
		for _, t := range strings.Fields(p.TagGen) {
			freq[t]++
		}
	}
	var required []string
	for t, n := range freq {
		if n*2 >= len(posts) && strings.Contains(t, "hair") && isDesignTag(t) {
			required = append(required, t)
		}
	}
	var best *refPost
	for i := range posts {
		p := &posts[i]
		if p.Rating != "g" && p.Rating != "s" {
			continue
		}
		switch p.FileExt {
		case "jpg", "jpeg", "png":
		default:
			continue // ingest decodes png/jpeg only
		}
		if p.Width < 700 {
			continue
		}
		chars := strings.Fields(p.TagChar)
		if len(chars) != 1 || chars[0] != canonical {
			continue // variant forms and multi-character posts drift off-design
		}
		tags := " " + p.TagAll + " "
		if hasAny(tags, forbidden) {
			continue
		}
		ok := true
		for _, r := range required {
			if !hasTag(tags, r) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if best == nil || effectiveScore(p) > effectiveScore(best) {
			best = p
		}
	}
	return best
}

// effectiveScore biases toward reference-friendly compositions: a neutral
// standing shot conditions identity far better than a dramatic angle,
// which leaks pose/perspective into the render (NAI's own guidance:
// full-body standing on a basic background — docs/research-novelai.md §2).
func effectiveScore(p *refPost) int {
	s := p.Score
	tags := " " + p.TagGen + " "
	for _, t := range []string{"standing", "simple_background", "white_background", "grey_background"} {
		if hasTag(tags, t) {
			s += 40
		}
	}
	for _, t := range []string{"from_below", "from_behind", "from_above", "lying", "chibi", "dutch_angle", "foreshortening"} {
		if hasTag(tags, t) {
			s -= 60
		}
	}
	// default costume is the identity; alternates only when nothing else
	if hasTag(tags, "alternate_costume") || hasTag(tags, "official_alternate_costume") {
		s -= 40
	}
	// face-rich framings condition identity best (full_body imports outfit)
	for _, t := range []string{"upper_body", "portrait"} {
		if hasTag(tags, t) {
			s += 30
		}
	}
	return s
}

// FullBody reports whether the exemplar depicts the whole figure — the
// caller bust-crops those, because canvas aspect is NOT a reliable proxy
// (a square official render holds a full body with margins).
func (p *refPost) FullBody() bool {
	tags := " " + p.TagGen + " "
	if hasTag(tags, "upper_body") || hasTag(tags, "portrait") {
		return false
	}
	return hasTag(tags, "full_body") || hasTag(tags, "cowboy_shot")
}

// hasTag reports whether a space-padded tag string carries the exact tag
// (padding avoids substring false positives: "hair" vs "black_hair").
func hasTag(spacePadded, tag string) bool {
	return strings.Contains(spacePadded, " "+tag+" ")
}

func hasAny(spacePadded string, set map[string]bool) bool {
	for t := range set {
		if hasTag(spacePadded, t) {
			return true
		}
	}
	return false
}

// ConsensusReference fetches the character's solo posts, picks the
// consensus-agreeing exemplar, and downloads it. maxBytes bounds the
// download (pass ingest.MaxBytes). nil bytes = no suitable reference.
func (l *Live) ConsensusReference(ctx context.Context, canonical string, maxBytes int64) ([]byte, int, bool, error) {
	q := url.Values{}
	q.Set("tags", canonical+" solo")
	q.Set("limit", "50")
	var posts []refPost
	if err := l.getJSON(ctx, "/posts.json?"+q.Encode(), &posts); err != nil {
		return nil, 0, false, err
	}
	p := pickConsensusReference(posts, canonical, l.forbidden)
	if p == nil {
		return nil, 0, false, nil
	}
	u := p.LargeURL
	if u == "" {
		u = p.FileURL
	}
	if u == "" {
		return nil, 0, false, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, false, err
	}
	req.Header.Set("User-Agent", "pulsar/1.0 (auto reference)")
	resp, err := l.http.Do(req)
	if err != nil {
		return nil, 0, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, false, fmt.Errorf("reference download: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, 0, false, err
	}
	return data, p.ID, p.FullBody(), nil
}
