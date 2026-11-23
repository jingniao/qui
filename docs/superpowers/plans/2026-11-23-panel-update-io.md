# 面板更新磁盘开销实施计划

> **For agentic workers:** Use superpowers:executing-plans; tasks use checkbox syntax.

**Goal:** 减少面板更新重复复制与停服时间，验证后提交推送，不操作生产或发布版本。

**Architecture:** 最终备份仍在停服后复制、持久化并逐字节比较。离线检查直接只读读取备份和 WAL；程序快照移到停服前；原子发布采用文件和目录 fsync。

**Tech Stack:** Go、modernc SQLite、Bash、Linux coreutils。

**Spec:** [设计](../specs/2026-11-23-panel-update-io-design.md)，Owner 已确认执行及回归测试、提交推送。

## Global Constraints

- 最低兼容 v1.7.5，无新迁移，不改 Agent 升级合同。
- 不操作生产；沿用当前工作分支与测试机，保留其他未提交改动。
- 校验/同步失败必须暴露，保留完整备份、迁移检查、10 秒健康检查和回滚。

## Review Focus

- WAL 中的迁移和业务数据不能漏读；检查不得修改原文件或生成 SHM。
- 非空 rollback journal 必须明确拒绝，不能忽略恢复需求后误报成功。
- 快照提前后，备份/预检/信号失败仍恢复旧服务，prepared journal 不得提前。
- 文件和目录 fsync 必须覆盖重命名持久性；递归快照保留整体同步。
- 旧版更新器入口仍可调用新目标数据库检查；脚本改进后续执行才生效。

## Task 1: 数据库检查

- [ ] 在 `internal/platform/db/inspect_test.go` 新增无需临时目录、WAL 文件集合不变和 rollback journal 拒绝回归，观察旧实现失败。
- [ ] 修改 `inspect.go` 通过私有硬链接与 WAL 索引只读打开离线输入，保留完整性与迁移分类；运行 Inspect 与 cmd/panel 数据库相关检查。
- [ ] 仅 `--check-database` 模式以 `signal.NotifyContext(ctx, syscall.SIGHUP)` 取消检查并清理私有索引；服务模式信号行为保持不变。手工检查仍要求文件父目录可写且支持硬链接。

## Task 2: 更新顺序和同步

- [ ] 在 `deploy/linux/tests/update-test.sh` 新增快照失败不停服、停服后最终备份和 fsync 失败中止回归，观察旧实现失败。
- [ ] 修改 `update.sh` 面板快照顺序和相关原子发布同步；修改 `install.sh` 文件发布同步，递归快照整体同步保留。
- [ ] 面板停服失败调用 `dispose_snapshot || true`，Agent 路径保持现状。新增 `preflight_backup` 在 root 登记目录 `check.XXXXXXXX` 内用 `ln -P` 链接备份主库和存在的 WAL（含符号链接，由检查程序拒绝），再调用 `preflight_database`；结束后删除目录并清空全局 `check_directory`。目录创建、链接或删除失败明确报错并返回 4，绝不跳过检查；`copy_database_atomic` 已保证登记目录和备份目录同文件系统，root 不在面板账号目录内创建或删除检查材料。
- [ ] 停服后改用 `preflight_backup`，停服前 `--database` 暂存库预检不变。信号处理清除检查目录；恢复流程清理被 `kill -9` 或断电打断的 `check.*` 真实目录，跳过并报告非目录或符号链接。
- [ ] 补回归场景 38～41：停服失败清理快照、检查路径在 root 登记目录且备份目录无隐藏条目、清理遗留检查目录、检查期间 TERM 恢复旧面板并清理检查目录和快照。
- [ ] Linux 隔离更新/安装器回归通过，检查中断、备份和健康失败恢复路径。

## Task 3: 审查与交付

- [ ] 同步 `docs/deployment.md`、`docs/project-spec.md` 和运维说明。
- [ ] 说明更新器的 root 私有检查路径与手工检查的可写父目录要求；首次由旧更新器升级且检查进程被 `kill -9` 的 `.proxy-panel-check-*` 残留只写运维清理说明。
- [ ] 相关窄检查、格式检查通过，独立只读审查并处理重要问题。
- [ ] 仅暂存本轮文件，提交并快进推送工作分支与 main；不打 tag、不连接生产。
