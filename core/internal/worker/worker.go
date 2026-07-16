// Package worker consumes the job queue and executes generations.
package worker

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"pulsar/core/internal/blob"
	"pulsar/core/internal/compliance"
	"pulsar/core/internal/job"
)

// Executor is satisfied by comfy.Router.
type Executor interface {
	Execute(ctx context.Context, graph json.RawMessage, onProgress func(step, total int)) ([]byte, error)
}

type Worker struct {
	Store   *job.Store
	Broker  *job.Broker
	Exec    Executor
	Checker compliance.Checker
	Blobs   blob.Store
	// ExecTimeout bounds a single generation; a prompt with no result by
	// then is stuck (ComfyUI dies — treat it as unreliable) and fails.
	ExecTimeout time.Duration
	Poll        time.Duration
}

func (w *Worker) Run(ctx context.Context) {
	poll := w.Poll
	if poll == 0 {
		poll = 500 * time.Millisecond
	}
	for {
		j, ok, err := w.Store.ClaimNext(ctx)
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return
			}
			log.Printf("worker: claim: %v", err)
		case ok:
			w.process(ctx, j)
			continue // drain the queue before sleeping
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(poll):
		}
	}
}

func (w *Worker) process(ctx context.Context, j *job.Job) {
	timeout := w.ExecTimeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	img, err := w.Exec.Execute(execCtx, j.Workflow, func(step, total int) {
		w.Broker.Publish(j.ID, job.Event{Type: job.EventProgress, Step: step, Total: total})
	})
	if err != nil {
		w.finish(ctx, j.ID, job.EventFailed, func() error {
			return w.Store.MarkFailed(ctx, j.ID, err.Error())
		})
		return
	}

	// post-generation, pre-delivery — the image must not reach storage or a
	// client unclassified, and an unavailable checker blocks (fail closed)
	dec, cerr := w.Checker.CheckOutput(ctx, img)
	if cerr != nil || !dec.Allowed {
		if cerr != nil {
			log.Printf("worker: job %s: compliance check unavailable, blocking: %v", j.ID, cerr)
		} else {
			log.Printf("worker: job %s blocked: %s", j.ID, dec.Reason)
		}
		w.finish(ctx, j.ID, job.EventBlocked, func() error {
			return w.Store.MarkBlocked(ctx, j.ID)
		})
		return
	}

	if err := w.Blobs.Put(j.ID, img); err != nil {
		w.finish(ctx, j.ID, job.EventFailed, func() error {
			return w.Store.MarkFailed(ctx, j.ID, "store image: "+err.Error())
		})
		return
	}
	w.finish(ctx, j.ID, job.EventDone, func() error {
		return w.Store.MarkDone(ctx, j.ID, j.ID+".png")
	})
}

// finish commits the terminal state before publishing, so subscribers that
// re-read the row on the event always see the final state.
func (w *Worker) finish(ctx context.Context, id, event string, mark func() error) {
	if err := mark(); err != nil {
		log.Printf("worker: job %s: mark %s: %v", id, event, err)
		return
	}
	w.Broker.Publish(id, job.Event{Type: event})
}
