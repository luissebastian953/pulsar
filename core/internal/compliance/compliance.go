// Package compliance: the mandatory hooks. Callers fail closed — a checker
// error means block/reject, never pass (docs/compliance.md).
package compliance

import (
	"context"

	"pulsar/core/internal/assembly"
)

type Decision struct {
	Allowed bool
	Reason  string
}

type Checker interface {
	// CheckPrompt runs before enqueue; a rejection never reaches the queue.
	CheckPrompt(ctx context.Context, spec assembly.Spec) (Decision, error)
	// CheckOutput runs after generation, before delivery. A rejection sets
	// state=blocked; the image is never written to public storage.
	CheckOutput(ctx context.Context, image []byte) (Decision, error)
	// CheckUpload runs on image ingest (img2img, references, inpaint bases).
	CheckUpload(ctx context.Context, image []byte) (Decision, error)
}

// Stub allows everything. Phases 1–3 only — nothing is public. The real
// classifier lands in phase 4 and must fully close before any public access.
type Stub struct{}

func (Stub) CheckPrompt(context.Context, assembly.Spec) (Decision, error) {
	return Decision{Allowed: true}, nil
}

func (Stub) CheckOutput(context.Context, []byte) (Decision, error) {
	return Decision{Allowed: true}, nil
}

func (Stub) CheckUpload(context.Context, []byte) (Decision, error) {
	return Decision{Allowed: true}, nil
}
