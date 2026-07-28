// Design anchors for post-cutoff characters: the model knows every design
// component (white_hair, yellow_eyes, ...) even when it never learned the
// character's name. Fetching the tag frequency of the character's solo posts
// recovers that vocabulary so an unknown name renders by description
// (.claude/skills/tag-grounding).
package tags

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"time"
)

// EnsureCreatedAt backfills danbooru_created_at for a tag seeded before the
// freshness migration. The date self-heals into the DB, so the
// newer_than_model flag (and autocomplete honesty) start working for the tag
// from then on. Returns nil when Danbooru does not know the tag.
func (l *Live) EnsureCreatedAt(ctx context.Context, canonical string) (*time.Time, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	tag, err := l.fetchTag(ctx, canonical)
	if err != nil || tag == nil || tag.CreatedAt == nil {
		return nil, err
	}
	if err := l.store.SetCreatedAt(ctx, canonical, tag.CreatedAt); err != nil {
		return nil, err
	}
	return tag.CreatedAt, nil
}

// designKeywords: a tag counts as design vocabulary when any underscore
// segment matches. Deliberately appearance-only — poses, composition, and
// meta stay out so anchors never fight the user's scene.
var designKeywords = map[string]bool{
	"hair": true, "eyes": true, "eye": true, "pupils": true, "ahoge": true,
	"bangs": true, "braid": true, "ponytail": true, "twintails": true,
	"bun": true, "intake": true, "sidelocks": true, "ornament": true,
	"hairclip": true, "hairband": true, "ribbon": true, "hat": true,
	"dress": true, "skirt": true, "shirt": true, "jacket": true,
	"coat": true, "gloves": true, "sleeves": true, "thighhighs": true,
	"pantyhose": true, "shorts": true, "pants": true, "boots": true,
	"collar": true, "choker": true, "scarf": true, "cape": true,
	"hood": true, "armor": true, "uniform": true, "bodysuit": true,
	"leotard": true, "sash": true, "obi": true, "tassel": true,
	"wings": true, "tail": true, "horns": true, "halo": true,
	"ears": true, "mole": true, "glasses": true, "mask": true,
	"strap": true, "belt": true, "earrings": true, "necklace": true,
	"bracelet": true, "crown": true, "veil": true, "apron": true,
	"vest": true, "necktie": true, "bowtie": true, "kimono": true,
	"hanfu": true, "footwear": true, "headwear": true, "legwear": true,
}

func isDesignTag(tag string) bool {
	for _, seg := range strings.Split(tag, "_") {
		if designKeywords[seg] {
			return true
		}
	}
	return false
}

// extractDesignAnchors aggregates general-tag frequency across sampled solo
// posts and keeps design tags that appear in at least 35% of them (min 2),
// most-frequent first, capped. exclude holds tags already in the prompt.
func extractDesignAnchors(postTags [][]string, exclude map[string]bool, cap_ int) []string {
	if len(postTags) == 0 {
		return nil
	}
	freq := map[string]int{}
	for _, ts := range postTags {
		for _, t := range ts {
			freq[t]++
		}
	}
	floor := len(postTags) * 35 / 100
	if floor < 2 {
		floor = 2
	}
	var out []string
	for t, n := range freq {
		if n >= floor && isDesignTag(t) && !exclude[t] {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if freq[out[i]] != freq[out[j]] {
			return freq[out[i]] > freq[out[j]]
		}
		return out[i] < out[j]
	})
	if len(out) > cap_ {
		out = out[:cap_]
	}
	return out
}

// DesignAnchors samples the character's solo posts and returns its design
// vocabulary. Cached per tag for the process lifetime — anchors only matter
// for post-cutoff characters, which are rare and stable.
func (l *Live) DesignAnchors(ctx context.Context, canonical string, exclude map[string]bool) ([]string, error) {
	l.mu.Lock()
	if cached, ok := l.anchors[canonical]; ok {
		l.mu.Unlock()
		return filterExcluded(cached, exclude), nil
	}
	l.mu.Unlock()

	q := url.Values{}
	q.Set("tags", canonical+" solo")
	q.Set("limit", "30")
	// only tag_string_general is needed; only= keeps the payload tiny
	q.Set("only", "tag_string_general")
	var posts []struct {
		TagStringGeneral string `json:"tag_string_general"`
	}
	if err := l.getJSON(ctx, "/posts.json?"+q.Encode(), &posts); err != nil {
		return nil, err
	}
	var postTags [][]string
	for _, p := range posts {
		postTags = append(postTags, strings.Fields(p.TagStringGeneral))
	}
	// cache the unfiltered set; exclusion is per-request
	anchors := extractDesignAnchors(postTags, map[string]bool{}, 12)

	l.mu.Lock()
	if l.anchors == nil {
		l.anchors = make(map[string][]string)
	}
	l.anchors[canonical] = anchors
	l.mu.Unlock()
	return filterExcluded(anchors, exclude), nil
}

func filterExcluded(anchors []string, exclude map[string]bool) []string {
	out := make([]string, 0, len(anchors))
	for _, a := range anchors {
		if !exclude[a] {
			out = append(out, a)
		}
	}
	return out
}
