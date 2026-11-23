# 节点存活判定与面板写库等待分离

日期：2026-11-23。状态：设计提案，等待 Owner 决定，尚未实施。

范围：审查后续计划「批次 4」，只交付设计，不包含代码实现。设计阶段未新增或运行测试、未连接生产、未开启子代理；本文随后随 v1.10.3 发布文档一并入库。证据按设计时工作树（HEAD `c910e89`）记录；行号可能随以后实施变化。

## 1. 结论与约束

推荐方向 A：新增进程内 `node.Liveness`，记录已认证控制流、最近接收时间和等待处理／写库／Ack 的在途消息。连接仍在且有在途消息时，展示、订阅和离线通知按存活解释；写库延迟另行记录 WARN 并在页面持续提示。控制资格、指标新鲜度和账本继续只读数据库事实。保留提交后 Ack，不改 Agent 协议，不增加迁移，最低兼容 v1.7.5。

这项设计解决“面板自己堵住了 Agent，随后把 Agent 判成离线”的误报。它不恢复写入吞吐，不证明代理核心健康，也不保证数据面可用。写库出错必须保留原始错误、返回失败、不发送成功 Ack；不得伪造 `last_heartbeat`、补造样本或提前确认遥测。

依据：`AGENTS.md:24` 起第 3 节要求不写静默兜底、失败完整暴露；`docs/superpowers/plans/2026-11-23-review-followups.md:89` 起批次 4 只授权设计。原计划中“内存与数据库较新者”需要扩展为在途消息判断，否则仍然误报。

## 2. 现状与误报机理

### 2.1 从接收到 Ack 的证据

| 环节 | 文件:行号 | 当前行为 |
| --- | --- | --- |
| 唯一主库连接 | `internal/platform/db/db.go:61` | `SetMaxOpenConns(1)`，主库读写都可能排队；WAL 不能让同一连接池同时拿出第二条连接 |
| 身份及 Hello | `internal/panel/node/control.go:225`、`:235`、`:239`、`:264` | 验证证书身份、读取并校验 Hello，再次核对身份；数据库检查也可能等待主库连接 |
| 活跃流登记 | `internal/panel/node/control.go:245` 至 `:262` | `connected` 在 mutex 下登记，重复连接拒绝，defer 依 entry 指针清理 |
| Hello 后写库 | `internal/panel/node/control.go:309`、`:358`、`:376`、`:420` | 握手序号与能力持久化先完成，再发 Welcome；首次连接同样有等待窗口 |
| 独立接收 | `internal/panel/node/control.go:427` 至 `:440` | 接收协程 `stream.Recv()` 后投递容量为 1 的 `incoming` |
| 串行业务循环 | `internal/panel/node/control.go:467`、`:482`、`:497`、`:506` | 主循环取得一条消息，查身份、序号，再处理业务；下次迭代前必须完成本轮 |
| 批次等业务写入 | `internal/panel/node/control.go:522`、`:565`、`:670` | 访问、流量、系统更新事件可在通用消息事务前等待，不只有心跳事务会卡住 Ack |
| 通用事务调用 | `internal/panel/node/control.go:681` | 同步调用 `Repository.record` |
| 事务与时钟 | `internal/panel/node/repository.go:292`、`:330`、`:360` | `BeginTx` 等待连接后，心跳 UPDATE 使用 `timestamp(time.Now())`，并非接收时间 |
| 同事务工作 | `internal/panel/node/repository.go:363`、`:366`、`:393`、`:396` | 指标、整机流量、已接受消息摘要与序号在事务中写入，最后 Commit |
| 提交后的确认 | `internal/panel/node/control.go:727` | record 成功后才发含 `AgentSequence` 的 Ack，发送仍可能阻塞／失败 |
| Agent 停等 | `internal/agent/control/control.go:966`、`:975`、`:979`、`:1040` | 分配序号、发送，循环接收命令并等待响应，校验 Ack 序号相等后才继续下一条 |

严格说，`last_heartbeat` 是提交前执行 UPDATE 时取的墙上时间，事务提交后才对外可见；它并非 Commit 返回瞬间的时间。设计保留其现有持久化含义。

面板的接收协程和业务处理并不在同一协程；每条消息的处理→提交→Ack 串行，Agent 的发送→匹配 Ack→下一次发送也串行。因此不能靠新增接收协程解决问题，当前已经有接收协程。

### 2.2 一次误报的时间线

1. 最后一条持久化心跳在 t=0。正常请求心跳间隔 15 秒（`internal/panel/node/control.go:37`）。
2. t=15，面板收到消息，但主库唯一连接被其他操作占用。阻塞可能发生在 `ValidateIdentity`，早于 `record` 的 `BeginTx`。
3. Agent 已发出这条带序号消息，等待 Ack。它不会在 t=30、45、60 再发心跳。
4. t>45，数据库心跳过期。即便内存记了 t=15，t>60 时这个时间也过期。两者取较新值只推迟误报。
5. `DisplayStatus` 进入等待恢复／离线，`ConfirmedOffline` 达到配置时限后确认离线，订阅剔除线路；通知 worker 恢复取得连接后也可能评估到离线。

45 秒定义在 `internal/panel/node/metrics.go:25`；展示与确认离线在 `internal/panel/node/availability.go:7`、`:66`。持续离线时限默认 3 分钟、0 分钟规则、状态优先级及通知启动宽限见 `docs/project-spec.md:719`、`:816`。压力复现必须覆盖超过配置离线时限，或在测试环境将规则设为 0；仅等待 46 秒不一定产生“确认离线”。

## 3. 可选方向比较

| 方向 | 能解决什么 | 代价与边界 | 结论 |
| --- | --- | --- | --- |
| A. 已认证连接＋最近接收＋在途消息 | Agent 因等待面板 Ack 无法继续发心跳时，仍有明确原因证明心跳静默来自处理未结束；可兼容旧 Agent | 必须独立检测断流、独立暴露延迟；前端及所有展示调用方需同步语义 | 推荐 |
| B. 只用 gRPC 连接／keepalive 判在线 | 写库排队不会影响 HTTP/2 PING 应答 | Agent 业务线程卡死但 gRPC 传输线程还能应答时，可永久“在线”；Hello 未完成、只建连接也可能误算；gRPC transport 不等于某节点业务流 | 不作为唯一依据；用作 A 的断流基础 |
| C. Agent 不等 Ack 继续发心跳 | 持续收到新心跳，可直接按接收时间判活 | 改变停等、顺序、重放、持久队列退役、背压与 Ack 分发；只有新 Agent 受益，旧 Agent 仍停等；面板还需避免接收积压 | 本批不选 |
| D. 拆心跳事务或写入优先级排队 | 减少正常负载时连接等待，提高 Ack 吞吐 | 已开始的事务不能抢占；外部锁、慢磁盘仍可超过 45 秒；拆事务影响序号、指标、整机流量游标的原子性 | 可作为另案性能治理，不能独自保证不误报 |

C 如以后推进，需要显式能力协商：旧 Agent 沿用停等，新 Agent 使用有界在途窗口和顺序确认，仍不得提前 Ack。旧面板与新 Agent 的协商及队列持久化规则要另写协议设计。直接并发发送会撞到当前 `last+1`、已接受摘要和响应序号合同（`repository.go:321` 至 `:328`、`:393`）。v1.7.5 实际代码也停等：`v1.7.5:internal/agent/control/control.go:913` 发送、`:918` 接收、`:978` 校验 Ack；不能只升级面板就假设旧 Agent 会继续发心跳。

D 如拆出“先更新在线、稍后写账本”，会把未完整接受的消息当成已完成，违背当前提交后 Ack 的合同；本设计不采用。优先队列要覆盖全部主库调用才有调度意义，无法绕过正在执行的事务，不在本次范围内。

### 3.1 A 与“不写兜底、失败要暴露”的关系

A 新增的是传输存活事实，持久化成功仍由数据库与 Ack 表示。存活不会刷新指标、不开放控制资格，也不把数据库错误改成成功。页面同时显示“控制连接存活；面板处理延迟；最后持久化心跳已过期”，日志在写库仍阻塞时发出，才能让失败可见。

如果只保留绿色在线、压掉离线通知却不提示写库问题，就会掩盖失败，应拒绝实施。无需把在途消息强行在某个时长后判成节点离线；10 秒是观测阈值，不是写入上限或静默降级开关。真正失败立即记录原始错误并断流；永久等待持续告警。Owner 应确认第 10 节的产品语义后再实施。

### 3.2 keepalive 能保证到什么程度

当前面板：`internal/platform/server/server.go:219` 设置 server `Time=30s, Timeout=10s`，enforcement `MinTime=10s`。当前 Agent：`internal/agent/control/control.go:236`、`:602` 设置 client `Time=20s, Timeout=10s`；v1.7.5 对应 `:236`、`:540` 同样配置。活跃双向流满足 keepalive 使用场景，不需要本次改参数。

gRPC 的 HTTP/2 transport 读写及 PING 处理独立于业务 SQL 等待。网络黑洞／进程死亡时，无 PING 应答会关闭 transport，传播到 `stream.Context().Done()` 与 `Recv` 错误。主动关流／收到 EOF 也必须立刻清除活跃标记。不能等业务循环从 SQL 返回才处理断流。

现有接收协程把错误送进 `incoming`（`control.go:430` 至 `:437`）；主循环卡在 SQL 时，错误可能尚未被消费；当前 map 的清理又只在 defer（`:256`）。因此仅查询 `connected` map 不足够。推荐同时：接收协程收到任何终止错误时直接标记会话断开并 cancel；每会话有独立 context watcher，在流 context 取消时更新 Liveness；读取快照时再次检查会话 context 已取消与否。所有更新都带 session token。

keepalive 提供有限延迟的故障探测，无法证明“此刻一定活着”。30+10 秒可作为无流量情况下的量级估计，不能承诺严格 40 秒：定时调度、传输活动、TCP 与部署中的代理都会影响实际时间。PING 也不能证明 Agent 业务循环／核心健康。中间代理是否终止 HTTP/2、网络黑洞实测上界均不确定，缺测试机实际网络拓扑与故障注入结果；验收需专门实测。业务死锁但连接应答、且面板确有在途消息时，A 仍可能持续判传输存活，必须依靠持续处理延迟信号暴露。

## 4. 推荐方案细节

### 4.1 `node.Liveness` 内存结构

面板启动时创建唯一实例，显式注入 Control、展示读取、RuntimeSnapshots、总览、门户、订阅 availability.Store 和通知来源。禁止每个 Repository 各建一份、禁止包级隐式单例；生产装配缺实例应报错，不能静默切回旧判断。

建议字段（概念结构，不是本次代码改动）：

| 层级 | 字段 | 含义 |
| --- | --- | --- |
| Liveness | `mu sync.RWMutex; entries map[nodeID]Entry` | 单实例，并发安全；Snapshot 批量复制，读端不持锁跑查询 |
| Entry | `SessionID`、`IdentityFingerprint`、`SessionContext` | 唯一会话代号、认证身份、取消信号；不输出完整证书或私钥 |
| Entry | `Connected`、`ConnectedAt`、`DisconnectedAt`、`LastReceivedAt` | 本机接收时钟；持续时间使用 Go 单调时钟，不信任 Agent Timestamp |
| Entry | `Established`、`DrainOnly` | 通过当前身份及 Hello 校验；替换 drain 会话只负责收尾，不给普通在线背书 |
| Entry | `InFlight map[MessageKey]Pending` | MessageKey 含序号及消息 ID；准确容纳接收通道中及正在处理的消息，不额外建立无界 payload 队列 |
| Pending | `Sequence`、`MessageID`、`Kind`、`ReceivedAt`、`Stage`、`StageStartedAt`、`CommittedAt` | 阶段为 `received/validating/processing/wait_db/in_tx/post_commit/ack_send`；保存元数据，不复制遥测内容 |
| Entry／Pending | `DelayActive`、`LastDelayLogAt`、`DelayStartedAt` | 延迟观测及限频；持续时间不用于强制判离线 |

当前停等协议下通常只有 1 条普通消息在途；map 用来表达实际接收和处理交叠，不增加协议窗口。Hello 序号 0 单独作握手 Pending，不混同普通序号去重。消息完成后删除；关闭会话清空在途信息；不积累消息历史。

### 4.2 更新点与锁规则

1. 初次验证身份后读取 Hello（`control.go:235`），通过纯内存 Hello 校验并获得独占会话登记后，记录 Hello 接收时间和握手在途。第二次身份校验仍执行，失败撤销会话；等待校验期间只显示“握手中／面板处理延迟”，不把未完成认证当在线。首次 pending 节点继续待登记。
2. 正常会话在 `Recv` 成功后、向 `incoming` 投递前记录 `LastReceivedAt` 与 Pending。先做 `validateMessage` 等不访问数据库的消息边界校验；失败不增加存活证据，记录拒绝并终止流。身份／序号等数据库校验仍按现有顺序执行，不移除事务内身份再核验。
3. 接收时间必须早于 `control.go:497` 的身份查询和 `:506` 的序号查询；放在 `record` 或 Commit 之后会漏掉最早的排队。已认证会话收到的消息在数据库校验尚未结束时是“待验证在途”，不表示接受成功。
4. 主循环更新阶段。`record` 内在 BeginTx 前标记 wait_db，拿到事务后标记 in_tx，Commit 返回后标记 committed；其他业务查询／写入同样归入阶段计时。外层 Received→Ack 计时必须覆盖全链路。
5. Ack 的 `stream.Send` 成功返回才移除 Pending；这仅表示服务器发出确认，不证明 Agent 已读到。若网络随后断开，按原协议重放去重。握手 Pending 在 Welcome 成功发出后移除。
6. 任意验证、写库、发送错误：记录原始 error、失败阶段、节点和序号，再撤销在途存活特权、取消会话并返回原有 gRPC 失败。当前 `control.go:689` 只给客户端通用文字，实施时必须在映射前记录原始错误，禁止只记 `message persistence failed`。
7. 所有状态变动在短锁内完成；SQL、日志、SSE 发布、回调、cancel 均在释放锁后执行。不得交叉持有 `Control.mu`、RuntimeSnapshots 的锁与 Liveness 锁。旧 token 的 Complete／Disconnect 不得修改新会话；同一 token 的重复关闭要幂等。

### 4.3 统一判断

新增显式的只读存活事实参数给展示／确认离线解释器，调用时冻结 `now` 和 Liveness 快照。保留原始数据库状态和时间，不用 `now` 覆写 LastHeartbeat 来“刷新在线”。

判断顺序：

1. 删除、禁用、维护、待登记优先级沿用现有规则；身份撤销后的会话不提供证据。`error/applying/rollback/degraded` 等业务状态不被连接标志强行改成正常。
2. 已建立、非 drain 的当前身份会话，context 未取消且有在途消息：连接存活，`ConfirmedOffline=false`。即使接收时间已超过 45 秒也成立；额外返回处理延迟状态和年龄，核心健康与指标年龄独立展示。
3. 没有在途消息时，用有效数据库心跳与该会话最近接收时间的较新者，按原有 45 秒和 offlineAfterSeconds 解释。只有连接而没有新消息不能无限延长在线。
4. 断流立即撤销第 2 条，不等待 Offline SQL 成功。保留该会话最后真实接收时间用于第 3 条的原有宽限，不把断开时间当新心跳，也不把长等待时间加到宽限；已经超过时限时可立即确认离线。0 分钟规则沿用显式断流／过期即离线语义；有显式断流证据时不被数据库的旧 online 状态反向遮住。
5. 无内存记录（面板重启等）时，只能使用数据库既有事实和原有启动宽限；这是无连接证据时的定义，不以伪造默认时间补足。坏时间沿用现有失效判定，不把解析错误当新心跳。

`repository.go:474` 至 `:478` 和 `runtime_snapshots.go:123` 至 `:127` 会把过期 online/degraded 改成 offline。实施应为展示额外携带原始持久化状态与存活事实，在展示适配层统一解释，避免先丢失 degraded 再凭空恢复 online。共享 scan 和控制侧既有解释维持数据库语义，不全局替换为 Liveness。

通知恢复同样使用统一解释后的“连接恢复”事实；`notification/evaluator.go:63` 的离线分支及其后只检查 online/degraded 的恢复分支应一起适配，避免停止新告警却不关闭已有离线事件。恢复文案指明连接恢复、写库延迟仍在，不声称数据已追平。

### 4.4 生命周期

| 事件 | 处理 |
| --- | --- |
| EOF、Recv 错误、context 取消、Ack／业务失败 | 独立路径立即标 disconnected，清空在途覆盖；保留最后接收时间到离线时限不再需要后删除。Offline SQL 仍尝试，失败完整记录（现状 `control.go:294` 至 `:300`） |
| 重连 | 分配新 SessionID，重新校验身份；旧关闭回调不能清理新记录。保持当前单活流拒绝规则，不能仅为了加快重连跳过原有序号恢复 |
| 节点禁用、吊销、立即撤销恢复 | 事务成功后立即失效内存身份并 cancel；应在审计／响应等后续可失败操作之前执行失效，防止已成功禁用却因后续错误漏清理。现有入口 `handler_http.go:262`、`:281`、`:326` |
| 节点删除 | 删除事务成功后失效并断开，物理／逻辑删除路径均覆盖。`handler_http.go:351`、`:383` 是入口，`deletion.go:240` 是逻辑删除提交点；不能只等下一次 ValidateIdentity 才清理 |
| 身份恢复、身份重置、全局身份换代 | 所有旧指纹记录删除，旧 token 不能再更新；事务内身份再核验照旧。状态重置现有 `state_reset.go:114` 至 `:118` 提交后 Disconnect；`recovery.go:89` 全局清空心跳时同时清空对应内存 |
| 维护 | 存活采集继续，展示及通知维持维护优先和已有事件保留规则 |
| replacement drain | 连接只承载最终遥测，不参与在线覆盖，防止退休身份的在途批次保活节点（现状 `control.go:267`、`:500`） |
| 面板停止、重启 | 取消 watcher／监测器，丢弃全部内存；启动为空，重新认证后建立。通知启动宽限保留 `worker.go:90`、`:93` 的配置 |

断流残留仅保存有用的最后接收元数据，在 `max(45s, 当前离线时限)` 后已不足以影响判断即可回收；配置离线时限变化时重新判断。禁用／删除／身份失效直接删除。写库阻塞中的节点管理操作尚未提交时，不能提前宣称禁用或删除成功。

## 5. 读取点清单与分流

本次实际使用以下 `git grep` 查找全部受版本控制文件，并补查 Go 的 `LastHeartbeat/freshHeartbeat` 与前端的 `lastHeartbeat/offlineAfterSeconds`，覆盖 SQL 字段经 scan 改名后的路径：

```text
git grep -n -E 'DisplayStatus|ConfirmedOffline|FreshSince|HeartbeatFreshness|last_heartbeat' -- '*.go'
git grep -n -E 'DisplayStatus|ConfirmedOffline|FreshSince|HeartbeatFreshness|last_heartbeat' -- ':!*.go'
git grep -n -E 'LastHeartbeat|freshHeartbeat' -- internal/panel/node/repository.go internal/panel/node/metrics.go internal/panel/node/deletion.go
git grep -n -E 'displayStatus|confirmedOffline|lastHeartbeat|offlineAfterSeconds' -- web/src
```

### 5.1 展示、订阅、通知：改用统一存活判断

以下路径仍读取原始数据库字段；改变的是解释过程，不重写数据库内容。

| 读取／调用位置 | 建议处理与理由 |
| --- | --- |
| `node/availability.go:7`、`:24`、`:66`（目录均为 `internal/panel/`） | DisplayStatus／ConfirmedOffline 接受同一冻结存活事实；保留业务状态优先级及原有时间边界 |
| `dashboard/repository.go:93`、`:122` | 总览在线／离线数量使用新判断，避免列表在线而总览离线 |
| `portal/service.go:203` | 门户直连节点使用新判断，保留现有权限过滤 |
| `portal/service.go:211`、`:227`、`:251` | 门户中转线路逐依赖节点使用新判断，线路取最差状态的规则不变 |
| `subscription/availability/view.go:49`、`:64`、`:85` | 在 readView 的同一生成过程合并 Liveness 批量快照，Evaluate 统一判断；禁用／维护优先、依赖材料错误继续报错，不能生成过程逐节点读取不同快照 |
| `notification/evaluator.go:63` 及恢复分支 | 离线事件及恢复事件都使用同一存活事实；不因面板 Pending 误开离线事件 |
| `notification/worker.go:78`、`:114`；`platform/server/server.go:931`、`:949` | 给 NodeSnapshot 增加存活事实，保留真实 LastHeartbeatRaw；worker 的 now 与快照采集时刻明确，不缓存 Connected 永久使用 |
| `node/repository.go:424`、`:435`、`:475` | 通用 Node 仍读数据库时间。展示 DTO 附加存活与原始状态；控制读取保持数据库准入，禁止把共享 scan 直接改成“有连接就在线” |
| `node/runtime_snapshots.go:95`、`:125`、`:185`、`:210`、`:233` | RuntimeNode／列表／SSE 带相同存活字段；过期心跳不再让展示预先丢失原始状态；指标新鲜度保持独立 |
| `node/deletion.go:260` | 删除预览 `online` 是展示提醒，改用统一存活事实；阻塞项与删除事务资格不变；`lastHeartbeat` 保持真实值 |

上述简称 `platform/server` 的完整目录为 `internal/platform/server`，其余均为 `internal/panel` 下的目录。

### 5.2 控制资格与观测数据：继续只用数据库

| 位置 | 保留理由 |
| --- | --- |
| `internal/panel/deployment/agent_system_updates.go:148` | 系统更新下发准入，需要持久化心跳、能力、修订匹配 |
| `internal/panel/deployment/coordination.go:162` | 核心重启等协调资格必须有已提交新鲜状态 |
| `internal/panel/node/health_checks.go:134` | 健康检查任务的节点／修订／能力准入维持 SQL |
| `internal/panel/node/latency.go:104`、`:136`、`:137` | 周期延迟探测选节点及插入前再次检查，均保留 FreshSince |
| `internal/panel/node/tcp_probes.go:206` | 手工 TCP 探测能力与新鲜度准入保留 |
| `internal/panel/node/tuning.go:142` | 调优写操作准入保留；现有 `>` 不改为 `>=` |
| `internal/panel/deployment/handler_topology.go:52`、`:90` | 虽是展示接口，`fresh_control/stale_control/revision_mismatch` 表示已提交控制观测及修订证据，不等于节点传输在线；维持数据库，页面应解释两者可不同 |
| `internal/panel/node/machine_traffic.go:279`、`:482` | 默认网卡及整机流量当前观测的新鲜度维持数据库；连接保活不让过期网卡报告变新 |
| `internal/panel/node/metrics.go:25`、`:28`、`:37`、`:45` | HeartbeatFreshness／FreshSince／观测时间筛选与 freshHeartbeat 不改；旧样本不得因存活覆盖被写成新鲜样本 |
| `internal/panel/node/repository.go:424`、`:475` 及 `runtime_snapshots.go:125` 的原始读取 | 原始时间保留用于指标／控制，只有展示适配层叠加新事实 |
| `internal/panel/notification/worker.go:90`、`:93` | HeartbeatFreshness 在此表示面板启动观察宽限，继续使用原值，不能取消 |

未遗漏的非读取项：`repository.go:360` 是心跳写入；`recovery.go:89` 是恢复时清空；`migrations/0152_baseline.sql:51` 是字段定义，都不改其存储合同。文档搜索命中是审查计划第 65、91 行，不是运行调用；前端 `canvasDisplayStatus` 是不同符号，下节说明。

### 5.3 前端链路与字段传递

仅改 Go 的 DisplayStatus 不足以解决页面误报。`web/src/modules/nodes/components/serverListModel.ts:11` 至 `:19` 自行按 45000 毫秒判断；`web/src/modules/nodes/runtimeSnapshots.ts:10`、`:99` 只投影现有字段；`web/src/modules/proxy/api.ts:203`、`:262` 的画布服务引用又调用 serverStatus。未来实施需让 NodeView、RuntimeNode、快照白名单、画布服务引用完整携带新事实，并使 serverStatus 使用统一语义。相关画布入口：`modules/proxy/components/relay/canvasLibrary.ts:70`、`pages/RelayCanvasPage.vue:1256`、`:1284`、`:1332`；代理列表使用 `pages/ProxyNodeListPage.vue:1318`。这些均为 `web/src/` 下路径。

建议展示字段为 `liveness: { evaluatedAt, validUntil, connected, sessionId, lastReceivedAt, inFlightCount, oldestInFlightAt, processingStage, processingDelayed, disconnectedAt }`，不输出消息内容。存活覆盖和延迟提示通过 RuntimeSnapshots/SSE 的既有 generation/revision 发布，读取列表也返回同一结构。状态变化、跨越 10 秒、断流、完成立即触发内存刷新标记，不能仅依赖 afterCommit。快照拼装用读池（已有装配 `internal/platform/server/server.go:175`），不再等待写连接；Liveness 回调不在接收协程同步访问 SQL。

浏览器不能无限相信旧 `connected=true`。建议服务端每次校准为覆盖证据签发 45 秒有效期，可见页已有 30 秒校准（`docs/project-spec.md:719`），新鲜响应续期；有效期过后转“面板存活状态未更新”，请求校准，不宣称节点已离线或写库成功。SSE 断连继续遵守冻结展示时钟的既有合同，显示面板连接提示；重连必须完整校准。前端时间与 evaluatedAt 的时差需校准，不用浏览器任意墙钟直接解释服务端单调时钟。

保留数据库心跳字段的前端用途：`NodeRestartPanel.vue:10`、`detail/ServerTuning.vue:342`、`modules/system/components/NodeVersionPanel.vue:1157`、`:1159`、`:1203` 的控制按钮／回执判定继续只用数据库；`ServerMetricsGrid.vue:46`、`serverCardValues.ts:22`、`modules/nodes/metrics.ts:25` 的指标年龄继续只用数据库。上述未带模块的组件位于 `web/src/modules/nodes/components/`。`detail/ServerOverview.vue:19`、`:292`、`serverListModel.ts:58`、`pages/NodeDetailPage.vue:41`、`NodeVersionPanel.vue:179` 的“最后心跳”照常显示真实持久化时间，另加处理延迟说明。

`web/scripts/canvas-library-selftest.mjs:82` 是画布测试导入替换，无线上判定；类型／展示适配后应纳入相关回归。前端不把有效期失效静默解释成正常，也不改变后端写操作准入。

### 5.4 搜索命中的现有测试

以下为直接匹配上述五个关键词的全部现有 Go 测试位置，均继续保留真实数据库夹具；设计实施时只调整展示调用适配，禁止把控制准入期望改成内存保活。本次未改、未运行。

| 文件（均为 `internal/` 下） | 行号 | 分类 |
| --- | --- | --- |
| `panel/dashboard/service_test.go` | 137、151 | 展示 |
| `panel/subscription/availability/view_test.go` | 39 | 订阅 |
| `platform/server/dashboard_test.go`、`portal_test.go` | 24；463 | 展示 |
| `panel/node/control_test.go` | 91 | 断流与数据库心跳回归 |
| `panel/deployment/baseline_test.go`、`relay_cleanup_test.go`、`relay_reachability_test.go` | 27；40、88；95 | 控制／清理 |
| `panel/node/latency_targets_test.go`、`latency_test.go` | 35；168、190、195 | 探测资格 |
| `panel/node/machine_period_test.go` | 101 | 整机观测 |
| `platform/server/k2b_http_test.go`、`latency_test.go`、`relay_cleanup_test.go`、`tcp_probes_test.go` | 42；129；17；37 | 控制／接口回归 |

## 6. 写库延迟必须独立暴露

### 6.1 阈值、计时与日志

建议阈值 10 秒，从面板接收消息到 Ack 发送完成计时；同时拆出 SQL 调用累计等待／执行耗时和 Commit→Ack 耗时，避免把慢发送冒称慢写库。BeginTx 等待外的 ValidateIdentity、LastSequence、批次处理同样需要计时。仅在函数返回时打印慢日志会错过永久阻塞，必须有不依赖数据库的监测器。

建议 Liveness 附带一个进程级定时观测循环，每秒扫 Pending 元数据，复制快照后记录日志。首个跨 10 秒立即 WARN `node_message_processing_delayed`；若阶段确实在 SQL，标 `bottleneck=db`，无法定位时写 `unknown`，不能编造“已确认 SQLite 锁”。恢复记录 INFO `node_message_processing_recovered`，任何失败立即 ERROR `node_message_processing_failed`，保留原始错误链。Hello 等待也纳入，便于区分握手阻塞。

日志字段：`node_id`、`session_id`、`agent_sequence`、`message_id`、`message_kind`、`received_at`、`elapsed_ms`、`stage`、`stage_elapsed_ms`、`db_elapsed_ms`、`commit_to_ack_ms`（尚未提交时明确未测）、`connected`、`last_persisted_heartbeat_age_ms`（仅使用已知快照，不为日志查库）、`db_in_use`、`db_wait_count`、`db_wait_duration_ms`（来自 `sql.DB.Stats()`）、`error`、`outcome`。不要记录 payload、证书内容、凭据、IP 或域名批次；Stats 是全池累计／瞬时事实，不能用它精确归因某条 SQL 等了多少。

限频：每节点／会话／同一在途阶段首次一次，持续期间每 60 秒一次，日志附当前年龄；阶段变更更新字段，不在每秒重复报警。失败不受慢日志限频，恢复每次延迟事件一次；重连不能因旧会话的 LastDelayLogAt 漏掉新事件。不延长消息截止时间、不取消既有超时、不设“超过上限算成功”。

### 6.2 页面与告警位置

必做建议：服务器卡片／列表、详情页连接状态旁以及总览出现橙色提示：“控制连接存活，面板处理已等待 67 秒；最后入库心跳 82 秒前。指标可能过期，控制操作仍按入库状态检查。”连接状态和处理状态分开，延迟不覆盖核心异常；资源数值沿用原有过期显示。即使收不到新 Ack，内存 Pending 年龄仍可增长。

可行接入点：`node/runtime_snapshots.go:185` 的 DTO、`:210`／`:233` 的读池刷新与 SSE；前端 `serverListModel.ts:6`、`detail/ServerOverview.vue:19`。总览统计读取 `dashboard/repository.go:93`，可附延迟节点数。页面提示与日志由内存直接产生，写库堵住时仍可观察；面板 HTTP 本身无法响应时，可依靠主机日志。

现有通知 worker 不适合承担唯一的实时延迟告警：`notification/worker.go:63`／`:70` 要读规则和事件状态，`:123` 保存状态，`:138` 插通知；装配 `platform/server/server.go:502` 使用主库节点仓库。主库堵住时这条链路也会堵住。现有日志采集告警可订阅 WARN 事件，部署环境有无日志告警设施不确定，缺运维配置。

本方案建议先交付日志＋页面提示；如 Owner 要 Telegram／Webhook 实时告警，需要单独授权一个不先写数据库的发送路径，定义无持久去重、失败日志、重启重报和渠道配置快照语义，不能声称现有 notificationWorker 能保证及时发送。普通离线通知仍保留持久状态与启动宽限；如需它在全局写阻塞期间也及时发出，涉及通知架构变更，另行决定。

## 7. 风险、行为差异与单进程假设

- 控制连接存活时，页面可能显示“连接存活／面板处理延迟”，而更新、重启、调优接口按数据库返回离线拒绝。这是有意保留的资格边界，提示需说清原因。
- 指标／核心健康来源仍是上次已提交报告。连接存活不能冒充核心恢复，不用内存接收时钟重新标记资源样本。
- 面板永久写入阻塞可导致在途覆盖长期存在；持续日志和页面告警是方案验收项。方案不能解决磁盘损坏或业务死锁。
- 断流探测有 keepalive 延迟；Recv 错误和 context watcher 必须绕过业务 SQL。取消后的 SQL 及时退出能力要由真实驱动验证，不以其及时返回作为内存清理前提。
- 身份替换与并发断流必须以 SessionID／指纹隔离，避免旧消息保活新身份。内存事实与数据库不是跨资源事务，展示快照允许短暂观察差异，控制仍在数据库事务内再核验。
- 门户现有装配 `platform/server/server.go:459` 使用主库节点仓库，普通通知同样使用主库；若要求写连接占用时门户也及时返回，需要把门户的纯展示读取改走既有读池，逐个确认不影响写操作。本设计将其列为实施配套，禁止偷偷给全部门户业务更换数据库。
- 假设单面板进程持有全部 Agent 控制流并服务展示／订阅请求，符合当前 Go 单体。读池与主库同进程共享一个 Liveness。多副本／独立订阅服务会看到不同内存事实，此设计不支持；以后引入时必须设计明确的跨进程存活来源，不能用各副本数据库时间猜测远端流存活。
- 本次未进行 Linux、黑洞、v1.7.5 二进制联调，故障探测实测值不确定。上线前不得将提案阈值写成已验证指标。

## 8. 测试清单与 Linux 测试机实测

本节是后续实施的验收要求。本次只写设计，没有新增测试方法、测试实体或 mock，没有运行任何测试任务。未来新增／运行需获得 Owner 明确授权，遵守最窄范围、Go `-p 1`，不先跑全仓门禁。

### 8.1 单元与窄回归

- Liveness：收消息、阶段切换、Commit 后 Ack 前仍在途、Ack 后清除、失败清除、断流立即失效、旧 token 回调不影响新 token；并发 Snapshot／Receive／Disconnect 的 race 检查。
- 判断表：无内存、较新接收、接收也已过期但仍在途、只连无消息、非心跳业务消息待 Ack、Hello 等待、drain、维护、禁用、删除、pending、error、degraded、坏时间、未来时间、45 秒边界、0／3 分钟离线规则。
- 写连接被真实占用超过 45 秒：同一 `sql.DB` 借出唯一 Conn 或保持真实事务，使真实 Agent 会话发送一条消息后停等；先有正常持久化心跳。收到消息 10 秒后、写库仍阻塞时必须看到 WARN；超过 45 秒仍无离线误报；离线规则设 0，另测超过 3 分钟规则。验证既有接收协程没有第二条心跳也能成立。
- 阻塞点分别覆盖 ValidateIdentity／LastSequence、BeginTx 等待、事务中写入、Commit 后 Ack 发送，不能只测试 record 内部。阶段日志应区分数据库和发送。
- 阻塞期间的显示、总览、门户、订阅与离线事件 Evaluate 一致；订阅包含在途存活线路，禁用／维护线路仍排除；控制 SQL 不因 Liveness 开放。通知 worker 若因主库阻塞尚未运行，要明确记录“未执行”，不能把没有通知当作判定正确；释放后再验证事件状态未误开。
- 写库失败不 Ack、不推进接受序号、不产生成功通知，原始错误可见；解除占用后正常 Commit→Ack，延迟恢复日志仅一次，账本去重和整机游标无重复／跳号，过期指标样本不被伪造为新鲜。
- 写库仍卡住时关闭控制流／断网，Liveness 必须自行撤销在途覆盖。离线按最后真实接收时间及原规则确认，不依赖 Offline SQL 成功；重连、面板重启、删除／禁用／身份重置并发均不残留保活。
- 前端：NodeView／RuntimeNode／画布投影不丢字段；SSE／30 秒校准、45 秒证据有效期、断开冻结和重连校准；指标保持过期，控制按钮保持数据库资格。已有核心异常不被延迟提示抹去。
- 通知：启动观察宽限、维护保留事件、延迟期间连接恢复关闭旧离线事件、实际断流每轮一次离线／恢复。只跑与这些行为相关的现有及获授权新增用例。

### 8.2 Linux 测试机步骤（未来执行）

1. 使用隔离 Linux 面板和至少一个 Agent，禁用生产通知渠道，建立专用测试规则／订阅并备份测试库；记录提交 SHA、Go／SQLite 驱动版本、面板与 Agent 版本、实际 gRPC 网络拓扑、时区和规则时限。准备当前 Agent 与 v1.7.5 Agent 各一套，使用真实 mTLS 接入、真实非敏感遥测。
2. 获授权后实现／运行一个最窄的真实连接池回归用例，在同进程里通过 `app.Database.Conn(ctx)` 或真实事务占用唯一连接 70 秒。设离线规则为 0，等 Agent 先完成一轮正常心跳，再占连接；测试协调用通道确认下一条消息已经 Recv，不用任意 sleep 猜测。该夹具只存在于测试，不增加生产管理接口或 mock 成功路径。
3. 占用开始、收到消息、10 秒 WARN、超过 45 秒、释放连接、Commit、Ack、恢复日志分别记录时间。测试过程每次等待保持可观察；采集日志和真实 DB.Stats；读取 RuntimeNode、总览、门户与专用订阅，确认消息没有完成但连接仍存活、提示持续存在、最后数据库心跳保持原值、控制资格仍拒绝。若某接口仍排队，记录具体链路，不能报告此项通过。
4. 释放同进程占用后确认消息完整提交与 Ack，重放去重正常；用读池读取最后心跳、接受序号、样本与整机流量。再用配置 3 分钟规则保持超过 3 分钟重复关键场景，证明不只是延迟了离线。
5. 保持主库阻塞，再停止测试 Agent，或只对测试机 Agent→面板实际 gRPC 端口的专用流量施加 DROP，保留独立管理通道。不得套用生产地址；记录临时防火墙规则及删除命令。主动关闭与网络黑洞分别测试，测量 context／Recv 关闭、内存失效、页面状态和离线判定时间。最后真实接收已过期时撤销覆盖后应按规则确认离线；释放数据库后验证普通离线通知最终落库一次。
6. 清除精确的临时规则，恢复 Agent，验证重连新会话、单次恢复通知、延迟状态清除；再测面板重启、节点禁用／删除／身份替换的清理及旧 session 回调。
7. 在 v1.7.5 Agent 上重复停等阻塞和断流两项，验证无协议协商新增、Welcome／Ack／重放不变。保留脱敏日志、时间线、接口状态和窄用例输出作为验收材料；测试后恢复测试规则并清理测试夹具。

外部 `sqlite3 BEGIN IMMEDIATE` 只能作为另一个数据库锁测试：驱动 busy_timeout 可能先报错，无法稳定模拟同池唯一连接被占用超过 45 秒。不得用它替代第 2 步，也不人为修改生产超时让实验“通过”。当前尚无获授权新增的测试用例名和 Linux 测试机路径，不确定最终命令；缺实施后的用例与测试机信息，因此不提供虚构可执行的测试名称。

## 9. 兼容性与后续实施范围

- 无迁移，不新增持久表，不改变 last_heartbeat、节点序号、accepted digest、流量退役、账本或整机游标的含义。
- 不改 protobuf／Agent 协议，不提前 Ack，不要求 Agent 升级。最低兼容 v1.7.5；该版本 keepalive 与停等代码已核对，真实二进制故障联调尚未做。
- Web/API 只增加展示字段，并同步本仓前端与 SSE 白名单；不把新字段当写操作凭据。旧网页不识别新字段仍可能沿用旧离线显示，需要随面板发布一起更新静态资源。
- 计划实施触及 node 存活与展示适配、server 依赖装配、dashboard／portal／subscription／notification 读取、前端统一状态与页面提示。身份失效后的清理属于本功能生命周期；写入队列改造、事务拆分、多副本、独立外部告警发送属于另案。
- 实施获批准后，应同步 `docs/project-spec.md` 第 27.11 节展示规则与通知章节、架构与相关运维说明；本次按要求只写本文，未改这些文件。

## 10. 需要 Owner 决定

1. 是否接受 A 的语义：已认证控制流仍在且存在待 Ack 消息时保留存活，指标和控制资格仍可能过期；页面必须同时显示处理延迟，不以任意超时将面板故障算成 Agent 离线。
2. 是否采用建议参数：10 秒首次延迟 WARN／页面提示、持续每 60 秒日志、1 秒扫描、浏览器 45 秒证据有效期＋现有 30 秒校准。keepalive 参数建议保持原值，待黑洞实测后再评估。
3. 告警交付是否接受“日志＋页面提示”，以及是否已有日志告警设施；如需写库阻塞期间实时 Telegram／Webhook，需另行授权不依赖通知入库的发送与去重设计。
4. 是否授权后续实施及第 8 节最窄单元、回归与 Linux 实测；本次没有此授权。并确认门户纯展示读取转既有读池、删除／身份失效清理、前端字段贯通作为该功能必要配套，不扩展到其他业务重构。

本次交付检查：文件存在、非空和文档内容检查在写入后执行；代码测试、构建与 Linux 实测均未运行。本文建议均为设计选择，尚无实施／性能／故障探测达标声明。
