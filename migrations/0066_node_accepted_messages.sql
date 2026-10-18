CREATE TABLE node_accepted_messages (
 node_id TEXT PRIMARY KEY REFERENCES nodes(id),
 sequence INTEGER NOT NULL CHECK(sequence>0),
 digest TEXT NOT NULL
);
