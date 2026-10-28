# flyx Pages: A Concise Reference for qui

**Date:** October 26, 2026  
**Source snapshot:** [flyx `79af497`](https://github.com/kure29/flyx/tree/79af4975727cf58c24ea0db3715048ee2f502436)  
**Method:** source review of the route map, navigation, page components, and relevant tab components. No browser walkthrough or runtime verification was performed.

## Summary

The strongest reference is flyx's division of responsibilities: infrastructure, proxy entry points, relay operations, subscription distribution, and personal access have different pages. Related secondary features sit in tabs or dialogs rather than all becoming sidebar items.

For qui, borrow that organization and its practical summaries. Preserve qui's configuration publishing, rollback, permissions, and accounting definitions. A fuller-looking page should come from useful information, not extra infrastructure features.

## Page inventory and reusable ideas

This inventory covers the routed pages and their main nested areas. Tabs and dialogs are grouped under their parent page; it is not an inventory of every form field.

| Page / area | What flyx presents | What qui can borrow |
| --- | --- | --- |
| Login | Account login and optional alternative sign-in flows | A clear, compact sign-in experience; add alternative methods only if needed |
| Administrator dashboard | Servers online, nodes running, relay lines, valid subscriptions, issues needing attention, server status, node traffic, and upcoming expirations | Put actionable problems beside a few meaningful summary cards |
| User dashboard | Personal forwarding allowances and usage, rule counts, authorized tunnel information, announcements, and activity summaries | Show only the user's own entitlement and usage; adapt to qui's proxy-access model |
| Servers | Agent state, addresses, versions, uptime, resources, network rates, monthly traffic, renewal reminders, and management actions | Make server health and configuration state readable at a glance |
| Cores, under Servers | Install, select/change version, start, stop, restart, logs, and uninstall | Clearly separate Agent updates, core updates, and configuration publishing |
| Nodes | Managed proxy inbounds and imported entries, protocol/address details, state, sharing, credentials, and configuration actions | A concise entry-point list with accurate availability and connection details |
| Sources, under Nodes | Remote subscription URLs or uploaded configurations, refresh settings, last update, and synchronization results | When import is implemented, make source freshness and failures visible |
| Node configuration dialogs | Protocol presets, transport/security, relay setup, and server-side routing controls | Use simple defaults and reveal advanced fields only when relevant |
| Relay: Lines | Relay paths grouped by their destination/landing, with status and probe information | Present existing qui relay/chain relationships in a readable path view |
| Relay: Tunnels | Entry-to-exit forwarding infrastructure, deployment and diagnostic operations | Keep infrastructure routes separate from end-user proxy entry points |
| Relay: Rules / standalone Forward page | Forward an incoming port through a tunnel to a target, with status, usage, batch operations, and diagnostics | Borrow clear source → path → target summaries, not an entire new forwarding subsystem |
| Relay: Quality | Historical tunnel latency, throughput, and errors | Add only after suitable measurements and storage exist |
| Subscription administration | Independent subscriptions, credentials, expiry, quota, resets, node-group selection, processing/preview, client links, and templates | A short create → assign → copy/import workflow and transparent quota/expiry status |
| Subscription node groups / templates | Reusable node selection, naming/filtering, client profile templates, and routing options | Reuse node groups; keep qui's initial rule-template design simple |
| My Subscriptions | Assigned subscriptions, node counts, traffic/limit, expiry/reset information, client import, links, QR codes, history, and link reset | The strongest reference for qui's personal access page |
| Users | Login accounts, enabled state, forwarding allowances, expiry, tunnel grants, and subscription assignment | Make account ownership explicit; distinguish login identity from access entitlements |
| User Groups / Tunnel Groups / Grants | Group users and tunnels, then grant tunnel access by group | Reuse qui's node-group authorization; these are not the same as subscription node groups |
| Speed Limits | Reusable bandwidth-limit rules used when granting tunnels | Keep out of scope unless bandwidth enforcement becomes a requirement |
| Monitoring | Resource history, current measurements, service checks, results, and monitoring permissions | Link from a server summary into detail; start with existing telemetry |
| Panel Sharing | Share local server resources with another panel, import remote shares, and show limits/bindings | Defer federation for qui's current internal-user scale |
| Settings | Basic configuration, appearance, security/login, server monitoring, probe targets, notifications, announcements, backup/restore, and upgrades | Organize settings by purpose and show a short summary before expanding |
| Account and Login | Account details, password changes, GitHub association, and passkeys | A single account-security entry, with only supported methods |
| Password / GitHub callback routes | Focused password-change flow and authentication callback handling | Preserve clear flow boundaries; callbacks are not navigation destinations |
| Mobile layouts / More | Alternate navigation and access to secondary/account actions | Keep primary actions reachable on narrow screens without duplicating business logic |

## Distinctions to retain

- **A server is not an entry point.** Agent online state, core health, and client reachability can differ.
- **Forwarding rules are not website-routing rules.** flyx's Forward page configures port/tunnel forwarding; website/client routing appears elsewhere.
- **Host traffic is not personal usage.** Label the scope and reporting period rather than reusing one number across pages.
- **A shared subscription does not identify physical devices.** Avoid promising exact device names, counts, or per-device usage without a distinct identity source.
- **Similar labels may describe different permissions.** flyx's tunnel groups/grants should not be copied into qui as though they were existing proxy node-group authorization.

## Recommended adaptation for qui

### First: improve the existing experience

1. **Admin overview:** a few summary cards, actual server state, recent configuration results, and issues that link to their resolution page.
2. **Admin servers/nodes:** resources, Agent/core versions, last heartbeat, configuration synchronization, and existing management actions.
3. **Personal overview:** the authenticated user's quota usage, expiry, access state, and subscription shortcut.
4. **Available nodes:** authorized entry points and supported connection protocols, with accurate status.
5. **Subscription access:** copy link, QR code, supported client import, and clear reset behavior.
6. **Settings:** grouped sections using the existing components and supported backend fields.

These are page responsibilities, not a requirement to add six independent navigation items. The personal overview and subscription access can share one compact page.

### Next: only where the backend already supports it

- User traffic history and recent activity with explicit scope.
- Clear relay/chain visualization.
- Richer configuration errors and task progress.
- Additional metadata or monitoring only where it solves a current operational need.

### Defer

Federation, a full external-source processing pipeline, many independent client templates, host tuning, bandwidth shaping, and extensive diagnostics would add substantially more than a UI improvement.

## Alignment with the current qui plan

The working tree now contains [the v1.1 implementation plan](v1.1-plan.md), which records user accounts, one shared subscription per user, user-level permissions/quotas/expiry, and sing-box builds from unmodified upstream source with `with_v2ray_api` enabled.

Use flyx as a reference for the personal subscription workflow and page organization. Do not reopen the accounting choice by adding nftables metering merely to match flyx; the current plan records it as an alternative that was considered but not selected. Device-management UI from earlier discussions should likewise follow the newer user-level access decisions.

Implementation should stay incremental: reuse existing Vue components and API helpers, introduce no new framework for visual similarity, and validate only the pages and behaviors actually changed.

## Source index

All references below are pinned to the reviewed flyx snapshot.

- [Routes](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/App.tsx), [desktop navigation](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/layouts/admin.tsx)
- [Login](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/index.tsx), [dashboard](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/dashboard.tsx), [admin overview](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/dashboard/admin-overview.tsx)
- [Servers hub](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/node-hub.tsx), [server list](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/node.tsx), [core management](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/inbound/kernel-panel.tsx)
- [Nodes](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/inbound.tsx), [external sources](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/inbound/sources.tsx)
- [Relay hub](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/relay.tsx), [tunnels](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/tunnel.tsx), [forwarding rules](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/forward.tsx)
- [Subscription administration](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/subscription.tsx), [My Subscriptions](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/my-subscriptions.tsx)
- [Users hub](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/user-hub.tsx), [users](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/user.tsx), [groups](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/group.tsx), [speed limits](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/limit.tsx)
- [Monitoring](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/monitor.tsx), [monitoring views](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/node/monitor-view.tsx)
- [Panel sharing](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/panel-sharing.tsx), [settings](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/config.tsx), [account and login](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/profile.tsx)

For deeper detail, see the existing [architecture comparison](flyx-comparison-report.en.md) and [server-module review](flyx-server-module-report.en.md). This report adds no application changes.
