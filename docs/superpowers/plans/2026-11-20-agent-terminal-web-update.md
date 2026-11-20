# 网页与终端 Agent 升级统一实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 网页通过节点本机调用与终端相同的完整 Agent 更新器，并显示可追溯的具体失败信息。

**Architecture:** 独立 root 更新服务固定调用已安装的 `qui-update --component agent --version X.Y.Z --yes`。Agent 只负责固定请求、状态和日志传输；面板持久化任务、尝试、阶段、结果和输出，网页展示真实结论。安装和恢复复用终端，不重新实现。

**Tech Stack:** Go、现有 gRPC/mTLS、SQLite 迁移、Bash、systemd/OpenRC、Vue 3/TypeScript；不增加运行时依赖。

**Spec:** [统一设计](../specs/2026-11-20-agent-terminal-web-update-design.md)

状态：Owner 后续已要求实施全部规划并发布 v1.7.5；实现与现有门禁验证进行中。生产不部署。

关联独立需求：[订阅线路维护与离线过滤](2026-11-20-subscription-route-availability.md)。该计划处理入口、中间、出口任一受管机器维护或确认离线时整条线路退出订阅，不依赖本升级改造。

## Global Constraints

- 下载来源、安装范围、信任、预检、锁、两阶段提交及失败恢复均沿用终端；网页不使用面板下载目录或自动切换来源。
- 固定更新 Agent，不接受远程命令、URL、路径或环境变量；同时覆盖 systemd/OpenRC。
- 已发布旧任务保留原读取、回执及恢复；新网页任务只接受新的完整升级能力，不降级为仅工作进程升级。
- 具体错误、退出码、恢复结果和引导/实际版本分别保存；未知结果不伪造，不吞错误、不静默截断。
- Owner 已明确只授权现有测试和发布门禁，不新增测试方法、实体、mock 或独立隔离场景；已授权完成后发布 v1.7.5，生产不部署。
- 保留当前离线判断工作区改动；本计划不扩展到核心版本升级、策略发布或其他页面改版。

## Review Focus

1. 外层网页任务不能进入旧工作进程升级队列，避免更新器把自身判断为忙碌。
2. Agent 重启及安装器更新 runner/watcher 文件时，root 更新进程必须继续运行；断电后不得自动重复未知执行。
3. 第二阶段失败可能留下新引导与旧工作进程；不得把这种结果报为整套成功或整套已回滚。
4. 更新器原错误、恢复错误、结果保存错误需分别保留；日志缺段和读取失败须有明确状态。
5. 旧任务、新任务、终端命令、核心升级、配置发布和节点接替之间的互斥须覆盖真实已发布状态。

下面各任务列出的验收场景仅为后续验证清单；实施只运行已获授权的现有测试与发布门禁，不新增测试。

---

## Task 1：完整升级协议和持久数据

**文件：**

- 新增 `pkg/contracts/release/agent_system_update.go`：固定请求、阶段、结果、日志分块和 ACK 合同，独立于旧 `UpdateResult`。
- 修改 `proto/agent.proto`，通过 `scripts/generate-proto.sh` 更新 `pkg/contracts/agentpb/` 生成文件。
- 新增 `migrations/0157_agent_system_updates.sql`（实施时核实编号）：新任务、尝试、日志表；扩展 `node_update_capabilities`。
- 修改 `internal/panel/node/update_support.go`、`repository.go`、`identity_recovery.go` 以及相关运行状态读取：完整升级能力与引导版本。

**接口：**

- `AgentSystemUpdateRequest`：schema、任务/尝试 ID、节点 ID、期望业务修订、目标正式版本；不携带目录项、下载地址或执行代码。
- `AgentSystemUpdateProgress`：尝试 ID、单调事件序号、阶段、观察时间。
- `AgentSystemUpdateResult`：尝试 ID、目标、结论、失败阶段、原始错误、可空的更新器/子命令退出码或信号、两个阶段结果、引导/安装/运行版本及各自读取错误、恢复结果。
- `AgentSystemUpdateLogChunk` / `AgentSystemUpdateAck`：尝试 ID、递增序号、偏移、内容摘要、内容与已持久化确认位置；日志与结果独立传送。
- 三张表定名 `agent_system_update_tasks`、`agent_system_update_attempts`、`agent_system_update_logs`。任务绑定节点及业务修订；尝试固定目标和原始请求；日志按尝试、序号唯一，同键不同内容拒收。
- 新任务状态区分 queued/running/success/failed/unconfirmed；失败结果附服务是否已恢复，未知终态保持 unconfirmed 并继续阻塞冲突操作。

- [x] 定义新协议及边界校验，不修改旧 Agent/核心结果合同的语义。
- [x] 定义独立完整升级能力；新能力探测失败携带原因，不靠版本字符串判断。
- [x] 新增迁移和 Go 读写模型；保持旧任务及其 FK 数据不变。
- [x] 静态审查新结果能区分第一阶段失败、第二阶段失败、恢复失败和未知结果。

**待授权验收：** 未知字段/非法版本拒绝；空错误不冒充明确失败；日志重复一致可 ACK、重复不一致拒收；迁移前后旧任务可读。

## Task 2：节点 root 更新桥接与安装器集成

**文件：**

- 新增 `deploy/linux/agent-update-request.sh`：固定请求校验、root 接收记录、固定更新命令、结果/日志发布。
- 新增 `deploy/systemd/proxy-agent-update.service`、`proxy-agent-update.path`。
- 新增 `deploy/openrc/proxy-agent-update`、`agent-update-watch`。
- 修改 `deploy/linux/install.sh`、`update.sh`、`update-recovery.sh`、`uninstall.sh`；必要的跨 init 操作放入已有 `service.sh`。
- 修改 `internal/agent/agenthost/jobs.go`、`local_release.go` 及必要的 `internal/agent/agentprocess/driver.go` 错误传递：实际安装和恢复的底层原因必须回到共同终端入口，不改变安装分支或旧回执字段含义。
- 新增 `internal/agent/systemupdate/`：Agent 侧固定目录协议、能力探测、状态和日志读取；不承担安装逻辑。

**接口与路径：**

- Agent 写入 `/var/lib/proxy-agent/system-update/request.json`，原子提交；固定操作为 probe 或 upgrade。
- root 私有接收和执行材料保存在 `/var/lib/proxycontrol-install/agent-system-updates/<attempt-id>/`；Agent 不得修改。
- root 向 `/var/lib/proxy-agent-update/` 发布仅 Agent 可读的状态和本任务日志；Agent 无写权限，不能让 root 跟随它指定的链接。
- 新安装和启动后通过固定 probe ID 与 root 回执确认能力、更新器可用及 watcher 就绪；探测不执行升级。
- `update.sh` 增加可选结构化报告 FD；仅报告固定阶段与明确取得的错误事实，不改变下载/安装/恢复分支。网页请求不得设置该 FD 或其他环境变量。

- [x] 实现 root 请求边界校验及独立、持久化的接收记录；先接收再执行，相同尝试只执行一次。
- [x] 固定调用更新器，节点直接从现有 GitHub 来源下载；保存 stdout/stderr、退出码和阶段事件。
- [x] 记录前后版本、实际服务状态及读取错误；原始失败与恢复失败分别落盘。
- [x] 补全实际安装调用链中仅返回分类码或后续错误覆盖先前错误的路径；校验、启动和恢复保留原始原因及可取得的本次子进程输出，共同改善终端与网页报错。
- [x] 实现 systemd/OpenRC 独立服务；更新 Agent 时不随 Agent 停止，不继承 Agent 的服务进程组。
- [x] 安装器发布桥接文件；更新器快照和回滚纳入新增系统文件，运行中使用固定脚本副本，当前任务未结束时不重启 runner/watcher。
- [x] 卸载时处理新增服务和脚本，保留身份、升级结果及恢复材料；不删除组合主机仍需要的共享更新器。
- [x] 启动恢复先核实原执行者和更新器原恢复记录，结果不明则保留原任务；不重建安装历史、不自动猜测成功、不自动重跑未知任务。

**待授权验收：** 两种 init 上 Agent 被停止后 root runner 仍运行；自更新桥接文件不中断当前任务；GitHub 下载失败、摘要错误、磁盘写失败、服务启动失败均保留原始报错；网页和终端命令争用同一个锁；原地重启、kill 和主机重启不重复执行。

## Task 3：控制连接、面板任务与重连补传

**文件：**

- 修改 `internal/agent/control/control.go`，新增 `internal/agent/control/agent_system_updates.go`。
- 修改 `internal/panel/node/control.go`、`internal/platform/server/server.go`。
- 新增 `internal/panel/deployment/agent_system_updates.go`、`agent_system_updates_http.go`。
- 修改现有 `agent_updates_http.go`、`agent_recovery.go` 的入口分派及 `task_history.go`。

**接口：**

- 网页提交新任务使用节点 ID、确认节点 ID、目标版本、期望修订；不再要求 releaseId 或导入面板目录。
- 新任务通过新 protobuf 消息传递；Hello/Welcome 明确协商完整升级能力。旧能力继续只服务历史任务回执和恢复。
- 对外沿用 `/api/v1/agent-updates` 的任务、尝试、重试入口，加入完整升级模式字段；新增 `GET /api/v1/agent-updates/{id}/logs`，按尝试及偏移读取、复制/下载输出，仅 Owner 可访问。
- 新任务请求 handler 仅接受新输入合同；任务历史可读旧表和新表，按真实模式解释结果，绝不把新请求悄悄路由到旧安装方式。
- 面板先持久化日志/事件/结果再发送 ACK；节点重连补传，重复结果必须相同。ACK 未到前，Agent 不清理待传数据。

- [x] 实现任务入队、固定目标及授权审计、节点身份/修订和真实能力检查。
- [x] 完整任务不进入旧 agentjobs，避免本机安装自锁；实际 Agent 切换继续由 qui-update 进入现有本地监督进程。
- [x] 持久化阶段、尝试和详细结果；错误返回保留可定位的具体原因，不统一压成“存在冲突”。
- [x] 实现日志分块持久化和 ACK；明确既有容量/保留规则，日志缺段、保存失败或清理均有状态，不静默截断。
- [x] Agent 重连后补传 root 保存的当前任务；浏览器刷新和面板重启后能继续查看。
- [x] 重试新建尝试，固定原目标，保留旧失败和日志；结果未知时先核实原执行和恢复事实。
- [x] 任务列表统一展示新旧记录；旧任务恢复继续使用真实旧请求，不重新生成当前格式请求冒充原操作。

**待授权验收：** 断连、重复下发、重复 ACK、面板重启；第二阶段失败时正确显示新引导和旧 Agent；root 结果已保存但尚未回传时不显示成功；阶段和日志保存失败可见。

## Task 4：互斥、节点生命周期与备份恢复

**文件：**

- 修改 `internal/panel/deployment/agent_updates.go` 的共用预留判断、`node_tasks.go`、`recovery.go` 及调用节点忙碌检查的相关入口。
- 修改 `internal/panel/node/identity_recovery.go`。
- 修改 `internal/panel/system/backup/agent_updates.go`、`inventory.go` 和相关恢复表清单。

- [x] 新完整任务纳入核心升级、配置发布、节点删除/接替、恢复的互斥；结果未知仍阻塞。
- [x] 终端保留原本机锁和恢复规则；网页冲突返回具体事实，不新增绕锁路径。
- [x] 节点操作不删除尚未收尾的 root 升级任务；清理必须遵循原权限和任务结果语义。
- [x] 新任务、尝试、日志、能力进入备份校验及恢复；恢复不能覆盖仍在执行的节点任务。
- [x] 区分旧式任务与新式任务的恢复，保存已发布历史，禁止把历史错误重写成成功。

**待授权验收：** 新旧任务交叉冲突、同节点两网页任务、终端并发、节点同机覆盖及新机接替、含任务备份恢复。

## Task 5：网页升级入口与具体错误展示

**文件：**

- 修改 `web/src/modules/system/components/NodeVersionPanel.vue`、`agentUpdatesApi.ts`、`pages/TaskRecordPage.vue`。
- 修改 `web/src/modules/nodes/api.ts` 及运行状态读取合同；新增独立 `AgentUpdateDetails.vue` 放在系统模块组件目录，避免继续扩张版本页。
- 必要时扩展 `updateResultText.ts` 的新模式标签，不用标签代替原始错误。

- [x] 单台、批量、列表和卡片中的所有 Agent 升级入口统一提交目标版本，移除新任务的面板材料导入依赖。
- [x] 缺少完整升级能力时明确展示本机迁移原因与终端命令；不显示可执行的旧式升级按钮。
- [x] 展示引导和实际运行版本；未知值显示读取错误，不用安装版本填充运行版本。
- [x] 任务详情展示阶段、原始错误、更新器/子命令退出码、恢复结果和每阶段版本，支持查看、复制、下载本次日志。
- [x] 批量升级逐节点保存和展示入队错误及执行错误；不再用空 catch 只累计失败数量。
- [x] “已提交”与“升级成功”文案分开；结果待确认时保留已知阶段，不按网页等待时间猜测结论。
- [x] 日志以纯文本显示，按需读取，保留换行；不渲染为 HTML。

**待授权验收：** 下载 404/超时、签名错误、安装错误、健康检查失败、恢复失败逐项显示具体文本；批量部分失败；刷新后继续查看；非 Owner 无权读取；特殊字符日志安全显示。

## Task 6：文档与验证交接

**文件：**

- 修改 `AGENTS.md`、`docs/deployment.md`、`docs/operations.md` 中相关升级说明；必要时同步 `docs/architecture.md` 的当前实现。
- 本设计和计划随实现记录实际差异，不改写旧发布说明。

- [x] 说明网页等价命令、节点直连 GitHub、完整更新范围、两阶段失败与具体错误入口。
- [x] 说明一次性安装新服务及旧更新器可能需要的同版本重跑；版本号在确定发布版本后填写。
- [x] 说明网页在线升级与终端离线参数的边界；不承诺网页离线包功能。
- [x] 实施完成后按 Owner 授权运行最窄构建、类型检查与相关验证，保留首轮失败；不把检查通过写成升级实测通过。
- [ ] 经单独授权，在隔离 systemd/OpenRC 节点比较网页和终端同目标升级的安装记录、系统文件、两个版本、服务及业务修订，并验证失败恢复；生产不在本计划执行范围。

## 规划自审与当前交付

- 已覆盖原始错误、退出码、两阶段部分成功、重启后补传、自更新执行者存活和自锁风险。
- 沿用终端信任与下载行为，不额外扩展签名体系或发布附件。
- 需要保留的兼容仅涉及已发布任务/结果和旧节点首次启用，已在设计中说明。
- 最初规划轮仅新增本计划和设计文档；后续实施状态见下一节，正式发布与生产操作以主发布记录为准。

## v1.7.5 实施进度

- 合同/protobuf/0157、新任务API、节点真实root probe、独立投递/补传ACK、互斥、历史合并、备份校验及恢复代码已落地，进入最终审查。
- root桥接、systemd/OpenRC、终端结构化原始错误和恢复收据由同一安装链实现；网页没有第二套下载/安装逻辑。
- Go现有backup、release包通过；node与Agent control首轮暴露列scan和缺root服务探测等待问题，修复后两包窄复验通过；deployment旧Agent/核心/任务历史定向通过。后续统一发布门禁由主代理记录，不把这些证据写成新Linux升级实测。
- 首轮deployment真实路径fixture的loopback解析错误由独立订阅任务修复，保留原失败并由该任务复验。
- 不新增测试、不建立独立Linux验证场景，不部署生产。

- 最终审查补充修复：第二阶段原 local request ID、候选摘要和完整签名清单在执行前进入 root 持久事件；通过监督进程只读核对原 job 与 InstallationRequest，断电后收敛真实安装/恢复结果，未知不重跑。原 cause 在 rollback 前、outcome 在结束前加密保存；不改变 jobs/spool ABI。无原 CLI 退出收据时保留空退出码和中断失败，真实 committed 不冒充整套成功。
- 本次恢复修正的现有最窄检查：cmd/agent flag 清单、agenthost 原本机签名/中断/恢复、updater 原安装日志/失败/保留恢复共 7 项通过（0.124s / 0.782s / 6.928s）；bash -n 通过。独立 systemd/OpenRC 新场景未运行，最终统一完整发布门禁尚由主代理执行与记录。
