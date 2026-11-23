# 审查后续修复实施计划

> **执行方式：** Codex 按任务逐批实施，Claude 只做审查与验证。每批完成后停下，等待审查结论再进入下一批。不要开启任何子智能体／子代理。

**来源：** 2026-11-23 项目审查。第一部分针对未提交的[面板更新磁盘开销优化](../specs/2026-11-23-panel-update-io-design.md)，第二部分针对项目整体。

**Owner 决定（2026-11-23）：** 按本计划执行；第一次升级由旧更新器执行时的 `kill -9` 残留只写运维说明；A、B1、B2、B5 允许新增并运行相关回归测试；B6 允许修改 AGENTS.md 的生产状态段；B3 只写设计文档。

## 全局约束

- 遵守仓库 `AGENTS.md`：不写兜底、不吞错误、不顺手重构、不改无关文件，不碰工作区其他未提交改动。
- 最低兼容 v1.7.5；无新迁移；Agent 协议与 `--check-database` 参数合同不变。
- 不连接生产，不发布，不打 tag，不推送。提交只在每批审查通过后按指示进行。
- Go 测试使用 `-p 1`；只运行与本批改动直接相关的最窄检查。
- Linux 专属 shell 回归（`deploy/linux/tests/*.sh`）由审查方在 Linux 测试机运行；执行方只做 `bash -n`。

---

## 批次 1：面板更新改动的三处修复（与现有未提交改动一起提交）

前提：工作区已有本轮未提交改动（`cmd/panel/main.go`、`cmd/panel/main_test.go`、`deploy/linux/install.sh`、`deploy/linux/update.sh`、`deploy/linux/tests/update-test.sh`、`docs/deployment.md`、`docs/operations.md`、`docs/project-spec.md`、`internal/platform/db/inspect.go`、`internal/platform/db/inspect_test.go` 及两份 `docs/superpowers` 文档），在其基础上修改。

### A1 面板停服失败遗留快照

- [ ] `deploy/linux/update.sh` `update_component` 中 `service_stop` 失败分支：面板组件在 `fail "无法停止 $name，保留恢复材料。"` 前调用 `dispose_snapshot || true`。Agent 分支保持现状（其快照可能承载已授权的核心交接现场）。

### A2/A3 检查在 root 私有目录中进行

- [ ] 新增全局 `check_directory=''`（与 `snapshot` 等全局一起声明并注释）。
- [ ] 新增 `preflight_backup BACKUP`：
  - `check_directory=$(mktemp -d "$registry/check.XXXXXXXX")`；
  - `ln -P -- "$backup" "$check_directory/panel.db"`；若 `$backup-wal` 存在（含符号链接）则同样 `ln -P` 为 `panel.db-wal`；
  - 对 `$check_directory/panel.db` 调用现有 `preflight_database`，原样返回其状态；
  - 无论结果都 `rm -rf -- "$check_directory"`，成功后置空变量；
  - 目录创建、链接或删除失败：打印明确中文错误并返回 4（调用方按"数据库无法用于这次更新"恢复服务并拒绝）。不得返回 2（跳过预检）。
  - 注释写明：`copy_database_atomic` 已保证 `$registry` 与备份目录同文件系统，所以硬链接可行；root 不在面板账号目录内创建或删除任何东西。
- [ ] `update_component` 停服后预检改为调用 `preflight_backup "$backup"`；`--database` 停服前对 `$staged_database` 的预检保持 `preflight_database` 不变。
- [ ] `on_signal`：在删除 `.check-panel.*` 处同时 `[[ -z $check_directory ]] || rm -rf -- "$check_directory"`。
- [ ] `recover_interrupted_updates`：在清理 `discard.*` 的循环旁，同样方式清理 `"$registry"/check.*`（仅删除真实目录，跳过并报告非目录或符号链接），注释说明来源是被 `kill -9` 或断电打断的预检。
- [ ] `cmd/panel/main.go`：仅 `--check-database` 模式额外以 `signal.NotifyContext(ctx, syscall.SIGHUP)` 包装 ctx，使 SSH 断线时 `Inspect` 返回并删除私有目录；面板服务模式的信号行为不变。

### A 文档

- [ ] 更新 `docs/deployment.md`、`docs/operations.md`、`docs/project-spec.md` 中本轮新增的面板更新段落：更新器在 root 登记目录 `check.*` 内对备份硬链接执行检查；手工运行 `--check-database` 时仍要求被检查文件父目录可写且支持硬链接。
- [ ] `docs/operations.md` 补一句：首次由旧更新器升级到本版本且检查进程被 `kill -9` 时，备份目录可能残留 `.proxy-panel-check-*`，确认无更新运行后可手工删除。
- [ ] `docs/superpowers/specs/2026-11-23-panel-update-io-design.md` 与 `docs/superpowers/plans/2026-11-23-panel-update-io.md` 同步上述三处修复。

### A 测试

- [ ] `deploy/linux/tests/update-test.sh`：
  - 替身 `service_stop` 支持 `FAKE_STOP_FAIL=1`（面板停止返回失败、服务保持 active）；替身程序 `--check-database` 在设置 `FAKE_CHECK_LOG` 时把收到的路径和该路径所在目录的条目写入该文件；两个变量加入替身复位列表与文件头说明。
  - 场景 38：面板停服失败 → 状态 1，`$REGISTRY` 无 `rollback.*`，程序版本不变。
  - 场景 39：正常更新 → 检查路径匹配 `$REGISTRY/check.*/panel.db`；结束后无 `check.*`；备份目录无隐藏条目。
  - 场景 40：预置 `$REGISTRY/check.stale/` 含文件 → 更新后被删除。
  - 场景 41：检查期间（替身检查程序向更新器发送 TERM 并等待）→ 无 `check.*`、无 `rollback.*`，旧面板恢复运行、版本不变。
  - 在文件末尾 `run_scenario` 列表登记 38～41。
- [ ] 执行方运行：`bash -n deploy/linux/update.sh deploy/linux/tests/update-test.sh`，`go test -p 1 ./cmd/panel ./internal/platform/db`，`gofmt -l` 涉及文件。

---

## 批次 2：B2 心跳门槛常量 + B5 代码小问题（无行为变化）

### B2

- [ ] `internal/panel/node/metrics.go` 的 `heartbeatFreshness` 改为导出 `HeartbeatFreshness`，新增 `func FreshSince(now time.Time) string { return timestamp(now.Add(-HeartbeatFreshness)) }`。
- [ ] 替换以下写死的 45 秒（时间格式已核实与 `timestamp()` 一致）：
  `node/availability.go`（2 处）、`node/health_checks.go`、`node/latency.go`（2 处）、`node/tcp_probes.go`、`node/tuning.go`、`deployment/agent_system_updates.go`、`deployment/coordination.go`、`deployment/handler_topology.go`、`notification/worker.go`（2 处启动宽限，注释说明与心跳新鲜度同值）。
- [ ] 不改 `internal/agent/updater/install.go` 的 45 秒（含义不同）。注意 `tuning.go` 使用 `>`，其余使用 `>=`，保持原比较符不变。

### B5

- [ ] `internal/platform/db/readpool.go`：`connections < 1` 改为返回错误。
- [ ] `internal/platform/server/server.go` 订阅读取处删除恒真的 `if s.readPool != nil` 分支，直接使用 `s.readPool`。
- [ ] `internal/platform/db`：新增内部函数按路径、`mode` 与 pragma 列表构造 SQLite URI，`URI()`、`OpenReadPool` 与 `Inspect` 改用它，输出的 URI 参数与现状一致。`readonly.go` 的 `preparedDSN` 不动。
- [ ] 执行方运行：`go test -p 1 ./internal/platform/db ./internal/panel/node ./internal/panel/deployment ./internal/panel/notification`；`go vet` 同范围；`go build ./...`。server 包只运行与订阅读取相关的窄用例。

---

## 批次 3：B6 文档整理

- [ ] 生产状态只在 `docs/operations.md`"当前生产环境"一节记录并写明最近核实日期；README 与 AGENTS.md 中的生产段落改为链接并保留"操作前须实时核实"。不写入管理员入口。
- [ ] `docs/archive/core-handover-plan.md`、`docs/archive/core-optional-nodes-plan.md` 移入 `docs/archive/`（`git mv`），修正所有引用（README、AGENTS.md、`docs/README.md`、`docs/architecture.md`、`docs/changelog.md`、`docs/deployment.md`、`docs/operations.md`、`docs/project-spec.md`、`docs/releases/release-notes-*.md`、`docs/superpowers/plans/*`、`internal/agent/core/xray/access_log_test.go` 注释）。
- [ ] `docs/releases/release-notes-*.md` 移入 `docs/releases/`，修正全部引用与 `docs/release-runbook.md` 中的命名约定；`docs/archive/README.md` 与 `docs/README.md` 写明 `docs/superpowers/` 的设计与计划在对应版本发布后移入 `docs/archive/`。
- [ ] 面板更新说明去重：`project-spec.md` 保留规则，`deployment.md` 保留流程，`operations.md` 改为链接。
- [ ] 执行方运行：链接检查（全仓 `git grep` 旧路径为空）；`scripts/quick-check.sh` 的文档部分（如可用）。

---

## 批次 4：B1 在线判定与写连接脱钩——细化设计（只写文档）

- [ ] 阅读控制流接收与心跳处理（`internal/panel/node/control.go`、`repository.go` 心跳事务、`DisplayStatus`/`ConfirmedOffline` 的全部调用方、`notification/worker.go`、订阅过滤），写 `docs/superpowers/specs/2026-11-23-node-liveness-design.md`，回答：
  1. 心跳接收与写库是否串行；内存时间应在何处更新。
  2. 内存表结构、并发保护、控制流断开与节点删除/禁用时的清理。
  3. 哪些读取改用"内存与数据库较新者"，哪些控制资格判断继续只用数据库，逐个列出调用点。
  4. 心跳写库延迟的日志/告警阈值与位置（失败暴露，不掩盖写入问题）。
  5. 测试清单与测试机实测步骤。
- [ ] 不改代码。完成后停下等待 Owner 确认。

## 批次 5：B3 更新器 Go 化设计（只写文档）

- [ ] 写 `docs/superpowers/specs/2026-11-23-updater-go-design.md`：入口（Bash 薄壳 + 发布包内独立 Go 程序，保留 `--system-update-contract`）、journal v1/`rollback.*`/`discard.*` 兼容、开机单元以 `/bin/bash "$dir/updater" --recover` 调用所需的快照内 Bash 转发脚本、与 `internal/agent/updater` 的复用边界、四个阶段的范围与每阶段首次升级仍由旧更新器执行的影响、工作量估计与是否值得做的结论。
- [ ] 不改代码。

## 不在本计划内

- B4 前端大页面：随后续触及这些页面的任务逐步拆分，不单独实施。
