# v1.5.0 发布说明

2026-11-11 发布。tag `v1.5.0` → 发布提交见文末「发布记录」，迁移 0138–0141，迁移头 `0141`。功能版：Agent 网页一键升级、多公网地址、通知系统、发布状态可见、按域名查找／申请证书、Hysteria2 端口跳跃、用途组 @all、规则集外部来源与关键字、服务器调优完善。**生产尚未升级**（面板与三台 Agent 仍为 1.4.6，见文末）。

**从 ≤1.4.7 升级 Agent 需要运行两次 `qui-update --component agent`**，第二次才启用网页升级，详见「迁移与升级注意」。

## 新增与改进

**通知不再被弹窗遮住（第 0 项）。** 提示条显示在所有对话框与抽屉之上。

**Agent 网页一键升级（第 1 项）。**
- Agent 内置官方 Ed25519 公钥。Owner 在「版本与更新 → Agent」查看 GitHub 上的官方签名版本，导入后面板把签名清单与归档放进只读目录 `GET /agent-catalogue/…`，再向节点排队升级（`GET /api/v1/agent-releases`、`POST /api/v1/agent-releases/{version}/import`）。
- Agent 只从本次控制连接的同一主机、Welcome 下发的端口取目录，按签名清单校验版本、平台、序号与摘要，切换后做健康检查，失败自动回滚到原版本。
- 节点要先启用受管模式才能网页升级：新装由 `node-install.sh` 自动启用，已有节点由 `qui-update --component agent` 启用（见升级注意）。面板节点接口新增只读 `agentUpdateSupported`；未启用的节点不显示「升级」按钮，改为提示先运行 qui-update，排队时返回 409 `agent_update_unsupported`。
- `proxy-agent --config … --initialize-agent-history` 现在是一次性命令。它先取得锁并确认尚未受管，再写入“进行中”标记；中途失败会自己清理，进程被杀留下的半成品由下次启动清理并按普通模式运行。

**多公网地址（第 2 项，迁移 0138）。** Agent 心跳（字段 36）上报本机全部公网全局单播地址（最多 16 个），面板保存为候选列表。Owner 可在节点设置里从候选中选择公网 IP（来源 `selected`），也可手填（`manual`）或留空自动推导（`auto`）。选定地址从候选中消失时，节点标记 `publicIpSelectionMissing`。登记注册码时可带 `connectHost`。

**通知系统（第 3 项，迁移 0139）。**
- 侧栏通知中心，以及 7 类默认规则：节点离线、CPU、内存、磁盘、公网 IP 变化、整机流量、系统告警。每类规则可调阈值、持续窗口与冷却时间；通知保留 90 天。Viewer 只看节点类 4 类。
- 外发渠道支持 Telegram 与 HTTPS Webhook。Webhook 有通用 JSON、企业微信、钉钉、飞书四种格式，钉钉与飞书可填加签密钥。渠道配置用 AES-256-GCM 密封，接口只回末 4 位，失败只返回固定错误码。

**发布状态可见（第 4 项）。** 节点编辑器「保存并发布」之后持续显示发布进度（待服务器确认／已生效／失败，已保留原配置／已回滚），节点列表显示每个节点最新发布的状态；新建服务器的安装命令对话框显示「等待登记 → 已登记 → 已上线」。只读现有接口与事件，后端不新增状态。

**去掉用户的协议控制（第 5 项）。** 每个用户自动拥有全部受支持协议。`allowedProtocols` 仅作兼容字段保留，不再限制凭据与订阅；用户编辑页不再有「允许协议」。

**证书：按域名查找，没有就申请（第 6 项）。** Agent 只读扫描 `/etc/proxy-agent/certs/`，上报证书清单（`HostInfo` 字段 9–10，绝不上报私钥内容）。TLS 入站编辑器填写域名后，依次列出覆盖该域名的未过期节点文件、证书库证书、已过期文件；已配置 Cloudflare Token 时，还会给出「申请证书」入口。新接口为 `GET /api/v1/nodes/{id}/certificate-matches?domain=`。

**Hysteria2 端口跳跃（第 7 项）。** sing-box Hysteria2 入站可设置跳跃端口段与间隔，由 host-network helper 维护 `inet proxy_agent_hop` 表。需要 Agent 上报 `port_hop_supported`。订阅输出 mihomo `ports`／`hop-interval`、sing-box `server_ports`／`hop_interval`、URI `mport`。

**用途组可以添加全部节点（第 8 项）。** 客户端策略的用途组成员选择里，增加了「全部节点 (@all)」和单个节点两类选项。

**规则集外部来源与关键字（第 9 项，迁移 0140）。** 规则集新增域名关键字条目，以及最多 10 个外部来源（HTTPS，格式 yaml／text／mrs，行为 classical／domain／ipcidr）。mihomo 订阅为外部来源生成 `rule-provider`，节点侧使用面板抓取后加密缓存的条目。来源与缓存随加密备份保存，恢复时不会自动抓取。

**服务器调优完善（第 10 项，迁移 0141）。**
- 白名单从 13 个 sysctl 键扩到 19 个。Agent 上报虚拟化类型、每个键的可用与只读状态、已就绪与可加载的拥塞控制和排队算法、进程文件句柄上限，以及宿主 sysctl 配置来源；心跳上报 TCP 出站段与重传段，面板据此计算 TCP 重传率。
- 面板按规则引擎（`RulesVersion 2026.11-1`）给出调优建议，Owner 可以不采纳（只在推荐值变化时重新显示），也可开启「新服务器上线后自动应用推荐」。
- 不可写的键显示「此环境不可用」，不进提交、预设、推荐或纳管。OpenRC 设置 `rc_ulimit="-n 524288"`，与 systemd `LimitNOFILE` 一致。

接口、字段与边界见[项目说明书](../AGENTS.md) §11、§12、§22.0、§23、§27.4、§27.9、§27.17、§28、§29、§31。

## 修复

- 2026-11-10 审计发现的 A01–A19 全部修复：工作流 YAML、策略缓存、通知状态持久化、规则源 URL 变更、调优漂移判定与可写性、文件句柄显示、取样窗口、备份校验、签名版本匹配、systemd 写路径、证书查找竞态、Go 与前端依赖漏洞等。逐项见 [v1.5.0-plan](v1.5.0-plan.md)「2026-11-10 审计修复」，以及[审计报告](v1.5.0-audit-2026-11-10.md)、[全面审计报告](v1.5.0-comprehensive-audit-2026-11-10.md)。`go.mod` 为 `go 1.26.10`，`golang.org/x/net` v0.60.0，Vite 6.4.6。
- 调优键可写性按覆盖该键的挂载点逐键判断，修正 LXC／只读子挂载下 `fs.file-max` 被误报为可写的问题（`8a6ceb6`）。
- systemd 的 `proxy-agent` 与调优 helper 两个单元，把 `ProtectKernelModules=true` 换成 `SystemCallFilter=~@module`，Agent 与 helper 因此能读取内核模块文件，判断可加载算法；显式加载模块的系统调用仍被禁止，能力集不含 `CAP_SYS_MODULE`（`b490f16`）。
- Agent 网页升级的启用路径：原先普通安装从不进入受管模式，排队一律 409。改为 qui-update 与 node-install.sh 显式启用，失败时退回普通模式；qui-update 在“已是最新版本”时也执行启用（`e847eb0`、`5fca8a9`，见[修复方案](v1.5.0-agent-supervision-plan.md)）。
- 浏览器走查发现的两处通知中心问题：侧栏展开时通知面板向左伸出窗口；Viewer 能看见会 403 的「全部标为已读」（`04ebb84`）。

## 迁移与升级注意

- 迁移均为加法：0138 `node_public_addresses`（`nodes.public_addresses_json`、`public_ip_selected`），0139 `notifications`（规则、渠道、通知记录、去重状态四张表），0140 `ruleset_sources`（关键字条目与 `service_set_sources`），0141 `tuning_recommendations`（`node_tuning_dismissals`、`node_meta.bandwidth_mbps`、`tuning_auto_apply`）。从 v1.4.7 升级会运行这四个迁移；面板降级回 1.4.7 须使用匹配的升级前备份（`qui-update --database`）。
- **Agent 从 ≤1.4.7 升级：运行两次 `qui-update --component agent`。**
  - 主机上旧版 qui-update 不含启用网页升级的步骤：第一次运行只把 Agent 升到 1.5.0，同时装上新的 qui-update；第二次运行打印「已是最新版本」后执行启用。启用会停止 Agent、执行初始化并重新启动，再做 10 秒健康检查。
  - 启用前，面板「版本与更新 → Agent」显示「需在服务器上运行一次 qui-update --component agent 启用网页升级」，启用后出现「升级」按钮。新装节点由 `node-install.sh` 自动启用。
  - 启用失败（例如数据目录不可写）不影响升级结果：节点按普通模式继续运行，失败现场移到 `/var/lib/proxy-agent/web-upgrade-failed-<UTC时间>/`，修好原因后可重跑 qui-update。服务未运行时跳过启用并给出提示。
- 启用后：
  - `/usr/local/bin/proxy-agent` 是监督进程（引导程序），实际运行的 Agent 位于 `/var/lib/proxy-agent/releases/agent-installed/`。`proxy-agent --version` 显示的是引导程序版本，实际版本以面板为准。
  - 之后再运行 qui-update 只更新引导程序，Agent 本身的升级在面板上进行。
  - 退出受管模式的手动步骤见[运维手册](operations.md)「Agent 网页升级启用与退出」。
  - 受管模式下每个节点多一个监督进程；验收中 systemd 主机上该服务内存约 23 MB。
- systemd 主机升级 Agent 后，`proxy-agent.service` 与 `proxy-agent-tuning.service` 的沙箱设置有变化（见「修复」第三条）。OpenRC 行为不变。
- 端口跳跃、证书文件清单与调优扩充都需要 Agent 1.5.0 的新能力位，旧 Agent 节点不显示或拒绝保存相应设置。

## 验证

| 项目 | 结论 |
| --- | --- |
| 审计修复 | A01–A19 已修；当时的完整 `check.sh` 与真实核心门禁均退出 0（见[全面审计报告](v1.5.0-comprehensive-audit-2026-11-10.md)与 [v1.5.0-plan](v1.5.0-plan.md)） |
| [Alpine/OpenRC 隔离验收](v1.5.0-alpine-openrc-verification.md) | 文件句柄、调优 helper、重启保持、端口跳跃、安装记录通过；能力上报（B 项）首轮不通过，修复后 rc.2 复验 19/19 一致 |
| [systemd 调优验收](v1.5.0-systemd-tuning-verification.md) | 首轮未加载的 BBR/CAKE 被沙箱拒绝，不通过；改用 `SystemCallFilter=~@module` 后复验通过 |
| [通知验收](v1.5.0-notification-verification.md) | 渠道创建与秘密保护、五渠道四格式（钉钉／飞书签名各 6/6）、失败分类与恢复、节点离线与恢复通知均通过；外部机器人用本地模拟接收端 |
| [Agent 网页升级验收](v1.5.0-agent-web-upgrade-verification.md) | 第二轮通过：OpenRC 与 systemd 上启用、失败退回、中断清理、升级到 1.5.1、损坏文件被拒、健康失败回滚均符合预期；systemd 上由原装 1.4.1 qui-update 走两次运行的真实路径 |
| [浏览器走查](v1.5.0-browser-walkthrough.md) | 30/30，`pageerror` 为 0；发现的两处通知中心问题已修复并复测 |

完整门禁与 GitHub `check` 的结果见「发布记录」。

## 发布记录

待发布完成后补写：发布 SHA、本地完整门禁耗时、GitHub `check` 运行号、`release` 运行号与附件核对。

## 生产状态

**未升级**：面板主机 us2 与三台 Agent（us2、uk、Alpine LXC/OpenRC）仍为 1.4.6。生产升级须 Owner 另行授权，顺序为先面板、后 Agent，Agent 按上文运行两次 qui-update。

## 已知限制

- Cloudflare + Let's Encrypt staging 的申请、续期与证书查找后的「申请证书」流程未实机验证（缺测试用 Cloudflare Token）。
- 企业微信、钉钉、飞书只用本地模拟接收端验证，未接真实群机器人；Telegram 手机端展示由 Owner 核对。
- Agent 日志中的遥测 WARN（`telemetry observations rejected during parsing attribution or submission`）原因未定。
- Windows 上 4 个证书清单测试失败（Linux 通过）。
- 原生 arm64 不做实机验收，只有交叉构建。
- `node-install.sh` 的自动启用与 qui-update 共用同一个函数，本轮未单独在实机执行 `node-install.sh`。
- 网页升级任务执行时，控制连接会断开 1–2 秒后自动重连，节点状态不变（观察项）。
- 推迟到后续版本：服务器列表的调优标签与筛选、调优应用时间标记、开机标识对比的重启保持检测。
- 生产尚未升级。
