ALTER TABLE nodes ADD COLUMN latency_targets_custom INTEGER NOT NULL DEFAULT 0 CHECK(latency_targets_custom IN (0,1));
CREATE TABLE node_latency_targets (
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    target_id TEXT NOT NULL REFERENCES probe_targets(id) ON DELETE CASCADE,
    PRIMARY KEY(node_id,target_id)
);
CREATE TRIGGER node_latency_targets_insert BEFORE INSERT ON node_latency_targets
WHEN (SELECT count(*) FROM node_latency_targets WHERE node_id=NEW.node_id)>=5
BEGIN SELECT RAISE(ABORT,'at most 5 node latency targets'); END;
CREATE TRIGGER node_latency_targets_update BEFORE UPDATE OF node_id ON node_latency_targets
WHEN NEW.node_id<>OLD.node_id AND (SELECT count(*) FROM node_latency_targets WHERE node_id=NEW.node_id)>=5
BEGIN SELECT RAISE(ABORT,'at most 5 node latency targets'); END;
