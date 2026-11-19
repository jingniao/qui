# 网页与终端同时升级实际 Agent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 开启网页升级后，终端 `qui` / `qui-update` 也能更新实际 Agent，准确报告两个版本。

**Architecture:** 现有监督进程持有生命周期及安装历史锁，在自己的事件循环中处理固定本地请求。候选经签名及内容校验后存入本地缓存，保留 `local-` 任务前缀标识本地来源，复用现有加密任务日志、Installer、Driver 和中断恢复；本地任务不走网络下载。shell 先提交引导更新，再发起实际 Agent 安装，第二阶段失败不能回滚已提交的引导程序。

**Tech Stack:** Go、Unix socket、Bash，systemd / OpenRC。

**Spec:** `docs/superpowers/specs/2026-11-16-qui-managed-agent-update-design.md`

## Global Constraints

- 终端和网页同时保留，无需网页再操作或面板管理员凭据。
- 保留配置、身份、业务修订、核心和队列；不修改已发布资产、不提交、不部署。
- Agent 只升级；失败自动恢复本次更新前镜像。
- 下载、签名、大小、摘要、平台及兼容性校验尽可能在停止服务前完成。
- 受管服务停止时拒绝更新；普通非受管节点保持现有行为。
- 离线 `--bundle` / `--sums` 使用新增 `--agent-manifest FILE`，不联网。
- Owner 禁止新增测试方法、测试实体和 mock，也禁止运行测试任务。本计划使用代码审查、Go 编译、Bash 语法和 diff 检查；不将这些称为运行验收。
- 原工作区已有流量淘汰与通知的未提交改动，保留原位，只改本任务涉及文件。当前为功能分支 `feat/v1.4-tunnels`，本次原位执行，不迁移或提交已有工作。

## Review Focus

- 引导版本已是目标、实际 Agent 仍旧：不能提前返回“已是最新”。
- 错误签名、旧版本、候选缺失：在停止前失败，不能执行未认证候选。
- 网页任务/发布/恢复并发：串行保留原任务，实际切换重新核对修订。
- 本地安装中断：按原日志恢复，本地候选缺失明确失败，不转成联网下载。
- 第二阶段失败、停止服务、离线参数：非零退出及准确状态，普通节点/面板路径隔离。

---

### Task 1: 固定本地运维入口与受管安装

**Files:**
- Create: `cmd/agent/local_update.go`
- Modify: `cmd/agent/main.go`
- Create: `internal/agent/agenthost/local_protocol.go`
- Create: `internal/agent/agenthost/local_release.go`
- Create: `internal/agent/agenthost/local_bootstrap.go`, `internal/agent/agentjobs/upgrade_status.go`
- Modify: `internal/agent/agenthost/host.go`, `internal/agent/agenthost/jobs.go`
- Create: `internal/agent/updater/inspection.go`

**Interfaces:**
- `agenthost.LocalStatus(configPath string, stopped bool) (LocalAgentStatus, error)`：运行中通过私有 socket 查询；停止时仅认证读取安装历史，明确 running=false。
- `agenthost.LocalRelease(ctx context.Context, configPath, manifestPath, binaryPath, version string, install bool) (LocalAgentStatus, error)`：验证并暂存本地候选，向监督进程检查或排队并等待实际安装结果。
- CLI：`--local-agent-contract`、`--local-agent-status`、`--check-agent-release`、`--install-agent-release`，后三者要求 config；后两者要求 `--agent-manifest` / `--agent-binary` / `--agent-version`。
- 本地响应状态含 `current.version`、`running`、`ready`、`local_recovery`；`--local-agent-status` 提供固定文本供 shell 读取，停止状态不得报告实际运行版本。
- `agenthost.ReserveBootstrap(ctx, configPath, manifestPath, binaryPath, version, input, output)` / `--reserve-agent-bootstrap`：在服务停止前持有任务日志锁和监督者轮询保留，管道 EOF 或服务停止释放。

- [x] 本地 socket 只接受 status/check/install/reserve 固定请求，0600，监督进程事件循环串行执行，无任意命令入口。
- [x] 候选依已有官方或配置的公钥验证，经 Stage / PreflightAgent 准备；服务端重验并绑定原节点和当前修订。
- [x] 本地安装复用 agentjobs 和 runner.poll；拒绝网页入口使用 local- 前缀。本地任务重启后仅使用 `releases/agent-local-candidates/<digest>.bin`，不要求目录服务在线。
- [x] 状态/预检拒绝其他任务和恢复；原本地 failed_closed 重跑同目标时绑定原任务执行 recover，再安装。就绪判断采用未解决状态，不采用历史失败汇总。完成后检查真实 worker、安装历史和目标一致。
- [x] 编译 `./cmd/agent` 并检查 gofmt；不新增或运行测试。

### Task 2: 终端编排与准确版本

**Files:** `deploy/linux/update.sh`, `deploy/linux/qui.sh`，必要时 `deploy/linux/service.sh`。

**Interfaces:**
- `--agent-manifest FILE` 仅与离线归档和 Agent 组件更新一起使用。
- 由现有服务账号调用 Task 1 CLI；阶段间候选放在服务账号可访问的数据目录，正常和中断后候选保留以供日志恢复。
- 版本查询读取实际受管状态，和引导程序版本分别显示。

- [x] 受管模式检查服务活动状态、当前实际版本和新本地入口；旧安装先通过旧更新器更新引导程序，随后重跑同版本完成迁移。
- [x] 在线取得官方签名清单，离线用显式文件；停止前调用安装中引导程序的校验入口验证候选，再探测已认证目标的新入口。
- [x] 修改“已经最新”判断为同时判断引导及实际版本；同版本仍可补做实际 Agent 更新。
- [x] 引导事务提交后清除 shell 活动回滚状态，再执行本地实际 Agent 安装；任何安装失败非零退出，保留原始错误及两个版本。
- [x] 最终检查服务稳定、真实 worker 版本准确后报告成功；菜单在停止时显示安装版本。
- [x] 运行本任务 Bash 语法检查；不运行脚本测试或真实更新。

### Task 3: 文档、审查与最窄检查

**Files:** `AGENTS.md`, `docs/deployment.md`, `docs/operations.md`，本设计与计划。

**Interfaces:** 对齐实际命令、首次迁移两次、版本语义、离线参数、失败退出与并发行为。

- [x] 更新相关运维说明，保留其他未提交内容。
- [x] 针对 Review Focus 做一次独立最终代码审查，解决重要发现；遵守不新增/运行测试的约束。
- [x] 对最终修改运行最窄 Go 编译、Bash 语法、gofmt 和 `git diff --check`；无新变更或失败不重复。
- [x] 汇报逐文件职责、检查结果、实现选择及未做的测试/部署。

## Execution ledger

Ruling: 用户已明确批准设计并要求实现，按当前会话原位连续执行，无额外计划批准或提交步骤。
Ruling: 采用监督进程内部的私有本地入口，让生命周期锁继续由唯一监督进程持有，避免终端临时启动第二套监督进程。
Pre-flight: Task 2 消费 Task 1 的固定 CLI，Task 3 对齐其命令；无接口冲突。

Task 1: 本地代码实现完毕。2026-11-18 最终明确文件列表执行 gofmt；`GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o .tools/agent-local-check ./cmd/agent` 退出 0。测试未新增、未运行。
Task 2: 终端编排及版本显示实现完毕。2026-11-18 对 `update.sh`、`qui.sh`、`service.sh` 分别执行 `bash -n`，全部退出 0。脚本运行测试、真实更新未运行。
Task 3: 设计、部署、运维及项目说明同步完毕；一次独立最终审查完成，三个 Important 发现已集中修复。最终 `git diff --check` 退出 0；未提交、未发布、未部署。

Final review: `/root/managed_update_final_review` 独立只读审查；无 Critical，无 Minor，三个 Important：
- fixed：本地失败缺少恢复入口——同目标重跑绑定原始签名任务执行 recover，再安装；保留原日志和修订绑定。
- fixed：已恢复的历史失败永久阻塞——新增只读认证状态查询，只检查尚未解决的恢复。
- fixed：预检到引导停服的并发窗口——监督进程持原任务锁并暂停轮询，管道 EOF 或停服释放保留。

Final: Ruling: Owner 明确禁止新增和运行测试，覆盖技能的测试及提交要求；本次采用静态核对、编译和语法检查，未提交。代价：运行时并发、中断与回滚行为尚无新增测试证据。
Final: Ruling: 审查未判断 systemd/OpenRC 实机、并发、中断、离线与失败回滚验收；本次仅交付本地实现，不将编译成功记为上述验收通过。代价：发布前仍需在隔离主机验证。
Final: Ruling: 审查未判断生产部署、正式资产及既有流量/通知改动；本次不部署、不改正式资产、不扩大到这些已有修改。代价：本地实现不会立即改变生产 v1.6.1 的行为。
Final: Ruling: 审查时文档仍在同步，最终文档一致性由实现者核对；已核对首次两次迁移、离线参数、恢复、版本显示和未发布状态。代价：文档未做第二轮独立审查。

## 本任务文件清单

| 文件 | 职责 |
| --- | --- |
| `cmd/agent/main.go` | 注册并分发固定本地运维入口 |
| `cmd/agent/local_update.go` | 参数互斥、状态输出、本地检查/安装/排他保留命令 |
| `internal/agent/agenthost/local_protocol.go` | 私有 socket、本地请求及运行/停止状态查询 |
| `internal/agent/agenthost/local_release.go` | 签名候选、预检、安装、同目标恢复与版本比较 |
| `internal/agent/agenthost/local_bootstrap.go` | 引导重启前的连接与管道保留 |
| `internal/agent/agenthost/host.go` | 监督进程串行处理本地请求、保留时暂停轮询 |
| `internal/agent/agenthost/jobs.go` | 原任务执行器加载本地候选、传播本地安装错误 |
| `internal/agent/agenthost/remote.go` | 网页入口拒绝保留的 local- 任务前缀 |
| `internal/agent/agenthost/state.go` | 登记本地候选受管路径 |
| `internal/agent/agentjobs/upgrade_status.go` | 认证读取待执行任务及未解决的恢复 |
| `internal/agent/updater/inspection.go` | 服务停止时只读认证受管安装历史 |
| `deploy/linux/update.sh` | 两阶段升级、离线清单、互斥、准确版本和退出状态 |
| `deploy/linux/qui.sh` | 分别显示引导、实际运行或停止时的安装版本 |
| `deploy/linux/service.sh` | 初始化失败隔离范围包含本地候选路径 |
| `AGENTS.md` | 同步功能边界、恢复及互斥语义 |
| `docs/deployment.md` | 迁移步骤、离线命令及两阶段更新说明 |
| `docs/operations.md` | 运维状态、恢复方式及未发布说明 |
| `docs/superpowers/specs/2026-11-16-qui-managed-agent-update-design.md` | 记录已批准设计及最终实现选择 |
| `docs/superpowers/plans/2026-11-18-qui-managed-agent-update.md` | 实施清单、检查证据及审查处置记录 |
