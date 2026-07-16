// Package comfytest is a fake ComfyUI server implementing the slice of the
// API core uses (/prompt, /ws, /history, /view, /queue). It exists so the
// request-to-image spine is testable end-to-end without a GPU.
package comfytest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
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

	// Steps progress messages are streamed per prompt.
	Steps int
	// PNG is returned by /view.
	PNG []byte
	// FailExecution makes every prompt end in an execution_error.
	FailExecution bool
}

func New(t *testing.T) *Fake {
	f := &Fake{
		graphs:    make(map[string]json.RawMessage),
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
	ctx := r.Context()
	// consume reads so close frames are processed and the client's graceful
	// close doesn't hang until its timeout
	go func() {
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

func (f *Fake) handleHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	f.mu.Lock()
	_, known := f.graphs[id]
	f.mu.Unlock()
	out := map[string]any{}
	if known && !f.FailExecution {
		out[id] = map[string]any{
			"outputs": map[string]any{
				"7": map[string]any{
					"images": []map[string]string{
						{"filename": "pulsar_00001_.png", "subfolder": "", "type": "output"},
					},
				},
			},
		}
	}
	json.NewEncoder(w).Encode(out)
}

func (f *Fake) handleView(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Write(f.PNG)
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
