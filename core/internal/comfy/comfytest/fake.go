// Package comfytest: fake ComfyUI for GPU-free end-to-end tests.
package comfytest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type Fake struct {
	srv *httptest.Server

	mu     sync.Mutex
	n      int
	graphs map[string]json.RawMessage
	lastID string

	submitted chan string
	uploads   map[string][]byte

	// Steps progress messages are streamed per prompt.
	Steps int
	// PNG is returned by /view.
	PNG []byte
	// FailExecution makes every prompt end in an execution_error.
	FailExecution bool
	// TagOutputs makes /view return a distinct image per filename (see
	// OutputFor), so a test can trace each delivered blob to its SaveImage
	// node. The identity lives in the pixels: anything outside them is
	// stripped before delivery.
	TagOutputs bool
	// EmbedGraph makes /view carry the last submitted graph in a `prompt`
	// tEXt chunk, as real ComfyUI's SaveImage does without
	// --disable-metadata.
	EmbedGraph bool
}

func New(t *testing.T) *Fake {
	f := &Fake{
		graphs:    make(map[string]json.RawMessage),
		uploads:   make(map[string][]byte),
		submitted: make(chan string, 16),
		Steps:     4,
		PNG:       tinyPNG(t),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /prompt", f.handlePrompt)
	mux.HandleFunc("GET /ws", f.handleWS)
	mux.HandleFunc("GET /history/{id}", f.handleHistory)
	mux.HandleFunc("GET /view", f.handleView)
	mux.HandleFunc("GET /queue", f.handleQueue)
	mux.HandleFunc("POST /upload/image", f.handleUpload)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *Fake) URL() string { return f.srv.URL }

// LastGraph returns the most recently submitted workflow graph.
func (f *Fake) LastGraph() json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.graphs[f.lastID]
}

func (f *Fake) handlePrompt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Prompt   json.RawMessage `json:"prompt"`
		ClientID string          `json:"client_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Prompt) == 0 {
		http.Error(w, "bad prompt", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.n++
	id := fmt.Sprintf("fake-%d", f.n)
	f.graphs[id] = body.Prompt
	f.lastID = id
	f.mu.Unlock()

	f.submitted <- id
	json.NewEncoder(w).Encode(map[string]string{"prompt_id": id})
}

func (f *Fake) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	// r.Context() is not cancelled when a hijacked WebSocket closes, so the
	// reader doubles as the liveness signal: once the client closes, cancel
	// and leave. Without this a stale handler from a finished job keeps
	// competing for `submitted` — and wins, channel receivers being FIFO —
	// so the second generation in an env never sees its completion.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	// consume reads so close frames are processed and the client's graceful
	// close doesn't hang until its timeout
	go func() {
		defer cancel()
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-f.submitted:
			for step := 1; step <= f.Steps; step++ {
				wsjson.Write(ctx, conn, map[string]any{
					"type": "progress",
					"data": map[string]any{"value": step, "max": f.Steps, "prompt_id": id},
				})
			}
			if f.FailExecution {
				wsjson.Write(ctx, conn, map[string]any{
					"type": "execution_error",
					"data": map[string]any{"prompt_id": id, "exception_message": "fake execution failure"},
				})
				continue
			}
			wsjson.Write(ctx, conn, map[string]any{
				"type": "executing",
				"data": map[string]any{"node": nil, "prompt_id": id},
			})
		}
	}
}

// one output image per SaveImage node, like real ComfyUI
func (f *Fake) handleHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	f.mu.Lock()
	graph, known := f.graphs[id]
	f.mu.Unlock()
	out := map[string]any{}
	if known && !f.FailExecution {
		var nodes map[string]struct {
			ClassType string `json:"class_type"`
		}
		outputs := map[string]any{}
		if json.Unmarshal(graph, &nodes) == nil {
			for nodeID, n := range nodes {
				if n.ClassType != "SaveImage" {
					continue
				}
				outputs[nodeID] = map[string]any{
					"images": []map[string]string{
						{"filename": "node-" + nodeID + ".png", "subfolder": "", "type": "output"},
					},
				}
			}
		}
		out[id] = map[string]any{"outputs": outputs}
	}
	json.NewEncoder(w).Encode(out)
}

func (f *Fake) handleView(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	out := f.OutputFor(r.URL.Query().Get("filename"))
	if f.EmbedGraph {
		out = WithText(out, "prompt", string(f.LastGraph()))
	}
	w.Write(out)
}

// OutputFor returns the pixels /view serves for filename: PNG, or with
// TagOutputs a 1x1 image whose colour is derived from the filename.
func (f *Fake) OutputFor(filename string) []byte {
	if !f.TagOutputs {
		return f.PNG
	}
	sum := sha256.Sum256([]byte(filename))
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{sum[0], sum[1], sum[2], 255})
	var buf bytes.Buffer
	png.Encode(&buf, img) // cannot fail on a bytes.Buffer
	return buf.Bytes()
}

// WithText returns src with a tEXt chunk (keyword, text) spliced in right
// after IHDR — the shape of ComfyUI's metadata. src must be a PNG.
func WithText(src []byte, keyword, text string) []byte {
	const ihdrEnd = 8 + 4 + 4 + 13 + 4 // signature + IHDR chunk
	body := append([]byte("tEXt"+keyword+"\x00"), text...)
	out := append([]byte{}, src[:ihdrEnd]...)
	out = binary.BigEndian.AppendUint32(out, uint32(len(body)-4))
	out = append(out, body...)
	out = binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(body))
	return append(out, src[ihdrEnd:]...)
}

func (f *Fake) handleUpload(w http.ResponseWriter, r *http.Request) {
	file, hdr, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "image field required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	data, _ := io.ReadAll(file)
	f.mu.Lock()
	f.uploads[hdr.Filename] = data
	f.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]string{"name": hdr.Filename})
}

// Upload returns the bytes forwarded under name, if any.
func (f *Fake) Upload(name string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.uploads[name]
	return d, ok
}

func (f *Fake) handleQueue(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]any{
		"queue_running": []any{},
		"queue_pending": []any{},
	})
}

func tinyPNG(t *testing.T) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}
