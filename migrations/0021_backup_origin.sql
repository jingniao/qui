ALTER TABLE backups ADD COLUMN origin TEXT NOT NULL DEFAULT 'local' CHECK(origin IN ('local','imported'));
