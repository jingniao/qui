CREATE TABLE canvas_entry_bindings (
 id TEXT PRIMARY KEY,
 scheme_id TEXT REFERENCES canvas_schemes(id) ON DELETE SET NULL,
 instance_id TEXT NOT NULL,
 inbound_id TEXT NOT NULL UNIQUE REFERENCES inbounds(id) ON DELETE RESTRICT,
 mode TEXT NOT NULL CHECK(mode IN ('create','reuse')),
 original_default_outbound TEXT NOT NULL DEFAULT '' CHECK(mode <> 'reuse' OR original_default_outbound <> ''),
 applied_default_outbound TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL,
 request_id TEXT UNIQUE,
 updated_at TEXT NOT NULL,
 UNIQUE(scheme_id,instance_id)
);
CREATE TRIGGER canvas_entry_binding_detached AFTER UPDATE OF scheme_id ON canvas_entry_bindings
WHEN OLD.scheme_id IS NOT NULL AND NEW.scheme_id IS NULL
BEGIN
 UPDATE canvas_entry_bindings SET state='detached' WHERE id=NEW.id;
END;
