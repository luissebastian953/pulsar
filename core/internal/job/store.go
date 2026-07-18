package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("job not found")

// ErrConflict means the row was not in the state the transition requires —
// transitions are guarded in SQL, not just in Go.
var ErrConflict = errors.New("illegal state transition")

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

func (s *Store) Enqueue(ctx context.Context, kind string, params, workflow json.RawMessage, inputPath, maskPath, refPath string, auxPaths []string) (*Job, error) {
	j := &Job{Kind: kind, State: StateQueued, Params: params, Workflow: workflow, InputPath: inputPath, MaskPath: maskPath, RefPath: refPath, AuxPaths: auxPaths}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO jobs (kind, params, workflow, input_path, mask_path, ref_path, aux_paths) VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), $7)
		 RETURNING id::text, created_at`,
		kind, params, workflow, inputPath, maskPath, refPath, auxPaths,
	).Scan(&j.ID, &j.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("enqueue: %w", err)
	}
	return j, nil
}

// ClaimNext claims the oldest queued job (FOR UPDATE SKIP LOCKED);
// ok=false when the queue is empty.
func (s *Store) ClaimNext(ctx context.Context) (*Job, bool, error) {
	j := &Job{State: StateRunning}
	err := s.pool.QueryRow(ctx,
		`UPDATE jobs SET state = 'running', started_at = now()
		 WHERE id = (
		   SELECT id FROM jobs WHERE state = 'queued'
		   ORDER BY created_at
		   LIMIT 1
		   FOR UPDATE SKIP LOCKED
		 )
		 RETURNING id::text, kind, params, workflow, coalesce(input_path, ''), coalesce(mask_path, ''), coalesce(ref_path, ''), coalesce(aux_paths, '{}')`,
	).Scan(&j.ID, &j.Kind, &j.Params, &j.Workflow, &j.InputPath, &j.MaskPath, &j.RefPath, &j.AuxPaths)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("claim: %w", err)
	}
	return j, true, nil
}

func (s *Store) MarkDone(ctx context.Context, id, imagePath string) error {
	return s.finish(ctx,
		`UPDATE jobs SET state = 'done', image_path = $2, finished_at = now()
		 WHERE id = $1::uuid AND state = 'running'`, id, imagePath)
}

func (s *Store) MarkFailed(ctx context.Context, id, errMsg string) error {
	return s.finish(ctx,
		`UPDATE jobs SET state = 'failed', error = $2, finished_at = now()
		 WHERE id = $1::uuid AND state = 'running'`, id, errMsg)
}

// MarkBlocked records a compliance rejection. No image path is ever written
// for a blocked job — the output must not be deliverable.
func (s *Store) MarkBlocked(ctx context.Context, id string) error {
	return s.finish(ctx,
		`UPDATE jobs SET state = 'blocked', finished_at = now()
		 WHERE id = $1::uuid AND state = 'running'`, id)
}

func (s *Store) finish(ctx context.Context, sql string, args ...any) error {
	tag, err := s.pool.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (s *Store) Get(ctx context.Context, id string) (*Job, error) {
	j := &Job{}
	err := s.pool.QueryRow(ctx,
		`SELECT id::text, kind, state, params, workflow,
		        coalesce(error, ''), coalesce(image_path, ''),
		        created_at, started_at, finished_at
		 FROM jobs WHERE id = $1::uuid`, id,
	).Scan(&j.ID, &j.Kind, &j.State, &j.Params, &j.Workflow,
		&j.Error, &j.ImagePath, &j.CreatedAt, &j.StartedAt, &j.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return j, nil
}

// QueuePosition is 1-based among queued jobs; only meaningful while queued.
func (s *Store) QueuePosition(ctx context.Context, id string) (int, error) {
	var pos int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) + 1 FROM jobs
		 WHERE state = 'queued'
		   AND created_at < (SELECT created_at FROM jobs WHERE id = $1::uuid)`, id,
	).Scan(&pos)
	return pos, err
}

func (s *Store) History(ctx context.Context, limit int) ([]*Job, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, kind, state, params, coalesce(image_path, ''), created_at
		 FROM jobs ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		j := &Job{}
		if err := rows.Scan(&j.ID, &j.Kind, &j.State, &j.Params, &j.ImagePath, &j.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
