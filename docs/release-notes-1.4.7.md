# v1.4.7 发布说明

2026-11-09 发布。tag `v1.4.7` → `25b95e3`，迁移 0135–0137，迁移头 `0137`。功能版：保存节点组后自动下发凭据、节点客户端连接地址（自动探测＋域名）、TLS 入站证书来源，以及面板经 Cloudflare DNS-01 自动申请与续期证书。**生产尚未升级**（面板与三台 Agent 仍为 1.4.6，见文末）。

## 新增与改进

**保存节点组后凭据自动下发。** 保存或删除节点组（`POST/PUT/DELETE /api/v1/node-groups`）成功后，按该组 `user_node_group_grants` 里的用户调用自动额度版本（§22.1），约一个 60 秒合并窗口内只对访问面不一致的节点生成 `auto:` 修订。删除时在删除前读取授权。之前把已发布入站加入已授权的组，要手动发布节点，订阅里才会出现该端点。只改名称或备注不发版；未发布的入站仍须手动发布。

**节点客户端连接地址（迁移 0135）。**
- Agent 现在填写 `Heartbeat.public_ip`：取默认路由网卡上的第一个公网 IPv4，否则取第一个公网 IPv6。私网、链路本地、回环、100.64/10 与组播地址一律排除，不做外部查询。上报为空且库里为空时，面板改用 gRPC 对端地址，同样只收公网地址。判定共用 `pkg/contracts/netaddr`。
- `nodes.public_ip_manual` 区分手工与自动（`publicIpSource`）。Owner 填写的 IP 心跳永不覆盖，清空即恢复自动。迁移把既有非空 `public_ip` 回填为手工。
- 新增 `connectHost`（域名或 IP）作为客户端连接地址。订阅、订阅预览与分享链接优先用它，中转、隧道与画布继续用 `publicIp`。
- 两者都为空时，该节点不进订阅，但不再静默丢弃：订阅预览给出「节点「<名称>」未设置连接地址，订阅不包含它的入站」，页面也不再回退显示主机名或 `127.0.0.1`。

**TLS 入站证书来源（迁移 0136）。** `inbounds.tls_source` 取 `paste`／`self_signed`／`file`／`acme`，`''` 表示历史行。
- 自签：Subject/SAN 取节点连接地址；内部中转有效期 1 年，用户入站 10 年。`regenerateCertificate` 或切换来源时重新签发。
- 节点文件：只存 `/etc/proxy-agent/certs/` 下的两个绝对路径。Agent 在渲染时解析符号链接，并校验目录、可读性、证书与私钥配对和到期。密钥权限宽于 0640 只记 WARN。
  - 渲染用核心原生字段：sing-box 为 `certificate_path`/`key_path`，Xray 为 `certificateFile`/`keyFile`。
  - 由新能力位 `CoreCapability.tls_certificate_files_supported`（字段 12）／`Capabilities` 字段 25 门禁，旧 Agent 不能保存这种入站。
  - 安装器以 root:proxy-agent 0750 创建该目录。
- 读取接口新增只读 `certificateInfo`，证书不覆盖 ServerName 或连接地址时给出提示。

**证书库与 Cloudflare DNS-01（迁移 0137）。**
- 「系统设置 → 证书与 DNS」（Owner）保存 ACME 邮箱、CA 环境（production／staging）、Cloudflare API Token 与低峰发布窗口。Token 先经 Cloudflare 校验再 vault 密封，接口只回末 4 位。新依赖为 `golang.org/x/crypto/acme`（v0.54.0）。
- 面板单 worker 串行签发：写 `_acme-challenge` TXT → 直接查询权威 NS 等待生效（最长 180 秒）→ 用 ECDSA P-256 密钥完成签发 → 密封保存。无论成败都删除 TXT。可选把 A/AAAA 指向 `dnsNodeId` 节点的 `public_ip`（灰云）。
- 失败写固定错误码，按 1 小时 → 6 小时 → 24 小时退避，不回显上游内容。到期前 30 天开始续期；剩余不足 7 天仍失败时发 `system.alert`，每证书每 UTC 日最多一次。
- 入站以 `certificateId` 引用证书（`acme` 来源），不保存 PEM。快照读取证书库现值；证书缺失或尚未签发时快照失败。
- 续期后，证书发布器只在低峰窗口内（默认 3–6 点，面板时区）为「已发布证书与证书库不同且 DNS 名称集合相同」的节点生成 `auto: certificate <域名> renewed` 修订。Owner 把入站改指另一张证书属于未审阅编辑，不会被自动推送。同一节点连续 3 次失败后停止并告警。
- 证书库随加密备份、清单与校验保存。恢复时 `issuing` 改为 `interrupted`，恢复本身不触发申请。

接口、字段与边界见[项目说明书](../AGENTS.md) §10.3、§11.1、§12、§22、§27.4、§27.16、§28、§29.3、§31，以及[运维](operations.md)。

## 迁移与升级注意

- 0135 `node_connect_address`、0136 `inbound_tls_source`、0137 `certificates`，均为加法。面板降级回 1.4.6 须使用匹配的升级前备份（`qui-update --database`），1.4.6 不认迁移头 0137。
- 节点文件来源需要 Agent 1.4.7（新能力位）。`acme`／自签／粘贴来源下发的仍是普通 PEM，旧 Agent 可用。自动探测公网地址同样需要 Agent 1.4.7，旧 Agent 节点继续靠手工 IP 或 gRPC 对端地址。
- 面板自动申请证书需要出站 HTTPS 到 `api.cloudflare.com` 与 Let's Encrypt，以及 UDP 53 到区域权威 NS；不需要开放 80 端口。

## 验证

- 分层检查（提交前，Windows）：`bash scripts/quick-check.sh` 覆盖 66 个 Go 包 vet/test、web 类型检查/构建/自检（新增 `node-address-selftest.mjs`、`certificates-selftest.mjs` 并登记进 `scripts/check.sh`）、deploy 脚本与文本检查，均通过。因含迁移，该检查只给出「需要完整门禁」。
- 完整门禁（同一提交 `25b95e3`）：

| 门禁 | 结果 |
| --- | --- |
| 本地 Linux 测试机 `~/.qui-local/gate.sh 25b95e3`（scope `full`，2026-11-09T12:33:10Z–13:22:46Z） | `PART check EXIT 0 DURATION 2277s`、`PART cores EXIT 0 DURATION 698s`、`GATE 25b95e3 PASS total=2976s`，无 DATA RACE 告警 |
| GitHub `check`（`workflow_dispatch`，run `42627577140`，同一 SHA，13:40:45Z–14:48:22Z） | `check` 与 `cores` 两个任务均 `success` |

两条门禁这次是先后跑的，没有并行。

## 发布记录

- tag `v1.4.7`（附注 tag）→ `25b95e3`（`feat(v1.4.7): node group auto-deploy, node connect address, TLS sources and ACME DNS-01 certificates`），`main` 与工作分支同提交。
- `release` 任务（tag push，run `42639012134`，2026-11-09T14:53:01Z–14:54:33Z）成功，Release 于 14:54:28Z 发布（非草稿、非预发布）。附件：`proxy-control-1.4.7-linux-{amd64,arm64}.tar.gz`、`sing-box-1.14.4-qui.1-linux-{amd64,arm64}.tar.gz` 及各自 `.provenance.txt`、`SHA256SUMS`、`node-install.sh`、`panel-install.sh`。
- 提交说明勘误：提交信息里的字段名与源码不一致，以源码和说明书为准。客户端连接地址字段是 `connectHost`（不是 `connectAddress`）；证书来源取值是 `paste`／`self_signed`／`file`／`acme`（不是 `uploaded`／`node_path`）；公网地址只经 `Heartbeat.public_ip` 上报，`Hello` 没有新增字段。

## 生产状态

截至 2026-11-09T15:55Z **未升级**：面板主机 us2 上 `proxy-panel 1.4.6`、`proxy-agent 1.4.6`，uk-node `proxy-agent 1.4.6`；alpine-node 本次未能连上核对，上次记录为 1.4.6。最近恢复点仍是 v1.4.6 升级时的加密备份 `FlK52Fyj…` 与 `pre-update-1.4.5-20261109-123223.db`。生产升级须 Owner 同意，完成后在本节补记。

## 未覆盖／未在本版实现

- 实机验收待做：用 Let's Encrypt staging 完成一次申请、一次强制续期、一次失败退避；入站引用证书后，客户端不额外信任 CA 即可连上 Hysteria2 与 VLESS-TLS；续期后自动发布换证；备份恢复后证书与 Token 可用（[v1.4.7 规划](v1.4.7-plan.md) §4.3）。全新安装下自动得到公网 IPv4、只有私网地址时不填写（§2.3）。
- 规划中未在本版落地的部分：
  - 节点证书文件被外部工具续期后的自动重载或只重启单个核心。当前须在到期前续期并由下一次发布或重启核心生效。
  - 心跳上报证书摘要与到期前 14 天告警。
  - 不重启核心的「更新证书」Agent 操作（规划第二阶段）。
  - 签发状态的 SSE 推送（当前只有 `system.alert`）。
  - 连接地址变化时同步 DNS 记录。
  - 订阅预览逐条展示被省略的格式原因。
- 原生 arm64 主机验收仍按 Owner 2026-11-06 的决定跳过；Release 的 arm64 归档只由门禁构建。
