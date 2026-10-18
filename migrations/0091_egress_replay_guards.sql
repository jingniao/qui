CREATE TABLE egress_replay_guards (
  origin_node_id TEXT NOT NULL,
  origin_core_epoch TEXT NOT NULL,
  origin_connection_id TEXT NOT NULL,
  hop_index INTEGER NOT NULL CHECK(hop_index>=0),
  nonce TEXT NOT NULL CHECK(length(nonce)>=1 AND length(nonce)<=128),
  source_time INTEGER NOT NULL CHECK(source_time>=0),
  max_datagram_seq INTEGER NOT NULL CHECK(max_datagram_seq>=0),
  first_seen_at TEXT NOT NULL,
  PRIMARY KEY(origin_node_id, origin_core_epoch, origin_connection_id, hop_index)
);

CREATE TABLE egress_replay_sources (
  origin_node_id TEXT NOT NULL,
  guard_count INTEGER NOT NULL CHECK(guard_count>=0),
  updated_at TEXT NOT NULL,
  PRIMARY KEY(origin_node_id)
);
