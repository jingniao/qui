# v1.4.6 发布说明

2026-11-09 发布。tag `v1.4.6` → `620c6d7`，无迁移，迁移头仍 `0134`。补丁版：只增加面板侧的发布信息查询与两个核心对话框的填写方式，不改协议、契约、数据库与 Agent。

## 新增与改进

**内核版本、地址与摘要不用再手抄。** 「服务器 → 代理核心」的「添加内核」「升级内核」对话框和「系统管理 → 系统设置 → 系统信息 → 核心更新」卡片，改为先选版本：面板按这台服务器的架构与 C 库把官方资源 URL 与厂商公布的归档摘要填好，字段仍可随意改。

- 新接口 `GET /api/v1/core-releases?component=&source=[&refresh=1]`（Owner-only）列最近 5 个**正式**版本（跳过草稿与预发布），每个版本给出各平台的官方资产 URL 与摘要。
- 摘要只读对方公布的地方，面板从不自行计算：Xray 取发布页旁 `<asset>.dgst` 的 SHA2-256；官方 sing-box 取 GitHub 资产摘要；qui 构建取我们自己的 `SHA256SUMS`；GOST 取 `pkg/contracts/release` 里已审阅的固定值，完全在本地作答、不发请求。
- 同一资产有两处公布值时必须一致，不一致或缺失就留空，由 Owner 从发布页核对后手动粘贴。
- 结果缓存 10 分钟（`refresh=1` 跳过），失败不缓存。查询失败（网络、限流、上游非 200、内容不可解析、超过 30 秒）一律 502 `core_release_lookup_failed`，不回显上游状态、正文或链接；对话框给固定提示并保留已填内容。
- 每台服务器按自身架构与 C 库取文件：Alpine 用官方 sing-box 的 `-musl` 归档，其余用通用归档，qui 构建是静态单包；没有匹配文件时不留地址、不猜。批量升级按每台各自解析，没有文件或文件没有可信摘要的那台跳过并说明，不会把一台的地址套到另一台上。
- 手输版本会清掉旧摘要：地址能按官方命名规则重建就重建（Xray、官方 sing-box），不能重建就清空（qui 构建在面板 tag 下、GOST 单一固定归档、架构未知），避免把别的版本的地址和摘要当一对提交。
- 构建来源（官方 / qui）在 Agent 支持 qui sing-box 时可选，默认沿用节点当前运行的构建；批量里只有全部目标都支持时才提供 qui。

**保留了原来的校验链。** 选版本只是省去抄写：提交仍走同一 `POST /api/v1/core-updates`，面板继续校验组件、纯发布版本号、摘要格式、URL 前缀与资源名/平台匹配，Agent 继续校验 URL、重定向主机与归档摘要，再解包、切换并在健康检查失败时回滚。UI 里除 GOST 固定值外不含任何摘要字面量。

**文档与测试。** 项目说明书 §22.2（核心更新与移除）记录查询语义、失败口径与上游开销，§27.9 补上新路由；[运维](operations.md) 的两处核心操作说明改为「选版本自动填」。后端新增 `internal/panel/deployment/core_releases{,_http,_test}.go`（6 组测试：`.dgst` 与 GitHub 摘要冲突、缺摘要、qui 去重、GOST 零请求、缓存/刷新/失败码、HTTP 固定错误），新路由登记进门户路由覆盖清单；前端新增 `web/src/modules/system/coreReleases.ts`，自检新增按架构/libc 选资产、来源默认值与手输版本 URL 规则的断言。

## 迁移

无。迁移头仍 `0134`，无需任何数据或配置改动。

## 验证

- 前端：`npm run typecheck` 通过；`node web/scripts/ui-regression-selftest.mjs` 全部 PASS（含新增 `PASS core releases: …`）。
- 后端：`go vet ./internal/panel/deployment` 通过；`go test ./internal/panel/deployment -run TestCoreReleases` ok（1.9 秒）；`go test ./internal/platform/server -run TestPortal` ok（28.8 秒，含新路由的门户越权与路由清单检查）。
- 裁剪门禁：`bash scripts/quick-check.sh` → `QUICK-CHECK PASS text web go real-core`（web 类型检查/构建/自检、6 个受影响 Go 包的 vet/test）。
- 浏览器走查（本地 harness，假接口返回固定发布信息，不联网）：单台升级对话框按 amd64 填出 `26.10.31` 的 URL 与 `.dgst` 摘要并写明来源；批量 xray 跨 amd64/arm64 只给 amd64 那台排队（其摘要来自 `.dgst`），arm64 因该资产没有公布摘要被跳过并提示；添加内核对话框在 qui 能力节点上默认 qui 构建并填出 `SHA256SUMS` 摘要；把 `core-releases` 改成 502 后对话框显示固定提示且不清空已填内容；两个面板下手输版本后 URL 按规则重建（官方 sing-box/ Xray）或清空（qui），摘要清空。走查中发现并修掉的两处问题（手输版本残留旧 URL 与摘要、摘要来源文案）就在本版。
- 完整门禁（同一提交 `620c6d7`，两条并行）：

| 门禁 | 结果 |
| --- | --- |
| 本地 Linux 测试机 `~/.qui-local/gate.sh 620c6d7`（scope `full`） | `PART check EXIT 0 DURATION 2077s`、`PART cores EXIT 0 DURATION 665s`、`GATE 620c6d7 PASS total=2742s`，无 DATA RACE 告警 |
| GitHub `check`（dispatch，run `42556934897`，同一 SHA） | `success` |

## 发布记录

- tag `v1.4.6` → `620c6d7`（`feat(cores): pick a release version and let the panel fill the official URL and digest`），工作分支与 `main` 同提交。
- `release` 任务（tag push，2026-11-09T04:20:18Z 起）成功；Release 附件齐全：`proxy-control-1.4.6-linux-{amd64,arm64}.tar.gz`、`sing-box-1.14.4-qui.1-linux-{amd64,arm64}.tar.gz` 及各自的 `.provenance.txt`、`SHA256SUMS`、`node-install.sh`、`panel-install.sh`。
- 发布前状态：面板 us2 已由 `qui-update` 升到 `1.4.5`（上一版只升了面板），三台 Agent 仍是 `1.4.4`；本版把面板与三台 Agent 一起升到 `1.4.6`。

## 生产升级

顺序：先面板，再按 uk、Alpine、us2 逐台 Agent（2026-11-09T04:32Z 起）。

- 基线（04:31Z）：面板 `1.4.5`、`NRestarts=0`、`/etc/proxy-panel/config.yaml` sha256 `e57bc1d9…`；三台 Agent 均 `1.4.4`，在线，修订 295（uk）／296（Alpine）／291（us2）；另有 1 个未登记的 `pending` 节点 `111`（升级前就存在，本次未改）。各主机无残留 `rollback.*`。
- 恢复点：加密面板备份 `FlK52Fyj…`（`/api/v1/backups` + `verify`，04:31:53Z 创建、04:31:58Z 校验通过，3,655,323 字节，sha256 `f4c1f900…`）；面板升级时更新器另建 `pre-update-1.4.5-20261109-123223.db`。
- 面板：`qui-update --component panel --version 1.4.6 --yes` → `proxy-panel 1.4.5 → 1.4.6`，`已更新到 1.4.6，服务运行正常（10 秒内无重启）`；升级后 `MainPID` 未再变，`NRestarts=0`，`/etc/proxy-panel/config.yaml` sha256 仍 `e57bc1d9…`（未改写），无 `rollback.*` 残留。
- Agent：依次 `qui-update --component agent --version 1.4.6 --yes` → uk `04:32:56Z`、Alpine `04:33:26Z`（OpenRC `started`）、us2 `04:33:50Z`，三次均打印 `已更新到 1.4.6，服务运行正常（10 秒内无重启）`。节点配置文件未变（uk `f5d407c1…`）。
- 升级后核对（04:34Z）：`/api/v1/system/runtime` 报 `v1.4.6`，迁移 `0134_node_identity_recovery.sql`，监听仍是 `[::]:443`／`[::]:9443`；三台 `online`、`agentVersion=1.4.6`，修订仍为 **295／296／291**（无新修订、无回滚）；面板与两台 systemd Agent 升级后 WARN/ERROR 均 0，`rollback.*` 无残留。
- 数据面抽样（面板主机发起，经 uk-node）：SOCKS5、HTTP、VLESS、Shadowsocks、Trojan 五个入口均返回 200（出网 IP `195.72.166.247`）；Netflix 组与 GitHub 直连 200；`example.com`、`www.tiktok.com`、`www.example.net` 及 VLESS 版 `example.com` 仍被阻断（SOCKS 拒绝），错误密码仍被拒。
- 未覆盖／未验证：本版不涉及协议、Agent 契约与迁移，没有做真核回归之外的额外数据面验收；原生 arm64 主机验收仍按 Owner 2026-11-06 的决定跳过，Release 里的 arm64 归档只由门禁构建，不代表实机通过；`docs/build-acceleration-plan.md` 是另一份未跟踪的提速方案，不属于本版。
