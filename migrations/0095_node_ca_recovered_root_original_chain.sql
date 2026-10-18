-- W05 first recovered root: bind the durable publication to the exact retained
-- original chain, not only the initial root fingerprint.
--
-- The publication previously named only the original expired ca.crt fingerprint
-- (O). A recovered root must be authorized against the exact original chain it
-- replaced, so the history head fingerprint (S) and a digest over the complete
-- original material set (configuration key, ca.crt and the ordinary
-- active/retired/successor records) are retained and re-derived by every
-- consumer. Legacy rows written before this column pair keep both values empty
-- and stay readable (explicit compatibility); a fresh same-request approval
-- supplies them without changing approved_at. Appended after 0094; the 0092
-- table and the retained ca.crt/ca.key history are untouched.
ALTER TABLE node_ca_recovered_root ADD COLUMN original_head_fingerprint TEXT NOT NULL DEFAULT '';
ALTER TABLE node_ca_recovered_root ADD COLUMN original_materials_digest TEXT NOT NULL DEFAULT '';
