# 部署与升级

当前发布状态见[项目说明书](project-spec.md#当前状态)，日常运维见[运维手册](operations.md)。升级顺序为**先面板，再逐台 Agent**；升级前备份数据库与身份目录，跨迁移降级面板必须使用匹配的升级前数据库。当前基线只接受从 **v1.7.5** 或更新版本升级与恢复（数据库须含迁移 `0158`，见[v1.7.6 发布说明](release-notes-1.7.6.md)）；更低版本 Agent 在控制连接时被拒绝，须先在终端升级。

核心来源显式参数为 `--core-source qui|official`：sing-box 默认 `qui`，使用同一 qui 发布内的静态构建（amd64/arm64、glibc/musl 共用各架构归档），仅在官方非 naive 标签基础上增加 `with_v2ray_api`，其逐用户流量计入额度；`official` 保留官方版本与 musl 专用归档及摘要。Xray 与 GOST（仅 linux/amd64，固定 3.3.2）保持官方来源。Linux/amd64 的 systemd 与 Alpine/OpenRC 已实机验证；arm64 不能沿用 amd64 结论。

## 1. 前置条件

- Linux（`x86_64` 或 `aarch64`/`arm64`），以 root 本地执行脚本。
- **初始化系统**：
  - **面板**需要 systemd（Debian、Ubuntu 等）；在 OpenRC 主机上 `install.sh --panel` 会被拒绝。
  - **节点 Agent** 支持 systemd 与 **OpenRC**（Alpine 3.19+，OpenRC ≥ 0.45，含无 systemd 的 LXC 容器）。脚本按 `/run/systemd/system` 或 `/run/openrc` 自动判断，两者均不存在时拒绝；服务定义位于 `deploy/systemd/` 与 `deploy/openrc/`，详见第 4.1 节。
- **面板 TLS 证书**：非开发模式下 `tls.certificate` 与 `tls.key` 必填（`internal/platform/config/config.go`），由自有 PKI 或 ACME 生成。
- **节点受信 CA**：节点需要签发面板 Web 证书的 CA（面板 gRPC 端口默认 18880 共用同一证书），默认路径 `/etc/proxy-agent/tls/ca.crt`，`server_name` 须与证书匹配。
- **依赖与工具**：
  - 服务器强制出站源地址需要 nftables（Linux ≥ 5.2）。systemd 主机缺少 `nft` 或 `unzip`（最小化 Debian/Ubuntu 常见）时，`install.sh --node` 通过 apt/dnf/yum 安装，失败则拒绝。
  - Alpine（OpenRC + `apk`）上 `install.sh --node` 自动补装 `bash curl ca-certificates coreutils findutils grep sed tar unzip shadow runuser nftables`。
  - `install.sh` 启动前检查必需命令：`install sha256sum od stat getent useradd groupadd flock id tr sed chmod rm mktemp rmdir sync mv ln dirname find grep head mkdir`，systemd 另需 `systemctl`，OpenRC 另需 `rc-service rc-update openrc-run`，核心解包需 `unzip`（Xray）/ `tar`（sing-box），下载需 `curl` 或 `wget`。
  - 所有需校验证书的 `curl` 调用均带 `--no-insecure`，避免被本地包装脚本注入的 `-k` 覆盖。

### 多核心安装

`install.sh --node` 与 `node-install.sh` 接受重复的 `--core`（允许 `xray`、`sing-box`、`gost`），不指定时新节点仅安装 Xray；重复同名核心、未知核心或非 linux/amd64 安装 GOST 会被拒绝。

```sh
# 在已解包并校验的同版本发布目录中；先停止已有 Agent 服务
deploy/linux/install.sh --node --binary bin/proxy-agent --sha256 <agent-sha256> \
  --core xray --core sing-box --qui-version <version>

# 一键安装追加多核心
bash node-install.sh --version <version> --panel <panel-host> \
  --ca-sha256 <panel-cert-sha256> --token <registration-token> \
  --core xray --core sing-box
```

- 默认固定版本：Xray `26.3.27`、qui sing-box `1.14.4-qui.1`、官方 sing-box `1.14.3`、GOST `3.3.2`（仅 linux/amd64，静态单一归档，glibc 与 musl 共用，不接受 `--core-url`/`--core-sha256` 覆盖）。
- `--core-source qui|official` 仅影响 sing-box（默认 `qui`）。`--core-version`、`--core-url`、`--core-sha256` 作用于紧邻其前的 `--core`。离线安装 qui sing-box 使用 `--core-bundle FILE --core-sums FILE`（一键安装自动从 `--bundle` 同目录选取对应架构归档与 `--sums`）；Xray 与官方 sing-box 仍走上游下载路径。
- 核心安装在 `/usr/local/lib/proxy-agent/<core>`。首次安装或显式传入 `--core` 时规范化顶层配置，其余配置与状态保留：

```yaml
core_name: xray
core_path: /usr/local/lib/proxy-agent/xray
cores:
  - name: xray
    path: /usr/local/lib/proxy-agent/xray
  - name: sing-box
    path: /usr/local/lib/proxy-agent/sing-box
```

- 升级不传 `--core` 时保留已记录的全部核心与原配置；显式传入 `--core` 仅替换或追加指定核心，不会删除未列出的核心，且会将自定义路径规范化到 `/usr/local/lib/proxy-agent/`。
- **v2 安装记录格式**（`/var/lib/proxycontrol-install/{panel,agent}.manifest`）：第 1 行 `proxycontrol-install-v2`；第 2–4 行为组件、二进制摘要、主服务文件摘要；第 5/6 行留空；第 7 行 `installed`；第 8/9 行为强制出站服务/触发器摘要；第 10 行为 `systemd` 或 `openrc`；第 11/12 行为调优服务/触发器摘要（不适用时留空）；第 13 行起每核心一行 `core <name> /usr/local/lib/proxy-agent/<name> <sha256>`。面板写前 12 行，无核心条目。脚本兼容读取 v1 单核心记录（缺第 10 行视为 systemd），升级后统一写 v2。
- `qui-update` 节点快照覆盖全部核心路径、Agent 配置、完整安装记录以及强制出站/调优单元；卸载按记录逐一校验删除核心并保留状态数据。`qui-update` 拒绝将 Agent 降级到更低版本。

## 2. 构建发布包

**工具链**：官方 Go（版本按 `go.mod`，当前 `go 1.26.11` 或更新）、Node 22（`npm --prefix web ci`）。构建从 `PATH` 读取工具（`.tools/` 仅作本机缓存，非前提）；sing-box 构建另需 `git`、GNU `tar`、`gzip`、`sha256sum`、`readelf`，并以 `GOTOOLCHAIN=go1.26.11` 在临时目录克隆构建。

```sh
VERSION=1.7.5 bash scripts/build-release.sh
```

- `SKIP_NPM_CI=1` 复用已安装前端依赖；`OUTPUT_DIRECTORY=/abs/path` 指定输出目录（默认 `dist/`）。
- 产物：双架构 `proxy-control-<version>-linux-<arch>.tar.gz`、`sing-box-1.14.4-qui.1-linux-<arch>.tar.gz`、`.provenance.txt` 及 `SHA256SUMS`。程序包内含 `VERSION`、`bin/proxy-panel`、`bin/proxy-agent`、`deploy/{linux,systemd,openrc,examples}`，单包同时适用 Debian/Ubuntu 与 Alpine。
- 归档按提交时间可复现（支持 `SOURCE_DATE_EPOCH`），脚本不运行测试也不部署。

**门禁**：日常改动跑增量检查 `bash scripts/quick-check.sh`（发布批次提交前用 `--release`）；完整 Linux 门禁与真实核套件在发布提交上运行并与 GitHub `check` 并行（纯前端补丁经 `scripts/gate-scope.sh` 自动走短门禁）：

```sh
bash scripts/check.sh
bash scripts/ci-real-core-test.sh -p 4 -count=1 -timeout 60m $(scripts/real-core-packages.sh)
```

完整流程、分片与重跑规则见[发布手册](release-runbook.md)与 `.github/workflows/check.yml`。

**本地开发（仅开发环境）**：

```sh
npm --prefix web ci
npm --prefix web run build
go run ./cmd/panel --config deploy/examples/panel.dev.yaml
go run ./cmd/agent --config deploy/examples/agent.dev.yaml
```

开发入口固定监听 `0.0.0.0:8080`（`npm --prefix web run dev`，端口冲突直接报错），API/订阅/健康检查转发至后端 `127.0.0.1:8081`，gRPC 为 `127.0.0.1:9443`，状态目录在 `state/`。

## 3. 安装面板

在解包目录内执行（要求同级 `systemd/` 与 `examples/`）：

```sh
tar -xzf proxy-control-1.7.5-linux-amd64.tar.gz
cd proxy-control-1.7.5-linux-amd64
sha256sum bin/proxy-panel
deploy/linux/install.sh --panel --binary bin/proxy-panel --sha256 <sha256>
```

`install.sh --help` 完整参数：

```text
Usage: install.sh --panel|--node --binary FILE --sha256 DIGEST [options]

  --binary FILE          proxy-panel or proxy-agent executable to install
  --sha256 DIGEST        lowercase SHA256 of that executable
  --config FILE          config template for a first install
                         (default: the packaged examples/<component>.yaml)
  --core xray|sing-box|gost  repeatable; default xray on a new node
  --core-source qui|official  sing-box source (default: qui)
  --qui-version VERSION same qui release as the Agent, required for qui source
  --core-bundle FILE --core-sums FILE  local qui core archive and SHA256SUMS
  --core-version VERSION version for the preceding --core (pinned default)
  --core-url URL         official release asset URL
                         (default: the official GitHub release asset)
  --core-sha256 DIGEST   lowercase SHA256 of the downloaded release archive
                         (default for xray: the release .dgst file)
  --reinstall            install again after uninstall.sh removed the service
```

面板安装脚本执行：
1. 校验 64 位小端 ELF、主机架构与 `--sha256`；
2. 创建 `proxy-panel` 账号及 `/etc/proxy-panel`、`/etc/proxy-panel/tls`、`/var/lib/proxy-panel`（0700）；
3. 安装 `/usr/local/bin/proxy-panel` 与 `/etc/systemd/system/proxy-panel.service`；
4. **首次安装**写入 `/etc/proxy-panel/config.yaml`（0600）；
5. 写入 `/var/lib/proxycontrol-install/panel.manifest`，不自动启动服务。

安装后配置证书、检查并启动：

```sh
install -o proxy-panel -g proxy-panel -m 0600 your-server.crt /etc/proxy-panel/tls/server.crt
install -o proxy-panel -g proxy-panel -m 0600 your-server.key /etc/proxy-panel/tls/server.key
$EDITOR /etc/proxy-panel/config.yaml          # http_address / grpc_address / database / tls
/usr/local/bin/proxy-panel --config /etc/proxy-panel/config.yaml --check-config
systemctl enable --now proxy-panel.service
```

配置示例见 `deploy/examples/panel.yaml`。`node_identity_directory`（默认 `/var/lib/proxy-panel/identity`）由面板首次启动自动创建，不要放在网络共享目录。

- **首次创建 Owner**：数据库无管理员时，面板以 WARN 打印一次性 `first-run setup token` 并写入 `<database 目录>/setup-token`（0600）。打开 `https://<host>/` 粘贴令牌设置 Owner、平台名称与时区，完成后令牌与文件立即失效。
- **日志**：面板只写 stdout，使用 `journalctl -u proxy-panel -f` 查看（不写 `/var/log`）。节点 Agent 运行日志自 v1.7.3 起写入 `/var/lib/proxy-agent/logs/agent.log`（轮转上限 100 MiB），`journalctl -u proxy-agent` / syslog 仅接收启动与日志写入错误。
- **终端菜单 `/usr/local/bin/qui`**（root 专用）：面板主机显示 `1. 修改管理员账号密码、2. 修改端口、3. 重启、4. 启动、5. 停止、6. 更新（从 GitHub）、7. 查看／重置管理员登录地址、8. 卸载并清除全部数据、0. 退出`；仅装 Agent 时显示精简菜单（`1. 更新、2. 卸载并清除全部数据、0. 退出` 及状态/版本）。详见[运维手册](operations.md) §14。

## 4. 接入节点

已登记且旧 Agent 仍可运行的服务器，从服务器列表「安装」生成带 `--replace-agent` 的原位恢复命令；准备条件、清理范围及重试规则见[运维手册第 5.1 节](operations.md#51-原位恢复推荐)。

**第 1 步（面板）**：在「节点」页新建节点，获取一次性注册码与**一键安装命令**（仅正式版本 `X.Y.Z` 且非开发模式显示）。命令里的面板地址、Web 端口与 Agent gRPC 地址/端口默认取「系统设置 → 基础设置 → 节点安装连接地址」。

### 一键安装（推荐）

```sh
if command -v apk >/dev/null 2>&1; then apk add --no-cache bash curl ca-certificates; \
elif ! command -v curl >/dev/null 2>&1 && command -v apt-get >/dev/null 2>&1; then apt-get update -qq && apt-get install -y -qq curl ca-certificates; fi; \
curl --no-insecure -fsSL https://github.com/jingniao/qui/releases/download/v1.7.5/node-install.sh | bash -s -- \
  --version 1.7.5 --panel panel.example.com --http-port 22550 --grpc-port 18880 \
  --ca-sha256 <面板显示的 64 位小写十六进制> --token <一次性注册码> --core xray
```

- 开头 `if … fi;` 在 ash/dash/bash 均合法，仅补齐 Alpine 的 `bash`/`curl` 或最小化 Debian/Ubuntu 的 `curl`。
- **离线安装**：附加 `--bundle <proxy-control-<version>-linux-<arch>.tar.gz> --sums <SHA256SUMS>` 改用本地归档；qui sing-box 从同目录选取，Xray/官方 sing-box 仍走上游下载。
- **证书信任与反向代理／Cloudflare**：脚本先按 `--ca-sha256` 核对 `GET /install/ca.pem`；仅当取回的面板证书本身链到公网 CA 时安装系统 CA 包，自签或私有 CA（含 Web 经 CDN/反代终止 TLS 的场景）直接安装该面板证书为 `/etc/proxy-agent/tls/ca.crt`，随后立即对 gRPC 端点预检。Agent 的 gRPC 必须直连面板，不能经 Cloudflare 橙云或终止 TLS 的反代：`--panel`（或 `--grpc-host`）须为直达面板且被面板自有证书覆盖的 DNS-only 主机名或 IPv4，`--http-port` 为面板自身 Web 端口（默认 22550，且不得与 `--grpc-port` 相同）。
- **登记与重跑**：登记前失败可直接重跑（未生成证书与 `enrollment-result.json` 时，旧重试标记与私钥移入 `/var/lib/proxy-agent-enrollment-retired.XXXXXXXX/`，root 0700，不删除）；已存在 `enrollment-result.json` 时拒绝覆盖，按[运维手册](operations.md)第 5 节处理。登记成功后请清理 shell 历史中的一次性注册码；已登记节点升级走 `qui-update`。

### 4.1 Alpine / OpenRC 节点

一键与手工安装共用脚本，区别仅在服务定义与管理：

| 作用 | systemd | OpenRC |
| --- | --- | --- |
| Agent 服务 | `/etc/systemd/system/proxy-agent.service` | `/etc/init.d/proxy-agent`（`supervise-daemon`） |
| 强制出站源地址 | `proxy-agent-host-network.service` + `.path` | `/etc/init.d/proxy-agent-host-network` + `host-network-watch`（`inotifyd`） |
| 低端口 / 出站能力 | `AmbientCapabilities=CAP_NET_BIND_SERVICE` / `CAP_NET_ADMIN` | `capabilities="^cap_net_bind_service"` / `"^cap_net_admin"` |
| 崩溃重启与停止 | `Restart=on-failure`（5s）、`TimeoutStopSec=90`、`KillMode=process` | `respawn_delay=5`、`retry="TERM/90/KILL/5"`、`rc_cgroup_cleanup="no"` |
| 停核屏障 | `ExecStopPost=+/usr/local/bin/proxy-agent --finish-core-stop --config /etc/proxy-agent/config.yaml`（root） | `stop_post` 以 root 执行 `/usr/local/bin/proxy-agent --finish-core-stop --config /etc/proxy-agent/config.yaml` |
| 运行日志 / 启动故障 | `/var/lib/proxy-agent/logs/agent.log`（100 MiB）；启动错误见 `journalctl -u proxy-agent` | `/var/lib/proxy-agent/logs/agent.log`（100 MiB）；启动错误见 `/var/log/messages`（`logger`） |
| 开机启动 | `systemctl enable` | `rc-update add … default` |

- 均以 `proxy-agent` 运行（`no_new_privs`、`umask=077`）；OpenRC 无 systemd 的 `ProtectSystem`/`PrivateTmp` 等文件系统隔离。
- Linux 核心独立于 Agent 存活，服务停止后由同一屏障确认 worker 退出并检查授权。仅有效更新授权及交接回执允许保留核心；普通 stop/restart、卸载、非更新崩溃仍停核，停核未确认须报失败。
- Linux 核心以 `Setsid` 启动，不设 `Pdeathsig`；退出清理由上述授权检查与停核屏障负责。OpenRC 自动 respawn 不执行 `stop_post`，新 worker 因而在 lifecycle 锁内先执行同一已认证停核屏障，再恢复保留配置；有效 detached/adopting 交接仍走授权认领。
- OpenRC 缺失有效 `/etc/machine-id` 时由 `install.sh --node` 自动补写（root 0444）；`agent.manifest` 第 10 行记为 `openrc`。服务命令对照见[运维手册](operations.md)第 1 节。

### 4.2 调优 helper 与自动续证

- **内核调优 helper**：Agent 安装创建 `/var/lib/proxy-agent/tuning/`（`proxy-agent` 0700）及 `proxy-agent-tuning.service`/`.path`（OpenRC 为 `proxy-agent-tuning` 与 `tuning-watch`），安装时启用并支持开机重放。主程序无调优特权，独立 root helper 仅持 `CAP_NET_ADMIN` + `CAP_DAC_OVERRIDE`，固定写入 **19** 个 sysctl 键（`fs.file-max` 用 root 权限）；systemd 默认将 `/proc/sys` 设为只读并仅放开白名单。生效文件为 `/etc/sysctl.d/99-proxy-tuning.conf`，原值快照为 root 0600 的 `.proxy-tuning-snapshot.json`（不得放入 Agent 可写目录）。卸载前先恢复快照，失败中止卸载；`qui-update` 快照覆盖上述单元。详见[运维手册第 18 节](operations.md#18-主机观测续证与调优-helper)。
- **Ubuntu 调优模块补装（仅 Ubuntu/systemd）**：安装器额外发布 `proxy-agent-tuning-modules.path` / `.service`（root 0644，由 Agent 主单元拉起，无独立开机启用项），内嵌 Python 脚本调用宿主 `/usr/bin/python3`（≥3.9）、APT/dpkg 与 `systemd-detect-virt`。升级或卸载前应等待进行中的 dpkg 事务完成。
- **证书自动续期**：面板签发 90 天节点证书；满 30 天或余期不超过 14 天时，Agent 在控制连接内用原密钥自动续证。

### 4.3 手工安装（备选）

**第 2 步（节点）**：安装 Agent 与所需核心：

```sh
tar -xzf proxy-control-1.7.5-linux-amd64.tar.gz
cd proxy-control-1.7.5-linux-amd64
deploy/linux/install.sh --node --binary bin/proxy-agent --sha256 <sha256> \
  --core xray --core-version 26.3.27
```

- 单独运行脚本装 qui sing-box 时须传 `--qui-version <version>` 且与包内 `VERSION` 一致；GOST（`--core gost`）固定官方 3.3.2（归档摘要 `162feb43…a9d18274`），仅 linux/amd64，以 Agent 子进程运行，无独立服务单元。
- 脚本同时创建 `proxy-agent` 账号、目录及强制出站单元（`proxy-agent-host-network.{service,path}`，仅维护 nft 表 `inet proxy_agent_source`），不启动 Agent 服务。若后续核心编译或启动失败，Agent 仍保持与面板连接并上报不健康，待下发可用修订后恢复。

**第 3 步（节点）**：放置面板 Web CA 与配置：

```sh
install -o root -g proxy-agent -m 0644 /path/to/web-ca.crt /etc/proxy-agent/tls/ca.crt
$EDITOR /etc/proxy-agent/config.yaml     # control_address / server_name / core_name / core_path
```

`ca.crt` 须为签发面板 Web 证书的 CA，**不是** `/var/lib/proxy-panel/identity/ca.crt`（节点 CA 由登记流程自动下发）。面板 Web CA 变更时用 `deploy/linux/node-ca.sh --trust` 更新（见[运维手册](operations.md)）。

**第 4 步（节点）**：登记并启动：

```sh
deploy/linux/enroll.sh --token-file /root/node1.token
systemctl enable --now proxy-agent.service
# OpenRC：rc-update add proxy-agent default && rc-service proxy-agent start
```

`enroll.sh` 校验 token 文件（普通文件、≤512 字节、0600/0400、单硬链接）、安装记录完整、服务已停止后完成登记；确认成功后安全删除原 token 文件。

**第 5 步（面板）**：配置入站/出站与分流并发布配置版本；发布前节点只上报状态，不代理流量。

## 5. 升级

**标准方式：`qui-update`**（`/usr/local/bin/qui-update`，面板与节点主机均由 `install.sh` 安装；同时发布 `/usr/local/bin/qui` 与 `/usr/local/lib/proxycontrol/uninstall.sh`，均不进 manifest、随更新快照）：

```sh
qui-update                       # 安装 GitHub 最新正式发布
qui-update --version 1.7.5       # 指定版本（支持 X.Y.Z 或 X.Y.Z-rc.N）
qui-update --component panel     # 仅更新面板（或 --component agent）
qui-update --yes                 # 免确认
qui-update --version 1.7.5 --bundle proxy-control-1.7.5-linux-amd64.tar.gz --sums SHA256SUMS
                                 # 离线：本地归档与 SHA256SUMS，不访问 GitHub
```

- **下载与校验**：从 GitHub Releases 拉取归档与 `SHA256SUMS` 校验后调用归档内 `install.sh` 原地替换程序、单元与脚本，保留配置、身份与数据库。受管 Agent 同时验证官方 Ed25519 签名清单（见第 5.3 节）。
- **顺序与组件隔离**：先面板，再逐台节点。同机同时装有面板与节点时，省略 `--component` 默认先节点后面板；指定 `--component panel|agent` 后版本检查、启停、快照与回滚仅作用于该组件（共享工具随本次更新快照/恢复；`--component agent` 不可与 `--database` 合用）。
- **数据库备份与预检（面板）**：停服后以 `.partial` + fsync + 逐字节校验备份至 `/var/lib/proxy-panel/backups/pre-update-<旧版本>-<时间>.db`（0600），再用目标程序只读执行 `--check-database <备份>`；跨迁移降级未带匹配备份时直接中止并列出可用备份。
- **快照、健康检查与自动回滚**：面板在停服后、`install.sh` 前快照；Agent 在停服前完成快照及交接准备（见下文）。可能替换的文件与运行中的 `qui-update`/`service.sh` 副本保存到 `/var/lib/proxycontrol-install/rollback.XXXXXXXX/`（root 0700），阶段日志 `journal` 为 `prepared` → `started` → `committed`/`restored`。启动后须通过 10 秒 PID 不变的稳定检查才提交并清理快照；Agent 还须满足认领与实际 worker 摘要核对。失败自动回滚，面板保留新版本写过的库为 `failed-update-<目标版本>-<时间>.db`。
- **中断与开机恢复**：Ctrl-C / `SIGTERM` / `SIGHUP`（断线输出写 `/var/lib/proxycontrol-install/interrupted-update.log`）触发自动回滚；`kill -9` 或断电后由开机单元 `proxycontrol-update-recovery`（在服务启动前运行，输出写 `update-recovery.log`）或下一次 `qui-update` 幂等完成恢复。互斥锁为 `/var/lib/proxycontrol-install/update.lock`。`pre-update-*` 与 `failed-update-*` 需确认无用后手工清理。

**Agent 核心交接（目标版本 1.10.0）**：`qui-update` 先用 `--core-handover-protocol` 探测源、目标及实际受管 worker 的能力。源或目标不支持时明确提示“本次更新会中断连接”；pidfd 不可用时不交接，说明原因后按停核更新，不使用裸 PID 替代。第一次升级到本版本由旧 Agent 执行，仍会断线。

支持交接时，流程为：停服前快照并保存带 SHA-256 校验的目标 helper 副本 → arm 授权 → stop → 确认 detached → 安装 → 启动 → 等待 adopted → 等服务稳定 → 查询并核对实际 worker 摘要（本地套接字未就绪时重试，最多 30 秒）→ 提交更新日志 → revoke 撤销授权。授权窗口为 300 秒 CLOCK_BOOTTIME。受管节点第二次 worker 切换同样单独授权并核对认领回执，受管 worker 接受 bootstrap 与 worker 两种作用域。配置或依赖确实变化仍走全核发布重启，不承诺这次连接持续。

失败回滚先撤销授权，再停服务并执行同一 `--finish-core-stop` 屏障；确认核心退出后才恢复旧镜像。屏障失败则保留快照、helper 与运行现场，不继续覆盖文件或启动旧 Agent；中断与开机恢复同样遵守此顺序。交接输出及本机排障命令见[运维手册](operations.md#agent-核心交接排障)。

### 5.1 RC 与组件选择

- 版本仅接受 `X.Y.Z` 或 `X.Y.Z-rc.N`（无前导零），按 semver 比较（`1.7.5-rc.1 < 1.7.5-rc.2 < 1.7.5`）。RC 标签为 GitHub pre-release，省略 `--version` 时不会自动选中。
- 安装 RC 或单独升级某一组件：

```sh
qui-update --component panel --version 1.7.5-rc.1
qui-update --component agent --version 1.7.5-rc.1
# 离线 RC：先校验 SHA256SUMS 再附加 --bundle / --sums
qui-update --component panel --version 1.7.5-rc.1 \
  --bundle proxy-control-1.7.5-rc.1-linux-amd64.tar.gz --sums SHA256SUMS
```

- **Agent 不可降级**：目标版本低于已安装 Agent（引导程序或实际运行版本任一）时，`qui-update` 在下载与停服前拒绝；组合主机只回退面板须显式加 `--component panel`。升级失败时的自动回滚不受此限制。

### 5.2 面板降级与数据库回退

运行数据库路径固定登记在 `/var/lib/proxycontrol-install/panel.database`（root 0600），暂存与目标目录须在同一文件系统。数据库迁移不可逆，跨迁移降级面板必须提供目标版本时期的 `pre-update-<目标版本>-*.db`：

```sh
qui-update --component panel --version <目标版本>
qui-update --component panel --version <目标版本> \
  --database /var/lib/proxy-panel/backups/pre-update-<目标版本>-<时间戳>.db
```

- `--database` 仅允许用于降级，停服前先做完整性与迁移预检；**备份时间点之后写入的数据会丢失**。降级前仍会照常备份当前库；任何失败均回滚至降级前状态。

**手工离线替换（仅无 `qui-update` 保护的备选）**：先停服再运行 `install.sh`（服务运行中脚本拒绝替换；已有 `config.yaml` 不会被 `--config` 覆盖）：

```sh
# 面板
systemctl stop proxy-panel.service
deploy/linux/install.sh --panel --binary bin/proxy-panel --sha256 <new-sha256>
systemctl start proxy-panel.service

# 节点（OpenRC 换为 rc-service proxy-agent stop/start）
systemctl stop proxy-agent.service
deploy/linux/install.sh --node --binary bin/proxy-agent --sha256 <new-sha256>
systemctl start proxy-agent.service
```

注意：`install.sh --node` 只替换引导镜像 `/usr/local/bin/proxy-agent`，不改变受管运行目录 `/var/lib/proxy-agent/releases/agent-installed/`；受管节点升级请使用 `qui-update --component agent` 或面板任务。

### 5.3 Agent 终端升级与网页完整升级

- **启用与退出网页升级**：`node-install.sh` 默认不启用网页升级；带 `--web-upgrade` 时在首次健康启动后自动完成初始化，未带时可在节点本机运行一次 `qui-update --component agent` 启用（最长等 60 秒写出存储基线；初始化失败将受管路径移至 `/var/lib/proxy-agent/web-upgrade-failed-<UTC>/` 并保持普通模式）。手动退出受管模式：停服、移走受管目录再启动即可回到引导版本（若存储格式已升级，旧引导会拒绝启动，须换新版本）。
- **终端受管升级**：`qui-update --component agent` 或 `qui` 菜单先校验官方签名清单、组件、平台、版本、SHA256 与存储兼容性，再通过 `/var/lib/proxy-agent/agent-local.sock`（0600）在监督进程内与网页任务串行执行，分两阶段提交引导程序与实际 Agent（第二阶段失败时保留新引导并回滚实际 Agent；修订为 0 须先发布基础配置）。离线受管升级另需 `--agent-manifest`：

```sh
qui-update --component agent --version 1.7.5 \
  --bundle proxy-control-1.7.5-linux-amd64.tar.gz --sums SHA256SUMS \
  --agent-manifest proxy-agent-1.7.5-linux-amd64.manifest.json
```

- **网页完整升级（v1.7.5+）**：独立 root 桥接服务（systemd：`proxy-agent-update.{path,service}`；OpenRC：watcher + `proxy-agent-update`）在本机执行 `/usr/local/bin/qui-update --component agent --version X.Y.Z --yes`，覆盖引导与实际 Agent 两阶段更新、校验与失败恢复；节点需能访问 GitHub，不支持网页离线包。桥接材料位于 `/var/lib/proxycontrol-install/agent-system-updates/`，只读结果与输出位于 `/var/lib/proxy-agent-update/`。面板按实测桥接就绪状态展示入口，任务输出与阶段记录见「系统管理 → 任务记录」；验证详情见[v1.7.5 验证记录](v1.7.5-verification.md)。
- **代理核心更新**：在面板「服务器 → 代理核心」填写稳定版本、官方 URL 与 SHA-256 下发（GOST 仅预填固定 3.3.2，仅 linux/amd64），Agent 校验切换、失败自动回滚。

### 5.4 面板网页更新

Owner 在「系统管理 → 系统设置 → 系统信息 → 面板更新」可一键升级到最新正式版（合同见[项目说明书](project-spec.md) §27.15，排障见[运维手册](operations.md)）：

- 依赖 `install.sh` 发布的 `panel-update-request` helper 与 `proxy-panel-update.{path,service}`；仅支持正式版（开发版与 RC 禁用），要求面板直连 `api.github.com` 与 `github.com`。
- 仅安装最新正式版，不支持选版本或降级；内部走与 `qui-update --component panel` 相同的备份、10 秒健康检查与自动回滚，期间页面短断重连。
- 只更新面板，不更新 Agent；同机含 Agent 时须另行升级 Agent。

推送 `vX.Y.Z` 标签触发 GitHub `release` 工作流构建双架构归档、签名清单与 `SHA256SUMS`，发布步骤见[发布手册](release-runbook.md)。

## 6. 卸载

```sh
deploy/linux/uninstall.sh panel     # 或 agent
```

普通卸载先确认服务已完全停止（systemd：`ActiveState=inactive` 且 `Result=success`；OpenRC：`stopped`），再删除程序、服务定义与安装记录，保留身份、配置、数据、备份与账号。Agent 卸载须先撤销全部交接授权，再停服并执行 `--finish-core-stop` 屏障确认核心退出；仅服务 inactive 不足以确认停核，屏障失败保留现场并中止卸载。旧 Agent 不支持协议时沿用其停核流程。Agent 卸载还须恢复内核调优快照并删除 `inet proxy_agent_source`、`inet proxy_agent_hop` 两张 nft 表。卸载后写入 `/var/lib/proxycontrol-install/<component>.removed`（重装须带 `--reinstall`）；最后一个组件卸载后一并移除公共 `qui`、`qui-update`、`service.sh`、本机卸载脚本与开机恢复单元。

彻底清除使用 `qui`「卸载并清除全部数据」或 `/usr/local/lib/proxycontrol/uninstall.sh all --purge`（也可指定 `panel`/`agent`）：按记录先 Agent 后面板，须交互依次输入 `y`、`purge`（无免确认参数），删除配置、状态、备份与账号；不要先普通卸载再补 `--purge`。详见[运维手册](operations.md) §14。

## 7. 目录与权限

| 路径 | 属主 / 权限 | 内容 |
| --- | --- | --- |
| `/usr/local/bin/proxy-panel`、`/usr/local/bin/proxy-agent` | root:root 0755 | 面板与 Agent 引导程序 |
| `/usr/local/bin/qui`、`/usr/local/bin/qui-update` | root:root 0755 | 共用终端菜单与更新器（不进 manifest，更新时快照） |
| `/usr/local/lib/proxycontrol/uninstall.sh` | root:root 0755 | 本机普通卸载／`--purge` 入口（不进 manifest，更新时快照） |
| `/usr/local/lib/proxycontrol/service.sh` | root:root 0644 | systemd/OpenRC 通用服务命令库 |
| `/usr/local/lib/proxycontrol/update-recovery` | root:root 0755 | 开机恢复脚本（由 `proxycontrol-update-recovery` 调用） |
| `/etc/systemd/system/proxycontrol-update-recovery.service` 或 `/etc/init.d/proxycontrol-update-recovery` | root:root 0644 / 0755 | 开机恢复单元，排在主服务与强制出站单元之前 |
| `/etc/proxy-panel/config.yaml` | proxy-panel:proxy-panel 0600 | 面板配置 |
| `/etc/proxy-panel/tls/` | proxy-panel:proxy-panel 0750 | 面板 Web TLS 证书与私钥 |
| `/etc/proxy-agent/config.yaml` | proxy-agent:proxy-agent 0600 | 节点配置 |
| `/etc/proxy-agent/tls/ca.crt` | root:proxy-agent 0644 | 受信面板 Web CA |
| `/etc/proxy-agent/certs/` | root:proxy-agent 0750 | 节点本地入站 TLS 证书目录（卸载保留） |
| `/var/lib/proxy-panel/` | proxy-panel:proxy-panel 0700 | `panel.db`、`identity/`、`backups/`（含 `pre-update-*` / `failed-update-*`） |
| `/var/lib/proxy-agent/` | proxy-agent:proxy-agent 0700 | `identity/`、`revisions/`、`spool/`、`runtime/`、`releases/`、`agent-local.sock` |
| `/var/lib/proxy-agent/logs/` | proxy-agent:proxy-agent 0700 | Agent 运行日志 `agent.log`–`agent.log.9`（轮转总上限 100 MiB） |
| `/var/lib/proxy-agent/host-network/` | proxy-agent:proxy-agent 0700 | 强制出站 `desired.json`、`status.json` |
| `/var/lib/proxy-agent/tuning/` | proxy-agent:proxy-agent 0700 | 内核调优 helper 通信目录 |
| `/var/lib/proxy-agent-update/` | root:proxy-agent | 网页完整升级向 Agent 发布的只读结果与输出 |
| `/var/lib/proxycontrol-install/` | root:root 0700 | 安装记录、`panel.database`、`operation.lock`、`update.lock`、`rollback.*/`、`agent-system-updates/`、恢复日志 |
| `/usr/local/lib/proxy-agent/<core>` | root:root 0755 | 安装器部署的代理核心 |
| `/etc/sysctl.d/99-proxy-tuning.conf` | root:root 0644 | 调优生效配置（原值快照 `.proxy-tuning-snapshot.json` 为 root 0600） |
| `/etc/init.d/proxy-agent*`、`/usr/local/lib/proxy-agent/*-watch` | root:root 0755 | 仅 OpenRC：Agent、强制出站、调优与更新服务及 watcher |
| `/etc/machine-id` | root:root 0444 | 仅 OpenRC 缺失时由 `install.sh --node` 创建 |

主 systemd 单元以专用非 root 账号运行，启用 `NoNewPrivileges`、`ProtectSystem=strict`、`PrivateTmp`、`ProtectHome`，仅保留 `CAP_NET_BIND_SERVICE`。强制出站 helper 单元（`proxy-agent-host-network.{service,path}`）以 `User=proxy-agent` + `CAP_NET_ADMIN` 运行，仅放开 `/var/lib/proxy-agent/host-network` 写入。

## 8. 排错入口

```sh
/usr/local/bin/proxy-panel --config /etc/proxy-panel/config.yaml --check-config
/usr/local/bin/proxy-agent --config /etc/proxy-agent/config.yaml --check-config
/usr/local/bin/proxy-panel --version
/usr/local/bin/proxy-agent --version
systemctl status proxy-panel.service proxy-agent.service
journalctl -u proxy-panel -n 200
tail -n 200 /var/lib/proxy-agent/logs/agent.log     # Agent 运行日志（100 MiB 轮转）
journalctl -u proxy-agent -n 200                    # Agent 启动与日志写入错误
ss -lntp
# OpenRC 节点：
rc-status default
rc-service proxy-agent status
grep -F proxy-agent /var/log/messages | tail -n 200 # OpenRC 启动与日志写入错误
```

- **面板起不来**：先跑 `--check-config`；非开发模式下证书缺失或 `database` 位于网络共享路径会被拒绝。怀疑数据库版本过新时，对停止状态的副本运行 `proxy-panel --check-database <副本>.db`，输出 `database: compatible|newer|foreign|damaged`（退出码 0/3/4/5）；`newer` 须按第 5.2 节带 `--database` 降级。
- **节点登记失败**：确认服务已停、token 权限与硬链接合法、安装记录完整、`/etc/proxy-agent/tls/ca.crt` 签发面板 Web 证书且覆盖 `server_name`。面板在反代/Cloudflare 后面时，须用直达面板的 DNS-only 地址或 IPv4 与面板自身 Web 端口；`node-install.sh` 会在登记前预检 gRPC 端点并报出证书（curl 60/51/58）或连通性错误（6/7/28）。
- **节点连上即断开**：核查面板里节点是否被禁用、证书是否过期或已吊销（见[运维手册](operations.md)）。
- **更新中断或重启后版本不对**：检查 `/var/lib/proxycontrol-install/update-recovery.log`、`interrupted-update.log` 与 `proxycontrol-update-recovery` 日志；若仍存在 `rollback.*`，再次运行 `qui-update` 会先重试恢复。
