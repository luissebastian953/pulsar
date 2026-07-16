// Package compliance holds the two mandatory hooks: pre-enqueue and
// post-generation-pre-delivery (plus upload ingest). These are call sites,
// not features — they may be stubs before phase 4, the call sites stay, and
// every new job type wires both (docs/compliance.md).
//
// Callers must fail closed: an error from a checker means block/reject,
// never pass.
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
