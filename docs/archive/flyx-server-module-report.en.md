# flyx Server Module Review

**Review date:** October 26, 2026  
**Repository:** https://github.com/kure29/flyx  
**Reviewed commit:** `79af4975727cf58c24ea0db3715048ee2f502436`  
**Scope:** the “服务器” (Servers) section, its related UI components, backend handlers, and selected Agent/core-management code.

## Executive summary

flyx treats a server as an infrastructure resource that runs an Agent and one or more proxy cores. Proxy entry points are separate resources running on those cores. This distinction explains why its Servers section contains operational information rather than only protocols and subscription links.

The section combines five responsibilities:

1. Server inventory, addresses, and renewal reminders.
2. Live resources, network rates, monthly traffic, and historical monitoring.
3. Agent installation, upgrades, and rollback.
4. Proxy-core installation and lifecycle management.
5. Network diagnostics and optional host tuning.

For qui, the most useful ideas are the separation of infrastructure from client access, richer server cards, and clear operational actions. Federation, SSH-only forwarding, automatic tuning, and extensive monitoring are independent features with substantial backend work; they are not required to improve the current UI.

This report is based on source inspection. No flyx deployment, benchmark, or end-to-end operation was performed.

## 1. Navigation and resource model

The entry component, `node-hub.tsx`, presents a page titled **服务器** with two top-level tabs:

| Tab | Responsibility |
| --- | --- |
| Servers | Machines managed through Agents, operational state, monitoring, and management actions |
| Cores | Proxy cores installed on those machines |

The server list also distinguishes **local servers** from **remote servers**. “Local” means managed by this panel; it does not mean the machine running the browser. Remote records represent resources associated with another provider/panel and expose different usage information.

The model separates:

```text
Server
  └─ Installed proxy core
       └─ Proxy inbound / client entry point
```

`NodeKernel` has a uniqueness constraint on the server/core-type pair. A server can therefore have different core types represented independently, with separate version, enabled state, status, errors, and configuration hashes. This is broader than qui's current per-Agent core configuration and would require backend changes to reproduce fully.

References: [server hub][hub], [server types][types], [core and inbound models][models].

## 2. What the server list displays

The list supports card and row layouts. The same resource information is reused in desktop rows, narrow layouts, and cards rather than requiring a different operational model for each layout.

### Managed server information

| Category | Displayed information |
| --- | --- |
| Identity | Name, configured host/IP, IPv4/IPv6 addresses, and remarks |
| Connectivity | Agent online/offline state |
| Agent | Reported version and upgrade progress |
| Runtime | Reported uptime |
| Resources | CPU, memory, and disk usage |
| Network | Upload/download rates and monthly incoming/outgoing traffic |
| Line quality | Selected latency reference points, loss information, and return-route labels |
| Administration | Renewal schedule, reminder indicators, and management actions |

The `NodeLiveStats` component groups CPU, memory, and disk as compact meters, with network rates and monthly traffic below or beside them. Offline records are passed no live metric object, so the component can render unavailable measurements separately from available monthly totals.

The data conversion code also contains zero defaults for missing fields. Its presentation should not be copied as a guarantee that every numeric zero represents a real measurement. qui should retain its explicit unknown/unavailable semantics.

### Remote server information

Remote cards include provider URL, share ID, current flow, bandwidth limit, occupied ports, and tunnel/resource bindings. They also display synchronization errors such as a provider deleting, disabling, or expiring a share.

These are federation features, not ordinary attributes of a self-managed proxy node.

References: [server list][list], [live-stat component][stats], [system-info conversion][systeminfo], [server types][types].

## 3. Creation, editing, and organization

The server form includes:

- Name and remarks.
- Renewal cycle and a renewal anchor date.
- Hostname, IPv4, and IPv6 addresses.
- Available port settings.
- Forwarding mode.
- Per-server notification overrides.
- Advanced network options, including output interface/IP and TCP/UDP listener addresses.

The list provides search, renewal-related filtering, ordering, multiple selection, batch deletion, batch Agent upgrades, and batch latency-target selection.

Renewal metadata is an administrative reminder. It should not be confused with the expiration of an end user's proxy subscription. The UI supports monthly, quarterly, and yearly schedules and derives the next renewal date from the saved anchor.

### Two forwarding management modes

The form distinguishes:

| Mode | Control path |
| --- | --- |
| Agent | Install an Agent on the server; the Agent manages the relevant workload |
| nftables | Store SSH connection settings and apply forwarding rules through SSH |

For SSH-managed nftables mode, the form accepts a host, port, user, password/private-key authentication, optional key passphrase, and sudo mode. Its description explicitly distinguishes this pure-forwarding path from Agent functionality.

This SSH-only forwarding mode is separate from using nftables counters alongside an Agent to account for subscription traffic. Both involve nftables, but they solve different problems.

References: [server form][form], [renewal calculations][renewal], [server mutations][mutations].

## 4. Agent installation, upgrades, and rollback

The installation dialog offers a generated shell command or Docker command. The backend builds that command using the selected release, panel address, and server secret.

The Docker path uses host networking, a persistent Agent directory, a restart policy, and `NET_ADMIN` for features that need nftables. The documentation also describes Linux amd64/arm64 and Alpine/OpenRC installation. These are flyx's documented support claims; this review did not validate them on hardware.

The server page supports individual and batch Agent upgrades, version selection, progress display, and rollback to a previous Agent version. These actions are distinct from changing the installed proxy-core version.

The frontend success message for an upgrade says the command was sent. That should not be interpreted as independent proof that the replacement Agent has already restarted successfully.

References: [installation command handler][mutations], [Agent dialogs][agentdialogs], [Agent installation guide][agentguide].

## 5. Proxy-core management

The Cores tab loads capabilities, servers, and installed core records, and delegates operations to `KernelPanel`. The tab refreshes its data periodically.

The panel exposes:

- Install a supported core on a selected server.
- Select a version or change to another version.
- Start, stop, or restart a core.
- Inspect logs and restart-count information.
- Uninstall a core.
- In an uninstall failure case, offer forced removal of the panel record with deferred cleanup behavior described by the UI.

The backend model records requested and installed versions separately, alongside status, last error, desired configuration hash, and applied configuration hash. This is useful for distinguishing an intended change from what the server reports as applied.

The examined installer includes download bounds and SHA-256 parsing/verification machinery. This report does not establish checksum coverage for every supported core or release path.

References: [Cores tab][coretab], [core management UI][corepanel], [core model][models], [Agent core installer][installer].

## 6. Monitoring and traffic semantics

Opening a server's monitoring view replaces the visible list while keeping the list mounted underneath. The monitoring components expose current resource summaries, metric history, service-monitor configuration, result charts, and result records.

Metric types include CPU, memory, disk, load averages, TCP/UDP connection counts, uptime, network byte totals, and network rates. Service-monitor views include configurable checks, manual execution, and recent results, with separate backend permission endpoints.

Server network traffic must remain distinct from personal proxy usage:

- Server metrics describe host/network activity.
- Subscription accounting attributes traffic to authenticated subscriptions through its own mechanism.
- A server's TCP/UDP count does not identify physical client devices or active proxy users.

The monthly traffic endpoint aggregates server traffic from the beginning of the month using the viewer's supplied UTC offset. This is not automatically equivalent to an individual user's billing period or quota-reset date.

References: [monitoring view][monitor], [server metrics conversion][systeminfo], [monthly server traffic handler][monthtraffic], [route registration][routes].

## 7. Latency, packet loss, and return routes

flyx provides selectable reference targets, with global defaults and per-server overrides. The list can pin selected targets, and the diagnostic views provide retesting, history, loss information, and return-route details.

The documented and inspected design distinguishes two perspectives:

1. **Server-to-reference-point measurements:** useful for comparing server routes to selected regions or carriers.
2. **Panel-to-entry-point connectivity checks:** useful for confirming that a published TCP endpoint can be reached from the panel.

Neither measurement is the browser user's actual end-to-end proxy latency. UDP-only protocols also cannot be treated as verified by a TCP connectivity probe.

Return-route code uses hop addresses and ASN information to classify recognizable network paths. Labels such as CN2 or CMI are diagnostic classifications, not guarantees of service quality.

The documentation describes dual-stack handling, target-level history, periodic probing, and notifications for repeated degradation or route changes. These features require Agent, backend, storage, and UI support; adding labels to qui's frontend would not implement them.

References: [latency and route guide][latencyguide], [latency handlers][latency], [route handlers][route].

## 8. Host tuning

The server actions include a tuning dialog with status, automatic tuning, deeper optimization, cancellation, and restoration controls. The backend dispatches explicit commands such as `TuneStatus`, `TuneStart`, `TuneCancel`, and `TuneApply` to the Agent.

The documentation describes measured bandwidth tests, TCP/sysctl adjustments, and optional traffic-shaping scans. The UI exposes progress and logs, and deeper scans require an explicit traffic budget.

This is an operational subsystem that can consume substantial traffic and modify host networking. It should be evaluated independently of the server-page redesign. This review inspected the UI and command handler, not the complete Agent-side tuning implementation or recovery behavior.

References: [tuning dialog][tuneui], [tuning handler][tunehandler], [Agent guide][agentguide].

## 9. Realtime updates and backend boundaries

The server list uses a WebSocket realtime hook, delayed offline handling, reconnection, and a polling fallback. The Cores tab separately refreshes periodically. The API routes cover inventory mutations, installation commands, upgrades, rollback, tuning, latency targets/history, routes, and monthly traffic.

Useful API groups include:

| Group | Examples |
| --- | --- |
| Inventory | `/api/v1/node/list`, `/create`, `/update`, `/delete` |
| Agent lifecycle | `/api/v1/node/install`, `/upgrade`, `/batch-upgrade`, `/rollback` |
| Proxy cores | `/api/v1/kernel/list`, `/install`, `/versions`, `/control`, `/logs` |
| Diagnostics | `/api/v1/node/latency/probe`, `/latency/history`, `/route`, `/route/probe` |
| Operations | `/api/v1/node/tune`, `/api/v1/node/traffic/month` |

These are flyx routes, not proposed qui endpoints. qui should continue using its existing API wrappers and single SSE connection where appropriate rather than adopting an additional realtime transport solely to resemble flyx.

References: [realtime hook][realtime], [route registration][routes].

## 10. What qui should borrow

| Priority | Recommendation | Expected scope |
| --- | --- | --- |
| First | Separate the administrator's server view from the user's usable-node view | Navigation and terminology, with explicit permission boundaries |
| First | Show actual state, CPU/memory/disk, Agent/core versions, last heartbeat, and configuration synchronization | Reuse available data and current APIs |
| First | Group installation, settings, update, health-check, and deletion actions clearly | Reuse existing operations and their actual semantics |
| Next | Add network rates, monthly host traffic, remarks, and renewal reminders where useful | Check existing measurements; add backend fields/aggregation only where missing |
| Later | Add history charts and target-based latency/route diagnostics | New collection, storage, and Agent work |
| Separate decision | Add SSH-only forwarding, federation, multiple core types per Agent, or host tuning | Broader architecture and operational scope |

For qui's 1–10 internal users, a compact administrative server card could contain:

```text
Name · actual state
Address · Agent version · proxy core/version
CPU · memory · disk
Last heartbeat · configuration sync state
Network rate / monthly traffic, when measured
Details · settings · more actions
```

Client-facing node cards should instead emphasize usable entry points, supported access protocols, and connection/import actions. Agent online state, proxy-core health, and externally reachable service state should be represented separately when they differ.

## Review limits

- This is a feature and implementation report, not a complete security audit.
- No flyx services, installation commands, diagnostics, upgrades, or tuning tasks were executed.
- Documentation-only operational details are identified as documented behavior rather than field-tested results.
- qui's working-tree changes were preserved; this task added only this report and read-only reference material.

## Source references

All flyx links below refer to the reviewed commit.

[hub]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/node-hub.tsx
[list]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/node.tsx
[types]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/node/types.ts
[models]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/go-backend/internal/store/model/node_kernel.go
[stats]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/node/node-live-stats.tsx
[systeminfo]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/node/system-info.ts
[form]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/node/node-form-modal.tsx
[renewal]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/node/renewal.ts
[mutations]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/go-backend/internal/http/handler/node_mutations.go
[agentdialogs]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/node/agent-modals.tsx
[agentguide]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/doc/guide/agent.md
[coretab]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/node/kernels-tab.tsx
[corepanel]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/inbound/kernel-panel.tsx
[installer]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/go-gost/x/kernel/install.go
[monitor]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/node/monitor-view.tsx
[monthtraffic]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/go-backend/internal/http/handler/node_traffic.go
[latencyguide]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/doc/guide/latency.md
[latency]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/go-backend/internal/http/handler/node_latency.go
[route]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/go-backend/internal/http/handler/node_route.go
[tuneui]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/node/tune-modal.tsx
[tunehandler]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/go-backend/internal/http/handler/node_tune.go
[realtime]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/node/use-node-realtime.ts
[routes]: https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/go-backend/internal/http/handler/handler.go
