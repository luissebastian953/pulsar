package assembly

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"pulsar/core/internal/prompt"
)

func stripChunks(tags []prompt.Tag) {
	for i := range tags {
		tags[i].Chunk = nil
	}
}

// TestChunksAcrossSurfaces: chunks expand in the prompt, the negative and
// every character; the Spec equals the inlined request's apart from the
// `chunk` provenance; the safety block still leads the negative.
func TestChunksAcrossSurfaces(t *testing.T) {
	// the first safety term, read from the test's config value
	safetyTerm := strings.TrimSpace(strings.Split(testForbidden, ",")[0])
	chunks := []prompt.Chunk{
		{Name: "Hair", Content: "silver hair, {long hair}"},
		{Name: "Bad", Content: "blurry, " + safetyTerm + ", 1.2::bad hands ::"},
		{Name: "Outfit", Content: "school uniform, !macro:Hair!"},
	}
	chunked := Request{
		Prompt:   "2girls, !macro:Hair!, smile",
		Negative: "!macro:Bad!, jpeg artifacts",
		Characters: []Character{
			{Prompt: "girl a, !macro:Outfit!"},
			{Prompt: "girl b, -1::hat ::, !macro:Hair!"},
		},
		Chunks: chunks,
	}
	inlined := Request{
		Prompt:   "2girls, silver hair, {long hair}, smile",
		Negative: "blurry, " + safetyTerm + ", 1.2::bad hands ::, jpeg artifacts",
		Characters: []Character{
			{Prompt: "girl a, school uniform, silver hair, {long hair}"},
			{Prompt: "girl b, -1::hat ::, silver hair, {long hair}"},
		},
	}
	{
		p, err := ParseRequest(chunked)
		if err != nil {
			t.Fatal(err)
		}
		got, notices, err := BuildParsed(chunked, p, opts())
		if err != nil {
			t.Fatal(err)
		}
		want, err := Build(inlined, opts())
		if err != nil {
			t.Fatal(err)
		}
		assertSafetyFirst(t, got, testForbidden)
		if got.Prompt != want.Prompt || got.Negative != want.Negative {
			t.Errorf("emitted strings differ:\n%q / %q\n%q / %q", got.Prompt, got.Negative, want.Prompt, want.Negative)
		}

		// provenance: on the chunk's tags only, named after the typed reference
		byName := map[string]string{}
		for _, tg := range append(append([]prompt.Tag{}, got.PromptTags...), got.NegativeTags...) {
			if tg.Chunk != nil {
				byName[tg.Name] = tg.Chunk.Name
				if tg.Source != prompt.User || len(tg.Chunk.Hash) != 16 {
					t.Errorf("chunk tag %q: source %v hash %q", tg.Text, tg.Source, tg.Chunk.Hash)
				}
			}
		}
		for name, chunk := range map[string]string{"silver_hair": "Hair", "long_hair": "Hair", "blurry": "Bad", "bad_hands": "Bad"} {
			if byName[name] != chunk {
				t.Errorf("tag %s chunk = %q, want %q (%v)", name, byName[name], chunk, byName)
			}
		}
		for _, name := range []string{"2girls", "smile", "jpeg_artifacts", "hat"} {
			if _, ok := byName[name]; ok {
				t.Errorf("typed tag %s carries provenance", name)
			}
		}
		// a nested chunk is attributed to the reference the user typed
		if c := got.Characters[0].Tags; c[1].Chunk == nil || c[1].Chunk.Name != "Outfit" || c[2].Chunk == nil || c[2].Chunk.Name != "Outfit" || c[0].Chunk != nil {
			t.Errorf("character 0 tags = %+v", c)
		}
		// the safety term inside the UC chunk was dropped with a notice on the reference
		found := false
		for _, n := range notices {
			if n.Code == prompt.NoticeDuplicateUC && n.Field == "negative" {
				found = true
				if n.Start != 0 || n.End != len("!macro:Bad!") || n.Tag != 0 {
					t.Errorf("duplicate_dropped notice = %+v, want the reference span", n)
				}
			}
		}
		if !found {
			t.Errorf("no duplicate_dropped notice in %+v", notices)
		}
		for _, tg := range got.NegativeTags[len(strings.Split(testForbidden, ",")):] {
			if tg.Name == safetyTerm {
				t.Errorf("a safety copy from a chunk survived dedupe: %q", got.Negative)
			}
		}

		// Spec JSON equals the inlined request's once `chunk` is removed
		stripChunks(got.PromptTags)
		stripChunks(got.NegativeTags)
		for i := range got.Characters {
			stripChunks(got.Characters[i].Tags)
		}
		gj, _ := json.Marshal(got)
		wj, _ := json.Marshal(want)
		if string(gj) != string(wj) {
			t.Errorf("Spec JSON differs from the inlined request:\n%s\n%s", gj, wj)
		}
		if strings.Contains(string(wj), `"chunk"`) || strings.Contains(string(gj), prompt.ChunkPrefix) {
			t.Errorf("chunk data leaked into the Spec: %s", gj)
		}
	}
}

// TestChunkErrorsAreLocated: an error inside a chunk is a FieldError on the
// reference, on the right field; bad chunk data is a plain 400.
func TestChunkErrorsAreLocated(t *testing.T) {
	chunks := []prompt.Chunk{{Name: "Bad", Content: "a, b}"}, {Name: "Neg", Content: "-1::hat ::"}}
	for _, tc := range []struct {
		req        Request
		field      string
		code       string
		start, end int
	}{
		{Request{Prompt: "1girl, !macro:Bad!", Chunks: chunks}, "prompt", prompt.CodeStrayCloser, 7, 18},
		{Request{Prompt: "1girl", Negative: "x, !macro:Neg!", Chunks: chunks}, "negative", prompt.CodeNegativeInUC, 3, 14},
		{Request{Prompt: "a, b", Characters: []Character{{Prompt: "x"}, {Prompt: "!macro:Bad!"}}, Chunks: chunks}, "characters.1", prompt.CodeStrayCloser, 0, 11},
		{Request{Prompt: "1girl, !macro:Missing!", Chunks: chunks}, "prompt", prompt.CodeUnknownChunk, 7, 22},
		{Request{Prompt: "1girl, !macro:Bad!"}, "prompt", prompt.CodeUnknownChunk, 7, 18},
	} {
		_, err := Build(tc.req, opts())
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != tc.field || fe.Issue.Code != tc.code || fe.Issue.Start != tc.start || fe.Issue.End != tc.end {
			t.Errorf("%+v: %v, want %s on %s [%d,%d)", tc.req, err, tc.code, tc.field, tc.start, tc.end)
		}
	}
	for _, bad := range [][]prompt.Chunk{
		{{Name: "a,b", Content: "x"}},
		{{Name: "a", Content: "x"}, {Name: "a", Content: "y"}},
		{{Name: "a", Content: strings.Repeat("x", prompt.MaxChunkBytes+1)}},
	} {
		_, err := Build(Request{Prompt: "1girl", Chunks: bad}, opts())
		var fe *FieldError
		if err == nil || errors.As(err, &fe) {
			t.Errorf("chunks %+v: %v, want a plain validation error", bad, err)
		}
	}
}

// TestChunkTokenRefusesConfig: a chunk token in any deployment prompt
// refuses boot (CheckConfig) and a style row with one is a ConfigError —
// user data can never be spliced into a config or safety surface.
func TestChunkTokenRefusesConfig(t *testing.T) {
	const tok = "!macro:X!"
	for _, cfg := range [][3]string{
		{testQuality + ", " + tok, testUC, testForbidden},
		{testQuality, testUC + ", " + tok, testForbidden},
		{testQuality, testUC, testForbidden + ", " + tok},
		{testQuality, testUC, tok},
	} {
		if err := CheckConfig(cfg[0], cfg[1], cfg[2]); err == nil {
			t.Errorf("CheckConfig(%q, %q, …) accepted a chunk token", cfg[0], cfg[1])
		}
	}
	if err := CheckConfig(testQuality, testUC, testForbidden); err != nil {
		t.Fatal(err)
	}
	// even when the request supplies a chunk of that name
	req := Request{Prompt: "1girl", Chunks: []prompt.Chunk{{Name: "X", Content: "y"}}}
	for _, o := range []Options{
		{Lineage: "illustrious", Seed: 1, ForbiddenTagUC: testForbidden + ", " + tok},
		{Lineage: "illustrious", Seed: 1, ForbiddenTagUC: testForbidden, QualityTags: tok},
		{Lineage: "illustrious", Seed: 1, ForbiddenTagUC: testForbidden, DefaultUC: tok},
		{Lineage: "illustrious", Seed: 1, ForbiddenTagUC: testForbidden, Style: &Style{Slug: "s", PromptPrefix: tok}},
		{Lineage: "illustrious", Seed: 1, ForbiddenTagUC: testForbidden, Style: &Style{Slug: "s", Negative: tok}},
	} {
		_, err := Build(req, o)
		var ce *ConfigError
		if !errors.As(err, &ce) {
			t.Errorf("opts %+v: %v, want a ConfigError", o, err)
		}
	}
}
