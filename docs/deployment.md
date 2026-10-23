# 部署与升级

更新时间：2026-10-22（节点 Agent 支持 Alpine / OpenRC）。本文命令取自 `deploy/linux/*.sh`、`deploy/systemd/*.service`、`deploy/openrc/*`、`cmd/panel` 和 `cmd/agent` 的实际参数（`go run ./cmd/panel -h`、`go run ./cmd/agent -h`），文件名与路径取自 `deploy/examples/*.yaml` 和安装脚本。

**未在 Linux 实机执行。** 端到端安装、升级和服务运行属于 owner 的 Linux VM 验收范围（见[验收矩阵](v1-acceptance.md)）。日常运维见[运维手册](operations.md)。

## 1. 前置条件

- Linux（`x86_64` 或 `aarch64`/`arm64`），以 root 本地执行脚本。
- 初始化系统：
  - **面板**需要 systemd（Debian、Ubuntu 等）；在 OpenRC 主机上 `install.sh --panel` 会被拒绝。
  - **节点 Agent** 支持 systemd，也支持 **OpenRC**（Alpine 3.19+，OpenRC ≥ 0.45，含没有 systemd 的 LXC 容器）。脚本按 `/run/systemd/system` 或 `/run/openrc` 自动判断，两者都没有就拒绝；对应的服务定义分别是 `deploy/systemd/` 与 `deploy/openrc/`，细节见第 4.1 节。
- 面板需要一对 Web TLS 证书：非开发模式下 `tls.certificate` 与 `tls.key` 是必填项（`internal/platform/config/config.go`）。证书不由本项目签发，用你自己的 PKI 或 ACME 工具生成。
- 节点需要签发面板 Web 证书的 CA 文件（第 1 条的 `tls.certificate`；面板的 gRPC 9443 也用同一张证书），默认放在 `/etc/proxy-agent/tls/ca.crt`，`server_name` 必须与该证书匹配。
- 节点若要使用服务器强制出站源地址，需要 nftables（Linux ≥ 5.2）。`install.sh --node` 在 systemd 主机上缺少 `nft` 时用发行版的包管理器（apt / dnf / yum）安装 `nftables`，装不上就拒绝安装；在 Alpine 上它随下一条的 apk 依赖一起安装。
- 在 Alpine（OpenRC 且有 `apk`）上，`install.sh --node` 先执行 `apk add --no-cache bash curl ca-certificates coreutils findutils grep sed tar unzip shadow runuser nftables`（已装的包不变），因此后面的命令与 Debian 上相同。
- `install.sh` 会检查它用到的命令是否存在，缺失即拒绝：`install sha256sum od stat getent useradd groupadd flock id tr sed chmod rm mktemp rmdir sync mv ln dirname find grep head mkdir`，systemd 上另加 `systemctl`，OpenRC 上另加 `rc-service rc-update openrc-run`。安装核心时还需要 `unzip`（Xray zip）或 `tar`（sing-box tar.gz），下载需要 `curl` 或 `wget`。最小化安装的 Debian/Ubuntu 没有 `unzip`：systemd 主机上缺少时，`install.sh` 像 `nftables` 一样用 apt / dnf / yum 安装它（实测：debootstrap minbase 的 Debian 13）。
- 脚本里所有需要校验证书的 `curl` 调用都带 `--no-insecure`。它会覆盖 curl 包装脚本或别名加在前面的 `-k`：实测有一台 Alpine LXC 的 `/usr/bin/curl` 被换成了 `curl.orig -k "$@"`。

## 2. 构建发布包

**构建工具链**：用官方安装的 Go（<https://go.dev/dl/>），版本按 `go.mod` 里的 `go` 指令（当前 `go 1.26.0`，即需要 Go 1.26 或更新；仓库最后一次完整验证用的是 **Go 1.27.1**）。CI 通过 `actions/setup-go` 的 `go-version-file: go.mod` 读取同一个要求，Node 用 22 加锁定的依赖（`npm --prefix web ci`）。

仓库里可能出现的 `.tools/`（Go 工具链、`protoc`、缓存）只是作者本机的产物，**不是构建前提**：`scripts/build-release.sh`、`.github/workflows/check.yml` 和 `deploy/linux/install.sh` 只使用 `PATH` 上的 `go`、`npm`、`tar`。只有 `scripts/generate-proto.sh` 会在存在时优先用 `.tools/protoc`，否则回退到 `PATH` 上的 `protoc`。

```sh
VERSION=1.0.2 bash scripts/build-release.sh
```

- `SKIP_NPM_CI=1` 复用已安装的前端依赖（离线构建）；`OUTPUT_DIRECTORY=/abs/path` 改变输出目录，默认 `dist/`。
- 产物：`proxy-control-<version>-linux-amd64.tar.gz`、`proxy-control-<version>-linux-arm64.tar.gz` 和 `SHA256SUMS`。
- 每个 tar.gz 内含 `bin/proxy-panel`、`bin/proxy-agent` 与 `deploy/{linux,systemd,openrc,examples}`；同一个归档既能装 Debian/Ubuntu，也能装 Alpine。
- 归档可复现：成员按名称排序、所有时间戳取源码提交时间（可用 `SOURCE_DATE_EPOCH` 覆盖）、属主统一为 root，gzip 不带时间戳，所以同一提交两次构建得到相同的 `SHA256SUMS`。
- 脚本不运行测试、不部署。

## 3. 安装面板

在解包目录内执行（脚本要求同级的 `systemd/` 与 `examples/`）：

```sh
tar -xzf proxy-control-1.0.2-linux-amd64.tar.gz
cd proxy-control-1.0.2-linux-amd64
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
if command -v apk >/dev/null 2>&1; then apk add --no-cache bash curl ca-certificates; \
elif ! command -v curl >/dev/null 2>&1 && command -v apt-get >/dev/null 2>&1; then apt-get update -qq && apt-get install -y -qq curl ca-certificates; fi; \
curl --no-insecure -fsSL https://github.com/jingniao/qui/releases/download/v1.0.2/node-install.sh | bash -s -- \
  --version 1.0.2 --panel panel.example.com --grpc-port 9443 \
  --ca-sha256 <面板显示的 64 位小写十六进制> --token <一次性注册码> --core xray
```

- 开头的 `if … fi;` 只补齐这条命令自己需要的工具：Alpine 上用 apk 安装 bash 与 curl（原版 Alpine 两者都没有，root 的登录 shell 是 busybox ash）；Debian/Ubuntu 上只在没有 curl 时用 apt 安装（最小化安装可能没有）；已经具备时什么也不做。它在 ash、dash 与 bash 里都合法，同一条命令可以直接粘贴到 Debian、Ubuntu 和 Alpine（含 LXC）的 root shell 里。
- **离线安装**：在 `node-install.sh` 后加 `--bundle <proxy-control-<版本>-linux-<架构>.tar.gz> --sums <SHA256SUMS>`（两个必须同时给出），脚本就改用本地文件而不访问 GitHub 下载归档。本地归档仍要按 `SHA256SUMS` 核对；Xray 核心仍从官方发布地址下载。

- 命令由面板生成：版本是面板自身的版本，端口取自 `grpc_address`，摘要是面板 Web 证书信任锚的 SHA-256。只有**正式发布版本**（X.Y.Z）且面板以非开发模式运行时对话框才显示该命令，否则提示按下面的手工步骤安装。
- `node-install.sh` 从 `https://github.com/jingniao/qui/releases/download/v<版本>/` 下载与面板**同版本**的归档和 `SHA256SUMS`，核对摘要后解压，再用归档里的 `deploy/linux/install.sh` 安装 Agent 与固定的官方核心（xray 26.3.27；sing-box 1.14.3，摘要按架构固定）。因此这台服务器必须能访问 GitHub，且**必须先有与该面板版本一致的发布**。
- 面板证书信任按两种情况之一建立：先尝试用系统 CA 包（即随后会被安装为节点 CA 的那个文件，`--cacert` 指定，并带 `--no-insecure`）验证 `https://<面板>/install/ca.pem`，成功说明面板证书由公网 CA 签发（例如 Let's Encrypt，其中间证书会轮换），此时把系统 CA 包（`/etc/ssl/certs/ca-certificates.crt` 或 `/etc/pki/tls/certs/ca-bundle.crt`）安装为 `/etc/proxy-agent/tls/ca.crt`；失败则改用 `curl -k` 取回该文件，并要求它的 SHA-256 等于 `--ca-sha256`，不一致立即中止（可能中间人攻击，或面板地址/端口填错）。
- 之后脚本把 `control_address` 与 `server_name` 写入 `/etc/proxy-agent/config.yaml`（保持原属主与权限），用一次性注册码登记，最后启用并启动 Agent：systemd 上是 `systemctl enable` + `start proxy-agent.service`，OpenRC 上是 `rc-update add proxy-agent default` + `rc-service proxy-agent start`。
- 注册码只能用一次且会过期；它出现在新服务器的 shell 历史里，登记成功后请清理该历史。节点**登记成功前**可以重跑这条命令：会原地升级 Agent 并用新的注册码重新登记，登记要求本机能访问面板的 gRPC 端口（默认 9443）；已登记的主机再跑会被拒绝，升级用 `qui-update`。
- 安装失败（例如缺少依赖）时脚本报告 `Agent 安装失败（原因见上面 install.sh 的输出）`：`install.sh` 可能已经替换了部分文件，但本机尚未登记，修正原因后直接重跑同一条命令即可。`install.sh` 不会留下暂存目录或 `.installing` 临时文件。
- 失败的登记会留下重试标记 `/var/lib/proxy-agent/enrollment-attempt.json` 和它生成的私钥。Agent 把这两个文件绑定到原注册码与面板，不允许换绑。所以重跑时，只要本机既没有证书也没有 `enrollment-result.json`，脚本就把它们移到 `/var/lib/proxy-agent-enrollment-retired.XXXXXXXX/`（root 0700，**不删除**），再用新注册码登记；新登记成功后可以删除该目录。存在 `enrollment-result.json` 时，面板可能已经签发过证书，脚本拒绝继续，处理方法见运维手册第 5 节。

### 4.1 Alpine / OpenRC 节点

一键安装与手工安装在 Alpine 上用同一套脚本，区别只在服务定义与服务管理命令：

| 作用 | systemd | OpenRC |
| --- | --- | --- |
| Agent 服务 | `/etc/systemd/system/proxy-agent.service` | `/etc/init.d/proxy-agent`（`supervise-daemon`） |
| 强制出站源地址 | `proxy-agent-host-network.service`（oneshot）+ `.path`（监视 `desired.json`） | `/etc/init.d/proxy-agent-host-network`，监管 `/usr/local/lib/proxy-agent/host-network-watch`：启动时先应用一次已保留的 `desired.json`（相当于开机重放），再用 busybox `inotifyd` 等待 Agent 发布新文件 |
| 低端口能力 | `AmbientCapabilities=CAP_NET_BIND_SERVICE` | `capabilities="^cap_net_bind_service"`（ambient，核心继承） |
| 强制出站 helper 能力 | `AmbientCapabilities=CAP_NET_ADMIN` | `capabilities="^cap_net_admin"` |
| 崩溃重启 | `Restart=on-failure`，`RestartSec=5` | `respawn_delay=5`，`respawn_max=0` |
| 停止 | `TimeoutStopSec=90`，`KillMode=mixed` | `retry="TERM/90/KILL/5"`，`rc_cgroup_cleanup="yes"` |
| 日志 | journald（`journalctl -u proxy-agent`） | `logger` → syslog（`/var/log/messages`，标签 `proxy-agent`、`proxy-agent-host-network`） |
| 开机启动 | `systemctl enable` | `rc-update add … default` |

- 两个服务都以 `proxy-agent` 账号运行，带 `no_new_privs`，`umask=077`，`directory=/var/lib/proxy-agent`。systemd 单元里的 `ProtectSystem`、`ProtectHome`、`PrivateTmp`、`ProtectKernel*`、`ProtectControlGroups`、`RestrictSUIDSGID`、`ReadWritePaths` 在 OpenRC 里没有对应项，所以 OpenRC 节点没有这层文件系统隔离。
- 代理核心以 `Pdeathsig=SIGKILL` 启动（`internal/agent/core/procattr_linux.go`）。supervise-daemon 只向 Agent 本身发信号，所以 Agent 被杀或崩溃时由内核杀掉核心，不会留下占着端口的孤儿核心（systemd 上同样生效，与 `KillMode=mixed` 不冲突）。
- `/etc/machine-id`：Agent 判断上一次核心是否退出时要用 machine-id、boot_id 和 pid 命名空间三者，原版 Alpine 没有 machine-id。缺失或无效时，`install.sh --node` 在 OpenRC 上会创建它：优先复制有效的 `/var/lib/dbus/machine-id`，否则写 32 位随机十六进制，root 0444。已有的有效文件不改，systemd 主机上不碰。
- 安装记录 `agent.manifest` 第 10 行写 `openrc`；没有第 10 行表示 systemd。`uninstall.sh` 发现记录与本机初始化系统不一致时拒绝执行。
- `install.sh` 还会安装 `/usr/local/lib/proxycontrol/service.sh`（两种初始化系统通用的服务命令库，systemd 主机也装），供 `qui-update` 与单独拷出的脚本使用。
- 服务管理命令见[运维手册](operations.md)第 1 节。

### 手工安装（备选）

**第 2 步（节点）**：安装 Agent，需要时同时安装官方核心。

```sh
tar -xzf proxy-control-1.0.2-linux-amd64.tar.gz
cd proxy-control-1.0.2-linux-amd64
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
# OpenRC（Alpine）：
# rc-update add proxy-agent default && rc-service proxy-agent start
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
qui-update --version 1.0.2 --bundle proxy-control-1.0.2-linux-amd64.tar.gz --sums SHA256SUMS
                            # 离线：用本地归档与其 SHA256SUMS，不访问 GitHub
```

- 它从 `https://github.com/jingniao/qui/releases` 下载 `proxy-control-<版本>-linux-<架构>.tar.gz` 和同一发布的 `SHA256SUMS`，按文件名核对归档的 SHA-256 后才解包，然后调用归档里的 `deploy/linux/install.sh` 原地替换程序、systemd 单元和脚本（`qui`、`qui-update`）；配置、节点身份与数据库都不动。**摘要只证明下载内容与仓库发布一致，信任仍建立在 GitHub 仓库本身**：它不下载也不执行别的东西。
- 只接受 `vMAJOR.MINOR.PATCH` 形式的发布；版本由 `--version X.Y.Z` 指定，否则取 GitHub 上的最新发布。
- 面板更新：先停止 `proxy-panel.service`，在停止状态下把数据库（以及存在的 `-wal`）复制到 `/var/lib/proxy-panel/backups/pre-update-<旧版本>-<YYYYmmdd-HHMMSS>.db`（属主 `proxy-panel`、0600），再替换程序，最后按停止前的状态启动服务并打印状态。
- 节点更新：节点主机用 SSH 登录后运行同一条命令（需要 root）。同一台主机同时装有面板与节点时，两个组件从同一个归档更新，**先节点后面板**。
- `--bundle` 与 `--sums` 必须同时给出，并且必须用 `--version` 指定版本（`SHA256SUMS` 里核对的文件名带版本号）；核对方式与下载时完全相同。
- **回滚快照**：服务停止后、`install.sh` 运行前，脚本把这次可能被替换的文件（程序、主服务定义、`qui-update`、`service.sh`，面板加 `qui`，节点加强制出站的两个文件，以及安装记录）用 `cp -a` 复制到 `/var/lib/proxycontrol-install/rollback.XXXXXXXX/`（root 0700）。保存不了快照就不更新，并把服务启动回来。
- **健康检查**：原本在运行的服务更新后会被启动，并且要在 10 秒内保持运行、主进程 PID 不变才算成功（两种初始化系统都会在崩溃 5 秒后重启程序，崩溃循环在每次轮询时都可能显示为运行中，但 PID 会变）。成功时打印 `已更新到 X，服务运行正常（10 秒内无重启）` 并删除快照。
- **自动回滚**：`install.sh` 失败（即使已经替换了部分文件）、装上的程序版本不是目标版本、或新版本没有持续运行时，脚本停止服务，把快照里的文件放回（与快照相同的文件不动，所以导致失败的只读或 immutable 文件不会让恢复也失败），删除新版本多出的文件，再启动旧版本并做同样的健康检查，打印 `更新失败，已恢复到 <旧版本>`。面板在新版本已经运行过之后回滚时，还会删除 `-wal`/`-shm` 并用更新前的备份覆盖数据库，因为新版本可能已经执行了不可逆的迁移。退出码为 1。
- 只有回滚本身没能让旧版本恢复健康时，脚本才保留快照目录并打印它的路径、日志命令（systemd：`journalctl -u proxy-<组件> -n 50`；OpenRC：`grep -F 'proxy-agent' /var/log/messages | tail -n 50`）、降级命令 `qui-update --version <旧版本>` 和数据库备份路径。**数据库迁移不可逆**：手工降级面板前先用该备份恢复数据库。
- 回滚保护由**正在运行的** `qui-update` 提供：从 1.0.1 升级到 1.0.2 时运行的是 1.0.1 的 `qui-update`，它没有快照与健康检查（失败时只把服务启动回来）；1.0.2 起的更新才有。在更新过程中按 Ctrl-C 中断不会触发回滚，快照目录会留下，可以用它手工恢复。
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
systemctl stop proxy-agent.service          # OpenRC：rc-service proxy-agent stop
deploy/linux/install.sh --node --binary bin/proxy-agent --sha256 <new-sha256>
systemctl start proxy-agent.service         # OpenRC：rc-service proxy-agent start
```

`qui-update` 在 OpenRC 节点上同样可用：它通过 `/usr/local/lib/proxycontrol/service.sh` 停止和启动 `proxy-agent`，健康检查读取 supervise-daemon 记录的子进程 PID（`/run/openrc/options/proxy-agent/child_pid`）。

节点升级同样保留 `config.yaml`、`identity/`、`revisions/`、`spool/`。注意：`install.sh` 只替换 `/usr/local/bin/proxy-agent`（引导镜像）。已受管的节点运行时使用 `/var/lib/proxy-agent/releases/agent-installed/` 下的镜像，**重跑 `install.sh` 不会改变该镜像**；Agent 自身升级要走面板的 Agent 更新任务（系统 → Agent），它按签名清单校验，并在失败时回滚。

**发布新版本**：在仓库里推一个 `vX.Y.Z` 标签即可。CI 的 `release` 任务（`.github/workflows/check.yml`，`needs: check`，只在 `refs/tags/v*` 上运行）会拒绝不是 `vMAJOR.MINOR.PATCH` 的标签，用 `scripts/build-release.sh` 构建两个架构的归档与 `SHA256SUMS`，再用 `gh release create` 建发布并附上这些文件。`qui-update` 默认安装最新发布，所以打标签就等于发布。

**代理核心**：不走 `install.sh`。在面板“系统 → 核心版本”里填写组件、稳定版本号、官方 GitHub 发布资源 URL 和归档 SHA-256；面板排队下发，Agent 在节点上再次校验摘要、解包、切换，并在健康检查失败时回滚到上一镜像。`install.sh --core` 只用于首装。

## 6. 卸载

```sh
deploy/linux/uninstall.sh panel     # 或 agent
```

`uninstall.sh` 会先停止服务，确认已经停下来：systemd 上要求 `ActiveState=inactive` 且 `Result=success`；OpenRC 上没有这两个属性，要求 `rc-service proxy-agent status` 为 `stopped`。然后删除程序、服务定义（systemd 单元，或 `/etc/init.d/proxy-agent`、`/etc/init.d/proxy-agent-host-network` 与 watcher 脚本）和安装记录，保留身份、配置、数据、备份与服务账号。它在 `/var/lib/proxycontrol-install/` 留下 `<component>.removed` 记录；再次安装必须加 `--reinstall`，否则脚本拒绝。

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
| `/usr/local/lib/proxycontrol/service.sh` | root:root 0644 | 两种初始化系统通用的服务命令库（`qui-update`、`uninstall.sh` 等使用） |
| `/etc/init.d/proxy-agent`、`/etc/init.d/proxy-agent-host-network` | root:root 0755 | 仅 OpenRC：Agent 与强制出站 watcher 服务 |
| `/usr/local/lib/proxy-agent/host-network-watch` | root:root 0755 | 仅 OpenRC：强制出站 watcher（busybox `inotifyd`） |
| `/etc/machine-id` | root:root 0444 | 仅在 OpenRC 主机缺失时由 `install.sh --node` 创建 |

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
# OpenRC 节点：
rc-status default
rc-service proxy-agent status
grep -F proxy-agent /var/log/messages | tail -n 200
```

- 面板起不来：先跑 `--check-config`；非开发模式下证书/私钥缺失、`database` 落在网络路径都会被拒绝。
- 节点登记失败：确认服务已停止、token 文件权限与硬链接数正确、安装记录未被改动、`/etc/proxy-agent/tls/ca.crt` 是签发面板 Web 证书的 CA 且与 `server_name` 匹配。
- 节点连上后立刻断开：检查面板里该节点是否被禁用、证书是否过期或被吊销（见[运维手册](operations.md)）。

