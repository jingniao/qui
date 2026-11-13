# 发布流程手册（提交到 main 并发布新版本）

本文写给接手发布工作的人或 AI：从“改动已经完成”到“新版本在 GitHub 发布、生产主机升级完毕、文档入库”的完整步骤，按顺序执行即可。规则来源是 [AGENTS.md](../AGENTS.md) 第 32 节、[部署与升级](deployment.md) 第 2、5 节和 `.github/workflows/check.yml`；三者与本文不一致时以它们为准，并顺手修正本文。

2026-11-08 Owner 同意缩短“修改 → 发布”的循环：日常改动只跑按范围裁剪的 `scripts/quick-check.sh`，完整的 Linux 门禁每个版本只在发布提交上跑一次，并与 GitHub `check` 在同一个已推送的提交上并行；Owner 接受“部分问题只在发布时才暴露”的风险，前提是一批改动先全部做完再发布。旧流程下最近一次完整走完的是 v1.4.1（2026-11-08），可对照 [v1.4.1 发布说明](release-notes-1.4.1.md) 的“验证”“发布记录”“生产升级”三节作为样例。

## 0. 必须遵守的规则

- **Owner 授权：** 提交、推送、打 tag、发布与升级生产都是对外或难以撤回的操作，每次都要 Owner 明确同意（例如“提交并推送到 main”“发布 1.4.2”“升级生产”）。一次同意只覆盖它说的那件事，不能顺延到下一步。
- **日常改动用裁剪检查：** 每批改动用 `bash scripts/quick-check.sh` 只测受影响的文件（第 2 节，约 2–10 分钟），不要每改一点就跑整条门禁。
- **先做完整批改动，再统一门禁：** Owner 一次提出的多项改动全部实现后，才跑一次完整的 Linux 门禁；它每个版本只在发布提交上跑一次，并与 GitHub `check` 并行（第 4 节）。进行中又来新需求，并进同一批。
- **tag 只能打在通过检查的提交上：** 发布任务要求同一个 SHA 已有成功的 GitHub `check`，本文还要求同 SHA 的本地完整门禁通过（第 4 节）。因此发布说明等文档通常在 tag **之后**另起一个只改 Markdown 的提交（v1.4.1：代码与 tag `0c1b061`，文档 `799304b`）。
- **生产配置不改写：** 升级只替换程序；`/etc/proxy-panel/config.yaml` 保持原样（生产仍是 `[::]:443`／`[::]:9443`，新默认端口 22550／18880 只用于新装）。
- **不在生产上随手运行程序：** 例如不带参数运行 `proxy-panel` 会尝试以默认配置启动；只运行本文列出的命令。
- **前端规则：** 不可用的选项直接隐藏，不要置灰（例外：请求进行中的按钮、编辑表单里的当前值、只读展示）。不要擅自替换 Owner 已有的页面布局。
- **文件格式：** 全部文本文件 LF、无 BOM；`deploy/`、`scripts/` 下的 `.sh` 必须保留可执行位（git 模式 `100755`）。**在 Windows 上新建的 `.sh` 会以 `100644` 登记**，提交前用 `git update-index --chmod=+x <文件>` 补上；发布提交和门禁快照都一样，否则 Linux 门禁与 CI 会以权限拒绝失败。
- **提交署名：** 提交信息末尾带当前会话要求的 `Co-Authored-By` 行。

## 1. 版本号与分支

- 版本格式 `X.Y.Z` 或 `X.Y.Z-rc.N`，tag 为 `vX.Y.Z`。RC tag 会被发布为 GitHub 预发布，`qui-update` 不带 `--version` 时不会选中它。
- 版本号由 Owner 决定。默认：只有修复和小功能、没有迁移升补丁号（1.4.0 → 1.4.1），有新迁移或较大功能升次版本号，必要时先发 RC；Owner 也可以让带新迁移的版本只升补丁号（v1.5.2 带只写数据的 0142，v1.5.4 带 0143）。
- 工作分支目前是 `feat/v1.4-tunnels`，`main` 与它保持一致。发布时两者都推到同一个提交：`git push origin feat/v1.4-tunnels` 加 `git push origin feat/v1.4-tunnels:main`（必须是快进，禁止强推）。仓库是 `github.com/jingniao/qui`（公开仓库，**不要提交密码、令牌、私钥或生产主机的登录信息**）。
- 新增迁移只能新加文件，不能修改已应用的迁移。新迁移还要同步测试里的“回退到旧结构”夹具（例如 0134 时新增的 `rewindIdentityRecovery`），否则旧归档类测试会失败。

## 2. 日常增量检查（`scripts/quick-check.sh`）

日常改动不跑整条门禁，用按改动文件裁剪的快速检查（约 2–10 分钟），Go 改动先对改动的 `.go` 跑 `gofmt -l`（v1.5.4 起；此前仓库里有 63 个文件未格式化，已在 v1.5.4 一并格式化）：

```sh
bash scripts/quick-check.sh                  # 与 HEAD 比较
bash scripts/quick-check.sh --base <rev>     # 与指定提交比较，例如一批改动开始前
bash scripts/quick-check.sh --dry-run        # 只打印将要执行的命令
```

改动文件 = `git diff --name-only <base>` 加上未跟踪文件，按命中的范围叠加执行，最后一行是 `QUICK-CHECK PASS` 或 `FAIL`：

| 触及 | 执行 |
| --- | --- |
| 只有 Markdown | 只做 CR/BOM 检查 |
| `web/**` | `npm --prefix web run typecheck`、`npm --prefix web run build`、全部 `web/scripts/*-selftest.mjs` |
| `deploy/**`、`scripts/*.sh` | 改动脚本的 `bash -n` 加相关的 `deploy/linux/tests/*.sh`；新增或改动的 `.sh` 在 git 里不是 `100755` 时告警 |
| Go | 改动包及其模块内依赖包的 `go vet` 和 `go test`（Windows 上带 `-p 1`） |
| agent／proto／pkg／contracts 或真实核相关包 | 真实核测试：本机配好核心时直接跑，否则到 Linux 测试机跑 |
| `migrations/**`、`.github/**` | 必须改跑完整门禁（第 4 节） |

`deploy/linux/tests/` 里需要 Linux 或 root 的用例会自行跳过，它们在发布门禁里覆盖。quick-check 只证明受影响范围没坏；跨模块影响、`-race`、部署脚本实跑与真实核回归仍由第 4 节的完整门禁保证。

Go 工具链可用仓库内的 `.tools/go/bin/go.exe`（设 `GOTOOLCHAIN=local`，`GOCACHE`／`GOPATH` 指向 `.tools` 下），或 PATH 上的 Go（版本按 `go.mod`）。

另外检查：`git diff --stat` 只含预期文件；新文件 LF 无 BOM；新增的 `.sh` 已登记到 `scripts/check.sh`（若是测试脚本）并已补可执行位（第 0、3 节）。

## 3. 提交与推送（需 Owner 同意）

1. `git add` 预期文件，`git status` 确认没有多余文件（例如临时报告、`web/dist`）。
2. 确认新脚本的执行位：`git ls-files -s <文件>` 应为 `100755`，否则 `git update-index --chmod=+x <文件>`（Windows 上新建的 `.sh` 默认 `100644`）。
3. 提交信息首行用 `type(scope): 摘要` 风格；发布提交可以用 `release vX.Y.Z: <要点>`。
4. 推送工作分支并快进 `main`（见第 1 节）。

发布提交要在推送**之后**才门禁（第 4 节），所以这里先取得 Owner 对提交与推送的同意；tag、发布与生产升级仍是各自独立的同意。

## 4. 发布门禁（本地完整门禁 ＋ GitHub `check`，并行）

完整门禁跑在**已推送的发布提交**上，不再要求先门禁本地快照：推送后立刻同时启动本地完整门禁与 GitHub `check`，两者在同一个 SHA 上通过才打 tag。Owner 已接受“部分问题只在发布时才暴露”的风险，代价是每批改动必须全部实现后才跑门禁，不能改一点跑一次。

本地完整门禁 = `bash scripts/check.sh`（含 `go test -race`、前端构建/自测、部署脚本测试）＋ 真实核套件，两段同时运行，合计约 16–21 分钟（2026-11-12 提速后实测 977 秒与 1258 秒，真实核 298–342 秒；提速前两段串行约 51 分钟，见[构建与门禁提速](build-speedup-plan.md)第 8 节）。命令见[部署与升级](deployment.md)第 2 节。门禁在 Linux 测试机上跑，**不能用 Windows 上的本地测试替代**。

**纯前端补丁走短门禁（Owner 2026-11-09 批准）：** 发布提交与上一个 `v*` 标签相比，只改了 `web/**`（不含任何 `.go` 文件，`web/embed.go` 决定面板嵌入什么）、`docs/**` 与仓库根目录的 `*.md` 时，`scripts/gate-scope.sh` 判为 `web`，两条门禁都自动缩短：

- `scripts/check.sh` 只跑 `npm --prefix web ci`、`npm --prefix web run build`（含类型检查）、`npm run test:time` 与全部前端自测，再构建面板并确认 `web/dist/index.html` 引用的每个资源都已嵌入二进制；跳过 `go vet`、`go test -race`、Agent 构建与部署脚本测试。日志里有 `GATE SCOPE web`。
- `~/.qui-local/gate.sh` 在开发机上（有 git 历史的地方）用该提交自带的 `scripts/gate-scope.sh` 判定范围，传给测试机，并跳过真实核套件（日志 `PART cores SKIPPED scope=web`）。
- GitHub `check` 自行判定（检出完整历史），打 tag 前仍要求同 SHA 的 `check` 成功。

判定按“自上一个标签以来的全部改动”累计，不是只看最后一个提交；碰到 Go、`go.mod`、迁移、proto、`deploy/**`、`scripts/**`、`.github/**` 或根目录非 Markdown 文件，或无法判定（没有 git 历史、找不到上一个标签、自上一个标签以来没有改动），一律走完整门禁。要强制完整门禁：本地 `QUI_GATE_SCOPE=full ~/.qui-local/gate.sh <SHA>`，GitHub 派发时设 `full_gate=true`。每周的 GitHub `check` 始终跑完整门禁，兜住前端改动与 Go 之间的冲突。

代价：Go 代码、部署脚本与真实核没有变化，所以不重跑它们；前端改动如果碰巧依赖了尚未发布的后端行为，只在下一次完整门禁（或每周运行）暴露。

Owner 本机已有封装（不在仓库里，接手者若在同一台开发机上可直接用）：

| 命令 | 作用 |
| --- | --- |
| `~/.qui-local/gate.sh <SHA>` | 把该提交 `git archive` 到测试机 `/root/qui-gate/<短 SHA>/`，后台**同时**跑 check 与真实核套件（真实核在副本 `<短 SHA>.cores/` 里先自建前端，两段日志结束后按 check、cores 的顺序并入主日志）；纯前端补丁只跑短门禁、跳过真实核（见上） |
| `~/.qui-local/gate-status.sh <短 SHA>` | 查看进度（运行中分别显示两段的进度与尾部）；最后一行 `GATE <sha> PASS total=<秒>` 才算通过 |
| `~/.qui-local/gate-rerun.sh <短 SHA>` | 只重跑上次失败的那些包（第 4.2 节） |
| `~/.qui-local/r.sh '<命令>'` | 在测试机上执行命令 |

### 4.1 GitHub `check`（与本地门禁同时启动）

推送后对**要打 tag 的那个 SHA** 派发 `check`（分片后约 12–14 分钟，2026-11-12 实测；提速前 21–73 分钟；纯前端补丁走短门禁），它与 `~/.qui-local/gate.sh <SHA>` 同时进行，互不等待。`check` 分成四个并行任务 `check (server)`、`check (backup)`、`check (system)`、`check (rest)`（`scripts/check.sh` 的 `QUI_CHECK_SHARD`：三个慢包分片只跑 `go test -race`，`rest` 跑其余包、前端、`go vet`、构建与部署脚本测试），名为 `check` 的汇总任务在四片全部成功时才成功，`release` 仍只认这个 `check`。本地没有 `gh` 命令时用 REST API，令牌从 git 凭据读取：

```sh
TOKEN=$(printf 'protocol=https\nhost=github.com\n\n' | git credential fill | sed -n 's/^password=//p')
curl -fsS -X POST -H "Authorization: Bearer $TOKEN" -H 'Accept: application/vnd.github+json' \
  https://api.github.com/repos/jingniao/qui/actions/workflows/check.yml/dispatches \
  -d '{"ref":"main","inputs":{"cores":false,"full_gate":false}}'
# 查找本次运行并轮询结果
curl -fsS -H "Authorization: Bearer $TOKEN" \
  "https://api.github.com/repos/jingniao/qui/actions/workflows/check.yml/runs?head_sha=$(git rev-parse HEAD)&per_page=5"
```

- 改动涉及核心适配、协议、订阅导出或 Agent 渲染时，把 `cores` 设为 `true`，同时跑真实核任务。
- 已知偶发：`TestRealV14CompletionPaths/sing-box/P2-external`（qui sing-box）曾一次失败、重跑通过。只允许对这种已知偶发重跑一次，并在发布说明里如实记录；其他失败按第 4.2 节处理。
- 令牌只在命令行使用，不写进文件、日志或提交。

### 4.2 偶发失败：只重跑失败的包一次

门禁失败时先读日志尾部找出失败的测试，再判断原因；不要先入为主地认定是环境问题或旧问题。

```sh
~/.qui-local/gate-rerun.sh <短SHA>              # 只重跑上次失败的包
~/.qui-local/gate-rerun.sh --status <短SHA>     # 查看上次重跑的结果
~/.qui-local/gate-rerun.sh --dry-run <短SHA>    # 只打印将重跑什么
```

- 重跑通过，且失败与本次改动无关（改动不触及该包，或失败点是已知的时序、端口类问题）→ 记为偶发，在发布说明“验证”一节写明包名与重跑结果。
- 重跑再次失败，或失败与本次改动有关 → 视为真失败：修复后重新提交推送，对新 SHA 重跑第 4 节的两条门禁。

### 4.3 门禁未提交的改动（可选）

需要在提交前先看结果时，用游离快照门禁，不动分支和暂存区：

```sh
idx=$(mktemp); cp .git/index "$idx"
snap=$(GIT_INDEX_FILE="$idx" sh -c 'git add -A && git commit-tree "$(git write-tree)" -p HEAD -m "gate snapshot"')
rm -f "$idx"
~/.qui-local/gate.sh "$snap"
```

一定要从**真实 index 的副本**出发：从空 index 构建会丢掉脚本的可执行位，门禁会因此失败。不要用 `git add -N`。用快照门禁时，提交内容必须与通过门禁的快照树一致（`git diff <快照> HEAD --stat` 为空），否则对新的快照重跑；正常流程直接在已推送的提交上门禁。

## 5. 打 tag 发布（需 Owner 同意）

第 4 节的本地门禁与 GitHub `check` 都在同一个 SHA 上通过后才打 tag。

```sh
git tag -a vX.Y.Z -m "vX.Y.Z" <已通过两条门禁的 SHA>
git push origin vX.Y.Z
```

tag 推送触发 `release` 任务：先确认同 SHA 有成功的 `check`，再构建并创建 GitHub Release。完成后核对 Release 的附件齐全：

- `proxy-control-X.Y.Z-linux-amd64.tar.gz`、`proxy-control-X.Y.Z-linux-arm64.tar.gz`
- `sing-box-<版本>-qui.<序号>-linux-{amd64,arm64}.tar.gz` 及各自的 `.provenance.txt`
- `proxy-agent-X.Y.Z-linux-amd64.bin`、`proxy-agent-X.Y.Z-linux-arm64.bin` 及各自的 `.manifest.json`（官方 Ed25519 签名清单，由 release 工作流用 Repository Secret `AGENT_SIGNING_KEY` 调用 `scripts/sign-agent-release.sh` 生成；正式版缺该 Secret 会直接失败）
- `SHA256SUMS`、`node-install.sh`、`panel-install.sh`

若 `release` 报 “No successful check job”，说明 tag 打错了提交或 `check` 还没跑完/失败；删除远端 tag 前先问 Owner。

## 6. 升级生产（需 Owner 单独同意）

生产主机的地址与登录方式不在仓库里，由 Owner 提供（Owner 本机封装为 `~/.qui-accept/r.sh panel|node|third '<命令>'`：`panel` 是装有面板和一个 Agent 的主机，`node` 是 UK 节点，`third` 是 Alpine LXC/OpenRC 节点）。

1. **记录基线：** 各主机 `proxy-panel --version`／`proxy-agent --version`，面板 API 的节点列表（在线状态、当前修订号），服务状态（`systemctl show proxy-panel -p ActiveState,NRestarts`；Alpine 用 `rc-service proxy-agent status`）。
2. **建立恢复点：** 用面板 API 建一份加密备份并验证（`POST /api/v1/backups`，再 `POST /api/v1/backups/{id}/verify`；需要 Owner 会话与 CSRF），记下备份 ID；`qui-update` 还会自动生成 `pre-update-<旧版本>-<时间>.db`。
3. **先面板：** `qui-update --component panel --version X.Y.Z --yes`。输出出现 `已更新到 X.Y.Z，服务运行正常` 才算成功；失败会自动回滚。只升面板（不动 Agent 与配置）、且生产面板已在带「面板更新」卡片的版本上时，也可以由 Owner 在「系统设置 → 系统信息 → 面板更新」点「更新到 vX.Y.Z」完成（AGENTS.md 第 27.15 节）：同样需要 Owner 单独同意，并照第 5 步做前后核对；涉及 Agent 或需要控制升级时间窗口时仍用 `qui-update`。
4. **再逐台 Agent：** 每台 `qui-update --component agent --version X.Y.Z --yes`（建议输出重定向到日志文件，并打印退出码），一台确认在线后再升下一台。也可以从面板“Agent 管理”下发托管更新。
5. **升级后核对：**
   - 版本号都是 X.Y.Z；面板服务升级后没有再重启；
   - 节点全部在线，修订号与基线相同（升级本身不产生新修订、不回滚）；
   - 面板日志升级后没有 WARN/ERROR（systemd：`journalctl -u proxy-panel --since <时间> -p warning`）；
   - `/etc/proxy-panel/config.yaml` 未变；`/var/lib/proxycontrol-install/` 下没有残留 `rollback.*`；
   - 做一次真实代理请求抽样（有可用凭据时）。
6. **失败处理：** `qui-update` 会自动回滚并打印 `更新失败，已恢复到 <旧版本>`。需要降级跨过迁移时，按[部署与升级](deployment.md) 5.1 节使用匹配的 `pre-update-*.db`，不要手工替换数据库。

## 7. 发布后的文档提交（需 Owner 同意推送）

只改 Markdown，一个提交，推到工作分支和 `main`：

- `docs/release-notes-X.Y.Z.md`：新增与改进、修复、迁移情况、验证（门禁的发布提交与耗时、GitHub check 运行、实机测试）、发布记录（tag → SHA、release 运行）、生产升级（顺序、恢复点、核对结果、未覆盖项）。如实记录偶发失败和未验证的范围。
- `docs/changelog.md`：把顶部“未发布”改成版本号与日期。
- `AGENTS.md` “当前状态”：最新正式版、tag、迁移头、生产版本与恢复点。
- `README.md`、`docs/README.md`：最新版本号与发布说明链接。
- 其他文档里“未发布”“迁移头 0xxx”等过期措辞。

**补丁版（只升补丁号、无迁移、小修复）合并成一个提交：** 生产升级完成后写**一个**短文档提交，把发布说明的“验证／发布记录／生产升级”三节一次写齐，同时改 `docs/changelog.md` 与 `AGENTS.md` 当前状态；不再分成“发布记录”和“升级记录”两个提交（v1.4.2、v1.4.3 各是两个，从这里起合并）。

## 8. 耗时参考

| 环节 | 约 |
| --- | --- |
| 日常裁剪检查（`bash scripts/quick-check.sh`） | 2–10 分钟 |
| 本地完整门禁（`scripts/check.sh` ＋ 真实核套件，同时运行） | 约 16–21 分钟（提速前约 51 分钟） |
| GitHub `check` | 四片并行约 12–14 分钟，含 `cores` 约 14 分钟（2026-11-12 实测；提速前约 21–73 分钟），与本地门禁并行，不额外占用时间 |
| 纯前端补丁的两条短门禁（第 4 节） | 预计约 5–10 分钟（尚无实测记录） |
| `release` 构建 | 几分钟 |
| 生产升级（面板 ＋ 逐台 Agent） | 逐台约 1 分钟，视节点数 |

涉及 Go 的小范围修复从“改动完成”到“版本发布”约 1 小时：裁剪检查 ＋ 一次完整门禁（GitHub `check` 并行，常是最慢的一环）。纯前端补丁走短门禁，预计约 15–20 分钟（含 `release` 构建与网页更新面板；首次实测后在此更新）。

## 9. 接手时的检查清单

- [ ] `git status` 干净，或清楚每个未提交文件属于哪一批改动
- [ ] 本批改动全部完成，`bash scripts/quick-check.sh` 打印 `QUICK-CHECK PASS`（第 2 节）
- [ ] 新增/改动的 `.sh` 在 git 里是 `100755`，否则已 `git update-index --chmod=+x`（第 0、3 节）
- [ ] Owner 同意提交推送 → 推送工作分支并快进 `main`（第 3 节）
- [ ] 推送后同时启动本地门禁 `GATE … PASS` 与 GitHub `check`，两者在同一 SHA 上通过（第 4、4.1 节）；偶发失败只重跑失败包一次并记录（第 4.2 节）
- [ ] Owner 同意发布 → tag，Release 附件齐全（第 5 节）
- [ ] Owner 同意升级 → 恢复点、面板、逐台 Agent、核对（第 6 节）
- [ ] 文档提交并推送（第 7 节；补丁版合并为一个提交），向 Owner 用简短中文汇报结果、恢复点和遗留事项
