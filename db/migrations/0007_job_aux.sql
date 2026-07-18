-- server-generated auxiliary inputs forwarded to the executing process
-- (regional masks today; anything mask-shaped tomorrow).
ALTER TABLE jobs ADD COLUMN aux_paths text[];
