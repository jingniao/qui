# 剩余页面加载、Agent 重装与 CPU 修复记录

2026-11-19；基于 `ec6c6a15` 工作区修改。以下为发布前的本地实现与验证记录。后续已按 Owner 授权发布 **v1.7.3**，仍**未部署**；完整 Linux 竞态检查、发布门禁与附件签名结果见[发布说明](release-notes-1.7.3.md)。生产仍按 v1.7.2 运行，旧版和此前跳过的节点未操作。

## 行为与本地证据

| 内容 | 修改前 | 当前本地行为 / 证据 |
| --- | --- | --- |
| 代理节点页 | 普通心跳进入整批配置刷新；发布状态构造完整用户/策略快照 | 普通心跳只合并运行指标；12 次普通心跳不触发静态配置重读。能力、分组、草稿、发布入队/回执、证书材料变化及重新连接仍使展示失效 |
| 入站草稿比较 | 加载用户凭据、策略与调优建议 | 只读取比较入站监听所需的真实配置、证书和链路。24 入站本地 SQLite 样本中，40 次/轮、21 轮的每次耗时中位数 1.710 → 0.250 ms；不代表生产整页耗时 |
| 新建节点弹窗 | 再读取当前页已有的服务器、入站、分组；提前读取全部高级选项 | 复用三类页面列表及其在途请求；四类高级选项展开或保存时读取。失败可重试且阻止保存；端口和能力仍实际校验 |
| 用户列表 | 代理用户等待管理员等辅助数据 | 代理用户和管理员独立显示，移除完全未使用的分组请求；管理员失败明确显示并可重试 |
| 用户详情 | 等待完整服务器/分组/入站后才开始订阅及活动读取 | 主资料返回后立即启动订阅、预览和活动；服务器改用轻量选择接口；后台状态刷新不作废仍在加载的订阅/预览。辅助失败不隐藏已读到的准确用量 |
| Agent 日志 | 没有应用级总容量限制 | 当前文件与 9 个归档，每个 10 MiB，总计 100 MiB；监督进程统一收集 worker stderr。队列和程序/核心不计入日志预算 |

隔离本地面板 `127.0.0.1:18085`、Chrome 无头浏览器、1600×1000 视口验证：刻意挂起管理员请求时，用户列表在 216 ms 显示；挂起节点分组时，用户详情在 206 ms 显示并已发出订阅请求；新建节点基础表单在 88 ms 显示，三类共享列表没有重复请求，四类高级选项没有提前请求，浏览器无未捕获异常。该数据为合成数据库、回环网络的一次样本，不用于承诺生产耗时。浏览器未实际安装 Agent，也未发布代理配置。

最终代码重新构建后再验：用户列表 246 ms、详情 223 ms、弹窗 97 ms；跨会话修改分组后弹窗读取到新分组，仍无重复选项请求或浏览器异常。相关包 `go vet`、Go 格式检查与 `git diff --check` 通过。本地验证面板已停止。

## 生产 CPU 根因与修复

仅对德国面板机做只读采样，未重启或修改配置。2026-11-19 14:08–14:13 UTC，主机 1 个 CPU，面板连续三段 4 秒占用 92.2%、93.6%、99.0%；Agent 占用很低。8 秒执行地址/调用链采样得到 174 个样本、0 丢失，以实际 v1.7.2 二进制符号解析：约 64.4% 样本经过 `serverAdviceSources.MedianRTT`，约 60.3% 经过控制连接的设备门控刷新。

调用链是 `Control.Connect` → 设备状态刷新 → `Repository.Get` → 调优建议 → 读取并排序最近 24 小时延迟。控制连接此处只需要能力字段，却顺带反复计算调优建议。门控刷新与待发送调优命令的能力读取改为 `GetForCoreSelection`，保持原门控与命令条件。回归在修改前触发 3 次调优辅助查询，修改后为 0；相关设备门控和连接退出/重连检查通过。

**生产修复后 CPU 和页面耗时未测量**：本次按要求不部署。

## 数据与显示约束

- 没有修改流量账本、额度、纳秒边界、流量小时统计或统计清理规则。展示读取只减去无关计算，没有缓存额度写入判断。
- 保留服务器卡片结构和状态优先级、总览内容卡片的固定高度与内部滚动。
- 用户状态、活动、选项分别处理在途响应；连续配置变化不会一直丢弃同一可见集合上已完成的发布状态读取。
- 日志写入失败向系统服务日志明确报告，错误保留到退出；父进程继续排空 worker 输出，避免关闭 stderr 管道导致 SIGPIPE。失败后的日志不落盘，不写到另一份无限增长的文件。
- 日志限额需主机上的监督/引导程序更新并重启后生效；只网页更新 worker 不会替换旧监督进程。旧 journald/syslog 历史不由本次改动删除，见[运维说明](operations.md)。

## 验证记录

- 已通过：用户加载延迟、迟到响应、状态刷新竞态、辅助失败和门户域名独立读取回归；代理节点状态/重试/交错响应；新建节点共享选项、延迟高级选项、失败阻止保存及原协议/端口/证书自测。
- 已通过：入站真实投影与链/证书一致性，分组无授权用户时仍通知，普通与批次发布提交后通知，快速接入/画布入口写入通知，隧道失败补偿只通知一次，证书发布窗口保持原行为。
- 已通过：日志容量边界、重启后继续轮转、并发写入、独占写入、轮转错误报告、写入失败后 worker 管道继续排空。
- Windows `-race` 已尝试但未通过运行环境初始化：现有 Zig 链接器先缺少同步 API，补充其现有 `api-ms-win-core-synch-l1-2-0` 导入库后构建成功，但 ThreadSanitizer 启动分配影子内存失败（Windows 错误 87），测试主体未执行。常规并发用例通过；不能记为竞态检测通过，也未安装或修改系统工具链。
- 已通过：前端类型检查、前端构建、面板构建、上述本地浏览器检查。后续审查修改按影响范围补验，安装与身份补传记录见下方收尾补充。
- 未运行：本次完整发布门禁、生产部署后复测、生产实际重装。正式发布前仍须按发布手册执行同一提交门禁。

## 文件职责

| 文件 | 本次职责 |
| --- | --- |
| `internal/panel/deployment/inbound_state.go`、`handler_http.go`、`proxy.go` | 轻量草稿接口、展示只读连接、只在必要时解密历史修订 |
| `internal/panel/proxy/inbound_projection.go`、`snapshot.go`、`service.go` | 共用真实监听/证书/链投影，与发布语义一致 |
| `internal/panel/node/repository.go`、`control.go` | 无调优建议的能力读取；控制连接避免重复高 CPU 查询；分组通知接口 |
| `internal/panel/node/groups.go` | 分组增删改提交后通知，不受是否有授权用户限制 |
| `internal/panel/proxy/repository.go`、`chains.go` | 入站、出站与链的公共写入完成后通知 |
| `internal/panel/proxy/canvas_entry_bindings.go`、`canvas_remove.go`、`relay_deletion.go` | 画布绑定/补偿/撤除与级联删除完成后通知 |
| `internal/panel/proxy/tunnels_publish.go`、`tunnels_finalize.go` | 受管监听、真实隧道准备与补偿事务完成后通知 |
| `internal/panel/deployment/repository.go`、`batches.go` | 普通发布和批次首跳提交后发送待发布事件 |
| `internal/platform/server/server.go` | 装配展示读取、草稿/分组/证书和部署入队事件 |
| `web/src/app/events.ts` | 转发草稿变更事件 |
| `web/src/modules/proxy/proxyPageInventory.ts`、`pages/ProxyNodeListPage.vue` | 共享列表、指标合并、能力变更失效、发布状态请求合并和恢复读取 |
| `web/src/modules/proxy/api.ts`、`pages/InboundListPage.vue`、`pages/OutboundListPage.vue` | 协议支持表失败明确展示 |
| `web/src/modules/proxy/components/node-editor/NodeEditorDrawer.vue`、`NodeAdvancedFields.vue` | 编辑器复用列表和延迟高级选项 |
| `web/src/modules/users/pages/UserListPage.vue`、`UserDetailPage.vue` | 用户资料与辅助分区独立加载、错误与竞态处理 |
| `internal/platform/logging/rotating.go`、`rotating_test.go` | 100 MiB 轮转及容量/故障/并发回归 |
| `cmd/agent/main.go`、`internal/agent/agenthost/host.go`、`jobs.go`、`recovery.go` | 主进程使用轮转日志，所有受管启动/升级/恢复路径共享 worker 输出 |
| `internal/panel/node/control_read_test.go`、`group_notify_test.go` | 高 CPU 路径和分组通知回归 |
| `internal/panel/deployment/inbound_state_test.go`、`queued_notify_test.go`、`tunnels_finalize_test.go` | 发布状态、入队通知与补偿通知回归 |
| `internal/panel/proxy/acme_certificate_test.go`、`chains_test.go`、`draft_notify_test.go` | 投影证书/链一致性与写入通知回归 |
| `web/scripts/inbound-state-selftest.mjs`、`node-editor-selftest.mjs`、`v158-node-list-selftest.mjs`、`user-loading-selftest.mjs` | 页面加载与既有显示/编辑行为回归 |
| `docs/operations.md`、本记录、实施计划 | 运行日志、重装约束、证据及未覆盖范围 |

| `web/src/modules/nodes/components/ServerEnrollment.vue`、`NodeRecoveryPanel.vue`、`ServerRowMenu.vue`、`pages/NodeListPage.vue`、`api.ts`、`coreInstallOptions.ts`、`enrollmentStage.ts` | 已登记节点安装入口、同机覆盖/新机接替选择、恢复进度与原主核心顺序 |
| `internal/panel/node/handler_http.go`、`identity_recovery.go`、`control.go`、`repository.go`、`pkg/contracts/agentpb/replacement.go` | 恢复码绑定旧身份，确认最终补传，拒绝退休证书及迟到旧心跳 |
| `internal/agent/control/replacement_drain.go`、`internal/agent/spool/collector_loss.go`、`cmd/agent/main.go` | 停机后用旧身份上传最终队列，核对 ACK 和采集关闭证据，确认登记后才创建运行日志 |
| `deploy/linux/node-install.sh`、`install.sh`、`agent-replace.sh` | 显式覆盖参数，先准备校验，再停服补传，确认后持锁清理固定 Agent 数据目录 |
| `internal/agent/control/replacement_drain_test.go`、`internal/panel/node/identity_recovery_enroll_test.go`、`cmd/agent/flags_test.go` | 真实队列、错误端点、旧证书拒绝、登记重试与实际二进制启动回归 |
| `deploy/linux/tests/agent-replace-test.sh`、`node-install-test.sh`、`web/scripts/core-install-options-selftest.mjs`、`node-address-selftest.mjs`、`ui-regression-selftest.mjs` | 安装故障、同机/新机命令、恢复进度和主核心顺序回归 |
| `AGENTS.md`、`docs/deployment.md`、`docs/README.md` | 同步安装/日志契约、部署说明与验证记录入口 |

## Agent 替换与最终审查

- 已登记节点可再次显示安装命令。默认同机覆盖：准备并校验程序后停止旧服务，以旧身份向本次安装指定的新端点上传最后的数据，收到面板完整确认后才删除旧 Agent 本机数据。上传失败保留原数据和重试进度；面板记录、历史流量、授权和配置全部保留。
- 新机接替为单独选项；新登记成功后旧证书失效，旧连接退出。该模式不能补传或删除另一台机器上的本地数据，弹窗明确说明。
- 默认保留节点报告的核心集合和主核心顺序。最终上传后安装失败，可去掉覆盖参数，沿用同一服务器记录的恢复码继续登记；回复丢失时保留原私钥重试。
- 自定义数据路径、CA、挂载或链接等不满足清理边界时明确拒绝。OpenRC 需要预装 `strace` 且允许观察原进程退出；不满足时在停服前拒绝，不自动安装工具。
- 真实临时 SQLite 队列、TLS/HTTP2 错误服务端点、恢复事务和实际 Agent 二进制的未登记/目录占用检查通过。systemd/OpenRC 服务替身覆盖 14 个成功与故障分支，另有 ACK 后登记丢回复重试；既有安装脚本 54 项通过。相关前端回归、类型检查和窄 Go 检查通过。
- 独立最终复审提出的五项问题已修复并限定复核通过：清理后的失败重试、新机模式、原核心选择/顺序、新端点真实补传验证、未登记时不创建日志目录。用户加载、CPU 路径和通知失效修复也经过审查。
- **未验证**：真实 Linux 主机同机覆盖、真实 systemd/OpenRC 管理器和实际发行附件的完整安装。独立附件验签尝试因本机缺少 `jq`、临时获取工具的命令被执行环境拒绝而未运行；故障脚本的签名阶段使用测试替身，不算真实发行包验签通过。
