# Agent 升级下载固定目录地址方案

状态：Owner 2026-11-15 已确认采用固定目录地址。本文记录配置与验证步骤，尚未执行生产配置修改。

## 采用方案与原因

通过 SSH 在各节点 `/etc/proxy-agent/config.yaml` 显式配置 `updates.catalogue` 为 `https://qui.lluu.de/agent-catalogue/`，并填写官方签名公钥。下载走域名的 HTTPS 443，不依赖德国公网 22550。以后更换域名时逐台修改该配置并重启。

`qui.lluu.de` 经 Cloudflare 代理；Agent 的 `control_address` 为 `5.45.96.43:18880`。未显式配置目录时，引导程序按控制主机与 Welcome 中的 Web 端口拼成 `https://5.45.96.43:22550/` 下的目录，实际目录为 `https://5.45.96.43:22550/agent-catalogue/`，无法利用 Cloudflare 的域名入口。

受管模式下载在引导程序（agenthost 父进程，`internal/agent/agenthost/jobs.go`）完成，目录解析见 `internal/agent/agenthost/catalogue.go`。网页升级替换受管子进程的 Agent，不替换引导程序；仅升级网页、面板或子进程无法让父进程自动采用浏览器网址。现有固定目录配置可直接用于当前引导程序。

## 节点配置与重启

通过 SSH 备份并编辑节点现有配置，合并以下顶层 `updates`，保留其他配置。`keys` 必须填写，不能只写 catalogue；下列公钥与组件照抄自 `pkg/contracts/release/official.go` 的 `OfficialKeyPins`，字段结构来自 `release.KeyPin`。

```yaml
updates:
  catalogue: https://qui.lluu.de/agent-catalogue/
  ca: ""
  keys:
    - public_key: "v1GzEWqRzysqZRFBryZStMCw4KeAkF3Vq2metTUlxTE="
      components: [proxy-agent]
```

`ca` 留空，使用系统 CA 校验 Cloudflare 域名证书；不要填 gRPC 的面板私有 CA。不修改 `control_address`、节点证书或 gRPC 的 CA。官方签名、组件、版本、大小与摘要校验继续执行。

先执行配置检查，成功后再重启整个 Agent 服务，使父进程重新读配置：

```sh
proxy-agent --config /etc/proxy-agent/config.yaml --check-config
```

systemd 节点检查成功后执行：

```sh
systemctl restart proxy-agent
systemctl status proxy-agent --no-pager
```

Alpine/OpenRC 节点检查成功后执行：

```sh
rc-service proxy-agent restart
rc-service proxy-agent status
```

配置检查失败时保留原始报错，修正配置后再检查；不能跳过检查重启。只操作已授权的德国与美国 CC，其余节点等 Owner 提供 SSH 配置。本文不授权回滚业务配置或重写 CC 修订状态。

## 生产条件与验证顺序

1. 确认域名的 443 反向代理完整覆盖 `/agent-catalogue/`，证书链有效且允许 TLS 1.3。Cloudflare/WAF 对目录返回原始文件，不要求浏览器挑战、交互登录或重定向。
2. 在节点通过 SSH 写配置、运行 `--check-config`，通过后重启服务并确认控制连接恢复。CC 的旧修订数据问题按 v1.5.8 修订接受规则另行验证。
3. 在 CC 验证经 `https://qui.lluu.de/agent-catalogue/` 获取签名目录、完整资产并完成签名及摘要校验，确认没有使用 IP:22550；记录实际地址、结果与摘要。仅请求首页或 HEAD 成功不足以确认下载可用。
4. 通过网页发起 Agent 升级，核对父进程下载结果、任务结果、升级后版本与在线状态。失败时保留错误码和日志，先修复下载链路。
5. 完整下载及网页升级均确认成功后，撤掉德国防火墙本次新增的美国 CC（148.135.40.213）→TCP 22550 单条临时放行规则。保留面板内部 22550 监听与现有其他规则，再核对经 443 的目录访问、控制连接及版本状态。

当前尚未执行上述步骤，生产下载、升级与规则撤除证据待补。不扩展为代理核心下载源修改，代理核心仍沿用既有官方资产下载流程。

## 以后可选：自动跟随当前网址

以后可考虑创建升级任务时携带浏览器 HTTPS origin 派生的目录地址，面板校验并随任务持久化，下发后固定到任务，重放与重启保持原地址。该项不属于本次固定目录方案。

需要同步修改前端、面板任务与控制协议，让任务携带下载地址；引导程序父进程增加地址读取、校验与下载支持，并上报父进程能力位；父子进程更新请求 schema 升版，区分旧引导程序与新请求。必须另行更新现有父进程，网页升级子进程无法完成此迁移。实施时还须明确固定目录配置的优先级及退出方式，保留正常 TLS 与官方签名校验。
