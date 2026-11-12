# 变更记录

本文件保存版本与说明书修订历史；带日期的“待验收”“未发布”只描述当时状态。当前行为以[项目说明书](../AGENTS.md)为准，发布与运行证据见[验收矩阵](v1-acceptance.md)。

**v1.5.3（2026-11-12，tag `v1.5.3` → `d1303f1`，无迁移，迁移头 0142）**：v1.5.2 补丁版。修复发布大策略后节点停在「发布中」并反复离线重连：Agent 切换核心后先上报了新修订的流量，面板只接受已确认修订的数据而断开控制连接，应用结果随会话丢失。面板现在接受「已下发、待确认」修订的流量与访问记录（失败、回滚与未下发的仍拒绝），Agent 先发应用结果再发数据。只升级面板即可让已卡住的节点恢复。见[发布说明](release-notes-1.5.3.md)。
- 门禁与发布：同一提交 `d1303f1` 上本地 Linux 完整门禁 `GATE d1303f1 PASS total=3073s`（check 2403s ＋ 真核 670s，无 DATA RACE）与 GitHub `check`（run 43136341231，check＋cores）均通过；Release 于 2026-11-12 03:08:03Z 发布，签名序号 1005003 核对通过。实机：qui.lluu.de 面板与「德国 netcup」Agent 经网页升级到 1.5.3，卡住的修订 28 在面板升级后 20 秒确认，阻断验证、还原（修订 32）完成。生产 2026-11-12 03:27–03:35Z 由 1.4.6 升到 1.5.3（面板＋三台 Agent，修订不变、0 WARN，五入口抽样 200），恢复点备份 `2Ciq37nO…`。

**v1.5.2（2026-11-12，tag `v1.5.2` → `0499c27`，迁移 0142，迁移头 0142）**：监控页不再随心跳重读并清空图表；延迟图改用 Chart.js；「版本与更新」国旗；Reality 握手目标说明；sing-box 健康检查连接不再记 WARN；HTTP／SOCKS5／Shadowsocks 入站只允许 TCP；三个「屏蔽回国」客户端预设与规则集上限 256；0142 补齐 18 个内置规则集、为 25 个内置集合挂 meta-rules-dat 来源（只抓被启用节点策略引用的集合）并内置默认禁用的「屏蔽回国」策略；节点策略展开后上限 2000 → 50000（新能力位 `expanded_policy_supported`）。**已知问题由 v1.5.3 修复**。见[发布说明](release-notes-1.5.2.md)。
- 门禁与发布：同一提交 `0499c27` 上本地 Linux 完整门禁 `GATE 0499c27 PASS total=3062s`（check 2378s ＋ 真核 684s，无 DATA RACE）与 GitHub `check`（run 43126081153，check＋cores）均通过；Release 于 2026-11-12 01:19:00Z 发布，签名序号 1005002 核对通过。生产只读检查：无不合法传输组合、无 0142 同名冲突。

**v1.5.1（2026-11-11，tag `v1.5.1` → `f8bca8a`，无迁移，迁移头 0141）**：v1.5.0 补丁版。修复受管 Agent 发布显式核心入站报 `apply_failed`（监督进程启动 worker 前升级存储格式，早期失败改报 `storage_preflight_failed`／`revision_store_failed`）、1.5.0 新装未真正启用网页升级（等待存储基线）、规则集页面保存清空关键字（共用编辑组件）、监控 CPU／网速大多为空、节点列表把未生效入站显示为「已生效」（`GET /api/v1/nodes/{id}/inbound-states`）、Windows 国旗；安装命令可勾选「启用网页升级」（`--web-upgrade`，默认不启用）、对话框不再编辑面板地址，用户详情「可用节点」只列订阅端点，监控图表改用 Chart.js。生产未升级。见[发布说明](release-notes-1.5.1.md)。

**v1.5.0（2026-11-11，tag `v1.5.0` → `d823018`，迁移 0138–0141，迁移头 0141）**：Agent 网页一键升级、多公网地址、通知系统、发布状态可见、去掉用户协议控制、按域名查找／申请证书、Hysteria2 端口跳跃、用途组 @all、规则集外部来源与关键字、服务器调优完善。见[发布说明](release-notes-1.5.0.md)。
- Agent 内置官方公钥，面板托管只读签名目录，网页排队升级，失败自动回滚。节点须先进入受管模式：新装由 `node-install.sh` 自动启用；**从 ≤1.4.7 升级要运行两次 `qui-update --component agent`**（旧脚本只升级，新脚本在“已是最新版本”时启用）。启用失败退回普通模式，现场留在 `web-upgrade-failed-<时间>/`。面板新增 `agentUpdateSupported` 与 409 `agent_update_unsupported`。
- 0138 多公网地址候选与选定；0139 通知规则、渠道（Telegram、Webhook 通用／企业微信／钉钉／飞书）与通知中心；0140 规则集关键字与外部来源；0141 调优建议、不采纳记录、线路带宽与自动应用开关。调优白名单扩到 19 键，Agent 上报虚拟化、可写性、可加载算法、句柄上限与 TCP 重传计数。
- 修复：审计 A01–A19；调优可写性按挂载点判断（`8a6ceb6`）；systemd 两个单元改用 `SystemCallFilter=~@module`（`b490f16`）；Agent 网页升级启用路径（`e847eb0`、`5fca8a9`）；通知面板越界与 Viewer 看见“全部标为已读”（`04ebb84`）。
- 验证：Alpine/OpenRC、systemd 调优、通知、Agent 网页升级（两种 init）与浏览器走查（30/30）报告均为通过；门禁与发布：同一提交 `d823018` 上本地 Linux 完整门禁 `GATE d823018 PASS total=3106s`（check 2405s ＋ 真核 700s，无 DATA RACE）与 GitHub `check`（run 42959338002，check＋cores）均通过；`release`（run 42966711025）于 2026-11-11T05:36:19Z 发布，13 个附件齐全，两个 Agent 签名清单用内置官方公钥核对通过。**生产尚未升级**（面板与三台 Agent 仍为 1.4.6）。

**v1.4.7（2026-11-09，tag `v1.4.7` → `25b95e3`，迁移 0135–0137，迁移头 0137）**：保存节点组后凭据自动下发、节点客户端连接地址、TLS 入站证书来源、面板经 Cloudflare DNS-01 自动申请与续期证书。见[发布说明](release-notes-1.4.7.md)。
- 节点组保存或删除后，按该组授权用户排自动额度版本，约 60 秒合并窗口内下发或移除凭据，无须手动发布。
- 0135：Agent 填写 `Heartbeat.public_ip`（默认路由网卡的公网地址），面板以 gRPC 对端公网地址兜底，`public_ip_manual` 保护手工值；新增客户端连接地址 `connectHost`，订阅与分享链接优先使用；没有地址的节点在订阅预览里给出警告，不再静默丢弃。
- 0136：`inbounds.tls_source` 为 `paste`／`self_signed`／`file`／`acme`；节点文件来源只允许 `/etc/proxy-agent/certs/` 下的路径，由新能力位 `tls_certificate_files_supported` 门禁；读取接口新增只读 `certificateInfo`。
- 0137：证书库与「证书与 DNS」设置（Owner）。Token 经 Cloudflare 校验后密封，签发与续期失败按固定错误码退避；入站以 `certificateId` 引用证书；续期后只在低峰窗口内、且已发布证书名称集合不变时自动发布。证书库随加密备份恢复。
- 门禁与发布：同一提交 `25b95e3` 上本地 Linux 完整门禁 `GATE 25b95e3 PASS total=2976s`（check 2277s ＋ 真核 698s，无 DATA RACE）与 GitHub `check`（run 42627577140，check＋cores）均通过；`release`（run 42639012134）于 2026-11-09T14:54:28Z 发布，附件齐全。**生产尚未升级**（截至 15:55Z 面板与 Agent 仍为 1.4.6）；Cloudflare／Let's Encrypt staging 实机验收待做。

**v1.4.6（2026-11-09，tag `v1.4.6` → `620c6d7`，无迁移，迁移头仍 0134）**：内核版本、地址与摘要不用再手抄，选一个版本就按服务器的架构与 C 库填好。见[发布说明](release-notes-1.4.6.md)。
- 内核版本、地址与摘要不用再手抄：服务器 → 代理核心的「添加内核」「升级内核」对话框和「系统设置 → 系统信息 → 核心更新」卡片改为先选版本，面板按这台服务器的架构与 C 库把官方资源 URL 与厂商公布的归档摘要填好。新接口 `GET /api/v1/core-releases?component=&source=[&refresh=1]`（Owner-only）列最近 5 个正式版，摘要只读公布处——Xray 取发布页旁的 `<asset>.dgst`，官方 sing-box 取 GitHub 资产摘要，qui 构建取我们自己的 `SHA256SUMS`，GOST 取 `pkg/contracts/release` 的审阅固定值（不发请求）；同一资产两处公布值必须一致，否则留空由人粘贴。面板从不自行计算摘要，提交仍走同一 `POST /api/v1/core-updates`，面板与 Agent 的校验链不变。列表缓存 10 分钟（`refresh=1` 跳过）、失败不缓存，失败一律 502 `core_release_lookup_failed`，不回显上游。
- 批量升级按每台服务器各自取资产（Alpine 用官方 sing-box 的 `-musl` 归档，qui 构建是静态单包），没有匹配资产或该资产没有可信摘要的那台跳过并说明，不会把一台的地址套到另一台上。手输版本会清掉旧摘要：地址能按官方命名规则重建就重建（Xray、官方 sing-box），不能重建（qui 构建在面板 tag 下、GOST 单一固定归档、架构未知）就清空，避免把别的版本的地址和摘要当一对提交。
- 前端去掉写死的 `CORE_PRESETS` 预设与「生成默认地址」按钮，改为 `web/src/modules/system/coreReleases.ts` 的共享解析；前端自检新增按架构/libc 选资产、来源默认值与手输版本 URL 规则的断言，并继续断言 UI 里除 GOST 固定值外不含摘要字面量。
- 后端新增 `internal/panel/deployment/core_releases.go`、`core_releases_http.go` 与 6 组测试（`.dgst` 与 GitHub 摘要冲突、缺摘要、qui 去重、GOST 零请求、缓存/刷新/失败码、HTTP 固定错误）；新路由登记到门户路由覆盖清单。文档：项目说明书 §22.2 与 §27.9、[运维](operations.md) §9.2／§20。
- 门禁与发布：同一提交 `620c6d7` 上本地 Linux 完整门禁 `GATE 620c6d7 PASS total=2742s`（check 2077s ＋ 真核 665s，scope `full`）与 GitHub `check`（run 42556934897）均通过，`release` 附件齐全。生产 2026-11-09T04:32Z 起先面板 `1.4.5 → 1.4.6`，再按 uk、Alpine、us2 逐台把 Agent `1.4.4 → 1.4.6`；四次 `qui-update` 均成功，三台在线，修订仍为 295／296／291，各主机无 `rollback.*` 残留，面板配置未改写（sha256 仍 `e57bc1d9…`），升级后 WARN/ERROR 为 0；五个入口抽样与阻断、错误密码拒绝仍生效。恢复点为校验过的加密面板备份 `FlK52Fyj…` 与更新器备份 `pre-update-1.4.5-20261109-123223.db`。详见[发布说明](release-notes-1.4.6.md)。

**v1.4.5（2026-11-09，tag `v1.4.5` → `88fe266`，无迁移，迁移头仍 0134）**：安装命令对话框改用「节点安装连接地址」里保存的面板与 Agent 地址端口；纯前端补丁走短门禁。生产当次只把面板升到 `1.4.5`，三台 Agent 到 v1.4.6 才一起升级（升级前基线见[1.4.6 发布说明](release-notes-1.4.6.md)）。
- 修复：「新增服务器」与「生成安装命令」对话框没有采用 系统设置 → 节点安装连接地址 里保存的面板地址、Web 端口与 Agent gRPC 地址／端口，总是生成浏览器地址与面板实际 gRPC 端口的命令。对话框把 `enrollment.value`（Vue `ref` 里存的是响应式代理）与原始对象比较，永远不相等，读到的设置因此被丢弃；改为按每次打开对话框的序号判断（`web/src/modules/nodes/components/ServerEnrollment.vue`）。原位身份恢复与版本页「添加内核」对话框不受影响。前端自检新增对应检查。
- 发布流程：纯前端补丁走短门禁。`scripts/gate-scope.sh` 对比上一个 `v*` 标签，只改了 `web/**`（不含 `.go`）、`docs/**` 与根目录 `*.md` 时判为 `web`：`scripts/check.sh` 只跑前端构建／自检并核对面板二进制已嵌入新资源，本地门禁封装跳过真实核套件；其他改动或无法判定时完整门禁，每周 GitHub `check` 始终完整，派发可设 `full_gate`。新增 `deploy/linux/tests/gate-scope-test.sh`（17 项）。见[发布流程手册](release-runbook.md)第 4 节与[项目说明书](../AGENTS.md) §32。

**v1.4.4（2026-11-08，tag `v1.4.4` → `67c9fb0`，无迁移，迁移头仍 0134）**：面板可在网页里更新自己；节点安装器在 CDN 或终止 TLS 的反向代理后固定面板证书；面板回滚不再因停用更新单元失败而中断。见[发布说明](release-notes-1.4.4.md)。
- 面板网页更新：「系统管理 → 系统设置 → 系统信息」顶部新增 Owner 专用「面板更新」卡片，显示当前版本与 GitHub 最新正式发布（`api.github.com/repos/jingniao/qui/releases/latest`，忽略草稿与预发布，缓存 10 分钟，「检查更新」强制刷新），只有存在更新的正式版时才出现「更新到 vX.Y.Z」。只能安装最新正式版，没有版本选择、降级与 rc；开发/候选版面板（`dev_build`）与没有 helper 目录的面板（`helper_missing`，卡片提示先在服务器上跑一次 `qui-update --component panel`）不支持。
- 接口：`GET /api/v1/system/panel-update`（Owner，`?refresh=1` 跳过缓存）与 `POST /api/v1/system/panel-update`（Owner＋CSRF，体 `{version}`，202 `{requestId,version}`；400 `invalid_request`、409 `panel_update_unsupported`／`panel_update_busy`／`panel_update_stale`、502 `panel_update_check_failed`），写成功记操作日志「更新面板」（target `panel`）。`pending.stalled`（请求超过 10 分钟未被应答）与 `status.interrupted`（running 超过 35 分钟，例如更新中主机重启）都允许重新提交。
- 机制：面板无特权且 `ProtectSystem=strict`，只原子写 `/var/lib/proxy-panel/panel-update/request.json`；`proxy-panel-update.path`（PathChanged）触发 root oneshot `proxy-panel-update.service`（TimeoutStartSec=1800，故意不沙箱、不对面板设 PartOf/BindsTo）运行 `/usr/local/lib/proxycontrol/panel-update-request`，校验请求（普通文件、属主 proxy-panel、≤4096 字节、32 位十六进制 requestId、严格更新的 X.Y.Z）后执行 `qui-update --component panel --version X.Y.Z --yes`（数据库备份、10 秒健康检查、失败回滚），把固定码写回 `status.json`（`running`／`succeeded`／`invalid_request`／`not_newer`／`update_failed`／`version_mismatch`，无自由文本），更新器输出只进 root 0600 的 `/var/lib/proxycontrol-install/panel-update.log`。更新期间面板重启，页面轮询重连。
- 安装与升级：`install.sh` 随面板发布 helper 与两个单元（与 update-recovery、`qui` 一样不进安装记录）、创建 0700 的请求目录并只 enable/start path 单元；`update.sh` 快照这三份文件，`uninstall.sh` 停用并删除它们、保留目录与日志。仅 systemd；已有面板需先在 shell 跑一次 `qui-update` 才具备该能力；只更新面板，不更新节点 Agent。**无迁移**（不新增表，也不改备份/恢复合同）。
- 文档：[项目说明书](../AGENTS.md) §27.15、§29.1、§30.1；[部署](deployment.md) §5.2；[运维](operations.md) §10。
- 修复（`8c67a46`，随本版发布）：节点安装器在面板 Web 位于 Cloudflare 或终止 TLS 的反向代理后时固定面板证书。此前 `GET /install/ca.pem` 用 `--cacert` 会成功（边缘证书由公网 CA 签发），但取回的文件是面板自己的证书，而 gRPC 端口呈现的正是它；把系统 CA 包装成 `/etc/proxy-agent/tls/ca.crt` 会让预检以 curl 60 失败并在登记前中止。现在按取回的文件判断：用 openssl 对照系统 CA 包，只有证书本身受公网信任时才走系统 CA 包分支，否则按固定 SHA-256 核对后直接安装该面板证书；没有 openssl 的主机回落到固定证书（同样正确）。新增 4 项 shell 测试（共 51 项通过）。
- 侧栏修复：展开状态下，已展开的分栏点击标题总是收起，不再因当前页面不在该分栏而跳转到它的第一个页面；收起的分栏点击后仍展开并在需要时导航（`web/src/components/AppSidebar.vue`，前端自检新增对应检查）。
- 发布流程提速：新增 `scripts/quick-check.sh`（按改动文件裁剪检查：文本 CR/BOM、web 类型检查/构建/自检、deploy 脚本 `bash -n` 与相关测试及可执行位告警、受影响 Go 包 vet/test、真实核包、迁移/CI 要求完整门禁）；完整门禁每个版本只在发布提交上跑一次并与 GitHub `check` 并行，偶发失败只重跑失败包一次；见[发布流程手册](release-runbook.md)、[部署](deployment.md) §2 与[项目说明书](../AGENTS.md) §32。
- 面板回滚修复（`67c9fb0`，发布前修复）：从网页发起的更新运行在 `proxy-panel-update.service` 内，回滚只在旧版本没有更新桥接时停用 `proxy-panel-update.path`；这一步原来是严格的，systemd 拒绝（安装失败留下的坏单元文件，或 systemd 不认识该单元）时会在写回任何文件前中止恢复，面板停在刚失败的版本上。现在只报告并继续恢复文件与数据库。该回归由 GitHub `check` 在 `aa549a1` 上抓到（`deploy/linux/tests/update-test.sh` 10 个回滚场景），修复后 28 通过 / 0 失败。

**v1.4.3（2026-11-08，tag `v1.4.3` → `22fb794`，无迁移，迁移头仍 0134）**：节点安装连接地址扩大到 Agent gRPC 地址与端口。
- 「系统管理 → 系统设置 → 基础设置 → 节点安装连接地址」在原有面板地址与面板 Web 端口之外，新增 **Agent gRPC 地址**与 **Agent gRPC 端口**；`install_endpoint` 的 JSON 增加 `grpcHost`/`grpcPort`，旧数据缺这两项时读作未设置，**无迁移**。
- 校验与安装器一致：`grpcHost` 与 `host` 同规则（空、域名或 IPv4，不含协议/端口）；`grpcPort` 为 0（未设置）或 1–65535；面板 Web 端口非 0 时不得与 gRPC 端口相同；四项可独立留空。
- `node-install.sh` 新增 `--grpc-host HOST`：省略或等于 `--panel` 时沿用面板地址；该地址用于 `control_address`、`server_name` 与登记前 gRPC 预检，失败信息按 `--grpc-host`/`--grpc-port` 命名。证书下载仍走 `--panel` 与 `--http-port`。
- 三个生成安装命令的对话框（新建服务器、原位身份恢复、版本页“添加内核”）默认采用保存的 gRPC 地址与端口；gRPC 地址与面板地址相同时命令不写 `--grpc-host`，gRPC 端口未保存时沿用注册响应里的实际监听端口。修改只影响之后生成的命令，不改动已有节点。
- 场景：面板 Web 位于 CDN 或终止 TLS 的反向代理后、gRPC 端口单独直连暴露时，把 gRPC 地址设为那条直连的 DNS-only 主机名；该名字必须被面板证书覆盖，否则安装器的预检会在登记前中止并说明。
- 测试：`go test ./internal/panel/system/`（安装地址读写与校验）、`deploy/linux/tests/node-install-test.sh`（47 项）、`npm run typecheck`、`node web/scripts/ui-regression-selftest.mjs` 通过。
- 发布（2026-11-08 UTC）：`v1.4.3` 打 tag → `22fb794` 并经 GitHub 发布工作流发布。门禁首轮真实核部分出现两项环境性偶发（回环端口被并行包占用、Xray 首次启动失败），隔离重跑均通过；同一快照重跑门禁 PASS（`check` 227 秒缓存命中且首轮已完整通过、实核 681 秒，合计 908 秒，无 DATA RACE），GitHub `check` run 42479913511 success。见[1.4.3 发布说明](release-notes-1.4.3.md)。
- 生产升级（2026-11-08，约 13:25–13:29 UTC）：用 `qui-update` 从 GitHub Release v1.4.3 升级。先面板 us2 `1.4.2` → `1.4.3`（13:26:02 UTC 起服务，10 秒内无重启；迁移头仍 0134；`/etc/proxy-panel/config.yaml` 未改写，sha256 仍 `e57bc1d9…`），再按 uk、Alpine、us2 逐台把 Agent 从 `1.4.2` 升到 `1.4.3`（13:26:27／13:27:36／13:28:27 UTC 启动）。四次 `qui-update` 均成功退出 0，三台在线，UK/Alpine/US 修订仍为 295/296/291（无新修订、无回滚），各主机无残留 `rollback.*`，面板升级后 0 WARN／0 ERROR（Alpine 仅在面板停机换版时记录一次预期的控制流断开并自动重连）。恢复点为校验过的加密面板备份 `yO25OluW…`（13:25:42 UTC 验证）与更新器备份 `pre-update-1.4.2-20261108-212559.db`。升级后经 uk-node 的 SOCKS5／HTTP／VLESS／Shadowsocks／Trojan 五个入口均返回 200，阻断规则与错误密码拒绝仍生效。

**v1.4.2（2026-11-08，tag `v1.4.2` → `a588c17`，无迁移，迁移头仍 0134）**：节点一键安装命令的面板地址与面板 Web 端口改为系统设置里保存一次，不再每台新服务器手改。
- 新增「系统管理 → 系统设置 → 基础设置 → 节点安装连接地址」（Owner 专用）：存于既有 `system_settings` 表单一键 `install_endpoint`，**无迁移**，键缺失即未设置；`GET/PUT /api/v1/system/install-endpoint` 带 `expectedRevision` 冲突检查（409 `settings_conflict`）、无改动不推进版本，写成功记操作日志“修改节点安装连接地址”。
- 校验口径与安装器一致：主机须匹配 `^[A-Za-z0-9.-]+$` 的域名或 IPv4（无协议、端口、括号），因此不接受裸 IPv6——`node-install.sh` 对 `--panel` 同样拒绝；端口为 0（未设置）或 1–65535；主机与端口可独立留空。
- 三个生成安装命令的对话框（新建服务器、原位身份恢复、版本页“添加内核”）默认取该设置，未设置或读取失败时回落到浏览器访问地址；对话框内改动只影响本次命令；端口标签统一改为“面板 Web 端口”，并说明它不是 Agent gRPC 端口 18880（gRPC 端口仍取注册响应里的实际监听端口）。
- 备份与恢复无需改动：加密归档是整库快照，`system_settings` 没有键白名单，新键随归档保存与校验；旧归档缺该键读作未设置。
- 设置页端口输入框在“未设置”（`httpPort=0`）时显示为空并保留 22550 占位符，不再显示 `0`；提交时留空按 0 处理。
- `node-install.sh` 加固：`--http-port` 与 `--grpc-port` 相同立即拒绝；`GET /install/ca.pem` 失败按 curl 的 HTTP 状态与退出码分类（415/426 → `--http-port` 指向 gRPC 端口；404 → 开发模式面板或没转发该路径的别的 Web 服务；DNS/拒绝/超时 → 不可达并给出 host:port），公网 CA 分支也按 `--ca-sha256` 核对取回的字节；装好信任锚、写完 `control_address` 后、登记之前用该信任锚与 `--panel` 预检 gRPC 端点（`--cacert`、无 `-k`、有 HTTP/2 时带 `--http2`，否则只测 TCP 可达），exit 0/22 通过、51/58/60/77/90/91 在登记前中止并说明面板在反代/Cloudflare 后面的常见成因与修法、6/7/28 报不可达、其余只警告不阻塞。新增 `deploy/linux/tests/node-install-test.sh`（curl/install 用 PATH 桩，42 项）并接入 `scripts/check.sh`；文档补充反代/CDN 场景。见[安装指南](install-guide.md)第 4 节与[部署](deployment.md)第 8 节。门禁（快照 `30ae663`，合计 2801 秒）与 GitHub `check` 均通过，详见[1.4.2 发布说明](release-notes-1.4.2.md)。

**v1.4.1（2026-11-08，tag `v1.4.1` → `0c1b061`）**：v1.4.0 之上的补丁版，**没有迁移**（迁移头仍 0134）：新增面板一键安装器与安装指南、默认端口改为 22550／18880，修正节点编辑器与用户列表交互，并为被拒的节点证书写日志。见[1.4.1 发布说明](release-notes-1.4.1.md)。

- 生产升级（2026-11-08，约 02:47–02:56 UTC）：用 `qui-update` 从 GitHub Release v1.4.1 升级。先面板 us2 `1.4.0` → `1.4.1`（迁移头仍 0134；`/etc/proxy-panel/config.yaml` 升级前后字节相同，仍监听 `[::]:443`／`[::]:9443`，新的 22550／18880 默认值只用于新装），再按 uk、Alpine、us2 逐台把 Agent 从 `1.4.0` 升到 `1.4.1`。四次 `qui-update` 均打印成功行，三台在线，UK/Alpine/US 修订仍为 295/296/291，无新修订、无回滚，面板服务自 02:49:22 UTC 起未重启，升级后面板日志 0 WARN／0 ERROR。恢复点为校验过的加密面板备份 `GenwwAYh…`（`.pcb`）与更新器备份 `pre-update-1.4.0-20261108-104919.db`。升级后经 uk-node 的 SOCKS5／HTTP／VLESS／SS 真实流量返回 200，阻断规则仍阻断、错误密码仍被拒；Trojan 端到端未执行（测试工具限制）。“UK → Alpine NAT”隧道自 Owner 于 2026-11-07 16:18 UTC 删除后为 `pending_delete`，仍由 Alpine 监听 21411 直到 Owner 发布移除，本次升级未改变它。净化的执行记录仅存本机忽略目录 `.tools/v141-prod-upgrade/upgrade.md`。
- 一键面板安装器 `deploy/linux/panel-install.sh` 与[项目安装与使用指南](install-guide.md)：Debian/Ubuntu 面板主机上一条命令完成下载、按 `SHA256SUMS` 校验归档、安装程序与服务账号、写端口与证书路径、用 `--check-config` 校验、开机自启、稳定性确认与防火墙放行（ufw/firewalld 已启用时才询问）；已安装主机上同一脚本变成更新／状态／重启／卸载菜单，更新交给 `qui-update`，不覆盖已有配置、证书与数据库。
- 默认端口改为面板 **22550**（`http_address`：Web/API/SSE/订阅）与 Agent **18880**（`grpc_address`）：安装器提问默认值与 `--help`、示例配置 `deploy/examples/panel.yaml`／`agent.yaml`、节点一键安装脚本（默认值与帮文，信任锚 URL 在端口不是 443 时带端口）、面板生成的节点安装命令（总是显式传 `--http-port` 与 `--grpc-port`，不再依赖脚本默认值）及文档同步；已有安装保持原端口，安装器从不改写已存在的 `/etc/proxy-panel/config.yaml`。本地开发配置 `*.dev.yaml` 仍是 8080／9443。
- 节点编辑器补充自动填充与就地说明：切到 Reality 时自动生成一个 Short ID、由握手目标一键回填 server names、追加 Short ID、生成 xhttp 路径，选 xhttp 而当前安全方式在该核心上不成立时自动切到核心上报的安全并提示；Reality／xhttp 字段增加填写说明。
- 隐藏不可用选项：入站协议与传输／安全按所属核心上报的能力过滤（编辑时保留原值），节点组“添加代理节点”不再列出无法授权的节点。
- 用户管理列表每行增加“详情”入口；编辑用户时用户名改为只读，避免提交被后端忽略的改名。
- 拒绝的节点证书写入日志（`95796b1`）：`Control.Connect` 在 `ValidateIdentity` 拒绝证书时写一条 WARN `node certificate refused`（节点、指纹、对端与错误），例如原位身份恢复后仍出示已作废证书的旧 Agent；拒绝行为本身不变，Agent 的 30 秒退避上限使过期 Agent 每分钟约两行日志。见[运维手册](operations.md)第 5.1 节。
- 文档：`docs/handoff.md` 归档到 `docs/archive/`（`bb18301`，只作追溯）。

**v1.4.0（2026-11-07，tag `v1.4.0` → `6a5a821`）**：中转拆为隧道管理（隧道/规则）、节点“添加中转”和代理中转方案。说明见[1.4.0 发布说明](release-notes-1.4.0.md)，阶段任务见[计划](v1.4-plan.md)。生产已于 2026-11-07 升级到 v1.4.0（面板 rc.3 → 1.4.0、迁移头 0133 → 0134，随后 uk／Alpine／us2 逐台 Agent rc.1 → 1.4.0），见下方首条。

- 生产升级（2026-11-07，约 13:21–13:35 UTC）：用 `qui-update` 从 GitHub Release v1.4.0 升级。先面板 us2 `1.4.0-rc.3` → `1.4.0`（迁移头 0133 → 0134），再按 uk、Alpine、us2 逐台把 Agent 从 `1.4.0-rc.1` 升到 `1.4.0`；三台在线，UK/Alpine/US 修订仍为 295/296/291，无新修订、无回滚，面板服务自 13:22 UTC 起未重启。恢复点为校验过的加密面板备份 `xWivErMT…`（13:21 UTC）与更新器备份 `pre-update-1.4.0-rc.3-20261107-212209.db`。升级后经 uk-node 的 SOCKS5／HTTP／VLESS／SS 真实流量返回 200，阻断规则与错误密码拒绝仍生效；持久“UK → Alpine NAT”隧道仍为已发布，`recovery-status` 在未签发恢复码时返回 404。Trojan 端到端与 0134 生产恢复未执行；净化的执行记录仅存本机忽略目录 `.tools/v140-prod-upgrade/`。

- 正式发布（2026-11-07 UTC）：`v1.4.0` 打 tag → `6a5a821`（迁移头 0134），经 GitHub 发布工作流发布、发布说明自动生成。发布前本地 Linux 门禁（快照 `b24faa1`，与 `6a5a821` 的树相同，仅差一行文档）通过：`check` 2069 秒、实核 680 秒、合计 2750 秒、无 DATA RACE；GitHub run 42351659212 `check` success，run 42365483798 `check` success 而 `cores` 第 1 次尝试出现一次偶发失败（`TestRealV14CompletionPaths/sing-box/P2-external`，qui sing-box 应用阶段报 `sing-box startup or health check failed`，同一次运行中官方 sing-box 的同一子测试通过），重跑该作业后官方与 qui sing-box 两步骤均通过，原因未定。新增迁移 0134（Agent 原位身份恢复）：主机重装但身份仍属原节点时，Owner 签发一次性恢复码，新 Agent 经 `Enroll` 核销后保留节点 ID 与业务配置、旧证书与指纹永久作废、已发布版本自动重新下发；systemd 与 Alpine/OpenRC 隔离主机测试通过（同一节点 ID、恢复记录核销、自动重新下发 `身份恢复：重新下发 r1`／`r3`、恢复后 SOCKS 200 且订阅 Token 不变、旧证书被拒且指纹进入 `node_identity_retirements`，`revokeNow`／supersede 与容器重启同样通过），已知缺口为恢复前跑 `uninstall.sh` 会使生成的安装命令失败（[运维 §5.1](operations.md#51-原位恢复推荐)）以及面板不为被拒的已作废证书记录日志。
- 最终收尾（2026-11-06 UTC）：生产面板已单独更新至离线 **1.4.0-rc.3**（最终内容快照 `0506a085ad5fd783`，非 Git commit），三台 Agent 保持 rc.1，UK/Alpine/US 修订 295/296/291，迁移 0133。全仓库 check／race 和实核均退出 0，浏览器 8 组场景及真实旧／新恢复、匹配降级完成。7202 秒混合流量 4028 次中成功 4024、瞬时失败 4，根因未定，不能判定零错误完整 A1 通过；临时资源和凭据清理、原资源基线及数据库检查通过。原生 arm64 已按 Owner 要求跳过；最新证据与恢复点见[最终验收记录](final-v1.4-acceptance.md)。
- 画布 P2/P3 与关联规则 P4 生产实流（2026-11-06 UTC）：四入口 12/12 次请求成功、4/4 次错误 UUID 拒绝，含 Reality TCP Vision/xhttp-auto 经现有 GOST 隧道；逐跳来源及无操作重发通过。P3 按 Alpine→UK→US 验收，保留原业务出站；临时资源清理、原配置保持，三台在线（UK/Alpine/US 274/276/273）。原拟反向 P3 的计划/发布环路校验差异已记录，见[报告](canvas-p2-p3-p4-client-verification.md)。
- VLESS / Reality 生产客户端验收（2026-11-06 UTC）：自建 TCP、Reality TCP Vision/xhttp-auto 的有效请求 21/21、错误凭据及失效公钥拒绝 11/11；草稿隔离、轮换发布和历史回滚均通过。临时资源清理，原配置不变，三台在线（UK/Alpine/US 253/255/258）。测试机使用既有 SOCKS 出口，直达网络仍有断连；完整 A1 未通过，见[报告](vless-reality-client-verification.md)。
- 三节点生产实流（2026-11-06 UTC）：UK→Alpine NAT→US 独立 GOST TLS/mTLS 路径 TCP/UDP 两轮 12/12 通过，包含 Alpine Agent 重启恢复；受控 UK 端口冲突触发失败批次并恢复三台原成功修订。测试路径及 US 临时 GOST 清理，正式 UK→Alpine 隧道保留，原资源不变。最终 UK/Alpine/US 修订 253/255/254，三台在线，见[验收报告](three-hop-host-verification.md)；不替代画布 P3 或终端用户认证。
- 收尾部署（2026-11-06 UTC）：生产面板单独更新到 `bcdf16a` 的离线 1.4.0-rc.2，三台 Agent 保持 rc.1、原成功配置修订和 8 个入站。门控反馈循环修复已在生产复测，版本页有效排队最长从约 14.8 秒降至 6ms，四页面有缓存导航首显 216–565ms。节点组界面与加高节点选择弹窗已走查，Mihomo 嵌套／空 REJECT 回环实核路由通过。恢复点、包摘要和未完成条件见[收尾报告](v1.4-rc2-closeout.md)；未公开发布。
- 节点组改为卡片列表，编辑弹窗以“授权账号／组内节点”菜单管理用户与接入成员，顶部提供分组设置。沿用现有授权和成员 API，无数据库或契约变更。
- 全量前端排版复查覆盖后台、门户及公共组件（114 个 Vue 文件）：分区标题显式统一 16px / 600，主要区域和弹窗标题 18px / 600，实体卡片名称 18px / 700，页面标题 20px / 700；标准按钮、输入、筛选移除局部小字号覆盖，普通下拉框恢复 14px，保留 12px 辅助信息、紧凑控件及代码区域。侧栏字体栈、400／600 导航字重和毛玻璃样式遵循 Owner 的回退要求。
- 版本与更新页排版对齐服务器列表：卡片名称统一 18px / 700，系统标签 12px / 600；IP、时间和已上报版本使用等宽字体，版本标签收敛为 600。搜索、状态筛选和查询/刷新恢复共享控件的 14px 字号。
- 节点组“添加代理节点”弹窗增加为视口高度减 96px（最高 960px），列表随剩余空间伸展并独立滚动，搜索筛选和底部操作保留可见。
- 前端字号统一（2026-11-05）：移除 9～11px 小字，辅助信息最低 12px；零散 13/15/17px 分别归并为 14/16/18px。正文、标准按钮与筛选控件 14px，页面标题 20px，公共弹窗标题 18px；状态徽标增加最小高度，流量单位固定 12px。移除负字距和全局强制灰度字体平滑，沿用系统字体；具体字阶与后续开发约束见[设计规范](design-system.md#三文字排版与层级-typography--hierarchy)。
- 字体清晰度对比曾尝试 XBoard 的 system-ui 字体栈、500 导航字重与不透明侧栏；Owner 按截图要求撤回该轮样式，恢复原字体栈、导航普通 400／选中 600、分类字距与毛玻璃背景。此前字号统一及节点选择弹窗加高保留。
- 节点创建/编辑改为单页分区表单：常用字段直接显示，协议参数动态联动，高级项与变更预览折叠，底部直接保存草稿或保存并发布。基础信息区提供所属节点组多选，创建时保存归属，编辑时回填并可调整。沿用现有字段、核心能力限制、校验和保存/发布流程，不增加客户端地址/端口覆盖。构建、现有编辑器/画布/UI 自测及本地合成 API 浏览器走查通过；未部署生产，见[协议验证续记](v1.4-protocol-verification.md)。
- 新登录会话默认有效期改为 1 天（1440 分钟）；系统设置继续支持 15～1440 分钟。既有自定义设置及已签发会话的到期时间保留，无需迁移。
- 页面加载优化（2026-11-05 America/Los_Angeles）：服务器基础列表不再等待逐节点元数据/流量；中转画布并行读取方案与资源，元数据稍后补齐，资源失败时重试完整读取；节点列表并行读取导入、分组和方案，中转明细独立补齐。全量节点读取使用每页 100 条，普通分页仍为 20 条。前端构建、现有画布/UI 自测及延迟请求行为检查通过；版本页原本独立读取节点列表，当前未复现慢加载。截图对应的访问地址仍待确认，浏览器共享中断后未完成最终页面走查；这些优化未部署到生产离线 RC。
- 生产页面诊断续记：用户共享 `23.254.158.3` 后复现版本页刷新风暴（节点列表 92 次、排队最长约 15 秒；13.2 秒内 379 次节点状态事件）。面板相同设备门控重复下发与 Agent 立即上报形成反馈循环。修复为变化立即发送、相同内容每分钟续期；版本页不再让重复事件覆盖进行中的读取。定向测试及构建通过，主机部署后复测待执行，见[加载诊断记录](page-loading-verification.md)。
- **客户端策略配置库：** 0132 新增服务端配置库与独立编辑/活动版本，启用后由客户端更新订阅生效；支持授权端点成员、关键词包含/排除、emptyPolicy 与只读草稿/已保存配置预览。共享规则集可查询全部客户端配置/规则及节点策略引用，有引用时禁止删除；禁用允许并提示下载 404、客户端可能继续使用缓存或报错。
- X0：Agent 上报 GOST 契约 4，支持带一个下一跳的中间 relay，保留契约 3 渲染兼容；最多 entry → mid → exit，TLS＋mTLS。Linux 运行证据与待测项见 [X0 记录](v1.4-x0-findings.md)。
- U1a/U1b：导航、隧道/规则与计划发布交互、节点添加中转、画布出口/监听字段；阶段范围见计划，浏览器验收待 A1。
- X1a/X1b：0129 的五张表、ID 归属和统一端口检查、草稿/删除/轮换与批次发布；手动保存按启用入站冲突，声明始终占用，自动分配含禁用入站；保留画布无范围默认，新隧道/规则不回退端口。见 [X1 报告](v1.4-x1a-report.md)。
- N1：节点 relays API、目标变化 needs_publish、删除引用保护、订阅/门户中转端点；0130 成功发布快照隔离草稿变化，Owner 专享真实凭据分享预览。Reality 未实现，未声称计划中的 Reality golden 已完成。
- **M1（自建 Reality／xhttp 契约，未发布，`feat/v1.4-selfhost-m1`）：** 仅服务端契约、上报与门禁，无渲染、无密钥生成、无编辑器选项、无订阅映射；阶段仍待个人复核与完整 Linux 门禁。新增 VLESS 入站的类型化服务端私有设置 `desiredstate.ProtocolSettings.reality`／`xhttp`（只接受 VLESS 入站，无 JSON 透传/未知字段），结构校验覆盖 X25519 base64url-32 且公钥须由私钥导出、Short ID 偶数位十六进制 ≤16、server names 上限与控制字符、Owner 必填 `host:port` 握手目标（仅语法）、Reality 与 TLS 材料互斥、xhttp 的 TCP/H2 模式子集，以及 Vision 仅限 TCP＋TLS/Reality。新增 `CoreSettingsCapability{core,settings_contract_version,combinations[]}` 与统一门禁：面板保存、配置快照与发布前检查一致失败关闭；契约经 `agentpb` 加法字段传输（`CoreCapability.server_settings_contract_version`／`server_settings_combinations` 为 10／11，单核标量形式在 `Capabilities` 的 23／24，`scripts/generate-proto.sh` 生成），Agent 多核心与旧标量装饰按各适配器上报复制，面板只转换不推断，单核回退保留字段，畸形上报由共享组合校验拒绝、未知版本接受但不授权。当前 Xray／sing-box 适配器尚无渲染器，生产 Agent 上报零，版本文本/VLESS 标志/他核上报都不授权；两个渲染入口对这类状态失败关闭。Reality 私钥只进加密设置与加密修订，读取与修订差异只暴露公开值。无迁移；无 Linux 门禁与实机证据，未声明可用。
- C1：画布仅新建代理中转，qui 入站出口使用独立内部凭据，外部出站可成为最终出口；relayPort 优先旧字段，旧 forward/gost 只读保留至迁移。见 [C1 报告](v1.4-c1-report.md)。
- M1：0131 启动/恢复一次性迁移合格已发布画布转发，保留对象 ID、端口、运行修订与 GOST 材料；遗留项由 Owner 查询持久迁移报告，不自动重试。备份/恢复覆盖新表、快照、声明、标记与报告，恢复须显式发布。见 [M1 报告](v1.4-m1-report.md)。
- A1b：同步说明书、架构、运维、协议/部署、文档索引及发布说明草稿；A1 测试、RC/主机与四路径客户端验收结果待补，不构成发布结论。
- 范围变更（2026-11-05，Owner 决定，**未实施**）：[协议支持补齐计划](subscription-import-compatibility-plan.en.md#14-review-decisions-2026-11-05)全部并入 v1.4.0，原定 v1.5.0。
  - 来源刷新失败误报成功的修复。
  - 外部订阅导入 SS2022/Reality/xhttp，附结构化刷新诊断。
  - 自建 Xray VLESS Reality/xhttp；sing-box 仅在固定版本支持时提供 Reality。
  - 两条工作线并行，各自通过 S5；RC 与 A1 等待两线完成。
  - 同一来源内 `name|protocol` 重复时保留首条、拒绝其余（`duplicate_endpoint_key`）。
- S1 订阅导入诊断（分支 `feat/v1.4-import-s1`，基线 `9045e1b`，随本阶段提交落地；主助手本人三轮审查依次给出首轮 5 项、第二轮 5 项阻塞与 1 项取消回归，均已在同一份改动中修正，代码已通过该审查；阶段提交自身的 Linux 门禁待执行）：抓取失败按 input/dns/address/redirect/tls/connect/deadline/http/size 分类且不回显链接/响应体；解析逐条给出原因与计数（`total = accepted + rejected + duplicates`），同键保留首条、完全相同条目单列重复，仅像 Base64 且无法解码的文档才报 `invalid_base64`；刷新与粘贴导入共用同一管线（`Save` 返回 `id + result`，200/400/422/500/404），上游 HTTP 状态（如 403）持久化并由同一固定文案格式化，不再出现“HTTP 0”；所有持久化失败一律返回 `commit`/`failed`/`import_commit_failed`，报告回滚后仍保留的上次成功时间与缓存/可用数量，不把本次解析结果说成已写入；迁移 0133 新增 `latest_result_json`/`last_success_at`，更换 URL 清空结果；备份校验结果与本行一致（来源、时间、状态、计数、诊断字段与索引，未知字段拒绝），旧归档仍可恢复；页面显示业务结果、计数、原因对话框，粘贴失败保留草稿并可直接查看原因。SS2022/Reality/xhttp 仍未实现。

**v1.3.0（2026-11-03）**：发布并部署到三台主机；说明见[发布说明](release-notes-1.3.0.md)，逐项证据见[验收矩阵](v1-acceptance.md) §12。

- 整机月流量（T1，迁移 0126）：每服务器按所选单张网卡统计整机用量，可选计量方式、可选额度与独立重置日；心跳事务内按（服务器、网卡、本地日期）记差值，首次采样、重启与计数回退只重建基线。

- Xray 端口转发字节计入节点总量（P2，修复 v1.2 的 F11）：只接受已渲染转发的原生 `inbound>>>` 统计，不进入用户额度。

- GOST 3.3.2 作为第三种核心（G1，迁移 0127）：只承载端口转发入站，固定版本与归档摘要，`Hello.gost_core_supported` 决定能否安装；GOST 不能作为节点的主核心。

- GOST 转发（G2）：目标池与轮询/主备、端口范围、双向限速与连接上限；每个展开的服务各计一次，只进节点总量。

- GOST 加密中继（G3，迁移 0128）：画布端口转发可选 GOST 隧道，出口 `relay` 监听配 TLS 与客户端 CA，凭据按隧道密封并可轮换，发布固定先出口后入口。

- 只验收 linux/amd64；未验证项与已知限制见发布说明。

**v1.3.1（2026-11-03）**：v1.3.0 的缺陷修复版，无迁移、无新功能；说明见[发布说明](release-notes-1.3.1.md)，补充验收见[验收矩阵](v1-acceptance.md) §12。

- 移除核心（含启动时恢复中断的移除）在确认核心退出后删除 `runtime/<组件>/`；退出未确认时目录、禁用清单与镜像全部保留。修复 v1.3.0“移除 GOST 后节点上留下 `runtime/gost`”。

- 画布发布先整体校验捕获到的依赖，再跳过“只为中继依赖纳入、且已运行相同状态（忽略修订号、用户访问集合按集合比较、空等于缺失）与相同启用意图”的服务器：没有修订、没有部署目标、不重启核心，改 us2↔uk 的隧道不再重启 alpine。裁剪后的批次按中继目标当前已发布状态校验依赖；显式中继批次接口仍发布全部成员。

- 画布隧道管理的入站不能在入站编辑器里改名（按 `canvas_tunnel_objects` 归属行判定，不按名字前缀）；画布自身的生成路径仍可改名并由回滚撤销。修复 v1.3.0“改名让入站脱离画布”。

- Agent 本地队列（spool）的 pragma 写进**每条连接**的 DSN：`journal_mode`／`journal_size_limit`／`max_page_count` 原只在打开时设一次，`database/sql` 在语句被中断（spool 取样有 1 秒截止）后换一条连接，页上限退回 SQLite 默认值，Agent 上报的数据库容量变成 17,592,186,036,224 B，面板以 `invalid spool capacity observation` 拒绝每次控制会话、节点一直离线到 Agent 重启，容量上限也没有生效；页上限仍由一次探测打开按页大小算出，回归测试 `TestSpoolDiskCapacitySurvivesConnectionReplacement`。既有缺陷，2026-11-03 见于 alpine-node，处置见[运维手册](operations.md) 第 13 节。

- 代码审查 Codex 3 轮（`4889cf3`，末轮无发现）：修复凭据顺序与 nil/空集合两项；中继依赖检查不比较传输方式一项不修改（既有行为）。spool 修复与门禁改动各 1 轮，均无发现。

- 主机设置（非代码，Owner 2026-11-03 批准）：uk-node 的 `nf_conntrack_max` 7680 → 131072（buckets 32768），负载下 `table full` 与随后的核心反复重启停止；做法见[运维手册](operations.md) 第 13 节。

- 补充 v1.3.0 的未验证项（v1.3.0 主机 RC-E 与测试机演练，见验收矩阵 §12）：经过 GOST 中继的 UDP、GOST 下载方向限速与连接上限、IPv6 转发、整机流量时区分桶、备份恢复两次、arm64 归档在 qemu-user 模拟下可用；GOST 上传方向限速由 RC-F 补测（见下）。arm64 仍未经真实主机验收。

- 门禁提速（`ebe8301`，只影响开发流程）：真实核套件只跑 `scripts/real-core-packages.sh` 列出的 8 个包（与全量跑相同的 119 项真实核通过）并用 `-p 4`，511–527 秒（原 1274 秒）；恢复类测试并行（单独约 330 秒，原 1133 秒）；qui sing-box 构建按构建脚本摘要缓存；GitHub 的 `cores` 改为每周或派发时勾选 `cores=true`，`check` 按发布提交跑（约 21 分钟）。本地门禁 2366 秒（原约 3140 秒）；`check` 仍约 31 分钟，最长的包是 `internal/platform/server`（152 个串行测试，`-race` 下约 1580 秒），并行化是后续可选工作。见[部署与升级](deployment.md) 第 2 节。

- 预发布版 `v1.3.1-rc.1`（`4889cf3`，GitHub 发布 432337602）与 `v1.3.1-rc.2`（`ebe8301`，432510161）。门禁：`4889cf3` 3137 秒、`27b5171` 3147 秒、`ebe8301` 2366 秒均通过；GitHub `check` 作业 41576853903 与 41607097068 成功（`27b5171` 的作业按 Owner 要求取消，`ebe8301` 的 `cores` 按设计跳过）。

- 实机 RC-F（2026-11-03，`v1.3.1-rc.1`，三台主机）：升级无迁移（头仍 `0128_gost_tunnels.sql`、`quick_check` ok、无回滚目录）；隧道创建、轮换凭据与删除三次发布都是 uk → us2，alpine 不在批次（修订 214 不变）；画布生成入站的改名被拒绝（400）而原样保存为 200；GOST 上传与下载限速实测（1 MiB/s 的 10 MB 用 8.6–9.5 秒）；移除 GOST 后 `runtime/gost` 消失而 `runtime/xray` 完好；结束状态与基线一致，0 次核心重启。详见[验收矩阵](v1-acceptance.md) §12。

- 2026-11-03 正式发布：tag `v1.3.1` 指向 `ebe8301`（与 rc.2 同一 commit），三台主机由 rc.2 升级到 1.3.1（10:44–10:48Z，面板先、Agent 后，迁移头仍为 0128，`quick_check` ok，0 个回滚目录，修订不变，spool 观测正常）。

**V1.2（2026-10-28）**：按 v1.1 计划同步已实现功能，发布说明仍为草稿。

- v1.2.0 发布（2026-11-01）：rc.1–rc.4 在三台主机验收后，v1.2.0 以 rc.4 的同一 commit（`8f6406e`）发布并部署到 us2-node、uk-node、alpine-node。实机发现 F1–F13 已修复（F11 记为限制），见[验收发现](archive/v1.2-rc1-findings.md)与[验收矩阵](v1-acceptance.md) §11。

- CI 与门禁（2026-11-01）：每个变更改在本地 Linux 测试机上过门禁（`scripts/check.sh` ＋ 真实核套件）；GitHub 只在手动与每周运行 `check`（同一脚本）与 `cores`，`v*` 标签只构建发布，且要求同一 commit 已有成功的 `check`，不再重复约 45 分钟的门禁。见[验收矩阵](v1-acceptance.md) §2。

- 实机验收（2026-11-01，见[验收发现](archive/v1.2-rc1-findings.md)与验收矩阵 §11）：v1.2.0-rc.1、rc.2、rc.3 作为 GitHub 预发布版发布。RC1 发现并修复 F1–F7（设备限制漏掉第二个来源、Alpine 上调优服务起不来等），RC2 在三台主机通过面板与 Agent 升级、设备限制、调优、多核心与端口转发验收，又发现 F8（同节点同协议的第二个入站从订阅中丢失）、F9（全新安装的节点登记被拒，发布阻塞）、F10（uk-node 上核心多次被判不健康而重启、日志无原因）与 F11（Xray 节点总量不含端口转发字节，已记为限制）。RC3 含 F8、F9 的修复与 F10 的诊断日志，待复验。Owner 要求的整机月流量（按服务器选择计量方式、网卡、可选额度与独立重置日）排入 v1.3.0，见 [v1.3 计划](archive/v1.3-plan.md)。

- 节点登记（2026-11-01，F9）：全新安装的 1.2 节点登记被拒（`install.sh` 在登记前创建 `tuning/`，登记目录检查只允许 `host-network/`），已修复；升级不受影响。

- 安全（2026-10-31，CVE-2026-98432，见[发布说明草稿](release-notes-1.2.0.md)）：面板与 Agent 以 `//go:debug multipathtcp=0` 构建（不改 go.mod），代理核心由 `core.CoreEnvironment` 以 `GODEBUG=multipathtcp=0` 启动，监听端口不再是 MPTCP；真实内核测试确认 Go 默认下核心会以 MPTCP 监听、Agent 启动后为普通 TCP。Owner 批准直接升级主机内核：us2-node、uk-node 已升级到 6.12.113，alpine-node 的内核由服务商管理（2026-11-01）。设备限制 UDP 范围按 Owner 决定（方案 1）标注，见第 9.2 节；sing-box 观测不再因一个 SOCKS5 UDP 流整体失败。自 2026-10-31 起测试在 Owner 提供的本地 Linux 测试机上进行，GitHub Actions 只做最终确认与发布构建。

- 原生核心 CI（2026-10-31，见[验收矩阵](v1-acceptance.md) §10.4 与[进度记录](archive/v1.1-progress.md)“Native-core CI on release/v1.2.0”）：`0628a5b` 的 cores 作业（Xray 26.3.27、sing-box 1.14.3、mihomo 1.19.33、qui sing-box 1.14.4-qui.1 可复现构建与双架构 release dry-run）与 check 作业（`go test -race`）全部通过，cores 作业首次通过。产品修复两项，原因相同（请求被截止时间取消后 Go 客户端仍可能返回响应且读正文不报错）：外部订阅抓取不再把残缺正文当作完整订阅缓存（`07253f9`）；Agent 自更新的清单下载被截止时间切断时报告取消而不是“清单无效”（`13cde0a`）。另修一个分钟边界导致的偶发测试失败（`c8c293b`）；最终代码 `13cde0a` 两个工作流再次全部通过。其余为 v1.1 后过时的实核测试与 Xray 26.3.27 的配置要求（带 fallbackTag 的 balancer 需要 observatory，生产渲染一直会生成）。已知上游行为：Linux 上 Xray 对可 splice 的 TCP 路径在连接关闭后才计入用户下行字节，额度判定随之推迟（第 9.1 节）。未发布。

- K4（2026-10-29）：共享隧道改为显式对象归属（`canvas_tunnels` / `canvas_tunnel_objects`，迁移 0124）：计划、发布、端口复用与清理不再按名称前缀认领，旧对象只按已发布、当前名称精确匹配且无歧义认领，未认领对象保留并可显式认领，改名后归属保持，删除仍在使用的隧道返回 409 `tunnel_in_use`。见第 14、16 节。

- K3c / MC4（2026-10-29）：多核心安装器：`install.sh` 与 `node-install.sh` 支持重复 `--core xray --core sing-box`，新节点不传参数仍只安装 Xray；安装记录升级为 v2（逐行记录核心名称、路径与摘要），继续读取 v1，目标 installer 不支持 v2 时拒绝降级；`qui-update` 快照并回滚全部核心路径、Agent config 与安装记录。见第 30.2 节。

- K3b（2026-10-29）：面板多核心支持：`Hello/Welcome` 协商 `multi_core_supported` 与独立的 `node_traffic_supported`；按核心保存能力/版本/标志与心跳健康、启动时间、统计源状态，旧 Agent 仍按一个核心建模，缺测保持未知。入站 `core` 进入草稿、DesiredState、自动额度版本、恢复与订阅，空值等于当前主核心，保存与发布按选中核心校验协议、转发、WireGuard、门控与跨核心端口唯一。核心更新任务改为每（节点、组件）一个（迁移 0125 回填既有行），缺失组件可用 schema-2 请求安装，结果按组件路由，成功且能力刷新后才可选。Owner 移除核心：`POST /nodes/{id}/cores/{component}/remove` 与 `removals`，幂等 `RemoveCoreCommand/Result`，断线后用同一身份重发，拒绝 `core_in_use`/`core_pending`/`last_core`/`revision_mismatch`，能力确认前组件仍可见。协商会话中的 node 计数器按（节点、epoch、凭据）增量计入每核心/节点总量，绝不进入额度或重复计入账户总量，同一节点可有多个活跃 epoch。连接观测按（节点、核心）合并活跃源地址后再做设备准入，保留掩码、易失存储、TTL 与放行行为；每个入站按所属核心显示 D5 覆盖。分区重启/健康检查带核心字段，移除核心后的历史回滚在面板侧直接拒绝并说明原因。备份、校验、清单与恢复覆盖新列/新表；未发布，Linux/root 实机验收待完成。

- K2b（2026-10-29）：面板接入主机信息、可空指标、分钟采样/7 天保留及查询、Owner+CSRF+审计的白名单调优（变更 L2）；断流和恢复标为未确认且不重发，续证准入与 Agent 对齐。迁移 0123；Linux/root 实机验收待完成，未发布。

- K2a（2026-10-29）：Agent 增量上报主机信息、负载/进程/套接字/字节指标及实际 sysctl；证书满 30 天或余期不超过 14 天时在连接内用原密钥续证。Owner 批准调优白名单 root helper（变更 L2），支持事务恢复与清除；面板已接入 K2b，Linux/root 实机验收待完成，未发布。

- L4：qui-update 支持 `X.Y.Z-rc.N`、semver 优先级与 `--component panel|agent`；本地修复完成，Linux/root 与主机验收待执行，RC 未运行。

- R1：订阅拆为中立代理模型、来源与格式注册表；增加 mihomo、legacy Clash 及一个全局客户端规则模板。
- R2：外部订阅导入已实现，按用户授权、加密缓存，仅用于客户端；客户端链仅 mihomo 输出。
- R3：Xray 保持官方发布核；sing-box 可用未修改上游源码的 qui 可复现构建，增加上游 `with_v2ray_api` 标签，官方核仍可用。
- R4、R5：独立终端用户账号和门户；每用户一条订阅，多端共享，新 Token 同时存哈希与加密副本，可再次查看；旧 Token 需重置后才能查看。
- R6：权限、额度、到期和凭据统一按用户管理，取消设备管理；每用户保留一个隐藏接入身份兼容 Agent，设备 API 返回 410。
- R7：并发设备限制以跨入口节点的源 IPv4 / IPv6 /64 估计，运行时门控；升级默认 0（不限），缺能力节点明确显示部分执行。
- R8、R9：单一 v1.1.0 发布目标，先面板后 Agent；保留 Owner 的页面布局，补齐实际接口、元数据、提醒、探测、端口转发与流量重置周期，未知值显示“—”。
- D-a：WireGuard 共享密钥只支持一次一个设备，不计入设备限制。D-b：活跃窗口 60 秒、释放窗口 120 秒、门控 TTL 3 分钟。
- D-c：用户可主动重置自己的订阅链接并撤销旧链接，操作记审计。D-d：Owner 与门户均只显示掩码地址（IPv4 /24、IPv6 /48）。D-e：移除无法按用户执行的上下行限速与备注中的旧限速元数据。
- 迁移 0114–0119 覆盖门户会话、设备限制、导入、节点元数据、重置周期和 TCP 探测。§34 与验收矩阵逐项记录待 CI/Owner 验收；不把跳过的原生核心测试、L8 UDP、L9 完整月流量或 RC 当作通过。

**V1.1（2026-10-17）**：按轻量重构计划决策 D1–D5 修订。

- 第 9.1 节：最大并发连接数改为观测软限制（D2），流量额度由面板软执行并自动发布（D3）。
- 第 15 节：出口组选择方式改为上游能力口径，权重策略移除（D4）。
- 第 20、21 节：每域名字节可空并显示“—”，出口记录为“配置出口”，补充上报周期与本地缓存上限的实际取值。
- 第 22 节：增加自动额度版本与核心更新流程。
- 第 23 节：更新方式明确为核心用官方 URL + SHA-256、Agent 用签名清单，并说明已删除的 receipt/journal 机制。
- 第 30 节：补充身份目录与日志走 journald 的说明。
- 第 11、13、23、30.2 节：节点可配置核心出站源 IP/网卡与服务器强制出站源地址（固定 nft 规则、独立 oneshot 单元）。
- 第 32 节阶段 4：改为适配官方 Xray 发布二进制（D1）。
- 第 34 节：验收标准按 D1–D5 补充观测口径。
- 第 9.1、22.1 节：自动额度版本失败重试、启动对账与连接数口径修正。
- 第 21、22.1 节：访问日志读后即清空；自动额度版本覆盖仍持有凭据的节点、启动对账失败重试、核心重启影响与草稿绑定例外。
- 第 30.1 节：面板主机增加 root 终端菜单 `/usr/local/bin/qui`（改管理员凭据、改 Web 端口、重启/启动/停止）。
- 第 30 节：面板与节点主机增加 `/usr/local/bin/qui-update`（从 GitHub Release 下载、校验 SHA256SUMS 后用 install.sh 原地升级，面板升级前备份数据库），`qui` 菜单增加“更新”。
- 第 11.3 节：注册节点后面板显示一键安装命令（`node-install.sh`：从 GitHub Release 下载与面板同版本的归档并校验，按公网证书或固定 SHA-256 建立对面板证书的信任，登记并启动 Agent）；新增无需登录的 `GET /install/ca.pem`。
- 文档版本号由 V1.0 升为 V1.1；文档中“第 5.5 步”的表述改为“Phase 14”。
- 第 4.3、30.2 节（2026-10-22）：节点 Agent 除 systemd 外正式支持 OpenRC（Alpine 3.19+，OpenRC ≥ 0.45，含无 systemd 的 LXC 容器）。脚本自动识别初始化系统，服务定义在 `deploy/openrc/`。强制出站 helper 在 OpenRC 上由 `inotifyd` watcher 服务触发；代理核心以 `Pdeathsig` 启动，Agent 被杀时核心随之退出；缺少 `/etc/machine-id` 时由安装脚本创建。面板仍只支持 systemd。
- 第 30 节（2026-10-23，v1.0.2）：`qui-update` 在替换前把会被替换的文件快照到 `/var/lib/proxycontrol-install/rollback.*`，更新后要求服务 10 秒内不重启；`install.sh` 失败、版本不符或新版本没有持续运行时自动恢复快照（面板还恢复更新前的数据库）并启动旧版本。新增离线选项 `--bundle/--sums`。`install.sh` 失败时不再留下 `.installing` 临时文件与暂存目录，systemd 主机缺少 `unzip` 时自动安装；面板的一键安装命令在 Debian/Ubuntu 缺少 curl 时先用 apt 安装。
- 第 30 节（2026-10-23，v1.0.3）：`qui-update` 在快照里写入进度日志（`prepared`/`started`/`committed`/`restored`，原子写入并落盘）与它自己的副本；Ctrl-C、SIGTERM、SSH 断线时按日志自动回滚，`kill -9`、断电后由开机单元 `proxycontrol-update-recovery`（systemd oneshot / OpenRC 初始化脚本，排在 Agent、面板与强制出站单元之前）完成回滚，下一次运行 `qui-update` 也会先完成未结束的恢复；同时只允许一个 `qui-update`。面板数据库备份先写 `.partial`、校验后改名；回滚覆盖数据库前把新版本的数据库保留为 `failed-update-*.db`。面板新增只读的 `--check-database FILE`（退出码 0/3/4/5），`qui-update` 用目标版本预检数据库：降级跨过迁移时不做任何替换并列出可用备份；新增 `--database FILE`，把面板降级与对应的 `pre-update-*` 数据库一起安装（第 30.1、31 节）。
- 第 30.2 节（2026-10-23，v1.0.4）：`proxy-agent-host-network.service` 设置 `StartLimitIntervalSec=0`，helper 连续失败不再让 systemd 停用 path 单元；Agent 只有在 `status.json` 确认同一请求成功后才把保留的“清除”请求视为已生效，否则重新发布；`install.sh` 发现 oneshot 单元处于 `failed` 时重放一次保留的请求。新增回归测试 `deploy/linux/tests/host-network-test.sh`。
- 第 10.3、30.2 节（2026-10-24，v1.0.5）：`install.sh` 与 `node-install.sh` 在 musl 主机（Alpine）上安装 sing-box 的官方 `-musl` 归档，按架构与 libc 固定摘要；新增 `deploy/linux/tests/core-asset-test.sh`。通用 URI 与 Base64 订阅省略钉了 `trusted_ca_pem` 的 Hysteria2/TUIC 端点，其余链接照常输出，不再整体返回 400。
- 第 22 节（2026-10-24，v1.0.6）：Agent 启动时如果已安装的核心编译或启动不了保留的成功修订（例如手工换成不支持 Hysteria2/TUIC 的 Xray），不再退出并被反复重启，而是让核心保持停止、照常连上面板并上报核心不健康，收到新修订后正常应用；supervisor 的重试间隔从 3 秒倍增到 1 分钟。重新应用保留修订失败时不再删除或改写它的修订目录。修订完整性错误（校验和、身份、回执、解密）仍然退出；核心更新中断后的修复路径也仍然退出，由重启重试。
- 第 9.3、21、22、22.1、22.2 节（2026-10-25，v1.0.7，见[验收矩阵](v1-acceptance.md) 9.15、9.16）：删除用户或设备时记录凭据墓碑（迁移 `0110_credential_tombstones.sql`），删除前产生的流量按批次所属修订照常入账，不再让节点的流量与访问记录上传卡死；Agent 对同一流量批次连续被拒 3 次后隔离它（保留在磁盘上，1 小时后或重启时重试）。每次部署或回滚成功后为该节点排队一次额度对账，回滚到旧修订不再恢复已禁用用户的凭据。“用户接入”快捷功能改为写入真实入站并按正常路径渲染；原始配置版本不再接受用户凭据（`unmanaged_user_credentials`）。核心更新结果在重连后不再被静默丢弃，Agent 在核心更新后上报新的能力。supervisor 只在核心健康时刷新运行时状态，核心不健康时只走退避后的恢复。
- 第 9.1、22.1 节（2026-10-26，v1.0.8，见[验收矩阵](v1-acceptance.md) 9.17–9.19）：删除用户或设备后自动发布移除其凭据的版本（`auto: user <name> deleted` / `device deleted`），不再需要 Owner 重新发布；此前被删除的凭据在下一次发布前仍可使用（验收矩阵 9.16）。草稿渲染不了时退回已发布状态的自动版本同样移除已删除设备的凭据。恢复较旧的备份时合并当前数据库的凭据墓碑（第 21 节）；核心更新成功后重连、任务已按上报版本标为成功时，面板仍接受随后到达的成功回执，不再断开控制连接（第 22.2 节）。Owner 清除一个已保存节点结果的核心更新任务时不再改写其状态；此前这样的任务（状态 `cleared`、结果 `failed_closed`）会让之后的每次备份都校验失败，备份校验现在接受已存在的这类记录（第 31 节）。自动版本不再启用 Owner 手动禁用、但未发布启用的入站：每次发布额外记下 Owner 当时启用的入站集合（迁移 `0111_inbound_enabled_intent.sql`，`config_revisions.inbound_enabled_json`），自动版本原样继承基线版本的记录，凭据回到已禁用入站时只有记录说 Owner 曾启用它才重新启用；记录为 NULL（本列之前发布的版本）时保守保持禁用，等 Owner 下一次发布。Agent 发核心更新结果时如果控制连接在等 Ack 时断开，结果不再随会话丢失，而是留在 core-update worker 里由下一个会话重发，直到面板确认；面板以 `FailedPrecondition` + `core update result rejected` 永久拒绝同一结果（例如任务已被 Owner 清除）连续 3 次后 Agent 丢弃并记 Warn，其他错误永不丢弃（第 22.2 节）。
- 第 22.1 节（2026-10-26，v1.0.9，见[验收矩阵](v1-acceptance.md) 9.18 问题 C、9.20）：面板停止服务（包括进程内恢复备份前）时，自动额度版本的合并窗口定时器全部停止，进行中的对账被取消并在服务返回前结束；停止过程中被打断的对账不计入 3 次失败、不告警，由下一次启动对账重新处理。此前旧实例会在恢复后继续对已关闭的数据库重试并记 `database is closed`。Agent 日志的 `retry_after` 改为可读时长。

**V1.0（2026-08-20）**：初版，完整需求与架构约束。

## 说明书归并的历史记录

以下保留说明书原有发布概览与初始开发顺序，便于追溯；不代表当前计划。上方版本条目提供详细修订。

| 版本 | 日期 | 内容 |
| --- | --- | --- |
| v1.0.0 | 2026-10-18 | 首个 GitHub 发布版 |
| v1.0.1 | 2026-10-23 | 节点 Agent 支持 Alpine/OpenRC 与实机验收修复 |
| v1.0.2 | 2026-10-23 | 一键安装；`qui-update` 的失败处理与自动回滚 |
| v1.0.3 | 2026-10-23 | `qui-update` 中断（含断电）自动恢复；面板跨迁移降级 |
| v1.0.4 | 2026-10-23 | 强制出站 helper 连续失败后不再停用 path 单元 |
| v1.0.5 | 2026-10-24 | Alpine 节点的 musl sing-box；钉证书 QUIC 端点不再让 URI 订阅整体失败 |
| v1.0.6 | 2026-10-24 | 核心起不了保留修订时 Agent 保持连接、不再反复重启 |
| v1.0.7 | 2026-10-25 | 删除用户后上传卡死、回滚恢复已禁用用户、重连丢失核心更新结果等修复 |
| v1.0.8 | 2026-10-26 | 凭据墓碑自动撤销、恢复备份、核心更新回执重发、备份校验与入站启用意图 |
| v1.0.9 | 2026-10-26 | 面板停服时停掉自动额度定时器；Agent 日志 `retry_after` 改为可读时长 |
| v1.2.0 | 2026-11-01 | v1.1 与 v1.2 合并：用户门户、设备限制、多核心（Xray + sing-box）、服务器调优、端口转发、更多订阅格式与导入、主机监控与延迟、共享隧道；CVE-2026-98432 缓解 |
| v1.3.0 | 2026-11-03 | 整机月流量（T1）；Xray 端口转发字节计入节点总量（P2，修复 F11）；GOST 3.3.2 作为第三种核心，含转发目标池/端口范围/限流（G1、G2）与节点间加密中继（G3） |

**v1.2.0 已发布（2026-11-01）**：预发布版 rc.1–rc.4 在三台主机（us2-node、uk-node、alpine-node）验收后，以 rc.4 的同一 commit（`8f6406e`）发布并部署；实机发现 F1–F13 已修复（F11 记为限制），见[验收矩阵](v1-acceptance.md) §11。只验收 amd64，arm64 归档未验证；安装器的真实断电未实机验证（`kill -9` 与重启演练已通过）。已知限制：L8（按方案 1 如实标注）、L9、L10、F11（v1.3.0 的 P2 已修复）。

**v1.3.0 已发布（2026-11-03）**：预发布版 rc.1–rc.6 与最终版在三台主机（us2-node、uk-node、alpine-node）验收后，以 rc.6 的同一 commit（`f849f01`）发布并按面板、Agent 顺序部署。内容：整机月流量（T1，迁移 0126）、Xray 端口转发字节计入节点总量（P2，修复 F11）、GOST 3.3.2 作为第三种核心（G1/G2，迁移 0127）与节点间加密中继（G3，迁移 0128）。只验收 amd64；未验证项与已知限制见[发布说明](release-notes-1.3.0.md)与[验收矩阵](v1-acceptance.md) §12，字段口径见第 21、22.2 节。

**v1.3.1 已发布（2026-11-03，tag `v1.3.1` → `ebe8301`）**：三处画布/运行时修复（`4889cf3`）、Agent 本地队列的 pragma 修复（`27b5171`）与门禁提速（`ebe8301`），无迁移、无新功能。预发布版 rc.1 → `4889cf3`、rc.2 → `ebe8301`，三台主机现运行 1.3.1。RC-F（rc.1）通过：升级无迁移、隧道发布不重发 alpine、画布生成入站改名被拒绝、移除 GOST 后 `runtime/gost` 消失、GOST 双向限速实测；RC-G（rc.2）通过：三台主机升级无迁移，10 分钟内每个节点每次心跳都报告正确的 spool 上限（34,906,112 B ≤ 104,857,600 B），0 次 `invalid spool capacity observation`。见[发布说明](release-notes-1.3.1.md)与[验收矩阵](v1-acceptance.md) §12。

CI：每个变更在本地 Linux 测试机上用 `scripts/check.sh` 与真实核套件过门禁（含 Xray 26.3.27、sing-box 1.14.3、mihomo 与 GOST 3.3.2；v1.3.1 起真实核套件只跑 `scripts/real-core-packages.sh` 列出的 8 个包并 `-p 4`，本地门禁约 2366 秒，此前约 3140 秒，见[部署与升级](deployment.md)第 2 节）；GitHub 只在手动与每周运行 `check`（同一脚本），`cores` 每周或派发时勾选才跑，`v*` 标签只构建发布、且要求同一 commit 已有成功的 `check`。

### 原始开发阶段（历史需求顺序）

## 阶段 1：项目基础骨架

实现：

- Go 项目结构
- Vue 项目结构
- SQLite 初始化
- 数据库迁移
- HTTP 服务
- gRPC 服务
- 日志
- 配置加载
- 健康检查
- 前端嵌入 Go 程序

验收：

- 一个中央程序可以启动
- 自动创建数据库
- 可以访问登录页面
- HTTP 和 gRPC 正常监听

## 阶段 2：管理员与后台框架

实现：

- 首次创建 Owner
- Owner 登录
- Viewer 管理
- 会话
- 权限控制
- 主页面布局
- 左侧导航
- 操作日志

验收：

- Owner 可以管理系统
- Viewer 只能查看
- 未登录不能访问后台 API

## 阶段 3：节点注册与控制连接

实现：

- 一次性注册码
- Agent 密钥生成
- 节点证书签发
- mTLS
- Agent 心跳
- 节点在线状态
- 能力上报
- 自动重连

验收：

- 节点无需日常 SSH 即可接入
- 中央端重启后 Agent 自动重连
- 非法证书不能连接

## 阶段 4：Xray 核心适配

实现（D1：只适配官方 Xray 发布二进制，不含任何 fork 或定制构建）：

- Xray 路径和版本检测（读取官方 `version` 输出，运行时探测统计与在线统计能力）
- 配置编译
- 原生配置校验
- 启动和停止
- 健康检查
- 配置版本
- 自动回滚

首个场景：

```text
用户
 ↓ SOCKS5
Node A
 ↓ Direct
Internet
```

## 阶段 5：用户、设备、节点组和订阅

实现：

- 用户管理
- 用户隐藏接入身份与凭据管理（不提供设备管理，见第 9.2 节）
- 凭据
- 节点组
- 用户节点组授权
- 订阅 Token
- 订阅格式生成

验收：

- 用户绑定日本节点组
- 日本组新增节点后，用户刷新订阅自动获得新节点
- 中转节点不会进入用户订阅

## 阶段 6：入站、出站与协议转换

实现：

- VLESS 入站
- Trojan 入站
- Shadowsocks 入站
- HTTP/SOCKS5 入站
- Direct 和 Block 出站
- Trojan、VLESS、Shadowsocks 和 SOCKS5 出站
- 节点间内部凭据

验收场景：

```text
用户
 ↓ VLESS
Node A
 ↓ Trojan
Node B
 ↓ Direct
Internet
```

## 阶段 7：分流和网站控制

实现：

- 域名规则
- 域名后缀
- IP/CIDR
- 用户范围
- 节点范围
- 服务规则
- Direct
- Block
- 指定出站
- 默认规则
- 策略模拟

验收：

```text
Netflix → Node B
TikTok → Block
GitHub → Direct
其他 → Direct
```

## 阶段 8：出口组和代理链

实现：

- 出口组
- 主备选择
- 简单健康检查
- A → B
- A → B → C
- 代理链能力校验
- 失败策略
- 反向发布

## 阶段 9：域名访问记录与流量统计

实现：

- Agent 域名记录
- 分钟级聚合
- 本地缓存
- 批量上传
- 全部访问页面
- 已阻断页面
- 用户、节点和出口流量

## 阶段 10：配置版本、备份与恢复

实现：

- 配置 Revision
- 发布状态
- Diff 摘要
- 自动回滚
- 手动回滚
- SQLite 在线备份
- 恢复和完整性检查

## 阶段 11：sing-box 扩展

在统一业务模型不变的情况下增加：

- sing-box 配置编译器
- Hysteria2
- TUIC
- TUN
- 后续扩展协议

## 阶段 12：系统加固

完成：

- 权限收敛
- 证书轮换
- 敏感字段保护
- 登录限速
- CSRF 防护
- 节点断网测试
- 中央端断网测试
- 核心崩溃测试
- 数据库恢复测试
- 安装和卸载脚本
- 运维文档

---

### Agent 原位身份恢复

新增 0134 迁移和 Owner 恢复码 API。恢复核销保持节点 ID 与业务配置，替换并吊销旧证书；恢复后自动重发已发布版本。Agent 二进制和安装脚本不变。
