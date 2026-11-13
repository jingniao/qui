-- v1.5.4 item 5: node-native rule set files. A source of format 'native' is one
-- list in two encodings the node's cores read directly: url is the sing-box
-- binary rule set (.srs), xray_url/xray_tag name the Xray geosite/geoip data
-- file and the tag in it. The panel never fetches or expands such a source; the
-- Agent downloads the files, and only for nodes whose Agent reports
-- native_rule_sets_supported. Other nodes keep the expanded yaml/text sources.
CREATE TABLE service_set_sources_new (
    id TEXT PRIMARY KEY,
    service_id TEXT NOT NULL REFERENCES service_sets(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    format TEXT NOT NULL CHECK(format IN ('yaml','text','mrs','native')),
    behavior TEXT NOT NULL CHECK(behavior IN ('classical','domain','ipcidr')),
    interval_seconds INTEGER NOT NULL DEFAULT 86400 CHECK(interval_seconds BETWEEN 3600 AND 604800),
    position INTEGER NOT NULL,
    last_status TEXT NOT NULL DEFAULT 'pending' CHECK(last_status IN ('pending','ok','failed')),
    last_error_code TEXT NOT NULL DEFAULT '',
    last_fetch_at TEXT,
    last_success_at TEXT,
    entry_count INTEGER NOT NULL DEFAULT 0,
    skipped_count INTEGER NOT NULL DEFAULT 0,
    encrypted_cache BLOB,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    xray_url TEXT NOT NULL DEFAULT '',
    xray_tag TEXT NOT NULL DEFAULT '',
    CHECK(format <> 'mrs' OR behavior IN ('domain','ipcidr')),
    CHECK(format <> 'native' OR (behavior IN ('domain','ipcidr') AND xray_url <> '' AND xray_tag <> '' AND encrypted_cache IS NULL)),
    CHECK(format = 'native' OR (xray_url = '' AND xray_tag = ''))
);
INSERT INTO service_set_sources_new(id,service_id,url,format,behavior,interval_seconds,position,last_status,last_error_code,last_fetch_at,last_success_at,entry_count,skipped_count,encrypted_cache,created_at,updated_at)
SELECT id,service_id,url,format,behavior,interval_seconds,position,last_status,last_error_code,last_fetch_at,last_success_at,entry_count,skipped_count,encrypted_cache,created_at,updated_at FROM service_set_sources;
DROP TABLE service_set_sources;
ALTER TABLE service_set_sources_new RENAME TO service_set_sources;
CREATE INDEX service_set_sources_service ON service_set_sources(service_id, position);

-- The built-in 国内 set gains the full geosite cn and geoip cn lists as native
-- sources, after its existing sources. Skipped when the set is not the
-- built-in one or already has a native source.
INSERT INTO service_set_sources(id,service_id,url,format,behavior,interval_seconds,position,created_at,updated_at,xray_url,xray_tag)
SELECT 'builtin-service-cn-native-' || v.n, 'service-cn',
 'https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/' || v.kind || '/cn.srs',
 'native', v.behavior, 86400,
 (SELECT coalesce(max(position),-1) FROM service_set_sources WHERE service_id='service-cn') + v.n,
 strftime('%Y-%m-%dT%H:%M:%SZ','now'), strftime('%Y-%m-%dT%H:%M:%SZ','now'),
 'https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/' || v.kind || '.dat', 'cn'
FROM (SELECT 1 AS n, 'geosite' AS kind, 'domain' AS behavior UNION ALL SELECT 2, 'geoip', 'ipcidr') v
WHERE EXISTS (SELECT 1 FROM service_sets WHERE id='service-cn' AND builtin=1)
 AND NOT EXISTS (SELECT 1 FROM service_set_sources WHERE service_id='service-cn' AND format='native')
 AND NOT EXISTS (SELECT 1 FROM service_set_sources WHERE id='builtin-service-cn-native-' || v.n);

UPDATE service_sets SET description='内置：国内域名与国内 IP（meta-rules-dat geolocation-cn 与 geoip cn；节点 Agent 支持时改用完整的 geosite cn 与 geoip cn 原生规则集），用于屏蔽回国'
WHERE id='service-cn' AND builtin=1
 AND description='内置：国内域名与国内 IP（meta-rules-dat geolocation-cn 与 geoip cn），用于屏蔽回国';
