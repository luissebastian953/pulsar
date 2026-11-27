package main

import (
	"reflect"
	"testing"

	"pulsar/core/internal/comfy/comfytest"
	"pulsar/core/internal/pngmeta"
)

const (
	upA = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	upB = "bbbbbbbb-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	upC = "cccccccc-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	job = "dddddddd-bbbb-4ccc-8ddd-eeeeeeeeeeee"
)

// the shape core submits: inpaint base + mask, an IP-Adapter reference, and
// the things that must not match
const fixtureGraph = `{
  "3":  {"class_type": "KSampler", "inputs": {"seed": 5, "latent_image": ["9", 0]}},
  "4":  {"class_type": "CheckpointLoaderSimple", "inputs": {"ckpt_name": "` + upC + `.png"}},
  "8":  {"class_type": "LoadImage", "inputs": {"image": "` + upA + `.png"}},
  "20": {"class_type": "LoadImage", "inputs": {"image": "` + upB + `.png"}},
  "21": {"class_type": "LoadImage", "inputs": {"image": "` + upA + `.png"}},
  "22": {"class_type": "LoadImage", "inputs": {"image": "` + job + `-lineart.png"}},
  "30": {"class_type": "LoadImage", "inputs": {"image": "example.png"}},
  "31": {"class_type": "LoadImage", "inputs": {"image": "../../etc/passwd.png"}},
  "32": {"class_type": "LoadImage", "inputs": {"image": "` + upC + `"}},
  "33": {"class_type": "LoadImage", "inputs": {"image": ["8", 0]}},
  "34": {"class_type": "LoadImage", "inputs": {}},
  "40": {"class_type": "SaveImage", "inputs": {"images": ["3", 0], "filename_prefix": "pulsar"}}
}`

func TestGraphBlobIDs(t *testing.T) {
	want := []string{upA, upB, job + "-lineart"}
	if got := graphBlobIDs([]byte(fixtureGraph)); !reflect.DeepEqual(got, want) {
		t.Errorf("ids = %v, want %v", got, want)
	}
	for _, bad := range []string{``, `null`, `[]`, `{"8": 3}`, `{"8": {"class_type": "LoadImage"`} {
		if got := graphBlobIDs([]byte(bad)); len(got) != 0 {
			t.Errorf("graphBlobIDs(%q) = %v", bad, got)
		}
	}
}

func basePNG() []byte {
	// signature, 1x1 IHDR, IEND — enough structure for the chunk walker
	out, err := pngmeta.Rewrite(
		[]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\x00IEND\xaeB`\x82"),
		pngmeta.Allowed, nil)
	if err != nil {
		panic(err)
	}
	return out
}

func TestEmbeddedGraph(t *testing.T) {
	base := basePNG()
	if _, ok := embeddedGraph(base); ok {
		t.Error("found a graph in a bare png")
	}
	if _, ok := embeddedGraph([]byte("not a png")); ok {
		t.Error("found a graph in garbage")
	}

	legacy := comfytest.WithText(base, "prompt", fixtureGraph)
	graph, ok := embeddedGraph(legacy)
	if !ok || string(graph) != fixtureGraph {
		t.Fatalf("tEXt prompt not recovered (ok=%v)", ok)
	}
	if got := graphBlobIDs(graph); len(got) != 3 {
		t.Errorf("ids from blob = %v", got)
	}

	// other keywords are not the graph; the prompt chunk after them still is
	mixed := comfytest.WithText(legacy, "parameters", "1girl")
	if graph, ok := embeddedGraph(mixed); !ok || string(graph) != fixtureGraph {
		t.Error("prompt chunk behind another text chunk not recovered")
	}
	if _, ok := embeddedGraph(comfytest.WithText(base, "workflow", fixtureGraph)); ok {
		t.Error("a non-prompt keyword was read as the graph")
	}

	itxt := func(body string) []byte {
		out, err := pngmeta.Rewrite(base, pngmeta.Allowed, []pngmeta.Chunk{{Type: "iTXt", Data: []byte(body)}})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if graph, ok := embeddedGraph(itxt("prompt\x00\x00\x00en\x00\x00" + fixtureGraph)); !ok || string(graph) != fixtureGraph {
		t.Error("uncompressed iTXt prompt not recovered")
	}
	for name, body := range map[string]string{
		"compressed": "prompt\x00\x01\x00\x00\x00" + fixtureGraph,
		"truncated":  "prompt\x00\x00",
		"no fields":  "prompt\x00\x00\x00en",
		"no keyword": "prompt",
	} {
		if _, ok := embeddedGraph(itxt(body)); ok {
			t.Errorf("%s iTXt read as a graph", name)
		}
	}
}

func TestMakePlan(t *testing.T) {
	set := func(ids ...string) map[string]bool {
		m := map[string]bool{}
		for _, id := range ids {
			m[id] = true
		}
		return m
	}
	const gone = "eeeeeeee-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	p := makePlan(
		set(upA, upB, upC, job, job+"-lineart", gone),
		set(job), // job rows
		set(upB), // a queued job still needs upB
		set(upA, upB, upC, job, job+"-lineart", "ffffffff-bbbb-4ccc-8ddd-eeeeeeeeeeee"),
	)
	want := plan{
		Delete:     []string{upA, upC},
		KeptOutput: []string{job, job + "-lineart"},
		KeptActive: []string{upB},
		Gone:       []string{gone},
	}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("plan = %+v\nwant   %+v", p, want)
	}
}
