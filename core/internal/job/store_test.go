package job_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"pulsar/core/internal/job"
)

// testStore gives each test its own Postgres schema so packages (and tests)
// sharing PULSAR_TEST_DATABASE_URL cannot collide.
func testStore(t *testing.T, schema string) *job.Store {
	return job.NewStore(testPool(t, schema))
}

func testPool(t *testing.T, schema string) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PULSAR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PULSAR_TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	// public stays on the path so pg_trgm's operator classes resolve
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	files, err := filepath.Glob("../../../db/migrations/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("no migrations found: %v", err)
	}
	sort.Strings(files)

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	// serialize schema setup across parallel test packages — CREATE
	// EXTENSION IF NOT EXISTS races with itself; the lock is session-scoped
	// so setup must run on this one connection
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(727272)"); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, "SELECT pg_advisory_unlock(727272)")

	stmts := []string{
		fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema),
		fmt.Sprintf("CREATE SCHEMA %s", schema),
	}
	for _, f := range files {
		sql, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		stmts = append(stmts, string(sql))
	}
	for _, sql := range stmts {
		if _, err := conn.Exec(ctx, sql); err != nil {
			t.Fatalf("test schema setup: %v", err)
		}
	}
	return pool
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
		j, err := s.Enqueue(ctx, "txt2img", params, graphRaw)
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

	j, err := s.Enqueue(ctx, "txt2img", params, graphRaw)
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

	j, err := s.Enqueue(ctx, "txt2img", params, graphRaw)
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
		if _, err := s.Enqueue(ctx, "txt2img", params, graphRaw); err != nil {
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
