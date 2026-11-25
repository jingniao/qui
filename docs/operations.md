# 运维手册

升级顺序为面板先、Agent 后；支持 systemd 与 Alpine/OpenRC。本文给出日常运维、故障排查与恢复步骤；安装与升级机制见[部署与升级](deployment.md)，产品行为与接口契约见[项目说明书](project-spec.md)。

## 当前生产环境

**最近核实日期：2026-11-25 04:45 UTC。** 本轮按 Owner 授权排查并恢复三台停机节点，将面板升级到 1.12.2 后完成美国 CC 与德国 netcup 的旧状态重建。Owner 会话不可用，未创建或校验手工加密备份，恢复点为正式更新器的数据库保护文件。后续生产操作仍须重新实时核实运行版本、迁移头、节点与备份状态。

| 项目 | 当前登记 |
| --- | --- |
| 服务器 / 面板地址 | `5.45.96.43`（德国 netcup，1 vCPU / 约 1 GB 内存） / `https://qui.lluu.de` |
| 面板版本 / 迁移头 | 1.12.2 / 0165（tag `984f332`；2026-11-25 04:39–04:41 UTC 升级；`panel.db` 约 466 MB，WAL 约 157 MB） |
| 节点状态 | 13 个节点，11 在线、2 离线（洛杉矶 rn、香港 阿里国际*1，本轮未排查）；台湾 wap 为 1.12.0，香港 阿里国际*1 为 1.9.0，其余 1.12.1 |
| 待完成事项 | 台湾 wap 升级前须先加 `ProtectKernelTunables=false` drop-in 并重启 Agent（本轮被会话权限策略拒绝，未执行）；洛杉矶 rn、香港 阿里国际*1 离线待查 |
| 本轮数据库保护文件 | `/var/lib/proxy-panel/backups/pre-update-1.12.1-20261125-053952.db`（正式更新器创建）；升级基线与日志在生产主机 `/root/qui-upgrade-1.12.2-20261125/` |
| 管理员登录 | 自定义地址不入文档；Owner 登录后在「系统设置」查看，忘记时用服务器 `qui` 菜单重置（需停面板），见[说明书 §27.18](project-spec.md#2718-系统设置管理员登录地址与门户字段v154) |
| 用户门户 | `https://qui.lluu.de/` |

2026-11-25 03:00 UTC 批量升级 Agent 到 1.12.1 后，德国 netcup、美国 CC、日本 zouter升级本身成功，旧状态重建封存超时，脚本退出后 Agent 与核心停止。根因是面板封存事务逐行改写十万级历史回执，超过 Agent 30 秒期限并独占写连接，已在 [v1.12.2](releases/release-notes-1.12.2.md) 修复。处置：日本 zouter 重跑重建完成（`complete`）；美国 CC、德国 netcup 面板侧身份未变，先手工启动原 Agent 与网络/调优监听恢复在线。面板以 `qui-update --component panel --version 1.12.2 --yes` 升级（退出 0，服务 active、`NRestarts=0`，配置 SHA256 未变，无 `rollback.*`/`discard.*`，`/healthz` 与门户 200，13 节点状态/版本/修订与升级前一致，升级后无新增面板 WARN/ERROR）；04:43 UTC 在美国 CC、德国 netcup 依次重跑重建，各约 10 秒 `sealed → rebuilt → complete`，核心运行并连上面板，三台重建的面板重部署状态均为 `success`（美国 CC 修订 203、德国 204）。台湾 wap（Proxmox LXC）因 `ProtectKernelTunables` 改变 `boot_id` 视图，自 1.10 起升级/安装均回滚，drop-in 操作本轮被会话权限策略拒绝，未执行。

此前 2026-11-24 记录：面板 1.10.1 → 1.10.4 升级（`qui-update --component panel --version 1.10.4 --yes`，数据库保护文件 `pre-update-1.10.1-20261124-032827.db`，证据在生产主机 `/root/qui-upgrade-1.10.4-20261124/`），详见 [v1.10.4 发布说明](releases/release-notes-1.10.4.md#生产面板升级)。Owner 登录操作、真实浏览器交互和真实代理请求本轮均未验证。

## 1. 日常检查

日常巡检关注：总览（在线数/今日流量/阻断/异常节点）、节点页（状态/版本/出站网络/心跳）、访问记录、系统 → 配置版本与备份、面板日志（`journalctl -u proxy-panel`）与 Agent 运行日志（`/var/lib/proxy-agent/logs/agent.log`）。面板通过单条 SSE（`GET /api/v1/events`）按角色推送状态与告警（Viewer 仅收监控类通知，每 15 秒重验角色）。

**在线但处理延迟**：节点的数据库心跳可因面板写库等待而变旧，已认证连接上的在途消息仍提供存活证据。页面在 10 秒后持续提示「面板处理延迟」，服务日志 `node message processing delayed` 带节点、会话、阶段、年龄及在途数量，每 60 秒重复。先排查面板数据库写入等待与存储状态；最后心跳和指标仍显示实际持久化时间。`sending_ack` 表示正在发确认，不能据此推断 Agent 已收到。页面提示「面板存活状态未更新，等待校准」时检查面板连接与只读接口，不将旧 connected 继续当作在线证据。断开 SSE 时仍冻结最后确认状态；不增加 Telegram/Webhook 的独立发送路径。

**节点服务管理（systemd 与 OpenRC 对照）**（部署见[部署与升级 §4.1](deployment.md#41-alpine--openrc-节点)）：

| 操作 | systemd | OpenRC |
| --- | --- | --- |
| 状态 | `systemctl status proxy-agent` | `rc-service proxy-agent status`（`started` / `stopped` / `crashed`） |
| 启停 / 重启 | `systemctl start\|stop\|restart proxy-agent` | `rc-service proxy-agent start\|stop\|restart` |
| 开机自启 / 取消 | `systemctl enable\|disable proxy-agent` | `rc-update add\|del proxy-agent default` |
| 全部服务 | `systemctl list-units 'proxy-agent*'` | `rc-status default` |
| 运行日志 | `tail -n 200 /var/lib/proxy-agent/logs/agent.log` | `tail -n 200 /var/lib/proxy-agent/logs/agent.log` |
| 启动与日志写入故障 | `journalctl -u proxy-agent -n 200` | `grep -F proxy-agent /var/log/messages \| tail -n 200` |
| 强制出站 helper | `systemctl status proxy-agent-host-network.path` | `rc-service proxy-agent-host-network status` |

OpenRC 由 `supervise-daemon` 监管：退出 5 秒重启；`stop` 先发 TERM，最多等 90 秒后发 KILL。

**Agent 日志容量（100 MiB 上限）**：运行日志位于 `<DataDirectory>/logs/`（默认 `/var/lib/proxy-agent/logs/`），含 `agent.log` 及 `.1`–`.9`，单文件 10 MiB、总计上限 **100 MiB**，最旧自动轮换删除（受管模式由主进程统一收集自身和子 Agent stderr）。此上限不含本地 spool、核心、安装/回滚文件或网页完整升级日志，不清理历史 journald/syslog。写入/轮转失败向服务日志报错并停写文件，父进程继续丢弃子进程输出防止代理退出，修好后重启恢复；已有受管节点须经终端 `qui-update --component agent` 更新引导并重启后核对 `logs/agent.log` 写入才算生效。

## 2. 备份与恢复

主机更新器的 Go 数据库工具安装在 `/usr/local/lib/proxycontrol/qui-update-db`。仍有任一组件时保留，最后卸载时清除。不要单独替换不同发布包的脚本和工具。中断快照使用自己的 `database-helper` 与摘要恢复；若缺失、不可执行或摘要不符，恢复明确失败且保留现场，不用当前安装工具代替、不手工删除 journal。先检查 `update-recovery.log` 与快照文件，并按原更新材料处理；详见[部署与升级 §5](deployment.md#5-升级)。

后续版本源码增加同包共享状态工具 `qui-update-state`，新快照必须带 `state-helper` 和摘要；新 updater 会先转交旧快照自己的 updater，再固定本轮工具和清理已丢弃目录。状态工具缺失或摘要错误时保留快照，不从其他版本补工具。“更新成功但快照清理失败”或“恢复已完成但快照清理失败”表示最终阶段已持久化，下轮只收尾；若提示“下次运行会回滚本次更新”或“下次运行会重新恢复”，先保留现场人工处理，面板 started 恢复会先保存当前库再用备份覆盖。报告工具/fd 失败会同时记录原操作错误，必要回滚继续执行，进程最终失败。

首次从 v1.10.4 及更早版本升级仍由旧 updater 执行：失败回滚时可能留下新安装的 `/usr/local/lib/proxycontrol/qui-update-state`，旧版卸载也不会删除它。从含该工具的版本成功降级到 v1.10.4 及更早版本时同样会保留它：旧版安装脚本不认识也不删除这个文件。这份工具本身不操作服务或联网，下次成功升级会覆盖；核实已回退版本且没有新格式快照后可人工移除。上述残留边界不影响旧快照自身恢复。

- **内容与计划**：加密备份（`.pcb`，默认 `<database 目录>/backups`，示例 `/var/lib/proxy-panel/backups`）含 SQLite 一致性快照、系统配置、中央证书/ACME/Cloudflare 凭据、规则集与密钥。默认每日 `03:00` 自动备份，保留 **7 每日 / 4 每周 / 3 每月**（口令加密存库）。面板支持手工备份、校验、预览、导入/导出及在线恢复（`POST /api/v1/backups/{id}/restore`，重启切换，记录见 `GET /api/v1/restore-records`）。
- **离线命令行恢复**（面板已停，恢复后直接继续提供服务；`--restore-backup` 与 `--restore-password-file` 须成对并显式给 `--config`，口令文件 ≤514 字节且仅属主可读，当前库空/缺失或校验失败拒绝替换）：

```sh
systemctl stop proxy-panel.service
/usr/local/bin/proxy-panel --config /etc/proxy-panel/config.yaml \
  --restore-backup /var/lib/proxy-panel/backups/<id>.pcb \
  --restore-password-file /root/backup-password
# 只读排查中断的恢复安装（不启动服务）：
/usr/local/bin/proxy-panel --config /etc/proxy-panel/config.yaml \
  --inspect-restore-installation --restore-installation-report /root/install-report.json
```

- **恢复后预期差异**：清除全部管理员与门户会话；管理员口令回退到恢复点，恢复点后新建 Viewer 与代理用户/设备被禁用（凭据撤销、已用流量不回退）；恢复点外节点证书被吊销并回到 `offline`/`pending`。

## 3. 节点证书

- 面板节点 CA 签发 90 天证书；满 30 天或余期 ≤14 天由 Agent 在控制连接自动续期（原私钥，失败每小时重试）。仅支持格式 2 身份存储（`identity/client.crt` + `client.key`），旧格式启动被拒需重新登记。吊销单证用 `POST /api/v1/nodes/{id}/revoke-certificate`。
- **手工续期 / 密钥轮换**（停服执行，面板须可达；中断直接重跑同一命令，待提交材料在 `identity/pending-key-rotation.enc`）：

```sh
systemctl stop proxy-agent.service
deploy/linux/renew-certificate.sh --renew        # 续期，保留原私钥
deploy/linux/renew-certificate.sh --rotate-key   # 轮换私钥并签发新证书
systemctl start proxy-agent.service
```

## 3.1 入站 TLS 证书：节点文件与续期

- **节点文件（`inbounds.tls_source=file`）**：证书与私钥仅限 `/etc/proxy-agent/certs/`（`root:proxy-agent 0750`，卸载保留），面板只存绝对路径（≤256 字节、无 `..`）。Agent 每 5 分钟重扫并上报摘要（不含私钥），应用时经 `EvalSymlinks` 校验目录、可读性、密钥对与未过期，失败回滚；私钥宽于 `0640` 记 WARN。**外部工具（certbot/acme.sh）续期后，须在「节点详情 → 重启核心」手动重启核心生效**，勿改名轮换路径。加密备份仅存路径，不含节点文件。
- **自签名（`self_signed`）**：按连接地址（其次公网 IP）签发，用户入站 10 年、内部中转 1 年；改连接地址后需在编辑器重新生成（否则 `coverageWarning` 提示）。

## 3.2 Cloudflare API Token 与自动申请

面板通过 Cloudflare DNS-01 向 Let's Encrypt 申请与续期证书（见[项目说明书 §27.16](project-spec.md#2716-系统管理证书与-cloudflare-dns-01)），在「系统管理 → 证书管理 / 设置」及 API 均可管理，入站选 `acme` 引用。

- **最小权限 Token**：禁用 Global API Key，不授 `Zone:Zone` / `Zone:Zone Settings`；在 Cloudflare 创建 Custom Token，Permissions 仅给 `Zone → DNS → Edit`，Zone Resources 仅选目标区域。保存前经 `/user/tokens/verify` 校验（无效返 `cloudflare_token_invalid`），保存后仅显示末 4 位。
- **staging 与自动 DNS**：先选 `staging` 测通申请/续期再切 `production`（换 CA/邮箱重注册 ACME 账户）。自动 DNS 仅对非通配域名写指向节点**公网 IP** 的 A/AAAA，强制 `proxied=false`（灰云，橙云不支持 UDP）、`ttl=1`；缺公网 IP 返 `dns_node_address_missing`。
- **退避与发布窗口**：失败按 1h → 6h → 24h 退避（`acme_rate_limited` 勿连续重试）；到期前 30 天续期，余 <7 天失败每日触发一次 `system.alert`。续期后仅在低峰窗口 `deployStartHour`–`deployEndHour`（默认面板时区 3–6 点，每小时检查）自动发布 `auto: certificate <域名> renewed`（会短暂重启节点全部核心）；连续 3 次失败停止并报 `certificate_deploy_failed`，修好后手动发布一次恢复。
- **恢复与出站**：备份含证书库、Token 与 ACME 密钥，恢复后不自动重申，中断任务按退避重试。面板须出站可达 `api.cloudflare.com`、Let's Encrypt API 与 UDP 53（无需开 80 端口）。

## 4. 手工轮换面板节点 CA

面板唯一节点 CA 位于 `node_identity_directory`（示例 `/var/lib/proxy-panel/identity`）：

1. 停止面板：`systemctl stop proxy-panel.service`。
2. 备份 `ca.crt`、`ca.key` 与 `configuration.key`（解密本机敏感材料必须一起备份），删除 `ca.crt` 与 `ca.key`、**保留 `configuration.key`**。可用 `deploy/linux/panel-ca.sh --inspect` 查看，`--rotate BACKUP_DIRECTORY` 校验属主/权限（非符号链接、`0700`）并自动备份后删除 CA。
3. 启动面板自动生成新节点 CA，**所有旧节点证书立即被拒**。
4. 逐节点恢复：面板禁用旧记录、新建节点并生成注册码，节点侧按第 5 节「复用同一台机器重新登记」处理并加回节点组重新发布。**无需替换 `/etc/proxy-agent/tls/ca.crt`**（它校验面板 Web 证书；仅换 Web 证书签发 CA 时运行 `deploy/linux/node-ca.sh --trust <new-ca.crt>`）。
5. 全部节点恢复后删除旧 CA 备份。（普通注册码仅用于启用且未登记的节点；普通重装请用第 5.1 节原位恢复。）

## 5. 替换丢失或失陷的节点

仅用于**主机失陷**或**面板节点 CA 轮换**（普通重装走第 5.1 节）：在面板**禁用**旧节点（mTLS 立即拒绝）→ 新建节点并生成注册码 → 按[部署与升级](deployment.md#4-接入节点)安装登记并更新节点组/出站/代理链引用后重新发布。

**复用同一台机器重新登记**（只删 `identity/` 会因受管状态与旧 spool 报错）：

1. 停服：`systemctl stop proxy-agent`（OpenRC 用 `rc-service proxy-agent stop`）。
2. 移走整个数据目录备份排查：`mv /var/lib/proxy-agent /var/lib/proxy-agent.old-$(date +%Y%m%d)`（未上传遥测丢弃，确认后删除）。
3. 重装并登记：`deploy/linux/install.sh --node --binary bin/proxy-agent --sha256 <sha256>`，再运行 `deploy/linux/enroll.sh --token-file FILE`。
4. 在面板加回节点组、修正引用并发布。严禁拷贝旧 `identity/` 到新节点。

## 5.1 原位恢复（推荐）

保留原节点 ID、入站/出站、节点组、隧道与历史配置；恢复码等同接管凭据（只显示一次，面板仅存 SHA-256 并绑定原证书指纹）。

### 路径 A：原服务器仍运行旧 Agent（`--replace-agent`）

服务器列表点「安装」，复制带 `--replace-agent` 的命令（码有效期 1 小时）以 root 运行：

1. 安装器校验发行包、签名、核心与 gRPC 端点后停服，用旧证书补传队列并逐批确认访问/流量 ACK、交付序号与结束确认；任一异常或存在隔离批次即报错并保留旧数据。
2. 确认后持进程锁清空 `/var/lib/proxy-agent`（保留 `/etc/proxy-agent/config.yaml` 其他项与 `/etc/proxy-agent/certs/`），安装核心并重新登记，核销时吊销旧证（自定义数据目录/符号链接/挂载点会拒绝）。
3. **中断接续**：部分批次已确认中断时，原队列与身份副本在 `/var/lib/proxy-agent/replacement-original.*`，修好后先启旧服再重跑；若已提示“旧 Agent 本机数据已清除”随后失败，去掉 `--replace-agent` 原机重跑（复用私钥）；若已有待确认 `enrollment-result.json`，保留原材料，将恢复码写入 root `0600` 文件 `/root/qui-recovery-token`，用同发布包运行 `deploy/linux/enroll.sh --token-file /root/qui-recovery-token`，成功后删文件并启服。
4. OpenRC 须已装 `strace` 观察原进程退出码；离线覆盖安装须将同版本同架构签名清单放归档旁。

### 路径 B：主机重装或干净新机器接替

1. 「节点详情 → 节点证书 → **原位身份恢复**」（或列表「安装 → 在新机器登记」）选有效期（15/30/60 分钟），不希望旧机续连时勾选“立即吊销当前证书”。有未完成发布/更新/移除任务返 409 `node_recovery_blocked`；未登记/禁用/已删返 409 `node_not_enrolled`。
2. 新机以 root 运行生成的 `node-install.sh`（复用旧机且留有 `/var/lib/proxy-agent` 先按第 5 节移走）。
3. **恢复前勿运行 `uninstall.sh agent`**：残留 `/var/lib/proxycontrol-install/agent.removed` 会让 `node-install.sh` 拒绝安装。若已触发，确认 `/var/lib/proxycontrol-install` 为 root `0700` 后执行 `mv /var/lib/proxycontrol-install/agent.removed /root/agent.removed.bak` 再重跑，或改用同发布包 `deploy/linux/install.sh --node … --reinstall`。
4. 确认节点页变为“已核销”并完成重下发（失败查 `redeploy_code` 后手工发布）。旧证指纹永久作废（备份恢复不复活），旧机重连记 WARN `node certificate refused`。

> **面板在 Cloudflare / 反向代理后**：Agent gRPC 必须直连面板（灰云或 IPv4），`--http-port` 填面板自身 Web 端口（默认 22550），安装器仅验面板自身证书；在「系统设置 → 节点安装连接地址」统一配置，预检错误见[部署与升级](deployment.md#一键安装推荐)。

## 6. 额度与连接限制（软执行）

规则与接口见[项目说明书 §22.1](project-spec.md#221-自动额度版本) 与[设备限制](device-limit.md)。运维要点：

- **超额窗口与核心重启**：Agent 每 5 秒采样、每 15 秒心跳上报；面板在 **60 秒**窗口合并同节点用户变化并基于已发布状态自动下发（不带草稿，继承 `inbound_enabled_json`）。超限到切断最长有「15s＋60s＋发布耗时」窗口，且每次自动版本会短暂重启核心。
- **删除/续期与故障兜底**：删用户/设备自动发移除版本（未上传流量按墓碑入账）；修改活跃用户到期自动刷新（`reason=renewed`）；面板重启自动对账补发。连续 3 次失败放弃并触发 `user_access_failed` 告警（查 `journalctl -u proxy-panel | grep "automatic quota revision gave up"`）。草稿渲染失败时退回仅按已发布状态移除不活跃用户；时间窗重渲染失败保旧配置重试。仅上报 `singbox_user_stats` 的 sing-box 计入用户额度。
- **并发设备限制**：运行时规则/断连门控，**不发布配置、不重启核心**；面板断联 3 分钟租约过期放行。NAT 少算、双栈/切网多算；WireGuard 不计数，Xray SOCKS5 UDP 与 sing-box SOCKS5/SS/Hysteria2/TUIC UDP 不支持设备限制（TCP 仍受限）。

## 7. 访问记录与流量

字段与保留策略见[项目说明书 §26.7](project-spec.md#267-访问记录和流量)。运维要点：

- **Xray / sing-box 访问日志**：Xray 位于 `/var/lib/proxy-agent/runtime/xray/access.log`，sing-box 从 debug 级别核心日志提取访问记录。Linux 文件 tail 每 ~100 ms 只交出完整行，半行留在文件直到补全，追平后清空；**20 MiB** 隐私截断会记录结构化丢弃事实。更新空窗允许文件积压，认领从完整行 checkpoint 接读，访问记录允许少量重复或缺失；备份无需保留。Windows 保留原读取行为。
- **本地 spool 与保留**：spool 上限 **100 MiB**，满时优先淘汰最旧允许记录、保阻断与流量，断线续传按 ACK 删除。默认保留允许 30 天、阻断 90 天、小时流量 90 天、日流量长期；每域名字节未知为 NULL（显示“—”），跨中转“配置出口”不代表确认实际出口。

## 8. 出口组与代理链

策略语义见[项目说明书](project-spec.md)。支持 `primary`（Agent 选首健康成员）、`round_robin`（Xray `roundRobin`+observatory / sing-box `urltest`）、`latency`（延迟优先）；`weighted` 已移除（旧版按 `round_robin` 解释）。固定出口失败**不隐式 Direct**；代理链按「出口 → 中转 → 入口」发布，失败反向回滚。

## 9. 节点出站网络

在代理节点高级选项选择「出口 IP」并发布；清空为「跟随服务器设置」。全部非 Block 流量固定所选地址，域名只解析该地址族；无该族地址、另一族 IP 字面量及回环目标连接失败。`inbound_egress_unsupported` 请升级入站使用的 Agent 核心，GOST 不支持。`network_unavailable` 请确认该地址仍配置在本机网卡上；失败保留旧配置。与服务器强制出站共用时需 helper 支持额外豁免。

**出口 IP 缺失**：服务器徽标或仪表盘显示「出口 IP 缺失」表示 Agent 心跳发现所列地址/网卡已不在本机，使用它的流量正在失败（强制出站缺失时整机该地址族新建连接失败；helper 未声明 `panel_exempt` 的旧节点上，强制出站 IPv4 缺失还会切断 Agent 与面板的连接，此时 SSH 执行 `nft delete table inet proxy_agent_source` 后再改设置并发布）。处理：把地址加回网卡（流量随即恢复），或改选/清空后重新发布。地址恢复但未重新发布时，`host-network/status.json` 可能仍为上次失败结果，下一次发布会刷新。「出口 IP 检查失败」查 Agent 日志 `outbound source check failed`。

出口组按每个出口 IP 分别探测、分别选路，保留原有策略及失败处理；默认出口探测失败不会删掉其他出口 IP 的可用成员。尚无探测结果时沿用组的失败策略，探测入口保留并持续检测。面板显示的出站健康仍来自默认来源，不能用它代替指定出口 IP 的连通结果。

> 注：旧示例配置（`deploy/examples/agent.yaml`、`panel.yaml`）中提及的签名发布下载与公钥配置仅适用于旧版工作进程自更新；现行 Agent 更新与排障见第 10 节与[部署与升级](deployment.md)。

「节点详情 → 节点设置 → 出站网络」含两条独立能力，均在**下一次发布后生效**（留空清除）：

| | A 核心出站源 IP / 网卡 | B 服务器强制出站 IPv4 / IPv6 |
| --- | --- | --- |
| 影响 / 机制 | 代理核心拨号套接字（Xray `sendThrough`/`sockopt.interface`；sing-box `inet4/6_bind_address`/`bind_interface`） | 经该网卡新建的全部出站连接（nft 表 `inet proxy_agent_source` postrouting SNAT，优先级 90） |
| 权限 / 平台 | 无特权 / 任意平台 | 仅独立 helper 持 `CAP_NET_ADMIN` / Linux ≥ 5.2 + nftables |

- A 自动豁免于 B；发往面板控制地址的连接也自动豁免（需本机 helper 回执含 `panel_exempt`，旧 helper 经 `qui-update --component agent` 升级），强制地址失效时 Agent 不掉线，可在面板改设置后重新发布；NAT 在首包决定，不改写已建连接与入站回包；双栈主机须同时填 IPv4/IPv6。
- **逐出站源 IP（`sourceIp`/`sourceInterface`）**：Direct 出站可覆盖服务器级设置（缺能力返 `outbound_source_unsupported`）；启用 B 时自动加入 `extra_exempt_ipv4/6`（每族 ≤16），返 `host_source_exempt_unsupported` 须升级本机 helper（`qui-update --component agent` 或 v1.7.5 完整网页升级）并核对 `status.json` 的 `features` 含 `extra_exempt`。`network_unavailable` / `host_network_failed` 均回滚保旧配置。

```sh
nft list table inet proxy_agent_source
systemctl status proxy-agent-host-network.service proxy-agent-host-network.path
journalctl -u proxy-agent-host-network -n 100
cat /var/lib/proxy-agent/host-network/desired.json /var/lib/proxy-agent/host-network/status.json
# 旧版连续失败触发 unit-start-limit-hit 时手动恢复：
systemctl reset-failed proxy-agent-host-network.service proxy-agent-host-network.path
systemctl start proxy-agent-host-network.path proxy-agent-host-network.service
```

OpenRC 替换为 `rc-service proxy-agent-host-network status|restart` 与 `grep -F proxy-agent-host-network /var/log/messages`。`desired.json` 与 `status.json` 的 `id` 不一致表示 helper 未运行或未完成。

## 10. 核心与 Agent 更新

安装、多核心参数、RC 规则与终端升级/降级见[部署与升级 §5](deployment.md#5-升级) 与[多核心安装](deployment.md#多核心安装)。日常排障要点：

- **sing-box 统计提示**：切到带 `with_v2ray_api` 的 qui sing-box 后，下次发布启用本地逐用户统计并计入共享额度，升级前核对用户额度。
- **代理核心更新排障**：面板经 `GET /api/v1/core-releases` 读官方摘要并校验平台/URL；Agent 仅认官方域名（≤3 跳重定向），先验 SHA-256 再解包，健康失败自动回滚。查状态用 `GET /api/v1/core-updates`，卡住或损坏任务由重连关闭或 Owner 核对后 `POST /api/v1/core-updates/{id}/clear`。
- **升级前基础配置**：在线且修订为 0 的节点触发升级前会经 `POST /api/v1/nodes/{id}/baseline` 下发仅含节点设置的基线配置（`auto: baseline before upgrade`，页面等最多 3 分钟；超时先核对实际修订再重试）。
- **Agent 完整系统升级（v1.7.5）排障**：
  - 网页由节点独立 root 服务固定执行 `/usr/local/bin/qui-update --component agent --version X.Y.Z --yes`（不受 Agent/浏览器/面板重启中断），按本机 root probe 回执开放按钮；缺桥接服务须先终端升级。
  - **路径与日志**：请求 `/var/lib/proxy-agent/system-update/request.json`；root 受理 `/var/lib/proxycontrol-install/agent-system-updates/<attempt-id>/`；只读状态/输出 `/var/lib/proxy-agent-update/<attempt-id>/`。日志分块 ACK，**不入 100 MiB 轮转且无自动截断**，确认终态留证后再清理。
  - **断电与 `unconfirmed`**：提交引导快照后断电，桥接核验 PID/启动时间/boot ID 后经只读 `--agent-install-receipt ID --agent-manifest FILE` 收敛终态；缺退出收据保持「结果待确认」，阻塞发布/核心更新/删除接替/备份恢复，绝不按耗时或版本号猜成功；明确失败且满足恢复条件才可重提同目标。`dispatchError` 仅表示本机投递失败。既有旧工作进程任务按原回执收尾，不再新建。
- **退出受管模式**：停服后将 `internal/agent/agenthost/state.go` 所列受管路径（含 `releases/agent-local-candidates/`）移走备份再启服；遗留 `agent-local.sock` 下次监督启动自动清理。

### Agent 核心交接排障

目标版本 1.10.0 的 Linux 更新可经授权保留并认领核心，普通 stop/restart、卸载与非更新崩溃仍停核。`qui-update` 输出“核心交接已认领并提交”表示该次认领和更新提交成功；“核心未确认 detached”表示旧 Agent 未确认脱离，更新进入失败回滚，须保留快照与 helper 查看原因。源或目标不支持协议、pidfd 不可用时按停核更新并提示；第一次从旧 Agent 升级仍会断线。既有验收与未实测项见[交接设计 §9](archive/core-handover-plan.md#9-实施与验收结果2026-11-22)。

Agent 日志中的 `core handover not authorized ...`（含 worker 相关同类 WARN）表示授权被拒，查看 `error` 中的具体原因：`OS environment is incomplete` / `OS environment differs` 对应 host、boot、PID namespace 环境缺失或不一致；`node differs`、`source Agent ... is not ...`、`target Agent ... is not ...`、`transaction ... is not ...`、`scope ... is not ...` 分别对应节点、源 PID/启动 token、目标镜像摘要、事务与作用域不匹配。`core_handover_expired` 表示 300 秒授权窗口失效，或更新进程已退出、PID 启动 token 不符。引导与 worker 两次切换均须有匹配授权，不能靠残留回执继续保留核心。

`core_traffic_tail_unavailable` 是本机结构化 WARN：核心在交接空窗内死亡，最后成功采样之后的尾部流量无法取得。只保留已确认的累计值，不估算、不补零，也不产生新的面板告警。

以下均为 root 本机固定命令，通过 `proxy-agent --config /etc/proxy-agent/config.yaml` 调用：

| 参数 | 用途 |
| --- | --- |
| `--core-handover-status --request <请求ID>` | 读取该请求的交接状态，如 armed、prepared、detached、adopting、adopted、failed、stopped、expired |
| `--core-handover-revoke-all --reason <原因>` | 撤销全部交接授权；回滚、卸载与人工停核前使用，本命令本身不停止核心 |
| `--finish-core-stop` | 停服后确认 worker 已退出，并按进程身份处理核心；有效授权和回执允许保留，其余路径确认停核。需要停核时先撤销授权；屏障失败保留现场，禁止继续恢复旧镜像或删除数据 |

### 面板网页更新

「系统设置 → 系统信息 → 面板更新」由无特权面板写请求、root 单元执行（见[说明书 §27.15](project-spec.md#2715-系统管理面板网页更新)）：

更新的备份、预检、快照与回滚流程见[部署与升级 §5「数据库备份与预检（面板）」「快照、健康检查与自动回滚」](deployment.md#5-升级)，网页入口与 root helper 步骤见[§5.4](deployment.md#54-面板网页更新)。磁盘本身很慢时，最终备份与完整性读取仍会耗时。

首次由旧更新器升级到本版本且检查进程被 `kill -9` 时，备份目录可能残留 `.proxy-panel-check-*`，确认无更新运行后可手工删除。

```sh
systemctl status proxy-panel-update.path proxy-panel-update.service
journalctl -u proxy-panel-update -n 100
tail -n 100 /var/lib/proxycontrol-install/panel-update.log
cat /var/lib/proxy-panel/panel-update/request.json /var/lib/proxy-panel/panel-update/status.json
```

`proxy-panel-update.path` 应为 `active (waiting)`（停掉时 `systemctl start` 即可）。helper 拒绝/失败均退出 0 并写 `status.json`，**非 0 退出表示未能写回执**，按 journal 查权限。

| `status.json` `code` | 含义与处置 |
| --- | --- |
| `running` / `succeeded` | 更新中（面板短暂重启） / 已换版并通过 10 秒健康检查 |
| `invalid_request` / `not_newer` | 请求非法（查 journal 重提） / 目标不新于当前版本（刷新核对） |
| `update_failed` / `version_mismatch` | `qui-update` 失败或版本不符并已自动回滚，查 `panel-update.log` 与 `pre-update-*` 备份 |

`pending.stalled`（超 10 分钟无回执）可直接重提；`status.interrupted`（`running` 超 35 分钟，多为断电）待开机单元 `proxycontrol-update-recovery` 恢复并确认 `proxy-panel --version` 正常后重提。更新前库备份至 `/var/lib/proxy-panel/backups/pre-update-*.db`，失败新库留为 `failed-update-*.db`；降级/跨迁移回退用命令行 [`--version`/`--database`](deployment.md#52-面板降级与数据库回退)。

## 11. Hysteria2 端口跳跃

节点上报 `port_hop_supported`（已装 host-network helper）后可配置端口跳跃：间隔 5–300 秒（默认 30），最多 10000 端口，不得含入站自身端口或与同节点其他端口/跳跃段重叠。防火墙须放行整段 UDP（NAT 主机须限制在已映射端口内），helper 用 nftables `redirect` 转至单监听端口。订阅输出见[协议矩阵](protocol-support-matrix.md)。

```sh
nft list table inet proxy_agent_hop     # 无输出表示当前无跳跃入站
journalctl -u proxy-agent-host-network -n 100
cat /var/lib/proxy-agent/host-network/desired.json /var/lib/proxy-agent/host-network/status.json
```

删除最后跳跃入站并发布后 `proxy_agent_hop` 表自动移除；重启后按最后成功修订重放（必要时 `systemctl restart proxy-agent-host-network.path`）。

## 12. 登录与安全取舍

- **登录限流**：同 IP 或同用户名 15 分钟失败 **5 次**即拒（内存计数，重启清空）。Owner 亦可能被他人锁 15 分钟，必要时重启 `proxy-panel.service` 解除。
- **代理头与固定安全项**：面板在受信反代后须配 `server.trusted_proxies` 才信转发头。会话 Cookie+CSRF、HTTPS、订阅 Token 哈希、操作审计（`GET /api/v1/operations`）与加密备份口令默认开启。

## 13. 常见故障

| 现象 | 检查 |
| --- | --- |
| 节点离线 | `systemctl status proxy-agent`、`logs/agent.log` / 服务日志、面板 gRPC（默认 18880）连通性、`/etc/proxy-agent/tls/ca.crt` 与 `server_name` |
| 节点连上即断 | 节点是否禁用、证书过期/吊销、Agent 版本低于面板要求 |
| `spool encryption key missing` 阻塞启动 | 从备份恢复 `/var/lib/proxy-agent/spool/spool.key`；无法恢复时移走整个 `spool/` 重建队列（未传记录丢失） |
| 发布回滚 / 出站源未生效 | 系统 → 配置版本看失败原因；出站网络需发布生效，核对 `nft list table inet proxy_agent_source` 与 helper（第 9 节） |
| 超限仍可上网 / 访问为空 / 额度未知 | 合并窗口（≤15s＋60s＋发布耗时）或离线；查 Agent 遥测与保留策略；sing-box 缺 `singbox_user_stats` 或协议无观测 |
| 核心反复重启报 `i/o timeout` | 宿主 `nf_conntrack` 表满，见下方处置 |
| 备份恢复失败 | 核对口令、先 `verify`、确认无其他进程占用库 |

### conntrack 表满（主机内核参数）

内核日志报 `nf_conntrack: table full, dropping packet` 且 Agent 报 `core unhealthy; restored last successful configuration`（带 `i/o timeout`）时在宿主检查并扩容（LXC 需在宿主机调整；conntrack 不在调优 helper 的 **19 个固定键**内，不随面板或 `qui-update` 变更）：

```sh
cat /proc/sys/net/netfilter/nf_conntrack_count /proc/sys/net/netfilter/nf_conntrack_max
dmesg | grep -c 'table full'
cat >/etc/sysctl.d/90-conntrack.conf <<'EOF'
net.netfilter.nf_conntrack_max = 131072
net.netfilter.nf_conntrack_buckets = 32768
EOF
printf 'options nf_conntrack hashsize=32768\n' >/etc/modprobe.d/nf_conntrack.conf
sysctl --system
```

### 读取耗时诊断（`QUI_READ_TIMING`）

排查只读接口慢响应时，临时为 `proxy-panel.service` 设环境变量 `QUI_READ_TIMING=true`（`strconv.ParseBool` 解析，默认关闭，非法值致启动失败），排查完清除并重启：仅向面板日志记录请求序号、路由模板、状态码与阶段耗时（`db_acquire`/`db_begin`/`sql`/`db_end`/`config_decrypt`/`config_decode`/`external_http`/`response_encode_write`），**不记 SQL、参数、Token、配置或密钥**。注意：`db_acquire` 含建连与排队，`sql` 计数为 `Query`/`Next`/`Scan`/`Close` 调用次数而非语句数，缺失阶段为未覆盖/未执行而非 0，总耗时差额不全归 CPU。

## 14. 终端菜单 qui

root 专用菜单 `/usr/local/bin/qui`（仅装 Agent 显示精简菜单：1. 更新、2. 彻底卸载、3. 启动及引导/实际 Agent 版本；含面板显示完整 8 项，不提供 shell 或日志入口）。

Agent 菜单「3. 启动」：Agent 已运行时不操作；更新或旧状态重建持有 `update.lock` 时拒绝；存在未完成的旧状态重建（`agent-state-reset/request` 且阶段不是 `complete`）时在后台继续该重建，由重建脚本完成后启动新 Agent（封存失败则恢复旧 Agent），不在重建旁启动旧状态；其余情况启动已安装的网络/调优监听与 `proxy-agent`。

| 菜单项 | 行为 |
| --- | --- |
| 1. 修改管理员账号密码 | 短停面板，经 stdin 运行 `--set-owner-credentials`（口令不进 argv/回显），恢复服务；**所有 Owner 会话立即失效** |
| 2. 修改端口 | 仅改 `http_address`（不动 `grpc_address`），`--check-config` 通过后备份 `config.yaml.bak`（0600）原子替换 |
| 3 / 4 / 5. 启停重启 | `systemctl restart\|start\|stop proxy-panel.service` |
| 6. 更新（从 GitHub） | 运行 `/usr/local/bin/qui-update`，校验 `SHA256SUMS`、备份库/快照并自动回滚（见[部署与升级 §5](deployment.md#5-升级)、[§5.2](deployment.md#52-面板降级与数据库回退)） |
| 7 / 8. 登录地址 / 彻底卸载 | 查看/重置管理员登录地址（需停面板，[§27.18](project-spec.md#2718-系统设置管理员登录地址与门户字段v154)） / 调用 `uninstall.sh --purge` |

**非交互改密**（面板已停，两行为新用户名与口令，首行空保持原用户名）：

```sh
systemctl stop proxy-panel.service
printf '%s\n%s\n' 'owner-name' 'new-password' | \
  runuser -u proxy-panel -- /usr/local/bin/proxy-panel --config /etc/proxy-panel/config.yaml --set-owner-credentials
systemctl start proxy-panel.service
```

### 彻底卸载（qui 菜单／uninstall.sh --purge）

以 root 走 `qui` 菜单或直接执行（终端输入 `y` 确认，v1.12.2 起不再要求输入 `purge`，无 `--yes`；**勿先普通卸载**，`all` 仅按现存 `.manifest` 清理，不补清 `.removed`；普通卸载保留数据/备份/账号与 `.removed`，重装需 `--reinstall`）：

```sh
/usr/local/lib/proxycontrol/uninstall.sh all --purge   # 单组件换为 panel 或 agent
```

- **清理范围**：面板清程序/单元/helper、`/etc/proxy-panel`、`/var/lib/proxy-panel`（含库/备份/CA）与账号；Agent 先恢复调优基线，再清程序/单元/helper/核心、`/etc/proxy-agent`、`/var/lib/proxy-agent`、两张 nft 表（`proxy_agent_source`/`proxy_agent_hop`）与账号；最后组件一并清 `qui`/`qui-update`/`/usr/local/lib/proxycontrol`/恢复单元与 `/var/lib/proxycontrol-install`；按当前 HTTP/gRPC 端口移除运行中 ufw/firewalld 对应 TCP 规则（同端口人工规则可能一并删除）。
- 清理前先导出面板备份；节点本机清理不在面板吊销身份，仍须在面板删节点；前置校验/调优恢复失败中止数据清理，数据阶段账号有残留进程报失败退出 1。

## 15. 删除节点

删除为**逻辑删除**（保历史流量/访问，立即吊销证书并退出分组/订阅，不可再启用；空白未登记节点直接“取消登记”移除）：

1. 查「删除」预览（`GET /api/v1/nodes/{id}/deletion-preview`），先解除 `blockers`（手工出站/出口组/代理链/策略/未收尾 `deployment_batch`，否则返 409 `node_delete_blocked`）。
2. 如需保离线节点未传数据，先复连补传 `/var/lib/proxy-agent/spool`；在节点停服或卸载 Agent（主机失联可直接删）。
3. 确认删除后按第 21.1 节核对关联节点「待清理」标签。重新接入同机须按第 5 节建新节点并更新引用。

## 16. 账号、订阅与客户端策略

模型与接口见[项目说明书 §27.10](project-spec.md#2710-用户订阅)、[§27.12](project-spec.md#2712-门户订阅观测与画布接口)。运维要点：

- **门户与订阅**：门户入口为 `/`。代理凭据、订阅 Token 与登录密码相互独立；关登录/改密/撤销会话/重置订阅链接立即生效（同步撤销 provider `/sub/{token}/rules/{id}`），重置代理凭据经自动版本生效；备份恢复结束所有门户会话。
- **格式与模板**：显式格式参数优先；开启 UA 识别按内置 UA 匹配表判定，未知回退默认格式。全局模板冲突返 409 需重读合并。
- **外部导入与额度**：HTTPS 来源抓取强制逐跳公网校验与 2 MiB/15 秒上限，刷新不在 `/sub` 请求内；导入加密存储，不入 Agent/修订/额度/设备限制。启用 `singbox_user_stats` 后流量计入共享额度，升级前核对。
- **16.1 单独导入改名/删除**：`content` 节点支持逐个改名（乐观锁防覆盖）与删除（画布引用须先改并发布，策略组删空被拒）；`url` 节点随源刷新不支持单删/改名，刷新同名保留先出现端点并告警。
- **16.2 维护与离线过滤**：订阅与预览按**整条已发布线路**评估（含出口组全部已发布候选与备用路径，外部导入无心跳但中转仍查全部受管跳）；维护开关保存即生效，离线统一按最后有效心跳与 `node_offline.duration_minutes` 判定（0 分钟须已断线或心跳过期）。过滤不动运行配置、不断现有连接，客户端刷新订阅生效；依赖索引重建缺加密发布材料时明确报错而不返回静默空订阅。

## 17. 元数据、探测、转发与重置周期

字段与接口见[项目说明书 §27.12](project-spec.md#2712-门户订阅观测与画布接口)。运维要点：

- **端口与入站删除**：`portRangeStart`/`portRangeEnd` 须成对；隧道/规则无范围且未手填返 `no_free_port`。隧道托管入站返 `managedBy`，普通编辑/删除返 409 `managed_by_tunnel`；普通入站删除先查 `deletion-preview`，外部引用返 409 `inbound_delete_blocked`。
- **TCP 探测与延迟**：非公网目标须 Owner 允许；定时探测固定每分钟 1 次（系统默认与单节点自定义均最多 5 个，节点须 45 秒内有心跳），采样留 7 天且**不进便携备份**（仅存目标配置）。列表“连通性”从面板测 TCP/可信 TLS，通过仅证明端口连接或 TLS 握手，不证明代理认证与转发。Hysteria2、TUIC、WireGuard 和纯 UDP 转发返回「未检测，暂不支持 UDP/QUIC 协议探测」，不发起 TCP 探测，不据此判成功或失败；实际可用性须由客户端验证。
- **转发与重置周期**：端口转发无用户身份，字节仅进节点/核心总量（relay 排除）。重置周期支持不重置、每月（短月夹紧、次月回锚点）或每 30 个面板时区日历日；备份恢复保守留当前边界，防旧备份重复清零。

## 18. 主机观测、续证与调优 helper

### Ubuntu 当前内核 BBR / fq 模块补装与中断恢复

仅支持 amd64/arm64 Ubuntu systemd 宿主（Python 3.9+、APT/dpkg、Canonical keyring、`/proc/version_signature` 与镜像包可核验），从临时 APT 配置拉取精确匹配的 `linux-modules(-extra)-<release>` 验签持锁安装（包阶段等 15 分钟、参数 30 秒）。按请求 ID 查 `journalctl -u proxy-agent-tuning-modules.service` 与 `/var/lib/qui-tuning-modules/status.json`，中断先查 `dpkg --audit`。

**包服务中断后回执一直为 `finished=false`**：同次开机内新请求会等待该回执。暂停提交调优请求，由宿主管理员收尾：

1. 运行 `systemctl show proxy-agent-tuning-modules.service -p ActiveState -p Job`，仅当 `ActiveState` 为 `inactive`/`failed` 且 `Job` 为空/0 时继续；运行 `dpkg --audit`，先修完未完成配置/触发器并确认包状态正常。
2. 核对 `/var/lib/proxy-agent/tuning/module-request.json` 与 `/var/lib/qui-tuning-modules/status.json`：`id`、`boot`、`release`、`targets`（含顺序）完全一致，`boot` 等于 `cat /proc/sys/kernel/random/boot_id`，`release` 等于 `uname -r`，且仍 `finished=false`；任一不符立即停止并查日志。
3. 条件全满足时执行 `systemctl start proxy-agent-tuning-modules.service`（固定 helper 写入中断终态并跳过安装，命令报错属预期）；复查 root 回执确认为 `finished=true` 后再由 Owner 决定是否重提。

### 主机指标、自动续证与 sysctl 调优排障

指标、推荐引擎与看板见[项目说明书 §22.0](project-spec.md#220-服务器调优与推荐应用流程)。运维要点：

- **自动续证**：原私钥，满 30 天或余 ≤14 天在控制连接续期（失败退避 1 小时，离线不跑，90 天期不变）。
- **19 个固定 sysctl 键**：仅认 `pkg/contracts/tuning` 白名单（`tcp_rmem`/`tcp_wmem` 仅填最大整数）。请求写 `/var/lib/proxy-agent/tuning/desired.json`（0600），回执写 `status.json`；断流或 30 秒超时转 `unconfirmed`，**不自动重发**，核对心跳 `current`、日志与 `status.json` 后由 Owner 决定。启动/调优后以 `--probe-tuning` 实测写权限并写入带 boot ID 与 helper SHA-256 的 `probe.json`，未就绪时面板禁设新参数。
- **持久化与快照**：配置落 `/etc/sysctl.d/99-proxy-tuning.conf`，首次原值存 root `0600` `.proxy-tuning-snapshot.json`（**严禁手删快照**）；清空/卸载按快照恢复，恢复失败中止卸载。便携备份排 `node_metric_samples`，恢复保当前库调优行并将 `pending` 置 `unconfirmed`，不改宿主 sysctl。
- **排查**：systemd 看 `systemctl status proxy-agent-tuning-probe.service proxy-agent-tuning.path proxy-agent-tuning.service` 与 `journalctl -u proxy-agent-tuning`；OpenRC 看 `rc-service proxy-agent-tuning status` 与 `/var/log/messages`；核对 `desired.json`/`status.json` 的 `request_id` 与逐键错（helper 不调 `modprobe`/`sysctl`）。隔离验证：`sudo bash deploy/linux/tests/tuning-helper-test.sh`。

### 整机月流量

按所选单张网卡统计整机用量（位于「服务器详情 → 设置 → 整机流量」，`sum`/`outbound`/`max`、自动或指定网卡、展示额度、1–31 重置日；无需发布、不入用户额度/订阅；三张表随备份保存，恢复以当前库为准）。

口径与限制（全文；界面提示只显示摘要）：

> 整机流量按所选单张网卡统计，包含所有服务；不等于代理流量或供应商账单。
> LXC 只能看到容器自己的网卡，不能统计物理宿主机。首次采样仅建立基线；
> Agent 离线期间若主机重启，跨重启的流量无法补计。同次启动的离线流量可由
> 累计计数补计，但跨日分配按时间比例估算。网卡计数重置会重新建立基线。
> 自动网卡来自 IPv4 默认路由；策略路由或 IPv6-only 请手动选择。
> 修改网卡会重新计算所选网卡的本期数据；修改面板时区不会重新分桶历史数据。

无本期日行或无法解析网卡时显示“—”而非 0；心跳过期但有日行时详情标注“截至上次采样”。

## 19. 多核心运维

契约见[项目说明书 §22.2](project-spec.md#222-核心更新与移除)。入站 `core` 留空跟随主核心，同节点端口跨所有核心与转发全局唯一。添加核心（`POST /api/v1/core-updates`）须待能力上报后才解除 `core_selectionBlocked`。移除核心前须删尽该核入站（含禁用）并发布确认，再 `POST /api/v1/nodes/{id}/cores/{component}/remove`（拒码 `core_in_use`/`core_pending`/`last_core`/`revision_mismatch`）；安装器二进制只禁用不删，**报 `ErrCoreStopUnconfirmed` 表示进程未退干净，勿手工 `rm` `runtime/<组件>/`**，移除后回滚到依赖该核的旧修订被拒。`restart-core`/`health-checks` 指定 `core` 仅作用于单核，回滚始终整节点；各核转发字节计入节点总量。

## 20. GOST 运维

- **定位与资源**：固定官方 3.3.2（仅 `linux/amd64`），仅做 TCP/UDP 转发，**无用户/订阅/额度/设备门控/域名记录**，**永远不能作主核心**。二进制 `/usr/local/lib/proxy-agent/gost`，配置 `<DataDirectory>/runtime/gost/config.yaml`，无独立单元；日志固定 `warn/text`（stderr 4 KiB），空闲约 45 MB，指标走私有 `runtime/gost/metrics.sock`。
- **健康与重启**：健康仅查进程与本机监听（指标错或目标不通不判不健康，测目标用 `tcp-probes`）。发布会重启同节点**全部核心**；GOST 崩溃独立退避拉起。
- **目标池/端口范围/限流**（节点转发入站）：`targets` ≤32（`round`/`fifo`，`fifo` 切换与超限均为先接后关）；`port_end` ≤256 端口（单节点 ≤1024 生产服务），限速与 TCP `max_connections`（UDP-only 禁用）均按展开后单服务计，健康检查短暂占 1 连接名额。转发字节仅进节点总量（见[项目说明书](project-spec.md)）。

## 21. 隧道管理与中转方案（v1.4）

配置与接口见[项目说明书 §27.13](project-spec.md#2713-隧道管理与节点中转)。各跳须 `linux/amd64` 且可跑 GOST（两跳契约 ≥3、三跳 ≥4），接收跳设公网地址/`connectHost` 并放通前跳 TCP 中继端口，入口按 network 放通；发布页可逐台补装缺失 GOST 3.3.2（单台等 5 分钟，直接调 API 返 409 `gost_install_required`），闲置 GOST 见 `gostRemovable`。先预览再按 `exit → mid → entry` 发布（失败反滚）；目标变动标 `needs_publish`，删除已发布对象先标 `pending_delete`；发布持久收尾，超 300 秒返 202（`status=publishing`），重启自动幂等完成。轮换凭据（`POST /api/v1/tunnels/{id}/rotate-credentials`）须重发且会短暂中断会话；删除不保证立即撤销离线节点旧凭据。目标仅见中继源 IP（设备限制合为一来源），账户流量在目标计量，UDP 经 TLS/TCP 封装可能受队头阻塞。

### 21.1 中转方案撤除与待清理

撤除用 `POST /api/v1/canvas-schemes/{id}/remove`（带 `{updatedAt}`），外部引用或未收尾 `deployment_batch` 返 409，确认后立即删方案并下发 `auto: relay cleanup` 修订（只移除已删对象）。离线重连自动清，失败按 1/5/30 分钟退避重试（之后每 30 分钟，面板重启继续）。排查看「待清理」标签、`relayCleanupPending`/`relayCleanupCode` 与面板日志 `relay cleanup attempt failed`，勿以页面消失推定远端已停。

## 22. 通知与告警

v1.10.2 修复监控整合长期占用数据库写连接导致心跳延迟、节点误报离线的问题：整合改为每桶提交和短时间预算。遇到“代理仍可用、节点显示离线”时，应比较最后心跳时间与面板的整合/数据库超时日志，勿仅凭服务 `active` 判断 Agent 正常。德国 v1.10.1 升级另发现重置与 helper 的循环等待；v1.10.2 调整了服务依赖、锁和停服重建顺序。发布本身不改变生产，已有故障须在单独授权升级后实机核对。

`node_offline` 通知与节点显示、订阅过滤共用确认离线规则。待登记、禁用和维护节点不发离线通知；维护期间保留已有离线事件状态。订阅排除经过任一禁用或维护受管节点的线路。关闭通知规则只停止通知，界面与订阅仍读取规则中的离线时限。

面板启动后，通知 worker 先按规则的离线时限重新观察（至少 45 秒）。这段时间不发送新的离线告警，避免升级停机后在 Agent 重连前集中误报；已存在的离线事件保留并正常发送恢复通知，观察期后仍未恢复的节点正常告警。此观察期只控制新离线通知，不改变界面、订阅、指标或操作准入判断。

每 60 秒评估一次：`node_offline` 每轮离线/恢复各一次（管理后台与订阅过滤共用持续时间，0 分钟须已断线/过期）；`cpu`/`memory`/`disk`、`public_ip`、`machine_traffic` 与 `system_alert` 沿用默认 30 分钟冷却，恢复不受限。渠道支持 Telegram Bot（2xx 须合法 JSON 且 `ok=true`）与 HTTPS Webhook（重定向亦须 HTTPS）；WARN 仅记渠道 ID 与错误码，不记 Token/正文：

| 错误码 | 含义与排查 |
| --- | --- |
| `channel_timeout` / `channel_network` | 超时（默认 10s）或 DNS/TCP 失败，查面板出站连通、DNS 与防火墙 |
| `channel_http` / `channel_rejected` | 上游 4xx/5xx 查接收端日志；被拒（Telegram `ok=false` 或非 HTTPS）核对 Token/Chat ID/URL |

## 流量存储自动整理

- **核心明细**：原始样本保留至少 24 小时后按 UTC 分钟合并；核心分钟保留至少 7 天，完整过期小时分批合并为小时汇总（不删累计位置，迟到下轮合并，查询切入已合并桶返回 `traffic_history_resolution`）。小时展示索引累加分钟行的原始样本数，保持整理前后一致。迁移 0164 只把含分钟汇总的已就绪小时索引标为待回填，之后每轮最多 16 桶纠正已有计数；准备期间查询使用精确源数据，不修改账本。
- **编号回执与冲突**：面板入库且节点确认本机持久删除后再留 ≥24 小时，每批 ≤500 条淘汰（隔离缺口延长保留）。升级先面板后 Agent，已切节点拒无编号上报。遇 `traffic_delivery_retired`/`traffic_delivery_conflict` **严禁删队列或换编号绕过**，须保原 `queue.db`/`spool.key`/独立 witness 恢复整组状态；真重装走恢复码登记在同事务封旧授新。
- **旧无编号回执**：接收超过 7 天、记账完成且无同批待处理输入才分批淘汰（每批最多 500 行）；7 天窗口供 Agent 丢失确认后的同 batch_id 重发去重，窗口外重发可能重复计入，Owner 接受该边界。未完成记账和仍有 pending 的回执保留。
- **观察与门禁**：「访问存储增长观察」看进度、缺口和待回收空闲页。新空库默认 incremental；回收独立于整理，每分钟检查，超过 32 页时按每段最多 256 页、每轮最多 2048 页、2 秒预算处理，未降至阈值则 1 秒续跑。整理失败仍按 1 分钟重试。已有 NONE/FULL 模式不自动转换，不执行全库 `VACUUM`；转换由 Owner 安排离线维护。删除先释放复用页，主库文件与 WAL 的实际体积分别观察。启用编号队列须基线、当前与上一 Agent 回滚镜像全支持 spool ABI 7，否则日志提示未启用，严禁手改 floor 或删回滚镜像强启。

## 历史未结记录与恢复材料保护（2026-11-20 文档核对）

以下仅索引仓库最后记录，当前是否仍存在未重新连接生产核实。核实前不把历史未结标成已完成，也不直接重做原操作。

| 记录 | 已有事实与缺失证据 |
| --- | --- |
| 美国 CC → 德国 TCP 22550 临时放行 | 2026-11-15 14:38:30 UTC 新增来源 `148.135.40.213/32` 单条临时 ACCEPT，未持久化，重载／重启可能已消失；CC 后改走域名 443，不再需要。最后记录因 SSH 被对端关闭未撤，仓库没有后续撤除回执。 |
| UK → Alpine NAT 隧道 | 2026-11-07 16:18 UTC Owner 删除后当时为 `pending_delete`，Alpine 仍监听 21411，需成功发布移除才结束；未见同 ID 完成回执。DDNS 当时指向不同 SSH 身份，未发送凭据；不能据旧地址访问或猜测现状。 |
| 明文 VLESS :35000 回程异常 | 2026-11-16 只读发现的新方案场景，当次未改配置，未见后续修订或关闭回执；前一天已撤的两个测试方案不作为关闭证据。 |

- **生产机 `/tmp` 1 GiB 在线扩容未重启验证**：v1.7.0 升级期间已将生产机 `/tmp` 从 482 MiB 在线扩至 1 GiB 并写入持久挂载配置，但尚未执行重启验证。

CC 旧修订 61／87 积压断连与两项旧测试方案删除阻塞已由后续记录解决；测试机模块清理与 Telegram 手机收件未核对属历史验证末态，不推定为生产任务。

恢复任务保护：既有 worker `queued`/`running`/`failed_closed`、`queued recover` 及原命令/尝试/迟到回执继续保留并按真实结果收尾；完整系统更新 `queued`/`running`/`unconfirmed` 或恢复未成功的失败持续占用节点。root 收据、原 RequestID/签名材料、未 ACK 日志与本地队列不因文档清理删除。隧道发布持久补偿、`relay_cleanup_nodes` 与流量 pending/游标/检查点继续按各模块规则恢复；无回执、超时、版本相同均不能代替完成证明。
