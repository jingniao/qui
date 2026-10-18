ALTER TABLE outbounds ADD COLUMN probe_url TEXT NOT NULL DEFAULT 'https://www.gstatic.com/generate_204';
ALTER TABLE outbounds ADD COLUMN probe_interval INTEGER NOT NULL DEFAULT 30;
