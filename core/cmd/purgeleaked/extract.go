package main

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"pulsar/core/internal/pngmeta"
)

// blobIDPattern mirrors the serving pattern in internal/api: a UUID with an
// optional role suffix. Anything else in a LoadImage input is not a blob.
var blobIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}(-[a-z]{1,16})?$`)

// graphBlobIDs returns the blob IDs an API-format graph loads through
// LoadImage nodes (`"image": "<id>.png"`), sorted and de-duplicated. A graph
// that does not parse yields none.
func graphBlobIDs(graph []byte) []string {
	var nodes map[string]struct {
		ClassType string `json:"class_type"`
		Inputs    struct {
			Image json.RawMessage `json:"image"`
		} `json:"inputs"`
	}
	if json.Unmarshal(graph, &nodes) != nil {
		return nil
	}
	seen := map[string]bool{}
	for _, n := range nodes {
		if n.ClassType != "LoadImage" {
			continue
		}
		var name string
		if json.Unmarshal(n.Inputs.Image, &name) != nil { // a link, not a filename
			continue
		}
		id := strings.TrimSuffix(name, ".png")
		if id != name && blobIDPattern.MatchString(id) {
			seen[id] = true
		}
	}
	return sortedKeys(seen)
}

// embeddedGraph returns the graph ComfyUI's SaveImage wrote into a PNG's
// `prompt` text chunk (tEXt, or uncompressed iTXt), if there is one.
func embeddedGraph(png []byte) ([]byte, bool) {
	chunks, err := pngmeta.Chunks(png)
	if err != nil {
		return nil, false
	}
	for _, c := range chunks {
		keyword, rest, ok := bytes.Cut(c.Data, []byte{0})
		if !ok || string(keyword) != "prompt" {
			continue
		}
		switch c.Type {
		case "tEXt":
			return rest, true
		case "iTXt":
			// compression flag, method, then NUL-terminated language tag
			// and translated keyword
			if len(rest) < 2 || rest[0] != 0 {
				continue
			}
			parts := bytes.SplitN(rest[2:], []byte{0}, 3)
			if len(parts) == 3 {
				return parts[2], true
			}
		}
	}
	return nil, false
}

// plan decides what to delete. referenced is every blob ID a stored graph or
// a blob's embedded graph loads. Two classes are kept no matter what:
//   - job outputs (a generated image reused as an img2img base or reference
//     is history, not a leaked upload);
//   - inputs of a queued or running job, which the worker still has to read.
type plan struct {
	Delete     []string // referenced, safe to remove, present on disk
	KeptOutput []string // referenced but a job's own output
	KeptActive []string // referenced by a queued/running job
	Gone       []string // referenced, already absent from the blob dir
}

func makePlan(referenced, jobIDs, active, onDisk map[string]bool) plan {
	var p plan
	for _, id := range sortedKeys(referenced) {
		switch {
		case jobIDs[id[:36]]:
			p.KeptOutput = append(p.KeptOutput, id)
		case active[id]:
			p.KeptActive = append(p.KeptActive, id)
		case !onDisk[id]:
			p.Gone = append(p.Gone, id)
		default:
			p.Delete = append(p.Delete, id)
		}
	}
	return p
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
