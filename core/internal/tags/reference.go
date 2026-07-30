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

// forbiddenRefTags gate fetching at the source, mirroring the training
// fetcher; the ingest compliance hook remains the real gate downstream
// (CLAUDE.md non-negotiables).
var forbiddenRefTags = map[string]bool{
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
func pickConsensusReference(posts []refPost, canonical string) *refPost {
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
		if hasAny(tags, forbiddenRefTags) {
			continue
		}
		ok := true
		for _, r := range required {
			if !strings.Contains(tags, " "+r+" ") {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if best == nil || p.Score > best.Score {
			best = p
		}
	}
	return best
}

func hasAny(spacePadded string, set map[string]bool) bool {
	for t := range set {
		if strings.Contains(spacePadded, " "+t+" ") {
			return true
		}
	}
	return false
}

// ConsensusReference fetches the character's solo posts, picks the
// consensus-agreeing exemplar, and downloads it. maxBytes bounds the
// download (pass ingest.MaxBytes). nil bytes = no suitable reference.
func (l *Live) ConsensusReference(ctx context.Context, canonical string, maxBytes int64) ([]byte, int, error) {
	q := url.Values{}
	q.Set("tags", canonical+" solo")
	q.Set("limit", "50")
	var posts []refPost
	if err := l.getJSON(ctx, "/posts.json?"+q.Encode(), &posts); err != nil {
		return nil, 0, err
	}
	p := pickConsensusReference(posts, canonical)
	if p == nil {
		return nil, 0, nil
	}
	u := p.LargeURL
	if u == "" {
		u = p.FileURL
	}
	if u == "" {
		return nil, 0, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "pulsar/1.0 (auto reference)")
	resp, err := l.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("reference download: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, 0, err
	}
	return data, p.ID, nil
}
