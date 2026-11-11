# 轻量多节点代理控制平台项目说明书

面向 1～10 名内部用户，自托管，用于自有或明确授权网络中的代理接入、节点管理、分流与域名级访问控制。本文规定当前产品行为、字段语义、安全边界与接口；源码为准，变更时同步修正文档。

## 当前状态

最新正式版 **v1.5.1**（2026-11-11，tag → `f8bca8a`，无迁移，迁移头仍 **0141**），v1.5.0 的补丁版：修复受管（已启用网页升级）Agent 发布显式核心入站报 `apply_failed`、1.5.0 新装未真正启用网页升级、规则集页面保存清空关键字、监控 CPU／网速大多为空、节点列表把从未生效的入站显示为「已生效」（新增 `GET /api/v1/nodes/{id}/inbound-states`）、Windows 国旗显示；安装命令对话框可勾选「启用网页升级」（`node-install.sh --web-upgrade`，默认不启用）且不再编辑面板地址，用户详情「可用节点」只列订阅端点，监控图表改用 Chart.js。同一提交的本地 Linux 完整门禁（`GATE f8bca8a PASS total=3139s`，无 DATA RACE）与 GitHub `check`（run 42996659863，含 cores，第 2 次运行通过）均通过，Release 于 12:28:18Z 发布，签名核对通过，见[发布说明](docs/release-notes-1.5.1.md)与[受管 Agent 实机验收](docs/v1.5.1-managed-agent-verification.md)；发布后经网页升级把 Owner 自己的面板 qui.lluu.de 与其「德国 netcup」Agent 升到 1.5.1，Hysteria2（sing-box）入站发布成功；**生产按 Owner 要求未升级**（面板与三台 Agent 仍为 1.4.6）；已知限制：「版本与更新」页国旗仍显示字母（等宽字体栈未加国旗字体）。上一正式版 v1.5.0（2026-11-11，tag → `d823018`）迁移头 **0141**（0138–0141），内容为 Agent 网页一键升级（内置官方公钥、面板托管只读签名目录、失败自动回滚；节点须先进入受管模式）、多公网地址、通知系统（Telegram 与 Webhook 通用／企业微信／钉钉／飞书）、发布状态可见、去掉用户协议控制、按域名查找／申请证书、Hysteria2 端口跳跃、用途组 @all、规则集外部来源与关键字、服务器调优完善；**从 ≤1.4.7 升级 Agent 需运行两次 `qui-update --component agent`**（第二次启用网页升级），新装由 `node-install.sh` 自动启用。同一提交的本地 Linux 完整门禁（`GATE d823018 PASS total=3106s`，无 DATA RACE）与 GitHub `check`（run 42959338002，含 cores）均通过，Release 于 2026-11-11 05:36:19Z 发布，见[发布说明](docs/release-notes-1.5.0.md)，验收见各[验收报告](docs/README.md)；**生产尚未升级**（面板与三台 Agent 仍为 1.4.6）；**已知问题**（已由 v1.5.1 修复）：受管（已启用网页升级）的 1.5.0 Agent 无法发布显式核心（如 sing-box）的入站，发布报 `apply_failed`，见[v1.5.1 规划](docs/v1.5.1-plan.md)第 2 项；Cloudflare／Let's Encrypt staging 与真实企业微信／钉钉／飞书机器人未实机验证。上一正式版 v1.4.7（2026-11-09，tag → `25b95e3`）迁移头 0137，内容为保存节点组后凭据自动下发、节点客户端连接地址（Agent 自动上报公网地址＋可选域名 `connectHost`，0135）、TLS 入站证书来源（0136）与面板经 Cloudflare DNS-01 自动申请和续期证书（0137），同一提交的本地 Linux 完整门禁（`GATE 25b95e3 PASS total=2976s`，无 DATA RACE）与 GitHub `check`（run 42627577140）均通过，Release 于 14:54:28Z 发布，见[发布说明](docs/release-notes-1.4.7.md)；**生产尚未升级**（截至 2026-11-09 15:55 UTC 面板与 Agent 仍为 1.4.6），Cloudflare／Let's Encrypt staging 实机验收待做。再上一版 v1.4.6（2026-11-09，tag → `620c6d7`）迁移头 0134，内容为内核版本、地址与摘要不再手抄：选一个正式版本，面板就按这台服务器的架构与 C 库填好官方资产地址与厂商公布的归档摘要（「添加内核」「升级内核」对话框与「核心更新」卡片，`GET /api/v1/core-releases`，面板从不自算摘要、批量按每台解析、手输版本清空旧地址与摘要），见[发布说明](docs/release-notes-1.4.6.md)；生产已于 2026-11-09 04:32–04:34 UTC 升级：先面板 us2 `1.4.5` → `1.4.6`（04:32Z，`NRestarts=0`，迁移头仍 0134），再按 uk、Alpine、us2 逐台把 Agent 从 `1.4.4` 升到 `1.4.6`（04:32:56／04:33:26／04:33:50 UTC）；四次 `qui-update` 均成功，三台在线，UK/Alpine/US 修订仍为 295/296/291（无新修订、无回滚），各主机无残留 `rollback.*`，面板配置未改写（sha256 仍 `e57bc1d9…`），升级后 0 WARN／0 ERROR，恢复点为校验过的加密面板备份 `FlK52Fyj…` 与更新器备份 `pre-update-1.4.5-20261109-123223.db`；升级后 SOCKS5、HTTP、VLESS、Shadowsocks、Trojan 五个入口抽样均返回 200，阻断与错误密码拒绝仍生效。v1.4.5（2026-11-09，tag → `88fe266`，同为 0134）修复安装命令对话框未采用「节点安装连接地址」里保存的面板与 Agent 地址端口，并让纯前端补丁改走短门禁；生产当次只把面板升到 `1.4.5`，三台 Agent 到 v1.4.6 才一起升级。v1.4.4（2026-11-08，tag → `67c9fb0`）迁移头 0134；v1.4.3（2026-11-08，tag → `22fb794`）同为 0134，v1.4.2（2026-11-08，tag → `a588c17`）同为 0134，v1.4.1（2026-11-08，tag → `0c1b061`）同为 0134，v1.4.0（2026-11-07，tag → `6a5a821`）同为 0134，v1.3.1（2026-11-03，tag → `ebe8301`）为 0128。v1.4.4 无迁移，内容为面板可在网页里更新自己（「系统管理 → 系统设置 → 系统信息 → 面板更新」，Owner 专用；面板只写请求文件，由 root 的 `proxy-panel-update.service` 调用 `qui-update --component panel`，沿用备份、健康检查与失败回滚）、节点安装器在 CDN 或终止 TLS 的反向代理后固定面板证书、侧栏展开分栏点标题总是收起、发布流程按改动范围检查，以及发布前修好的面板回滚回归，见[发布说明](docs/release-notes-1.4.4.md)；生产已于 2026-11-08 17:55–18:01 UTC 升级到 v1.4.4：先面板 us2 `1.4.3` → `1.4.4`（17:56:23 UTC 起服务，10 秒内无重启，迁移头仍 0134），随后按 uk、Alpine、us2 逐台把 Agent 从 `1.4.3` 升到 `1.4.4`（17:57:18／17:59:27／18:00:41）；四次 `qui-update` 均成功，三台在线，UK/Alpine/US 修订仍为 295/296/291（无新修订、无回滚），各主机无残留 `rollback.*`，面板与各 Agent 升级后 0 WARN／0 ERROR，`/etc/proxy-panel/config.yaml` 未改写（sha256 仍 `e57bc1d9…`），恢复点为校验过的加密面板备份 `moJ3TMSm…` 与更新器备份 `pre-update-1.4.3-20261109-015620.db`；升级后 SOCKS5、HTTP、VLESS、Shadowsocks、Trojan 五个入口抽样均返回 200，阻断与错误密码拒绝仍生效。v1.4.3 无迁移，内容为「节点安装连接地址」增加 Agent gRPC 地址与端口、`node-install.sh` 新增 `--grpc-host`、三个安装命令对话框共用该默认值，门禁／CI 与发布见[发布说明](docs/release-notes-1.4.3.md)；v1.4.2 无迁移，内容为「节点安装连接地址」系统设置、三个安装命令对话框共用该默认值及 `node-install.sh` 的失败诊断与登记前 gRPC 预检，门禁／CI 见[发布说明](docs/release-notes-1.4.2.md)；v1.4.1 无迁移，内容为一键面板安装器、新装默认端口 22550／18880、节点编辑器与用户列表交互修正及拒绝证书日志，见其[发布说明](docs/release-notes-1.4.1.md)。生产已于 2026-11-08 07:04–07:06 UTC 升级到 v1.4.2：先面板 us2 `1.4.1` → `1.4.2`（07:04:15 UTC 起服务），随后按 uk、Alpine、us2 逐台把 Agent 从 `1.4.1` 升到 `1.4.2`（07:04:57／07:05:31／07:05:59）；四次 `qui-update` 均成功，三台在线，UK/Alpine/US 修订仍为 295/296/291（无新修订、无回滚），面板升级后 0 WARN／0 ERROR，`/etc/proxy-panel/config.yaml` 未改写（sha256 仍 `e57bc1d9…`）；恢复点为校验过的加密面板备份 `KT7hNrOz…` 与更新器备份 `pre-update-1.4.1-20261108-150413.db`；升级后真实代理抽样 SOCKS5、HTTP、VLESS、Shadowsocks、Trojan 五个入口均返回 200。生产已于 2026-11-08 13:25–13:29 UTC 升级到 v1.4.3：先面板 us2 `1.4.2` → `1.4.3`（13:26:02 UTC 起服务，10 秒内无重启，迁移头仍 0134），随后按 uk、Alpine、us2 逐台把 Agent 从 `1.4.2` 升到 `1.4.3`（13:26:27／13:27:36／13:28:27）；四次 `qui-update` 均成功，三台在线，UK/Alpine/US 修订仍为 295/296/291（无新修订、无回滚），各主机无残留 `rollback.*`，面板升级后 0 WARN／0 ERROR，`/etc/proxy-panel/config.yaml` 未改写（sha256 仍 `e57bc1d9…`）；恢复点为校验过的加密面板备份 `yO25OluW…` 与更新器备份 `pre-update-1.4.2-20261108-212559.db`；升级后真实代理抽样 SOCKS5、HTTP、VLESS、Shadowsocks、Trojan 五个入口均返回 200，阻断与错误密码拒绝仍生效。此前的 v1.4.1 于 2026-11-08 约 02:47–02:56 UTC 升级到生产： 先面板 us2 `1.4.0` → `1.4.1`，随后按 uk、Alpine、us2 逐台把 Agent 从 `1.4.0` 升到 `1.4.1`；四次 `qui-update` 均成功，三台在线，UK/Alpine/US 修订仍为 295/296/291（无新修订、无回滚），面板服务自 02:49:22 UTC 起未重启、升级后 0 WARN／0 ERROR。`/etc/proxy-panel/config.yaml` 未改写（仍 `[::]:443`／`[::]:9443`，新端口只用于新装）；恢复点为校验过的加密面板备份 `GenwwAYh…` 与更新器备份 `pre-update-1.4.0-20261108-104919.db`。Alpine 使用 LXC/OpenRC，其余使用 systemd；v1.4.0 的升级过程与 0134 隔离主机测试见其[发布说明](docs/release-notes-1.4.0.md)。v1.4.2 的发布、门禁、GitHub `check` 与生产升级见其[发布说明](docs/release-notes-1.4.2.md)。

生产已完成升级、mTLS 重连、配置回滚、失败批次回滚和同版本加密备份恢复。rc.2 的字体规范、节点组卡片／编辑弹窗及一天默认会话已部署；慢加载修复生产有缓存导航抽样 216–565ms，版本页排队降至个位数毫秒，见[收尾报告](docs/v1.4-rc2-closeout.md)与[耗时记录](docs/page-loading-verification.md)。

已完成的数据面验收：UK→Alpine→US GOST TCP/UDP 两轮 12/12、受控失败回滚与中间 Agent 重启恢复；自建 VLESS TCP／Reality TCP Vision／xhttp-auto 的 21 次有效请求、11 次错误认证及失效公钥拒绝、密钥轮换与历史回滚；画布 P2（UK→US）、P3（Alpine→UK→US）和关联规则 P4（UK→Alpine 隧道→US Reality TCP/xhttp）的 12 次请求、4 次错误认证。各轮临时资源已清理，相关证据见[三节点](docs/three-hop-host-verification.md)、[协议客户端](docs/vless-reality-client-verification.md)及[画布／P4](docs/canvas-p2-p3-p4-client-verification.md)报告。“UK → Alpine NAT”隧道（公网 113.29.231.218:21411，映射 21411–21419 TCP/UDP 已实测）已由 Owner 于 2026-11-07 16:18 UTC 删除，现为 pending_delete，仍由 Alpine 监听 21411 直到 Owner 发布移除。

最终补充验收收尾（2026-11-06 UTC）：外部导入生产 Token 订阅有效请求 9/9、错误凭据拒绝 3/3，隔离实机的旧／新加密备份恢复、0128→0133 升级、匹配旧库降级、重复导入和双会话冲突均通过；8 组真实浏览器场景通过。最终源码快照 `0506a085ad5fd783` 的全仓库 check／race 与实核门禁均退出 0（check 2042 秒、合计 2706 秒），无 DATA RACE 告警。2 小时混合流量完成 4028 次请求，4024 成功、4 次瞬时失败，后续恢复，根因未定，不能记为全程零错误通过。画布环路规划前置检查、可选预览用户和删除确认层级修复已随离线 rc.3 部署至生产面板；临时资源清理、原账号回归 10/10、原资源与数据库检查通过。完整证据和限制见[最终验收记录](docs/final-v1.4-acceptance.md)。

Owner于2026-11-06明确决定本轮跳过原生arm64主机验收，早前qemu-user只作为模拟证据，不声称原生实机通过。首次历史隧道应用失败首因因材料缺失无法确定，保留失败／自动回滚／后续成功事实。正式版证据及限制见[验收矩阵](docs/v1-acceptance.md) §12；v1.4 使用独立验收结论，范围见[v1.4 计划](docs/v1.4-plan.md)及[发布说明](docs/release-notes-1.4.0.md)。

入口：[文档导航](docs/README.md)、[架构](docs/architecture.md)、[部署与开发门禁](docs/deployment.md)、[运维](docs/operations.md)、[协议矩阵](docs/protocol-support-matrix.md)、[发布流程手册](docs/release-runbook.md)。版本历史集中在[变更记录](docs/changelog.md)，完成的计划与报告在[归档](docs/archive/README.md)。

## 1. 文档目的

为产品、前后端、Agent、数据库、核心适配、测试与运维提供统一规范。需求变化须同步本文件与相关文档。

# 2. 项目定位

轻量多节点代理网络控制平台，提供用户访问管理、节点组订阅、分流与阻断、域名记录、协议转换中转和链式代理。不以商业计费、支付、多租户或大规模运营为目标。

保持部署简单、资源占用低、配置清晰、节点稳定，便于增加节点和协议；中央失联节点继续运行，错误配置自动回滚。可按域名控制流量经其他节点转发，只审计域名元数据。

# 3. 项目核心原则

## 3.1 轻量优先

中央端为 Go 单程序，嵌入前端并使用 SQLite。不强制引入 Kubernetes、Redis、Kafka、RabbitMQ、Elasticsearch、ClickHouse、Prometheus、独立配置中心/任务队列、微服务或服务网格。

## 3.2 模块化单体

用户、节点、订阅、代理、分流、发布、访问记录与系统模块边界清晰，通过 Go 接口或应用服务调用，不通过内部 HTTP 或消息队列通信。

## 3.3 控制面与数据面分离

中央面板负责用户、节点、策略、配置发布、状态、域名记录与订阅；用户代理流量不经过面板，由节点核心处理。GOST 只做转发与中继，WireGuard 按核心协议能力使用。

## 3.4 入站与出站解耦

接入与下一跳协议独立。例如用户以 VLESS 接入 A，A 终止会话后按规则以 Trojan 连接 B，再由 B Direct 出网。

## 3.5 节点独立运行

中央失联时节点保留最后成功配置、继续代理、本地缓存状态和记录，重连后补传。

## 3.6 最小化审计

只记录用户、时间、入口、域名、命中规则、允许/阻断与配置出口。不记录网页正文/标题、完整 HTTPS 路径、表单、密码、Cookie、聊天、上传下载内容或数据包正文；不做 HTTPS 中间人解密。出口证据口径见第 20 节。

# 4. 系统组成

## 4.1 Web 管理后台

Vue 3、TypeScript、Vite、Vue Router、轻量状态管理与 Fetch 请求封装。负责状态、用户凭据、节点组、入站/出站、出口组/代理链、策略、访问记录、发布/回滚与系统设置。构建后嵌入中央程序，生产环境不运行 Node.js 服务。

前端排版遵循[设计规范](docs/design-system.md)第 3 节：系统无衬线与等宽字体栈统一维护于 Tailwind 配置；常规字号仅 12/14/16/18/20px，辅助信息不得小于 12px，正文与标准控件 14px，页面标题 20px，弹窗标题 18px。统计数值可用 24/30px，错误页编号可用 36px。默认正常字距，保留系统原生字体平滑，不以缩小文字解决布局拥挤。

分区标题显式使用 16px / 600，普通表单、标准按钮和筛选不得局部覆盖为 12px；实体卡片主名称统一 18px / 700。紧凑下拉触发器、辅助说明及配置代码区域可为 12px，侧栏按设计规范保留普通 400／选中 600 与原分类字距。

## 4.2 中央控制服务

Go 模块化单体，提供 REST、SSE 与 gRPC，使用 SQLite。负责管理员认证、用户隐藏接入身份与节点组授权、订阅、代理资源与策略、配置版本和发布、访问记录、流量、备份恢复。

## 4.3 Node Agent

Go 单文件程序，由 systemd 或 Alpine/OpenRC（含 LXC）管理；通过 gRPC 双向流与 mTLS 主动连接。负责注册/证书、能力与状态上报、配置编译/原生校验/应用/失败回滚、核心管理、流量及域名采集、本地持久队列和断线补传。

## 4.4 代理核心

`cores: [{name,path}]` 列启用核心，首项为兼容主核心（Xray 或 sing-box），旧标量映射单核心。生产节点均用 MultiRuntime 与 `runtime/<core>/`；节点绑定的认证清单 `cores.json.enc` 与 YAML 合并并持久覆盖禁用状态。安装/移除/回滚见第 22.2 节；Agent 不改安装器二进制/记录，职责见第 30 节。

Xray、sing-box 承载用户接入、身份、分流、Direct/Block、代理出站、中转/链与统计；协议按能力上报（第 11–13 节）。GOST 固定官方 3.3.2、仅 linux/amd64，只承载 forward（TCP/UDP）与加密中继，不承载用户身份、凭据、订阅、额度、门控、源观测、域名记录、出站资源、策略或出口组；转发按自身目标直连，仅要求启用的 direct 默认出站。

`desiredstate.Validate` 与 `ValidateCoreProtocol` 拒绝 GOST 的非 forward 入站；空 core 随主核心，是否可安装由 Hello.gost_core_supported 决定，不推断版本号。GOST 不可排 cores 首项，安装参数同样限制；必须保留至少一个 Xray/sing-box（last_core）。隧道管理要求各跳已启用 GOST，两节点契约至少 3，三节点至少 4；接收跳须公网地址或连接地址覆盖，见第 14 节。

# 5. 总体架构

管理员浏览器 → HTTPS/SSE → proxy-panel（嵌入 Web、REST、SQLite、管理与发布）→ gRPC/mTLS → 多台 Node Agent → 各节点核心（接入、中转、链与出口）。控制面只运行一个中央程序。

建议监听 22550（Web/REST/SSE/订阅）、18880（Agent gRPC/mTLS），两者均可修改；已有安装保持原端口。用户数据不经过中央端。

# 6. 用户角色

后台有 Owner 与 Viewer 两种角色。

## 6.1 Owner

Owner 拥有全部权限，包括：

管理后台管理员；创建、修改和删除用户；管理用户凭据、共享订阅与门户登录；管理节点；管理节点组；管理用户节点组授权；管理入站和出站；管理出口组和代理链；管理分流规则；管理网站访问控制；查看访问记录；发布配置；回滚配置；管理 Agent 和代理核心版本；备份与恢复；修改系统设置。

## 6.2 Viewer

Viewer 只允许查看：

总览；节点状态；节点基础信息；流量统计。

Viewer 默认不能：

修改任何配置；查看敏感凭据；查看详细访问记录；发布或回滚配置；管理用户和管理员。

底层权限设计应保留细粒度权限点，以便后期增加 Operator 或 Auditor，页面不暴露复杂角色配置。

## 6.3 终端用户与门户

代理用户使用自己的用户名与 Owner 设置的密码登录 `/portal/login`；门户提供账号状态、订阅复制与二维码、授权节点、流量和修改密码。门户会话与管理员的表、Cookie、CSRF 状态和路由守卫独立，互相不能访问。禁用、到期或超额用户在登录开关开启时仍可登录查看原因，但不能取得订阅内容。Owner 的门户预览为只读，不签发 Token、不写数据；Viewer 使用后台只读视图。

# 7. 网站功能模块

网站沿用 Owner 的布局，当前入口如下（设备与订阅旧路由跳转到用户管理）：

```text
系统总览

用户管理（用户详情内管理订阅、登录与凭据）

服务器
├─ 服务器列表
├─ Agent 管理
├─ 代理核心
├─ 节点组
└─ 节点拓扑

节点配置
├─ 节点列表（入站、出站、出口组；添加中转）
├─ 节点分组
├─ 隧道管理
│  ├─ 隧道
│  └─ 规则
└─ 中转方案（代理中转画布）

分流控制
├─ 客户端策略（全局订阅规则模板）
├─ 节点策略
├─ 规则集
└─ 策略模拟

访问审计
├─ 全部访问记录
├─ 已阻断拦截
└─ 域名资产统计

系统管理
├─ 管理员
├─ 备份恢复
├─ 操作日志
└─ 系统设置
```

---

配置版本、发布、差异与选定版本回滚在节点详情内操作。终端用户另有门户入口：概览、订阅、节点、流量、账号。

# 8. 总览模块

总览页面用于快速了解系统状态，不做复杂的大屏监控。

主要展示：

在线用户数；在线设备数；在线节点数；当前连接数；今日上传流量；今日下载流量；今日阻断次数；异常节点数；最近访问域名；最近阻断域名；简单节点拓扑；最近配置发布结果。

节点状态使用统一颜色：

- 绿色：正常
- 黄色：状态降级
- 红色：异常
- 灰色：离线
- 蓝色：配置发布或更新中

# 9. 用户管理模块

## 9.1 用户信息

字段：用户名、显示名称、启用、到期、`traffic_limit/used_traffic`、`device_limit`、估计在线设备数/执行范围、门户开关/密码/最后登录、重置周期/锚点、授权节点组、备注。每个用户自动拥有全部受支持协议，`allowedProtocols` 仅为兼容字段不再作为限制。重置周期为不重置、每月或每 30 个本地日。

`device_limit` 为 0–100，0 不限；旧 `maxDevices/maxConnections` 不再是 API 字段，数据库旧列仅兼容保留。升级默认不限。

每用户只有一个跨节点额度，无每节点预算、预算转移或核内字节配额。面板累加 Agent 增量到 `used_traffic`，可关联隐藏身份时同步其用量。禁用、到期、超额、删除及提额、清零、续期、启用都触发只调整用户访问面的自动版本，移除或恢复凭据，无须手动发布；基线、范围、重试见第 22.1 节。

正常在线、采集/发布成功且无部署占用时，超额到切断最多一个上报周期（15 秒）＋最长 60 秒合并窗口＋发布耗时；每次重试另加窗口，期间流量仍放行并计量。Linux 官方 Xray 可 splice 的 TCP（如 SOCKS5/HTTP/forward 经 Direct、识别内层 TLS 后的 XTLS Vision）通常在连接关闭后才计下行，长连接因此延后额度判定。

账户额度只计 Xray 与带 `with_v2ray_api` 且上报 `singbox_user_stats` 的 sing-box；缺能力的官方 sing-box 只报节点总量，用户未知，页面说明未计范围。升级到有用户统计的 qui sing-box 会开始计入统一额度。

## 9.2 用户接入身份与设备限制

每用户一个隐藏接入身份，沿用 `DeviceID` 与旧 Agent 合同，历史记录保留，不提供设备管理。0113 保留主身份并撤销其他设备凭据/链接；升级前核对步骤见[部署](docs/deployment.md)。

跨入口节点按不同 IPv4、IPv6 /64 源地址去重，先到先得。活跃窗口 60 秒，随后保留名额 120 秒，再提升最早等待的活跃地址；Owner 可重置顺序。NAT 多端可能算一个，双栈/切网可能多算；这是源地址估计，不是 TCP 会话或物理设备数，缺测显示“未知”。

Agent 每 500 ms 采样，按凭据保留最近 20 秒来源。Xray 连接结束后仍短暂报告 IP；sing-box 只列活连接，短于采样间隔的连接可能漏观测。失败或节点/修订/核心纪元变化清空保留值，未测账户不继承旧来源。来源取自 Xray 在线 IP RPC，或 sing-box 用户日志与 Clash 连接联接。`ConnectionStatus.cores` 仅在协商成功会话接收，按（节点、核心）保存易失快照、合并后准入；旧 `snapshot_json` 仍兼容，部分核心缺测显示部分执行。

超限约一个上报周期＋投递延迟后生效：Xray 拒绝新连接、保留既有连接；sing-box 每约 2 秒关闭匹配连接。门控变化立即下发，同一控制连接中相同内容每分钟续期，避免命令与上报相互触发连续刷新；断联 3 分钟过期放行，旧 Agent/观测失败显示部分执行。WireGuard 一次一端，不计限制。

| 核心 | UDP 受限 | UDP 不支持设备限制（TCP 仍受限） |
| --- | --- | --- |
| Xray | VLESS、Trojan、Shadowsocks | SOCKS5 |
| sing-box | VLESS、Trojan | SOCKS5、Shadowsocks、Hysteria2、TUIC |

SOCKS5 UDP 无用户归属；sing-box 其余三种会为同一用户重建关闭的 UDP 会话。无法归属的 SOCKS5 UDP 不使整核观测失败。`node.DeviceGateUDPUnsupported` 统一判定；入站 API 给 `deviceGateCoverage=full/tcp_only/none`，用户/门户给 `deviceLimitUdpUnsupportedProtocols`。页面与编辑器说明“不支持”的 UDP 范围，与节点缺能力的“部分执行”区分（L8）。

面板完整源地址仅存内存，不入库/日志/备份；Owner/门户均掩码 IPv4 /24、IPv6 /48。Xray CLI 临时 JSON 权限 0600，调用返回即删除。

Reality/xhttp 直连的 Xray Windows 针对性检查覆盖认证、用户流量与 TCP/auto 门控；这不是 Linux/UDP 全范围验收。xhttp 前置 CDN 时，节点看到边缘地址，源地址设备估计可能合并或分散客户端。sing-box 官方无用户统计标签时继续显示未知，不由新协议推断计量或门控支持。

隧道/规则后的目标看到中继源地址，目标设备限制把同一中继后的客户端合为一个来源，不透传原始源地址。账户流量仍在目标用户入站计量；GOST 及隧道仅 linux/amd64（D10）。

## 9.3 凭据管理

凭据类型根据协议确定，包括：

用户名和密码；UUID；Shadowsocks 密钥；Trojan 密码；WireGuard 密钥；订阅 Token。

用户凭据与节点间中转凭据必须分开管理。

订阅 Token 以哈希查找，新签发 Token 另存 vault 加密副本，Owner 与本人可再次查看；升级前无加密副本的 Token 返回 `not_recoverable`，重置后才可查看。每用户一条共享订阅。用户可主动重置链接；链接重置撤销旧 Token，凭据重置则在自动版本应用后使旧代理设置失效。登录密码与代理凭据分开管理。

# 10. 用户订阅与节点组

## 10.1 节点组授权

节点组页面采用卡片布局，每组显示成员与授权用户数量；编辑打开分组管理弹窗，通过“授权账号”“组内节点”菜单管理成员和授权，顶部“分组设置”修改名称、用途与备注。“添加代理节点”选择弹窗按视口高度伸展，列表独立滚动。

Owner 在节点组页授权用户，原用户授权 API 保留。用户接入来自具体自建入站（`node_group_inbounds`）或导入（`node_group_imports`），不是服务器成员。导入 `proxy_key=''` 表示整个来源，否则用源内稳定键 `name|protocol`，不依赖刷新后的内容哈希 ID。

`node_group_servers` 保存显式服务器，`node_group_members` 为显式服务器与入站所属服务器的去重并集，用于节点策略、服务器筛选与批量操作。显式服务器不授予凭据、订阅或门户可见性。仓储写入与事务触发器在入站移动/删除时维护并集，删除服务器也删显式成员；新入站不自动入组。

0122 显式保留旧 relay/egress 全部服务器，以及 subscription/mixed 中无合格入站的服务器，保证迁移前后服务器集合不变。API 返回 `serverIds`（显式）和 `nodeIds`（并集）。`serverIds` 最多 100 个存在且未删除服务器；省略/null 保留，[] 清空。旧 `nodeIds` 在 subscription/mixed 且 `inboundIds` 缺省/null 时展开合格入站，并按选中服务器过滤旧显式成员；relay/egress 则设置显式成员，显式 `serverIds` 优先。旧批量请求保留省略的导入成员。

订阅按“用户 → 授权组 → 合格入站/导入 → 可用性与协议过滤”生成；组内新增可用接入后，用户刷新订阅即可获得，无须重新授权。保存或删除节点组后按自动额度版本（§22.1）下发：受影响用户为 `user_node_group_grants` 中的账号，约一个合并窗口（60 秒）生效，只有访问面不一致的节点才生成 Revision。

## 10.2 节点组类型

节点组支持以下用途：

- `subscription`：用户订阅入口节点
- `relay`：节点间中转节点
- `egress`：系统出口节点
- `mixed`：混合用途

纯中转或纯出口节点可以设置为不在用户订阅中显示。

## 10.3 订阅过滤

订阅生成时需要执行：

1. 验证用户状态。
2. 验证用户是否到期。
3. 验证用户订阅 Token。
4. 获取用户授权节点组。
5. 展开启用、订阅可见且用途为 subscription/mixed 的授权分组，取得具体入站与导入成员。
6. 去除重复节点。
7. 过滤禁用节点。
8. 过滤维护中节点。
9. 过滤不允许订阅的节点。
10. 仅保留组内可用入站；入站非空 `allowedUserIds` 仍须包含本人，订阅还须核对已发布凭据。凭据渲染按入站授权，同服务器其他入站不自动获得用户凭据。
11. （兼容保留，不再过滤）。
12. 仅保留节点有客户端连接地址（`connectHost`，否则 `publicIp`，见第 11.1 节）的入站；没有地址的节点不产生端点。
13. 输出订阅内容。

输出名称按节点与协议命名：同一节点同一协议有多个入站时追加端口（`节点 · 协议 · 端口`），只有一个时保持 `节点 · 协议`。地址缺失不再静默：订阅预览按节点给出 `节点「<名称>」未设置连接地址，订阅不包含它的入站` 警告，`/sub` 本身只输出能连的端点。

关联目标入站的中转规则按成功发布快照追加订阅端点；仅纳入已发布、启用且对用户可见的规则，仍须通过目标入站的授权、可用性、协议与已发布凭据检查。保留目标协议、凭据、传输与 TLS 等协议字段，仅把连接地址/端口换为入口地址/规则端口；原隐含 TLS server name 显式写为目标地址。名称为 `<目标节点> · <协议> · 经 <入口节点>`（目标协议原有端口消歧保留），直接端点保持自身可见性。Hysteria2/TUIC/WireGuard 按 UDP、其余按 TCP 与规则 network 过滤；Reality/xhttp 保留目标公开参数与原 SNI，xhttp 限 TCP，格式继续按各自能力省略。

草稿修改与 D8 目标变化不推进订阅快照，pending_delete 仍保留上次快照，成功发布移除后才消失。0130 前无快照的规则须再次成功发布才进入订阅。节点中转 API 的 `shareLink` 仅 Owner 可得，使用首个有合格已发布凭据的用户生成预览；Viewer、无合格凭据或 URI 无法表达时为 null，草稿预览不代表已进入订阅。

订阅格式支持：

- 通用 URI 列表
- Base64 URI 列表
- sing-box JSON
- mihomo（`clash`，别名 `mihomo`、`clash-meta`）
- legacy Clash（`clash-legacy`，别名 `clash-premium`）

通用 URI 与 Base64 列表带不了固定证书，因此省略钉了 `trusted_ca_pem` 的 Hysteria2/TUIC 端点（不因此报错，其余链接照常输出）；这类端点由 sing-box 格式输出，并写入 `tls.certificate`。

URI、Base64 和 sing-box 保持旧输出兼容；sing-box 仍是出站片段，不是完整客户端配置。mihomo 支持全局客户端规则、provider 与客户端链；legacy Clash 过滤 VLESS、Hysteria2、TUIC、WireGuard、GEOSITE 与 RULE-SET/provider，并给 Owner 预览警告。mihomo 可用证书指纹表达固定证书的 QUIC 端点。

订阅路径是 Token → 用户 → 已发布节点及授权导入源 → 中立代理模型 → 格式导出器。`?format=` 优先，未知格式返回 400；其次为已验证 UA 表，再回退系统默认。UA 开关已实现，当前已验证 UA 匹配表为空，不能宣称自动识别已验收；一键导入协议也待核实，现提供复制与二维码。响应包含流量、到期、更新间隔与下载文件名等订阅头。

全局模板存在 `system_settings.client_rules`，有版本冲突检查与预设，按用户授权展开 `@all`、`@nodeGroup:<id>`、`@source:<id>`；服务端生成 YAML。客户端规则不替代节点策略。外部源仅 HTTPS，逐跳解析并固定经校验的公网 IP，限制 2 MiB / 15 秒；抓取失败按类型（input、dns、address、redirect、tls、connect、deadline、http、size）返回固定代码与固定中文说明，不回显链接、Location、响应体或凭据。定时抓取加密缓存，订阅请求不抓取。导入节点仅用于客户端，不参与 Agent 发布、用户额度或设备门控。客户端链与格式边界见[协议矩阵](docs/protocol-support-matrix.md)。

刷新返回结构化结果（S1）：`outcome` 为 `ok`/`partial`/`failed`，`counts` 含 `total`（= `accepted` + `rejected` + `duplicates`）、`accepted`、`rejected`、`duplicates`、`cached`、`usable`，未确定值为 null；每条目只计一次，多因条目按第一个原因计数。逐条原因最多 50 条且结果最多 32 KiB，超出置 `diagnosticsTruncated`。完整成功 200 `ok`；部分成功 200 `partial`，仍提交缓存且 `last_status=ok`、`last_error` 为空；抓取失败 502（超时 504）、内容无效或无可导入条目 422、输入错误 400、持久化失败 500、来源不存在 404，错误体 `details.result` 携带同一结果（仅限已完成的已分类结果）；请求在抓取进行中被取消时返回 503 固定文案“刷新已取消，来源状态保持不变”且不带结果，来源行、状态、缓存与订阅端点均不变。失败保留旧缓存行但 `last_status=failed`，该来源不进入订阅；`cached` 为现有行数，失败时 `usable=0`。`last_fetch_at` 是最近尝试，`last_success_at` 只在提交成功时更新；更换 URL 清空结果并回到 `pending`，不把旧结果当作新来源的结果。同一来源内 `name|protocol` 重复时保留文档顺序第一条，其余以 `duplicate_endpoint_key` 拒绝并附保留条目序号；完全相同的条目以 `duplicate_endpoint_content` 计为重复。诊断只含条目序号、协议、阶段、固定代码与固定文本，不含名称、UUID、密码或链接；手工刷新、定时刷新与粘贴导入共用同一分析路径。

外部导入支持 SS2022、VLESS Reality TCP/gRPC 与 xhttp，仍只用于客户端，不进入 Agent 发布/账户额度/设备门控。统一公开模型区分证书指纹 `TLS.Fingerprint` 与客户端指纹 `TLS.ClientFingerprint`；Reality 只携带公钥、Short ID、SNI 与可表达的 SpiderX，xhttp 为独立 host/path/mode。URI/Base64 保留公开字段；mihomo 仅输出已测 Reality TCP Vision、Reality xhttp-auto 且 ALPN 恰为 [h2]；sing-box 仅输出已测 Reality TCP Vision、无 xhttp。其他有效组合逐条省略并警告，保留链依赖与 emptyPolicy。xhttp 模式必须明确为 auto/packet-up/stream-up/stream-one，ALPN 只允许空列表或 h2/http/1.1；未知 extra、H3 与未建模参数拒绝，不保存原始配置透传。新模型与最近结果受加密备份校验覆盖。

0132 增加 `client_policy_profiles` 服务端配置库，最多 50 个配置；Owner 通过 `/api/v1/client-policy-profiles` 管理，名称最多 64 字符、描述最多 500 字符，`uiMetadata` 为最多 64 KiB 的 JSON 对象且不进入订阅。`system_settings.client_policy_active` 只保存 `{profileId}`，`client_rules` 继续是订阅读取的活动快照，其 revision 为独立的 activeRevision。编辑使用配置 revision，编辑活动配置与启用还须匹配 activeRevision；启用即使内容相同也推进 activeRevision，未修改保存不推进版本。旧 PUT 同步活动配置与快照，旧 GET 继续允许 Viewer。初始化与恢复在无配置时复制既有快照或服务端默认模板，保留 activeRevision；备份校验指针与模板一致性。浏览器旧卡片经预览后按内容摘要幂等导入为未启用配置，全部成功才清除本地副本，不采用浏览器旧启用选择。设为订阅模板，客户端更新订阅后生效。成员支持 `@endpoint:inbound:<id>`、`@endpoint:relay:<rule id>` 和 `@endpoint:import:<source id>:<name|protocol>`，只从该用户授权且已通过订阅来源检查的端点解析，客户端链克隆跳不可精确选择。`includeKeywords/excludeKeywords` 由服务器展开时对代理名称做 NFKC 规范化、不区分大小写的字面子串筛选，空包含列表不限制，排除优先，不过滤内置动作或其他组。`emptyPolicy=direct/reject` 统一作用于授权展开和格式过滤后的空组，省略兼容 DIRECT；新地区组使用 `@all` 加包含关键词并默认 REJECT。Owner-only `GET /api/v1/client-policy-candidates?userId=<id>` 复用订阅授权管线，只返回端点引用、名称、协议、来源种类/名称和节点组 ID；省略用户时按仓储顺序选择首个有节点组授权的有效用户并标明 autoSelected，无合格用户返回空列表与诊断。页面仅展示授权候选，不在浏览器重复执行筛选；分组类别按名称存入 `uiMetadata.groupCategories`，重命名同步移动类别键与组/规则引用。Owner-only `POST /api/v1/client-policy-profiles/preview` 预览草稿 template 或已保存配置（profileId + profileRevision），两者互斥；要求 CSRF，只读且不记审计。草稿复用保存校验，失败返回 invalid_rules 及字段路径；已保存版本过期返回 409 stale_client_policy 及当前 revision。与候选接口共享自动选用户，无合格用户仍校验并返回空正文与诊断。预览与真实订阅共用渲染和单次 Expand，返回用户上下文、解析分组、全部警告、规范化模板 SHA-256 和 activeRevision；不签发 Token、不标记抓取，provider URL 为占位。旧用户 GET 预览仍用活动模板。页面未保存时预览草稿，否则预览当前配置；地区组解析成员使用同一接口。编辑器高级预览上下文可显式选择有效且已授权的用户，预览、解析与候选共用userId；默认仍自动选择，仅影响本次查看，不改活动模板或授权。分组编辑规则始终打开目标筛选列表，空状态可新增并预选该组，筛选时禁用全局拖拽排序；保存、启用或预览失败均保留草稿。共享规则集由 policy 模块持有，subscription/rules 唯一扫描全部已保存配置（含活动/未启用配置及启用/禁用规则），通过 platform/server 注入事务内读取。Owner-only `GET /api/v1/service-sets/{id}/references` 返回 nodePolicies 与 clientProfiles/rules；任何已保存引用均阻止删除（409 service_set_in_use，error 内附两类引用）。保存共享规则集立即写入，独立于配置草稿；禁用允许，保存响应附 references 与 warning。禁用后 `/sub/{token}/rules/{id}` 返回 404，客户端可能继续使用缓存或报告错误，不保证缓存规则立即停用。带 Lookup 保存时，启用的 RULE-SET 规则不能引用缺失或禁用的面板规则集；已保存模板读取只做结构校验，预览通过导出器警告路径提示缺失/禁用。节点、节点组和规则集选择项读取全部分页；自动预览用户由服务端分页选择，手动预览选择列表读取完整用户分页并由服务端重新校验。

# 11. 节点管理模块

## 11.1 节点列表

节点列表显示：

节点名称；在线状态；公网 IP；操作系统；CPU 使用率；内存使用率；磁盘使用率；当前连接数；今日流量；Agent 版本；代理核心；核心版本；当前配置版本；最后心跳时间；本月整机流量（单张网卡的本期整机用量与可选额度，见第 21 节）。

节点有两个地址字段（迁移 0135、0138）。`publicIp` 是公网地址：Agent 心跳（第 36 字段 `public_addresses`）上报当前机器的所有非回环公网全局单播地址（最多 16 个，包含网卡与对端源），面板在数据库保存为候选列表 `public_addresses_json` 并由接口暴露为 `publicAddresses` 数组；上报为空且库里为空时以控制连接的 gRPC 对端公网地址填入。Owner 可在节点设置里从候选地址列表中勾选 `publicIpSelected`，选定地址记为 `selected` 来源；设置里手填 IP 则记为 `manual`；清空则为 `auto` 自动推导（优先默认路由 IPv4，其次首个公网 IPv6，最后连接对端）。`publicIpSource` 取值统一为 `manual` / `selected` / `auto` / 空。若选定地址从当前心跳候选列表中消失，节点标记 `publicIpSelectionMissing=true` 提醒 Owner。节点登记接口支持可选 `connectHost`。`connectHost` 是 Owner 选的客户端连接地址（域名或 IP，无协议、端口与方括号），可再清空。

客户端连接地址 = `connectHost` 非空则用它，否则用 `publicIp`；订阅、订阅预览与分享链接都按这个顺序，中转、隧道与画布继续用 `publicIp`，因为它们需要节点之间的地址。使用域名且入站 TLS 没有显式服务器名时，SNI 规则不变，不因连接地址是域名而新增 SNI。两者都为空时该节点不进订阅，订阅预览对每个这样的节点给出一条 `节点「<名称>」未设置连接地址，订阅不包含它的入站` 警告（第 10.3 节）。

## 11.2 节点详情

展示基础信息、资源、能力、入站/出站、节点组、当前连接、今日代理流量、最近域名、当前/上一成功配置、发布记录、事件、出站网络与本月整机流量。出站源 IP/网卡、整机强制出站 IPv4/IPv6 未配置显示“—”，整机与代理流量分开。

`capabilities.cores` 列启用核心的版本、协议、用户统计、转发和门控能力；`heartbeat.cores` 列健康/错误/启动时间/统计源状态。`core_selectionBlocked` 列添加/移除中不可选组件。入站的 `core`、`userTrafficSupported`、`deviceGateSupported` 给所属与覆盖。`Welcome.multi_core_supported` 仅对声明支持的 Agent 为真；旧 Agent 按 `core_name` 单核心建模，缺 `Capabilities.cores` 时由旧标量合成。不得接受未上报启用核心；缺测保留 null，不编造 0/健康。GOST 仅 forward，无用户统计/门控/连接快照，coverage=none；编辑器只为 forward 提供已上报 GOST。端口唯一性见第 12 节。

`Capabilities.host` 刷新 CPU 型号/逻辑核、架构、内核、系统、内存/交换/状态目录文件系统总量；v1.5.0 扩充宿主虚拟化（`virtualization`：none/kvm/lxc/openvz/wsl 等）、19 个白名单调优键的可用与只读状态（`tuning_keys`）、拥塞控制与排队规则已就绪及可加载算法（`congestion_available/loadable`、`qdisc_available/loadable`）、进程文件句柄限制（`fd_limits`：Agent 与核心进程的 soft/hard 上限，OpenRC 设 `rc_ulimit="-n 524288"` 对齐 systemd `LimitNOFILE=524288`）及宿主机 `/etc/sysctl.d/` 与 `/etc/sysctl.conf` 的显式配置源（`sysctl_sources`）。心跳提供 load1/5/15、processCount、tcpConnections、udpSockets、memoryUsedBytes、swapUsedBytes、diskUsedBytes、tuningCurrent，以及 TCP 出站段 `tcp_out_segs`（字段 37）与重传段 `tcp_retrans_segs`（字段 38）；optional 缺测省略，实测 0 保留，uint64 JSON 为十进制字符串，旧 snake_case 字段保留。HostInfo 普通标量 0 无法区分缺测。`bootId/defaultRouteInterface` 缺测仅影响整机流量。

资源曲线每节点每 UTC 分钟保存最后一条新鲜心跳（消息/接收时间差 ≤45 秒），确认过的重放和过期消息不采样，随延迟调度清理、保留 7 天。查询 CPU/内存/磁盘百分比及字节、load1、进程/套接字/已测活连接、非 lo 接口速率合计与 TCP 重传率 `tcpRetransPercent`（根据两分钟心跳的 TCP 出站段与重传段增量比值计算，缺测或出站段为 0 时为 null，不补 0）；网卡合计可能重复计数，不等于代理量。缺测 null，不补 0。调优 GET 允许 Owner/Viewer，修改限 Owner＋CSRF＋匹配 confirmNodeId；期望与实际分列（第 22.0 节）。

自建入站的发布状态按入站而非按服务器判定：`GET /api/v1/nodes/{id}/inbound-states` 把草稿入站与节点当前运行修订比较，给出 `effective`／`modified`／`pending`／`unpublished`（未生效且最近一次包含它的发布失败时带 `code` 与 `revision`）；比较忽略修订号、用户凭据与 WireGuard 对端，自动额度版本因此不会把未发布入站算作已生效，只读、不写库。

## 11.3 节点注册与删除

Owner 建节点 → 一次性注册码 → 安装 Agent → 本地生成私钥并提交注册码/公钥/机器信息 → 中央签证并核销注册码 → mTLS 长连接。私钥不上传。

注册对话框提供 `deploy/linux/node-install.sh` 一键命令：下载面板同版本 GitHub Release、核对 SHA256SUMS，以公网证书或面板提供的固定 SHA-256 建立证书信任，再登记启动，操作见[部署](docs/deployment.md) §4。命令里的面板地址、面板 Web 端口以及 Agent gRPC 地址与端口默认取系统设置里的“节点安装连接地址”（第 27.14 节），未设置或读取失败时回落到当前浏览器访问面板的地址，gRPC 端口回落到注册响应里的实际监听端口。三个生成该命令的对话框（新建服务器、原位身份恢复、版本页“添加内核”）共用这一默认值，对话框不再提供面板地址与端口输入框，只读显示本次命令使用的地址，需要修改时到系统设置里改；gRPC 地址与面板地址相同时不写 `--grpc-host`。三个对话框另有默认不勾选的「启用网页升级」，勾选才在命令末尾追加 `--web-upgrade`（第 23 节）。

原位身份恢复（迁移 0134）：主机重装但身份仍属原节点时，Owner 在节点证书区签发一次性恢复码（默认 900 秒，允许 300–3600 秒，可勾选立即吊销现有证书），响应只返回一次，数据库只保存 SHA-256 并绑定当时的证书指纹。签发与核销都检查未完成任务，存在时返回 409 `node_recovery_blocked` 与 `blockers`。新 Agent 通过现有 `Enroll` RPC 核销恢复码，节点 ID、入站、出站、节点组、隧道、代理链与历史配置均保留，旧指纹随后永久作废（备份恢复也不复活），用它连接会被拒并记一条 WARN 日志 `node certificate refused`，当前已发布配置在新 Agent 首次能力上报后自动重新下发。Agent 二进制与安装脚本不变，操作见[运维](docs/operations.md) §5.1。

未登记且无身份的节点取消登记时直接删除；已登记节点逻辑删除（`deleted_at`）、吊销证书、退出分组并从订阅/列表/选择器消失，保留流量与访问记录，不可重新启用，同服务器须新建节点登记。删除不停止远端 Agent/代理。

删除前清理入站、出站（含其他节点指向它的出站）、出口组、链、规则引用并等待批量发布结束，否则 409 `node_delete_blocked` 列阻塞项；`GET /api/v1/nodes/{id}/deletion-preview` 可预览。删除同时关闭本节点待发布、核心/Agent 更新及排队健康检查。操作见[运维](docs/operations.md) §15。

## 11.4 节点状态

节点状态统一为：

- `pending`
- `online`
- `degraded`
- `offline`
- `applying`
- `rollback`
- `disabled`
- `error`

# 12. 入站代理模块

入站表示用户连接节点时使用的协议。

支持 HTTP CONNECT、SOCKS5、Shadowsocks、VLESS、Trojan、WireGuard（仅 Xray 入站）、Hysteria2/TUIC（仅 sing-box，需 QUIC 能力）与端口转发。VMess 与完整系统 TUN 不在当前支持表内；每核心范围以能力上报及[协议矩阵](docs/protocol-support-matrix.md)为准。

v1.4 自建 VLESS 用户入站：Xray 支持 Reality TCP（无 flow/Vision）、Reality gRPC（无 flow）及 Reality/TLS xhttp 四种模式；sing-box 仅 Reality TCP（无 flow/Vision），须单个具体 DNS SNI。必须由所属核心原生探测并上报设置契约与精确组合，旧 Agent、未知版本或未列组合不能保存/发布。内部代理中转暂保留 TCP/TLS。Reality 私钥加密保存并在普通查询/预览/差异中隐藏，普通编辑不轮换；显式轮换先保存草稿，再发布并让客户端更新订阅，失败/回滚保留成功版本的密钥。

一个节点可同时有多个入站。

节点创建与编辑使用同一单页表单：基础信息、连接设置、协议设置、授权与订阅直接显示；基础信息提供“所属节点组”多选，创建时保存组归属，编辑时回填并可调整，不选则不加入任何组。高级选项及配置预览与变更默认折叠，保存成功后展开预览。协议与安全字段随选择和所属核心能力联动，编辑时所属服务器及协议锁定。底部提供保存草稿和保存并发布；校验或保存失败保留输入并定位错误。连接地址继续取服务器信息，本轮不增加独立的客户端地址/端口覆盖字段。

入站配置主要包括：

- 名称
- 所属节点
- 所属核心（`core`／`inbounds.core`；留空表示该节点当前报告的主核心，填写值必须是该 Agent 报告为启用的核心）
- 协议
- 监听地址
- 监听端口
- 传输方式
- 安全方式
- 允许用户
- 是否显示在订阅中
- 是否启用
- 默认出站

用于节点间中转的内部入站，应设置为不在用户订阅中显示。

订阅隐藏（`subscriptionVisible=false`）的用户入站不会获得用户凭据，发布后渲染为停用，编辑器对这种组合给出提示。

隧道/规则生成入站返回 `managedBy:{kind,id,name}`，按 `tunnel_objects` 的 ID 归属管理；普通入站 PUT/DELETE 返回 409 `managed_by_tunnel`，须在隧道管理修改/删除并发布。目标入站被关联规则引用时，删除返回 409 `inbound_has_relays` 与 `rules:[{id,name}]`，包括尚待发布删除的规则。

由画布创建或接管的入站不能在入站编辑器中改名：面板按 `canvas_tunnel_objects` 的归属行判定（不按名称前缀），普通保存若改了名字会被拒绝（“该入站由画布隧道管理，不能在入站编辑器中修改名称。”），画布自身的生成路径仍可改名并由回滚撤销。未归属的入站可以自由改名，即使名字看起来像生成的。入站接口不额外暴露画布归属，页面因此不额外提示。

端口转发（`forward`）在 GOST 核心上另有可选字段（设置契约版本 2）：`targets[]`（1–32 个有序 `{host,port}`，与旧 `host`/`port` 互斥）、`strategy`（`round` 轮询／`fifo` 主备，默认 `round`）、`max_fails`（1–100，默认 1）、`fail_timeout`（1–3600 秒的整数秒，默认 `5s`）、`port_end`（含尾端，`Inbound.Port` 为起端，最多 256 个端口）、`bandwidth_in_bps`／`bandwidth_out_bps`（0 或 1024–1000000000000 字节/秒，0 为不限）与 `max_connections`（0–100000，仅 TCP 服务；UDP-only 转发拒绝该字段）。这些字段按“是否出现”判定：显式写 0 或默认值也算已设置，Xray／sing-box 转发携带它们会被共同校验、面板保存/发布与 Agent 渲染三处一致拒绝，不会被静默丢弃；选择非 GOST 核心前必须先清除。渲染时 qui 自己展开端口（GOST 的 `addr` 不支持范围），每个（端口、传输）一个服务，名字为 `fwd-<入站ID>-<端口>-<tcp|udp>`，每个服务都带完整有序目标池与独立的选择器与限速/连接限值，不会把第 n 个目标映射到第 n 个端口。每节点最多渲染 1024 个生产服务。

同一节点按数字端口跨核心检查，不区分 TCP/UDP，转发范围每个端口均占用。手动保存检查启用入站对启用入站的冲突；禁用入站保存只检查持久声明。`port_claims` 无论启停始终保留，发布变更/删除成功前旧声明也保留。自动分配跳过所有入站（含禁用及范围）与声明；`ports/check` 同样报告禁用入站供提示，故其占用提示不等于手动保存必然失败。共享检查按 ID 排除自身，Agent 校验为最终门禁。

协议、转发能力、WireGuard 与设备门控按入站所属核心的能力校验；组件正在被添加或移除（任务未落定）期间 `core` 不可选，保存草稿与发布都会拒绝，错误码 `core_pending`。

自建 VLESS 的 xhttp／Reality（v1.4，契约、渲染、密钥生命周期与订阅映射已实现）已加入 `desiredstate.ProtocolSettings` 的类型化服务端私有字段：`reality{private_key,public_key,handshake_target,server_names[],short_ids[]}` 与 `xhttp{host,path,mode}`。两者只接受 VLESS 入站；不接受 JSON 透传、`extra`／`xmux` 或未知键（API 与 `desiredstate.Decode` 均拒绝未知字段）。Reality 私钥只进加密的 `encrypted_settings` 与加密修订，普通读取（`mask`）、修订差异和预览只暴露公钥、握手目标、server names 与 Short ID；省略私钥的编辑沿用已存值，普通保存不轮换密钥。校验口径：两把 X25519 键均为无填充 base64url、32 字节，且公钥必须由私钥导出（角色正确）；Short ID 为 1–16 个字符的偶数位十六进制；server names 1–16 个、每个 ≤253 且无控制字符；握手目标由 Owner 必填 `host:port`，面板只校验语法，不做默认站点或网络可达检查（Agent 原生校验是最终门禁）；Reality 与普通 TLS 证书材料互斥。xhttp 只接受 `auto`／`packet-up`／`stream-up`／`stream-one` 这一 TCP/H2 承载子集，`path` 归 xhttp 自己，`service_name` 必须为空；Vision（`xtls-rprx-vision`）只允许 TCP 上的 TLS 或 Reality，xhttp／gRPC 上的 Vision 在保存前拒绝。

TLS 入站的证书来源（v1.4.7 §3，C1 后端＋Agent）记在 `inbounds.tls_source`（迁移 0136）：`paste` 粘贴内容、`self_signed` 面板自签、`file` 节点上的文件、`acme` 引用面板自己申请的证书（第 27.16 节），`''` 为 0136 之前的历史行或不带证书的入站。该列只是面板元数据，不进 `desiredstate`（Agent 的 `DesiredState` 解码拒绝未知字段）。自签沿用内部中转的生成逻辑，有效期由参数区分：内部中转 1 年，用户入站 10 年（客户端会钉住证书）；证书的 Subject/SAN 取节点客户端连接地址（第 11.1 节的 `ConnectAddress()`），没有地址时保存返回 400「请先在节点设置里填写公网 IP 或客户端连接地址，再生成自签名证书」。写库仍是 CertificatePEM＋PrivateKeyPEM＋TrustedCAPEM＝同一张证书并设置 ServerName，所以订阅无需改动：URI/Base64 省略钉了 CA 的 Hysteria2/TUIC，sing-box 写 `tls.certificate`，mihomo 写 fingerprint。来源本来就是自签时编辑保留已发证书，`regenerateCertificate: true` 或从其他来源切到自签才签发新证书（例如连接地址变化后）。节点文件来源只保存两个路径（`certificate_path`／`private_key_path`）：仅限 TLS 入站、与粘贴材料互斥、必须同时填写、绝对且已规范（`path.Clean` 不变）、≤256 字节、位于 `/etc/proxy-agent/certs/` 下、不得含 `..`；出站永不携带。Agent 在渲染时用 `filepath.EvalSymlinks` 解析并确认仍在同一目录、文件可读、`tls.LoadX509KeyPair` 成功且叶子未过期，密钥权限宽于 0640 只记一条 WARN；任一失败按普通失败规则回滚。渲染用核心自身字段（sing-box `certificate_path`/`key_path`，Xray `certificateFile`/`keyFile`），原生 `check` 照常执行。文件来源受 `CoreCapability.tls_certificate_files_supported`（字段 12，`Capabilities` 字段 25 为单核标量形式）门禁，保存、快照与发布前三处一致拒绝旧 Agent。读取接口另有只读 `certificateInfo{subject,dnsNames,ipAddresses,notAfter,selfSigned,coverageWarning}`：用 `crypto/x509` 解析面板持有的证书，ServerName 与连接地址都不被覆盖（`VerifyHostname`）时给出中文提示；`file` 来源返回 null，因为面板读不到节点文件。证书文件过期或丢失时，下一次应用失败并保留原配置；Agent 重启后仍保留的修订若引用过期或缺失的文件就无法渲染，该核心停到文件恢复为止，请在到期前续期再重启核心。

v1.5.0 §6 起编辑器按域名查找证书：TLS 区块先填「域名（服务器名称）」，点「查找证书」（输入框失焦也触发，400ms 防抖）后调用 `GET /api/v1/nodes/{id}/certificate-matches?domain=`，结果按「节点上覆盖该域名且未过期的证书文件 → 证书库里已签发的证书 → 已过期的节点文件 → 申请证书」排序显示；节点文件选项只在所属核心上报 `tls_certificate_files_supported` 时出现，旧 Agent 仍能走证书库与申请路径。选中节点文件或证书库证书即写入对应来源与路径／证书 ID；「申请证书」只在已配置 Cloudflare Token 且没有可用证书时出现，点击后在证书库创建该域名的申请并每 5 秒轮询，签发成功自动选中（`acme` 来源，由面板随配置下发，不往节点写文件），失败显示原因。自签与粘贴仍保留在「证书来源」下拉里。

`acme` 来源（v1.4.7 §4，D2）：入站只保存 `certificate_id`（请求与读取字段 `certificateId`，指向证书库，迁移 0137），**不保存 PEM**——密封的 `encrypted_settings` 里没有证书副本，面板在读入站时把证书库里的证书与私钥填进设置，所以快照（`SnapshotIntent`，所有发布路径共用）、预览与差异看到的都是证书库当前内容；私钥同其他来源一样在普通读取、差异与预览里隐藏。保存校验：仅 TLS 入站（不含 Reality、不含内部中转）、必须选择存在且已签发（有证书与私钥）的证书，否则 400「所选证书尚未签发成功」等中文提示；不得同时粘贴私钥或填写节点文件路径；`server_name` 为空时取证书的第一个非通配域名；改用其他来源会清空 `certificate_id`，且不会把证书库的证书冻结成粘贴副本。快照遇到证书缺失或没有签发材料的 `acme` 入站直接失败，绝不把没有证书的入站发布到节点。`certificateInfo` 用证书库里的证书解析（`selfSigned` 为 false，覆盖提示照常按 ServerName 与连接地址判断）。`acme` 入站没有 `TrustedCAPEM`，订阅按公网可信证书输出，URI/Base64 也包含 Hysteria2/TUIC。Agent 收到的仍是普通 PEM，无需新能力。
能力门禁独立于协议标志与版本号：`desiredstate.CoreSettingsCapability{core,settings_contract_version,combinations[]}` 按（协议、方向、安全、传输、flow）逐项声明。版本 0、缺失、未知版本、其他核心的上报以及未列出的组合一律不授权；Agent 版本文本、VLESS 入站标志、`multi_core_supported` 与 GOST 契约位都不能替代它。M1 已把该上报落到 `agentpb`：`CoreCapability.server_settings_contract_version`（字段 10）与 `server_settings_combinations`（字段 11）为逐核声明，`Capabilities` 的同名字段（23／24）是单核 Agent 的标量形式，均由 `scripts/generate-proto.sh` 生成、只做加法，旧 Agent／旧面板各自忽略或保留未知字段。Agent 的 `MultiRuntime.DetectCapabilities` 按各适配器上报复制到对应核心，`control.reportCapabilities` 的旧标量装饰同样保留；面板 `node.SettingsContractCapability` 只把上报转换为共享契约、不推断来源，`node.EnabledCores` 的单核回退也保留这些字段。上报结构在 `validateMultiCapabilities` 里用共享组合校验一次：组合缺版本、方向／传输非法、重复项或超上限都拒绝整条上报；未知版本仍被接受（不因未来版本断连）但不授权。当前 Xray／sing-box 适配器都没有渲染器，因此生产 Agent 上报版本 0 与空列表，面板保存（`proxy.ValidateInboundServerSettings`）、配置快照（`SnapshotIntent`）与发布前（`deployment.validateStateCoresTx`）三处一致拒绝 Reality／xhttp 设置；合成上报可完整传输并放行对应组合，真实启用仍待渲染器（M2／M4）。

M1 不提供渲染：Xray 与 sing-box 适配器都没有 Reality／xhttp 渲染器，渲染入口（`desiredstate.ValidateServerSettingsRenderer`）对携带这些设置的入站或出站一律失败，避免结构校验通过后静默丢弃设置；已发布状态若意外带这些字段，`subscription/source/published` 也跳过该端点而不是导出缺公钥与 Short ID 的客户端条目，这只是渲染器缺席期间的临时保护，不代表客户端映射已完成。`/api/v1/protocol-support` 的 VLESS 行把 `security_reality`、`transport_xhttp` 列为 `pending`，选项、订阅映射与 sing-box 适配留待 M2–M4。

M1 复核修正：Reality server names 只接受具体主机名或 IP，不接受通配符（固定版 Reality 按 ClientHello SNI 精确匹配）。未知非零设置契约版本只检查数量、空对象与字段长度，不按当前版本解释枚举；接收后覆盖旧报告且始终不授权。排队的应用与回滚在投递时再次检查当前报告，携带新设置的命令还须本次连接已持久接收一条新序号能力报告；旧连接报告及已接受消息的原样重放均不能开门。支持撤回时保留命令、继续确认消息，不断开健康控制流；普通旧配置不要求此报告。备份检查对携带 Reality／xhttp 或对应安全／传输标志的草稿按协议与入站／出站角色复用 `ValidateSettings`，保留旧草稿的既有校验语义；加密修订继续走完整 `desiredstate.Decode`。

M4（sing-box Reality 适配，待复核与 Linux 门禁）覆盖前述 M1 的 sing-box 渲染器缺席状态：Agent 实现 VLESS Reality + TCP 的无 flow 与 Vision，先用自身类型化渲染器和安装核心的原生 `check` 逐项探测，成功才上报对应契约。仅单个具体 server name，ShortID 列表完整映射；多名称、冲突的旧 server name、Reality+gRPC 与 xhttp 显式拒绝。Windows/amd64 固定官方 1.14.3 已独立验证回环认证连接、错误凭据和密钥回滚；官方无用户统计构建仍显示未知，不因 Reality 能力推断计量。此处只是 Agent 适配器能力，订阅映射、UI、Linux／统计构建／设备门控验收另行记录，见 [M4 报告](docs/v1.4-m4-singbox-reality.md)。

Hysteria2 端口跳跃（v1.5.0 §7）：入站 `settings.quic` 可带 `hop_port_start`、`hop_port_end`、`hop_interval_seconds`。仅 Hysteria2 **入站**、仅 sing-box 核心；范围 1–65535、`end > start`、最多 10000 个端口、不得包含入站自身端口，间隔 5–300 秒（0 用默认 30，只用于订阅）。`Capabilities.port_hop_supported`（字段 28）由 Agent 在 host-network helper 已安装时上报；面板在保存、`SnapshotIntent` 与发布前（`validateStateCoresTx`）三处一致拒绝旧 Agent（`port_hop_unsupported`）。面板端口冲突用 `desiredstate.ClaimedPorts`（监听端口＋跳跃段），Agent 的监听预留仍用 `ListenPorts`；helper 的 `inet proxy_agent_hop` 表整表替换、无跳跃入站时删除。订阅输出：mihomo `ports`/`hop-interval`，sing-box `server_ports`/`hop_interval`，URI `mport`；legacy Clash 本就不输出 Hysteria2。NAT 主机的跳跃段必须在服务商放行的端口内，主机防火墙需放行该段 UDP。

# 13. 出站代理模块

出站表示节点收到用户流量后，以什么方式继续访问。

支持的出站类型：

Direct；Block；HTTP；SOCKS5；Shadowsocks；VLESS；Trojan；WireGuard；远程节点；出口组；Proxy Chain。

出站主要字段：

名称；所属节点；出站类型；目标节点；目标地址；目标端口；使用协议；认证凭据；TCP 支持情况；UDP 支持情况；是否启用；健康状态；失败处理方式。

失败处理方式包括：

Block；Direct；备用出站；备用出口组；备用代理链。

对于要求固定出口的网站，默认失败策略建议为 Block 或备用出口，不自动 Direct。

## 13.1 节点出站网络

节点可以在“节点详情 → 节点设置”里指定出站网络。改动在下一次**发布配置**时生效；字段留空并保存表示清除。

- **核心出站源 IP / 核心出站网卡**：只影响本节点代理核心转发的流量。Xray 渲染为出站 `sendThrough` 与 `streamSettings.sockopt.interface`，sing-box 渲染为 `inet4/inet6_bind_address` 与 `bind_interface`；出口组成员同样固定，sing-box 的 `selector`/`urltest` 组本身不带拨号字段（上游不接受），入站 WireGuard 端点只监听、不改写。地址族按源地址固定（Xray `ForceIPv4`/`ForceIPv6`，sing-box `ipv4_only`/`ipv6_only`）；目标是另一族的字面量 IP 时连接失败。
- **服务器强制出站 IPv4 / IPv6**：整台服务器经该网卡新建的出站连接都改用此源地址（核心出站源 IP 除外）；仅 Linux，失败自动回滚。执行方式、双栈注意事项与排查命令见[运维手册](docs/operations.md)。

面板只校验语法（地址须为单播、`host_source_ipv4` 为 v4、`host_source_ipv6` 为 v6、网卡名符合 Linux 命名）；地址或网卡是否存在由节点在应用配置时判断，缺失时按临时失败码重试。

# 14. 协议转换中转

中转分为隧道管理、节点“添加中转”和代理中转画布。隧道转发字节，不终止目标用户协议；画布由节点终止协议 A、按策略以协议 B 建立下一跳，用户凭据与节点间凭据隔离。

隧道为 entry → [mid] → exit，最多三台不同服务器、两段中继；仅 GOST relay over TLS＋mTLS。两节点要求各跳契约 ≥3，三节点各跳 ≥4；接收跳使用公网地址或 `connectHost`。每个接收跳为同一隧道的规则共享一个监听，中间只知道一个下一跳。分段独立密封 CA/叶子证书，私钥不进 API、计划或日志。

规则指定入口监听、network=tcp/udp/both、可选隧道及最终目标；不选隧道即直接转发，优先用 GOST，否则主 Xray/sing-box。linked 关联用户入站，由目标服务器地址与入站端口派生，隧道出口就是目标服务器时用 127.0.0.1；manual 固定 host/port，不生成用户订阅。D8：目标公网地址或入站端口变化在同一事务递增草稿版本，已发布规则标为 needs_publish；读取派生新目标，上次成功发布快照保持不动。删除目标前须先发布移除关联规则。

节点“添加中转”可选择已有隧道，为本节点用户入站创建 linked 规则（隧道入口须为其他节点，ownerNodeId 为目标节点）；也可新建中转方案，以当前节点预选为代理入口。订阅与分享见第 10.3 节。

隧道/规则保存只写草稿；draft 从未发布，modified 为手动修改，needs_publish 为目标变化，published 为成功发布，pending_delete 为待发布移除且只读。未发布对象可直接删除；已发布对象保留行、旧监听声明及快照直到成功移除。隧道有规则引用时返回 tunnel_in_use（包括待删除规则）。

生成对象按 `tunnel_objects` 的 owner kind/ID 归属，不按名称认领；`port_claims` 保存节点级数字端口，改端口保留新旧声明直到成功发布。隧道和规则必须手填端口或从 `node_meta.port_range_start/end` 分配，无默认端口回退。自动建议不预留，保存时再查并声明，重发复用已保存端口；冲突语义见第 12 节。

中转方案画布只创建代理中转（proxy_chain/tunnel，tls 或 wireguard）；出口可为 qui 用户入站或外部 SOCKS5/HTTP/Shadowsocks/Trojan/VLESS 出站。qui 入站出口生成专用内部监听和独立中转凭据，不改变原用户入站；外部出站属于前一跳服务器，作为最终出站且按 TCP/UDP 能力校验。relay/exit 实例的 relayPortMode/relayPort 优先于旧 tunnelPort；自动分配复用稳定端口，无范围时画布仍保留 TLS 8443 / WireGuard 51820，范围耗尽则失败。

画布内部资源仍用 `canvas_tunnels/canvas_tunnel_objects` 的 ID 归属与共享依赖。旧 port_forward/gost 连接在迁移前只读保留，可删除，不可新增或改动；一次性转换及遗留原因见第 26.9、31 节。GOST 目标池、范围、限流与连接上限仍属普通 forward 入站（第 12 节），不由新画布创建。

# 15. 出口组模块

出口组用于系统内部对多个出口进行统一管理。

支持的选择方式（按上游能力）：

| 策略 | Xray | sing-box |
| --- | --- | --- |
| 主备（`primary`） | 由 Agent 在节点上选择当前成员，只渲染选中的成员（上游没有“首健康”策略） | 同左 |
| 简单轮询（`round_robin`） | 渲染 `routing.balancers` 的 `roundRobin` + `observatory` + `fallbackTag` | 渲染 `urltest` |
| 延迟优先（`latency`） | 由 Agent 在节点上选择（leastPing 语义） | 渲染 `urltest` |
| 权重（`weighted`） | 已移除 | 已移除 |

权重策略在上游没有对应实现，已从校验、API 与页面移除；迁移 `0104_egress_weighted_to_round_robin.sql` 把既有权重出口组改写为 `round_robin`，读取旧配置版本时同样按 `round_robin` 解释。

出口组与用户订阅节点组必须分开：

- 用户节点组决定用户客户端可以连接哪些入口节点。
- 出口组决定节点内部流量最终从哪里出去。

出口组的备用策略按配置执行，**固定出口失败不会隐式 Direct**（见第 13 节失败处理）。

# 16. 代理链模块

隧道管理计划/发布接受 tunnelIds/ruleIds；选隧道纳入其规则，选规则纳入隧道，隧道有改动时一并纳入其他拨号规则。计划只读，展示服务器、监听、路由、变更、冲突和依赖；发布重新规划，复用部署批次按 exit → mid → entry 执行，失败反向回滚。仅全部成功推进版本及订阅快照；失败恢复生成对象、保留草稿与声明。未变发布为 unchanged，无修订或批次。

轮换隧道凭据仅生成新草稿，须再计划/发布才切换；接收监听先于拨号方，切换期间可短暂中断，不保留既有 TCP/UDP 会话。中间/出口 relay 健康仅指进程与监听，不代表目标可达。

画布代理中转仍按 `canvas_tunnels` 共享对象与依赖：相同 tunnelId 只生成一组对象，未变可无操作发布；不同方案对同一入口声明冲突路径则拒绝。删除方案不删仍共享对象，最后引用移除后须发布清理；在用画布隧道删除返回 409 tunnel_in_use 与方案列表。不同资源不自动接管监听。客户端方案自包含，拒绝 tunnelId，proxy_chain/tunnel 均表达 mihomo dialer-proxy 单路径。

画布发布先整体校验依赖，跳过仅为依赖且运行配置、启用意图未变的服务器（忽略修订号、用户访问按集合比较、空等于缺失，历史 NULL 启用意图不等于空集合）；跳过者无修订或回滚项。裁剪后按批外目标当前已发布状态校验；显式中继批次仍发布全体。新画布不接受端口转发/GOST 隧道连接。

代理链支持 A→B、A→B→C，各跳协议可不同，不支持无限多跳。

代理链包含：

链路名称；入口节点；中转节点；出口节点；每一跳协议；每一跳出站；最终出口方式；失败处理策略；健康状态。

代理链保存和发布前必须检查：

是否存在重复节点；是否存在路由环路；是否超过最大跳数；每一跳协议是否受支持；每一跳 TCP/UDP 能力是否匹配；端口是否冲突；内部入站是否存在；节点间凭据是否有效；最终出口是否存在。

按出口→中转→入口发布，按入口→中转→出口回滚。

# 17. 分流模块

分流模块统一处理：

网站分流；网站阻断；直接连接；指定节点出口；指定代理链；指定出口组。

规则可以匹配：

用户；入口节点；入口节点组；域名；域名后缀；网站服务分类；IP；CIDR；端口；TCP；UDP；时间段。

规则动作包括：

Direct；Block；指定 Outbound；指定出口组；指定 Proxy Chain。

规则按优先级从高到低执行，命中第一条有效规则后停止。

每个节点必须存在默认规则。

# 18. 服务规则模块

服务规则用于将常见网站的域名集合封装成易于选择的服务。

内置服务分类包括：

Netflix；Disney+；YouTube；OpenAI；Google；GitHub；Telegram。

管理员不需要手工维护大量域名，只需要设置：

```text
Netflix → Node B
OpenAI → SG-Exit
YouTube → Direct
```

系统内部将服务规则编译为域名或 IP 匹配规则。

系统也支持创建自定义服务分类，例如：

```text
公司开发服务
├─ git.example.com
├─ registry.example.com
└─ api.example.com
```

服务规则更新必须经过配置版本发布，不直接在线修改节点运行规则。

# 19. 网站访问控制

网站访问控制与分流使用同一套策略引擎。

支持控制：

某个用户不能访问哪些网站；某个节点不能访问哪些网站；某个节点组不能访问哪些网站；某个网站必须从指定节点出去；某类网站只允许在指定时间访问。

访问控制支持：

精确域名；域名后缀；域名关键字（domain_keyword / keyword）；通配域名；自定义网站分类；内置服务规则；IP/CIDR。

# 20. 网站访问记录

页面、筛选、策略作用域与模拟器不再暴露设备字段；历史数据中的设备 ID 与隐藏身份仍保留以兼容统计和墓碑。按用户查询，不提供终端用户访问记录入口。

系统只记录域名级访问结果。

记录字段包括（设备仅是内部历史/隐藏身份字段，不展示）：

时间；用户；设备；入口节点；入站协议；访问域名；目标 IP；目标端口；命中规则；执行动作；出站协议；代理链；最终出口节点；允许或阻断；连接数量；上传流量；下载流量。

**字段口径**：

- **上传/下载流量是按域名的可空字段**。官方上游核无法提供每域名精确字节，未知时为 NULL，页面显示“—”，**不写零**。用户与节点级的总量仍来自核心统计。
- **最终出口节点记录的是“配置出口”**：该节点按策略解析出的出站或出口组所选成员。它表示配置，不证明跨节点链路实际到达了哪个出口节点；跨中转的实际出口无法确认，页面会分别标注“本节点直连”和“配置出口未确认”。
- 跨分钟仍存活的连接不会每分钟重复计为新连接；连接数不等于实时并发或请求次数。

阻断如果节点无法确定域名，则记录目标 IP，不根据网页内容猜测域名。

# 21. 访问记录轻量化处理

内部身份用于流量归属/补传，界面隐藏设备维度。不逐连接同步写中央库，按时间桶、用户、节点、域名、动作、出口聚合连接数；跨分钟存活连接不重复算新连接，域名字节可空。

| 项目 | 默认值/规则 |
| --- | --- |
| 允许/阻断记录 | 30/90 天 |
| 小时/日流量 | 小时 90 天；日长期保留 |
| 本地 spool | 100 MiB（`DefaultLimit`）；容量 pragma 对每条 SQLite 连接生效 |
| 采样/上报 | 每 5 秒采样写 spool；面板下发 HeartbeatInterval=15 秒并批量上传 |
| Xray 原始访问行 | `/var/lib/proxy-agent/runtime/xray/access.log`，约 100 ms 轮询，读后/停核清空，20 MiB 截断兜底，避免保留 HTTP 路径/查询串 |
| sing-box 访问行 | 核心日志 |

离线缓存，重连补传，收到 Ack 才删。满时先淘汰最旧允许记录，优先保留阻断与流量。同批流量被面板连续拒绝 3 次则隔离、保留且不淘汰，后续批次继续；1 小时后或 Agent 重启重试。删除用户/旧设备前产生的批次按其修订与凭据墓碑归属，只累加仍存在的用户/身份。

**代理月流量**：按面板时区半开月区间汇总保留样本，`scope=retained_account_samples`。HourlyDays 清理后不能还原完整历史月份（L9），不得拼累计心跳/UTC 日桶冒充完整月份。

**核心/节点总量**：Hello/Welcome 双方协商 `node_traffic_supported`（面板须有 AccessRecorder），Agent 额外上传每核心累计 `traffic.Counter{kind:"node"}`；未协商则整批 FailedPrecondition。校验 core=xray/sing-box/gost、credential_id=node、epoch 后缀 .node、当前已确认修订、字节非负且 ≤1 PiB。

按（节点、epoch、node）保存游标、计算增量写 `core_traffic_samples`，批次幂等，多核心 epoch 互不淘汰。`node_traffic_samples` 仅在同 epoch 无 node 游标时回退账户样本，避免重复；总量用于今日/月节点与访问审计节点汇总，绝不进入用户额度，用户维度只取账户计数。官方 sing-box 无 with_v2ray_api 时用户未知。

Xray 总量=账户计数＋已渲染 forward 的原生入站计数。渲染生成 tag 白名单，额外查询 `inbound>>>`（Reset_=false），仅接受白名单精确 uplink/downlink，忽略用户入站/管理 API/其他入站，避免重算。sing-box 取核心总量（含转发）；旧 Agent 缺失转发字节不补记。forward 无用户身份，不进额度、设备计数或域名归属。

**GOST 统计**：沿用采集周期及停核前采样，从 `runtime/gost/metrics.sock` 经 Unix DialContext 读取 Prometheus 文本，仅求和已渲染 `fwd-<入站ID>-<端口>-<tcp|udp>` 的 `gost_service_transfer_input_bytes_total/output_bytes_total`（上传/下载）。范围的各服务只计一次，TCP/UDP 都计；丢弃 client/host 标签，不作身份。负数、非有限、溢出、标签/行损坏使整次采样失败并标统计源失败，不补零。计数随进程累计，每次应用从零开始并换 epoch；不提供每转发历史表/API。

`relay-<入站ID>-<端口>` 重复搬运入口已计的转发载荷，**不计节点总量**；无用户额度、设备限制、域名归属。

隧道规则入口/直接规则的转发字节只进核心/节点总量；中间与出口 relay 服务继续排除，避免重复计量。中转用户的账户字节在目标用户入站计入原额度，不从隧道或规则字节推算用户用量。

**整机月流量**：独立于代理统计、用户额度和订阅。心跳上报非 lo 网卡累计字节、bootId、IPv4 默认路由网卡，面板在同一事务按（服务器、网卡、本地日期）写差值到 `node_machine_traffic_daily`。首次采样、重启、计数回退只建基线；同次启动断线量可补，跨日按时间比例分摊，跨重启缺口不能补。

长期保留各网卡日行，摘要只选一张：显式 interfaceName，否则最近新鲜心跳的默认路由网卡；不同网卡不合并/拼接，改网卡重算所选网卡本期。周期按面板时区重置日 1–31 起算半开区间，短月夹到月末；sum=入＋出，outbound=出，max=本期两方向合计较大者。无本期日行/无法解析网卡为 null（“—”）；有日行而缺新鲜采样为 stale（“截至上次采样”）。

整机含所有服务，不等于代理量/供应商账单；LXC 只见容器网卡。IPv6-only/策略路由须手选网卡。改时区只影响之后的日桶，历史不重分桶。额度仅展示，不限速、不停服；设置/接口见第 27.4 节，备份见第 31 节。

# 22. 配置发布与回滚

草稿不直接覆盖运行配置：保存业务配置 → 生成 Revision → 校验用户/节点/能力 → DesiredState → Agent 编译 → 原生校验 → 保存/切换 → 健康检查 → 成功或自动回滚。

一条 Revision 覆盖全部核心，`core.json.enc` 按核心名保存 JSON；恢复从校验过的 DesiredState 重建，兼容旧单核心修订。所有活跃核心先校验，再按配置顺序切换，失败逆序补偿。每核 `runtime/<core>/` 隔离标记、accounting key、访问日志与临时文件，无启用入站则停止；旧 core_healthy 是逐核 AND，supervisor 分核恢复/退避。

**发布仍重启目标节点全部核心**，即使某核配置未变，所有用户既有连接会中断；崩溃恢复只影响异常核心。编译/校验失败不改配置，启动/健康失败回滚；中央失联保留成功配置。Agent 启动时核心不能运行保留修订，核心停止但 Agent 继续连接、上报不健康并等待新版本。默认保留最近 3～5 个成功版本。

续期后的证书由面板自动发布（第 27.16 节）：这同样是一次会重启该节点全部核心的发布，所以只在 Owner 设定的低峰窗口（`deployStartHour`–`deployEndHour`，面板时区，默认 3–6 点，结束小时不含）内进行。

## 22.0 服务器调优与推荐应用流程

调优白名单固定为 19 个 sysctl 键（`pkg/contracts/tuning`），禁止任意 shell、modprobe 或外部命令。root helper 直接操作 procfs 并原子写入 `/etc/sysctl.d/99-proxy-tuning.conf`，通过 `/etc/sysctl.d/.proxy-tuning-snapshot.json` 保存首次应用前的基线原值，失败自动回滚，清空调优恢复基线。当内核模块存在时，helper 允许将未直接加载的可用算法（如 `bbr`、`cake`）直接写入内核 procfs，并在回执的 `initial` 字典中返回完整 19 键初始值快照。

面板基于纯规则引擎（`internal/panel/node/tuningadvice`，`RulesVersion = "2026.11-1"`）为节点提供调优建议：综合评估节点虚拟化类型、可用及可加载算法、总内存大小、线路带宽（`node_meta.bandwidth_mbps`）、最近 24 小时探测 RTT 中位数、是否启用 QUIC/Hysteria2 入站以及是否承担中继角色，推荐最优拥塞控制算法、排队规则、缓冲上限及核心网络参数。每条建议具有级别（`suggested`/`optional`）与详细说明。

Owner/Viewer 可通过 `GET /api/v1/nodes/{id}/tuning/recommendations` 获取建议列表；Owner 可通过 `POST /api/v1/nodes/{id}/tuning/recommendations/dismiss` 记录不采纳某项建议（`{key, value}`），仅当该键推荐值变化时重新展示。系统设置支持可选的“新服务器上线后自动应用调优推荐”（`GET/PUT /api/v1/system/tuning-auto-apply`，Owner 权限，PUT 须 CSRF 与 `expectedRevision`）；开启后，节点首次上报能力且尚无任何 `node_tuning` 请求时，面板以 `auto` 操作者自动下发推荐 profile。

Owner＋CSRF 提交完整 settings/clear 和匹配 confirmNodeId → 校验白名单/边界 → 事务保存 node_tuning 唯一 request_id、期望 profile、pending、审计“修改服务器调优” → Connect 下发 TuningCommand。每节点最多一个 pending（409 tuning_busy）；缺能力 409 tuning_unsupported；禁用/离线/心跳过期 409 node_offline。

等待 helper 时仍收消息/Ack。TuningResult 须匹配认证 node_id/request_id，事务保存逐键结果再 Ack；相同结果幂等，冲突拒绝。断流/重连把 pending 改 unconfirmed，不重发或换 ID 重试；不确认不代表未执行。依据后续 tuningCurrent/运维检查，再由 Owner 发起新操作。恢复见第 31 节，续证见第 29.2 节。

## 22.1 自动额度版本

流量入库/定时 tick 判定用户（禁用 > 超限 > 到期），状态变化按节点排队，60 秒内合并；以**已发布状态**为基线只替换用户访问面，创建描述为 `auto: user <name> <reason>` 的 Revision。

- 唯一草稿例外是当前用户/入站绑定，未发布的绑定可能被带出；中转凭据、出站、出口组、链、规则均保留已发布状态。
- 启动先播种状态机，不因重启全量发布；随后对已有发布状态的节点对账，仅不一致者排队。读取失败每 tick 重试，直到完整读完。
- 忙碌/等 Agent 时等下窗口；读/建/部署失败或 Agent 失败/回滚时，下窗口按最新已发布状态重建。连续 3 次失败停止重试，以 system.alert 和“自动额度发布失败”日志告警。草稿不能渲染则回退基线、只移除不活跃用户，恢复访问须等草稿可渲染。
- 启用意图来自基线 `config_revisions.inbound_enabled_json`，不是草稿；无凭据渲染为 disabled。凭据恢复时只有基线意图含该入站才启用，Owner 已禁用或历史 NULL 均保持禁用直至手工发布；凭据仍可挂禁用入站。
- 目标为当前授权节点＋已发布状态仍持凭据的节点。删除用户/旧设备也排队（deleted/device deleted、user_access 告警），渲染不再带被删凭据，一致则不发版。
- 每次部署/回滚成功排一次对账，纠正旧修订带回禁用用户或漏掉已启用用户；同样合并 60 秒，一致不发版。
- 自动版本仍重启节点全部核心；中央离线继续旧配置直到重连。渲染使用同份草稿快照，入站 core 原样继承（空串随主核心），若核心不可用/添加移除中则失败（core selection pending）并按上述重试，不能静默搬移凭据。

## 22.2 核心更新与移除

核心更新独立于配置发布：Owner 提交组件、稳定版本、白名单 GitHub URL、归档 SHA-256 → 面板校验平台/版本/来源/摘要 → Agent 再校验 URL/重定向主机/摘要 → 解包（拒绝链接、穿越、多余候选）→ 原生版本/健康校验与切换，失败回滚。无可信摘要不安装。缺失组件走同一 schema-2 请求安装启用，空闲核心不启动；每组件有 worker、staging、认证 release hold 与独立历史，重新添加使用新历史命名空间并保留反回滚序号检查。

- 版本、地址与摘要可由 Owner 在「代理核心」的对话框里选版本自动填写（`GET /api/v1/core-releases`，Owner-only，见第 27.9 节）：按（组件、来源）列最近 5 个正式版，按平台给出官方资产 URL 与厂商公布的归档摘要。摘要只读对方公布处，面板从不自行计算：Xray 取发布页旁 `<asset>.dgst` 的 SHA2-256，官方 sing-box 取 GitHub 资产摘要，qui 构建取我们自己的 `SHA256SUMS`，GOST 取 `pkg/contracts/release` 里已审阅的固定值（不发请求）。同一资产有两处公布值时必须一致，否则该项摘要留空由 Owner 手动粘贴；资产没有发布时间或为空时同样留空。提交仍走同一 `POST /api/v1/core-updates`，面板与 Agent 的校验链不变，选版本只是省去抄写。
- 每台服务器按自身架构与 C 库取资产（Alpine 用官方 sing-box 的 `-musl` 归档，其余用通用归档；qui 构建是静态单包），没有匹配资产时不猜；批量升级按每台各自解析，`uiMetadata` 之外的表格值仍是一次提交里的同一版本。Owner 手动改过版本、地址或摘要后，表单按所填写内容提交，不再按发布信息解析；手输版本会清空摘要，地址能按官方命名规则重建就重建（Xray、官方 sing-box），不能重建（qui 构建在面板 tag 下、GOST 单一固定归档、架构未知）就清空。
- 查询失败（网络、限流、上游非 200、内容不可解析、超过 30 秒）一律 502 `core_release_lookup_failed`，不回显上游状态、正文或链接；对话框给出固定提示并保留已填内容，Owner 可点「刷新」重试或全部手填。列表结果缓存 10 分钟（`refresh=1` 跳过），失败不缓存；GOST 完全在本地作答。一次冷查询的上游开销：Xray 为 1 次 releases 列表加最多 5 个正式版 × 2 个资产共 10 次 `.dgst`，qui 为 1 次列表加 5 次 `SHA256SUMS`，官方 sing-box 只用 1 次列表（`.dgst`／`SHA256SUMS` 走 github.com 下载路径，列表走 `api.github.com` 且受其匿名限流，按面板主机 IP 计）；被限流时表现为上面的固定失败，不影响任务本身。

- Xray 只收官方前缀；sing-box 也收精确 `sing-box-<版本>-qui.<序号>-linux-<架构>.tar.gz` 的 qui 归档，面板/Agent 双检，旧 Agent 未声明能力不能排队 qui 来源。
- GOST 仅 linux/amd64、3.3.2、`v3.3.2/gost_3.3.2_linux_amd64.tar.gz`，前缀 `https://github.com/go-gost/gost/releases/download/`，固定**归档**摘要 `162feb43…a9d18274`（非二进制摘要）。下划线资产不套通用连字符匹配。共享规则只定义在 `pkg/contracts/release` 的 CoreAssetURL/CoreAssetMatches/CorePinnedTarget；改版本须审阅固定值并重跑真实核回归。安装缺失 GOST 另须当前 Hello.gost_core_supported；nodes 对应位恢复清零、重连刷新，多核心能力不能替代该位。
- Agent 自更新使用配置固定 Ed25519 公钥、签名清单与安装历史防重放，失败回滚确认镜像；核心不用签名目录，也无旧 receipt/outcome journal 路径。

任务按（节点、组件）唯一活跃，sequence 按各自历史递增；不同组件可同时排队。PendingCoreUpdate 先派 queued，再重发 running 同一身份，断线不换 ID。POST /core-updates 必填 component，回执信封 component 必须匹配，重复幂等、变更结果拒绝。卡住任务重连关闭，也可 Owner 清理。

更新回执在 worker **内存**队列按先后保留至 Ack，断流下会话重发。面板连续 3 次以 FailedPrecondition＋core update result rejected 永久拒绝同结果则丢弃并 Warn；网络/Unavailable/取消永不丢弃。Agent 重启丢失内存队列，由重连关闭/Owner 清理兜底。

success 仅证明镜像切换；须等 Capabilities.cores 出现组件才允许选择。进行中列入 core_selectionBlocked，保存/草稿渲染/发布拒绝 core_pending，不伪造能力。

移除使用 core_remove_tasks：Owner＋CSRF＋confirmNodeId＋当前确认修订 → POST /nodes/{id}/cores/{component}/remove，审计 remove_core → RemoveCoreCommand{id,component,revision} → 幂等保存 RemoveCoreResult/Ack。断线重发同身份，更晚修订已确认也接受迟到重放；移除回执**持久保存**至 Ack。

| 拒绝码 | 条件 |
| --- | --- |
| core_in_use | 草稿/已发布含该组件入站，禁用也算；返回 inboundIds；Agent 还检查中断待定修订 |
| core_pending | 组件添加/移除或部署占用 |
| last_core | 不得移除最后一个 Xray/sing-box；GOST 不可作主核心 |
| revision_mismatch | 提交修订不是当前已确认修订 |

移除顺序：确认退出 → 原子持久禁用清单 → 移除运行实例 → 删 runtime/<组件>/ 与自管镜像。标记/accounting key 是无残留进程的证据，ErrCoreStopUnconfirmed 时目录、清单、镜像全保留；启动恢复中断移除也须 Close 确认。`cores.json.enc`、更新历史、安装器外部二进制/摘要记录保留，Agent 不改安装器文件。重加组件重新启用并建新运行目录；操作后刷新能力，确认前仍显示组件。

回滚到已移除核心的历史修订在面板拒绝（`rollback refused: … core "sing-box" is unavailable`），先重装组件或发显式核心的新修订。

# 23. Node Agent 模块设计

模块：Identity（身份/证书）、Control（连接/心跳/重连/Ack）、Capability（系统/核心探测）、Reconciler（期望协调）、Compiler（Xray/sing-box/GOST）、Supervisor（进程/健康）、Collector（统计/域名）、Spool（持久缓存）、Updater（Agent/核心）。GOST 持久化 JSON，执行前生成 YAML。

更新方式见第 22.2 节；qui sing-box 仅为未修改上游加上游标签。受管 Agent 启动按安装历史完成中断切换，不使用旧候选确认恢复或 supervisor 巡检模式。 v1.5.0 起支持面板托管的官方签名 Agent 一键升级（详见 27.9、28 与 29.2 节）。
新装节点仅在安装命令对话框勾选「启用网页升级」（命令追加 `--web-upgrade`，默认不勾选）时由 node-install.sh 启用网页升级，否则在节点本机运行一次 `qui-update --component agent` 启用；已有节点在服务运行时执行 `qui-update --component agent`（从 ≤1.4.7 升级需执行两次）启用，面板 Agent 管理页出现“升级”按钮即已启用。

**禁止**任意 Shell、Web Terminal、任意命令、上传脚本执行与未定义远程操作。只允许：应用/回滚版本、重启/重载核心、健康检查、证书轮换、Agent/核心更新与核心移除，以及以下固定操作：

- 强制出站源地址：固定 nft 规则，独立 oneshot，仅 CAP_NET_ADMIN。
- 端口跳跃：同一个 host-network helper 额外维护 `inet proxy_agent_hop` 一张表（prerouting，`udp dport <start>-<end> redirect to :<port>`），整表替换、无跳跃入站时删除；仍是同一个固定 oneshot、仅 CAP_NET_ADMIN，不新增单元，也不引入任意命令。
- 调优/清除：19 个白名单 sysctl 键及边界；root helper 直接写 procfs、读回校验、原子保存 99-proxy-tuning.conf，失败恢复原值，clear 恢复首次快照；无 shell/任意 sysctl（第 22.0、30 节）。
- TTL 设备门控：Xray 官方运行时规则 API / sing-box Clash 关连接，不发 Revision/不重启。
- TCP-connect：只接受已解析 IP、端口、次数、超时、截止时间，Agent 再检，最多 10 次、每次 3 秒，无 shell。
- 转发/中继：正常发布，旧能力拒绝；Xray dokodemo-door、sing-box direct override、GOST tcp/udp。无用户身份，不进订阅/额度/设备限制/域名归属。

GOST 目标按序写 forwarder.nodes（target-0…），selector={strategy,maxFails,failTimeout}；自行展开每端口/传输服务，addr 无范围。限速为 limiters[{name,limits:["$ <in> <out>"]}]＋service.limiter，连接上限为 climiters[{name,limits:["$ <n>"]}]＋service.climiter。ParseBase2Bytes 不收裸整数，速率写 `<整数字节>B`，唯 0 表不限；typed 输出校验引用/目标，避免未知 limiter 静默不限速。

GOST 出口为 handler:relay＋listener:tls＋客户端 CA；入口链挂 handler.chain，connector:relay＋dialer:tls（CA/服务器名）。证书材料按入站/半边写 `runtime/gost/tls/<入站>/<半边>/`，目录 0700、文件 0600，旧修订回滚不覆盖在用证书。

GOST 当前上报契约 4，允许中间 relay listener 携带一个 `gost_hops` 下一跳，链挂 handler.chain；前段监听与后段拨号证书分别保存，中间与出口 relay 不计节点总量。旧契约 3 的入口链（最多两个原生 hop）和出口渲染兼容。隧道管理限定入口→可选单个中间→出口（最多三台不同服务器），发布前按每跳上报能力校验两节点 ≥3、三节点 ≥4，不从二进制版本推断。TLS relay 无生产 UDP 监听，UDP 由 TLS/TCP 封装至出口再发往目标；健康只测进程与监听，不代表握手/目标可达。运行证据见 [X0 记录](docs/v1.4-x0-findings.md)，A1 结果待补。

**TCP 探测**只测建连耗时/失败率，不是带宽、ICMP、UDP/QUIC 可用性。手动历史每节点/目标最多 100 条；定时每分钟测启用未删除、心跳 ≤45 秒且支持的节点，每目标 3×3000 ms。默认目标用 probe_targets.scheduled（最多 5）；节点可自定义最多 5 个，custom=true＋空表=不测，false=清空自定义并继承。队列按 source 隔离、手动优先，在途结束或 2 分钟到期才派下一条。

定时结果 ValidateResult 后入独立采样表，保留 7 天。当前 Agent 仅 medianMs；原始 avg/min/max 为 null。≤12 小时返回原点，>12 小时按 Unix epoch 分桶，步长 ceil(小时×60/720)×60 秒，每目标约 720 点（边界可多 1）。桶 medianMs 为 sent>failed 且非 NULL 中位数的算术平均，min/max 是这些中位数极值，无值均 null，avgMs 恒 null；sent/failed 求和，samples 为样本数（原点 1）。删目标级联样本，逻辑删节点停止查询、等保留清理。API 见第 27.12 节。

**健康与恢复**：Health 每次即时查监听（心跳、手工检查、就绪、发布核对、更新激活不变）。巡检每 3 秒，仅 ErrListenerUnavailable 连续 3 次才恢复；退出、未确认停止、路由恢复失败或无原因不健康立即恢复。失败退避，成功/修订变化/组件更新重置计数。分核判断，全健康不再聚合二次采样，单次拨号失败不能重启仍服务的核心。

GOST 健康=确切子进程存活＋每个生产 TCP 可拨＋每个生产 UDP socket 属该进程；失败报 ErrListenerUnavailable 并列监听。指标失败只标统计源失败，目标可达性不属核心健康，须另做探测/真实流量验证。

# 24. 中央端模块设计

中央控制端后端模块如下：

```text
auth          管理员登录和权限
user          用户、隐藏接入身份和凭据
portal        终端用户登录、独立会话和本人数据
node          节点、节点组和节点身份
subscription  用户订阅生成
proxy         入站、出站、出口组和代理链
policy        分流、服务规则和网站控制
deployment    配置编译、发布和回滚
access        域名访问记录和流量统计
dashboard     总览聚合查询
connections   面板内存中的实时连接快照（不落库）
system        设置、备份、版本和操作日志
```

推荐模块目录结构：

```text
internal/panel/user/
├─ model.go
├─ service.go
├─ repository.go
├─ repository_sqlite.go
├─ handler_http.go
├─ dto.go
└─ errors.go
```

模块化开发约束：

1. 每个模块只能通过自己的 Repository 修改所属数据。
2. 其他模块不能直接修改该模块的数据表。
3. 跨模块操作通过接口或应用编排服务完成。
4. 协议配置必须经过类型化结构校验。
5. 不允许直接把未经校验的 JSON 下发给节点。
6. Dashboard 只负责查询和聚合，不直接修改业务数据。
7. 配置发布必须统一经过 Deployment 模块。

# 25. 前端模块设计

前端按照业务模块组织：

```text
web/src/
├─ app/
│  ├─ router.ts
│  ├─ auth.ts
│  └─ api.ts
│
├─ modules/
│  ├─ auth/
│  ├─ dashboard/
│  ├─ users/
│  ├─ nodes/
│  ├─ proxy/
│  ├─ routing/
│  ├─ access/
│  └─ system/
│
├─ components/
├─ layouts/
├─ types/
└─ styles/
```

每个业务模块包含：

```text
pages/         页面组件
components/    模块内组件（按需）
api.ts         模块接口封装
```

路由统一在 `web/src/app/router.ts` 集中注册，共享类型在 `web/src/types/index.ts`。

前端轻量化要求：

不使用服务端渲染；生产环境不运行 Node.js；页面按路由懒加载；只保留少量全局状态；列表使用服务器分页；不一次加载全部访问记录；实时状态使用一条 SSE 连接；不为普通 CRUD 使用 WebSocket；通用表格、状态标签和确认弹窗统一复用。

# 26. 数据库设计

中央数据库使用 SQLite。

建议数据库路径：

```text
/var/lib/proxy-panel/panel.db
```

SQLite 仅运行在中央服务器本地磁盘，不放在网络共享目录。

主要数据表如下。

## 26.1 管理员

```text
admins
sessions
admin_operations
```

## 26.2 用户与设备

`devices` 只保存每用户一个隐藏接入身份；不再提供设备管理。

```text
users
devices
credentials
subscription_tokens
```

## 26.3 节点与节点组

```text
nodes
node_identities
node_groups
node_group_members
user_node_group_grants
```

## 26.4 代理配置

```text
inbounds
outbounds
outbound_groups
outbound_group_members
proxy_chains
proxy_chain_hops
```

## 26.5 分流与网站控制

```text
policies
service_sets
service_set_entries
custom_domain_entries
```

## 26.6 配置发布

```text
config_revisions
deployment_targets
```

## 26.7 访问记录和流量

```text
access_rollups
traffic_hourly
traffic_daily
node_status_snapshots
```

## 26.8 系统

```text
system_settings
backups
```

协议配置使用密封 JSON（encrypted_settings），策略/能力使用结构化 JSON，例如：

```text
inbounds.encrypted_settings
outbounds.encrypted_settings
policies.scope_json
policies.match_json
policies.action_json
nodes.capabilities_json
```

JSON 字段必须在 Go 中解析为明确的数据结构，不允许无结构透传。

## 26.9 迁移与兼容规则

启动及恢复在 0124 认领后运行 0131 的一次性 `ReconcileCanvasForwards`，同一事务转换已发布且归属完整的画布端口转发。普通转发变规则，GOST 转发变两节点隧道及规则；原入站 ID、端口、设置与密封凭据不改，监听为 manual，订阅默认隐藏。目标公网地址与成功运行修订中的用户入站端口唯一匹配且与当前行一致才 linked，否则保留 manual。转换连接从布局移除；无法确定的连接保留，Owner 可读 migration-report 的中文原因。标记保存已迁移 ID，首次无修改发布保留旧物化形态；完成后不自动重试，事务失败可重试，不重新按名称认领。

共享隧道迁移（仅在升级/恢复数据库时应用）：

| 迁移 | 状态 |
| --- | --- |
| 0124 `canvas_tunnels` | canvas_tunnels（严格类型连接 JSON、端点引用、稳定端口、版本标记）、canvas_tunnel_objects（对象唯一归属）、canvas_tunnel_publications（待清理发布关联）、一次性 reconcile 标记；服务端布局增加 tunnelId，客户端保持内嵌；备份清单/验证与恢复覆盖，旧对象只按已发布、精确当前名称、无歧义规则认领 |
| 0125 `multi_core_panel` | `core_update_tasks.adding`（0007/0107 已按签名目录回填 component，本迁移不动既有行）；唯一索引由“每节点一个活跃任务”改为 `(node_id,component)`；`inbounds.core`（空/xray/sing-box，空＝当前主核心；gost 由 0127 扩展）并删除旧 `inbound_active_port`（端口唯一改由面板按 Agent 的节点级数字端口规则跨核心与转发校验）；`nodes.multi_core_supported/node_traffic_supported`；`node_health_checks.core`、`node_restart_requests.core`；`core_remove_tasks`（每（节点、组件）一个活跃移除任务、幂等回执）；`node_traffic_cursors`（每（节点、epoch、`node`）的累计游标）、`core_traffic_samples`（每核心观察增量）与去重视图 `node_traffic_samples` |

| 迁移 | 实际存储变化 |
| --- | --- |
| 0114 `end_user_accounts` | `users.password_hash/login_enabled/last_login_at`；独立 `user_sessions` 与过期索引；`subscription_tokens.encrypted_token` |
| 0115 `device_limit` | `users.device_limit`（0–100，默认 0）；源地址与门控租约不落库 |
| 0116 `subscription_sources` | `subscription_sources`、`imported_proxies`、`user_source_grants`；`canvas_schemes.client_published_layout` |
| 0117 `node_meta` | `node_meta`：地区、备注、续费日期/周期/价格/币种、提醒天数与每日提醒标记 |
| 0118 `traffic_reset` | `users.traffic_reset_cycle/traffic_reset_anchor/traffic_reset_at`，从旧备注迁移周期并去除元数据前缀 |
| 0119 `tcp_probes` | `nodes.tcp_probe_supported`；`probe_targets`、`node_tcp_probes` 与历史索引；转发复用入站设置，不新增转发表 |
| 0120 `latency_samples` | `probe_targets.scheduled`（默认关闭，触发器限制最多 5 个）；`node_tcp_probes.source/dispatched_at` 与单节点在途唯一索引；`node_latency_samples`（REAL 毫秒，含 median_ms；按节点/时间索引，保留 7 天） |
| 0121 `node_latency_targets` | 每节点目标关联表（节点、目标外键级联删除，插入及移动触发器限制每节点最多 5 个）；`nodes.latency_targets_custom`（0/1，默认 0） |
| 0122 `node_group_entries` | `node_group_inbounds`、`node_group_imports`；服务器成员转入站、旧来源授权转生成分组；`node_meta.port_range_start/end`（同时为空或 1–65535 的有序范围），迁移备注末尾有效 `[ports:A-B]` |
| 0123 `node_metrics_tuning` | `node_metric_samples`：可空分钟遥测、7 天保留、不进便携备份；`node_tuning`：完整期望 profile、request_id、状态与逐键回执，随备份保留，恢复以当前状态为准且不重发 |
| 0126 `node_machine_traffic` | `node_machine_traffic_settings`（每服务器 method/interface_name/可空 allowance_bytes 1–2^53-1/reset_day 1–31，默认 sum/''/NULL/1）、`node_machine_traffic_daily`（主键（服务器、网卡、本地日期），非负 rx/tx 字节，长期保留，非遥测）、`node_machine_traffic_cursors`（每（服务器、网卡）的 boot_id 与累计字节，用于增量）；无时区列，按写入时的面板时区分日（第 21 节） |
| 0127 `gost_core` | 五表核心 CHECK 接受 gost：inbounds.core（空/xray/sing-box/gost），core_update_tasks、core_remove_tasks、node_traffic_cursors、core_traffic_samples；保留 ID/外键/归属触发器、adding、活跃任务唯一索引和 core_traffic_time，node_traffic_samples 视图语义不变。nodes.gost_core_supported 为 0/1，默认 0，恢复清零/重连刷新 |
| 0128 `gost_tunnels` | `canvas_tunnels.gost_material`（可空密封列；每条隧道一套 CA ＋ 服务端/客户端叶子证书，CA 私钥签发后丢弃、有效期 10 年；轮换替换整列并更新 `updated_at`）。不新增表，隧道归属表、外键与触发器不变 |
| 0129 `tunnels` | `tunnels`（密封材料、草稿/发布标记）、有序 `tunnel_hops`、`tunnel_rules`、节点数字端口唯一的 `port_claims`、ID 归属 `tunnel_objects`；目标公网地址/入站端口变化触发关联规则草稿失效 |
| 0130 `relay_subscription_snapshot` | `tunnel_rules.published_snapshot`：无凭据的成功发布端点快照；旧发布不回填猜测地址，须再次成功发布 |
| 0131 `canvas_forward_migration` | `canvas_forward_reconcile` 单次标记及 migrated_ids、`tunnel_migration_report` 持久原因；启动/恢复事务转换合格画布转发，保留运行对象与凭据 |
| 0133 `subscription_source_results` | `subscription_sources.latest_result_json`（有界刷新结果）与 `last_success_at`；默认空串，旧来源与旧归档读出“未记录原因”，迁移不抓取订阅 |
| 0134 `node_identity_recovery` | `node_enrollment_tokens.purpose`（`enroll`/`recover`，默认 `enroll`）与 `bound_fingerprint`；`node_identity_recoveries`：每次恢复一行（token 哈希、旧/新指纹、签发人、状态、`redeploy_state`/`redeploy_revision`/`redeploy_code`，每节点最多一条 `issued`）；`node_identity_retirements`：被恢复替换的指纹永久拒绝，签发不重签证书，核销由 Enroll 完成 |
| 0135 `node_connect_address` | `nodes.public_ip_manual`（0/1，默认 0）与 `nodes.connect_host`（客户端连接地址，默认空串）；迁移把既有 `public_ip<>''` 的行回填为手工，因为此前只有 Owner 能写该列。心跳只在不手工时填 `public_ip`，语义见第 11.1 节 |
| 0136 `inbound_tls_source` | `inbounds.tls_source`（`''`/`paste`/`self_signed`/`file`/`acme`，默认 `''`，CHECK 限定取值）；`''` 表示 0136 之前的行或不带证书的入站。证书本身在密封列里，SQL 无法据此回填，历史行在读取时按「有证书即 `paste`」解析（第 12 节）；`acme` 引用证书库（`certificate_id`，0137） |
| 0137 `certificates` | `certificates`：域名 JSON（1–10 个，可带一个前导 `*.`）、状态（`pending`/`issuing`/`valid`/`failed`）、密封的证书与私钥（`{certificate_pem, private_key_pem}`，首次签发前为 NULL）、签发与到期时间、失败码、上次/下次尝试时间、尝试次数、`auto_dns` 与 `dns_node_id`；`inbounds.certificate_id` 引用证书行、默认 NULL，D2 才写入。域名格式、状态集合、错误码与密封材料可解密由面板与归档校验共同约束 |
| 0138 `node_public_addresses` | `nodes.public_addresses_json`（文本 JSON，默认 `[]`，严格校验最多 16 项全局单播地址列表）与 `nodes.public_ip_selected`（文本，默认 `''`）。接口与心跳上报联动，设置支持选择候选公网 IP |
| 0139 `notifications` | 4 张表：`notification_rules`（7 类默认规则、阈值、持续窗口、冷却分钟及 CHECK 约束）、`notification_channels`（AES-256-GCM 密封配置、末 4 位摘要、投递状态与时间）、`notifications`（通知记录，90 天保留自动清理）、`notification_state`（去重状态与冷却追踪） |
| 0140 `ruleset_sources` | `service_sets` / `service_set_entries` 增加 `keyword` 匹配类型支持；新增 `service_set_sources` 表（存储外部规则源 URL、格式 yaml/text/mrs、行为 classical/domain/ipcidr、刷新周期、运行状态及密封缓存） |
| 0141 `tuning_recommendations` | 新增 `node_tuning_dismissals` 表（主键 `(node_id, key)`，列 `key` / `value` 记录不采纳的参数与推荐值，另存规则版本与时间，级联删除）；`node_meta` 扩充可空整数 `bandwidth_mbps`（1–100000 Mbps）；`system_settings` 新增 `tuning_auto_apply` 开关 |

备份校验、清单与恢复覆盖这些结构。恢复结束门户会话；当前安全状态、Token 与授权按模块的保守合并规则处理，不用旧备份恢复已撤销访问。排队探测在恢复时过期，运行能力待重新上报。

# 27. API

API 统一使用：

```text
/api/v1
```

## 27.1 身份认证

```text
POST /api/v1/auth/login
POST /api/v1/auth/logout
GET  /api/v1/auth/session
PUT  /api/v1/auth/password
```

## 27.2 用户

```text
GET    /api/v1/users
POST   /api/v1/users
GET    /api/v1/users/{id}
PUT    /api/v1/users/{id}
DELETE /api/v1/users/{id}
```

## 27.3 节点组授权与移除的设备 API

```text
POST   /api/v1/users/{id}/node-groups/{groupId}
DELETE /api/v1/users/{id}/node-groups/{groupId}
```

旧设备路由返回 410 `devices_removed`；订阅 Token 请求携带 `deviceId` 同样拒绝。用户 API 不接受旧的最大设备数/最大连接数字段，使用 `deviceLimit`。

## 27.4 节点

```text
GET    /api/v1/nodes
GET    /api/v1/nodes?deleted=1
GET    /api/v1/nodes/{id}
GET    /api/v1/nodes/{id}/metrics?hours=1..168
GET    /api/v1/nodes/{id}/tuning
PUT    /api/v1/nodes/{id}/tuning
GET    /api/v1/nodes/{id}/tuning/recommendations
POST   /api/v1/nodes/{id}/tuning/recommendations/dismiss
GET    /api/v1/system/tuning-auto-apply
PUT    /api/v1/system/tuning-auto-apply
GET    /api/v1/nodes/machine-traffic
GET    /api/v1/nodes/{id}/machine-traffic
GET    /api/v1/nodes/{id}/machine-traffic/settings
PUT    /api/v1/nodes/{id}/machine-traffic/settings
GET    /api/v1/nodes/{id}/deletion-preview
POST   /api/v1/nodes/enrollment-token
POST   /api/v1/nodes/{id}/recovery-token
GET    /api/v1/nodes/{id}/recovery-status
PUT    /api/v1/nodes/{id}
DELETE /api/v1/nodes/{id}
GET    /api/v1/nodes/{id}/certificate-matches?domain=<域名>
```

`GET /api/v1/nodes` 默认只返回未删除的节点；`?deleted=1` 只返回已删除的节点（Owner 与 Viewer 都可读）。`DELETE` 需要 Owner：未登记节点等同取消登记（记录移除），已登记节点执行逻辑删除，被引用时返回 409 `node_delete_blocked`，阻塞项（`blockers: [{kind,id,name}]`）位于错误体中（见第 11.3 节）。

节点对象含 `publicIp`、`publicIpSource`（`manual`/`selected`/`auto`/空）、`publicAddresses` 候选数组、`publicIpSelected`、`publicIpSelectionMissing` 与 `connectHost`，含义与优先级见第 11.1 节。`PUT /api/v1/nodes/{id}` 的 `publicIp`、`publicIpSelected` 与 `connectHost` 都是可选字段（`publicIpSelected` 必须是候选列表中存在的合法地址，或空串表示回到自动推导）；`POST /api/v1/nodes/enrollment-token` 支持在生成注册码时传入可选的 `connectHost`：省略（或 JSON null）保留原值；`publicIp` 写非空 IP 记为手工并停用自动填写，写空串清除并恢复自动；`connectHost` 接受空串、IPv4/IPv6 字面量或域名（复用 `install_endpoint.host` 的 `^[A-Za-z0-9.-]+$` 与逐标签规则，落在 `netaddr.ValidHostName`），不接受协议、端口与方括号，非法值返回 400 `invalid_request`。保存仍记“修改节点”操作日志并要求 CSRF。

原位身份恢复：`POST /api/v1/nodes/{id}/recovery-token`（Owner＋CSRF，体 `{expiresInSeconds,revokeNow}`，0 或省略为 900 秒、范围 300–3600）201 返回只出现一次的 `enrollmentToken`、`expiresAt`、`install` 与 `purpose:"recover"`；`GET /api/v1/nodes/{id}/recovery-status`（Owner）返回最近一条恢复记录，无记录为 404。未登记、禁用或已删除节点返回 409 `node_not_enrolled`，存在未完成任务返回 409 `node_recovery_blocked`，`blockers:[{kind,id,name}]` 在错误体中（见第 11.3 节）。

`GET /install/ca.pem` 无需登录，返回面板 Web 证书链顶层的 PEM（`application/x-pem-file`，`Cache-Control: no-store`），供节点安装脚本建立对面板证书的信任（见第 11.3 节）；开发模式或证书不可读时返回 404。

`GET /api/v1/nodes/{id}/certificate-matches?domain=<域名>`（Owner；v1.5.0 §6）：按域名返回该节点可用的证书候选 `{domain, inventorySupported, nodeFiles, library, cloudflareConfigured}`。`nodeFiles` 只列覆盖该域名、带配对私钥的节点证书文件，字段是路径、DNS 名称、到期时间与是否过期，**绝不上报私钥内容**；按「未过期在前、到期晚的在前」排序。`library` 是证书库里已签发且覆盖该域名的证书。`inventorySupported` 为假（旧 Agent）时仍返回 `library` 与 `cloudflareConfigured`，编辑器据此只显示证书库与申请路径。域名用 `netaddr.ValidHostName` 校验，非法 400 `invalid_request`，已删除节点 404。

metrics 与 tuning GET 均允许 Owner/Viewer；逻辑删除节点返回 404。metrics 默认 hours=24，合法范围 1–168；≤12 小时原始分钟点，>12 小时 epoch 对齐，intervalSeconds=ceil(hours×60/720)×60，sampleSeconds=60。响应 {intervalSeconds,sampleSeconds,points}；空 points 为 []，无测量字段为 null。桶内各字段对非 null 值取算术平均，计数/字节可能为小数，samples 为桶内行数。指标字段见第 11.2 节。

tuning PUT 请求 {settings:对象,clear:布尔,confirmNodeId:节点ID}，拒绝未知 JSON 字段；202 返回 {requestId,status:"pending"}。settings 为完整字符串 profile，空 profile 等同 clear；clear=true 不得带非空 settings。GET 返回 supported、desired、requestId、status、result、requestedAt、updatedAt、current；未操作时 ID/结果/时间为 null、期望为空对象、status=unconfirmed。current 是最后心跳实际读数，可能过期。PUT 权限、并发和错误码见第 22.0 节。

调优建议接口：`GET /api/v1/nodes/{id}/tuning/recommendations`（Owner/Viewer）返回建议规则版本与建议项列表；`POST /api/v1/nodes/{id}/tuning/recommendations/dismiss`（Owner＋CSRF，体 `{key, value}`，操作日志“不采纳调优建议”）；`GET/PUT /api/v1/system/tuning-auto-apply`（Owner；PUT 须 CSRF 与 `expectedRevision`，体 `{enabled, expectedRevision}`，操作日志“修改调优自动应用”）。节点元数据支持可选整数 `bandwidthMbps`（1–100000），供调优建议与线路显示使用。

整机流量：GET settings 与两个摘要接口允许 Owner/Viewer；PUT settings 仅 Owner＋CSRF；逻辑删除节点返回 404，字段/设置非法返回 400 `invalid_request`。settings 为 `{method:"sum"|"outbound"|"max", interfaceName, allowanceBytes, resetDay}` 的完整替换，PUT 必须带全部四个字段（含显式 nullable allowance，1–9007199254740991 的整数字节或 null），未知字段拒绝，并写入操作日志“服务器流量设置”。摘要 `{nodeId, settings, interfaceName, timezone, periodStart, periodEnd, nextResetAt, rxBytes, txBytes, usedBytes, status, lastObservedAt}`，字节为十进制字符串，未知为 null，status 为 measured/stale/unknown；批量接口 `{items:[摘要],timezone}` 一次返回全部未删除节点，服务器列表只用它，详情用单节点接口。口径与限制见第 21 节。

## 27.5 节点组

Owner 专用，写操作要求 CSRF。列表及保存响应保留原字段，新增 `inboundIds: string[]`、`imports: [{sourceId,proxyKey}]`、`grantedUserIds: string[]`；`nodeIds` 是只读派生服务器列表。POST/PUT 接受 `inboundIds`、`imports`（最多 500 个入站、200 个导入成员）；仅传 `nodeIds` 而未传 `inboundIds` 时，兼容展开服务器当前订阅可见、非中转的入站。显式空入站数组优先，未知字段仍拒绝。

导入来源 API 继续接受但忽略 `userIds`；响应 `userIds` 为经有效分组可达该来源的去重用户集合（不代表其当前活跃或缓存可用）。`GET /imported-proxies` 项增加 `proxyKey: "name|protocol"`。`user_source_grants` 仅保留供旧备份兼容，不参与访问判断。

节点元数据 GET/PUT 增加可空整数 `portRangeStart`、`portRangeEnd`，须同时设置或同时为空；无效输入返回 400 `invalid_request`。画布未指定中转监听端口时优先在服务器范围内分配，按数字端口跳过该节点上任何已配置入站（含禁用）与本次计划已占用的端口，不区分 TCP/UDP（与 Agent 的节点级数字端口规则一致）；范围耗尽拒绝发布，未设置范围沿用原默认端口。

```text
GET    /api/v1/node-groups
POST   /api/v1/node-groups
PUT    /api/v1/node-groups/{id}
DELETE /api/v1/node-groups/{id}
```

## 27.6 代理配置

```text
GET/POST/PUT/DELETE /api/v1/inbounds
GET/POST/PUT/DELETE /api/v1/outbounds
GET/POST/PUT/DELETE /api/v1/outbound-groups
GET/POST/PUT/DELETE /api/v1/proxy-chains
```

## 27.7 分流

```text
GET/POST/PUT/DELETE /api/v1/policies
GET/POST/PUT/DELETE /api/v1/service-sets
GET                 /api/v1/service-sets/{id}/references
POST                /api/v1/service-sets/{id}/sources/{sourceId}/refresh
POST                /api/v1/policies/simulate
```

## 27.8 访问记录

```text
GET /api/v1/access-records
GET /api/v1/access-records/blocked
GET /api/v1/access-records/domains
GET /api/v1/traffic/summary
```

## 27.9 配置版本

```text
GET  /api/v1/revisions
POST /api/v1/revisions
POST /api/v1/revisions/{id}/deploy
POST /api/v1/nodes/{id}/rollback
```

每节点、每核心操作：

```text
GET  /api/v1/nodes/{id}/restart-core            # 列表项含 core
POST /api/v1/nodes/{id}/restart-core            # {expectedRevision,confirmNodeId,core?}；core 留空表示整节点
GET  /api/v1/nodes/{id}/health-checks           # 列表项含 core
GET  /api/v1/nodes/{id}/inbound-states          # Owner/Viewer；每个自建入站相对节点运行修订的发布状态 {items:[{inboundId,state,code?,revision?}]}，见 11.2
GET  /api/v1/core-releases                      # Owner；?component=&source=[&refresh=1]，发布信息查询，见 22.2
GET  /api/v1/core-updates                       # 任务列表，含 component 与 adding
POST /api/v1/core-updates                       # {nodeId,confirmNodeId,revision,component,version,url,sha256}
POST /api/v1/core-updates/{id}/clear
GET  /api/v1/agent-releases                      # Owner；?refresh=1，查询官方签名 Agent 发布版本列表
POST /api/v1/agent-releases/{version}/import     # Owner+CSRF；从 GitHub 导入官方签名清单与归档到面板只读目录
GET  /agent-catalogue/...                        # 只读托管 Agent 签名清单与归档，严格白名单防穿越
POST /api/v1/nodes/{id}/cores/{component}/remove    # Owner+CSRF；{confirmNodeId,revision}
GET  /api/v1/nodes/{id}/cores/{component}/removals  # 最近 100 个移除任务
```

`restart-core` 的 `core` 必须属于 `Capabilities.cores`，否则 409 `restart_conflict`（核心不可用）；`core` 留空仍是整节点重启，历史回滚也仍是整节点操作。回滚到使用已移除核心的修订同样返回 409，消息与补救方式见第 22.2 节。

## 27.10 用户订阅

```text
GET /sub/{token}
GET /sub/{token}?format=base64
GET /sub/{token}?format=sing-box
```

## 27.11 实时状态

```text
GET /api/v1/events
```

SSE 只推送：

节点状态变化；配置发布状态；总览统计变化；系统告警。

## 27.12 门户、订阅、观测与画布接口

- `GET /api/v1/service-sets/{id}/references`：Owner 读取共享规则集的节点策略与客户端配置/规则引用，包含启停及活动状态。

Owner 专用 GET/POST /canvas-tunnels、PUT/DELETE /canvas-tunnels/{id}、GET /canvas-tunnels/legacy-objects、POST /canvas-tunnels/{id}/adopt；写入要求 CSRF 并记录审计。服务端连接接受 tunnelId 引用，其余配置由资源读取；客户端拒绝该字段。PUT 隧道要求 updatedAt；端点修改在同一事务中重定向所有引用方案并更新方案令牌。旧内嵌写法按方案 ID + 连接 ID 创建/更新该方案最初创建的隧道，显式引用不隐式修改资源。规划响应含 tunnels:[{tunnelId,affectedSchemes:[{id,name}]}]，冲突增加 reason/objectId。

`POST /api/v1/canvas-tunnels/{id}/rotate-credentials`（Owner ＋ CSRF，体为 `{updatedAt}`，操作日志“轮换隧道凭据”）仅用于仍属旧画布的 GOST 资源，签发新一代材料并标 modified；只读视图只给代数、服务器名、到期与指纹。已迁移对象使用第 27.13 节隧道管理接口。

画布规划接口 `POST /api/v1/canvas-schemes/{id}/plan`（Owner，会话与 CSRF 同 publish）。无正文或 `{}` 规划已保存布局；`{"layout": <CanvasLayout>}` 规划未保存布局。返回 `ok`、按服务器的 `creates/updates/removes`、逐连接状态、监听端口冲突和警告；失败原因另有 `message`。接口仅读取，不写对象、方案、审计或 Revision。它复用发布的规划、生成对象识别、端口冲突与删除引用检查；客户端方案返回 `ok:false`，客户端已有 publish 路径不变。更新表示发布会执行同名对象的保存（不表示字段一定变化）；计划与执行之间状态变化仍可能导致发布失败。

延迟目标 GET 允许 Owner/Viewer，返回 `{custom,targetIds,defaultTargetIds}`；PUT 仅 Owner + CSRF，事务保存并记审计。超过 5 个返回 409 `node_target_limit`，未知目标返回 400 `unknown_target`，重复 ID 返回 400 `invalid_request`，已删除节点返回 404。延迟查询目标包含当前有效目标与查询范围内仍有采样的历史目标。

延迟查询返回 `{intervalSeconds,sampleSeconds,targets,series}`，series 项为 `{targetId,points}`，points 项为 `{at,avgMs,minMs,maxMs,medianMs,sent,failed,samples}`。sampleSeconds 为 60（原始检测周期）；intervalSeconds 表示返回点的桶步长，在不超过 12 小时时为 60，超过时为分桶步长（24 小时 120 秒，168 小时 840 秒）。at 为 UTC 原始时间或桶起点；桶统计含义见 §23，目标选择逻辑不变。

以下路径均以 `/api/v1` 为前缀；写操作需要相应 CSRF。Owner 接口与门户接口互相拒绝对方会话。

```text
PUT    /users/{id}/login
DELETE /users/{id}/sessions
POST   /users/{id}/credentials/reset
GET    /users/{id}/subscription-link
GET    /users/{id}/subscription-preview
GET    /users/{id}/portal-preview
GET    /users/{id}/devices-online
POST   /users/{id}/devices-online/reset
GET    /subscription-formats
GET/PUT /subscription-rules
GET    /subscription-rules/presets
GET/POST /client-policy-profiles
GET/PUT/DELETE /client-policy-profiles/{id}
POST   /client-policy-profiles/{id}/activate
POST   /client-policy-profiles/preview
GET    /client-policy-candidates?userId={id}
GET/POST /subscription-sources
PUT/DELETE /subscription-sources/{id}
POST   /subscription-sources/{id}/refresh
GET    /imported-proxies
GET/PUT /nodes/{id}/meta
GET/POST /probe-targets
PUT/DELETE /probe-targets/{target}
GET/POST /nodes/{id}/tcp-probes
GET    /nodes/{id}/latency?hours=24
GET/PUT /nodes/{id}/latency-targets
POST   /inbounds/{id}/reachability
POST   /nodes/{id}/publish
GET    /revisions?nodeId={id}            (a node's revisions; there is no /nodes/{id}/revisions route)
GET/POST /canvas-tunnels
PUT/DELETE /canvas-tunnels/{id}
GET    /canvas-tunnels/legacy-objects
POST   /canvas-tunnels/{id}/adopt
POST   /canvas-tunnels/{id}/rotate-credentials
GET/POST /canvas-schemes
PUT/DELETE /canvas-schemes/{id}
POST   /canvas-schemes/{id}/publish
POST   /portal/auth/login
POST   /portal/auth/logout
GET    /portal/auth/session
PUT    /portal/auth/password
GET    /portal/me
GET    /portal/traffic
GET    /portal/nodes
GET    /portal/devices-online
GET    /portal/subscription
POST   /portal/subscription/reset
```

`GET /sub/{token}/rules/{id}` 用订阅 Token 授权，输出客户端 classical provider 文本，撤销 Token 后返回 403；不使用管理员 Cookie。格式与协议矩阵见相应文档。

## 27.13 隧道管理与节点中转

以下路径均以 `/api/v1` 为前缀。GET 允许 Owner/Viewer，唯 migration-report 仅 Owner；全部 POST/PUT/DELETE（含只读 plan 和端口建议）要求 Owner＋CSRF，实际写操作记审计。门户会话不能访问。PUT 与轮换须带上次读取的 updatedAt，不匹配为 409 stale；DELETE 无该请求字段。请求/响应字段详见[v1.4 API 合同](docs/v1.4-plan.md#5-api-contract-fixed-before-u1a-backend-must-match)。

| 接口 | 合同 |
| --- | --- |
| `GET/POST /tunnels`、`PUT/DELETE /tunnels/{id}` | 列表 `{items,total}`；保存返回 Tunnel；删除 204，在用返回 409 tunnel_in_use 与 rules，已发布则 pending_delete |
| `POST /tunnels/{id}/rotate-credentials` | `{updatedAt}` → Tunnel，仅轮换草稿 |
| `GET/POST /tunnel-rules`、`PUT/DELETE /tunnel-rules/{id}` | GET 可按 tunnelId/nodeId 筛选；保存返回 TunnelRule；已发布删除待成功发布移除 |
| `GET /nodes/{id}/ports/check?port=` | `{nodeId,port,free,usedBy?}`，含禁用入站占用提示 |
| `POST /nodes/{id}/ports/allocate` | `{count?}` → `{ports:[]}`，只建议不预留；保存时重新查冲突 |
| `POST /tunnels/plan` | `{tunnelIds:[],ruleIds:[]}` → `{ok,servers,conflicts,warnings,message?}`，仅读取 |
| `POST /tunnels/publish` | 同一选择体 → `{batchId?,servers}`；逐节点 revision 可空，status 为 applied/unchanged/failed/rolled_back/skipped |
| `GET /tunnels/migration-report` | Owner 专用，`{items:[{tunnelId,name,reason}]}`；单次转换记录，不是实时问题列表 |
| `GET/POST /nodes/{id}/relays` | GET 返回 `{items:[{rule,shareLink}]}`；POST `{tunnelId,inboundId,portMode,listenPort?,subscriptionVisible,name?}` 创建本目标节点的 linked 规则，network=both；shareLink 权限见第 10.3 节 |

端口错误：409 port_in_use（含 usedBy）、400 port_out_of_range、409 no_free_port（无范围或耗尽）。auto 无 listenPort 则保存时分配；带建议端口则空闲时保留，后续重发固定。pending_delete 只读并继续保留声明/引用。

## 27.14 系统设置：节点安装连接地址

一键安装命令用的面板地址、面板 Web 端口以及 Agent gRPC 地址与端口只需在 Owner 的系统设置里保存一次。值存于既有 `system_settings` 表的单一键 `install_endpoint`（`{"host","httpPort","grpcHost","grpcPort","revision","updatedAt","updatedBy"}`，无迁移；键缺失即未设置，读出 `host`/`grpcHost` 为空、`httpPort`/`grpcPort` 为 0），随加密备份的整库快照保存与恢复。旧归档缺 `grpcHost`/`grpcPort` 时读作未设置。

| 接口 | 合同 |
| --- | --- |
| `GET /api/v1/system/install-endpoint` | Owner 专用（只有 Owner 看得到安装对话框）；返回 `{host,httpPort,grpcHost,grpcPort,revision,updatedAt,updatedBy}` |
| `PUT /api/v1/system/install-endpoint` | Owner＋CSRF，体 `{host,httpPort,grpcHost,grpcPort,expectedRevision}`，未知字段拒绝；`expectedRevision` 不匹配返回 409 `settings_conflict`，内容未变化返回当前值且不推进 `revision`，写成功记操作日志“修改节点安装连接地址”（target `install_endpoint`） |

`host` 与 `grpcHost` 为空或匹配 `^[A-Za-z0-9.-]+$` 的域名／IPv4（长度 1–253，标签不以 `.` 或 `-` 开头结尾，且每段 ≤63）；**不接受裸 IPv6** 与其他写法（协议、端口、括号），与 `node-install.sh` 对 `--panel`/`--grpc-host` 的校验一致，存下装不上节点的值没有意义。两个端口各为 0（未设置）或 1–65535；面板 Web 端口非 0 时不得与 Agent gRPC 端口相同（面板两个监听不能共用一个端口，安装器同样拒绝）。四项可独立留空：`host` 为空时对话框保留浏览器主机名；`grpcHost` 为空表示与 `host` 相同（命令不写 `--grpc-host`）；`grpcPort` 为 0 表示沿用注册响应里的实际监听端口。

三个安装命令对话框（新建服务器、原位身份恢复、版本页“添加内核”）共用该默认值；只有在面板 Web 位于 CDN 或终止 TLS 的反向代理后面、gRPC 单独暴露时才需要设置 `grpcHost`，且该名字必须被面板证书覆盖并直达面板。修改只影响之后生成的命令，不改动已有节点。

## 27.15 系统管理：面板网页更新

「系统设置 → 系统信息」顶部的「面板更新」卡片只有 Owner 可见：显示当前版本与 GitHub 最新正式发布（`api.github.com/repos/jingniao/qui/releases/latest`，忽略草稿与预发布，缓存 10 分钟，「检查更新」强制刷新），仅在确实存在更新的正式版时给出「更新到 vX.Y.Z」。只能安装最新正式版：没有版本选择、降级与 rc；开发版与候选版面板不支持（`dev_build`），没有 helper 目录的面板也不支持（`helper_missing`，卡片提示先在服务器上执行一次 `qui-update --component panel`）。

| 接口 | 合同 |
| --- | --- |
| `GET /api/v1/system/panel-update` | Owner 专用（Viewer 不可见）；`?refresh=1` 跳过发布缓存。返回 `{currentVersion,supported,unsupportedReason,latest{version,publishedAt,url},latestError,updateAvailable,pending,status}`：查询失败时 `latest` 为 null、`latestError` 为固定码 `check_failed`（不回显上游文本）；`pending` 为 `{requestId,version,requestedAt,requestedBy,stalled}`，`status` 为 `{requestId,version,previousVersion,state,code,startedAt,finishedAt,interrupted}` |
| `POST /api/v1/system/panel-update` | Owner＋CSRF，体 `{version}`，未知字段与非 X.Y.Z 版本 400 `invalid_request`，成功 202 `{requestId,version}`；409 `panel_update_unsupported`（details.reason 为 `dev_build`／`helper_missing`）、409 `panel_update_busy`、409 `panel_update_stale`（details 含 `latestVersion`／`currentVersion`）、502 `panel_update_check_failed`；请求文件写成功即记操作日志“更新面板”（target `panel`，结果为成功，实际结果看 `status`） |

文件合同：面板以无特权账号运行且 `ProtectSystem=strict`，只能原子写 `/var/lib/proxy-panel/panel-update/request.json`（`{requestId,version,requestedAt,requestedBy}`，requestId 为 32 位小写十六进制、version 为正式 X.Y.Z）；`proxy-panel-update.path`（PathChanged）触发 root oneshot `proxy-panel-update.service`（`TimeoutStartSec=1800`，故意不做沙箱、不对 `proxy-panel.service` 设 PartOf/BindsTo/Requires）运行 `/usr/local/lib/proxycontrol/panel-update-request`，它校验请求（非符号链接的普通文件、属主为 proxy-panel、≤4096 字节、合法 requestId 与版本号、且版本严格新于已安装面板）后执行 `/usr/local/bin/qui-update --component panel --version X.Y.Z --yes`（更新前数据库备份、10 秒健康检查、失败回滚），把固定码写回同一目录的 `status.json`（state 为 `running`／`succeeded`／`failed`，code 为 `running`／`succeeded`／`invalid_request`／`not_newer`／`update_failed`／`version_mismatch`，无自由文本）；更新器的输出只追加到 root 0600 的 `/var/lib/proxycontrol-install/panel-update.log`。同一 requestId 只执行一次：`status.json` 已含该 id 即为该请求的答案，不再重复更新。更新期间面板重启，页面轮询重连；`pending.stalled` 表示请求超过 10 分钟没有被应答、`status.interrupted` 表示 running 超过 35 分钟（例如更新中主机重启），两者都不再算占用，允许重新提交。仅 systemd（面板本身只支持 systemd）：`install.sh` 随面板发布 helper 与两个单元（与 update-recovery、`qui` 一样不进安装记录）、创建 0700 的请求目录并只 enable/start path 单元，`update.sh` 快照这三份文件，`uninstall.sh` 停用并删除它们、保留目录与日志。已有面板需先在 shell 里跑一次 `qui-update` 才具备该能力；它只更新面板，不更新节点 Agent。

## 27.16 系统管理：证书与 Cloudflare DNS-01

面板自己申请、保存并续期 TLS 证书（v1.4.7 §4，D1 后端）。Owner 保存一次 Cloudflare API Token 与 ACME 账户邮箱后，在证书库里提交域名即可；Token 只留在面板，不分发到节点。全部接口 Owner 专用，写操作需要 CSRF 并记操作日志；Viewer 与门户会话都被拒绝。入站通过 `certificateId` 引用证书、续期后自动发布（D2，第 12 节与本节末）；页面在「系统管理 → 系统设置 → 基础设置 → 证书与 DNS」（D3）。

| 接口 | 合同 |
| --- | --- |
| `GET /api/v1/system/certificate-settings` | 返回 `{email, directory, deployStartHour, deployEndHour, revision, cloudflare:{configured, last4, verifiedAt}}`；`directory` 缺省为 `production`（`staging` 为另一取值），Token 本身永不返回 |
| `PUT /api/v1/system/certificate-settings` | 体 `{email, directory, deployStartHour?, deployEndHour?, cloudflareToken?, expectedRevision}`，未知字段拒绝；两个小时为 0–23 的整数且不得相同（结束小时不含，开始大于结束表示跨零点），省略则保留原值，非法 400 `invalid_request`，旧存储值没有这两项时读作 3 与 6；省略 `cloudflareToken` 保留原值、`""` 清除、非空值先调 `GET https://api.cloudflare.com/client/v4/user/tokens/verify` 验证再保存（无效 400 `cloudflare_token_invalid`，验证请求本身失败 502 `cloudflare_api_failed`）；`expectedRevision` 不匹配 409 `settings_conflict`；内容未变不推进 revision、不写日志；成功记“修改证书设置” |
| `GET /api/v1/certificates` | `{items:[{id,domains,status,notBefore,notAfter,lastErrorCode,lastErrorText,lastAttemptAt,nextAttemptAt,autoDns,dnsNodeId,usedBy:[{inboundId,name,nodeId}],createdAt,updatedAt}]}`；`usedBy` 来自 `inbounds.certificate_id`，`lastErrorText` 是固定码对应的唯一中文说明 |
| `POST /api/v1/certificates` | 体 `{domains[], autoDns, dnsNodeId?}`；域名 1–10 个、小写、可带一个前导 `*.`、不接受 IP 与重复；缺少邮箱或 Token 返回 400 `certificate_settings_missing`；成功 202 返回该行（状态 `pending`）并排队申请，记“申请证书” |
| `POST /api/v1/certificates/{id}/renew` | 202 并立即排队续期，记“续期证书” |
| `DELETE /api/v1/certificates/{id}` | 仍被入站引用时 409 `certificate_in_use` 并在错误体给出 `inbounds:[{id,name}]`，否则 204，记“删除证书” |

签发在面板上以单 worker 串行执行（`internal/panel/certificate`）：确保 ACME 账户（同意条款，目录或邮箱变化时重新注册）→ 可选把 A/AAAA 记录（`proxied=false`、`ttl=1`）指向 `dns_node_id` 的 `public_ip`（没有公网地址即 `dns_node_address_missing`；不使用连接地址，因为它可能正是这个域名）→ 授权订单、写 `_acme-challenge.<域名>` TXT（ttl 60）→ 直接查询该区域 `name_servers` 等 TXT 生效（每 5 秒、最长 180 秒，超时 `dns_propagation_timeout`）→ 接受挑战、等待授权、生成 ECDSA P-256 密钥与 CSR 并完成签发 → 密封保存证书与私钥。无论成功失败，它创建的 TXT 记录都会被删除。失败写固定错误码（`cloudflare_not_configured`、`cloudflare_token_invalid`、`cloudflare_zone_not_found`、`cloudflare_api_failed`、`cloudflare_dns_record_failed`、`dns_node_address_missing`、`dns_propagation_timeout`、`acme_account_failed`、`acme_order_failed`、`acme_authorization_failed`、`acme_finalize_failed`、`acme_rate_limited`、`interrupted`、`internal`），按 1 小时 → 6 小时 → 24 小时退避重试；仍在服务有效证书的续期失败保留 `valid` 状态。上游正文、带 Token 的 URL 与 Token 本身都不入库、不进日志、不回显。续期调度每小时一次并在启动后立即跑一次：`valid` 且到期不足 30 天、或 `failed` 且 `next_attempt_at` 已到、或 `pending` 的行进入队列；续期失败且剩余不足 7 天时发布 `system.alert` 并记“证书续期失败”，每证书每 UTC 日最多一次。启动时残留的 `issuing` 行改为 `failed`/`interrupted` 并按退避重试。成功签发或续期后调用 `Renewed` 回调，唤醒证书发布器（见下）。仍有效但续期失败的行同样遵守退避：`valid` 行在续期窗口内只有 `next_attempt_at` 为空或已到才再次入队。

续期后的自动发布（D2，`internal/platform/server/certificate_deploy.go`）：证书发布器没有队列，每次检查都把证书库与各节点**已发布状态**比较——某节点已发布状态里含一个入站，其当前 `certificate_id` 指向某证书，而已发布的证书或私钥与证书库现值不同，且**已发布证书的 DNS 名称集合与证书库证书完全相同**，该节点即待发布（续期保持名称；Owner 把入站改指另一张证书但尚未发布时名称不同，属于未审阅的编辑，不会被自动推送，须手动发布，快照本身就带新证书；已发布 PEM 为空或无法解析也不算待发布）。对待发布节点，以其已发布状态为基线，只替换这些入站的 `CertificatePEM`／`PrivateKeyPEM`，用户、启用意图（沿用已发布修订的意图）与其他入站原样保留，创建说明为 `auto: certificate <第一个域名> renewed` 的修订（作者 `auto`）并部署。因此 Owner 手动发布（快照已带新证书）后发布器发现无差异、什么也不做；面板重启后第一次检查即找回重启前完成的续期。检查在启动时、每小时与每次续期完成时运行，且只在窗口内部署：窗口外的待发布节点等到窗口打开。节点忙（`ErrNodeBusy`）不计失败，下次检查继续并复用未部署的草稿；Agent 尚未应答时等待（节点离线则重连后收到，与其他修订相同）；部署失败或 Agent 回滚计一次失败，当前窗口内不再重试、下个窗口用新修订重试；同一节点连续 3 次失败后停止，发布 `system.alert`（`alert: certificate_deploy_failed`）并记操作日志“证书自动发布失败”，下一次续期得到新证书时重新开始。


## 27.17 通知

```text
GET    /api/v1/notifications?unread=1&page=1&pageSize=20
POST   /api/v1/notifications/read-all
GET    /api/v1/notification-rules
PUT    /api/v1/notification-rules
GET    /api/v1/notification-channels
POST   /api/v1/notification-channels
PUT    /api/v1/notification-channels/{id}
DELETE /api/v1/notification-channels/{id}
POST   /api/v1/notification-channels/{id}/test
```

`GET /api/v1/notifications` 支持 Owner 与 Viewer：Viewer 角色仅返回允许的 4 类监控通知（`node_offline`、`cpu`、`memory`、`disk`），Owner 可查看全部通知（含 `public_ip`、`machine_traffic`、`system_alert`）；支持 `unread=1` 只看未读与分页参数，返回 `{items,total,unread}`。
`POST /api/v1/notifications/read-all`：仅限 Owner＋CSRF，批量将全部未读通知标记为已读。
`GET /api/v1/notification-rules` 与 `PUT /api/v1/notification-rules`：仅限 Owner，PUT 要求 CSRF。体 `{items:[Rule]}` 进行完整规则替换，包含 7 类规则的启用开关、阈值、持续分钟与冷却分钟，非法值返回 400 `invalid_request`，记操作日志“修改通知规则”。
`GET /api/v1/notification-channels`：仅限 Owner，返回渠道列表；Webhook 项增加非敏感 `format`，不包含配置明文。`last4` 仍只显示 Telegram Token 或 Webhook URL 的末 4 位，不显示签名密钥摘要。
`POST /api/v1/notification-channels`、`PUT /api/v1/notification-channels/{id}`、`DELETE /api/v1/notification-channels/{id}`：仅限 Owner＋CSRF。支持 `telegram`（Bot Token、Chat ID）与 `webhook`（须为 HTTPS URL）。Webhook 的 `config.format` 支持 `generic`、`wecom`、`dingtalk`、`feishu`；新建或旧配置为空时等同 `generic`，无需迁移。`config.secret` 为可选签名密钥，仅钉钉和飞书允许，最多 1024 字节；Telegram 不允许设置 format/secret。未知字段拒绝，记操作日志“修改通知渠道”。

PUT 省略 `config` 保留整份原配置；携带时按字段合并，Webhook 必须明确填写非空 `format`。`url` 空串保留原地址，`secret` 空串保留原密钥；`config.clearSecret: true` 删除密钥且优先于新填密钥。切换到 generic/wecom 自动清除已保存签名密钥，不接受这两种格式新填 secret。Telegram 的 Bot Token、Chat ID 空串同样保留原值。

Webhook 四种格式：generic 保持原 `POST {kind,severity,title,body,node_id,at}`；wecom、dingtalk 发送 `{"msgtype":"text","text":{"content":"文本"}}`；feishu 发送 `{"msg_type":"text","content":{"text":"文本"}}`。机器人文本与 Telegram 统一为 `[SEVERITY] 标题\n正文`，无 severity 省略前缀。钉钉有密钥时用毫秒时间戳，以 secret 为 HMAC-SHA256 key、`ts+"\n"+secret` 为消息，Base64 后 URL 编码一次，追加 timestamp/sign 并保留原 query；飞书使用秒时间戳，以 `ts+"\n"+secret` 为 HMAC-SHA256 key、空消息计算 Base64 签名，timestamp/sign 放入 JSON。三家均须 HTTP 2xx 且业务码为 0（企业微信/钉钉 errcode，兼容钉钉官方示例的字符串 "0"；飞书 code、兼容旧 StatusCode）才成功；非法或非零业务码返回固定 `channel_rejected`。响应最多读取 64 KiB，不回显上游内容、签名或带 Token 地址。
`POST /api/v1/notification-channels/{id}/test`：仅限 Owner＋CSRF，立即向指定渠道发送一条测试通知并记录投递状态，响应 `{code}`（空串表示成功，否则为固定错误码）。

# 28. gRPC 节点通信

节点通信使用 Enroll 与双向流 Connect。

Agent 上报消息包括：

Hello；Heartbeat；Capabilities；ApplyResult；HealthResult；TrafficBatch；AccessBatch；UpdateResult；CertificateRequest。

Heartbeat 省略即缺测，实测 0 保留。v1.3 增加 `boot_id`（字段 34）与 `default_route_interface`（字段 35）供整机流量记账（第 21 节）：两者各自独立，读取失败只省略自己的字段，旧面板忽略未知字段。整机网卡的累计字节沿用既有 `interfaces`（字段 20）。

Heartbeat 字段 36（`public_addresses`，repeated PublicAddress）由 Agent 填写：上报当前机器上所有非回环网卡的全部公网全局单播地址（最多 16 个，包含 address、source、family），供面板维护候选地址列表。

`Heartbeat.public_ip`（字段 9）由 Agent 填写：它读默认路由网卡的地址列表（字段 35 的同一来源），用 `netaddr.Select` 取第一个公网 IPv4，否则第一个公网 IPv6，没有合格地址就留空，绝不发送私网、链路本地、回环、100.64/10 或组播地址，也不做任何外部查询。面板只在 `public_ip_manual=0` 时写入该值（手工地址不覆盖），上报为空且库中为空时改用控制连接的 gRPC 对端地址，同样按 `netaddr` 判定是否为公网（第 11.1 节）。

GOST 使用两个能力字段：`Hello.gost_core_supported`（字段 13，bool）表示该 Agent 懂 GOST 组件的安装与移除——即使 GOST 尚未安装；`CoreCapability.gost_contract_version`（字段 9，uint32）表示 GOST 设置契约版本（0 = 不支持，1 = 基础转发，2 = 池/范围/限流，3 = 加密中继，4 = 中间 relay 的下一跳链；开发分支 Agent 报告 4，正式版 1.3.1 为 3）。隧道发布按各跳能力要求两节点 ≥3、三节点 ≥4。GOST 设置随既有 `ApplyCommand.desired_state_json` 下发，不占用独立字段；没有新的 Welcome 标志，多核心与节点流量协商照旧。面板把 Hello 标志持久化在 `nodes.gost_core_supported`，恢复时清零、重连刷新，不用 Agent 版本号文本推断；旧 Agent 无法收到 GOST 分配。

节点证书文件的能力位（v1.4.7 §3）：`CoreCapability.tls_certificate_files_supported`（字段 12，bool）逐核声明该核心的渲染器能读节点上的证书与私钥文件；`Capabilities` 的同名字段（25）是单核 Agent 的标量形式，与 `forward_inbound_supported`（18／5）、`device_gate_supported`（17／6）同一写法，因为渲染按核心进行，逐核上报才作数。旧 Agent／旧面板各自忽略未知字段；面板 `node.SettingsContractCapability` 把该位并入共享的 `CoreSettingsCapability.certificate_files`，保存、配置快照与发布前三处由同一个 `RequireInboundServerSettings` 判断，因此没上报该位的 Agent 无法保存 `file` 来源。Xray、sing-box 适配器各自上报为真（GOST 不渲染 TLS 入站，保持假），原生 `check` 仍是最终门禁。

节点证书文件清单（v1.5.0 §6）：`HostInfo.certificate_files`（字段 9，repeated `CertificateFileSummary`）与 `HostInfo.certificate_inventory_supported`（字段 10，bool）由 Agent 只读扫描 `/etc/proxy-agent/certs/` 上报：每条摘要含证书路径、私钥路径（仅表示是否有配对私钥）、Subject、SAN、到期时间与指纹，**绝不读取或上报私钥内容**；解析符号链接后路径必须仍在该目录，读取失败只省略该文件。清单每 5 分钟重扫，变化时重新上报能力（`control.reportCapabilities`）。面板持久化在 `nodes.capabilities_json` 并提供按域名查找（第 27.4 节）；旧 Agent 不上报该能力时编辑器只走证书库与申请路径。

Agent 目录更新能力与端口协商（v1.5.0）：
- `Capabilities.panel_catalogue_supported`（字段 26，bool）：Agent 声明支持从面板只读目录拉取官方签名清单与更新归档。
- `Capabilities.domain_keyword_supported`（字段 27，bool）与 `CoreCapability.domain_keyword_supported`（字段 13，bool）：逐核声明该核心的路由渲染器支持域名关键字（domain keyword）匹配规则。Xray 与 sing-box 各自上报为真，GOST 保持为假。
- `Welcome.agent_catalogue_port`（字段 22，uint32）：面板告知 Agent 托管只读签名目录的 HTTP/HTTPS 端口号（未配置安装端点时取面板 Web 监听端口）。旧 Agent 忽略未知字段。
- `Capabilities.port_hop_supported`（字段 28，bool）：Agent 声明 host-network helper 已安装，可维护 Hysteria2 跳跃表（第 12 节）。旧 Agent 不上报，面板在保存、快照与发布前三处拒绝跳跃入站。
- `HostInfo` 扩充字段 11–18：`virtualization`（11，string）、`tuning_keys`（12，repeated TuningKeyState）、`congestion_available`（13，repeated string）、`congestion_loadable`（14，repeated string）、`qdisc_available`（15，repeated string）、`qdisc_loadable`（16，repeated string）、`fd_limits`（17，repeated ProcessLimit，包含 soft 与 hard）、`sysctl_sources`（18，repeated SysctlSource，包含 key/value/file）。
- `Heartbeat` 扩充 `tcp_out_segs`（字段 37，uint64）与 `tcp_retrans_segs`（字段 38，uint64），用于面板计算每分钟 TCP 重传率。
- `TuningResult` 扩充 `initial`（字段 4，map<string, string>，回传 19 键初始快照值）。

中央端下发消息包括：

Welcome；DesiredState；ApplyCommand；RollbackCommand；HealthCommand；CertificateRotate；UpdateCommand；Ack。

每条消息包含：

节点 ID；消息 ID；顺序号；时间；配置 Revision；消息类型。

节点保存最后确认的消息顺序，避免断线重连后重复执行配置命令。

# 29. 安全设计

## 29.1 后台安全

新登录会话默认有效期为 1 天（1440 分钟），Owner 可在系统设置的“认证与会话安全”中调整为 15～1440 分钟；管理员和门户共用此时长策略。已保存的自定义时长继续使用，修改只对之后的新登录生效，已有会话保持原到期时间。

终端用户认证与管理员认证隔离：门户 Cookie 为 `qui_portal_session`（Secure 时使用 `__Host-` 前缀），HttpOnly、SameSite=Strict、Path=/；会话表只存 Token 哈希。复用密码哈希、限流、同源 CSRF 与会话时长策略。禁用登录、改密码、撤销会话和删除用户立即结束相应会话；恢复备份结束门户会话。门户仅从会话取用户 ID，不接受请求指定其他用户；不展示域名访问记录。订阅链接不存浏览器 localStorage。

管理员密码安全哈希存储；HTTPS；安全 Cookie；登录失败限制；会话超时；CSRF 防护；API 请求大小限制；Owner 与 Viewer 权限隔离；敏感数据默认隐藏；管理操作日志。

面板网页更新是固定的 root 操作而非命令执行：面板只能写一个只含版本号的请求文件，root helper 仅安装 GitHub 最新正式版并调用既有 `qui-update`（第 27.15 节），也不因此获得其他远程操作能力。

## 29.2 节点安全

- 默认 Agent 签名目录安全边界（v1.5.0）：出厂内置官方公钥；Agent 默认目录仅接受主机名严格等于控制连接主机、端口来自本次 Welcome 握手且证书受面板信任的连接；面板端只读路由 `GET /agent-catalogue/` 仅开放匹配 `^proxy-agent/linux/(amd64|arm64)/[0-9]{1,12}/manifest\.json$` 与 `^artifacts/[0-9a-f]{64}\.bin$` 的路径，底层使用 `os.OpenRoot` 隔离文件系统，禁止路径穿越。
- Agent 主动连接中央端
- 网页升级须先在节点建立受管安装历史（node-install.sh 带 `--web-upgrade` 或 qui-update 自动完成）
- gRPC 使用 mTLS
- 每个节点独立证书
- 每个节点独立私钥
- 节点证书支持轮换和吊销
- 自动续证仅在控制连接内执行：NotBefore + 30 天或剩余不超过 14 天，原私钥不变；失败退避 1 小时，离线期间不执行，重连检查。90 天签发期不变。服务端准入须与之一致，当前如此：只在两个条件均未满足时拒绝自动申请，仍保留身份、原密钥、回执与确认检查。
- 一次性注册码注册
- 不使用 SSH 作为日常控制方式
- 不提供任意命令执行

## 29.3 凭据安全

- 用户凭据与节点间凭据分离、订阅 Token 的哈希查找与加密副本规则、节点私钥不上传中央端：见第 9.3 与 11.3 节。
- 节点间 GOST 凭据与用户凭据隔离，隧道管理存 `tunnels.gost_material`，旧画布材料存 `canvas_tunnels.gost_material`；三节点按段独立 CA 与服务端/客户端叶子证书。Vault 密封、CA 私钥签发后丢弃、有效期 10 年；API/计划/日志只露代数、服务器名、到期与指纹。轮换换整套草稿，发布才生效；删除不会撤销离线节点仍生效的凭据（见[运维手册](docs/operations.md)第 20–21 节）。
- 通知渠道配置（Telegram Bot Token、Chat ID、Webhook URL、format 与可选签名密钥 secret）加密保存在 `notification_channels.encrypted_config`，使用 `configuration.key` AES-256-GCM 密封并随加密备份校验；接口仅回公开格式与 Token/URL 末 4 位 last4，签名密钥不回显、不进入日志或错误。
- 自动申请证书用的 Cloudflare API Token 与 ACME 账户私钥（ECDSA P-256）各自 vault 密封保存在 `system_settings` 的 `cloudflare_token`、`acme_account` 两行；接口只回“已设置”、末 4 位与验证时间，永不回显 Token 或私钥。保存前先向 Cloudflare 校验 Token；签发与续期的上游正文、带 Token 的链接与 Token 本身都不入库、不进日志、不回显。证书与私钥密封在 `certificates.encrypted_material`，只随加密备份与恢复离开面板（第 27.16 节）。
- 敏感配置写入节点时限制文件权限
- 配置和备份中的敏感字段加密保存

## 29.4 数据安全

只记录域名级信息；不保存内容数据；按保留周期自动清理；访问记录查询需要权限；SQLite 备份使用一致性快照；恢复前执行数据库完整性检查。

# 30. 部署目录

## 30.1 中央端与更新公共目录

| 路径 | 用途 |
| --- | --- |
| /usr/local/bin/proxy-panel、qui、qui-update | 面板、root 菜单、更新器 |
| /etc/proxy-panel/config.yaml、tls/ | 配置、Web TLS |
| /var/lib/proxy-panel/ | panel.db、identity/（节点 CA）、backups/、rulesets/、releases/ |
| /var/lib/proxycontrol-install/ | root 0700；安装记录、operation.lock、update.lock、更新中的 rollback.*/（文件快照、updater、service.sh、journal） |
| /usr/local/lib/proxycontrol/service.sh | systemd/OpenRC 共用服务封装 |
| /usr/local/lib/proxycontrol/update-recovery | 开机恢复；systemd 的 proxycontrol-update-recovery.service 或 OpenRC 同名单元，不记安装记录，排在 Agent/面板/强制出站之前 |
| /usr/local/lib/proxycontrol/panel-update-request | 面板网页更新的 root helper；由 proxy-panel-update.path 触发，校验请求后运行 qui-update（第 27.15 节） |
| /etc/systemd/system/proxy-panel-update.path、proxy-panel-update.service | 监视 /var/lib/proxy-panel/panel-update/request.json 并运行 helper；root oneshot，TimeoutStartSec=1800，不沙箱、不 PartOf 面板；随面板发布，不记安装记录 |
| /var/lib/proxy-panel/panel-update/ | proxy-panel 0700；面板写 request.json，helper 写 status.json |

日志写 stdout → journald，OpenRC 经 logger → syslog，不写 /var/log 文件。面板网页更新的更新器输出例外，只追加到 /var/lib/proxycontrol-install/panel-update.log（root 0600），拒绝原因留在 journalctl -u proxy-panel-update。更新前备份名为 pre-update-<版本>-<时间>.db，回滚保留 failed-update-<版本>-<时间>.db。

自动申请证书（第 27.16 节）需要面板**出站** HTTPS 到 `api.cloudflare.com`（Token 校验与 DNS 记录）与 Let's Encrypt 的 `acme-v02.api.letsencrypt.org`／`acme-staging-v02.api.letsencrypt.org`。它不需要开放 80 端口，也不接受来自 CA 的入站连接：DNS-01 由面板自己写 TXT 记录，再用 `net.Resolver` 直接查该区域的权威名称服务器（UDP 53）。出站被阻断时的现象是固定错误码 `cloudflare_api_failed` 或 `acme_*_failed`，页面只给中文说明、不回显上游内容。

`qui-update --version X.Y.Z[-rc.N] --component panel|agent` 支持 RC 与组件范围（快照/日志/回滚），组合主机明确先 panel 后 agent；省略 component 的默认仍先 Agent 后面板。`--database` 仅面板，跨迁移降级须匹配备份。首次升级步骤见[部署](docs/deployment.md) §5.0。

## 30.2 节点端

| 路径 | 用途 |
| --- | --- |
| /usr/local/bin/proxy-agent、qui-update | Agent 引导镜像、更新器 |
| /etc/proxy-agent/config.yaml、tls/ca.crt | 配置、受信面板 CA |
| /etc/proxy-agent/certs/ | 入站可引用的证书与私钥文件（`file` 来源，第 12 节）。安装器以 root:proxy-agent 0750 幂等创建；卸载与配置一样保留 |
| /usr/local/lib/proxy-agent/<name> | 安装器核心二进制 |
| /var/lib/proxy-agent/identity/ | 节点身份与证书 |
| /var/lib/proxy-agent/revisions/、spool/、releases/ | 修订、缓存、Agent/核心镜像 |
| /var/lib/proxy-agent/runtime/<core>/ | 逐核心运行状态、日志、临时文件 |
| /var/lib/proxy-agent/host-network/、tuning/ | Agent 的 desired.json 请求与 helper 的 status.json 回执 |

强制出站 helper：proxy-agent-host-network.service（oneshot，仅 CAP_NET_ADMIN）执行 `proxy-agent --apply-host-network /var/lib/proxy-agent/host-network`，.path 监视并开机重放，只维护 nft 表 inet proxy_agent_source 与 inet proxy_agent_hop（Hysteria2 端口跳跃，第 12 节）；卸载删除表/单元。OpenRC 的 /etc/init.d/proxy-agent-host-network 监管 /usr/local/lib/proxy-agent/host-network-watch（busybox inotifyd）。Agent 本身由 systemd 或 /etc/init.d/proxy-agent 的 supervise-daemon 管理。

调优 helper：Agent 写 0600 请求；root oneshot proxy-agent-tuning.service/.path 执行 `--apply-tuning /var/lib/proxy-agent/tuning`。CAP_NET_ADMIN＋CAP_DAC_OVERRIDE（读写 Agent 0700/0600 目录），fs.file-max 靠 root 权限。helper 与 Agent 可读模块目录以判断可加载算法，模块加载系统调用被 SystemCallFilter=~@module 过滤，能力集不含 CAP_SYS_MODULE。systemd 仅开放固定 procfs 键、/etc/sysctl.d、请求目录；OpenRC 的 proxy-agent-tuning 监管 tuning-watch，无文件系统命名空间隔离。root 0600 的 /etc/sysctl.d/.proxy-tuning-snapshot.json 保存原值/管理键，flock 串行，按 request_id 回逐键结果，Agent 最多等 30 秒，超时未确认。clear/空配置删 99-proxy-tuning.conf 并恢复原值；卸载先恢复再移单元。写入拒绝即失败，不扩权；当内核模块存在时，允许将可加载算法直接写入内核 procfs。主机信息随能力刷新，不可读心跳值未知；磁盘与 disk_percent 均指状态目录文件系统。

install.sh/node-install.sh 可重复 `--core xray --core sing-box --core gost`，新装缺省仅 Xray。显式安装将顶层规范为 cores 列表，保留首项 core_name/core_path 供旧 Agent，其余配置/身份不变；手工版本/URL/摘要紧跟对应核心。升级不带核心参数保留全部记录核心与 config 原文；显式参数只替换/追加，省略不删除。`--core-source qui|official` 仅 sing-box，维持架构/libc、官方 musl 与同版本 qui 选项。

GOST 固定官方 3.3.2 静态 linux/amd64（glibc/musl 同包），其他架构拒绝；不能独立或排首项，按已有顺序升级不受影响。Agent 托管 runtime/gost，无独立单元；service/enroll/续证/update/recovery/uninstall 的固定路径集合包含它。降级 Agent 到 1.3 以下前先移除 GOST。

安装记录 v2：1=proxycontrol-install-v2，2–4=组件/程序摘要/主服务摘要，5–6 空，7=installed，8–9=强制出站摘要，10=init（openrc 表 OpenRC），11–12=调优摘要，13 起 `core <name> <absolute-path> <二进制sha256>`。面板写 12 行无核心；兼容 v1 的 5–6 核心/摘要及可缺省 init/调优，升级迁至 v2；enroll/续证/CA 脚本兼容两版。

qui-update 快照/回滚全部核心路径、Agent config、记录、主服务/强制出站/调优文件，恢复依赖固定路径集合而非待替换记录。首次迁移操作见[部署](docs/deployment.md) §1、§5.0；v2 Agent 拒绝不支持 v2 的目标 installer 降级，失败仍可恢复原 v1 记录/config。uninstall 验证并删记录核心，保留配置/身份/状态，先清调优。验证范围见[验收矩阵](docs/v1-acceptance.md) §11–12。

# 31. 备份与恢复

v1.4 开发分支：`tunnels/tunnel_hops/tunnel_rules/port_claims/tunnel_objects`、0130 的 `published_snapshot` 和 0131 的迁移标记/报告随归档保留并校验。旧归档先升级再运行一次性转换；新归档保留原有表及凭据，恢复仍把已发布对象标为 modified，须 Owner 显式发布。校验拒绝孤立或重复归属、缺失/冲突端口声明、损坏发布快照和迁移标记，不自动重建可能改变监听的声明。

备份 SQLite、系统配置、中央证书、规则集、必要密钥加密副本；默认保留每日 7、每周 4、每月 3。恢复须停写并验证文件/数据库完整性；操作与恢复后核对见[运维](docs/operations.md) §2。

| 数据 | 备份/恢复合同 |
| --- | --- |
| client_policy_profiles、system_settings.client_policy_active/client_rules | 加密备份与恢复校验覆盖配置库、活动指针和模板快照；旧归档初始化配置库并保留 activeRevision，新归档校验指针与活动模板一致性 |
| system_settings.install_endpoint | 随整库快照整体备份，无键白名单，不单独校验；恢复按归档值（该键无校验步骤，损坏值读出即报 invalid_settings），旧归档缺该键读作未设置、安装命令回落到浏览器地址 |
| node_latency_samples、定时探测队列、node_metric_samples | 不进便携备份：快照清空再 VACUUM，恢复候选再清空，不影响运行库样本；手动排队探测过期 |
| scheduled 默认目标、node_latency_targets、latency_targets_custom | 属配置；兼容 0121 前归档并默认继承。进程内恢复保留当前自定义（含空表），补其新目标但不提升为系统默认 |
| node_tuning | 备份/校验/清单包含；恢复以当前库为准，删归档独有行、保留当前期望/结果，pending→unconfirmed，不重发、不写宿主 sysctl；旧归档迁移后同处理，需核对 tuningCurrent |
| 多核心配置/任务 | inbounds.core、nodes 的 multi_core_supported/node_traffic_supported/gost_core_supported、health_checks/restart_requests.core、core_update_tasks.adding、core_remove_tasks 纳入备份；validateMultiCore 要求结果 JSON 与 id/component/revision/code/status 一致，不能同组件同时排添加/移除。移除表清空后复制归档，queued/running 更新/移除均变 unconfirmed、不重发；能力位待重连刷新 |
| node_traffic_cursors、core_traffic_samples | 随访问数据恢复，避免同段字节重复计量 |
| canvas_tunnels.gost_material | 随加密备份且纳入校验，原样恢复不重签；不自动发布，界面 modified，Owner 下次发布才下发 |
| tunnels、tunnel_hops、tunnel_rules（含 published_snapshot） | 加密备份/清单/校验覆盖；保留端口、版本与密封材料，不重签；恢复已发布对象标 modified，须 Owner 显式发布 |
| port_claims、tunnel_objects | 保留 ID 归属与历史声明；拒绝孤立/重复归属、缺失或冲突声明，不自动重分配 |
| canvas_forward_reconcile、tunnel_migration_report | 保留单次完成标记、已迁移 ID 与原因；0129 前归档先升级再转换，已完成的不自动重试；事务失败可重试，转换不改运行修订/入站/证书 |
| node_machine_traffic_settings/daily/cursors | 三表长期保留、全部加密备份，非 7 天遥测；validateMachineTraffic 校验设置/日期/字节/主外键，0126 前无表合法，清单有三项计数。冻结当前库已到 0126 则整组复制当前表（空表也权威），否则保留归档；不丢当前量、不重复/复活旧量 |
| certificates、inbounds.certificate_id | 面板自签发的证书库随加密备份、清单与校验保存；validateCertificates 要求域名 JSON 已是面板写出的规范形式（1–10 个、小写、可带一个 `*.`）、状态在集合内、错误码是已知固定码、时间列为 UTC、密封材料能用配置密钥打开、`inbounds.certificate_id` 必须指向存在行，0137 前无表合法并有一项计数。恢复保留归档行，`issuing` 改为 `failed`/`interrupted` 并按正常退避重试；恢复本身不触发申请。`system_settings` 的 `certificate_settings`、`cloudflare_token`、`acme_account` 随整库快照原样往返，密封值不需要重新加密 |
| subscription_sources 的 latest_result_json/last_success_at | 加密备份与校验覆盖：结果必须是固定阶段/代码、counts 一致且诊断文本等于代码固定文本，超界或夹带任意文本拒绝整份归档；0133 前归档无该列仍合法，恢复按旧列复制，保留“未记录原因”，不自动抓取、不编造原因 |
| node_identity_retirements、node_identity_recoveries | 作废指纹在恢复时并入当前库，只注册不复活：已作废的指纹既不能覆盖也不能写入当前身份；恢复记录以当前库为准，`issued` 统一改为已过期，不重发恢复码、不重签证书 |
| node_public_addresses（nodes.public_addresses_json 与 public_ip_selected） | 随 nodes 主表加密备份；validateNodePublicAddresses 校验列表格式、项数不超过 16 项且均为合法单播 IP 与源标识，恢复保留归档选定地址 |
| notifications、notification_rules、notification_channels、notification_state | 四表纳入加密备份；notification_channels 密封材料必须能由当前密钥打开，校验 rules 包含全部 7 类规则且无越界值；历史通知超过 90 天由后台自动 Prune |
| service_set_sources | 随服务规则集加密备份；validateServiceSetSources 校验 format / behavior / last_status 的合法集合及 mrs 行为组合，非空 encrypted_cache 须能由配置 vault 解密并解码为策略 ParsedEntries；保留配置、状态与计数，恢复时不触发抓取；0140 之前无该表的归档仍合法 |
| node_tuning_dismissals、node_meta.bandwidth_mbps | 随加密备份保留与恢复；主键 `(node_id, key)`，列 `key` / `value`；validateTuningDismissals 校验 key 属于共享 tuning 白名单、value 非空且长度不超过 256 字节、node_id 关联存在节点；0141 之前无该表的归档仍合法。node_meta.bandwidth_mbps 为 1–100000 的整数（可空），沿用数据库约束校验；system_settings.tuning_auto_apply 随系统设置整体原样往返 |

调度随停机/进程内恢复取消并等待退出，新实例仅一个循环。其他模块的会话结束、安全状态与撤销访问保守合并规则见第 26.9、29 节。

# 32. 开发流程

新增能力通过既有模块接口扩展，不修改已应用迁移。

日常改动用 `bash scripts/quick-check.sh` 按改动文件裁剪检查（Markdown 只查 CR/BOM；`web/**` 跑类型检查、构建与全部 `web/scripts/*-selftest.mjs`；`deploy/**`、`scripts/*.sh` 跑 `bash -n` 与相关 `deploy/linux/tests/*.sh`，并对 git 模式不是 `100755` 的脚本告警；Go 跑改动包及其模块内依赖包的 vet/test，Windows 带 `-p 1`；agent／proto／pkg／contracts 与真实核相关包跑真实核测试；`migrations/**`、`.github/**` 要求完整门禁）。完整的 Linux 门禁（`scripts/check.sh` ＋ 真实核套件）每个版本在推送到 `main` 的发布提交上跑一次，并与同一 SHA 的 GitHub `check` 并行，两者都通过才打 tag。自上一个 `v*` 标签以来只改了 `web/**`（不含 `.go`）、`docs/**` 与根目录 `*.md` 的纯前端补丁，由 `scripts/gate-scope.sh` 判为 `web`，两条门禁只跑前端构建／自测与面板嵌入核对，跳过 Go `-race`、部署脚本测试与真实核（Owner 2026-11-09 批准）；其他改动或无法判定时一律完整门禁，`QUI_GATE_SCOPE=full`／派发 `full_gate=true` 可强制，每周 GitHub `check` 始终完整。GitHub `check` 仍按发布手动派发（另有每周运行），`cores` 只在每周或派发 `cores=true` 时运行真实核任务，`v*` 标签只构建发布且要求同 SHA 已有成功 `check`；选包、执行命令与依赖见[部署与升级](docs/deployment.md) §2。

# 33. 测试要求

## 33.1 单元测试

重点测试：

用户节点组展开；订阅节点过滤；用户协议过滤；域名匹配；策略优先级；代理链环路检测；最大跳数检查；协议能力判断；配置编译；访问记录聚合；数据保留清理。

## 33.2 集成测试

至少覆盖：

```text
SOCKS5 → Direct
VLESS → Direct
VLESS → Trojan → Node B
Shadowsocks → SOCKS5 → Node B
Netflix → Node B
TikTok → Block
A → B → C
```

## 33.3 故障测试

必须验证：

中央端离线；Agent 离线；下一跳节点离线；错误配置；节点端口占用；代理核心启动失败；节点本地缓存写满；节点证书过期；SQLite 锁等待；备份恢复。

# 34. 验收标准

验收须覆盖以下 25 条，按下方当前口径判定：

1. 一个中央面板可以管理至少三个节点。
2. 节点通过 Agent 主动连接中央端。
3. 节点使用独立证书进行 mTLS 认证。
4. 中央端使用 Go 单体服务和 SQLite。
5. 前端静态资源嵌入中央程序。
6. 可以创建 Owner 和 Viewer。
7. 可以创建代理用户，维护共享订阅、凭据与独立门户账号。
8. 用户可以绑定一个或多个节点组。
9. 用户订阅根据节点组自动生成。
10. 新节点加入节点组后，用户无需重新授权。
11. 中转节点可以不显示在用户订阅中。
12. 节点可以同时运行多个入站。
13. 支持协议 A 入站、协议 B 出站。
14. 支持 Node A 使用另一种协议中转到 Node B。
15. 支持 `A → B` 和 `A → B → C` 代理链。
16. 支持 Direct、Block、指定 Outbound 和代理链。
17. 支持 Netflix 等服务分流到指定节点。
18. 支持网站域名阻断。
19. 可以查看访问过的域名。
20. 不记录网页正文、密码或聊天内容。
21. 配置发布前能够执行校验。
22. 配置失败能够自动回滚。
23. 中央端离线后节点继续运行。
24. 节点访问记录可以断线缓存和恢复补传。
25. 可以备份并恢复中央数据库。

补充判定口径：

- \* 第 3 条：Xray 运行官方发布二进制；sing-box 可选官方或 qui 的未修改上游源码构建（上游标签），能力缺失明确降级。
- \* 第 7 条：设备限制按第 9.2 节的地址估计执行，缺能力显示部分执行；额度仍由面板自动版本软执行，正常路径窗口见第 9.1 节。
- \* 第 16 条：出口组按上游能力判定，权重策略已移除；固定出口失败不隐式 Direct。
- \* 第 19 条：每域名字节可以为空并显示“—”，出口记录为“配置出口”且不声称是跨节点实际出口。
- \* 第 7、19、24 条：有 `singbox_user_stats` 的 sing-box 流量进入每用户额度；官方无标签构建维持未知与节点总量。在线设备是源地址估计，账户字节与每域名字节分别判断。

L8（部分协议 UDP 不支持设备限制）、L9（保留样本不能还原清理后的完整月份）、L10（TCP 探测不能证明 UDP/QUIC 可用、转发无用户身份）仍成立；不得用本地测试通过替代实机或浏览器证据。

执行方式、证据要求和实机清单见[验收矩阵](docs/v1-acceptance.md)。

# 35. 明确不做

为保持轻量，不包含：

微服务；Kubernetes；Redis；Kafka；Elasticsearch；ClickHouse；高可用中央控制集群；商业套餐；支付；订单；工单；邀请返利；多租户；经销商；动态插件市场；无限多跳代理链；完整数据包记录；网页内容审计；HTTPS 中间人解密；Web Shell；任意远程命令执行；每条连接实时同步写数据库；大型日志分析平台。

# 36. 扩展边界

v1.4.7 规划见[v1.4.7 规划](docs/v1.4.7-plan.md)（节点组自动下发凭据、节点连接地址自动探测与域名、证书文件路径与 Cloudflare 自动申请，已随 v1.4.7 发布；证书文件热加载、心跳证书摘要、不重启核心的「更新证书」操作等子项未实施，见[发布说明](docs/release-notes-1.4.7.md)）。v1.5.0 规划见[v1.5.0 规划](docs/v1.5.0-plan.md)（Agent 网页一键升级、多公网地址、通知系统、发布状态、去掉用户协议控制、按域名找证书、Hysteria2 端口跳跃、用途组 @all、规则集外部来源、服务器调优完善，已随 v1.5.0 发布）。v1.5.1 规划见[v1.5.1 规划](docs/v1.5.1-plan.md)（「分流控制 → 规则集」页面补关键字与外部来源并修复保存清空关键字、修复受管 Agent 因发布历史锁冲突无法发布显式核心入站的 `apply_failed`、安装命令对话框去掉面板地址与端口输入并可勾选「启用网页升级」、修复 1.5.0 新装未真正启用网页升级、Windows 国旗、用户详情「可用节点」只列订阅端点、监控 CPU／网速修复与 Chart.js 图表、节点列表按入站显示是否生效，九项已随 v1.5.1 发布，见[发布说明](docs/release-notes-1.5.1.md)）。v1.5.2 规划见[v1.5.2 规划](docs/v1.5.2-plan.md)（监控页面反复读取历史指标并短暂清空图表、「版本与更新」页国旗、延迟图与机器监控样式统一，尚未实施）。完整系统 TUN、Passkey、OIDC、更细管理员权限、灰度升级及更大规模存储/多中央等仅为可能方向，不是交付承诺。新增能力须通过现有模块接口扩展，保持第 3、35 节的架构与安全边界；已支持协议见第 12、13 节。

# 37. 项目运行流程

用户取得按授权节点组生成的订阅 → 选择入口并以协议 A 接入 → 节点识别用户与域名、执行本地策略 → Block、Direct、协议 B 中转或 A→B→C 链 → 节点聚合域名/动作/配置出口记录并批量上报。

中央与节点职责见第 4、23 节；中央失联时按第 3.5 节继续运行。

# 38. 项目总结

以统一数据模型、Go 模块化单体、声明式节点发布与可替换核心适配层，完成少量内部用户的多节点、多协议访问管理，保持轻量部署和清晰的安全边界。
