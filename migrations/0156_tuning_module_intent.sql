ALTER TABLE node_tuning ADD COLUMN install_missing_modules INTEGER NOT NULL DEFAULT 0 CHECK (install_missing_modules IN (0,1));
