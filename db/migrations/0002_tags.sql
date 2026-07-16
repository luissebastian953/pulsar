-- Tag vocabulary: the intersection of "exists on Danbooru" and "the model
-- learned it" (docs/tagdb.md). Queried by autocomplete and NL grounding.
-- pinned to public so its operator classes resolve under any search_path
CREATE EXTENSION IF NOT EXISTS pg_trgm SCHEMA public;

CREATE TABLE tags (
    name          text PRIMARY KEY,  -- canonical, underscored
    category      text NOT NULL
                  CHECK (category IN ('general', 'character', 'copyright', 'artist', 'meta')),
    post_count    int  NOT NULL,     -- Danbooru frequency; proxy for model knowledge
    pose_relevant boolean NOT NULL DEFAULT FALSE
);

CREATE TABLE tag_aliases (
    alias     text PRIMARY KEY,
    canonical text NOT NULL REFERENCES tags (name)
);

CREATE INDEX tags_trgm ON tags USING GIN (name gin_trgm_ops);
CREATE INDEX tags_pose ON tags (pose_relevant) WHERE pose_relevant;
-- prefix autocomplete for 1-2 char queries, where trigrams can't help
CREATE INDEX tags_prefix ON tags (name text_pattern_ops);
