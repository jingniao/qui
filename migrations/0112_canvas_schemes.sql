-- The relay canvas keeps its drawings on the server instead of in one browser's
-- localStorage. A scheme is a typed graph of references (server:<nodeId>,
-- inbound:<inboundId>, import:<id>) plus the visual layout; layout_json is
-- validated by the proxy module before it is stored and never written by
-- another module. Saving or deleting a scheme touches no proxy object: the
-- objects a scheme generates are created by publishing it (U2c), which then
-- records published_at and published_batch_id. Both stay empty until then.
CREATE TABLE canvas_schemes(
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    execution TEXT NOT NULL CHECK(execution IN ('server','client')),
    layout_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    published_at TEXT NOT NULL DEFAULT '',
    published_batch_id TEXT NOT NULL DEFAULT ''
);
