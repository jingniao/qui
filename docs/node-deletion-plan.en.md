# Node Deletion Implementation Plan

**Date:** 2026-10-19  
**Status:** Superseded. A reduced version is implemented; see [AGENTS.md](../AGENTS.md) §11.3 and [operations](operations.md) §15  
**Scope:** Deleting registered nodes from panel management while preserving historical records

## 1. Purpose

Allow an Owner to remove a registered node from normal operation without breaking configuration references, historical reporting, or certificate security.

The current implementation supports cancelling an unused node registration. It rejects deletion after Agent enrollment or when configuration references remain. This proposal extends that behavior to registered nodes.

The recommended approach is logical deletion for registered nodes: retain the node identity record for historical references, mark it as deleted, revoke management access, and exclude it from operational queries. Physical deletion remains available for unused registrations that satisfy the existing checks.

This document describes proposed behavior. It does not claim that these capabilities are currently available. Project constraints remain defined by [AGENTS.md](../AGENTS.md) and decisions D1–D5 in the [lightweight refactor plan](lightweight-refactor-plan.md).

## 2. User-visible behavior

| Area | Behavior after deletion |
| --- | --- |
| Default node list, dashboard, and topology | Exclude the deleted node. |
| Node groups | Remove its membership without deleting groups or user grants. |
| Subscription generation | Stop returning endpoints belonging to the node. Previously downloaded client configurations are not erased. |
| Agent authentication | Reject the old identity, enrollment tokens, and certificate renewal attempts. |
| Configuration deployment and software updates | Reject new tasks targeting the node. |
| Access records and traffic history | Preserve existing records and their original node name; label the node as deleted. |
| Configuration revisions and deployment history | Preserve history; validate node availability before any new deployment or rollback. |
| Node settings | Read-only after deletion; ordinary enable operations cannot restore it. |
| Programs on the node server | Unchanged by panel deletion. Stop or uninstall the Agent and proxy core separately. |

An optional **Show deleted nodes** filter lets an Owner inspect retained node details and history. Deleted nodes do not count toward active inventory, expected reporting coverage, or operational health totals. Historical traffic remains included in historical totals under existing reporting rules.

## 3. Scope and exclusions

The first release includes:

- Deletion impact preview and dependency checks.
- Logical deletion of registered nodes.
- Revocation of management access and removal from operational selection.
- Historical reporting, backup, and restore compatibility.
- Existing cancellation behavior for unused registrations.

The first release does not include:

- Purging all historical records or guaranteeing erasure of every retained credential copy.
- Restoring a deleted node to active service.
- Remote uninstallation or arbitrary remote commands.
- Automatic rewriting of other nodes' routing or proxy configuration.
- A new job service, message broker, deletion journal, or general workflow engine.

To reconnect a previously deleted server, create a new node and enroll it with a new node ID and certificate. Do not reuse the deleted identity. Existing encrypted configuration archives and backups continue to follow their retention policies.

## 4. Deletion eligibility and impact preview

Before asking for final confirmation, display:

- Node name, address, registration state, and last heartbeat time.
- Node group memberships.
- Counts of the node's own inbounds, outbounds, and outbound groups.
- References from other nodes, proxy chains, and routing policies.
- Active deployment, rollback, core update, or Agent update tasks.
- Last reported spool and pending-upload information, including observation freshness. Missing or stale information is unknown, not zero.

The following conditions block deletion:

1. Another node's current published configuration still uses the target as a relay or exit.
2. Current cross-node draft configuration still references the target, including disabled objects that could later be enabled.
3. A deployment, rollback, core update, or Agent update involving the target is still active or awaiting reconciliation.

Return actionable blockers with object names and links to the relevant management pages. Existing task recovery mechanisms must resolve active or uncertain tasks; deletion must not silently mark them successful or discard them.

Inspect both relational references and structured data. Relevant examples include outbound target node IDs, proxy chain node lists and hops, policy scopes, and references resolved through groups. Checking foreign keys alone is insufficient.

Historical records do not block deletion. An immutable revision mentioning a node is different from a configuration currently running on another node. Remove operational dependencies through a new revision and wait for successful application before deleting the target.

The target node's own configuration can be retained for read-only inspection, but it must be excluded from active configuration lists and choices. References from other nodes to those retained objects still block deletion.

## 5. Operator workflow

### 5.1 Normal retirement

1. Open the node deletion preview.
2. Remove or replace cross-node references and publish the affected configurations successfully.
3. Allow final traffic and access batches to upload where possible.
4. Stop the Agent and proxy core on the server using the existing local service management procedure.
5. Confirm deletion in the panel.

Stopping a service is a separate operation. Panel deletion must never report that it has stopped a remote proxy unless an independent, implemented mechanism has actually verified that result.

### 5.2 Lost or unreachable server

Permit removal after operational dependency checks pass, even if the server is unreachable. Display a specific acknowledgement:

> Management access will be revoked. The panel cannot confirm that the proxy software on this server has stopped. Records not yet uploaded may be unavailable.

This acknowledgement does not bypass dependency or active-task blockers. Deletion does not revoke previously downloaded proxy credentials on a disconnected core that continues running its last configuration.

### 5.3 Confirmation dialog

Use one flow from both the node list and node details:

```text
Delete node
    -> Fetch impact preview
    -> Show blockers or eligible deletion details
    -> Enter the node name to confirm
    -> Submit deletion
    -> Refresh affected views and return to the node list
```

Disable repeat submission while the request is pending. Show server errors in the dialog and keep it open when deletion fails. Do not display a success message until the server confirms completion. A cancelled dialog makes no changes.

## 6. Backend and data model

### 6.1 Schema

Add a new migration after the latest migration present at implementation time. Do not modify historical migrations.

Proposed additions to `nodes`:

| Field | Meaning |
| --- | --- |
| `deleted_at` | Nullable UTC timestamp. `NULL` means the node has not been deleted. |
| `deleted_by` | Retained actor identifier or username snapshot for the deletion. |

Keep the existing status enumeration. A deleted node has `enabled=0`, `allow_subscription=0`, and an existing disabled status, while `deleted_at` expresses the permanent lifecycle decision.

Preserve the node ID and name for joins used by access, traffic, revision, and audit history. New registrations receive new IDs.

### 6.2 Transaction and module ownership

Use an application service to coordinate deletion. Each module modifies its own data through its Repository, consistent with the existing architecture. Transaction-aware module methods should share the same database transaction where needed.

Inside the transaction:

1. Load the node and recheck deletion eligibility against current state.
2. Mark the node deleted and disabled, and disable subscription eligibility.
3. Revoke its certificate, invalidate enrollment tokens, and invalidate pending certificate renewals.
4. Remove node group memberships.
5. Record the deletion operation, including node ID, node name, actor, and time.
6. Commit only if every required operation succeeds.

After commit:

- Clear the node's live connection snapshots and related transient observations.
- Publish notifications using the existing event mechanism so affected views refresh.
- Reject subsequent messages and reconnect attempts through identity validation. Existing streams must close on their next validation; do not promise immediate remote process shutdown.

Check every SQL error. Do not disable foreign keys, erase shared revisions, or use a best-effort loop that ignores failed cleanup statements. No remote network call should hold the database transaction open.

### 6.3 Lifecycle enforcement

Treat deletion as permanent throughout the application, rather than as a node-list filter alone:

- Operational node queries exclude `deleted_at IS NOT NULL`.
- Historical joins retain access to deleted node identity records.
- Settings updates cannot re-enable deleted nodes.
- Enrollment, enrollment replay, identity validation, and renewal cannot reactivate them.
- Group edits, proxy configuration, and policy edits reject new references to deleted nodes.
- Manual deployment, automatic quota revisions, health operations, restarts, and update scheduling reject deleted targets.
- Publishing or rolling back an old revision validates all relevant nodes and rejects references to deleted targets.

Deletion and creation of new references or tasks must use compatible transaction checks. A successful preview is not a reservation: a concurrent change can make a later delete ineligible.

## 7. HTTP API

Proposed endpoints:

| Endpoint | Purpose |
| --- | --- |
| `GET /api/v1/nodes/{id}/deletion-preview` | Read eligibility, impact counts, blockers, and observation freshness. |
| `DELETE /api/v1/nodes/{id}` | Cancel an eligible unused registration or logically delete an eligible registered node. |

Both endpoints require Owner access. Keep the existing CSRF protection on deletion. Preview responses must not include credentials, enrollment tokens, or private key material.

Suggested response behavior:

- `200` for a successful preview.
- `204` for successful deletion and repeated deletion of an already logically deleted node.
- `404` for an unknown node ID. Repeated cancellation of a physically removed unused registration may also return `404`.
- `409` with structured, readable blockers when deletion is not currently allowed or dependencies changed after preview.
- Standard authentication and authorization errors for unauthenticated users or Viewers.

Recheck all conditions when processing `DELETE`. The frontend must not be responsible for enforcing deletion safety.

## 8. Backup and restore

Include deletion metadata in backup inspection, export, restore, and consistency checks.

When restoring an older backup into an existing installation that still has the current database available, preserve known deletion and revocation decisions. An older backup must not re-enable a deleted identity, restore its enrollment capability, or make it operational through restored group membership or settings.

If the current database has been lost and a new installation is rebuilt solely from an older backup, it cannot know about deletions made after that backup. Document this limitation and require review of recovered node identities before reconnecting Agents.

Restoring old backups must still run migrations forward. Historical joins, access queries, traffic summaries, and retained deployment records must remain valid after restoration.

## 9. Implementation sequence

### Phase A: Backend lifecycle

- Add deletion metadata and define operational versus historical repository reads.
- Implement dependency preview and transaction-time validation.
- Implement logical deletion, credential revocation, and atomic operation logging.
- Enforce deletion across settings, identity, configuration references, and task creation.

### Phase B: Frontend and read paths

- Add Owner deletion actions to node list and details.
- Implement impact preview, blocker links, name confirmation, and submission states.
- Update node lists, groups, subscriptions, topology, dashboard, and configuration choices.
- Add deleted-node labels to historical views and optional read-only inspection.

### Phase C: Recovery and verification

- Update backup and restore handling.
- Validate concurrent operations and reconnect behavior.
- Update the operations manual and relevant API and architecture documentation.
- Run focused tests, broader affected checks, and real Agent acceptance checks.

## 10. Acceptance criteria

The feature is complete only when the following have been demonstrated:

1. Existing unused-registration cancellation still works and invalidates its tokens.
2. An eligible registered node with access and deployment history can be deleted without foreign-key errors; historical names and records remain queryable.
3. Cross-node draft and currently published dependencies block deletion with useful explanations. Removing dependencies and successfully publishing the changes makes deletion eligible.
4. Active or uncertain deployment and update tasks prevent deletion until resolved.
5. Deleted nodes disappear from normal inventory, subscriptions, topology, and operational coverage calculations.
6. Old certificates, renewal candidates, and enrollment replays cannot reconnect or reactivate the node. Ordinary enable requests are rejected.
7. New proxy references, group membership, manual deployments, automatic quota revisions, and update tasks cannot target deleted nodes.
8. Old revision deployment and rollback cannot restore operational references to deleted nodes.
9. Preview/delete races and concurrent reference creation either complete consistently or return a conflict without partial changes.
10. Repeated requests are safe; Viewer requests and invalid CSRF requests are rejected.
11. A failed transaction leaves both deletion state and required audit state uncommitted. Notifications are emitted only after commit.
12. Restoring an older backup into an existing installation preserves known deletions and revocations. Recovery from an older backup alone documents its knowledge limitation.
13. Real Agent checks verify rejection after deletion. UI text does not claim that remote proxy processes were stopped or uninstalled.
14. Frontend type checking and production build pass; affected Go tests and backup compatibility checks pass. Any skipped acceptance check is explicitly recorded.

## 11. Expected outcome

Owners can remove retired nodes from daily management while preserving meaningful history and preventing identity reuse. The implementation stays within the existing Go monolith, SQLite database, module boundaries, deployment paths, and event notifications.
