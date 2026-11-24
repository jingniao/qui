# 更新器 Go 化设计（B3）

> 状态：Owner 已于 2026-11-23 选择方案 B（渐进下沉）。数据库文件链路已随 v1.10.4 发布；其余阶段仍为后续范围。实施见[数据库 Go 下沉计划](../plans/2026-11-23-update-database-go.md)，验证见[发布说明](../../releases/release-notes-1.10.4.md)。来源：2026-11-23 项目审查第二部分第 3 条。

## 1. 现状

| 组件 | 规模 | 职责 |
| --- | --- | --- |
| `deploy/linux/update.sh`（安装为 `/usr/local/bin/qui-update`） | 2177 行、56 个函数 | 下载与摘要校验、版本排序、面板数据库备份/预检/恢复、快照与 journal、信号与中断恢复、更新锁、Agent 引导保留与核心交接、受管 Agent 安装、健康检查、事件上报 |
| `deploy/linux/service.sh` | 565 行、28 个函数 | systemd/OpenRC 抽象、核心交接 CLI 包装、版本比较、安装记录；也被其他脚本复用 |
| `deploy/linux/install.sh` | 1055 行 | 首次安装与更新时的文件发布，被 `qui-update`、`panel-install.sh`、`node-install.sh` 调用 |
| `deploy/linux/update-recovery.sh` | 开机单元 | 以 `/bin/bash "$dir/updater" --recover "$dir" --boot` 恢复每个 `rollback.*` |
| `deploy/linux/tests/update-test.sh` | 1933 行、45 个场景 | 以 `QUI_UPDATE_TEST_ROOT`、替身 init、`export -f cp/sync` 覆盖各失败与中断路径 |

对外合同（任何方案都不能变）：

- 命令行参数、退出码；`--system-update-contract` 输出 `proxy-agent-system-update-v1`（`agent-update-request.sh:127` 校验）。
- `QUI_UPDATE_REPORT_FD`（3～8）上的 JSON 事件行 `{"stage","state","exitCode","error"}` 与阶段名，`panel-update-request.sh`、`agent-update-request.sh` 依赖。
- 登记目录布局：`rollback.*`、`discard.*`、`check.*`、`web-handover.*`、`update.lock`（fd 9 + flock，Alpine busybox 只有 `-n`）、journal v1 文本格式。
- 开机单元固定用 Bash 调用快照内的 `updater` 文件。
- 旧版本写下的快照由快照内自带的旧 updater 恢复（`recover_interrupted_updates` 现有机制），新实现只需能转交。

与 `internal/agent/updater`（2558 行）的关系：它管理受管 Agent worker 的镜像、加密历史与签名清单，作用于 Agent 进程内部；主机级更新（systemd 单元、程序文件、面板数据库、快照）不在其中。可复用的只有摘要/签名校验与暂存读取，重叠比审查时预估的小。

## 2. 方案

### 方案 A：完整迁移到独立 Go 程序（4 个阶段）

**入口**：新增 `cmd/qui-updater`，静态构建 amd64/arm64，打进发布归档 `bin/qui-updater`，安装到 `/usr/local/lib/proxycontrol/qui-updater`。`/usr/local/bin/qui-update` 改为十几行的 Bash 外壳：校验 Go 程序归 root 且非符号链接后 `exec`。不放进 `proxy-panel`/`proxy-agent`：单组件主机只有其中一个，且被更新的程序会在运行中被替换。

**快照**：Go 实现写入快照时，同时放入运行中的 `qui-updater` 副本和一个 Bash 转发脚本 `updater`（`exec "$dir/qui-updater" --recover "$@"`），满足开机单元的固定调用方式。

**阶段**：

| 阶段 | 范围 | 发版 | 风险 |
| --- | --- | --- | --- |
| 1 | `internal/platform/hostupdate`：原子发布与 fsync、journal v1 读写（以 Bash 产出作测试向量）、快照建立/恢复/丢弃、更新锁、数据库备份比较、硬链接预检、事件上报。只合入，不接线 | 不发版 | 低 |
| 2 | `cmd/qui-updater` 接管 `--component panel`（网页面板更新与终端面板更新）：下载校验、版本排序、停服/备份/预检/journal/install.sh/健康检查/回滚/信号、`--database` 降级、Go 快照恢复；外壳对其余参数继续调用旧 Bash（保留为 `/usr/local/lib/proxycontrol/qui-update-legacy`） | 1 个版本 | 中 |
| 3 | Agent 引导更新：受管 Agent 保留/释放、核心交接 arm/status/revoke、受管安装、组合主机默认顺序 | 1～2 个版本，需 Alpine/OpenRC 与真实核心实机验收 | 高 |
| 4 | 删除 Bash `update.sh` 与 legacy；保留外壳与快照转发脚本 | 1 个版本 | 低 |

**兼容**：每个阶段第一次升级都由已安装的旧更新器执行，新实现从下一次更新起生效；v1.7.5 主机照常可升级。`service.sh` 继续供其他脚本使用，Go 侧只实现更新器需要的 init 子集与核心交接 CLI 调用。

**测试**：`update-test.sh` 场景逐步移植为 Go 表驱动测试（假 init 接口 + 临时根目录）；保留 shell 测试覆盖外壳和跨实现恢复（Bash 快照由 Go 转交、Go 快照由旧开机单元恢复）；Linux 测试机跑 systemd，Alpine 主机跑 OpenRC。

**工作量**：约 4000～6000 行 Go（含测试），跨 3～4 个版本；过渡期两套实现并存。

### 方案 B：渐进下沉（已选定）

不重写入口，冻结 `update.sh` 的规模：

1. 今后新增的复杂更新逻辑（需要事务、并发、文件系统细节或复杂校验的部分）写成 Go 子命令，由 Bash 调用。仓库已有同类先例：`proxy-panel --check-database`、`proxy-agent` 的核心交接 CLI。
2. 按出问题的频率挑选现有热点下沉，每次一个，单独评审：
   - 数据库备份比较与预检：`copy_database_atomic`、`compare_database_source`、`preflight_backup`；
   - journal 读写与快照丢弃：`journal_write`、`journal_read`、`dispose_snapshot`；
   - 事件上报的 JSON 转义：`report_json`。
3. 每次下沉保持上面的对外合同与磁盘格式不变，并用现有 `update-test.sh` 场景回归。

**工作量**：每项 200～600 行 Go，随需求进行，不占用专门版本。

## 3. 比较与结论

| | 方案 A | 方案 B |
| --- | --- | --- |
| 可测试性 | 最终最好 | 逐项提升 |
| 风险 | 3～4 个版本都在改主机更新这条最关键的路径，任何缺陷都可能让主机停在半更新状态 | 每次只动一个函数，回归范围清楚 |
| 过渡成本 | 两套实现并存，同一问题要修两次 | 无并存 |
| 与项目规模匹配 | 面向 1～10 名用户，投入偏大 | 匹配 |

推荐方案 B。现有 Bash 实现有 45 个隔离场景覆盖，并经多个版本实机验证；问题主要在新增改动时容易出错，冻结规模加逐项下沉能直接针对这一点。如果今后更新流程仍要大改（例如新增组件或新的 init 系统），再启动方案 A 的阶段 1。

## 4. Owner 决定与后续范围

Owner 已选择方案 B：保留 Bash 入口，冻结脚本规模，后续新增复杂逻辑及现有热点逐项下沉到 Go。方案 A 留作比较记录，不进入实施。

v1.10.3 未包含 Go 下沉。Owner 随后明确要求先实施数据库备份比较与预检，并与 B1 在线判定改造一起完成；当前工作区已落地该首项，验证见实施计划。journal/快照丢弃与事件 JSON 下沉仍为后续独立范围。
