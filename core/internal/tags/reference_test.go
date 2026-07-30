package tags

import "testing"

func post(id, score int, rating, ext string, width int, charTags, genTags string) refPost {
	return refPost{
		ID: id, Score: score, Rating: rating, FileExt: ext, Width: width,
		TagChar: charTags, TagGen: genTags,
		TagAll:  charTags + " " + genTags,
		FileURL: "https://example/x." + ext,
	}
}

// TestPickConsensusReference pins the selection rules: consensus hair tags
// required, outliers rejected, rating/format/solo gates enforced, then
// score decides.
func TestPickConsensusReference(t *testing.T) {
	char := "hysilens_(honkai:_star_rail)"
	// 6 posts: black_hair dominant (5/6 ≥ 50%); one popular pink outlier
	posts := []refPost{
		post(1, 90, "g", "jpg", 1200, char, "pink_hair long_hair"),               // outlier: top score, wrong design
		post(2, 60, "g", "jpg", 1200, char, "black_hair long_hair blue_hair"),    // consensus, best eligible
		post(3, 50, "g", "png", 1200, char, "black_hair long_hair"),              // consensus, lower score
		post(4, 80, "e", "jpg", 1200, char, "black_hair long_hair"),              // wrong rating
		post(5, 70, "g", "gif", 1200, char, "black_hair long_hair"),              // undecodable format
		post(6, 75, "g", "jpg", 1200, char+" other_(x)", "black_hair long_hair"), // not solo-character
	}
	got := pickConsensusReference(posts, char)
	if got == nil || got.ID != 2 {
		t.Fatalf("picked %+v, want post 2", got)
	}

	// forbidden tag at source is rejected regardless of score
	posts = append(posts, post(7, 99, "g", "jpg", 1200, char, "black_hair long_hair loli"))
	if got := pickConsensusReference(posts, char); got == nil || got.ID != 2 {
		t.Fatalf("forbidden-tagged post must not win, picked %+v", got)
	}

	// composition bias: a standing/simple-background exemplar beats a
	// higher-raw-score dramatic angle (pose leaks into the render)
	posts = append(posts,
		post(10, 39, "g", "jpg", 1200, char, "black_hair long_hair from_below sitting"),
		post(11, 18, "g", "jpg", 1200, char, "black_hair long_hair standing simple_background grey_background"))
	if got := pickConsensusReference(posts, char); got == nil || got.ID != 11 {
		t.Fatalf("composition bias failed, picked %+v", got)
	}

	// no eligible candidate → nil, never a forced pick
	if got := pickConsensusReference([]refPost{post(9, 5, "e", "jpg", 1200, char, "black_hair")}, char); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
	if got := pickConsensusReference(nil, char); got != nil {
		t.Fatalf("empty input must yield nil")
	}
}
