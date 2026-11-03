# Proxy Control

面向 1～10 名内部用户的自托管多节点代理控制平台。中央端是一个 Go 单体程序（SQLite + 嵌入式 Vue 前端），Node Agent 通过 gRPC + mTLS 主动接入并管理**官方** Xray 或 sing-box 二进制（以及只承载端口转发的固定版本 GOST 3.3.2）；用户代理流量不经过中央端。

当前状态：**v1.3.0 是最新发布版**（2026-11-03）（GitHub Release，仓库 <https://github.com/jingniao/qui>）：v1.0.1 起节点 Agent 支持 Alpine/OpenRC，v1.0.2 起 `qui-update` 失败时自动回滚，v1.0.3 起更新中断（含断电）后自动恢复、面板可带数据库跨迁移降级，v1.0.4 修复强制出站 helper 连续失败后不再执行新配置，v1.0.5 让 Alpine 节点可用 musl sing-box，v1.0.6 起核心运行不了保留的修订时 Agent 保持连接、不再反复重启，v1.0.7–v1.0.9 修复删除用户或设备后的凭据与上传、恢复备份与核心更新回执等问题。v1.2.0 合并了 v1.1 与 v1.2 的功能（用户门户、设备限制、多核心、服务器调优、端口转发、更多订阅格式、主机监控）与 CVE-2026-98432 缓解，预发布版 rc.1–rc.4 已在三台主机验收；逐项结果见[验收矩阵](docs/v1-acceptance.md) §11，发布说明见 [docs/release-notes-1.2.0.md](docs/release-notes-1.2.0.md)。v1.3.0 增加整机月流量（每台服务器单张网卡、可选额度与重置日）、把 Xray 端口转发字节计入节点总量，并把 GOST 3.3.2 作为第三种核（转发目标池/端口范围/限流与节点间加密中继）；rc.1–rc.6 已在三台主机验收，逐项结果见[验收矩阵](docs/v1-acceptance.md) §12，发布说明见 [docs/release-notes-1.3.0.md](docs/release-notes-1.3.0.md)。v1.3.1 是紧随其后的缺陷修复版（移除核心后的运行目录清理、画布只重发布改动的服务器、画布管理的入站不能改名、Agent 本地队列的容量设置不再随连接丢失），预发布版 rc.2 已在三台主机上运行，RC-F 与 RC-G 均通过，见 [docs/release-notes-1.3.1.md](docs/release-notes-1.3.1.md)。

## 快速开始

```sh
VERSION=1.0.9 bash scripts/build-release.sh      # 产出 Linux amd64/arm64 包与 SHA256SUMS
```

在目标 Linux 主机上（root、systemd）：

```sh
tar -xzf proxy-control-1.0.9-linux-amd64.tar.gz
cd proxy-control-1.0.9-linux-amd64
deploy/linux/install.sh --panel --binary bin/proxy-panel --sha256 <sha256>
# 放好 Web TLS 证书、改好 /etc/proxy-panel/config.yaml，然后
systemctl enable --now proxy-panel.service       # 首次访问用日志/文件里的一次性令牌创建 Owner
```

节点侧安装与登记：

```sh
deploy/linux/install.sh --node --binary bin/proxy-agent --sha256 <sha256> \
  --core xray --core-version 26.3.27
deploy/linux/enroll.sh --token-file /root/node1.token
systemctl enable --now proxy-agent.service
```

上面是手工步骤。面板登记节点后还会给出**一键安装命令**（`deploy/linux/node-install.sh`），在目标机 root shell 里粘贴执行即可完成下载、校验、面板证书信任、登记与启动。之后的面板与节点升级都用 `qui-update`（从 GitHub Release 取同版本归档并核对摘要）；面板主机上 root 输入 `qui` 可打开终端菜单（改管理员口令、改 Web 端口、重启/启停、更新）。

完整步骤、参数说明和排错见 **[部署与升级](docs/deployment.md)**。

## 文档

| 目的 | 文档 |
| --- | --- |
| 需求与架构约束、当前状态 | [项目说明书](AGENTS.md) |
| 系统组成与数据流 | [架构总览](docs/architecture.md) |
| 前端设计系统与按钮、表单规范 | [设计系统规范](docs/design-system.md) |
| 安装、接入节点、升级 | [部署与升级](docs/deployment.md) |
| 备份恢复、证书、额度、故障 | [运维手册](docs/operations.md) |
| 协议与能力范围 | [协议支持矩阵](docs/protocol-support-matrix.md) |
| 验收标准与实机清单 | [验收矩阵](docs/v1-acceptance.md) |
| 版本计划与发布说明 | [v1.3 计划](docs/v1.3-plan.md)、[发布说明 1.3.0](docs/release-notes-1.3.0.md)、[发布说明 1.3.1](docs/release-notes-1.3.1.md) |
| 版本修订记录 | [变更记录](docs/changelog.md) |
| 文档索引与历史归档 | [文档导航](docs/README.md) |

## 本地开发

要求 Go 1.26+ 与 Node.js 22+。

```sh
npm --prefix web ci
npm --prefix web run build
go run ./cmd/panel --config deploy/examples/panel.dev.yaml
go run ./cmd/agent --config deploy/examples/agent.dev.yaml
```

开发 Web 地址 `http://127.0.0.1:8080`，gRPC 为 `127.0.0.1:9443`，数据库在 `state/`。前端热更新用 `npm --prefix web run dev`（页面 `http://localhost:3000`，API 转发到中央端）。

门禁命令：`go build ./...`、`go vet ./...`、`go test -count=1 ./...`、`npm --prefix web run build`、`npm run test:time`；真实核用例需要 `XRAY_BINARY` / `SINGBOX_BINARY`，未设置时跳过（跳过不等于通过）。

## 项目结构

| 目录 | 内容 |
| --- | --- |
| `cmd/panel`、`cmd/agent`、`cmd/release` | 中央程序、节点程序、离线签名工具 |
| `internal/panel` | 中央业务模块 |
| `internal/agent` | 节点控制、核心适配、采集、缓存、更新 |
| `internal/platform` | 配置、数据库、服务装配、证书、恢复 |
| `pkg/contracts`、`proto` | 共享数据结构与节点通信协议 |
| `web` | Vue 前端及静态资源嵌入 |
| `migrations` | 版本化数据库迁移 |
| `deploy`、`scripts` | 部署脚本、systemd 单元、示例配置与构建入口 |

## 必须保持的边界

仅用于自有或明确授权网络。保留 Go 模块化单体与本地 SQLite；配置草稿统一经 Deployment 发布；节点离线时保持最后成功配置。只收集域名级元数据，不解密 HTTPS、不记录正文或凭据、不提供任意 Shell，也不在核内做硬性并发准入或字节配额（见 AGENTS 第 3、9.1 节）。

数据库、私钥、真实配置与运行状态不进入发布包。主项目尚未选择开源许可证；扩展核心遵循对应上游许可并保留来源。
