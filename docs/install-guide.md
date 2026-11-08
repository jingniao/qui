# 项目安装与使用指南

面向第一次接触本项目的人，从一台空服务器开始，到面板可用、节点上线、用户拿到订阅。按顺序做即可；每一步只写要做的事，细节（字段口径、边界、排错）指向[部署与升级](deployment.md)与[运维手册](operations.md)。

## 1. 准备

- **面板服务器**：一台 Debian 或 Ubuntu（需要 systemd），1 核 1 GB 内存即可。面板只做控制面，用户代理流量不经过它。
- **节点服务器**：一台或多台 Debian、Ubuntu 或 Alpine（systemd 与 OpenRC 都支持，含 LXC 容器）。节点承载实际代理流量。
- **端口**：

| 端口 | 位置 | 用途 |
| --- | --- | --- |
| 22550（可改） | 面板 | 管理后台、REST、SSE 与用户订阅 |
| 18880（可改） | 面板 | 节点 Agent 的 gRPC（mTLS），节点注册时必须能连到 |
| 各入站端口（可改） | 节点 | 用户客户端接入的协议端口 |

- 云厂商的安全组或上游防火墙要放行上面这些 TCP 端口（Hysteria2、TUIC 等 QUIC 协议还需放行对应 UDP）。
- 面板服务器需要能访问 GitHub（下载发布包）；不通时用离线方式，见第 2 节末。
- 全程用 root（`sudo -i` 或直接 root 登录）执行命令。

## 2. 一键安装面板

在**面板服务器**上执行：

```sh
wget -O panel-install.sh https://raw.githubusercontent.com/jingniao/qui/main/deploy/linux/panel-install.sh
bash panel-install.sh
```

脚本会依次问下面几件事，每项都有默认值，直接回车即用默认：

1. **版本**：默认是 GitHub 上最新的正式发布；也可以输入 `1.4.0`、`1.4.0-rc.3` 这样的版本号。
2. **Web/API 端口**：默认 22550，会检查是否被占用。
3. **Agent gRPC 端口**：默认 18880，同样检查占用。
4. **面板对外地址**：默认自动探测本机默认路由的公网 IPv4；填域名或公网 IP，它决定访问链接与证书内容。
5. **TLS 证书**：
   - 选 **1（默认）自动生成自签名证书**：用 openssl 现场生成 RSA 2048、10 年有效、SAN 含上面地址的证书，写在 `/etc/proxy-panel/tls/server.{crt,key}`（属主 `proxy-panel`，权限 0600）。
   - 选 **2 使用已有证书**：给出现有证书与私钥路径（可以是含中间证书的链文件），脚本用 openssl 校验两者匹配后复制到同一位置。自有 Let's Encrypt 等证书适合这一项。
6. 最后打印汇总（版本、两个端口、访问地址、TLS 方式、路径），**输入 `y` 才开始安装**，其他任意输入直接取消。

之后脚本自己完成：下载发布包 → 校验 SHA256SUMS → 调用归档里的 `deploy/linux/install.sh` 安装程序、服务账号、目录与 systemd 单元 → 写入端口与证书路径 → 用程序自带的 `--check-config` 校验配置 → `systemctl enable --now proxy-panel` 开机自启并启动 → 等 10 秒确认没有反复重启 → 询问是否放行防火墙（ufw/firewalld 已启用时才问，默认放行；没有防火墙只提示）→ 打印结果。

自签名证书时，**节点注册靠证书的 SHA-256 指纹建立信任**：脚本会打印该指纹，面板的注册对话框里显示的也是同一个值（不一致时以面板显示的为准）。

**自动化安装**（无人值守，不提问、不确认）：

```sh
bash panel-install.sh --version 1.4.2 --http-port 22550 --grpc-port 18880 \
  --address panel.example.com --tls self-signed --yes
```

常用参数：`--version`、`--http-port`、`--grpc-port`、`--address`、`--tls self-signed|files`、`--cert`/`--key`、`--yes`、`--update`、`--help`。服务器不通 GitHub 时，先把 `proxy-control-<版本>-linux-<架构>.tar.gz` 与 `SHA256SUMS` 传到主机，再用 `--bundle FILE --sums FILE --version X.Y.Z` 安装。

**卸载后重装**：卸载会留下记录（`/var/lib/proxycontrol-install/panel.removed`），脚本检测到后提示并按重新安装处理（等价于给 `install.sh` 传 `--reinstall`），可以照原命令直接重跑；数据库、证书与配置只要没删就照旧沿用。

## 3. 首次登录

1. 浏览器打开 `https://<地址>:22550/`（换过 Web/API 端口就填实际端口；只有 443 才能在 URL 里省略端口）。自签名证书会有一次安全警告，确认继续即可。
2. 页面要求粘贴**一次性安装令牌**。它就是脚本打印的那串字符，同时写在 `<数据库目录>/setup-token`（默认 `/var/lib/proxy-panel/setup-token`，权限 0600），日志里以 WARN 打印一次（`journalctl -u proxy-panel -n 50 | grep 'first-run setup token'`）。
3. 粘贴令牌，设置 Owner 用户名、密码、平台名称与**时区**。时区决定流量日/月统计与整机流量按哪个本地日切分。
4. 初始化成功后令牌与文件立即失效。之后用该 Owner 账号登录。

没记住令牌或已经失效时：**只要还没有 Owner，每次重启面板都会重新生成一枚令牌**并覆盖 `setup-token` 文件、在日志里重新以 WARN 打印一行 `first-run setup token`，所以重启后要读最新的值（`cat /var/lib/proxy-panel/setup-token`，或 `journalctl -u proxy-panel -n 50 | grep 'first-run setup token'`），旧值随即失效。确认数据库里没有 Owner 后重启服务即可重新生成；Owner 密码忘记用 `qui` 菜单第 1 项重置。

## 4. 添加节点

**面板侧**：左侧「服务器 → 服务器列表 → 新建」，填名称等基础信息。创建成功后注册对话框会给出**一键安装命令**与一次性注册码（注册码只能用一次，会过期），并可勾选要安装的代理核心。

命令里的**面板地址**与**面板 Web 端口**默认取「系统管理 → 系统设置 → 基础设置 → 节点安装连接地址」，在那里保存一次（例如 `panel.example.com` 与 `22550`），之后每台新服务器都不用再改。该设置只影响之后生成的命令；对话框里也能临时改，但不回写设置。标题为“面板 Web 端口”的输入框填**面板 Web（HTTPS）端口**，不是 Agent 的 gRPC 端口（默认 18880，面板自动按实际监听端口写进命令）。未保存时对话框使用当前浏览器访问面板的地址；该设置只有 Owner 可见可改。

**节点侧**：SSH 登录新服务器，把对话框里的命令整段粘贴执行即可。它做的事是：下载与面板同版本的发布包、核对 SHA256SUMS、安装 Agent 与所选核心、按面板证书建立信任、用注册码登记并启动 Agent。命令里包含面板地址、gRPC 端口、证书指纹与注册码，**不要转发给别人**。

要点：

- 节点必须能访问面板的 gRPC 端口（默认 18880）。登记失败通常是注册码过期/已用、端口不通或地址填错；此时节点尚未登记，换一个新注册码重跑命令即可。
- **面板在反向代理或 Cloudflare 后面时**：Agent 的 gRPC 必须直连面板，不能经过 CDN 或终止 TLS 的反向代理。请给 `--panel` 用一个 DNS-only（灰云）的主机名或面板的 IPv4，它要直达面板并被面板**自己的**证书覆盖；`--http-port` 填面板自己的 Web 端口（默认 22550），不是代理的 443；这两个端口都要在防火墙放行。Web 一侧显示 CDN 的公网证书、而 gRPC 端口是面板自己的证书（多为私有 CA 签发）是这两者最容易混的地方：脚本装好信任锚后会用它与 `--panel` 预检 gRPC 端点，证书不覆盖该名字或不被它信任时会在登记前中止并说明原因。这个地址可以在「系统管理 → 系统设置 → 基础设置 → 节点安装连接地址」保存一次，之后生成的命令都用它。
- 核心选择：`xray`（默认，协议最全）、`sing-box`（Hysteria2/TUIC 与部分 Reality 场景需要）、`gost`（只做端口转发与加密中继，仅 amd64）。
- 核心版本、Agent 版本都从面板的「服务器 → Agent 管理 / 代理核心」升级，不要在节点上手工替换二进制。
- 节点上线后，「服务器 → 服务器列表」会显示在线状态、CPU/内存/磁盘与核心版本。

## 5. 创建入站并发布

入站就是用户客户端连接的协议与端口。路径：「节点配置 → 节点列表 → 新建」。

1. 选择所属节点与**所属核心**（留空表示该节点当前的主核心）。
2. 选择协议（VLESS、Trojan、Shadowsocks、Hysteria2/TUIC、SOCKS5/HTTP、端口转发等），填监听端口。
3. 需要 TLS/Reality 时按协议展开设置：
   - **Reality**：握手目标可用「常用目标」按钮填入（如 `www.microsoft.com:443`），再点「生成」得到密钥对与 Short ID；server names 只写具体主机名，Short ID 为偶数位十六进制。
   - 仅 VLESS 支持 Reality 与 xhttp；能保存哪些组合由该节点所属核心上报的能力决定，未列出的组合会被拒绝。
4. 「允许用户」留空表示不按用户限制；「显示在订阅中」决定用户订阅里能否看到该入站（节点间中转用的内部入站应关闭）。
5. 保存草稿后点**发布**生成配置版本，Agent 应用、核心原生校验、启动并健康检查；失败会自动回滚到上一成功版本。发布成功前客户端连不上。

相关细节（端口冲突规则、转发参数、字段含义）见[部署与升级](deployment.md)与[协议支持矩阵](protocol-support-matrix.md)。

## 6. 节点组与用户

1. **建节点组**：「服务器 → 节点组 → 新建」。用途选 `subscription`（用户订阅入口）或 `mixed`；`relay`/`egress` 用于节点间中转与系统出口。
2. **把入站加进组**：打开该组的编辑弹窗，「组内节点」里选择入站（不是服务器）；导入的机场/上游节点用「授权账号」或导入来源加入。组内之后新增的可用入站会自动进入订阅，用户刷新订阅即可拿到，不必重新授权。
3. **建用户**：「用户管理 → 新建」，填用户名、显示名称、到期时间、流量额度、设备上限（0 不限）、重置周期，并在「授权节点组」里勾选第 1 步建的组。
4. **拿订阅**：用户详情页可复制**订阅链接**或扫二维码。链接里的 Token 可随时重置（旧链接立即失效）。支持通用 URI、Base64、sing-box、mihomo/Clash 等格式，见[部署与升级](deployment.md)与[协议矩阵](protocol-support-matrix.md)。
5. **门户**：给用户设置门户登录密码并启用后，用户可访问 `/portal/login` 查看自己的用量、到期、订阅与修改密码；门户与会话与管理员完全隔离。Owner 的「门户预览」只读，不写数据。

客户端规则的全局模板在「分流控制 → 客户端策略」维护；节点之间的中转与代理链在「节点配置 → 隧道管理 / 中转方案」。这两块不必在首次安装时配置。

## 7. 日常运维

**终端菜单 `qui`**（面板主机输入 `qui`）：1 修改管理员账号密码、2 修改端口、3 重启、4 启动、5 停止、6 从 GitHub 更新、0 退出。改端口只改 `http_address` 的端口并先做配置校验，成功后提示重启。

**一键脚本再次运行**：在面板主机上重跑 `panel-install.sh`，它检测到已安装就变成小菜单：

1. 更新到最新或指定版本 —— 实际调用 `qui-update --component panel [--version X]`；重跑脚本时带了 `--bundle/--sums`（离线归档），这一项直接用该归档更新，不访问 GitHub；菜单任一步失败只报错并回到菜单，不会结束脚本；
2. 查看状态（服务状态、版本、访问地址、日志命令）；
3. 重启面板；
4. 卸载 —— 需要输入 `uninstall` 确认；**数据默认保留**，只有再输入一次 `delete` 才会删除数据库、备份、证书与配置目录。本目录不是解包出来的 release 布局时，卸载会先从 GitHub 下载已安装版本的发布归档（版本没发布时这一步会失败，按提示改用归档里自带的 `deploy/linux/uninstall.sh panel`）；卸载后照原安装命令重跑即可重装。

也可以不进入菜单：`bash panel-install.sh --update` 直接更新（同样接受 `--version`、`--bundle/--sums`、`--yes`；未安装面板的主机上会报错退出），`bash panel-install.sh --yes` 在已安装的主机上只打印当前状态，不改任何东西。

重跑一键脚本不会覆盖已有的配置、证书与数据库。

**更新**：一律走 `qui-update`（面板主机 `/usr/local/bin/qui-update`）。它会在停止服务时备份数据库到 `/var/lib/proxy-panel/backups/pre-update-<旧版本>-<时间>.db`，把将被替换的文件快照到 `/var/lib/proxycontrol-install/rollback.*`，并用目标版本程序预检数据库，健康检查失败或中断（Ctrl-C、断线、断电）时自动回滚。先更新面板，再更新节点；节点主机上的 Agent 与核心从面板下发更新。

**备份与恢复**：面板「系统管理 → 备份恢复」创建加密备份（默认保留每日 7、每周 4、每月 3）；恢复要求停写并校验完整性，操作步骤见[运维手册](operations.md) 第 2 节。

**日志**：程序只写 stdout，用 `journalctl -u proxy-panel -f` 跟踪面板日志，`journalctl -u proxy-agent -f` 看节点日志（Alpine 节点用 `grep proxy-agent /var/log/messages`）。排错入口见[部署与升级 §8](deployment.md#8-排错入口)。

**保活**：`proxy-panel.service` 带 `Restart=on-failure`、`RestartSec=5`，安装时已 `enable` 为开机自启，所以**崩溃后 5 秒自动重启、重启主机后自动起来**。本方案不安装定时任务、定时器或看门狗守护进程；不需要 crontab，也不要用 `nohup` 手工拉起。要确认状态：`systemctl is-active proxy-panel`、`systemctl is-enabled proxy-panel`。

## 8. 常见问题

**浏览器提示证书不受信任**：自签名证书的必然结果。确认 `https://<地址>/` 与证书 SAN 里的地址一致后接受一次例外，或用自有证书（`--tls files`）后重新运行安装脚本的更新入口。节点一侧不依赖浏览器信任，而是对比面板显示的证书 SHA-256 指纹。

**端口被占用**：安装脚本在开始前用 `ss` 检查两个端口；被占用时报错退出，用 `--http-port` / `--grpc-port` 换端口，或先停掉占用者（`ss -ltnp | grep :22550`）。改端口后 `systemctl restart proxy-panel`，并记得放行新端口、通知用户刷新订阅。

**节点不上线**：依次确认 ①面板的 gRPC 端口（默认 18880）在安全组与主机防火墙都放行；②节点到面板的 `telnet <面板地址> 18880` 可通；③节点上 `systemctl status proxy-agent`（Alpine：`rc-service proxy-agent status`）是否在运行；④注册码是否过期或已被使用（过期就删掉节点记录重建，或使用原位身份恢复）；⑤两端的证书与时间是否正常。节点在面板里被禁用或逻辑删除也不会再上线。

**Hysteria2 / TUIC 不可用**：这两个协议只在 **sing-box** 核心上存在，需要该节点安装 sing-box（注册时勾选，或之后在「代理核心」里添加）并放行对应 **UDP** 端口；只装了 Xray 的节点不会出现这些协议。

**发布后客户端仍然连不上**：确认该入站「显示在订阅中」已开启、所属节点组已授权给该用户、订阅已刷新；再看节点上核心是否健康（节点详情 → 事件/发布记录）以及发布是否成功（失败会自动回滚）。

**客户端无法导入订阅（证书）**：客户端报 `self-signed certificate in certificate chain`（Clash Verge、v2rayN 等严格校验 TLS 的工具）。原因是面板用的是私有 CA／自签名证书，客户端不信任这条证书链。临时办法：用浏览器打开订阅的 clash 或 base64 链接（浏览器手动信任一次即可），把下载到的文件导入客户端，或把内容直接粘贴进去；这样**没有自动更新**，改了配置要重新导入一次。要根治就给面板换可信证书：安装时选 `--tls files` 提供自有证书，或换成 Let's Encrypt 等公网证书。节点侧不受影响，它靠面板证书的 SHA-256 指纹建立信任。

**忘记 Owner 密码**：面板主机执行 `qui`，第 1 项重置 Owner 账号密码（会短暂停止面板）。

**想换面板地址或证书**：重跑 `panel-install.sh` 的更新入口不会改配置；直接编辑 `/etc/proxy-panel/config.yaml` 后 `systemctl restart proxy-panel`，或重新执行安装脚本前先移走配置以走首次安装流程（会保留数据库）。操作前先备份，见[运维手册](operations.md)。

## 继续阅读

| 需求 | 文档 |
| --- | --- |
| 手工安装、构建发布包、升级与降级、目录权限 | [部署与升级](deployment.md) |
| 备份恢复、证书轮换、额度、整机流量、故障处理 | [运维手册](operations.md) |
| 协议、核心能力与客户端格式支持范围 | [协议支持矩阵](protocol-support-matrix.md) |
| 模块边界、数据流与开发门禁 | [架构总览](architecture.md)、[项目说明书](../AGENTS.md) |
| 全部现行文档 | [文档导航](README.md) |

