# User-level access plan (draft, 2026-10-26)

> **Superseded** by [v1.1-plan.md](v1.1-plan.md) (2026-10-26). Kept for history only; do not implement from this file.

Status: draft for Owner review. Nothing here is implemented yet.

## 1. Decisions (Owner, 2026-10-26)

| ID | Decision | Replaces |
| --- | --- | --- |
| U1 | Access is managed per **user** only. Users and admins no longer see devices: one subscription link and one set of credentials per user, shared by all of that user's clients. No client or device identification (no HWID, no per-device links). | AGENTS.md §9.2, §9.3 (device part), §10 per-device tokens |
| U2 | sing-box is **built by us from unmodified official source**, adding only the upstream `with_v2ray_api` build tag, so sing-box nodes report exact per-user traffic. Official sing-box binaries remain supported and fall back to today's D5 behavior. | D1 (sing-box part), D5 |
| U3 | End users get **their own login** (username + password) to a self-service portal. Subscription tokens are never used as a login. | AGENTS.md §6 (adds a third principal kind, not an admin role) |

Unchanged: Owner/Viewer roles, node groups, the quota state machine and automatic revisions (§9.1, §22.1), Xray as the primary core, official Xray binaries (D1 for Xray).

## 2. Design summary

### 2.1 Devices become an internal detail (U1)

The device ID is part of the Agent wire format: `desiredstate.Credential.DeviceID`, Xray stats accounts, sing-box render checks (`render.go:247` rejects user credentials without a device ID), collector accounts, policy scopes, traffic counters, and credential tombstones. Removing it from the wire format would require a lockstep panel and Agent upgrade, and touches about 40 Go files and 4 tables.

**Approach.** Every user keeps exactly **one hidden access identity**: a `devices` row created and owned by the user module. It never appears in the UI or the API, and nothing besides the user module creates or deletes it. Its ID keeps flowing to the Agents unchanged, so:
- Agents 1.0.9 keep working with a 1.1 panel. The panel can be upgraded first, as `qui-update` does today.
- Traffic accounting, tombstones, the quota deployer and access-record collection need no change.

Product-visible changes:
- `max_devices` is retired. The column stays (its CHECK needs a value ≥ 1, so it is set to 1), but it is hidden from the API and UI and no longer validated beyond that.
- The device APIs are removed: `GET/POST /users/{id}/devices`, `DELETE /devices/{id}`, and `deviceId` in the subscription-token request.
- New: `POST /users/{id}/credentials/reset` re-issues all of the user's credentials and publishes the change through the existing automatic revision. This is the "my link leaked" button.
- The device scope in policies and in the simulator is removed. Existing rules are rewritten to the device's user (equivalent, since a user now has a single identity).
- Access records, the dashboard and user pages drop the device column and filters.
- `max_connections` (a soft limit: distinct client IPs on Xray) stays, and is the only remaining signal of how many clients a user has online.

**WireGuard caveat.** WireGuard credentials are a key pair plus an address. Two clients using the same peer key at once make the peer endpoint flap, and both connections break. With one identity per user, a WireGuard user inbound works for **one client at a time**. Default in this plan: WireGuard stays allowed for users, and the portal and admin user page say "WireGuard: one device at a time". Relay WireGuard is unaffected. (Open decision O1.)

### 2.2 Self-built sing-box (U2)

- Build from a pinned upstream tag and commit, with `CGO_ENABLED=0`, `-trimpath`, upstream `release/LDFLAGS`, and the tags `release/DEFAULT_BUILD_TAGS_OTHERS` + `with_v2ray_api`. No patches: the build script fails if the checkout is dirty.
- A static binary runs on both glibc (Debian) and musl (Alpine), so each architecture needs one asset instead of the separate glibc and musl assets the official release ships.
- The assets are published **in the qui GitHub release itself** (`sing-box-<ver>-qui.<n>-linux-<arch>.tar.gz`, same archive layout as upstream) and covered by the release `SHA256SUMS`. Installers verify them like the panel and Agent archives, so `--core-sha256` is no longer needed for sing-box.
- The Agent already parses `Tags:` from `sing-box version`. When `with_v2ray_api` is present, it renders `experimental.v2ray_api` on a random loopback port with `stats.users` set to the user credential accounts, and reads per-user counters with reset. The Clash API stays for node totals and connection counts. Per-user **online counts** stay "unknown" on sing-box (the v2ray API has no online stats).
- On official binaries (no tag), behavior stays exactly as under D5.

### 2.3 End-user accounts (U3)

- A **third principal kind**, separate from admins: the end-user login identity is the proxy user (`users.username`).
  - The `admins` table, its `Require` middleware and the Owner/Viewer roles are untouched.
  - End-user sessions live in their own table under their own cookie, and can't pass the admin middleware by construction.
- Login is allowed for disabled, expired or over-quota users, so they can see why they have no access. The subscription content follows the user's status as it does today. The Owner can turn login off per user.
- The portal is read-mostly:
  - status, quota and expiry;
  - daily/hourly traffic;
  - granted nodes (name, region, online);
  - the subscription link (copy, QR code, reset);
  - change password.
- No access records in the portal (§3.6 minimal audit; can be added later behind an Owner setting).
- **Subscription link storage changes.** Today only the token hash is stored and the plaintext is shown once (§9.3). A portal user must be able to copy their link at any time, so the token is also stored **encrypted with the existing vault**, alongside the hash used for lookup. (Open decision O2.)

## 3. Phases

The working rules of `lightweight-refactor-plan.md` §3 apply:
- Cline implements; one commit per step.
- Never edit existing migrations, `go.mod` or `go.sum`.
- Gate: `go build ./...`, `go vet ./...`, `go test -p 1 ./...`, web build + type check, shell syntax checks; `-race` builds via `.tools/zcc-linux.cmd` for changed packages with concurrency.
- Commit, push and release only on the Owner's request.

Proposed releases:
- **v1.1.0** = phases 1–3: schema change. A downgrade needs the `pre-update` database.
- **v1.2.0** = phases 4–6: Agent change and new core assets.

### Phase 0 — Stop-condition probes (no product code)

1. sing-box:
   - Build the current stable tag as in 2.2 locally (Windows cross-compile to linux/amd64 and linux/arm64).
   - Confirm:
     - (a) `sing-box version` lists `with_v2ray_api`;
     - (b) the stats gRPC service and method names, and whether `proto/xray_stats.proto` can be reused or a sibling proto is needed;
     - (c) `QueryStats` with `reset` and the name pattern `user>>>NAME>>>traffic>>>{uplink,downlink}`;
     - (d) which inbound types attribute users (vless, vmess, trojan, shadowsocks multi-user, socks, http, hysteria2, tuic; WireGuard expected **not**);
     - (e) the binary runs on uk-node (Debian) and alpine-node (Alpine LXC) — copy to a scratch directory only, no service changes;
     - (f) binary size.
2. Data audit on the test panel (read-only copy via `VACUUM INTO`): users with more than one device, policies with a device scope, WireGuard user inbounds.

**Stop and report** if (a)–(c) fail or (e) fails on either host.

Acceptance:
- P0.1 A findings note in this file (section 6) with commands and outputs. Not containing tokens or keys.
- P0.2 The Owner confirms or revises O1–O3.

### Phase 1 — User-level identity (panel only)

Changes:
1. Migration `0112_user_level_access.sql` (new file):
   - Pick each user's primary device: the device of the active subscription token; else the oldest active device; else the oldest device.
   - Write `credential_tombstones` for every credential of the other devices, then delete those devices (credentials and WireGuard addresses cascade).
   - Rewrite policy `scope_json.device_ids` to the owning user IDs (deduplicated) and drop the key.
   - Set `max_devices = 1`.
2. User module:
   - `EnsureAccessIdentity(userID)`: idempotent; creates the hidden device and one credential per allowed protocol, and revokes credentials for protocols no longer allowed.
   - Called on user create/update, at panel startup, and after a backup restore.
   - Changes queue the existing automatic revision. The reason strings are new: `auto: user <name> credentials reset` and `... protocols changed`.
3. Subscription service:
   - `Rotate(userID)` / `Endpoints(userID)` resolve the hidden identity.
   - Existing per-device token hashes for the primary device keep working, so the current links survive the upgrade.
4. Remove the device API endpoints, device policy scope and simulator device field. Add `POST /users/{id}/credentials/reset`.
5. Web (admin):
   - Remove the device pages, columns and filters.
   - The subscription page works per user: one row per user, generate/reset, copy.
   - The user form loses max devices and gains "reset credentials".
6. Backup restore: restoring a pre-0112 backup migrates it the same way. Covered by the existing `TestPreRefactorBackupRestores`-style test with a multi-device fixture.

Acceptance:
- A1.1 Unit tests:
  - the migration on a fixture with 0/1/3 devices per user, device-scoped policies and WireGuard addresses;
  - `EnsureAccessIdentity` idempotency;
  - a protocol change adds and revokes the right credentials.
- A1.2 After startup on a migrated DB, the startup reconcile creates exactly one automatic revision per node whose running config still carries removed credentials, and none for nodes already consistent.
- A1.3 Traffic for a removed device credential arriving after the upgrade is charged to the user, via tombstones; the batch is not rejected.
- A1.4 A subscription link issued before the upgrade (primary device) still returns content; links of removed devices return 404.
- A1.5 Mixed versions: panel 1.1 with Agents 1.0.9 publishes, rolls back and reports per-user Xray traffic (integration test plus a field test in phase 3).
- A1.6 `grep` for device routes and fields in `web/src` and the API handlers finds only the hidden-identity code and tests.

### Phase 2 — End-user accounts and portal API

Changes:
1. Migration `0113_end_user_login.sql`:
   - `users.password_hash`, `users.login_enabled` (default 0), `users.last_login_at`;
   - new `user_sessions(token_hash PK, user_id FK users ON DELETE CASCADE, expires_at, created_at)`;
   - `subscription_tokens.encrypted_token` (nullable; filled on the next generate/reset).
2. New module `internal/panel/portal`:
   - Its own handler, cookie (`qui_portal_session`), middleware and repository for `user_sessions`.
   - Reuses `auth/password.go` hashing, the `login_attempts` IP limiter, the same-origin CSRF check and the session timeout settings.
   - It reads user, traffic, node and subscription data only through those modules' services (§24 rule 2).
3. Endpoints under `/api/v1/portal/` (all end-user-only):
   - `POST auth/login`, `POST auth/logout`, `GET auth/session`, `PUT auth/password`;
   - `GET me` (status, quota, used, expiry, protocols);
   - `GET traffic?range=`;
   - `GET nodes` (granted, subscription-visible; name, region, online);
   - `GET subscription` (decrypted link or "none yet"), `POST subscription/reset`.
4. Admin side:
   - `PUT /users/{id}/login` (enable/disable, set or reset password; Owner only, audited);
   - `DELETE /users/{id}/sessions`.
   - Disabling login, resetting the password or deleting the user revokes that user's sessions.
5. Token storage: generate/reset stores the vault-encrypted plaintext. Tokens created before 0113 can't be re-shown until reset; the portal says so.

Acceptance:
- A2.1 An end-user session is rejected by every `/api/v1` admin route; an admin session is rejected by every `/api/v1/portal` route. The tests iterate the registered mux routes, so no route is forgotten.
- A2.2 Login rate limit, CSRF and session expiry behave like the admin login (shared tests).
- A2.3 A disabled, expired or over-quota user can log in and sees the reason; the subscription content follows the status exactly as today.
- A2.4 Disabling login, resetting the password or deleting the user ends existing portal sessions immediately.
- A2.5 The portal never returns another user's data. Tests use two users, with IDs swapped in every parameter.
- A2.6 Backups include the new columns; restoring a backup preserves the end-user logins; the backup verifier accepts the new tables.

### Phase 3 — Portal web UI, docs, v1.1.0 field test

Changes:
1. Portal as a separate layout and router branch:
   - `/portal/login` and `/portal/*` with its own guard, built on the in-progress `ClientAccessPage.vue` design.
   - The page switches to `/api/v1/portal` and becomes user-centric (no "我的设备" device list, no "添加设备").
   - `qrcode.ts` is reused for the link QR code.
   - `viewMode.ts` and the Viewer redirect to the portal are dropped: Viewers are admins and stay in the admin UI.
2. Admin user page: login switch, set/reset password, reset credentials, subscription link (copy/reset).
3. Docs:
   - AGENTS.md V1.2 change log: §6, §7 menu, §9.1–9.3, §10, §26, §27, §29.1.
   - `architecture.md`, `operations.md` (portal, password reset), `v1-acceptance.md` new section.

Acceptance:
- A3.1 Web build and type check pass; no admin API is called from portal pages (grep).
- A3.2 Field test on the three hosts, after Owner approval of the v1.1.0 release:
  - upgrade with `qui-update` (panel first);
  - the existing alice link keeps working;
  - one shared link on two clients at once (Xray VLESS and Trojan via uk-node; chain via alpine): both work and traffic is charged to alice;
  - credential reset cuts the old link within one automatic revision;
  - quota exceed, disable and expiry cut access on all granted nodes;
  - portal login, link copy and password change as an end user;
  - a Viewer can't reach the portal and an end user can't reach the admin API (curl);
  - restore of backup B3 (pre-0112) on the test panel migrates cleanly;
  - coexisting services untouched.

### Phase 4 — sing-box build pipeline and installers

Changes:
1. `scripts/build-singbox.sh`:
   - Pinned `SINGBOX_VERSION`, upstream commit SHA, and Go toolchain version.
   - Clones the tag and verifies the commit, then asserts `git status --porcelain` is empty.
   - Builds amd64 and arm64 as in 2.2 and packs the upstream layout (`sing-box-<ver>-linux-<arch>/sing-box`).
   - Writes `sing-box-<ver>-qui.<n>.provenance.txt` (tag, commit, tags, Go version, ldflags).
2. The release job in `.github/workflows/check.yml` runs it before `build-release.sh`; the assets and provenance file go into the release and `SHA256SUMS`. The `cores` CI job tests against the self-built binary as well as the official one.
3. `install.sh` / `node-install.sh`:
   - `--core sing-box` defaults to the qui-built asset from the same release, verified against `SHA256SUMS`, one asset per architecture for both libc.
   - `--core-source official` keeps today's official path, with `--core-sha256` and the musl selection.
4. `pkg/contracts/release/core_source.go`: also accept `https://github.com/jingniao/qui/releases/download/<tag>/sing-box-<ver>-qui.<n>-linux-(amd64|arm64).tar.gz` for component `sing-box`, with the same redirect hosts. Both the panel and the Agent validate this. Agents older than v1.2.0 reject it, so the panel refuses a core update with a qui-built URL for an Agent that doesn't advertise the new capability (phase 5).
5. `deploy/linux/tests/core-asset-test.sh` covers both sources.

Acceptance:
- A4.1 Two CI runs of the build script at the same pin produce byte-identical archives (reproducible), or the differences are explained and recorded.
- A4.2 The release contains both archives and the provenance file; `SHA256SUMS` verifies; `sing-box version` shows `with_v2ray_api` and the pinned version.
- A4.3 The installer test passes for both sources on glibc and musl fixtures; a tampered asset is refused.
- A4.4 `core_source` tests: the new prefix is accepted only with the exact asset pattern; lookalike repos, other assets, queries and traversal are rejected.

### Phase 5 — Agent: sing-box per-user stats

Changes:
1. `internal/agent/core/singbox`:
   - `SupportsV2rayAPI` returns true when the detected `Tags:` include `with_v2ray_api`; the capability is reported to the panel.
   - When supported, render `experimental.v2ray_api` on a random 127.0.0.1 port with `stats.enabled` and `stats.users` set to the user credential account names.
   - Add a stats reader mirroring `core/xray/stats.go` (same `traffic.Counter` mapping, reset semantics, revision tagging).
   - Node totals and connection counts stay on the Clash API; per-user online stays unknown.
2. `TrafficSourceStatus` reports per-user available on such nodes; the existing traffic batch, spool and quarantine paths are reused unchanged.
3. Capability flag in `Capabilities` (new proto field, regenerated with `scripts/generate-proto.sh`). Older panels ignore it.

Acceptance:
- A5.1 Golden render tests with and without the tag; `sing-box check` passes on the rendered config (CI `cores` job, self-built binary).
- A5.2 Real-core test (the `TestReal…` pattern): two users through VLESS, Trojan, Hysteria2 and TUIC on the self-built binary; the per-user counters match the bytes transferred within 5%; counters reset after each read; an official binary on the same test reports per-user unknown.
- A5.3 Restarting the core or the Agent neither loses nor double-counts traffic (spool path, same as the Xray tests).
- A5.4 The stats port listens on loopback only (the existing management-port isolation test pattern).

### Phase 6 — Panel: sing-box quotas, docs, v1.2.0 field test

Changes:
1. Panel:
   - The user page D5 note shows only when a granted node runs sing-box without per-user stats.
   - Quotas charge sing-box traffic automatically; the counters already arrive through the same path.
   - The core update form offers the qui-built asset URL and digest for the current release.
2. Docs:
   - D1/D5 superseded by U2 in `lightweight-refactor-plan.md`.
   - AGENTS.md §4.4, §9.1 (sing-box per-user), §10.3, §22.2, §23, §34 D-notes.
   - `protocol-support-matrix.md`, `deployment.md` (core source options).

Acceptance:
- A6.1 Field test on the three hosts, after Owner approval of v1.2.0:
  - switch alpine-node to the qui-built sing-box (stop the Agent, change the core in config.yaml, start it; delete core-incompatible inbounds first);
  - alice's traffic through it is charged to alice;
  - exceeding the quota on sing-box-only traffic cuts her access on every granted node;
  - a panel core update from the qui-built URL succeeds and rolls back on a bad digest;
  - official binaries elsewhere are unchanged.
- A6.2 arm64: self-built arm64 binary verified at least with `sing-box version` and `check` under qemu in CI. A real arm64 host remains unverified (unchanged from V1).

## 4. Open decisions

| ID | Question | Default in this plan |
| --- | --- | --- |
| O1 | WireGuard for users when one identity is shared by all clients | Keep it, labeled "one device at a time" |
| O2 | Store the subscription token encrypted so users can re-copy it | Yes (changes §9.3 "plaintext shown once") |
| O3 | Releases | v1.1.0 (phases 1–3), then v1.2.0 (phases 4–6) |

## 5. Risks

- **Migration 0112 is destructive** for extra devices. Clients configured with a removed device's link stop working after the upgrade. `qui-update` keeps the `pre-update` database, and the plan documents this in the release notes.
- **Maintenance cost of U2.** Every sing-box upgrade means a qui release with a new pin. A security fix in sing-box therefore waits for a qui release; the official-binary path remains as a fallback.
- **Loopback v2ray API has no authentication.** Any local process on the node can read traffic counters. These are read-only counters, the same as Xray's API today.
- **Sharing one link** means a leaked link gives full access until the Owner or user resets credentials (`credentials/reset` revokes the credentials, not just the token).

## 6. Phase 0 findings

(to be filled)
