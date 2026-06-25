ALTER TABLE downloads ADD COLUMN file_name_markers TEXT NOT NULL DEFAULT '[]';
ALTER TABLE downloads ADD COLUMN data_rows TEXT;
