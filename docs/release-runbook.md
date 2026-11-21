# 发布流程手册（提交到 main 并发布新版本）

本文面向接手发布工作的人或 AI，按顺序给出从“改动完成”到“GitHub 发布、生产升级与文档入库”的完整步骤。规则来源为[项目说明书](project-spec.md)第 32 节、[部署与升级](deployment.md)第 2、5 节与 `.github/workflows/check.yml`；如有冲突以上述文档为准并同步修正本文。

日常改动只跑按范围裁剪的 `scripts/quick-check.sh`；完整 Linux 门禁每个版本只在发布提交上跑一次，并与 GitHub `check` 在同一已推送提交上并行，因此一批改动应全部实现后再发布。

## 0. 必须遵守的规则

- **Owner 授权：** 提交、推送、打 tag、发布与升级生产均为不可逆或对外操作，每次须经 Owner 明确同意（如“提交并推送到 main”“发布 X.Y.Z”“升级生产”）。一次授权不顺延到下一步。
- **日常改动用裁剪检查：** 每批改动用 `bash scripts/quick-check.sh`（第 2 节，约 2–10 分钟），不频繁跑完整门禁。
- **先做完整批改动，再统一门禁：** 一批改动全部实现后再在发布提交上跑一次完整 Linux 门禁并与 GitHub `check` 并行（第 4 节）。
- **tag 只能打在通过检查的提交上：** `release` 要求同 SHA 已有成功的 GitHub `check`，本文还要求同 SHA 本地完整门禁通过。发布说明等纯 Markdown 文档在 tag **之后**另起提交。
- **生产配置不改写：** 升级只替换程序，不改写已有 `/etc/proxy-panel/config.yaml`（新装默认端口 22550／18880；生产实际端口与运行状态须在操作前核实）。
- **不在生产上随手运行程序：** 例如不带参数执行 `proxy-panel` 会按默认配置启动；仅执行本文明确列出的命令。
- **前端规则：** 不可用选项直接隐藏而非置灰（请求中按钮、编辑表单当前值、只读展示除外）；不擅自替换已有页面布局。
- **文件格式：** 全部文本文件须 LF、无 BOM；`deploy/`、`scripts/` 下 `.sh` 须保留 `100755` 可执行位。**Windows 新建 `.sh` 默认登记为 `100644`**，提交前须执行 `git update-index --chmod=+x <文件>`，否则 Linux 门禁与 CI 失败。
- **提交署名：** 提交信息末尾带当前会话要求的 `Co-Authored-By` 行。

## 1. 版本号与分支

- 版本格式为 `X.Y.Z` 或 `X.Y.Z-rc.N`，tag 为 `vX.Y.Z`。RC tag 会发布为 GitHub 预发布，`qui-update` 省略 `--version` 时不选中。
- 版本号由 Owner 决定：通常仅修复/小功能且无迁移升补丁号，有新迁移或较大功能升次版本号，必要时先发 RC。
- 工作分支 `<branch>` 与 `main` 保持一致；发布时两者快进推送到同一提交：`git push origin <branch>` 与 `git push origin <branch>:main`（禁止强推）。仓库 `github.com/jingniao/qui` 为公开仓库，**禁止提交密码、令牌、私钥或生产登录信息**。
- 新增迁移只能新加文件（当前迁移头见[项目说明书](project-spec.md#当前状态)，如 `0158` 之后顺延编号），不能修改已应用的迁移；同时同步测试里的旧结构回退夹具，否则归档/迁移测试会失败。

## 2. 日常增量检查（`scripts/quick-check.sh`）

日常改动使用按改动范围裁剪的快速检查（约 2–10 分钟），Go 改动会先对改动的 `.go` 运行 `gofmt -l`：

```sh
bash scripts/quick-check.sh                                  # 与 HEAD 比较
bash scripts/quick-check.sh --base <rev>                     # 与指定提交比较
bash scripts/quick-check.sh --dry-run                        # 仅打印将执行的命令
bash scripts/quick-check.sh --release --base <上一个 tag>    # 发布批次：仅快速检查，测试交给门禁
```

改动文件 = `git diff --name-only <base>` 加未跟踪文件，按命中范围叠加执行，末行输出 `QUICK-CHECK PASS` 或 `FAIL`：

| 触及 | 执行 |
| --- | --- |
| 只有 Markdown | 仅检查 CR/BOM |
| `web/**` | `npm --prefix web run typecheck`、`npm --prefix web run build`、全部 `web/scripts/*-selftest.mjs` |
| `deploy/**`、`scripts/*.sh` | 改动脚本的 `bash -n` 与相关 `deploy/linux/tests/*.sh`；非 `100755` 时告警 |
| Go | 改动包及其模块内依赖包的 `go vet` 与 `go test`（Windows 带 `-p 1`） |
| agent／proto／pkg／contracts 或真实核相关包 | 真实核测试（本机无核心时到 Linux 测试机运行） |
| `migrations/**`、`.github/**` | 要求跑完整门禁（第 4 节） |

**发布批次用 `--release`**：准备提交并紧接着跑完整门禁时，运行 `bash scripts/quick-check.sh --release --base <上一个 tag>`。它保留 CR/BOM、前端检查/构建/自测、`bash -n`、执行位、`gofmt` 与 `go vet`（含测试编译），跳过与门禁重复的 `go test`、`deploy/linux/tests` 与真实核测试，并隐含 `--allow-full-gate-later`（约 1–2 分钟）。日常小改动仍不带 `--release`。

Go 工具链可用仓库 `.tools/go/bin/go.exe`（配 `GOTOOLCHAIN=local` 及 `.tools` 下的 `GOCACHE`/`GOPATH`）或 `PATH` 上的 Go（版本按 `go.mod`）。提交前另核对 `git diff --stat`、LF/无 BOM、新测试脚本已登记到 `scripts/check.sh` 且具备 `100755`。

## 3. 提交与推送（需 Owner 同意）

1. `git add` 预期文件，`git status` 确认无多余文件（如临时报告、`web/dist`）。
2. 核对新增/改动脚本执行位：`git ls-files -s <文件>` 须为 `100755`，否则 `git update-index --chmod=+x <文件>`。
3. 提交信息首行用 `type(scope): 摘要` 风格；发布提交可用 `release vX.Y.Z: <要点>`。
4. 推送工作分支并快进 `main`（见第 1 节）。

发布提交在推送**之后**立即门禁（第 4 节），此处先取得 Owner 对提交与推送的同意；tag、发布与生产升级各自独立确认。

## 4. 发布门禁（本地完整门禁 ＋ GitHub `check`，并行）

推送发布提交后，在**同一 SHA** 上同时启动本地完整门禁与 GitHub `check`，两者均通过后才能打 tag。

本地完整门禁 = `bash scripts/check.sh`（含 `go test -race`、前端构建/自测、部署脚本测试）＋ 真实核套件，两段并行合计约 16–21 分钟（命令见[部署与升级](deployment.md)第 2 节）。门禁必须在 Linux 测试机上执行，**不能用 Windows 本地测试替代**。

**纯前端补丁短门禁**：自上一个 `v*` 标签以来仅改动 `web/**`（不含 `.go`）、`docs/**` 与根目录 `*.md` 时，`scripts/gate-scope.sh` 输出 `web`，两条门禁自动缩短：

- `scripts/check.sh` 仅跑前端 `ci`/构建（含类型检查）/`test:time`/自测，再构建面板并验证 `web/dist/index.html` 引用资源均已嵌入，跳过 `go vet`、`go test -race`、Agent 构建与部署脚本测试（日志含 `GATE SCOPE web`）。
- `~/.qui-local/gate.sh` 在有完整 git 历史处判定范围并传给测试机，跳过真实核套件（`PART cores SKIPPED scope=web`）。
- GitHub `check` 自行检出完整历史判定。触及任何 Go、`go.mod`、迁移、proto、`deploy/**`、`scripts/**`、`.github/**` 或无法判定时一律走完整门禁；强制完整门禁可用 `QUI_GATE_SCOPE=full ~/.qui-local/gate.sh <SHA>` 与 GitHub 输入 `full_gate=true`（每周例行运行始终完整门禁）。

Owner 本机封装（不在仓库内）：

| 命令 | 作用 |
| --- | --- |
| `~/.qui-local/gate.sh <SHA>` | 将该提交 `git archive` 至测试机 `/root/qui-gate/<短 SHA>/`，并行执行 `check.sh` 与真实核套件（纯前端补丁自动跳过真实核） |
| `~/.qui-local/gate-status.sh <短 SHA>` | 查看两段进度与尾部日志；末行 `GATE <sha> PASS total=<秒>` 为通过 |
| `~/.qui-local/gate-rerun.sh <短 SHA>` | 仅重跑上次失败的包（第 4.2 节） |
| `~/.qui-local/r.sh '<命令>'` | 在测试机执行命令 |

### 4.1 GitHub `check`（与本地门禁同时启动）

推送后立即对目标 SHA 派发 `check`（四片并行 `server`/`backup`/`system`/`rest`，汇总任务名为 `check`，约 12–14 分钟；纯前端走短门禁）。本地无 `gh` 时用 REST API（令牌仅命令行读取，不落盘不入日志）：

```sh
TOKEN=$(printf 'protocol=https\nhost=github.com\n\n' | git credential fill | sed -n 's/^password=//p')
curl -fsS -X POST -H "Authorization: Bearer $TOKEN" -H 'Accept: application/vnd.github+json' \
  https://api.github.com/repos/jingniao/qui/actions/workflows/check.yml/dispatches \
  -d '{"ref":"main","inputs":{"cores":false,"full_gate":false}}'
# 查询本次运行结果
curl -fsS -H "Authorization: Bearer $TOKEN" \
  "https://api.github.com/repos/jingniao/qui/actions/workflows/check.yml/runs?head_sha=$(git rev-parse HEAD)&per_page=5"
```

- 改动涉及核心适配、协议、订阅导出或 Agent 渲染时将 `cores` 设为 `true`。
- 已知偶发（如 `TestRealV14CompletionPaths/sing-box/P2-external`）只允许重跑一次并在发布说明中如实记录；其余失败按第 4.2 节处理。

### 4.2 偶发失败：只重跑失败的包一次

门禁失败时先读日志尾部定位原因，不预设为环境或旧问题：

```sh
~/.qui-local/gate-rerun.sh <短SHA>              # 仅重跑上次失败的包
~/.qui-local/gate-rerun.sh --status <短SHA>     # 查看重跑结果
~/.qui-local/gate-rerun.sh --dry-run <短SHA>    # 仅打印将重跑的包
```

- 重跑通过且失败与本次改动无关（已知时序/端口类偶发）→ 记为偶发并在发布说明“验证”节写明包名与结果。
- 重跑仍失败或与本次改动相关 → 视为真失败：修复、提交推送后在新 SHA 重跑完整门禁。

### 4.3 门禁未提交的改动（可选）

提交前预检可用游离快照，不动分支与暂存区（必须从**真实 index 副本**构建以保留脚本 `100755` 执行位，勿用 `git add -N`）：

```sh
idx=$(mktemp); cp .git/index "$idx"
snap=$(GIT_INDEX_FILE="$idx" sh -c 'git add -A && git commit-tree "$(git write-tree)" -p HEAD -m "gate snapshot"')
rm -f "$idx"
~/.qui-local/gate.sh "$snap"
```

提交树须与通过门禁的快照完全一致（`git diff <快照> HEAD --stat` 为空），否则需重跑。

## 5. 打 tag 发布（需 Owner 同意）

本地门禁与 GitHub `check` 在同一 SHA 均通过后打 tag：

```sh
git tag -a vX.Y.Z -m "vX.Y.Z" <已通过两条门禁的 SHA>
git push origin vX.Y.Z
```

推送 tag 触发 `release` 任务，核验同 SHA 已有成功 `check` 后构建并创建 GitHub Release。完成后核对附件齐全：

- `proxy-control-X.Y.Z-linux-amd64.tar.gz`、`proxy-control-X.Y.Z-linux-arm64.tar.gz`
- `sing-box-<版本>-qui.<序号>-linux-{amd64,arm64}.tar.gz` 及各自 `.provenance.txt`
- `proxy-agent-X.Y.Z-linux-amd64.bin`、`proxy-agent-X.Y.Z-linux-arm64.bin` 及各自 `.manifest.json`（Ed25519 官方签名清单，由 Repository Secret `AGENT_SIGNING_KEY` 签发，正式版缺失会直接失败）
- `SHA256SUMS`、`node-install.sh`、`panel-install.sh`

若 `release` 报 “No successful check job”，先核对 tag 指向与 `check` 状态；删除远端 tag 前先问 Owner。

## 6. 升级生产（需 Owner 单独同意）

生产环境登记见[当前生产环境](operations.md#当前生产环境)（主服务器 `5.45.96.43` / 面板 `https://qui.lluu.de` 及受管节点；登录方式由 Owner 提供，不写入仓库）。**每次生产操作前必须实机核实当前主机版本、配置与节点清单**，不沿用历史记录推断。

1. **记录基线：** 核实面板与各节点 `proxy-panel --version`／`proxy-agent --version`、面板 API 节点列表（在线状态与当前修订号）及服务状态（`systemctl show proxy-panel -p ActiveState,NRestarts`；Alpine 用 `rc-service proxy-agent status`）。
2. **建立恢复点：** 通过面板 API 创建并校验加密备份（`POST /api/v1/backups` 与 `POST /api/v1/backups/{id}/verify`，需 Owner 会话与 CSRF），记录备份 ID；`qui-update` 也会自动生成 `pre-update-<旧版本>-<时间>.db`。
3. **先面板：** 执行 `qui-update --component panel --version X.Y.Z --yes`，出现 `已更新到 X.Y.Z，服务运行正常` 方为成功（失败自动回滚）。仅升面板时也可由 Owner 在「系统设置 → 系统信息 → 面板更新」完成（见[项目说明书](project-spec.md)第 27.15 节），同样须独立同意并执行升级前后核对。
4. **再逐台 Agent：** 逐台执行 `qui-update --component agent --version X.Y.Z --yes`（建议输出记入日志并检查退出码），或从面板发起网页完整升级；确认在线后再升下一台。
5. **升级后核对：**
   - 面板与节点版本均为 `X.Y.Z`，面板无额外重启；
   - 节点全部在线，修订号与基线一致（升级本身不产生新修订、不回滚）；
   - 面板日志无新增 WARN/ERROR（`journalctl -u proxy-panel --since <时间> -p warning`），节点运行日志 `/var/lib/proxy-agent/logs/agent.log` 无异常；
   - `/etc/proxy-panel/config.yaml` 未变；`/var/lib/proxycontrol-install/` 无残留 `rollback.*`；
   - 有可用凭据时抽样验证一次真实代理请求。
6. **失败处理：** `qui-update` 自动回滚并输出 `更新失败，已恢复到 <旧版本>`。跨迁移降级按[部署与升级](deployment.md)第 5.2 节使用匹配 `pre-update-*.db`，勿手工替换数据库。

## 7. 发布后的文档提交（需 Owner 同意推送）

只改 Markdown，合并为一个提交，推到工作分支与 `main`（补丁版待生产升级完成后一并写齐验证、发布记录与生产升级）：

- `docs/release-notes-X.Y.Z.md`：新增与改进、修复、迁移、验证（发布提交与耗时、GitHub `check`、实机测试）、发布记录（tag → SHA、release 运行）、生产升级（顺序、恢复点、核对结果与未覆盖项）；如实记录偶发失败与未验证范围。
- `docs/changelog.md`：将顶部“未发布”更新为版本号与日期。
- [docs/project-spec.md](project-spec.md#当前状态) “当前状态”：更新最新正式版、tag、迁移头、生产状态与恢复点。
- `README.md`、`docs/README.md`：同步最新版本号与发布说明链接。
- 清理其他文档中过期的“未发布”或旧迁移头措辞。

## 8. 耗时参考

| 环节 | 约 |
| --- | --- |
| 日常裁剪检查（`bash scripts/quick-check.sh`） | 2–10 分钟 |
| 发布批次提交前（`quick-check.sh --release`） | 1–2 分钟 |
| 本地完整门禁（`scripts/check.sh` ＋ 真实核套件并行） | 16–21 分钟 |
| GitHub `check`（四片并行，与本地门禁并行） | 12–14 分钟 |
| 纯前端补丁短门禁（第 4 节） | 预计 5–10 分钟 |
| `release` 构建 | 几分钟 |
| 生产升级（面板 ＋ 逐台 Agent） | 每台约 1 分钟，视节点数 |

涉及 Go 的小范围修复从改动完成到发布约 1 小时；纯前端补丁走短门禁预计约 15–20 分钟。

## 9. 接手时的检查清单

- [ ] `git status` 干净，或明确每个未提交文件所属批次
- [ ] 本批改动全部完成，`bash scripts/quick-check.sh --release --base <上一个 tag>` 输出 `QUICK-CHECK PASS`
- [ ] 新增/改动 `.sh` 为 `100755`，否则已 `git update-index --chmod=+x`
- [ ] Owner 同意提交推送 → 推送工作分支并快进 `main`（第 3 节）
- [ ] 推送后同时启动本地门禁 `GATE … PASS` 与 GitHub `check`，同 SHA 均通过（第 4、4.1 节）；偶发失败只重跑失败包一次并记录（第 4.2 节）
- [ ] Owner 同意发布 → 打 tag，核对 Release 附件齐全（第 5 节）
- [ ] Owner 同意升级 → 核实当前生产状态、建恢复点、先面板再逐台 Agent、升级后核对（第 6 节）
- [ ] 文档合并提交并推送（第 7 节），向 Owner 简短汇报结果、恢复点与遗留事项
