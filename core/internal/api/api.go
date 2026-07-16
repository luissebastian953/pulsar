// Package api is the HTTP surface (docs/architecture.md#endpoints).
package api

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"regexp"
	"time"

	"pulsar/core/internal/assembly"
	"pulsar/core/internal/blob"
	"pulsar/core/internal/compliance"
	"pulsar/core/internal/job"
	"pulsar/core/internal/workflow"
)

type Config struct {
	Lineage    string
	Checkpoint string
}

type Server struct {
	Store   *job.Store
	Broker  *job.Broker
	Checker compliance.Checker
	Blobs   blob.Store
	Tpl     workflow.Graph // txt2img base, hash-verified at load
	Cfg     Config
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /generate", s.handleGenerate)
	mux.HandleFunc("GET /jobs/{id}", s.handleJob)
	mux.HandleFunc("GET /jobs/{id}/stream", s.handleStream)
	mux.HandleFunc("GET /images/{id}", s.handleImage)
	mux.HandleFunc("GET /history", s.handleHistory)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	return mux
}

var jobIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// POST /generate: validate → CheckPrompt → assemble → build workflow →
// enqueue → 202. Generation is 20–40s; it never blocks this handler.
func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	var req assembly.Request
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	seed := int64(0)
	if req.Seed != nil {
		seed = *req.Seed
	} else {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			httpError(w, http.StatusInternalServerError, "seed generation failed")
			return
		}
		seed = int64(binary.BigEndian.Uint64(b[:]) >> 1) // non-negative
	}

	spec, err := assembly.Build(req, assembly.Options{Lineage: s.Cfg.Lineage, Seed: seed})
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}

	// pre-enqueue compliance hook; an unavailable checker rejects (fail closed)
	decision, err := s.Checker.CheckPrompt(r.Context(), spec)
	if err != nil {
		httpError(w, http.StatusServiceUnavailable, "compliance check unavailable")
		return
	}
	if !decision.Allowed {
		httpError(w, http.StatusUnprocessableEntity, "prompt rejected: "+decision.Reason)
		return
	}

	graph, err := workflow.BuildTxt2Img(s.Tpl, spec, s.Cfg.Checkpoint)
	if err != nil {
		log.Printf("api: build workflow: %v", err)
		httpError(w, http.StatusInternalServerError, "workflow build failed")
		return
	}

	params, err := json.Marshal(spec)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "params marshal failed")
		return
	}
	j, err := s.Store.Enqueue(r.Context(), "txt2img", params, graph)
	if err != nil {
		log.Printf("api: enqueue: %v", err)
		httpError(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": j.ID})
}

type jobPayload struct {
	ID            string          `json:"id"`
	Kind          string          `json:"kind"`
	State         job.State       `json:"state"`
	Params        json.RawMessage `json:"params"`
	ImageURL      string          `json:"image_url,omitempty"`
	Error         string          `json:"error,omitempty"`
	QueuePosition int             `json:"queue_position,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

func (s *Server) payload(r *http.Request, j *job.Job) jobPayload {
	p := jobPayload{
		ID: j.ID, Kind: j.Kind, State: j.State,
		Params: j.Params, Error: j.Error, CreatedAt: j.CreatedAt,
	}
	if j.State == job.StateDone && j.ImagePath != "" {
		p.ImageURL = "/images/" + j.ID
	}
	if j.State == job.StateQueued {
		if pos, err := s.Store.QueuePosition(r.Context(), j.ID); err == nil {
			p.QueuePosition = pos
		}
	}
	return p
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) (*job.Job, bool) {
	id := r.PathValue("id")
	if !jobIDPattern.MatchString(id) {
		httpError(w, http.StatusNotFound, "no such job")
		return nil, false
	}
	j, err := s.Store.Get(r.Context(), id)
	if errors.Is(err, job.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such job")
		return nil, false
	}
	if err != nil {
		log.Printf("api: get job: %v", err)
		httpError(w, http.StatusInternalServerError, "lookup failed")
		return nil, false
	}
	return j, true
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	j, ok := s.getJob(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.payload(r, j))
}

func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !jobIDPattern.MatchString(id) {
		httpError(w, http.StatusNotFound, "no such image")
		return
	}
	rc, err := s.Blobs.Open(id)
	if err != nil {
		httpError(w, http.StatusNotFound, "no such image")
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	io.Copy(w, rc)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.Store.History(r.Context(), 50)
	if err != nil {
		log.Printf("api: history: %v", err)
		httpError(w, http.StatusInternalServerError, "history failed")
		return
	}
	out := make([]jobPayload, 0, len(jobs))
	for _, j := range jobs {
		p := jobPayload{ID: j.ID, Kind: j.Kind, State: j.State, Params: j.Params, CreatedAt: j.CreatedAt}
		if j.State == job.StateDone && j.ImagePath != "" {
			p.ImageURL = "/images/" + j.ID
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": out})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.Ping(r.Context()); err != nil {
		httpError(w, http.StatusServiceUnavailable, "db unreachable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
