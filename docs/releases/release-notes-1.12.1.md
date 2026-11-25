# v1.12.1 发布说明

## 修复

- 修复 Btrfs 上 Agent 生命周期锁误判：使用内核锁记录找到候选进程，再以 fdinfo 完整锁记录和文件描述符的设备号/inode 核对锁文件，避免路径 stat 与 /proc/locks 设备号不同导致升级失败。保留原进程身份及交接授权校验。
- 面板连通性检查对 Hysteria2、TUIC、WireGuard 和纯 UDP 转发返回“未检测”，不再用 TCP 失败误报节点不可用。TCP/TLS 通过时明确说明尚未验证代理认证和转发；实际 UDP 协议可用性仍须客户端验证。
- 旧状态重建先等待 root 桥接最终健康复核、真实结果持久化及面板确认，停 Agent 后再次核对未确认任务，防止升级停在“下发中，待确认 / agent_health”。重建后释放桥接锁并重挂升级目录监听，再启动 Agent。
- 配置恢复等待 pending/queued 的最终结果，错误完整返回；面板收到升级终态时也触发待恢复配置，覆盖能力报告先到、任务结果后到的顺序。

## 迁移与兼容

无新迁移，迁移头保持 **0165**，最低兼容 **v1.7.5**；有效面板数据及在途任务继续保留。升级仍沿用既有旧状态换代规则，本次修改换代顺序和确认条件；已进入删除/重建的恢复继续原阶段。

## 文件职责

| 文件 | 本次改动 |
| --- | --- |
| `internal/agent/handovercli/local_linux.go` | 通过进程锁描述符核对生命周期 owner，修复 Btrfs 设备号误判 |
| `deploy/linux/agent-state-reset.sh` | 等待 bridge 与面板 ACK，删除前复核，重挂升级监听再启动 Agent |
| `internal/agent/statereset/state.go` | 等待配置恢复完成，明确返回真实错误 |
| `internal/panel/node/control.go` | 升级终态入库后再次触发待恢复配置 |
| `internal/panel/proxy/handler_http.go` | UDP 专用协议返回未检测，TCP/TLS 返回检查范围 |
| `web/src/modules/proxy/reachability.ts` | 连通性请求、结果解释与明确提示 |
| `web/src/modules/proxy/pages/ProxyNodeListPage.vue` | 调用检查模块并更新菜单名称 |
| `deploy/linux/tests/agent-state-reset-test.sh` | 补齐 restart 夹具及 ACK、锁、监听时序回归检查 |

同批完成先前要求的文档精简：`README.md`、`docs/README.md` 聚焦阅读入口，`docs/architecture.md` 聚焦模块和数据流；`docs/changelog.md` 保留兼容基线后的记录，旧版本全文移入 `docs/archive/changelog-before-1.7.5.md` 并更新归档索引；`docs/deployment.md`、`docs/implementation-decisions.md` 和更新器设计状态同步已发布事实。`AGENTS.md`、项目说明书与本说明同步版本和行为，运维文档仅提交 UDP 检查说明，三个本轮修复计划保留排障和恢复证据。既有未提交的生产升级记录保持在工作区。

## 验证与发布

- 发布提交：`ccebc95281f02362eb062d677b4ffdc8c289e6d5`。
- `quick-check.sh --release --base v1.12.0` 通过：文本格式、前端类型检查/构建/自测、脚本语法与 Go vet。
- Linux 隔离重建检查通过，覆盖 systemd/OpenRC、延迟 ACK、缺失结果、停服后新增未确认任务、已删除阶段恢复、桥接锁释放与升级监听先于 Agent 启动。
- 首轮快速检查触发节点页既有行数限制；将本次连通性逻辑移入 `reachability.ts` 后通过，未修改断言。既有 OpenRC 夹具补齐 restart 行为，失败场景精确检查退出码 1，拒绝将超时误判为预期失败。
- 同 SHA Linux 完整门禁首轮通过：check 1047 秒，真实核心 418 秒、213 项，总耗时 1048 秒。真实核心套件中需要另行指定旧 Agent/固定签名 Agent 的三个用例，以及仅供父进程夹具调用的子进程用例按既有条件跳过；213 项真实核心无核心或环境缺失跳过。
- [GitHub check 45530595757](https://github.com/jingniao/qui/actions/runs/45530595757) 同 SHA 四分片及汇总 check 成功；本次 GitHub cores 未重复运行，真实核心证据来自 Linux 完整门禁。
- [Release workflow 45532742672](https://github.com/jingniao/qui/actions/runs/45532742672) 成功；[v1.12.1 Release](https://github.com/jingniao/qui/releases/tag/v1.12.1) 于 2026-11-25T02:33:34Z 正式发布，tag 指向上述发布提交。
- 已下载核验全部 13 个附件：GitHub SHA256 与大小、SHA256SUMS 的 10 项、双架构四个 Go 程序的 ELF/源码 SHA/无修改标记、归档与独立 Agent 一致、官方 Ed25519 签名序号 1012001，以及安装脚本与双架构 sing-box provenance/二进制摘要/上游源码/构建标签均通过。
- 本机首次附件核验封装会话退出 1，原始日志全部为 PASS、无错误输出；显式检查退出码的缓存复核退出 0，附件未变。最终补核 sing-box 包内二进制与 provenance 后退出 0，保留原始与最终日志。
- 在 Linux 隔离目录运行正式 amd64 面板、Agent、数据库和状态更新工具，均报告 1.12.1；工具协议分别为 qui-update-database-v1、qui-update-state-v1。

## 实机边界

发布前已恢复两台授权节点。Btrfs 节点升级恢复记录见[锁识别计划](../superpowers/plans/2026-11-24-btrfs-lock-owner.md)；Alpine/OpenRC 节点原始 `failed/agent_health` 回执正常收尾，配置恢复成功、重建 complete，身份、配置和持久队列已核对，详见[时序修复计划](../superpowers/plans/2026-11-25-upgrade-reset-order.md)。保留原始失败结果，未改写为成功。

本次发布不部署生产面板或 Agent。新正式包的生产升级、真实浏览器交互、UDP 协议客户端连接与 arm64 原生执行未运行。
