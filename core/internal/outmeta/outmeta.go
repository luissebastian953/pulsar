// Package outmeta builds the metadata pulsar writes back into delivered
// images (docs/compliance.md#output-metadata). Pure: no I/O, no DB — it
// turns a stored assembly.Spec into text payloads; pngmeta splices them in
// at serve time.
//
// Three payloads exist:
//
//   - XMP — the "AI-generated" provenance mark, on every served image.
//   - A1111 — an A1111-style `parameters` text, on downloads, for the
//     readers the ecosystem already has (Forge PNG Info, Civitai, …).
//   - JSON — pulsar's own versioned `pulsar` chunk, on downloads, for a
//     lossless round trip into /create (orbit's PulsarMetaV1).
//
// A1111 and JSON are built from one projection (Payload) that holds ONLY
// what the user authored. Never in it, by construction:
//
//   - the safety negative (FORBIDDEN_TAG_UC), DEFAULT_UC, QUALITY_TAGS,
//     style prefix/suffix/negative text and auto-injected anchors — only
//     tags whose Source is User are read;
//   - upload ids, reference ids, mask ids, file paths;
//   - the job id or image id, or anything else that resolves to a core URL
//     (an image that named its job would be a key to GET /jobs/{id});
//   - model and LoRA filenames — display names only.
//
// pulsar never writes `Software: NovelAI`, NovelAI's key schema, or a
// stealth (pixel LSB) copy.
package outmeta

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"pulsar/core/internal/assembly"
	"pulsar/core/internal/prompt"
)

const (
	// KeyXMP is the PNG iTXt keyword the XMP specification assigns.
	KeyXMP = "XML:com.adobe.xmp"
	// KeyParameters is the keyword A1111 and its readers use.
	KeyParameters = "parameters"
	// KeyPulsar holds the JSON payload.
	KeyPulsar = "pulsar"
	// CreatorTool names pulsar in the XMP mark and the A1111 Version field.
	CreatorTool = "pulsar"
	// Version of the JSON payload.
	Version = 1
)

// Lora is a LoRA by display name.
type Lora struct {
	Name     string  `json:"name"`
	Strength float64 `json:"strength"`
}

// Payload is the user-only projection of a generation. The JSON field names
// are the contract with orbit's PulsarMetaV1 (orbit/app/utils/imgmeta/types.ts).
type Payload struct {
	V int `json:"v"`
	// Prompt and Negative are the user's tags in the SD syntax the model
	// received (prompt.EmitComfy).
	Prompt     string   `json:"prompt"`
	Negative   string   `json:"negative"`
	Characters []string `json:"characters"`
	Width      int      `json:"width"`
	Height     int      `json:"height"`
	Steps      int      `json:"steps"`
	CFG        float64  `json:"cfg"`
	Sampler    string   `json:"sampler"`
	Scheduler  string   `json:"scheduler"`
	Rescale    float64  `json:"rescale"`
	Seed       int64    `json:"seed"`
	Lineage    string   `json:"lineage"`
	Style      string   `json:"style"`
	Kind       string   `json:"kind"`
	// Model and Loras are display names: no directory, no extension.
	Model string `json:"model,omitempty"`
	Loras []Lora `json:"loras,omitempty"`
	// Denoise is the img2img/inpaint strength; 0 for other kinds.
	Denoise      float64 `json:"denoise,omitempty"`
	HiresScale   float64 `json:"hires_scale,omitempty"`
	HiresDenoise float64 `json:"hires_denoise,omitempty"`
	// Chunks names the prompt chunks the user's tags came from.
	Chunks []string `json:"chunks,omitempty"`
	// NAI is the same user text in the dialect the editor takes, with the
	// weights as typed (Prompt/Negative carry the lineage-calibrated weights
	// the model received). It is what makes the round trip lossless; the
	// A1111 text never carries it.
	NAI *Dialect `json:"nai,omitempty"`
}

// Dialect is the user's text in the NovelAI weighting dialect.
type Dialect struct {
	Prompt     string   `json:"prompt"`
	Negative   string   `json:"negative"`
	Characters []string `json:"characters"`
}

// FromSpec projects a stored Spec onto what the user authored. A legacy
// Spec without structured tags yields empty prompts: its strings mix user
// text with the safety and quality blocks and cannot be separated.
func FromSpec(spec assembly.Spec, kind string) Payload {
	// belt and braces: a user tag that matches a safety-block tag is dropped
	// even if it somehow carries Source User
	safety := map[string]bool{}
	for _, t := range spec.NegativeTags {
		if t.Source == prompt.Safety {
			for _, f := range prompt.MatchForms(t.Name) {
				safety[f] = true
			}
		}
	}
	var chunks []string
	seenChunk := map[string]bool{}
	// user returns a surface's user tags emitted for ComfyUI and in the
	// NovelAI dialect
	user := func(in []prompt.Tag) (string, string) {
		var out []prompt.Tag
		for _, t := range in {
			if t.Source != prompt.User || len(t.Runs) == 0 {
				continue
			}
			blocked := false
			for _, f := range prompt.MatchForms(t.Name) {
				blocked = blocked || safety[f]
			}
			if blocked {
				continue
			}
			out = append(out, t)
			if t.Chunk != nil && t.Chunk.Name != "" && !seenChunk[t.Chunk.Name] {
				seenChunk[t.Chunk.Name] = true
				chunks = append(chunks, t.Chunk.Name)
			}
		}
		s, err := prompt.EmitComfy(out)
		if err != nil {
			return "", "" // a stored tag that will not emit is not written
		}
		return s, prompt.FormatNAI(out)
	}
	nai := &Dialect{Characters: []string{}}

	p := Payload{
		V:          Version,
		Characters: []string{},
		Width:      spec.Width,
		Height:     spec.Height,
		Steps:      spec.Steps,
		CFG:        spec.CFG,
		Sampler:    spec.Sampler,
		Scheduler:  spec.Scheduler,
		Rescale:    spec.CFGRescale,
		Seed:       spec.Seed,
		Lineage:    spec.Lineage,
		Style:      spec.Style,
		Kind:       kind,
		Model:      DisplayName(spec.Checkpoint),
		Denoise:    spec.Denoise,
	}
	p.Prompt, nai.Prompt = user(spec.PromptTags)
	p.Negative, nai.Negative = user(spec.NegativeTags)
	for _, c := range spec.Characters {
		sd, typed := user(c.Tags)
		p.Characters = append(p.Characters, sd)
		nai.Characters = append(nai.Characters, typed)
	}
	if nai.Prompt != "" || nai.Negative != "" || len(nai.Characters) > 0 {
		p.NAI = nai
	}
	for _, l := range spec.Loras {
		name := DisplayName(l.Name)
		if name == "" {
			continue
		}
		st := 0.0
		if l.Strength != nil {
			st = *l.Strength
		}
		p.Loras = append(p.Loras, Lora{Name: name, Strength: st})
	}
	if spec.Hires {
		p.HiresScale = spec.HiresScale
		p.HiresDenoise = spec.HiresDenoise
	}
	p.Chunks = chunks
	return p
}

// modelExts are the model file extensions a display name never carries.
var modelExts = []string{".safetensors", ".ckpt", ".pt", ".pth", ".bin", ".gguf", ".sft"}

// DisplayName turns a model or LoRA filename into a display name: the
// directory is dropped, then every trailing model extension.
func DisplayName(file string) string {
	name := file
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	for trimmed := true; trimmed; {
		trimmed = false
		for _, ext := range modelExts {
			if len(name) > len(ext) && strings.EqualFold(name[len(name)-len(ext):], ext) {
				name = name[:len(name)-len(ext)]
				trimmed = true
			}
		}
	}
	return strings.TrimSpace(name)
}

// JSON is the `pulsar` chunk: `{"v":1,…}`, compact, HTML characters left
// as written.
func JSON(p Payload) (string, error) {
	if p.Characters == nil {
		p.Characters = []string{}
	}
	p.V = Version
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		return "", fmt.Errorf("outmeta: %w", err)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// a1111Samplers / a1111Schedules map ComfyUI names onto the labels A1111
// readers show. Unknown names pass through.
var a1111Samplers = map[string]string{
	"euler":           "Euler",
	"euler_ancestral": "Euler a",
	"dpmpp_2m":        "DPM++ 2M",
}

var a1111Schedules = map[string]string{
	"normal": "Normal",
	"karras": "Karras",
}

// quote is A1111's infotext quote(): values holding `,`, `:` or a newline
// are written as a JSON string; everything else bare.
func quote(s string) string {
	if !strings.ContainsAny(s, ",:\n") {
		return s
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return `""`
	}
	return strings.TrimRight(b.String(), "\n")
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// oneLine keeps a prompt on its line: A1111's reader splits the text on
// newlines and reads `Negative prompt:` and the settings line by position.
func oneLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' }), " ")
}

// A1111 writes the payload as A1111 infotext, following create_infotext:
// the prompt, an optional `Negative prompt:` line, then one `Key: value`
// settings line in A1111's order (Steps, Sampler, Schedule type, CFG scale,
// Seed, Size, Model, Denoising strength, Hires upscale, …, Version), with
// pulsar's own keys between the standard ones and Version.
func A1111(p Payload) string {
	var b strings.Builder
	b.WriteString(oneLine(p.Prompt))
	if neg := oneLine(p.Negative); neg != "" {
		b.WriteString("\nNegative prompt: " + neg)
	}
	type kv struct{ k, v string }
	var kvs []kv
	add := func(k, v string) {
		if v != "" {
			kvs = append(kvs, kv{k, v})
		}
	}
	label := func(m map[string]string, v string) string {
		if l, ok := m[v]; ok {
			return l
		}
		return v
	}
	add("Steps", strconv.Itoa(p.Steps))
	add("Sampler", label(a1111Samplers, p.Sampler))
	add("Schedule type", label(a1111Schedules, p.Scheduler))
	add("CFG scale", num(p.CFG))
	add("Seed", strconv.FormatInt(p.Seed, 10))
	add("Size", fmt.Sprintf("%dx%d", p.Width, p.Height))
	add("Model", p.Model)
	if p.Denoise > 0 {
		add("Denoising strength", num(p.Denoise))
	}
	if p.HiresScale > 0 {
		add("Hires upscale", num(p.HiresScale))
		// the refine pass has its own strength; A1111's single key is taken
		// by img2img above, so it gets a pulsar key
		if p.HiresDenoise > 0 {
			add("Hires denoising strength", num(p.HiresDenoise))
		}
	}
	if p.Rescale > 0 {
		add("CFG Rescale", num(p.Rescale))
	}
	if len(p.Loras) > 0 {
		parts := make([]string, len(p.Loras))
		for i, l := range p.Loras {
			parts[i] = l.Name + ": " + num(l.Strength)
		}
		add("Lora strengths", strings.Join(parts, ", "))
	}
	for i, c := range p.Characters {
		add(fmt.Sprintf("Character %d", i+1), oneLine(c))
	}
	add("Lineage", p.Lineage)
	add("Style", p.Style)
	add("Kind", p.Kind)
	add("Version", CreatorTool)
	b.WriteString("\n")
	for i, e := range kvs {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(e.k + ": " + quote(e.v))
	}
	return b.String()
}

// IPTC digital source types (https://cv.iptc.org/newscodes/digitalsourcetype/).
const (
	sourceTrained   = "http://cv.iptc.org/newscodes/digitalsourcetype/trainedAlgorithmicMedia"
	sourceComposite = "http://cv.iptc.org/newscodes/digitalsourcetype/compositeWithTrainedAlgorithmicMedia"
)

// DigitalSourceType is the IPTC term for a job kind: an image generated
// onto a user's image (img2img, inpaint) is a composite; everything else —
// and any kind this build does not know — is purely generated.
func DigitalSourceType(kind string) string {
	switch kind {
	case "img2img", "inpaint":
		return sourceComposite
	}
	return sourceTrained
}

// XMP is the provenance packet for a job kind: IPTC DigitalSourceType plus
// xmp:CreatorTool. It is a constant per source type — no id, no date, no
// prompt — so it is safe on every served image, thumbnails included.
// Read-only packet (end="r"), no padding.
func XMP(kind string) string {
	return "<?xpacket begin=\"\ufeff\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n" +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/">` + "\n" +
		` <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` + "\n" +
		`  <rdf:Description rdf:about=""` + "\n" +
		`    xmlns:Iptc4xmpExt="http://iptc.org/std/Iptc4xmpExt/2008-02-29/"` + "\n" +
		`    xmlns:xmp="http://ns.adobe.com/xap/1.0/"` + "\n" +
		`    Iptc4xmpExt:DigitalSourceType="` + DigitalSourceType(kind) + `"` + "\n" +
		`    xmp:CreatorTool="` + CreatorTool + `"/>` + "\n" +
		` </rdf:RDF>` + "\n" +
		`</x:xmpmeta>` + "\n" +
		`<?xpacket end="r"?>`
}
