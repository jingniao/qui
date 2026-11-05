-- Only a successful rule publication may advance the advertised relay endpoint.
-- Existing publications have no trustworthy historical address snapshot and
-- become available to subscriptions after their next successful publication.
ALTER TABLE tunnel_rules ADD COLUMN published_snapshot TEXT;
