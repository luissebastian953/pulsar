-- blob id of the inpaint mask (white = repaint).
ALTER TABLE jobs ADD COLUMN mask_path text;
