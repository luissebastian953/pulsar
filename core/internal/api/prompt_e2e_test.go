package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"pulsar/core/internal/assembly"
	"pulsar/core/internal/compliance"
	"pulsar/core/internal/nltag"
	"pulsar/core/internal/prompt"
	"pulsar/core/internal/tags"
	"pulsar/core/internal/workflow"
)

func postJSON(t *testing.T, url, body string) (int, []byte) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

type parseResp struct {
	Tags []struct {
		Text  string `json:"text"`
		NAI   string `json:"nai"`
		Start int    `json:"start"`
		End   int    `json:"end"`
		Tag   int    `json:"tag"`
		Runs  []struct {
			Text    string  `json:"text"`
			W       float64 `json:"w"`
			Emitted float64 `json:"emitted"`
		} `json:"runs"`
	} `json:"tags"`
	Relocated []struct {
		Text string `json:"text"`
	} `json:"relocated"`
	Errors  []prompt.Issue `json:"errors"`
	Notices []prompt.Issue `json:"notices"`
	Emitted string         `json:"emitted"`
}

func (e *env) parse(t *testing.T, body string) parseResp {
	t.Helper()
	code, raw := postJSON(t, e.ts.URL+"/prompt/parse", body)
	if code != http.StatusOK {
		t.Fatalf("POST /prompt/parse %s = %d: %s", body, code, raw)
	}
	var out parseResp
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func codes(is []prompt.Issue) map[string]prompt.Issue {
	m := map[string]prompt.Issue{}
	for _, i := range is {
		m[i.Code] = i
	}
	return m
}

// TestPromptParseEndpoint: orbit's live validator — the same parser and
// calibration as /generate, with UTF-16 spans and piece indices.
func TestPromptParseEndpoint(t *testing.T) {
	e := newEnv(t, "e2e_prompt_parse", compliance.Stub{})

	// the user's example: no error; weights; open-section + calibration notices
	out := e.parse(t, `{"text": "0.9::sleeveless, [0.71::long legs], {1.8::holding hands}", "surface": "prompt", "lineage": "illustrious"}`)
	if len(out.Errors) != 0 || len(out.Tags) != 3 {
		t.Fatalf("user example: %+v", out)
	}
	for i, want := range []struct {
		text  string
		w, em float64
	}{{"sleeveless", 0.9, 0.9}, {"long legs", 0.71, 0.71}, {"holding hands", 1.8, 1.3856}} {
		g := out.Tags[i]
		if g.Text != want.text || g.Runs[0].W != want.w || g.Runs[0].Emitted != want.em || g.Tag != i {
			t.Errorf("tag %d = %+v, want %+v", i, g, want)
		}
	}
	n := codes(out.Notices)
	if _, ok := n[prompt.NoticeOpenSection]; !ok {
		t.Errorf("notices %+v lack open_section", out.Notices)
	}
	if c, ok := n[prompt.NoticeCalibrated]; !ok || c.Tag != 2 {
		t.Errorf("notices %+v lack calibrated on tag 2", out.Notices)
	}
	if out.Emitted != "(sleeveless:0.9), (long legs:0.71), (holding hands:1.3856)" {
		t.Errorf("emitted = %q", out.Emitted)
	}
	// noobai's lower ceiling
	out = e.parse(t, `{"text": "{1.8::holding hands}", "lineage": "noobai"}`)
	if out.Tags[0].Runs[0].Emitted != 1.2971 {
		t.Errorf("noobai emitted = %v", out.Tags[0].Runs[0].Emitted)
	}

	// errors carry UTF-16 offsets into the surface and the piece index
	out = e.parse(t, `{"text": "1girl, 😀 red}", "surface": "prompt"}`)
	if len(out.Errors) != 1 || out.Errors[0].Code != prompt.CodeStrayCloser || out.Errors[0].Start != 13 || out.Errors[0].End != 14 || out.Errors[0].Tag != 1 {
		t.Errorf("stray closer = %+v", out.Errors)
	}
	if len(out.Tags) != 0 {
		t.Errorf("tags with errors = %+v", out.Tags)
	}
	// negative weights: relocated from a prompt, rejected in UC
	out = e.parse(t, `{"text": "1girl, -1::hat ::", "surface": "character"}`)
	if len(out.Relocated) != 1 || out.Relocated[0].Text != "hat" {
		t.Errorf("relocated = %+v", out.Relocated)
	}
	out = e.parse(t, `{"text": "lowres, -1::hat ::", "surface": "negative"}`)
	if len(out.Errors) != 1 || out.Errors[0].Code != prompt.CodeNegativeInUC {
		t.Errorf("negative weight in UC = %+v", out.Errors)
	}
	// SD syntax converts with a notice; parens are escaped on the way out
	out = e.parse(t, `{"text": "(castorice (honkai: star rail):1.2)"}`)
	if _, ok := codes(out.Notices)[prompt.NoticeConvertedSD]; !ok || out.Emitted != `(castorice \(honkai: star rail\):1.1848)` {
		t.Errorf("SD convert: %q %+v", out.Emitted, out.Notices)
	}

	for _, bad := range []string{`{"text": "x", "surface": "nope"}`, `{"text": "x", "lineage": "pony"}`, `{"text": "x", "extra": 1}`} {
		if code, _ := postJSON(t, e.ts.URL+"/prompt/parse", bad); code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", bad, code)
		}
	}
}

// TestGenerateParseErrorShape: /generate answers a parse error with 400
// and a located body; `error` stays the message for older clients.
func TestGenerateParseErrorShape(t *testing.T) {
	e := newEnv(t, "e2e_prompt_400", compliance.Stub{})
	for _, tc := range []struct {
		body, field, code string
		start, end, tag   int
	}{
		{`{"prompt": "1girl, red}"}`, "prompt", prompt.CodeStrayCloser, 10, 11, 1},
		{`{"prompt": "1girl", "negative": "lowres, {blurry"}`, "negative", prompt.CodeUnclosedBracket, 8, 9, 1},
		{`{"prompt": "a, b", "characters": [{"prompt": "x"}, {"prompt": "y, 1.5::year 2000::"}]}`, "characters.1", prompt.CodeGluedNumber, 13, 19, 1},
		{`{"prompt": "1girl, a | b"}`, "prompt", prompt.CodePipe, 9, 10, 1},
		{`{"prompt": "1girl", "negative": "-1::hat ::"}`, "negative", prompt.CodeNegativeInUC, 4, 7, 0},
	} {
		code, raw := postJSON(t, e.ts.URL+"/generate", tc.body)
		var body struct {
			Error, Code, Field, Hint string
			Start, End, Tag          int
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		if code != http.StatusBadRequest || body.Field != tc.field || body.Code != tc.code ||
			body.Start != tc.start || body.End != tc.end || body.Tag != tc.tag || body.Error == "" {
			t.Errorf("%s = %d %s, want %s on %s [%d,%d) tag %d", tc.body, code, raw, tc.code, tc.field, tc.start, tc.end, tc.tag)
		}
	}
}

// TestGenerateNotices: the 202 carries the parse/assembly notices, and the
// relocated tag lands in the stored negative after the safety block.
func TestGenerateNotices(t *testing.T) {
	e := newEnv(t, "e2e_prompt_notices", compliance.Stub{})
	code, raw := postJSON(t, e.ts.URL+"/generate", `{"prompt": "1girl, -1::hat ::, (smile:1.2)", "seed": 3}`)
	if code != http.StatusAccepted {
		t.Fatalf("generate = %d %s", code, raw)
	}
	var out struct {
		JobID   string            `json:"job_id"`
		Notices []assembly.Notice `json:"notices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, n := range out.Notices {
		got[n.Code] = n.Field
	}
	if got[prompt.NoticeRelocated] != "prompt" || got[prompt.NoticeConvertedSD] != "prompt" {
		t.Errorf("notices = %+v", out.Notices)
	}
	v := e.waitTerminal(t, out.JobID)
	var spec assembly.Spec
	if err := json.Unmarshal(v.Params, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Negative != "loli, shota, child, hat" || strings.Contains(spec.Prompt, "hat") {
		t.Errorf("stored prompt %q / negative %q", spec.Prompt, spec.Negative)
	}
	// clean requests still return an (empty) notices list
	code, raw = postJSON(t, e.ts.URL+"/generate", `{"prompt": "1girl"}`)
	if code != http.StatusAccepted || !strings.Contains(string(raw), `"notices":[]`) {
		t.Errorf("clean generate = %d %s", code, raw)
	}
}

// recordChecker records every Spec CheckPrompt sees.
type recordChecker struct {
	compliance.Stub
	mu    sync.Mutex
	specs []assembly.Spec
}

func (r *recordChecker) CheckPrompt(_ context.Context, s assembly.Spec) (compliance.Decision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.specs = append(r.specs, s)
	return compliance.Decision{Allowed: true}, nil
}

func (r *recordChecker) last() assembly.Spec {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.specs[len(r.specs)-1]
}

// TestEveryTextInputIsChecked guards future patch points: for every kind
// and fragment, each text input in the graph sent to ComfyUI is one of the
// strings CheckPrompt scanned (spec.Prompt, spec.Negative, a character
// prompt) — nothing reaches the encoder unchecked.
func TestEveryTextInputIsChecked(t *testing.T) {
	rec := &recordChecker{}
	e := newEnv(t, "e2e_text_inputs", rec)
	img := e.uploadPNG(t, 100, 150)
	mask := e.uploadPNG(t, 100, 150)
	ref := e.uploadPNG(t, 100, 150)
	for _, body := range []string{
		`{"prompt": "1girl, {smile}", "negative": "[blurry]"}`,
		`{"prompt": "1girl, standing", "kind": "pose"}`,
		`{"prompt": "1girl", "kind": "img2img", "image_id": "` + img + `"}`,
		`{"prompt": "1girl", "kind": "inpaint", "image_id": "` + img + `", "mask_id": "` + mask + `"}`,
		`{"prompt": "1girl", "reference_id": "` + ref + `"}`,
		`{"prompt": "1girl", "hires": true}`,
		`{"prompt": "2girls", "characters": [{"prompt": "a, (b:1.2)"}, {"prompt": "c"}]}`,
		`{"prompt": "3girls", "characters": [{"prompt": "a"}, {"prompt": "b"}, {"prompt": "c, [d]"}], "hires": true}`,
	} {
		id := e.generate(t, body)
		if v := e.waitTerminal(t, id); v.State != "done" {
			t.Fatalf("%s: %s %s", body, v.State, v.Error)
		}
		spec := rec.last()
		checked := map[string]bool{spec.Prompt: true, spec.Negative: true}
		for _, c := range spec.Characters {
			checked[c.Prompt] = true
		}
		var g workflow.Graph
		if err := json.Unmarshal(e.fake.LastGraph(), &g); err != nil {
			t.Fatal(err)
		}
		texts := 0
		for id, n := range g {
			v, ok := n.Inputs["text"]
			if !ok {
				continue
			}
			texts++
			s, isStr := v.(string)
			if !isStr || !checked[s] {
				t.Errorf("%s: node %s (%s) text %v was never checked", body, id, n.ClassType, v)
			}
		}
		if texts < 2 {
			t.Errorf("%s: found %d text inputs", body, texts)
		}
	}
}

// TestHistoryCarriesSources: the stored Spec carries the structured tags,
// so history restores the UC box from user-sourced negative tags only —
// never the safety block or the preset.
func TestHistoryCarriesSources(t *testing.T) {
	e := newEnv(t, "e2e_history_tags", compliance.Stub{})
	e.srv.Cfg.DefaultUC = "lowres, watermark"
	// a legacy-style round-trip: the whole stored negative re-sent as user text
	id := e.generate(t, `{"prompt": "1girl, {smile}", "negative": "loli, shota, child, lowres, watermark, blurry, 1.2::bad hands ::"}`)
	e.waitTerminal(t, id)
	resp, err := http.Get(e.ts.URL + "/history")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var hist struct {
		Jobs []struct {
			Params assembly.Spec `json:"params"`
		} `json:"jobs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&hist); err != nil || len(hist.Jobs) == 0 {
		t.Fatalf("history: %v", err)
	}
	p := hist.Jobs[0].Params
	var userNeg, userPos []string
	for _, tg := range p.NegativeTags {
		if tg.Source == prompt.User {
			userNeg = append(userNeg, tg.NAI)
		}
	}
	for _, tg := range p.PromptTags {
		if tg.Source == prompt.User {
			userPos = append(userPos, tg.NAI)
		}
	}
	if strings.Join(userNeg, ", ") != "blurry, 1.2::bad hands ::" {
		t.Errorf("user negative = %v (stored %q)", userNeg, p.Negative)
	}
	if strings.Join(userPos, ", ") != "1girl, 1.05::smile ::" {
		t.Errorf("user prompt = %v", userPos)
	}
	if p.Negative != "loli, shota, child, lowres, watermark, blurry, (bad hands:1.1848)" {
		t.Errorf("stored negative = %q: safety once, preset once, then the user's", p.Negative)
	}
}

// TestWeightedCharacterAnchors: autoAnchor reads parsed tags, so a
// weighted post-cutoff character still gets its design anchors.
func TestWeightedCharacterAnchors(t *testing.T) {
	e := newEnv(t, "e2e_anchor_weighted", compliance.Stub{})
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `INSERT INTO tags (name, category, post_count, pose_relevant, danbooru_created_at)
		VALUES ('newchar_(series)', 'character', 900, false, '2026-06-01')`); err != nil {
		t.Fatal(err)
	}
	danbooru := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/posts.json" {
			http.NotFound(w, r)
			return
		}
		posts := make([]map[string]string, 6)
		for i := range posts {
			posts[i] = map[string]string{"tag_string_general": "1girl solo white_hair yellow_eyes smile"}
		}
		json.NewEncoder(w).Encode(posts)
	}))
	t.Cleanup(danbooru.Close)
	e.srv.Live = tags.NewLive(e.pool, danbooru.URL, 50, "loli, shota, child")

	id := e.generate(t, `{"prompt": "1girl, 1.2::newchar (series) ::, smile", "auto_reference": false}`)
	v := e.waitTerminal(t, id)
	var spec assembly.Spec
	if err := json.Unmarshal(v.Params, &spec); err != nil {
		t.Fatal(err)
	}
	want := `1girl, (newchar \(series\):1.1848), white hair, yellow eyes, smile`
	if !strings.Contains(spec.Prompt, want) {
		t.Errorf("prompt = %q, want it to contain %q", spec.Prompt, want)
	}
	anchors := 0
	for _, tg := range spec.PromptTags {
		if tg.Source == prompt.Anchor {
			anchors++
		}
	}
	if anchors != 2 {
		t.Errorf("anchor tags = %d in %+v", anchors, spec.PromptTags)
	}
}

// mapDecomposer decomposes by exact input and records what it was sent.
type mapDecomposer struct {
	mu   sync.Mutex
	by   map[string][]string
	seen []string
}

func (m *mapDecomposer) Decompose(_ context.Context, text string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seen = append(m.seen, text)
	return m.by[text], nil
}

// TestTranslateWeights: /translate parses before nltag; weighted stretches
// are grounded separately and inherit their weight; the LLM never sees an
// operator; the response carries NovelAI syntax.
func TestTranslateWeights(t *testing.T) {
	e := newEnv(t, "e2e_translate_weights", compliance.Stub{})
	if _, err := e.pool.Exec(context.Background(), `
		INSERT INTO tags (name, category, post_count, pose_relevant) VALUES
		('silver_hair', 'general', 500000, false),
		('sitting',     'general', 2000000, true),
		('hat',         'general', 900000, false)`); err != nil {
		t.Fatal(err)
	}
	dec := &mapDecomposer{by: map[string][]string{
		"a girl with silver hair": {"silver hair"},
		"sitting on a bench":      {"sitting", "ethereal vibes"},
		"a hat":                   {"hat"},
	}}
	e.srv.Translator = &nltag.Translator{Decomposer: dec, Grounder: &nltag.Grounder{Store: tags.NewStore(e.pool)}}

	code, raw := postJSON(t, e.ts.URL+"/translate", `{"text": "a girl with silver hair, 1.3::sitting on a bench ::, -1::a hat ::"}`)
	if code != http.StatusOK {
		t.Fatalf("translate = %d %s", code, raw)
	}
	var out struct {
		Tags []struct {
			Canonical string  `json:"canonical"`
			Weight    float64 `json:"weight"`
		} `json:"tags"`
		Dropped []string `json:"dropped"`
		Prompt  string   `json:"prompt"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Prompt != "1.3::sitting ::, -1::hat ::, silver_hair" {
		t.Errorf("prompt = %q (tags %+v)", out.Prompt, out.Tags)
	}
	if len(out.Dropped) != 1 || out.Dropped[0] != "ethereal vibes" {
		t.Errorf("dropped = %v", out.Dropped)
	}
	for _, s := range dec.seen {
		if strings.ContainsAny(s, "{}[]") || strings.Contains(s, "::") {
			t.Errorf("the LLM saw an operator: %q", s)
		}
	}
	// a parse error is the same located 400 as /generate
	code, raw = postJSON(t, e.ts.URL+"/translate", `{"text": "silver hair}"}`)
	if code != http.StatusBadRequest || !strings.Contains(string(raw), `"field":"prompt"`) || !strings.Contains(string(raw), `"code":"stray_closer"`) {
		t.Errorf("translate parse error = %d %s", code, raw)
	}
}
