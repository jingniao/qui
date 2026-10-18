ALTER TABLE node_ca_successor_rounds ADD COLUMN source_format INTEGER NOT NULL DEFAULT 0;
ALTER TABLE node_ca_successor_rounds ADD COLUMN recovery_parent BLOB NOT NULL DEFAULT X'';
CREATE INDEX node_ca_successor_recovery_parent ON node_ca_successor_rounds(source_format) WHERE source_format<>0;
