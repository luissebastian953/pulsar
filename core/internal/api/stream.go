package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"pulsar/core/internal/job"
)

// GET /jobs/{id}/stream — SSE relay: queue position → step progress →
// terminal state. Job state lives in the DB, never in this handler, so a
// client can drop and re-attach to an in-flight job at any point: every
// connection starts from a fresh DB snapshot, and the periodic re-read
// recovers anything the broker dropped.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	j, ok := s.getJob(w, r)
	if !ok {
		return
	}
	flusher, canFlush := w.(http.Flusher)
	if !canFlush {
		httpError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// subscribe before the snapshot so no event can fall between them
	events, cancel := s.Broker.Subscribe(j.ID)
	defer cancel()

	emit := func(event string, v any) {
		data, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		flusher.Flush()
	}
	terminal := func(j *job.Job) {
		emit(string(j.State), s.payload(r, j))
	}

	if j.State.Terminal() {
		terminal(j)
		return
	}
	if j.State == job.StateQueued {
		emit("queue", s.payload(r, j))
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return

		case ev := <-events:
			switch ev.Type {
			case job.EventProgress:
				emit("progress", map[string]int{"step": ev.Step, "total": ev.Total})
			default: // terminal event — re-read the row, it is the truth
				fresh, err := s.Store.Get(r.Context(), j.ID)
				if err != nil {
					return
				}
				terminal(fresh)
				return
			}

		case <-ticker.C:
			fresh, err := s.Store.Get(r.Context(), j.ID)
			if err != nil {
				return
			}
			switch {
			case fresh.State.Terminal():
				terminal(fresh)
				return
			case fresh.State == job.StateQueued:
				emit("queue", s.payload(r, fresh))
			}
		}
	}
}
