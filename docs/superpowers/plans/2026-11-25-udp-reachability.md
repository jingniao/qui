# UDP 入站连通性提示修复与升级状态排查

> 后续状态：本修复已随 [v1.12.1](../../releases/release-notes-1.12.1.md) 正式发布；连通性请求和提示逻辑位于 `web/src/modules/proxy/reachability.ts`，页面调用该模块。下文保留排查阶段的授权与验证记录，节点后续恢复见[时序修复计划](2026-11-25-upgrade-reset-order.md)。

**目标：** 修复 Hysteria2 等 UDP 入站被 TCP 探测误报失败的问题；只读分析指定 Alpine 节点停留在 agent_health 的原因。

**方案：** 沿用现有 TCP/TLS 检查；UDP 专用协议明确返回不支持检测及空结果，页面显示未检测。当前没有通用 UDP 端口拨号能证明协议可用；不将 UDP connect 或发送成功当作成功，不新增协议客户端或依赖。

**范围：** `internal/panel/proxy/handler_http.go`、`web/src/modules/proxy/pages/ProxyNodeListPage.vue`、产品说明和运维对应说明。本轮不部署、不提交、不推送；不覆盖既有修改。按 AGENTS.md 不新增或运行测试。

- [x] 读取接口、调用方、协议约定与前次实机证据。
- [x] Hysteria2/TUIC/WireGuard/纯 UDP 转发返回 supported=false、reachable=null、udp_probe_unsupported，不发 TCP 探测。
- [x] 页面区分未检测与失败，TCP/TLS 通过也不宣称代理认证和转发可用。
- [x] 只读核对 root 原始终态、面板任务与本地跟踪文件。
- [x] `npm --prefix web run typecheck`、Linux/amd64 `go build ./cmd/panel`、gofmt、`git diff --check` 均通过；独立只读审查未发现需要修正的问题。测试未运行，生产未部署。

## 升级排查证据

2026-11-25 UTC：目标由 1.11.0 更新到 1.12.0。原更新器退出 0，终端事件为 success；00:54:49 root 桥接复核时旧状态重建已停止服务，生成 failed/agent_health 回执。00:54:52 旧状态重建重建数据目录，删除其中的 system-update 任务跟踪；00:54:57 新 Agent 已连面板，但当前 system-update 只保留 probe 请求。root 私有目录及只读发布目录的原请求、结果、日志均仍存在；面板原任务仍 running/agent_health、result_json 为空。Agent 仅枚举本地 *.request.json 发送回执，因此已无原任务可回传。新连接还报告 root 探测启动超时（`root system update probe did not acknowledge this startup`），系统升级能力为 false，面板没有通过协商通道重发原任务。重建服务为 crashed、阶段 rebuilt；配置恢复记录为 pending、修订 0，Agent 心跳未报告配置修订。未修改主机状态、未回填或伪造成功回执，后续恢复须先保全并补回原真实任务回执，再处理配置恢复。
