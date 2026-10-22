# Agent handoff: qui (lightweight multi-node proxy control platform)

Updated: 2026-10-21, at `publish-main` 2aaadd6 plus the documentation sync that came with this file. Give this file to the next agent as its starting prompt. It summarizes the project. When it disagrees with the source code, the code wins; fix this file in the same change.

---

## 0. Your role and first steps

You are taking over a working, released (v1.0.0) codebase. Most of the work left is verification, maintenance and small features, not a rebuild. Before you change anything:

1. Read `AGENTS.md`. It is the product spec (Chinese, V1.1). Its §3 principles, §23 Agent limits, §29 security and §35 "not in V1" list are hard constraints.
2. Read `docs/architecture.md` (current code map), then `docs/lightweight-refactor-plan.md` §2 (decisions D1–D5), §3 (working rules) and §9 (what changed after the plan).
3. Run `git status -sb` and `git log --oneline -10`. Compare them with §5 of this file.
4. Ask the owner before anything outward-facing or hard to undo: pushing, tagging, deleting branches, publishing releases, or changing a migration.

The owner is the GitHub user `jingniao`. Reply in the language they write in. Most docs are Chinese; the refactor plan and this file are English.

## 1. What the product is

A self-hosted proxy control plane for 1–10 internal users, used only on networks the owner runs or is authorized to use.

- **Panel** (`cmd/panel`, `proxy-panel`): one Go binary.
  - Uses local SQLite and serves the embedded Vue 3 UI.
  - Exposes REST `/api/v1`, SSE `/api/v1/events` and subscriptions `/sub/{token}` over HTTPS (default 443).
  - Runs gRPC with node mTLS on 9443.
  - Proxy traffic never passes through the panel.
- **Agent** (`cmd/agent`, `proxy-agent`): one Go binary per node, run by systemd. It:
  - dials the panel itself, never the other way round;
  - renders, validates and applies config for the **official, unmodified** Xray (26.3.27) or sing-box (1.14.3) binary;
  - rolls back on failure;
  - samples traffic and domain-level access records, with an offline spool.
- **Features:**
  - users, devices and credentials;
  - node groups as the subscription unit;
  - inbounds and outbounds, including protocol A in and protocol B out;
  - outbound groups and proxy chains A→B and A→B→C;
  - routing policies and service sets: Direct, Block, a named outbound, a group or a chain;
  - domain-level access records, with no content, no MITM and no paths;
  - config revisions with diff, deploy and rollback;
  - per-user quota with automatic redeploy;
  - backup and restore, certificate renewal and rotation;
  - core and Agent updates;
  - per-node outbound source IP and interface;
  - logical node deletion.

## 2. Architecture in one screen

| Area | Path | Notes |
| --- | --- | --- |
| Panel modules | `internal/panel/{auth,user,node,subscription,proxy,policy,deployment,access,dashboard,connections,system}` | Each module changes only its own tables through its Repository. Every deploy goes through `deployment`. `dashboard` is read-only. |
| Panel wiring | `internal/platform/server` | HTTP and gRPC wiring and cross-module orchestration, e.g. `quota.go` for automatic quota revisions |
| Platform | `internal/platform/{config,db,events,filelock,httpapi,id,logging,panelhost,panelstore,pki,recovery,restoretasks,vault}` | `db`: one SQLite writer plus a read-only pool (`mode=ro`, 4 connections). `pki`: the node CA. `vault`: field encryption. |
| Agent | `internal/agent/{control,reconcile,core/xray,core/singbox,collector,spool,identity,hostnet,updater,agenthost,agentjobs,...}` | `hostnet` keeps one nft table, `inet proxy_agent_source`, through a oneshot helper unit that holds only `CAP_NET_ADMIN` |
| Contracts | `pkg/contracts/*`, `proto/` | `desiredstate` is the typed config sent to Agents. `desiredstate/support.go` is the one source of truth for protocol support. |
| Web | `web/src/{app,modules/*,components,layouts,styles}` | Vue 3 + TS + Vite + Tailwind 3.4, iOS-style design system (`docs/design-system.md`). Built into `web/dist` and embedded with `web.Files()`. |
| Migrations | `migrations/0001…0109` | `checksums_test.go` freezes every migration's bytes. Never edit or delete one; add a new file. |
| Deploy | `deploy/linux/*.sh`, `deploy/systemd`, `deploy/examples/*.yaml`, `scripts/build-release.sh` | `install.sh --panel/--node`, `node-install.sh` (one-line installer), `qui.sh` (installed as the `qui` menu), `update.sh` (installed as `qui-update`), `enroll.sh`, `renew-certificate.sh`, `panel-ca.sh`, `node-ca.sh`, `uninstall.sh` |
| CI | `.github/workflows/check.yml` | `check` runs on every push. `cores` runs on dispatch or weekly against real Xray and sing-box. `release` runs on a `v*` tag, builds both archives and `SHA256SUMS`, then creates a GitHub release. |

Key flows (details in `docs/architecture.md` §6):

- **Enrollment:** a one-time token, then the Agent creates its own key and CSR, then the panel CA signs it, then the Agent connects over mTLS. Certificates last 90 days and renew automatically.
- **Publishing:** draft, then revision, then deploy. The Agent renders the config and checks it with the core's own validator, then swaps it in and runs a health check; any failure rolls back. Chains publish exit node first and entry node last, and roll back in reverse.
- **Automatic quota revision (D3):** traffic is ingested and the quota state machine changes state. Changes for one node are merged over a 60 s window. The revision starts from the node's *published* state and swaps only users and credentials; user-to-inbound bindings come from the current draft. After 3 failures in a row, the Owner gets an alert.
- **Telemetry:** the Agent samples every 5 s into the spool and uploads on each 15 s heartbeat. Spool data is deleted only after the panel ACKs it.
  - The Xray `access.log` is truncated as soon as it is read, because plain-HTTP lines contain URLs.
  - Access rows are bucketed per minute.
- **Node deletion:**
  - An unenrolled node is removed outright.
  - An enrolled node is deleted logically: `nodes.deleted_at` is set, the certificate is revoked, group memberships are removed, history is kept, and deleted rows are filtered with `deleted_at IS NULL`.
  - Configuration that still references the node blocks the delete with `409 node_delete_blocked`.
  - `deployment.CloseNodeTasks` closes the node's own pending tasks in the same transaction.

## 3. Owner decisions you must keep (D1–D5, full text in the plan §2)

- **D1:** official upstream cores only. No fork, no `integrations/`, no custom builds. A missing capability is detected at runtime and shown as "unavailable".
- **D2:** max concurrent connections is an *observed soft limit*. Exceeding it raises a warning and an alert, never a disconnect. Xray counts distinct client IPs; sing-box shows "unknown".
- **D3:** one quota per user, soft-enforced by the panel through automatic revisions. There are no per-node budgets, no in-core byte quotas and no ledgers.
- **D4:** follow what upstream can do.
  - Per-domain bytes may be NULL and show "—".
  - The exit field is the *configured* exit.
  - The weighted egress strategy is gone; migration 0104 rewrote it to `round_robin`.
- **D5:** sing-box nodes report only node totals. Per-user traffic and online count are unknown there, and quota counts Xray traffic only.

Other standing rules:

- **Security:** keep mTLS, single-use enrollment tokens, CSRF, Owner/Viewer separation and hashed tokens. Never add a remote shell or arbitrary command execution.
- **Privacy:** domain-level metadata only.
- **Keep it lightweight:** no new services, queues or ledgers.
- **Prefer deleting code to adding it.**

## 4. Current status (2026-10-21)

- **Done:**
  - all 15 phases of the lightweight refactor plan, accepted on Windows on 2026-10-17;
  - post-plan features: outbound network, `qui`, `qui-update`, `node-install.sh`, the CI release job, the iOS UI, cancelling unused registrations, logical node deletion;
  - a dead-code cleanup (2aaadd6).
- **Released:** `v1.0.0`, tagged at 69be6d6, a GitHub release at https://github.com/jingniao/qui. Commits after the tag (iOS UI, node deletion, cleanup) are not in any release yet.
- **V1 acceptance is NOT done.** Every [实机] item in `docs/v1-acceptance.md` needs the owner's Linux VMs, including the §33.2 multi-node scenarios and the §33.3 failure tests. Do not mark any of them passed without real evidence.
- **Still open:**
  1. Linux VM acceptance, plus the CI `cores` job on Linux.
  2. An optional owner decision: move Agent self-update from the Ed25519 signed catalogue (`cmd/release`) to URL + SHA-256 like core updates.
  3. Dead proto fields stay until `protoc` is available (`scripts/generate-proto.sh`). Leave `*.pb.go` alone.
  4. Two Windows-only flaky tests: reconcile `TestSyncHostNetworkClearsTheTableWhenNothingIsConfigured` (file lock on status.json) and singbox `TestRealHTTPDirectBlockRollbackAndReconcilerRestart` (port reuse under load). Rerun them alone before calling a failure a regression.
  5. Xray HandlerService live add/remove of users was skipped on purpose (Phase 15.4). Quota changes redeploy instead.
  6. Small UI bug, not fixed: `web/src/layouts/MainLayout.vue` sizes the expanded sidebar with `w-68`. Tailwind 3.4 has no such step, so the expanded width comes from the sidebar's content instead of the intended 17rem. The fix is `w-[17rem]` or a `spacing` extension in `web/tailwind.config.js`. Check it in the browser.
  7. A gap in the spec, not in the code: `AGENTS.md` §27 lists the main API routes only, not every route (for example `/api/v1/deployment-batches` and `/api/v1/traffic/hourly`). The module `handler_http.go` files are the full list.
- **Kept on purpose, even if a dead-code tool flags it:**
  - old proto fields;
  - migrations;
  - the Agent spool's budget-batch handling, which old Agents and old databases still use;
  - `RecoveryCursor` and `RecoveryAccountingCompletion` in `pkg/contracts/traffic`;
  - functions used only by tests that act as test entry points.

## 5. Repository and git state

- Remote: `origin` = https://github.com/jingniao/qui.git. The default branch is `main`.
- Work branch: `publish-main`. It tracks `origin/main` and is built on the public history (`e5023e3` initial public release and later).
  - At handoff it is 1 commit ahead (2aaadd6 cleanup) and has uncommitted documentation changes, unless the owner has since committed them.
- Other local branches are history only; do not push them:
  - `master`: old pre-publication history;
  - `public-main`: points at v1.0.0;
  - `local-dev-before-v1-clear`: an earlier copy of the node-deletion work on the old history.
- `github/main` is a stale remote-tracking ref.
- Commit identity: author and committer are `jingniao <luanfang1@gmail.com>`. The local git user is "OpenCode Refactor", so set the identity per commit:

  ```sh
  GIT_COMMITTER_NAME=jingniao GIT_COMMITTER_EMAIL=luanfang1@gmail.com \
    git commit --author="jingniao <luanfang1@gmail.com>" ...
  ```

- Commit messages are lowercase imperative and scoped, e.g. `nodes: ...`, `web: ...`. They end with the trailer the owner asks for, currently `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Check the trailer with `git log -1 --format=%B`.
- Commit or push only when the owner asks. Never force-push or rewrite pushed history. A `vX.Y.Z` tag push publishes a release that `qui-update` installs by default, so treat tags as production deploys.

## 6. Toolchain and gate (Windows dev box, Git Bash)

- The repo is at `C:\Users\luanf\qui`.
- A local toolchain lives in `.tools/`. It is untracked and very large. **Never run a recursive grep, find or ls from the repo root**; scope searches to `internal/ cmd/ pkg/ web/src/ deploy/ scripts/ migrations/ docs/`.
- Go environment for each shell:

  ```sh
  export PATH="$PWD/.tools/go/bin:$PATH" GOCACHE="$PWD/.tools/gocache" GOPATH="$PWD/.tools/gopath" GOTOOLCHAIN=local
  ```

- npm through Git Bash: `cmd //c "npm --prefix web run build"`, `cmd //c "npm run test:time"`.
- Gate. Every commit must pass:

  ```sh
  gofmt -l <changed .go files>     # empty output; many untouched files were unformatted at baseline, don't reformat them
  go build ./... && go vet ./...
  go test -count=1 ./...
  npm --prefix web run build
  npm run test:time
  ```

  A test that needs a real core skips when `XRAY_BINARY` or `SINGBOX_BINARY` is unset, and a skip is not a pass. Official Windows binaries are in `C:\Users\luanf\qui-cores\{xray-26.3.27,singbox-1.14.3}-windows-amd64\`.
- Dead-code check: `.tools/gopath/bin/deadcode -test ./...`. It was empty after 2aaadd6, apart from the kept items in §4.
- Disk C: is ~97% full. Disk-full errors look like test failures. `.tools/gocache` is the largest thing you can safely clear; the next build is slower.
- Local dev:
  - `go run ./cmd/panel --config deploy/examples/panel.dev.yaml` serves http://127.0.0.1:8080 with gRPC on 127.0.0.1:9443 and data in `state/`.
  - `npm --prefix web run dev` serves http://localhost:3000.
  - `.tools/start-panel-local.cjs` and `.tools/start-web-dev.cjs` are the owner's local launchers. Check their `.pid` files before you start a second copy.
- Other checkouts:
  - `C:\Users\luanf\qui-head` is a clean gate checkout with no `.tools`. Use qui's toolchain paths there.
  - `C:\Users\luanf\qui-scratch` is scratch space.
- Real-core downloads: only stable official GitHub releases of xtls/xray-core and sagernet/sing-box, verified against their published SHA-256, extracted outside the repo, never committed. Any other download needs the owner's approval.

## 7. Conventions and pitfalls

- **Migrations:** add `0110_*.sql` and later; never touch existing files. A pre-refactor backup must still restore (`TestPreRefactorBackupRestores`). When you drop a table, also remove its backup inspect, compare and restore handlers.
- **Typed JSON:** `*_json` columns are parsed into Go structs. Never forward unvalidated JSON to Agents.
- **Protocol support:** change `pkg/contracts/desiredstate/support.go`, then keep the UI, the API validation and `docs/protocol-support-matrix.md` in sync.
- **Deleted nodes:** every new operational query on `nodes` needs `deleted_at IS NULL`. Every new reference to a node (proxy, policy, group, deploy target) must reject deleted nodes. Historical joins must still resolve deleted nodes.
- **Docs:** state conclusions in the docs, tasks in the plan and evidence in the acceptance matrix. When code changes behaviour, update `AGENTS.md`, `docs/architecture.md`, `docs/operations.md` and `docs/deployment.md` in the same change. `docs/archive/` is history; leave it alone.
- **UI:** reuse the shared components in `web/src/components`: `DataTable`, `StatusBadge`, `ConfirmDialog`, `FormDrawer` (a centered modal despite its name), `AppDialog`, `PageHeader`. Follow `docs/design-system.md`.
- **Line endings:** some editors flip CRLF and LF. Compare `git diff --stat` with `git diff -w --stat` before committing.
- **Wording:** never claim the panel stopped or uninstalled anything on a remote server. It cannot, and the docs say so.

## 8. Where to look first for common tasks

| Task | Start at |
| --- | --- |
| New API endpoint | The module's `handler_http.go`, which registers its own routes on the mux with `Auth.Require` (`true` means Owner only). `internal/platform/server` wires the modules together. Update `AGENTS.md` §27. |
| Change what Agents receive | `pkg/contracts/desiredstate`, `internal/panel/deployment`, then `internal/agent/core/{xray,singbox}/render.go` with golden tests |
| Quota behaviour | `internal/panel/user` (state machine), `internal/platform/server/quota.go` |
| Access records | `internal/agent/collector`, `internal/agent/spool`, `internal/panel/access` |
| Install, upgrade, release | `deploy/linux/*.sh`, `scripts/build-release.sh`, `.github/workflows/check.yml`, `docs/deployment.md` |
| Acceptance work | `docs/v1-acceptance.md` (record evidence in the format its §1 gives) |
