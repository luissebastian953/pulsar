package tags

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
		post(8, 95, "s", "jpg", 1200, char, "black_hair long_hair"),              // sensitive: general-only gate
	}
	got := pickConsensusReference(posts, char, forbiddenRefFloor)
	if got == nil || got.ID != 2 {
		t.Fatalf("picked %+v, want post 2", got)
	}

	// forbidden tag at source is rejected regardless of score
	posts = append(posts, post(7, 99, "g", "jpg", 1200, char, "black_hair long_hair loli"))
	if got := pickConsensusReference(posts, char, forbiddenRefFloor); got == nil || got.ID != 2 {
		t.Fatalf("forbidden-tagged post must not win, picked %+v", got)
	}

	// composition bias: a standing/simple-background exemplar beats a
	// higher-raw-score dramatic angle (pose leaks into the render)
	posts = append(posts,
		post(10, 39, "g", "jpg", 1200, char, "black_hair long_hair from_below sitting"),
		post(11, 18, "g", "jpg", 1200, char, "black_hair long_hair standing simple_background grey_background"))
	if got := pickConsensusReference(posts, char, forbiddenRefFloor); got == nil || got.ID != 11 {
		t.Fatalf("composition bias failed, picked %+v", got)
	}

	// no eligible candidate → nil, never a forced pick
	if got := pickConsensusReference([]refPost{post(9, 5, "e", "jpg", 1200, char, "black_hair")}, char, forbiddenRefFloor); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
	if got := pickConsensusReference(nil, char, forbiddenRefFloor); got != nil {
		t.Fatalf("empty input must yield nil")
	}
}

// TestPickPrefersDefaultCostume pins the costume tier: any default-outfit
// candidate beats every costumed one regardless of score (live Castorice:
// a score-142 ghost_costume post beat the canonical design under the old
// -40 penalty), and costumes are the fallback only when nothing else
// qualifies.
func TestPickPrefersDefaultCostume(t *testing.T) {
	char := "castorice_(honkai:_star_rail)"
	costumed := []refPost{
		post(1, 300, "g", "jpg", 1200, char, "purple_hair long_hair ghost_costume"),
		post(2, 140, "g", "jpg", 1200, char, "purple_hair long_hair halloween"),
		post(3, 120, "g", "jpg", 1200, char, "purple_hair long_hair official_alternate_costume"),
		post(4, 110, "g", "jpg", 1200, char, "purple_hair long_hair cosplay"),
	}
	posts := append(append([]refPost{}, costumed...),
		post(5, 15, "g", "jpg", 1200, char, "purple_hair long_hair dress"))
	if got := pickConsensusReference(posts, char, forbiddenRefFloor); got == nil || got.ID != 5 {
		t.Fatalf("default outfit must beat every costume, picked %+v", got)
	}
	if got := pickConsensusReference(costumed, char, forbiddenRefFloor); got == nil || got.ID != 1 {
		t.Fatalf("costume fallback should pick the best costumed post, picked %+v", got)
	}
}

// TestConsensusReferenceQueriesGeneral: the Danbooru query itself asks for
// rating:g (so the consensus sample is general posts), and the picked
// post's bytes come back.
func TestConsensusReferenceQueriesGeneral(t *testing.T) {
	char := "castorice_(honkai:_star_rail)"
	var gotTags string
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("GET /posts.json", func(w http.ResponseWriter, r *http.Request) {
		gotTags = r.URL.Query().Get("tags")
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": 12344863, "score": 21, "rating": "g", "file_ext": "jpg", "image_width": 1200,
			"tag_string_character": char, "tag_string_general": "purple_hair long_hair dress upper_body",
			"tag_string":     char + " purple_hair long_hair dress upper_body",
			"large_file_url": srv.URL + "/ref.jpg",
		}})
	})
	mux.HandleFunc("GET /ref.jpg", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("jpeg-bytes"))
	})
	l := NewLive(nil, srv.URL, 50, "loli")
	data, id, fullBody, err := l.ConsensusReference(context.Background(), char, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if want := char + " solo rating:g"; gotTags != want {
		t.Fatalf("query tags = %q, want %q", gotTags, want)
	}
	if string(data) != "jpeg-bytes" || id != 12344863 || fullBody {
		t.Fatalf("got data=%q id=%d fullBody=%v", data, id, fullBody)
	}
}

// TestFullBody: framing comes from tags, never canvas shape.
func TestFullBody(t *testing.T) {
	for gen, want := range map[string]bool{
		"black_hair full_body standing": true,
		"black_hair cowboy_shot":        true,
		"black_hair upper_body":         false,
		"black_hair portrait full_body": false, // face-framing tag wins
		"black_hair standing":           false, // unknown framing: don't crop
	} {
		p := refPost{TagGen: gen}
		if got := p.FullBody(); got != want {
			t.Errorf("FullBody(%q) = %v, want %v", gen, got, want)
		}
	}
}

// TestForbiddenUnionFailClosed pins the gate's fail-closed shape: the
// hardcoded floor survives any FORBIDDEN_TAG_UC value, env tags are added,
// and a post carrying an env-added tag is rejected at the source.
func TestForbiddenUnionFailClosed(t *testing.T) {
	l := NewLive(nil, "http://danbooru.test", 50, "loli, extra_env_tag")
	for _, want := range []string{"baby", "loli", "extra_env_tag"} {
		if !l.forbidden[want] {
			t.Fatalf("forbidden union missing %q", want)
		}
	}
	char := "c_(x)"
	posts := []refPost{post(1, 90, "g", "jpg", 1200, char, "black_hair extra_env_tag")}
	if got := pickConsensusReference(posts, char, l.forbidden); got != nil {
		t.Fatalf("env-added forbidden tag not gated: %+v", got)
	}
}
