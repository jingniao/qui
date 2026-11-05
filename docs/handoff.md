# Handoff: Outstanding Work and Open Decisions

Prepared 2026-11-05 (America/Los_Angeles) for the next agent. This is the single list of what is unfinished and what still needs the Owner's confirmation. Mark items done here as you close them, or delete this file once everything is closed.

## 1. Where Things Stand

- **Repository:** `C:\Users\luanf\qui`, branch `feat/v1.4-tunnels`.
  - Everything below is **committed locally only; nothing has been pushed.**
  - `main` = `origin/main` = `a15da45` (v1.3.1 plus docs).
  - The last released version is **v1.3.1** (tag → `ebe8301`). All three hosts run it, and their migration head is 0128.
- **Locally reviewed work (integration worktree `compat-integration`, baseline `3e449e7`):**
  - **v1.4 tunnels:** tunnel management (Tunnels/Rules pages), "Add Relay" on nodes, relayed subscription entries, the canvas narrowed to proxy relaying, and migration of old canvas forwards. Migrations 0129–0131. Commits `d4fad8c` … `8134314`.
  - **Protocol compatibility:** S1–S4 and M1–M4 locally integrated, migration 0133; final Linux/browser acceptance pending (§4.4).
  - **Client policy profile library:** server-side profiles, activation, endpoint members, keyword filters, `emptyPolicy`, draft preview, and rule-set references. Migration 0132. Commits `258c1c7` … `1bbb836`, plus docs `aff8153` and `49358db`.
- **Specs and plans to read first:**
  - `AGENTS.md`: the product spec, in Chinese. It is the authoritative description of behavior.
  - `docs/v1.4-plan.md`: the v1.4 plan. Read §1–§5, and the A1 section for its status.
  - `docs/release-notes-1.4.0.md`: the release-notes draft with the acceptance table.
  - `web/docs/client-policy-backend-plan.en.md`: the client-policy plan. §13 holds the review decisions and §14 the status and remaining items.
  - `web/docs/client-policy-change-log.md`: a per-iteration change log, appended to for every client-policy change.
  - `docs/subscription-import-compatibility-plan.en.md`: the protocol-support plan, now part of v1.4.0. §14 holds the review and Owner decisions, which override earlier sections. See §4.4.

## 2. How to Work in This Repo

- **Toolchain on Windows:** `.tools/go/bin/go.exe` with `GOPATH=.tools/gopath`, `GOCACHE=.tools/gocache` and `GOTOOLCHAIN=local`. For the web app: `npm.cmd --prefix web run typecheck|build`, and the selftests in `web/scripts/*.mjs`.
- **Linux gate (one final integrated run under the latest user instruction):** the local test machine at 192.168.5.36 runs `scripts/check.sh` plus the real-core suite on one commit.
  - Start it with `sh ~/.qui-local/gate.sh <commit>`.
  - Check it with `sh ~/.qui-local/gate-status.sh <shortsha>`.
  - It takes about 40 minutes and gates a *commit*, so commit locally first. Run only one gate at a time, because they share ports.
  - Remote access goes through `~/.qui-local/r.sh`. **Never copy the machine's credentials into docs, commits or messages.**
- **Rules the Owner set:**
  - Do not push, tag, publish a pre-release or touch the production hosts without the Owner's explicit approval for that specific step.
  - Keep the Owner's existing page layouts. Ask before replacing one.
  - No duplicated logic and no defensive code: validate at the API boundary and reuse existing helpers.
  - Never edit applied migrations; add the next number instead.
  - Report in plain language. Say plainly what was and wasn't verified, and don't present a Windows test run as Linux or host evidence.
- **Tracked plan files:** the three `docs/subscription-import-compatibility-*.md` files are the protocol-support plan (§4.4). They have been tracked since `84e8725`, so they belong to the normal document tree rather than to local scratch work.

## 3. Needs the Owner's Confirmation

| # | Question | Context and current proposal |
| --- | --- | --- |
| C1 | **Approve pushing `feat/v1.4-tunnels` and publishing a 1.4.0 RC pre-release?** | Nothing is pushed. GitHub `check` must pass on the same SHA before a `v*` tag (see `docs/deployment.md` §2). Since 2026-11-05 the RC also waits for the protocol-support work (§4.4), so don't ask for C1 until both of its tracks pass S5. |
| C2 | **Host role mapping for the RC.** | Proposal: us2-node = US destination + panel, uk-node = HK entry. The JP relay hop needs a third GOST-capable linux/amd64 host, because alpine-node (Alpine LXC) cannot run GOST. Without one, the RC tests only the two-server tunnel (entry → exit). |
| C3 | **Client policy: should the preview let the Owner pick a user?** | The preview currently always auto-selects a user and labels the choice as automatic. The API already accepts `userId`; only the UI is missing. (Plan §14.2 item 1.) |
| C4 | **Client policy: batch input when editing a rule.** | Pasting several domains creates multiple rules only when *creating* a rule; editing an existing rule keeps it as one rule. Confirm this, then document it. (§14.2 item 2.) |
| C5 | **Client policy: disabled rule sets.** | Saving a profile is refused when an *enabled* RULE-SET rule points at a *disabled* service set. So disabling a set the active profile uses blocks re-saving that profile until the rule is disabled or the set is re-enabled. Keep this, or relax it to a warning? (§14.2 item 3.) |
| C6 | ~~Protocol-support plan decisions R1–R4~~ **Decided 2026-11-05.** | Plan `.en.md` §14.3–§14.7:<br>• both tracks run in parallel;<br>• the refresh fix ships in v1.4.0;<br>• duplicate `name\|protocol` keeps the first and rejects the rest;<br>• everything ships in v1.4.0.<br>The implementation is locally integrated (§4.4). S0 and original M1 gates passed; S1 checksum correction is running; final integrated verification and browser checks remain pending. |

## 4. Outstanding Tasks

### 4.1 Verification in Progress or Not Yet Run

1. **Linux gates for the client-policy phases.**
   - Phase A `42491ab`: **passed**.
   - Phases B+C `6cfc6c5`: **passed** (check 1960 s, cores 558 s, total 2518 s).
   - Final commit `1bbb836` (covers B–D and the mihomo case): queued after `6cfc6c5`.
   - Check with `gate-status.sh 6cfc6c5` and `gate-status.sh 1bbb836`. If either is missing or was killed, rerun `gate.sh 1bbb836`.
   - Then record the result in client-policy plan §14.1 item 1 and in the release-notes row.
   - The later commits `aff8153` and `49358db` change docs only.
2. **Real mihomo routing behavior for the client policy** (plan §11 item 10). Today mihomo only checks that the config loads (`mihomo -t`, in `format/clash/clash_test.go`). Extend `TestMihomoRoutesProxyDirectAndReject` in `format/clash/routing_test.go` so that traffic actually goes through a purpose group → region group → endpoint, and so that an empty group with `emptyPolicy: reject` blocks.
3. **Manual client-policy scenarios** (plan §11 items 1, 2 and 13), which so far only Go tests cover:
   - two browsers seeing the same activation
   - concurrent edits keeping the draft
   - repeating the localStorage import
   - restoring a backup from before 0132 and one that includes it
4. **Logged-in browser walkthrough of the client-policy page**, covering:
   - profile cards: create, edit, activate and delete
   - 409 conflict handling
   - import preview
   - region-group keywords and "Add selected endpoints"
   - "Resolve members" and the YAML preview context
   - the filtered "Edit rules" view
   - rule-set references and delete blocking

### 4.2 v1.4 Acceptance and Release (A1), Blocked on C1, C2 and the §4.4 Work

A1 must also cover the protocol-support work once it lands (plan `.en.md` §14.7). That means real clients using imported SS2022, Reality and xhttp entries, plus a self-hosted Xray Reality/xhttp node and its relayed subscription entries.

5. RC deployment on the hosts (after C1 and C2), following `docs/deployment.md`: panel first, then each Agent. Back up the database and the identity directory first.
6. P1–P4 with real clients (mihomo or sing-box) on the hosts. Check the subscription address, port and credentials, traffic, publish results, that republishing changes nothing, and recovery after a node restart. Record the results in `docs/v1-acceptance.md` §13.
7. A1 combined checks: the one-time migration and its report, backup and restore, credential rotation, deletion, and republishing with no changes.
8. A formal browser walkthrough of the v1.4 pages: Tunnels, Rules, Add Relay and the canvas.
9. Fill in the acceptance table in `docs/release-notes-1.4.0.md`. Only the A1a row is filled so far: gate `8134314` passed, with every P1–P4 sub-case passing and none skipped.
10. Final release: tag the release, deploy to the hosts and update `AGENTS.md` "当前状态" (current status). This needs separate approval.

### 4.3 Documentation Still Open

11. `docs/architecture.md` and `docs/protocol-support-matrix.md` do not describe the client-policy profile library, `@endpoint` members, keyword filters or `emptyPolicy`.
12. `docs/v1-acceptance.md` has no entries yet for the client-policy items in §4.1.
13. After C3–C5 are decided, update `AGENTS.md` (the client-policy paragraph in §10.3) and plan §14.

### 4.4 Protocol Support: Integrated, Final Verification Pending

14. S1–S4 and M1–M4 are locally integrated in `compat-integration` at `3e449e7`, migration head 0133. Imports/codecs, self-hosted renderers, public subscription mapping and editor are implemented; see the protocol-support plan §14.8 and phase reports for precise combinations and omissions.

    S0 and original M1 gates passed. S1 `ccf23e4` failed solely because 0133 was absent from the migration checksum map (check 1972 s, cores 560 s, total 2533 s, END `2026-11-05T15:01:31Z`); correction `4ded28f` has a gate running, not a passing result. Corrections/new phases have focused Windows evidence, including native/authenticated tests, rather than Linux acceptance.

    The latest user instruction replaces repeated per-phase gates with focused checks and **one final integrated Linux gate**. Keep verification lean; do not add the broad S5 test expansion previously proposed. Still complete the focused configured real-core regressions and browser walkthrough of source diagnostics, format previews and the node editor before claiming S5. No real-source fetch or production action is implied. C1/A1 remain later release work.

    Duplicate `name|protocol` keys keep the first. User-inbound Reality/xhttp settings reuse one public client model; private keys stay encrypted/masked, rotation is explicit, and clients update subscriptions after successful publication. Internal proxy-relay creation stays TCP/TLS. Detailed format/core restrictions are in `docs/protocol-support-matrix.md`.

## 5. Known Limitations (Accepted; No Action Unless the Owner Asks)

- **Client policy:**
  - A profile save and a concurrent rule-set delete can race. A preview warning covers the effect.
  - The page fetches each profile separately (at most 50).
  - A preview with no eligible user does not validate the requested format.
  - Each subscription render that includes panel rule sets looks up whether each set is enabled.
- **v1.4 tunnels:**
  - GOST runs only on linux/amd64.
  - Behind a relay, the destination's device limit sees the relay address, so all users behind one relay count as one source.
  - Relay health means only that the process and listener are up, not that the destination is reachable.
  - These are documented in `AGENTS.md` §9.2 and §23.
