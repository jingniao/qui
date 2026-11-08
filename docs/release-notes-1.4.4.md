# v1.4.4 发布说明

发布日期：2026-11-08（UTC）（tag `v1.4.4` → `67c9fb0`，即 `fix(update): finish the panel rollback when the update path unit cannot be disabled`；tag 指向的发布提交为 `release v1.4.4: panel web update, sidebar collapse fix, scoped quick checks` 之上的修复提交，经 GitHub 发布工作流的 tag 推送发布）。v1.4.4 是 v1.4.3 之上的补丁版本：**没有迁移**（迁移头仍为 **0134**）；内容为**面板可在网页里更新自己**、节点安装器在 CDN 后固定面板证书、侧栏展开分栏的收起行为、发布流程的按改动范围检查，以及发布前发现并修好的**面板回滚回归**。门禁、GitHub `check` 与发布结果见“验证”与“发布记录”。

**平台：** 与 v1.4.3 相同，构建 linux/amd64 与 linux/arm64；GOST 仍只支持 linux/amd64。本版未新增 arm64 实机验收。

## 新增与改进

- **面板网页更新：** 「系统管理 → 系统设置 → 系统信息」顶部新增 Owner 专用的「面板更新」卡片，显示当前版本与 GitHub 最新正式发布（`api.github.com/repos/jingniao/qui/releases/latest`，忽略草稿与预发布，缓存 10 分钟，「检查更新」强制刷新），只有存在更新的正式版时才出现「更新到 vX.Y.Z」。只能安装最新正式版，没有版本选择、降级与 RC；开发/候选版面板（`dev_build`）与没有 helper 目录的面板（`helper_missing`）不支持，卡片里各有一行说明。
- **接口：** `GET /api/v1/system/panel-update`（Owner，`?refresh=1` 跳过缓存）与 `POST /api/v1/system/panel-update`（Owner ＋ CSRF，体 `{version}`，202 `{requestId,version}`；400 `invalid_request`、409 `panel_update_unsupported`／`panel_update_busy`／`panel_update_stale`、502 `panel_update_check_failed`），写成功记操作日志「更新面板」（target `panel`）。`pending.stalled`（请求超过 10 分钟未被应答）与 `status.interrupted`（`running` 超过 35 分钟，例如更新中主机重启）都允许重新提交。
- **机制：** 面板无特权且 `ProtectSystem=strict`，只原子写 `/var/lib/proxy-panel/panel-update/request.json`；`proxy-panel-update.path`（PathChanged）触发 root oneshot `proxy-panel-update.service`（`TimeoutStartSec=1800`，故意不沙箱、不对面板设 `PartOf`/`BindsTo`）运行 `/usr/local/lib/proxycontrol/panel-update-request`，校验请求（普通文件、属主 `proxy-panel`、≤4096 字节、32 位十六进制 requestId、严格更新的 `X.Y.Z`）后执行 `qui-update --component panel --version X.Y.Z --yes`（数据库备份、10 秒健康检查、失败回滚），把固定码写回 `status.json`（`running`／`succeeded`／`invalid_request`／`not_newer`／`update_failed`／`version_mismatch`，无自由文本），更新器输出只进 root 0600 的 `/var/lib/proxycontrol-install/panel-update.log`。更新期间面板重启，页面轮询重连。
- **安装与升级：** `install.sh` 随面板发布 helper 与两个单元（与 update-recovery、`qui` 一样不进安装记录）、创建 0700 的请求目录并只 enable/start path 单元；`update.sh` 快照这三份文件，`uninstall.sh` 停用并删除它们、保留目录与日志。仅 systemd；已有面板需先在 shell 跑一次 `qui-update` 才具备该能力；只更新面板，不更新节点 Agent。**无迁移**（不新增表，也不改备份/恢复合同）。
- **发布流程提速：** 新增 `scripts/quick-check.sh`（按改动文件裁剪检查：文本 CR/BOM、web 类型检查/构建/自检、deploy 脚本 `bash -n` 与相关测试及可执行位告警、受影响 Go 包 vet/test、真实核包、迁移/CI 要求完整门禁）；完整门禁每个版本只在发布提交上跑一次并与 GitHub `check` 并行；偶发失败只重跑失败包一次（`~/.qui-local/gate-rerun.sh`）。规则见[发布流程手册](release-runbook.md)、[部署](deployment.md) §2 与[项目说明书](../AGENTS.md) §32。
- **文档：** [项目说明书](../AGENTS.md) §27.15、§29.1、§30.1、§32，[部署](deployment.md) §5.2，[运维](operations.md) §10。

## 修复

- **节点安装器在 CDN 或终止 TLS 的反向代理后固定面板证书**（`8c67a46`，随本版发布）：此前 `GET /install/ca.pem` 用 `--cacert` 会成功（边缘证书由公网 CA 签发），但取回的文件是面板自己的证书，而 gRPC 端口呈现的正是它；把系统 CA 包装成 `/etc/proxy-agent/tls/ca.crt` 会让预检以 curl 60 失败并在登记前中止。现在按取回的文件判断：用 openssl 对照系统 CA 包，只有证书本身受公网信任时才走系统 CA 包分支，否则按固定 SHA-256 核对后直接安装该面板证书；没有 openssl 的主机回落到固定证书。新增 4 项 shell 测试（共 51 项）。
- **侧栏：** 已经展开的分栏点击标题总是收起，不再因为当前页面不在该分栏而跳转到它的第一个页面；收起的分栏点击后仍展开并在需要时导航（`web/src/components/AppSidebar.vue`，前端自检新增对应检查）。
- **面板回滚不再因为停用更新单元失败而中断**（`67c9fb0`，发布前修复）：从网页发起的更新运行在 `proxy-panel-update.service` 内，回滚只停用 `proxy-panel-update.path`，且只在旧版本没有更新桥接时停用。原来这一步是严格的，systemd 一旦拒绝（安装失败留下的坏单元文件，或 systemd 不认识该单元）就在写回任何文件之前中止恢复，面板于是停在刚刚失败的那个版本上。现在只报告并继续恢复，文件与数据库照常回滚。该回归由 GitHub `check` 在发布提交 `aa549a1` 上抓到（`deploy/linux/tests/update-test.sh` 10 个回滚场景失败），本地 gate 因 go test 先失败未跑到脚本测试；修复后同一测试 28 通过 / 0 失败。

## 验证

- **发布提交 `aa549a1` 的门禁（未通过，已废弃该提交）：** 本地完整门禁 2026-11-08 15:57:01–16:42:40 UTC，合计 2739 秒，`GATE aa549a1 FAIL`：`check` 部分 2072 秒退出 1，唯一失败为 `internal/platform/server` 的 `TestRealSingboxThreeAgentChainBatchTCPUDPAndReverseReset`（第三个节点 `Status:failed`，链路批次 `rolled_back`）；`cores` 部分 667 秒退出 0。GitHub `check` 运行 42497414214，15:57:41–16:27:53 UTC **failure**，失败在 `deploy/linux/tests/update-test.sh`：场景 3／4／5／6／7／9／10／12b／19／24 共 10 个回滚场景，全部停在 `Failed to disable unit: Unit file proxy-panel-update.path does not exist.` 之后的“无法恢复更新前的文件”。
- **回滚回归的定位与修复：** 在 Linux 测试机上用该提交的树单独复现，`update-test` 18 通过 / 10 失败，与 GitHub 一致；根因与修复见“修复”第三条。修复后同一测试 **28 通过 / 0 失败**。
- **本地完整门禁（发布提交 `67c9fb0`）PASS：** 2026-11-08 16:47:54–17:34:13 UTC，`check` 2096 秒退出 0、真实核套件 683 秒退出 0，`GATE 67c9fb0 PASS total=2779s`，无 DATA RACE 告警。上一轮失败的 `TestRealSingboxThreeAgentChainBatchTCPUDPAndReverseReset` 在同一包内通过，确认为环境性偶发（该包在 v1.4.2 门禁也出现过同类时序失败）。
- **GitHub `check`（发布提交 `67c9fb0`）：** 运行 42501712348，2026-11-08 16:47:57–17:38:56 UTC **success**（51 分钟，`cores=false`）。
- **发布前本机检查：** `deploy/linux/tests/update-test.sh` 28/28；其余检查由上述两条门禁覆盖（前端构建与自检、`go vet`、`go test -race`、部署脚本测试、真实核套件）。

## 已知限制

- **第一次仍要手工升级一次：** 已在 v1.4.3 或更早的面板不支持网页更新，需先在服务器上跑一次 `qui-update --component panel`；本次生产升级即用 `qui-update` 完成，之后网页卡片才可用。
- **只能装最新正式版：** 没有版本选择、降级与 RC；开发/候选版面板与没有 helper 目录的面板只显示说明。
- **升级期间面板会短暂不可用：** 面板重启，页面断开十几到几十秒后自动恢复；更新失败时按 `qui-update` 既有逻辑自动回滚并保留日志。
- **只更新面板：** 节点 Agent 与代理核心仍按原方式升级。
- **私有 CA 证书导致客户端拉不动订阅：** 与 v1.4.1／v1.4.2／v1.4.3 相同，未在本版改变，见 [v1.4.1 发布说明](release-notes-1.4.1.md)的“已知限制”。
- **arm64：** 仍只构建、只做 qemu-user 模拟，没有原生 arm64 实机验收。

## 升级

- **先面板，后 Agent：** 面板主机执行 `qui-update --component panel --version 1.4.4`，确认面板就绪后，再在每台节点主机执行 `qui-update --component agent --version 1.4.4`；组合主机同样先 panel 后 agent。已在 1.4.4 上的面板之后也可以由 Owner 在「系统设置 → 系统信息 → 面板更新」点按钮完成后续版本。
- **没有迁移：** 迁移头仍为 0134，升级不做数据库变更；`qui-update` 仍在替换前备份面板数据库，健康检查失败或中断时自动回滚。
- **已有安装保持原端口与证书：** 配置不会被改写；新装用 `panel-install.sh`（默认 22550／18880）。
- **升级 Agent 会短暂重启该节点的代理核心**（核心是 Agent 的子进程），既有连接瞬断；修订不变、没有回滚。

## 发布记录

| 项目 | 结果 / 证据 |
| --- | --- |
| 发布提交 `aa549a1`（已废弃） | 本地 `check` FAIL（sing-box 三节点链偶发）、GitHub `check` 42497414214 FAIL（update-test 10 个回滚场景） |
| 回滚回归修复 `67c9fb0` | `deploy/linux/tests/update-test.sh` 28 通过 / 0 失败（修复前 18 / 10） |
| 本地完整门禁（`67c9fb0`） | **PASS**：`check` 2096 秒、真实核 683 秒、合计 2779 秒（16:47:54–17:34:13 UTC），无 DATA RACE |
| GitHub `check`（`67c9fb0`） | **PASS**：运行 42501712348，16:47:57–17:38:56 UTC（`cores=false`） |
| 发布日期、tag 与迁移头复核 | **2026-11-08 17:45 UTC 推送注解 tag `v1.4.4` → `67c9fb0`**（tag 对象 `269193e`）；迁移头仍为 **0134**，本版无迁移 |
| GitHub 发布产物 | 运行 42506527328（17:45:18–17:54:07 UTC）发布 `proxy-control-1.4.4-linux-amd64.tar.gz`、`proxy-control-1.4.4-linux-arm64.tar.gz`、`sing-box-1.14.4-qui.1-linux-{amd64,arm64}.tar.gz` 及各自 `.provenance.txt`、`SHA256SUMS`、`node-install.sh`、`panel-install.sh`；Release 于 17:54:03 UTC 正式发布 |

## 生产升级

2026-11-08 约 17:55–18:01 UTC，经 Owner 同意后用 `qui-update` 从 GitHub Release v1.4.4 升级。

- **恢复点：** Owner 会话建立并校验的加密面板备份 `moJ3TMSm…`（17:55:49 UTC 创建，3,650,045 字节，sha256 `034f1032…`，17:55:53 UTC 校验通过），更新器另外生成 `pre-update-1.4.3-20261109-015620.db`。
- **先面板：** us2 `1.4.3` → `1.4.4`（17:56:23 UTC 起服务，10 秒内无重启）；迁移头仍 **0134**（本版无迁移），`/etc/proxy-panel/config.yaml` 未改写（sha256 仍 `e57bc1d9…`，仍监听 `[::]:443`／`[::]:9443`）。这是本版最后一次需要手工执行的 `qui-update`。
- **更新桥接落位：** `/usr/local/lib/proxycontrol/panel-update-request`（0755）、`/etc/systemd/system/proxy-panel-update.{service,path}`，path 单元 `enabled`／`active`，请求目录 `/var/lib/proxy-panel/panel-update` 为 0700 且属 `proxy-panel`。
- **再逐台 Agent：** 按 uk（17:57:18 UTC）、Alpine（17:59:27 UTC）、us2（18:00:41 UTC）把 Agent 从 `1.4.3` 升到 `1.4.4`；四次 `qui-update` 均打印 `已更新到 1.4.4，服务运行正常` 并退出 0，一台确认在线后再升下一台。
- **核对：** 三台在线（`uk-node`／`alpine-node`／`us2-node` 均为 `proxy-agent 1.4.4`），UK/Alpine/US 修订仍为 295/296/291（无新修订、无回滚），各主机无残留 `rollback.*`；面板与各 Agent 升级后日志 0 WARN／0 ERROR；`/etc/proxy-agent/config.yaml` 未被改写。
- **新功能在生产可用：** `GET /api/v1/system/panel-update` 返回 `currentVersion 1.4.4`、`supported true`、`latest 1.4.4`（17:54:03 UTC 发布）、`updateAvailable false`、`pending null`。
- **真实流量抽样：** 升级后经 uk-node 的 SOCKS5、HTTP、VLESS、Shadowsocks、Trojan 五个入口均返回 200 且出口为 UK 地址 `195.72.166.247`；阻断域名仍不可达（连接失败），错误密码仍被拒。
