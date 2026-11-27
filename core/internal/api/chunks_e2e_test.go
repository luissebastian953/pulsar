package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"pulsar/core/internal/assembly"
	"pulsar/core/internal/compliance"
	"pulsar/core/internal/nltag"
	"pulsar/core/internal/prompt"
	"pulsar/core/internal/tags"
	"pulsar/core/internal/workflow"
)

func chunksJSON(pairs ...string) string {
	var cs []prompt.Chunk
	for i := 0; i+1 < len(pairs); i += 2 {
		cs = append(cs, prompt.Chunk{Name: pairs[i], Content: pairs[i+1]})
	}
	b, _ := json.Marshal(cs)
	return string(b)
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestGenerateChunkErrorShape: an error inside a chunk — or about the
// reference itself — is the located 400, on the field, with the span of
// the `!macro:Name!` the user typed.
func TestGenerateChunkErrorShape(t *testing.T) {
	e := newEnv(t, "e2e_chunk_400", compliance.Stub{})
	chunks := chunksJSON("Bad", "red, blue}", "Neg", "-1::hat ::", "A", "x, !macro:B!", "B", "y, !macro:A!", "Glued", "year 2000::")
	for _, tc := range []struct {
		body, field, code string
		start, end, tag   int
	}{
		{`{"prompt": "1girl, !macro:Bad!, smile", "chunks": ` + chunks + `}`, "prompt", prompt.CodeStrayCloser, 7, 18, 1},
		{`{"prompt": "1girl", "negative": "lowres, !macro:Neg!", "chunks": ` + chunks + `}`, "negative", prompt.CodeNegativeInUC, 8, 19, 1},
		{`{"prompt": "a, b", "characters": [{"prompt": "x"}, {"prompt": "😀, !macro:Glued!"}], "chunks": ` + chunks + `}`, "characters.1", prompt.CodeGluedNumber, 4, 17, 1},
		{`{"prompt": "1girl, !macro:Nope!", "chunks": ` + chunks + `}`, "prompt", prompt.CodeUnknownChunk, 7, 19, 1},
		{`{"prompt": "1girl, !macro:Bad!"}`, "prompt", prompt.CodeUnknownChunk, 7, 18, 1},
		{`{"prompt": "!macro:A!", "chunks": ` + chunks + `}`, "prompt", prompt.CodeChunkCycle, 0, 9, 0},
		{`{"prompt": "1girl, !macro:Bad", "chunks": ` + chunks + `}`, "prompt", prompt.CodeChunkSyntax, 7, 17, 1},
		{`{"prompt": "1girl, standing, !macro:Bad!", "kind": "pose", "chunks": ` + chunks + `}`, "prompt", prompt.CodeStrayCloser, 17, 28, 2},
	} {
		code, raw := postJSON(t, e.ts.URL+"/generate", tc.body)
		var body struct {
			Error, Code, Field, Message string
			Start, End, Tag             int
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		if code != http.StatusBadRequest || body.Field != tc.field || body.Code != tc.code ||
			body.Start != tc.start || body.End != tc.end || body.Tag != tc.tag || body.Error == "" {
			t.Errorf("%s = %d %s, want %s on %s [%d,%d) tag %d", tc.body, code, raw, tc.code, tc.field, tc.start, tc.end, tc.tag)
		}
	}
	// depth 9
	var deep []string
	for i := 1; i <= 9; i++ {
		c := "t"
		if i < 9 {
			c = fmt.Sprintf("t, !macro:L%d!", i+1)
		}
		deep = append(deep, fmt.Sprintf("L%d", i), c)
	}
	code, raw := postJSON(t, e.ts.URL+"/generate", `{"prompt": "!macro:L1!", "chunks": `+chunksJSON(deep...)+`}`)
	if code != http.StatusBadRequest || !strings.Contains(string(raw), `"code":"chunk_depth"`) {
		t.Errorf("depth 9 = %d %s", code, raw)
	}
	// D4 limits at decode: bad name, duplicate, oversized, too many — plain 400s
	var many []string
	for i := 0; i <= prompt.MaxChunks; i++ {
		many = append(many, fmt.Sprintf("c%d", i), "x")
	}
	for _, bad := range []string{
		chunksJSON("a,b", "x"),
		chunksJSON("a", "x", "a", "y"),
		chunksJSON("a", strings.Repeat("x", prompt.MaxChunkBytes+1)),
		chunksJSON(many...),
		`[{"name": "a", "content": "x", "extra": 1}]`,
	} {
		for _, path := range []string{"/generate", "/prompt/parse"} {
			body := `{"prompt": "1girl", "chunks": ` + bad + `}`
			if path == "/prompt/parse" {
				body = `{"text": "1girl", "chunks": ` + bad + `}`
			}
			if code, raw := postJSON(t, e.ts.URL+path, body); code != http.StatusBadRequest || strings.Contains(string(raw), `"field"`) {
				t.Errorf("%s with chunks %.60s = %d %s, want a plain 400", path, bad, code, raw)
			}
		}
	}
}

// TestChunkTextInputsAreChecked is TestEveryTextInputIsChecked with chunks
// in every surface: every text input patched into the graph is a string
// CheckPrompt scanned, the chunk's content is in it, and no reference token
// ever reaches ComfyUI.
func TestChunkTextInputsAreChecked(t *testing.T) {
	rec := &recordChecker{}
	e := newEnv(t, "e2e_chunk_text_inputs", rec)
	img := e.uploadPNG(t, 100, 150)
	mask := e.uploadPNG(t, 100, 150)
	ref := e.uploadPNG(t, 100, 150)
	chunks := `, "chunks": ` + chunksJSON("Pos", "chunkpos one, {chunkpos two}", "Neg", "chunkneg one, [chunkneg two]",
		"Char", "chunkchar one, !macro:Pos!")
	for _, tc := range []struct {
		body string
		want []string // chunk content that must reach a checked text input
	}{
		{`{"prompt": "1girl, !macro:Pos!", "negative": "!macro:Neg!"`, []string{"chunkpos one", "chunkpos two", "chunkneg one", "chunkneg two"}},
		{`{"prompt": "1girl, standing, !macro:Pos!", "negative": "!macro:Neg!", "kind": "pose"`, []string{"chunkpos one", "chunkneg two"}},
		{`{"prompt": "!macro:Pos!", "negative": "x, !macro:Neg!", "kind": "img2img", "image_id": "` + img + `"`, []string{"chunkpos one", "chunkneg one"}},
		{`{"prompt": "!macro:Pos!", "kind": "inpaint", "image_id": "` + img + `", "mask_id": "` + mask + `"`, []string{"chunkpos two"}},
		{`{"prompt": "{!macro:Pos!}", "reference_id": "` + ref + `"`, []string{"chunkpos one"}},
		{`{"prompt": "1.3::!macro:Pos! ::", "negative": "!macro:Neg!", "hires": true`, []string{"chunkpos one", "chunkneg one"}},
		{`{"prompt": "2girls", "characters": [{"prompt": "a, !macro:Char!"}, {"prompt": "!macro:Pos!, c"}]`, []string{"chunkchar one", "chunkpos two"}},
		{`{"prompt": "3girls, !macro:Pos!", "negative": "!macro:Neg!", "characters": [{"prompt": "!macro:Char!"}, {"prompt": "b"}, {"prompt": "c, [!macro:Pos!]"}], "hires": true`,
			[]string{"chunkchar one", "chunkpos one", "chunkneg one"}},
	} {
		body := tc.body + chunks + `}`
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
		var all strings.Builder
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
			if strings.Contains(s, "!macro") {
				t.Errorf("%s: a reference token reached ComfyUI: %q", body, s)
			}
			all.WriteString(s + "\n")
		}
		if texts < 2 {
			t.Errorf("%s: found %d text inputs", body, texts)
		}
		for _, w := range tc.want {
			if !strings.Contains(all.String(), w) {
				t.Errorf("%s: chunk content %q never reached a text input:\n%s", body, w, all.String())
			}
		}
		// the safety block still leads every negative, at weight 1
		if !strings.HasPrefix(spec.Negative, e.srv.Cfg.ForbiddenTagUC) {
			t.Errorf("%s: negative %q lost the safety block", body, spec.Negative)
		}
	}
}

// lateGuard is the dev prompt policy, built from the env's own config once
// the env exists.
type lateGuard struct {
	compliance.Stub
	p *compliance.Prompts
}

func (g *lateGuard) CheckPrompt(_ context.Context, s assembly.Spec) (compliance.Decision, error) {
	return g.p.Check(s)
}

// TestWatchedTagInChunkRejected: chunk content is covered by CheckPrompt
// exactly like typed text — a watched tag hidden in a chunk (weighted,
// nested, in a character) is a 422. Terms come from config and the floor.
func TestWatchedTagInChunkRejected(t *testing.T) {
	g := &lateGuard{}
	e := newEnv(t, "e2e_chunk_watch", g)
	g.p = compliance.NewPrompts(e.srv.Cfg.ForbiddenTagUC, "")

	if code, raw := postJSON(t, e.ts.URL+"/generate", `{"prompt": "1girl, !macro:Ok!", "negative": "!macro:Ok!", "chunks": `+chunksJSON("Ok", "smile, blurry")+`}`); code != http.StatusAccepted {
		t.Fatalf("clean chunk = %d %s", code, raw)
	}
	for i, term := range safetyTerms(e) {
		text := strings.ReplaceAll(term, "_", " ")
		for _, tc := range []struct{ name, body string }{
			{"plain", `{"prompt": "1girl, !macro:W!", "chunks": ` + chunksJSON("W", "smile, "+text) + `}`},
			{"weighted inside", `{"prompt": "1girl, !macro:W!", "chunks": ` + chunksJSON("W", "0.2::"+text+" ::") + `}`},
			{"weighted outside", `{"prompt": "1girl, [[!macro:W!]]", "chunks": ` + chunksJSON("W", text) + `}`},
			{"nested", `{"prompt": "1girl, !macro:Outer!", "chunks": ` + chunksJSON("Outer", "smile, !macro:W!", "W", "{"+text+"}") + `}`},
			{"character", `{"prompt": "a, b", "characters": [{"prompt": "x"}, {"prompt": "y, !macro:W!"}], "chunks": ` + chunksJSON("W", text) + `}`},
			{"relocated", `{"prompt": "1girl, !macro:W!", "chunks": ` + chunksJSON("W", "smile, -1::"+text+" ::, hat") + `}`},
		} {
			code, raw := postJSON(t, e.ts.URL+"/generate", tc.body)
			// a relocated copy of a term already in this env's safety block is
			// deduped away (the block stays the only copy); every other case —
			// and a floor term outside the block — must reject
			if tc.name == "relocated" && strings.Contains(e.srv.Cfg.ForbiddenTagUC, term) {
				if code != http.StatusAccepted {
					t.Errorf("term #%d %s = %d %s, want the deduped copy accepted", i, tc.name, code, raw)
				}
				continue
			}
			if code != http.StatusUnprocessableEntity {
				t.Errorf("term #%d %s = %d, want 422", i, tc.name, code)
			}
		}
	}
}

// TestHistoryCarriesChunkProvenance: the stored Spec keeps the expanded
// tags as the record, each with `chunk {name, hash}`; `nai` still restores
// the tag as typed text.
func TestHistoryCarriesChunkProvenance(t *testing.T) {
	e := newEnv(t, "e2e_chunk_history", compliance.Stub{})
	content := "silver hair, {smile}"
	id := e.generate(t, `{"prompt": "1girl, !macro:Look!, outdoors", "negative": "!macro:Neg!, blurry", "chunks": `+
		chunksJSON("Look", content, "Neg", "1.2::bad hands ::")+`}`)
	e.waitTerminal(t, id)
	resp, err := http.Get(e.ts.URL + "/history")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var hist struct {
		Jobs []struct {
			Params json.RawMessage `json:"params"`
		} `json:"jobs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&hist); err != nil || len(hist.Jobs) == 0 {
		t.Fatalf("history: %v", err)
	}
	raw := hist.Jobs[0].Params
	if strings.Contains(string(raw), "!macro") {
		t.Errorf("a reference token was stored: %s", raw)
	}
	var p assembly.Spec
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	type row struct{ nai, chunk, hash string }
	user := func(ts []prompt.Tag) []row {
		var out []row
		for _, tg := range ts {
			if tg.Source != prompt.User {
				if tg.Chunk != nil {
					t.Errorf("non-user tag %q carries provenance", tg.Text)
				}
				continue
			}
			r := row{nai: tg.NAI}
			if tg.Chunk != nil {
				r.chunk, r.hash = tg.Chunk.Name, tg.Chunk.Hash
			}
			out = append(out, r)
		}
		return out
	}
	h := prompt.HashChunk(content)
	wantPos := []row{{"1girl", "", ""}, {"silver hair", "Look", h}, {"1.05::smile ::", "Look", h}, {"outdoors", "", ""}}
	if got := user(p.PromptTags); fmt.Sprint(got) != fmt.Sprint(wantPos) {
		t.Errorf("prompt tags = %v, want %v", got, wantPos)
	}
	wantNeg := []row{{"1.2::bad hands ::", "Neg", prompt.HashChunk("1.2::bad hands ::")}, {"blurry", "", ""}}
	if got := user(p.NegativeTags); fmt.Sprint(got) != fmt.Sprint(wantNeg) {
		t.Errorf("negative tags = %v, want %v", got, wantNeg)
	}
	// the wire shape orbit reads: prompt_tags[].chunk.{name,hash}
	var wire struct {
		PromptTags []struct {
			Chunk map[string]string `json:"chunk"`
		} `json:"prompt_tags"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil || len(wire.PromptTags) < 4 ||
		wire.PromptTags[3].Chunk["name"] != "Look" || wire.PromptTags[3].Chunk["hash"] != h || len(wire.PromptTags[3].Chunk) != 2 {
		t.Errorf("stored params lack the chunk field: %v %s", err, raw)
	}
	// /prompt/parse reports the same hash for the same chunk today — what
	// orbit compares before restoring the reference
	out := e.parseChunks(t, `{"text": "!macro:Look!", "chunks": `+chunksJSON("Look", content)+`}`)
	if len(out.Chunks) != 1 || out.Chunks[0].Hash != h {
		t.Errorf("parse hash = %+v, want %s", out.Chunks, h)
	}
}

type chunkParseResp struct {
	Tags []struct {
		Text  string           `json:"text"`
		NAI   string           `json:"nai"`
		Start int              `json:"start"`
		End   int              `json:"end"`
		Tag   int              `json:"tag"`
		Chunk *prompt.ChunkRef `json:"chunk"`
	} `json:"tags"`
	Errors []prompt.Issue `json:"errors"`
	Chunks []struct {
		Name, Hash, Text string
		Start, End, Tag  int
	} `json:"chunks"`
	Marks   []prompt.Mark `json:"marks"`
	Emitted string        `json:"emitted"`
}

func (e *env) parseChunks(t *testing.T, body string) chunkParseResp {
	t.Helper()
	code, raw := postJSON(t, e.ts.URL+"/prompt/parse", body)
	if code != http.StatusOK {
		t.Fatalf("POST /prompt/parse %s = %d: %s", body, code, raw)
	}
	var out chunkParseResp
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestPromptParseChunks: the live validator expands chunks, locates every
// resolved reference (with its expansion, for tinting and baking) and the
// typed weighting marks.
func TestPromptParseChunks(t *testing.T) {
	e := newEnv(t, "e2e_chunk_parse", compliance.Stub{})
	chunks := chunksJSON("Hair", "silver hair, !macro:Len!", "Len", "{long hair}", "Bad", "a}")
	out := e.parseChunks(t, `{"text": "😀, !macro:Hair!, {smile}", "chunks": `+chunks+`}`)
	if len(out.Errors) != 0 || len(out.Tags) != 4 {
		t.Fatalf("parse = %+v", out)
	}
	if c := out.Chunks; len(c) != 1 || c[0].Name != "Hair" || c[0].Start != 4 || c[0].End != 16 || c[0].Tag != 1 ||
		c[0].Text != "silver hair, {long hair}" || c[0].Hash != prompt.HashChunk("silver hair, {long hair}") {
		t.Errorf("chunks = %+v", out.Chunks)
	}
	for _, i := range []int{1, 2} {
		if g := out.Tags[i]; g.Chunk == nil || g.Chunk.Name != "Hair" || g.Start != 4 || g.End != 16 || g.Tag != 1 {
			t.Errorf("chunk tag %d = %+v", i, g)
		}
	}
	if g := out.Tags[3]; g.Chunk != nil || g.Tag != 2 || g.Text != "smile" {
		t.Errorf("typed tag = %+v", g)
	}
	if out.Emitted != "😀, silver hair, (long hair:1.0497), (smile:1.0497)" {
		t.Errorf("emitted = %q", out.Emitted)
	}
	// marks: only the typed `{smile}` — the chunk's own `{…}` is not painted
	want := []prompt.Mark{{Start: 18, End: 19, Kind: "op", Weight: 1.05}, {Start: 19, End: 24, Kind: "up", Weight: 1.05}, {Start: 24, End: 25, Kind: "op", Weight: 1}}
	if fmt.Sprint(out.Marks) != fmt.Sprint(want) {
		t.Errorf("marks = %+v, want %+v", out.Marks, want)
	}
	// an error inside a chunk lands on its reference; the references that
	// resolved are still reported so the editor keeps marking them
	out = e.parseChunks(t, `{"text": "!macro:Len!, !macro:Bad!", "chunks": `+chunks+`}`)
	if len(out.Errors) != 1 || out.Errors[0].Code != prompt.CodeStrayCloser || out.Errors[0].Start != 13 || out.Errors[0].End != 24 ||
		!strings.Contains(out.Errors[0].Message, "in chunk `Bad`") {
		t.Errorf("error in chunk = %+v", out.Errors)
	}
	if len(out.Chunks) != 2 || len(out.Tags) != 0 || len(out.Marks) != 0 {
		t.Errorf("with an error: chunks %+v tags %+v marks %+v", out.Chunks, out.Tags, out.Marks)
	}
	// negative surface rules apply to chunk content
	out = e.parseChunks(t, `{"text": "!macro:N!", "surface": "negative", "chunks": `+chunksJSON("N", "-1::hat ::")+`}`)
	if len(out.Errors) != 1 || out.Errors[0].Code != prompt.CodeNegativeInUC || out.Errors[0].End != 9 {
		t.Errorf("negative weight in a UC chunk = %+v", out.Errors)
	}
	// no chunks: marks and chunks are still arrays
	code, raw := postJSON(t, e.ts.URL+"/prompt/parse", `{"text": "1girl"}`)
	if code != http.StatusOK || !strings.Contains(string(raw), `"chunks":[]`) || !strings.Contains(string(raw), `"marks":[]`) {
		t.Errorf("plain parse = %d %s", code, raw)
	}
}

// TestTranslateChunksSkipLLM: in plain-language mode chunk text never goes
// to the LLM — it is tag text. The reference survives into `prompt` where
// that is exact (so /generate keeps its provenance) and is written out
// expanded where an outer weight applies.
func TestTranslateChunksSkipLLM(t *testing.T) {
	e := newEnv(t, "e2e_chunk_translate", compliance.Stub{})
	if _, err := e.pool.Exec(context.Background(), `
		INSERT INTO tags (name, category, post_count, pose_relevant) VALUES
		('silver_hair', 'general', 500000, false),
		('school_uniform', 'general', 800000, false)`); err != nil {
		t.Fatal(err)
	}
	dec := &mapDecomposer{by: map[string][]string{"a girl with silver hair": {"silver hair"}}}
	e.srv.Translator = &nltag.Translator{Decomposer: dec, Grounder: &nltag.Grounder{Store: tags.NewStore(e.pool)}}
	chunks := chunksJSON("Outfit", "school uniform, 1.2::made up tag ::, -1::hat ::")

	type resp struct {
		Tags []struct {
			Canonical string  `json:"canonical"`
			Category  string  `json:"category"`
			Count     int     `json:"count"`
			Stage     string  `json:"stage"`
			Weight    float64 `json:"weight"`
			Chunk     string  `json:"chunk"`
		} `json:"tags"`
		Dropped []string `json:"dropped"`
		Prompt  string   `json:"prompt"`
	}
	post := func(body string) resp {
		t.Helper()
		code, raw := postJSON(t, e.ts.URL+"/translate", body)
		if code != http.StatusOK {
			t.Fatalf("translate %s = %d %s", body, code, raw)
		}
		var out resp
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	out := post(`{"text": "a girl with silver hair, !macro:Outfit!", "chunks": ` + chunks + `}`)
	if out.Prompt != "silver_hair, !macro:Outfit!" {
		t.Errorf("prompt = %q", out.Prompt)
	}
	if len(out.Dropped) != 0 {
		t.Errorf("dropped = %v — chunk text is never dropped", out.Dropped)
	}
	if len(out.Tags) != 4 || out.Tags[1].Canonical != "school_uniform" || out.Tags[1].Chunk != "Outfit" || out.Tags[1].Stage != "chunk" ||
		out.Tags[1].Count != 800000 || out.Tags[2].Canonical != "made_up_tag" || out.Tags[2].Weight != 1.2 || out.Tags[2].Count != 0 ||
		out.Tags[3].Canonical != "hat" || out.Tags[3].Weight != -1 || out.Tags[0].Chunk != "" {
		t.Errorf("tags = %+v", out.Tags)
	}
	for _, s := range dec.seen {
		if strings.Contains(s, "school") || strings.Contains(s, "made up") || strings.Contains(s, "hat") || strings.Contains(s, "macro") {
			t.Errorf("the LLM saw chunk text: %q", s)
		}
	}
	// the translated prompt round-trips through /generate with provenance
	id := e.generate(t, `{"prompt": `+quote(out.Prompt)+`, "chunks": `+chunks+`}`)
	v := e.waitTerminal(t, id)
	var spec assembly.Spec
	if err := json.Unmarshal(v.Params, &spec); err != nil {
		t.Fatal(err)
	}
	if n := len(spec.PromptTags); n < 2 || spec.PromptTags[n-1].Chunk == nil || spec.PromptTags[n-1].Chunk.Name != "Outfit" {
		t.Errorf("provenance lost through plain mode: %s", v.Params)
	}

	// an outer weight: the tags are written out, weights kept
	out = post(`{"text": "{!macro:Outfit!}", "chunks": ` + chunksJSON("Outfit", "school uniform, tie") + `}`)
	if out.Prompt != "1.05::school uniform ::, 1.05::tie ::" {
		t.Errorf("weighted chunk prompt = %q", out.Prompt)
	}
	if len(dec.seen) != 1 {
		t.Errorf("the LLM was called for a chunk-only text: %v", dec.seen)
	}
	// errors inside a chunk are the located 400, like /generate
	code, raw := postJSON(t, e.ts.URL+"/translate", `{"text": "a girl, !macro:Bad!", "chunks": `+chunksJSON("Bad", "x}")+`}`)
	if code != http.StatusBadRequest || !strings.Contains(string(raw), `"code":"stray_closer"`) || !strings.Contains(string(raw), `"start":8`) {
		t.Errorf("translate chunk error = %d %s", code, raw)
	}
	if code, _ := postJSON(t, e.ts.URL+"/translate", `{"text": "x", "chunks": `+chunksJSON("a,b", "x")+`}`); code != http.StatusBadRequest {
		t.Errorf("bad chunk name = %d", code)
	}
}
