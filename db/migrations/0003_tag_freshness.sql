-- Danbooru creation date; NULL until the next seed-tags backfill
-- (docs/tagdb.md).
ALTER TABLE tags ADD COLUMN danbooru_created_at timestamptz;
