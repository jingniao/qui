-- W05 first recovered root: retain the exact sealed protected current-root
-- record and its digest in the durable publication.
--
-- vault sealing is randomized, so re-sealing the recovered base at install time
-- produced different bytes on every attempt. After the base file had already
-- been renamed into place but the publication was still 'installing', a legal
-- retry compared the freshly sealed bytes against the existing file and failed
-- forever. Persisting the exact ciphertext generated once at approval lets an
-- interrupted install republish identical bytes and resume. Appended after 0093;
-- the 0092 table is not rewritten and the retained ca.crt/ca.key history is
-- untouched by this table.
ALTER TABLE node_ca_recovered_root ADD COLUMN base_sha256 TEXT NOT NULL DEFAULT '';
ALTER TABLE node_ca_recovered_root ADD COLUMN base_ciphertext BLOB;
