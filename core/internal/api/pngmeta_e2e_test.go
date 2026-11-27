// Output metadata is a compliance surface (docs/compliance.md#output-metadata):
// delivered bytes carry only allow-listed PNG chunks, plus the one XMP
// provenance mark core itself adds on the serve path. These tests pin that
// on both layers — the worker strip before CheckOutput and the serve-path
// strip. What `?meta=1` adds is pinned in outmeta_e2e_test.go.
package api_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"pulsar/core/internal/comfy/comfytest"
	"pulsar/core/internal/compliance"
	"pulsar/core/internal/outmeta"
	"pulsar/core/internal/pngmeta"
	"pulsar/core/internal/tags"
)

var uuidShaped = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// recordOutput allows everything and keeps the exact bytes CheckOutput saw.
type recordOutput struct {
	compliance.Stub
	mu   sync.Mutex
	seen [][]byte
}

func (r *recordOutput) CheckOutput(_ context.Context, img []byte) (compliance.Decision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, append([]byte{}, img...))
	return compliance.Decision{Allowed: true}, nil
}

func (r *recordOutput) outputs() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen
}

// safetyTerms is every term the safety negative and the hard-coded floor
// carry, read from config and tags.WatchFloor() — never spelled out here.
func safetyTerms(e *env) []string {
	terms := tags.WatchFloor()
	for _, t := range strings.Split(e.srv.Cfg.ForbiddenTagUC, ",") {
		if t = strings.TrimSpace(t); t != "" {
			terms = append(terms, t)
		}
	}
	return terms
}

func fetch(t *testing.T, url string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// isProvenanceMark reports whether c is exactly the XMP packet core adds on
// the serve path: an uncompressed iTXt chunk, keyed as the XMP spec says,
// whose whole text is one of outmeta's two constant packets. Anything else
// in an iTXt chunk is not the mark.
func isProvenanceMark(c pngmeta.Chunk) bool {
	key, text, ok := pngmeta.ITXtText(c)
	return ok && key == outmeta.KeyXMP && (text == outmeta.XMP("txt2img") || text == outmeta.XMP("img2img"))
}

// withoutMark checks that served carries exactly one provenance mark, of
// the source type wantKind implies, ahead of the pixel data — and returns
// the bytes with it removed, for comparison with what was stored.
func withoutMark(t *testing.T, what string, served []byte, wantKind string) []byte {
	t.Helper()
	chunks, err := pngmeta.Chunks(served)
	if err != nil {
		t.Fatalf("%s: not a png: %v", what, err)
	}
	marks, seenIDAT := 0, false
	for _, c := range chunks {
		if c.Type == "IDAT" {
			seenIDAT = true
		}
		if !isProvenanceMark(c) {
			continue
		}
		marks++
		if seenIDAT {
			t.Errorf("%s: provenance mark sits after the pixel data", what)
		}
		if _, text, _ := pngmeta.ITXtText(c); text != outmeta.XMP(wantKind) {
			t.Errorf("%s: provenance mark has the wrong source type for kind %q", what, wantKind)
		}
	}
	if marks != 1 {
		t.Fatalf("%s: carries %d provenance marks, want exactly 1", what, marks)
	}
	out, err := pngmeta.Strip(served)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// assertClean fails if data carries anything but allow-listed chunks (and,
// on served bytes, the provenance mark), any safety term, or anything
// shaped like a blob/job id.
func assertClean(t *testing.T, e *env, what string, data []byte) {
	t.Helper()
	chunks, err := pngmeta.Chunks(data)
	if err != nil {
		t.Fatalf("%s: not a png: %v", what, err)
	}
	for _, c := range chunks {
		if !pngmeta.Allowed(c.Type) && !isProvenanceMark(c) {
			t.Errorf("%s: carries a %s chunk", what, c.Type)
		}
	}
	lower := bytes.ToLower(data)
	for _, term := range safetyTerms(e) {
		if bytes.Contains(lower, []byte(strings.ToLower(term))) {
			t.Errorf("%s: carries a safety-negative term", what)
		}
	}
	for _, s := range []string{"class_type", "ckpt_name", "LoadImage"} {
		if bytes.Contains(data, []byte(s)) {
			t.Errorf("%s: carries graph text %q", what, s)
		}
	}
	if m := uuidShaped.Find(data); m != nil {
		t.Errorf("%s: carries an id-shaped string", what)
	}
}

// TestDeliveredImagesCarryNoGraph pins the output-metadata invariant the way
// TestForbiddenTagUCIsUnconditional pins the negative: ComfyUI returns PNGs
// with the full patched graph in a `prompt` chunk (safety negative, model
// filenames, the user's upload id), and none of it may be classified-around,
// stored, or delivered.
func TestDeliveredImagesCarryNoGraph(t *testing.T) {
	rec := &recordOutput{}
	e := newEnv(t, "e2e_nograph", rec)
	e.fake.EmbedGraph = true

	upload := e.uploadPNG(t, 100, 150)
	id := e.generate(t, `{"prompt": "1girl, red dress", "seed": 5, "kind": "img2img", "image_id": "`+upload+`"}`)
	v := e.waitTerminal(t, id)
	if v.State != "done" {
		t.Fatalf("state = %s (%s), want done", v.State, v.Error)
	}

	// the precondition: what comfy handed back really is the leak
	leaked := comfytest.WithText(e.fake.PNG, "prompt", string(e.fake.LastGraph()))
	if !bytes.Contains(leaked, []byte(upload)) {
		t.Fatal("fixture graph does not reference the upload id")
	}
	if !bytes.Contains(leaked, []byte(e.srv.Cfg.ForbiddenTagUC)) {
		t.Fatal("fixture graph does not carry the safety negative")
	}
	if cs, err := pngmeta.Chunks(leaked); err != nil || cs[1].Type != "tEXt" {
		t.Fatalf("fixture is not a png with a text chunk: %v", err)
	}

	status, served := fetch(t, e.ts.URL+v.ImageURL)
	if status != http.StatusOK {
		t.Fatalf("GET %s = %d", v.ImageURL, status)
	}
	assertClean(t, e, "served image", served)

	stored, err := os.ReadFile(filepath.Join(e.blobs.Root, id+".png"))
	if err != nil {
		t.Fatal(err)
	}
	assertClean(t, e, "stored blob", stored)

	// CheckOutput saw exactly the bytes that were stored and served — not
	// the unstripped original, not a re-encode
	seen := rec.outputs()
	if len(seen) != 1 {
		t.Fatalf("CheckOutput ran %d times, want 1", len(seen))
	}
	// (served = stored + the one provenance mark the serve path adds)
	if !bytes.Equal(seen[0], stored) || !bytes.Equal(withoutMark(t, "served image", served, "img2img"), stored) {
		t.Error("classified, stored and served bytes differ")
	}
	// the mark is added on the way out only: nothing at rest carries it
	if bytes.Contains(stored, []byte(outmeta.KeyXMP)) {
		t.Error("stored blob carries the serve-path mark")
	}
	// and the pixels are the ones comfy produced
	if !bytes.Equal(stored, e.fake.PNG) {
		t.Error("strip changed more than the metadata")
	}
}

// Every role of a multi-output kind is stripped and checked.
func TestDeliveredPoseRolesCarryNoGraph(t *testing.T) {
	rec := &recordOutput{}
	e := newEnv(t, "e2e_nograph_pose", rec)
	e.fake.EmbedGraph = true
	e.fake.TagOutputs = true

	id := e.generate(t, `{"prompt": "1girl, standing", "seed": 7, "kind": "pose"}`)
	v := e.waitTerminal(t, id)
	if v.State != "done" {
		t.Fatalf("state = %s (%s), want done", v.State, v.Error)
	}
	if len(v.Images) != 3 {
		t.Fatalf("images = %v", v.Images)
	}
	for role, url := range v.Images {
		status, served := fetch(t, e.ts.URL+url)
		if status != http.StatusOK {
			t.Fatalf("GET %s = %d", url, status)
		}
		assertClean(t, e, role, served)
		withoutMark(t, role, served, "pose")
	}
	seen := rec.outputs()
	if len(seen) != len(v.Images) {
		t.Fatalf("CheckOutput ran %d times for %d images", len(seen), len(v.Images))
	}
	for _, img := range seen {
		assertClean(t, e, "classified image", img)
	}
}

// The strip must not open a path around the gate: a graph-carrying output
// that the checker rejects still blocks the whole job.
func TestBlockedStillBlocksWithEmbeddedGraph(t *testing.T) {
	e := newEnv(t, "e2e_nograph_blocked", denyOutput{})
	e.fake.EmbedGraph = true
	id := e.generate(t, `{"prompt": "1girl"}`)
	if v := e.waitTerminal(t, id); v.State != "blocked" || v.ImageURL != "" {
		t.Fatalf("state = %s, image_url = %q; want blocked with no image", v.State, v.ImageURL)
	}
	if status, _ := fetch(t, e.ts.URL+"/images/"+id); status != http.StatusNotFound {
		t.Errorf("blocked image fetch = %d, want 404", status)
	}
}

// An output that will not parse fails the job closed: it is never
// classified as-is, stored, or delivered.
func TestMalformedOutputFailsJob(t *testing.T) {
	for name, mangle := range map[string]func([]byte) []byte{
		"not_a_png": func([]byte) []byte { return []byte("<html>comfy error page</html>") },
		"bad_crc": func(p []byte) []byte {
			out := append([]byte{}, p...)
			out[len(out)-1] ^= 0xff
			return out
		},
		"truncated": func(p []byte) []byte { return p[:len(p)-6] },
	} {
		t.Run(name, func(t *testing.T) {
			rec := &recordOutput{}
			e := newEnv(t, "e2e_malformed_"+name, rec)
			e.fake.PNG = mangle(e.fake.PNG)
			id := e.generate(t, `{"prompt": "1girl"}`)
			v := e.waitTerminal(t, id)
			if v.State != "failed" {
				t.Fatalf("state = %s, want failed", v.State)
			}
			if v.ImageURL != "" {
				t.Error("failed job exposed an image URL")
			}
			if n := len(rec.outputs()); n != 0 {
				t.Errorf("CheckOutput ran %d times on unparseable bytes", n)
			}
			if status, _ := fetch(t, e.ts.URL+"/images/"+id); status != http.StatusNotFound {
				t.Errorf("image fetch = %d, want 404", status)
			}
		})
	}
}

// TestLegacyBlobServedStripped: blobs written before the worker strip
// existed still hold the graph on disk; the serve path cleans them without
// a migration and without touching pixels.
func TestLegacyBlobServedStripped(t *testing.T) {
	e := newEnv(t, "e2e_legacy_blob", compliance.Stub{})

	src := image.NewRGBA(image.Rect(0, 0, 5, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 5; x++ {
			src.Set(x, y, color.RGBA{uint8(50 * x), uint8(60 * y), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	const legacyID = "11111111-2222-4333-8444-555555555555"
	const uploadID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	graph := `{"7":{"class_type":"CLIPTextEncode","inputs":{"text":"` + e.srv.Cfg.ForbiddenTagUC + `"}},` +
		`"8":{"class_type":"LoadImage","inputs":{"image":"` + uploadID + `.png"}}}`
	legacy := comfytest.WithText(buf.Bytes(), "prompt", graph)
	legacy = append(legacy, "bytes after IEND"...)
	// straight onto disk, the way the old worker stored it
	if err := os.WriteFile(filepath.Join(e.blobs.Root, legacyID+".png"), legacy, 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(e.ts.URL + "/images/" + legacyID)
	if err != nil {
		t.Fatal(err)
	}
	served, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "private, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q", got)
	}
	assertClean(t, e, "served legacy blob", served)
	// no job row: still marked, with the default source type
	if !bytes.Equal(withoutMark(t, "served legacy blob", served, ""), buf.Bytes()) {
		t.Error("served bytes are not the original image minus metadata")
	}
	got, err := png.Decode(bytes.NewReader(served))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 4; y++ {
		for x := 0; x < 5; x++ {
			if color.RGBAModel.Convert(got.At(x, y)) != src.At(x, y) {
				t.Fatalf("pixel (%d,%d) changed", x, y)
			}
		}
	}

	// a blob that will not parse is not served at all
	const corruptID = "99999999-2222-4333-8444-555555555555"
	corrupt := append([]byte{}, legacy...)
	corrupt[40] ^= 0xff // inside the text chunk: CRC no longer matches
	if err := os.WriteFile(filepath.Join(e.blobs.Root, corruptID+".png"), corrupt, 0o644); err != nil {
		t.Fatal(err)
	}
	status, body := fetch(t, e.ts.URL+"/images/"+corruptID)
	if status != http.StatusInternalServerError {
		t.Errorf("corrupt blob = %d, want 500", status)
	}
	if bytes.Contains(body, []byte(uploadID)) || bytes.Contains(body, []byte("LoadImage")) {
		t.Error("error response leaked blob content")
	}
}
