# v1.5.8 发布说明

**已于 2026-11-15 发布，tag `v1.5.8` → `a1b100b`。** 迁移 **0150**，迁移头 **0150**。范围由 Owner 当日确认，见[确认规划](v1.5.8-plan.md)。下文时间均为 2026-11-15 UTC。

## 变更内容

- 中转方案「停用并移除」一次确认后立即删除面板方案与专用对象，不再等待离线或应用失败节点。`POST /api/v1/canvas-schemes/{id}/remove` 与保留的 `DELETE /api/v1/canvas-schemes/{id}` 均执行完整撤除，请求体同样需要 `updatedAt`，200 返回 `{schemeId,removed:true,pendingNodes:[{nodeId,name,reason}]}`。移除旧 `remove-plan`、`remove-status`、遗留资源与重新挂接路由。
- 受影响节点按已发布状态删去对象，在线即下发、离线重连后下发；失败进入持久待清理队列，1／5／30 分钟后及之后每 30 分钟重试。新撤除登记将 `attempts` 归零、清空失败码并立即重试；该节点的清理修订仍 pending 时先等其结束，再按新增删除生成下一次清理。清理修订作者 `auto`，摘要 `auto: relay cleanup`。
- 删除入站连带删除任何实例引用它的方案、它所属的入口方案、linked 规则及失去全部规则的隧道；删除服务器连带删除其入站及涉及任一实例或隧道跳的方案、隧道、规则，已配置资源的待登记节点也走同一连带删除。两个 DELETE 接口均成功返回 200 `{pendingNodes:[{nodeId,name,reason}]}`，取消空白登记或重复删除服务器返回空数组；被删节点自身待清理行在同一事务清除。一次确认列明名称，服务器显示「待清理」与最近失败码。
- 手工中转出站、代理链等外部消费者分别以 409 `inbound_delete_blocked`／`node_delete_blocked` 阻止删除；原 `inbound_has_relay_outbound` 也统一为 `inbound_delete_blocked`。阻塞项在错误体 `error.blockers:[{kind,id,name}]`。受影响的隧道发布批次未收尾时，即使已 success/failed，仍作为 `deployment_batch` blocker，等待收尾后再删。
- 方案入口一律新建且只属于一个方案，任何有入口绑定的入站不得作为画布 inbound 实例或 linked 规则目标；保存、规划和发布共用校验，返回 400 `inbound_is_scheme_entry`。移除复用入口、仅删除画布、遗留资源及重新挂接流程。
- 面板接受本节点已下发过的任何修订的流量、访问记录与设备观测，包括失败／回滚修订；未知或不属于本节点的修订仍拒绝，解除 CC 积压旧修订数据导致控制连接反复断开的阻塞。
- 节点列表载入互相独立，列表变化时重新获取选项；卡片去掉直连／中转／隧道转发类型标签，只保留导入来源标签，协议名称移至第二行。

## 迁移与升级注意

0150 新增 `relay_cleanup_nodes` 待清理表与 `relay_cleanup_reconcile` 一次性清理标记，两表纳入加密备份、清单、校验与恢复。升级前保存并验证加密备份。

旧 reuse 绑定被删除，对应入站保留为普通入站，配置不改。历史无绑定的方案入口按新建入口处理，撤除时删除。遗留资源页面移除；升级后启动自动清理脱离方案的入口、无方案引用的画布通道及其对象，并下发清理修订，须核对服务器「待清理」与实际监听。

旧代理客户端无需升级，保留入口的协议与订阅格式不变；被撤除的入口及连带删除对象停止提供服务。受管 Agent 的固定下载目录配置另见[固定目录地址方案](superpowers/specs/2026-11-15-agent-download-page-origin-design.md)，须通过 SSH 写配置、检查并重启引导程序。

## 已知限制

- 面板删除成功不代表远端立即停用；离线节点仍运行旧配置，失败节点须排障并等待清理成功。删除服务器不停止或卸载远端 Agent。
- 清理只删除已发布对象，不夹带草稿；其他手工引用仍须先处理。固定下载域名以后变更时须逐台修改配置。
- A1 的 Windows backup/server 全包曾受文件权限错误影响；A2 的 server 全包及最终删除接口窄测已通过。发布提交的完整 Linux 门禁与下述生产操作已有结果；这些记录不扩展为独立加密备份恢复往返或全面浏览器验收通过的结论。

## 发布验证

### 本地完整 Linux 门禁

- 发布提交 `a1b100b` 的本地 Linux 完整门禁：`GATE a1b100b PASS total=1220s`，真实核部分 332 秒通过。
- 发布前未提交快照 `b3f53a6` 的门禁曾发现 `internal/platform/recovery` 10 个旧版本归档恢复测试失败：测试回退链遗漏 0150，另有一个测试仍写 reuse 绑定。仅修正测试，生产恢复代码无缺陷；保留该轮失败事实。
- 实现由 Codex 完成：A1 撤除模型、A2 连带删除与入口归属、A3 修订接受、B 前端、C 文档；Claude 审查。审查后修正新撤除重置待清理退避、删除确认 blocker 类别前缀。

### 同 SHA GitHub check

[GitHub check 43738637967](https://github.com/jingniao/qui/actions/runs/43738637967) 与发布提交同 SHA。首次 `check (server)` 分片的 `TestReadsAndSubscriptionsAnswerWhileTheWriterIsBusy` 失败：持写事务时订阅内容用时 1.105 秒返回，超过 1 秒阈值。本版未改订阅读取路径，本地门禁同测试通过。按[发布手册 §4.2](release-runbook.md) 仅重跑失败任务一次，第 2 次运行通过（含 cores）。

### Release 与签名

[Release 43741437421](https://github.com/jingniao/qui/actions/runs/43741437421) 成功，发布时间 `2026-11-15T17:38:45Z`，tag `v1.5.8` → `a1b100b`，13 个资产。Agent 签名清单序号 `1005008`，使用官方公钥 `v1GzEWqRzysqZRFBryZStMCw4KeAkF3Vq2metTUlxTE=` 经 `go run ./cmd/release verify` 验证通过；amd64 摘要为 `adb2eafe01265b60c7ae196a242cbbe6aa97f8c1684640b88a0f6073cc7753b8`。

## 生产升级

- **面板网页更新**：17:41:35–17:42:22，德国面板由 1.5.7 升到 1.5.8，状态 `succeeded`；更新器自带更新前备份。
- **CC 恢复在线**：1.5.8 面板接受美国 CC 修订 87 的积压数据后，CC 于 17:42:17 连上并保持在线；此前每 30 秒连上约 1 秒即因 `traffic batch rejected` 断开。面板显示 CC 第 90–97 版修订均成功，CC 与 shlii 全部入站状态均为「已生效」。原计划「重新发布 CC」因无差异未执行，避免额外一次核心重启。shlii 第 98 版在启用 35001／35004 时已成功，未出现内存不足，因此未停用「测试-直连-SS」（:35001）。
- **方案撤除与节点清理**：一键撤除「测试中转-shlii-CC」和「清理-遗留-shlii-CC」，均返回 200 `removed:true,pendingNodes:[]`。随后 CC 修订 #100、#102 与 shlii 修订 #99、#101（`auto: relay cleanup`）均成功。中转方案清零，CC 内部监听 `trojan:35003`、`trojan:35004` 及 shlii 入口 :35004、:35000 全部删除，无节点待清理。升级后启动的一次性残留清理未删除任何对象：这些残留当时仍挂在方案上，随后由撤除删除。
- **CC 固定下载目录**：经 Owner 授权 SSH 操作，17:01 前在 CC 用 Go 客户端 UA 经 `https://qui.lluu.de/agent-catalogue/` 下载 1.5.7 清单与完整安装包，21184674 字节、耗时 3 秒、SHA256 一致（此前经 22550 为 101 秒）。配置备份为 `/etc/proxy-agent/config.yaml.bak-20261115-catalogue`；追加 `updates.catalogue` 与官方公钥，`--check-config` 返回 `configuration valid` 后重启 Agent，CC 自身代理短暂中断。17:49:32 经网页下发 Agent 1.5.8 升级，17:49:49 成功：`installed_version=1.5.8`、`installed_sequence=1005008`，摘要与清单一致，下载经 443 完成。详见[固定目录实际结果](superpowers/specs/2026-11-15-agent-download-page-origin-design.md#实际结果2026-11-15utc)。
- **2026-11-15 当次升级结束时的版本**：面板 1.5.8；德国 netcup Agent 1.5.7；美国 CC Agent 1.5.8；其余四台（香港 阿里国际*1、台湾 wap、zouter、美国 shlii.io）Agent 均为 1.5.5，按 Owner 要求暂不处理。
- **未完成：临时防火墙规则撤除**。德国防火墙在 14:38:30 添加的来源 `148.135.40.213/32` → TCP 22550 临时 ACCEPT 尚未删除。本机 SSH 到 `5.45.96.43` 被对端直接关闭（`Connection closed by 5.45.96.43 port 22`），需 Owner 恢复 SSH 访问或在 1Panel 中删除。规则仅运行时生效、未持久化，防火墙重载或重启后自动消失；CC 已不再需要它。

### shlii 补充升级（2026-11-16 UTC）

- Owner 指定把美国 shlii.io（`38.244.50.17`，Alpine 3.21.10 / OpenRC）升级到 1.5.8。升级前启动器已为 1.5.8，受管 Agent 实际进程仍为 1.5.5；本次通过网页升级实际进程，未使用仍只更新启动器的 `qui-update`。
- 在节点经 `https://qui.lluu.de/agent-catalogue/` 的 443 端口取得序号 `1005008` 的签名清单并完整下载 21184674 字节安装文件，HTTP 200，耗时 6.98 秒，SHA256 与本页官方发布摘要一致。预检下载文件随后删除。
- 01:56:39 为 `/etc/proxy-agent/config.yaml` 追加固定下载目录、系统 CA 与官方签名公钥；原配置备份为 `/etc/proxy-agent/config.yaml.bak-20261116T015639Z-catalogue443`。候选配置检查通过后替换并重启服务，01:56:42 控制连接恢复。配置只增加 `updates`，未改控制地址、节点身份、代理核心或业务配置。原配置 SHA256 为 `44f95d6b9c3d02743756cf8701df58c25b1c76876a546406a80ad2b6eca7ef73`，新配置为 `3ed7cabf18ba8d996edd083bbcc41561bb7d1dcd18ab7af10f68245eca2d311b`。
- 02:02 从网页向 shlii 单独下发 1.5.8 升级。02:02:31 新进程连接面板；02:05 实机读取 `/proc/<worker-pid>/exe --version` 为 `proxy-agent 1.5.8`，运行镜像 SHA256 为 `adb2eafe01265b60c7ae196a242cbbe6aa97f8c1684640b88a0f6073cc7753b8`，与签名清单一致，OpenRC 服务为 `started`。面板显示 Agent 1.5.8、在线、三个核心正常；运行与成功应用修订均为 #103，Xray 仍监听 TCP 35002。
- 02:02:26 切换期间记录了 `traffic queue read failed` 与 `telemetry queue read failed` 两条 ERROR，原日志没有底层错误信息，具体原因未确定；02:02:31 重连后至 02:05 核验未见新增同类错误，不记为零错误升级。SSH 跳板会话两次中断，重连后核实配置、运行进程与面板状态，无重复下发升级。
- 本次仅操作 shlii；其他节点与德国防火墙未修改。未运行项目测试任务，未提交或推送代码；`qui` 更新实际受管 Agent 的新功能仍待实现。
