-- Job queue. blocked is a first-class state from the first migration
-- (docs/compliance.md) — never a flag on failed.
CREATE TABLE jobs (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind        text NOT NULL DEFAULT 'txt2img',
    state       text NOT NULL DEFAULT 'queued'
                CHECK (state IN ('queued', 'running', 'done', 'failed', 'blocked')),
    user_id     text,
    -- resolved, post-translation params (docs/architecture.md#storage):
    -- same seed + same params = same image
    params      jsonb NOT NULL,
    -- patched API-format graph, built before enqueue
    workflow    jsonb NOT NULL,
    error       text,
    image_path  text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    started_at  timestamptz,
    finished_at timestamptz
);

CREATE INDEX jobs_claim ON jobs (created_at) WHERE state = 'queued';
CREATE INDEX jobs_history ON jobs (created_at DESC);
