# 运维手册

更新时间：2026-10-17（Phase 14）。性质：按当前源码整理的运维结论，不是运行证据。未在 Linux 实机执行；带 **[待实机]** 的操作属于 owner 的 Linux VM 验收范围（见[验收矩阵](v1-acceptance.md)）。安装与目录约定见[部署与升级](deployment.md)。

## 1. 日常检查

| 位置 | 看什么 |
| --- | --- |
| 总览 | 在线节点/用户/设备、今日流量、阻断次数、异常节点、最近访问与阻断域名、最近发布结果 |
| 节点 | 在线状态、资源、Agent 与核心版本、当前配置版本、出站网络（核心出站源 IP/网卡、服务器强制出站）、最后心跳 |
| 访问记录 | 全部访问 / 已阻断，可按时间、节点、用户、域名筛选 |
| 系统 → 配置版本 | 发布状态、差异、手动回滚入口 |
| 系统 → 备份 | 备份列表、校验、恢复预览 |
| `journalctl -u proxy-panel`、`journalctl -u proxy-agent` | 结构化日志（两个程序都写 stdout） |

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

**恢复后的预期差异**：所有管理员会话被清除（必须重新登录）；管理员口令按恢复点写回（恢复点之后的改密会被覆盖），恢复点之外的 Viewer 被禁用；恢复点里不存在的节点证书被标记撤销，节点状态回到 `offline`/`pending`，需要重新连接。

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

**代码与计划草案不一致（以代码为准）**：轻量重构计划第 7 节的草案写的是“禁用旧节点记录并重新登记”。按当前代码：

- 注册码只在节点记录**启用**时可用（`internal/panel/node/repository.go:88`、`repository.go:110` 要求 `nodes.enabled=1`），被禁用的节点不能登记；
- `node_identities.node_id` 是主键，登记时是普通 `INSERT`（`repository.go:121`），所以**同一个节点 ID 无法二次登记**；
- 面板只支持逻辑删除节点（见第 15 节）；被删除的节点记录仍保留，但不能再登记。

因此 CA 轮换后，节点必须以**新的节点记录**回来：在面板新建节点、生成注册码，节点侧按第 5 节「复用同一台机器重新登记」的步骤处理（只清空 `/var/lib/proxy-agent/identity` 不够：其中的 `identity.json` 会让 Agent 以 `node is already enrolled` 拒绝登记，受管状态与 spool 也会拒绝）。旧节点记录请禁用，避免它继续计入在线节点；新节点需要重新加入原节点组并重新发布配置。

仓库里的 `deploy/linux/panel-ca.sh` 执行与上面第 2–3 步等价的备份与删除：`--inspect` 只打印现状，`--rotate BACKUP_DIRECTORY` 备份 `ca.crt`、`ca.key`、`configuration.key` 并用 SHA256 校验后删除 CA 文件。它要求身份目录不是符号链接、权限恰好 `0700`、属主是 root 或面板服务账号 `proxy-panel`（打包安装在 `/var/lib/proxy-panel/identity` 上是后者），三个材料文件的属主必须与身份目录一致；服务必须已停止。它的用法文本和结尾提示都写明轮换后节点要以新记录回来。

## 5. 替换丢失或失陷的节点 **[待实机]**

1. 面板里**禁用**旧节点：这会让它的证书在 mTLS 上被拒绝（`internal/panel/node/repository.go:135` 要求节点启用；`TestDisabledNodeIsRefusedAtMTLS` 覆盖该行为）。
2. 新建节点记录并生成一次性注册码。
3. 按[部署与升级](deployment.md)第 4 节安装并登记新节点，然后把它加入原节点组并重新发布配置。

**复用同一台机器重新登记**（替换丢失/失陷节点、CA 轮换后都按此做）：只清空 `/var/lib/proxy-agent/identity` 不够 —— 受管 Agent 状态（`agent-release-history`、`releases/agent-installed`、`agent-jobs` 等，见 `internal/agent/agenthost/state.go:30`）会让登记以 `cannot enroll over supervised Agent state` 被拒绝，spool 队列也绑定旧节点 ID（`spool identity or schema mismatch`，`internal/agent/spool/spool.go:129`）。步骤：

1. `systemctl stop proxy-agent`；
2. 把整个数据目录移作备份：`mv /var/lib/proxy-agent /var/lib/proxy-agent.old-$(date +%Y%m%d)` —— 旧节点尚未上传的遥测与访问记录会随目录一起丢弃；备份只用于排查，确认无误后删除；
3. 重跑安装脚本（见[部署与升级](deployment.md)第 4 节）：`deploy/linux/install.sh --node --binary bin/proxy-agent --sha256 <sha256>`。它会重建 `/var/lib/proxy-agent`，属主 `proxy-agent`、权限 `0700`（`deploy/linux/install.sh:296`）；
4. 登记：`deploy/linux/enroll.sh --token-file FILE`；
5. 在面板把新节点加回原节点组，并把引用旧节点的出站 / 出口组成员 / 代理链改指新节点，然后发布配置。

不要把旧节点的私钥或数据目录拷到新节点上；`identity/` 里保存的是旧节点身份。

## 6. 额度与连接限制（软执行）

**每用户一个额度**（D3）：`users.traffic_limit` / `users.used_traffic`，跨所有节点累计；没有每节点预算、没有核内字节配额。

- 采样与上报：Agent 每 5 秒从核心统计采样写入本地 spool；心跳（面板指定 **15 秒**，`internal/panel/node/control.go` 的 `HeartbeatInterval`）里上传流量批次，面板收到 ACK 后才删除本地批次。
- 判定：用户处于 `quota_exceeded`（已用 ≥ 额度）、`expired`（到期）或 `disabled` 时，面板自动创建并部署一个**只调整用户访问面**的配置版本，把该用户的凭据从节点配置中移除；恢复（提额、清零、续期、启用）同样触发一次自动版本。
- 合并与去重：同一节点上的转换在 60 秒窗口内合并成一次发布（`internal/platform/server/quota.go`）；自动版本以节点**已发布**状态为基线（`mergeUserAccess`），除用户与入站的绑定取自当前草稿（这类未发布编辑可能被带出）外，未发布的 Owner 编辑不会被顺带推下去；中转凭据、出站、出口组、代理链与分流规则始终按已发布状态。
- 目标节点：该用户**当前授权**的节点，加上**已发布状态里仍持有该用户凭据**的节点（例如授权已移除但尚未发布），所以禁用也能到达这类节点。
- **超额窗口**：在**正常路径**上（节点在线、读取与发布都成功、没有其他部署占用该节点），超限到被切断之间最多有一个上报周期（当前 15 秒）＋最长 60 秒合并窗口＋发布耗时（Agent 应用并重启核心的时间），每次重试再加一个窗口。这段时间内的流量仍会放行，并且会计入 `used_traffic`。这是被接受的取舍。
- **核心重启**：每次自动额度版本和其他版本一样会在该节点重启核心（先停旧核心再起新核心），该节点上**所有用户**的既有连接都会短暂中断；60 秒合并窗口限制了重启频率。
- 面板重启不会引发发布风暴：状态机启动时先从数据库播种，只有状态**变化**才触发（`internal/panel/user/quota.go`）；启动后还会对每个有已发布状态的节点对账一次（`quotaDeployer.recover`），只为「用户访问面与节点正在运行的配置不一致」的节点创建版本，因此面板停机期间变成不活跃的用户、以及合并窗口里丢失的工作都会被补上；如果这次启动对账读不到状态（读取失败），面板会在每个 tick 重跑一次，直到一次完整读完为止。
- **失败重试与放弃**：节点繁忙或等待 Agent 结果只等下一个窗口，不算失败；读取、创建、部署失败，或 Agent 上报失败/回滚，则用**当前已发布状态**重新生成版本重试，连续 **3** 次失败后停止并告警 Owner：`system.alert`（`alert=user_access_failed`）＋系统 → 操作日志里的「自动额度发布失败」记录（面板日志 `automatic quota revision gave up`，可 `journalctl -u proxy-panel | grep "automatic quota revision gave up"` 查看）。
- **草稿无法渲染时的兜底**：Owner 已保存的草稿整体渲染失败时，自动版本退回按已发布状态生成，只移除不活跃用户（日志 `automatic quota revision rendered from the published state alone`），恢复类转换要等草稿能再次渲染。
- 每用户额度**只统计 Xray 节点**上看到的流量；用户被授权了任何 sing-box 节点组时，用户页会给出提示（D5）。

**并发连接是观测软限制**（D2）：面板不切断、不阻塞任何连接。

| 核心 | 面板显示 | 依据 |
| --- | --- | --- |
| Xray | 该用户的去重客户端 IP 数（回环地址排除），来自 `GetStatsOnline` | `internal/agent/core/xray/online.go` |
| sing-box | 未知；只有节点总量（Clash API `uploadTotal`/`downloadTotal`，自 Agent 启动起累计） | `internal/agent/core/singbox/node_traffic.go` |

连接快照保存在面板内存里，45 秒（3 个心跳）后过期视为未知，从不落库、从不编造数值。超过 `max_connections` 只在向上跨越时发一次 `system.alert`（`alert=connection_limit`），用户列表/详情页显示“连接超限”徽标。总览页的“当前连接数”、总览页的说明文字、节点页的“当前用户连接”和用户页的“当前连接”都按本节口径显示（Xray 为去重客户端 IP 数，sing-box 为未知）。面板显示的值是各凭据、各节点上报值之和，**不跨节点去重**；没有任何上报节点为该用户给出条目时（例如只用 WireGuard 的用户，`internal/agent/core/xray/online.go` 不为其产出在线条目）显示“未知”，而不是 0。

**续期刷新**：延长一个仍在活跃状态的用户到期时间不会产生“状态转换”，但 Agent 会按自己保存的到期时间切断用户，所以面板在到期时间被修改时会直接为该用户排队一次刷新（`reason=renewed`，不发告警）；如果这次修改同时跨越了活跃边界，则按上面的转换流程处理并发出告警。

**时间窗与到期的延迟（1.4 取舍）**：Agent 定时重渲染（时间窗规则）或应用失败时，会保持上一成功配置继续服务，并在下一个周期重试，不会退回全阻断、也不会停核心（`internal/agent/reconcile/schedule.go`）。所以时间窗阻断或到期**可能被推迟**到下一次成功渲染为止；同理，面板侧自动版本失败时，节点仍运行旧配置。

## 7. 访问记录与流量

- 记录粒度：分钟时间桶 × 用户 × 设备 × 入口节点 × 域名/IP × 规则 × 动作 × 配置出口，含连接数；同一分钟内同一组合只写一条。
- **每域名字节是可空字段**（D4）：官方核无法提供每域名精确字节，未知时列为 NULL，页面显示“—”。未知不写零。
- **配置出口**：页面上的“配置出口”是该节点按策略解析出的出站或出口组所选成员，表示**配置**而非跨节点实际到达的出口节点；跨中转链路的实际出口无法确认。只有本节点直连路由能确认出口节点，页面会区分标注。
- **sing-box 的访问来源是核心日志**：上游 Clash API `/connections` 只列出仍打开的连接、且不含用户，所以 Agent 读核心日志（渲染时提高到 debug 级别）来获得每条连接的用户、目标、命中规则索引和出站，包括被拒绝的连接（`internal/agent/collector/singbox.go`）。
- **Xray 的访问日志**在 Agent 运行时目录 `/var/lib/proxy-agent/runtime/access.log`：它是瞬时文件：Agent 以约 100 ms 轮询跟随，**读到就立刻清空**，核心停止时再清一次，因为明文 HTTP 行会带上 URL 路径与查询串；超过 **20 MiB** 仍会截断作为兜底，避免写满磁盘（`internal/agent/core/xray/access_log.go`）。检查大小与清空之间新追加的一行可能丢失（轻微少计，不会把路径留在磁盘上）。备份节点目录时不需要保留它。
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

`desired.json` 是 Agent 下发的目标状态（含随机 `id`），`status.json` 是 helper 对同一 `id` 的结果；两者不一致说明 helper 未安装或尚未处理完。

## 10. 核心与 Agent 更新

**代理核心（面板驱动，直接 URL + SHA-256）**：系统 → 核心版本里填 `组件 / 稳定版本号 / 官方 GitHub 发布资源 URL / 归档 SHA-256`。面板在入队时校验组件、纯发布版本号、摘要格式、URL 前缀和资源名与节点平台是否匹配（`internal/panel/deployment/core_updates.go`）。Agent 侧再次校验：URL 只接受官方前缀，重定向最多 3 跳且只允许 `github.com`、`objects.githubusercontent.com`、`release-assets.githubusercontent.com`；先校验归档摘要再解包；解包拒绝符号链接、路径穿越和多余候选；版本不匹配或健康检查失败时回滚到上一镜像（`internal/agent/updater/core_source.go`、`internal/agent/control/core_updates.go`）。排队、状态和结果见 `GET /api/v1/core-updates`；卡住的任务在节点重连时关闭，Owner 也可用 `POST /api/v1/core-updates/{id}/clear` 关闭。旧任务（无 URL）按 `failed_closed/history_invalid` 关闭。

**Agent 自身（签名目录）**：Agent 自更新仍然使用 Ed25519 签名清单，不改成 URL + SHA-256。

- 面板侧：`release_keys` 固定受信公钥；`POST /api/v1/releases` 保存签名条目，`POST /api/v1/agent-updates` 为节点排队（另有 `/recover`、`/attempts`）。
- 签名端：`cmd/release` 是离线工具，子命令为 `keygen`、`sign`、`verify`、`stage`、`fetch`、`prepare-agent`、`queue-agent`、`agent-result`。典型流程是 `keygen` 生成密钥对，`sign` 对本地二进制生成签名清单，`verify`/`stage` 校验并生成候选，`fetch`/`prepare-agent`/`queue-agent`/`agent-result` 面向本机已登记的 Agent 配置（`--agent-config`）。私钥始终留在离线签名端。
- 节点侧：Agent 用配置里固定的公钥校验清单，按安装历史防重放，失败回滚到已确认镜像；受管 Agent 启动时会自行修复中断的切换（`internal/agent/agenthost/recovery.go`、`internal/agent/reconcile/core_rollback_repair.go`）。

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
| 发布后节点回滚 | 系统 → 配置版本看差异与失败原因；核心原生校验失败、端口冲突、健康检查失败都会自动回滚并保留上一成功版本 |
| 出站源地址未生效或发布失败 | 是否已发布配置（字段改动只在下一次发布生效）；`nft list table inet proxy_agent_source`；`systemctl status proxy-agent-host-network`、`journalctl -u proxy-agent-host-network`；失败码 `network_unavailable` / `host_network_failed` 见第 9 节 |
| 用户仍能上网但已超限 | 自动版本尚未完成（最长一个上报周期＋60 秒＋发布耗时），或节点离线未收到版本 |
| 访问记录为空 | Agent 是否上报（`journalctl -u proxy-agent` 里的遥测错误）、核心是否在跑、保留策略是否已清理该时间范围 |
| 额度/在线数显示“未知” | 该用户只在 sing-box 节点上有流量（D5），或核心不支持/未启用对应统计（D2），或该用户在 Xray 节点上没有在线条目（例如只用 WireGuard） |
| 备份恢复失败 | 口令是否正确、备份是否完整（先 `verify`）、当前库是否被其他进程占用（恢复要求面板停止写入） |

## 14. 终端菜单 qui

面板主机上安装的是 root 专用终端菜单 `/usr/local/bin/qui`（`deploy/linux/install.sh --panel` 安装；SSH 会话里输入 `qui` 打开）。菜单固定只有以下项目：

| 菜单项 | 行为 |
| --- | --- |
| 1. 修改管理员账号密码 | 依次提示新用户名（留空保持不变）和两次新密码，确认后**短暂停止** `proxy-panel.service`，再以服务账号身份运行 `proxy-panel --config /etc/proxy-panel/config.yaml --set-owner-credentials`（用户名与口令两行从标准输入读入，口令不进 argv、不回显），然后按停止前的状态把服务启动回来。口令策略由程序校验，错误原样显示；成功后**所有 Owner 会话立即失效**，必须重新登录。 |
| 2. 修改端口 | 只改 `http_address` 的端口，`grpc_address`（节点连接）不动：校验端口是 1-65535 的整数且与当前不同，端口与 `grpc_address` 相同时给一行提示；在 `/etc/proxy-panel` 里生成候选文件，先让程序 `--check-config` 校验，通过后把原文件备份为 `/etc/proxy-panel/config.yaml.bak`（属主与 0600 权限和原来一致）再原子替换；校验或备份失败时删除候选文件、保留原配置。最后询问是否立即重启，并提示放行防火墙与含旧端口的订阅链接需要用户刷新。 |
| 3 / 4 / 5. 重启 / 启动 / 停止 | 直接 `systemctl restart`、`systemctl start`、`systemctl stop proxy-panel.service` 并打印结果状态；启动或重启失败会提示 `journalctl -u proxy-panel -n 50`。服务本来就在运行或已停止时只提示，不重复操作。 |
| 6. 更新（从 GitHub） | 运行 `/usr/local/bin/qui-update`：默认取 `https://github.com/jingniao/qui/releases/latest`（也可 `--version X.Y.Z` 指定），下载归档与同一发布的 `SHA256SUMS` 并核对摘要，停止 `proxy-panel.service`，在停止状态下把数据库备份到 `/var/lib/proxy-panel/backups/pre-update-<旧版本>-<时间戳>.db`，再用归档里的 `deploy/linux/install.sh` 原地替换程序与单元，最后按停止前的状态启动服务并打印结果；失败时打印 `journalctl`、降级命令与备份路径。 |

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

