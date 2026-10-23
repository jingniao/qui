# V1 验收矩阵

更新时间：2026-10-23（第五轮：qui-update 中断后的自动恢复与面板跨迁移降级，9.8）。标准来自 [项目说明书](../AGENTS.md) 第 34 节的 25 条，以及第 33 节的测试要求与第 35 节的边界；本轮按轻量重构计划的决策 D1–D5 更新验收动作。

**当前总判定：V1 未完成。** 2026-10-22 在两台 Linux 主机上做了第一轮实机验收（结果见第 8 节）：大部分项通过，同时发现并修复了 10 个缺陷，三节点项（V01、A→B→C）与 arm64 因环境缺失跳过。同日第二轮接入第三台主机（Alpine LXC，第 9 节），节点 Agent 的 OpenRC 安装、服务管理、崩溃恢复与强制出站实机通过（9.4）。2026-10-23 两轮的修复作为 **v1.0.1** 发布，第三轮（9.5）用发布版完成了 V01（三节点）、A→B→C、原版 Alpine 引导、从 GitHub 一键安装、真实 `qui-update` 与真实 LXC 重启。第四轮（9.6）修复并实测了一键安装与 `qui-update` 的失败处理和自动回滚，作为 **v1.0.2** 发布后在 systemd 与 OpenRC 上复测了从 GitHub 安装与更新（9.7）。arm64 仍未验证，其余 [实机] 项以第 8 节为准。带 **[实机]** 的项必须在 owner 的 Linux VM 上执行；面板与节点两侧都要有真实进程、真实核心和真实流量。

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
| V01 | 第一轮跳过，第三轮通过（9.5） | 只有两台主机，最多两个节点；两个节点各自登记、在线，任一 Agent 重启不影响另一个 |
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
| V15 | 通过（A→B→C 见 9.5） | A→B（链 UK→US，VLESS 跳）真实可用；重复节点/环路与超过 3 跳被拒；A→B→C 第一轮因环境缺失跳过，第三轮通过 |
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
| V01 | 第三轮通过（9.5） | 本轮时 `uk-node`、`nat-node` 在线，第三个节点待登记 |
| V15 A→B→C | 第三轮通过（9.5） | 本轮缺少出口 C |

以上是手工安装的 `nat-node` 的结果。该节点已在 9.4 中删除，第三台主机改用受支持的 OpenRC 安装重新登记为 `alpine-node`。

### 9.3 待处理

- ~~面板主机上仍运行第一轮 P04 中已删除的 `us-node` 的 Agent（被面板拒绝，不承载流量）。要把该主机登记为新节点 `us2-node`，需要停止这个 Agent 并把它的数据目录移到备份位置。~~ 已处理：owner 于 2026-10-23 确认，旧 Agent 的配置与数据移到 `/root/qui-accept/us-node.old/`，主机重新登记为 `us2-node`，V01 与 A→B→C 见 9.5。

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

未覆盖（除 arm64 外均已在 9.5 用 v1.0.1 补测）：
- ~~原版 Alpine（没有 bash/curl）上的引导段。~~ 见 9.5。
- arm64。
- ~~真正重启容器。只用重启 watcher 服务模拟开机重放，没有实际重启 LXC。~~ 见 9.5。
- ~~通过 GitHub 发布下载 `node-install.sh`（需要正式发布）。~~ 见 9.5。
- ~~主机上的 `qui-update` 只验证了 `--help`；真实更新会降级到 GitHub 上的 v1.0.0，所以没有执行。~~ 见 9.5。

### 9.5 第三轮：v1.0.1 发布版（2026-10-23）

**发布。** 两轮修复提交为 `0cd70c3`（第一轮缺陷修复）与 `68688dd`（Alpine/OpenRC 支持），推送到 `main` 后 CI `check` 通过；标签 `v1.0.1` 的 CI 再次通过 `check` 并由 `release` 任务发布 GitHub Release `v1.0.1`。附件为两个架构的归档、`SHA256SUMS` 与 `node-install.sh`；`proxy-control-1.0.1-linux-amd64.tar.gz` 的 SHA-256 为 `a1c235b4…727d`。发布前本地门禁：`bash -n`/`sh -n` 全部脚本、`gofmt`、`go vet`（Windows 与 `GOOS=linux`）、`go test -p 1 ./...` 无失败、Web 构建与前端时间测试通过。

**环境。** 三台主机同 9.1 与第 8 节：面板主机（Debian 13，systemd，23.254.158.3）、`uk-node`（Debian 13，systemd，195.72.166.247）、第三台主机（Alpine 3.19 LXC，OpenRC，NAT 出口 113.29.231.218）。本轮结束时三个节点 `alpine-node`、`uk-node`、`us2-node` 都运行发布版 Agent `1.0.1` 与官方 Xray 26.3.27。

| 项 | 结果 | 实际观察 |
| --- | --- | --- |
| 真实 `qui-update`：面板主机（面板＋Agent） | 通过 | 用主机上已有的旧版 `qui-update`（发布前的 systemd 版，没有 `service.sh`）执行 `qui-update --yes`：查询到最新发布 `v1.0.1`，归档摘要与 `SHA256SUMS` 一致；先停止并更新 Agent，再停止面板、在停止状态下把数据库复制为 `/var/lib/proxy-panel/backups/pre-update-1.0.1-dev.3a61bbd.fix7-20261023-082249.db`（0600，proxy-panel 所有）、更新面板；两个服务重新 `active`，`--version` 均为 `1.0.1`，并写入了 `/usr/local/lib/proxycontrol/service.sh` 与新版 `qui-update`。面板重启后 `uk-node`、`alpine-node` 自动重连。备份文件头为 SQLite 3；主机上没有 `sqlite3`，未做完整性检查。 |
| `qui-update` 失败路径：`uk-node` | 通过 | 用 `chattr +i` 锁住 `/usr/local/bin/proxy-agent` 制造安装失败：`install.sh` 在替换程序时 `mv` 失败，脚本输出“更新失败，程序未被替换”，重新启动 Agent（`active`），给出日志与降级提示，退出码 1；程序版本仍是 `1.0.1-dev.3a61bbd.fix6`，三个 systemd 单元摘要不变，安装记录仍完整。解除锁定后重跑 `qui-update --yes` 成功：Agent `1.0.1`、单元摘要与升级前相同、记录 9 行，节点重新 `online`。版本降级（`--version` 旧版本）未测试：v1.0.1 含迁移 `0109`，面板不可直接降级，节点降级没有实际需要。 |
| `qui-update`：`alpine-node`（OpenRC） | 部分通过 | 新版 `qui-update` 在 OpenRC 上识别初始化系统、查询 GitHub 并正确报告“已是最新版本 1.0.1”。没有更早的支持 OpenRC 的发布，因此 OpenRC 上带版本变化的真实升级无法测试，要等下一个发布。随后按 `qui-update` 的同样步骤（`rc-service proxy-agent stop` → 发布归档的 `install.sh --node` → `start`）把本地构建换成发布版二进制，记录第 10 行仍为 `openrc`。 |
| 原版 Alpine 引导 ＋ 从 GitHub 一键安装（OpenRC） | 通过（容器为模拟） | 环境：在面板主机上把官方 `alpine-minirootfs-3.19.9-x86_64.tar.gz`（SHA-256 与官方 `.sha256` 一致）解包，`apk add alpine-base`（与 Alpine LXC 镜像相同：busybox、apk、OpenRC 0.52，**没有 bash、curl，没有 `/etc/machine-id`**），在独立的 pid/mount/uts/ipc 命名空间里以 busybox `init` 为 PID 1 启动，`openrc sysinit/boot/default` 正常执行（`rc_sys="lxc"`，boot 运行级别为 bootmisc/hostname/syslog/networking）。**与真实 LXC 的差别**：网络命名空间与面板主机共享，`/etc/network/interfaces` 因此不配置任何接口。<br>在面板新建节点 `alpine-clean`，按面板显示的命令原样在 ash 里执行（`command -v apk … && apk add --no-cache bash curl ca-certificates; curl --no-insecure -fsSL https://github.com/jingniao/qui/releases/download/v1.0.1/node-install.sh \| bash -s -- --version 1.0.1 …`）：apk 装上 bash、curl、ca-certificates，脚本从 GitHub 下载并校验归档，补装依赖，创建 `/etc/machine-id`，安装 Agent 与 Xray，两个服务加入 `default` 并启动，固定 SHA-256 建立面板证书信任，登记后 `proxy-agent` `started`；Agent 以 `proxy-agent` 运行，`CapEff` 为 `0x400`，日志进入 `/var/log/messages`，面板显示 `alpine-clean` `online`、Agent `1.0.1`。测试后停止容器、在面板删除该节点并删除 rootfs。<br>前两次尝试失败，都是模拟容器本身的问题，已记录：第一次只伪造了 `/run/openrc/softlevel`，OpenRC 没有状态目录；第二次缺少 `/etc/network/interfaces`，`need net` 拉起的 `networking` 启动失败。 |
| 从 GitHub 一键安装（systemd）：`us2-node` | 通过 | 面板主机上停止并禁用旧 `us-node` 的 Agent，把 `/etc/proxy-agent`、`/var/lib/proxy-agent` 移到 `/root/qui-accept/us-node.old/`；删除过期的待登记 `us2-node`，新建 `us2-node`，原样执行面板命令（apk 段在 Debian 上为空操作）：从 GitHub 下载、校验、安装、登记，`proxy-agent.service` 启用并 `active`，面板显示 `online`、Agent `1.0.1`。面板与共存服务不受影响。 |
| V01 三节点 | 通过 | 面板同时管理 `alpine-node`（OpenRC）、`uk-node`、`us2-node`（systemd），均 `online`、Agent `1.0.1`。依次停止每个节点的 Agent：被停节点约 25 秒内显示 `offline`，另外两个节点心跳持续更新、保持 `online`；再启动后 20 秒内恢复 `online`。面板状态与各主机上的服务状态一致。 |
| V15 A→B→C | 通过 | 链 `ALP-UK-US2`：`alpine-node` →（Shadowsocks，复用 `uk-node` 内部中转入站 `uk-relay-ss`）→ `uk-node` →（VLESS，`us2-node` 新建内部中转入站 `us2-relay-vless`，35210）→ `us2-node` → Direct。分流规则：`alpine-node` 上 `api.ipify.org`（TCP）→ 该链。按出口、中转、入口顺序发布版本 36、37、38，均成功。<br>经 `alpine-node` 的 `127.0.0.1:990`：`api.ipify.org` 三次都返回 `us2-node` 地址 23.254.158.3，`ifconfig.me` 返回本机出口 113.29.231.218，错误凭据被拒。访问记录：`api.ipify.org` 命中该规则，动作 `Proxy`，出站为 `alpine-to-uk-ss`。<br>失败处理：停止 `us2-node` 的 Agent（其 Xray 随之停止）后，经链的请求失败（curl 退出码 35），没有回退为直连，直连流量不受影响；恢复后链路重新可用。<br>创建过程中的校验也符合预期：`us2-node` 未设置公网地址时，指向它的中转出站被拒（`target node needs an enabled public endpoint`）；匹配 `all` 的规则因链只支持 TCP 被拒。 |
| 真实 LXC 重启（`alpine-node`） | 通过 | 重启前设置强制出站 `hostSourceIpv4=10.111.111.41`（版本 39），nft 表存在，链路可用，主机已运行 2 天。执行 `reboot` 后约 10 秒 SSH 恢复（`up 0 min`）：`proxy-agent` 与 `proxy-agent-host-network` 从 `default` 运行级别启动，Xray 运行，watcher 开机时重放规则（`status.json` 为新的 `id`、`ok:true`，表 `inet proxy_agent_source` 存在），Agent 重连面板，链路与直连都恢复。随后清空该字段（版本 40），表被删除，链路仍可用。 |
| 共存服务 | 通过 | 第三台主机：`komari-agent`、`cf-probe`、`crond` 重启前后都是 `started`（重启只发生在本次批准的 LXC 重启中），`xboard-node` 保持测试前的 `crashed`。面板主机与 `uk-node`：`xboard-node`、`cf-probe` 始终 `active`，启动时间仍是 2026-10-17，未被重启。 |

本轮发现的问题（未修复，留待下一个发布）：
1. **失败的安装留下临时程序文件。** `install.sh` 的 `mv` 失败时，`/usr/local/bin/.proxy-agent.installing.<pid>`（约 20 MB）留在原处，`qui-update` 不清理；暂存目录按设计保留供检查。测试后已手工删除。
2. **`node-install.sh` 的失败提示不准确。** `install.sh` 在发布程序与服务定义之后失败时（例如服务启动失败），`node-install.sh` 仍提示“Agent 安装失败，本机未被修改”；实际上程序、init 脚本与运行级别条目已经写入。
3. **OpenRC 上带版本变化的 `qui-update`** 要等下一个发布才能实测（见上表）。

仍未覆盖：arm64；面板与节点的版本降级。

### 9.6 第四轮：一键安装与 `qui-update` 的失败处理和回滚（2026-10-23，发布前）

**修复（提交 `8895e0a`，已作为 v1.0.2 发布，见 9.7）。** 9.5 的问题 1、2 已修复，另修复本轮发现的三处问题：
- `install.sh` 任何失败都删除进行中的 `.installing` 临时文件与暂存目录（不再保留 20–40 MB 的 `staging.*`）；已开始替换文件后失败时，明确提示新旧文件可能混合。
- `node-install.sh` 的失败提示改为“`install.sh` 可能已经替换了部分文件，本机尚未登记，可以重跑”。
- **新发现：** 最小化 Debian（debootstrap minbase）上一键安装失败，提示 `unzip is required to extract the Xray release archive`。现在 systemd 主机缺少 `unzip` 时，`install.sh` 像 `nftables` 一样用 apt/dnf/yum 安装它。面板的一键命令在 Debian/Ubuntu 缺少 curl 时也会先用 apt 安装。
- **新发现：** 原 `qui-update` 在 `install.sh` 已替换程序、随后失败时（例如单元文件不可写）仍提示“程序未被替换”，并把新程序当作旧程序启动；它也不检查新版本启动后是否持续运行。现在它在替换前快照会被替换的文件，启动后要求 10 秒内主进程 PID 不变；`install.sh` 失败、版本不符或新版本崩溃循环时恢复快照（与快照相同的文件不动），面板还恢复更新前的数据库，然后启动旧版本并再次检查健康。新增离线选项 `--bundle/--sums`。
- **新发现：** 恢复成功后仍打印“降级：用 qui-update --version … 装回旧版本”，现在只在回滚本身失败时打印。

**本地门禁。** `bash -n` 全部 `deploy/linux/*.sh` 与 OpenRC 脚本、`sh -n host-network-watch`、`gofmt`、`go vet`（Windows 与 `GOOS=linux`）、`go test -p 1 ./...` 无失败、Web 构建、前端时间测试，全部通过。

**测试材料。** 用工作区代码构建的测试归档（未发布）：
- `1.0.2`：发布候选。
- `1.0.90`：同一代码，只改版本号。
- `1.0.91`：两个程序都换成测试用崩溃程序，`--version` 报 `1.0.91`，启动 2 秒后退出码 1。
- `1.0.93`：面板换成崩溃前会向 `panel.db` 与 `panel.db-wal` 追加垃圾的程序。
- 损坏的 `SHA256SUMS`：把首个十六进制字符换成另一个。

第一次的损坏规则是“首字符改成 0”，恰好遇上首字符本来就是 0 的摘要，等于没有损坏，因此 OpenRC 容器上那一次“错误摘要”测试实际做成了一次正常升级（脚本行为正确）。已改用必然不同的字符并重测，下表只记录重测结果。

**测试环境：**
- **Debian 容器**：面板主机上用 `debootstrap --variant=minbase` 新建 Debian 13，`systemd-nspawn -b` 以 systemd 为 PID 1 启动，与主机共享网络，因此未在其中测试强制出站。为此在面板主机上安装了 `systemd-container` 与 `debootstrap`。
- **Alpine 容器**：同 9.5 的原版 Alpine 3.19 容器，重新从官方 minirootfs 构建，没有 bash、curl 与 machine-id。
- **真实主机**：`uk-node`、面板主机（`us2-node`）、第三台主机（`alpine-node`，真实 LXC）。真实主机上运行的是新 `update.sh`（与 `service.sh` 一起放在测试目录），对象是 Agent `1.0.1`，因此每次失败都应回到 `1.0.1`。

| 项 | 结果 | 实际观察 |
| --- | --- | --- |
| 全新一键安装（systemd，离线归档） | 通过 | 先在容器里卸载 curl，执行面板一键命令的引导段：apt 装上 curl。再运行 `node-install.sh --bundle/--sums`。第一次失败，暴露了 unzip 问题（见上）：失败后没有暂存目录，也没有 `.installing` 文件。修复后在同一主机直接重跑成功（验证了“可以重跑”）：自动安装 unzip 与 nftables，安装 Agent 与 Xray 26.3.27，登记，`proxy-agent.service` 启用并 `active`。面板显示 `deb-clean` `online`、Agent `1.0.2`。 |
| 全新一键安装（OpenRC，离线归档） | 通过 | 原版 Alpine 容器里在 ash 中执行同一引导段：apk 装上 bash、curl 与 ca-certificates。`node-install.sh --bundle/--sums` 补装依赖，创建 `/etc/machine-id`，两个服务加入 `default` 并 `started`，记录第 10 行为 `openrc`。面板显示 `alpine-clean` `online`、Agent `1.0.2`。 |
| 错误摘要：Debian 容器、Alpine 容器、`uk-node`、第三台主机 | 通过 | 每次都输出“归档 SHA256 与 SHA256SUMS 不一致”，退出码 1；服务没有被停止（主进程 PID 不变），程序、服务定义、`qui-update`、`service.sh` 与安装记录的摘要都不变。 |
| 正常升级：Debian 容器 1.0.2 → 1.0.90，Alpine 容器 1.0.2 → 1.0.90 | 通过 | 打印快照路径，`install.sh` 替换文件，服务启动后输出“已更新到 1.0.90，服务运行正常（10 秒内无重启）”，退出码 0。快照目录被删除，没有临时文件；安装记录 9 行，Alpine 上 10 行且第 10 行为 `openrc`。 |
| 崩溃循环版本自动回滚：Debian 容器、Alpine 容器、`uk-node`、面板主机、第三台主机 | 通过 | 升级到 `1.0.91` 后，健康检查发现主进程 PID 变化（systemd `Restart=on-failure`，supervise-daemon `respawn_delay=5`），输出“新版本 1.0.91 没有持续运行”和对应的日志命令（OpenRC 上是 `grep -F 'proxy-agent' /var/log/messages`）。随后停止服务，恢复快照，启动旧版本并再次检查，输出“更新失败，已恢复到 <旧版本>，服务运行正常（10 秒内无重启）”，退出码 1。<br>真实主机上五个文件的摘要与测试前完全相同，Agent 为 `1.0.1`，Xray 在运行，没有留下快照与临时文件。第三台主机上 watcher `proxy-agent-host-network` 被重启（恢复快照时的预期行为）。<br>面板主机上 Agent 失败后脚本退出，面板没有被停止（PID 不变，没有新的数据库备份）。 |
| `install.sh` 替换程序后失败：Debian 容器、Alpine 容器、`uk-node` | 通过 | 用 `chattr +i` 锁住主服务定义（systemd 单元或 `/etc/init.d/proxy-agent`）：`install.sh` 先替换了程序，再在发布服务定义时 `mv` 失败，提示新旧文件可能混合。`qui-update` 恢复快照（被锁的文件与快照相同，未改动），输出“更新失败，已恢复到 <旧版本>”，退出码 1。程序摘要与安装记录第 3 行都回到旧值，没有 `.installing`、`staging.*` 或 `rollback.*` 残留。第三台主机（LXC）上 `chattr +i` 不生效，未测此项。 |
| 面板崩溃回滚并恢复数据库（Debian 容器） | 通过 | 在容器里装面板 `1.0.2`（`127.0.0.1:18443/19443`，自签证书，HTTP 200），暂时移开 Agent 安装记录，只更新面板到 `1.0.93`。脚本在停止状态下备份数据库并快照文件；新面板启动后向 `panel.db` 与 `panel.db-wal` 写入垃圾（journald 记录 1 次），健康检查失败。回滚恢复文件，删除 `-wal`/`-shm` 并用备份覆盖数据库，启动 `1.0.2`：`active`，`NRestarts=0`，HTTP 200，两个数据库文件里的垃圾计数均为 0，没有残留快照。随后恢复 Agent 安装记录。 |
| 回滚后的数据面 | 通过 | 第三台主机上经 `127.0.0.1:990`：`api.ipify.org` 仍经链路从 `us2-node` 出口（23.254.158.3），`ifconfig.me` 直连（113.29.231.218）。面板显示 `uk-node`、`alpine-node`、`us2-node` 均 `online`、Agent `1.0.1`。 |
| 共存服务 | 通过 | 面板主机与 `uk-node` 上 `xboard-node`、`cf-probe` 保持 `active`，启动时间仍是 2026-10-17。第三台主机上 `komari-agent` 的运行时长持续增加（未重启），`cf-probe`、`crond` 为 `started`，`xboard-node` 保持测试前的 `crashed`。 |
| 从 GitHub 一键安装 v1.0.2（systemd、OpenRC） | 通过 | 见 9.7。 |
| 用旧版 `qui-update`（1.0.1）从 GitHub 升级到 v1.0.2（`uk-node`、`alpine-node`、面板主机） | 通过 | 见 9.7。 |

仍未覆盖：arm64；面板跨迁移的手工降级；更新中途被 Ctrl-C 或断电打断（此时不回滚，快照保留，见[部署与升级](deployment.md)第 5 节）。

### 9.7 v1.0.2 发布版：从 GitHub 安装与更新（2026-10-23）

经 owner 批准，`8895e0a` 与 `a67d9b9` 推送到 `main`，标签 `v1.0.2` 由 CI 发布：`main` 与标签两次检查都通过。发布的 `SHA256SUMS`：amd64 `0287a69b…cf90`，arm64 `6b9c3308…2759`。

| 项 | 结果 | 实际观察 |
| --- | --- | --- |
| 旧版 `qui-update`（1.0.1）升级 `uk-node`（systemd） | 通过 | `qui-update --version 1.0.2 --yes`：从 GitHub 下载并核对摘要，Agent `1.0.1 → 1.0.2`，`active`，退出码 0。没有残留，`xboard-node`、`cf-probe` 的 PID 与启动时间（2026-10-17）不变。 |
| 旧版 `qui-update` 升级 `alpine-node`（OpenRC，真实 LXC） | 通过 | OpenRC 上第一次带版本变化的真实 `qui-update`（关闭 9.5 问题 3）：Agent `1.0.1 → 1.0.2`，`proxy-agent` 与 `proxy-agent-host-network` 都 `started`，安装记录第 10 行仍为 `openrc`，没有残留。`komari-agent` PID 不变，`cf-probe` `started`。 |
| 旧版 `qui-update` 升级面板主机（Agent + 面板） | 通过 | 先 Agent 后面板，都到 `1.0.2`；面板停止时备份数据库为 `pre-update-1.0.1-20261023-130428.db`。两个服务 `active`、`NRestarts=0`。`us2-node` 在面板重启期间按退避重连，约 20 秒后 `online`。`xboard-node`、`cf-probe` PID 不变。 |
| 升级后的数据面 | 通过 | 第三台主机经 `127.0.0.1:990`：`api.ipify.org` 经链路 ALP→UK→US2 从 23.254.158.3 出口，`ifconfig.me` 直连 113.29.231.218。`dataplane.sh` 对 `uk-node`：SOCKS5/HTTP/VLESS/SS/Trojan 直连都返回 `uk-node` 地址，四条阻断规则都被拒（curl 35），错误口令被拒（97），GitHub 与 Netflix robots 都 200。<br>脚本里三条“中转/链到 US”的检查也返回 `uk-node` 地址：对应的分流规则在先前一轮改为链 ALP-UK-US2 时已删除（当前策略只有阻断、GitHub 直连和该链），因此直连是正确结果，不是回归。 |
| 从 GitHub 一键安装（systemd，全新 Debian 13 minbase 容器） | 通过 | 面板登记 `deb-gh`，返回安装版本 `1.0.2`；在容器里原样执行面板一键命令。容器自带 curl，没有 unzip：脚本从 GitHub 下载 `node-install.sh` 与归档并核对摘要，自动安装 unzip。面板证书自签，按固定 SHA-256 建立信任。登记后 `proxy-agent.service` `enabled` 并 `active`，Agent `1.0.2`。没有 `staging.*` 或 `.installing` 残留，面板显示 `online`。 |
| 从 GitHub 一键安装（OpenRC，全新 Alpine 3.19.9 容器） | 通过 | 登记 `alp-gh`，在 ash 里原样执行同一命令（容器没有 bash 与 curl）：apk 装上依赖，创建 `/etc/machine-id`，`proxy-agent` 与 `proxy-agent-host-network` 加入 `default` 并 `started`。记录第 10 行为 `openrc`，Agent `1.0.2`，没有残留，面板显示 `online`。 |
| 新版 `qui-update` 取最新版本（两个容器） | 通过 | `qui-update --yes`：经 GitHub API 查到最新发布，输出“已是最新版本 1.0.2。”，退出码 0。 |
| 新版 `qui-update` 指定不存在的版本（两个容器） | 通过 | `--version 9.9.9`：下载返回 404，输出“更新被拒绝：下载失败…”。服务未被停止（启动时间与 `NRestarts=0` 不变），没有残留。 |
| 新版 `qui-update` 从 GitHub 降级到 1.0.1（Debian 容器） | 通过 | 本意是锁文件测试，但全新容器里没有 `chattr`，所以实际做成了一次正常降级：下载 1.0.1，快照，替换，10 秒健康检查通过，Agent `1.0.1`。随后用容器里已是 1.0.1 的 `qui-update` 再从 GitHub 升回 `1.0.2`，成功。 |
| 新版 `qui-update` 从 GitHub 下载后 `install.sh` 失败（Debian 容器） | 通过 | 在主机侧对容器的 `proxy-agent.service` 执行 `chattr +i`，再 `qui-update --version 1.0.1 --yes`：1.0.1 的 `install.sh` 替换程序后 `mv` 单元失败。脚本恢复快照，输出“更新失败，已恢复到 1.0.2，服务运行正常（10 秒内无重启）”。程序与 `qui-update` 的摘要和测试前相同，Agent `1.0.2` `active`，`rollback.*` 已删除。<br>**残留（预期）：** 本次运行的是 1.0.1 的 `install.sh`，它仍会留下 `staging.*` 与 `.proxy-agent.service.installing.*`。这是 9.5 问题 1，已在 1.0.2 的 `install.sh` 修复，所以只在降级到 ≤1.0.1 时出现。 |
| 共存服务 | 通过 | 测试前后面板主机与 `uk-node` 上 `xboard-node`、`cf-probe` 的 PID 不变；第三台主机上 `komari-agent` PID 不变，`cf-probe` 保持 `started`。 |

**第三台主机的 curl 包装脚本（经 owner 批准修复）。** `/usr/bin/curl` 是两行脚本 `exec /usr/bin/curl.orig -k "$@"`，因此该主机上所有用 curl 的程序都不校验 HTTPS 证书。
- **来源：** 文件时间 2026-02-02 22:43，与 `/etc/hosts`、`/etc/hostname`、`/etc/inittab`、`/fastboot` 同时写入，即服务商的 LXC 模板在开通时加入，不属于任何应用。模板自带的 `ca-certificates-bundle` 是 2023-05 版，这很可能是当初加 `-k` 的原因；当前的 CA 文件（2026-05-30）已能正常校验。
- **影响范围：** 没有程序直接调用 `curl.orig`。唯一用 curl 的应用是 `cf-probe`（bash 脚本），它访问的上报地址使用 Google Trust Services 证书，`cloudflare.com` 同样有效。`komari-agent` 与 `xboard-node` 是独立程序，不用 curl。
- **修复：** `curl.orig` 的 SHA-1 与 apk 数据库里 `curl-8.5.0-r0` 的 `/usr/bin/curl` 记录一致，即未被改动的原版程序。先把包装脚本备份到 `/root/qui-accept/curl-wrapper.bak`，再把原版复制为临时文件，用 `mv` 原子替换 `/usr/bin/curl`；`curl.orig` 保留不动，便于回退。之后 `apk audit` 不再报告 `/usr/bin/curl`。不需要重启任何服务。
- **验证：** 不带任何选项时 `github.com`、`cf-probe` 的上报地址、`cloudflare.com/cdn-cgi/trace` 都返回 200，`ssl_verify_result=0`；`expired.badssl.com`、`self-signed.badssl.com`、`wrong.host.badssl.com` 都被 curl 以错误 60 拒绝。`cf-probe` 继续 `started`，它的上报不写日志，为不暴露其密钥，没有重放它的带密钥请求。随后链路测试与一键安装都在启用校验的 curl 下完成。

**清理。**
- 面板上删除测试节点 `deb-clean`、`alpine-clean`、`deb-gh`、`alp-gh`（均无阻塞项，HTTP 204）。
- 停止两个容器，删除面板主机上的 `debian-clean`、`alpine-clean`、`bundles` 目录，以及三台主机上的 `v102test` 目录。
- 卸载本轮在面板主机上安装的 `systemd-container`、`debootstrap` 及其自动依赖 `libnss-mymachines`、`arch-test`、`distro-info`；`/etc/nsswitch.conf` 的 `hosts` 行恢复为 `files dns`，DNS 正常，没有失败的单元。
- 最终状态：`uk-node`、`alpine-node`、`us2-node` 均 `online`，Agent 与面板都是 `1.0.2`。三台主机上 `/var/lib/proxycontrol-install` 只有安装记录与锁文件。

### 9.8 第五轮：中断后的自动恢复与面板跨迁移降级（2026-10-23，发布前；已作为 v1.0.3 发布，见 9.9）

**风险审查（修改前）。**
- 面板本身是安全的：数据库里有它不认识的迁移时，旧面板在迁移检查阶段拒绝启动，不改动数据库；所有迁移在一个事务里执行。
- `qui-update` 的问题：
  - 没有持久的进度记录。`kill -9` 或断电后，新旧文件可能混合，开机时没有任何东西去恢复。
  - 没有信号处理。Ctrl-C 或 SSH 断线后，服务停着、文件只换了一半。
  - 更新前的数据库备份没有落盘，也没有校验；断电会留下看起来可用的半截 `pre-update-*.db`。
  - 回滚直接覆盖新版本写过的数据库，不留副本。
  - 无法与匹配的旧备份一起降级面板：跨迁移降级只会失败并回滚。

**修复（提交 `8f592f0`，作为 v1.0.3 发布）。**
- `update.sh`：快照里写入进度日志（`prepared`/`started`/`committed`/`restored`，临时文件 → 同步 → 改名 → 同步目录）以及正在运行的 `qui-update` 与 `service.sh` 的副本。
  - 一套恢复例程 `recover_snapshot` 供所有路径共用：失败、信号（INT/TERM/HUP）、`--recover`、开机。
  - 加更新锁；下次运行时先完成未结束的恢复。
  - 备份经 `.partial`、`cmp` 校验后改名；覆盖数据库前保留 `failed-update-*.db`。
  - 用目标版本的 `--check-database` 预检；新增 `--database FILE` 降级。
  - 被杀掉的 `install.sh` 留下的暂存目录在 `operation.lock` 空闲时清除。
- 面板：新增只读的 `proxy-panel --check-database FILE`（`internal/platform/db/inspect.go`），在私有副本上做 `integrity_check` 并比较迁移记录，退出码 0/3/4/5。
- 开机恢复：
  - 新增 `deploy/linux/update-recovery.sh`、`proxycontrol-update-recovery.service` 与 OpenRC 的 `proxycontrol-update-recovery`。
  - `install.sh` 安装并启用它们（不记入安装记录，回滚也保留）；`uninstall.sh` 在两个组件都卸载时删除它们。
- 回归测试：
  - 新增 `deploy/linux/tests/update-test.sh`：用 `QUI_UPDATE_TEST_ROOT` 把真实的 `update.sh` 与开机脚本放进临时根目录，配假的服务库与安装器，共 22 个场景。
  - CI 增加 “Deploy script checks” 步骤：对所有部署脚本做 `bash -n`/`sh -n`，并以 root 运行该测试。
  - Go 新增 `inspect_test.go` 与 `cmd/panel` 的 `--check-database` 测试。

**测试与实测中发现并修复的问题。**
- 停止服务期间收到信号时，处理程序只在 `stop` 返回后运行，那时“服务已停”的标记尚未设置，服务会一直停着。改为在 `stop` 前设置标记（回归测试场景 8 发现）。
- Alpine 的 busybox `flock` 不支持 `-w`，开机恢复等待锁会直接失败（Alpine 上首次运行回归测试：5 个开机场景失败）。改为循环 `flock -n`。
- 预检直接执行归档里的程序：程序权限不是可执行时，或 `/tmp` 挂成 `noexec` 时，会被误判为“数据库无法使用”（真实容器实测发现；该次运行没有替换任何文件，面板继续运行 1.0.3）。改为把程序复制成 root 0700 的私有副本放在安装登记目录里执行，仍无法执行（126/127）时按“不支持预检”处理。
- `--recover` 校验快照路径时没有解析安装登记目录的真实路径；`--boot` 用 `tee` 但没有检查它是否存在；预检拒绝时会留下多余的备份；`--database` 后的提示写成了“已恢复更新前的数据库”。都已修正。

**本地门禁。**
- 通过：`gofmt` 无差异，`go vet ./...`（Windows 与 `GOOS=linux`）通过，`go test -p 1 ./...` 无失败。
- 通过：所有 `deploy/linux/*.sh` 与 `deploy/linux/tests/*.sh` 的 `bash -n`；`update-recovery.sh`、`host-network-watch` 与 `proxycontrol-update-recovery` 的 `sh -n`。
- 未执行：Web 构建与前端时间测试，本轮没有修改前端。

**回归测试（`deploy/linux/tests/update-test.sh`，root）。**

| 环境 | 结果 |
| --- | --- |
| 面板主机（Debian 13，bash 5、GNU coreutils） | 通过：22/22 |
| 第三台主机（Alpine 3.19 LXC，busybox `flock`） | 通过：22/22（修复 `flock -w` 之前 17/22） |

场景：成功更新；`install.sh` 替换后失败；新版本不健康且已写数据库；`prepared` 与 `started` 两个阶段断电后开机恢复；SIGINT、SIGHUP；快照前 SIGTERM；下次运行接手未完成的更新；恢复的幂等性（含 `restored` 阶段）；并发运行被拒；带 `--database` 降级（成功与不健康两种）；`--database` 用于升级被拒；预检 `newer` 被拒且不留备份；预检不支持时继续；备份原子写入；OpenRC 的成功、不健康与断电；只装 Agent 的主机的成功与断电。另验证开机恢复删除被杀的 `install.sh` 的暂存目录，`operation.lock` 被持有时保留。

**实测环境。**
- 面板主机上临时安装 `systemd-container` 与 `debootstrap`，建两个一次性容器：
  - Debian 13 minbase，`systemd-nspawn -b`，systemd 为 PID 1。
  - 官方 Alpine 3.19.9 minirootfs，busybox init + OpenRC，`rc_sys=lxc`。
- 两个容器与主机共享网络，面板测试实例监听 `127.0.0.1:18443/19443`，自签证书。
- 测试归档：
  - 用工作区代码构建 `1.0.3`。
  - `1.0.95`：同一代码，另加一个**只用于测试、未提交**的迁移 `0110_qa_downgrade_probe.sql`（建表并写入一行），用来制造真实的跨迁移降级。构建后已删除该文件。
  - 修复脚本后，用新的 `deploy/` 目录重新打包这两个归档。
- “断电”的做法：对整个容器发 SIGKILL（`machinectl kill --signal=SIGKILL --kill-whom=all`；Alpine 容器杀掉其 init），然后真实开机。它验证进度日志与开机恢复的逻辑，但宿主机的页缓存不会丢失，所以 `sync` 顺序是靠代码审查保证的，没有在真实掉电下验证。

| 项 | 结果 | 实际观察 |
| --- | --- | --- |
| E1 正常升级跨迁移（systemd，面板 1.0.3 → 1.0.95） | 通过 | 预检通过，`pre-update-1.0.3-*.db` 生成，健康检查通过，HTTP 200；数据库 110 个迁移，`qa_downgrade_probe` 有一行。 |
| E2 不带 `--database` 降级 1.0.95 → 1.0.3 | 通过 | 输出“目标版本 1.0.3 无法打开当前数据库”，列出 `pre-update-1.0.3-20261023-145040.db` 与命令，退出码 1。程序摘要不变，1.0.95 继续运行，没有快照，也没有留下检查程序副本。修复后复测：没有留下多余的 `pre-update-1.0.95-*.db`。 |
| E3 带 `--database` 降级 | 通过 | 面板 1.0.3，`NRestarts=0`，HTTP 200，数据库 109 个迁移、没有探测表。`--database` 指定的文件摘要不变；降级前的数据库保留为 `pre-update-1.0.95-*.db`。修复后提示为“已换用 --database 指定的数据库”。 |
| E4 `started` 阶段断电（新面板已运行 3 秒） | 通过 | 断电时数据库已迁移（`--check-database`：110 个迁移，`last: 0110_qa_downgrade_probe.sql`），日志为 `phase=started`。开机时 `proxycontrol-update-recovery` 先于面板运行：恢复文件，把迁移后的数据库保留为 `failed-update-1.0.95-*.db`（探测行仍在），用更新前的备份恢复。面板 1.0.3 `active`、`NRestarts=0`、HTTP 200，数据库 109 个迁移，快照已删除。 |
| E5 `prepared` 阶段断电（新程序已就位、尚未启动） | 通过 | 断电时程序为 1.0.95，`install.sh` 的 `staging.*` 残留。开机恢复只放回文件，没有恢复数据库（与备份相同）：恢复后数据库摘要等于断电时，面板 1.0.3 健康、HTTP 200。该次运行发现暂存目录残留，加入清理后由回归测试场景 4 覆盖、E8 实测。 |
| E6 真实 SIGINT（systemd，安装进行中） | 通过 | 输出“更新被中断（INT）”，恢复文件，1.0.3 健康，退出码 130；程序、单元与 `qui-update` 的摘要与更新前相同。 |
| E7 真实 SIGHUP（模拟 SSH 断线） | 通过 | 退出码 129，文件摘要与更新前相同，`interrupted-update.log` 记录了完整的回滚输出，1.0.3 `active`。 |
| E8 OpenRC `prepared` 阶段断电（Agent 1.0.3 → 1.0.95，已登记到真实面板的临时节点 `e2e-alp`） | 通过 | 断电时程序为 1.0.95，有 `staging.*` 残留。开机后 `proxycontrol-update-recovery` 为 `started`，恢复日志记录“已恢复到 1.0.3”。四个文件摘要与更新前相同，暂存目录已删除。运行中的 Agent 进程的可执行文件不是 `(deleted)`，说明它启动于恢复之后；面板显示节点 `online`。 |
| E9 真实 SIGINT（OpenRC） | 通过 | 退出码 130，文件摘要与更新前相同，强制出站 watcher 重启，Agent 1.0.3 `started`。第一次尝试时 busybox `sh` 的后台任务忽略了 SIGINT（信号没有送达，更新正常完成）；用 `env --default-signal=INT` 启动以模拟前台按 Ctrl-C 后通过。 |
| E10 OpenRC `started` 阶段断电（新 Agent 已运行 2 秒） | 通过 | 开机恢复后 Agent 1.0.3 运行于恢复后的程序，文件摘要相同，节点 `online`。 |
| 共存服务 | 通过 | 面板主机上只在临时目录与容器里操作；真实面板与 `us2-node` 没有被更新或重启，回归测试只使用临时根目录与假服务库。第三台主机上回归测试同样只使用临时目录，`komari-agent`、`cf-probe` 未受影响。 |

仍未覆盖：
- 真实掉电下的 `sync` 顺序（见上）。
- arm64。
- 面板 ≤1.0.2 作为降级目标时的预检（这类目标没有该参数，代码按“不支持”处理，回归测试场景 15 覆盖）。
- 从 1.0.2 升级到本版本时，运行的仍是 1.0.2 的 `qui-update`，没有本节的保护。

### 9.9 v1.0.3 发布与三台主机升级（2026-10-23）

经 owner 批准，`8f592f0` 推送到 `main`，标签 `v1.0.3` 由 CI 发布。`main` 与标签两次检查都通过，新增的 “Deploy script checks”（以 root 运行 `update-test.sh`）也在其中。发布为正式版（非草稿、非预发布），发布时间 2026-10-23T07:58:16Z，附件为两个归档、`SHA256SUMS` 与 `node-install.sh`；amd64 摘要 `ae7778a5…cada`，arm64 `abb7138e…4d33`。

**升级（从 GitHub，各主机已安装的 1.0.2 `qui-update`）。** 1.0.2 的 `qui-update` 没有进度日志与开机恢复，这一次升级本身按 1.0.2 的方式执行（快照 + 健康检查）。

| 主机 | 结果 | 实际观察 |
| --- | --- | --- |
| `uk-node`（Debian，systemd） | 通过 | Agent `1.0.2 → 1.0.3`，摘要核对一致，健康检查通过，退出码 0。`proxycontrol-update-recovery.service` 已创建并启用。`xboard-node`、`cf-probe` PID 不变（833、834）。 |
| `alpine-node`（Alpine LXC，OpenRC） | 通过 | Agent `1.0.2 → 1.0.3`，`proxy-agent` 与 watcher `started`；`proxycontrol-update-recovery` 加入 `default` 运行级（安装时不启动，符合设计）。`komari-agent` PID 不变（544），`cf-probe` `started`，`xboard-node` 保持测试前的 `crashed`。 |
| 面板主机（Agent + 面板，systemd） | 通过 | 先 Agent 后面板，都到 `1.0.3`；面板停止时备份为 `pre-update-1.0.2-20261023-160445.db`。两个服务 `active`、`NRestarts=0`，HTTP 200。`xboard-node`、`cf-probe` PID 不变（738、740）。 |

**开机恢复单元的验证（三台主机，不重启、不影响服务）。** 每台主机从 GitHub 重新下载 v1.0.3 归档并核对 `SHA256SUMS`，然后：

| 项 | 结果 | 实际观察 |
| --- | --- | --- |
| 安装的文件与发布一致 | 通过 | 三台主机上 `/usr/local/lib/proxycontrol/update-recovery`、`/usr/local/bin/qui-update`，以及 `proxycontrol-update-recovery.service` 或 `/etc/init.d/proxycontrol-update-recovery`，都与归档内的文件逐字节相同。 |
| 启用与顺序（systemd） | 通过 | `enabled`，`WantedBy=multi-user.target`；`Before=` 包含 `proxy-agent.service`、`proxy-panel.service`、`proxy-agent-host-network.service`、`proxy-agent-host-network.path`；`systemd-analyze verify` 没有报告。 |
| 启用与顺序（OpenRC） | 通过 | 在 `default` 运行级。OpenRC 依赖树（`/run/openrc/deptree`）中它 `ineed localmount`、`ibefore proxy-agent proxy-panel proxy-agent-host-network`，而 `proxy-agent` 与 `proxy-agent-host-network` 都记录 `iafter proxycontrol-update-recovery`。 |
| 没有进度日志时实际启动 | 通过 | systemd：`systemctl start` 返回 0，单元因 `ConditionPathExistsGlob=/var/lib/proxycontrol-install/rollback.*/journal` 不满足而跳过。OpenRC：`rc-service proxycontrol-update-recovery start` 为 `[ ok ]`，状态 `started`。三台主机直接运行脚本，退出码都是 0，安装登记目录没有变化。 |
| 恢复路径（发布内的回归测试） | 通过 | 三台主机各以 root 运行归档里的 `deploy/linux/tests/update-test.sh`（临时根目录、假服务库，不接触真实服务），都是 22/22 通过；其中包括开机脚本在 `prepared`、`started` 两个阶段断电后的恢复，以及 OpenRC 变体。真实初始化系统下的断电与信号恢复见 9.8 的 E4–E10。 |

**最终状态。**
- 面板显示 `uk-node`、`alpine-node`、`us2-node` 均 `online`、Agent `1.0.3`；面板 `1.0.3`。
- 链路：第三台主机经 `127.0.0.1:990`，`api.ipify.org` 从 `us2-node` 出口（23.254.158.3），`ifconfig.me` 直连（113.29.231.218）。
- 三台主机的 `/var/lib/proxycontrol-install` 只有安装记录与锁文件，没有残留快照。
- 共存服务的 PID 与升级前相同。

**顺带发现并清理的两个旧失败状态。**
- 面板主机：`systemd-machined.service` 处于 `failed`/`not-found`，是 9.8 那一轮卸载 `systemd-container` 时留下的记录；已 `systemctl reset-failed`。
- `uk-node`：`proxy-agent-host-network.service` 自 2026-10-22 17:52 起处于 `failed`。
  - 来源：第一轮验收里故意用文档地址 `2001:db8::5` 做的强制出站失败测试。连续失败触发了 systemd 的启动频率限制，path 单元以 `unit-start-limit-hit` 停止，17:53 写入的“清除”请求（四个字段全空）因此没有被执行，`status.json` 一直停留在旧请求的失败结果上。18:03 的重新安装重新启用了 path 单元。
  - 处理：期望状态是清除（不加任何规则），所以执行它不会改变网络。`reset-failed` 后手动运行一次 oneshot 单元（等同开机时的重放），`status.json` 变为当前请求 `ok:true`，nft 表仍不存在。之后两台 systemd 主机都没有失败的单元。
  - **遗留问题**：强制出站 helper 在短时间内连续失败时，systemd 会停用 path 单元，之后的期望状态在下次安装或重启前都不会被执行。已在发布前修复，见 9.10。

### 9.10 强制出站 helper 的启动频率限制（2026-10-23，发布前）

**原因。**
- systemd 默认每个单元 10 秒内最多启动 5 次，成功和失败都计数。path 单元每次发布触发一次 oneshot 单元；helper 连续失败几次后，path 单元以 `unit-start-limit-hit` 停止，之后写入的 `desired.json` 都不会被执行。
- Agent 在没有配置强制出站时，只要保留的 `desired.json` 已经是“清除”请求就直接跳过。如果这个清除请求从未被执行，之后的清除版本也不会再到达 helper，旧的 nft 表可能一直生效到重启。
- OpenRC 不受影响：watcher 吞掉 helper 的失败，`supervise-daemon` 无限重启。

**修复。**
- `proxy-agent-host-network.service` 的 `[Unit]` 增加 `StartLimitIntervalSec=0`。触发频率由 Agent 自己的重试决定。
- `reconcile/hostnetwork.go`：只有 `status.json` 确认同一 `id` 的清除请求 `ok:true` 时才跳过；否则重新发布清除请求并等待结果。从未使用该功能（没有 `desired.json`）的节点行为不变。
- `install.sh`（systemd）：oneshot 单元处于 `failed` 时执行 `reset-failed`，并用 `start --no-block` 重放一次保留的文件（相当于开机，也相当于 OpenRC 重启 watcher）。helper 失败不会让安装失败。
- `deploy/openrc/proxy-agent-host-network`：修正 `use logge` → `use logger` 与一处注释拼写。
- 新增回归测试 `deploy/linux/tests/host-network-test.sh`，并加入 CI 的 “Deploy script checks”。
  - 它在 `/run` 下用仓库里的真实单元文件（systemd，完整保留 `[Unit]`）或真实 watcher（OpenRC），配合一个假 helper，按 Agent 的方式发布。
  - 先发布 8 个失败请求，再发布一个清除请求；然后发布一个失败请求后立刻发布清除请求。
  - 不接触真实单元、`/var/lib/proxy-agent` 或 nft。

**结果。**

| 检查 | 结果 | 证据 |
|---|---|---|
| Go 检查 | 通过 | `gofmt -l` 无输出；`go vet`（Windows 与 `GOOS=linux`）通过；`go test ./internal/agent/... ./cmd/agent/` 全部 `ok`。新测试 `TestSyncHostNetworkRepublishesAClearTheHelperNeverConfirmed` 覆盖四种情况：没有状态、同一 `id` 失败、其他 `id` 成功、没有 helper。恢复旧的跳过逻辑后，这四种情况都失败。 |
| 回归测试能复现问题（v1.0.3 单元，`uk-node`） | 通过（按预期失败） | 0/4。`fail-6`～`fail-8` 没有回应，helper 只运行 5 次，path 单元停止，清除请求没有被执行。 |
| 修复后（systemd 257，`uk-node` 与面板主机） | 通过 | 两台主机都是 4/4：清除请求被执行，path 单元仍在监视，helper 每次发布运行一次，最后一个请求生效。 |
| OpenRC（第三台主机，旧版与新版 watcher） | 通过 | 都是 4/4，8 次失败后 watcher 仍在运行。 |
| 单元文件 | 通过 | `systemd-analyze verify` 无输出；加载后 `StartLimitIntervalUSec=0`。 |
| `install.sh` 重放 | 通过 | 在一个必然失败的临时单元上运行同一段代码：`failed` 单元被重新执行一次（运行次数 1 → 2），代码段退出码 0。 |
| 清理与共存服务 | 通过 | 测试单元、`/run/qui-hn-test.*` 与上传的文件都已删除，没有失败的单元。面板主机与 `uk-node` 上 `proxy-agent`、`proxy-panel`、`xboard-node`、`cf-probe` 的 PID 与测试前相同；第三台主机上 `komari-agent`、`cf-probe`、`proxy-agent`、`proxy-agent-host-network` 的运行时长持续增加，`xboard-node` 保持测试前的 `crashed`。 |
| 真实 Agent 端到端（发布强制出站失败后再清除） | 跳过 | 修复尚未发布；三台主机仍运行 v1.0.3。 |
