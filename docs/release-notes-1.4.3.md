# v1.4.3 发布说明

发布日期：2026-11-08（UTC）（tag `v1.4.3` → `22fb794`，即 `release v1.4.3: Agent gRPC address and port in the install endpoint`；经 GitHub 发布工作流的 tag 推送发布）。v1.4.3 是 v1.4.2 之上的补丁版本：**没有迁移**（迁移头仍为 **0134**）；内容为「系统设置 → 节点安装连接地址」新增 **Agent gRPC 地址与端口**，`node-install.sh` 新增 `--grpc-host`，三个生成安装命令的对话框共用这些默认值。门禁、GitHub `check` 与发布结果见“验证”与“发布记录”。

**平台：** 与 v1.4.2 相同，构建 linux/amd64 与 linux/arm64；GOST 仍只支持 linux/amd64。本版未新增 arm64 实机验收。

## 新增与改进

- **安装连接地址增加 Agent gRPC 地址与端口：** 「系统管理 → 系统设置 → 基础设置 → 节点安装连接地址」在面板地址、面板 Web 端口之外新增 **Agent gRPC 地址**与 **Agent gRPC 端口**。值仍存于 `system_settings` 的单一键 `install_endpoint`，JSON 增加 `grpcHost`/`grpcPort`，**无迁移**；旧数据缺这两项时读作未设置。
- **语义：** `grpcHost` 留空表示与面板地址相同（命令不写 `--grpc-host`）；`grpcPort` 留空表示沿用面板实际监听端口（注册响应里携带）。四项可独立留空，未填写的项保持 v1.4.2 的行为。
- **校验与安装器一致：** 两个主机同规则（空、域名或 IPv4，`^[A-Za-z0-9.-]+$`，无协议、端口、括号，不接受裸 IPv6，标签 ≤63 且不以 `-` 开头结尾，总长 ≤253）；两个端口各为 0（未设置）或 1–65535；面板 Web 端口非 0 时不得与 Agent gRPC 端口相同。设置页在校验失败时保留输入并就地说明，端口为空时显示占位符而不是 `0`。
- **`node-install.sh` 新增 `--grpc-host HOST`：** 省略时等于 `--panel`。该地址用于 `control_address`、`server_name` 与登记前的 gRPC 预检；证书下载仍走 `--panel` 与 `--http-port`。预检失败信息与登记失败提示改为按 `--grpc-host`／`--grpc-port` 命名地址。
- **三个对话框共用默认值：** 新建服务器、原位身份恢复、版本页“添加内核”生成的安装命令默认采用保存的 gRPC 地址与端口；对话框内对面板地址/端口的临时修改只影响本次命令，不回写设置。命令只在 gRPC 地址与面板地址不同时写 `--grpc-host`。
- **适用场景：** 面板 Web 位于 CDN 或终止 TLS 的反向代理后、gRPC 端口单独直连暴露时，把 Agent gRPC 地址设为那条直连的 DNS-only 主机名。该名字必须被面板自己的证书覆盖；安装器的预检会在登记前发现证书不匹配并说明原因。
- **文档：** [项目说明书](../AGENTS.md)第 11.3、27.14 节，[部署与升级](deployment.md)第 8 节，[安装指南](install-guide.md)第 4 节，[运维手册](operations.md)同步更新。备份无需改动：加密归档是整库快照，新键随归档保存；旧归档缺这两项读作未设置。

## 修复

本版没有面向生产用户的缺陷修复。门禁首轮的真实核部分出现两个环境性偶发（回环端口被并行包占用、Xray 首次启动失败），隔离重跑均通过，未改代码，详见“验证”。

## 验证

- **本地检查（Windows 开发机）：** `go test ./internal/panel/system/`（89 秒）、`deploy/linux/tests/node-install-test.sh`（47/47）、`npm --prefix web run typecheck`、`npm --prefix web run build`、`node web/scripts/ui-regression-selftest.mjs`（含新增的 `--grpc-host` 断言）与 `bash -n` 均通过。
- **Linux 门禁首轮（快照 `174d048`，11:17:34–12:05:37 UTC）FAIL，合计 2883 秒：** `check` 部分 2200 秒退出 0；真实核部分 683 秒退出 1，失败为 `TestRealSS2022ImportedAES256Connections/S0-SS-02/sing-box/valid-key`（日志为 `listen tcp 127.0.0.1:45419: bind: address already in use`，门禁并行运行 4 个包时回环端口被占用）与 `TestRealV14CompletionPaths/xray/P1`（`Xray startup or health check failed`，同测试其余 11 个子路径全部通过）。
- **隔离复验：** 两项在同一快照上各自 `-race -count=1` 重跑均通过（8.2 秒 / 1.3 秒），确认是满载并行下的环境性偶发，与本批改动无关。
- **Linux 门禁（同一快照 `174d048` 重跑，12:06:20–12:21:28 UTC）PASS：** 合计 908 秒；`check` 227 秒（同一快照与环境的 Go 测试结果缓存命中，首轮已完整执行并通过）、真实核套件 681 秒全新执行，0 个失败、无 DATA RACE 告警。发布提交 `22fb794` 的树与该快照一致（`git diff` 为空）。
- **GitHub `check`：** 运行 42479913511，SHA `22fb794`（`cores=false`），2026-11-08 12:22:06–13:13:00 UTC **success**（51 分钟）。

## 已知限制

- **Agent gRPC 地址必须直达面板：** 该地址必须被面板自己的证书覆盖，且不能经过 CDN 或终止 TLS 的反向代理；mTLS 的 gRPC 无法经过 Cloudflare。保存一个不可达或证书不匹配的地址不会影响已有节点，但之后生成的安装命令会在登记前预检失败。
- **只影响之后生成的命令：** 修改不改动已有节点、`/etc/proxy-panel/config.yaml` 或正在运行的 Agent。
- **不保存端口时的端口校验：** 面板 Web 端口留空、gRPC 端口保存为浏览器当前端口时，生成的命令可能得到两个相同端口，`node-install.sh` 会拒绝；这类组合需显式填写面板 Web 端口。
- **私有 CA 证书导致客户端拉不动订阅：** 与 v1.4.1／v1.4.2 相同，未在本版改变，见 [v1.4.1 发布说明](release-notes-1.4.1.md)的“已知限制”。

## 升级

- **先面板，后 Agent：** 面板主机执行 `qui-update --component panel --version 1.4.3`，确认面板就绪后，再在每台节点主机执行 `qui-update --component agent --version 1.4.3`；组合主机同样先 panel 后 agent。
- **没有迁移：** 迁移头仍为 0134，升级不做数据库变更；`qui-update` 仍在替换前备份面板数据库，健康检查失败或中断时自动回滚。
- **已有安装保持原端口与证书：** 配置不会被改写；新装用 `panel-install.sh`（默认 22550／18880）。
- **升级 Agent 会短暂重启该节点的代理核心**（核心是 Agent 的子进程），既有连接瞬断；修订不变、没有回滚。

## 发布记录

| 项目 | 结果 / 证据 |
| --- | --- |
| 门禁首轮（快照 `174d048`） | `check` PASS（2200 秒）；真实核 683 秒失败于两项环境性偶发，见“验证” |
| 门禁（快照 `174d048` 重跑，`scripts/check.sh` + 真实核套件） | **PASS**：`check` 227 秒（缓存命中，首轮已完整通过）、真实核 681 秒、合计 908 秒、0 个失败、无 DATA RACE；发布提交 `22fb794` 与该快照树一致 |
| GitHub `check`（发布提交与 run 编号） | **PASS**：`22fb794` 的 run 42479913511，2026-11-08 12:22:06–13:13:00 UTC（`cores=false`） |
| 发布日期、tag 与迁移头复核 | **2026-11-08 13:13 UTC 推送注解 tag `v1.4.3` → `22fb794`**（tag 对象 `98a77b7`）；迁移头仍为 **0134**，本版无迁移 |
| GitHub 发布产物 | run 42483950370 发布 `proxy-control-1.4.3-linux-amd64.tar.gz`、`proxy-control-1.4.3-linux-arm64.tar.gz`、`sing-box-1.14.4-qui.1-linux-{amd64,arm64}.tar.gz` 及各自 `.provenance.txt`、`SHA256SUMS`、`node-install.sh`、`panel-install.sh`；Release 于 13:22:41 UTC 正式发布 |

## 生产升级

2026-11-08 约 13:25–13:29 UTC，经 Owner 同意后用 `qui-update` 从 GitHub Release v1.4.3 升级。

- **先面板：** us2 `1.4.2` → `1.4.3`（13:26:02 UTC 起服务，10 秒内无重启）；迁移头仍 **0134**（本版无迁移），`/etc/proxy-panel/config.yaml` 未改写（sha256 仍 `e57bc1d9…`，仍监听 `[::]:443`／`[::]:9443`）。
- **再逐台 Agent：** 按 uk（13:26:27 UTC）、Alpine（13:27:36 UTC）、us2（13:28:27 UTC）把 Agent 从 `1.4.2` 升到 `1.4.3`；四次 `qui-update` 均打印 `已更新到 1.4.3，服务运行正常` 并退出 0，一台确认在线后再升下一台。
- **核对：** 三台在线，UK/Alpine/US 修订仍为 295/296/291（无新修订、无回滚），各主机无残留 `rollback.*`；面板升级后日志 0 WARN／0 ERROR，Alpine 只在面板停机换版的约 3 秒内记录一次预期的控制流断开并自动重连；各 Agent 升级后无 WARN／ERROR。
- **恢复点：** 校验过的加密面板备份 `yO25OluW…`（13:25:42 UTC 验证，3,636,998 字节，sha256 `b4572e50…`）与更新器备份 `pre-update-1.4.2-20261108-212559.db`。
- **真实流量抽样：** 升级后经 uk-node 的 SOCKS5、HTTP、VLESS、Shadowsocks、Trojan 五个入口均返回 200；阻断规则仍阻断（TLS 握手被断开），错误密码仍被拒。
