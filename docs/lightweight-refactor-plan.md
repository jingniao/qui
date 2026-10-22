# Lightweight Refactor Plan (2026-10-15, revision 2)

**Status:** all phases implemented and accepted on Windows (Phase 15, 2026-10-17); v1.0.0 published 2026-10, and V1 still awaits Linux acceptance (Sections 8 and 9, owner)
**Implementer:** OpenCode, one phase per delegation (Phases 0–5 and 9–11); Cline from Phase 6 on, by the owner's instruction of 2026-10-17 (Phases 6–8 and 12–15)
**Reviewer:** Claude Code. Each phase is reviewed against its acceptance criteria before the next one starts (Section 6).
**Supersedes as the working entry point:** `docs/current-status.md`, `docs/remaining-work-plan.md`, `docs/superpowers/plans/*`, `docs/test-repair-plan-20261015.md`. Those files are history only.

Revision 2 changes the plan's layout, not its decisions:
- the old Phases 1–6 are split into 15 smaller phases, each with explicit acceptance criteria;
- the end-to-end safety-net test (old step 4.0) moves to Phase 3, before any deletion starts;
- a review log (Section 7) records what each phase changed and how it was verified.

This plan doesn't change the product goals in `AGENTS.md`, except where Section 2 says so.

---

## 1. Why

The project works, and its test suite passed on 2026-10-15 (`go build ./...`, `go vet ./...` and `go test -count=1 ./...` on Windows with the bundled Go 1.27.1). But it has grown far beyond the spec for a tool serving 1–10 users:

- **Scope creep.** There are 131.8k lines of non-test Go. An estimated 55–60k of them implement things the spec never asked for:
  - multi-round node CA rotation, "recovered roots", successor and retirement chains;
  - recovery cases, an offline recovery coordinator, controller handoff;
  - per-node traffic budgets with transfer, return, conversion and pause;
  - access "receipt archives";
  - spool forensics.
- **Patched core forks.** It depends on patched forks of Xray (`26.3.27-proxycontrol.N`) and sing-box (`1.14.2-proxycontrol.N`), built from `integrations/`. The forks mainly exist to hard-enforce connection limits and byte quotas inside the core.
- **Surface area.** 78 agent CLI flags, 38 flag definitions in `cmd/panel`, 263 HTTP routes, 99 migrations, about 430 docs, 23 deploy scripts.

The largest packages at the start, in non-test lines:

| Package | Lines |
|---|---|
| `internal/agent/identity` | 14.2k |
| `internal/panel/system` | 14.2k |
| `internal/panel/system/backup` | 11.5k |
| `internal/panel/node` | 8.2k |
| `internal/panel/access` | 7.8k |
| `internal/panel/user` | 7.0k |
| `internal/panel/deployment` | 6.2k |
| `internal/agent/spool` | 5.4k |
| `internal/platform/recovery` | 4.5k |
| `cmd/panel` | 4.1k |

**Goal:** a small, maintainable V1 that:
- keeps every core feature in AGENTS.md;
- runs official upstream Xray and sing-box binaries;
- is easy to deploy.

---

## 2. Product decisions (made by the project owner, 2026-10-15)

These decisions override the matching parts of AGENTS.md. Phase 14 updates AGENTS.md to match.

### D1. Official upstream cores only
- Nodes run unmodified official release binaries of Xray-core and sing-box.
- No forked or patched builds, no custom build tags, and no core source in this repo.
- Delete `integrations/` and the core-building scripts.
- The agent detects core capabilities at runtime: it runs the version command and probes the stats and API features it needs.
- When an optional feature is missing, the agent degrades gracefully: it reports "unavailable" rather than refusing to run.

### D2. Max concurrent connections is not hard-enforced in the core
- Remove all core-level connection admission, slot ledgers and cross-node connection authorization. This covers `connection_allowances`, connection generations, and the `ConnectionGrant`/admission code paths.
- The panel keeps enforcing "max devices" when devices are created. This is pure panel logic.
- "Max concurrent connections" becomes an **observed, soft limit**:
  - The agent reads online or connection counts from upstream capabilities only:
    - Xray: per-user online statistics through StatsService, where the installed version supports it.
    - sing-box: the Clash API `/connections` endpoint, where it's available.
  - The agent reports these counts with its normal telemetry.
  - The panel shows the current count per user and flags users over the limit, with a warning badge and a system-alert SSE event.
  - V1 never disconnects or blocks automatically.
  - If the core can't report counts, show "unknown". Never invent a value.

### D3. No per-node traffic budgets
- Remove:
  - per-node budgets, budget transfer, budget return and quota conversion;
  - node pause, accounting runs and final-usage freezing;
  - in-core byte quotas.
- Keep **one traffic quota per user**, summed across all nodes. This is `users.traffic_limit` / `users.used_traffic` from migration 0005.
- The panel enforces the quota softly:
  1. The agent samples per-user byte deltas from upstream stats every 5 s and reports them through the existing telemetry and spool path on the 15 s heartbeat.
  2. The panel adds the deltas to `users.used_traffic`, and also to device usage where a credential maps to one device.
  3. When a user is over quota, expired or disabled, the panel automatically creates and deploys a revision without that user's credentials, through the normal Deployment module.
  4. On the healthy path, overshoot of one reporting interval plus a coalescing window of up to 60 s plus deploy time is accepted and documented.
- Re-enabling a user triggers the same automatic redeploy. Re-enabling means raising the quota, resetting usage or renewing.

### D4. Features that patched cores provided and upstream cannot
Use the upstream behaviour and document the gap. Do not re-implement any of these inside the agent.

| Former patched feature | V1 behaviour with upstream |
|---|---|
| Exact bytes per domain in access records | Access records keep the time bucket, user, node, domain or IP, rule, action, configured outbound or chain, and connection count. Byte columns are nullable, and the UI shows "—". Per-user and per-node totals still come from stats. |
| Observed actual egress node across relays | Record the **configured** egress resolved by the policy at that node, labelled "configured egress". |
| Per-target UDP routing (patched) | Upstream UDP behaviour as-is. |
| Weighted balancer (patched sing-box) | Xray: balancer + observatory (`random`, `roundRobin`, `leastPing`, `leastLoad`) plus `fallbackTag` for primary/backup. sing-box: `selector` / `urltest`. Weighted selection is dropped unless upstream supports it; if it's unsupported, remove it from the API and the UI. |
| WireGuard per-user identification (patched sing-box) | WireGuard only as far as upstream supports it. Where a peer can't be attributed to a user, that inbound reports no per-user traffic and the UI says so. |

If an upstream capability that this plan assumes turns out to be missing, **stop and report**. Don't patch a core, and don't build a substitute subsystem.

### D5. Per-user traffic on sing-box nodes (decided 2026-10-16)
- Official sing-box builds have no v2ray stats API, and the Clash API doesn't name the user of a connection (see the Phase 6 pre-step findings in section 7).
- **Xray nodes** report exact per-user uplink, downlink and online counts.
- **sing-box nodes** report node totals only: the Clash API `uploadTotal` / `downloadTotal`. Their per-user traffic and online counts are "unknown".
- A per-user quota counts only traffic seen on Xray nodes. The user page says so when the user is granted any sing-box node group.
- Don't add per-user route tagging or Clash API polling to approximate sing-box per-user traffic.

---

## 3. Working rules for the implementer

1. **Version control.** Commit every step separately with a clear message. Never push, and never rewrite earlier commits.
2. **Gate.** Every commit must pass all of these:
   - `go build ./...`
   - `go vet ./...`
   - `go test -count=1 ./...`
   - `npm --prefix web run build`
   - `gofmt -l` prints nothing for the Go files the phase changed (`gofmt -l $(git diff --name-only <phase-start>..HEAD -- '*.go')`). 88 files were already unformatted at baseline; don't reformat untouched files.

   Tests that need a real core may skip when no binary is configured. A skip isn't a pass: list the skips in the commit message and in the phase report.
3. **Migrations.**
   - Never edit or delete an existing migration (0001–0102 as of Phase 4). Removals go in new migrations, numbered after the last one, using `DROP TABLE IF EXISTS` or column drops.
   - A pre-refactor backup must still restore: the old DB is restored, then migrations run forward and drop the dead tables.
4. **Backup/restore.** When a table is dropped, remove its backup inspect, compare and restore handlers in the same commit. Keep a test that restores a pre-refactor backup.
5. **No new subsystems.** Don't add ledgers, journals, receipts, format floors, new CLI flags, or new docs per step. Prefer deleting code to adding code.
6. **Don't weaken security.** Keep:
   - mTLS on the agent port;
   - single-use enrollment codes;
   - CSRF protection;
   - Owner/Viewer separation;
   - hashed subscription and session tokens;
   - no remote shell.
7. **One progress record.** Tick the checklist in Section 5 of this file. Don't create journal docs.
8. **Stop and report** instead of guessing when:
   - an upstream capability is missing;
   - a removal would break enroll, connect, deploy, subscription, access records, or backup/restore;
   - a step needs an owner decision that isn't covered here;
   - an acceptance criterion can't be met.
9. **Scope.** Work only on the phase you were given. If you find a problem that belongs to a later phase, note it in the report and leave it alone.
10. **Downloads.** The owner approved downloading official core binaries on 2026-10-15 (Option B). The conditions:
    - **Sources:** only the official GitHub releases of https://github.com/xtls/xray-core and https://github.com/sagernet/sing-box. Documentation: https://xtls.github.io/ and https://sing-box.sagernet.org/.
    - **Releases:** only stable ones. No pre-releases, betas or alphas.
    - **Verification:** check every archive against a published SHA-256 before extracting it. Use Xray's `.dgst` file, or the `digest` field of the GitHub release-asset API. If neither is available, stop and report.
    - **Location:** extract outside the repository, for example to `../qui-cores/<core>-<version>-<os>-<arch>/`. Never commit a binary.
    - **Use:** pass the binaries to tests through `XRAY_BINARY` / `SINGBOX_BINARY`, and record each version and SHA-256 in the phase report.
    - Any other executable or toolchain download still needs the owner's approval.

### Toolchain (Windows, Git Bash)

```sh
cd <repository>
export PATH="$PWD/.tools/go/bin:$PATH" GOCACHE="$PWD/.tools/gocache" GOPATH="$PWD/.tools/gopath" GOTOOLCHAIN=local
```

- The full test run takes about 3–5 minutes. Run it in the background with a long timeout, and use targeted `go test ./internal/<pkg>/...` while iterating.
- Measure non-test Go lines with:
  `git ls-files '*.go' | grep -v '_test.go$' | grep -v '\.pb\.go$' | grep -v '/testdata/' | xargs cat | wc -l`

### Phase report (every phase ends with this)

Return it to the reviewer:
1. Commits: hash and message.
2. Gate results for the final commit: build, vet, test, web build. List every skipped test.
3. Each acceptance criterion: met or not met, with evidence (command output, test name).
4. Non-test Go lines before and after.
5. Deviations from the plan, and why.
6. Problems noticed that belong to later phases.

---

## 4. Phases

Every phase lists:
- **Goal**
- **Changes**: the exact work
- **Files**: likely affected
- **Remove / simplify**
- **Compatibility & migration**
- **Tests**
- **Verification**
- **Expected result**
- **Risks & rollback**
- **Acceptance criteria**: all of them must hold before the next phase starts

### Revised execution order (2026-10-15, after Phase 4 Part A)

Phase 4 Part A showed that the deletion targets depend on each other in a chain:

**budgets/connections ← recovery cases ← multi-round CA (successor/recovered root) ← offline CLI modes and HTTP routes**

`internal/platform/server/recovery_services.go` binds 17 budget methods into the recovery services. The recovery code uses budget types. `system/backup/successor_ca_candidates.go` uses recovery identity completions. Deleting the lowest layer first breaks every layer above it.

So deletion now runs **top-down**: entry points first (CLI modes, HTTP routes, server wiring, Vue panels), then the code that becomes unreferenced, and the lowest layer last. At each step the compiler shows what's still used, and every commit stays green.

Phase numbers are unchanged. The execution order is now:

**0 → 1 → 2 → 3 → 4 Part A (done) → 9 → 10 → 11 → 4 Part B → 5 → 6 → 7 → 8 → 12 → 13 → 14 → 15**

Rules for the deletion phases (9, 10, 11 and 4 Part B):
- Delete entry points first. Then remove code that has no remaining caller, found with `go build` / `go vet` errors and grep. Unexported dead code shows up as `unused` only via vet/staticcheck, which isn't available offline here, so also grep for each deleted function's callers.
- If a file in scope mixes in code owned by a later deletion phase (budget, CA), delete it with this phase when it has no other caller. Don't keep shims. List these files in the report as "pulled forward from Phase N".
- One delegation should change about 10k lines at most. A large phase is split into parts (A, B, …), each ending green and committed. The reviewer reviews the whole phase after its last part.
- Drop migrations: one per phase, numbered in execution order (0099, 0100, …). The budget drop migration is therefore not necessarily 0099.

The default rollback for every phase: each step is its own commit on `v1`, so `git revert <hash>` (or `git reset --hard <last-accepted-phase-commit>` before review sign-off) restores the previous accepted state. The phase sections only mention extra rollback concerns.

---

### Phase 0: baseline and working-tree cleanup (DONE)

Commits:
- `de6a681` baseline import
- `4e04ee9` clutter moved to an archive directory outside the repository, `.gitignore` extended
- `bd28ea3` docs archived and index rewritten; unused `@lucide/vue` and `lucide-vue-next` removed

---

### Phase 1: agent reliability fixes

**Goal.**
- The agent never leaves a node without a running core because of an apply error.
- It never destroys access to existing encrypted data.
- It never caches a temporary failure forever.
- It never drops a good config because a scheduled refresh failed.

**Changes.**

**1.1 Apply never leaves the core stopped.**
- `internal/agent/core/xray/adapter.go` `Apply`: every check, including the budget recheck, runs **before** `a.stop()`.
- After `stop()`, every error path restarts `previous` (when `previous.Revision > 0`) and sets `RolledBack`. If that restart fails, it returns `rollback_failed`.
- `internal/agent/core/singbox/adapter.go` `Apply`: same rule. The `accountingRun != nil` branch must restart `previous` before returning `accounting_interrupted`.
- *State at handover:* this restructure is already in the working tree, uncommitted, together with `internal/agent/core/xray/apply_rollback_test.go`. Review it, finish it, and commit it.

**1.2 Storage key guard.**
- `internal/agent/storage/contract.go` `Ensure()` may create `configuration.key` only when the directory is fresh:
  - no `current.json`;
  - no non-empty `revisions/`;
  - no `identity/*.enc`;
  - no spool database file.
- Otherwise it returns an error wrapping `ErrStorage` that says the key is missing.
- `internal/platform/vault/vault.go`: after a key file is created, fsync the parent directory. On Windows, a failed directory sync is ignored, matching existing helpers if any.

**1.3 Retryable temporary failures.**
- `internal/agent/reconcile/reconcile.go`: persist a failure in `result.json` only for permanent codes, `render_failed` and `validation_failed`.
- Stop, start, health and rollback failures must be retried when the same revision is sent again.
- Delete a failed revision's directory, which holds sealed secrets, after the failure is recorded. Keep only `result.json` for permanent failures.

**1.4 Scheduled refresh keeps the last good config.**
- `internal/agent/reconcile/schedule.go`: when a time-window re-render or apply fails, keep the running revision, report the error through the normal status path, and retry on the next tick.
- Don't switch to block-all, and don't stop the core.

**1.5 Fsync after atomic renames.**
- In `reconcile.go`, after each atomic rename of `current.json` or `result.json`, fsync the parent directory, using the same helper as 1.2.

**Files.**
- `internal/agent/core/xray/adapter.go`
- `internal/agent/core/singbox/adapter.go`
- `internal/agent/storage/contract.go`
- `internal/platform/vault/vault.go`
- `internal/agent/reconcile/reconcile.go`
- `internal/agent/reconcile/schedule.go`
- tests next to each

**Remove / simplify.** Nothing is removed. Don't refactor beyond the fix.

**Compatibility & migration.**
- No format changes.
- An existing node whose `configuration.key` was lost now fails loudly instead of silently re-keying. This is intended. Document the recovery in the commit message: re-enroll the node.

**Tests.**
- 1.1:
  - The real-core test `TestRealBudgetRecheckKeepsRunningCore` (skips without `XRAY_BINARY`).
  - **Plus** a unit test that runs without a core. Inject a fake process, or factor the stop/start sequence into a small function with function-typed `stop`/`start` parameters, and assert that every post-stop error path calls `start(previous)`. One such test per adapter.
- 1.2: table test with each "not fresh" marker present (one at a time). `Ensure` returns `ErrStorage` and creates no key. A fresh directory still gets a key.
- 1.3: fake adapter with `start_failed` on the first apply and success on the second apply of the same revision. The second apply succeeds. A `validation_failed` revision is still reported from the cache without re-running. A failed revision's directory is removed.
- 1.4: fake adapter whose re-render fails. The running revision stays current, the core isn't stopped, and the next tick retries.
- Each new test must fail on the pre-fix code. Check this by temporarily reverting the fix locally, and mention it in the report.

**Verification.**
- Run `go test ./internal/agent/... ./internal/platform/vault/...`, then the full gate.

**Expected result.** Four bug classes closed, each with a regression test. No behaviour changes on success paths.

**Risks & rollback.**
- The risk is a changed result code that the panel interprets. Before changing codes, grep `internal/panel` for `budget_changed`, `accounting_interrupted`, `start_failed` and `rollback_failed`, and keep existing codes.

**Acceptance criteria.**
- [ ] A1.1 In both adapters, no `return` between `stop()` and a successful `start()` skips restarting `previous`. Verified by the unit tests.
- [ ] A1.2 `Ensure` refuses to create a key in a non-fresh directory. Test passes. The vault fsyncs the parent directory.
- [ ] A1.3 The same revision retries after a temporary failure. Permanent failures stay cached. Failed revision directories are deleted.
- [ ] A1.4 A failed scheduled refresh leaves the running revision and core untouched.
- [ ] A1.5 Parent-directory fsync after renames in `reconcile.go`.
- [ ] A1.6 Each new test is shown to fail without its fix.
- [ ] A1.7 Gate passes on the final commit. Skips are listed.

---

### Phase 2: panel fixes and hardening

**Goal.**
- Subscriptions follow AGENTS.md §10.3.
- First-run setup can't be hijacked.
- Login limiting can't be bypassed by rotating usernames or forging headers.
- Unused config is no longer required.

**Changes.**

**2.1 Subscription node filter.**
- `internal/panel/node/groups.go` (around line 176) must exclude only `disabled` nodes, nodes in maintenance, and non-subscribable nodes.
- Nodes with status `pending`, `online`, `degraded`, `offline`, `applying`, `rollback` or `error` stay in the subscription.
- `pending` nodes have no inbounds yet, so the inbound step drops them anyway.
- Check how "maintenance" is represented. If no such field exists, only `disabled` and non-subscribable apply.

**2.2 Login rate limit.**
- `internal/panel/auth/repository_sqlite.go` (around lines 114–135) limits by IP only. Key it by IP **and** by username: a failure counts against both, and either bucket over the limit blocks.
- Add an optional `server.trusted_proxies` config: a list of CIDRs.
- `X-Forwarded-For` / `X-Real-IP` are honoured only when the direct peer is inside a listed CIDR. Otherwise use `RemoteAddr`.
- Find where the client IP is derived today and change it there only.

**2.3 First-run setup token.**
- At startup, when no Owner exists, generate a random token (32 bytes, base64url) and keep it in memory.
- Log it once at WARN: `first-run setup token: …`. Also write it to `<data_dir>/setup-token` with mode 0600, and delete that file after setup.
- `POST` setup (`internal/panel/auth/handler_http.go` around line 115) requires the token, compared in constant time.
- The web setup page gets a token field.
- After an Owner exists, setup returns 409/403 as it does today.

**2.4 `tls.client_ca` optional.**
- `internal/platform/config/config.go` (around lines 265–269): stop requiring `client_ca`.
- Remove it from `deploy/examples/*.yaml` if nothing reads it. Otherwise keep it as an optional field.
- Configs that still set it must keep loading.

**Files.**
- `internal/panel/node/groups.go`
- `internal/panel/auth/*`
- `internal/platform/config/config.go`
- `internal/platform/httpapi` (client IP helper, if there)
- `internal/platform/server/server.go` (setup token wiring)
- `web/src/modules/system` or the auth setup view
- `deploy/examples/panel*.yaml`

**Remove / simplify.** The `client_ca` requirement.

**Compatibility & migration.**
- Existing installs with an Owner are unaffected by 2.3.
- No schema change is needed if the limiter stores keys as text. If the limiter table has a fixed "ip" column, prefer an in-memory limiter keyed by `ip` and `user:<name>`. It's acceptable for 1–10 users, and restarts reset it. Don't add a migration just for this.

**Tests.**
- Subscription test: offline and degraded nodes are present; a disabled node is absent; a non-subscribable node is absent.
- Login: N failures with different usernames from one IP are blocked. N failures for one username from different IPs are blocked. `X-Forwarded-For` from an untrusted peer is ignored. From a trusted peer it's honoured.
- Setup: without a token → 403; wrong token → 403; correct token → Owner created and the token is invalidated; a second setup → rejected.
- Config: loads without `client_ca`.

**Verification.**
- Targeted tests for `internal/panel/auth`, `internal/panel/node`, `internal/panel/subscription` and `internal/platform/config`, then the full gate.
- Web build: the setup page compiles with the token field.

**Expected result.** The four issues are fixed, with tests. The setup flow is documented in `deploy/examples` comments.

**Risks & rollback.**
- Setup-token wiring could lock out a fresh install. Mitigation: the log line plus the file, plus a test that the full setup path works end to end through `httptest`.

**Acceptance criteria.**
- [ ] A2.1 Subscription includes non-online, non-disabled nodes. Test passes.
- [ ] A2.2 Login is limited per IP and per username. `trusted_proxies` behaves as specified. Tests pass.
- [ ] A2.3 Setup requires the one-time token. The token is shown in the log and in the 0600 file. Tests pass. The web setup page sends the token.
- [ ] A2.4 The panel config loads without `client_ca`.
- [ ] A2.5 Gate passes. Skips are listed.

---

### Phase 3: end-to-end core-flow safety net

**Goal.** One fast test proves that the main product flow works. Every deletion phase after this one runs it.

**Changes.**
- Add `internal/e2e/coreflow_test.go`, or put it in `internal/platform/server` if wiring a server outside that package is impractical.
- The test starts an in-process panel (temp SQLite, temp TLS material) and an in-process agent that uses a **fake core adapter**, implementing `core.Adapter`, that records applied configurations.
- The flow:
  1. Run the first-run setup (with the Phase 2 token), then log in as Owner.
  2. Create a node, then an enrollment token. The agent enrolls and connects over mTLS.
  3. Create a user, a device, a node group (subscription type) containing the node, a VLESS inbound, a Direct default policy and a Block rule for `blocked.example`.
  4. Create and deploy a revision. Wait until the agent reports apply success. The fake adapter received a configuration that contains the user's credential.
  5. Fetch `/sub/{token}` in URI, Base64 and sing-box formats. Each contains the node.
  6. The agent submits an access batch (one allowed record and one blocked record) and a traffic batch for the user.
  7. Query `/api/v1/access-records` and `/blocked`. Both records are present.
  8. Create a backup, change data (delete the user), restore the backup, and verify the user, node group and policy are back.
- It must finish in under about 60 s and need no core binary.
- Helpers may live in `internal/e2e/harness.go` (test-only, `_test.go`) to keep them out of the production build.

**Files.**
- new: `internal/e2e/*_test.go`
- possibly small exported test hooks in `internal/platform/server` and `internal/agent/agenthost`, only if strictly needed

**Remove / simplify.** Nothing.

**Compatibility & migration.** None.

**Tests.** The new test itself. It must not be skipped by default.

**Verification.**
- `go test -count=1 -run CoreFlow ./internal/e2e/...` passes three times in a row (flakiness check).
- Then the full gate.

**Expected result.** A green safety-net test that fails loudly if enroll, connect, deploy, subscription, access records or backup/restore break.

**Risks & rollback.**
- The harness may need production hooks. Keep them minimal and unexported where possible.
- If a step can't be driven in-process without large changes (for example, restore needs a process restart), stop and report it with a proposal. Don't build a new subsystem.

**Acceptance criteria.**
- [ ] A3.1 The test covers all 8 flow steps above. Any omitted step is reported, with the reason.
- [ ] A3.2 It needs no core binary and isn't skipped.
- [ ] A3.3 Three consecutive passes.
- [ ] A3.4 Production code changes are limited to small test hooks, listed in the report.
- [ ] A3.5 Gate passes.

---

### Phase 4: remove per-node budgets and core-level connection ledgers (D2, D3: deletion)

**Goal.** Remove all budget, accounting-run and connection-admission machinery, while keeping per-user traffic ingest working.

**Changes.**

**4.1 Panel.** Delete:
- `internal/panel/budgettransfer/`, `internal/panel/budgetreturn/`, `internal/panel/conversion/`, `internal/panel/nodepause/`;
- their wiring in `internal/platform/server/server.go`;
- budget, transfer, return and recovered-budget files in `internal/panel/user` (27 files reference budgets);
- budget and final-usage code in `internal/panel/deployment` (8 files), `internal/panel/subscription`, `internal/panel/system` (3 files) and `internal/panel/system/backup` (11 files);
- connection allowance and generation code in `deployment`, `node`, `subscription`, `user` and `system/backup`;
- `internal/panel/access/budget_*` and any budget-traffic paths in access.

**4.2 Contracts.**
- `pkg/contracts/desiredstate`: delete `budget.go`, `connections.go`, `quiesce.go` and their tests. Remove `BudgetGrants` and connection grant fields from `state.go`.
- `pkg/contracts/traffic`: keep `batch.go` and `source.go`. Delete `final.go`, `retirement.go`, `connections.go` and `recovery_*.go`, unless they are still used after this phase.
- `proto/`: if removed messages are referenced only by budget code, remove those fields and regenerate (`scripts/generate-proto.ps1`). Otherwise leave the wire as it is and stop populating the fields.

**4.3 Agent.** Delete:
- `internal/agent/core/accounting*.go`;
- accounting, final, retirement, quota and connection files in `core/xray` and `core/singbox`;
- `internal/agent/spool/budget.go` and the spool connection ledgers;
- the `BudgetUsers` / `BindBudgets` adapter hooks and their wiring in `agenthost`;
- the Phase 1 budget recheck and `TestRealBudgetRecheckKeepsRunningCore`. Keep the Phase 1 unit test for the stop/start invariant.

**4.4 Migration.**
- Add `migrations/0102_drop_node_budgets_and_connection_ledger.sql` (0099–0101 went to Phases 9–11).
- It drops the tables created in 0017 and 0023–0034 (budgets, final usage, runtime sources, quota conversions, budget returns, transfers, node pause) and in 0038–0040 (`user_connection_limits`, `user_node_connections`, `user_node_connection_retirements`, `node_live_connections`, `node_live_connection_accounts`).
- Before dropping a table, grep for it in Go and confirm that no remaining code reads it.
- Keep `users.max_connections`, `users.traffic_limit` and `users.used_traffic`.

**4.5 Frontend.**
- Delete the `BudgetTransfer`, `NodeBudget`, `NodePause`, `TrafficReconciliation` and `OriginalTrafficResume` components, their routes and their API calls.
- User pages keep showing `traffic_limit` / `used_traffic`.

**Files.** Listed above. Find the rest with:

```sh
grep -rliE 'NodeBudget|node_budget|BudgetGrant|BudgetUsers|BindBudgets|ConnectionGrant|connection_allowance|ConnectionAllowance|BudgetTransfer|BudgetReturn|QuotaConversion|NodePause|FinalUsage|final_usage|AccountingRun|accounting_run' --include=*.go --include=*.ts --include=*.vue internal pkg cmd web/src
```

**Remove / simplify.** All of the above. Per-user traffic ingest stays: traffic batches still add to `users.used_traffic` (or this becomes trivially possible in Phase 5).

**Compatibility & migration.**
- Pre-refactor backups contain the dropped tables. Restore must accept them: the restore path runs migrations forward, and 0102 drops the tables.
- Backup inspect and compare code for these tables is deleted in the same commit.
- Keep, or add, a test that restores a backup fixture created at baseline (`de6a681`). If there's no fixture, generate one with the baseline code in a temporary worktree (`git worktree add ../qui-baseline de6a681`), commit it under `internal/panel/system/backup/testdata/`, and remove the worktree.
- Agents running old builds may still send budget fields. The panel must ignore unknown or obsolete fields without failing.

**Tests.**
- Delete tests of removed code.
- Keep or update: traffic ingest updates the user's usage; pre-refactor backup restore; the Phase 3 core flow.

**Verification.**
- The grep above returns no matches in non-archive code, apart from comments explaining removal (there should be none).
- Run the core flow test.
- Run the full gate.
- Record non-test lines before and after.

**Expected result.** About 15–20k fewer non-test lines. No budget or connection-admission concept anywhere. Per-user traffic is still counted.

**Risks & rollback.**
- The largest risk is deleting something deploy or subscription depends on. The Phase 3 test catches that.
- Commit in sub-steps (panel, contracts, agent, migration, frontend) so a single bad step can be reverted on its own.

**Acceptance criteria.**
- [x] A4.1 The grep in "Files" returns nothing.
- [x] A4.2 Migration 0102 exists and drops only tables with no remaining readers. The list is in the report.
- [x] A4.3 A pre-refactor backup restores successfully (test name in the report).
- [x] A4.4 Traffic batches still update user usage (test name in the report).
- [x] A4.5 The core-flow test passes.
- [x] A4.6 The web build has no references to deleted components or APIs.
- [x] A4.7 Gate passes. Line counts are reported.

---

### Phase 5: per-user quota with automatic redeploy, and soft connection limit (D2, D3: new behaviour)

**Goal.**
- Implement D3's soft per-user quota enforcement, with automatic revisions.
- Implement D2's observed connection count and over-limit alert.
- Keep the new code small. Target: under about 600 non-test lines in total.

**Changes.**

**5.1 Quota evaluation.**
- In the panel's traffic ingest path (`internal/panel/access/traffic.go` or `internal/panel/user`), after adding deltas, evaluate each affected user: `active = enabled && !expired && (traffic_limit == 0 || used_traffic < traffic_limit)`.
- A daily (or 1-minute) ticker also evaluates expiry, so an idle expired user is still removed.

**5.2 Automatic redeploy.**
- When a user's `active` changes, call the Deployment module to create a revision with summary `auto: user <name> <reason>`, where the reason is `quota_exceeded`, `expired`, `disabled` or `reactivated`. Then deploy it to the affected nodes: those in the user's granted node groups.
- Renderers must already exclude inactive users. Verify this; if they don't, make deployment exclude them.
- Debounce: at most one automatic deploy per node per 60 s. Pending changes coalesce into the next window. Use an in-memory timer only; no new table.
- Manual changes to `enabled`, `expires_at`, `traffic_limit` or `used_traffic` reset go through the same evaluation.

**5.3 Observed connections.**
- Agent telemetry carries a per-user online or connection count, or "unknown". The field may already exist; reuse it.
- Until Phase 6 adds the upstream readers, the agent sends "unknown".
- The panel keeps the latest value per (user, node) in memory, with a timestamp. Values older than 3 report intervals count as unknown.

**5.4 API and UI.**
- User list and detail responses gain `onlineConnections` (number or null) and `overLimit` (bool: true when the sum is known and greater than `max_connections`).
- UI: show the count or "unknown", and a warning badge when over the limit.
- Show quota usage as a bar, with "auto-disabled: quota exceeded" when applicable.

**5.5 SSE.**
- Emit a `system.alert` event when a user crosses into `overLimit`, and when a user is auto-disabled or reactivated.
- Emit once per transition, not per report.

**Files.**
- `internal/panel/access` (traffic ingest)
- `internal/panel/user` (evaluation, DTOs)
- `internal/panel/deployment` (auto revision entry point)
- `internal/platform/events`
- `internal/agent/collector` (unknown count)
- `web/src/modules/users/*`

**Remove / simplify.** Any leftover per-node quota UI wording.

**Compatibility & migration.**
- No migration should be needed (`users` already has the columns). If one is needed, it's 0100 or the next number.
- Behaviour change: users over quota are now removed from node configs automatically. Document it in `deploy/examples` comments or the commit message. Phase 14 documents it properly.

**Tests.**
- Unit:
  - quota crossing → one auto revision whose rendered state excludes the user;
  - raising the quota → reactivation revision includes the user;
  - expiry with no traffic → removal on tick;
  - two crossings within 60 s on one node → one deploy that contains both changes;
  - `overLimit` computation with known, unknown and stale counts;
  - one SSE alert per transition.
- Extend the Phase 3 core-flow test: report traffic over the user's quota and assert that the fake adapter receives a configuration without the user's credential.

**Verification.** Targeted tests, the core-flow test, the full gate, and a manual web build.

**Expected result.** Owner-visible quota enforcement and soft-limit visibility, without any core-level enforcement.

**Risks & rollback.**
- The risk is a redeploy storm caused by a bug in the evaluation. Debounce plus "deploy only on a transition" bounds it, and a test asserts that a steady state produces no deploys.

**Acceptance criteria.**
- [x] A5.1 Crossing the quota produces exactly one automatic revision that excludes the user. Re-enabling produces one that includes the user.
- [x] A5.2 Expiry is enforced without traffic.
- [x] A5.3 The debounce holds (≤1 automatic deploy per node per 60 s). A steady state causes no deploys.
- [x] A5.4 The API returns `onlineConnections` (null when unknown) and `overLimit`. The UI shows the count or "unknown", plus the badge.
- [x] A5.5 SSE alerts are emitted once per transition.
- [x] A5.6 The core-flow test is extended and passes.
- [ ] A5.7 New non-test code is under about 600 lines, or the report explains why not.
- [x] A5.8 Gate passes.

---

### Phase 6: upstream core detection, rendering and stats; delete fork support (D1, D4)

**Goal.** The agent renders only configs that official Xray and sing-box accept, reads per-user traffic and online counts from upstream APIs, and contains no fork-specific code.

**Pre-step (stop condition check).** Before coding, determine from upstream docs and source (web access is allowed for reading):
- (a) Xray: whether `statsUserOnline` and the `GetStatsOnline*` RPCs exist in the latest stable release, and the minimum version.
- (b) sing-box: whether the **official release binaries** include the v2ray API (`with_v2ray_api` build tag) and whether Clash API `/connections` metadata identifies the inbound user.
- (c) Whether Xray `leastLoad` supports weights or costs.

Report the findings in the phase report. If (b) shows that sing-box can't attribute traffic to users on official builds, **stop and report** with options:
- Xray-only per-user stats;
- sing-box nodes report node totals only.

Don't continue without an owner decision.

**Changes.**

**6.1 Detection.**
- `internal/agent/core/xray/detect.go` and the sing-box `executable.go`: accept official version strings. Remove all `-proxycontrol.N` parsing and minimum fork versions.
- Capabilities come from the version number plus runtime probes: stats API reachable; online stats (Xray); Clash API; v2ray API (sing-box).
- Remove `ConnectionAdmissionSupported`, `EgressEnvelope*`, `UdpPerTargetObserved`, `DomainBytesSupported` and `ObservationEdition` from the capability contract (`pkg/contracts/agentpb` / `proto`), or make them always false where removing them would break wire compatibility cheaply.

**6.2 Rendering.**
- Remove `proxyControlConnections`, `proxycontrol_balance`, quota and UDP-envelope options.
- Egress groups map per D4:
  - Xray: `routing.balancers` + `observatory` / `burstObservatory` + `fallbackTag`.
  - sing-box: `selector` / `urltest`.
- Remove the "weighted" option from the API, validation and UI unless the pre-step showed upstream support.
- Enable stats:
  - Xray: `stats {}`, `policy.levels.0.statsUserUplink/Downlink` (+ `statsUserOnline` when supported), `policy.system.statsInbound*`, and an `api` with `StatsService` on a loopback-only inbound/port.
  - sing-box: `experimental.clash_api` on `127.0.0.1:<random port>` with a random secret. No `v2ray_api`: official builds don't include it (D5).
- Every rendered config is validated by the core itself (`xray run -test`, `sing-box check`). This path exists already; keep it.

**6.3 Stats readers.**
- One small reader per core returns `{user → (uplinkDelta, downlinkDelta)}` and `{user → onlineCount | unknown}`.
- Xray: `QueryStats` with `reset: true` on `user>>>` patterns, plus `GetStatsOnline` where supported.
- sing-box (D5): node totals only, from Clash API `/connections` `uploadTotal` / `downloadTotal`, as cumulative counters with diffing. Per-user values are "unknown".
- The reader feeds the existing traffic batch and spool path. Replace the Phase 5 "unknown" placeholder with real counts where available.

**6.4 Delete fork support.**
- Delete `integrations/`, `scripts/build-xray-connections.ps1`, `scripts/build-singbox-scheduler.ps1` and `scripts/build-linux-wireguard-release.ps1`.
- `pkg/contracts/singboxstatspb`: keep only if it matches an upstream proto (v2ray API stats). Otherwise delete it.
- `pkg/contracts/xraystatspb`: keep only if it matches upstream `xray.app.stats.command`. Otherwise regenerate it from upstream.

**6.5 Protocol matrix.**
- Update `pkg/contracts/desiredstate/support.go` and `docs/protocol-support-matrix.md` to what upstream supports: VLESS, Trojan, Shadowsocks, SOCKS5, HTTP, and WireGuard per D4, for each core.
- UI dropdowns read from it; verify that they do.

**Files.**
- `internal/agent/core/xray/*`, `internal/agent/core/singbox/*`
- `internal/agent/capability`, `internal/agent/collector`
- `pkg/contracts/{agentpb,desiredstate,singboxstatspb,xraystatspb}`, `proto/`
- `internal/panel/proxy` (egress group modes)
- `web/src/modules/proxy`
- `integrations/`, `scripts/`

**Remove / simplify.** All fork-only render options, capability flags and build scripts.

**Compatibility & migration.**
- Existing egress groups using `weighted` mode: a migration (next free number) maps them to `roundRobin`. The panel logs the change, and the report lists it.
- Nodes running fork binaries still report fork version strings. Detection must still accept them as their base version.

**Tests.**
- Golden render tests for both cores for: SOCKS5→Direct, VLESS→Direct, VLESS→Trojan, a Block rule, and a primary/backup egress group. The golden files contain no fork keys. Add a test that greps the rendered JSON for `proxycontrol`.
- Stats reader unit tests against a fake gRPC/HTTP server: delta computation, reset, unknown.
- Real-core validation tests (`xray run -test`, `sing-box check`) skip without `XRAY_BINARY` / `SINGBOX_BINARY`.
- **Core binaries:** download them under rule 10. The old `sing-box-1.14.2-windows-amd64.zip` in `qui-archive` has unknown provenance; don't use it.

**Verification.**
- `grep -rniE 'proxycontrol|proxyControl' --include=*.go internal pkg cmd` returns nothing, except possibly the Go module path `proxycontrol/` in imports. Exclude that with `grep -v '"proxycontrol/'`.
- `integrations/` doesn't exist.
- Golden tests, the core-flow test and the gate all pass.

**Expected result.** The agent works with official binaries. There's no fork code.

**Risks & rollback.**
- The risk is an upstream capability gap, handled by the stop condition.
- Rendering regressions are caught by golden tests and native validation.
- This phase has the highest production risk. Keep it in sub-commits (6.1–6.5).

**Acceptance criteria.**
- [x] A6.1 Pre-step findings are reported. No unresolved capability gap.
- [x] A6.2 The grep above is clean. `integrations/` and the fork scripts are deleted.
- [x] A6.3 Golden render tests for the 5 scenarios on both cores. No fork keys.
- [x] A6.4 Stats readers have unit tests. The agent reports real per-user deltas and online counts, or unknown.
- [x] A6.5 Weighted mode is handled per the pre-step result, including a data migration if it was removed.
- [x] A6.6 Protocol matrix code and doc are updated and consistent.
- [x] A6.7 Core-flow test and gate pass. Real-core skips are listed.

---

### Phase 7: access records from upstream sources, and upstream integration tests

**Goal.** Domain-level access records come from upstream outputs only (Xray access log, sing-box Clash API or log). Real-core integration tests cover the AGENTS.md scenarios where binaries are available.

**Changes.**

**7.1 Xray.**
- Render `log.access` to a file in the agent runtime directory, with rotation by the agent (size cap, for example 20 MB, truncate on rotate).
- Parse lines: time, source, `tcp:`/`udp:` target host:port, `[inbound -> outbound]` tags, `email:`, and accepted/rejected.
- Map outbound tags back to the rule and action through the render's tag map. Aggregate per minute into the existing access rollup contract (`pkg/contracts/access`).

**7.2 sing-box.**
- Poll Clash API `/connections`: host, rule, chains, and user metadata if present.
- Record each new connection ID once. Aggregate per minute. If the user can't be determined, record the user as unknown.

**7.3 Delete the fork observation path.**
- Remove the fork-only observation and association contracts (`pkg/contracts/observation` datagram/association envelopes) and their collectors.
- Remove `internal/panel/access/{association,observation*,storage_observation}.go` if they only serve the fork path.

**7.4 Nullable bytes.**
- Byte fields per domain become nullable, with a migration if the columns are `NOT NULL`. The UI shows "—".
- Label the egress "configured egress" in the UI.

**7.5 Integration tests** (skip without binaries):
- SOCKS5 → Direct
- VLESS → Direct
- VLESS → Trojan → node B (two cores on loopback)
- a Block rule
- per-user stats readout
- access log parse → rollup

**Files.**
- `internal/agent/collector`
- `internal/agent/core/{xray,singbox}`
- `pkg/contracts/{access,observation}`
- `internal/panel/access/*`
- `web/src/modules/access`
- migration (next number) if needed

**Remove / simplify.** The fork observation and association code.

**Compatibility & migration.** Existing access rows keep their byte values. New rows have null bytes.

**Tests.**
- Parser unit tests from captured upstream log lines, including IPv6, UDP, rejected lines and lines without `email`.
- Clash API poller test against a fake HTTP server.
- Rollup aggregation test.
- The Phase 3 core-flow test still passes.

**Verification.** Unit tests, gate, and the real-core tests when binaries are provided.

**Expected result.** Access records work with official cores. AGENTS.md §20 fields are covered, except the D4 gaps.

**Risks & rollback.**
- Access log volume on busy nodes. The cap and rotation bound it.
- Log format changes between Xray versions. The parser tolerates unknown fields, and its tests pin the format.

**Acceptance criteria.**
- [x] A7.1 Xray access log parser and aggregation have tests covering the listed line types.
- [x] A7.2 The sing-box source works with user-or-unknown semantics. Tested.
- [x] A7.3 Fork observation code is deleted (list in report).
- [x] A7.4 Bytes are nullable end to end. The UI shows "—" and "configured egress".
- [x] A7.5 The 6 integration scenarios exist. Their pass or skip status is reported.
- [x] A7.6 Core-flow test and gate pass.

---

### Phase 8: official core install and update with checksum verification

**Goal.** Nodes install and update official core releases, verified by SHA256.

**Changes.**
- The managed core update in `internal/agent/updater` and `pkg/contracts/release` gets the download URL for an official GitHub release asset for the node's OS and arch, plus an expected SHA256. The Owner enters the SHA256 (or it comes from the release's published digest file).
- The agent downloads, verifies, extracts, runs `version`, then activates the new core through the existing supervised switch with rollback.
- Keep the existing signed-catalogue mechanism only if it's already simpler than this. Otherwise reduce it to "version + URL + SHA256 entered by the Owner".
- `cmd/release`: keep only what the reduced flow needs.

**Files.** `internal/agent/updater/*`, `pkg/contracts/release/*`, `cmd/release/*`, the panel system "core versions" handlers and view.

**Remove / simplify.** The fork release catalogue and any fork-specific packaging.

**Compatibility & migration.** Agents already installed keep their current core until an update is triggered.

**Tests.**
- Updater test with a local `httptest` server serving a fake archive: the correct SHA activates it; the wrong SHA is rejected and the old core stays; a failed `version` run rolls back.

**Verification.** Gate. A manual dry run on Windows with the fake archive.

**Expected result.** One simple, verifiable core update path.

**Risks & rollback.**
- A broken update could leave a node without a core. The existing supervised switch with rollback mitigates this; a test covers it.

**Acceptance criteria.**
- [x] A8.1 A checksum mismatch never activates a binary (test).
- [x] A8.2 A failed post-install `version` check rolls back (test).
- [x] A8.3 The update flow needs only version, URL and SHA256 from the Owner.
- [x] A8.4 Gate passes.

---

### Phase 9: remove offline CLI modes and receipt archives

**Goal.** Delete the offline coordinator, CA and controller CLI modes of `cmd/panel`, and the access receipt archives.

**Changes.**

**9.1 `cmd/panel`.**
- Delete `ca_*.go`, `recovered_root.go`, `recovery_coordinator*.go`, `recovery_owner_credentials.go`, `controller_*.go`, `settled_candidate.go`, `disaster.go`, `historical_backup.go` and their flags.
- Keep: `config`, `check-config`, `version`, `restore-backup`, `restore-password-file`.
- `internal/platform/recovery`: delete `controller_*`, `coordinator*`, `recovered_root*` and `settled_candidate*`.
- Keep `database`, `files`, `install`, `verify`, `cleanup` and `file_progress`, which online restore uses.

**9.2 Receipt archives.**
- Delete `internal/panel/access/*receipt*` and `*_receipt_archive*`, plus the related jobs, routes and Vue panels.
- Drop the tables from migrations 0062–0065 in a new migration.
- Keep ingest, queries, the domains view and retention (30 days allowed, 90 days blocked, hourly, daily).

**Files.** `cmd/panel/*`, `internal/platform/recovery/*`, `internal/panel/access/*`, `internal/platform/server/server.go`, `web/src/modules/access`, a new migration.

**Remove / simplify.** As listed.

**Compatibility & migration.**
- Pre-refactor backup restore must still pass.
- `restore-backup` must keep working: covered by the existing `cmd/panel/restore_test.go`.

**Tests.** Delete the tests of removed code. Keep the restore tests and the core-flow test.

**Verification.**
- `cmd/panel` has ≤ about 10 flags: count with `grep -c 'flag\.' cmd/panel/main.go`.
- Gate and core-flow test pass.

**Expected result.** Several thousand lines removed. The panel CLI is simple.

**Risks & rollback.**
- The online restore path may import deleted recovery files. Build failures reveal that. Keep the files it needs and report them.

**Acceptance criteria.**
- [ ] A9.1 The listed files are deleted. Panel CLI flags are ≤ about 10 (list them).
- [ ] A9.2 Receipt archive code, routes, UI and tables are gone. Retention still works (test).
- [ ] A9.3 `restore-backup` and the pre-refactor backup restore tests pass.
- [ ] A9.4 Core-flow test and gate pass. Line counts are reported.

---

### Phase 10: remove recovery cases

**Goal.** Delete the recovery-case machinery: node identity recovery, replacement, isolation, loss review and disposition, and owner-expired renewal.

**Changes.** Delete:
- the `internal/panel/system` recovery files (`recovery*`, `handler_recovery*`, `offline_recovery*`);
- in `internal/panel/system/backup`: `recovery*`, `recovered*`, `controller*`, `disaster*`, `historical*`, `node_*_compare`, `node_evidence`;
- node recovery, replacement, isolation and loss-review code in `internal/panel/node`;
- recovery code in `internal/panel/deployment` and `internal/panel/user`;
- the agent-side recovery-evidence and owner-trust-recovery paths (`cmd/agent/recovery_*`, `owner_*`, and the matching `internal/agent/identity` and `control` files that only serve recovery);
- the `Recovery*` Vue panels (about 20).

Then:
- Drop the tables from 0037, 0052–0055, 0058, 0060, 0061, 0066–0085, 0087–0089 and 0098 in a new migration. Verify each table against the remaining code before dropping it.
- Replacement for a lost or compromised node: disable the node (which revokes its certificate) and enroll a new one. Phase 14 documents this.

**Files.** As listed. Find them with `find internal cmd web/src -iname '*recover*' -o -iname '*isolation*' -o -iname '*disposition*' -o -iname '*loss*'`.

**Remove / simplify.** As listed.

**Compatibility & migration.**
- Pre-refactor backups with recovery data must restore. The tables are dropped by the migration.
- Nodes in a "recovery" state in an existing DB: the migration sets them to `disabled` or `offline`. Pick `offline` if they still hold a valid certificate, and report which.

**Tests.** Delete the removed tests. Keep backup restore, node disable/revoke, enrollment and the core flow.

**Verification.** The `find` above returns nothing in `internal`, `cmd` or `web/src`. Gate and core-flow test pass.

**Expected result.** About 15–20k fewer lines. Node lifecycle is: enroll → active → disabled.

**Risks & rollback.**
- Recovery code is entangled with the deploy and node status paths. Delete it in small commits by area, running the core-flow test after each.

**Acceptance criteria.**
- [ ] A10.1 The recovery file search returns nothing (or only an explicitly justified remainder).
- [ ] A10.2 The drop migration lists each table, and each one is confirmed unused.
- [ ] A10.3 Disabling a node revokes its mTLS access (existing or new test).
- [ ] A10.4 Pre-refactor backup restore passes.
- [ ] A10.5 Core-flow test and gate pass. Line counts are reported.

---

### Phase 11: single node CA and agent storage format collapse

**Goal.** One node CA: signing, leaf issuance at enrollment, leaf renewal and key rotation before expiry, and revocation by node disable. One agent storage format.

**Changes.**

**11.1 PKI.**
- In `internal/platform/pki`, keep `authority.go`, `active_authority.go`, `current_signing.go` and whatever they need.
- Delete the successor, recovered, retirement and global-retirement code.
- Delete `ca_*` and `recovered_*` in `internal/panel/system` and `system/backup`, and root retirement in `internal/panel/node`.
- `cmd/agent/successor_ca_stage.go` and `ca_trust.go`: keep only plain CA trust loading.

**11.2 Agent identity.**
- In `internal/agent/identity`, keep identity, material, renewal, key rotation, enrollment and CA trust. Delete the rest. The package is 14.2k lines today; the target is under about 3k.

**11.3 Storage formats.**
- `pkg/contracts/agentstorage` keeps one current format: the one written by default today (Write=2) plus a simple version check.
- Delete the recovered and successor formats (up to 17).

**11.4 Migration.** Drop the tables from 0043–0046, 0049–0051, 0071–0073, 0086 and 0092–0097, after verifying each.

**11.5 Manual CA rotation procedure.**
- Write it as a short section, saved for Phase 14's `docs/operations.md`: generate a new CA, configure it, then re-enroll each node.
- Put the draft text in the phase report, not in a new doc.

**Files.**
- `internal/platform/pki/*`, `internal/agent/identity/*`, `pkg/contracts/agentstorage/*`
- `cmd/agent/*`
- `internal/panel/system/*`, `internal/panel/node/*`
- a new migration

**Remove / simplify.** As listed.

**Compatibility & migration.**
- *Assumption:* no production node uses the recovered or successor formats (V1 was never accepted). A dev node that fails to start must be re-enrolled. Document this in the report.
- An agent with a Write=2 store must start unchanged. Add a test with a fixture store created by the current code.

**Tests.**
- Enrollment issues a leaf.
- Renewal before expiry works.
- Key rotation works.
- A disabled node is refused at mTLS.
- A Write=2 fixture store opens.
- Core-flow test.

**Verification.** Gate, core-flow test, identity package line count.

**Expected result.** The PKI is easy to understand. About 15k fewer lines.

**Risks & rollback.**
- The highest security sensitivity of all phases. Every deletion must keep: verified chain on gRPC; client cert not requested on port 443; single-use enrollment.
- The report must re-check these three points explicitly, naming the tests.

**Acceptance criteria.**
- [ ] A11.1 Enrollment, renewal, key rotation and disable/revoke tests pass.
- [ ] A11.2 The three security invariants above hold, with test names.
- [ ] A11.3 A Write=2 fixture store opens. The other formats are deleted.
- [ ] A11.4 `internal/agent/identity` is under about 3k non-test lines, or the report explains why not.
- [ ] A11.5 The drop migration has verified table list. Pre-refactor backup restore passes.
- [ ] A11.6 Core-flow test and gate pass.

---

### Phase 12: spool reduction and agent CLI reduction

**Goal.**
- The spool does only: queue, upload, ACK, a 100 MB cap, and eviction of the oldest allowed-access records first.
- The agent CLI has about 10 flags.

**Changes.**

**12.1 Spool.**
- Delete ack history, replay inspection, proof, retirement, retained key, digest export, database candidate and cache comparison from `internal/agent/spool`.
- The SQLite or file format stays readable for queued records. If the schema changes, the spool opens the old schema and migrates in place, or drains it. Pick the simpler option, and test it.

**12.2 Agent CLI.**
- Reduce `cmd/agent` flags (78 today) to: `config`, `check-config`, `version`, `enroll` (+ `token-file`), `renew-node-certificate`, `rotate-node-key`, plus the supervisor/worker flags the updater needs.
- Delete `identity_diagnostic.go`, `recovery_*`, `owner_*` and `enrollment_token.go`'s extra modes if unused.
- Delete the matching `deploy/linux/*` scripts in Phase 13, not here.

**Files.** `internal/agent/spool/*`, `cmd/agent/*`, `internal/agent/agenthost/*`, `internal/agent/supervisor/*`.

**Remove / simplify.** As listed.

**Compatibility & migration.** Records queued by an older agent must be uploaded, not lost, after an upgrade. Test this with a fixture.

**Tests.**
- Cap and eviction order: allowed records are evicted first; blocked and traffic records are kept.
- Offline queue → reconnect → upload → ACK → deletion.
- Old-spool fixture drains.
- CLI flag parsing.

**Verification.** Gate, core-flow test, flag count.

**Expected result.** The spool is under about 1.5k lines (5.4k today). The agent CLI has about 10 flags.

**Risks & rollback.**
- Losing queued records on upgrade. The fixture test covers it.

**Acceptance criteria.**
- [x] A12.1 Spool behaviour tests pass (cap, eviction order, upload/ACK, old-fixture drain).
- [x] A12.2 Agent flags are ≤ about 12 (list them). The supervisor/updater still works (tests).
- [x] A12.3 Core-flow test and gate pass. Line counts are reported.

---

### Phase 13: install scripts, release build, CI

**Goal.** One install script per role, one release build script, and a CI workflow that matches the gate.

**Changes.**

**13.1 `deploy/linux/`.**
- Keep `install.sh`, with `--panel` and `--node` modes:
  - install the binary;
  - create the user and the AGENTS.md §30 directories;
  - install the systemd unit;
  - for a node, install the official Xray or sing-box release with SHA256 verification.
- Keep `enroll.sh`, `uninstall.sh`, `panel-ca.sh`, `node-ca.sh` and `renew-certificate.sh`.
- Delete all `repair-*`, `resume-*`, `*legacy*`, `inspect-*`, `restore-*`, `prepare-*`, `install-prepared-*` and `export-original-*` scripts.

**13.2 Release build.**
- Add `scripts/build-release.sh`:
  1. `npm --prefix web ci && npm --prefix web run build`;
  2. embed the web build;
  3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64|arm64` builds of panel and agent;
  4. one tar.gz per architecture;
  5. `SHA256SUMS`.
- Convert `generate-proto.ps1` to `generate-proto.sh`.
- Delete `build-linux-release.ps1`, `build-only.ps1`, `check.ps1`, `check-snapshot.ps1`, `snapshot-source.ps1` and `package-development-build.ps1`.
- Keep `time.test.cjs` only if `npm run test:time` still has a meaning.

**13.3 CI (`.github/workflows/check.yml`).**
- Steps: web build, `go vet ./...`, `go test -race ./...`, build panel and agent.
- Drop the special storage-test agent builds unless a remaining test needs them.
- Add an optional (manual or nightly) job that downloads official Xray and sing-box releases, verifies their checksums, and runs the core integration tests.

**Files.** `deploy/linux/*`, `deploy/systemd/*`, `scripts/*`, `.github/workflows/*`, root `package.json` (if it only wraps scripts).

**Remove / simplify.** As listed.

**Compatibility & migration.**
- Existing installs use the same paths (§30), so re-running `install.sh` upgrades in place without touching data.

**Tests.**
- `shellcheck` on the remaining scripts, if it's available. Otherwise `bash -n`.
- `scripts/build-release.sh` runs under Git Bash and produces both archives and `SHA256SUMS`.

**Verification.**
- Run the release build locally and list the artifacts.
- Validate the CI YAML (actionlint if available; otherwise a careful read).

**Expected result.** 6 deploy scripts (from 23), and 2–3 build scripts.

**Risks & rollback.**
- The install script can't be tested end to end on Windows. The owner's Linux VM acceptance covers it, and the report must say so.

**Acceptance criteria.**
- [x] A13.1 `deploy/linux` contains only the 6 listed scripts. All pass `bash -n` (and shellcheck if available).
- [x] A13.2 `build-release.sh` produces amd64 and arm64 archives and `SHA256SUMS` locally.
- [x] A13.3 The CI workflow mirrors the gate. The optional core job is defined.
- [x] A13.4 Gate passes.

---

### Phase 14: docs and AGENTS.md V1.1

**Goal.** Short, current docs that match the code. AGENTS.md reflects D1–D4.

**Changes.**

**14.1 Docs** (Chinese, consistent with AGENTS.md):
- `docs/README.md`: index.
- `docs/architecture.md`: one page.
- `docs/deployment.md`: install the panel, add a node, upgrade.
- `docs/operations.md`:
  - backup and restore;
  - certificate renewal;
  - manual CA rotation (from Phase 11);
  - replacing a lost node (from Phase 10);
  - quota and soft-limit behaviour, including the documented overshoot;
  - the D4 upstream limits.
- `docs/protocol-support-matrix.md`.
- `docs/v1-acceptance.md`, updated for D1–D4.

**14.2 `AGENTS.md`.**
- Update:
  - §9.1: the connection limit is an observed soft limit; the quota is soft-enforced by the panel;
  - §15: egress selection modes per D4;
  - §20/§21: bytes per domain optional; configured egress;
  - §22: automatic quota revisions;
  - §32: stage 4 uses official Xray;
  - §34: affected acceptance items;
  - the status header.
- Bump to **V1.1** and add a change log entry for D1–D4.
- Replace the "第 5.5 步" reference in the header with "Phase 14".

**14.3 Root `README.md`.** A short quick start that points to the deployment doc.

**Files.** `docs/*`, `AGENTS.md`, `README.md`.

**Remove / simplify.** Any remaining stale docs outside `docs/archive`.

**Compatibility & migration.** None.

**Tests.**
- Every doc link resolves. Check with a small grep for `](` targets plus `test -e`.
- The commands in `docs/deployment.md` match the script flags.

**Verification.** Reviewer read-through against the code.

**Expected result.** About 6 current docs. AGENTS.md V1.1.

**Risks & rollback.** None beyond inaccuracy, which the review catches.

**Acceptance criteria.**
- [x] A14.1 The listed docs exist and are short (each under about 300 lines, except the protocol matrix).
- [x] A14.2 AGENTS.md is V1.1, with D1–D4 reflected in the listed sections and a change log.
- [x] A14.3 No broken relative links.
- [x] A14.4 Gate passes (docs-only, but run it anyway).

---

### Phase 15: performance and polish

**Goal.** Remove the remaining measured hot spots without new subsystems.

**Changes.**

**15.1 SQLite.**
- Keep the single writer (`MaxOpenConns(1)` in `internal/platform/db/db.go`).
- Add a second read-only pool (WAL, `mode=ro`, a few connections) for queries, so backup and retention jobs don't block login or `/sub`.
- Route read-only repository calls through it where that's trivial: auth session lookup, subscription, lists.

**15.2 Auth request cost.**
- Cache owner-count and setup state in memory, invalidated on change, to cut per-request queries from 4 to about 2 (`internal/panel/auth/service.go` around lines 116–128).

**15.3 Policy UI note.** Show that IP/CIDR rules match only literal IPs; there's no DNS resolution.

**15.4 Optional (Xray only).**
- Use HandlerService add/remove user for quota changes, to avoid restarting the core.
- Only do this if it stays under about 200 lines. Otherwise keep the redeploy.

**15.5 Toolchain doc.**
- Document a normal Go install, version per `go.mod`, last tested 1.27.1.
- `.tools/` stays for the owner's machine, but nothing depends on it.

**Files.** `internal/platform/db/*`, `internal/panel/auth/*`, `internal/panel/subscription/*`, `web/src/modules/routing`, `docs/deployment.md`.

**Remove / simplify.** Redundant per-request queries.

**Compatibility & migration.** WAL mode may already be enabled. Verify it, and don't change the DB file format.

**Tests.**
- A test where a long write transaction is held and an authenticated GET plus `/sub` still answer within 1 s.
- The query count per authenticated request is asserted, using a counting driver wrapper in the test or a hook.

**Verification.** Gate, core-flow test, and the two tests above.

**Expected result.** Login and subscriptions stay responsive during backups.

**Risks & rollback.** A read pool on the same file can see stale reads right after writes. Use it only for read-only endpoints that tolerate that.

**Acceptance criteria.**
- [x] A15.1 The concurrency test passes.
- [x] A15.2 The per-request query count is ≤ 2 (asserted).
- [x] A15.3 The UI note is present.
- [x] A15.4 Gate passes. Section 8 definition of done is evaluated in the report.

---

## 5. Progress checklist

- [x] Phase 0: baseline and cleanup (`de6a681`, `4e04ee9`, `bd28ea3`)
- [x] Phase 1: agent reliability fixes (`a41584f`…`b363ec5`, review fix below)
- [x] Phase 2: panel fixes and hardening (`2013941`…`7ca4d2f`)
- [x] Phase 3: end-to-end core-flow safety net (`04c55d6`)
- [x] Phase 4: remove per-node budgets and connection ledgers. Part A `a7ce863`…`8b07a90`; Part B `d1be4ac`…`6c24909` (4B.1 agent, 4B.2 panel, 4B.3 contracts and migration 0102). The agent spool's final-usage residue is deferred to Phase 12; see the review log.
- [x] Phase 5: per-user quota auto-redeploy and soft connection limit (`bd3b416`…`fab5586`, as 5A/5B)
- [x] Phase 6: upstream core detection, rendering, stats; delete fork support (`7adaee6`…`594289b`, as 6.1–6.5)
- [x] Phase 7: access records from upstream sources, integration tests (`89b1c1b`…`de6c6b7`, as 7.1–7.5)
- [x] Phase 8: official core install/update with checksum (`e6fd2f1`…`a87ff63`, as 8A investigation, 8B, 8C fix)
- [x] Phase 9: remove offline CLI modes and receipt archives (`bf38d45`…`25ffcf3`, plus fix `7aaeb07`)
- [x] Phase 10: remove recovery cases (`88598d5`…`9cb8c5a`, as 10A/10B/10C)
- [x] Phase 11: single node CA and storage format collapse (`111a6d1`…`9f6a86d`, as 11A/11B)
- [x] Phase 12: spool and agent CLI reduction (`7b86043`…`9438df2`, as 12A spool, 12B CLI)
- [x] Phase 13: install scripts, release build, CI (`b30b40a`…`27eddba`, as 13.0 `--enroll` regression fix, 13.1 six deploy scripts, 13.2 `scripts/build-release.sh`, 13.3 CI)
- [x] Phase 14: docs and AGENTS.md V1.1 (14.1 `c9ae940`/`485cd73`/`7b8e98f`/`d04d8c1`, 14.2 `a58c29f`, 14.3 `e4ac591`, plus the follow-up fixes `fa9e71a`/`7ed9b81`/`49f72c2`/`3610d06`/`522c683`)
- [x] Phase 15: performance and polish (`a8007ee` read-only pool, `0b431d9` auth state cache, `0874ee3` IP/CIDR note, `29274b4` connection wording, `c421cfa` panel-ca.sh, `83177b8` reproducible archives, `a497899` toolchain doc; 15.4 skipped on purpose)

## 6. Review procedure (reviewer, after each phase)

1. Read the phase report.
2. Check `git log` and `git diff <phase-start>..HEAD --stat`.
3. Independently re-run the gate (`go build`, `go vet`, full `go test`, web build) and the core-flow test, once it exists.
4. Check each acceptance criterion against evidence: tests, greps, file listings. Spot-read the riskiest diffs.
5. Record the result in Section 7.
6. If any criterion fails, send the phase back to the implementer with the specific gaps. The next phase starts only when all of them pass, or when the owner explicitly waives one.

## 7. Review log

(One entry per phase: date, commits, gate result, skips, criteria status, line counts, deviations, follow-ups.)

### Phase 1: accepted 2026-10-15

**Commits.**

| Commit | Step |
|---|---|
| `a41584f` | 1.1 |
| `b1ec016` | 1.2 |
| `b7418d2` | 1.3 |
| `1ab8340` | 1.4 |
| `a3b1da0` | 1.5 |
| `b363ec5` | 1.2 follow-up: a test fixture opened the spool before the key existed |
| (next commit) | reviewer gofmt fix in `internal/agent/storage/contract.go` |

**What changed.**
- **1.1** Both adapters route stop/start/rollback through a small `applySwitch` helper. In Xray, the budget recheck runs before `stop()`. In sing-box, an interrupted accounting run still restarts `previous`.
- **1.2** `storage.Ensure` creates `configuration.key` only in a fresh directory. `vault.Open` fsyncs the parent directory, through a new exported `vault.SyncDir` that is a no-op on Windows.
- **1.3** Only `render_failed` and `validation_failed` are cached. Other apply failures delete the revision directory and are retried.
- **1.4** A failed scheduled refresh returns the error and keeps the running revision. It no longer switches to block-all or stops the core.
- **1.5** `writeFile` fsyncs the parent directory after the rename.

**Verification.**
- **OpenCode** ran the gate on `b363ec5`: green, with 179 real-core or child-process tests skipped (list in the phase report).
- **Reviewer** ran:
  - the full gate on `a3b1da0`: build, vet, test and web build all green;
  - the targeted tests on `b363ec5` (`platform/server`, `agent/reconcile`, `agent/storage`, `agent/core/...`, `platform/vault`): green.
- Reviewer read the diffs and checked three things:
  - every `identity/*.enc` file is sealed with `configuration.key`, so the freshness guard can't block a node that has just enrolled;
  - `control.go` calls `storage.Ensure` (line 249) before it opens the spool (line 316);
  - existing result codes are unchanged.
- Each new test was shown to fail without its fix (A1.6, evidence in the report).
- `gofmt` found one unformatted changed file; the reviewer fixed it, and a gofmt check is now part of the gate.

**Criteria.** A1.1–A1.7 all met.

**Lines.** Non-test Go, excluding `*.pb.go`: 126,393 → 126,525.

**Deviations, accepted.**
- The post-apply `persistence_failed` receipt is still cached. It's a recovery marker; revisit it in Phase 10.
- The fsync covers every `writeFile` call, a superset of what 1.5 asked for.

**Follow-ups.**
- Phase 14 must document the 1.4 trade-off: after a failed scheduled or expiry transition, the previous config keeps serving until the next successful render, so a time-window block or an expiry can be delayed.
- The spool marker in the 1.2 guard is weak, because the spool has its own key. Revisit it in Phase 12.
- Real-core coverage needs owner-supplied `XRAY_BINARY` / `SINGBOX_BINARY` by Phase 6.

### Phase 2: accepted 2026-10-15

**Commits.**

| Commit | Step |
|---|---|
| `2013941` | 2.1 |
| `0324201` | 2.2 |
| `1c14650` | 2.3 |
| `76ef17c` | 2.4 |
| `40a7944` | 2.3 follow-up: restore test needs the token |
| `50c1c8b`, `ac9e4d2`, `ef44eae` | fixes requested in review |
| `7ca4d2f` | reviewer fix |

**What changed.**
- **2.1** The subscription filter drops only disabled, maintenance and non-subscribable nodes. Its only caller is subscription generation.
- **2.2** Login uses an in-memory limiter keyed by `ip:` and `user:` (5 attempts per 15 min). `login_attempts` is no longer written.
  - New optional `server.trusted_proxies` setting (CIDR or bare IP, checked when the config loads).
  - Forwarding headers are honoured only when the direct peer is a trusted proxy. The client IP is then the rightmost entry in `X-Forwarded-For` that isn't itself a trusted proxy.
- **2.3** One-time setup token: 32 bytes, logged once at WARN, and written to `<db dir>/setup-token`. The file is created fresh with `O_EXCL` and mode 0600.
  - Validation, Owner creation and token consumption happen atomically under a mutex.
  - A wrong token is rejected before PBKDF2 runs (reviewer fix).
  - The web setup page has a token field.
- **2.4** `tls.client_ca` is optional for the panel. Node mTLS is unaffected: `EnrollmentTLS` always replaces `ClientCAs` with the node CA and sets `ClientAuth`.

**Review findings, all fixed.**
1. The client IP came from the leftmost `X-Forwarded-For` entry, which the client controls, so the per-IP limit could be bypassed by rotating it.
2. The setup token fields were read and written without a lock.
3. `os.WriteFile` kept the mode of a stale token file.
4. After fix 2, the token check ran after PBKDF2, which let unauthenticated requests burn CPU.

**Verification.**
- OpenCode's gate on `ef44eae` was green.
- Reviewer's full gate on `7ca4d2f`: build, vet, test, web build and gofmt all green.
- Each new test was shown to fail without its fix, except the concurrent-setup test. The race detector needs cgo, and this machine has no gcc, so that test only guards behaviour here. CI with `-race` (Phase 13) is the real check.
- No new skips.

**Criteria.** A2.1–A2.5 met.

**Lines.** 126,526 → 126799.

**Deviations, accepted.**
- The limiter is in memory and resets on restart.
- Audit IPs outside the auth package still use `RemoteAddr`.
- `Server.SetupToken()` is an in-process accessor for tests.

**Follow-ups.**
- Add a migration dropping `login_attempts` in a later drop migration. Phase 10 also removes its only remaining reader, `auth/recovery.go`.
- Phase 3's end-to-end test reads the token through `Server.SetupToken()`.
- The username bucket means anyone can lock the Owner out of login for 15 minutes with 5 bad attempts. That's the accepted trade-off for 1–10 users; document it in Phase 14.
- Windows can't check 0600 permissions, so there's no permission test.

### Phase 3: accepted 2026-10-15

**Commit.** `04c55d6`.

**What changed.**
- New test `internal/platform/server/coreflow_test.go`, `TestCoreFlowEnrollDeploySubscriptionTelemetryAndBackup` (401 lines). It runs an in-process panel and the real agent `control.Run`, with a fake `core.Runtime`.
- New production hook, 8 lines: `config.Agent.NewRuntime` (`yaml:"-"`, nil means current behaviour), used in `control.run`.

**Steps covered.**
1. Setup with the token, then login.
2. Enroll and mTLS connect.
3. User, device, node group, VLESS inbound, Direct default and Block policy.
4. Deploy. The fake core receives a config that contains the user's UUID, and the panel records success with the node online.
5. Subscriptions in URI, Base64 and sing-box formats.
6. Allowed and blocked access records plus a traffic batch, queued offline and delivered on reconnect.
7. Both record views are correct, and `users.used_traffic` = 300.
8. Backup, verify, delete the user, then restore through the production `backup.PrepareRestore`. The staged DB contains the user, the node group and the policy.

**Verification.**
- OpenCode: the gate was green, and 3× runs took 3.5 s, 3.1 s and 3.0 s.
- Reviewer: independent gate on `04c55d6` was green (full test run 3 min), and the 3× run passed (12.2 s). Read the test and the hook diff; the hook adds no cycle, and the line-ending churn in an intermediate edit didn't reach the commit.

**Criteria.** A3.1–A3.5 met. A3.1 holds with the deviation below.

**Lines.** 126,799 → 126,806.

**Deviation, accepted.**
- Step 8 doesn't swap the live database. The swap needs a process restart, and `cmd/panel/restore_test.go` covers it. The safety net covers backup integrity and restore preparation.

**Follow-ups.**
- `platform/config` now imports `agent/core` for the hook. Move the seam into the `control` package when Phase 12 reshapes the agent CLI and config.
- Phases 4 and 12 must keep the test compiling. It uses `spool.Open`, `Enqueue` and `SaveTraffic`, and `backup.PrepareRestore`.
- Phase 5 extends the test with a quota crossing.

### Phase 4 Part A: partial; accepted parts recorded 2026-10-15

**Commits.**

| Commit | What it removed |
|---|---|
| `a7ce863` | `budgettransfer/` and `budgetreturn/` with their wiring |
| `a300c38` | `conversion/` and `nodepause/` with the scheduler goroutine |
| `8b07a90` | the frontend: `BudgetTransfer`, `NodeBudget`, `NodePause`, `ConnectionLimit`, `TrafficReconciliation` and `OriginalTrafficResume` panels, plus their API methods |

**Blocker, stopped correctly under rule 8.** Removing the budget code in `user`, `deployment` and `system/backup` broke more than 20 compile sites in the recovery subsystem: `recovery_services.go` binds 17 budget methods. The chain continued into CA successor code. OpenCode reverted the uncommitted attempt.

**Reviewer's check.** 20 of the 72 panel files that reference budget or connection identifiers are recovery files. Recovery code in panel and platform totals about 26.8k lines.

**Decision (reviewer, sequencing only).** Delete top-down. See "Revised execution order" in Section 4.

**Verification.** Reviewer's independent gate on `8b07a90`: build, vet, full test, web build and gofmt all green.

**Lines.** 126,806 → 124,936.

**Criteria.** A4.4, A4.5 and A4.6 met. A4.1, A4.2 and A4.3 are deferred to Part B.

**Deviations, accepted.**
- Two unexported `Server` fields replace test access through the deleted `nodePauseService`.
- `ConnectionLimitPanel` and `connectionsApi.ts` were removed as connection-allowance UI.

**Notes.**
- `quota_conversions` and `node_pause_tasks` are still read by `user.Save` and `deployment.nodeAvailable`. Part B removes those readers.
- `golang.org/x/tools/cmd/deadcode` can't be built offline: it needs `x/telemetry`, which isn't in the module cache. Downloading it needs owner approval under rule 10. For now, use compile errors and grep.

### Finding during Phase 9: migration bytes changed at baseline import (fixed in `7aaeb07`)

**What happened.**
- The baseline commit `de6a681` ran under Git for Windows' system-wide `core.autocrlf=true`. That rewrote the mixed line endings in `migrations/0004_deployment.sql` and `0036_agent_updates.sql`.
- `db.Open` (`internal/platform/db/db.go:113-118`) and backup validation (`system/backup/inspect.go` `validateMigrations`) compare each migration's SHA-256 with `schema_migrations.checksum`.
- As a result, any build from git refused to open an existing pre-refactor database and rejected pre-refactor backups. Fresh databases were unaffected, which is why the gate stayed green.

**How it was found.** OpenCode's Phase 9 backup fixture was generated in a fresh worktree, which also had LF migrations. It failed only in the main tree. The reviewer bisected it, compared the two trees, and instrumented the archive checks.

**Fix.**
- Committed the original bytes of both files.
- Added `.gitattributes` with `migrations/*.sql -text`.
- Added `migrations/checksums_test.go` (`TestAppliedMigrationsAreImmutable`), which pins the checksum of every migration. New migrations must be added to it.

**Consequences.**
- That first fixture was invalid; it's archived in `qui-archive/2026-10-15/invalid-fixture/`.
- A pre-refactor fixture must be generated from `de6a681` with the original bytes of 0004 and 0036 copied in.
- A stray `internal/platform/server/state/` directory from an Oct 13 test run was moved to `qui-archive/2026-10-15/stray-server-state/`.

**Note.** The working branch is now `v1`, at the same commit as `master`. It was created outside this review. Work continues on `v1`.

### Phase 9: accepted 2026-10-16

**Commits.**
- `bf38d45`, `99daa3b`: 9.1. Deleted the offline CA, recovery, controller and disaster CLI modes, and the platform recovery code they used.
- `1ae8d2d`, `dfb47fd`: 9.2. Removed the receipt archives (backend, migration `0099_drop_receipt_archives.sql`, UI).
- `7aaeb07`: migration-bytes fix (reviewer; see the finding below).
- `25ffcf3`: pre-refactor fixture, generator and restore test. This was re-delegated after the first run was canceled mid-task with nothing committed.

**What was added.**
- `internal/platform/server/testdata/prerefactor_backup.pcb`: 58,740 bytes, sha256 `daddf055…d07a`. It was generated at `de6a681` with the original bytes of 0004 and 0036 copied in. It contains:
  - a user, a node group and a block policy, created via the HTTP API;
  - a valid, vault-sealed `access_receipt_archives` row;
  - a `user_node_budgets` row with its version row.
- `testdata/generate_prerefactor/`: the generator (a Go test that only runs when `PREREFACTOR_FIXTURE_OUT` is set) and a README with the exact steps.
- `TestPreRefactorBackupRestores`: runs `PrepareRestore`, then `db.Open` (forward migrations). It asserts:
  - the business rows are present;
  - the four receipt-archive tables are gone;
  - the budget row is present.
- `.gitignore`: one negation line for the fixture.

**Meaningfulness check (OpenCode, not committed).** Changing one byte in `0004_deployment.sql` makes the test fail with `backup authentication or integrity verification failed`.

**Reviewer's independent gate.** `go build`, `go vet` and `go test -count=1 ./...` (all packages, including the core-flow and `cmd/panel` restore tests) pass. `npm --prefix web run build` passes. `gofmt -l` on the new Go files is clean.

**Criteria.**
- A9.1 met. The panel CLI has 7 flags: `config`, `check-config`, `version`, `restore-backup`, `restore-password-file`, `inspect-restore-installation`, `restore-installation-report`.
- A9.2 met. No receipt-archive code remains outside the fixture generator. The retention tests (`internal/panel/access/retention_settings_test.go`, `repository_test.go`, `server/retention_test.go`) pass.
- A9.3 and A9.4 met.

**Lines.** Non-test Go, excluding `*.pb.go` and `testdata`, counted with `git ls-files`: 129,321 (`50c3840`) → 118,923, a drop of 10,398. This method counts more files than the one used in earlier entries, so compare deltas, not totals. Later entries use this method.

**Follow-ups.**
- Phase 4B drops the budget tables. When it does, it must change the budget assertion in `TestPreRefactorBackupRestores` to "table gone", not delete the check.
- Every later drop migration must keep this test green and add its tables to the gone-check.

### Phase 6 pre-step: findings 2026-10-16 (stop condition: owner decision needed)

The reviewer ran this against the official binaries downloaded under rule 10:
- `xray-26.3.27-windows-amd64`: zip sha256 `d004c392…e1ad`, which matches its `.dgst`.
- `singbox-1.14.3-windows-amd64`: zip sha256 `d8b86ef5…1518`, which matches the GitHub asset `digest`.

**(a) Xray online stats.**
- Present in v26.3.27: `statsUserOnline`, plus the `GetStatsOnline` and `GetStatsOnlineIpList` RPCs (the `statsonline` and `statsonlineiplist` commands).
- Also present: `stats` / `statsquery` and `adu` / `rmu`.

**(b) sing-box.**
- The official 1.14.3 release is built without `with_v2ray_api`. The current sing-box stats path (`internal/agent/core/singbox/stats.go` renders `experimental.v2ray_api`) therefore can't run on official binaries.
- Probe (`qui-cores/probe/`, socks inbound with two users, Clash API): `/connections` metadata has **no user field**. It has `type`, `sourceIP`, `host`, `destinationPort` and similar.
- Per-user attribution **is** possible indirectly. With a route rule `auth_user: [alice]` → outbound `u:alice`, each connection shows `chains: ["u:alice"]` and `rule: "auth_user=alice => route(u:alice)"`, with live `upload` / `download` counters.
- Costs of that approach:
  1. Every policy rule that routes to a real outbound would need a per-user copy (or per-user outbound clones), so the config size grows with users × rules.
  2. `/connections` only lists open connections. Bytes of connections that open and close between polls are lost, so counts are approximate.

**(c) Xray `leastLoad`.**
- Its `costs` field (`StrategyWeight{regexp, match, value}`) is an RTT cost multiplier for picking the lowest-latency outbound, not weighted traffic sharing.
- sing-box `selector` / `urltest` has no weights.
- Per the plan, "weighted" is removed and existing groups migrate to `roundRobin`. No owner decision is needed.

**Decision needed before Phase 6 (asked 2026-10-16).**
- **Option 1, recommended.** Xray nodes report exact per-user traffic and online counts. sing-box nodes report node totals only (Clash API `uploadTotal` / `downloadTotal`), and per-user quotas are enforced only for traffic seen on Xray nodes.
- **Option 2.** sing-box per-user traffic through per-user route tagging plus Clash API polling. Counts are approximate, and rendering gets more complex.
- **Option 3.** Same as Option 1, but for now the panel forbids per-user quotas on users granted sing-box node groups.

Phases 10, 11, 4B and 5 don't depend on this decision and continue meanwhile.

### Phase 10: accepted 2026-10-16

**Commits.**
- 10A.1 `88598d5`: Vue recovery-case panels and their API.
- 10A.2 `481f2b3`: `internal/panel/system` recovery-case services, routes and server wiring.
- 10A.3 `3d76568`: `system/backup` recovery-case archive code and its inspection calls.
- 10B.1 `d8d471c`: node identity recovery, replacement, isolation, loss review and disposition, and owner-expired renewal (panel).
- 10B.2 `9a343a7`: recovered-release floor; controller fence (`panelstore/controller_fence.go`, `server/controller_runtime_guard.go`).
- 10B.3 `fd60640`: migration `0100_drop_recovery_cases.sql`.
- 10C.1 `613811b`, 10C.2 `9cb8c5a`: agent recovery evidence, owner-trust recovery and owner-expired renewal.

**How it ran.**
- The plan's Phase 10 was split into 10A, 10B and 10C to stay near 10k lines per delegation.
- The first 10A run ended `child_rejected` (context exhausted) after committing 10A.1. Its uncommitted work, 55 coherent deletions, was handed to a narrower follow-up.
- Two of OpenCode's runs stalled on path-less `grep -r` crawling `.tools`. The reviewer killed them, and later handoffs warn about it.

**Migration 0100.**
- Drops 21 tables, children before parents.
- Rebuilds `owner_expired_certificate_renewals` without its foreign key to `node_identity_recovery_decisions`. The reviewer checked that all 18 columns and constraints are kept, and that the original table had no indexes or triggers.
- The reviewer checked that every table referencing a dropped table is either dropped or rebuilt.
- Kept, because Phase 11 or 4B code still reads them: `recovery_cases`, `recovery_case_issues`, `recovery_case_decisions`, `recovery_identity_completions`, `owner_expired_certificate_renewals` (Phase 11), `recovered_budget_retirements` (4B) and `recovery_owner_credentials` (auth, revisit in Phase 11).
- `node_accepted_messages` and `node_enrollment_receipts` are normal operation and stay.
- No node status migration was needed: the `nodes.status` CHECK never included a recovery-only value.

**Controller fence.** Removed. It only checked the handoff and retired-controller marker files. Single-panel ownership (`panelstore.Ownership`) is separate and stays.

**Reviewer's independent gate.**
- Run at `3d76568`, `fd60640` and `9cb8c5a` in the separate `qui-head` worktree, so OpenCode's edits couldn't interfere.
- `go build`, `go vet` and `go test -count=1 ./...` all pass. `npm --prefix web run build` passes.
- `TestDisabledNodeIsRefusedAtMTLS` (new), `TestPreRefactorBackupRestores` (now also checks the 0100 tables are gone) and the core-flow test pass.
- OpenCode saw `TestAgentReconnectsAfterCentralDatabaseReopen` fail once under load. The reviewer ran it with `-count=5`: pass. Watch it.

**Criteria.**
- A10.1 met with a justified remainder. The remaining `*recover*` / `*loss*` files are:
  - Phase 11: CA rounds, successor, recovered root;
  - Phase 4B: budget, transfer and connection adoption;
  - Phase 12: collector and spool loss;
  - kept features: traffic ingest crash recovery, the restore path, normal agent update recovery, and `auth` owner credentials.
  - No `*isolation*` or `*disposition*` files remain.
- A10.2, A10.3, A10.4 and A10.5 met.

**Lines.** 118,923 → 97,776 (−21,147). 211 files changed: +390 / −22,694.

**CLI.** The agent has 78 → 63 flags. Phase 12 reduces it further.

**Deviations, accepted.**
- 10A.1 normalized CRLF in `BackupPage.vue`, which had mixed line endings. Ignoring line endings, only 3 lines changed.
- `node/identity_helpers.go` was created to hold helpers that kept code still uses.

**Follow-ups.**
- Phase 11 must delete the kept recovery-case tables and files above, together with the CA round code.
- Phase 11 must also check `auth/recovery*.go` / `recovery_owner_credentials`. If they only served the offline coordinator removed in Phase 9, delete them.
- Phase 12 should check the agent's "normal update recovery" files (`updater/baseline_recovery.go`, `recovery_proof.go`, `recovery_inspection.go`, `control/baseline_recovery.go`, `reconcile/release_hold_recovery.go`). Keep them only if a test proves they are part of normal update rollback.

### Phase 11: accepted 2026-10-16

**Commits.**
- 11A (panel), `111a6d1`…`0c619ba`:
  - successor/round UI;
  - successor, transition, retirement and recovered-root CA code;
  - offline-coordinator auth (`recovery_owner_credentials`);
  - migration `0101_single_node_ca.sql`;
  - three invariant tests;
  - an LF fix for `SettingsPage.vue`.
- 11B (agent), `59bbd25`…`6ad54bd`:
  - successor/round/retirement CA stages;
  - storage collapsed to the current format;
  - Write=2 store fixture test;
  - the remaining successor/transition PKI code.
- Fix `9f6a86d`.

**Reviewer finding (fixed in `9f6a86d`).**
- `.gitignore` has `*.key`, so the fixture's `configuration.key` and `identity/client.key` were never committed.
- The fixture tests passed only in OpenCode's tree. They failed in the reviewer's clean worktree.
- The fix adds two negations plus `.gitattributes` `-text` for the fixture, and was proven in a fresh worktree.
- **Rule from now on:** the reviewer's gate always runs in the separate clean `qui-head` worktree, never only in the tree that produced the change.

**Security invariants (A11.2).** All are in `internal/platform/server`.
- (a) gRPC requires a node-CA client certificate:
  - `TestGRPCRequiresNodeCAClientCertificate` (`node_ca_invariants_test.go`) shows that no certificate, or one from another CA, is refused.
  - `TestProductionTLSRequiresNodeCertificateButNotBrowserCertificate` (`server_test.go`) is the positive control: the same production setup and the same client trust config accept a node-CA leaf.
- (b) Port 443 never requests a client certificate: `TestHTTPSDoesNotRequestClientCertificate`.
- (c) Enrollment tokens are single-use: `TestEnrollmentTokenIsSingleUse`.
- Also passing:
  - `TestDisabledNodeIsRefusedAtMTLS`;
  - `TestRenewalCannotReplaceAuthenticatedKeyOrBypassRevocation`;
  - `TestRenewalLostResponseIsIdempotentAndPossessionActivatesNewIdentity`;
  - the agent key-rotation tests in `internal/agent/identity`.

**What the reviewer checked by reading code.**
- `pki.loadAuthorityMode` loads one `ca.crt`/`ca.key`. It rejects a certificate that isn't a CA, isn't ECDSA, or is outside its validity window.
- The `Welcome` CA-transition fields are always empty, and the agent ignores them.
- Key rotation now commits by writing `client.key` then `client.crt` atomically (0600, fsync). It no longer uses the vault-sealed `active-certificate.enc` (format 3).
  - That is the same storage every never-rotated node already used.
  - A torn pair after a crash fails `tls.X509KeyPair`; the agent never uses mismatched material. Rerunning the rotation command redoes the commit from the staged journal.

**Storage.**
- Only identity format Write=2 / ReadMax=2 remains.
- A store using a deleted format is refused with `stored agent identity format N is no longer supported; re-enroll this node`.
- **Consequence:** a dev node that ever rotated its key under the old code is on format 3 and must re-enroll. This is consistent with the plan's assumption that no production node exists; V1 was never accepted.
- Fixture: `internal/agent/identity/testdata/store_write2/` (generator in `testdata/generate_store/`). Tests:
  - `TestWrite2StoreFixtureOpensUnchanged`;
  - `TestDeletedIdentityFormatStoreIsRefusedWithReenroll`.

**Migration 0101.**
- Drops 14 tables, children first: the CA successor, transition, retirement, recovered-root and operation-request tables, the remaining recovery-case tables, `owner_expired_certificate_renewals` and `recovery_owner_credentials`.
- Every candidate was unreferenced. No kept table references a dropped one.
- `TestPreRefactorBackupRestores` checks that all of them are gone.

**Auth.**
- `AuthenticateRecoveryOwner` and `RecoverOriginalOwner` had no callers.
- `recovery_owner_credentials` only served the offline coordinator removed in Phase 9.
- Online Owner password handling uses `admins` and is unchanged.

**Manual CA rotation (draft for `docs/operations.md`, Phase 14).**
1. Stop the panel.
2. Back up the identity directory (`node_identity_directory`, default `state/panel-identity`): `ca.crt`, `ca.key`, `configuration.key`.
3. Delete `ca.crt` and `ca.key`, keeping `configuration.key`. On start, `pki.LoadOrCreate` creates a new self-signed node CA.
4. The gRPC port now trusts only the new CA (`node.EnrollmentTLS` uses `authority.VerificationPEM()`).
5. For each node: disable the old node record (this revokes it), create an enrollment token, and re-enroll the agent.
6. When every node is back, delete the backed-up old CA files.

**Criteria.**
- A11.1 met.
- A11.2 met (tests above).
- A11.3 met.
- A11.4 met: `internal/agent/identity` went from 12,882 to 2,022 non-test lines.
- A11.5 met.
- A11.6 met: the reviewer's clean-worktree gate passed at `9f6a86d` (build, vet, all tests, web build).

**Lines.**
- Non-test Go: 97,776 → 78,017 (−19,759).
- `internal/platform/pki`: 2,953 → 390.

**CLI.** The agent has 63 → 36 flags. Phase 12 targets about 10.

**Follow-ups.**
- Phase 12: on agent start, consider finishing a key rotation commit that a crash interrupted (the staged journal plus a mismatched pair). For now the operator reruns the command.
- Phase 14: document the manual CA rotation above, the format-3 re-enroll consequence, and rerunning an interrupted key rotation.

### Phase 4 Part B: accepted 2026-10-17 (Phase 4 complete)

**Environment.** Work moved to a new machine (main checkout plus a separate reviewer worktree). The uncommitted 4B.2 tree arrived with `internal/platform/recovery/database_test.go` zeroed (all NUL bytes) and CRLF churn in about 100 files. The reviewer restored the zeroed file from HEAD, reverted the CRLF-only changes and kept the 27 real edits plus 98 staged deletions.

**Commits.**

| Commit | Step |
|---|---|
| `d1be4ac`, `948d2d3` | 4B.1 agent: accounting runs, final usage, in-core quotas, connection admission and ledgers |
| `e6ccf1c` | 4B.2a panel: user/node budgets, connection allowances, final-usage settlement, pause, conversion, source epochs (133 files) |
| `a257e7e` | 4B.2b fix: subscription still asserted removed `NodeBudgetAvailable`/`ConnectionAvailable`, so every user with `traffic_limit > 0` got no subscription. Test `TestSubscriptionTrafficLimitAllowsBelowAndRefusesAtLimit`, shown failing on `e6ccf1c` |
| `204f80e` | 4B.3a remaining panel/web final-usage residue. The panel accepts and ignores `FinalUsageBatch` from old agents |
| `70e6555` | 4B.3b migration `0102_drop_node_budgets_and_connection_ledger.sql`, sha256 `5c73021e…a6e4`, pinned |
| `6c24909` | 4B.3c `desiredstate` budget, connection-allowance and quiesce types removed. Old states carrying `budget`/`connection_allowance` user keys still decode (`TestStateToleratesRemovedBudgetAndConnectionAllowanceKeys`) |

**Migration 0102** drops 22 tables, children first. Each was confirmed unreferenced: `user_node_budgets`, `user_node_budget_versions`, `quota_conversions`, `quota_conversion_targets`, `unissued_budget_returns`, `budget_return_tasks`, `budget_return_commands`, `issued_budget_returns`, the five `budget_transfer_*` tables, `recovered_budget_retirements`, `deployment_final_usage`, `runtime_final_usage`, `runtime_sources`, `node_pause_tasks`, `node_pause_attempts`, `user_connection_limits`, `user_node_connections`, `user_node_connection_retirements`. Kept: `node_live_connections` and `node_live_connection_accounts`, because the dashboard still reads them (see Phase 5 note).

**Reviewer checks.**
- `DisableGuard` removal is safe. The deleted `deployment.DisableNodeTx` only refused to disable a node during a budget pause or conversion; it never revoked anything. Revocation is `ValidateIdentity` checking `nodes.enabled`, covered by `TestDisabledNodeIsRefusedAtMTLS`.
- Over-quota today: `user.Active()` refuses the subscription. The deployed DesiredState still contains the user's credentials (deployment never reads quota or expiry). Phase 5 adds the automatic redeploy.

**Criteria.**
- A4.1 met with a documented remainder. Remaining grep hits: the agent spool final-usage queue/replay/inspection (`internal/agent/spool/*`, `control/spool_status.go`, `core/lifetime.go`) and `pkg/contracts/traffic/{final,source}.go` that serves it, moved to Phase 12; `proto/agent.proto` and `agent.pb.go` (no `protoc` on this machine, so the wire stays and the fields are unpopulated); the legacy-key tolerance in `desiredstate/state.go`; the ignore branch in `node/control.go`; the pre-refactor fixture generator and backup test assertions.
- A4.2 met (0102, list above).
- A4.3 met: `TestPreRefactorBackupRestores` asserts all 22 tables are gone after restore.
- A4.4 met: `TestCoreFlowEnrollDeploySubscriptionTelemetryAndBackup` asserts `users.used_traffic == 300`.
- A4.5 met.
- A4.6 met: web build clean.
- A4.7 met: reviewer's clean-worktree gate passed at `a257e7e`, `70e6555` and `6c24909` (build, vet, all tests, web build, gofmt on changed files).

**Lines.** Non-test Go: 74,654 → 63,466 (Part B total; 78,017 at the end of Phase 11).

**Flake.** `TestAgentReconnectsAfterCentralDatabaseReopen` failed once in an implementer full run under load and passed on rerun, the same as during Phase 11. Phase 15 should look at it.

**Follow-ups.**
- Phase 5: the dashboard reads `node_live_connections*`, but its writer went away in 4B.2a, so measured connections are empty until Phase 5 feeds the soft connection count. Either reuse these tables for D2 or replace them and drop them in that phase's migration.
- Phase 12: delete the agent spool final-usage queue, heads, ACK and replay, then `traffic/final.go`; let old spool DBs open by ignoring or dropping the local table.
- When `protoc` is available (needs owner approval to download), remove the dead fields: `Hello.budget_retirement_supported`, `Welcome.final_usage_supported`/`runtime_source_supported`/`budget_retirement_supported`, `Ack.final_usage_epoch`/`runtime_source_epoch`, `ApplyResult.final_usage_json`, `FinalUsageBatch`, `RuntimeSourceBatch`, `BudgetRetirementCommand`/`Result`, and the `final_usage_*` heartbeat heads. Mark their numbers `reserved`.

### Phase 5: accepted 2026-10-17

**Commits.**

| Commit | Step |
|---|---|
| `bd3b416` | 5A.1 `user.QuotaState` (seed and evaluate, reasons `disabled` > `quota_exceeded` > `expired`), triggers from traffic ingest, Owner save and traffic reset |
| `590109a` | 5A.2 `server/quota.go` debounced per-node automatic revisions (60 s window, in-memory timers), 1-minute expiry tick, `system.alert` per transition |
| `2479e3e` | 5A.3 core-flow test: a quota cut makes the fake core apply a revision without the user's credential |
| `58b9596` | 5A.4 review fix: automatic revisions are built from the node's **published** state (`mergeUserAccess`), not the current DB, so unpublished Owner edits are never pushed. `schedule` uses `deployment.Published` instead of raw SQL on a deployment table |
| `35b2ca9` | 5A.5 review fix: agents enforce `ExpiresAt` themselves (`RuntimeAt`), so merged users come from the fresh view, and an Owner expiry change on an active user schedules a `renewed` refresh (no alert) |
| `e39a751` | 5B.1 in-memory `connections.Store` fed by the existing `ConnectionStatus` snapshots (stale after 3 heartbeat intervals); `onlineConnections`/`overLimit` on user list and detail; dashboard measured numbers from the store; `connection_limit` alert once per upward crossing; migration `0103_drop_node_live_connections.sql` (sha256 `7e1361a7…06ea`) |
| `fab5586` | 5B.2 user pages: count or "未知", over-limit badge, quota bar and auto-disabled text, D5 sing-box note |

**Reviewer findings fixed during the phase.**
- 5A.2 rebuilt the whole node state from the DB, which would have published Owner edits without review (AGENTS.md §22). Fixed in 5A.4.
- After 5A.4, extending an active user's expiry never reached nodes, which keep enforcing the old `ExpiresAt`. Fixed in 5A.5.
- The first 5A.5 run returned a fabricated report (a nonexistent commit `5836c52`, no file changes). It was caught by checking git and redone. Since then every handoff asks for raw `git`/`go test` output, and the reviewer still checks.

**Environment incident.** The 5B run died when C: filled up: `.tools/gocache` had grown to 13 GB from repeated full test runs. The reviewer ran `go clean -cache` (regenerable), checked the git index and scanned the uncommitted files (no NUL or empty files). Keep at least 5 GB free; clear the cache when it isn't.

**Criteria.**
- A5.1 met: `TestQuotaCrossingDeploysOnceWithoutCredential`, `TestQuotaResetReactivatesAndRestoresCredential`, `TestQuotaRevisionExcludesUnpublishedOwnerChanges`, `TestQuotaReactivationRestoresCredentialToMatchingInboundOnly`.
- A5.2 met: `TestExpiredUserRemovedByTickWithoutTraffic`.
- A5.3 met: `TestTwoCrossingsCoalesceIntoOneDeploy`, plus repeated over-quota reports and saves without changes cause no deploy (`TestQuotaSaveWithoutAccessChangeDoesNotDeploy`). A restart seeds the evaluation state, so it causes no deploys.
- A5.4 met: `TestHandlerObserveFillsConnectionFields`, `TestForUserReportsCountsAndUnknown`, `TestStoreKeepsOnlyFreshSnapshots`.
- A5.5 met: one alert per transition (quota tests), `TestObserverAlertsOncePerUpwardLimitCrossing`.
- A5.6 met: `TestCoreFlowEnrollDeploySubscriptionTelemetryAndBackup` extended.
- A5.7 **not met, accepted**: Phase 5 adds +1,014/−70 non-test lines, against a target of about 600. About 190 lines are the two review fixes (published-state merge, renewal refresh). The rest is the connection store, per-user aggregation and the dashboard switch, which also allowed dropping two tables.
- A5.8 met: reviewer's clean-worktree gate at `2479e3e`, `58b9596`, `35b2ca9` and `fab5586` (build, vet, all tests, web build, gofmt).

**Lines.** Non-test Go 63,466 → 64,410.

**Deviation.** The `e39a751`/`fab5586` trailers read `Claude 5.5` instead of `Claude Opus 5.5`. Left as is (no history rewrites).

**Follow-ups.**
- Phase 6: the agent's current connection snapshot source is fork-era (`proxy_user_sessions_v1`). Replace it with upstream readers (Xray `GetStatsOnline*`; sing-box sends no per-user snapshot, so counts stay unknown under D5).
- Phase 14: document the quota overshoot (one report interval + up to 60 s debounce + deploy time) and that automatic revisions only change user access on top of the published state.

### Phase 6: accepted 2026-10-17

From this phase on, implementation is delegated to Cline (owner's instruction, 2026-10-17). The workflow is unchanged.

**Commits.**

| Commits | Step |
|---|---|
| `7adaee6`, `ca0349a`, `3dbcd87` | 6.1 detection: official version strings only. A fork `-proxycontrol.N` suffix is stripped to its base version. Capabilities come from the version: Xray stats API always, online stats from 24.11.11; sing-box Clash API yes, v2ray API never. The fork capability contract and the relay envelope are removed. |
| `8b115d4`, `4c20bec`, `66ab628`, `ffd4ed0`, `6caff55` | 6.2 rendering: no fork keys or types. Weighted strategy removed, with migration `0104_egress_weighted_to_round_robin.sql` (sha256 `e74f8003…eab`) and `desiredstate.Decode` reading stored `weighted` revisions as `round_robin`. Fork egress-proof ingest deleted. Golden render tests for both cores. |
| `0f002fa`, `a89706d`, `42e5ea6`, `e4c58ec` | 6.3 readers: Xray `GetStatsOnline` per user (a hand-written invoke on the existing messages, because protoc isn't available). sing-box Clash API `/connections` node totals, diffed, sent in the existing `Heartbeat.upload_bytes/download_bytes` and shown on the node page. Real-core test adapters got their node identity. |
| `1cfc9f0`, `a474315`, `c49cc3d` | 6.4 deleted `integrations/`, the three fork build scripts, `singboxstatspb`, and the tests that needed a patched core (17 files). |
| `bddf91c`, `deef628`, `594289b` | 6.5 protocol matrix (code and doc) for the official cores. `egress_proof`/`inbound_bytes` dimensions removed. The panel rejects protocols the node's core can't carry (`proxy.ValidateCoreProtocol`). The UI reads `/api/v1/protocol-support`. |

**Criteria.**
- A6.1 met: pre-step findings above; the owner decided D5.
- A6.2 met, with explained residue. `integrations/` and the fork scripts are gone. The grep still hits:
  - the version-suffix tolerance in both `Detect` regexes, which the compatibility note requires;
  - the golden tests' negative assertions;
  - two digest-salt literals;
  - the `PROXYCONTROL-OBS` / association-envelope code, which Phase 7.3 deletes.
- A6.3 met: `TestGoldenRenderedConfigurations` on both cores (SOCKS5→Direct, VLESS→Direct, VLESS→Trojan, Block, primary/backup group). It is validated by the real cores when binaries are set.
- A6.4 met:
  - `TestOnlineSnapshot*` and `TestNodeTrafficTotals*` (fake servers).
  - `TestRealOnlineStatisticsTrackAnAuthenticatedClient` and `TestRealClashApiNodeTrafficTotalsCountForwardedBytes`.
  - Xray per-user deltas keep the existing cumulative `QueryStats` and panel-cursor path (`reset: false`, deliberately: the panel diffs cumulative counters). `TestRealCoreAccessAccountAggregationPrivacyAndOfflineRestartDelivery` sees `used_traffic > 0` from real Xray.
- A6.5 met: weighted removed from validation, API and UI, with migration 0104 and a decode compatibility test.
- A6.6 met: `support_test.go` pins the matrix, and `core_support_test.go` covers panel rejection.
- A6.7 met: the reviewer's clean-worktree gate passed at `3dbcd87`, `6caff55`, `e4c58ec` and `594289b`. The full suite with official Xray 26.3.27 and sing-box 1.14.3 at `594289b` passes except two real-core tests that already failed before this phase (they failed with `start_failed` before 6.3c):
  - `TestRealXrayContinuesOfflineAndReplaysTrafficAfterCentralRestore`: the recovery test never calls `ConfigureStartupRecorder`.
  - `TestRealMTLSCoreUpdate…/{xray,sing-box}-pre-switch-rejection`: the update ends `failed_closed` where the test expects a rollback. This is Phase 8 territory.

**Accepted deviations.**
- Xray `primary` and `latency` groups are agent-selected. Only the chosen member is rendered, because upstream has no first-healthy strategy. `round_robin` uses `routing.balancers` (`roundRobin`) with `observatory` and `fallbackTag`. On sing-box, `round_robin` and `latency` both render `urltest`.
- The proto keeps the dead capability and welcome fields because protoc isn't available. Nothing sets or reads them.
- `xraystatspb` stays a wire-compatible subset of upstream `xray.app.stats.command`.
- The 6.3 commit trailers read `Claude 5.5`. The 6.2 report's line count (60,407) was wrong: the method gives 62,891 at `6caff55`.

**Lines.** Non-test Go 64,410 → 58,890. This meets the under-70k target.

**Follow-ups.**
- Phase 7: delete the `PROXYCONTROL-OBS` observation path, the association envelopes, the egress-proof read side (`GET /access-records/egress-proofs` and its page), and `Welcome.connection_observation_supported`. Fix or replace the recovery replay real-core test.
- Phase 8: real-binary coverage of official core updates (the pre-switch-rejection expectation). `scripts/package-development-build.ps1` still packages fork-era `.tools/w*` cores.
- Phase 8/12: agent self-update no longer has a real-binary test, because the supervised/managed agent tests needed a patched core and were deleted. `verifyManagedParentDeath` has no caller, and `TestManagedAgentParentChild` only skips.
- Phase 14: the "当前用户连接" label is a count of distinct client IPs on Xray. Document the sing-box node-total semantics (counted since the agent started).
- `TestRealHTTPDirectBlockRollbackAndReconcilerRestart` (sing-box) is flaky on Windows because of port-reuse semantics.

### Phase 7: accepted 2026-10-17

**Commits.**

| Commits | Step |
|---|---|
| `89b1c1b` | 7.1 Xray: `log.access` goes to `<data dir>/runtime/access.log`. The agent tails the file (`xray/access_log.go`) with a 20 MB cap: it truncates and keeps following, the file is truncated at core start so nothing is replayed, and the tail drains on stop. This replaces the unbounded stdout pipe. Parser tests use captured upstream lines (`testdata/xray-access-lines.txt`). |
| `e484fd2` | 7.2 sing-box: the core log stays the single access source. Upstream Clash API `/connections` lists only open connections (a probe saw 0 of 20 rejected requests) and names no user, while the debug log carries user, destination, `match[N]` rule index and outbound for every connection. Captured-line tests plus `TestRealSingboxAccessRecordsRejectedAndAllowedConnections`. |
| `545d6e2`, `9b8fd52`, `849603e` | 7.3 deleted the `PROXYCONTROL-OBS` ingest, the observation/association contracts, the panel observation read side, the egress-proof page/API and the Welcome observation flags. Migration `0105_drop_fork_observation.sql` (sha256 `9290d1d0…a436`) drops six tables. Their restore preservation went in the same commit, and the pre-refactor backup test asserts the tables are gone. |
| `8e91165` | 7.4 migration `0106_nullable_access_bytes.sql` (sha256 `8651342e…1eb7`) rebuilds `access_rollups` with nullable byte columns. Existing rows keep their values; unknown bytes are NULL and show as "—". The egress column is labelled 配置出口. |
| `de6c6b7` | 7.5 the recovery replay test now installs `ConfigureStartupRecorder` and passes with real Xray, with no assertion weakened. Added `TestRealVlessToDirectForwarding` (sing-box). |
| `7dc41b1` | checklist tick (by the implementer, rule 7) |

**Integration scenarios (A7.5).** All pass with official Xray 26.3.27 and sing-box 1.14.3 and skip without the binaries.

| Scenario | Xray | sing-box |
|---|---|---|
| SOCKS5/HTTP → Direct | `TestRealAllProtocolInboundOutboundAndConversion`, golden `socks5-direct` | `TestRealHTTPDirectBlockRollbackAndReconcilerRestart`, golden `socks5-direct` |
| VLESS → Direct | `TestRealAllProtocolInboundOutboundAndConversion` | `TestRealVlessToDirectForwarding` |
| VLESS → Trojan → node B | `TestRealTwoAgentVlessTrojanDeployment` (two agents through the panel) | `TestRealVlessToTrojanRelayWithIndependentUserAndRelayCredentials` |
| Block rule | `TestRealPolicyDomainAndCIDRBlockWithDefaultDirect` | `TestRealSingboxAccessRecordsRejectedAndAllowedConnections` |
| Per-user stats | `TestRealOnlineStatisticsTrackAnAuthenticatedClient`, `TestRealCumulativeUserCountersAndManagementPortIsolation` | node totals only (D5): `TestRealClashApiNodeTrafficTotalsCountForwardedBytes` |
| Access log → rollup | `TestRealXrayAccessEventsActualRuleAndPrivateContentExcluded`, `TestRealCoreAccessAccountAggregationPrivacyAndOfflineRestartDelivery` | `TestRealSingboxAccessRecordsRejectedAndAllowedConnections` |

**Criteria.**
- A7.1 met: captured-line parser tests (IPv6, UDP, rejected, no `email`, unknown trailing fields) and the aggregation test.
- A7.2 met, with a deviation from the text: the source is the core log, not Clash API polling, with the evidence above. The user is taken from the inbound line, or unknown.
- A7.3 met: deleted file list in `545d6e2`/`9b8fd52`/`849603e`. The remaining `observation` hits are generic names (enrollment, storage, replay inspection), not the fork path.
- A7.4 met: nullable bytes end to end, "—", 配置出口.
- A7.5 met: see the table.
- A7.6 met: the reviewer's clean-worktree gate at `e484fd2` and `7dc41b1` passed (build, vet, all tests, web build, gofmt). The full suite with both official binaries at `7dc41b1` passes except `TestRealMTLSCoreUpdate…/{xray,sing-box}-pre-switch-rejection` (Phase 8).

**Deviations.**
- The 7A commits carry the `Claude 5.5` trailer; the 7B commits are correct.
- The 7B Cline session was cancelled during its final test run, after all commits had landed. The reviewer reviewed from git and ran the full gate instead.

**Lines.** Non-test Go 58,890 → 56,703.

**Follow-ups.**
- Phase 12: the spool `access_replay_restore` / `replay_inspection` / `access_cache_comparison` files.
- Phase 14: document that the Xray access log lives under the agent runtime directory (transient, 20 MB cap).

### Phase 8: accepted 2026-10-17

**Process.** The phase ran in three handoffs:
- 8A: investigation only, no code. It mapped the flow, found the root cause, proposed a design and listed deletions.
- 8B: implementation under the reviewer's binding decisions.
- 8C: a review fix.

**Reviewer decisions.**
- **Variant A.** Cores use version + official GitHub asset URL + SHA-256. The signed catalogue (`release_catalogue`, `pkg/contracts/release` manifest/trust/catalogue, the signed `History.Accept`, `cmd/release`) stays only for agent self-update, which is Phase 12's.
- Redirects allowed: at most 3 hops, to `github.com`, `objects.githubusercontent.com` and `release-assets.githubusercontent.com`.
- After a crash or an unreadable hold marker, the install history decides a rollback at startup.
- Stuck tasks are closed when the node reconnects, or by an Owner `clear`.

**Commits.**

| Commit | Step |
|---|---|
| `e6fd2f1` | schema-2 `CoreUpdateRequest` inside the existing `CoreUpdateCommand.request_json` (no proto change); URL allow-list `release.CoreAssetURL` |
| `bbb2738` | `updater.CoreSource`: streamed archive SHA-256 checked before extraction; safe zip/tar.gz extraction (no traversal, links or duplicates; one candidate); size and entry caps |
| `26e856d` | image-based `Installer.ApplyImage`; version check through the adapter's `Detect` |
| `18ed582` | new agent worker; startup rollback repair (`reconcile/core_rollback_repair.go`). The core receipt/outcome/history-initialization/baseline-recovery journal is deleted, along with `release_hold_recovery.go`. |
| `2fa0541` | panel queue `{component, version, url, sha256}` with validation; `/clear`; reconnect closure; `/recover`, `/attempts` and generations removed. Migration `0107_core_update_direct_url.sql` (sha256 `35465d33…4cdc33`, pinned) drops `core_update_attempts` and rebuilds `core_update_tasks` with a backfill. Web form updated. The fork core-packaging block was removed from `package-development-build.ps1`. |
| `b60f856`, `29bb1fa` | backup inspection of pre-0107 archives; Windows staging suffix |
| `a87ff63` | 8C review fix. A backfilled legacy task (`url=''`, still queued) made `PendingCoreUpdate` fail validation, so `node/control.go` dropped the stream on every reconnect. It is now closed as `failed_closed/history_invalid`. Test: `TestCoreUpdateUndispatchableLegacyTaskIsClosed`. |

**Root cause of the old `pre-switch-rejection` failure.** The receipt-era history bootstrap ran before signature verification, and a damaged evidence directory made it fail with `failed_closed/history_invalid`. The test and the journal it exercised are deleted.

**Criteria.**
- A8.1 met: `TestCoreUpdateChecksumMismatchNeverActivates` (no install record, no image, staging removed, no activation).
- A8.2 met: `TestCoreUpdateVersionMismatchKeepsCurrentImage` and `TestCoreUpdateHealthFailureRollsBackToPreviousImage`.
- A8.3 met: `TestCoreUpdateQueueValidation`. The Owner supplies only component, version, URL and SHA-256.
- A8.4 met: the reviewer's clean-worktree gate at `29bb1fa` and `a87ff63`. The full suite with official Xray 26.3.27 and sing-box 1.14.3 at `29bb1fa` has **0 failures**, including `TestRealCoreDirectURLInstallProxies`.

**Deviations.**
- The real-core install test lives in `internal/agent/updater` (real reconciler, installer, httptest TLS archive and a proxied request). The panel queue, dispatch, clear and reconnect path is covered by `internal/panel/deployment` tests.
- The optional GitHub-digest button was not added.
- The 8B report claimed 54,197 non-test lines; the method gives 56,383.
- 8B left four scratch files in the repo root. The reviewer removed them; they were never committed.

**Lines.** Non-test Go 56,703 → 56,383 (−320). Most of the update machinery is shared with agent self-update and stays for Phase 12.

**Follow-ups.**
- Phase 12: agent self-update still uses the signed catalogue, `updater/{history,baseline_recovery,recovery_proof,download,stage}`, `agenthost`, `agentjobs` and `agentprocess`. Decide there whether it moves to URL + SHA-256 too, which would let the catalogue and most of `cmd/release` go.
- Phase 14: AGENTS.md §22/§23, `docs/v1-acceptance.md` and `docs/protocol-support-matrix.md` still describe the signed core catalogue and the receipt journal.

### Phase 12: accepted 2026-10-17

**Commits.**

| Commit | Step |
|---|---|
| `7b86043` | 12.1 spool: 3,755 → 1,398 non-test lines. It keeps queue, upload, ACK-delete, the 100 MB cap and eviction of the oldest allowed records first. Removed: ack history, replay inspection/heads/relations, the final-usage queue and replay (plus `pkg/contracts/traffic/{final,source}.go`), retained key, digest export, database candidate, cache comparison, inspection, the installation guard, 13 `agenthost` files, five `cmd/agent` inspection/recovery modes, and the panel replay-head validation with the node page's replay/diagnosis panels. Old stores open and drain: the dropped tables are ignored, and their names stay in the v1 checkpoint digest. The Phase 1 follow-up is done: the freshness guard treats `spool/spool.key` as a marker. |
| `b5ab6af`, `c308a2b` | 12.2 CLI: 23 → 13 flags (`config`, `check-config`, `version`, `storage-contract`, `storage-contract-version`, `control-only-contract`, `managed-worker`, `supervise`, `initialize-agent-history`, `enroll`, `token-file`, `renew-node-certificate`, `rotate-node-key`), pinned by `TestAgentFlagsAreExactlyTheSupportedSurface`. Deleted: the enrollment inspection/restore/publication modes, supervisor inspection, confirmed-image restore, legacy job migration, `updater/recovery_inspection.go` and `restore_confirmed_image.go`. |
| `e0b8128` | the `NewRuntime` seam moved from `platform/config` into the agent control package (Phase 11 follow-up) |
| `9438df2` | `updater/baseline_recovery.go` and `recovery_proof.go` stay. Tests prove they serve the authenticated agent-update refusal recovery (Phase 10 follow-up). |

**Criteria.**
- A12.1 met: `TestSpoolCapacityKeepsBlockedAndTrafficBeforeAllowed`, `TestSpoolOfflineQueueUploadsAndDeletesAfterAck`, `TestOlderSpoolFormatStillDrainsAfterUpgrade`, `TestOlderBoundSpoolVerifiesAndDrainsAfterUpgrade` and `TestDroppingOlderSpoolTablesKeepsTheCheckpointDigest`.
- A12.2 met: 13 flags, about 12 targeted. The agent self-update tests pass.
- A12.3 met: the reviewer's clean-worktree gate at `7b86043` and `9438df2` passed, and the full suite with official Xray and sing-box has 0 failures at both.

**Deviations.**
- The 12A and 12B Cline sessions were cancelled during their final test runs, after their commits had landed. The reviewer reviewed from git and ran the gates.
- The 12B commit trailers read `Claude Opus 5`.
- Regression found in review: removing `--enroll-only` left `--enroll` enrolling and then running the worker, while `deploy/linux/enroll.sh` expects enroll-and-exit and passes the removed flag. `renew-certificate.sh` still uses `--confirm-node-certificate`. Both are fixed first in Phase 13 (13.0).

**Lines.** Non-test Go 56,383 → 49,656.

**Follow-ups.**
- Phase 14: document the removed operator procedures (spool key restore, spool database candidate install, enrollment-result restore, confirmed agent image restore, supervisor inspection), and that an interrupted key rotation is finished by rerunning the command.
- Owner decision, not blocking: agent self-update still uses the signed catalogue (Ed25519 manifest via `cmd/release`). Moving it to URL + SHA-256 like cores would delete the catalogue and most of `cmd/release`.

### Phase 13: accepted 2026-10-17

**Commits.**

| Commit | Step |
|---|---|
| `b30b40a` | 13.0 Phase 12 regression fix: `--enroll` is a terminal mode. It accepts only `--config` and `--token-file`, enrolls and exits. Tests: `TestEnrollmentAcceptsOnlyConfigEnrollAndTokenFile`, `TestEnrollmentModeIsNotTheSupervisedOrManagedWorker`. |
| `aacf441` | 13.1 `deploy/linux` holds exactly `install.sh` (`--panel`/`--node`), `enroll.sh`, `uninstall.sh`, `panel-ca.sh`, `node-ca.sh` and `renew-certificate.sh` (17 scripts deleted). For a node, `install.sh` installs the official Xray or sing-box release from the GitHub allow-list with a verified SHA-256 (`--core-sha256` or the Xray `.dgst`). `panel-ca.sh`/`node-ca.sh` became local file operations for the Phase 11 manual CA rotation. `*.sh` are pinned LF in `.gitattributes`. |
| `6bffb75` | 13.2 `scripts/build-release.sh`: web build (`SKIP_NPM_CI=1` for offline use), `CGO_ENABLED=0` linux amd64/arm64 builds with `-X main.version`, one tar.gz per arch with the deploy scripts, units and examples, and `SHA256SUMS`. `generate-proto.sh` replaces the `.ps1`. Six Windows build/snapshot/packaging scripts deleted; `/dist/` ignored. |
| `27eddba` | 13.3 `.github/workflows/check.yml`: `check` job (web ci and build, `test:time`, `go vet`, Agent fixture, `go test -race`, panel and agent builds) and an optional `cores` job (`workflow_dispatch` plus weekly) with pinned Xray 26.3.27 and sing-box 1.14.3, SHA-256 checked from `.dgst` and the GitHub asset `digest`. |
| `4e55bf6` | checklist tick (implementer, rule 7) |

**Criteria.**
- A13.1 met: 6 scripts, all `bash -n` clean (reviewer re-ran it). shellcheck isn't installed.
- A13.2 met: the reviewer ran `SKIP_NPM_CI=1 bash scripts/build-release.sh` in the clean worktree and got `proxy-control-1.0.0-linux-{amd64,arm64}.tar.gz` and `SHA256SUMS`, with the expected layout.
- A13.3 met: the YAML parses, and the jobs mirror the gate. actionlint isn't installed; the workflow was read in full.
- A13.4 met: the reviewer's clean-worktree gate at `4e55bf6` passed.

**Not verified.** An end-to-end `install.sh` on Linux can't be run on Windows; it is part of the owner's Linux VM acceptance.

**Lines.** Non-test Go 49,656 → 49,671 (the enroll validation).

**Follow-ups.**
- Phase 14:
  - `deploy/examples/*.yaml` have mojibake comments and reference a missing `docs/signed-releases.md`.
  - `install.sh --config` is ignored when a config already exists.
  - Upgrading the bootstrap agent with `install.sh` doesn't change the supervised image under `releases/agent-installed`.
  - The archived docs still mention removed flags; leave them as history.
- Phase 15: the release archives aren't byte-reproducible (tar timestamps). Optional.

### Phase 14: accepted 2026-10-17

**Commits.**
- Docs: `c9ae940` (index, architecture), `485cd73` (deployment), `7b8e98f` (operations), `d04d8c1` (protocol and acceptance matrices).
- `a58c29f`: AGENTS.md V1.1, with a change log covering D1–D5.
- `e4ac591`: root README quick start.
- Accuracy fixes: `fa9e71a`, `49f72c2`, `3610d06`, `522c683`, `7ed9b81`.
- Checklist entries: `3ecb7ff`, `cf3b06f`, `8de1a11`.

**Criteria.**
- A14.1 met: `docs/README.md` 23 lines, `architecture.md` 119, `deployment.md` 181, `operations.md` 183, `protocol-support-matrix.md` 90, `v1-acceptance.md` 84.
- A14.2 met: the header, §9.1, §15, §20, §21, §22 (with new §22.1 automatic quota revisions and §22.2 core update flow), §23, §30, §32 stage 4 and §34 are updated; the change log covers D1–D5; "第 5.5 步" is replaced.
- A14.3 met: the link check (`qui-scratch/p14/link-check.sh`, 45 relative links in 23 files) finds 0 missing. The reviewer re-ran it.
- A14.4 met: no Go file changed; build, vet, full tests and the web build passed.

The commands in `deployment.md` and `operations.md` were compared flag by flag against `cmd/panel -h` (7 flags), `cmd/agent -h` (13 flags) and each script's `--help`.

**Code-vs-plan disagreements.** The code wins; the docs record each one.
1. Telemetry is reported every 15 s (`node.HeartbeatInterval`), sampled every 5 s, not every 30–60 s.
2. The Phase 11 CA-rotation draft step "disable, then re-enroll the same node" can't work. Tokens need `nodes.enabled=1`, and `node_identities.node_id` is a primary key with no delete path. `operations.md` §4 documents the working procedure: the node comes back as a new node record.
3. `deploy/linux/panel-ca.sh` requires a root-owned identity directory, but a packaged install owns it as `proxy-panel`. Its usage text repeats the unworkable draft. → Phase 15.
4. The `deploy/examples` "mojibake" was a false alarm: the files are valid UTF-8. Only the missing `signed-releases.md` link was real, and it is fixed.
5. Logs go to journald (stdout), not `/var/log/proxy-panel/panel.log`. AGENTS.md §30 notes this.
6. The dashboard's connection wording ("TCP 会话与 UDP 关联") is stale for Xray's distinct-IP counts. → Phase 15.

### Phase 15: accepted 2026-10-17 (plan complete)

**Commits.**

| Commit | Step |
|---|---|
| `a8007ee` | 15.1 read-only SQLite pool (WAL already on; `mode=ro`, `query_only`, 4 connections, no idle connections). It serves the auth session lookup (now one joined statement), the whole `/sub` read path, and the admin, subscription-token and operation lists. Writes stay on the single writer. The subscription last-fetched marker is best effort, capped at 250 ms. |
| `0b431d9` | 15.2 in-memory first-run and base-setting state, refreshed on setup and settings changes and invalidated on refresh failure |
| `0874ee3` | 15.3 IP/CIDR rules match literal IPs only (`RoutingRuleEditorPage.vue`, `ServiceRulePage.vue`) |
| `29274b4` | dashboard and node page connection wording matches the measured semantics (Phase 14 follow-up) |
| `c421cfa` | `panel-ca.sh` accepts the packaged `proxy-panel` ownership (symlink, mode and owner checks kept); the usage text states the new-node-record procedure (Phase 14 follow-up) |
| `a497899` | 15.5 toolchain section in `docs/deployment.md`; nothing depends on `.tools/` |
| `83177b8` | reproducible release archives (`tar --sort=name --mtime=<commit time> --owner=0 --group=0 --numeric-owner`, `gzip -n`); two runs give identical `SHA256SUMS` |
| `10efa86` | checklist tick (implementer, rule 7) |

**Criteria.**
- A15.1 met: `TestReadsAndSubscriptionsAnswerWhileTheWriterIsBusy` holds a write transaction. The authenticated list, the session request and `/sub` answer in 3 ms, 1 ms and 257 ms (the 250 ms is the marker cap). Also `TestReadPoolServesQueriesWhileTheWriterIsBusy` and `TestReadPoolDoesNotBlockOnTheSingleWriterConnection`.
- A15.2 met: `TestAuthenticatedRequestQueryCount` uses a counting `driver.Connector`. An authenticated session request is 1 statement, down from 4. A protected list is 3 (1 auth plus the endpoint's count and page queries, asserted ≤3).
- A15.3 met.
- A15.4 met: the reviewer's clean-worktree gate at `10efa86` (build, vet, all tests, web build, gofmt, `bash -n`) passed. The full suite with official Xray 26.3.27 and sing-box 1.14.3 has 0 failures.

**Skipped on purpose.** 15.4 (Xray HandlerService add/remove user). There is no HandlerService client. Adding one needs new proto and generated code (protoc isn't available), per-protocol paths and an apply/rollback branch, which is well over the ~200-line limit. Quota changes keep using a redeploy.

**Lines.** Non-test Go 49,671 → 49,903.

### Definition of done (section 8), evaluated 2026-10-17

| Item | Verdict | Evidence |
|---|---|---|
| Gate passes; core integration tests pass with official Xray (and sing-box) on Linux amd64 | Verified on Windows amd64; Linux amd64 pending (owner) | The reviewer's full suite with official Xray 26.3.27 and sing-box 1.14.3 has 0 failures at `10efa86`. On Linux, run the CI `cores` job (`workflow_dispatch`) or the owner's VM. |
| No forked core, no `integrations/`, no core-level connection admission or byte quotas | Met | Phases 4 and 6. `integrations/` is gone. The only `proxycontrol` hits are version-suffix tolerance, golden negative assertions and digest salts. |
| Per-user quota auto-disables and re-enables through normal revisions | Met | Phase 5 tests, `TestCoreFlow…` |
| Soft connection limit visible, or "unknown" | Met | Phase 5 and 6.3 (Xray `GetStatsOnline`; sing-box unknown under D5); the UI wording was fixed in Phase 15 |
| Restoring a pre-refactor backup succeeds | Met | `TestPreRefactorBackupRestores` passes after migrations 0102–0107 |
| Non-test Go roughly halved (under 70k, from 131.8k) | Met | 49,903 (−62%) |
| Panel and agent CLIs ~10 flags each | Met | panel 7, agent 13 |
| One install script per role | Met | `deploy/linux/install.sh --panel/--node` plus 5 helpers (from 23) |
| Real multi-node acceptance (AGENTS.md §33.2 on Linux VMs) | **Owner step**, not claimed by this plan | `docs/v1-acceptance.md` marks the VM items |

**Open items for the owner.**
1. Linux VM acceptance (AGENTS.md §33.2), including an end-to-end `install.sh` and the CI `cores` job on Linux.
2. Optional decision: move agent self-update from the signed catalogue (`cmd/release`, Ed25519) to URL + SHA-256 like core updates. That would delete the catalogue and most of `cmd/release`.
3. Known residue, not blocking: the dead proto fields stay until `protoc` is available (`scripts/generate-proto.sh`); the sing-box `TestRealHTTPDirectBlockRollbackAndReconcilerRestart` can flake on Windows port reuse.

## 8. Definition of done

- The gate passes. Core integration tests pass with official Xray (and sing-box where it's used) on Linux amd64.
- No forked core, no `integrations/`, and no core-level connection admission or byte quotas.
- The per-user quota auto-disables a user and re-enables them through normal revisions.
- The soft connection limit is visible in the UI, or shows "unknown".
- Restoring a pre-refactor backup succeeds.
- Non-test Go is roughly halved (target: under 70k lines, from 131.8k).
- The panel and agent CLIs have about 10 flags each.
- There's one install script per role.
- Real multi-node acceptance (AGENTS.md §33.2 on Linux VMs) is a separate owner-run step and isn't claimed by this plan.

---

## 9. After the plan (2026-10-17 to 2026-10-21)

Phase 15 closed the plan on 2026-10-17. The items below landed afterwards. They are listed here so this file stays the working entry point, but they are **not** part of the 15 phases and none of them carries Linux evidence yet.

| Item | Migration / artifact | Where it is documented |
|---|---|---|
| Node outbound network: core outbound source IP or interface, plus a server-wide enforced source address handled by a `CAP_NET_ADMIN` oneshot helper (nft table `inet proxy_agent_source`) | `migrations/0108_node_outbound_network.sql`; `internal/agent/hostnet`; `deploy/systemd/proxy-agent-host-network.service` and `.path` | AGENTS.md §11.3 and §30.2; architecture.md §4, §6, §7; deployment.md §4, §7; operations.md §9; protocol-support-matrix.md §2 |
| Root terminal menu `qui` on the panel host (change Owner credentials, change the Web port, restart/start/stop, update); `qui-update` on both hosts (GitHub Release + `SHA256SUMS`, database backup before a panel upgrade) | `deploy/linux/qui.sh` → `/usr/local/bin/qui`; `deploy/linux/update.sh` → `/usr/local/bin/qui-update` | AGENTS.md §30.1; README quick start; deployment.md §3, §5; operations.md §14 |
| One-line node installer and the unauthenticated CA endpoint it relies on | `deploy/linux/node-install.sh`; `GET /install/ca.pem` | AGENTS.md §11.3 and §27.4; deployment.md §4 |
| CI `release` job and the v1.0.0 publication | `.github/workflows/check.yml` (`release`, only on `refs/tags/v*`); tag `v1.0.0` at `69be6d6` | README; deployment.md §5; v1-acceptance.md §2 |
| iOS design system applied to the whole web UI; Owner can cancel an unused node registration | `web/src/styles/main.css`, `web/tailwind.config.js`, `web/src/components/*`, `internal/panel/node/deletion.go` | design-system.md; AGENTS.md §11.3 |
| Logical deletion of registered nodes (blockers → 409 `node_delete_blocked`, certificate revoked, history kept, `GET /api/v1/nodes?deleted=1` and the deletion preview) | `migrations/0109_node_deletion.sql`; `internal/panel/node/deletion.go`; `internal/panel/deployment/node_tasks.go` | AGENTS.md §11.3 and §27.4; architecture.md §3, §6; operations.md §15; v1-acceptance.md §5 |
| Cleanup: dead Go code, unused web components (`AppLayout`, `FilterBar`, `NodeBadge`, `NodePath`), `web/src/mocks`, the traffic budget-retirement and recovery-execution contracts, two old audit reports | `2aaadd6` | — |

`v1.0.0` (tag at `69be6d6`) contains the first four rows. The iOS design system, cancelling unused registrations, logical node deletion and the cleanup came after the tag and are not in any release yet.

**Still open.**

1. **Linux VM acceptance.** The whole of `docs/v1-acceptance.md` is unexecuted, including the post-plan items in its §5; the verdict stays "V1 未完成" even though v1.0.0 is published.
2. **Optional:** move Agent self-update from the signed catalogue (Ed25519, `cmd/release`) to URL + SHA-256 like core updates, which would delete most of `cmd/release` (see the open items after §7).
3. **Known residue:** the dead `pkg/contracts/agentpb` fields stay until `protoc` is available (`scripts/generate-proto.sh`).
4. **Windows-only flaky tests:** reconcile `TestSyncHostNetworkClearsTheTableWhenNothingIsConfigured` and sing-box `TestRealHTTPDirectBlockRollbackAndReconcilerRestart`.
5. Gate commands for later changes: `gofmt -l` on the changed Go files, `go build ./...`, `go vet ./...`, `go test -count=1 ./...`, `npm --prefix web run build`, `npm run test:time`.
