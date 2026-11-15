# v1.5.6 发布说明

**状态：已发布（2026-11-15，tag `v1.5.6` → `b624458`，迁移 0149，迁移头 0149）。** 范围对应[审计修复规划](v1.5.6-audit-fix-plan.md)，另含生产中转清理修复（R2／D1／D2）与 Owner 本轮界面调整；R1 空方法拒绝已在发布前撤销。门禁与发布见文末「发布」，生产面板已升级，Agent 未升级（见「生产升级」）。各项测试见[验收记录](v1.5.6-acceptance.md)。

## 修复内容

- 更新安全（A1/A2）：root helper 在私有目录暂存并原子发布状态；更新器只使用 root 安装记录固定的数据库路径。旧安装第一次更新时自动校验并登记，校验失败明确拒绝。配置选择的数据库以面板账号读取，保留 Owner 显式指定备份的权限语义。
- 数据一致性（A3/B1/B2/B6/B7）：快捷编辑固定保存目标与表单快照；隧道发布持久收尾，断开请求、等待超时或面板重启后继续处理；画布更新与补偿保留出站全部字段；外部来源变更后丢弃旧抓取结果；登录、改密写入校验验证时的密码哈希，避免旧密码留下新会话或并发改密互相覆盖。
- Agent（B3/B4/B5）：规则集下载与控制循环分离，失败恢复旧规则文件；应用结果仍先于新修订数据。没有 desired 时 helper 也写初始能力回执，systemd/OpenRC 安装器等待回执，不将空 ID 误认作配置或清除结果。
- 权限与通知（B8/C4）：SSE 按角色过滤并在重新认证时更新角色；Telegram 只接受完整合法且 `ok=true` 的响应；通知处理错误记录脱敏 WARN；Webhook 拒绝跳转到非 HTTPS 地址。
- 其他计划修复（C1–C9）：画布分组补偿只撤销本次入站成员变化；面板更新请求串行检查和写入；卸载先检查更新锁；同名同端口订阅端点追加节点 ID 前六位；修复规则集关键字模拟、入站未知状态、节点状态刷新、排序续存和半年费用；解包失败关闭文件；quick-check 纳入测试依赖、helper 属主场景实际执行；删除重复鉴权与十个不可达函数，校正文档迁移头。

## 工具链与扫描

Go **1.26.11**，`golang.org/x/net v0.62.0`、`golang.org/x/crypto v0.59.0`；必要传递依赖为 `x/sys v0.50.0`、`x/text v0.44.0`。qui sing-box 构建脚本与 CI 断言同步使用 Go1.26.11。

2026-11-14 在独立 Linux 测试目录运行 `govulncheck@v1.9.0 -show verbose ./...`，覆盖 88 个根包、16 个模块及 Go1.26.11 标准库，退出 0，`No vulnerabilities found.`；数据库 `https://vuln.go.dev` 的报告更新时间为 2026-11-11 02:51:11 UTC。扫描针对当时 A4 工作区快照的 Linux/amd64 默认标签，不含上游 sing-box 模块、前端依赖或测试代码。两份独立 GOCACHE 构建的 qui sing-box 产物逐文件相同，实际版本 1.14.4、Go1.26.11、`with_v2ray_api`。

## 升级与验证边界

迁移 0149 为隧道发布批次保存 Vault 密封收尾记录与完成标记，旧备份读取、恢复和未完成操作检查已同步。跨迁移降级仍须配套升级前数据库。helper 与本机更新／卸载脚本需通过本机安装或更新器更新，单独网页升级 Agent 不替换这些脚本。

t3 隔离 Debian 容器已验证真实认证更新 API → systemd helper → updater 的更新成功、受控失败回滚及旧安装路径登记；没有浏览器点击证据。90 秒限速下载实测先复现节点离线，再验证修复后 23/23 样本在线、最大观测心跳间隔 15.279 秒。

发布提交上的完整门禁与 GitHub check 见文末「发布」。另一个会话的 v1.5.5 生产升级记录不属于本版证据。

## 生产中转清理问题修复（R1／R2／D1／D2）

- R1：Shadowsocks 空方法合法：Xray 的 `method` 与 sing-box 的 `cipher` 均默认使用 `chacha20-ietf-poly1305`。R1 的空方法拒绝在发布前撤销，因为它破坏了合法内部中转入站：发布提交 8aa92ab 的本地 Linux 门禁（真实核部分）中 `TestRealThreeAgentChainBatchTCPUDPAndReverseReset` 与 `TestRealSingboxThreeAgentChainBatchTCPUDPAndReverseReset` 均报 `入站「C internal Shadowsocks」（）：Shadowsocks 入站必须指定加密方法`。本轮未重跑 Linux 实核门禁。画布／快速中转新建入口和内部监听仍显式填写默认方法。 TLS 新建入口缺证书生成地址时仍在规划阶段拒绝。shlii 自 #63 起 `validation_failed` 的原因发布后查明：原生规则集策略按「入站×凭据」复制，Xray 校验超出该容器 256 MB 内存上限（见[删除测试记录](v1.5.5-production-relay-cleanup-test.md)第 6.3 节）。#63 相比 #62 仅为两个 Shadowsocks 入站加入 amdin 凭据并启用，同时使方案入口的中转出站（Trojan TLS → 美国 CC:35003）首次可达并进入渲染。需要 Agent 的具体消息，或读取 38.244.50.17 上 2026-11-14 17:06 UTC 前后的 `journalctl -u proxy-agent`。面板目前只保存 Agent 的失败码：`agentpb.ApplyResult` 没有消息字段，Agent 内部的失败消息不会发到面板；由面板记录该消息需要新增协议字段并升级 Agent，留待后续版本。
- R2：发布只校验从实际启用入口可达的中转出站，与 Agent 复用 `desiredstate.ForCore` 的路由图；覆盖出口组、链、策略和备用目标。不可达遗留出站不再挡住发布，可达且目标不匹配仍拒绝；错误指出出站／目标节点 ID 并保留固定原因码。画布撤除返回 409 `dependency_not_ready`。
- D1：画布入口直接删除返回 409 `inbound_in_canvas_scheme` 或 `inbound_detached_entry`，节点列表持续展示原因及方案／遗留资源链接。
- D2：待删除关联规则尽力补齐目标，目标不可用时保留存储信息；前端缺字段显示「目标已删除」／「—」，保留保存、删除后立即应用。
- 撤除阶段一：画布「停用并移除」在停入口的同一修订里一并停用该方案在入口服务器上的专用中转出站，入口服务器不再发布两次；遗留资源中整组无法还原的（如同一对服务器间两条并行通道）可勾选其中连通且成直线的一部分分别挂接或撤除。

## Owner 本轮提出的界面调整（2026-11-14／15）

- 服务器页：去掉卡片与列表视图的「上移／下移」与「添加中转」按钮；顺序在卡片视图拖动调整（触屏暂无排序入口），中转从「节点配置 → 节点列表」添加。
- 节点列表：「添加中转」新建的方案入口及关联到节点的隧道规则入口不再单独成行，只在出口节点的「中转线路」中显示并附入口地址与复制；复用入口、已到不了列表中出口的入口及手动目标规则入口仍单独显示。节点分组页照常把它们列为独立节点。
- 隧道管理：去掉单独的「发布」按钮与确认弹窗；创建、修改、删除和轮换凭据保存后立即检查并下发（缺 GOST 时自动安装），失败保留草稿、显示原因，未生效的行提供「重试」；节点「使用已有隧道」创建后同样直接生效。

本轮 Windows 离线验证：proxy、deployment、desiredstate、agent/reconcile、backup 五包测试及 vet 通过；vue-tsc、44 项前端自测、Vite 构建通过。首轮旧夹具失败及沙箱目录权限失败均保留在[验收记录](v1.5.6-acceptance.md)；权限相关检查经沙箱外离线重跑通过。未连接网络／生产，未运行浏览器、真实核心和 Linux 发布门禁，未提交。

## 发布

提交顺序：`8aa92ab`（发布内容）→ `6538a40`（撤销 R1 空方法拒绝）→ `b624458`（部署脚本测试在非 root／OpenRC 下可运行），均推送到 `main`。

- `8aa92ab` 的本地 Linux 门禁失败：真实核三节点链测试因 R1 拒绝内部空方法 Shadowsocks 入站而失败，据此撤销 R1。
- `6538a40` 的本地门禁与 GitHub check 失败：`panel-update-helper-test` 以非 root 运行时安装记录目录属主不符、`host-network-test` 在 OpenRC 上 `runuser` 无法执行 /run 下的 helper。`b624458` 只改测试夹具（非 root 时登记目录属主取当前用户；可执行文件放到 `/usr/local/lib/qui-hn-test.*`），Linux 上 nobody 68/0、root 72/0、7/7 通过。
- `b624458`：本地完整 Linux 门禁 `GATE b624458 PASS total=1253s`（`QUI_GATE_SCOPE=full`）；GitHub `check` run 43681655857（`cores=true`、`full_gate=true`）第 1 次 `cores` 的 `TestRealV14CompletionPaths/xray/P4` 因 mihomo 端口被占用（`bind: address already in use`，已知偶发）失败，按流程整体重跑一次，第 2 次 `check` 与 `cores` 均成功。
- tag `v1.5.6` → `b624458`；Release run 43684931965 成功，Release 于 2026-11-15 06:26:49Z 发布，13 个资产（安装脚本、amd64／arm64 面板与 Agent 及签名清单、qui sing-box 1.14.4 两架构及来源说明、`SHA256SUMS`）齐全。

## 生产升级

- 面板：2026-11-15 06:38:51–06:39:37 UTC 经「系统设置 → 系统信息 → 面板更新」由 1.5.5 升到 1.5.6，`status.json` 为 `succeeded`；迁移 0149 应用。升级后 6 台节点在线、修订与升级前一致（43／38／39／51／61／62）。
- 恢复点：网页创建加密备份时，请求经 Cloudflare 超过 60 秒被切断（504，数据库 223.4 MiB），本次没有新的加密备份；恢复点为更新器生成的 `pre-update-1.5.5-*.db` 与 2026-11-14 已校验的加密备份。备份改为异步或绕开 60 秒限制列为后续事项。
- Agent：Owner 于 07:10–07:12 UTC 导入官方 1.5.6 并排队网页升级。德国 netcup（面板本机）成功；香港、台湾、zouter、美国 CC、美国 shlii.io 均 `download_failed`，面板的 Agent 目录端口对这些节点不可达（11-14 同一路径曾成功）。这五台仍为 1.5.5，须放行目录端口或在节点本机运行 `qui-update --component agent`。
- 升级后网页删除复测仍被美国 shlii.io 的 `validation_failed` 挡住，复测过程与新发现的 E1–E6 见[生产删除测试记录](v1.5.5-production-relay-cleanup-test.md)第 6 节；E1–E4 已在本地修复，待随下一版发布。
