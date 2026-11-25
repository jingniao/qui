# 完整升级收尾与旧状态重建顺序修复

> 后续状态：本修复已随 [v1.12.1](../../releases/release-notes-1.12.1.md) 正式发布，并完成获准的发布测试和相关夹具调整。下文保留单节点恢复阶段的授权范围与验证事实；正式版门禁见发布说明。

**目标：** root 桥接复核与面板回执确认先于旧状态重建；恢复已授权 Alpine 节点的真实任务终态和已发布配置，保留当前配置与持久数据。

**方案：** 重建服务先取得 bridge.lock，再取得 update.lock、operation.lock；删除前要求当前数据根中的完整升级任务已有 root 终态和面板确认摘要，停 Agent 后再核对一次，已封存/删除/重建阶段沿用既有阶段恢复。重建后先重新挂升级目录监听再启动 Agent。complete 对面板 pending/queued 等待，其他失败显式返回；面板在系统任务终态入库后也触发原配置恢复。沿用现有任务、ACK、重建阶段，不新增状态或迁移。

**范围与约束：** 修改 `deploy/linux/agent-state-reset.sh`、`internal/agent/statereset/state.go`、`internal/panel/node/control.go` 和对应产品文档；不新增或运行测试、不提交推送、不改无关未提交工作。当前节点只从 rebuilt 继续，禁止重跑删除阶段，不重登记身份，不重写原始失败回执。恢复前备份节点当前状态、root 回执与面板库；用原始任务和面板已提交游标恢复上传。

- [x] 核对 root 原始结果、面板任务、代码调用与重建阶段。
- [x] 修复桥接复核、ACK、目录监听和配置恢复确认顺序。
- [x] Linux/amd64 Agent 与面板编译、目标机 bash -n、gofmt、git diff --check 通过；独立审查最终无阻塞问题。测试未新增、未运行。
- [x] 保存节点约 11 MiB 停服一致备份、配置/身份/队列身份摘要，以及面板约 392 MiB 在线一致数据库备份。
- [x] 补回原任务及面板已提交 ACK；原始 failed/agent_health 结果由 Agent 正常补传并确认，完整输出由 2569 字节续传至 2972 字节。
- [x] 从 rebuilt 恢复已发布配置，核对身份、配置和数据完整性；该节点基线没有入站及代理核心，不需要新增监听。
- [x] 保留原始失败记录，写入最终恢复证据。

**恢复判定：** 原任务按原始结果 failed 收尾（不伪造 success）；节点实际版本 1.12.0、目标配置成功应用，重建 complete，持久队列与身份不被本轮更换或删除。是否还需新的升级任务以实际结果判断。

## 最终结果

- 2026-11-25 UTC 实机核验：原任务 failed/agent_health 正常终结，root 原始结果与面板入库结果 SHA256 均为 `503104236932bc3948b6da1bd4740f8cbc3c9de2b35aa56a67862d3775268675`，无结果改写。
- 面板自动从已发布修订 174 生成恢复修订 198；解密后除 revision 外全部内容相等。恢复记录 success/198，节点 online、实际 1.12.0、当前及成功修订均为 198，完整升级桥接能力重新就绪。
- 固定重建 helper 已部署到本节点，仅从 rebuilt 继续到 complete，没有进入封存或删除阶段。已停用完成的一次性重建服务并清除其 OpenRC 遗留 crashed 状态；Agent 和升级 watcher 均 started。
- 本轮前后的 config.yaml、configuration.key、客户端私钥和证书、spool-store 身份 SHA256 完全一致；queue.db 只读 integrity_check 为 ok，spool_store、spool_metadata、traffic_delivery_state 和所有批次/游标/预算表内容一致（批次为空）。collector_loss_state 会随进程重启更新检查点，未要求其密文逐字相同。
- 节点材料目录 `/root/qui-upgrade-reset-20261125.ix9Hbygi` 保留前后数据归档、原始任务、旧/新 helper、摘要及只读队列副本；面板材料目录 `/root/qui-node-recovery-20261125.fNy3z9La` 保留一致库备份、已提交游标和最终核验记录。两份节点归档摘要已验证。
- 本轮未替换 Agent 或生产面板二进制；Go 源码改动已编译并审查，尚未打包发布。未提交、推送、公开发布、操作其他节点或运行真实代理请求。

## 审查修正

删除已进入 deleting 时不再等待可能被删除的旧 ACK；停 Agent 后再次检查新增请求，未确认则显式停止而不删除。系统结果已提交后触发幂等配置恢复，覆盖“能力报告先到、任务终态后到”的顺序。
