package outmeta

import (
	"encoding/json"
	"go/build"
	"regexp"
	"strings"
	"testing"

	"pulsar/core/internal/assembly"
	"pulsar/core/internal/prompt"
	"pulsar/core/internal/tags"
)

// Deployment prompt config for these tests. The safety negative is the
// hard-coded floor, read from tags — never spelled out here.
const (
	testQuality   = "masterpiece, best quality"
	testDefaultUC = "lowres, bad anatomy, jpeg artifacts"
)

func testForbidden() string { return strings.Join(tags.WatchFloor(), ", ") }

var testStyle = &assembly.Style{
	Slug:         "ink",
	PromptPrefix: "official art, game cg",
	PromptSuffix: "painterly, soft shading",
	Negative:     "1.3::logo ::, signature",
	Lora:         "styles/ink wash_v3.safetensors",
	LoraStrength: 0.7,
}

func f64(v float64) *float64 { return &v }

func build1(t *testing.T, req assembly.Request, style *assembly.Style) assembly.Spec {
	t.Helper()
	spec, err := assembly.Build(req, assembly.Options{
		Lineage: "noobai", Seed: 1234567890123, Style: style,
		QualityTags: testQuality, DefaultUC: testDefaultUC, ForbiddenTagUC: testForbidden(),
	})
	if err != nil {
		t.Fatal(err)
	}
	spec.Checkpoint = `C:\models\checkpoints/noobai-xl-vpred.safetensors`
	return spec
}

func fullSpec(t *testing.T) assembly.Spec {
	t.Helper()
	return build1(t, assembly.Request{
		Prompt:   "1girl, 1.3::red dress ::, castorice (honkai: star rail), -1::hat ::, !macro:Light!",
		Negative: "blurry, 1.2::extra fingers ::",
		Steps:    30, CFG: 5.5, Width: 1216, Height: 832,
		Hires: true, HiresScale: f64(2),
		Denoise: f64(0.6),
		Loras:   []assembly.Lora{{Name: "chars/acheron.safetensors", Strength: f64(0.9)}},
		Chunks:  []prompt.Chunk{{Name: "Light", Content: "rim light, golden hour"}},
	}, testStyle)
}

func TestFromSpecIsUserOnly(t *testing.T) {
	p := FromSpec(fullSpec(t), "img2img")
	want := Payload{
		V:          1,
		Prompt:     `1girl, (red dress:1.2285), castorice \(honkai: star rail\), rim light, golden hour`,
		Negative:   "blurry, (extra fingers:1.1748), hat",
		Characters: []string{},
		Width:      1216, Height: 832, Steps: 30, CFG: 5.5,
		Sampler: "euler", Scheduler: "normal", Rescale: 0.55,
		Seed: 1234567890123, Lineage: "noobai", Style: "ink", Kind: "img2img",
		Model:   "noobai-xl-vpred",
		Loras:   []Lora{{Name: "acheron", Strength: 0.9}, {Name: "ink wash_v3", Strength: 0.7}},
		Denoise: 0.6, HiresScale: 2, HiresDenoise: 0.4,
		Chunks: []string{"Light"},
		NAI: &Dialect{
			Prompt:     "1girl, 1.3::red dress ::, castorice (honkai: star rail), rim light, golden hour",
			Negative:   "blurry, 1.2::extra fingers ::, hat",
			Characters: []string{},
		},
	}
	got, _ := json.Marshal(p)
	exp, _ := json.Marshal(want)
	if string(got) != string(exp) {
		t.Errorf("payload\n got %s\nwant %s", got, exp)
	}
}

func TestFromSpecCharacters(t *testing.T) {
	spec := build1(t, assembly.Request{
		Prompt: "2girls, garden",
		Characters: []assembly.Character{
			{Prompt: "silver hair, 1.2::smile ::", ReferenceID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"},
			{Prompt: "black hair, school uniform"},
		},
	}, nil)
	p := FromSpec(spec, "txt2img")
	if len(p.Characters) != 2 || p.Characters[0] != "silver hair, (smile:1.1748)" || p.Characters[1] != "black hair, school uniform" {
		t.Errorf("characters = %q", p.Characters)
	}
	if p.NAI == nil || p.NAI.Characters[0] != "silver hair, 1.2::smile ::" || p.NAI.Prompt != "2girls, garden" {
		t.Errorf("nai = %+v", p.NAI)
	}
	if p.Prompt != "2girls, garden" || p.Negative != "" || p.Style != "" || len(p.Loras) != 0 || p.HiresScale != 0 {
		t.Errorf("payload = %+v", p)
	}
}

// A row stored before structured tags existed has only the emitted strings,
// which mix user text with the safety and quality blocks: nothing is written.
func TestFromSpecLegacyRowWritesNoPrompt(t *testing.T) {
	spec := fullSpec(t)
	spec.PromptTags, spec.NegativeTags = nil, nil
	p := FromSpec(spec, "txt2img")
	if p.Prompt != "" || p.Negative != "" || p.NAI != nil {
		t.Errorf("legacy row leaked prompt %q / negative %q", p.Prompt, p.Negative)
	}
}

// A tag matching the safety block never leaves, whatever Source it claims.
func TestFromSpecDropsSafetyFormsEvenIfUserSourced(t *testing.T) {
	spec := fullSpec(t)
	var planted prompt.Tag
	for _, tg := range spec.NegativeTags {
		if tg.Source == prompt.Safety {
			planted = tg
			break
		}
	}
	planted.Source = prompt.User
	spec.PromptTags = append(spec.PromptTags, planted)
	spec.NegativeTags = append(spec.NegativeTags, planted)
	p := FromSpec(spec, "txt2img")
	for _, s := range []string{p.Prompt, p.Negative, p.NAI.Prompt, p.NAI.Negative} {
		for _, tg := range strings.Split(s, ", ") {
			if prompt.Normalize(tg) == planted.Name {
				t.Error("a safety-form tag was written")
			}
		}
	}
}

func TestJSONGolden(t *testing.T) {
	got, err := JSON(FromSpec(fullSpec(t), "img2img"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"v":1,"prompt":"1girl, (red dress:1.2285), castorice \\(honkai: star rail\\), rim light, golden hour",` +
		`"negative":"blurry, (extra fingers:1.1748), hat","characters":[],"width":1216,"height":832,"steps":30,"cfg":5.5,` +
		`"sampler":"euler","scheduler":"normal","rescale":0.55,"seed":1234567890123,"lineage":"noobai","style":"ink",` +
		`"kind":"img2img","model":"noobai-xl-vpred","loras":[{"name":"acheron","strength":0.9},{"name":"ink wash_v3","strength":0.7}],` +
		`"denoise":0.6,"hires_scale":2,"hires_denoise":0.4,"chunks":["Light"],` +
		`"nai":{"prompt":"1girl, 1.3::red dress ::, castorice (honkai: star rail), rim light, golden hour",` +
		`"negative":"blurry, 1.2::extra fingers ::, hat","characters":[]}}`
	if got != want {
		t.Errorf("JSON\n got %s\nwant %s", got, want)
	}
	// the contract's required keys are always present, even when empty
	min, err := JSON(Payload{})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(min), &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"v", "prompt", "negative", "characters", "width", "height", "steps", "cfg", "sampler", "scheduler", "rescale", "seed", "lineage", "style", "kind"} {
		if _, ok := m[k]; !ok {
			t.Errorf("key %q missing from %s", k, min)
		}
	}
	if m["v"] != float64(1) {
		t.Errorf("v = %v", m["v"])
	}
	if cs, ok := m["characters"].([]any); !ok || len(cs) != 0 {
		t.Errorf("characters = %v, want []", m["characters"])
	}
	// HTML is not escaped away (and is just text to any reader)
	h, _ := JSON(Payload{Prompt: "<b>&"})
	if !strings.Contains(h, `"<b>&"`) {
		t.Errorf("html escaped: %s", h)
	}
}

func TestA1111Golden(t *testing.T) {
	got := A1111(FromSpec(fullSpec(t), "img2img"))
	want := `1girl, (red dress:1.2285), castorice \(honkai: star rail\), rim light, golden hour` + "\n" +
		`Negative prompt: blurry, (extra fingers:1.1748), hat` + "\n" +
		`Steps: 30, Sampler: Euler, Schedule type: Normal, CFG scale: 5.5, Seed: 1234567890123, Size: 1216x832, ` +
		`Model: noobai-xl-vpred, Denoising strength: 0.6, Hires upscale: 2, Hires denoising strength: 0.4, CFG Rescale: 0.55, ` +
		`Lora strengths: "acheron: 0.9, ink wash_v3: 0.7", Lineage: noobai, Style: ink, Kind: img2img, Version: pulsar`
	if got != want {
		t.Errorf("A1111\n got %s\nwant %s", got, want)
	}

	// no negative → no `Negative prompt:` line, as in A1111
	p := FromSpec(build1(t, assembly.Request{Prompt: "1girl"}, nil), "txt2img")
	if lines := strings.Split(A1111(p), "\n"); len(lines) != 2 || lines[0] != "1girl" || !strings.HasPrefix(lines[1], "Steps: ") {
		t.Errorf("lines = %q", lines)
	}
}

// reParamCode is A1111's re_param_code (modules/infotext_utils.py), verbatim.
var reParamCode = regexp.MustCompile(`\s*(\w[\w \-/]+):\s*("(?:\\.|[^\\"])+"|[^,]*)(?:,|$)`)

// parseSettings is the settings-line half of A1111's parse_generation_parameters.
func parseSettings(t *testing.T, line string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, m := range reParamCode.FindAllStringSubmatch(line, -1) {
		v := m[2]
		if strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) && len(v) >= 2 {
			var s string
			if err := json.Unmarshal([]byte(v), &s); err != nil {
				t.Fatalf("quoted value %s does not unquote: %v", v, err)
			}
			v = s
		}
		out[m[1]] = v
	}
	return out
}

func TestA1111SettingsLineParsesWithA1111Regex(t *testing.T) {
	spec := build1(t, assembly.Request{
		Prompt: "2girls",
		Characters: []assembly.Character{
			{Prompt: `silver hair, say "hi": loud, back\slash`},
			{Prompt: "black hair"},
		},
	}, nil)
	p := FromSpec(spec, "txt2img")
	lines := strings.Split(A1111(p), "\n")
	last := lines[len(lines)-1]
	// A1111 treats the last line as settings only with three or more pairs
	if n := len(reParamCode.FindAllString(last, -1)); n < 3 {
		t.Fatalf("settings line yields %d pairs: %s", n, last)
	}
	got := parseSettings(t, last)
	want := map[string]string{
		"Steps": "28", "Sampler": "Euler", "Schedule type": "Normal", "CFG scale": "5",
		"Seed": "1234567890123", "Size": "832x1216", "Model": "noobai-xl-vpred", "CFG Rescale": "0.55",
		// values holding `,` `:` are JSON-quoted and round-trip exactly
		"Character 1": p.Characters[0], "Character 2": "black hair",
		"Lineage": "noobai", "Kind": "txt2img", "Version": "pulsar",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("pairs = %v", got)
	}
	if !strings.Contains(p.Characters[0], `"`) || !strings.Contains(p.Characters[0], `\`) {
		t.Fatalf("fixture lost its quote/backslash: %q", p.Characters[0])
	}
}

func TestQuote(t *testing.T) {
	for in, want := range map[string]string{
		"plain":   "plain",
		"a, b":    `"a, b"`,
		"k: v":    `"k: v"`,
		"x\ny":    `"x\ny"`,
		`say "x"`: `say "x"`, // A1111 quotes only on , : newline
		`"a", b`:  `"\"a\", b"`,
		"<lora>":  "<lora>",
		"<a>, &b": `"<a>, &b"`,
	} {
		if got := quote(in); got != want {
			t.Errorf("quote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestDisplayName(t *testing.T) {
	for in, want := range map[string]string{
		"noobai-xl-vpred.safetensors":             "noobai-xl-vpred",
		"sub/dir/honkai star rail_v3.SafeTensors": "honkai star rail_v3",
		`C:\models\x.ckpt`:                        "x",
		"a.safetensors.safetensors":               "a",
		"plain":                                   "plain",
		".safetensors":                            ".safetensors", // nothing left to name: kept, never used as a path
		"":                                        "",
	} {
		if got := DisplayName(in); got != want {
			t.Errorf("DisplayName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestXMP(t *testing.T) {
	for kind, term := range map[string]string{
		"txt2img": "trainedAlgorithmicMedia",
		"pose":    "trainedAlgorithmicMedia",
		"":        "trainedAlgorithmicMedia",
		"unknown": "trainedAlgorithmicMedia",
		"img2img": "compositeWithTrainedAlgorithmicMedia",
		"inpaint": "compositeWithTrainedAlgorithmicMedia",
	} {
		x := XMP(kind)
		if !strings.Contains(x, `Iptc4xmpExt:DigitalSourceType="http://cv.iptc.org/newscodes/digitalsourcetype/`+term+`"`) {
			t.Errorf("kind %q: source type missing: %s", kind, x)
		}
		if !strings.Contains(x, `xmp:CreatorTool="pulsar"`) {
			t.Errorf("kind %q: no CreatorTool", kind)
		}
		if !strings.HasPrefix(x, "<?xpacket begin=\"\uFEFF\"") || !strings.HasSuffix(x, `<?xpacket end="r"?>`) {
			t.Errorf("kind %q: not an XMP packet", kind)
		}
	}
	golden := "<?xpacket begin=\"\uFEFF\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n" +
		"<x:xmpmeta xmlns:x=\"adobe:ns:meta/\">\n" +
		" <rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">\n" +
		"  <rdf:Description rdf:about=\"\"\n" +
		"    xmlns:Iptc4xmpExt=\"http://iptc.org/std/Iptc4xmpExt/2008-02-29/\"\n" +
		"    xmlns:xmp=\"http://ns.adobe.com/xap/1.0/\"\n" +
		"    Iptc4xmpExt:DigitalSourceType=\"http://cv.iptc.org/newscodes/digitalsourcetype/trainedAlgorithmicMedia\"\n" +
		"    xmp:CreatorTool=\"pulsar\"/>\n" +
		" </rdf:RDF>\n" +
		"</x:xmpmeta>\n" +
		"<?xpacket end=\"r\"?>"
	if XMP("txt2img") != golden {
		t.Errorf("XMP golden mismatch:\n%s", XMP("txt2img"))
	}
}

var uuidShaped = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// TestDenyList: nothing pulsar writes carries the safety negative,
// DEFAULT_UC, QUALITY_TAGS, style text, an id, a model path — or the job id.
// The Spec below holds all of them; the payloads must hold none.
func TestDenyList(t *testing.T) {
	const jobID = "0f1e2d3c-4b5a-4978-8695-a4b3c2d1e0f9"
	const uploadID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	spec := build1(t, assembly.Request{
		Prompt:   "2girls, garden, 1.3::red dress ::",
		Negative: "blurry",
		Hires:    true,
		Characters: []assembly.Character{
			{Prompt: "silver hair", ReferenceID: uploadID},
			{Prompt: "black hair", ReferenceID: "bbbbbbbb-bbbb-4ccc-8ddd-eeeeeeeeeeee"},
		},
		Loras: []assembly.Lora{{Name: "chars/acheron.safetensors"}},
	}, testStyle)

	// the precondition: the Spec really does carry everything on the list
	stored, _ := json.Marshal(spec)
	for _, must := range []string{tags.WatchFloor()[0], "lowres", "masterpiece", "official art", "painterly", "signature", uploadID, ".safetensors"} {
		if !strings.Contains(string(stored), must) {
			t.Fatalf("fixture spec does not carry a deny-listed value (%d bytes checked)", len(stored))
		}
	}

	var deny []string
	for _, cfg := range []string{testForbidden(), testDefaultUC, testQuality,
		testStyle.PromptPrefix, testStyle.PromptSuffix, "logo, signature"} {
		for _, term := range strings.Split(cfg, ",") {
			if term = strings.TrimSpace(term); term != "" {
				deny = append(deny, strings.ToLower(term), strings.ReplaceAll(strings.ToLower(term), "_", " "))
			}
		}
	}
	deny = append(deny, jobID, uploadID, ".safetensors", ".ckpt", "/models", `\models`, "checkpoints", "chars/", "styles/",
		"reference_id", "image_id", "/images/", "/jobs/", "novelai")

	for _, kind := range []string{"txt2img", "img2img", "inpaint", "pose"} {
		p := FromSpec(spec, kind)
		js, err := JSON(p)
		if err != nil {
			t.Fatal(err)
		}
		for name, out := range map[string]string{"json": js, "a1111": A1111(p), "xmp": XMP(kind)} {
			lower := strings.ToLower(out)
			for _, d := range deny {
				if strings.Contains(lower, strings.ToLower(d)) {
					t.Errorf("%s/%s carries a deny-listed value (index %d of the list)", kind, name, indexOf(deny, d))
				}
			}
			if uuidShaped.MatchString(out) {
				t.Errorf("%s/%s carries an id-shaped string", kind, name)
			}
		}
		// what the user wrote is there
		if !strings.Contains(js, "2girls, garden, (red dress:1.2285)") || !strings.Contains(js, `"negative":"blurry"`) ||
			!strings.Contains(js, `"characters":["silver hair","black hair"]`) {
			t.Errorf("%s: user content missing: %s", kind, js)
		}
	}
}

func indexOf(list []string, v string) int {
	for i, s := range list {
		if s == v {
			return i
		}
	}
	return -1
}

// The package is pure: prompt + assembly + stdlib, nothing that does I/O.
func TestPackageIsPure(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"bytes": true, "encoding/json": true, "fmt": true, "strconv": true, "strings": true,
		"pulsar/core/internal/assembly": true, "pulsar/core/internal/prompt": true,
	}
	for _, imp := range pkg.Imports {
		if !allowed[imp] {
			t.Errorf("outmeta imports %q; it must stay pure (no I/O, no DB)", imp)
		}
	}
}
