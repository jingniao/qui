# Project Documentation and Code Audit

**Project:** Lightweight Multi-Node Proxy Control Platform  
**Audit date:** October 18, 2026  
**Audited commit:** `cdd23b05f35d9411f339dd5dfa3640f2fb2234c9`  
**Report type:** Documentation review, selected implementation review, and local verification

This report documents the latest audit in this session. It is separate from `project-audit-summary.en.md`, which describes an earlier revision. Findings apply to the commit above and should be revalidated after subsequent changes. Preparing this English report did not rerun the audit.

**Overall assessment:** The principal architecture and business flows broadly match the documentation. The project has a substantial implementation and test foundation and is suitable for continued internal candidate testing. However, confirmed gaps in privacy, automatic access revocation, and recovery procedures prevent a recommendation to declare V1 complete.

The audit reviewed the main active documents and selected implementation paths. It was not an exhaustive line-by-line security audit or a complete Linux acceptance exercise. Product source code was not changed. Targeted reproductions were loaded through a Go overlay, with local evidence retained under `.tools/`.

**F1 — High priority: Raw Xray access logs retain ordinary HTTP URL paths and query parameters.**

Acceptance requirement V20 requires collection, queues, APIs, logs, and archives to exclude paths, cookies, passwords, and other prohibited content. See [the acceptance matrix](v1-acceptance.md).

The collector removes HTTP URL paths and query parameters during parsing, but the raw access line has already been written to disk. The relevant implementation is in `internal/agent/core/xray/access_log.go:68` and `internal/agent/collector/xray_line.go:72`.

Using the installed Xray 26.3.27 binary, the audit sent an ordinary HTTP proxy request to a temporary local HTTP server. The request contained synthetic markers only. The on-disk access log included:

```text
accepted http://127.0.0.1:<port>/AUDIT-PRIVATE-PATH?token=AUDIT-PRIVATE-QUERY
```

Sensitive values embedded in URLs can therefore remain on the node's disk. The log is truncated at core startup and when it reaches 20 MiB; closing the log does not remove its contents. With low traffic, there is no time-based deletion. File permissions of `0600` and the size limit restrict access and volume but do not satisfy the promise to avoid recording paths.

This reproduction concerns ordinary HTTP proxy traffic. It does **not** establish disclosure of encrypted HTTPS CONNECT paths, cookies, or request/response bodies.

The existing test in `internal/agent/core/xray/access_test.go:40` uses SOCKS5 and checks the serialized collected records. It does not inspect raw disk logs for HTTP inbound traffic.

**Recommended action:** Apply the privacy boundary before raw output is persisted. Evaluate logging options supported by the official core. If domain logging and raw-log privacy cannot both be provided for every accepted HTTP mode, explicitly resolve that capability limitation and update the supported scope accordingly. Add real-core acceptance coverage that inspects raw logs as well as collected records.

Local evidence: `.tools/audit-20261018-privacy_test.go` and `.tools/audit-20261018-privacy.log`.

**F2 — High priority: Grant changes can cause automatic disablement to miss nodes that still hold published credentials.**

Sections 9.1 and 22.1 of the project specification promise automatic revisions that remove credentials when a user is disabled, over quota, or expired.

In `internal/platform/server/quota.go:211`, `scheduleTransition` discovers target nodes through the user's current group grants and current group membership. It does not also find nodes whose published configurations still contain that user's credentials.

The audit reproduced this sequence:

1. Authorize a user and publish their credentials to node A.
2. Save removal of the user's node-group grant without manually publishing a new configuration.
3. Disable the user.
4. Drive the quota tick and flush operations.

The result was:

```text
automatic revisions=0, published credential remains=true
```

The node can continue serving the previously published credential even when it is online and deployment is otherwise healthy. Subsequent periodic checks do not recover the missing target because they look for user-state transitions. Filtering the subscription does not revoke credentials already installed on a node.

The finding does not treat the requirement to manually publish an ordinary grant edit as a defect. The defect is that the subsequent disable operation, which is explicitly promised to execute automatically, also misses the old node.

**Recommended action:** Build the target set from the union of currently authorized nodes and nodes whose published configurations still hold the user's credentials. Read published state through the Deployment module and reconcile the required access change.

Local evidence: `TestAuditDisableAfterRemovingGrant` in `.tools/audit-20261018-quota_test.go`, with output in `.tools/audit-20261018-reproductions.log`.

**F3 — Medium priority: A read failure at the startup reconciliation entry point is not retried.**

The operations manual describes startup reconciliation as recovering work lost during downtime or the coalescing window, and describes retries after read failures.

In `internal/platform/server/quota.go:181`, failure to list nodes returns immediately. Failure to read a node's published configuration skips that node. The `run` method calls `recover` only once. Later `tickOnce` calls retry held transitions and evaluate users, but cannot recover startup work that was never queued.

The audit reproduced the issue by publishing an active user, making the user exceed quota, and constructing a restarted quota state machine seeded from that state. It then temporarily hid the `config_revisions` table to inject a read failure, ran startup recovery, restored the table, and drove three tick/flush cycles.

The result was:

```text
startup read recovered but no retry after 3 ticks: published over-quota credential remains=true
```

This is controlled database-read fault injection; it does not claim to reproduce a specific production disk failure. Normal retries after work enters the pending queue already exist. The remaining gap is at the startup scan boundary.

**Recommended action:** Retain retry state for an incomplete startup scan or individual failed nodes. Reuse the existing coalescing window and alert mechanism.

Local evidence: `TestAuditStartupReadFailureRecoversOnTick` and `.tools/audit-20261018-reproductions.log`.

**F4 — Medium priority: Re-enrollment instructions omit local state bound to the previous node identity.**

The operations manual at `docs/operations.md:86` and `:93` instructs operators to create a new node record and clear `identity/` before re-enrolling on the same machine.

Other local state remains tied to the previous identity:

- `internal/agent/agenthost/state.go:30` treats paths such as `agent-release-history`, `releases/agent-installed`, and `agent-jobs` as evidence of managed Agent state. Clearing `identity/` alone leaves these paths in place. Enrollment in `internal/agent/control/control.go:72` rejects existing managed state.
- Even for an unmanaged Agent, an existing spool retains the previous NodeID. `internal/agent/spool/spool.go:129` rejects a different identity with `spool identity or schema mismatch`. The startup path in `control.go:306` opens this queue.

Following the documented procedure can therefore fail during enrollment or prevent proxy service from resuming afterward. Rejoining the original node groups also does not establish that every outbound and chain reference to the old NodeID has been migrated.

This finding is supported by source inspection and the passing existing test `TestSpoolMissingKeyAndDifferentIdentityDoNotReplaceExistingQueue`. A complete Linux CA-rotation exercise was not performed.

**Recommended action:** Document the complete replacement procedure: stop the relevant services, back up and isolate the previous node's data, enroll with a clean data directory and correct ownership, and rebuild configuration references. Explicitly address retention or disposal of queued telemetry. Preserve identity checks rather than weakening them to accommodate the incomplete procedure.

**Documentation consistency needs another pass.**

1. **Automatic publication has conflicting boundaries.** `AGENTS.md:1250` says automatic revisions do not include unpublished edits, while `:1253` allows user-to-inbound bindings to come from the draft. The implementation documents and implements this exception in `quota.go:540` onward. The architecture document and acceptance matrix still use the unconditional promise. Define automatically synchronized fields and manually published fields in one authoritative rule.
2. **The primary D3 decision is stale.** It still specifies reporting every 30–60 seconds and an overshoot window of one report interval plus deployment time. The implementation and newer operations text specify sampling every 5 seconds, reporting every 15 seconds, and an additional coalescing window of up to 60 seconds. The plan records this discrepancy later, but its primary decision remains unchanged.
3. **Acceptance wording omits qualifications.** The D3 acceptance row does not fully reflect the healthy-path timing conditions, retries, or the draft-binding exception. Requirements, observed behavior, and accepted tradeoffs should be distinguished. “Source code takes precedence” should not silently waive an unmet requirement.
4. **Completion labels are inconsistent.** The plan header says execution is in progress; later sections mark the plan complete; some earlier acceptance boxes remain unchecked. A Linux criterion is described as “Met on Windows.” The README correctly states that V1 is incomplete. Use explicit states such as implementation complete, verified on Windows, and awaiting Linux acceptance.
5. **The earlier English audit is historical.** `project-audit-summary.en.md` identifies its older baseline. This audit found implementation fixes for earlier internal-credential merging, WireGuard peer synchronization, and missing online observations being shown as zero. Those issues are not repeated here as current defects.

**The principal implementation is substantially aligned with the documentation.**

| Area | Assessment |
| --- | --- |
| Modular Go monolith, SQLite, embedded Vue frontend | Architecture and build flow broadly match the lightweight deployment design. |
| Control-plane/data-plane separation | The panel manages configuration while Agents run the cores. The selected real two-Agent relay test passed. |
| Owner/Viewer, CSRF, token hashing | Backend enforcement and regression coverage exist. This audit was not a complete penetration test. |
| Users, devices, node-group subscriptions | Subscription generation checks users, devices, protocols, grants, and published credentials. Device operations explicitly tell operators that node authorization changes require publication. |
| D1–D5 decisions | Official-core adaptation, soft limits, unknown sing-box per-user values, nullable domain byte counts, and configured-egress semantics are substantially implemented. |
| Publication, native validation, rollback | Main mechanisms exist; selected Xray and sing-box real-core validation and rollback tests passed. F2 and F3 remain in automatic quota handling. |
| Updates | Core URL/digest/redirect/extraction restrictions and signed Agent updates are implemented separately. |
| Node outbound networking | Core configuration rendering and the restricted nftables helper exist. Linux kernel effects, reboot behavior, and failure rollback were not accepted on real Linux hosts in this audit. |
| Access records | Metadata filtering exists, but raw Xray HTTP logs violate V20 as described in F1. |
| Installation, CA replacement, multi-node failure recovery | Full Linux acceptance is still required; the re-enrollment instructions also have the F4 gap. |

**Verification results support the implementation baseline, with explicit limits.**

Environment: Windows amd64, local Go 1.27.1, installed frontend dependencies, and the existing Xray 26.3.27 and sing-box 1.14.3 binaries. Dependencies and cores were not downloaded again.

| Check | Result |
| --- | --- |
| `go build ./...` | Passed |
| `go vet ./...` | Passed |
| `npm --prefix web run build` | Passed, including Vue/TypeScript type checking |
| `npm run test:time` | 3 passed |
| `go test -json -count=1 ./...` | 806 tests/subtests passed, 86 skipped, 0 failed; 48 packages passed |
| Five additional selected real-core integration tests | All passed |
| Three targeted audit reproductions | All exposed the defects under test: two quota edge cases and raw HTTP log privacy |

The five additional real-core tests covered Xray native validation and rollback, Xray account statistics and management-port isolation, sing-box forwarding/rollback/restart, sing-box node traffic totals, and two-Agent VLESS-to-Trojan deployment.

The initial test and frontend-build attempts encountered Windows sandbox directory-access restrictions. They were rerun with approved normal filesystem permissions; the table reports those rerun results. Skips included missing Agent/core fixtures, platform conditions, and helper subprocess entry points. A skip is not a pass. The five additional real-core tests do not mean that all 86 skipped cases were subsequently executed.

The audit did not run the race detector, complete browser interaction coverage, Linux systemd/nftables installation and removal, amd64/arm64 release-package acceptance on real machines, the complete cross-host three-node protocol matrix, or long-duration stability tests.

Local evidence is stored in the repository's ignored `.tools/` directory and may not be available in a clone:

- `audit-20261018-tests-retry.jsonl`
- `audit-20261018-web-retry.log`
- `audit-20261018-real-cores.jsonl`
- `audit-20261018-reproductions.log`
- `audit-20261018-privacy.log`
- `audit-20261018-overlay.json`

**Engineering judgment: prioritize reliable delivery and state consistency.**

The architecture is appropriate for 1–10 internal users. A Go monolith, local SQLite, and official proxy cores keep deployment dependencies manageable. Removing custom core patches, hard core-level quotas, and extensive recovery machinery was a useful reduction in scope.

Using the refactor plan's counting convention, the audited revision contains 51,460 non-test Go lines across 375 files, 71 Vue files, and 108 SQL migrations. This is a substantial reduction from the plan's recorded 131,800-line baseline. Nevertheless, it remains a control platform with meaningful maintenance demands. The migration count is historical and does not justify rewriting applied migrations.

The highest-value work now is ensuring that state converges when operations overlap: a grant changes, a user is disabled, the panel restarts, or a node identity is replaced. Individual CRUD paths have extensive tests, while combinations of transitions still expose gaps. Fix F1–F4, preserve the triggering scenarios as regression coverage, and then complete a Linux three-node exercise from installation through recovery.

Automatic quota changes also carry an operational tradeoff. Xray's `adapter.go:170` and sing-box's `adapter.go:234` stop the running core before starting the replacement configuration. A quota change for one user can therefore affect existing connections belonging to other users on the same node. Measure this interruption and document it in the product behavior and acceptance criteria. Retain the coalescing window, and consider bounded incremental updates only if measured usage justifies the additional complexity.

Freeze new protocols and operational features while completing the existing delivery requirements. Prioritize Xray for the main acceptance path; validate sing-box separately within its promised scope and retain clear per-user statistics limitations. Host-wide SNAT has a broader effect than proxy configuration and deserves dedicated Linux tests for new connections, dual-stack operation, containers, reboot, and rollback.

**Release recommendation:** Continue internal candidate testing. Do not declare formal V1 completion until the identified defects are resolved and the required Linux acceptance evidence is available.
