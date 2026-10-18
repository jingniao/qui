# Project Documentation and Code Audit Summary

**Project:** Lightweight Multi-Node Proxy Control Platform  
**Original audit date:** October 18, 2026  
**Audited commit:** `d3f31fc`  
**Purpose:** English summary of the documentation review, code audit, and protocol support assessment previously performed in this session.

> This report describes the audited revision, not a fresh audit of the current checkout. The checkout is now at `2827653`. Findings below have not been revalidated against that revision and must not be assumed to remain unresolved. The updated project specification also describes node egress source IP/interface settings and host-wide source-address enforcement through a restricted nftables oneshot service; those additions were outside the original audit.

## Overall Assessment

The architecture and principal business flows broadly match the project documentation. The move to official proxy cores, a modular Go monolith, SQLite, and an embedded Vue frontend is appropriate for a self-hosted system serving 1–10 internal users.

However, the audited implementation did not fully satisfy its documented guarantees. The most significant gaps concerned automatic quota deployment: retry behavior, restart recovery, separation of published configuration from draft changes, and WireGuard credential consistency.

The project has a substantial implementation and test foundation. It should be treated as a development or release candidate pending defect resolution and Linux multi-node acceptance, consistent with the documentation's explicit statement that V1 is not complete.

## Scope and Evidence

The review covered the active project documentation and selected code paths for:

- Users, devices, credentials, node-group authorization, and subscriptions.
- Configuration revisions, deployment, automatic quota enforcement, and rollback.
- Official Xray and sing-box adapters, protocol capabilities, and connection observations.
- Domain-level access collection, authentication, authorization, and core updates.

The audit included builds, static checks, the existing Go test suite, frontend time tests, and three targeted reproductions loaded through a Go overlay. It did not modify tracked project source files.

It did **not** include a complete security audit, race-detector testing, Linux installation acceptance, or real multi-node traffic acceptance. Source-level support must not be interpreted as successful production validation.

## Architecture and Documentation Alignment

| Area | Assessment at the audited revision |
| --- | --- |
| Go monolith, SQLite, embedded Vue frontend | Broadly aligned with the lightweight deployment design. |
| Control-plane/data-plane separation | Implemented; user proxy traffic does not pass through the central panel. |
| Node-group subscriptions | Substantial implementation; subscription generation checks published state and matching credentials. |
| Official core adapters and D1–D5 decisions | Broadly aligned, with upstream limitations explicitly acknowledged. |
| Revision validation and rollback | Main mechanisms exist; automatic quota handling has the defects described below. |
| Domain-level access records | Reviewed collection paths use constrained metadata parsing rather than webpage-content inspection. |
| Owner/Viewer and CSRF protection | Enforced in backend code, not solely through hidden frontend controls. |
| Core update verification | Official source restrictions, archive digest checks, and redirect controls have implementation support. |
| Linux installation and multi-node operation | Not validated by this audit. |

## Protocol Support

The audited code supports the following protocol directions, subject to core capabilities and configuration validation:

| Protocol | Xray inbound | Xray outbound | sing-box inbound | sing-box outbound |
| --- | --- | --- | --- | --- |
| HTTP CONNECT | Yes | Yes | Yes | Yes |
| SOCKS5 | Yes | Yes | Yes | Yes |
| Shadowsocks | Yes | Yes | Yes | Yes |
| VLESS | Yes | Yes | Yes | Yes |
| Trojan | Yes | Yes | Yes | Yes |
| WireGuard | Yes, major version ≥25 | Yes | No | Yes |
| Hysteria2 | No | No | Yes | Yes |
| TUIC | No | No | Yes | Yes |

Additional routing actions include Direct, Block, outbound groups, and two-node or three-node proxy chains. Inbound and outbound protocols are independent, enabling paths such as VLESS → node A → Trojan → node B.

Important limits:

- HTTP CONNECT is TCP-only. UDP support for other protocols depends on the core and specific configuration; some combinations are restricted.
- VLESS and Trojan have WebSocket and gRPC transport implementations. Trojan requires TLS in the platform's configuration validation.
- Hysteria2 and TUIC require sing-box's `with_quic` capability.
- The documented reference binaries are official Xray `26.3.27` and sing-box `1.14.3`.
- VMess and full system TUN were not delivered as current supported capabilities. The support matrix does not establish VLESS REALITY support.
- Xray provides account-level traffic and online observations where supported. sing-box provides node traffic totals; per-user traffic and online counts remain unknown.
- Per-domain byte counts may be unavailable. Recorded egress describes configured routing, not proof of the actual final exit across a relay chain.

See [Protocol Support Matrix](protocol-support-matrix.md) for the detailed capability contract.

## Principal Findings

### 1. High: Failed automatic quota deployments can lose their retry

The specification promises retries in the next window after failure or node contention. In `internal/platform/server/quota.go`, the pending entry is removed before processing. Failures reading published state, creating a snapshot, or creating a revision return without requeueing. Only `ErrNodeBusy` explicitly schedules another attempt.

Meanwhile, `internal/panel/user/quota.go` has already recorded the user's new inactivity state. A later evaluation sees no transition and therefore does not necessarily schedule another deployment.

**Impact:** A transient failure can leave an over-quota or disabled user authorized on the node until another relevant change or manual publication occurs.

There is a related restart gap: a panel restart during the debounce window loses in-memory work, while startup seeding treats the current inactive state as the baseline. The operations documentation acknowledges part of the startup tradeoff, but the stated reporting-period-plus-60-seconds bound is not unconditional.

**Recommendation:** Reconcile current user access requirements against published node state, retain failed work, and recover outstanding changes after restart. Reuse the existing revision/deployment mechanisms where possible.

### 2. High: Quota merging can break published outbound credentials

`mergeUserAccess` preserves published outbounds but replaces the entire credential collection with credentials from a fresh draft snapshot. That collection includes internal outbound and relay credentials, not just user credentials.

If an Owner saves an outbound credential change without publishing it, a subsequent quota revision can retain the old outbound reference while removing the referenced credential.

**Reproduced result:**

```text
quota-only merge broke published outbound: outbound credential missing
```

Inbound credential assignments also come from the fresh snapshot, so unpublished changes to inbound user authorization can affect the automatic revision.

**Recommendation:** Preserve published internal credentials and approved proxy bindings. Limit automatic changes to an explicitly defined user-access surface.

### 3. High: WireGuard quota removal leaves stale peer references

The automatic merge removes a user's credentials and updates inbound credential IDs, but retains the published `Settings.WireGuard.Peers` collection.

For an inbound shared by multiple users, removing one user's credential can leave an enabled inbound containing a peer that refers to the removed credential. Xray rendering then fails.

**Reproduced result:**

```text
WireGuard peer credential missing
```

The original test configuration rendered successfully; the merged configuration failed after one user's credential was removed.

**Recommendation:** Update WireGuard peer bindings together with user credentials, including removal, reactivation, and key changes. Add shared-inbound regression coverage.

### 4. Medium: Missing user online observations can become a false zero

`ConnectionObserver.ForUser` returns a numerical sum whenever an authorized node has a fresh snapshot, even if that snapshot contains no account entry for the requested user.

**Reproduced result:**

```text
no sample for this user's credential: got 0, want unknown
```

This is particularly relevant to WireGuard peers, for which the Xray online reader explicitly documents missing online statistics. Partial reporting across several nodes can also be presented as a complete count.

**Recommendation:** Distinguish explicit zero, unavailable data, and partial coverage. Document that summing per-credential/per-node distinct-IP counts does not produce a globally deduplicated client-IP count.

### 5. Medium: The UI confuses quota exhaustion with completed enforcement

The user detail page displays “quota exhausted; user automatically disabled” based only on `usedTraffic >= trafficLimit`. It does not establish that the restricting configuration reached the node.

**Impact:** The message can be incorrect during the debounce window, disconnection, deployment failure, or rollback.

**Recommendation:** Display quota state separately from deployment state: pending, applying, applied, or failed.

A smaller UI inconsistency remains in the audited outbound-group page: help text mentions weighted selection after that strategy was removed. It also does not explain that sing-box maps the platform's round-robin option to upstream `urltest` behavior.

## Documentation Quality

Strengths:

- Historical material is archived and current entry points are clearly identified.
- Upstream limitations are documented, including unknown user statistics on sing-box and unavailable per-domain byte counts.
- The README and acceptance matrix explicitly distinguish implementation progress from V1 completion.

Recommended improvements:

1. **Separate requirements from implementation descriptions.** “Source code takes precedence” is reasonable for an architecture inventory, but must not silently waive unmet acceptance requirements.
2. **Use unambiguous completion states.** The phase checklist is marked complete while some earlier acceptance checkboxes remain empty. A Linux-specific condition supported only by Windows evidence should be marked partially complete or pending.
3. **Reduce duplicated business rules.** For example, the specification states an inactivity reason order of quota exceeded → expired → disabled, while the audited implementation checks disabled first.
4. **Qualify enforcement timing.** Document healthy-path timing separately from disconnection, retry, restart, and rollback behavior.

## Validation Results

| Check | Result |
| --- | --- |
| `go build ./...` | Passed |
| `go vet ./...` | Passed |
| `npm --prefix web run build` | Passed, including Vue/TypeScript type checking |
| `npm run test:time` | 3 tests passed |
| Existing Go suite, `-count=1` with JSON output | 671 tests/subtests passed; 85 skipped; 0 failed |
| Three additional audit reproductions | All three demonstrated the defects described above |

The first sandboxed run encountered Windows filesystem access restrictions. The full-suite results above come from a subsequent run with normal filesystem permissions. Skips included missing core/Agent fixtures and platform-specific conditions; a skip is not a pass.

Local evidence from the original audit is stored under `.tools/verification/`:

- `audit-tests-unrestricted.jsonl`: existing Go test-suite results.
- `audit-overlay.json`: temporary test overlay mapping.
- `audit_server_test.go` and `audit_user_test.go`: targeted reproductions.

These are local, ignored audit artifacts and are not part of the committed project test suite.

## Engineering Opinion and Next Steps

The project is lightweight in deployment, but its implementation remains substantial. The audit counted approximately 49,900 non-test Go lines across `internal`, `cmd`, and `pkg`, excluding generated protobuf files and testdata, plus 226 Go test files.

The complexity comes primarily from interactions between protocols, nodes, credentials, deployment, updates, and recovery. A small user population does not eliminate those interactions.

Using official cores and accepting explicitly unknown metrics are sound decisions. The next investment should be in operational correctness and acceptance evidence:

1. Revalidate these findings against the current revision and fix any remaining automatic quota reconciliation defects.
2. Add regression coverage for failure/retry, restart during debounce, unpublished proxy edits, WireGuard shared inbounds, and unknown observation coverage.
3. Make the UI distinguish desired business state from confirmed node execution state.
4. Run fixed-version Linux acceptance with three nodes, including installation, two-hop and three-hop traffic, failed publication, offline replay, certificate operations, and backup/restore.

The existing tests provide a useful foundation, but the reproduced defects show that individually tested modules can still fail when combined. Cross-module and failure-transition tests should take priority over expanding the feature set.

## Reference Documents

- [Project Specification](../AGENTS.md)
- [Architecture Overview](architecture.md)
- [Deployment and Upgrades](deployment.md)
- [Operations Manual](operations.md)
- [Protocol Support Matrix](protocol-support-matrix.md)
- [V1 Acceptance Matrix](v1-acceptance.md)
- [Lightweight Refactoring Plan](lightweight-refactor-plan.md)
