# 发布流程手册（提交到 main 并发布新版本）

本文写给接手发布工作的人或 AI：从“改动已经完成”到“新版本在 GitHub 发布、生产主机升级完毕、文档入库”的完整步骤，按顺序执行即可。规则来源是 [AGENTS.md](../AGENTS.md) 第 32 节、[部署与升级](deployment.md) 第 2、5 节和 `.github/workflows/check.yml`；三者与本文不一致时以它们为准，并顺手修正本文。

最近一次完整走完本流程的是 v1.4.1（2026-11-08），可对照 [v1.4.1 发布说明](release-notes-1.4.1.md) 的“验证”“发布记录”“生产升级”三节作为样例。

## 0. 必须遵守的规则

- **Owner 授权：** 提交、推送、打 tag、发布与升级生产都是对外或难以撤回的操作，每次都要 Owner 明确同意（例如“提交并推送到 main”“发布 1.4.2”“升级生产”）。一次同意只覆盖它说的那件事，不能顺延到下一步。
- **先做完整批改动，再统一测试：** Owner 一次提出的多项改动全部完成后，才跑本地检查和一次 Linux 门禁；不要每改一点就跑约 47 分钟的门禁。进行中又来新需求，并进同一批。
- **tag 只能打在通过检查的提交上：** 发布任务要求同一个 SHA 已有成功的 GitHub `check`。因此发布说明等文档通常在 tag **之后**另起一个只改 Markdown 的提交（v1.4.1：代码与 tag `0c1b061`，文档 `799304b`）。
- **生产配置不改写：** 升级只替换程序；`/etc/proxy-panel/config.yaml` 保持原样（生产仍是 `[::]:443`／`[::]:9443`，新默认端口 22550／18880 只用于新装）。
- **不在生产上随手运行程序：** 例如不带参数运行 `proxy-panel` 会尝试以默认配置启动；只运行本文列出的命令。
- **前端规则：** 不可用的选项直接隐藏，不要置灰（例外：请求进行中的按钮、编辑表单里的当前值、只读展示）。不要擅自替换 Owner 已有的页面布局。
- **文件格式：** 全部文本文件 LF、无 BOM；`deploy/`、`scripts/` 下的 `.sh` 必须保留可执行位（git 模式 `100755`）。
- **提交署名：** 提交信息末尾带当前会话要求的 `Co-Authored-By` 行。

## 1. 版本号与分支

- 版本格式 `X.Y.Z` 或 `X.Y.Z-rc.N`，tag 为 `vX.Y.Z`。RC tag 会被发布为 GitHub 预发布，`qui-update` 不带 `--version` 时不会选中它。
- 只有修复和小功能、没有迁移：升补丁号（1.4.0 → 1.4.1）。有新迁移或较大功能：升次版本号，必要时先发 RC。
- 工作分支目前是 `feat/v1.4-tunnels`，`main` 与它保持一致。发布时两者都推到同一个提交：`git push origin feat/v1.4-tunnels` 加 `git push origin feat/v1.4-tunnels:main`（必须是快进，禁止强推）。仓库是 `github.com/jingniao/qui`（公开仓库，**不要提交密码、令牌、私钥或生产主机的登录信息**）。
- 新增迁移只能新加文件，不能修改已应用的迁移。新迁移还要同步测试里的“回退到旧结构”夹具（例如 0134 时新增的 `rewindIdentityRecovery`），否则旧归档类测试会失败。

## 2. 本地检查（Windows 开发机即可）

Go 工具链可用仓库内的 `.tools/go/bin/go.exe`（设 `GOTOOLCHAIN=local`，`GOCACHE`／`GOPATH` 指向 `.tools` 下），或 PATH 上的 Go（版本按 `go.mod`）。

```sh
go build ./...
go vet ./<改动的包>/...
go test ./<改动的包>/... -count=1
npm --prefix web run typecheck
npm --prefix web run build
node web/scripts/ui-regression-selftest.mjs        # 以及改动涉及的其他 web/scripts/*-selftest.mjs
bash -n deploy/linux/<改动的脚本>.sh
```

另外检查：`git diff --stat` 只含预期文件；新文件 LF 无 BOM；新增的 `.sh` 已登记到 `scripts/check.sh`（若是测试脚本）。

## 3. Linux 门禁（必须）

完整门禁 = `bash scripts/check.sh`（约 31–36 分钟，含 `go test -race`、前端构建/自测、部署脚本测试）+ 真实核套件（约 9–11 分钟）。命令见[部署与升级](deployment.md)第 2 节。门禁在 Linux 测试机上跑，**不能用 Windows 上的本地测试替代**。

Owner 本机已有封装（不在仓库里，接手者若在同一台开发机上可直接用）：

| 命令 | 作用 |
| --- | --- |
| `~/.qui-local/gate.sh <commit>` | 把该提交 `git archive` 到测试机 `/root/qui-gate/<短 SHA>/`，后台依次跑 check 与真实核套件 |
| `~/.qui-local/gate-status.sh <短 SHA>` | 查看进度；最后一行 `GATE <sha> PASS total=<秒>` 才算通过 |
| `~/.qui-local/r.sh '<命令>'` | 在测试机上执行命令 |

**未提交的改动怎么门禁：** 门禁吃的是提交，可以在不动分支和暂存区的前提下做一个游离快照提交：

```sh
idx=$(mktemp); cp .git/index "$idx"
snap=$(GIT_INDEX_FILE="$idx" sh -c 'git add -A && git commit-tree "$(git write-tree)" -p HEAD -m "gate snapshot"')
rm -f "$idx"
~/.qui-local/gate.sh "$snap"
```

一定要从**真实 index 的副本**出发：从空 index 构建会丢掉脚本的可执行位，门禁会因此失败。不要用 `git add -N`。

门禁失败时，先读日志尾部找出失败的测试，再判断原因；不要先入为主地认定是环境问题或旧问题。修复后对新的快照重跑。

## 4. 提交与推送（需 Owner 同意）

1. `git add` 预期文件，`git status` 确认没有多余文件（例如临时报告、`web/dist`）。
2. 提交信息首行用 `type(scope): 摘要` 风格；发布提交可以用 `release vX.Y.Z: <要点>`。
3. 提交内容必须与通过门禁的快照**树一致**（`git diff <快照> HEAD --stat` 为空）；不一致就要重新门禁。
4. 推送工作分支并快进 `main`（见第 1 节）。

## 5. GitHub `check`

推送后对**要打 tag 的那个 SHA** 手动派发 `check`（约 21 分钟）。本地没有 `gh` 命令时用 REST API，令牌从 git 凭据读取：

```sh
TOKEN=$(printf 'protocol=https\nhost=github.com\n\n' | git credential fill | sed -n 's/^password=//p')
curl -fsS -X POST -H "Authorization: Bearer $TOKEN" -H 'Accept: application/vnd.github+json' \
  https://api.github.com/repos/jingniao/qui/actions/workflows/check.yml/dispatches \
  -d '{"ref":"main","inputs":{"cores":false}}'
# 查找本次运行并轮询结果
curl -fsS -H "Authorization: Bearer $TOKEN" \
  "https://api.github.com/repos/jingniao/qui/actions/workflows/check.yml/runs?head_sha=$(git rev-parse HEAD)&per_page=5"
```

- 改动涉及核心适配、协议、订阅导出或 Agent 渲染时，把 `cores` 设为 `true`，同时跑真实核任务。
- 已知偶发：`TestRealV14CompletionPaths/sing-box/P2-external`（qui sing-box）曾一次失败、重跑通过。只允许对这种已知偶发重跑一次，并在发布说明里如实记录；其他失败按第 3 节处理。
- 令牌只在命令行使用，不写进文件、日志或提交。

## 6. 打 tag 发布（需 Owner 同意）

```sh
git tag -a vX.Y.Z -m "vX.Y.Z" <已通过 check 的 SHA>
git push origin vX.Y.Z
```

tag 推送触发 `release` 任务：先确认同 SHA 有成功的 `check`，再构建并创建 GitHub Release。完成后核对 Release 的附件齐全：

- `proxy-control-X.Y.Z-linux-amd64.tar.gz`、`proxy-control-X.Y.Z-linux-arm64.tar.gz`
- `sing-box-<版本>-qui.<序号>-linux-{amd64,arm64}.tar.gz` 及各自的 `.provenance.txt`
- `SHA256SUMS`、`node-install.sh`、`panel-install.sh`

若 `release` 报 “No successful check job”，说明 tag 打错了提交或 `check` 还没跑完/失败；删除远端 tag 前先问 Owner。

## 7. 升级生产（需 Owner 单独同意）

生产主机的地址与登录方式不在仓库里，由 Owner 提供（Owner 本机封装为 `~/.qui-accept/r.sh panel|node|third '<命令>'`：`panel` 是装有面板和一个 Agent 的主机，`node` 是 UK 节点，`third` 是 Alpine LXC/OpenRC 节点）。

1. **记录基线：** 各主机 `proxy-panel --version`／`proxy-agent --version`，面板 API 的节点列表（在线状态、当前修订号），服务状态（`systemctl show proxy-panel -p ActiveState,NRestarts`；Alpine 用 `rc-service proxy-agent status`）。
2. **建立恢复点：** 用面板 API 建一份加密备份并验证（`POST /api/v1/backups`，再 `POST /api/v1/backups/{id}/verify`；需要 Owner 会话与 CSRF），记下备份 ID；`qui-update` 还会自动生成 `pre-update-<旧版本>-<时间>.db`。
3. **先面板：** `qui-update --component panel --version X.Y.Z --yes`。输出出现 `已更新到 X.Y.Z，服务运行正常` 才算成功；失败会自动回滚。
4. **再逐台 Agent：** 每台 `qui-update --component agent --version X.Y.Z --yes`（建议输出重定向到日志文件，并打印退出码），一台确认在线后再升下一台。也可以从面板“Agent 管理”下发托管更新。
5. **升级后核对：**
   - 版本号都是 X.Y.Z；面板服务升级后没有再重启；
   - 节点全部在线，修订号与基线相同（升级本身不产生新修订、不回滚）；
   - 面板日志升级后没有 WARN/ERROR（systemd：`journalctl -u proxy-panel --since <时间> -p warning`）；
   - `/etc/proxy-panel/config.yaml` 未变；`/var/lib/proxycontrol-install/` 下没有残留 `rollback.*`；
   - 做一次真实代理请求抽样（有可用凭据时）。
6. **失败处理：** `qui-update` 会自动回滚并打印 `更新失败，已恢复到 <旧版本>`。需要降级跨过迁移时，按[部署与升级](deployment.md) 5.1 节使用匹配的 `pre-update-*.db`，不要手工替换数据库。

## 8. 发布后的文档提交（需 Owner 同意推送）

只改 Markdown，一个提交，推到工作分支和 `main`：

- `docs/release-notes-X.Y.Z.md`：新增与改进、修复、迁移情况、验证（门禁快照与耗时、GitHub check 运行、实机测试）、发布记录（tag → SHA、release 运行）、生产升级（顺序、恢复点、核对结果、未覆盖项）。如实记录偶发失败和未验证的范围。
- `docs/changelog.md`：把顶部“未发布”改成版本号与日期。
- `AGENTS.md` “当前状态”：最新正式版、tag、迁移头、生产版本与恢复点。
- `README.md`、`docs/README.md`：最新版本号与发布说明链接。
- 其他文档里“未发布”“迁移头 0xxx”等过期措辞。

## 9. 接手时的检查清单

- [ ] `git status` 干净，或清楚每个未提交文件属于哪一批改动
- [ ] 本批改动全部完成，本地检查通过（第 2 节）
- [ ] Linux 门禁 `GATE … PASS`，快照与要提交的树一致（第 3、4 节）
- [ ] Owner 同意提交推送 → 推送工作分支并快进 `main`
- [ ] GitHub `check` 在该 SHA 上成功（第 5 节）
- [ ] Owner 同意发布 → tag，Release 附件齐全（第 6 节）
- [ ] Owner 同意升级 → 恢复点、面板、逐台 Agent、核对（第 7 节）
- [ ] 文档提交并推送（第 8 节），向 Owner 用简短中文汇报结果、恢复点和遗留事项
