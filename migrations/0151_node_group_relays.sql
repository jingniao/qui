-- Linked tunnel rules become node group members of their own. A rule endpoint
-- reaches a subscription through this membership, independently of whether the
-- group also holds the target inbound (the direct endpoint).
CREATE TABLE node_group_relays (
    group_id TEXT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
    rule_id TEXT NOT NULL REFERENCES tunnel_rules(id) ON DELETE CASCADE,
    PRIMARY KEY(group_id,rule_id)
);
CREATE INDEX node_group_relays_rule ON node_group_relays(rule_id);

-- Until now a rule followed its target inbound: keep every existing
-- subscription unchanged by granting each linked rule to the groups that hold
-- its target.
INSERT INTO node_group_relays(group_id,rule_id)
    SELECT DISTINCT e.group_id,r.id FROM tunnel_rules r
    JOIN node_group_inbounds e ON e.inbound_id=r.target_inbound_id
    WHERE r.target_mode='linked';
