// Package worker consumes the job queue and executes generations.
package worker

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"time"

	"pulsar/core/internal/blob"
	"pulsar/core/internal/comfy"
	"pulsar/core/internal/compliance"
	"pulsar/core/internal/job"
	"pulsar/core/internal/workflow"
)

// Executor is satisfied by comfy.Router; outputs keyed by SaveImage node ID.
type Executor interface {
	ExecuteAll(ctx context.Context, graph json.RawMessage, inputs []comfy.Input, onProgress func(step, total int)) (map[string][]byte, error)
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

	roles, ok := workflow.OutputRoles(j.Kind)
	if !ok {
		w.finish(ctx, j.ID, job.EventFailed, func() error {
			return w.Store.MarkFailed(ctx, j.ID, "unknown job kind "+j.Kind)
		})
		return
	}

	var inputs []comfy.Input
	for _, blobID := range append([]string{j.InputPath, j.MaskPath, j.RefPath}, j.AuxPaths...) {
		if blobID == "" {
			continue
		}
		data, rerr := readBlob(w.Blobs, blobID)
		if rerr != nil {
			w.finish(ctx, j.ID, job.EventFailed, func() error {
				return w.Store.MarkFailed(ctx, j.ID, "input image unavailable: "+blobID)
			})
			return
		}
		inputs = append(inputs, comfy.Input{Name: blobID + ".png", Data: data})
	}

	outs, err := w.Exec.ExecuteAll(execCtx, j.Workflow, inputs, func(step, total int) {
		w.Broker.Publish(j.ID, job.Event{Type: job.EventProgress, Step: step, Total: total})
	})
	if err != nil {
		w.finish(ctx, j.ID, job.EventFailed, func() error {
			return w.Store.MarkFailed(ctx, j.ID, err.Error())
		})
		return
	}

	// pre-delivery compliance, fail closed; one blocked role blocks the job
	for role, node := range roles {
		img, ok := outs[node]
		if !ok {
			w.finish(ctx, j.ID, job.EventFailed, func() error {
				return w.Store.MarkFailed(ctx, j.ID, "missing output for node "+node)
			})
			return
		}
		dec, cerr := w.Checker.CheckOutput(ctx, img)
		if cerr != nil || !dec.Allowed {
			if cerr != nil {
				log.Printf("worker: job %s: compliance check unavailable, blocking: %v", j.ID, cerr)
			} else {
				log.Printf("worker: job %s blocked (%s): %s", j.ID, roleName(role), dec.Reason)
			}
			w.finish(ctx, j.ID, job.EventBlocked, func() error {
				return w.Store.MarkBlocked(ctx, j.ID)
			})
			return
		}
	}

	for role, node := range roles {
		if err := w.Blobs.Put(blobID(j.ID, role), outs[node]); err != nil {
			w.finish(ctx, j.ID, job.EventFailed, func() error {
				return w.Store.MarkFailed(ctx, j.ID, "store image: "+err.Error())
			})
			return
		}
	}
	w.finish(ctx, j.ID, job.EventDone, func() error {
		return w.Store.MarkDone(ctx, j.ID, j.ID+".png")
	})
}

func readBlob(store blob.Store, id string) ([]byte, error) {
	rc, err := store.Open(id)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func blobID(jobID, role string) string {
	if role == "" {
		return jobID
	}
	return jobID + "-" + role
}

func roleName(role string) string {
	if role == "" {
		return "primary"
	}
	return role
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
