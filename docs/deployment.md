# 部署与升级

更新时间：2026-10-21（v1.0.0 发布后的同步）。本文命令取自 `deploy/linux/*.sh`、`deploy/systemd/*.service`、`cmd/panel` 和 `cmd/agent` 的实际参数（`go run ./cmd/panel -h`、`go run ./cmd/agent -h`），文件名与路径取自 `deploy/examples/*.yaml` 和安装脚本。

**未在 Linux 实机执行。** 端到端安装、升级和服务运行属于 owner 的 Linux VM 验收范围（见[验收矩阵](v1-acceptance.md)）。日常运维见[运维手册](operations.md)。

## 1. 前置条件

- Linux（`x86_64` 或 `aarch64`/`arm64`）、systemd 正在运行、以 root 本地执行脚本。
- 面板需要一对 Web TLS 证书：非开发模式下 `tls.certificate` 与 `tls.key` 是必填项（`internal/platform/config/config.go`）。证书不由本项目签发，用你自己的 PKI 或 ACME 工具生成。
- 节点需要签发面板 Web 证书的 CA 文件（第 1 条的 `tls.certificate`；面板的 gRPC 9443 也用同一张证书），默认放在 `/etc/proxy-agent/tls/ca.crt`，`server_name` 必须与该证书匹配。
- 节点若要使用服务器强制出站源地址，需要 nftables（Linux ≥ 5.2）；`install.sh --node` 在缺少 `nft` 时用发行版的包管理器（apt / dnf / yum）安装 `nftables`，装不上就拒绝安装。
- `install.sh` 会检查它用到的命令是否存在，缺失即拒绝：`systemctl install sha256sum od stat getent useradd groupadd flock id tr sed chmod rm mktemp rmdir sync mv ln dirname find grep head mkdir`；安装核心时还需要 `unzip`（Xray zip）或 `tar`（sing-box tar.gz），下载需要 `curl` 或 `wget`。

## 2. 构建发布包

**构建工具链**：用官方安装的 Go（<https://go.dev/dl/>），版本按 `go.mod` 里的 `go` 指令（当前 `go 1.26.0`，即需要 Go 1.26 或更新；仓库最后一次完整验证用的是 **Go 1.27.1**）。CI 通过 `actions/setup-go` 的 `go-version-file: go.mod` 读取同一个要求，Node 用 22 加锁定的依赖（`npm --prefix web ci`）。

仓库里可能出现的 `.tools/`（Go 工具链、`protoc`、缓存）只是作者本机的产物，**不是构建前提**：`scripts/build-release.sh`、`.github/workflows/check.yml` 和 `deploy/linux/install.sh` 只使用 `PATH` 上的 `go`、`npm`、`tar`。只有 `scripts/generate-proto.sh` 会在存在时优先用 `.tools/protoc`，否则回退到 `PATH` 上的 `protoc`。

```sh
VERSION=1.0.0 bash scripts/build-release.sh
```

- `SKIP_NPM_CI=1` 复用已安装的前端依赖（离线构建）；`OUTPUT_DIRECTORY=/abs/path` 改变输出目录，默认 `dist/`。
- 产物：`proxy-control-<version>-linux-amd64.tar.gz`、`proxy-control-<version>-linux-arm64.tar.gz` 和 `SHA256SUMS`。
- 每个 tar.gz 内含 `bin/proxy-panel`、`bin/proxy-agent` 与 `deploy/{linux,systemd,examples}`。
- 归档可复现：成员按名称排序、所有时间戳取源码提交时间（可用 `SOURCE_DATE_EPOCH` 覆盖）、属主统一为 root，gzip 不带时间戳，所以同一提交两次构建得到相同的 `SHA256SUMS`。
- 脚本不运行测试、不部署。

## 3. 安装面板

在解包目录内执行（脚本要求同级的 `systemd/` 与 `examples/`）：

```sh
tar -xzf proxy-control-1.0.0-linux-amd64.tar.gz
cd proxy-control-1.0.0-linux-amd64
sha256sum bin/proxy-panel
deploy/linux/install.sh --panel --binary bin/proxy-panel --sha256 <sha256>
```

`install.sh --help` 的完整参数：

```text
Usage: install.sh --panel|--node --binary FILE --sha256 DIGEST [options]

  --binary FILE          proxy-panel or proxy-agent executable to install
  --sha256 DIGEST        lowercase SHA256 of that executable
  --config FILE          config template for a first install
                         (default: the packaged examples/<component>.yaml)
  --core xray|sing-box   install the official core release with --node
  --core-version VERSION release version to download, for example 26.3.27
  --core-url URL         official release asset URL
                         (default: the official GitHub release asset)
  --core-sha256 DIGEST   lowercase SHA256 of the downloaded release archive
                         (default for xray: the release .dgst file)
  --reinstall            install again after uninstall.sh removed the service
```

面板安装脚本会：

1. 校验二进制是 64 位小端 ELF、架构与主机匹配、SHA256 与 `--sha256` 一致；
2. 创建服务账号 `proxy-panel` 与目录 `/etc/proxy-panel`、`/etc/proxy-panel/tls`、`/var/lib/proxy-panel`（0700）；
3. 安装 `/usr/local/bin/proxy-panel` 与 `/etc/systemd/system/proxy-panel.service`；
4. **首次安装**时把配置模板写到 `/etc/proxy-panel/config.yaml`（0600）；
5. 写入安装记录 `/var/lib/proxycontrol-install/panel.manifest`，不启动任何服务。

安装后：

```sh
install -o proxy-panel -g proxy-panel -m 0600 your-server.crt /etc/proxy-panel/tls/server.crt
install -o proxy-panel -g proxy-panel -m 0600 your-server.key /etc/proxy-panel/tls/server.key
$EDITOR /etc/proxy-panel/config.yaml          # http_address / grpc_address / database / tls
/usr/local/bin/proxy-panel --config /etc/proxy-panel/config.yaml --check-config
systemctl enable --now proxy-panel.service
```

配置字段与示例见 `deploy/examples/panel.yaml`。`node_identity_directory`（示例 `/var/lib/proxy-panel/identity`）是面板自己的节点 CA 目录，首次启动时自动创建，不要指向网络共享目录。

**首次创建 Owner**：数据库里还没有管理员时，面板生成一次性安装令牌，在日志里以 WARN 打印一次（`first-run setup token`），并写入 `<database 所在目录>/setup-token`（0600）。用浏览器打开 `https://<host>/`，在初始化页面粘贴该令牌并设置 Owner 用户名、密码、平台名称和时区；成功后令牌与文件立即失效。令牌校验在 PBKDF2 之前完成，错误令牌不会消耗 CPU。

`journalctl -u proxy-panel -f` 查看日志；程序只写 stdout，不写 `/var/log` 下的文件。

面板安装脚本还会安装终端菜单 `/usr/local/bin/qui`（root 专用）。在面板主机的 SSH 会话里输入 `qui` 即可打开菜单：1. 修改管理员账号密码、2. 修改端口、3. 重启、4. 启动、5. 停止、6. 更新（从 GitHub）、0. 退出。它只做这几件事，细节见[运维手册](operations.md)。

## 4. 接入节点

**第 1 步（面板）**：在“节点”页创建节点记录，生成一次性注册码。注册码只能使用一次；节点记录被禁用时注册码不可用。创建成功后对话框同时给出**一键安装命令**，把它粘贴到新服务器的 root shell 里执行即可完成安装、登记与启动。

### 一键安装（推荐）

```sh
curl -fsSL https://github.com/jingniao/qui/releases/download/v1.0.0/node-install.sh | bash -s -- \
  --version 1.0.0 --panel panel.example.com --grpc-port 9443 \
  --ca-sha256 <面板显示的 64 位小写十六进制> --token <一次性注册码> --core xray
```

- 命令由面板生成：版本是面板自身的版本，端口取自 `grpc_address`，摘要是面板 Web 证书信任锚的 SHA-256。只有**正式发布版本**（X.Y.Z）且面板以非开发模式运行时对话框才显示该命令，否则提示按下面的手工步骤安装。
- `node-install.sh` 从 `https://github.com/jingniao/qui/releases/download/v<版本>/` 下载与面板**同版本**的归档和 `SHA256SUMS`，核对摘要后解压，再用归档里的 `deploy/linux/install.sh` 安装 Agent 与固定的官方核心（xray 26.3.27；sing-box 1.14.3，摘要按架构固定）。因此这台服务器必须能访问 GitHub，且**必须先有与该面板版本一致的发布**。
- 面板证书信任按两种情况之一建立：先尝试用系统信任链验证 `https://<面板>/install/ca.pem`，成功说明面板证书由公网 CA 签发（例如 Let's Encrypt，其中间证书会轮换），此时把系统 CA 包（`/etc/ssl/certs/ca-certificates.crt` 或 `/etc/pki/tls/certs/ca-bundle.crt`）安装为 `/etc/proxy-agent/tls/ca.crt`；失败则改用 `curl -k` 取回该文件，并要求它的 SHA-256 等于 `--ca-sha256`，不一致立即中止（可能中间人攻击，或面板地址/端口填错）。
- 之后脚本把 `control_address` 与 `server_name` 写入 `/etc/proxy-agent/config.yaml`（保持原属主与权限），用一次性注册码登记，最后 `systemctl enable --now proxy-agent.service`。
- 注册码只能用一次且会过期；它出现在新服务器的 shell 历史里，登记成功后请清理该历史。节点**登记成功前**可以重跑这条命令：会原地升级 Agent 并用新的注册码重新登记，登记要求本机能访问面板的 gRPC 端口（默认 9443）；已登记的主机再跑会被拒绝，升级用 `qui-update`。

### 手工安装（备选）

**第 2 步（节点）**：安装 Agent，需要时同时安装官方核心。

```sh
tar -xzf proxy-control-1.0.0-linux-amd64.tar.gz
cd proxy-control-1.0.0-linux-amd64
deploy/linux/install.sh --node --binary bin/proxy-agent --sha256 <sha256> \
  --core xray --core-version 26.3.27
```

- `--core` 只允许 `xray` 或 `sing-box`，且必须给出 `--core-version`；脚本对版本号只做宽松格式校验，能装到的仍是官方发布的稳定版本（面板的“核心版本”入口额外要求纯发布版本号，如 `26.3.27`）。
- 归档 URL 只能来自官方发布前缀（`https://github.com/XTLS/Xray-core/releases/download/`、`https://github.com/SagerNet/sing-box/releases/download/`）。
- SHA256 来自 `--core-sha256`，或对 Xray 自动读取同版本的 `.dgst` 文件；**没有可信摘要就不安装**。
- 核心安装到 `/usr/local/lib/proxy-agent/<core>`，摘要记入安装记录；首次安装 sing-box 时脚本会把配置模板里的 `core_name`/`core_path` 改成 sing-box。
- 脚本还会创建服务账号 `proxy-agent` 和目录 `/etc/proxy-agent`、`/etc/proxy-agent/tls`、`/var/lib/proxy-agent`，不启动服务。
- 脚本还会创建 `/var/lib/proxy-agent/host-network`（0700，服务器强制出站的 helper 目录），安装并启用两个固定单元：`proxy-agent-host-network.service`（oneshot，`proxy-agent --apply-host-network /var/lib/proxy-agent/host-network`，只带 `CAP_NET_ADMIN`）与监视 `desired.json` 的 `proxy-agent-host-network.path`。它们只维护一个 nft 表 `inet proxy_agent_source`，不改变主机上的其他规则。

**第 3 步（节点）**：配置控制地址与受信 CA。

```sh
install -o root -g proxy-agent -m 0644 /path/to/web-ca.crt /etc/proxy-agent/tls/ca.crt
$EDITOR /etc/proxy-agent/config.yaml     # control_address / server_name / core_name / core_path
```

`ca.crt` 必须是签发面板 Web 证书（`tls.certificate`，含其证书链的顶层）的 CA；它由你自己的 PKI/ACME 提供，不是 `/var/lib/proxy-panel/identity/ca.crt`。后者是面板**节点 CA**，只签发节点客户端证书，登记时由面板下发到 Agent 的 `identity/`，不需要手工安装。面板 Web 证书换了签发 CA 时，用 `deploy/linux/node-ca.sh --trust` 替换本文件（见[运维手册](operations.md)）。配置示例见 `deploy/examples/agent.yaml`。

**第 4 步（节点）**：登记。

```sh
deploy/linux/enroll.sh --token-file /root/node1.token
systemctl enable --now proxy-agent.service
```

`enroll.sh` 会校验：token 文件是普通文件、大小 ≤512 字节、权限 0600/0400、只有一个硬链接；安装记录完整且二进制摘要未变；`proxy-agent.service` 处于停止状态；`/etc/proxy-agent/config.yaml` 与 `/var/lib/proxy-agent` 存在。它把 token 复制成 `proxy-agent` 可读的临时文件，以 `--config /etc/proxy-agent/config.yaml --enroll --token-file <临时文件>` 调用 Agent，登记成功后退出，不启动核心或服务。源 token 文件保留，确认登记成功后请自行安全删除。

**第 5 步（面板）**：在“代理 → 入站/出站”和“分流”里配置该节点的入站与规则，然后创建并发布配置版本（`POST /api/v1/revisions`、`POST /api/v1/revisions/{id}/deploy`）。发布前节点只上报状态，不代理流量。

## 5. 升级

**正常方式（面板与节点主机）：qui-update**

`install.sh` 会把 `deploy/linux/update.sh` 装成 `/usr/local/bin/qui-update`（面板与节点主机都有；面板主机的 `qui` 菜单第 6 项也调用它）。

```sh
qui-update                  # 安装 GitHub 上的最新发布
qui-update --version 1.0.1  # 指定版本
qui-update --yes            # 不询问确认
```

- 它从 `https://github.com/jingniao/qui/releases` 下载 `proxy-control-<版本>-linux-<架构>.tar.gz` 和同一发布的 `SHA256SUMS`，按文件名核对归档的 SHA-256 后才解包，然后调用归档里的 `deploy/linux/install.sh` 原地替换程序、systemd 单元和脚本（`qui`、`qui-update`）；配置、节点身份与数据库都不动。**摘要只证明下载内容与仓库发布一致，信任仍建立在 GitHub 仓库本身**：它不下载也不执行别的东西。
- 只接受 `vMAJOR.MINOR.PATCH` 形式的发布；版本由 `--version X.Y.Z` 指定，否则取 GitHub 上的最新发布。
- 面板更新：先停止 `proxy-panel.service`，在停止状态下把数据库（以及存在的 `-wal`）复制到 `/var/lib/proxy-panel/backups/pre-update-<旧版本>-<YYYYmmdd-HHMMSS>.db`（属主 `proxy-panel`、0600），再替换程序，最后按停止前的状态启动服务并打印状态。
- 节点更新：节点主机用 SSH 登录后运行同一条命令（需要 root）。同一台主机同时装有面板与节点时，两个组件从同一个归档更新，**先节点后面板**。
- 失败时旧程序仍在原处（`install.sh` 用一次 rename 发布），脚本按停止前的状态把服务启动回来，并打印 `journalctl -u proxy-<组件> -n 50`、降级命令 `qui-update --version <旧版本>` 和数据库备份路径。**数据库迁移不可逆**：降级前先用该备份恢复数据库。
- **先更新面板，再更新节点**；面板升级期间节点继续用旧配置运行。

以下手动步骤用于离线环境，或需要用自己构建的归档时。

**面板**

```sh
systemctl stop proxy-panel.service
deploy/linux/install.sh --panel --binary bin/proxy-panel --sha256 <new-sha256>
systemctl start proxy-panel.service
```

升级会替换程序、systemd 单元和安装记录，服务账号、目录、配置与数据保持不变。**`--config` 在 `/etc/proxy-panel/config.yaml` 已存在时被忽略**（`install.sh` 只在首次安装写配置）；要改配置请直接编辑该文件。脚本拒绝在服务运行中替换程序。

**节点**

```sh
systemctl stop proxy-agent.service
deploy/linux/install.sh --node --binary bin/proxy-agent --sha256 <new-sha256>
systemctl start proxy-agent.service
```

节点升级同样保留 `config.yaml`、`identity/`、`revisions/`、`spool/`。注意：`install.sh` 只替换 `/usr/local/bin/proxy-agent`（引导镜像）。已受管的节点运行时使用 `/var/lib/proxy-agent/releases/agent-installed/` 下的镜像，**重跑 `install.sh` 不会改变该镜像**；Agent 自身升级要走面板的 Agent 更新任务（系统 → Agent），它按签名清单校验，并在失败时回滚。

**发布新版本**：在仓库里推一个 `vX.Y.Z` 标签即可。CI 的 `release` 任务（`.github/workflows/check.yml`，`needs: check`，只在 `refs/tags/v*` 上运行）会拒绝不是 `vMAJOR.MINOR.PATCH` 的标签，用 `scripts/build-release.sh` 构建两个架构的归档与 `SHA256SUMS`，再用 `gh release create` 建发布并附上这些文件。`qui-update` 默认安装最新发布，所以打标签就等于发布。

**代理核心**：不走 `install.sh`。在面板“系统 → 核心版本”里填写组件、稳定版本号、官方 GitHub 发布资源 URL 和归档 SHA-256；面板排队下发，Agent 在节点上再次校验摘要、解包、切换，并在健康检查失败时回滚到上一镜像。`install.sh --core` 只用于首装。

## 6. 卸载

```sh
deploy/linux/uninstall.sh panel     # 或 agent
```

`uninstall.sh` 会先停止服务，确认 `ActiveState=inactive` 且 `Result=success`，再删除程序、systemd 单元和安装记录，保留身份、配置、数据、备份与服务账号。它在 `/var/lib/proxycontrol-install/` 留下 `<component>.removed` 记录；再次安装必须加 `--reinstall`，否则脚本拒绝。

## 7. 目录与权限

| 路径 | 属主 / 权限 | 内容 |
| --- | --- | --- |
| `/usr/local/bin/proxy-panel`、`/usr/local/bin/proxy-agent` | root:root 0755 | 程序 |
| `/etc/proxy-panel/config.yaml` | proxy-panel:proxy-panel 0600 | 面板配置 |
| `/etc/proxy-panel/tls/` | proxy-panel:proxy-panel 0750 | Web TLS 证书与私钥 |
| `/etc/proxy-agent/config.yaml` | proxy-agent:proxy-agent 0600 | 节点配置 |
| `/etc/proxy-agent/tls/ca.crt` | root:proxy-agent 0644 | 受信中央 CA（签发面板 Web 证书的 CA） |
| `/var/lib/proxy-panel/` | proxy-panel:proxy-panel 0700 | `panel.db`、`identity/`、`backups/` |
| `/var/lib/proxy-agent/` | proxy-agent:proxy-agent 0700 | `identity/`、`revisions/`、`spool/`、`runtime/`、`releases/` |
| `/var/lib/proxy-agent/host-network/` | proxy-agent:proxy-agent 0700 | `desired.json`、`status.json`（服务器强制出站，只有 helper 单元能写） |
| `/var/lib/proxycontrol-install/` | root:root 0700 | 安装记录与操作锁 |
| `/usr/local/lib/proxy-agent/<core>` | root:root 0755 | 安装脚本放置的核心 |

两个 systemd 单元都以专用非 root 账号运行，启用 `NoNewPrivileges`、`ProtectSystem=strict`、`PrivateTmp`、`ProtectHome`，只保留 `CAP_NET_BIND_SERVICE`。

节点端另有两个固定单元：`proxy-agent-host-network.service`（oneshot，`User=proxy-agent`，只带 `CAP_NET_ADMIN`，`ReadWritePaths` 只放开 `/var/lib/proxy-agent/host-network`）和 `proxy-agent-host-network.path`（监视该目录的 `desired.json`；开机时由 oneshot 单元重放保留的规则）。`uninstall.sh agent` 会停止并删除这两个单元，并删除 nft 表 `inet proxy_agent_source`。

## 8. 排错入口

```sh
/usr/local/bin/proxy-panel --config /etc/proxy-panel/config.yaml --check-config
/usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml --check-config
/usr/local/bin/proxy-panel --version
/usr/local/bin/proxy-agent --version
systemctl status proxy-panel.service proxy-agent.service
journalctl -u proxy-panel -n 200
journalctl -u proxy-agent -n 200
ss -lntp
```

- 面板起不来：先跑 `--check-config`；非开发模式下证书/私钥缺失、`database` 落在网络路径都会被拒绝。
- 节点登记失败：确认服务已停止、token 文件权限与硬链接数正确、安装记录未被改动、`/etc/proxy-agent/tls/ca.crt` 是签发面板 Web 证书的 CA 且与 `server_name` 匹配。
- 节点连上后立刻断开：检查面板里该节点是否被禁用、证书是否过期或被吊销（见[运维手册](operations.md)）。

