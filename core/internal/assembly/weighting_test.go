package assembly

import (
	"errors"
	"math"
	"strings"
	"testing"

	"pulsar/core/internal/prompt"
)

// comfyTags re-reads an emitted string with ComfyUI's own grammar.
func comfyTags(t *testing.T, s string) []prompt.Tag {
	t.Helper()
	segs, err := prompt.ComfyParse(s)
	if err != nil {
		t.Fatalf("ComfyParse(%q): %v", s, err)
	}
	return prompt.ComfyTags(segs)
}

// assertSafetyFirst is the parsed form of the child-safety invariant: the
// first len(safety) negative tags are the FORBIDDEN_TAG_UC terms, in
// order, Source=Safety, at exactly weight 1 — in the structure AND in what
// ComfyUI reads from the emitted string.
func assertSafetyFirst(t *testing.T, s Spec, forbidden string) {
	t.Helper()
	var want []string
	for _, p := range strings.Split(forbidden, ",") {
		if n := prompt.Normalize(p); n != "" {
			want = append(want, n)
		}
	}
	if len(s.NegativeTags) < len(want) {
		t.Fatalf("negative tags %d < safety %d", len(s.NegativeTags), len(want))
	}
	parsed := comfyTags(t, s.Negative)
	if err := prompt.Equivalent(s.NegativeTags, parsed, 5e-5); err != nil {
		t.Fatalf("emitted negative %q disagrees with its structure: %v", s.Negative, err)
	}
	for i, name := range want {
		st, ct := s.NegativeTags[i], parsed[i]
		if st.Source != prompt.Safety || st.Name != name || ct.Name != name {
			t.Errorf("negative[%d] = %q/%q (%s), want safety %q — negative %q", i, st.Name, ct.Name, st.Source, name, s.Negative)
		}
		for _, r := range append(st.Runs, ct.Runs...) {
			if r.Emitted != 1 {
				t.Errorf("safety tag %q at weight %v, want exactly 1", name, r.Emitted)
			}
		}
	}
	if !strings.HasPrefix(s.Negative, forbidden) {
		t.Errorf("negative %q does not start with the safety terms", s.Negative)
	}
}

// TestSafetyFirstParsed extends TestForbiddenTagUCIsUnconditional to the
// parsed negative under every request shape the grammar allows (S9).
func TestSafetyFirstParsed(t *testing.T) {
	styled := opts()
	styled.Style = &Style{Slug: "s", Negative: "(logo:1.3), film grain"}
	noUC := opts()
	noUC.DefaultUC = ""
	for _, o := range []Options{opts(), styled, noUC} {
		for _, req := range []Request{
			{Prompt: "1girl"},
			{Prompt: "1girl", Negative: ""},
			{Prompt: "1girl", UCPreset: boolPtr(false)},
			// user negative holding safety copies at 1.05 and 0.952 — deduped
			{Prompt: "1girl", Negative: "{loli}, [shota], child, blurry"},
			{Prompt: "1girl", Negative: "1.5::loli ::, (shota:0.5), \\(child\\)"},
			// the legacy history round-trip: the whole stored negative re-sent
			{Prompt: "1girl", Negative: testForbidden + ", " + testUC + ", blurry"},
			// relocated negative weights land after the safety block
			{Prompt: "1girl, -1::hat ::, -3::loli ::", Negative: "blurry"},
			{Prompt: "1girl", Negative: "{{{{lowres}}}}, 1.3::bad hands ::"},
			{Prompt: "a, b", Characters: []Character{{Prompt: "x, -2::hat ::"}, {Prompt: "y"}}},
		} {
			s, err := Build(req, o)
			if err != nil {
				t.Fatalf("%+v: %v", req, err)
			}
			assertSafetyFirst(t, s, testForbidden)
			for _, tg := range s.NegativeTags[3:] {
				if tg.Source == prompt.User && (tg.Name == "loli" || tg.Name == "shota" || tg.Name == "child" || tg.Name == "(child)") {
					t.Errorf("%+v: safety copy %q survived dedupe in %q", req, tg.Text, s.Negative)
				}
			}
		}
	}
	// a stray or unclosed bracket at the start of the user negative can no
	// longer capture anything: it is a 400 before assembly
	for _, neg := range []string{"}lowres", "{lowres", "]x, [y", "(x:1.2"} {
		s, err := Build(Request{Prompt: "1girl", Negative: neg}, opts())
		if err == nil {
			assertSafetyFirst(t, s, testForbidden)
			continue
		}
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != "negative" {
			t.Errorf("negative %q: err = %v, want a negative FieldError", neg, err)
		}
	}
}

// TestBootRefusals: deployment prompt config is parsed at boot; a
// FORBIDDEN_TAG_UC with any operator refuses (S3).
func TestBootRefusals(t *testing.T) {
	if err := CheckConfig(testQuality, testUC, testForbidden); err != nil {
		t.Fatalf("valid config refused: %v", err)
	}
	for _, f := range []string{"", "  ", "{loli}, x", "loli, 1.2::x ::", "loli::", "(loli:1.2)", "loli (x)", "a | b", "embedding:x"} {
		if err := CheckConfig(testQuality, testUC, f); err == nil {
			t.Errorf("FORBIDDEN_TAG_UC %q must refuse boot", f)
		}
	}
	if err := CheckConfig("masterpiece, {best quality", testUC, testForbidden); err == nil {
		t.Error("unbalanced QUALITY_TAGS must refuse boot")
	}
	if err := CheckConfig(testQuality, "lowres, -1::x ::", testForbidden); err == nil {
		t.Error("negative weight in DEFAULT_UC must refuse boot")
	}
	if err := CheckConfig(testQuality, "(worst quality:1.4), lowres", testForbidden); err != nil {
		t.Errorf("SD weights in DEFAULT_UC auto-convert: %v", err)
	}
	// and per request: a malformed FORBIDDEN_TAG_UC never builds
	o := opts()
	o.ForbiddenTagUC = "{loli}, shota"
	if _, err := Build(Request{Prompt: "1girl"}, o); err == nil {
		t.Error("Build must refuse an operator in FORBIDDEN_TAG_UC")
	}
}

func TestStyleRowParseIsConfigError(t *testing.T) {
	o := opts()
	o.Style = &Style{Slug: "bad", PromptSuffix: "glint, {sparkle"}
	_, err := Build(Request{Prompt: "1girl"}, o)
	var ce *ConfigError
	if !errors.As(err, &ce) || !strings.Contains(ce.Source, "bad") {
		t.Errorf("bad style row: %v, want a ConfigError naming the style", err)
	}
	o.Style = &Style{Slug: "hot", Negative: "(logo:2.5)"}
	if _, err := Build(Request{Prompt: "1girl"}, o); !errors.As(err, &ce) {
		t.Errorf("style weight above 2.0: %v, want ConfigError", err)
	}
}

// TestPerSurfaceReset: every surface starts at 1.0 — an open `w::` section
// in the user prompt never reaches the style suffix or a character.
func TestPerSurfaceReset(t *testing.T) {
	o := opts()
	o.Style = &Style{Slug: "s", PromptSuffix: "glint"}
	s, err := Build(Request{Prompt: "a, 1.5::b", Characters: []Character{{Prompt: "c"}, {Prompt: "d"}}}, o)
	if err != nil {
		t.Fatal(err)
	}
	last := s.PromptTags[len(s.PromptTags)-1]
	if last.Name != "glint" || last.Runs[0].Emitted != 1 {
		t.Errorf("style suffix %+v must stay at 1", last)
	}
	if s.Characters[0].Prompt != "c" || s.Characters[1].Prompt != "d" {
		t.Errorf("characters %q %q must stay at 1", s.Characters[0].Prompt, s.Characters[1].Prompt)
	}
	if !strings.Contains(s.Prompt, "(b:1.3393)") {
		t.Errorf("prompt %q", s.Prompt)
	}
}

// TestWeightedEmission: escaping, calibration per lineage, the user's
// example, and notices with their fields.
func TestWeightedEmission(t *testing.T) {
	req := Request{Prompt: "castorice (honkai: star rail), 0.9::sleeveless, [0.71::long legs], {1.8::holding hands}"}
	p, err := ParseRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	s, notices, err := BuildParsed(req, p, opts())
	if err != nil {
		t.Fatal(err)
	}
	want := testQuality + `, castorice \(honkai: star rail\), (sleeveless:0.9), (long legs:0.71), (holding hands:1.3856)`
	if s.Prompt != want {
		t.Errorf("prompt = %q\nwant     %q", s.Prompt, want)
	}
	codes := map[string]string{}
	for _, n := range notices {
		codes[n.Code] = n.Field
	}
	if codes[prompt.NoticeOpenSection] != "prompt" || codes[prompt.NoticeCalibrated] != "prompt" {
		t.Errorf("notices = %+v", notices)
	}
	if s.PromptTags[3].NAI != "0.9::sleeveless ::" {
		t.Errorf("NAI = %q", s.PromptTags[3].NAI)
	}

	for lineage, hi := range map[string]string{"illustrious": "(x:1.4)", "noobai": "(x:1.3)"} {
		o := opts()
		o.Lineage = lineage
		s, err := Build(Request{Prompt: "10::x ::", QualityTags: boolPtr(false)}, o)
		if err != nil || s.Prompt != hi || s.Calibration.Hi != lineageSpecs[lineage].calibration.Hi {
			t.Errorf("%s: %q %+v %v", lineage, s.Prompt, s.Calibration, err)
		}
	}
	// SD syntax from a style row keeps working (emitted units, identity)
	o := opts()
	o.Style = &Style{Slug: "hsr", Negative: "(logo:1.3)"}
	s, err = Build(Request{Prompt: "1girl"}, o)
	if err != nil || !strings.HasSuffix(s.Negative, ", (logo:1.3)") {
		t.Errorf("style negative: %q %v", s.Negative, err)
	}
}

// TestRelocation pins decision 1 end to end in assembly.
func TestRelocation(t *testing.T) {
	o := opts()
	o.Style = &Style{Slug: "s", Negative: "film grain"}
	req := Request{
		Prompt:     "a, b, -1::hat ::, -6::simple background ::",
		Negative:   "blurry",
		Characters: []Character{{Prompt: "x, (glasses:-1.5)"}, {Prompt: "y"}},
	}
	p, err := ParseRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	s, notices, err := BuildParsed(req, p, o)
	if err != nil {
		t.Fatal(err)
	}
	want := testForbidden + ", " + testUC + ", blurry, hat, (simple background:1.4), (glasses:1.3393), film grain"
	if s.Negative != want {
		t.Errorf("negative = %q\nwant       %q", s.Negative, want)
	}
	if strings.Contains(s.Prompt, "hat") || strings.Contains(s.Characters[0].Prompt, "glasses") {
		t.Errorf("relocated tags left in the positive: %q / %q", s.Prompt, s.Characters[0].Prompt)
	}
	var reloc []string
	for _, n := range notices {
		if n.Code == prompt.NoticeRelocated {
			reloc = append(reloc, n.Field)
		}
	}
	if strings.Join(reloc, ",") != "prompt,prompt,characters.0" {
		t.Errorf("relocation notices = %v", reloc)
	}
	for _, tg := range s.NegativeTags {
		if tg.Name == "hat" && (!tg.Relocated || tg.Source != prompt.User || tg.NAI != "hat") {
			t.Errorf("relocated tag = %+v", tg)
		}
	}
	// nothing positive left → the prompt is empty
	if _, err := Build(Request{Prompt: "-1::hat ::"}, opts()); err == nil {
		t.Error("a prompt that is all relocated must be refused as empty")
	}
	// a negative weight inside UC is a 400
	_, err = ParseRequest(Request{Prompt: "1girl", Negative: "-1::hat ::"})
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Field != "negative" || fe.Issue.Code != prompt.CodeNegativeInUC {
		t.Errorf("negative weight in UC: %v", err)
	}
}

func TestNegativeDedupe(t *testing.T) {
	req := Request{Prompt: "1girl", Negative: "lowres, {{lowres}}, [child], watermark, extra fingers"}
	p, err := ParseRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	s, notices, err := BuildParsed(req, p, opts())
	if err != nil {
		t.Fatal(err)
	}
	// exact DEFAULT_UC copies drop; a re-weighted one is the user's call
	want := testForbidden + ", " + testUC + ", (lowres:1.1003), extra fingers"
	if s.Negative != want {
		t.Errorf("negative = %q\nwant       %q", s.Negative, want)
	}
	n := 0
	for _, no := range notices {
		if no.Code == prompt.NoticeDuplicateUC && no.Field == "negative" {
			n++
		}
	}
	if n != 3 {
		t.Errorf("want 3 duplicate notices, got %+v", notices)
	}
}

func TestParseRequestFields(t *testing.T) {
	for _, tc := range []struct {
		req   Request
		field string
		code  string
	}{
		{Request{Prompt: "a}, b"}, "prompt", prompt.CodeStrayCloser},
		{Request{Prompt: "a", Negative: "{x"}, "negative", prompt.CodeUnclosedBracket},
		{Request{Prompt: "a", Characters: []Character{{Prompt: "x"}, {Prompt: "a | b"}}}, "characters.1", prompt.CodePipe},
		{Request{Prompt: "1.5::year 2000::"}, "prompt", prompt.CodeGluedNumber},
	} {
		_, err := ParseRequest(tc.req)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != tc.field || fe.Issue.Code != tc.code {
			t.Errorf("%+v: %v, want %s on %s", tc.req, err, tc.code, tc.field)
		}
	}
}

// TestSpecTagsMatchEmission: for every surface, the structured tags are
// what ComfyUI's grammar reads from the emitted strings.
func TestSpecTagsMatchEmission(t *testing.T) {
	o := opts()
	o.Style = &Style{Slug: "s", PromptPrefix: "1990s (style), official art", Negative: "(logo:1.3)"}
	s, err := Build(Request{
		Prompt:     "castorice (honkai: star rail), blue {hair}, (smile:1.2), artist:foo, 1.5::year 2000 ::",
		Negative:   "[blurry], 0.5::bad hands ::",
		Characters: []Character{{Prompt: "makima \\(chainsaw man\\), {{red hair}}"}, {Prompt: "z"}},
	}, o)
	if err != nil {
		t.Fatal(err)
	}
	for _, sf := range []struct {
		name, emitted string
		tags          []prompt.Tag
	}{{"prompt", s.Prompt, s.PromptTags}, {"negative", s.Negative, s.NegativeTags}, {"c0", s.Characters[0].Prompt, s.Characters[0].Tags}} {
		if err := prompt.Equivalent(sf.tags, comfyTags(t, sf.emitted), 5e-5); err != nil {
			t.Errorf("%s %q: %v", sf.name, sf.emitted, err)
		}
		for _, tg := range sf.tags {
			for _, r := range tg.Runs {
				if math.IsNaN(r.Emitted) || r.Emitted <= 0 || r.Emitted > 2 {
					t.Errorf("%s: %q emitted %v", sf.name, tg.Text, r.Emitted)
				}
			}
		}
	}
	if !strings.Contains(s.Prompt, `1990s \(style\)`) {
		t.Errorf("style prefix parens must be escaped: %q", s.Prompt)
	}
}
