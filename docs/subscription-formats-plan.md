# Subscription formats plan (draft, 2026-10-26)

> **Superseded** by [v1.1-plan.md](v1.1-plan.md) (2026-10-26). Kept for history only; do not implement from this file.

Status: draft for Owner review. Nothing here is implemented yet.

Goal:
- Rule-based subscriptions for the major Clash variants.
- Correct universal formats.
- A subscription layer where a new client format is added as one self-contained exporter, without touching the service, handler, settings or other formats.

Related:
- `flyx-comparison-report.en.md` (evaluated in section 1).
- `user-level-access-plan.md` (which endpoints a user gets). That plan and this one are independent: exporters consume the same `[]Endpoint` either way.

## 1. Evaluation of the flyx comparison report

| Report finding | Assessment | Effect on qui |
| --- | --- | --- |
| §1 One default subscription per user; keep qui's shared user quota | Agree | Already decided as U1 in `user-level-access-plan.md`. Multiple subscriptions per user and per-subscription quotas are not planned. |
| §2 sing-box per-user accounting is possible with **official** binaries via `auth_user` routing to per-user outbounds with `routing_mark`, plus nftables counters | The correction is right: official binaries don't rule it out. For qui it's still the costlier route. (1) Every route target (policy outbounds, egress groups, relay and chain hops) would need a per-user clone carrying the mark, so the config grows with users × targets. Phase 6 of the refactor rejected the Clash API variant (D5 option 2) for this same reason. (2) Marks must survive relays and chains, which interacts with the existing host-network helper. (3) It counts network bytes after protocol overhead, which differs from Xray's per-user counters. (4) It is Linux-only and needs new privileged code. | Keep U2 (our own build of unmodified source with `with_v2ray_api`). The Agent reads both cores' counters the same way and needs no new privileges. Record the nftables route as the fallback if U2 becomes untenable. |
| §3 Portal identity: the in-progress portal matches the login name to a proxy user and falls back to the first user; the node page treats an admin ID as a proxy-user ID | Confirmed: `ClientAccessPage.vue` imports the admin `userAPI`. | Resolved by U3: end users log in as their proxy user, and every portal endpoint takes the identity from the session. The Owner/Viewer "portal mode" (`viewMode.ts`) should be removed, not fixed. |
| §4 Don't copy flyx's frontend stack or forwarding subsystem | Agree | No new frontend dependencies. flyx is GPL-3.0, so no code or templates are copied; this plan is an independent design. |
| §5 Recommended sequence | Matches the user-level plan, except item 4, sing-box accounting (handled by U2 instead). | — |

The report doesn't cover client formats. Observations from the code relevant here:
- `internal/panel/subscription/export.go` builds all three formats with `if format == …` branches in one 306-line function. The format list is duplicated in `system/runtime_policy.go` validation and in the handler's Content-Type switch.
- The `sing-box` format outputs only `outbounds` and `endpoints`, a fragment. The official sing-box apps can't import it as a remote profile, which needs inbounds and route. It is fine for tools that merge outbounds, but it isn't a full client profile.
- The uncommitted `ClashRulePanel.vue`:
  - stores its template in `localStorage`, so nothing reaches `/sub/{token}`;
  - builds YAML by string concatenation, so names containing quotes, colons or newlines produce broken YAML.

  Its rule and group editing UX is worth keeping; the storage and rendering move to the server.

## 2. Scope

**In scope**

| Format ID | Aliases | Target clients | Rules |
| --- | --- | --- | --- |
| `clash` | `mihomo`, `clash-meta` | mihomo core: Clash Verge Rev, FlClash, Clash Meta for Android, Clash Party, ClashX Meta | Yes |
| `clash-legacy` | `clash-premium` | Discontinued original Clash / Clash Premium clients (Clash for Windows, ClashX) | Yes; protocols they can't parse are left out |
| `uri` | — | Universal share links (v2rayN, v2rayNG, NekoBox, Hiddify, Shadowrocket) | No |
| `base64` | — | Same, Base64-wrapped | No |
| `sing-box` | — | Existing outbounds fragment, unchanged for compatibility | No |

Also:
- `subscription-userinfo`, `profile-update-interval` and `Content-Disposition` headers, for usage, expiry and the profile name in clients.
- User-Agent based format selection when `?format=` is absent.

**Later, each as a single exporter (not in this plan's phases):**
- `sing-box-profile`: a full sing-box profile with rules (optional phase 6 here);
- Stash, Surge, Quantumult X, Loon.

**Out of scope, to keep it small:**
- Template scripting or JS hooks (Sub-Store style).
- Arbitrary YAML/JSON passthrough or overrides.
- Importing remote templates.
- **Implementing** external subscription import. The interfaces it plugs into are reserved in 3.3 so it can be added later without refactoring; no tables, API, UI or fetcher are built now.
- Per-user or per-group rule templates. One global template for now.
- Mirroring server-side policies into client rules: node policies stay authoritative, and client rules only decide proxy vs. direct and which group.

## 3. Architecture

### 3.1 Package layout

```text
internal/panel/subscription/
├─ service.go          token → user → Profile (asks every registered source, merges)
├─ handler_http.go     format resolution, headers; no per-format branches
├─ proxy/
│  └─ proxy.go         format-neutral client proxy model (Proxy, TLS, Transport, options)
├─ source/
│  ├─ source.go        Source interface, Viewer, registry
│  └─ published/       qui's own nodes: published state → []proxy.Proxy (today's Endpoints logic)
├─ format/
│  ├─ format.go        Exporter (+ reserved Parser) interfaces, Profile, Warning, registry, UA resolution
│  ├─ uri/             uri + base64 (moved from export.go, byte-identical output)
│  ├─ singbox/         existing fragment (moved, byte-identical)
│  └─ clash/           mihomo + legacy profiles (one renderer, two capability sets)
└─ rules/
   ├─ model.go         format-neutral rule template (typed, validated)
   ├─ presets.go       built-in presets (embedded Go data)
   └─ store.go         system_settings persistence with revision (like runtime_policy)
```

`proxy/` and `format/` have no dependency on `system`, `user`, `desiredstate` or HTTP. Only `source/published` knows qui's internal types (`user.Credential`, `desiredstate.ProtocolSettings`) and converts them to `proxy.Proxy`. `system` validates the default format through a `func(string) bool` injected at wiring, so there is no import cycle.

### 3.2 Exporter contract

```go
type Profile struct {
    Title   string          // shown as profile name (Content-Disposition)
    Usage   Usage           // used, limit, expiresAt → subscription-userinfo
    Proxies []proxy.Proxy   // from all sources, in source order
    Rules   *rules.Template // nil for formats without rules
}

type Exporter interface {
    ID() string
    Aliases() []string
    ContentType() string
    UserAgents() []string          // lowercase substrings, checked in registry order
    UsesRules() bool
    Render(Profile) (Output, error) // Output{Body []byte, Warnings []Warning}
}
```

Each exporter does its own capability filtering. An endpoint it can't express (protocol, transport, pinned certificate) is left out with a `Warning` rather than failing the whole subscription. This generalizes the existing URI rule for pinned Hysteria2/TUIC endpoints. Warnings never reach the unauthenticated `/sub` caller; the admin preview shows them.

**Adding a format** means one package implementing `Exporter`, one `Register` call, and golden tests. The handler, settings, admin preview and format picker in the UI read the registry, so nothing else changes.

### 3.3 Proxy sources and the reserved import interface

Exporters never see where a proxy came from. Today there is one source, qui's own published nodes. External subscription import will later be a second source plus parsers. Neither needs changes to exporters, the handler or the rule template.

**Neutral proxy model (`proxy/`).** It describes what a client needs, not how qui stores it:
- origin: `{Source, Ref, Labels}`;
- name and server/port;
- protocol;
- credential fields;
- TLS: SNI, ALPN, pinned CA, fingerprint, insecure flag;
- transport: ws/grpc/quic/h2 options;
- QUIC tuning.

New protocols are added as new `Protocol` values with optional typed option structs. Exporters that don't know a protocol leave it out with a warning, which the capability rule already requires. Adding VMess, VLESS Reality and similar for imported sources therefore doesn't change any interface.

**Source contract (implemented now, one implementation):**

```go
type Viewer struct {           // who the profile is for
    UserID     string
    NodeGroups []Group         // granted, subscription-visible
}

type Source interface {
    ID() string                                     // "qui", later "external:<id>"
    Proxies(ctx context.Context, v Viewer) ([]proxy.Proxy, []Warning, error)
}
```

- `service.go` builds the `Viewer` from the token, asks every registered source, and merges the results.
- Name collisions are resolved after the merge, with qui's own nodes taking precedence for unsuffixed names.
- A failing source contributes nothing plus a warning; it never fails the whole subscription. For the `published` source this keeps today's behavior.
- Labels carry `nodeGroup:<id>` for qui nodes, so the template's `@nodeGroup:` expansion works on labels rather than qui types.

**Reserved, not implemented:**

| Reserved item | Purpose later | What exists now |
| --- | --- | --- |
| `format.Parser` interface: `Parse([]byte) ([]proxy.Proxy, []Warning, error)` and `Detect([]byte) bool` | Read a fetched external subscription in a known format (uri/base64, Clash YAML, sing-box JSON) into the neutral model, symmetric to `Exporter` | Interface and doc comment only. Formats may implement it later; the registry already lists parsers with zero entries |
| `source.Register` for additional sources | An `external` source backed by stored, periodically refreshed imports | Registry with the single `published` source |
| Template member `@source:<id>` | Groups such as "provider X nodes" | Grammar reserved: validation rejects it with "not available yet", so templates written now stay valid later |
| `proxy.Origin.Source` | Per-source naming, grouping, admin preview attribution | Always `qui` |

**Constraints for the future import feature.** These are recorded now so the reservation doesn't preclude them; none are built:
- Imported proxies are **client-side only**. They are never sent to Agents, never enter config revisions or node policies, and carry no qui quota accounting. The plan says so in the UI and docs when the feature lands.
- Fetching runs on a schedule in the panel, never during a `/sub` request:
  - `https` only;
  - size and time limits;
  - no redirects to private addresses (SSRF);
  - the stored copy is encrypted with the vault, because it contains third-party credentials.
- Parsed content is untrusted input: typed parsing only, no passthrough of unknown fields into output.
- Access is decided per user: which users may receive which import is an explicit grant, like node groups.

These need a migration (import sources, grants, cached content) and an Owner API at that time. The interfaces above don't change.

### 3.4 Format resolution (`GET /sub/{token}`)

1. If `?format=` is present and names a registered ID or alias, use it. An unknown value returns 400, as today.
2. Otherwise, match the User-Agent against the registered exporters' substrings in a fixed order. Specific patterns come first: legacy Clash UAs (`clashforwindows`, `clashx/` without `meta`) go to `clash-legacy` before the generic `clash`/`mihomo` → `clash`.
3. Otherwise, use the Owner's default format (existing runtime setting, now validated against the registry).

`?format=` always wins. The UA table is data inside each exporter, tested with recorded real UAs from Phase 0.

### 3.5 Format-neutral rule template

Typed, validated, and stored as one `system_settings` row (`client_rules`) with an optimistic `revision`, like `runtime_policy`. **No migration is needed.**

```text
Template
├─ client: mixedPort, allowLan, mode(rule|global|direct), logLevel, ipv6
├─ dns:    enable, enhancedMode(fake-ip|redir-host), nameservers[], fallback[]   (validated IP/URL)
├─ groups[]: name, type(select|url-test|fallback), members[]
│     member = DIRECT | REJECT | <group name> | @all | @nodeGroup:<id> | @source:<id> (reserved, rejected for now)
│     url-test/fallback: testUrl, intervalSeconds
├─ providers[]: name, behavior(domain|ipcidr|classical), sources{clash: url, sing-box: url}, intervalSeconds
└─ rules[] (ordered; last must be MATCH):
      type: DOMAIN | DOMAIN-SUFFIX | DOMAIN-KEYWORD | IP-CIDR | IP-CIDR6 | GEOIP | GEOSITE | RULE-SET | MATCH
      payload (validated per type), target (DIRECT | REJECT | group name), noResolve
```

- `@all` and `@nodeGroup:<id>` expand per user, through the proxies' origin labels (3.3). `@all` means every proxy in the profile; once external sources exist it includes them, and `@source:qui` selects only qui's own nodes. One template therefore yields a user-specific group such as "日本节点" (Japan nodes) from the user's granted node groups. An empty expansion falls back to `DIRECT` with a warning, never to an empty group (Clash rejects those).
- Validation:
  - unique names; targets exist; no group cycles; exactly one final `MATCH`;
  - domains, CIDRs and country codes are syntactically checked;
  - provider URLs must be `https`;
  - limits: 64 groups, 2,000 rules, 32 providers.
- Presets from the draft panel (经典分流 standard split, 基础代理 basic proxy, 分流+去广告 split plus ad blocking, 白名单 whitelist) are built-in Go data. "Apply preset" replaces the editable template. Presets are not live-linked.
- Exporters map each rule type through their own capability table. An unsupported rule is dropped with a warning; its target is never changed silently. Expected gaps, to be confirmed in Phase 0: `GEOSITE` on `clash-legacy`; `GEOIP` and `GEOSITE` on sing-box, which since 1.12 needs `rule_set` providers.

### 3.6 Clash renderer

- Rendering uses `gopkg.in/yaml.v3` (already in `go.mod`) with typed structs. No string concatenation.
- Proxy names are `<node name> <protocol>`, deduplicated with a numeric suffix and stable across fetches. Names are escaped by the encoder.

| qui endpoint | `clash` (mihomo) | `clash-legacy` |
| --- | --- | --- |
| http, socks5 | `http`, `socks5` (+ tls/sni) | same |
| shadowsocks | `ss` (method) | same |
| trojan | `trojan` (sni, ws/grpc opts) | same |
| vless (+flow, ws/grpc, tls) | `vless` | left out, with a warning |
| hysteria2, tuic | `hysteria2`, `tuic`; pinned CA via mihomo's CA/fingerprint option if Phase 0 confirms it, else left out like URI | left out |
| wireguard | `wireguard` | left out (to confirm in Phase 0) |

- Top level: `mixed-port`, `allow-lan`, `mode`, `log-level`, `ipv6`, `dns`, `proxies`, `proxy-groups`, `rule-providers`, `rules`.
- No `external-controller` or `secret`: clients manage their own controller. No TUN block: client-side choice.

### 3.7 HTTP headers

| Header | Value |
| --- | --- |
| `Content-Type` | from the exporter (`text/yaml; charset=utf-8` for Clash) |
| `Content-Disposition` | `attachment; filename*=UTF-8''<platform name>` |
| `subscription-userinfo` | `upload=0; download=<used>; total=<limit or 0>; expire=<unix or 0>` (qui tracks one combined total) |
| `profile-update-interval` | hours, Owner setting in the subscription settings (default 12) |
| existing | `Cache-Control: no-store`, `Referrer-Policy: no-referrer` |

## 4. Phases

Working rules as in `user-level-access-plan.md` §3:
- Cline implements; one commit per step.
- No edits to existing migrations, `go.mod` or `go.sum`.
- Gate: build, vet, `go test -p 1 ./...`, web build and type check.
- Commit, push and release only on the Owner's request.

### Phase F0 — Probes (no product code)

1. Run the current stable mihomo core binary (official release, scratch directory on the panel host, no service) with `-t` (config test) on hand-written samples covering every row of the table in 3.6. Confirm:
   - field names, including the pinned-certificate option for hysteria2/tuic;
   - `GEOSITE`/`GEOIP` handling;
   - `rule-providers` formats.
2. Record real User-Agent strings for:
   - mihomo clients: Clash Verge Rev, FlClash, Clash Meta for Android, Clash Party;
   - Shadowrocket, v2rayN and the sing-box apps;

   from upstream source or docs, and from a local fetch log where a client is at hand.
3. Determine which of `vless`/`wireguard`/`GEOSITE` the original Clash Premium accepted (from archived docs; the binary is no longer published).

**Stop and report** if mihomo can't express an endpoint type that qui serves today, other than the pinned-certificate case.

Acceptance: findings in section 6 of this file (commands, outputs, UA table), no secrets.

### Phase F1 — Proxy model, sources and exporter registry (refactor, no behavior change)

Changes:
- Add `proxy/proxy.go` (neutral model), `source/source.go` (Source, Viewer, registry), and `format/format.go` (Exporter, the reserved Parser interface, registry).
- Move today's endpoint selection into `source/published`, which converts published endpoints to `proxy.Proxy` with origin labels.
- Move uri/base64/sing-box into `format/uri` and `format/singbox`, rendering from `proxy.Proxy`.
- The handler resolves the format through the registry. Content-Type comes from the exporter, and the headers from 3.7 are added.
- Runtime-policy validation and the admin settings dropdown use the registry list, via a new `GET /api/v1/subscription-formats` (Owner and Viewer).
- UA resolution as in 3.4.

Acceptance:
- AF1.1 Golden tests: the uri/base64/sing-box output is byte-identical to the pre-refactor output for the existing fixtures (WireGuard, QUIC pinned, ws/grpc, Shadowsocks).
- AF1.2 A test-only exporter registered in a test makes `/sub/{token}?format=test` work and appear in the format list, with no other code change: the extensibility contract.
- AF1.3 UA table tests: recorded UAs map as specified; `?format=` overrides the UA; an unknown UA falls back to the Owner default; an unknown `format` returns 400.
- AF1.4 `subscription-userinfo` matches the user's used, limit and expiry, including unlimited (`total=0`) and no expiry (`expire=0`).
- AF1.5 Import-readiness contract:
  - A test-only `Source` registered in a test adds proxies that appear in every format, after qui's own nodes, with collision-safe names.
  - A source that returns an error leaves the rest of the subscription intact and produces a preview warning.
  - No exporter, handler or template code changes for this test.
- AF1.6 Dependency check: `proxy/` and `format/` import nothing from `internal/panel/user`, `pkg/contracts/desiredstate`, `system` or `net/http` (`go list -deps` test).
- AF1.7 No dead feature code: the `Parser` interface has no implementations and no stubs, and there are no import tables, routes or settings (grep).

### Phase F2 — Rule template model, storage and admin API

Changes:
- `rules/` model, validation, presets, `system_settings` store with revision.
- `GET/PUT /api/v1/subscription-rules` (Owner; Viewer read-only), audited.
- `GET /api/v1/subscription-rules/presets`.
- `GET /api/v1/users/{id}/subscription-preview?format=` returns the rendered body (Owner only; credentials are already visible to the Owner there) and the warnings.
- `source/published` sets `nodeGroup:<id>` origin labels (with the group names) for `@nodeGroup:` expansion.
- Template validation accepts the `@source:` grammar but rejects every value with "not available yet". `@source:qui` stays rejected until a second source exists, so templates can't depend on it prematurely.
- Backup and restore carry the setting row (no verifier change expected; to be confirmed by test).

Acceptance:
- AF2.1 Validation tests for every rule in 3.5, including cycle detection, MATCH placement, limits, bad CIDR/domain/country, and non-https providers.
- AF2.2 A concurrent PUT with a stale revision returns 409 (same pattern as runtime settings).
- AF2.3 Template survives a backup and restore round trip.
- AF2.4 `@nodeGroup` expansion uses only the user's granted, subscription-visible nodes. A user without the group gets `DIRECT` plus a warning, never another user's nodes.

### Phase F3 — Clash exporters

Changes:
- `format/clash` with the `clash` and `clash-legacy` capability sets, as in 3.6.
- Rule and provider mapping and per-format rule capability tables.
- Pinned-certificate handling per F0.

Acceptance:
- AF3.1 Golden tests for every protocol × transport × TLS combination qui supports, for both profiles. Names with quotes, colons, `#`, newlines and CJK round-trip through a YAML parse.
- AF3.2 CI `cores` job: the generated configs (all fixtures and every built-in preset) pass `mihomo -t` with the pinned official mihomo release, downloaded and digest-checked like Xray and sing-box.
- AF3.3 Legacy profile output contains no vless/hysteria2/tuic (and whatever else F0 rules out), and still has a valid group graph when that removes every proxy (falls back to `DIRECT`, warning).
- AF3.4 Real client test on the hosts:
  - run official mihomo from a scratch dir on the panel host with the subscription fetched from the panel;
  - through its mixed port, `api.ipify.org` routed to a group returns the uk-node exit, and a `DIRECT` rule returns the panel host's own IP;
  - a `REJECT` rule fails;
  - Hysteria2/TUIC behave per F0.

  No services are touched, and the scratch directory is removed afterwards.

### Phase F4 — Admin UI

Changes:
- `ClashRulePanel.vue` loads and saves through `/api/v1/subscription-rules` (no `localStorage`). It keeps the preset, group and rule editing UX and adds member pickers for `@all` and `@nodeGroup`.
- The preview comes from the server for a chosen user and format, with warnings listed.
- The "订阅地址" (subscription address) dialog lists links from the format registry, including Clash, with copy and QR (`qrcode.ts`). It also offers a `clash://install-config?url=` import link for the Clash format, if F0 confirms client support.

Acceptance:
- AF4.1 Web build and type check pass. No YAML is built in the browser (grep: the panel only displays server output).
- AF4.2 Saving with a stale revision shows the conflict and reloads. Server-side validation messages are shown per field.

### Phase F5 — Docs and field test

Changes:
- AGENTS.md change log: §10.3 formats, §27.10 endpoints and headers, §7 settings menu.
- `architecture.md` (subscription layer).
- `operations.md` (rule template, UA selection, troubleshooting a client that rejects a profile).
- `protocol-support-matrix.md` (per-format columns).
- `v1-acceptance.md` new section.

Acceptance:
- AF5.1 Field test with the release build on the three hosts, after Owner approval of the release:
  - upgrade;
  - the existing alice link in uri/base64/sing-box is unchanged;
  - the Clash link works in mihomo (as in AF3.4) and shows usage and expiry from the headers;
  - a template change shows on the next fetch without any node revision;
  - coexisting services untouched.

### Phase F6 (optional) — `sing-box-profile`

A full sing-box client profile (mixed inbound, outbounds, `selector`/`urltest` groups, route rules via `rule_set`) from the same template. It is the first proof that a second rule-based format needs only a new exporter. Validated with `sing-box check` in CI. The existing `sing-box` fragment format stays.

## 5. Open decisions

| ID | Question | Default in this plan |
| --- | --- | --- |
| S1 | The generic `clash` User-Agent (not matching a known legacy client) | Serve mihomo (`clash`). Legacy only for recognized legacy UAs or `?format=clash-legacy`. |
| S2 | Template scope | One global template now; per-user or per-group overrides later if needed. |
| S3 | Order relative to `user-level-access-plan.md` | Formats first: no migration and no Agent change, and they unblock the current UI work. The user-level phases follow. |
| S4 | Phase F6 | Not included unless requested. |
| S5 | External subscription import | Interfaces reserved (3.3); implementation is a separate future plan with its own migration, Owner API, fetcher and per-user grants. |

## 6. Phase F0 findings

(to be filled)
