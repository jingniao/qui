-- v1.5.2 plan item 8: built-in service sets after the client presets' layout,
-- their MetaCubeX meta-rules-dat sources and a disabled 屏蔽回国 node policy.
-- Data only. A set whose ID or name already exists is skipped, so an Owner's
-- custom set of the same name stays as it is. Sources are added only to
-- built-in sets that have none yet, so sources an Owner added are never touched.

WITH v(id,name,description) AS (VALUES
 ('service-ads','广告拦截','内置：广告与追踪域名（meta-rules-dat category-ads-all）'),
 ('service-ai','AI 服务','内置：境外 AI 服务（meta-rules-dat category-ai-!cn、openai）'),
 ('service-microsoft','微软服务','内置：微软（meta-rules-dat microsoft）'),
 ('service-apple','苹果服务','内置：苹果与 Apple Music（meta-rules-dat apple、apple-music）'),
 ('service-social','社交媒体','内置：Facebook、Instagram、Twitter、TikTok、LinkedIn'),
 ('service-streaming','流媒体','内置：Netflix、Hulu、Disney+、HBO、Amazon、巴哈姆特、哔哩哔哩国际版'),
 ('service-games','游戏平台','内置：Steam、Epic、EA、Ubisoft、暴雪'),
 ('service-education','教育资源','内置：Coursera、edX、Udemy、可汗学院与境外学术站点'),
 ('service-finance','金融服务','内置：PayPal、Visa、Mastercard、Stripe、Wise'),
 ('service-cloud','云服务','内置：AWS、Azure、DigitalOcean、Heroku、Dropbox、Cloudflare、OneDrive'),
 ('service-spotify','Spotify','内置：Spotify'),
 ('service-pixiv','Pixiv','内置：Pixiv'),
 ('service-abema','Abema','内置：AbemaTV'),
 ('service-ehentai','E-Hentai','内置：E-Hentai'),
 ('service-gfw','代理服务','内置：常见被屏蔽站点（meta-rules-dat gfw）'),
 ('service-media','代理媒体','内置：境外媒体（meta-rules-dat category-media）'),
 ('service-cn','国内','内置：国内域名与国内 IP（meta-rules-dat geolocation-cn 与 geoip cn），用于屏蔽回国'),
 ('service-private','私有网络','内置：私有与保留地址（meta-rules-dat geoip private）')
)
INSERT INTO service_sets(id,name,builtin,enabled,description,created_at,updated_at)
SELECT v.id, v.name, 1, 1, v.description, strftime('%Y-%m-%dT%H:%M:%SZ','now'), strftime('%Y-%m-%dT%H:%M:%SZ','now')
FROM v
WHERE NOT EXISTS (SELECT 1 FROM service_sets s WHERE s.id=v.id OR s.name=v.name);

-- Built-in sets that will receive sources: built-in, no source yet. Decided
-- before any source is inserted.
CREATE TEMP TABLE m0142_targets AS
SELECT s.id FROM service_sets s
WHERE s.builtin=1 AND s.id IN ('service-netflix','service-disney','service-youtube','service-openai','service-google','service-github','service-telegram',
 'service-ads','service-ai','service-microsoft','service-apple','service-social','service-streaming','service-games','service-education','service-finance',
 'service-cloud','service-spotify','service-pixiv','service-abema','service-ehentai','service-gfw','service-media','service-cn','service-private')
 AND NOT EXISTS (SELECT 1 FROM service_set_sources src WHERE src.service_id=s.id);

WITH v(service_id,position,path,behavior) AS (VALUES
 ('service-netflix',0,'geosite/netflix','domain'),('service-netflix',1,'geoip/netflix','ipcidr'),
 ('service-disney',0,'geosite/disney','domain'),
 ('service-youtube',0,'geosite/youtube','domain'),
 ('service-openai',0,'geosite/openai','domain'),
 ('service-google',0,'geosite/google','domain'),('service-google',1,'geoip/google','ipcidr'),
 ('service-github',0,'geosite/github','domain'),('service-github',1,'geosite/gitlab','domain'),
 ('service-telegram',0,'geosite/telegram','domain'),('service-telegram',1,'geoip/telegram','ipcidr'),
 ('service-ads',0,'geosite/category-ads-all','domain'),
 ('service-ai',0,'geosite/category-ai-!cn','domain'),('service-ai',1,'geosite/openai','domain'),
 ('service-microsoft',0,'geosite/microsoft','domain'),
 ('service-apple',0,'geosite/apple','domain'),('service-apple',1,'geosite/apple-music','domain'),
 ('service-social',0,'geosite/facebook','domain'),('service-social',1,'geosite/instagram','domain'),('service-social',2,'geosite/twitter','domain'),
 ('service-social',3,'geosite/tiktok','domain'),('service-social',4,'geosite/linkedin','domain'),('service-social',5,'geoip/facebook','ipcidr'),
 ('service-social',6,'geoip/twitter','ipcidr'),
 ('service-streaming',0,'geosite/netflix','domain'),('service-streaming',1,'geosite/hulu','domain'),('service-streaming',2,'geosite/disney','domain'),
 ('service-streaming',3,'geosite/hbo','domain'),('service-streaming',4,'geosite/amazon','domain'),('service-streaming',5,'geosite/bahamut','domain'),
 ('service-streaming',6,'geosite/biliintl','domain'),('service-streaming',7,'geoip/netflix','ipcidr'),
 ('service-games',0,'geosite/steam','domain'),('service-games',1,'geosite/epicgames','domain'),('service-games',2,'geosite/ea','domain'),
 ('service-games',3,'geosite/ubisoft','domain'),('service-games',4,'geosite/blizzard','domain'),
 ('service-education',0,'geosite/coursera','domain'),('service-education',1,'geosite/edx','domain'),('service-education',2,'geosite/udemy','domain'),
 ('service-education',3,'geosite/khanacademy','domain'),('service-education',4,'geosite/category-scholar-!cn','domain'),
 ('service-finance',0,'geosite/paypal','domain'),('service-finance',1,'geosite/visa','domain'),('service-finance',2,'geosite/mastercard','domain'),
 ('service-finance',3,'geosite/stripe','domain'),('service-finance',4,'geosite/wise','domain'),
 ('service-cloud',0,'geosite/aws','domain'),('service-cloud',1,'geosite/azure','domain'),('service-cloud',2,'geosite/digitalocean','domain'),
 ('service-cloud',3,'geosite/heroku','domain'),('service-cloud',4,'geosite/dropbox','domain'),('service-cloud',5,'geosite/cloudflare','domain'),
 ('service-cloud',6,'geosite/onedrive','domain'),
 ('service-spotify',0,'geosite/spotify','domain'),
 ('service-pixiv',0,'geosite/pixiv','domain'),
 ('service-abema',0,'geosite/abema','domain'),
 ('service-ehentai',0,'geosite/ehentai','domain'),
 ('service-gfw',0,'geosite/gfw','domain'),
 ('service-media',0,'geosite/category-media','domain'),
 ('service-cn',0,'geosite/geolocation-cn','domain'),('service-cn',1,'geoip/cn','ipcidr'),
 ('service-private',0,'geoip/private','ipcidr')
)
INSERT INTO service_set_sources(id,service_id,url,format,behavior,interval_seconds,position,created_at,updated_at)
SELECT 'builtin-' || v.service_id || '-' || v.position, v.service_id,
 'https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/' || v.path || '.yaml',
 'yaml', v.behavior, 86400, v.position, strftime('%Y-%m-%dT%H:%M:%SZ','now'), strftime('%Y-%m-%dT%H:%M:%SZ','now')
FROM v JOIN m0142_targets t ON t.id=v.service_id;

UPDATE service_sets SET description='内置：手写基础条目＋meta-rules-dat 来源（被启用的节点策略引用时每天更新）'
WHERE id IN (SELECT id FROM m0142_targets)
 AND id IN ('service-netflix','service-disney','service-youtube','service-openai','service-google','service-github','service-telegram')
 AND description IN ('内置基础域名集合，变更后需配置发布','内置基础域名集合');

DROP TABLE m0142_targets;

-- The built-in 屏蔽回国 policy: disabled, every node, Chinese domains and IPs
-- → Block, priority 1. An ordinary policy the Owner may edit or delete.
INSERT INTO policies(id,name,priority,enabled,scope_json,match_json,action_json,logging_json,timezone,remark,created_at,updated_at)
SELECT 'policy-block-china','屏蔽回国',1,0,'{}','{"service_ids":["service-cn"]}','{"type":"Block","failover":"Block"}',
 '{"log_domain":true,"log_traffic":false,"alert_on_block":false}','UTC',
 '内置：启用并发布后，节点拒绝访问国内域名与国内 IP',strftime('%Y-%m-%dT%H:%M:%SZ','now'),strftime('%Y-%m-%dT%H:%M:%SZ','now')
WHERE NOT EXISTS (SELECT 1 FROM policies WHERE id='policy-block-china')
 AND EXISTS (SELECT 1 FROM service_sets WHERE id='service-cn' AND builtin=1);
