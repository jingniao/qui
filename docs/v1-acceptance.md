# V1 验收矩阵

更新时间：2026-10-21（v1.0.0 发布后的同步）。标准来自 [项目说明书](../AGENTS.md) 第 34 节的 25 条，以及第 33 节的测试要求与第 35 节的边界；本轮按轻量重构计划的决策 D1–D5 更新验收动作。

**当前总判定：V1 未完成。** 2026-10-22 在两台 Linux 主机上做了第一轮实机验收（结果见第 8 节）：大部分项通过，同时发现并修复了 10 个缺陷；修复尚未进入任何发布，三节点项（V01、A→B→C）与 arm64 因环境缺失跳过。同日第二轮接入第三台主机（Alpine LXC，第 9 节）：节点 Agent 的 OpenRC 一键安装、服务管理、崩溃恢复与强制出站已实机通过（9.4），V01 与 A→B→C 仍待面板主机重新登记出口节点。带 **[实机]** 的项必须在 owner 的 Linux VM 上执行；面板与节点两侧都要有真实进程、真实核心和真实流量。

## 1. 状态与证据规则

每条结果至少包含：要求编号、源码摘要、中央/Agent/核心版本、操作系统与架构、前置数据、操作步骤、预期、实际结果、退出状态、日志位置和产物摘要。

状态只用：通过、失败、未执行、因环境缺失而跳过、不适用（需说明范围依据）。**外部程序缺失导致的跳过不算通过。** 截图只能证明页面显示，不能单独证明服务端已持久化或流量已转发。证据中不得出现真实私钥、订阅 Token 或口令。

## 2. 执行环境与入口

| 环节 | 入口 |
| --- | --- |
| 发布包 | `VERSION=<v> bash scripts/build-release.sh`（两个架构 + `SHA256SUMS`） |
| 面板 | `deploy/linux/install.sh --panel ...`，配置 `deploy/examples/panel.yaml` |
| 节点 | `deploy/linux/install.sh --node ... [--core xray|sing-box ...]`，登记 `deploy/linux/enroll.sh --token-file FILE` |
| 官方核心 | Xray 26.3.27、sing-box 1.14.3 官方发布二进制（核心相关项必须使用） |
| 自动门禁 | `.github/workflows/check.yml` 的 `check` 作业（web 构建、前端时间测试、`go vet`、Agent fixture、`go test -race`、两个构建）与可选的 `cores` 作业（真实核测试）；`release` 作业只在 `refs/tags/v*` 上运行，拒绝非 `vMAJOR.MINOR.PATCH` 标签，构建两个架构的归档与 `SHA256SUMS` 并用 `gh release create` 发布（v1.0.0 即由它发布）。CI 通过不等于验收通过 |
| 单元/集成测试 | `go test -count=1 ./...`；设置 `XRAY_BINARY` / `SINGBOX_BINARY` 后真实核用例不再跳过 |

测试命令与参数见[部署与升级](deployment.md)，日常操作见[运维手册](operations.md)，能力范围见[协议支持矩阵](protocol-support-matrix.md)。

## 3. 原 25 条标准

| 编号 | 原要求 | 验收动作与通过条件 | 位置 |
| --- | --- | --- | --- |
| V01 | 至少管理三个节点 | 三个独立 Agent 注册在线；任一重连不影响其他节点；面板状态与真实进程一致 | [实机] |
| V02 | Agent 主动连接中央 | 节点主动建连；中央重启后按退避恢复；数据面不中断 | [实机] |
| V03 | 独立证书与 mTLS | 每节点独立私钥/证书；无效证书、其他 CA 的证书、被禁用节点、被吊销身份都不能连接；续期后恢复 | [实机] |
| V04 | Go 单体与 SQLite | 单一中央程序提供全部业务；SQLite 在本地磁盘，迁移、外键与 WAL 正常 | [实机] |
| V05 | 前端嵌入中央 | 无前端开发服务器时页面与路由可用，产物是当前前端 | [实机] |
| V06 | Owner 与 Viewer | 首次创建 Owner；Viewer 只能读许可范围；直接调用修改/敏感接口被拒 | [实机] |
| V07 | 用户与设备 | 创建、修改、禁用、到期、设备上限、凭据重置/撤销都作用到运行与订阅；额度按 D3 软执行 | [实机] |
| V08 | 用户授权多个节点组 | 一个用户可授权多个组，授权持久化且权限正确 | [实机] |
| V09 | 按节点组生成订阅 | 去重、用户/设备/协议/节点过滤后与已发布凭据一致；三种格式可用 | [实机] |
| V10 | 新节点入组无需重新授权 | 入组并发布成功后刷新订阅可见；移出或禁用后消失 | [实机] |
| V11 | 中转入口可隐藏 | relay/egress 节点与内部入站不出现在用户订阅，中转本身正常 | [实机] |
| V12 | 节点多入站 | 同节点多个协议真实连接；冲突端口在发布或应用时安全拒绝 | [实机] |
| V13 | 协议 A 入站/B 出站 | 入站协议与下一跳协议不同，TCP 与受支持的 UDP 转发和认证正确 | [实机] |
| V14 | A 经不同协议转 B | VLESS→A→Trojan→B 真实可用；节点间凭据与用户凭据隔离 | [实机] |
| V15 | A→B 与 A→B→C | 两/三节点路径可用；拒绝重复、环路、超跳数；失败按策略处理 | [实机] |
| V16 | Direct/Block/出站/链 | 每种动作实际生效；不支持的能力被拒绝；出口组见 D4 补充 | [实机] |
| V17 | 网站服务分流 | 内置与自定义服务集展开后实际命中指定出口；策略模拟不能单独作证 | [实机] |
| V18 | 域名阻断 | 精确/后缀/通配与作用域命中正确；连接被拒并记录阻断 | [实机] |
| V19 | 查看访问域名 | 真实访问形成分页查询；无法确定域名时显示目标 IP；权限与筛选正确 | [实机] |
| V20 | 不采集正文等内容 | 核对采集、队列、API、日志与归档，均无正文、路径、Cookie、口令或聊天内容 | [实机] |
| V21 | 发布前校验 | 类型、引用、端口、能力、链路与核心原生校验失败时不覆盖当前成功配置 | [实机] |
| V22 | 失败自动回滚 | 配置错误、端口占用、启动或健康失败都恢复上一成功配置并保留失败结果 | [实机] |
| V23 | 中央离线节点继续运行 | 停中央后原配置继续代理；Agent 侧到期保护仍执行；恢复后重连；时间窗/到期的延迟按 D3 补充记录 | [实机] |
| V24 | 断线缓存与补传 | 断线、重启、重复或丢失 ACK 后批量补传不重复入账；容量异常有明确保护 | [实机] |
| V25 | 中央备份与恢复 | 一致性加密备份、完整性验证、恢复用户/组/策略并与节点状态协调 | [实机] |

## 4. D1–D5 补充验收

| 决策 | 必须观察的结果 |
| --- | --- |
| D1 官方核 | 仓库内没有 `integrations/` 与核心构建脚本；节点运行官方发布二进制；缺失的统计/API 能力上报为不可用并降级运行，而不是拒绝启动 |
| D2 并发软限制 | Xray 显示去重客户端 IP 数（回环排除）；sing-box 显示未知；超限只产生一次告警和徽标，从不主动断开 |
| D3 额度 | 只有每用户一个额度；超限/到期/禁用后自动生成并发布只移除该用户凭据的版本，目标为该用户当前授权的节点加上已发布状态里仍持有其凭据的节点；提额/清零/续期后自动恢复；正常路径下超额窗口不超过一个上报周期＋60 秒合并窗口＋发布耗时，失败按窗口重试，连续 3 次失败后告警 Owner（`system.alert` ＋操作日志「自动额度发布失败」）；自动版本不带出未发布的 Owner 编辑（用户与入站的绑定取自当前草稿，可能被带出）；重启面板不产生发布风暴 |
| D4 上游缺失能力 | 每域名字节可为空并显示“—”；出口显示为“配置出口”并标注不代表实际到达；权重策略已移除（旧组按 `round_robin`）；UDP 行为按上游 |
| D5 sing-box 每用户 | sing-box 节点只报节点总量；每用户流量与在线数为未知；额度只统计 Xray 节点流量；用户页在授权 sing-box 节点组时给出说明 |

## 5. 计划完成后新增功能的实机项

以下功能在轻量重构计划结束（2026-10-17）之后加入：P01–P03 已包含在 v1.0.0 发布包中，P04（节点逻辑删除）在 v1.0.0 之后合入主分支、尚未进入任何发布；它们和原 25 条一样**没有任何实机证据**。全部 **[实机]**。

| 编号 | 功能 | 验收动作与通过条件 |
| --- | --- | --- |
| P01 | 节点出站网络 | **核心出站源**：在节点设置里填源 IP 或网卡并发布后，渲染结果出现 Xray `sendThrough` + `streamSettings.sockopt.interface`（或 sing-box `inet4/inet6_bind_address` + `bind_interface`），实际出站源地址就是所填值；目标是另一族的字面量 IP 时连接失败。**服务器强制出站**：填 IPv4/IPv6 并发布后 `nft list table inet proxy_agent_source` 出现规则，经该网卡新建的连接源地址被改写，核心出站源地址被豁免，其他网卡不被改写；只填一个地址族时不改写另一族。**失败路径**：源地址或网卡在节点上不存在（`network_unavailable`）、helper 未安装或执行失败（`host_network_failed`）时保留上一成功配置并重试；B 失败时发布回滚且规则还原；四个字段留空再发布后表被删除 |
| P02 | 终端菜单 `qui` 与升级工具 `qui-update` | `qui` 第 1 项改管理员口令后旧 Owner 会话全部失效、口令不进 argv；第 2 项改 Web 端口时 `--check-config` 失败会保留原配置（备份为 `config.yaml.bak`），成功重启后新端口可用；重启/启动/停止只作用于 `proxy-panel.service`。`qui-update` 从 GitHub Release 取归档与 `SHA256SUMS` 并核对摘要，面板升级前把数据库（含 `-wal`）备份到 `/var/lib/proxy-panel/backups/pre-update-<旧版本>-<时间戳>.db`，失败时服务回到停止前状态并打印降级命令；**迁移不可逆**，降级前必须用该备份恢复数据库 |
| P03 | 一键节点安装与证书信任 | 用面板显示的 `node-install.sh` 命令装出的 Agent 与核心版本与面板一致，登记后节点在线；面板证书由公网 CA 签发时信任系统 CA 包，自签时要求 `GET /install/ca.pem` 的 SHA-256 等于 `--ca-sha256`（不一致立即中止）；`GET /install/ca.pem` 无需登录返回 PEM、开发模式下返回 404；登记成功前可重跑同一条命令，已登记主机被拒绝 |
| P04 | 节点逻辑删除 | 有引用时 `DELETE /api/v1/nodes/{id}` 返回 409 `node_delete_blocked` 并列出 `blockers`，`GET /api/v1/nodes/{id}/deletion-preview` 给出同样的阻塞项；无引用时删除成功：证书立即吊销（旧证书的 mTLS 连接被拒）、节点从列表/选择器/订阅消失、`GET /api/v1/nodes?deleted=1` 可见、历史流量与访问记录保留、不能再次启用或重新登记；同一事务关闭该节点的待发布配置、核心/Agent 更新任务与排队健康检查；未登记节点走“取消登记”；删掉的服务器要作为新节点重新接入 |

这些项目与第 6 节的清单一起执行；缺少任何一项证据都不能判定 V1 完成。

## 6. 必须由 owner 在 Linux VM 执行的清单

以下项目**无法在 Windows 上完成**，是 V1 完成的必要条件：

1. 两个架构（amd64、arm64）的发布包安装、`systemctl` 启停、`uninstall.sh` 与 `--reinstall`。
2. 面板首启（含 Web TLS、一次性初始化令牌创建 Owner）与节点登记（`enroll.sh`）。
3. 原 25 条中所有带 **[实机]** 的数据面项：V13、V14、V15、V16、V17、V18，以及 V01、V02、V09–V12、V19、V23、V24。
4. 第 33.3 节的故障测试：中央离线、Agent 离线、下一跳离线、错误配置、端口占用、核心启动失败、本地缓存写满、节点证书过期、SQLite 锁等待、备份恢复。
5. 证书操作：自动续期、`renew-certificate.sh --renew`、`--rotate-key`、手工 CA 轮换（含节点以新记录重新登记）。
6. 备份与恢复：计划备份、校验、恢复预览、停机命令行恢复、恢复后节点重连。
7. 多节点验收：AGENTS 第 33.2 节的集成场景在真实多节点拓扑上跑一遍（轻量重构计划明确把这一步留给 owner，不在计划内声明完成）。
8. 第 5 节的 P01–P04：节点出站网络（含回滚）、`qui` 菜单与 `qui-update` 升级（含数据库备份）、`node-install.sh` 一键安装与证书指纹信任、节点逻辑删除。

## 7. 正式完成条件

- V01～V25 全部有最终版本证据，D1–D5 补充项全部观察通过，第 5 节的 P01–P04 全部通过。
- 第 33.3 节故障测试全部有结果，无未解决的数据正确性、越权、配置恢复、身份/额度问题。
- Linux 支持范围、核心能力与实际发布包一致，运行限制（D2/D4/D5）在文档中写明。
- 发布包有摘要、源码与依赖来源、迁移/升级路径和可重复的运维手册。
- 未执行测试或尚缺实机证据时，只能标为开发包或候选包，不能标为 V1 正式完成。

## 8. 实机验收记录（2026-10-22，第一轮）

### 8.1 环境与版本

| 项 | 值 |
| --- | --- |
| 面板主机 | Debian 13（trixie），内核 6.12，x86_64，2 vCPU / 2 GB；同机另装 Agent 作为第二个节点 `us-node` |
| 节点主机 | Debian 13（trixie），内核 6.12，x86_64，1 vCPU / 0.9 GB；节点 `uk-node` |
| 共存服务 | 两台主机都运行与本项目无关的 `xboard-node`、`cf-probe`、`fail2ban`、ufw；全程未受影响（每个阶段核对 `systemctl is-active`） |
| 源码 | `main` 3a61bbd 加第 8.4 节的修复（未提交时的工作区） |
| 发布包 | `VERSION=1.0.1-dev.3a61bbd bash scripts/build-release.sh`，amd64 归档 SHA256 `044828003667a091…`；修复后的程序以 `1.0.1-dev.3a61bbd.fixN` 单独构建并经 `install.sh` 原地升级（面板最终 fix7，Agent 最终 fix6） |
| 核心 | 官方 Xray 26.3.27（`install.sh` 按官方 `.dgst` 校验）；`us-node` 后续经面板核心更新切到 26.2.6 并验证往返；D5 阶段换装官方 sing-box 1.14.3（按 `node-install.sh` 固定摘要校验） |
| 面板证书 | 自签 Web CA + 含 IP SAN 的服务器证书；节点 `ca.crt` 为该 CA，`server_name` 为面板 IP |
| 客户端 | 面板主机上的 curl 与前台运行的官方 Xray 客户端；管理 API 通过 TLS 1.3 客户端走 owner 的 SOCKS 代理访问 |

证据里不含口令、订阅 Token、私钥或凭据明文；ID 与 IP 是测试环境的值。

### 8.2 原 25 条

| 编号 | 结果 | 实际观察 |
| --- | --- | --- |
| V01 | 第一轮跳过，第二轮见第 9 节 | 只有两台主机，最多两个节点；两个节点各自登记、在线，任一 Agent 重启不影响另一个 |
| V02 | 通过 | 停面板约 2 分钟后启动，两节点在约 90 秒内按退避重连；期间数据面不中断 |
| V03 | 通过 | 无客户端证书、自签伪造节点 ID、其他 CA（Web CA）签发的证书调用 `Connect` 均为 `Unauthenticated`；`renew-certificate.sh --renew` 换序列号不换密钥、`--rotate-key` 换序列号与密钥，旧身份被拒（`node certificate revoked or unknown`）；删除节点后其 Agent 被拒 |
| V04 | 通过 | 单一 `proxy-panel` 进程，SQLite 在本地 ext4，WAL 模式 |
| V05 | 通过 | 无前端开发服务器，`/` 与 `/nodes` 等前端路由由程序内嵌资源返回 |
| V06 | 通过 | 一次性令牌：错误令牌 403、正确令牌 201、令牌文件随即删除、重复初始化 409；新建管理员只能是 Viewer；Viewer 可读总览/节点/流量汇总，用户、设备、访问记录、入站、订阅、备份、操作日志、按用户筛选流量及全部写操作均 403；错误 CSRF 与跨站 Origin 被拒 |
| V07 | 通过 | 设备上限 2 时第 3 台 409；`maxDevices` 低于现有设备数 400；重置凭据并发布后旧的 SOCKS5/HTTP/VLESS/SS/Trojan 凭据全部失效、新凭据可用；禁用/到期/超额见 D3 |
| V08 | 通过 | 一个用户授权两个节点组并持久化 |
| V09 | 通过 | `uri`、`base64`（解码后与 uri 相同）、`sing-box` 三种格式均可用；错误 Token 403 |
| V10 | 通过 | 把 `us-node` 加入已授权组并发布后，原订阅出现其入站，用户授权不变；移出后消失 |
| V11 | 通过 | `relay` 组被强制 `subscriptionVisible=false`；内部中转入站从未出现在订阅里，中转本身可用 |
| V12 | 通过 | 同节点 SOCKS5/HTTP/VLESS/SS/Trojan 五个入站同时真实可用；重复端口在保存时 400；被其他进程占用的端口在应用时 `start_failed` 并回滚 |
| V13 | 通过 | SS 入站 → SOCKS5 中转出站 → `us-node`，出口为 `us-node` 地址 |
| V14 | 通过 | VLESS → `uk-node` → Trojan(TLS) → `us-node` → Direct，出口为 `us-node`；中转凭据由面板单独生成，与用户凭据不同 |
| V15 | 部分通过 | A→B（链 UK→US，VLESS 跳）真实可用；重复节点/环路与超过 3 跳被拒；A→B→C 因环境缺失而跳过 |
| V16 | 通过 | Direct、Block、指定 Outbound、出口组（primary 选中首成员）、代理链都实际生效；`weighted` 策略被拒 |
| V17 | 通过 | 内置 Netflix 服务 → 出口组、内置 GitHub → Direct、自定义服务 `ip-check` → 代理链，访问记录里命中的规则与出站正确 |
| V18 | 通过 | 精确 `example.com`、后缀 `tiktok.com`、通配 `*.example.net` 在 SOCKS5 与 VLESS 入站都被拒，并记录为阻断 |
| V19 | 通过 | 每次真实访问都形成按分钟聚合的记录；全部/阻断/按域名三个视图与节点筛选正确；认证失败的连接不产生记录 |
| V20 | 通过 | 经 HTTP 入站访问带路径与查询串的明文 URL 后，在两台主机的 Agent 数据目录、面板数据目录与全部 journald 日志中都找不到该路径和查询串；访问记录只有域名；Xray `access.log` 读后为 0 字节 |
| V21 | 通过 | 渲染失败（缺陷 4）与 `network_unavailable` 都不覆盖当前成功配置 |
| V22 | 通过 | 端口占用 → `start_failed` → 回滚到上一成功版本并保留失败结果；手动回滚到更早版本生效，再发布最新版本恢复 |
| V23 | 通过 | 停面板后节点继续代理（直连、中转、阻断均正常）；面板离线期间用户到期，Agent 在到期后 2 秒自行移除其访问；面板启动后只产生一个 `auto: startup reconcile` 版本 |
| V24 | 通过 | 面板离线期间产生的流量（约 800 KB）与访问记录在重连后补传且只入账一次；重启 Agent 后用量与记录数均 +0。本地缓存写满未测（见 8.5） |
| V25 | 通过（修复后） | 修复缺陷 7 前任何备份都失败。修复后：加密备份（文件内无明文）、正确/错误口令校验、恢复预览、在线恢复（面板自动重启、会话全部失效、节点重新连接、数据面正常）。备份后新建的用户在恢复后被**禁用而非删除**，属有意的保守合并（见 8.4 文档项） |

### 8.3 D1–D5 与第 5 节新增项

| 项 | 结果 | 实际观察 |
| --- | --- | --- |
| D1 | 通过 | 节点只运行官方 Xray 26.3.27/26.2.6 与 sing-box 1.14.3；面板核心更新（修复缺陷 8、9、10 后）完成 26.3.27→26.2.6 与 26.2.6→26.3.27 往返，错误摘要被拒且核心不变 |
| D2 | 通过 | `maxConnections=1` 时两个客户端 IP 同时下载：在线数 2、`overLimit=true`、SSE 只发出一次 `system.alert`（`connection_limit`），两次下载都完整结束，未被断开 |
| D3 | 通过 | 超额：流量开始后 85 秒两个节点（含已移出授权但仍持有凭据的节点）的自动版本部署完成并拒绝该用户；提额、禁用、启用、到期、续期都自动发布对应版本并生效；面板重启无发布风暴。另见 8.4 观察项（设置未来到期时间也会触发一次 `renewed` 版本） |
| D4 | 通过 | 每域名字节为 `null`、`traffic_known=false`；中转与链的记录 `egress_known=false` 且只记配置出站；`weighted` 被拒 |
| D5 | 通过 | 经 sing-box 节点下载 700 KB 后用户用量 +0；节点心跳上报节点总量（上行约 30 KB、下行约 776 KB）；用户页对授权了 sing-box 节点组的用户显示说明 |
| P01 | 部分通过 | 核心出站源 IP（`us-node` 的第二个 IPv6）生效，经中转的出口就是该地址，Xray 与 sing-box 都成立；源 IP 不存在 → `network_unavailable` 并保留上一成功配置；强制出站 IPv6 不存在 → `host_network_failed` 回滚、未建 nft 表、helper 写出原因；清空字段后无 nft 表。**服务器强制出站改写本身未测**：它会改写同机 `xboard-node` 的流量 |
| P02 | 部分通过 | `qui` 第 1 项改口令后旧 Owner 会话失效、旧口令 401、新口令 200；第 2 项非法端口拒绝、改为 8443 生成 `config.yaml.bak` 并重启生效、再改回 443；`qui-update` 指定版本与取最新版本都下载并核对了 `SHA256SUMS`，确认时选择“否”后不做修改。真实升级与升级前数据库备份未执行（会把修复后的构建降级为 v1.0.0） |
| P03 | 部分通过 | `GET /install/ca.pem` 无需登录返回 CA，其 SHA-256 等于面板给出的 `caSha256`；开发版本号下对话框不给一键命令。`node-install.sh` 一键安装本身未测（需要包含修复的正式发布） |
| P04 | 通过 | 未登记节点“取消登记”204；被引用时 409 `node_delete_blocked`，阻塞项与预览一致（4 入站、3 中转出站、1 链）；清理引用后删除 204：列表消失、`?deleted=1` 可见、退出所有组、再次启用 409 `node_deleted`、访问记录与版本保留、旧证书连接被拒。远端进程仍在运行，符合文档说明 |

### 8.4 发现的缺陷与修复（均有回归测试）

| # | 缺陷 | 影响 | 修复 |
| --- | --- | --- | --- |
| 1 | `install.sh`/`uninstall.sh` 对 host-network 单元用绝对路径调用 `systemctl start`/`disable --now`，systemd 把它当作挂载单元 | v1.0.0 起**所有 Linux 节点安装失败**（退出码 5）；卸载时 path 单元不停止 | 改用单元名 |
| 2 | `enroll.sh` 把临时 Token 放进 `/var/lib/proxy-agent` | Agent 拒绝含未知文件的登记目录，**登记永远失败** | 临时 Token 放到 `/run` 下的私有目录 |
| 3 | 登记目录检查不认识 `install.sh` 创建的 `host-network/` | 同上，全新节点无法登记 | 允许该目录（必须是真实目录） |
| 4 | 同一设备的 HTTP 与 SOCKS5 凭据共用用户名，Xray 统计名冲突即报错 | 任一设备同时有 HTTP 与 SOCKS5 时**整个节点配置渲染失败** | 同一用户同一设备的该别名合并计数 |
| 5 | 首个版本失败后留下 `revisions/` 而没有 `current.json` | Agent **启动失败并循环重启** | 首次写版本目录前先提交空的当前清单 |
| 6 | Agent 自己的 TCP 健康检查被 Shadowsocks 记为无目的地拒绝 | 含 SS 入站的节点采集状态长期为 `observation_loss`，每 30 秒告警 | 忽略回环来源的无目的地拒绝 |
| 7 | 备份把可选的 `tls.client_ca` 当必需项（创建、检查、恢复三处） | 默认配置的生产面板**任何备份都失败**（含计划备份） | `client_ca` 仅在配置时归档与要求 |
| 8 | 核心更新只打开、不创建发布历史；历史只由签名目录工具创建 | 默认安装的节点**永远无法通过面板更新核心**（`history_invalid`） | 首次核心更新时创建历史，并以当前核心为回滚基线 |
| 9 | 解出的核心以 0600 写入暂存目录 | Linux 上预检无法执行（`preflight_failed`）；Windows 测试不暴露 | 暂存文件 0700，并在真实 Linux 上跑了修复前后的测试 |
| 10 | 面板在 gRPC 消息校验里用 Agent 更新的解码器校验核心更新结果 | 成功结果被拒、控制流断开，任务被记为 `failed_closed` 而核心实际已切换 | 改用核心更新解码器 |

观察项（未修改）：
- 面板不自动填写节点 `publicIp`，订阅要求手工填写。
- Agent 的渲染失败原因只写在节点的 `revisions/<n>/result.json`，面板只显示失败码；面板备份失败的日志也没有原因。
- 已激活用户设置一个未来的到期时间会立刻产生一次 `auto: … renewed` 版本，重启该节点核心。
- 在线恢复会禁用（不删除）备份之后创建的用户；`operations.md` 第 2 节未写明，预览字段 `usersRemoved` 实际表示“将被禁用”。
- `uninstall.sh` 的提示仍提到已删除的 “quota ledgers / reserved allowances”；`install.sh --reinstall` 仍提示去登记已登记的节点。
- sing-box 适配器的 `TrafficSourceStatus` 注释仍写着节点总量读取器“尚未实现”，实际已实现并在心跳中上报。
- `/var/lib/proxy-panel` 与 `/var/lib/proxy-agent` 被 systemd `StateDirectory` 设为 0755（内容仍为 0600/0700），与 `install.sh` 和文档写的 0700 不一致。
- Windows 上构建的发布包里两个程序是 0644（`install.sh` 安装时会设 0755）；正式发布由 CI 在 Linux 上构建。

### 8.5 未执行

- V01、V15 的 A→B→C：只有两台主机（第二轮补测见第 9 节）。
- arm64 发布包：两台主机都是 amd64。
- 第 33.3 节的本地缓存写满、节点证书过期、SQLite 锁等待：未在实机构造。
- P01 服务器强制出站改写、P02 真实升级与升级前备份、P03 一键安装：原因见 8.3。
- 计划备份与命令行（停机）恢复、手工 CA 轮换。

## 9. 实机验收记录（2026-10-22，第二轮：第三台主机）

### 9.1 环境

| 项 | 值 |
| --- | --- |
| 第三台主机 | Alpine Linux 3.19（LXC 容器，OpenRC，无 systemd），x86_64，1 vCPU / 328 MB；私网地址 10.111.111.41，NAT 后只转发了 SSH，外部无法访问其他端口；出站可达面板 443/9443、节点主机与 GitHub |
| 共存服务 | `komari-agent`、`cf-probe`、`xboard-node`（测试前已是 `crashed`）；全程未改动，每步核对 `rc-status` |
| 节点 | `nat-node`：Agent `1.0.1-dev.3a61bbd.fix6`，官方 Xray 26.3.27（在主机上下载 `Xray-linux-64.zip`，SHA-256 与官方 `.dgst` 一致，二进制摘要与 `uk-node` 相同） |
| 安装方式 | **不是受支持的安装路径**：`install.sh`/`enroll.sh` 需要 systemd 与 `runuser`，在该主机上无法运行。按 `install.sh` 的布局手工放置程序、核心、`config.yaml`（0600，proxy-agent 所有）、CA 与 0700 数据目录（含 `host-network/`），以 `proxy-agent` 账户执行 `--enroll`，再用仅供验收的 OpenRC 服务（`supervise-daemon`，以 `proxy-agent` 运行，umask 077，日志写 `/var/log/proxy-agent/agent.log`）启动。BusyBox 的 `setpriv` 不支持 `--reuid`，改用 `su -s /bin/sh proxy-agent` |

### 9.2 结果

| 项 | 结果 | 实际观察 |
| --- | --- | --- |
| 登记与 mTLS | 通过 | 新节点 `nat-node` 一次性令牌登记成功，令牌文件随即删除；Agent 以 mTLS 连接 9443 并上报能力（http/socks5/shadowsocks/vless/trojan/wireguard），面板显示 `online`、`linux/amd64` 与 Agent 版本 |
| 发布顺序 | 通过 | 先发布中转源 `nat-node` 时面板返回 409 `dependency_not_ready`（`internal_credential_missing`）；先发布目标 `uk-node`（版本 30）再发布 `nat-node`（版本 31）成功 |
| 数据面 | 通过 | 回环 SOCKS5 入站（127.0.0.1:35201）用设备凭据直连出口为第三台主机地址；错误凭据被拒 |
| A→B（新主机） | 通过 | `nat-node` 的分流规则把 `api.ipify.org` 经 Shadowsocks 中转出站送到 `uk-node` 的内部中转入站，出口为 `uk-node` 地址；其他域名仍直连。访问记录按分钟聚合，命中规则与动作正确 |
| 订阅 | 通过 | `nat-node` 加入已授权的 `UK entry` 组后用户授权不变；其入站只监听回环，订阅按设计不包含它 |
| 重连隔离 | 通过 | 重启 `nat-node` 的 Agent 后约 15 秒内重连，数据面恢复；`uk-node` 心跳不受影响 |
| V01 | 未完成 | 面板上 `uk-node`、`nat-node` 在线；第三个节点 `us2-node`（面板主机）已在面板创建，待在面板主机上重新登记（见 9.3） |
| V15 A→B→C | 未执行 | 需要 `us2-node` 作为出口 C（见 9.3） |

以上是手工安装的 `nat-node` 的结果。该节点已在 9.4 中删除，第三台主机改用受支持的 OpenRC 安装重新登记为 `alpine-node`。

### 9.3 待处理

- 面板主机上仍运行第一轮 P04 中已删除的 `us-node` 的 Agent（被面板拒绝，不承载流量）。要把该主机登记为新节点 `us2-node`，需要停止这个 Agent 并把它的数据目录移到备份位置。该步骤等待 owner 确认，确认后再完成 V01 与 A→B→C，计划改为 `alpine-node` → `uk-node` → `us2-node` → Direct。

观察项：
- ~~受支持的安装与登记脚本只适用于 systemd 主机；Alpine/OpenRC 等环境没有安装路径，文档也未说明这一前提。~~ 已解决：节点 Agent 正式支持 OpenRC，见 9.4。

### 9.4 Alpine / OpenRC 正式支持复测（2026-10-22）

本轮使用工作区代码的本地构建，不是发布版：`VERSION=1.0.1 bash scripts/build-release.sh`，`proxy-control-1.0.1-linux-amd64.tar.gz` 的 SHA-256 为 `56433e30…`。测试之后工作区只改了一处：公网证书探测的 curl 由 `-fsS` 改为 `-fs`，回退到固定摘要时不再打印 curl 错误。改动内容：

- `deploy/linux/service.sh`：初始化系统识别与服务命令库。
- `deploy/openrc/`：Agent 与强制出站 watcher 的 init 脚本，以及 `host-network-watch`。
- 各安装脚本支持 OpenRC，`node-install.sh` 新增离线参数 `--bundle/--sums`。
- 代理核心以 `Pdeathsig` 启动。
- 面板一键命令前加 Alpine 依赖安装。

**测试前准备（第三台主机）。**
- 在面板删除 `nat-node`：先删除它的分流规则、出站、入站，并移出 `UK entry` 组。
- 在主机上停止并移除手工安装：`rc-update del` 移除服务，程序、配置与数据移到 `/root/qui-accept/manual-install.old/`，删除手工建的 `proxy-agent` 账号。
- 在面板新建节点 `alpine-node`。
- 共存服务 `komari-agent`、`cf-probe`、`crond` 全程保持 `started`，`xboard-node` 保持测试前的 `crashed`。

| 项 | 结果 | 实际观察 |
| --- | --- | --- |
| 一键安装（离线归档） | 通过 | 执行面板命令同款的一行：`command -v apk … && apk add …; cat node-install.sh \| bash -s -- … --bundle … --sums …`，结果如下：<br>• 归档按 `SHA256SUMS` 校验，apk 补装 `findutils nftables runuser shadow` 等依赖。<br>• 创建 `/etc/machine-id`。<br>• 安装 Agent 与官方 Xray 26.3.27（`.dgst` 摘要核对）。<br>• `/etc/init.d/proxy-agent`、`/etc/init.d/proxy-agent-host-network` 加入 `default` 运行级别。<br>• 面板证书按固定 SHA-256 建立信任，一次性注册码登记，`proxy-agent` 启动，状态 `started`。<br>• 安装记录第 10 行为 `openrc`。<br>这台主机原本就装有 bash 与 curl，所以开头的 apk 一段是空操作，原版 Alpine 上的 bash/curl 引导没有实测。 |
| 登记与 mTLS | 通过 | 面板显示 `alpine-node` 为 `online`，Agent `1.0.1`，`linux/amd64`。 |
| 进程与权限 | 通过 | `supervise-daemon`（root）→ `proxy-agent`（uid 122）→ `xray run`。Agent 的 `CapEff`/`CapAmb` 只有 `cap_net_bind_service`（`0x400`），watcher 以 `proxy-agent` 运行并带 `cap_net_admin`。 |
| 低端口与数据面 | 通过 | 入站 `127.0.0.1:990`（SOCKS5，版本 33）由非 root 的 Xray 监听。设备凭据经该入站出口为 113.29.231.218，错误凭据被拒。 |
| 服务管理 | 通过 | `rc-service proxy-agent status/stop/start/restart`：`stop` 后状态为 `stopped`，990 端口释放；`start`、`restart` 后为 `started`，990 端口恢复监听。 |
| 崩溃恢复 | 通过 | 向 Agent 发 `SIGKILL` 后，Xray 随之退出（`Pdeathsig`）。`supervise-daemon` 5 秒后重启 Agent（syslog：`terminated by signal 9`），重连面板，新核心正常启动，数据面恢复。没有出现孤儿核心或 `ErrCoreStopUnconfirmed`。 |
| 日志 | 通过 | 两个服务的输出经 `logger` 进入 `/var/log/messages`，标签分别为 `proxy-agent`、`proxy-agent-host-network`。 |
| 强制出站源地址 | 通过 | 设置 `hostSourceIpv4=10.111.111.41` 并发布（版本 34，成功）：watcher 触发 helper，`status.json` 为 `ok`，生成表 `inet proxy_agent_source`（`oifname "eth0" ip saddr != 10.111.111.41 snat ip to 10.111.111.41`）。<br>手工删除该表再重启 watcher 服务后，表重新生成（开机重放）。<br>清空该字段并发布（版本 35）后表被删除。 |
| 卸载与重装 | 通过 | `uninstall.sh agent` 停止并移出两个服务，删除 init 脚本、watcher、程序、核心、服务命令库与 nft 表，保留身份、配置与数据。<br>`install.sh --node --reinstall … --core xray` 后用 `rc-update add` + `rc-service start` 启动，节点用原身份恢复 `online`，数据面恢复。 |
| systemd 回归（`uk-node`，Debian 13） | 通过 | 用同一归档原地升级：停止 Agent、`install.sh --node`、启动。三个 systemd 单元的摘要与升级前逐字节相同，安装记录仍是 9 行（没有第 10 行），`/etc/machine-id` 未改动，`proxy-agent` 与 `.path` 单元都是 `active`，节点重新 `online`。<br>`service.sh` 在该主机上识别为 `systemd`，日志提示为 `journalctl -u proxy-agent -n 50`。 |

复测中发现并修复的问题：
1. **安装锁被 watcher 继承。** `install.sh` 在持有 fd 9 上的安装锁时用 `rc-service` 启动 watcher，`supervise-daemon` 继承了该描述符并一直持有锁，随后的 `enroll.sh` 报 “Another installation operation is running”。已修复：`service.sh` 在 OpenRC 上启动或重启服务时关闭 fd 9（systemd 由 PID 1 启动服务，不受影响）。
2. **curl 包装脚本跳过证书校验，导致误判公网证书。** 该主机的 `/usr/bin/curl` 是 `exec /usr/bin/curl.orig -k "$@"`，会跳过所有证书校验。`node-install.sh` 因此把自签面板证书误判为公网证书，安装了系统 CA 包，登记时 gRPC 握手失败。已修复：
   - 所有需要校验证书的 curl 调用都加 `--no-insecure`（它覆盖前面的 `-k`）。
   - 公网证书判定改为用将要安装的那个 CA 包（`--cacert`）去验证。
   - 面板一键命令同样带 `--no-insecure`。
   - 该包装脚本属于主机本身，未改动。
3. **重跑登记被拒。** 失败的登记留下 `enrollment-attempt.json` 与私钥，Agent 拒绝把它们换绑到新注册码，所以“登记成功前可以重跑”实际不可行（systemd 主机同样如此）。已修复：`node-install.sh` 在本机没有证书、也没有 `enrollment-result.json` 时，把这两个文件移到 `/var/lib/proxy-agent-enrollment-retired.*`，不删除，再登记。
4. **`node-ca.sh` 只支持 systemd。** 它直接调用 `systemctl show`，已改为通过 `service.sh` 读取服务状态。

未覆盖：
- 原版 Alpine（没有 bash/curl）上的引导段。
- arm64。
- 真正重启容器。只用重启 watcher 服务模拟开机重放，没有实际重启 LXC。
- 通过 GitHub 发布下载 `node-install.sh`（需要正式发布）。
- 主机上的 `qui-update` 只验证了 `--help`；真实更新会降级到 GitHub 上的 v1.0.0，所以没有执行。
