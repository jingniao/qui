# 画布 TLS 通道与入口创建基线验证（2026-11-14）

本轮按 Owner 指定的两项行为实施；当前会话完成实现、测试和复核，没有子代理，没有提交、推送或部署。现有未提交改动保留。

## 1. TLS 通道与既有方案

`git log` 与 `git blame` 确认，`canvas_publish.go` 的 `protocol = in.Protocol` 来自 `c66bee48da9d97e3fe1245776c0d47bb642f206d`，作者时间 2026-11-04 20:58:45 -08:00，提交说明 `feat(v1.4): node relays, relayed subscription entries and canvas exits (N1, C1)`。`git tag --contains` 包含 v1.4.0 及后续正式版；问题早于 v1.5.5。原始 TLS→Trojan 映射来自更早的 `7025079`，被出口入站覆盖。

本轮仅阻止 TLS 通道的覆盖，保留 wireguard 通道原分支。内部监听和配套出站固定为 Trojan/TLS，复用 `SaveInbound`、`managedTLS`、`createCredential`；内部证书仍为 365 天，凭据独立于用户。入口仍使用 Owner 选择的协议，订阅、routeSummary、status 未改变。

协议参与生成名称，普通入站／出站 API 又禁止改协议，因此只删覆盖语句会变成创建新对象或端口冲突。新增的专用更新路径根据 `canvas_tunnel_objects`、目标监听和配套出站引用识别旧对象；预览显示两个 update，沿用 ID、绑定关系与稳定端口，再更新协议、名称、密封设置及独立凭据。不开放公共 API 的协议可变性，不按名字猜归属。

**既有方案下次发布的行为**：已登记归属的旧 TLS 画布（含 v1.5.5 之前创建的画布），即使布局未改动，也会执行上述更新；出口→中间→入口顺序不变。切换期间会短暂中断，已有连接不保证保持。物化 undo 恢复原协议、名称、密封设置和凭据引用；运行版本回滚沿用部署批次的原修订。普通画布原有的快照失败／崩溃补偿边界未扩大。未归属历史对象仍须沿用现有显式认领流程。

SOCKS5、HTTP、Shadowsocks、Trojan、VLESS 外部出口均直接复用前一跳服务器的原出站，不生成额外内部通道；五类均有对象不变回归。Xray／sing-box 无本轮实现改动，复用其已有 Trojan 黄金配置测试。

## 2. 创建基线与撤除

根因是 0146 绑定只保存已应用默认出站，没有创建配置和分组证据。新增 0148，仅为 `canvas_entry_bindings` 增加两个可空 TEXT 列：

- `creation_config_json`：固定字段类别到小写 SHA-256 的规范 JSON。类别为名称、协议、监听地址、端口、核心、协议设置（含 TLS 来源和证书引用）、启用、订阅可见、允许用户。读取已保存的完整入站再摘要；不存秘密明文，不包含用户凭据或动态授权；允许用户集合排序去重，PEM 统一换行并去除首尾空白。
- `creation_groups_json`：创建时加入的分组 ID 排序去重后的规范 JSON 数组。

仅首次实际创建入口时记录；快速创建与高级画布共用同一路径。重复发布不覆盖，重新挂接保留原值，旧 NULL 行不猜测补齐。默认出站继续由绑定原字段比较，reuse 继续恢复原出站。

有基线且未改动：remove-plan 为 ok:true、changes 为空，仍显示将移除的分组。有改动：分别列字段类别、新增／移出分组，要求 acknowledgeChanges。缺基线：保留原确认规则。外部引用仍阻止撤除，确认不能绕过。

备份随整库快照和现有绑定清单保留新列；校验摘要键集合、64 位小写十六进制、规范 JSON、分组排序去重及两列同时 NULL 的历史状态。不要求历史分组仍存在。0148 前归档合法，恢复升级后保持 NULL，恢复不自动发布。恢复往返和旧归档 rewind 链已覆盖。

前端现有 `view.changes.length` 已正确控制确认框显示，本轮不改 Vue 组件；自测补充无改动可直接确认、分组信息保留及确认框条件。

## 3. 修改文件

| 文件 | 本轮修改 |
| --- | --- |
| `internal/panel/proxy/canvas_publish.go` | TLS 不再取出口用户协议；连接保存待升级配对信息 |
| `internal/panel/proxy/canvas_ports.go` | TLS 升级沿用已归属监听的稳定端口 |
| `internal/panel/proxy/canvas_plan.go` | 旧对象显示 update，识别原监听占用与原出站引用 |
| `internal/panel/proxy/canvas_ensure.go` | 物化前进入受控协议升级并注册 undo |
| `internal/panel/proxy/canvas_tls_upgrade.go` | 旧对象归属识别、事务升级与原密封材料／凭据恢复 |
| `internal/panel/proxy/canvas_entry_baseline.go` | 配置摘要、分组规范化、格式校验、差异分类 |
| `internal/panel/proxy/canvas_entry_bindings.go` | 两列读写；重发更新保留基线 |
| `internal/panel/proxy/canvas_entry_materialize.go` | 首次 create 从实际保存对象记录基线 |
| `internal/panel/proxy/canvas_remove_plan.go` | 按基线检测与决定确认，更新说明文案 |
| `internal/panel/proxy/canvas_tls_baseline_test.go` | TLS／旧 SS 升级与 undo、证书年限、五类外部出口、未改动撤除、字段／分组／NULL、TLS 用户入口、重新挂接、用户集合规范化 |
| `internal/panel/proxy/canvas_c1_test.go` | 按 Owner 新行为将内部通道断言更新为 Trojan/TLS |
| `internal/panel/proxy/canvas_entry_materialize_test.go` | 高级 create 和重复物化后撤除无需确认 |
| `migrations/0148_canvas_entry_baseline.sql` | 两个可空列；未修改已有 SQL 迁移 |
| `migrations/checksums_test.go`、`migrations/canvas_entry_baseline_test.go` | 新摘要登记和旧行 NULL 测试 |
| `internal/panel/system/backup/canvas_entry_bindings.go` | 0148 基线归档校验，旧归档合法 |
| `internal/panel/system/backup/canvas_entry_baseline_test.go` | 合法／坏摘要、部分 NULL、非法分组 JSON 等校验 |
| `internal/panel/system/backup/canvas_entry_baseline_rewind_test.go`、`imported_proxy_identity_rewind_test.go`（同目录） | 0148 接入旧归档 rewind 链 |
| `internal/platform/recovery/canvas_entry_bindings_test.go` | 基线恢复往返、不新增部署、0148 前旧行保持 NULL |
| `internal/platform/recovery/node_tags_rewind_test.go`、`import_identity_test.go` | 旧版本 fixture 先撤回 0148 |
| `web/scripts/canvas-remove-selftest.mjs` | 无 changes 的确认与分组展示自测 |
| `AGENTS.md` | 第 14、26.9、31 节同步行为、迁移与备份合同 |
| `docs/superpowers/plans/2026-11-14-canvas-tls-baseline.md`、本报告 | 实施计划与验证证据 |

## 4. 验证

先运行新增针对性测试，复现内部监听／出站为 SOCKS5、未改动入口仍要求确认。升级回归还发现并修正了配套出站及 relay_bindings 凭据须同步更新的问题；TLS 用户入口回归发现并修正了从脱敏返回值取基线的问题。原 C1 测试仍要求内部出口为 SOCKS5，与本次决定冲突，已按新合同改为 Trojan/TLS，保留其余断言。

均使用指定 Go 工具链、GOCACHE、GOPATH，所有 Go vet/test 带 `-p 1`，test 带 `-count=1`。执行五包完整测试，backup/recovery 在沙箱出现 `Access is denied` 后，获准在沙箱外完整重跑。前端使用 Windows `npm.cmd`；typecheck 通过，build 的 esbuild 父目录访问被沙箱拒绝后，获准在沙箱外运行构建与全部自测。

结尾输出（各包最后一次完整运行）：

```text
go vet exit=0
ok  proxycontrol/internal/panel/proxy           10.557s
ok  proxycontrol/internal/panel/deployment       8.900s
ok  proxycontrol/internal/panel/system/backup   21.862s
ok  proxycontrol/internal/platform/recovery    101.472s
ok  proxycontrol/migrations                     2.137s
backup/recovery exit=0

ok  proxycontrol/internal/agent/core/xray         0.112s
ok  proxycontrol/internal/agent/core/singbox      0.103s
Agent golden exit=0

> vue-tsc --noEmit
typecheck exit=0
✓ built in 5.29s
build exit=0
selftests PASS 40/40
New files LF/no BOM PASS 9
```

Go 原始日志：`.tools/canvas-tls-go-test.log`（保留沙箱权限失败）、`.tools/canvas-tls-proxy-final.log`、`.tools/canvas-tls-deployment-final.log`、`.tools/canvas-tls-backup-recovery.log`、`.tools/canvas-tls-vet-final.log`、`.tools/canvas-tls-agent-golden.log`。前端日志：`.tools/canvas-tls-typecheck.log`、`.tools/canvas-tls-build.log`、`.tools/canvas-tls-selftests.log`（另有去除 PowerShell 混合编码 NUL 的 `canvas-tls-selftests-readable.log`）。40 项自测均退出 0；Node 的已有实验性功能 warning 不影响通过。

没有超出两项行为的改动，没有需 Owner 再决定的事项。未运行本轮实机部署／真实代理连通测试；已有实机记录仍代表修改前版本。
