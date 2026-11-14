# qui · 多节点代理控制平台

**当前生产环境（2026-11-13，Owner 指定）**：`5.45.96.43`（德国 netcup），面板 `https://qui.lluu.de`；管理员入口见[运维手册](docs/operations.md#当前生产环境)。下文 us2／uk／Alpine 的生产升级状态为历史记录，当前服务器的运行版本须另行核实。

面向 1～10 名内部用户的自托管平台，用于自有或明确授权网络。中央端为 Go 单体程序（SQLite ＋ 嵌入式 Vue 前端），Node Agent 经 gRPC ＋ mTLS 主动接入；代理流量由节点处理，不经过面板。

支持用户门户与节点组订阅、分流和域名级访问控制、协议中转/代理链、设备限制、用户额度、主机观测与整机月流量。节点可同时运行官方 Xray、官方或 qui 的未修改上游 sing-box 构建，以及仅做端口转发/加密中继的 GOST 3.3.2。中央失联时保留最后成功配置，配置失败自动回滚。

最新正式版 **v1.5.5**（2026-11-14，tag → `6066f93`，迁移头 **0148**）：订阅 DNS 与协议往返、服务器标签／排序／国旗、总览与页面刷新、中转入口生命周期、单独导入稳定身份、逐出站源地址及清除数据菜单。两套同 SHA 发布门禁与官方签名核验通过，见[发布说明](docs/release-notes-1.5.5.md)和[验收记录](docs/v1.5.5-acceptance.md)。**本次未升级生产。** 旧版本升级、helper 更新要求及未覆盖范围见发布说明；历史见[变更记录](docs/changelog.md)。

| 任务 | 入口 |
| --- | --- |
| 下载、安装、接入节点与升级 | [GitHub Releases](https://github.com/jingniao/qui/releases/latest)、[部署与升级](docs/deployment.md) |
| 从零安装：一键脚本、加节点、日常操作 | [安装与使用指南](docs/install-guide.md) |
| 备份恢复、证书、流量与排错 | [运维手册](docs/operations.md) |
| 理解模块、数据流与开发边界 | [架构总览](docs/architecture.md)、[项目说明书](AGENTS.md) |
| 查询协议与执行范围 | [协议矩阵](docs/protocol-support-matrix.md)、[设备限制](docs/device-limit.md) |
| 查找全部现行文档与历史 | [文档导航](docs/README.md) |

## 快速开始

在 Debian/Ubuntu 面板服务器上以 root 执行（交互式，逐项可回车用默认值，汇总确认后才动手；已安装的主机上同一脚本变成更新／状态／重启／卸载菜单）。默认监听 **22550**（Web/API/订阅）与 **18880**（节点 Agent gRPC），安装时都可以改；已有安装保持自己的端口。

```sh
wget -O panel-install.sh https://raw.githubusercontent.com/jingniao/qui/main/deploy/linux/panel-install.sh
bash panel-install.sh
```

首次登录、添加节点、创建入站、节点组与订阅、日常运维与常见问题见[项目安装与使用指南](docs/install-guide.md)；手工安装与升级细节见[部署与升级](docs/deployment.md)。

开发要求 Go（版本以 `go.mod` 为准）和 Node.js 22。构建、开发启动与 Linux 门禁命令统一见[部署与升级 §2](docs/deployment.md#2-构建发布包)。每个变更的证据来自 Linux 测试机的 `scripts/check.sh` 与真实核套件；GitHub 对发布提交再运行 `check`，标签构建要求同 SHA 已通过。

仅记录域名级元数据，不解密 HTTPS、不记录正文、不提供任意远程命令。数据库、真实配置、私钥与运行状态不进入发布包。主项目尚未选择开源许可证；代理核心遵循各自上游许可并保留来源。
