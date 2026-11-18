package compliance

import (
	"context"
	"errors"
	"strings"
	"testing"

	"pulsar/core/internal/assembly"
	"pulsar/core/internal/prompt"
	"pulsar/core/internal/tags"
)

// Placeholder fixture lists — never real terms. The watch list is injected
// as FORBIDDEN_TAG_UC, the complement list as AGE_COMPLEMENT_TAGS.
const (
	testWatch      = "testwatch, test watch two"
	testComplement = "testadult"
)

func policy() *Prompts { return NewPrompts(testWatch, testComplement) }

func decide(t *testing.T, p *Prompts, req assembly.Request) Decision {
	t.Helper()
	dec, err := p.Check(build(t, testWatch, req))
	if err != nil {
		t.Fatalf("%+v: check error %v", req, err)
	}
	return dec
}

// TestWatchedTagEveryForm: a watched tag is rejected in every syntax the
// grammar offers — weight never launders it (S7 step 7).
func TestWatchedTagEveryForm(t *testing.T) {
	p := policy()
	for _, req := range []assembly.Request{
		{Prompt: "1girl, testwatch"},
		{Prompt: "1girl, TestWatch"},
		{Prompt: "1girl, {testwatch}"},
		{Prompt: "1girl, [[testwatch]]"},
		{Prompt: "1girl, 1.5::testwatch ::"},
		{Prompt: "1girl, 0.3::testwatch ::"},
		{Prompt: "1girl, 1.5::smile, testwatch"},
		{Prompt: "1girl, (testwatch:1.2)"},
		{Prompt: "1girl, (testwatch:0.2)"},
		{Prompt: "1girl, \\(testwatch\\)"},
		{Prompt: "1girl, (testwatch)"},
		{Prompt: "1girl, test{watch}"},
		{Prompt: "1girl, test 1.2::watch ::"},
		{Prompt: "1girl, test_watch_two"},
		{Prompt: "1girl, {test watch} two"},
		{Prompt: "a, b", Characters: []assembly.Character{{Prompt: "x"}, {Prompt: "{testwatch}"}}},
		{Prompt: "a, b", Characters: []assembly.Character{{Prompt: "(test watch two:0.5)"}, {Prompt: "y"}}},
	} {
		if dec := decide(t, p, req); dec.Allowed {
			t.Errorf("%+v: watched tag allowed", req)
		}
	}
	if dec := decide(t, p, assembly.Request{Prompt: "1girl, testwatcher, test_watch_twofold, smile"}); !dec.Allowed {
		t.Errorf("a longer tag that merely contains a watch entry must pass (exact match forms): %s", dec.Reason)
	}
}

// TestSafetyBlockAllowed: the watched tags inside the mandatory safety
// block are the point, not a violation.
func TestSafetyBlockAllowed(t *testing.T) {
	if dec := decide(t, policy(), assembly.Request{Prompt: "1girl, smile", Negative: "blurry"}); !dec.Allowed {
		t.Errorf("clean request rejected: %s", dec.Reason)
	}
}

// TestFloorTermWithoutEnv: the hard-coded floor holds even when the env
// value lacks a term — in the prompt and in the negative (S7 steps 5, 8).
func TestFloorTermWithoutEnv(t *testing.T) {
	p := policy()
	for _, term := range tags.WatchFloor() {
		text := strings.ReplaceAll(term, "_", " ")
		if dec := decide(t, p, assembly.Request{Prompt: "1girl, {" + text + "}"}); dec.Allowed {
			t.Errorf("floor term #%d allowed in the prompt", len(term))
		}
		// not in this env's safety block, so dedupe keeps it: a watched tag
		// in the negative outside the safety block rejects
		if dec := decide(t, p, assembly.Request{Prompt: "1girl", Negative: text}); dec.Allowed {
			t.Errorf("floor term #%d allowed in the negative outside the safety block", len(term))
		}
	}
}

// TestComplementRules: env-listed complement tags (decision 5) — anywhere
// in the negative rejects; down-weighted in a positive rejects;
// up-weighted or plain in a positive passes.
func TestComplementRules(t *testing.T) {
	p := policy()
	for _, req := range []assembly.Request{
		{Prompt: "1girl", Negative: "testadult"},
		{Prompt: "1girl", Negative: "{testadult}"},
		{Prompt: "1girl", Negative: "0.5::testadult ::"},
		{Prompt: "1girl, [testadult]"},
		{Prompt: "1girl, 0.9::testadult ::"},
		{Prompt: "1girl, (testadult:0.8)"},
		{Prompt: "1girl, -1::testadult ::"}, // relocated into the negative
		{Prompt: "a, b", Characters: []assembly.Character{{Prompt: "[testadult]"}, {Prompt: "y"}}},
	} {
		if dec := decide(t, p, req); dec.Allowed {
			t.Errorf("%+v: complement rule did not reject", req)
		}
	}
	for _, req := range []assembly.Request{
		{Prompt: "1girl, testadult"},
		{Prompt: "1girl, {testadult}"},
		{Prompt: "1girl, 1.3::testadult ::"},
	} {
		if dec := decide(t, p, req); !dec.Allowed {
			t.Errorf("%+v: complement at weight ≥ 1 must pass: %s", req, dec.Reason)
		}
	}
	// the list is env-only: unset means no complement rules
	if dec := decide(t, NewPrompts(testWatch, ""), assembly.Request{Prompt: "1girl", Negative: "testadult"}); !dec.Allowed {
		t.Errorf("empty AGE_COMPLEMENT_TAGS must not apply complement rules: %s", dec.Reason)
	}
}

// TestDifferential: the checker never trusts the structured fields alone —
// a Spec whose strings and tags disagree is rejected (S7 step 3).
func TestDifferential(t *testing.T) {
	p := policy()
	good := build(t, testWatch, assembly.Request{Prompt: "1girl, smile", Negative: "blurry"})
	if dec, err := p.Check(good); err != nil || !dec.Allowed {
		t.Fatalf("baseline: %+v %v", dec, err)
	}
	for name, mutate := range map[string]func(*assembly.Spec){
		"string has an extra tag": func(s *assembly.Spec) { s.Prompt += ", testwatch" },
		"string weight differs":   func(s *assembly.Spec) { s.Prompt = strings.Replace(s.Prompt, "smile", "(smile:1.3)", 1) },
		"unescaped group in text": func(s *assembly.Spec) { s.Prompt = strings.Replace(s.Prompt, "smile", "smile (x)", 1) },
		"tags hide a tag":         func(s *assembly.Spec) { s.PromptTags = s.PromptTags[:1] },
		"negative weight changed": func(s *assembly.Spec) { s.NegativeTags[len(s.NegativeTags)-1].Runs[0].Emitted = 0.5 },
		"character string differs": func(s *assembly.Spec) {
			s.Characters = []assembly.Character{{Prompt: "x, testwatch", Tags: s.PromptTags[:1]}}
		},
	} {
		s := build(t, testWatch, assembly.Request{Prompt: "1girl, smile", Negative: "blurry"})
		mutate(&s)
		if dec, err := p.Check(s); err == nil && dec.Allowed {
			t.Errorf("%s: allowed", name)
		}
	}
	// missing structure is an error (503, fail closed), not a pass
	if _, err := p.Check(assembly.Spec{Prompt: "1girl", Negative: "testwatch, test watch two"}); err == nil {
		t.Error("a Spec without structured tags must error")
	}
}

// TestSafetyPosition: the first len(safety) parsed negative tags must be
// FORBIDDEN_TAG_UC in order at weight 1 (S7 step 4).
func TestSafetyPosition(t *testing.T) {
	p := policy()
	tag := func(text string, w float64, src prompt.Source) prompt.Tag {
		return prompt.Tag{Text: text, Name: prompt.Normalize(text), Source: src, Runs: []prompt.Run{{Text: text, Weight: w, Emitted: w}}}
	}
	base := build(t, testWatch, assembly.Request{Prompt: "1girl"})
	for name, neg := range map[string][]prompt.Tag{
		"reordered":     {tag("test watch two", 1, prompt.Safety), tag("testwatch", 1, prompt.Safety)},
		"up-weighted":   {tag("testwatch", 1.05, prompt.Safety), tag("test watch two", 1, prompt.Safety)},
		"down-weighted": {tag("testwatch", 1, prompt.Safety), tag("test watch two", 0.9524, prompt.Safety)},
		"preceded":      {tag("lowres", 1, prompt.DefaultUC), tag("testwatch", 1, prompt.Safety), tag("test watch two", 1, prompt.Safety)},
		"truncated":     {tag("testwatch", 1, prompt.Safety)},
		"copy after":    {tag("testwatch", 1, prompt.Safety), tag("test watch two", 1, prompt.Safety), tag("testwatch", 0.5, prompt.User)},
	} {
		s := base
		s.NegativeTags = neg
		emitted, err := prompt.EmitComfy(neg)
		if err != nil {
			t.Fatal(err)
		}
		s.Negative = emitted
		if dec, err := p.Check(s); err == nil && dec.Allowed {
			t.Errorf("%s negative %q allowed", name, emitted)
		}
	}
}

func TestEmbeddingAndAmbiguity(t *testing.T) {
	p := policy()
	s := build(t, testWatch, assembly.Request{Prompt: "1girl"})
	s.Prompt = "1girl, Embedding:foo"
	s.PromptTags = append(s.PromptTags, prompt.Tag{Text: "Embedding:foo", Runs: []prompt.Run{{Text: "Embedding:foo", Weight: 1, Emitted: 1}}})
	if dec, err := p.Check(s); err == nil && dec.Allowed {
		t.Error("embedding: must reject")
	}
	s = build(t, testWatch, assembly.Request{Prompt: "1girl"})
	s.Prompt = "(1girl:١)" // Python would read this weight; the port refuses to guess
	if _, err := p.Check(s); !errors.Is(err, prompt.ErrComfyAmbiguous) {
		t.Errorf("ambiguous ComfyUI reading must error (fail closed), got %v", err)
	}
}

// TestPromptGuard: the dev checker runs the same prompt policy without the
// output sidecar.
func TestPromptGuard(t *testing.T) {
	g := PromptGuard{Prompts: policy()}
	if dec, err := g.CheckPrompt(context.Background(), build(t, testWatch, assembly.Request{Prompt: "{testwatch}"})); err != nil || dec.Allowed {
		t.Errorf("PromptGuard must apply the prompt policy: %+v %v", dec, err)
	}
	if _, err := (PromptGuard{}).CheckPrompt(context.Background(), assembly.Spec{}); err == nil {
		t.Error("PromptGuard without a policy must fail closed")
	}
}
