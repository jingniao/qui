# flyx and qui: Source Code Comparison

**Review date:** October 26, 2026  
**flyx snapshot:** [`79af4975727cf58c24ea0db3715048ee2f502436`](https://github.com/kure29/flyx/tree/79af4975727cf58c24ea0db3715048ee2f502436)  
**qui baseline:** `be63347`, including the frontend changes present in the working tree during the review.

## Executive assessment

flyx provides useful reference implementations for subscription delivery, user self-service, and traffic accounting with official proxy binaries. Its subscription workflow is closer to the experience currently being considered for qui: users obtain a subscription, import it into a client, and view usage and expiration without first managing device credentials.

qui already has reusable foundations: user-level quotas, node-group authorization, independent device credentials, configuration revisions, deployment, and rollback. Adopting a simpler subscription experience does not require replacing these foundations or switching frontend frameworks.

The most useful next step is to establish an explicit relationship between the authenticated portal account and its proxy user. Subscription simplification can then build on that relationship. sing-box accounting through nftables is a separate implementation decision.

## Scope and evidence

This was a targeted source review of subscription models, user ownership, configuration rendering, and traffic accounting. flyx was not installed or executed, and this is not a complete security or reliability audit. Downloaded source files were inspected without running their scripts or installing dependencies.

The findings describe the pinned flyx snapshot and the qui code observed during the comparison. They do not establish behavior across every protocol, operating system, or deployment environment.

## Comparison at a glance

| Area | flyx | qui at the reviewed baseline |
| --- | --- | --- |
| Main access model | A subscription owns credentials and is assigned to a panel user | A proxy user owns devices with independent credentials; subscription tokens reference a user and device |
| Quota ownership | Per subscription | Per proxy user, shared across devices and reporting nodes |
| User experience | Assigned subscriptions, import links, usage, expiration, and resets | Management features are implemented; the new portal is still being integrated |
| sing-box personal accounting | Marked per-subscription outbounds plus Linux nftables counters | Node totals only; no per-user accounting through this adapter |
| Configuration application | Relevant kernel configurations are rendered and pushed | Explicit revisions, deployment coordination, and rollback |
| Frontend | React with a broader set of UI and utility dependencies | Vue with fewer runtime dependencies |
| Product scope | Node deployment, forwarding, imported sources, and client templates | Lightweight internal proxy access, policy management, and controlled configuration delivery |

## 1. Subscription and quota models

### flyx

The current primary model is `Subscription`. The older `SubUser` model alone does not describe the current design.

A subscription contains its own token, UUID, password, node-group selection, expiration, enabled state, traffic limit, monthly reset day, and cumulative upload/download counters. Its `UserID` assigns it to a panel login account.

One login account can own multiple subscriptions. Limits belong to each subscription; the model does not automatically impose one shared account quota across all of that account's subscriptions.

Sources:

- [Subscription model](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/go-backend/internal/store/model/subscription.go)
- [Subscription handling and synchronization](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/go-backend/internal/http/handler/subscription.go)
- [Authenticated user subscription handlers](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/go-backend/internal/http/handler/user_subscription.go)

### qui

qui assigns the quota to the proxy user. Devices carry independent protocol credentials, and a subscription token references both the user and a device.

Subscription generation selects endpoints from published configurations and verifies that the published credentials match the requested device. Existing node-group authorization and configuration publishing can remain in place if device creation is hidden behind a simpler user workflow.

Sources: [subscription service](../internal/panel/subscription/service.go) and [user/device models](../internal/panel/user/model.go).

**Recommendation:** provide one default subscription per user as the initial experience, while retaining qui's shared user quota. Multiple subscriptions, separate subscription quotas, and extensive template processing are optional features rather than prerequisites.

## 2. sing-box accounting without modifying or rebuilding the proxy core

flyx demonstrates an alternative to the optional V2Ray statistics API:

1. Give each subscription independent authentication credentials and an internal user name.
2. Route authenticated traffic through an outbound associated with that subscription.
3. Set a socket mark on the outbound connection.
4. Use Linux nftables to count outgoing packets by socket mark and returning packets by connection mark.
5. Report cumulative counters with an epoch, apply their deltas to the subscription, and re-evaluate its quota.

In `render_singbox.go`, the per-user outbound uses `routing_mark`, and a routing rule selects it through `auth_user`. The node-side nftables code creates named counters, copies the packet mark to the connection mark, and counts traffic in both directions. Xray rendering and other routing paths also account for user-specific outbounds.

Sources:

- [User identities and mark allocation](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/go-backend/internal/kernel/users.go)
- [sing-box configuration rendering](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/go-backend/internal/kernel/render_singbox.go)
- [Routing integration](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/go-backend/internal/kernel/split.go)
- [Node-side nftables counters](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/go-gost/x/kernel/nft.go)
- [Counter accounting repository](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/go-backend/internal/store/repo/repository_subscription.go)

### Correction to the earlier assessment

The earlier statement that keeping the official sing-box binary prevents personal traffic accounting was too broad. A binary without the V2Ray API cannot expose that API, but its existing authenticated routing and socket-mark capabilities can be combined with external nftables counters.

This route does not require changes to proxy-core source code or recompilation to enable the V2Ray API. It does require a new accounting implementation in the surrounding platform.

### Costs and limitations

- It requires Linux nftables and the appropriate host permissions.
- It counts marked outbound network bytes. These are not necessarily equivalent to Xray StatsService user byte counters; the accounting definition must be explicit.
- Routing policies, relay chains, and alternate outbounds must preserve correct user attribution.
- Counter resets, reconnects, duplicate reports, and offline delivery must be handled consistently.
- Integration with qui's existing host-network rules must be verified.
- flyx supports independent credentials only for a defined set of protocols. Its documentation identifies shared-credential cases, including Shadowsocks, Snell, WireGuard, and imported nodes, where individual accounting or revocation of previously imported access is limited.
- flyx's mark allocator has an explicit numeric ID range; it is not an unrestricted identity mapping.

qui currently implements D5 with sing-box node totals only. Adding this path would extend that implementation scope and require corresponding documentation and deployment acceptance. It is not already available in qui.

The reference implementation also does not identify individual physical devices when those devices share subscription credentials.

## 3. Portal identity and ownership

flyx's user subscription handlers derive the caller's identity on the backend, list subscriptions assigned to that identity, and check ownership before resetting a subscription.

At the reviewed qui baseline, `Session.CurrentUser` is an administrative account (`auth.Admin`, Owner or Viewer), separate from a proxy user (`user.User`). The new portal reads the proxy-user management list, tries to match the login name, and falls back to the first returned user. A similar selection appears during subscription generation. The node page also contains a path that treats an administrator ID as a proxy-user ID.

Sources: [authentication models](../internal/panel/auth/model.go), [portal client page](../web/src/modules/portal/pages/ClientAccessPage.vue), and [node list page](../web/src/modules/nodes/pages/NodeListPage.vue).

Matching names or selecting the first record is not a reliable ownership relationship. An Owner's permission to read management data does not make that data the Owner's personal subscription. A Viewer does not acquire user-management permissions by entering portal mode.

**Recommendation:** define an explicit login-account-to-proxy-user relationship and expose personal subscription, node, and usage endpoints resolved from the authenticated session. Do not grant broad management access to Viewer accounts as a shortcut.

## 4. Architecture and implementation choices

flyx uses React, Go with GORM, and node functionality associated with its go-gost implementation. It covers a broader set of forwarding, import, and client-template workflows.

qui uses Vue, Go with SQLite, a gRPC/mTLS Agent control channel, and explicit configuration revisions with deployment and rollback. These remain suitable foundations for a small internal platform.

Feature breadth alone does not establish that either project is more secure, stable, or resource-efficient. No comparative runtime benchmark or complete failure-mode audit was performed.

The appropriate adaptation is to borrow the user workflow and study the accounting technique while preserving qui's existing deployment model. Copying flyx's entire frontend stack or forwarding subsystem would add scope beyond the immediate requirements.

## 5. Recommended sequence for qui

1. Establish explicit portal identity ownership and remove name-based or first-record user selection.
2. Offer one default subscription per user and hide routine device-creation steps.
3. Reuse existing Xray accounting, shared user quotas, and automatic access revisions to complete the subscription workflow.
4. Add sing-box nftables accounting only when needed, with a documented protocol matrix, deployment requirements, and byte-counting definition.
5. Add client import shortcuts, subscription usage information, and optional reset schedules incrementally.

If multiple devices share credentials, present device-level observations conservatively. Accurate per-device usage requires distinct identities; a shared subscription cannot supply that distinction by itself.

## Licensing and review limits

flyx is published under GPL-3.0. This review did not copy its implementation into qui. Direct code reuse would require checking license compatibility.

This report records the targeted comparison already performed. It introduces no implementation changes and does not claim successful runtime tests for flyx or the proposed adaptations.
