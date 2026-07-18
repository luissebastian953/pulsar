package job_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"pulsar/core/internal/job"
	"pulsar/core/internal/pgtest"
)

func testStore(t *testing.T, schema string) *job.Store {
	return job.NewStore(pgtest.Pool(t, schema))
}

var (
	params   = json.RawMessage(`{"prompt":"1girl","seed":42}`)
	graphRaw = json.RawMessage(`{"5":{"class_type":"KSampler","inputs":{}}}`)
)

func TestEnqueueClaimFIFO(t *testing.T) {
	s := testStore(t, "job_fifo")
	ctx := context.Background()

	var ids []string
	for i := 0; i < 3; i++ {
		j, err := s.Enqueue(ctx, "txt2img", params, graphRaw, "", "", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, j.ID)
	}
	for i, id := range ids {
		pos, err := s.QueuePosition(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if pos != i+1 {
			t.Errorf("job %d queue position = %d, want %d", i, pos, i+1)
		}
	}
	for i := 0; i < 3; i++ {
		j, ok, err := s.ClaimNext(ctx)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		if j.ID != ids[i] {
			t.Errorf("claim %d = %s, want %s (FIFO)", i, j.ID, ids[i])
		}
		if string(j.Workflow) == "" || string(j.Params) == "" {
			t.Error("claimed job missing params/workflow")
		}
	}
	if _, ok, err := s.ClaimNext(ctx); err != nil || ok {
		t.Errorf("empty queue: ok=%v err=%v, want false nil", ok, err)
	}
}

func TestTransitionGuards(t *testing.T) {
	s := testStore(t, "job_guards")
	ctx := context.Background()

	j, err := s.Enqueue(ctx, "txt2img", params, graphRaw, "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// done requires running — enforced in SQL, not just in Go
	if err := s.MarkDone(ctx, j.ID, "x.png"); !errors.Is(err, job.ErrConflict) {
		t.Errorf("MarkDone on queued = %v, want ErrConflict", err)
	}
	if _, ok, _ := s.ClaimNext(ctx); !ok {
		t.Fatal("claim failed")
	}
	if err := s.MarkDone(ctx, j.ID, j.ID+".png"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkFailed(ctx, j.ID, "late"); !errors.Is(err, job.ErrConflict) {
		t.Errorf("MarkFailed on done = %v, want ErrConflict", err)
	}

	got, err := s.Get(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != job.StateDone || got.ImagePath != j.ID+".png" || got.FinishedAt == nil {
		t.Errorf("got %+v", got)
	}
}

func TestBlockedIsFirstClass(t *testing.T) {
	s := testStore(t, "job_blocked")
	ctx := context.Background()

	j, err := s.Enqueue(ctx, "txt2img", params, graphRaw, "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.ClaimNext(ctx); !ok {
		t.Fatal("claim failed")
	}
	if err := s.MarkBlocked(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != job.StateBlocked {
		t.Errorf("state = %s, want blocked", got.State)
	}
	if got.ImagePath != "" {
		t.Error("blocked job must never have an image path")
	}
	if got.Error != "" {
		t.Error("blocked is a state, not an error")
	}
}

func TestGetNotFound(t *testing.T) {
	s := testStore(t, "job_notfound")
	_, err := s.Get(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, job.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestHistory(t *testing.T) {
	s := testStore(t, "job_history")
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := s.Enqueue(ctx, "txt2img", params, graphRaw, "", "", "", nil); err != nil {
			t.Fatal(err)
		}
	}
	jobs, err := s.History(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Errorf("history len = %d, want 2 (limit)", len(jobs))
	}
}
