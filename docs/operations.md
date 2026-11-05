# 运维手册

升级顺序为面板先、Agent 后；支持 systemd 与 Alpine/OpenRC。本文给出操作规则，实机证据与未验证项见[验收矩阵](v1-acceptance.md) §11–12；未找到专项证据的操作保留“待实机”标记。安装与目录约定见[部署与升级](deployment.md)。

## 1. 日常检查

| 位置 | 看什么 |
| --- | --- |
| 总览 | 在线节点/用户/设备、今日流量、阻断次数、异常节点、最近访问与阻断域名、最近发布结果 |
| 节点 | 在线状态、资源、Agent 与核心版本、当前配置版本、出站网络（核心出站源 IP/网卡、服务器强制出站）、最后心跳 |
| 访问记录 | 全部访问 / 已阻断，可按时间、节点、用户、域名筛选 |
| 系统 → 配置版本 | 发布状态、差异、手动回滚入口 |
| 系统 → 备份 | 备份列表、校验、恢复预览 |
| `journalctl -u proxy-panel`、`journalctl -u proxy-agent` | 结构化日志（两个程序都写 stdout）；OpenRC 节点见下表 |

**节点服务管理（systemd 与 OpenRC 对照）**。节点 Agent 可以运行在 systemd（Debian、Ubuntu）或 OpenRC（Alpine，含 LXC）上，见[部署与升级](deployment.md)第 4.1 节；本手册其余部分写的 `systemctl`/`journalctl` 命令，在 OpenRC 节点上按此表替换：

| 操作 | systemd | OpenRC |
| --- | --- | --- |
| 状态 | `systemctl status proxy-agent` | `rc-service proxy-agent status`（`started` / `stopped` / `crashed`） |
| 启动 / 停止 / 重启 | `systemctl start\|stop\|restart proxy-agent` | `rc-service proxy-agent start\|stop\|restart` |
| 开机启动 / 取消 | `systemctl enable\|disable proxy-agent` | `rc-update add\|del proxy-agent default` |
| 全部服务 | `systemctl list-units 'proxy-agent*'` | `rc-status default` |
| 日志 | `journalctl -u proxy-agent -n 200` | `grep -F proxy-agent /var/log/messages \| tail -n 200` |
| 强制出站 helper | `systemctl status proxy-agent-host-network.path` | `rc-service proxy-agent-host-network status` |

OpenRC 上 Agent 由 `supervise-daemon` 监管：进程退出后 5 秒重启，不设次数上限；`stop` 先发 TERM，最多等 90 秒，再发 KILL。

面板通过一条 SSE（`GET /api/v1/events`）推送节点状态、发布状态、总览变化和告警，不轮询数据库。

## 2. 备份与恢复

**备份内容**：SQLite 一致性快照、系统配置、中央证书材料、规则集和必要密钥的加密副本。备份文件（`.pcb`）用备份口令加密，默认目录是 `<database 所在目录>/backups`（示例 `/var/lib/proxy-panel/backups`）。

**自动备份**：系统 → 备份里设置计划，默认每天 `03:00` 执行，保留 **7 个每日、4 个每周、3 个每月**（`internal/panel/system/backup_schedule.go` 的默认策略）。计划需要配置备份口令，口令加密保存在数据库里。

**手工备份 / 校验 / 预览**：面板按钮对应 `POST /api/v1/backups`、`POST /api/v1/backups/{id}/verify`、`POST /api/v1/backups/{id}/restore-preview`。外部备份可用 `POST /api/v1/backups/import` 导入，`GET /api/v1/backups/{id}/download` 导出。

**恢复（面板运行中）**：走恢复预览 + `POST /api/v1/backups/{id}/restore`，面板会写入离线候选库、校验完整性、检查迁移校验和，然后在重启后切换。恢复记录见 `GET /api/v1/restore-records`。

**恢复（面板已停）**：命令行的恢复会先恢复再继续提供服务：

```sh
systemctl stop proxy-panel.service
/usr/local/bin/proxy-panel --config /etc/proxy-panel/config.yaml \
  --restore-backup /var/lib/proxy-panel/backups/<id>.pcb \
  --restore-password-file /root/backup-password
```

- `--restore-backup` 与 `--restore-password-file` 必须成对出现；恢复必须显式给出 `--config`，不能与 `--version` 或 `--check-config` 同时使用。
- 口令文件必须是 ≤514 字节的普通文件，权限只对属主可读。
- 当前数据库缺失或为空时恢复会拒绝，不会凭空新建一个空库。
- 恢复前的完整性检查和迁移校验和不通过就拒绝替换。

**中断的恢复安装**：用只读检查命令查看保留的安装日志，不做恢复也不启动服务：

```sh
/usr/local/bin/proxy-panel --config /etc/proxy-panel/config.yaml \
  --inspect-restore-installation --restore-installation-report /root/install-report.json
```

**恢复后的预期差异**：所有管理员会话被清除（必须重新登录）；管理员口令按恢复点写回（恢复点之后的改密会被覆盖），恢复点之外的 Viewer 被禁用；恢复点之后新建的代理用户与设备被禁用而不删除（恢复预览的 `usersRemoved` 计的就是这些用户），其凭据被撤销，已用流量不回退；恢复点里不存在的节点证书被标记撤销，节点状态回到 `offline`/`pending`，需要重新连接。

## 3. 节点证书

- 节点证书由面板节点 CA 签发，默认有效期 90 天（`internal/platform/pki/authority.go`）。
- **自动续期**：证书进入到期前 14 天，Agent 会在控制连接上自动续期（`internal/agent/control/renewal.go`）；失败时最多每小时重试一次，期间继续使用当前证书。
- **手工续期 / 密钥轮换**（节点停机时）：

```sh
systemctl stop proxy-agent.service
deploy/linux/renew-certificate.sh --renew        # 续期，保留原私钥
deploy/linux/renew-certificate.sh --rotate-key   # 轮换私钥并签发新证书
systemctl start proxy-agent.service
```

  脚本要求安装记录完整、二进制摘要未变、服务已停止、`/etc/proxy-agent/config.yaml` 与 `/var/lib/proxy-agent` 存在，并且面板可达（续期在控制通道上完成）。**中断的续期或轮换，重跑同一条命令即可**：轮换把完整证书对保存在待提交日志（`identity/pending-key-rotation.enc`）里，重跑会从该日志完成提交；崩溃后若 `client.key` 与 `client.crt` 不匹配，`tls.X509KeyPair` 会直接失败，Agent 不会用错配材料。

- **吊销单个证书**：面板节点页的 `POST /api/v1/nodes/{id}/revoke-certificate`；被吊销的证书在 mTLS 上被拒绝，需要重新登记。
- **身份存储格式**：当前只支持一种格式（写 2 / 读最大 2，`identity/client.crt` + `identity/client.key`）。若节点上保存的是已删除的旧格式（例如旧代码下轮换过密钥的节点，格式 3），Agent 启动会被拒绝并提示 `stored agent identity format N is no longer supported; re-enroll this node`，该节点必须重新登记。

## 4. 手工轮换面板节点 CA **[待实机]**

面板只持有一个节点 CA，加载或创建都在 `node_identity_directory`（示例 `/var/lib/proxy-panel/identity`）。轮换是本地文件操作：

1. 停止面板：`systemctl stop proxy-panel.service`。
2. 备份身份目录：`ca.crt`、`ca.key`、`configuration.key`（`configuration.key` 用于解密本机保存的敏感材料，必须一起备份）。
3. 删除 `ca.crt` 与 `ca.key`，保留 `configuration.key`。
4. 启动面板：`pki.LoadOrCreate` 会生成新的自签名节点 CA，gRPC 端口从此只信任新 CA，**所有旧证书立即被拒绝**。
5. 逐节点恢复：按第 5 节「复用同一台机器重新登记」的完整步骤处理（只清空 `identity/` 不够）。**不需要**替换 `/etc/proxy-agent/tls/ca.crt`：它校验的是面板 Web 证书，而轮换节点 CA 不改 Web 证书。只有面板 Web 证书换了签发 CA 时才用 `deploy/linux/node-ca.sh --trust <new-ca.crt>` 替换它（该脚本只替换这个文件、把旧文件保留为 `ca.crt.previous` 并用 openssl 校验新 CA，不动节点身份）。
6. 全部节点回来后，再删除第 2 步备份的旧 CA 文件。

**重新登记必须新建节点记录**：

- 注册码只在节点记录**启用**时可用（`internal/panel/node/repository.go:106` 的查询与 `repository.go:126` 的核销更新都要求 `nodes.enabled=1`），被禁用的节点不能登记；
- `node_identities.node_id` 是主键，登记时是普通 `INSERT`（`repository.go:137`），所以**同一个节点 ID 无法二次登记**；
- 面板只支持逻辑删除节点（见第 15 节）；被删除的节点记录仍保留，但不能再登记。

因此 CA 轮换后，节点必须以**新的节点记录**回来：在面板新建节点、生成注册码，节点侧按第 5 节「复用同一台机器重新登记」的步骤处理（只清空 `/var/lib/proxy-agent/identity` 不够：其中的 `identity.json` 会让 Agent 以 `node is already enrolled` 拒绝登记，受管状态与 spool 也会拒绝）。旧节点记录请禁用，避免它继续计入在线节点；新节点需要重新加入原节点组并重新发布配置。

仓库里的 `deploy/linux/panel-ca.sh` 执行与上面第 2–3 步等价的备份与删除：`--inspect` 只打印现状，`--rotate BACKUP_DIRECTORY` 备份 `ca.crt`、`ca.key`、`configuration.key` 并用 SHA256 校验后删除 CA 文件。它要求身份目录不是符号链接、权限恰好 `0700`、属主是 root 或面板服务账号 `proxy-panel`（打包安装在 `/var/lib/proxy-panel/identity` 上是后者），三个材料文件的属主必须与身份目录一致；服务必须已停止。它的用法文本和结尾提示都写明轮换后节点要以新记录回来。

## 5. 替换丢失或失陷的节点 **[待实机]**

1. 面板里**禁用**旧节点：这会让它的证书在 mTLS 上被拒绝（`internal/panel/node/repository.go:151` 的 `ValidateIdentity` 要求节点启用；`TestDisabledNodeIsRefusedAtMTLS` 覆盖该行为）。
2. 新建节点记录并生成一次性注册码。
3. 按[部署与升级](deployment.md)第 4 节安装并登记新节点，然后把它加入原节点组并重新发布配置。

**复用同一台机器重新登记**（替换丢失/失陷节点、CA 轮换后都按此做）：只清空 `/var/lib/proxy-agent/identity` 不够 —— 受管 Agent 状态（`agent-release-history`、`releases/agent-installed`、`agent-jobs` 等，见 `internal/agent/agenthost/state.go:30`）会让登记以 `cannot enroll over supervised Agent state` 被拒绝，spool 队列也绑定旧节点 ID（`spool identity or schema mismatch`，`internal/agent/spool/spool.go:129`）。步骤：

1. `systemctl stop proxy-agent`；
2. 把整个数据目录移作备份：`mv /var/lib/proxy-agent /var/lib/proxy-agent.old-$(date +%Y%m%d)` —— 旧节点尚未上传的遥测与访问记录会随目录一起丢弃；备份只用于排查，确认无误后删除；
3. 重跑安装脚本（见[部署与升级](deployment.md)第 4 节）：`deploy/linux/install.sh --node --binary bin/proxy-agent --sha256 <sha256>`。它会重建 `/var/lib/proxy-agent`，属主 `proxy-agent`、权限 `0700`（`deploy/linux/install.sh:310`）；
4. 登记：`deploy/linux/enroll.sh --token-file FILE`；
5. 在面板把新节点加回原节点组，并把引用旧节点的出站 / 出口组成员 / 代理链改指新节点，然后发布配置。

不要把旧节点的私钥或数据目录拷到新节点上；`identity/` 里保存的是旧节点身份。

## 6. 额度与连接限制（软执行）

**每用户一个额度**（D3）：`users.traffic_limit` / `users.used_traffic`，跨所有节点累计；没有每节点预算、没有核内字节配额。

- 采样与上报：Agent 每 5 秒从核心统计采样写入本地 spool；心跳（面板指定 **15 秒**，`internal/panel/node/control.go` 的 `HeartbeatInterval`）里上传流量批次，面板收到 ACK 后才删除本地批次。
- 判定：用户处于 `quota_exceeded`（已用 ≥ 额度）、`expired`（到期）或 `disabled` 时，面板自动创建并部署一个**只调整用户访问面**的配置版本，把该用户的凭据从节点配置中移除；恢复（提额、清零、续期、启用）同样触发一次自动版本。
- 删除：删除用户或设备后，面板同样自动发布一个移除其凭据的版本（描述为 `auto: user <name> deleted` 或 `auto: user <name> device deleted`），无需 Owner 重新发布；目标是已发布状态里仍持有该用户凭据的节点，节点已一致时不产生版本。删除前产生、尚未上传的流量仍按凭据墓碑入账。
- 合并与去重：同一节点上的转换在 60 秒窗口内合并成一次发布（`internal/platform/server/quota.go`）；自动版本以节点**已发布**状态为基线（`mergeUserAccess`），除用户与入站的绑定取自当前草稿（这类未发布编辑可能被带出）外，未发布的 Owner 编辑不会被顺带推下去；中转凭据、出站、出口组、代理链与分流规则始终按已发布状态。
- 入站的启用状态：渲染器会把「没有任何用户凭据」的入站写成 `enabled=false`，所以已发布状态本身分不清「Owner 禁用了它」和「它暂时没有用户」。每个版本因此另外记下 Owner 当时启用的入站集合（`config_revisions.inbound_enabled_json`，迁移 `0111_inbound_enabled_intent.sql`），自动版本**原样继承**基线版本的这份记录：凭据回到一个已发布状态下被禁用的入站时，只有记录里说 Owner 曾启用它才重新启用；记录缺失（本列之前发布的版本，列为 NULL）时保守处理，保持禁用，等 Owner 下一次发布决定。
- 目标节点：该用户**当前授权**的节点，加上**已发布状态里仍持有该用户凭据**的节点（例如授权已移除但尚未发布），所以禁用也能到达这类节点。
- **超额窗口**：在**正常路径**上（节点在线、读取与发布都成功、没有其他部署占用该节点），超限到被切断之间最多有一个上报周期（当前 15 秒）＋最长 60 秒合并窗口＋发布耗时（Agent 应用并重启核心的时间），每次重试再加一个窗口。这段时间内的流量仍会放行，并且会计入 `used_traffic`。这是被接受的取舍。
- **核心重启**：每次自动额度版本和其他版本一样会在该节点重启核心（先停旧核心再起新核心），该节点上**所有用户**的既有连接都会短暂中断；60 秒合并窗口限制了重启频率。
- 面板重启不会引发发布风暴：状态机启动时先从数据库播种，只有状态**变化**才触发（`internal/panel/user/quota.go`）；启动后还会对每个有已发布状态的节点对账一次（`quotaDeployer.recover`），只为「用户访问面与节点正在运行的配置不一致」的节点创建版本，因此面板停机期间变成不活跃的用户、以及合并窗口里丢失的工作都会被补上；如果这次启动对账读不到状态（读取失败），面板会在每个 tick 重跑一次，直到一次完整读完为止。
- **失败重试与放弃**：节点繁忙或等待 Agent 结果只等下一个窗口，不算失败；读取、创建、部署失败，或 Agent 上报失败/回滚，则用**当前已发布状态**重新生成版本重试，连续 **3** 次失败后停止并告警 Owner：`system.alert`（`alert=user_access_failed`）＋系统 → 操作日志里的「自动额度发布失败」记录（面板日志 `automatic quota revision gave up`，可 `journalctl -u proxy-panel | grep "automatic quota revision gave up"` 查看）。
- **草稿无法渲染时的兜底**：Owner 已保存的草稿整体渲染失败时，自动版本退回按已发布状态生成，只移除不活跃用户（日志 `automatic quota revision rendered from the published state alone`），恢复类转换要等草稿能再次渲染。
- 每用户额度统计 Xray 节点，以及上报 `singbox_user_stats` 能力的 sing-box 节点流量。官方 sing-box 无 `with_v2ray_api` 的构建仍仅提供节点总量；用户被授权这类节点时，用户页会给出提示（D5）。

**v1.1 并发设备限制取代 D2 的旧 UI/API**：`deviceLimit` 范围 0–100，升级默认 0（不限）。在用户详情查看估计在线设备、部分执行节点与掩码地址，必要时由 Owner 重置准入顺序。跨入口节点按 IPv4 与 IPv6 /64 去重、先到先得；活跃 60 秒，失活后保留 120 秒再释放。限制不是物理设备识别：NAT 少算、双栈与切网多算，超限约延迟一个心跳加投递时间。

Xray 通过运行时规则阻止新连接，保留既有连接；sing-box 联接用户日志与 Clash 源 IP/端口，每约 2 秒关闭匹配连接。门控不发布 Revision、不重启核心。旧 Agent、缺观测能力或失败节点明确显示部分执行；面板断联 3 分钟租约过期放行。WireGuard 一次一个设备、不计数；Xray SOCKS5 UDP 不携带用户，不能限制。sing-box 的 SOCKS5、Shadowsocks、Hysteria2、TUIC UDP 均不支持设备限制（L8），这些入站的 TCP 仍受限制。详见 [设备限制](device-limit.md)。面板完整源地址只在内存，Owner 与门户分别展示 /24、/48 掩码，不入库、日志或备份；节点调用 Xray 官方 CLI 使用的临时 JSON 为 0600，返回即删除。

**续期刷新**：延长一个仍在活跃状态的用户到期时间不会产生“状态转换”，但 Agent 会按自己保存的到期时间切断用户，所以面板在到期时间被修改时会直接为该用户排队一次刷新（`reason=renewed`，不发告警）；如果这次修改同时跨越了活跃边界，则按上面的转换流程处理并发出告警。

**时间窗与到期的延迟（1.4 取舍）**：Agent 定时重渲染（时间窗规则）或应用失败时，会保持上一成功配置继续服务，并在下一个周期重试，不会退回全阻断、也不会停核心（`internal/agent/reconcile/schedule.go`）。所以时间窗阻断或到期**可能被推迟**到下一次成功渲染为止；同理，面板侧自动版本失败时，节点仍运行旧配置。

## 7. 访问记录与流量

- 记录粒度：分钟时间桶 × 用户 × 设备 × 入口节点 × 域名/IP × 规则 × 动作 × 配置出口，含连接数；同一分钟内同一组合只写一条。
- **每域名字节是可空字段**（D4）：官方核无法提供每域名精确字节，未知时列为 NULL，页面显示“—”。未知不写零。
- **配置出口**：页面上的“配置出口”是该节点按策略解析出的出站或出口组所选成员，表示**配置**而非跨节点实际到达的出口节点；跨中转链路的实际出口无法确认。只有本节点直连路由能确认出口节点，页面会区分标注。
- **sing-box 的访问来源是核心日志**：上游 Clash API `/connections` 只列出仍打开的连接、且不含用户，所以 Agent 读核心日志（渲染时提高到 debug 级别）来获得每条连接的用户、目标、命中规则索引和出站，包括被拒绝的连接（`internal/agent/collector/singbox.go`）。
- **Xray 的访问日志**在 Agent 运行时目录 `/var/lib/proxy-agent/runtime/xray/access.log`：它是瞬时文件：Agent 以约 100 ms 轮询跟随，**读到就立刻清空**，核心停止时再清一次，因为明文 HTTP 行会带上 URL 路径与查询串；超过 **20 MiB** 仍会截断作为兜底，避免写满磁盘（`internal/agent/core/xray/access_log.go`）。检查大小与清空之间新追加的一行可能丢失（轻微少计，不会把路径留在磁盘上）。备份节点目录时不需要保留它。
- 本地缓存：spool 上限 **100 MiB**（`internal/agent/spool/spool.go` 的 `DefaultLimit`），容量不足时按“允许访问的记录最旧优先”淘汰，阻断记录与流量记录优先保留；断线期间继续缓存，恢复后批量补传并按 ACK 删除。
- 保留策略：默认允许访问 30 天、阻断记录 90 天、小时流量 90 天，日流量长期保留（`pkg/contracts/access/retention.go`），可在系统 → 设置里调整。

## 8. 出口组与代理链

| 策略 | Xray | sing-box |
| --- | --- | --- |
| `primary`（主备） | 由 Agent 在节点上选择当前成员，只渲染选中成员（上游没有“首健康”策略） | 同左 |
| `round_robin`（轮询） | 渲染 `routing.balancers` 的 `roundRobin` + `observatory` + `fallbackTag` | 渲染 `urltest` |
| `latency`（延迟优先） | 由 Agent 选择（leastPing 语义） | 渲染 `urltest` |
| `weighted`（权重） | 已移除 | 已移除 |

迁移 `0104_egress_weighted_to_round_robin.sql` 把旧的权重出口组改写为 `round_robin`；读取旧配置版本时 `weighted` 也按 `round_robin` 解释。失败处理按配置执行，固定出口失败**不会隐式 Direct**。代理链发布顺序为先出口、后中转、最后入口，回滚反向。

## 9. 节点出站网络

“节点详情 → 节点设置 → 出站网络”里的四个字段是两条独立能力，改动都在**发布配置后生效**；留空保存表示清除该值。

| | A 核心出站源 IP / 网卡 | B 服务器强制出站 IPv4 / IPv6 |
| --- | --- | --- |
| 改变什么 | 代理核心为被代理流量打开的套接字 | 经该网卡新建的每个出站连接（Agent、apt、DNS、cron、容器、显式绑定的套接字） |
| 机制 | 核内配置：Xray `sendThrough` + `streamSettings.sockopt.interface`；sing-box `inet4/inet6_bind_address` + `bind_interface` | 专用 nft 表 `inet proxy_agent_source` 的 postrouting SNAT（优先级 90，先于 Docker 等 srcnat 链） |
| 权限 | 无 | 仅独立 oneshot 单元持有 `CAP_NET_ADMIN`，Agent 与核心始终没有 |
| 平台 | 任意 | 仅 Linux（≥ 5.2，需 nftables） |

- **A 豁免于 B**：A 配置的源地址从 B 的规则里排除，被代理的流量仍用 A 的源地址发出。
- A 只作用于渲染出的拨号出站：出口组成员同样固定，sing-box 的 `selector`/`urltest` 组本身不带拨号字段（上游不接受），入站 WireGuard 端点只监听、不改写；目标是**另一族的字面量 IP** 时连接失败（源地址已固定到某一族）。
- **NAT 在连接的第一个包上决定**：规则变化后，已建立的连接保持旧源地址直到重连；对入站用户连接（用户 → 代理监听端口）的回包不做转换。
- 其他网卡（LAN、隧道）不转换；**只转换配置了地址的地址族**：双栈主机请同时填写 IPv4 和 IPv6，未填写的地址族不会被改写（面板在只填一个时提示）。
- 清除：四个字段留空并保存、再发布一次，helper 删除 `inet proxy_agent_source` 表；`uninstall.sh agent` 同样删除该表。没有配置 B 的节点不调用 helper，未装 nftables 的旧安装照常工作。
- 失败码：`network_unavailable`（A 的源地址或网卡在节点上不存在）与 `host_network_failed`（helper 未安装、执行失败，或改完后新连接连不上面板）都是**临时**失败码，节点保留上一成功配置并重试；B 失败时本次发布回滚到上一版本（规则一并还原）。

排查：

```sh
nft list table inet proxy_agent_source
systemctl status proxy-agent-host-network.service proxy-agent-host-network.path
journalctl -u proxy-agent-host-network -n 100
journalctl -u proxy-agent -n 200
cat /var/lib/proxy-agent/host-network/desired.json /var/lib/proxy-agent/host-network/status.json
```

OpenRC 节点上，helper 由 `proxy-agent-host-network` 服务运行：它监管 `host-network-watch`，后者在服务启动时应用一次已保留的 `desired.json`，之后由 busybox `inotifyd` 在 Agent 发布新文件时再运行 helper。排查时把上面的 `systemctl`/`journalctl` 换成 `rc-service proxy-agent-host-network status` 与 `grep -F proxy-agent-host-network /var/log/messages`。重启该服务（`rc-service proxy-agent-host-network restart`）会重放一次保留的规则，作用相当于开机。

`desired.json` 是 Agent 下发的目标状态（含随机 `id`），`status.json` 是 helper 对同一 `id` 的结果；两者不一致说明 helper 未安装或尚未处理完。

helper 连续失败不会让后续发布失效：oneshot 单元设置了 `StartLimitIntervalSec=0`，不受 systemd 默认的启动频率限制（10 秒内 5 次），path 单元因此不会以 `unit-start-limit-hit` 停止；OpenRC 的 watcher 不会因 helper 失败而退出。Agent 只有在 `status.json` 确认同一 `id` 的“清除”请求成功后，才把保留的清除请求视为已生效，否则重新发布并等待结果。早于此修复的版本可能已经停在这种状态（`systemctl status proxy-agent-host-network.path` 显示 `unit-start-limit-hit`）：升级时 `install.sh` 发现 oneshot 单元处于 `failed` 会 `reset-failed` 并重放一次保留的文件；不升级时可手动执行 `systemctl reset-failed proxy-agent-host-network.service proxy-agent-host-network.path && systemctl start proxy-agent-host-network.path proxy-agent-host-network.service`。

## 10. 核心与 Agent 更新

多核心安装：新节点默认单 Xray，需要两核时传
`--core xray --core sing-box`；两个安装器都支持重复参数。手工
`--core-version/--core-url/--core-sha256` 跟在对应 `--core` 后，
`--core-source` 全局选择 sing-box 来源。升级不传核心参数会保留全部安装核心，
显式传入只替换/追加，省略不表示删除。新 Agent 配置使用 `cores: [{name, path}]`，
旧 `core_name/core_path` 保留为第一个核心的兼容值；显式安装核心时会将核心路径规范化到
`/usr/local/lib/proxy-agent/<name>`，其他设置保留；无核心参数的升级保留已有 config（包括兼容旧键）原样。首次迁移从校验过的新发布包
运行新版 update.sh，旧 updater 不含多核心和 config 快照保护。

记录 v2 继续读取 v1，第 10 行 init、第 11/12 行 调优摘要保持含义；
第 13 行起每行 `core <name> <absolute-path> <sha256>`。新 qui-update 将全部
核心、Agent config、记录、主服务、强制出站与调优文件一起快照/恢复，进度日志
沿用原格式；v2 Agent 记录遇到不支持它的旧目标 installer 时，在停服务前拒绝。
卸载逐一校验并移除所有记录核心，保留状态数据，仍先恢复 sysctl 原值再删
调优单元。完整格式、离线参数与回退边界见[部署说明](deployment.md#多核心安装)。


节点安装时，`--core sing-box` 默认使用与 Agent 同一 qui 发布的静态
`sing-box-1.14.4-qui.1-linux-(amd64|arm64).tar.gz`，按发布的 `SHA256SUMS`
校验；glibc 与 musl 使用同一个归档。`node-install.sh --core-source official`
保留官方 1.14.3 路径和四组固定摘要，手工 `install.sh` 的官方路径仍支持
`--core-version`、`--core-url`、`--core-sha256`。手工 qui 路径需要
`--qui-version X.Y.Z` 与 Agent 发布版本一致；离线文件参数见[部署说明](deployment.md)。

核心更新合约接受严格的 qui 资源 URL：
`https://github.com/jingniao/qui/releases/download/<tag>/sing-box-<ver>-qui.<n>-linux-(amd64|arm64).tar.gz`。
运行时版本号仍是上游版本（如 1.14.4），qui 修订号写在归档名和 provenance 文件中。
该来源只对上报 `singbox_user_stats` 能力的 Agent 开放；面板表单与排队校验
使用同一能力门控。旧 Agent 未上报该字段时仍仅能使用官方来源。

**v1.1 升级后的额度变化**：新版 Agent 检测到 `with_v2ray_api` 后，下一次
渲染/发布配置会在随机空闲的 `127.0.0.1` 端口启用每用户统计。原来未计入
用户额度的 sing-box 流量从此开始计入，可能触发已有额度的自动停用版本；
升级前请核对用户额度。统计不追补启用前的流量。此 API 只有本机信任边界，
不应对外转发其端口；代理入站也会阻断访问该端口。Clash API 继续用于节点
总量；没有该构建标签的官方核心保留 D5。

**代理核心（面板驱动，直接 URL + SHA-256）**：服务器 → 代理核心里填 `组件 / 稳定版本号 / 官方 GitHub 发布资源 URL / 归档 SHA-256`。面板在入队时校验组件、纯发布版本号、摘要格式、URL 前缀和资源名与节点平台是否匹配（`internal/panel/deployment/core_updates.go`）。Agent 侧再次校验：URL 只接受官方前缀，重定向最多 3 跳且只允许 `github.com`、`objects.githubusercontent.com`、`release-assets.githubusercontent.com`；先校验归档摘要再解包；解包拒绝符号链接、路径穿越和多余候选；版本不匹配或健康检查失败时回滚到上一镜像（`internal/agent/updater/core_source.go`、`internal/agent/control/core_updates.go`）。排队、状态和结果见 `GET /api/v1/core-updates`；卡住的任务在节点重连时关闭，Owner 也可用 `POST /api/v1/core-updates/{id}/clear` 关闭。旧任务（无 URL）按 `failed_closed/history_invalid` 关闭。

**Agent 自身（签名目录）**：Agent 自更新仍然使用 Ed25519 签名清单，不改成 URL + SHA-256。

- 面板侧：`release_keys` 固定受信公钥；`POST /api/v1/releases` 保存签名条目，`POST /api/v1/agent-updates` 为节点排队（另有 `/recover`、`/attempts`）。
- 签名端：`cmd/release` 是离线工具，子命令为 `keygen`、`sign`、`verify`、`stage`、`fetch`、`prepare-agent`、`queue-agent`、`agent-result`。典型流程是 `keygen` 生成密钥对，`sign` 对本地二进制生成签名清单，`verify`/`stage` 校验并生成候选，`fetch`/`prepare-agent`/`queue-agent`/`agent-result` 面向本机已登记的 Agent 配置（`--agent-config`）。私钥始终留在离线签名端。
- 节点侧：Agent 用配置里固定的公钥校验清单，按安装历史防重放，失败回滚到已确认镜像；受管 Agent 启动时会自行修复中断的切换（`internal/agent/agenthost/recovery.go`、`internal/agent/reconcile/core_rollback_repair.go`）。

### RC 更新与组合主机的组件选择

`qui-update --component panel|agent` 只更新指定的已安装组件；不指定仍先 Agent 后面板。组合主机先运行 `--component panel`，确认后再 `--component agent`。`qui` 菜单更新项仍使用默认方式，组合主机升级应使用命令行组件选择。RC 版本必须是 `X.Y.Z-rc.N`（N 为无前导零的正整数），比较顺序为 `1.0.9 < 1.3.1-rc.1 < 1.3.1-rc.2 < 1.3.1`；线上显式指定 RC 使用同名 v 标签，离线仍必须同时提供 bundle/sums。旧版更新器不能识别新参数，首次更新从已审核并校验的 RC 归档运行新版 update.sh，完整命令见[部署与升级 §5.0](deployment.md#50-rc-与旧更新器的首次升级)。

组件的回滚/恢复不替换或重启另一个组件，共享更新脚本与服务库随本次更新恢复；进入新更新前仍先处理已有未完成日志。面板数据库的备份、只读预检与跨迁移回退不变；`--database` 不能用于 `--component agent`。锁、10 秒健康窗口、systemd/OpenRC 和断电恢复机制均不变。RC1/RC2 仍 awaiting Owner approval，未执行。

## 11. 已移除的运维操作（不要再找这些命令）

精简重构删掉了以下 Agent 本地运维模式（Phase 9/12 删除，`cmd/agent` 现在只有 14 个参数，含服务器强制出站的 `--apply-host-network`）。它们**不再存在**，对应的替代做法如下：

| 已移除的操作 | 现在怎么做 |
| --- | --- |
| spool 密钥恢复模式 | 没有 CLI。Agent 打开 spool 时若 `queue.db` 在而 `spool.key` 缺失会报 `spool encryption key missing`，并因遥测队列不可用而不启动核心（日志：`telemetry queue unavailable; proxy remains blocked`）。请从备份恢复 `/var/lib/proxy-agent/spool/spool.key`；密钥确实不可恢复时，把整个 `spool/` 目录移走让 Agent 建新队列，代价是丢弃尚未上传的记录。 |
| spool 数据库候选安装 | 没有“候选库”概念，也没有安装模式。spool 只使用自己的 `queue.db`，面板迁移不会作用于它；旧格式库在打开时忽略已删除的表并继续排空。 |
| 登记结果恢复模式 | 没有 CLI。登记中断时重新登记：确认面板上节点启用、生成新注册码，必要时清空节点本地身份后执行 `deploy/linux/enroll.sh --token-file FILE`。 |
| 已确认 Agent 镜像恢复 | 没有 CLI。受管 Agent 启动时按安装历史自行决定回滚并释放限制；需要换回旧版本时，用签名目录重新排队一次该版本。 |
| supervisor 巡检模式 | 没有 CLI。看 `systemctl status proxy-agent.service`、`journalctl -u proxy-agent`，以及面板节点页显示的 Agent 版本和状态。 |

## 12. 登录与安全取舍

- 登录失败限制：同一来源 IP 或同一用户名在 15 分钟内失败 **5 次**即被拒绝（`internal/panel/auth/service.go` 的 `loginAttemptLimit`、`loginAttemptWindow`）。限流器在内存里，重启面板即清空。
- **Owner 可能被锁 15 分钟**：桶按用户名和 IP 划分，因此任何人都能用 5 次错误尝试把 Owner 锁在门外 15 分钟。这是 1～10 人内部使用的被接受取舍；必要时重启面板即可清除限制。
- 面板位于受信反向代理之后时，配置 `server.trusted_proxies` 才会采信转发头；否则只认直连对端地址。
- 其他固定项：会话 Cookie 与 CSRF 校验、HTTPS、订阅 Token 只保存哈希、审计操作日志（`GET /api/v1/operations`）、备份口令加密保存。

## 13. 常见故障

| 现象 | 检查 |
| --- | --- |
| 节点显示离线 | `systemctl status proxy-agent`、`journalctl -u proxy-agent`、节点到面板 9443 的连通性、`/etc/proxy-agent/tls/ca.crt` 是否是签发面板 Web 证书的 CA 且与 `server_name` 匹配 |
| 节点连上就断开 | 节点是否被禁用、证书是否过期或被吊销、Agent 版本是否低于面板要求 |
| 节点突然离线，Agent 每 30 秒重复 `invalid spool capacity observation` | 本地队列的容量观测被面板拒绝（1.3.1 之前是 spool pragma 只对一条连接生效的缺陷），见下面“节点因 spool 容量观测离线”；临时处置是重启 Agent |
| 发布后节点回滚 | 系统 → 配置版本看差异与失败原因；核心原生校验失败、端口冲突、健康检查失败都会自动回滚并保留上一成功版本 |
| 出站源地址未生效或发布失败 | 是否已发布配置（字段改动只在下一次发布生效）；`nft list table inet proxy_agent_source`；`systemctl status proxy-agent-host-network`、`journalctl -u proxy-agent-host-network`；失败码 `network_unavailable` / `host_network_failed` 见第 9 节 |
| 用户仍能上网但已超限 | 自动版本尚未完成（最长一个上报周期＋60 秒＋发布耗时），或节点离线未收到版本 |
| 核心反复重启，日志有 `core listener did not answer … i/o timeout` | 宿主 conntrack 表是否已满（内核日志 `nf_conntrack: table full`），见下面“conntrack 表满”；这是主机设置，不是面板或 Agent 的缺陷 |
| 访问记录为空 | Agent 是否上报（`journalctl -u proxy-agent` 里的遥测错误）、核心是否在跑、保留策略是否已清理该时间范围 |
| 额度/在线数显示“未知” | sing-box 节点没有 `singbox_user_stats` 能力时仅提供节点流量（D5）；每用户在线数仍为未知，或核心不支持/未启用对应统计（D2），或该用户在 Xray 节点上没有在线条目（例如只用 WireGuard） |
| 备份恢复失败 | 口令是否正确、备份是否完整（先 `verify`）、当前库是否被其他进程占用（恢复要求面板停止写入） |

**conntrack 表满（主机设置，不是面板或 Agent 的缺陷）**

- 症状：内核日志反复出现 `nf_conntrack: table full, dropping packet`，Agent 日志出现 `core unhealthy; restored last successful configuration`，同一行带 `core listener did not answer ... i/o timeout`；表现为核心被反复重启、连接偶发失败。uk-node 在 v1.3.0 的 RC-D 里因此 5 分钟内重启 Xray 10 次（见[发布说明 1.3.0](release-notes-1.3.0.md) 的已知限制）。
- 检查（root）：

```sh
cat /proc/sys/net/netfilter/nf_conntrack_count /proc/sys/net/netfilter/nf_conntrack_max
dmesg | grep -c 'table full'
journalctl -u proxy-agent | grep -c 'core unhealthy'
```

- 处理：提高上限与桶数，然后让 sysctl 生效。两个值要一致（桶数决定哈希表分片），写进 `/etc/modprobe.d` 的 `hashsize` 只在模块加载时生效，已加载时以 sysctl 为准：

```sh
cat >/etc/sysctl.d/90-conntrack.conf <<'EOF'
net.netfilter.nf_conntrack_max = 131072
net.netfilter.nf_conntrack_buckets = 32768
EOF
printf 'options nf_conntrack hashsize=32768\n' >/etc/modprobe.d/nf_conntrack.conf
sysctl --system
```

- 结果（2026-11-03，uk-node，Owner 批准）：`nf_conntrack_max` 7680 → 131072、`nf_conntrack_buckets` 32768 后，此前累计的 920 条 `table full` 不再增长，负载下 count 稳定在约 4.6k–7.4k，核心重启归零（v1.3.0 主机 RC-E 期间 0 次 `core unhealthy; restored`）。逐项见[验收矩阵](v1-acceptance.md) §12。
- 注意：LXC/容器内改的是**宿主机**的设置，容器里可能只读或被宿主策略拒绝。conntrack 不在服务器调优 helper 的 13 个固定键里，因此不由面板下发，也不随 `qui-update` 或恢复备份改变。

**节点因 spool 容量观测离线（v1.3.1 已修复）**

- 症状：Agent 日志每 30 秒重复一对 `node control connected` 与 `WARN control channel disconnected … invalid spool capacity observation`（`journalctl -u proxy-agent`），面板上该节点一直离线。2026-11-03 的 alpine-node 即此现象（约 05:07Z 起离线，06:50Z 重启 Agent 后恢复）。
- 原因：本地队列（SQLite）的 `journal_mode`／`journal_size_limit`／`max_page_count` 属于**单条连接**，1.3.1 之前只在打开时设过一次；`database/sql` 在语句被中断（spool 取样有 1 秒截止时间，负载下可能触发）后换一条连接，新连接带着 SQLite 默认页上限，Agent 上报的数据库容量变成 17,592,186,036,224 B，面板按 `spool.DiskLimit`／`DatabaseLimit` 校验时拒绝**每一次**控制会话。队列容量上限同时也没有真正生效。
- 处置：升级到 v1.3.1 或更高（pragma 写进每条连接的 DSN）。**1.3.1 之前**的版本临时重启 Agent（`systemctl restart proxy-agent`；Alpine 为 `rc-service proxy-agent restart`）即可恢复连接，但只是缓解，负载下可能再次发生。队列里的记录不会因此丢失，恢复后照常补传。

## 14. 终端菜单 qui

面板主机上安装的是 root 专用终端菜单 `/usr/local/bin/qui`（`deploy/linux/install.sh --panel` 安装；SSH 会话里输入 `qui` 打开）。菜单固定只有以下项目：

| 菜单项 | 行为 |
| --- | --- |
| 1. 修改管理员账号密码 | 依次提示新用户名（留空保持不变）和两次新密码，确认后**短暂停止** `proxy-panel.service`，再以服务账号身份运行 `proxy-panel --config /etc/proxy-panel/config.yaml --set-owner-credentials`（用户名与口令两行从标准输入读入，口令不进 argv、不回显），然后按停止前的状态把服务启动回来。口令策略由程序校验，错误原样显示；成功后**所有 Owner 会话立即失效**，必须重新登录。 |
| 2. 修改端口 | 只改 `http_address` 的端口，`grpc_address`（节点连接）不动：校验端口是 1-65535 的整数且与当前不同，端口与 `grpc_address` 相同时给一行提示；在 `/etc/proxy-panel` 里生成候选文件，先让程序 `--check-config` 校验，通过后把原文件备份为 `/etc/proxy-panel/config.yaml.bak`（属主与 0600 权限和原来一致）再原子替换；校验或备份失败时删除候选文件、保留原配置。最后询问是否立即重启，并提示放行防火墙与含旧端口的订阅链接需要用户刷新。 |
| 3 / 4 / 5. 重启 / 启动 / 停止 | 直接 `systemctl restart`、`systemctl start`、`systemctl stop proxy-panel.service` 并打印结果状态；启动或重启失败会提示 `journalctl -u proxy-panel -n 50`。服务本来就在运行或已停止时只提示，不重复操作。 |
| 6. 更新（从 GitHub） | 运行 `/usr/local/bin/qui-update`：默认取 `https://github.com/jingniao/qui/releases/latest`（也可 `--version X.Y.Z` 或 `--version X.Y.Z-rc.N` 指定），下载归档与同一发布的 `SHA256SUMS` 并核对摘要，停止 `proxy-panel.service`，在停止状态下把数据库备份到 `/var/lib/proxy-panel/backups/pre-update-<旧版本>-<时间戳>.db`，替换前把会被替换的文件快照到 `/var/lib/proxycontrol-install/rollback.*`，再用归档里的 `deploy/linux/install.sh` 原地替换程序与单元，最后按停止前的状态启动服务，并要求它 10 秒内不重启；`install.sh` 失败、版本不符或新版本没有持续运行时自动恢复快照与更新前的数据库（先把新版本的数据库保留为 `failed-update-*.db`），启动旧版本。按 Ctrl-C、SSH 断线或被 `kill` 时同样自动回滚；`kill -9` 或断电后由开机单元 `proxycontrol-update-recovery` 在服务启动前完成回滚。目标版本打不开当前数据库（降级跨过了迁移）时不做任何替换，并列出可用 `--database` 一起回退的 `pre-update-*` 备份。只有回滚也失败时才保留快照并打印日志命令（细节见[部署与升级](deployment.md)第 5 节与 5.1）。 |

菜单只调用上面这些固定操作，不提供 shell 或日志查看入口。

**非交互等价写法**（第 1 项的等价命令；面板已停止、以服务账号运行，两行分别是新用户名与口令，用户名为空行表示保持不变）：

```sh
systemctl stop proxy-panel.service
printf '%s\n%s\n' 'owner-name' 'new-password' | \
  runuser -u proxy-panel -- /usr/local/bin/proxy-panel --config /etc/proxy-panel/config.yaml --set-owner-credentials
systemctl start proxy-panel.service
```

## 15. 删除节点

删除是**逻辑删除**：节点记录保留（`nodes.deleted_at`），证书立即吊销，节点退出所有节点组、不再出现在订阅与列表中，历史流量与访问记录保留，且不能再次启用。未登记且没有身份的节点走“取消登记”，记录直接移除。

1. 打开节点的删除预览（页面上的“删除”，或 `GET /api/v1/nodes/{id}/deletion-preview`），确认 `mode`：`cancel`（未登记）、`delete`（已登记）或 `deleted`（已删除）。预览同时给出阻塞项 `blockers` 与所属节点组 `groups`。
2. 逐项处理阻塞项：入站 / 出站 / 出口组 / 其他节点指向它的中转出站 / 代理链 / 分流规则 / 进行中的批量发布。入口节点的出站被删除后，**重新发布仍经由该节点中转的其他节点**，否则它们会继续按旧配置连接一个已吊销的节点。
3. 可选：让离线节点先复连一次，把本地缓存（`/var/lib/proxy-agent/spool`）中的访问与流量记录补传完；删除后未上报的记录将丢失。
4. 在服务器上停止或卸载 Agent（`systemctl stop proxy-agent`，或按[部署与升级](deployment.md)卸载脚本）。**面板无法停止远端代理**：删除只吊销证书，不会结束正在转发的进程。
5. 面板确认删除。删除同时关闭该节点自己的待发布配置、核心/Agent 更新任务与排队中的健康检查；返回 409 `node_delete_blocked` 时先回到第 2 步。

**服务器已经丢失 / 无法登录**时：直接删除即可，不需要先停机；删除后该节点不会再出现在订阅中，旧证书在 mTLS 上被拒绝。

**重新接入同一台服务器**：删除后不能复用原节点记录（也不能重新登记），请按第 5 节“复用同一台机器重新登记”作为**新节点**接入，并把引用旧节点的出站、出口组成员与代理链改指新节点后重新发布。新节点需要重新加入原节点组。

## 16. v1.1 账号、订阅与客户端策略

Owner 在用户详情开启门户登录并设置密码；代理凭据、订阅 Token、登录密码是三种不同操作。门户从 `/portal/login` 进入，显示本人状态、授权节点、流量、共享订阅与账号；被禁用/到期/超额仍可登录看原因，但不能取订阅。关闭登录、改密码或撤销会话立即生效；恢复备份结束门户会话。Owner 预览只读，不签发 Token。Viewer 留在后台只读视图。

每用户一个共享链接，无设备新增/改名/删除。新 Token 可再次查看、复制或生成二维码，旧 Token 显示不可恢复时需显式确认重置。重置链接立即撤销旧链接；重置凭据通过自动版本替换代理设置，节点成功应用前旧设置仍可能使用。0113 移除多余设备及其链接，主身份链接保留，升级前通知受影响用户。历史记录仍保留内部设备 ID，页面隐藏设备列与筛选。

在“客户端策略”编辑唯一全局模板，选择预设后保存为独立副本。服务端校验组引用、循环与最终 MATCH，按用户授权展开 `@all`、节点组及导入来源；保存遇到 409 时重新读取并合并，不覆盖别人的新版本。用用户/格式预览看过滤警告；客户端模板不覆盖服务器节点策略。provider 是订阅 Token 授权的 `/sub/{token}/rules/{id}` classical 文本，撤销旧链接也撤销 provider。格式从注册表读取；mihomo 完整规则、legacy 受限，URI/Base64 无规则，sing-box 仍是片段。

格式显式参数优先。UA 开关已有，但可信匹配表因 Phase 0 A.7 未完成而为空；未知 UA 使用默认格式。当前只提供复制与二维码，不宣称未核实的一键导入协议。使用前选好显式格式，不依赖尚未验收的识别。

外部订阅通过导入对话框保存 HTTPS 来源并授权用户；缓存刷新不发生在 `/sub` 请求内。逐跳公网地址校验与固定 IP、2 MiB/15 秒上限限制抓取；失败来源只给 Owner 预览警告。导入内容加密保存，支持中立模型内的 URI/Base64/Clash 条目，不透传 VMess、Reality、插件、不安全 TLS 或未知字段。旧浏览器导入不自动迁移，须重导入并授权。客户端链仅 mihomo `dialer-proxy`，其他格式省略依赖链并警告；凭据/端点变化需重新选择画布引用。导入不进入 Agent、Revision、配额或设备限制；不能转为被管理的服务器隧道（L3、L5、L6）。

qui sing-box 增加上游 `with_v2ray_api`，声明 `singbox_user_stats` 后逐用户字节计入原有共享额度，官方无标签构建仍仅节点总量。升级后 sing-box 用户可能更早达到额度，升级前核对用量、额度并告知用户。计数使用累计游标和 spool 重传，不重置核心计数。原生双用户载荷与 Xray 对比证据见[验收矩阵](v1-acceptance.md) §10.4，主机结果见 §11。

## 17. v1.1 元数据、探测、转发与重置周期

分组迁移（0122）：节点分组改为具体入站及导入代理。升级前备份；原服务器成员只展开迁移时订阅可见、非内部中转的入站，后建入站须手动加入分组。原来源授权迁移为“导入 · 来源名”分组，同名时加唯一后缀，用户授权随迁；`user_source_grants` 保留但不再生效。来源接口 `userIds` 输入被忽略，输出从有效分组推导。导入单项按 `name|protocol` 匹配，同名同协议条目共享权限；名称或协议改变后不再匹配，选择整个来源则自动包含刷新后的全部代理。缓存条目暂时消失时成员记录保留，不自动转成来源授权。

`node_group_members` 是入站所属服务器的派生索引，供服务器视图、节点策略及批量操作使用；订阅、门户可用服务器与凭据渲染按具体入站及其 `allowedUserIds` 过滤，服务器订阅开关继续生效。进程内恢复沿用当前分组、成员和用户授权，缺失入站不恢复引用，避免旧备份恢复已撤销访问；旧归档先迁移 0122。额度与删除用户仍覆盖已发布状态里持有其凭据的服务器。

节点元数据 `portRangeStart/portRangeEnd` 同时设置或同时为空，1–65535。升级提取备注末尾有效 `[ports:A-B]`，无效后缀保留。自动分配按数字端口跳过所有入站（含禁用/范围）、持久声明与本计划占用，不区分 TCP/UDP；显式端口不受范围限制。v1.4 隧道/规则无范围且未手填时返回 no_free_port；仅画布保留 TLS 8443 / WireGuard 51820 默认，已设范围耗尽仍报错。端口建议不预留，保存时再检查；手动保存可复用禁用入站端口，但 port_claims 始终占用。ports/check 仍报告禁用入站，供编辑器提示。

服务器详情可保存地区、备注、续费日期、周期、价格、币种与提醒天数。首次 Owner 读取可导入本机旧元数据一次；服务器已有值优先，失败保留本机数据，Viewer 不导入。到期窗口内通过持久化每日标记发送 `system.alert`，不是支付或自动续费。宿主/核心 uptime、网卡速率和只读 sysctl 来自实际观测，旧 Agent、首个样本或过期心跳显示“—”；基础调优和恢复已获 Owner 批准，由固定白名单 helper 执行。月节点流量标为保留记录，基于面板时区内的 node_traffic_samples 去重视图，清理后不能保证完整月（L9）。

在节点测试位置配置具名主机/端口并发起 TCP 探测；非公网目标必须 Owner 显式允许。面板解析并校验全部 IP，Agent 再校验字面量地址、次数与期限；最多 10 次、每次 3 秒，每节点同时一个待执行请求，历史每节点/目标最多 100 条。结果的丢失率指 TCP 建连失败比例，中位数只取成功尝试；全失败显示未知中位数。代理列表“连通性”从面板主机测自建入站首个解析地址的 TCP/已配置的可信 TLS，不关闭证书验证，不测带宽或 UDP/QUIC，也不改变节点可用状态（L10）。

定时 TCP 检测由面板每 1 分钟排队：目标开启 `scheduled` 表示系统默认（系统最多 5 个，超限保存返回 409 `scheduled_target_limit`）；节点默认继承，可用 `GET/PUT /api/v1/nodes/{id}/latency-targets` 配置每节点最多 5 个自定义目标，`custom=true,targetIds=[]` 停止该节点监测，`custom=false` 清除自定义目标并继承系统默认。GET 允许 Owner/Viewer，PUT 需要 Owner + CSRF，超过 5 个返回 409 `node_target_limit`，未知目标返回 400 `unknown_target`，重复 ID 返回 400 `invalid_request`。节点须启用、未删除、支持探测且 45 秒内有心跳。每次 3 次建连、每次 3000 ms，沿用公网地址校验及 Owner 的非公网许可。手动请求优先，等待已发出的单条探测完成或 2 分钟期限结束，不受定时队列的 `probe_pending` 阻挡。定时数据独立保存 7 天，不占手动的 100 条历史；`GET /api/v1/nodes/{id}/latency?hours=24` 支持 1–168 小时，Owner/Viewer 可读，目标写操作仅 Owner。现有 Agent 只上报 `medianMs`，均值和极值字段为 null；全部失败记录 sent=3、failed=3，耗时为 null。删除目标同时删除采样，已删除节点的采样不再提供查询。便携备份不含延迟采样和定时任务，保留系统默认、节点自定义目标与继承开关；0121 之前归档可恢复并迁移为继承默认。进程内恢复沿用当前节点目标设置及所需目标引用；恢复后等待新采样。查询目标包含当前有效目标和范围内有采样的历史目标。间隔固定，不提供设置项。

普通端口转发在入站设置源监听、目标与 TCP/UDP，通过节点发布部署；v1.4 的规则在隧道管理创建，画布不再新增转发。需 Agent 声明 forward 能力；Xray 为 `dokodemo-door`，sing-box 为 `direct` override，GOST 为原生转发。转发不带用户身份，不直接进入订阅/用户额度/设备限制/域名归属；关联规则可另外生成目标用户入站的中转订阅（第 21 节）。转发字节只进节点/核心总量，relay 搬运服务排除。

画布生成/接管入站按 `canvas_tunnel_objects` 的 ID 归属，普通编辑不能改名；不按名字前缀识别。v1.4 隧道/规则生成入站另由 `tunnel_objects` 管理，返回 managedBy，普通 PUT/DELETE 均为 409 managed_by_tunnel，须回隧道管理修改并发布。目标入站删除遇 inbound_has_relays 时，先删除关联规则并成功发布移除，再删入站。

用户重置周期可选不重置、每月或每 30 个本地日，存储独立周期、锚点与已处理边界；每月保留原日号、短月夹紧，下月恢复原锚点，30 日按面板时区日历运算而非固定秒数。调度器清零使用量，恢复活跃通过原额度自动发布。0118 迁移闭合的旧 `[config:…]` 前缀并去除旧 up/down 限速元数据；每用户限速已移除（L1），普通备注保留。恢复保守保留当前重置边界，避免旧备份再次清零；停止/恢复取消并等待调度任务。月末、时区与重启边界有假时钟测试，Linux/浏览器观察仍待 Owner。

## 18. 主机观测、续证与调优 helper

Owner/Viewer 可读 `GET /api/v1/nodes/{id}/metrics?hours=1..168`（默认 24）。每节点每分钟保留最后一条新鲜心跳，7 天清理；≤12 小时原始点，>12 小时分桶步长 ceil(hours×60/720)×60 秒。响应 intervalSeconds 为桶步长、sampleSeconds 固定 60、points 缺测为 null；桶内只平均已测值，不补零。非 lo 接口速率合计是宿主流量，可能跨接口重复计数。HostInfo 和 heartbeat 可空指标随节点详情提供，缺测不得当作 0。

`GET /api/v1/nodes/{id}/tuning` 返回期望 desired、操作 status、逐键 result 与末次心跳 current。Owner 通过带 CSRF 的 PUT 提交完整字符串 settings、clear 和匹配 confirmNodeId；新 profile 省略旧键会恢复其初始值。clear=true 只能带空 settings，空 profile 同样清除。只接受 13 个固定 sysctl 键和 Agent 相同边界；tcp_rmem/tcp_wmem 仅填最大整数。无能力返回 409 tuning_unsupported，离线/过期心跳返回 409 node_offline，同节点 pending 返回 409 tuning_busy。成功受理 202 仅表示排队，操作日志为“修改服务器调优”。

等待结果期间其他心跳照常确认；断流后 pending 变为 unconfirmed，不自动换 ID 或重发。unconfirmed 时先查看后续 current、节点日志及 helper status.json，再由 Owner 明确提交新操作，不能根据本地期望宣称已成功或未应用。应用成功也要按实际读回值核对。

便携备份排除 node_metric_samples，快照删除后压实；恢复再次清空采样。node_tuning 随备份保存，但恢复合并保留当前库的调优行，删除仅在归档存在的行，pending 变为 unconfirmed，不下发旧操作。恢复不会修改服务器 sysctl，恢复后重新核对观测。面板自动续证准入已与 对齐：证书满 30 天或剩余不超过 14 天，沿用原密钥与现有确认流程。

主机信息随 Capabilities 重发刷新；磁盘总量/用量与 disk_percent 都取 Agent 状态目录所在文件系统。load、进程数、TCP 全状态套接字数、UDP 套接字数及内存/交换/磁盘字节来自 Linux 实际读取，失败不填。tuning_current 是完整当前值，tcp_rmem/tcp_wmem 含三个整数；下发这两个键时只填最大值，前两个值保持应用时的现值。

自动续证使用原私钥，证书满 30 天或余期不超过 14 天即在连接内尝试；失败退避 1 小时，离线不执行。面板须支持相同的自动续证准入，否则旧面板会返回 renewal is not due；90 天签发期不变。

调优是 Owner 发起的固定特权操作。Agent 将 request_id、settings、clear 写入 /var/lib/proxy-agent/tuning/desired.json（0600），helper 返回同目录 status.json。status 为 applied/cleared/failed，keys 为逐键读回值与错误；30 秒未等到匹配 request_id 只表示结果未确认，不能宣称没有应用。请求 ID 须唯一，不自动重试未确认的变更。

- systemd：查看 `systemctl status proxy-agent-tuning.path proxy-agent-tuning.service`、`journalctl -u proxy-agent-tuning`；path 应保持运行，oneshot 执行完为 inactive 正常。反复失败后仍应接收新请求。
- OpenRC：查看 `rc-service proxy-agent-tuning status`、syslog 的 proxy-agent-tuning；检查 `/usr/local/lib/proxy-agent/tuning-watch`、busybox inotifyd 与启动运行级。
- 检查 desired/status 的 request_id 是否一致及 keys 错误；算法必须已在 tcp_available_congestion_control 中。helper 不加载模块，不调用 shell 或 sysctl 命令。容器只读 procfs、权限不足、内核不支持或读回不符均报告失败，并尝试恢复全部原值；恢复失败也逐键记错，须管理员核对真实 sysctl。
- `/etc/sysctl.d/99-proxy-tuning.conf` 保存成功配置；root 0600 的 `.proxy-tuning-snapshot.json` 保存首次应用前原值，不由 Agent 写。空 profile/clear 删除配置并恢复原值。卸载先停 watcher、恢复快照，恢复失败保留程序并中止卸载。不要手动删除快照来处理失败。

systemd helper 是 root oneshot，能力上界为 CAP_NET_ADMIN + CAP_DAC_OVERRIDE（后者读取 Agent 的 0600 请求并写入其 0700 目录），fs.file-max 靠 root 文件权限；/proc/sys 默认只读，只开放 13 个固定键。OpenRC 同为 root/CAP_NET_ADMIN + CAP_DAC_OVERRIDE，但缺少 systemd 文件系统隔离。若宿主策略拒绝写入，保留失败结果；不扩权。Linux/root 与 OpenRC 的主机结果见[验收矩阵](v1-acceptance.md) §11；可运行 `sudo bash deploy/linux/tests/tuning-helper-test.sh` 验证隔离的触发/重放流程，Go tuning 测试用假 proc 验证应用/恢复，不改宿主。

### 整机月流量（v1.3 T1）

每台服务器可以按所选单张网卡统计本期的整机网络用量，用来与供应商的月流量额度对照。它是整机流量，不等于代理流量，也不进入用户额度、订阅或计费。Agent 每个心跳上报各非 lo 网卡的累计字节、`bootId` 与 IPv4 默认路由网卡，面板在同一心跳事务里按（服务器、网卡、面板时区日期）写入日行并长期保留；首次采样、主机重启或网卡计数回退只重建基线。

设置位于“服务器详情 → 设置 → 整机流量”，单独保存（操作日志“服务器流量设置”），不需要发布配置、不影响节点运行：

- 统计方式：入站 + 出站（`sum`，默认）、仅出站（`outbound`）、入站与出站取较大值（`max`；取本期两个方向合计的较大值，不是每日最大值的合计）。
- 统计网卡：留空为自动，即最近上报心跳的 IPv4 默认路由网卡，界面显示“自动（eth0）”或“自动（—）”；也可以填写明确网卡名，离线或尚未上报的网卡同样可以配置，统计到数据后才有数值。
- 额度：可选，1 字节以上的整数，界面按 GiB/TiB 输入；只用于展示与进度条，不限速、不停用服务、不告警。
- 流量重置日：1–31，默认 1；短月份按该月最后一天（1 月 31 日 → 2 月 28/29 日 → 3 月 31 日）。周期是半开区间，边界瞬间开始新周期；重置不清零计数、不删除日行，只改变汇总区间。

查询：Owner/Viewer 都可读 `GET /api/v1/nodes/{id}/machine-traffic` 与批量 `GET /api/v1/nodes/machine-traffic`；设置用 `GET/PUT /api/v1/nodes/{id}/machine-traffic/settings`，PUT 仅 Owner 且要求 CSRF，四个字段整体替换（清除额度时显式传 `allowanceBytes: null`）。服务器列表的“本月整机流量”与详情页的系统卡片共用同一份摘要；详情页另一个“代理流量”是账户样本口径，两者不能互相替代。

口径与限制（全文；界面提示只显示摘要）：

> 整机流量按所选单张网卡统计，包含所有服务；不等于代理流量或供应商账单。
> LXC 只能看到容器自己的网卡，不能统计物理宿主机。首次采样仅建立基线；
> Agent 离线期间若主机重启，跨重启的流量无法补计。同次启动的离线流量可由
> 累计计数补计，但跨日分配按时间比例估算。网卡计数重置会重新建立基线。
> 自动网卡来自 IPv4 默认路由；策略路由或 IPv6-only 请手动选择。
> 修改网卡会重新计算所选网卡的本期数据；修改面板时区不会重新分桶历史数据。

同一次启动内中断的采集按时间比例分摊到跨过的日期，所以日行是估算，不保证与宿主或供应商逐日一致。没有本期日行或解析不到网卡时用量为未知，界面显示“—”，不显示为 0；已有日行但当前心跳过期时显示已测总量并标注“截至上次采样”。切换网卡或统计方式会按保留的日行重算本期，不删除数据；改额度只影响展示。修改面板时区后，之后的流量按新时区分日，已记录的日数据不重新分桶。三张机器表（`node_machine_traffic_settings`、`node_machine_traffic_daily`、`node_machine_traffic_cursors`）随加密备份保存，恢复以当前库的用量为准。

## 19. 多核心运维

**看节点到底有几个核心**：`GET /api/v1/nodes/{id}` 的 `capabilities.cores` 是 Agent 报告为**启用**的核心清单（`name/version/protocols/singbox_user_stats/forward_inbound_supported/device_gate_supported/observation_contract_version`），`heartbeat.cores` 是逐核心运行状态（`name/healthy/error/started_at/traffic_source_status`）。未上报 `multi_core_supported` 的旧 Agent 仍显示一个核心（`capabilities.core_name`），缺失的观测保持缺省而不是 `false`。`core_selectionBlocked` 列出当前**不可选**的组件（正在添加或移除）。

**给入站指定核心**：入站表单的 `core` 留空表示“当前主核心”，不要为了跟主核心显式绑定而填值——主核心换人后空值会跟着走，显式值不会。保存与发布都会按该核心的能力拒绝不支持的协议、转发入站、WireGuard 与设备门控；同一节点的监听端口在所有核心与端口转发之间**唯一**（同一数字端口即使传输不同也不能复用）。

**添加缺失核心**：服务器 → 代理核心，`POST /api/v1/core-updates`，`component` 填 `xray`、`sing-box` 或 `gost`，并带当前已确认修订、官方（或严格匹配的 qui）资源 URL 与归档 SHA-256；GOST 的版本、URL 与摘要固定，见第 20 节。每个（节点、组件）同时只有一个活跃任务，不同组件可以同时排队。任务 `success` 只表示镜像切换成功；只有下一次能力上报里出现该组件，它才会从 `core_selectionBlocked` 消失并可选为入站核心。

**移除核心**：先删掉该组件上的所有入站（包括禁用的），发布并确认该修订，然后 `POST /api/v1/nodes/{id}/cores/{component}/remove`（带 `confirmNodeId` 与当前已确认修订）。拒绝码：`core_in_use`（错误体 `inboundIds` 列出仍占用该组件的入站）、`core_pending`（该组件正在添加/移除或有部署占用）、`last_core`（最后一个启用核心不能移除）、`revision_mismatch`（提交的修订不是当前已确认修订）。断线后用同一 id 重发即可，回执幂等；在能力上报确认之前该组件仍然显示。安装器拥有的二进制不会被 Agent 删除，只会被禁用，重新安装同组件即重新启用。移除成功时 Agent 还会删除该核心的 `runtime/<组件>/`（运行标记、accounting key、访问日志等）：确认退出之后才删，退出未确认（`ErrCoreStopUnconfirmed`）时目录、清单与镜像都保留，所以看到该目录存在且移除报这个码，说明进程没退干净，别手工 rm。`cores.json.enc`、更新历史与安装器二进制保留；重新添加同一组件会建全新的运行目录。移除后要回滚到使用该组件的历史修订会被拒绝（`rollback refused: …`），必须先装回组件或发布一份显式指定核心的新修订。

**逐核心重启/健康检查**：`POST /api/v1/nodes/{id}/restart-core` 与 `/health-checks` 的 `core` 留空是整节点操作，填 `xray`/`sing-box`/`gost` 只作用于该核心；历史回滚始终是整节点。执行结果在 `GET` 同名路径的历史里带 `core`。

**按核心计量**：协商了 `node_traffic_supported` 的 Agent 上报核心/节点总量，包含 Xray 的已渲染转发与 sing-box/GOST 转发字节，不与账户样本重复相加。用户额度只计账户流量，官方 sing-box 缺用户统计标签时仍未知；旧 Agent 未计入的转发字节不补记。累计游标、epoch 与统计源规则见[项目说明书](../AGENTS.md) §21。


## 20. GOST 运维

**它是什么**：GOST 是第三种核心类型，只做端口转发（`forward` 入站，TCP/UDP），固定官方 3.3.2，只有 linux/amd64。它不承载用户：没有凭据、订阅、额度、设备门控与域名记录，凡是在页面上找不到“GOST 用户”的入口，都是设计如此，不是缺页。

**装在哪里、怎么跑**：二进制 `/usr/local/lib/proxy-agent/gost`（安装器持有，Agent 只禁用不删除）；Agent 把该修订的原生 JSON 生成为 `<Agent 状态目录>/runtime/gost/config.yaml` 后启动子进程，没有独立的 systemd/OpenRC 单元。日志级别固定 `warn`、格式 `text`（默认级别会按连接刷屏并淹没 journald），子进程 stderr 只保留最近 4 KiB 作为失败原因。空闲常驻内存约 45 MB（G0 实测口径），远高于 Xray 与 sing-box，因此只在确实需要转发的节点上安装。指标只走运行时目录内私有 unix socket `runtime/gost/metrics.sock`（配置里写成 `unix://…`），不开放 HTTP、pprof 或任何公开监听；该 socket 提供节点累计字节，供节点总量使用。

**健康怎么读**：GOST 的“健康”只表示**运行期转发可用**——确切子进程存活 ＋ 每个生产 TCP 监听可拨通 ＋ 每个生产 UDP socket 归属该子进程。失败会带 `core.ErrListenerUnavailable` 并在错误里点名监听地址，逐核心健康与错误码见节点详情与 `GET /api/v1/nodes/{id}/health-checks`。两点不要误读：指标读取失败只改统计源状态，不判核心不健康；**目标 IP 是否可达不属于核心健康**，`tcp-probes` 与手工验证才说明目标状态。

**发布与重启**：配置改动走正常发布；当前实现一次发布会重启该节点的**全部**核心，所以新增/修改 GOST 转发会短暂中断同节点 Xray 与 sing-box 的连接，反之亦然。GOST 进程崩溃只由它自己的 supervisor 槽位按既有退避恢复，不会重启其他核心。发布失败按第 10 节的回滚规则回到上一镜像/上一修订。

**安装、更新与移除**：面板“服务器 → 代理核心 → 添加内核”选 GOST 会预填 3.3.2、固定 URL 与归档摘要，其它版本/摘要一律拒绝；排队还要求该 Agent 上报了 `Hello.gost_core_supported` 且平台是 linux/amd64。移除走“内核管理 → 移除”，前置条件与其他核心相同（当前草稿与已发布修订都没有 GOST 入站、不是最后一个核心）。**把 Agent 降级到 1.3 以下之前必须先移除 GOST**。

**它永远不是主核心**：GOST 不能作为节点的第一个（主）核心；节点始终保留 Xray 或 sing-box 作为主核心，`--core gost` 单独使用或排在 xray/sing-box 之前的安装参数会被拒绝，`cores` 首项为 gost 的配置也一样；移除最后一个 Xray/sing-box 会被拒绝（`last_core`），因为 `core` 留空的入站与设备门控等都跟随主核心。GOST 只能是附加核心。


### GOST 转发的池、端口范围与限流

目标池、端口范围与限速只在**节点配置 → 编辑转发入站**里配置；新隧道规则为单目标单端口，画布不再创建转发。字段与校验：

- 目标池 `targets`：1–32 个有序 `{host,port}`；与旧的单一 `host`/`port` 互斥。`round` 轮询，`fifo` 主备（默认 `round`）。`max_fails` 1–100（默认 1），`fail_timeout` 为 1–3600 的整数秒（默认 `5s`）。
- 端口范围 `port_end`：含尾端，起端是入站的监听端口，最多 256 个端口。GOST 的 `addr` 不支持范围，所以 Agent 把范围展开成**每个端口一个服务**（`tcp,udp` 再乘 2），每个服务都带完整的目标池与自己的选择器/限速/连接上限；单节点最多 1024 个生产服务。
- 限速 `bandwidth_in_bps`／`bandwidth_out_bps`：字节/秒，0 表示该方向不限，否则 1024–1000000000000。界面按 KiB/s 输入。GOST 用 `ParseBase2Bytes` 解析，裸整数会被拒绝，因此渲染为 `<整数字节>B`（`0` 是上游唯一特殊处理的“不限”）。限速按**每个展开后的服务**各自生效，不共享额度。
- 连接上限 `max_connections`：0–100000，只统计 **TCP 并发连接**；UDP-only 转发不允许设置（G0 只测量了 TCP 并发）。Agent 的监听健康检查每次也会短暂建立一个连接（GOST 随之连一次目标），会暂时占用一个名额，所以上限很小（如 1–2）时偶尔会有一个连接因此被关闭，上限请留出余量。

**行为与限制**：主备（`fifo`）切换不是无损的——每个失败窗口内客户端会遇到**一次**被接受后关闭的请求，之后才走备用目标；轮询（`round`）同样会在标记失败后才避开坏目标。超过连接上限的连接同样是“先接受再关闭”，所以监控上表现为短连接失败而不是拒绝握手。目标全部不可用时转发仍会接受连接然后失败，健康检查不因此判核心不健康（核心只检查自己的监听），请用 `tcp-probes` 或真实流量验证目标。修改这些设置后发布配置会重启该节点的**全部**核心（含 Xray 与 sing-box），既有连接短暂中断。转发字节只计入节点总量（AGENTS §21 的 GOST 段），不进入用户额度、设备限制或域名记录。

### 旧画布 GOST 中继（v1.3.x 操作）

**v1.4 开发分支迁移说明（尚未发布）**：C1 已禁止新建画布端口转发，M1 在面板启动和恢复时把安全的旧转发移入隧道管理。转换本身不下发配置、不改端口、不签发证书，规则默认不显示在订阅。Owner 可读取 `GET /api/v1/tunnels/migration-report`，按中文原因核对仍在画布的未发布连接、缺失归属、共享路径不一致、缺失服务器或端口冲突；一次性迁移不会在 Owner 修改后自动重试。先核对原服务器配置和对象归属，再通过正常管理与发布流程处理这些连接。恢复仍需显式发布，既有密封凭据保持不变。下面的画布操作说明对应已发布的 v1.3.x；v1.4 的转换范围与验证见 [M1 报告](v1.4-m1-report.md)。

在画布上把一条 `port_forward` 连接的协议选成 `gost`，按普通端口转发填源监听端口、目标地址/端口与 TCP/UDP，再发布即可。**两端服务器都必须装了 GOST 核心**（两侧都上报 GOST 核心与设置契约 3），出口服务器的端口范围里要有空闲端口给中继监听；端口由面板分配，规则与入站一致（AGENTS §12）。入口节点把这条转发的目标经出口节点的 `relay` 监听访问，中继用面板按隧道生成的双向 TLS 证书认证；证书存在面板库的密封列里，CA 私钥签发后即丢弃、有效期 10 年，界面只显示代数、服务器名、到期与指纹。

**防火墙**：出口节点必须让入口节点能访问**中继监听端口**（TCP，端口范围里分配出的那个数字）。入口节点不需要对出口开放任何入站，也不需要放开 9443 之类管理端口。

**发布顺序与轮换**：GOST 隧道固定先发布出口（中继监听），再发布入口（拨它的链），批次回滚反向。隧道页面的“轮换凭据”（`POST /api/v1/canvas-tunnels/{id}/rotate-credentials`，带 `updatedAt`，Owner＋CSRF，记操作日志“轮换隧道凭据”）换掉整套 CA 与两张证书并把隧道标成 modified，由**下一次普通发布**完成切换；出口与入口两次部署之间隧道会中断几十秒到一分钟，这是可接受的代价。旧画布数据里没有凭据的隧道在第一次发布时自动补发一代，不改变发布状态。

**发布只覆盖改动的服务器**：画布发布只为本次绘制涉及的服务器创建版本（顺序仍是出口、中转、入口）；仅为中继依赖被纳入、且已运行相同配置与相同启用意图的服务器不再发布——没有新版本、没有部署目标、不重启核心，所以改 us2↔uk 的隧道不会再重启 alpine。想强制重发某台服务器，就改它的配置或从服务器页单独发布。`POST /api/v1/deployment-batches/relay` 这类显式中继批次仍发布全部成员。

**删除不等于远程撤销**：删除隧道不会给节点发撤销消息，**离线节点上仍在生效的旧凭据不会被撤销**，直到它重新连接并收到新配置（或手工移除那个中转监听入站）。要立刻断开时，先发布一份不含该隧道的配置并确认出口节点已停掉中继，再删除隧道。

**排查**：入口连不上中继时，先看出口节点 GOST 子进程的警告行（`journalctl -u proxy-agent` 里的 `tls: client didn't provide a certificate`）——这句话同时表示“没带证书”和“证书不是这条隧道的 CA 签的”；再确认中继端口在防火墙放通、两端 GOST 都在运行。

## 21. 隧道管理与中转方案（v1.4 草稿，未发布）

以下为开发分支行为，A1 实核、主机与浏览器验收结果待补。正式版 v1.3.1 仍按第 20 节旧画布流程操作。

1. 在各跳安装 GOST 并等待能力上报：两节点各跳契约至少 3，三节点至少 4；GOST 仅 linux/amd64，不能作主核心。为接收跳和规则入口设置服务器端口范围，或手填监听。接收跳须有公网地址或 connectHost 覆盖，防火墙放通前一跳到接收跳的 TCP 中继端口；规则入口按 network 放通 TCP/UDP。
2. 在“节点配置 → 隧道管理 → 隧道”创建 entry → [mid] → exit，最多三台不同服务器，传输为 TLS＋mTLS。入口不另占隧道监听；每个接收跳一个共享监听，多规则复用，保存后端口固定。
3. 在“规则”选择入口、监听端口、network 和隧道；不选隧道就是直接转发（有 GOST 优先用它，否则主 Xray/sing-box）。目标选 linked 用户入站或 manual host/port。linked 随目标公网地址/入站端口变化，出口即目标时用 127.0.0.1；manual 保持手填且不能对用户显示。也可在目标节点点“添加中转”选择已有隧道，创建由该目标节点持有的 linked 规则。
4. 保存只是草稿。选择隧道/规则先查看计划中的服务器、端口、路由、顺序、冲突与警告，再发布；按 exit → mid → entry 应用，失败反向回滚。选择隧道会纳入规则，选择规则会纳入其隧道及必要拨号依赖；逐服务器核对 applied/unchanged/failed/rolled_back/skipped。重复无修改发布不创建修订或批次。
5. linked 规则成功发布、启用且对用户可见后才追加中转订阅；用户须已获目标入站授权与已发布凭据。保留目标协议/凭据/TLS，显式原 server name，仅把地址/端口换成入口/规则监听，名称追加“经 入口节点”。UDP 协议须规则支持 UDP，其他须 TCP；导出器原有省略继续有效。Owner 的 shareLink 是合格用户的真实凭据预览，Viewer 恒为 null，草稿链接不代表已部署。

目标公网地址/入站端口变更后，已发布规则显示 needs_publish；核对新目标再发布，草稿不会提前改变订阅快照。删除已发布规则/隧道变 pending_delete 且只读，旧端口声明、运行设置与订阅快照保留到移除成功；隧道须先移除所有引用规则。目标入站被引用时不能删除。

轮换使用 `POST /api/v1/tunnels/{id}/rotate-credentials`，带最新 updatedAt（Owner＋CSRF）；重新签发各段材料、标 modified，再计划/发布。三节点两段使用独立 CA/叶子证书。监听先切换再切拨号端，期间可中断，TCP 会话与 UDP 关联需客户端重建。不要按旧画布地址轮换已迁移对象；删除也不保证立即撤销离线节点的旧凭据。

升级后由 Owner 读取 `GET /api/v1/tunnels/migration-report` 的 `{items:[{tunnelId,name,reason}]}`。逐项核对原画布、已发布修订、生成入站归属、端口/范围、目标地址及共享引用。未发布、缺失归属/服务器、目标池/范围或路径/端口冲突等留在旧画布；不猜测接管。报告是单次转换的持久记录，处理后不会自动重跑或消项；通过正常管理流程明确重建所需隧道/规则、预览计划，协调发布移除旧监听后再启用新监听。转换本身保留 ID、端口、材料与运行修订，默认订阅隐藏，首次无修改发布保持旧物化形态。旧归档恢复先升级再转换，新归档保留完成标记/原因；恢复标 modified，须显式发布。详见 [M1 报告](v1.4-m1-report.md)。

“中转方案”只建代理中转：可从节点“添加中转”新建并预选当前节点为入口，出口可选 qui 入站或外部代理出站。qui 入站出口使用独立内部监听/凭据；外部出站须属于前一跳且满足 TCP/UDP，HTTP 不支持 UDP。relay/exit 实例可填 relayPortMode/relayPort，优先旧 tunnelPort；未设范围才保留 8443/51820 默认。旧 port_forward/gost 连接在迁移前只读保留，可删除，不可新增或改动。

目标只能看到中继源地址，同一中继后的客户端在设备限制中合为一个来源；账户流量在目标计量，规则入口字节仅节点总量，中间/出口 relay 排除。UDP 经 TLS/TCP 封装，延迟可能受丢包和队头阻塞影响；监听健康不等于握手或目标可达，不承诺重启保留既有流或未测的 UDP 空闲寿命。
