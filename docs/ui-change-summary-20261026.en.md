# qui UI Changes and Module Boundaries

**Date:** October 26, 2026  
**Code baseline:** `be63347` and the uncommitted frontend changes present during the review  
**Basis:** product decisions discussed with the user, the user's stated editing scope, and static inspection of the current routes, pages, and data-handling code.

## 1. Overview

The UI is moving toward a distinction between personal access and administration:

- The portal focuses on personal usage, authorized entry points, and subscriptions.
- Administration focuses on users, servers, proxy entry points, relay schemes, and routing.
- Servers represent managed machines; proxy nodes represent services clients connect to.
- A canvas editor connects reusable resources into relay or proxy-chain schemes.
- Client and server policies are configured independently while sharing rule-set content.
- Configuration publishing moves into node creation/editing and canvas workflows.

This document distinguishes **UI changes**, **partial integration**, and **planned behavior**. A button, form, or status label does not establish that the corresponding backend, Agent, or client behavior is implemented. This is a change record, not a complete audit or acceptance report.

## 2. Scope and navigation

The user reported changes across the portal, plus the administrative dashboard, user management, the first-level server page, the node-configuration pages and their secondary views, and parts of routing and system management.

```text
Portal
├─ Overview, including usage and subscription access
└─ Nodes and available lines

Administration
├─ System overview
├─ User management
├─ Servers
│  ├─ Server list
│  ├─ Agent management
│  └─ Proxy cores
├─ Node configuration
│  ├─ Node list
│  └─ Relay builder
├─ Routing control
│  ├─ Client policies
│  ├─ Node policies
│  ├─ Rule sets
│  └─ Policy simulator
├─ Access audit
└─ System management
   ├─ Backup and restore
   ├─ Operation log
   └─ Settings
```

Retained destinations such as access audit, backup, and operation logs are not all classified as fully redesigned or validated in this round. Likewise, the reported portal redesign does not establish that login, setup, and error pages were redesigned.

| Previous entry | Current navigation change |
| --- | --- |
| Separate user, device, and subscription entries | Consolidated under User Management; `/devices` and `/subscriptions` redirect to `/users` |
| Nodes mixing machine administration and proxy access | `/nodes` serves administrative server management; `/proxy/nodes` is the consolidated proxy-entry list |
| Separate inbound, outbound, outbound-group, and chain menus | Main entries become Node List and Relay Builder; older paths redirect |
| Routing Rules, Service Rules, Website Control | Client Policies, Node Policies, Rule Sets, and Simulator; website control joins node policies |
| Agent and core pages under System | Moved into the server area through redirects to the appropriate tabs |
| Standalone Configuration Versions | Removed from the primary workflow; `/system/revisions` redirects to Servers |
| Separate client-access destination | `/portal/client` redirects to the dashboard as personal access moves into the overview |

Sources: [routes](../web/src/app/router.ts) and [sidebar](../web/src/components/AppSidebar.vue). Redirecting an entry does not remove its old component, API, or underlying backend mechanism.

## 3. Portal changes

### Personal overview and subscription access

The intended experience puts usage, quota, expiration, access state, and connection information in front of the user without requiring knowledge of devices, credentials, inbounds, or revisions.

The portal direction includes personal traffic summaries with explicit periods, access-state notices, subscription copying, QR codes, client import, and available-node shortcuts.

The final access direction is one account and one shared subscription per user, without manual device creation. Earlier discussion of a device-management page is superseded by that direction. Shared credentials do not reliably identify physical devices or their individual traffic.

### Portal nodes

Show authorized entry points with names, regions/purposes, protocols, addresses, real status, and connection/import actions. Multiple routes to the same exit should have meaningful line names. Draft schemes, pending registration, and unapplied routes must not be presented as working access.

### Current integration boundary

The current implementation uses `viewMode` to switch presentation. It is not an independent end-user authentication model. Dashboard code still reads management APIs, matches a proxy user by login name, and falls back to the first record. Personal data must eventually be resolved through backend session ownership.

Sources: [dashboard](../web/src/modules/dashboard/pages/DashboardPage.vue), [node page](../web/src/modules/nodes/pages/NodeListPage.vue), [view mode](../web/src/app/viewMode.ts), and [guards](../web/src/app/guards.ts).

## 4. Administrative dashboard

The revised overview emphasizes operational state and actionable problems:

- Summary cards for nodes, proxy cores, effective users, and today's traffic.
- Outstanding issues and a node-status list.
- Node traffic totals, upload/download breakdown, and rankings.
- User quota usage and expiration.
- Recent access audit information visible to Owner.

Cards and lists have been reorganized for clearer hierarchy and alignment. Existing dashboard, node, user, and account-traffic APIs are reused. This document does not validate every aggregation or reporting definition. Unmeasured values should remain unknown rather than becoming artificial zeroes.

## 5. User management

### Product direction

Replace the fragmented user/credential experience with user-level management. The system maintains access credentials and hides routine device creation.

The UI work covers user creation/editing, enabled state, deletion, display information, node-group authorization, allowed protocols, quota, usage resets, expiration/renewal, subscription access/reset, password-related actions, speed limits, and reset-cycle fields.

Administrator actions also appear in the current user-management code. Ordinary proxy accounts and Owner/Viewer administrator accounts remain distinct identities and permissions.

### Integration status

| Capability | Static implementation observation |
| --- | --- |
| User details, groups, quota, and expiration | Existing management APIs can be reused; specific interactions still require acceptance |
| Manual traffic reset and renewal | Pages call existing operations |
| User-level subscription experience | UI has moved in this direction but still bridges through default devices and the old model |
| End-user login password reset | The examined detail-page handler rotates proxy credentials; this is not a login-password update |
| Upload/download speed limits and automatic reset cycle | Currently encoded into the remark field by the frontend; this does not implement enforcement or scheduled resets |
| Independent user login and repeatedly viewable shared link | Requires the authentication and backend work in the v1.1 plan |

Login passwords, subscription tokens, and proxy credentials must have distinct actions and descriptions. Changing a login password or link alone must not imply that an imported proxy configuration has been revoked.

Sources: [user list](../web/src/modules/users/pages/UserListPage.vue), [user detail](../web/src/modules/users/pages/UserDetailPage.vue), and [user API/metadata](../web/src/modules/users/api.ts).

## 6. Servers

The first-level server UI has been reorganized around readable status and management actions, with Agent and core entries moved into this area. Server-detail changes are partial; the complete feature set below is the agreed direction, not a statement that every tab is implemented.

| Area | Intended content |
| --- | --- |
| Identity | Name, hostname/address, IPv4/IPv6, region/purpose, remarks |
| State | Agent connectivity, core health, last heartbeat, configuration synchronization |
| Resources | CPU, memory, disk utilization and available capacity data |
| Network | Current upload/download rates and monthly totals, with scope stated |
| Versions | Agent/core versions, current configuration, and update results |
| Uptime | Explicitly identify host, Agent, or core uptime |
| Latency | Server-to-target latency, loss, test time, and retesting |
| Renewal | Date, cycle, price/currency, reminder interval, remarks |
| Operations | Installation command, editing, health checks, updates, restart, disable/delete |
| Tuning | Current settings, basic tuning, measured verification, task records, restoration |

Additional server metadata such as renewal and region is currently stored in browser storage. Probe targets, historical monitoring, and tuning require real collection and Agent execution capabilities. A display slot does not provide those capabilities.

Agent connectivity, core health, and proxy reachability are separate. A benchmark is also different from current network rate or the user's experienced latency.

Sources: [server list](../web/src/modules/nodes/pages/NodeListPage.vue), [server metadata](../web/src/modules/nodes/nodeMeta.ts), and [version panel](../web/src/modules/system/components/NodeVersionPanel.vue).

## 7. Node configuration and relay builder

### Unified node list

The page groups self-hosted entry points, subscription-imported nodes, and individually imported nodes. It presents source, address, protocol, actual state, and editing actions consistently.

Multiple schemes associated with an exit appear beneath that node in an expandable relay-line section. Each line identifies its path, execution location, and draft/published state. Editing opens the relevant canvas scheme and focuses the connection. Adding a relay preselects the node as the destination. Direct access and relay-only access are distinguished.

### Canvas and resource organization

The editor has a left resource library, a central canvas, a right properties panel, and a top scheme toolbar.

```text
Resources
├─ My servers
└─ Proxy nodes
   ├─ Self-hosted, grouped by server
   ├─ Subscription imports, grouped by source
   └─ Individual imports
```

Interactions include dragging resources, moving instances, zooming, panning, directed connections, property editing, saving/opening schemes, and deep links from the node list. Removing a canvas instance must not delete its source resource.

### Execution and tunnels

| Execution mode | Example | Intended application |
| --- | --- | --- |
| Client chain | Client → imported A → imported B → website | Generate a complete configuration supported by the target client |
| Server relay | Client → managed server → imported A → website | Generate and deploy the server-side configuration through the Agent |
| Server tunnel | Managed server A → managed server B | Deploy the necessary tunnel functionality on managed hosts |

Connections offer tunnel forwarding, port forwarding, or proxy chaining according to resource capabilities. Imported proxy nodes can be intermediate hops or exits; their presence does not authorize Agent installation or arbitrary listener/tunnel deployment on their servers.

Tunnel creation and editing happen through canvas connections. A tunnel list provides a consolidated view and a way to focus the relevant connection.

### Current boundary

`proxyNodes.ts` contains local persistence and seeded demo resources. This does not establish complete remote-source fetching, import parsing, probes, or chain compilation.

The examined `handleConfirmPublish()` only changes local publication flags and displays a success message. It does not deploy an Agent task or generate an actual client subscription. This is an interaction prototype until backend integration supplies real execution results.

Self-hosted inbound editing calls existing APIs and attempts revision creation. Creating a revision is not equivalent to deploying it; result confirmation remains necessary.

Sources: [node list](../web/src/modules/proxy/pages/ProxyNodeListPage.vue), [canvas](../web/src/modules/proxy/pages/RelayCanvasPage.vue), and [resource/scheme state](../web/src/modules/proxy/proxyNodes.ts).

## 8. Routing control

Website control is consolidated into Node Policies because both use the same policy mechanism. Service Rules becomes Rule Sets to include IP/CIDR content as well as domains.

| Page | Responsibility |
| --- | --- |
| Client Policies | Clash/Mihomo conditions, ordering, direct/reject, and node/group selection |
| Node Policies | Server-side user/node scopes, matches, outbounds, groups, and chains |
| Rule Sets | Reusable domains, suffixes, IPs, and CIDRs; matches without routing actions |
| Simulator | Separate client/server evaluation, showing matches, actions, and reasons |

Client and server policies are independent and reuse rule-set content. Client DIRECT originates on the user's device; server direct originates on the server.

Client policy delivery requires a complete configuration subscription. Rule-set delivery requires a compatible `rule-providers` resource. Loading nodes through `proxy-providers` does not load policy groups and routing rules.

**Current boundary:** client policies are stored in `localStorage`. Provider snippets point to an existing service-set API, which does not by itself establish compatible formatting, authorization, or updating for external clients. Management endpoints, policy storage, and subscription export must be integrated and checked separately.

Sources: [routing API/state](../web/src/modules/routing/api.ts), [client policies](../web/src/modules/routing/pages/ClientPolicyPage.vue), [node policies](../web/src/modules/routing/pages/RoutingRulePage.vue), [rule sets](../web/src/modules/routing/pages/ServiceRulePage.vue), and [simulator](../web/src/modules/routing/pages/PolicySimulatorPage.vue).

## 9. System management and publishing

Agent/core management moves into Servers. Backup, operation logs, and system settings remain. Client-rule editing moves toward Routing Control, while platform information, timezone, and retention retain their existing mechanisms.

The intended publishing workflow is:

```text
Create/edit self-hosted node → create/save and publish → inspect result
Canvas → validate → publish → inspect per-node results
Client scheme → generate complete subscription → client updates and applies
```

Snapshots, dependency ordering, deployment batches, and rollback remain backend responsibilities. Node detail and canvas views expose progress, errors, retry, history, and rollback.

Navigation changes are present, but complete business integration is not established. Removing the Configuration Versions menu must not remove revision machinery or strand publishing operations before replacement flows work.

Sources: [settings](../web/src/modules/system/pages/SettingsPage.vue) and [routes](../web/src/app/router.ts).

## 10. Integration and acceptance priorities

| Priority | Completion criterion |
| --- | --- |
| Identity and personal data | Resolve personal nodes, quota, and subscriptions from backend session ownership |
| User-level access | Complete account, authorization, subscription, and revocation workflows; keep hidden devices only as compatibility details |
| Actual publishing | Generate, deploy, and confirm real tasks from node/canvas actions; locate failures and support retry/rollback |
| Canvas/import persistence | Store resources, schemes, and lines with explicit backend ownership; distinguish demo resources |
| Client policies/rule sets | Deliver valid complete profiles and rule-set formats with working authentication and updating |
| Speed limits/reset cycles | Execute enforcement and scheduling beyond metadata storage |
| Server additions | Persist metadata centrally and establish actual metric/probe/tuning sources |
| Portal/admin isolation | Check shared layouts, components, routes, and data permissions |

Continue coordinating with [the v1.1 plan](v1.1-plan.md). The broader canvas and import requests need explicit scope alignment: that plan initially reserves import extension points rather than implementing the full import and visual-deployment system.

## 11. Modular architecture and boundaries

### 11.1 Architecture

qui is a **modular monolith**. Its central Go panel contains business modules, uses SQLite, and embeds the built frontend. Agents are separate programs on managed servers, controlling proxy cores and connecting to the panel through gRPC/mTLS.

Portal/admin separation does not require microservices. Browser requests enter the panel API, in-process modules cooperate through interfaces and application services, and Agents execute host-side changes.

### 11.2 Existing responsibilities

| Layer/module | Location | Responsibility |
| --- | --- | --- |
| Frontend application | `web/src/app/` | Routes, sessions, navigation guards, API helpers, SSE, time presentation |
| Frontend business modules | `web/src/modules/` | Business pages, interactions, and portal views |
| Shared UI | `web/src/components/` | Tables, dialogs, drawers, status, pagination |
| Authentication | `internal/panel/auth` | Current administrator authentication, sessions, permissions; end-user authentication is extended by v1.1 |
| Users | `internal/panel/user` | Accounts, grants, credentials, quota/access state, compatibility identities |
| Nodes | `internal/panel/node` | Enrollment, identity, heartbeat, capabilities, groups, deletion |
| Subscriptions | `internal/panel/subscription` | Subscription identity, authorized endpoint selection, client-format output |
| Proxy configuration | `internal/panel/proxy` | Inbounds, outbounds, groups, chains, snapshot construction |
| Policy | `internal/panel/policy` | Node policies, service sets, simulation; ownership of new client-policy/rule-set outputs must be explicit |
| Deployment | `internal/panel/deployment` | Revisions, tasks, dependency batches, results, rollback, updates |
| Records/overview | `internal/panel/access`, `connections`, `dashboard` | Traffic/access records, connection observations, read-only aggregation |
| System | `internal/panel/system` | Settings, backups, retention, related operations |
| Panel composition | `internal/platform/server` | HTTP/gRPC wiring and cross-module orchestration, including quota-triggered publishing |
| Infrastructure | Other `internal/platform/` packages | Database, configuration, encryption, certificates, locking, recovery |
| Agent | `internal/agent/` | Control channel, core adapters, reconciliation, supervision, collection/spooling, networking, updates |
| Shared contracts | `pkg/contracts/` | Desired state, messages, telemetry, and release structures |

Directories are evidence of modular organization, not proof that every boundary is complete. Table-owning modules should perform their own writes; cross-module workflows should use interfaces/application services. Dashboard aggregation must not become a second configuration-writing path.

### 11.3 Responsibilities for new features

| Feature | Frontend | Panel | Agent/client |
| --- | --- | --- | --- |
| User subscription | Forms, personal presentation, copy/import | Identity, authorization, credentials, subscription output | Client connects; Agent applies access configuration and collects observations |
| Canvas | Layout, instances, connections, property editing | Resolve resources, authorize, persist, validate, translate into existing configuration/deployment operations | Agent renders/applies server plans; client executes supported chains |
| Direct node publishing | Business action, progress, errors | Save configuration, construct snapshot, coordinate deployment | Validate/apply, report result, roll back where required |
| Shared rule sets | Content editing, references, output explanation | Persistence, versions, authorization, target formats | Update through supported client mechanisms or server publishing |
| Client policy | Match/order/group editing and preview | Maintain the global template and assemble full subscriptions | Target client executes routing |
| Monitoring/tuning | Show measurements and operation results | Persist metadata, coordinate requests, aggregate results | Agent measures or executes supported tasks |

Frontend input checks provide immediate feedback; backend authorization and configuration validation remain authoritative. Layout state can be stored separately from executable plans. A browser-local `published` field cannot confirm deployment.

### 11.4 Execution paths

```text
Server plan:
Page/canvas → business validation and persistence → configuration snapshot
→ Deployment coordination → Agent render/check/apply → reported result → UI

Client plan:
Policy/rule-set/chain editing → backend persistence and validation
→ complete subscription output → user updates client → client executes
```

Removing a menu changes the entry point, not module ownership. Generating a client profile also does not prove that every client has downloaded or applied it.

### 11.5 Isolation and lightweight implementation

- Reuse API helpers and foundational UI, while keeping portal/admin pages and layouts appropriately separated.
- Split large mode-dependent pages incrementally when touched; avoid unrelated rewrites.
- Treat `viewMode` as presentation, never authorization. Resolve personal ownership on the backend.
- Use `localStorage` for preferences or clearly labeled local drafts, not authoritative shared configuration or execution state.
- Reuse proxy, subscription, deployment, and Agent modules. Do not build a second deployment system inside the canvas.
- Prefer direct, readable logic and explicit contracts. Avoid generic engines, redundant validation, speculative compatibility layers, and excessive fallbacks. Test meaningful behavior changes proportionately; layout-only edits do not need large automated suites.

No builds, automated tests, or browser acceptance checks were run to produce this document. No application code was changed. This report records agreed behavior, static implementation observations, and remaining boundaries for continued collaboration.

Chinese counterpart: [前后台界面与业务交互更改说明](ui-change-summary-20261026.zh.md).
