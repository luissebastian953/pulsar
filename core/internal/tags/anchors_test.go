package tags

import (
	"reflect"
	"testing"
)

// TestExtractDesignAnchors: frequency floor, design-only allowlist,
// prompt exclusion, and frequency-descending order.
func TestExtractDesignAnchors(t *testing.T) {
	// 10 posts: white_hair in all, yellow_eyes in 8, ahoge in 6,
	// yellow_jacket in 4, smile in 10 (not design), standing in 9 (not
	// design), black_gloves in 2 (below the 35% floor of 3.5→3)
	var posts [][]string
	for i := 0; i < 10; i++ {
		p := []string{"white_hair", "smile"}
		if i < 9 {
			p = append(p, "standing")
		}
		if i < 8 {
			p = append(p, "yellow_eyes")
		}
		if i < 6 {
			p = append(p, "ahoge")
		}
		if i < 4 {
			p = append(p, "yellow_jacket")
		}
		if i < 2 {
			p = append(p, "black_gloves")
		}
		posts = append(posts, p)
	}
	got := extractDesignAnchors(posts, map[string]bool{}, 12)
	want := []string{"white_hair", "yellow_eyes", "ahoge", "yellow_jacket"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("anchors = %v, want %v", got, want)
	}

	// tags already in the prompt are excluded
	got = extractDesignAnchors(posts, map[string]bool{"white_hair": true}, 12)
	if len(got) != 3 || got[0] != "yellow_eyes" {
		t.Errorf("excluded anchors = %v", got)
	}

	// cap holds
	if got := extractDesignAnchors(posts, map[string]bool{}, 2); len(got) != 2 {
		t.Errorf("cap: %v", got)
	}

	// empty input
	if got := extractDesignAnchors(nil, nil, 12); got != nil {
		t.Errorf("nil posts should yield nil, got %v", got)
	}
}

func TestIsDesignTag(t *testing.T) {
	for tag, want := range map[string]bool{
		"white_hair":         true,
		"single_hair_intake": true,
		"yellow_jacket":      true,
		"thigh_strap":        true,
		"looking_at_viewer":  false,
		"standing":           false,
		"smile":              false,
		"outdoors":           false,
	} {
		if got := isDesignTag(tag); got != want {
			t.Errorf("isDesignTag(%q) = %v, want %v", tag, got, want)
		}
	}
}
