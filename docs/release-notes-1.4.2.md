# v1.4.2 发布说明

发布日期：2026-11-08（UTC）（tag `v1.4.2` → `a588c17`，即 `release v1.4.2: saved node install endpoint, installer preflight checks`；经 GitHub 发布工作流的 tag 推送发布）。v1.4.2 是 v1.4.1 之上的补丁版本：**没有迁移**（迁移头仍为 **0134**）；内容为「系统设置 → 节点安装连接地址」、三个安装命令对话框共用该默认值，以及 `node-install.sh` 的失败诊断与登记前 gRPC 预检。门禁、GitHub `check` 与发布结果见“验证”与“发布记录”。

**平台：** 与 v1.4.1 相同，构建 linux/amd64 与 linux/arm64；GOST 仍只支持 linux/amd64。本版未新增 arm64 实机验收。

## 新增与改进

- **节点安装连接地址（系统设置）：** 一键安装命令里的面板地址与面板 Web（HTTPS）端口只需在「系统管理 → 系统设置 → 基础设置 → 节点安装连接地址」保存一次，不再每台新服务器手改。值存于既有 `system_settings` 表的单一键 `install_endpoint`，**无迁移**；键缺失即未设置。`GET/PUT /api/v1/system/install-endpoint`（Owner＋CSRF）带 `expectedRevision` 冲突检查（409 `settings_conflict`），内容未变化不推进版本，写成功记操作日志“修改节点安装连接地址”。
- **三个对话框共用默认值：** 新建服务器、原位身份恢复、版本页“添加内核”生成的安装命令都默认取该设置；未设置或读取失败时回落到当前浏览器访问面板的地址。对话框内改动只影响本次命令，不回写设置。端口标签统一为“面板 Web 端口”，并说明它不是 Agent gRPC 端口（gRPC 端口仍取注册响应里的实际监听端口）。
- **校验口径与安装器一致：** 主机须匹配 `^[A-Za-z0-9.-]+$` 的域名或 IPv4（无协议、端口、括号），因此不接受裸 IPv6；端口为 0（未设置）或 1–65535；主机与端口可独立留空。设置页在 `httpPort=0` 时显示空输入框并保留 22550 占位符，不再显示 `0`。
- **`node-install.sh` 加固：** `--http-port` 与 `--grpc-port` 相同立即拒绝；`GET /install/ca.pem` 失败按 HTTP 状态与 curl 退出码分类（415/426 → `--http-port` 指向了 gRPC 端口；404 → 开发模式面板或没转发该路径的别的 Web 服务；DNS/拒绝/超时 → 不可达并给出 host:port）；公网 CA 分支也按 `--ca-sha256` 核对取回的字节；装好信任锚后、登记之前用该信任锚与 `--panel` 预检 gRPC 端点（`--cacert`、无 `-k`、有 HTTP/2 时带 `--http2`，否则只测 TCP 可达），证书不匹配或不可达时在登记前中止并说明成因（面板在反向代理／Cloudflare 后面是最常见的一种）。新增 `deploy/linux/tests/node-install-test.sh`（curl/install 用 PATH 桩，42 项）并接入 `scripts/check.sh`。
- **文档：** 新增[发布流程手册](release-runbook.md)（提交、门禁、发布与升级生产的步骤，交接用）；[安装指南](install-guide.md)、[部署与升级](deployment.md)、[运维手册](operations.md)补充反向代理／Cloudflare 场景与该设置的用法。备份无需改动：加密归档是整库快照，新键随归档保存与校验，旧归档缺该键读作未设置。

## 修复

本版没有面向生产用户的缺陷修复。门禁首轮发现新增的两个 `/api/v1/system/install-endpoint` 路由没有登记到 `internal/platform/server/testdata/portal_route_coverage.txt`，导致“管理员路由拒绝门户会话／门户路由拒绝管理员会话”两个覆盖测试失败；已补登记两项路由，**没有进入任何已发布版本**。另有两个与本批改动无关的时序敏感测试在满载门禁中失败，隔离重跑均通过，见“验证”。

## 验证

- **本地检查（Windows 开发机）：** `go build ./...`、`go vet ./internal/panel/system/`、`go test ./internal/panel/system/`、`npm --prefix web run typecheck`、`npm --prefix web run build`、`node web/scripts/ui-regression-selftest.mjs`、`node-install-test.sh`（42/42）与 `bash -n` 均通过。
- **Linux 门禁首轮（快照 `c21e273`）FAIL，合计 3175 秒：** `check` 部分 2494 秒退出 1，失败为新增路由未登记（见“修复”）及 `TestKilledBackupProcessRecoversJournalWithoutTouchingUnrelatedBackup`（子进程 12 秒检查点超时）、`TestRealXrayContinuesOfflineAndReplaysTrafficAfterCentralRestore`（启动记录时序）；`cores` 部分 680 秒退出 1，仅重复上述路由覆盖失败，真实核套件本身通过。
- **无关失败隔离复验：** 上述两个时序敏感测试在 Linux 测试机上各自 `-race -count=2` 隔离重跑均通过（备份 87 秒、恢复 157 秒），确认是满载并行下的偶发，与本批改动无关。
- **Linux 门禁（快照 `30ae663`）PASS：** `check` 2135 秒、真实核套件 666 秒、合计 2801 秒，0 个失败、无 DATA RACE 告警；提交 `a588c17` 的树与该快照树一致（`git diff` 为空）。
- **GitHub `check`：** 运行 42452253767，SHA `a588c17`（`cores=false`），2026-11-08 05:49–06:27 UTC **success**。

## 已知限制

- **面板在反向代理／Cloudflare 后面：** Agent 的 gRPC 必须直连面板，mTLS 无法经过 Cloudflare 或终止 TLS 的反向代理。`--panel` 要用 DNS-only（灰云）主机名或面板的 IPv4，并被面板自己的证书覆盖；`--http-port` 是面板自己的 Web 端口，不是代理的 443。`node-install.sh` 现在会在登记前预检并说明原因，但网络与证书仍需按[部署与升级](deployment.md)第 8 节配置。
- **私有 CA 证书导致客户端拉不动订阅：** 与 v1.4.1 相同，严格校验 TLS 的客户端经 HTTPS 拉取生产订阅会失败，临时办法与后续计划见 [v1.4.1 发布说明](release-notes-1.4.1.md)的“已知限制”。
- **Trojan 端点的自签证书在订阅中没有信任材料：** 自 v1.0 验收起即为已知限制，未在本版改变。
- **安装连接地址只影响之后生成的命令：** 修改不改变已有节点、`config.yaml` 或正在运行的 Agent。

## 升级

- **先面板，后 Agent：** 面板主机执行 `qui-update --component panel --version 1.4.2`，确认面板就绪后，再在每台节点主机执行 `qui-update --component agent --version 1.4.2`；组合主机同样先 panel 后 agent。
- **没有迁移：** 迁移头仍为 0134，升级不做数据库变更；`qui-update` 仍在替换前备份面板数据库，健康检查失败或中断时自动回滚。
- **已有安装保持原端口与证书：** 配置不会被改写；新装用 `panel-install.sh`（默认 22550／18880）。
- **升级 Agent 会短暂重启该节点的代理核心**（核心是 Agent 的子进程），既有连接瞬断；修订不变、没有回滚。
- 离线升级、降级与旧更新器的首次升级见[部署与升级](deployment.md)第 5 节与 5.1。

## 发布记录

| 项目 | 结果 / 证据 |
| --- | --- |
| 门禁（快照 `30ae663`）：`scripts/check.sh` + 真实核套件 | **PASS**（2026-11-08 05:01–05:48 UTC，`check` 2135 秒、真实核 666 秒、合计 2801 秒，0 个失败、无 DATA RACE）；发布提交 `a588c17` 与该快照树一致 |
| GitHub `check`（发布提交与 run 编号） | **PASS**：`a588c17` 的 run 42452253767，2026-11-08 05:49–06:27 UTC（`cores=false`） |
| 发布日期、tag 与迁移头复核 | **2026-11-08 06:28 UTC 推送注解 tag `v1.4.2` → `a588c17`**；迁移头仍为 **0134**，本版无迁移 |
| GitHub 发布产物 | run 42454853305 发布 `proxy-control-1.4.2-linux-amd64.tar.gz`、`proxy-control-1.4.2-linux-arm64.tar.gz`、`sing-box-1.14.4-qui.1-linux-{amd64,arm64}.tar.gz` 及各自 `.provenance.txt`、`SHA256SUMS`、`node-install.sh`、`panel-install.sh`；Release 于 06:36:44 UTC 正式发布 |

## 生产升级

**已完成（2026-11-08，Owner 单独同意后执行）。** 先面板 us2 `1.4.1` → `1.4.2`（07:04:15 UTC 起服务），随后按 uk、Alpine、us2 逐台把 Agent 从 `1.4.1` 升到 `1.4.2`（分别在 07:04:57、07:05:31、07:05:59 UTC 启动）；四次 `qui-update` 均输出“已更新到 1.4.2，服务运行正常”。

- **恢复点：** 加密面板备份 `KT7hNrOzw3rD1eWjs5sq0B48pyShxgFl21UxY04MhB8`（07:03:46Z 创建，07:03:53Z 校验通过）与更新器备份 `/var/lib/proxy-panel/backups/pre-update-1.4.1-20261108-150413.db`；四台主机的 `rollback.*` 快照在成功后已自动清理。
- **核对：** 面板与三台 Agent 均为 1.4.2、服务 active/started；三台节点 online，修订仍为 295/296/291，核心健康（xray、gost）；面板 `NRestarts=0`，升级窗口内 0 WARN／0 ERROR；`/etc/proxy-panel/config.yaml` 的 SHA-256 未变（`e57bc1d9…`）；无 `rollback.*` 残留。Alpine 在面板停服瞬间记录一条预期的 `control channel disconnected; retaining local state` WARN，随后自动重连。
- **真实代理抽样：** 升级后经生产入口请求，SOCKS5、HTTP、VLESS、Shadowsocks、Trojan 五个路径均返回 200。首次抽样用的旧临时客户端配置没有 Trojan 出站，误报 `000`；改用订阅中的当前凭据重测通过，与升级无关。
- **未覆盖：** 跨迁移降级（本版无迁移）、原生 arm64 与浏览器交互回归本轮未重复验收。
