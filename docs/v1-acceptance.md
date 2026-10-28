# V1 验收矩阵

更新时间：2026-10-26（v1.0.8 发布与三台主机升级，9.19；此前为 v1.0.8 发布前复验：核心更新结果重发与入站启用意图两项修复及五项回归，9.18（`1663e4a`）；此前为 v1.0.7 发布、三台主机升级与四项修复的实机复测，9.16；其后为 v1.0.8 发布前验收与五项实机回归，9.17（`7b99d8e`、`8608678`）；此前为代码审查报告五个问题的核实与修复，9.15；此前为 v1.0.6 发布与 Agent 启动恢复失败不再反复重启的实机复测，9.14；此前为 v1.0.5 发布与两项修复的实机复测，9.13）。标准来自 [项目说明书](../AGENTS.md) 第 34 节的 25 条，以及第 33 节的测试要求与第 35 节的边界；本轮按轻量重构计划的决策 D1–D5 更新验收动作。

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
- `uninstall.sh` 的提示仍提到已删除的 “quota ledgers / reserved allowances”；`install.sh --reinstall` 仍提示去登记已登记的节点。（v1.0.7 修复：卸载提示按组件列出保留内容，只对 Agent 提示去面板删除节点；已有 `identity/client.crt` 时不再提示登记。）
- sing-box 适配器的 `TrafficSourceStatus` 注释仍写着节点总量读取器“尚未实现”，实际已实现并在心跳中上报。（v1.0.7 修正注释。）
- `/var/lib/proxy-panel` 与 `/var/lib/proxy-agent` 被 systemd `StateDirectory` 设为 0755（内容仍为 0600/0700），与 `install.sh` 和文档写的 0700 不一致。（v1.0.7 加 `StateDirectoryMode=0700`，升级后实测两台 systemd 主机的状态目录为 0700，见 9.16。）
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

### 9.10 强制出站 helper 的启动频率限制（2026-10-23，发布前；已作为 v1.0.4 发布，见 9.11）

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
| 真实 Agent 端到端（发布强制出站失败后再清除） | 见 9.11 | 发布前跳过（当时三台主机仍运行 v1.0.3），发布后在 9.11 完成。 |

### 9.11 v1.0.4 发布、三台主机升级与实机复测（2026-10-23）

**发布。**
- 提交 `a820647` 推送到 `main`。该提交与标签 `v1.0.4` 的 CI 都是 `success`，包括调用 `host-network-test.sh` 的 “Deploy script checks” 步骤。CI 日志需要登录才能下载，所以无法确认该测试在 runner 上实际运行，还是因环境不满足而跳过；它的实际结果以 9.10 的三台主机为准。
- GitHub Release `v1.0.4` 于 2026-10-23T10:36:29Z 发布，正式版，不是草稿。附件有两个归档、`SHA256SUMS` 与 `node-install.sh`。
- amd64 归档 SHA-256 为 `5ea8a4a9…9173893`，与 `SHA256SUMS` 一致。归档里的单元带 `StartLimitIntervalSec=0`，并包含 `deploy/linux/tests/host-network-test.sh`。

**升级（各主机用已安装的 1.0.3 `qui-update --version 1.0.4 --yes` 从 GitHub 升级）。**

| 主机 | 结果 |
|---|---|
| `uk-node`（Debian，systemd） | Agent `1.0.3 → 1.0.4`，`active`，10 秒内无重启，退出码 0；加载后 `StartLimitIntervalUSec=0`，path 单元 `active` |
| 第三台主机 `alpine-node`（OpenRC） | Agent `1.0.4`，`started`，watcher 随升级重启，退出码 0 |
| 面板主机 | Agent 与面板都到 `1.0.4`；面板更新前备份 `pre-update-1.0.3-20261023-183900.db`；HTTPS 返回 200 |

三台主机都没有残留快照（`/var/lib/proxycontrol-install` 只有安装记录与锁文件），也没有失败的单元。面板显示三个节点 `online`、Agent `1.0.4`。

**实机复测：真实 Agent、真实 helper，经面板发布。**

| 检查 | 结果 | 证据 |
|---|---|---|
| 失败请求被拒绝并回滚（`uk-node`） | 通过 | 把强制出站 IPv6 设为文档地址 `2001:db8::5`，然后发布修订 41～47。每个修订都是 `rollback`、`host_network_failed`。 |
| 超过 systemd 原限制的连续启动（`uk-node`） | 通过 | 修订 41～44 依次部署：helper 共启动 16 次、失败 8 次，但任意 10 秒内最多 4 次。修订 45～47 一结束就部署下一个：10 秒内启动 12 次、失败 6 次，超过旧的 10 秒 5 次限制。日志里没有 start-limit 消息，path 单元仍为 `ActiveState=active`、`Result=success`。 |
| 连续失败后清除生效（`uk-node`） | 通过 | 清空字段，发布修订 48：`success`。`desired.json` 与 `status.json` 是同一个新 `id`，且 `ok:true`。nft 表 `inet proxy_agent_source` 不存在，oneshot 单元 `Result=success`，没有失败的单元。 |
| OpenRC 同一流程（`alpine-node`） | 通过 | 修订 49～52 连续部署：都是 `rollback`、`host_network_failed`。清除修订 53：`success`，`status.json` 与 `desired.json` 是同一个 `id` 且 `ok:true`，没有 nft 表。`proxy-agent-host-network` 仍是 `started`。 |
| 数据面 | 通过 | 第三台主机经 `127.0.0.1:990`（带 SOCKS 凭据）：`api.ipify.org` 从 `us2-node` 出口（23.254.158.3），`ifconfig.me` 直连（113.29.231.218），与升级前相同。 |
| 共存服务 | 通过 | 面板主机与 `uk-node` 上 `xboard-node`、`cf-probe` 的 PID（738/740、833/834）与测试前相同。第三台主机上 `komari-agent` 的运行时长持续增加（未重启），`cf-probe` 为 `started`，`xboard-node` 保持测试前的 `crashed`。 |

最终状态：两个节点的强制出站字段都为空，与测试前相同。复测时上传和生成的临时文件都已删除。

### 9.12 全部协议的真实流量验收（2026-10-23，v1.0.4）

**范围与方法。**
- 三台主机都运行 v1.0.4 发布版，核心为官方 Xray 26.3.27 与 sing-box 1.14.3。
- 为本轮新建测试用户 `proto` 与节点组 `proto-test`（三个节点），一台设备持有全部 8 种协议的凭据。已有用户、凭据与配置都未改动。
- 用户侧入站由面板主机上的真实客户端测试。客户端配置直接由该设备的 sing-box 格式订阅生成：每个端点一个本地 SOCKS 端口（TCP），以及两个 UDP 转发（到 `1.1.1.1:53` 与 `1.0.0.1:53`）。
  - TCP 判定：经代理访问 `api.ipify.org` 得到的出口地址。
  - UDP 判定：经代理向 Cloudflare 查询 `whoami.cloudflare`（CH TXT）得到的来源地址。
- 中转出站在源节点上测试：在源节点建回环 SOCKS5 入站，默认出站指向被测的中转出站。在该节点上用 curl 测 TCP，用 Xray 客户端的 SOCKS5 UDP ASSOCIATE 测 UDP。
- 认证：每个入站都用同一客户端、但把凭据全部换成错误值再测一遍（UUID、口令、SS 密钥、WireGuard 私钥）。
- 发布一律用 `POST /api/v1/deployment-batches/relay`，面板按“先目标、后源”的顺序发布到三个节点（修订 54～71），全部 `success`。
- 环境限制：面板主机的 ufw 只放行 35000～40000/tcp，`uk-node` 另放行同范围的 UDP，第三台主机在 NAT 后。本轮没有改动任何防火墙，所以凡是需要 UDP 包直接到达面板主机的组合都无法测试（见下文“未覆盖”）。

**Xray 节点用户侧入站（`uk-node`，客户端在面板主机，经公网）。**

| 入站 | TCP | UDP | 错误凭据 |
|---|---|---|---|
| SOCKS5 | 通过（出口 `uk-node`） | 通过 | 拒绝 |
| HTTP | 通过 | 不适用（HTTP 无 UDP，客户端拒绝） | 拒绝 |
| VLESS tcp / 无 TLS | 通过 | 通过 | 拒绝 |
| VLESS tcp + TLS + `xtls-rprx-vision` | 通过 | 通过 | 拒绝 |
| VLESS ws + TLS | 通过 | 通过 | 拒绝 |
| VLESS gRPC（无 TLS） | 通过 | 通过 | 拒绝 |
| Trojan ws + TLS | 通过 | 通过 | 拒绝 |
| Trojan gRPC + TLS | 通过 | 通过 | 拒绝 |
| Shadowsocks aes-256-gcm / aes-128-gcm / chacha20-ietf-poly1305 / xchacha20-ietf-poly1305 | 均通过 | 均通过 | 均拒绝 |
| WireGuard（Xray 入站，设备地址 10.77.0.2/32 由面板分配） | 通过 | 通过 | 拒绝（错误私钥无法握手） |

第一轮建的 `uk-trojan` 没有 `trusted_ca_pem`，订阅里不带它的自签证书，按订阅生成的客户端因此无法校验证书。这是该入站的配置，不是缺陷；它在第一轮用固定证书验证过。

**中转出站（Xray → Xray，源 `uk-node`，目标 `us2-node` 的内部入站）。**

| 出站 | TCP | UDP | 说明 |
|---|---|---|---|
| HTTP | 通过（出口 `us2-node`） | 不适用 | — |
| SOCKS5 | 通过 | 未覆盖 | SOCKS5 UDP 需要 UDP 包直达 `us2-node`，被面板主机防火墙丢弃 |
| Trojan ws + TLS（自动自签证书，固定信任） | 通过 | 通过 | UDP 封装在 TCP 内 |
| VLESS gRPC + TLS | 通过 | 通过 | 同上 |
| Shadowsocks xchacha20-ietf-poly1305 | 通过 | 未覆盖 | 同 SOCKS5 |
| WireGuard（第三台主机 → `uk-node` 的 WireGuard 中转入站） | 通过（出口 `uk-node`） | 通过 | `us2-node → uk-node` 被拒绝：`relay outbound would create a routing loop`（已有 `uk → us2` 中转），符合预期 |
| SOCKS5（第三台主机 → `uk-node`） | 通过 | 通过 | 补测 SOCKS5 的 UDP 出站 |
| Shadowsocks（第三台主机 → `uk-node`，即链 1 的第一跳） | 通过 | 通过 | 该内部入站属于链 1，流量按链继续到 `us2-node` |

源节点上错误口令的 SOCKS5 连接全部被拒。

**分流与代理链。**
- UDP 分流：`uk-node` 上的规则把发往 `1.0.0.1` 的 UDP 送到 VLESS gRPC 中转，发往 `1.1.1.1` 的 UDP 仍直连。
  - Xray 上：经 WireGuard 与 SOCKS5 中转进入 `uk-node` 的 UDP 命中该规则，从 `us2-node` 出口，说明节点规则也作用于中转进来的流量。用户侧入站的 UDP 也命中了该规则（访问记录为 `udp`、`Proxy`），但那时规则还指向 SOCKS5 中转，包被面板主机防火墙丢弃；改为 VLESS gRPC 后没有再从面板主机跑这一项。
  - sing-box 上：经 12 个可承载 UDP 的用户侧入站（SOCKS5 除外，见下文）发起的查询都从 `us2-node` 出口。
- 策略优先于入站默认出站：第三台主机的回环入站默认出站指向中转，但命中链规则的目标（`api.ipify.org`、`1.1.1.1`）仍走代理链。
- 新建第二条 A→B→C 链 `ALP-UK-US2-tj-vlessws`：第三台主机 → Trojan gRPC + TLS → `uk-node` → VLESS ws + TLS → `us2-node`，`supportUdp=true`。经该链的 TCP（`icanhazip.com`，从 `us2-node` 的 IPv6 出口）与 UDP（`1.1.1.1`）都从 `us2-node` 出口。原有链 `ALP-UK-US2`（SS → VLESS）同时正常。
- 访问记录：以上流量都按用户、设备、入站协议、网络（tcp/udp）、动作与出站协议记录。WireGuard 入站的 TCP 记录为目标 IP（客户端自行解析域名），符合“无法确定域名时记录目标 IP”。

**sing-box 节点。** 先把第三台主机、再把 `uk-node` 临时切换为 sing-box 1.14.3。做法：停 Agent，改 `config.yaml` 的 `core_name`/`core_path`，再启动；Agent 用新核心重新渲染当前修订后，面板发布新修订。

| 检查 | 结果 | 证据 |
|---|---|---|
| 能力校验 | 通过 | 在 sing-box 节点上保存 WireGuard 入站，以及在 Xray 节点上保存 Hysteria2 入站，都返回 400，并说明该核心不支持此协议 |
| 用户侧入站（`uk-node`） | 通过 | 与 Xray 相同的入站（SOCKS5、HTTP、各种 VLESS/Trojan/SS），加上 **Hysteria2** 与 **TUIC**：TCP 全部从 `uk-node` 出口；除 HTTP 与 SOCKS5 外 UDP 全部通过；`1.0.0.1` 的 UDP 分流从 `us2-node` 出口；15 个入站都拒绝错误凭据。旧 `uk-trojan` 与 Xray 时一样因订阅不带证书而失败 |
| 出站（`uk-node` sing-box → `us2-node` Xray） | 通过 | HTTP、SOCKS5、Trojan ws、VLESS gRPC + TLS、SS 的 TCP 都从 `us2-node` 出口；Trojan ws 与 VLESS gRPC 的 UDP 通过 |
| Hysteria2 / TUIC 中转（第三台主机 sing-box → `uk-node` sing-box） | 通过 | TCP 与 UDP（`1.1.1.2` 的 whoami）都从 `uk-node` 出口 |
| WireGuard 出站（第三台主机 sing-box → `uk-node` 的 Xray WireGuard 入站） | 通过 | TCP 从 `uk-node` 出口，UDP 经 `uk-node` 的 UDP 规则从 `us2-node` 出口 |
| 两条代理链（sing-box 入口） | 通过 | 链 1 TCP、链 2 TCP 与 UDP 都从 `us2-node` 出口 |
| sing-box SOCKS5 入站的 UDP | 未完全覆盖 | 在 `uk-node` 本机通过（TCP 与 UDP 都从 `uk-node` 出口）。从面板主机或第三台主机远程访问时 UDP 失败，而同一路径在 Xray 上通过。最可能的原因是 sing-box 的 UDP ASSOCIATE 使用另一个 UDP 端口，不在 `uk-node` 防火墙放行的 35000～40000 范围内。没有抓到该端口，所以记为环境所限，既不判为通过，也不判为缺陷 |

**发现的问题（已修复，v1.0.5 发布，实机复测见 9.13）。**

| # | 问题 | 影响 | 修复与验证 |
|---|---|---|---|
| 1 | `install.sh --core sing-box` 与 `node-install.sh --core sing-box` 在 Alpine（musl）上安装 `sing-box-<版本>-linux-amd64.tar.gz`。该构建动态链接 glibc，在 Alpine 上执行报 `not found`。官方还发布了 `linux-amd64-musl`（以及 `-glibc`）归档，但脚本只认前者，固定摘要也只有前者 | **Alpine 节点无法使用 sing-box**。本轮在第三台主机上改用官方 musl 归档，SHA-256 与 GitHub 资产摘要 `41fc81fd…bab1f` 一致，之后一切正常。Xray 不受影响 | 两个脚本按 `/lib/ld-musl-*.so.1` 判断 libc，musl 主机使用 `-musl` 归档；`node-install.sh` 按架构与 libc 固定四个官方摘要。新增 `deploy/linux/tests/core-asset-test.sh`（17 项，无需 root，CI 中运行），在 `uk-node` 与第三台主机上均 17/17。用修复后的函数在真实主机上选择并下载归档：`uk-node` 选 `linux-amd64`、第三台主机选 `linux-amd64-musl`，摘要都与固定值一致，解出的 sing-box 都能运行（只在临时目录，未安装）。v1.0.5 发布后用发布物实机安装并作为节点核心运行，见 9.13 |
| 2 | 只要 Hysteria2 或 TUIC 入站设置了 `trusted_ca_pem`，该用户的整个 URI/Base64 订阅就返回 400 `Subscription unavailable`。这是 `subscription/export.go` 的有意行为（URI 无法携带证书），但错误信息不说明原因，也连带让其他协议的链接不可用 | 用户只能改用 sing-box 格式订阅。sing-box 格式正常，可携带证书 | URI/Base64 只省略这类端点（URI 没有放证书的字段），其余链接照常输出；sing-box 格式不变，仍带证书。单元测试 `TestURIFormatsOmitQUICEndpointWithPinnedCA` 覆盖省略、无固定证书的 Hysteria2 照常输出、全部被省略时返回空列表而非错误，以及 sing-box 格式保留证书。v1.0.5 发布后已实机复测，见 9.13 |

**未覆盖。**
- SOCKS5 与 Shadowsocks 的 UDP 中转到 `us2-node`，以及 sing-box 的 WireGuard 出站指向 `us2-node`：都需要面板主机放行 UDP。前两者已改在 `uk-node` 方向上验证。
- sing-box SOCKS5 入站的远程 UDP：见上表。
- arm64：仍无主机。

**恢复。**
- 两台主机都恢复 Xray（原 `config.yaml` 原样复制回去），本轮下载的 sing-box 二进制已删除。
- 测试用户、节点组、全部入站/出站/链/规则都已删除，面板对象数恢复到测试前：入站 8、出站 2、链 1、规则 4。之后发布了修订 69～71。
- 恢复后，原链 `ALP-UK-US2` 的 `api.ipify.org` 从 `us2-node` 出口（23.254.158.3），直连为 113.29.231.218。
- 共存服务：`xboard-node`、`cf-probe` 的 PID 在面板主机与 `uk-node` 上与测试前相同（738/740、833/834），没有失败的单元。第三台主机的 `komari-agent` 重启计数为 0、运行时长持续增加，`xboard-node` 保持测试前的 `crashed`。
- 三台主机上的临时脚本与凭据文件都已删除。

### 9.13 v1.0.5 发布、三台主机升级与两项修复的实机复测（2026-10-24）

**发布。**
- 标签 `v1.0.5` 指向 `c52643c`（修复在 `4c7572e`）。标签的 CI 是 `success`，包括 `check` 与 `release` 两个任务。
- GitHub Release `v1.0.5` 于 2026-10-24T14:59:54Z 发布，正式版，不是草稿。附件有两个归档、`SHA256SUMS` 与 `node-install.sh`。amd64 归档的 SHA-256 为 `b6e8c34b…6918`，arm64 为 `d5d16f97…e833`。

**升级（各主机用已安装的 1.0.4 `qui-update --version 1.0.5 --yes` 从 GitHub 升级）。**

| 主机 | 结果 |
|---|---|
| `uk-node`（Debian，systemd） | Agent `1.0.4 → 1.0.5`，`active`，10 秒内无重启，退出码 0；path 单元 `active` |
| 第三台主机 `alpine-node`（OpenRC） | Agent `1.0.5`，`started`，watcher `started`，退出码 0 |
| 面板主机 | Agent 与面板都到 `1.0.5`；面板更新前备份为 `pre-update-1.0.4-20261024-230344.db`；HTTPS 返回 200 |

- 三台主机都没有残留快照，也没有失败的单元。面板显示三个节点 `online`、Agent `1.0.5`。
- 升级后原链 `ALP-UK-US2` 正常：`api.ipify.org` 从 `us2-node` 出口（23.254.158.3），`ifconfig.me` 直连（113.29.231.218）。

**修复 1：Alpine 上的 sing-box（9.12 问题 1）。** 只使用发布物：在第三台主机上从 GitHub 下载 v1.0.5 归档，按 `SHA256SUMS` 核对一致后解压。

| 检查 | 结果 | 证据 |
|---|---|---|
| `node-install.sh` 的选择 | 通过 | 取发布版 `node-install.sh` 里的 `host_libc` 与 `singbox_digest` 在主机上执行：`libc=musl`，摘要 `41fc81fd…ab1f` |
| `install.sh` 安装 | 通过 | 发布版 `install.sh --node … --core sing-box --core-version 1.14.3 --core-sha256 <上面的摘要>` 下载 `-musl` 资产并通过摘要核对；安装记录第 5 行为 `sing-box`。二进制是静态链接，`sing-box version` 输出 `1.14.3`。修复前同一命令装的是 glibc 构建，执行报 `not found` |
| 作为节点核心运行 | 通过 | 停 Agent，把 `core_name`/`core_path` 改成 sing-box，再启动。面板显示 `sing-box 1.14.3`，发布修订 72～74 全部 `success`，节点上运行的核心进程是 `sing-box` |
| 数据面 | 通过 | 第三台主机经 `127.0.0.1:990`（带 SOCKS 凭据）：`api.ipify.org` 经链从 `us2-node` 出口，`ifconfig.me` 直连，错误口令被拒绝（curl rc 97） |

**修复 2：钉证书的 Hysteria2/TUIC 不再让 URI 订阅整体失败（9.12 问题 2）。**
- 临时对象，均只为本测试建立：
  - 节点组 `subtest`（只含 `alpine-node`）和用户 `subtest`，用户只允许 hysteria2、tuic、shadowsocks；一台设备，以及该设备的订阅 Token。
  - `alpine-node` 上的三个入站：Hysteria2 `:35260` 与 TUIC `:35261`，两者用同一张临时自签证书，`trusted_ca_pem` 是这张证书本身；另有 Shadowsocks `:35262` 作对照。
  - 测试期间把 `alpine-node` 的公网 IP 设成 `113.29.231.218`。没有公网 IP 的节点不进入订阅，该节点的这个字段原本为空。
- 发布修订 75～77，全部 `success`。

| 检查 | 结果 | 证据 |
|---|---|---|
| URI 列表 | 通过 | `GET /sub/<token>` 返回 200，只有 `ss://…@113.29.231.218:35262` 一条；修复前同样的数据返回 400 `Subscription unavailable` |
| Base64 列表 | 通过 | `?format=base64` 返回 200，解码后与 URI 列表相同 |
| sing-box 格式 | 通过 | `?format=sing-box` 返回 200，有 hysteria2、shadowsocks、tuic 三个出站；两个 QUIC 出站都带 `tls.server_name` 与 `tls.certificate`（即钉住的证书） |
| 用订阅里的证书收发真实流量 | 通过 | 用 sing-box 格式订阅生成客户端（`ptclient.py`），在第三台主机上运行。第三台主机在 NAT 后，所以只把 `server` 改成 `127.0.0.1`，`server_name` 保持不变。三个协议的 TCP 都经链从 `us2-node` 出口，UDP（Cloudflare `whoami`）都是 113.29.231.218。系统信任库里没有这张证书，所以 QUIC 握手成功只能是靠订阅里的 `certificate` 验证 |
| 错误凭据 | 通过 | 把凭据换成错误值后，三个协议都失败，Hysteria2 报 `authentication failed` |

**测试中发现的问题（未修复，见下文“遗留问题”第 1 项）。**
- 删除临时入站后，测试先把 `alpine-node` 换回 Xray，然后才发布。Agent 启动时要恢复上一个成功修订 77，其中还有 Hysteria2/TUIC 入站。用 Xray 编译失败，Agent 报 `restore previous core configuration: unsupported inbound protocol` 后退出。supervise-daemon 每 5 秒重启它一次，从 15:14:29 到 15:19:03 共 53 次。
- 这段时间 Agent 连不上面板，修订 80 一直是 `pending`，面板仍显示该节点是 sing-box。`alpine-node` 没有核心运行，原链入口中断约 4.5 分钟；另外两台节点不受影响。
- 恢复方法：暂时装回同一发布版的 sing-box musl 核心并改回配置，Agent 恢复修订 77 后连上面板，修订 80 随即 `success`。之后再切换到 Xray，Agent 恢复修订 80 正常，最后发布修订 81～83，全部 `success`。

**恢复。**
- `alpine-node` 恢复 Xray 26.3.27：用发布版 `install.sh --core xray` 重装，安装记录第 5 行重新为 `xray`；`config.yaml` 的 `core_name`/`core_path` 改回 Xray，与测试前只差这两行；sing-box 二进制已删除。
- 公网 IP 恢复为空。临时用户（连同设备与 Token）、节点组与三个入站都已删除。面板对象数与测试前相同：入站 8、出站 2、链 1、规则 4、节点组 2、用户 2（`alice`、`bob`）。
- 原链恢复：`api.ipify.org` 从 `us2-node` 出口（23.254.158.3），`ifconfig.me` 直连（113.29.231.218），错误口令被拒绝。
- 共存服务：面板主机与 `uk-node` 上 `xboard-node`、`cf-probe` 的 PID 与测试前相同（738/740、833/834），没有失败的单元。第三台主机上 `komari-agent`、`cf-probe` 的 PID 与测试前相同（539、415），状态 `started`；`xboard-node` 保持测试前的 `crashed`。没有 nft 表 `inet proxy_agent_source`。
- 三台主机上的临时脚本、归档、客户端配置与证书都已删除。本地的临时证书与私钥也已删除。

**遗留问题。**

| # | 问题 | 影响 | 建议 |
|---|---|---|---|
| 1 | Agent 启动时，如果当前核心编译不了上一个成功修订，就整体退出：`internal/agent/control/control.go` 中 `manager.Restore` 的错误只有 `ErrReleaseRecovery` 与 `ErrCoreStopUnconfirmed` 两种会被容忍 | 本地换了核心，或者核心换成不支持某入站的版本以后，Agent 反复重启、连不上面板，面板也无法用新修订修复，只能上主机处理。换核心不是面板支持的操作，所以只在手工操作时会遇到 | **已在 v1.0.6 修复**：核心编译或启动不了保留修订时（新错误 `reconcile.ErrRuntimeRestore`），核心保持停止，Agent 继续建立控制连接，等面板下发新修订；重新应用保留修订失败时不再删除它的目录；supervisor 重试间隔 3 秒倍增到 1 分钟。完整性错误仍然退出。单元测试见 `internal/agent/reconcile/runtime_restore_test.go`、`internal/agent/control/startup_restore_test.go`、`internal/agent/supervisor/supervisor_test.go`；实机复测见 9.14 |
| 2 | sing-box SOCKS5 入站的远程 UDP、SOCKS5/SS 到 `us2-node` 的 UDP 中转、arm64 | 与 9.12 相同，受环境限制 | 未变 |

### 9.14 v1.0.6 发布与 Agent 启动恢复失败不再反复重启的实机复测（2026-10-24）

**发布。**
- 修复提交 `03b87d0`，`main` 上的 CI 为 `success`。标签 `v1.0.6` 指向该提交，标签的 CI 为 `success`。
- GitHub Release `v1.0.6` 于 2026-10-24T17:46:28Z 发布，正式版，不是草稿。附件有两个归档、`SHA256SUMS` 与 `node-install.sh`。amd64 归档的 SHA-256 为 `fc9f7368…a322`，arm64 为 `fcb7f2b7…40c2`。

**升级（各主机用已安装的 1.0.5 `qui-update --version 1.0.6 --yes` 从 GitHub 升级）。**

| 主机 | 结果 |
|---|---|
| `uk-node`（Debian，systemd） | Agent `1.0.5 → 1.0.6`，`active`，`NRestarts=0`，退出码 0；path 单元 `active` |
| 第三台主机 `alpine-node`（OpenRC） | Agent `1.0.6`，`started`，watcher `started`，退出码 0 |
| 面板主机 | Agent 与面板都到 `1.0.6`；面板更新前备份为 `pre-update-1.0.5-20261025-014812.db`；HTTPS 返回 200 |

- 三台主机都没有残留快照，也没有失败的单元。面板显示三个节点 `online`、Agent `1.0.6`。升级后原链 `ALP-UK-US2` 正常。

**复现 9.13 的场景（`alpine-node`，只使用 v1.0.6 发布物）。**
1. 从 GitHub 下载 v1.0.6 归档，按 `SHA256SUMS` 核对后解压。停 Agent，用发布版 `install.sh --node … --core sing-box --core-version 1.14.3 --core-sha256 41fc81fd…ab1f` 装上 musl 版 sing-box，把 `config.yaml` 的 `core_name`/`core_path` 改成 sing-box，再启动。面板显示 `sing-box 1.14.3`，修订 83 恢复正常。
2. 在 `alpine-node` 上新建临时入站 `v106-hy2-test`（Hysteria2，内部中转，`127.0.0.1:35270`，证书由面板生成），发布修订 84：`success`，节点上 UDP `127.0.0.1:35270` 在监听。
3. **不发布**，直接停 Agent、把 `config.yaml` 改回 Xray（与测试前的文件逐字节相同）、再启动（17:55:45）。保留的成功修订 84 含 Hysteria2，Xray 编译不了。

| 检查 | 结果 | 证据 |
|---|---|---|
| Agent 不再反复重启 | 通过 | 从 17:55:45 启动到恢复后（17:59:48 之后），Agent 一直是同一个 PID，supervise-daemon 没有重启它。v1.0.5 在同样情况下 4.5 分钟里重启了 53 次 |
| 启动日志 | 通过 | 启动时只有一条 `ERROR retained revision cannot start with the installed core; proxy remains stopped; control connection continues`，错误为 `unsupported inbound protocol`；不到 1 秒后 `node control connected` |
| 恢复重试的退避 | 通过 | `core recovery failed` 的 `retry_in` 从 3 秒开始倍增，17:57:33 到达 60 秒上限后只再记一次；之后每分钟尝试一次 |
| 面板显示 | 通过 | 节点 `degraded`，核心 `xray 26.3.27`，心跳 `core_healthy=false`、`core_error=core_unhealthy`，心跳时间持续更新（查询时 8 秒前） |
| 代理在此期间停止 | 符合设计 | 没有核心进程，`127.0.0.1:990` 拒绝连接（curl rc 7）；另外两台节点不受影响 |
| 用新修订修复 | 通过 | 删除 `v106-hy2-test` 并发布修订 85（17:59:37 发起），13 秒后 `success`。节点 `online`、`core_healthy=true`，Xray 在运行，PID 仍不变 |
| 恢复后的数据面 | 通过 | `api.ipify.org` 经链从 `us2-node` 出口（23.254.158.3），`ifconfig.me` 直连（113.29.231.218），错误口令被拒绝（curl rc 97） |
| 保留修订未被删除 | 通过 | 失败期间修订目录 84 一直存在，修复后修订目录为 77、80、83、84、85 |

**测试中发现的问题（未修复，见下文“遗留问题”第 1 项）。** 核心停止期间，supervisor 每 3 秒的 `Refresh`（按时间段规则重新渲染当前修订）不受恢复退避控制，每 3 秒记一条 `ERROR approved runtime state reconciliation failed: unsupported inbound protocol`（17:55:48～17:59:48 约 80 条）。

**恢复。**
- `alpine-node` 恢复 Xray 26.3.27：停 Agent，用发布版 `install.sh --core xray --core-version 26.3.27` 重装，安装记录第 5 行重新为 `xray`；sing-box 二进制已删除；`config.yaml` 与测试前逐字节相同。重启后恢复修订 85，没有错误或警告日志。
- 临时入站已删除。面板对象数与测试前相同：入站 8、出站 2、链 1、规则 4、节点组 2、用户 2。`alpine-node` 的公网 IP 保持为空。
- 原链恢复：`api.ipify.org` 从 `us2-node` 出口（23.254.158.3），`ifconfig.me` 直连（113.29.231.218），错误口令被拒绝。三个节点 `online`、核心健康。
- 共存服务：面板主机与 `uk-node` 上 `xboard-node`、`cf-probe` 的 PID 与测试前相同（738/740、833/834），没有失败的单元。第三台主机上 `komari-agent`、`cf-probe` 的 PID 与测试前相同（539、415），状态 `started`；`xboard-node` 保持测试前的 `crashed`。没有 nft 表 `inet proxy_agent_source`，三台主机都没有残留快照。
- 第三台主机上的临时脚本、归档与配置备份都已删除。

**遗留问题。**

| # | 问题 | 影响 | 建议 |
|---|---|---|---|
| 1 | `internal/agent/supervisor/supervisor.go` 每个 tick 先调用 `reconciler.Refresh`，再判断健康与退避。启动恢复失败后 `renderedDigest` 为空：渲染失败时每 3 秒记一条错误；如果修订能渲染、只是启动失败（例如端口被别的进程占用），`Refresh` 会每 3 秒调用一次 `Adapter.Apply` 重新启动核心，绕过退避 | Agent 仍保持连接，面板仍能修复，不影响本次修复的目标；但日志被刷屏，第二种情况下退避实际不生效。另外 `retry_in` 以纳秒整数记录（`3000000000`），不便阅读 | **已修复，v1.0.7 发布（实机复测见 9.16）**：每个 tick 先查核心健康，核心不健康时不调用 `Refresh`，只走退避后的 `Restore`；`Refresh` 只在核心健康时执行，同样的失败只记一次，恢复时记一条 Info；`retry_in` 记为 `3s` 这样的时长；出站探测失败只在由成功变为失败时告警。测试见 `internal/agent/supervisor/supervisor_test.go` 与 `internal/agent/reconcile/schedule_reliability_test.go` |
| 2 | sing-box SOCKS5 入站的远程 UDP、SOCKS5/SS 到 `us2-node` 的 UDP 中转、arm64 | 与 9.12 相同，受环境限制 | 未变 |

### 9.15 代码审查报告的五个问题：核实与修复（2026-10-24，v1.0.7 发布）

外部审查报告了五个问题。每个都先在独立的工作树里用临时测试复现（复现后删除），再修复。下表的“核实”列是复现结果。修复与 9.14 遗留问题 1 的修复一起在提交 `9980c86` 中，作为 v1.0.7 发布，实机复测见 9.16。

| # | 问题 | 核实 | 根因 | 修复 | 测试 |
|---|---|---|---|---|---|
| 1 | 删除用户或设备后，节点上尚未上传的流量批次永远无法确认 | 确认，且比报告更严重 | `internal/panel/user/traffic.go` 按当前 `users`/`devices` 表查身份，删除是硬删除并级联删掉凭据，于是返回 `identity_conflict`；`internal/panel/node/control.go` 因此结束控制连接且不确认，Agent 每次重连都重发同一个队首批次，后面的流量与访问记录也被堵住，队列写满后开始淘汰允许访问记录 | 新迁移 `0110_credential_tombstones.sql`：删除用户或设备时在同一事务里记录凭据墓碑。只有墓碑存在、且批次所属修订里确实有这个身份时才接受计数：推进游标保证重放幂等，只给仍存在的行计量（删除设备时用户仍计量）。没有墓碑或修订里没有这个身份仍是 `identity_conflict`。Agent 侧：同一队首批次连续被拒 3 次后进入隔离，保留在磁盘上、不被淘汰，后面的批次继续上传；隔离 1 小时后或 Agent 重启时重新尝试 | `internal/panel/user/traffic_test.go`、`internal/panel/access/traffic_service_test.go`、`internal/agent/spool/traffic_quarantine_test.go`、`internal/agent/control/traffic_delivery_test.go` |
| 2 | 回滚到旧修订会恢复已禁用用户的凭据，且不会自动纠正 | 确认 | 回滚（`coordination.go`）与部署旧草稿都原样下发存储的状态；额度状态机只在用户状态变化时触发，tick 只在启动时做全量对比，所以只有面板重启才会纠正。反过来，回滚也会把已重新启用的用户锁在外面 | 每次部署或回滚成功后，部署模块回调平台，为该节点排队一次额度对账（沿用 60 秒合并窗口与忙碌处理）。节点已一致时对账不产生修订，自动修订自身的成功不会形成连锁 | `internal/panel/deployment/applied_test.go`；`internal/platform/server/quota_test.go` 的回滚、旧草稿部署与反向用例 |
| 3 | 启动对账删除“用户接入”快捷功能发布的有效凭据 | 确认，且范围更大 | 快捷功能手工拼出整份修订（合成入站 `user-inbound`），业务数据里没有对应的入站；自动修订从入站业务数据渲染用户凭据，因此启动对账、续期、任何用户的额度变化都会删掉这些凭据并停用该入站，Owner 正常发布也会删掉它；快捷功能本身还会覆盖该节点的其他入站与出站 | 快捷功能改为通过代理模块写入（或复用同一监听的）真实入站，并用与 `POST /revisions/proxy` 相同的渲染器生成修订，保留该节点的其他配置。原始 `POST /api/v1/revisions` 拒绝携带用户凭据的状态（400 `unmanaged_user_credentials`），配置版本页面去掉了手工填写用户名和密码的模式 | `internal/panel/deployment/user_access_test.go`；`internal/platform/server/quota_test.go` 的启动对账、续期、禁用后重新启用、旁观用户与 Owner 发布用例 |
| 4 | 节点重连后，核心更新的成功结果被静默忽略 | 确认 | 面板在收到 Hello 后、Agent 发来新能力之前就按缓存的旧核心版本关闭任务（`failed_closed`）；随后的成功结果只更新 `queued`/`running`，影响 0 行却仍返回成功并确认。另外 Agent 每次重连都发送进程启动时检测的能力，核心更新成功后重连会把面板上的核心版本改回旧值 | 面板在本次连接存下第一条能力消息之后才关闭过期任务；结果处理也接受被重连关闭且没有结果的任务，并检查影响行数，0 行时返回错误而不确认。Agent 在核心更新成功或回滚后重新检测能力，之后的连接都用新值 | `internal/panel/node/core_capabilities_test.go`、`internal/panel/deployment/core_updates_test.go`、`internal/agent/control/capabilities_test.go`、`internal/agent/control/core_update_worker_test.go` |
| 5 | 节点详情页的过期响应覆盖当前节点，操作发到错误的节点 | 部分确认：代码里存在竞态并已复现，但现有界面没有从一个节点详情直接跳到另一个节点详情的入口，目前触发不了 | `NodeDetailPage.vue` 的 `load()` 不检查响应是否过期，同一组件实例跨节点复用，吊销与设置使用已加载节点的 ID；`UserDetailPage.vue` 同样 | 两个详情页按请求序号与路由 ID 丢弃过期响应，切换 ID 时清空数据并关闭对话框，提交前核对目标 ID；`MainLayout.vue` 按 `route.path` 给路由组件加 key，参数变化时得到新实例（也覆盖了不监听参数的编辑页） | 无前端测试框架；用编译后的真实组件脚本与模拟 API 复现与验证，`npm --prefix web run build` 通过 |

一并修复的较小问题（9.5 观察项与 9.14 遗留问题 1）见对应条目。

**门禁。** `gofmt -l` 无输出，`go vet ./...` 通过，`go test -p 1 -count=1 ./...` 全部 49 个包通过，`npm --prefix web run build`（含 `vue-tsc`）通过。

**实机复测。** 已在 v1.0.7 上完成，见 9.16：supervisor 端口被占用场景、删除用户后的流量上传、回滚后的自动纠正、快捷用户接入在面板重启后的保留。

### 9.16 v1.0.7 发布、三台主机升级与四项修复的实机复测（2026-10-25）

**发布。**
- 修复提交 `9980c86`（9.15 的五个问题与 9.14 遗留问题 1）在 `main` 上的 CI 失败：`internal/platform/server` 有六个额度测试在 `-race` 下失败（五个快捷接入测试部署时返回 409 `deployment_conflict`，`TestQuotaDeployFailureKeepsTheEntryForTheNextWindow` 得到 2 个修订）。本地 Windows 不带 `-race` 的全量测试通过。CI 日志需要仓库管理员权限，因此用 Zig 交叉编译 `-race` 测试二进制，在面板主机的临时目录里以 `nobody` 运行，复现了同样的六个失败；其余 12 个改动过的包在 `-race` 下通过。
- 原因在测试，不在产品代码：每次部署成功后会排队一次额度对账，而测试夹具的发布与快捷接入辅助函数没有等它执行完。在 `-race` 下变慢后，这次对账在测试的下一步中途触发：要么用自动修订占住节点（409），要么对测试刚设置的超额用户再生成一个修订。修复提交 `f87ffdf`：额度部署器记录仍在对账的 flush 数，夹具在准备性部署之后等节点没有排队、计时或进行中的对账再继续。修复后该包在 Linux `-race` 下通过（1351 秒），`main` 上的 CI 为 `success`。
- 标签 `v1.0.7` 指向 `f87ffdf`，标签的 CI 为 `success`。GitHub Release `v1.0.7` 于 2026-10-25T11:37:03Z 发布，正式版，不是草稿。附件有两个归档、`SHA256SUMS` 与 `node-install.sh`。amd64 归档的 SHA-256 为 `ae83a06d…d07c`，arm64 为 `ef96e55e…c621`。
- 面板主机上的临时测试目录已删除，`/root` 恢复为 0700。

**升级（各主机用已安装的 1.0.6 `qui-update --version 1.0.7 --yes` 从 GitHub 升级）。**

| 主机 | 结果 |
|---|---|
| `uk-node`（Debian，systemd） | Agent `1.0.6 → 1.0.7`，`active`，`NRestarts=0`，退出码 0；path 单元 `active` |
| 第三台主机 `alpine-node`（OpenRC） | Agent `1.0.7`，`started`，watcher `started`，退出码 0 |
| 面板主机 | Agent 与面板都到 `1.0.7`（含迁移 `0110_credential_tombstones.sql`）；面板更新前备份为 `pre-update-1.0.6-20261025-194149.db`；HTTPS 返回 200 |

- 三台主机都没有残留快照，也没有失败的单元。面板显示三个节点 `online`、Agent `1.0.7`。升级引起的面板重启没有产生自动修订（最新仍为 85）。原链 `ALP-UK-US2` 正常。
- 两台 systemd 主机的 `/var/lib/proxy-agent` 与 `/var/lib/proxy-panel` 升级后为 0700（`StateDirectoryMode=0700`）。

**测试前的基线。** 面板对象：入站 8、出站 2、链 1、规则 4、节点组 2、用户 2；三个节点 `online`、Xray 26.3.27。`api.ipify.org` 经链从 `us2-node` 出口（23.254.158.3），`ifconfig.me` 直连（113.29.231.218），错误口令被拒绝（curl rc 97）。共存服务的 PID：面板主机与 `uk-node` 上 `xboard-node`、`cf-probe` 为 738/740、833/834；第三台主机上 `komari-agent`、`cf-probe` 为 539、415，`xboard-node` 为 `crashed`。

**复测 1：核心起不来时 supervisor 走退避（9.14 遗留问题 1，`alpine-node`）。** 停 Agent，用 `socat` 占住入站端口 `127.0.0.1:990`（11:43:59），再启动 Agent；保留的修订能渲染但 Xray 起不来。4 分钟后（11:48:05）结束 `socat`。

| 检查 | 结果 | 证据 |
|---|---|---|
| 启动 | 通过 | 只有一条 `retained revision cannot start with the installed core; proxy remains stopped; control connection continues`（`Xray startup or health check failed`），1 秒后 `node control connected` |
| 恢复只走退避 | 通过 | 端口被占期间的恢复尝试在 11:44:02、:08、:17、:32、:59、11:45:50、11:46:53、11:47:56，共 8 次，间隔 6、9、15、27、51 秒后到达 1 分钟上限。v1.0.6 的 `Refresh` 会每 3 秒重新启动一次核心 |
| 日志 | 通过 | `core recovery failed` 只记 2 条：第一次失败（`retry_in`=`3s`）与到达上限（`retry_in`=`1m0s`）；没有任何 `approved runtime state reconciliation failed` |
| Agent 不重启 | 通过 | 从启动到恢复后 Agent 一直是同一个 PID |
| 端口释放后恢复 | 通过 | 11:48:59 的下一次尝试成功，Xray 运行，节点 `online`；原链、直连、错误口令三项与基线相同；修订目录 77、80、83、84、85 都在 |

**复测 2～4 的准备。** 新建临时用户 `t107`（只有 SOCKS5，授权节点组“UK entry”，含 `alpine-node` 与 `uk-node`）与一台设备。现有入站的允许用户列表都为空（表示该组的所有授权用户），授权本身不触发自动修订。

**复测 4：快捷用户接入在面板重启与 Owner 发布后保留（9.15 问题 3，`uk-node`）。**

| 检查 | 结果 | 证据 |
|---|---|---|
| 写入真实入站 | 通过 | `POST /api/v1/revisions/user-access`（SOCKS5 `0.0.0.0:35190`）后入站数 8 → 9，名称为 `User access socks5`，绑定 `alice` 与 `t107`；修订 86 `success`；`t107` 经 `195.72.166.247:35190` 访问 `api.ipify.org` 得到 UK 地址 |
| 第一次重启面板（11:54:30） | 通过 | `t107` 仍可用。启动对账为 `alpine-node` 生成修订 87（`auto: startup reconcile`）：授权 `t107` 后 `alpine-socks-990` 的用户面已变化但从未发布，对账把它补上，`t107` 随后能经 `127.0.0.1:990` 访问。这是对账应做的事；`uk-node` 没有修订 |
| 第二次重启面板（11:58:31） | 通过 | 一切已一致，150 秒内没有任何新修订；`t107` 仍可用 |
| Owner 正常发布 | 通过 | `POST /api/v1/revisions/proxy` 生成修订 88，`success`，`35190` 仍在监听，`t107` 仍可用；随后的部署后对账没有产生修订。v1.0.6 的这次发布会删掉快捷接入的入站 |

**复测 3：回滚到禁用前的修订后自动纠正（9.15 问题 2，`uk-node`）。**

| 时间（UTC） | 事件 |
|---|---|
| 12:03:08 | 禁用 `t107` |
| 12:04:08 | 修订 89（`alpine-node`）与 90（`uk-node`）`auto: user t107 disabled`，都 `success`；`t107` 被拒绝（curl rc 97） |
| 12:04:43 | `POST /api/v1/nodes/{uk}/rollback`，回滚到修订 88（`t107` 仍启用） |
| 12:04:50 | 旧修订生效，`t107` 又能访问 |
| 12:05:08 | 修订 91 `auto: post-rollback reconcile; user t107 disabled`，`success` |
| 12:05:24 | `t107` 再次被拒绝（curl rc 97） |

- 判定：通过。纠正在回滚成功后约 18 秒创建（合并窗口内），修订 91 自身成功后没有再产生修订。
- 随后重新启用 `t107`：修订 92（`uk-node`，`auto: post-deploy reconcile; user t107 reactivated`）与 93（`alpine-node`）恢复凭据；经 `uk-node` 下载 5 MB 后 `usedTraffic` 为 5 061 573，计量正常。

**复测 2：节点离线期间删除用户，删除前的流量与访问记录照常上传（9.15 问题 1，`uk-node`）。**
1. 12:08:27 在 `uk-node` 上加临时 nft 表 `inet qui_test107`，只丢弃发往面板控制端口 `23.254.158.3:9443` 的包。
2. 12:08:29 经 `uk-node` 用 `t107` 下载 20 MB 并访问 3 次 `api.ipify.org`。面板显示 `uk-node` `offline`（最后心跳 12:08:24），`t107` 的 `usedTraffic` 仍为 5 061 573，流量留在 Agent 的队列里。
3. 12:09:17 删除 `t107`（204）。
4. 12:09:48 删除 nft 表。

| 检查 | 结果 | 证据 |
|---|---|---|
| 重连后不再被拒 | 通过 | Agent 在 12:09:55 `node control connected`，之后一直保持连接；Agent 日志没有 `quarantin`、`rejection`、`FailedPrecondition`，面板日志没有 `identity_conflict` 或拒绝记录。v1.0.6 在这里会对队首流量批次返回 `identity_conflict` 并结束连接，每次重连都重复 |
| 后面的记录没有被堵住 | 通过 | 离线期间（12:08 时间桶，修订 92，用户 `t107`）的访问记录 `speed.cloudflare.com` 与 `api.ipify.org`（3 次连接）在删除用户之后入库，访问记录总数 14 → 18 |
| 节点状态 | 通过 | `uk-node` 重新 `online`，核心健康 |

**观察（不是回归）。** 删除用户不会自动发布修订：删除后 `t107` 在 `uk-node` 与 `alpine-node` 上仍能访问，直到 Owner 重新发布（下文恢复步骤里的修订 94、95）。删除处理与 v1.0.6 相同，用户列表的删除确认框也写明“节点配置需重新发布以撤销既有接入”。禁用会自动撤销，删除不会。（之后已在 `main` 修复，尚未发布：删除用户或设备后自动发布移除其凭据的版本，见下文遗留问题第 1 项。）

**恢复。**
- 删除快捷接入入站，Owner 发布 `uk-node`（修订 94）与 `alpine-node`（修订 95），都 `success`；之后 75 秒内没有自动修订。`uk-node` 不再监听 `35190`（curl rc 7），`alpine-node` 拒绝 `t107`（curl rc 97）。
- 面板对象数与测试前相同：入站 8、出站 2、链 1、规则 4、节点组 2、用户 2。三个节点 `online`、Agent 1.0.7、核心健康。`alpine-node` 的公网 IP 保持为空。
- 原链恢复：`api.ipify.org` 从 `us2-node` 出口（23.254.158.3），`ifconfig.me` 直连（113.29.231.218），错误口令被拒绝。面板主机上对 `uk-node` 的数据面脚本：SOCKS5、HTTP、VLESS、Shadowsocks、Trojan 直连都从 UK 出口，三条阻断规则生效（curl rc 35），GitHub 直连正常，错误口令被拒绝（rc 97）。脚本里“经中转到 US”的三行现在也从 UK 出口，与当前规则一致：`uk-node` 上已没有这些域名的中转规则，只剩两条阻断、GitHub 直连与 `alpine-node` 的链规则。
- 共存服务：面板主机与 `uk-node` 上 `xboard-node`、`cf-probe` 的 PID 与测试前相同（738/740、833/834），没有失败的单元。第三台主机上 `komari-agent`、`cf-probe` 的 PID 与测试前相同（539、415），状态 `started`；`xboard-node` 保持测试前的 `crashed`。三台主机都没有 nft 表 `inet proxy_agent_source` 与 `inet qui_test107`，没有残留快照，没有残留的 `socat`。
- 三台主机上的临时脚本、凭据文件与日志都已删除。

**遗留问题。**

| # | 问题 | 影响 | 建议 |
|---|---|---|---|
| 1 | 删除用户或设备不会自动撤销节点上的凭据（见上文观察） | 已删除用户在下一次发布前仍能使用代理；界面已提示需要重新发布 | owner 决定改为自动撤销。**已在 `main` 修复，尚未发布**：删除成功后为已发布状态里仍持有该用户凭据的节点排队一次自动修订（`auto: user <name> deleted` 或 `device deleted`，发出 `user_access` 告警），删除用户时同时从额度状态机移除该用户；用户与设备的删除确认框改为说明会自动撤销。测试见 `internal/platform/server/quota_test.go`（删除用户、只删一台设备、授权已移除后删除、没有已发布凭据时不产生修订）与 `internal/panel/user/handler_delete_test.go`（删除失败时不撤销）。代码审查发现草稿渲染不了时退回已发布状态的路径只禁用整个用户，删除活跃用户的一台设备不会撤销；已改为同时移除数据库里已不存在的凭据（`TestQuotaDeletedDeviceAfterInvalidDraftRevokesIt`）。发布新版本后需要实机复测 |
| 2 | sing-box SOCKS5 入站的远程 UDP、SOCKS5/SS 到 `us2-node` 的 UDP 中转、arm64 | 与 9.12 相同，受环境限制 | 未变 |
| 3 | 代码审查：恢复较旧的备份会丢掉备份之后产生的凭据墓碑（v1.0.7 引入） | 备份后创建又删除的用户或设备，删除前产生的流量在恢复后再次因身份未知被拒（隔离只让后面的批次继续上传） | **已在 `main` 修复，尚未发布**：恢复时合并当前数据库的 `credential_tombstones`（`TestRestoreKeepsTombstonesCreatedAfterTheBackup`） |
| 4 | 代码审查：核心更新成功后重连，面板拒绝有效的成功回执（v1.0.7） | 重连时按上报的核心版本把任务标成 `success` 但没有结果，随后到达的回执更新不到任务，控制连接被断开 | **已在 `main` 修复，尚未发布**：没有结果的 `success` 任务同样接受经过认证的回执，Owner 清除的任务仍然拒绝（`TestCoreUpdateClosedAsSuccessStillAcceptsTheReceipt`） |

### 9.17 v1.0.8 发布前验收（2026-10-25）

范围：9.16 遗留问题 1、3、4 的修复（提交 `7b99d8e`），以及本轮实机回归中发现并修复的备份问题（提交 `8608678`）。两个提交都已推送到 `main`，未打标签、未发布。

**提交前检查（Windows 本机，两次提交各做一遍）。**

- 工作区只含对应的修复、测试与文档；`go.mod`、`go.sum` 与已有迁移未改动。
- `gofmt -l` 无输出，`go vet ./...` 通过，`go test -p 1 -count=1 ./...` 49 个包全部通过，前端构建通过。
- 变异检查：分别撤掉每项修复后，对应测试失败：
  - 草稿回退路径：`TestQuotaDeletedDeviceAfterInvalidDraftRevokesIt` 超时，没有产生撤销修订。
  - 删除钩子：`TestQuotaDeletedUserRevokedOnNode` 与 `TestQuotaDeletedDeviceRevokesOnlyThatDevice` 超时。
  - 墓碑合并：`TestRestoreKeepsTombstonesCreatedAfterTheBackup`，恢复后剩 0 条墓碑。
  - 回执接受：`TestCoreUpdateClosedAsSuccessStillAcceptsTheReceipt`，回执被拒。
  - WireGuard peer 移除：`TestWithoutCredentials…` 失败。
  - 备份校验：`TestClearedCoreUpdateKeepsStoredResultAndStillBacksUp`，备份校验失败。
  - 清除守卫：`TestCoreUpdateClearKeepsStoredResult`，清除改写了已有结果的任务。

  恢复修复后全部通过。新增测试各连续运行 10 次全部通过。

**CI。** GitHub Actions `Check` 运行 40205812791（`7b99d8e`）与 40212630199（`8608678`）结论都是 success，包括 `go test -race -timeout 35m ./...`、`go vet`、前端构建与测试、部署脚本检查。可选的 `cores` 作业只在手动或定时触发时运行，两次都跳过。

**测试环境。**
- 面板主机先后安装 `7b99d8e` 与 `8608678` 的构建（版本号 `1.0.8-rc.<提交>`）。安装方式是仓库的 `install.sh --panel --binary`，用发布包的目录结构；第一次安装前保存了 1.0.7 的程序与停止状态下的数据库。
- 三台主机的 Agent 保持 1.0.7。
- 测试前基线：
  - 面板对象：入站 8、出站 2、链 1、规则 4、节点组 2、用户 2、设备 2，最新修订 95；
  - 链路：原链从 `us2-node` 出口（23.254.158.3），直连 113.29.231.218，错误口令被拒绝（rc 97）；
  - 共存服务 PID：面板主机与 `uk-node` 上为 738/740、833/834；第三台主机 `komari-agent`、`cf-probe`、`crond` 均为 `started`。
- 临时测试用户：`t108`（两台 SOCKS5 设备 a、b）与 `t108r`（一台设备），授权“UK entry”。

**回归 1、2：草稿无法渲染时删除设备，只撤销该设备（`7b99d8e`，`uk-node`）。**

| 时间（UTC） | 事件 |
|---|---|
| — | 修订 96（`uk-node`）、97（`alpine-node`）发布 `t108`；两台设备经 `195.72.166.247:35101` 都能访问，错误口令被拒绝 |
| — | 在 `uk-node` 上保存出站 `ft108-dummy`（HTTP，`127.0.0.1:9`）与只含它的主备出口组 `ft108-group`，再把该出站改为不支持 TCP。手工发布 `uk-node` 返回 400 `group member unavailable or capability changed`，草稿已无法渲染 |
| 15:53:36 | 面板日志 `automatic quota revision rendered from the published state alone`（部署后对账走回退，结果与已发布状态相同，没有修订） |
| 15:53:47 | 删除设备 a（204） |
| 15:53:53 | 修订 98（`alpine-node`，草稿能渲染）`auto: post-deploy reconcile; user t108 device deleted`，success |
| 15:54:36 | 回退路径生成修订 99（`uk-node`），摘要同上，success |
| 之后 | 设备 a 被拒绝（rc 97），设备 b 照常从 UK 出口，错误口令被拒绝 |

- 判定：通过。在修复前，回退路径只禁用整个用户，设备 a 会一直可用。
- 删除两个临时对象后草稿恢复可渲染，之后没有回退日志，也没有新修订。

**回归 3：恢复备份保留备份之后的墓碑（`8608678`）。**
1. 16:45:34 创建备份 B（`ZQLg1iFD…`，归档 SHA-256 `9dba0c5e…3a77`）。同一数据库在 `7b99d8e` 上无法备份，见下文问题 A。
2. 备份之后创建 `t108r` 并发布 `uk-node`（修订 100）；经 `uk-node` 下载 3 MB，`usedTraffic` 为 3 013 262。
3. 16:46:43 删除 `t108r`。16:46:54 生成修订 103（`alpine-node`）、104（`uk-node`），success，`t108r` 被拒绝（rc 97）。
4. 当前数据库有 3 条墓碑，其中 1 条的凭据、用户、设备与 `t108r` 完全一致；没有待执行的部署。
5. 恢复预览：没有移除的用户，没有重新启用的用户，没有待执行操作。提示项为 `session_revocation_required`、`backup_file_journal_reconciliation`、`node_reconciliation_required`、`message_cursor_regression`。
6. 16:47:53 `POST /api/v1/backups/{B}/restore`（202）。面板在进程内切换：16:48:04 停止，16:48:11 以恢复后的数据库提供服务，恢复记录为 `serving`，管理员会话被清除。

| 检查 | 结果 | 证据（恢复后数据库的只读快照） |
|---|---|---|
| 墓碑保留 | 通过 | 墓碑共 3 条（备份 B 自带 2 条），`t108r` 的那条在 |
| 删除的用户没有回来 | 通过 | `users`、`credentials` 中没有 `t108r` |
| 修订与节点 | 通过 | 修订保留到 104，三个节点 `online`，恢复没有产生修订，面板日志没有 identity 相关记录 |

- 限制：“恢复之后才到达的旧流量批次被接受”这一步无法在实机上制造。恢复要求没有待执行的部署，而删除后面板会立即为离线节点排队撤销版本，这个版本在节点重新连接前一直处于待执行状态，所以恢复与“节点离线、流量还没上传”无法同时成立。该步骤由 `TestRestoreKeepsTombstonesCreatedAfterTheBackup` 覆盖。

**回归 4、5：核心更新回执（`uk-node`，两种构建各做一遍）。**

方法：
- 在 `uk-node` 上用临时 nft 表 `inet qui_test108`：一条规则按 cgroup 只限制 Agent 的 HTTPS 下载速度（80 或 150 KB/s）；另一条在下载开始后对发往面板 `23.254.158.3:9443` 的连接回 TCP RST。
- 更新完成后再放行，因此 Agent 在没有控制连接时得到结果，重新连接后先上报新能力，再送出回执。
- 面板在保存这个连接的第一条 Capabilities 时调用 `CloseStaleCoreUpdates`，会先把任务标成 `success`、结果为空，所以随后到达的回执走的正是修复的路径。

| 回归 | 构建 | 过程 | 结果 |
|---|---|---|---|
| 4 | `7b99d8e` | 26.3.27→26.2.6；16:07:26 下载开始并断开控制，约 16:11 更新完成，16:17:50 放行，16:19:27 重连 | 通过：任务 `success`，保存了回执（`installed_version` 26.2.6，序号 1），连接没有断开 |
| 5 | `7b99d8e` | 26.2.6→26.3.27；16:22:32 断开，16:22:40 任务为 `running` 时 Owner 清除（`cleared`），16:25:23 放行 | 通过：16:26:03 重连后面板回 `FailedPrecondition: core update result rejected` 并结束该连接，任务仍为 `cleared`、没有结果；16:26:33 再次连接后一直在线 |
| 5（复测） | `8608678` | 26.3.27→26.2.6，16:50:56 清除 | 通过：16:54:24 回执被拒，任务仍为 `cleared`、没有结果，16:54:54 重连后在线 |
| 4（复测） | `8608678` | 26.2.6→26.3.27，16:55:55 断开，16:58:41 放行 | 通过：16:59:56 重连，任务 `success`，回执 `installed_version` 26.3.27，连接没有断开 |

- `uk-node` 最终运行官方 Xray 26.3.27：Agent 管理的镜像 `releases/installed/xray-8255dd93….bin`，大小与原来的 `/usr/local/lib/proxy-agent/xray` 相同。
- 测试工件（不是产品问题）：放行后的第一次重连有时仍收到 `connection refused`，30 秒后的下一次重试成功。

**本轮发现的问题。**

| # | 问题 | 影响 | 处理 |
|---|---|---|---|
| A | Owner 清除一个已保存节点结果的核心更新任务后，面板再也无法创建备份（P2，早于 v1.0.8，自核心更新“清除”功能起就存在） | 一个 `failed_closed` 任务带着 Agent 的 `failed_closed`/`history_invalid` 结果，被清除后状态是 `cleared`，结果仍是 `failed_closed`。备份校验要求结果状态与任务状态一致，每次 `POST /api/v1/backups` 都返回 `backup_verification_failed`。测试面板上这个任务清除于 2026-10-22 10:05，此后的手工备份全部失败（最后一个成功的备份是同日 09:42 的 `4N3rU_Fo…`）。`qui-update` 的更新前备份是直接复制 `.db`，不受影响 | 已在 `8608678` 修复：清除不再改写已有结果的任务（UPDATE 带 `result_json IS NULL`）；备份校验接受已存在的这类记录。实机：同一数据库在 `8608678` 上创建备份 B 成功并通过校验 |
| B | 部署繁忙时自动对账留下没有目标节点的草稿修订（修订 101 `auto: startup reconcile`，`draft`） | 只在修订列表里多一条草稿，下一个窗口另建了修订 104，节点状态正确 | 观察，未修改；与 v1.0.7 相同 |

**恢复。**
- 删除 `t108`：17:02:11 生成修订 105（`uk-node`）、106（`alpine-node`）`auto: user t108 deleted`，success，两台设备都被拒绝。
- 面板重新安装 1.0.7 程序。v1.0.7 之后没有新迁移，`deploy/` 也没有变化。面板 `active`、`NRestarts=0`，没有失败的单元，重启后没有新修订。
- 面板对象数与测试前相同：入站 8、出站 2、出口组 0、链 1、规则 4、节点组 2、用户 2、设备 2。三个节点 `online`、Agent 1.0.7。
- 原链、直连、错误口令三项与基线相同。共存服务 PID 与测试前相同（738/740、833/834），第三台主机上 `komari-agent`、`cf-probe`、`crond` 为 `started`。`uk-node` 的 Agent PID 全程未变。
- 临时文件全部删除：面板主机 `/root/qui-rc108`（含数据库副本与测试凭据）、`uk-node` 的脚本与日志、本机的构建与凭据文件。两台主机上没有 `inet qui_test108` 表，没有更新快照。`/root`、`/var/lib/proxy-panel`、`/var/lib/proxy-agent` 为 0700。

**留下的差异。**
- 面板数据库是从备份 B 恢复后的库（备份 B 之后的数据都按恢复规则合并回来了）。
- 修订历史增加到 106。
- 核心更新任务列表增加 4 条（2 条 `cleared`、2 条 `success`）。
- 备份列表增加备份 B。
- `uk-node` 的核心改为由 Agent 管理的同版本镜像。
- 测试面板仍有问题 A 的那条旧记录。1.0.7 上备份仍会失败，升级到 v1.0.8 后恢复正常。

**结论。** 五项回归全部通过，CI 通过，没有未解决的 P1/P2；v1.0.8 可以发布，等待 owner 批准打标签与 GitHub Release。（后续：同日又收到两个 P2 审查问题，修复后候选改为 `1663e4a`，见 9.18。）

### 9.18 v1.0.8 发布前复验：核心更新结果重发与入站启用意图（2026-10-26 UTC）

范围：代码审查的两个 P2，修复在提交 `1663e4a`（已推送到 `main`，未打标签、未发布）。
- **问题 1**：控制连接在等 Ack 时断开，Agent 的核心更新结果随会话丢失。修复后结果留在 core-update worker 里，由下一个会话重发，直到面板确认；同一结果被永久拒绝 3 次后丢弃并记 Warn。
- **问题 2**：重新启用用户时，自动版本会启用一个只在草稿里启用的入站。修复后每个版本记录 Owner 启用的入站集合（迁移 `0111_inbound_enabled_intent.sql`），自动版本只重新启用记录里的入站。

本节同时重跑 9.17 的五项回归。

**提交前检查（Windows 本机）。**
- 只改动修复、测试与文档；`go.mod`、`go.sum` 与已有迁移未改动，新增迁移 0111。
- `gofmt -l` 无输出，`go vet ./...` 通过，`go test -p 1 -count=1 ./...` 49 个包全部通过。
- 变异检查，分别撤掉一项修复，对应测试都失败：
  - 发送失败时丢弃结果：4 个 Agent 测试失败；
  - 不识别永久拒绝：2 个测试失败；
  - 撤掉额度重新启用条件：3 个测试失败；
  - 批量发布不记录意图：`TestBatchRevisionRecordsInboundIntent` 失败。
- 新增测试各连续运行 10 次全部通过。

**CI。** GitHub Actions `Check` 运行 40251098078（`1663e4a`）结论为 success，包括 `go test -race -timeout 35m ./...`、`go vet`、前端构建与测试、部署脚本检查；`cores` 与 `release` 作业跳过。

**测试环境。**
- 面板主机安装 `1663e4a` 的构建（`1.0.8-rc.1663e4a`），方式与 9.17 相同；安装前保存了 1.0.7 程序与停止状态下的数据库。启动时执行迁移 0111，已有的 106 个修订的 `inbound_enabled_json` 都为 NULL。
- `uk-node` 的 Agent 按测试需要在 1.0.7 与 `1.0.8-rc.1663e4a` 之间切换（`install.sh --node --binary`），另两台保持 1.0.7。
- 测试前基线与 9.17 结束时相同：
  - 面板对象：入站 8、出站 2、出口组 0、链 1、规则 4、节点组 2、用户 2，最新修订 106，三个节点 `online`；
  - 链路：原链从 23.254.158.3 出口，直连 113.29.231.218，错误口令被拒绝（rc 97）；
  - 共存服务：PID 738/740、833/834，第三台主机 `komari-agent`、`cf-probe`、`crond` 为 `started`。

**问题 1：核心更新结果重发（`uk-node`，Xray 26.3.27 与 26.2.6 来回切换）。**

方法：在 `uk-node` 上建临时 nft 表 `inet qui_test108b`。
- 按 cgroup 把 Agent 的 HTTPS 下载限到 150 KB/s。
- 归档下载完成后，第一个发往面板 `23.254.158.3:9443`、长度为 573–580 字节的包，把面板地址加入动态集合 `@hole`。依据：空闲时与重连时的包长记录（另一张只记录 IP 头的 `log` 表），结果消息约 575–577 字节，心跳 490–545、遥测 831/832、Hello/Capabilities 约 302–318。
  - `both` 模式：从这个包起双向丢弃，即“发送失败”；
  - `in` 模式：只丢弃面板发来的包，面板收到结果但 Ack 回不来，即“Ack 丢失”。
- 更新结束 45 秒后删表放行。
- 包长来自 IP 头；内容是 TLS，看不到消息类型。

| 运行 | Agent | 方式 | 结果 |
|---|---|---|---|
| 0a | 1.0.7（面板仍是 1.0.7） | 下载接近完成时双向黑洞 | 不作数：重连后回执被 1.0.7 面板拒绝（缺少 `7b99d8e`），任务 `success`、无结果。随后升级面板 |
| 0b | 1.0.7 | 同上（按时间触发） | 回执在重连后送达。`ss` 采样显示断开时会话正卡在另一条消息的发送上，结果还在通道里，没进入会话，所以按时间触发碰不到问题窗口 |
| 回归 4 | 候选 | 下载开始即对控制端口回 RST，直到更新结束 | 通过：重连后先发结果，再发 Capabilities，回执 `installed_version` 26.3.27 入库，连接不再断开 |
| S1 | 候选 | `both` | 01:35:08.83 更新结束，0.3 秒后 575 字节的包触发黑洞，01:35:19 会话超时（`connection timed out`），01:35:55 重连，回执 26.2.6 入库 |
| S2a | 候选 | `in`（下载中途就武装） | 不作数：触发的是下载途中一个 581 字节的无关消息，结果在断线期间产生，等同回归 4；回执照常入库 |
| S2b | 候选 | `in`（下载完成后武装） | 通过：01:45:48.76 更新结束，01:45:49.069 576 字节的包触发，面板 70 毫秒后（.139）存下回执，Ack 被丢弃，会话 01:45:59 超时。01:46:34 重连后依次为 Hello（311）、Capabilities（312）、577 与 312（结果收到 Ack 后才发的能力刷新）、第一个心跳（504）。这就是重发，面板对相同结果确认，连接稳定 |
| S0 | 1.0.7 | `both` | 无法判定：放行时任务仍为 `running`，重连后回执照常送达。被丢弃的包可能是同样大小的另一条消息，会话卡在它上面，结果没被取走 |
| S0b | 1.0.7 | `in` | 对照：面板在第一次发送时存下回执，Ack 丢失后会话超时。重连后为 Hello（308）、Capabilities（312），接着是心跳与遥测，**没有**心跳之前的“结果＋能力刷新”，即 1.0.7 不重发 |
| B（回归 5） | 候选 | 下载中 Owner 清除任务（02:10:38，`cleared`） | 通过：更新完成后面板三次回 `FailedPrecondition: core update result rejected`（02:12:49、:51、:53），第三次时 Agent 记 `core update result dropped after repeated rejection`（`attempts` 3，带任务 ID），02:12:58 重连后一直在线；任务仍为 `cleared`、没有结果 |
| S1b | 候选 | `both` | 02:18:40.99 更新结束，0.32 秒后 577 触发，放行时任务为 `running`（面板没收到）。02:19:36 重连后依次为 Hello、Capabilities、576（面板 70 毫秒后存下回执 26.2.6）、311，然后才是心跳 |

- 判定：通过。
  - Ack 丢失：S2b 与 S0b 对照，说明修复生效。两次面板都已存下回执，只有候选在新会话里先重发结果，面板按相同结果确认。
  - 发送失败：S1、S1b 的回执都在重连后入库，没有一次丢失。
  - 永久拒绝：B 说明 3 次后丢弃，节点不再为同一个结果反复重连。
- 限制：
  - 重连附近两个版本都会出现另一对 576/577＋312 字节的包（S2b 在 +4 秒，S0b、S1b 也有），无法从包长识别它是什么。面板只保存每个节点最后一条已接受消息的摘要，也没法逐条计数。
  - 因此“会话已取走结果、随后发送失败”只在 Ack 丢失的对照里直接证实；发送失败这条路径的同一交错由 `TestSessionResendsCoreUpdateResultAfterSendFailure` 覆盖。
  - 1.0.7 在实机的各次断线里回执也都送达了：原问题要求结果正好被一个随即断开的会话取走，窗口很窄，S0 没有碰到。
- 最后用一次正常更新把 `uk-node` 的核心换回 26.3.27（02:21:44，success），运行 Agent 管理的镜像 `xray-8255dd93….bin`，与测试前相同。

**问题 2：自动版本不启用 Owner 禁用的入站（`uk-node`）。**

临时用户 `t108i`（一台 SOCKS5 设备，授权“UK entry”），临时入站 `ft108-intent`（SOCKS5，`0.0.0.0:35120`，只允许 `t108i`）。“意图”一列表示该修订的 `inbound_enabled_json` 是否包含这个入站。

| 修订 | 作者 | 操作 | 意图 | 探测（35101 / 35120） |
|---|---|---|---|---|
| 107 | Owner | 发布新入站 | 是 | 可用 / 可用 |
| 108、109 | Owner | 测试脚本读取入站失败，PUT 返回 400，配置未变，两次重复发布 | 是 | — |
| 110 | Owner | 禁用入站并发布 | 否 | 可用 / 拒绝连接（rc 7） |
| — | Owner | 只在草稿里重新启用入站，不发布 | — | — |
| 111 | 自动 | 禁用 `t108i`（`post-deploy reconcile; user t108i disabled`） | 否（继承） | rc 97 / rc 7 |
| 112 | 自动 | `alpine-node`：`user t108i reactivated` | NULL（基线 106 为 NULL） | — |
| 113 | 自动 | `uk-node`：重新启用 `t108i` | 否（继承） | 可用 / **rc 7**，节点上 35120 无监听 |
| 114 | Owner | 发布草稿（入站启用） | 是 | 可用 / 可用 |
| 115 | 自动 | 再次禁用 `t108i` | 是（继承） | rc 97 / rc 7（入站没有凭据，自动停用） |
| 116 | 自动 | 再次启用 `t108i` | 是（继承） | 可用 / **可用**（记录里 Owner 启用过，自动恢复） |
| 117 | Owner | 删除入站并发布 | 否 | — |
| 118、119 | 自动 | 删除 `t108i` | NULL / 否 | rc 97 |

- 判定：通过。
  - 113 是被修复的场景：凭据回到 Owner 已发布禁用、只在草稿里启用的入站，入站保持禁用。修复前自动版本会取草稿的启用状态，把它打开。
  - 114–116 是正向对照：Owner 发布过启用的入站，在凭据清空后被自动停用、凭据回来后自动恢复。
  - 自动版本都原样继承基线版本的记录；迁移前的修订（NULL）上，自动版本继续保持 NULL。
- 错误口令每次都被拒绝（rc 97）。

**回归 1、2：草稿无法渲染时删除设备，只撤销该设备（`uk-node`）。**

| 时间（UTC） | 事件 |
|---|---|
| 02:32–02:34 | 创建 `t108`（设备 a、b）；手工发布 `uk-node` 的修订 120 因节点正忙于部署后对账（122）停在 `draft`（与 9.17 观察 B 相同）；修订 122 已带上 `t108` 的凭据；修订 123 发布 `alpine-node`。两台设备经 35101 都能访问 |
| 02:35 | 保存出站 `ft108-dummy` 与只含它的主备出口组 `ft108-group`，再把出站改为不支持 TCP；手工发布 `uk-node` 返回 400 `group member unavailable or capability changed` |
| 02:36:19 | 删除设备 a（204） |
| 02:37:18 | 面板日志 `automatic quota revision rendered from the published state alone`；生成修订 124（`uk-node`，回退路径）与 125（`alpine-node`），摘要 `auto: user t108 device deleted`，均 success |
| 之后 | 设备 a 被拒绝（rc 97），设备 b 照常可用 |

- 判定：通过。回退路径生成的修订 124 的意图记录与基线（117、122）完全相同。
- 删除两个临时对象后，100 秒内没有回退日志，也没有新修订。

**回归 3：恢复备份保留备份之后的墓碑。**
1. 02:40:16 创建备份 B2（`ylRguNif…`，归档 SHA-256 `cb8d390c…5252`），校验通过。数据库里仍有 9.17 问题 A 的旧记录，备份照常成功。
2. 创建 `t108r` 并发布 `uk-node`（修订 126），经 35101 下载 3 MB，`usedTraffic` 3 013 205。
3. 02:41:31 删除 `t108r`，生成修订 127（`auto: user t108r deleted`），success，`t108r` 被拒绝。
4. 当前数据库有 7 条墓碑，其中 1 条与 `t108r` 的凭据、用户、设备完全一致；没有待执行的部署。
5. 恢复预览 `executionAvailable: true`，没有待执行操作。02:43:00 `POST /api/v1/backups/{B2}/restore`（202）。
6. 面板在进程内切换：02:43:11 停止，02:43:18 以恢复后的数据库提供服务，`NRestarts=0`。

| 检查 | 结果 | 证据（恢复后数据库的只读快照） |
|---|---|---|
| 墓碑保留 | 通过 | 7 条，`t108r` 的那条在 |
| 删除的用户没有回来 | 通过 | `users`、`credentials` 中没有 `t108r` |
| 修订与意图 | 通过 | 修订保留到 127，`inbound_enabled_json` 随恢复复制（18 条有记录，109 条 NULL） |
| 节点 | 通过 | 三个节点 `online`，恢复没有产生修订 |

**回归 4、5**：见上面问题 1 的“回归 4”与“B”两行。回归 5 现在按设计会被拒绝 3 次再丢弃，不再只拒绝 1 次。

**本轮发现的问题。**

| # | 问题 | 影响 | 处理 |
|---|---|---|---|
| C | 进程内恢复后，旧面板实例的额度工作仍在运行（P3，早于 v1.0.8，自 `e5023e3` 起）。`quotaDeployer` 用 `time.AfterFunc` 排队节点的合并窗口，`flush` 使用 `context.Background()`，面板停止时这些定时器不会停 | 恢复时修订 127 还在等待结果确认。02:43:30、02:44:30、02:45:30 各记一次 `ERROR automatic quota revision could not read its target status … sql: database is closed`，随后记 `automatic quota revision gave up` 与 `automatic quota failure could not be recorded`。旧实例的数据库已关闭，写不进任何东西；新实例正常工作，02:46:33 删除 `t108` 后生成修订 128、129 | 记录，未修改；建议面板停止时停掉这些定时器 |
| D | Agent 断线日志的 `retry_after` 以纳秒整数输出（如 `1000000000`） | 只影响日志可读性 | 观察，未修改 |
| B | 部署繁忙时手工发布留下没有目标节点的草稿修订（修订 120） | 与 9.17 相同 | 观察，未修改 |

**恢复。**
- 临时用户 `t108i`、`t108`、`t108r` 都已删除（修订 118/119、128/129、127），临时入站在修订 117 移除，两个临时出站对象已删除。面板对象数与测试前相同：入站 8、出站 2、出口组 0、链 1、规则 4、节点组 2、用户 2；没有待执行的部署。
- `uk-node` 的 Agent 换回 1.0.7（`/usr/local/bin/proxy-agent` 摘要与测试前相同），三个节点 `online`、Agent 1.0.7，核心与测试前相同。
- 面板**保留** `1.0.8-rc.1663e4a`（owner 决定）。数据库已有迁移 0111，1.0.7 面板会拒绝打开。换回测试前的数据库会让修订号回到节点已持有的修订之下，Agent 会拒绝新修订（`older revision requires explicit rollback`）。v1.0.8 发布后按正常方式升级。
- 原链、直连、错误口令三项与基线相同。
- 共存服务 PID 与测试前相同（738/740、833/834），第三台主机上 `komari-agent`、`cf-probe`、`crond` 为 `started`。`us2-node` 的 Agent PID 未变；`uk-node` 的 Agent 因多次重装换了 PID。
- 临时文件全部删除：两台主机的 `/root/qui-rc108b`（含数据库副本与测试凭据）、本机的构建、脚本与凭据文件。`uk-node` 上没有 `qui_*` nft 表；它的内核日志里留有本轮的 `qui108*` 包长记录（只有 IP/TCP 头字段）。`/root`、`/var/lib/proxy-panel`、`/var/lib/proxy-agent` 为 0700。

**留下的差异。**
- 面板版本为 `1.0.8-rc.1663e4a`，数据库含迁移 0111。
- 面板数据库是从备份 B2 恢复后的库（B2 之后的数据按恢复规则合并）。
- 修订历史增加到 129（修订 120 为 `draft`）。
- 核心更新任务增加 12 条（1 条 `cleared`，其余 `success`），备份列表增加备份 B2。

**结论。** 两项修复实机通过（问题 1 的限制见上），五项回归全部通过，CI 通过。本轮新发现的问题 C 早于本版本、影响仅限日志，没有未解决的 P1/P2。v1.0.8 候选为 `1663e4a`，等待 owner 批准打标签与 GitHub Release。（后续：已于 2026-10-26 发布，见 9.19。）

### 9.19 v1.0.8 发布与三台主机升级（2026-10-26 UTC）

**发布。**
- 标签 `v1.0.8` 指向 `b7a71fa`（`1663e4a` 加 9.18 的验收记录，只改文档）。标签的 CI（运行 40265357526）与 `main` 上同一提交的 CI（运行 40263903277）结论都为 success，包括 `go test -race`。
- GitHub Release `v1.0.8` 于 2026-10-26T04:09:44Z 发布，正式版，不是草稿。附件有两个归档、`SHA256SUMS` 与 `node-install.sh`。amd64 归档的 SHA-256 为 `bf6e165d…a1c1`，arm64 为 `649e671c…80ab`。

**升级（各主机用已安装的 `qui-update --version 1.0.8 --yes` 从 GitHub 升级）。**

| 主机 | 结果 |
|---|---|
| `uk-node`（Debian，systemd） | Agent `1.0.7 → 1.0.8`，`active`，`NRestarts=0`，退出码 0；path 单元 `active` |
| 第三台主机 `alpine-node`（OpenRC） | Agent `1.0.7 → 1.0.8`，`started`，watcher `started`，退出码 0 |
| 面板主机 | Agent `1.0.7 → 1.0.8`，面板 `1.0.8-rc.1663e4a → 1.0.8`（迁移与候选相同，没有新迁移）；面板更新前备份为 `pre-update-1.0.8-rc.1663e4a-20261026-121345.db`；`NRestarts=0`，HTTPS 返回 200 |

- 三台主机都没有残留快照，也没有失败的单元。
- 面板显示三个节点 `online`、Agent `1.0.8`、核心健康。各节点的当前修订与升级前相同：`uk-node` 129、`alpine-node` 128、`us2-node` 81。升级引起的面板重启没有产生自动修订（最新仍为 129）。
- 面板对象数与基线相同：入站 8、出站 2、出口组 0、链 1、规则 4、节点组 2、用户 2。
- 经 `alpine-node` 的入站：`api.ipify.org` 经链从 `us2-node` 出口（23.254.158.3），`ifconfig.me` 直连（113.29.231.218），错误口令被拒绝（curl rc 97），与基线相同。
- 共存服务未受影响：面板主机与 `uk-node` 上 `xboard-node`、`cf-probe` 的 PID 仍为 738/740、833/834；第三台主机上 `komari-agent`、`cf-probe`、`crond` 为 `started`。

**遗留。** 9.18 的问题 C（进程内恢复后额度定时器记 `database is closed`，P3）与 Agent 日志里 `retry_after` 以纳秒整数输出（外观问题）未在 v1.0.8 修改。arm64 仍未实机验证。（后续：两项已在 `c83f1c1` 修复，于 v1.0.9 发布并实机通过，见 9.20：面板停止服务时额度部署器停掉所有合并窗口定时器、取消进行中的对账并等它结束后才返回，停止过程中被打断的对账不计失败、不告警、不重排，留给下一个进程的启动对账；新测试 `TestQuotaRunStopsTimersOnShutdown`、`TestQuotaEnqueueAfterStopCreatesNoTimer`、`TestQuotaFlushInFlightStopsOnShutdownWithoutCountingAnAttempt`。Agent 的 `retry_after` 改为 `1s` 这样的时长文本。）

### 9.20 v1.0.9 发布、三台主机升级与额度定时器实机复验（2026-10-26 UTC）

**发布。**
- 标签 `v1.0.9` 指向 `c83f1c1`（面板停止服务时停掉额度部署器的定时器；Agent 日志 `retry_after` 改为时长文本）。`main` 上同一提交的 CI（运行 40275103033）与标签的 CI（运行 40282357175）结论都为 success，包括 `go test -race`。
- GitHub Release `v1.0.9` 于 2026-10-26T07:20:10Z 发布，正式版，不是草稿。附件有两个归档、`SHA256SUMS` 与 `node-install.sh`。amd64 归档的 SHA-256 为 `20e70aee…7f64`，arm64 为 `b5c7f750…e453`。

**升级（各主机用已安装的 `qui-update --version 1.0.9 --yes` 从 GitHub 升级；先升级两台节点，使 1.0.9 的 Agent 经历面板重启）。**

| 主机 | 结果 |
|---|---|
| `uk-node`（Debian，systemd） | Agent `1.0.8 → 1.0.9`，`active`，`NRestarts=0`，退出码 0；path 单元 `active` |
| 第三台主机 `alpine-node`（OpenRC） | Agent `1.0.8 → 1.0.9`，`started`，watcher `started`，退出码 0 |
| 面板主机 | Agent 与面板 `1.0.8 → 1.0.9`（没有新迁移）；更新前备份 `pre-update-1.0.8-20261026-152521.db`；`NRestarts=0`，HTTPS 返回 200 |

- 三台主机都没有残留快照，也没有失败的单元。三个节点 `online`、Agent `1.0.9`；升级引起的面板重启没有产生自动修订（最新仍为 129），面板日志没有 WARN/ERROR。
- **问题 D 复验：通过。** 面板 07:25:21 重启时，`uk-node` 与 `alpine-node` 的 Agent 记 `control channel disconnected; retaining local state`，`retry_after` 为 `"1s"`、`"2s"`，不再是纳秒整数。

**问题 C 复验：进程内恢复时额度合并窗口的定时器随旧实例停止（`alpine-node`）。**

| 时间（UTC） | 操作与结果 |
|---|---|
| 07:26:48 | 创建备份 B3（`gGPojvLl…`，归档 SHA-256 `821cf3f2…7328`），校验通过 |
| 07:27:05 | 创建临时用户 `t109`（一台 SOCKS5 设备，授权“UK entry”），发布 `alpine-node`，修订 130 success；经 `127.0.0.1:990` 用 `t109` 访问返回 200 |
| 07:28:01 | 删除 `t109`（204），按节点排入 60 秒合并窗口 |
| 07:28:03 | 恢复预览 `executionAvailable: true`，`pendingOperations` 0；`POST /api/v1/backups/{B3}/restore` 返回 202 |
| 07:28:15 | 旧实例 `panel stopped`；07:28:24 新实例以恢复后的数据库提供服务（恢复记录 `serving`），`NRestarts=0` |
| 07:29:24 | 新实例的启动对账生成修订 131（`auto: startup reconcile`，`alpine-node`），success |
| 之后 | `t109` 经 990 被拒绝（curl rc 97）；用户列表为 `alice`、`bob` |

- 判定：通过。恢复前合并窗口里还有待执行的工作。07:27:55 之后面板日志只有 `panel stopped` 与 `panel started` 两行，没有 `database is closed`、`automatic quota revision gave up`、`automatic quota failure could not be recorded`，也没有其他 WARN/ERROR；9.18 的同一场景在恢复后连续 3 分钟每分钟记一次 ERROR。被停止打断的工作没有计为失败，由新实例的启动对账完成：运行中的修订 130 仍带 `t109` 的凭据，而恢复后的数据库里没有这个用户，于是生成修订 131 撤销它。
- 升级时 `qui-update` 停止面板也走同一停止路径，同样没有错误日志。

**回归。**
- 经 `alpine-node` 的入站：`api.ipify.org` 经链从 `us2-node` 出口（23.254.158.3），`ifconfig.me` 直连（113.29.231.218），错误口令被拒绝（curl rc 97），与基线相同。
- 面板对象数与基线相同：入站 8、出站 2、出口组 0、链 1、规则 4、节点组 2、用户 2。
- 三个节点 `online`；本轮之后 `uk-node` 与 `alpine-node` 的 Agent 日志没有 ERROR。

**恢复与留下的差异。**
- 临时用户 `t109` 已删除，凭据由修订 131 撤销；本机保存的临时凭据与备份信息文件已删除。
- 面板数据库是从备份 B3 恢复后的库（B3 之后只有 `t109` 的创建与删除，按恢复规则合并墓碑）。修订历史增加到 131，备份列表增加 B3 与 `pre-update-1.0.8-*` 备份。
- 共存服务未受影响：面板主机与 `uk-node` 上 `xboard-node`、`cf-probe` 的 PID 仍为 738/740、833/834；第三台主机上 `komari-agent`、`cf-probe`、`crond` 为 `started`。`/root`、`/var/lib/proxy-panel`、`/var/lib/proxy-agent` 为 0700。

**结论。** v1.0.9 已发布，三台主机已升级到面板/Agent 1.0.9，两项修复实机通过，回归通过，没有未解决的 P1/P2。arm64 仍未实机验证。

## 10. v1.1 验收清单（2026-10-28，尚未发布）

此节覆盖 [v1.1 计划](v1.1-plan.md) 全部 P/A/AU 验收编号。下表“本地证据”指既有阶段记录和本轮本地 gate；具体命令、测试名与审查修复见 [进度记录](v1.1-progress.md) 对应阶段，Phase 11 保留本轮真实退出码。未设置代理核心环境变量的跳过不算原生核心通过。历史第 3、4 节的设备、D1/D2/D5 标准不再适用于 v1.1 的对应部分，现按 R1–R9 与 D-a 至 D-e 验收；历史通过结论不自动延伸到新版本。

### 10.1 Phase 0–7

| 编号 | 证据与状态 |
| --- | --- |
| P0.1 | 部分：`v1.1-phase0-findings.md` A.1–A.6 记录上游命令/结果；数据审计与 Linux 探测 awaiting Owner；A.7 UA/导入 scheme 无可信数据，仍阻塞 |
| P0.2 | 已记录停止条件及调整：计划 §3、§7、§12.3，findings A.6/A.7；没有补丁核或替代系统；剩余批准仍待 Owner |
| A1.1 | 本地证据：`TestSubscriptionFormatsMatchThePreRefactorFixture`，URI/Base64/sing-box 全 fixture 字节对比 |
| A1.2 | 本地证据：`TestTestOnlySourceAndExporterWorkEndToEnd`、`TestFailingSourceKeepsTheRestAndWarns` |
| A1.3 | 部分：`TestFormatResolutionOrder` 覆盖优先级与开关；真实 UA 字符串 awaiting Owner/来源核实（Phase 0 A.7），当前表为空 |
| A1.4 | 本地证据：`TestSubscriptionUserInfoHeader`、`TestSubscriptionHeadersAreSent`，有限/无限/到期头 |
| A1.5 | 本地证据：`TestNeutralPackagesDoNotImportThePanel`；原“仅保留导入接口”已被 U5 明确取代，`TestImportFeatureParsersRegistered` 覆盖实际注册 |
| A2.1 | 本地证据：`TestValidationCoversEveryRule`、`TestValidationAcceptsEveryType`、`TestPresetsAreValid` |
| A2.2 | 本地证据：`TestStoreRevisions`、`TestRulesAPI`，409 与字段错误 |
| A2.3 | 本地证据：`TestClientRuleTemplateBackupRoundTrip`，真实备份/恢复候选 |
| A2.4 | 本地证据：`TestExpandPerUser`，授权、空组 DIRECT 与警告 |
| A3.1 | 本地证据：`TestGoldenProfiles`、`TestHostileNamesRoundTrip`，协议/传输/TLS 与恶意名字 |
| A3.2 | awaiting CI/Owner：`TestMihomoAcceptsTheProfiles`，cores job 已固定核心与 geodata，未在本轮执行 |
| A3.3 | 本地证据：`TestLegacyExclusions`，排除类型与空组图 |
| A3.4 | awaiting CI/Owner：`TestMihomoRoutesProxyDirectAndReject`，真实节点数据面不得以 golden 替代 |
| A4.1 | 本地证据：web 构建、client-policy self-test，服务端模板；Phase 4 记录无浏览器 YAML 拼装 |
| A4.2 | 本地证据：Phase 4 冲突处理/字段错误检查与 `TestRulesAPI`；完整浏览器走查另待 Owner |
| A5.1 | 部分：`internal/panel/user/user_level_migration_test.go` 0/1/3 设备、策略与 WG fixtures；B3 副本实跑 awaiting Owner approval |
| A5.2 | 本地证据：`TestStartupReconciliationAfterUserLevelMigration`，等待完整合并窗口后仅受影响节点自动版本 |
| A5.3 | 本地证据：Phase 5 与审查记录的 tombstone 流量用例，删除凭据的迟到批次不拒绝 |
| A5.4 | 本地证据：Phase 5 subscription/migration 用例，主链接保留、移除设备链接 403 |
| A5.5 | 本地证据：`TestPanelPublishesRollsBackAndAccountsTrafficWithALegacyAgent`，旧合同 Agent harness 发布/回滚/Xray 计量；真实 1.0.9 兼容留 RC1 |
| A5.6 | 本地证据：`TestProtocolChangeAddsAndResetRevokesCredentials`，HTTP 协议变化及凭据重置触发自动版本并移除旧凭据 |
| A6.1 | 本地证据：`TestEveryAdminRouteRefusesAPortalSession`、`TestEveryPortalRouteRefusesAnAdminSession`，注册 mux 清单覆盖 |
| A6.2 | 本地证据：Phase 6 portal 限流、CSRF、过期测试；复用管理员策略 |
| A6.3 | 本地证据：`TestPortalInactiveUsersCanLoginButCannotSubscribe`，可见原因而订阅 403 |
| A6.4 | 本地证据：Phase 6 HTTP 禁用登录、密码重置、删除用户后下次请求 401 |
| A6.5 | 本地证据：Phase 6 两用户逐门户路由隔离，query/body 用户 ID 不改变归属 |
| A6.6 | 本地证据：Phase 6 新链接再查看、旧 NULL 加密副本不可恢复、重置撤销 |
| A6.7 | 本地证据：Phase 6 与 Review fix round 2 的 backup/candidate coordinator，密码/登录开关/Token 保守合并、恢复清会话 |
| A7.1 | 本地证据：web 构建、`portal-selftest.mjs`，独立路由/CSRF、只读预览、无 viewMode |
| A7.2 | awaiting Owner approval：Phase 7 RC1 三主机清单未执行；L4 拒绝 RC 版本及组合主机组件顺序是阻塞 |

### 10.2 Phase 8–11

| 编号 | 证据与状态 |
| --- | --- |
| A8.1 | awaiting CI/Owner：双次同 pin 构建比较、静态 ELF 与 provenance；本机缺 readelf（L7），不宣称可复现验收通过 |
| A8.2 | awaiting CI/Owner：实际 release dry-run、两架构归档/溯源、SHA256SUMS、version/tag；脚本/工作流已有不等于产物已验收 |
| A8.3 | 本地证据：Phase 8 34 项 installer fixtures，qui/official、glibc/musl、篡改拒绝；root/Linux 服务级行为留 Owner |
| A8.4 | 本地证据：Phase 8 严格来源 URL/lookalike/能力与队列拒绝测试 |
| A9.1 | 部分：两套 tag/no-tag golden 本地证据；原生 `sing-box check` awaiting CI/Owner |
| A9.2 | awaiting CI/Owner：两用户 VLESS/Trojan/Hysteria2/TUIC、载荷 5% 与 Xray parity 的 env-gated 实核套件 |
| A9.3 | 本地证据：Phase 9 累计计数、spool/游标重启、重试和 epoch 假服务回归；实核重启路径另待 CI |
| A9.4 | 部分：render/free-port/non-loopback 拒绝本地证据；原生启动的 loopback 观察 awaiting CI/Owner |
| A9.5 | 本地证据：Phase 9 sing-box-only 流量超额触发自动版本的面板测试 |
| A10.1 | 本地证据：`TestAdmissionOrderStickyReleasePromotionLimitsResetAndIPv6`，假时钟顺序/窗口/升降限制/跨节点/中转排除 |
| A10.2 | 本地证据：`TestLeaseTTLRefreshExpiryAndInvalidFailOpen`、`TestDeviceGateNegotiationOldAgentsAndReconnect`，租约与 mTLS 重连 |
| A10.3 | awaiting CI/Owner：Xray scope/established/balancer/reload/UDP 与 sing-box source/close 套件；Hysteria2/TUIC UDP 数据报还需专项实验（L8），Xray 本身无此入站 |
| A10.4 | 本地证据：`TestDeviceLimitViewsResetPrivacyAndOldNodePartial` 与 mTLS 能力协商，旧 Agent 不收门控并显示节点名 |
| A10.5 | awaiting Owner approval：面板主机 -race；先前 Windows 尝试退出 1（cgo/gcc 不可用），本轮不冒充运行 |
| A10.6 | 本地证据：全表/备份/accepted-message 无源地址断言及 `TestAddressImplementationsHaveNoPersistenceOrLoggingCalls`，两视图掩码 |
| A11.1 | 部分：本轮完整本地 gate 与 A.7 的真实结果见进度 Phase 11；main CI（含 race/cores）awaiting Owner approval，无 push，不触发 |
| A11.2 | awaiting Owner approval：RC2 未执行，进度 Phase 11 有三主机清单；先解决 L4 |
| A11.3 | 未满足：仍有 P1/P2 发布阻塞/验收缺口，优先级见进度 Phase 11；arm64 version/check/qemu Agent 验证待安排，真实 arm64 主机未验证 |
| A11.4 | awaiting Owner approval：未打 tag、未发布、未部署；进度 Phase 11 有发布清单 |

### 10.3 UI 集成 U1–U8

| 编号 | 证据与状态 |
| --- | --- |
| AU1.1 | 本地阶段证据：U1 静态检查移除隐式 Token 写入、缓存、伪造延迟/uptime/version、外部 IP 查询；QR/portal/web gate |
| AU1.2 | 本地阶段证据：U1 endpoint tests，已登记节点拒绝重发注册码、按节点版本查询、Viewer 权限 |
| AU1.3 | 本地阶段证据：U1 入站全字段 edit round-trip，改名不扩大授权或丢 TLS 设置 |
| AU1.4 | 本地证据：`qr-selftest.mjs` 参考解码器 round-trip，跨版本/纠错级别、长串与超长拒绝 |
| AU1.5 | awaiting Owner approval：Owner/Viewer 全页浏览器走查和前后 Token/设备/版本行数；已有 API/静态证据不能替代点击 |
| AU2.1 | 部分：单步发布及 deployer 单元测试已有；fake-Agent 一步发布集成证据仍缺（计划 §12.5 U2a/U2c） |
| AU2.2 | 本地证据：`TestCanvasMatchesTheEditors`、single/two-hop publish 与拒绝/删除边界；复用 Deployment 批次 |
| AU2.3 | 本地阶段证据：U2 回滚到选定成功版本与节点归属校验；真实节点回滚仍属 RC |
| AU3.1 | 本地证据：`node-metrics-selftest.mjs`、`TestHostMetricsProcReadOnlyAndMissing`，1.0.9 缺字段/首样本显示未知 |
| AU3.2 | 部分：Part I 本地月界/时区/retained samples 求和测试；清理后的完整历史月缺口 L9，待明确保留/聚合方案 |
| AU4.1 | 本地证据：`TestTCPProbeAddressPolicyAndLimits`、`TestTCPProbeOwnerLimitsRetentionAndOldAgent`、`TestTCPProbeFixedOperationOverAuthenticatedStream`；特殊用途 IP 拒绝含审查修复 |
| AU5.1 | 本地证据：U5 SSRF/逐跳解析固定 IP、大小/时间边界、parser fuzz seeds、失败来源/取消与缓存读取测试；未运行额外长时 fuzz campaign |
| AU5.2 | awaiting CI/Owner：`TestMihomoAcceptsClientDialerProxyChain`，本机不运行下载的核心（L6） |
| AU6.1 | 部分：两核 `TestForwardGolden` 与 `TestForwardCanvasPublishCapabilityAndIdentity` 本地证据；原生 check/TCP/UDP `TestRealForward` 及受管节点转发 awaiting CI/Owner（L10） |
| AU7.1 | 部分：`TestRuleProviderEndpoint` 本地 Token/403/classical 文本证据；mihomo 原生接受 awaiting CI/Owner，与 A3.2 共用 cores 作业 |
| AU8.1 | 本地证据：`TestTrafficResetCalendarFakeClock`、`TestTrafficResetDueTimezoneIdempotentAndDisabled`、`TestCycleSchedulerReactivatesThroughAutomaticRevision`，短月/闰年/时区/清零再启用 |

### 10.4 限制和证据边界

L1 限速已移除；L2 调优/内核转发未批准；L3 无 Agent 的导入隧道不可用；L4 RC 安装阻塞；L5 导入类型和仅 mihomo 客户端链；L6 原生链检查与恢复保留当前撤销状态；L7 构建/dry-run；L8 UDP 门控；L9 样本保留月汇总；L10 TCP 测量范围与无身份转发。完整说明见计划 §12.3 与 [发布草稿](release-notes-1.1.0.md)。

R1–R9、D-a 至 D-e 的文档映射见 AGENTS V1.2 变更记录；所有 F1–F31、B1–B11 的实现与审查证据分别留在 U1/U2、Phases 1–10、U5 和 Part I 记录，不把 F29 的保留样本结果称为完整月统计。原生核心工作流虽已有双架构构建，现有 check.yml 无 qemu Agent 测试步骤，arm64 version/check 与 Agent/qemu 验收仍是待办，未改 CI 冒充完成。
