# Backend Development Plan for the Client Policy UI

Status: approved with the amendments in §13 (November 4, 2026); Phases A–D implemented, remaining items in §14. Review date: November 4, 2026, America/Los_Angeles.

English translation of [the Chinese plan](client-policy-backend-plan.md). This translation preserves the original scope and recommendations; it does not introduce additional implementation commitments.

Review baseline: branch `feat/v1.4-tunnels`, HEAD `8134314`, database migration head `0131`, including the local, uncommitted Client Policy UI changes reviewed at that time. Existing behavior and proposed behavior are distinguished throughout this document.

Sources:

- `ClientPolicyPage.vue`, `routing/api.ts`, and `web/docs/client-policy-change-log.md`, through iteration 33 at the review baseline.
- This conversation's conclusions about client runtime settings, subscriptions, and node policies.
- The responsibilities, purpose-group → region-group structure, and navigation discussed in [Introduction to Clash Configuration](codeg://session/384).
- `internal/panel/subscription/`, `internal/panel/policy/`, and the applicable AGENTS.md.

The original review consisted of source inspection, frontend static checks, and planning. It changed no frontend or backend behavior. No authenticated browser walkthrough was performed. Conceptual examples from the referenced conversation are not treated as existing backend capabilities.

## 1. Recommended Product Definition

Keep the current **profile cards → six editing modules** interface and define the feature as **client subscription configuration template management**. Allow multiple saved profiles, with exactly one globally active profile. Per-user profile assignment is outside the initial scope.

The six modules are Settings, Proxy Groups, Routing Rules, DNS, Rule Sets, and YAML Preview.

The three main modules have these responsibilities:

| Module | Responsibility | Example |
| --- | --- | --- |
| Rule Sets | Domain, suffix, and IP lists that identify traffic, or external provider definitions | OpenAI rule set |
| Routing Rules | Match conditions, destinations, and evaluation order | OpenAI rule set → AI Services |
| Proxy Groups | Candidate endpoints or other groups, and the selection method | AI Services → US Group → an authorized endpoint in US Group |

Purpose groups and region groups are management categories of existing Clash `proxy-groups`. They do not require a separate routing engine. A group name neither identifies traffic nor filters endpoints by region. Routing rules must direct traffic into purpose groups, and region groups must have actual membership constraints.

Keep Settings and DNS: they configure the user's client. Mode, mixed port, `allow-lan`, logging, and IPv6 do not configure the panel server or Agent. Default to Rule mode. If Global and Direct remain available, explain that normal routing-rule evaluation is bypassed in those modes. Client-side overrides may take precedence over the downloaded settings.

Successfully saving or activating a template means subsequent subscription generation will use it. Clients must update and load their subscriptions before applying the changes. Use wording such as “Set as the subscription template; takes effect after clients update their subscriptions,” rather than claiming all clients have received the changes.

## 2. Findings and Priorities

| Priority | Current UI or code | Gap | Recommended action |
| --- | --- | --- | --- |
| P0 | Multiple profile cards; create, activate, and delete | `savedPolicies` and `activePolicyId` exist only in localStorage; the backend stores one `client_rules` value | Add a server-side profile store and global active-profile pointer |
| P0 | New profiles start at version 0; existing cards carry individual versions | Those versions actually belong to the global template, causing `stale_rules` conflicts when treated as profile versions | Separate profile `revision` from global `activeRevision` |
| P0 | Region groups such as US Nodes | `addNewRegionGroup` and the standard topology still use `@all`; group names do not constrain exported members | Use real node-group references, authorized endpoints, and persisted filters |
| P0 | “Sync matching nodes” | Writes server display names into members, which backend validation does not accept as arbitrary endpoint references | Store stable endpoint references instead of display names |
| P0 | Latency, refresh measurements, final node, and available counts | Latency comes from name hashes; missing regions receive invented nodes; some counts and statuses are inferred constants | Remove simulated values; display configuration candidates and runtime-unknown states |
| P0 | YAML preview | Requests the globally active template, not the edited card or draft; discards response warnings | Add side-effect-free draft/profile preview and display diagnostics |
| P0 | Empty region groups | Both `rules.Expand` and the Clash exporter can fall back to DIRECT | Add an explicit empty-group policy; default new strict region groups to REJECT |
| P1 | Purpose/region classification and include/exclude keywords | Categories are guessed from names; filters live in temporary state keyed by array index | Persist category metadata and executable filter conditions |
| P1 | “Edit rules” | One rule opens its drawer; multiple rules open the list; zero rules starts creation | Always open the rules module filtered by the group; show an empty state and an add action when needed |
| P1 | Editing/cloning shared rule sets | Already calls `service-sets` and saves immediately, independently of the profile dialog | Keep this implementation and clarify shared scope, save timing, and reference impact |
| P1 | Save failures and conflicts | Local cards change before requests succeed; `load()` replaces edits after `stale_rules`; returned revisions are not consistently propagated | Update the UI after success; retain drafts on conflicts and show the current server version |
| P1 | External rule sets | Clients fetch providers; the panel has no client fetch-status feedback | Display source, format, and interval without inventing update or availability status |
| P2 | Node, group, user, and rule-set selectors | Only the first page is loaded, sometimes capped at 100; preview silently chooses the first user | Add pagination/search and identify the automatically selected preview user |

Source locations below refer to the review baseline; use function names after subsequent edits:

- `ClientPolicyPage.vue`: `loadSavedPolicies` (2351), `enablePolicy` (2412), `saveAll` (4317).
- Same file: `getNodeSimulatedLatency` (2711), `refreshDelays` (2734), `resolveSymbolicNode` (2885), `getRegionNodeStats` (2940).
- Same file: `getMatchedNodesForGroup` (3021), `applyMatchedNodesToGroup` (3036), `addNewRegionGroup` (3145), and `loadPreview` (around 4453).
- The keyword separator `/[,，s]+/` treats the letter `s` as a delimiter. An empty include condition also returns early and bypasses exclusions. The backend should define filtering semantics; frontend filtering should serve as a preview.
- `validateMember` in `internal/panel/subscription/rules/validate.go` accepts built-in actions, symbolic sources, and other groups. Currently, `node:<id>` is routing-rule target syntax, not group-member syntax.

## 3. Existing Code to Reuse

| Capability | Existing implementation | Planned extension |
| --- | --- | --- |
| Template fields | Template/Client/DNS/Group/Provider/Rule in `subscription/rules/model.go` | Preserve the model; add only necessary optional fields |
| Whole-template validation | `rules.Validate`, `FieldError`, cycle detection, `Ordered` | Extend membership/filter validation and retain field error paths |
| Template persistence and compare-and-swap | `rules.Store.Read/Save` | Extract transaction-aware writes for profile operations |
| Existing template endpoints | GET/PUT `/subscription-rules` and presets in `subscription/rules_handler.go` | Preserve endpoints and route writes through the unified profile service |
| Presets | `rules.Presets` | Initialize profiles from server presets rather than maintaining another frontend set |
| User authorization and proxy sources | `subscription.Service.account/collect/Proxies`, `source.Registry`, published/importer sources | Reuse for previews and member expansion; avoid duplicated authorization SQL |
| Member and rule expansion | `rules.Expand` | Filter authorized endpoints and return actual results and diagnostics |
| YAML generation | Meta/Legacy exporters, typed document, and yaml.v3 in `format/clash` | Share rendering; do not assemble complete YAML in the frontend |
| Domain/IP lists | Service/Services/SaveService/DeleteService in `policy.Repository` | Keep `service_sets` and `service_set_entries` |
| Panel-hosted providers | `ClassicalRuleSet`, GET `/sub/{token}/rules/{id}` | Preserve token authorization and URLs; add reference diagnostics |
| Frontend DTO conversion | `toConfig/fromConfig` in `routing/api.ts` | Extend round-trip mapping without dropping fields |
| Existing simulator | `routing/api.ts` and client-policy-selftest | Reuse rule matching and retain results that cannot be determined until runtime |
| Authorization, audit, and storage | `auth.Require`, CSRF, system audit, SQLite, backup/restore framework | Wire profile operations into existing mechanisms |

Keep implementation within `internal/panel/subscription/` and its `rules` package. Inject cross-module interfaces through `internal/platform/server`. No additional standalone service, cache system, message queue, or Agent command is required.

## 4. Profile Storage and Activation

### 4.1 Minimal New Storage

Add one `client_policy_profiles` table rather than putting every profile into one large JSON value with one shared edit lock:

```text
id                 Stable server-generated identifier
name               Profile name
description        Profile description
template_json      Existing rules.Template, with necessary optional extensions
ui_metadata_json   Management metadata such as group categories; excluded from Clash YAML
revision           Optimistic-concurrency version of this profile
created_at / created_by
updated_at / updated_by
```

Retain `system_settings.client_rules` as the active snapshot used for subscription generation. Store `activeProfileId` and its corresponding `profileRevision` in a separate system_settings metadata entry, rather than inserting undefined fields into the existing `rules.Stored` object.

The profile store holds multiple templates. The active snapshot is a controlled projection of that store. All writes go through one service so the two representations cannot be updated independently.

Suggested initial limits: 50 profiles, 64 characters per name, and 500 characters per description. Preserve existing per-template limits: 64 groups, 2,000 rules, 32 providers, and 256 members per group. Keep the existing HTTP request-size limit. List responses should omit full templates by default.

### 4.2 Save, Activate, and Delete

| Operation | Proposed behavior |
| --- | --- |
| Create/save an inactive profile | Validate the complete template and persist it without changing subscriptions |
| Save the active profile | Update the profile and `client_rules` snapshot in one transaction, matching the current UI's save-to-active-template behavior |
| Activate another profile | Validate references and expected versions; update the active pointer and snapshot in one transaction |
| Delete an inactive profile | Check its revision before deletion |
| Delete the active profile | Return 409; another profile must be activated first |
| Create and activate | Provide an atomic server operation; a partial create/activate result must not be reported as ambiguous success |

Use two distinct version concepts:

- `revision`: the editing version of an individual profile.
- `activeRevision`: reuse `client_rules.revision` to protect global activation state and the active subscription snapshot.

Activation sends both `expectedRevision` and `expectedActiveRevision`. Switching between different profiles advances activeRevision even if their template contents are identical, so concurrent switches cannot be mistaken for the same state. Ordinary saves with unchanged contents retain the existing no-op behavior.

Saving the active profile also checks activeRevision. If another administrator has switched away from it, return a conflict rather than guessing whether the user still intends to activate it. A global revision cannot substitute for a profile's own compare-and-swap check.

Reuse validation and persistence inside the transaction. Do not call a transaction-opening `Store.Save` from inside another transaction. Replace frontend card state only after a successful response, using the returned contents, revision, activeRevision, and updatedAt. Reuse operation auditing and keep transaction results consistent with responses.

### 4.3 Compatibility and Migration

- The reviewed migration head is 0131. At implementation time, add the next unused migration, 0132 if still available. Do not alter historical migrations.
- Preserve existing `client_rules`. During idempotent initialization, copy it into the first active profile while retaining its active revision. Use the server default preset when no previous template exists.
- Existing GET `/subscription-rules` continues returning the active template. Existing PUT updates its active profile and snapshot through the same service; it must not become a separate write path bypassing the profile store.
- Database migrations cannot read browser-local v2 cards. Offer a one-time local-profile import that previews names/counts and uses the normal creation API. Validate each profile, report individual results, and import them as inactive by default.
- Derive an idempotent import identifier from the legacy local ID and a content digest. Retries must not create duplicates. Preserve localStorage until success is confirmed. Never let a browser's activePolicyId override the server's active state.
- Include new fields, tables, the active pointer, and the snapshot in backup inventories, restore validation, and consistency checks. Initialize a default profile when restoring an older archive. When restoring a newer archive, verify pointer/snapshot consistency without creating a new activation event.

## 5. Proxy Groups and Region Filtering

### 5.1 Reuse Existing Member Expressions First

Continue supporting these existing expressions:

```text
DIRECT / REJECT
The name of another client proxy group
@all
@nodeGroup:<id>
@source:<id>
```

For example, if a US Entry Nodes group already exists, a region group can reference `@nodeGroup:<id>`. Expansion still uses the specific inbounds and imported members authorized for the user. Referencing a server or node-group name does not grant additional access.

Persist group categories in the profile's `ui_metadata_json`: service, region, or other. Existing templates may have an inferred initial category, but subsequent saves should use explicit metadata. The initial implementation needs neither a separate group table nor a special two-level execution model.

### 5.2 Add Endpoint Selection and Keyword Filtering

A server list is not a list of the user's available proxy endpoints. A server can expose multiple inbounds; imported endpoints and successfully published relay-rule endpoints must also be represented.

Add a metadata-only candidate query backed by the existing subscription source/proxy pipeline. Return stable references, display names, protocols, origins, node-group membership, and diagnostics. Do not return passwords, credential-bearing URIs, or tokens.

Add `@endpoint:<opaqueRef>` for selecting an exact member. The backend generates and validates the reference:

- Self-hosted entry: use the inbound ID.
- Relay endpoint: use the rule ID, distinct from the original inbound.
- Imported entry: use the source ID plus its stable `name|protocol` key, rather than a content-hash ID that may change after refresh.
- Reuse the neutral proxy model's Origin metadata. Add a stable-reference field there if needed, preserving the package's independence from user, database, and HTTP modules.
- Preserve existing client-chain support. Candidate queries must distinguish selectable entries from internal cloned chain hops.

Add optional `includeKeywords` and `excludeKeywords` string arrays to Group, with these semantics:

1. Collect proxies authorized for the user and passing the existing publication and protocol checks.
2. Resolve candidates from their member sources, deduplicate them, and preserve deterministic ordering.
3. An empty include list imposes no inclusion restriction; otherwise, matching any include keyword is sufficient.
4. Exclusions always apply and take precedence. Initially use normalized, case-insensitive literal matching rather than arbitrary regular expressions.
5. Apply filters only to candidate endpoints. Node names do not replace authorization, and group names do not silently introduce filtering rules.
6. Apply exporter capability filtering and final name disambiguation before producing the client configuration.

Filters must be part of Template and evaluated by backend Expand; UI metadata alone cannot control subscription output. Update strict decoding, validation, frontend `toConfig/fromConfig`, backup validation, and tests for the optional fields.

Region labels and name matching describe an administrator-defined candidate set; they do not prove the geographic location of the actual public exit. A requirement such as “AI traffic must use US endpoints only” needs an explicit US member set, without other regions or DIRECT alternatives in the purpose group.

### 5.3 Empty Groups and Preferred Selection

The current implementation can replace empty groups with DIRECT after authorization expansion and after format filtering. Add `emptyPolicy: direct | reject`, defaulting to direct when omitted for backward compatibility. New strict region groups default to reject. Carry this policy and its diagnostics through both empty-group handling stages; changing Expand alone is insufficient.

Reuse member ordering for the preferred exit: place the preferred member first in a select group. This expresses a configuration preference, not an observed current selection. Clients may retain their local selection.

The actual endpoint used by url-test or fallback is determined at client runtime. Recursively following the first member must not be labeled as the actual final node. The panel can show configured candidate paths and authorized member counts; runtime state remains unknown without measurements.

Do not add a client remote-control channel in this phase. Remove simulated latency and invented region nodes. Rename “Refresh latency” to “Refresh candidates,” or make the measurement action explicitly unavailable without client measurement support. Existing node TCP probes describe a server-side perspective and must not be presented as client latency.

## 6. Routing Rules, Rule Sets, and Navigation

### 6.1 Routing Rules

Keep the existing Rule fields: id, type, payload, target, priority, enabled, noResolve, and description. Evaluate in ascending priority, retaining original order for ties. Enabled rules must contain exactly one final MATCH rule.

- Dragging, copying, batch entry, and moving rules continue to build a complete frontend template. They do not need individual backend endpoints for every movement.
- Disable global reordering while filters are active. Entry from a purpose group displays a target-filter chip and a “View all” action.
- “Edit rules” always opens the group's associated rule list. New rules preselect the group target. A rule set's “Use in routing” action preselects RULE-SET and its payload.
- Names and descriptions have no matching effect. Initially reuse description as a human-readable label rather than adding a field solely for a decorative title.
- Renaming a group/provider updates references in the current frontend template; the backend validates the entire result. Deleting a referenced object should produce a field-level error or expose its references first, never silently redirect to DIRECT.
- Batch input produces separate rules, with deduplication and consistent priority assignment, without placing rules after MATCH. Define explicitly whether editing an existing rule also supports batch expansion.

### 6.2 Rule Sets

Retain the existing two kinds of rule sets:

| Kind | Storage and reusable code | Update behavior |
| --- | --- | --- |
| Panel-managed built-in/custom lists | `service_sets`, `service_set_entries`, policyAPI, ClassicalRuleSet | Saved in the panel; clients fetch according to the provider interval. Node policies require a separate deployment |
| External provider | Template.Providers | The client fetches its URL at the configured interval; the panel stores its definition |

Rule-set content does not contain a DIRECT, REJECT, or purpose-group action. The referencing RULE-SET rule supplies that action.

Keep immediate saving for shared lists, with an explicit notice that editing a shared rule set affects all references. Closing the profile editor or discarding a template draft does not undo an already saved shared list. External provider edits, by contrast, are saved with the current profile.

Add reference queries showing client profiles/rules and node policies that use a rule set. Distinguish active/inactive profiles and enabled/disabled rules. Existing DeleteService checks node-policy references only. Inject a client-reference query interface through platform, and perform deletion/disable checks and writes within a consistent transaction boundary.

Whole-template validation should identify missing or disabled panel service sets used as providers. Reading older subscription data must surface diagnostics. Disabled service-set downloads currently return 404; a client may retain its cache or report an error. A status label must not imply cached rules immediately stop taking effect.

Do not add scheduled panel-side fetching of external rule providers in the initial release. The existing subscription/importer handles proxy sources and cannot directly parse domain/classical/mrs rule files. If remote rule counts or update timestamps are required later, plan their parsing and caching separately. For now, show “Updated by the client / not observed by the panel.”

The Provider Configuration dialog should continue showing token placeholders and usage notes. Never store real subscription tokens in the browser profile library or change logs.

## 7. YAML Preview and Diagnostics

Add a service method for previewing a supplied template, such as `PreviewTemplate`. Share the existing account → collect → Expand → exporter.Render pipeline with Preview. Pass the template as an argument; do not temporarily mutate the shared Service.Rules field or save and roll back to generate a preview.

Support two mutually exclusive input sources: a specified profileId/revision, or the current unsaved template. Neither path changes profiles, the active pointer, subscription tokens, or the last subscription-fetch timestamp.

Real endpoint lists remain dependent on a particular user's authorization. Preserve the layout that removed the permanent user dropdown. A preview may select an eligible user automatically, but its response must identify that user, and the UI must state “Generated using user X's authorization.” Put alternative user selection in an advanced preview context when needed. Do not present one user's result as universal.

If no eligible preview user exists, return an explicit diagnostic. Do not invent nodes or represent the result as a complete runnable configuration. Structural validation can still run independently.

Return and display:

```text
body / contentType / format
profileId / profileRevision, when applicable
templateDigest / previewSource: draft or saved
activeRevision, for comparison only
context: the actual preview user and automatic-selection information
warnings: stage, referenced object, and explanation
```

Warnings must cover missing authorized members, empty-group handling, format omissions, invalid references, and runtime-unknown results. Preserve existing Owner-preview behavior: no token issuance, and placeholder panel-provider URLs. For the same template, user, and format, draft previews and real subscriptions should match except for documented differences such as token-bearing URLs.

Existing GET `/users/{id}/subscription-preview` keeps its meaning: preview the currently active subscription. Do not use it to imply arbitrary draft-preview support.

## 8. Proposed API

These are proposed endpoints, not existing APIs. All use the `/api/v1` prefix. Profile management, user-context previews, and candidate diagnostics are Owner-only. Writes use existing CSRF protection. Preserve compatibility for existing subscription-rules read permissions without expanding Viewer access to user data.

| Method and path | Purpose | Reuse |
| --- | --- | --- |
| GET /client-policy-profiles | Paginated summaries plus global activeProfileId/activeRevision | Existing HTTP pagination and profile service |
| POST /client-policy-profiles | Create; optionally accept activate=true and expectedActiveRevision | Presets, Validate, transactional writes |
| GET /client-policy-profiles/{id} | Full template, metadata, and revision | Template DTO |
| PUT /client-policy-profiles/{id} | Save with expectedRevision; also check expectedActiveRevision when active | Transaction-aware Store writes |
| DELETE /client-policy-profiles/{id}?expectedRevision=N | Delete an inactive profile | Compare-and-swap, Owner authorization, audit |
| POST /client-policy-profiles/{id}/activate | Activate globally with both version checks | client_rules active snapshot |
| POST /client-policy-profiles/validate | Validate a draft without persistence | rules.Validate |
| POST /client-policy-profiles/preview | Generate a complete preview for a draft or saved profile | Service.PreviewTemplate |
| POST /client-policy-profiles/resolve | Expand draft members for a user and return path diagnostics without credentials | account, collect, Expand |
| GET /service-sets/{id}/references | Find references from saved profiles and node policies | Injected reference-query interface |

Resolve serves both region-member previews and configured paths. Do not add separate “final node” or “instant latency” endpoints. Without a user context, the UI can show symbolic sources; concrete member counts must identify the authorization context used.

Reuse `invalid_rules`, field paths, and existing error envelopes. Use a new `stale_client_policy` code for profile-edit conflicts. Reuse or explicitly map `stale_rules` for global activation conflicts. Use `client_policy_active` when deletion is blocked because the profile is active. Conflict responses include current versions; the client retains the unsaved draft rather than overwriting it automatically.

## 9. Implementation Phases

| Phase | Work | Completion criteria |
| --- | --- | --- |
| A: Profile store and activation | New table, initialization, CRUD, activation transactions, legacy API compatibility, local-profile import, initial backup integration | Different browsers see the same profiles and single active state; profile and global versions are not confused |
| B: Actual group membership | Reuse @nodeGroup/@source; add stable endpoint references, filters, category metadata, and empty-group policy; remove simulated data | US groups follow actual membership constraints; missing authorization/endpoints produce explicit outcomes; no invented status |
| C: Preview and navigation | Draft preview, member resolution, warnings, visible preview context, consistent “Edit rules” navigation, draft retention on errors | Previewing an inactive profile shows that profile; saving and previewing cannot mix profiles |
| D: Shared rule sets and completion | Cross-profile references, deletion/disable protection, paginated selectors, restore consistency, documentation, and regression checks | Shared-resource effects are visible; restored activation relationships and authorized subscription output remain consistent |

Keep existing subscription generation usable throughout implementation. Phase A can continue using existing member syntax. Until Phase B is complete, controls relying on new filters or endpoint fields must be marked as pending integration rather than active. Remove unsupported metrics or display unknown values before their backend integration.

The initial scope excludes client telemetry/remote control, per-user profile assignment, arbitrary YAML-field editing, and remote rule-content caching. Those require additional protocols or product behavior and must not be introduced incidentally as UI integration work.

## 10. Expected Implementation Files

- `internal/panel/subscription/rules/`: extend existing models, validation, expansion, and transaction-aware persistence; add the profile-store implementation.
- `internal/panel/subscription/`: profile handlers, preview/resolve service methods, and existing-endpoint adapters.
- `internal/panel/subscription/source/`, `importer/`, and `proxy/`: stable endpoint metadata, preserving the authorization pipeline and dependency boundaries.
- `internal/panel/subscription/format/clash/`: preserve the YAML structure; propagate empty-group policy and render necessary extensions.
- `internal/platform/server/`: dependency wiring, cross-module reference queries, and integration tests.
- `internal/panel/policy/`: reuse service-set CRUD; extend cross-profile reference, version, and consistency protection.
- `internal/panel/system/backup/`, necessary restore coordination, a new migration, and the migration checksum inventory: profile inventories and compatible restoration.
- `web/src/modules/routing/api.ts`, `ClientPolicyPage.vue`, and components extracted by responsibility, such as profile lists and group editors: API integration, state management, and complete field round trips.
- AGENTS.md, architecture documentation, the protocol matrix, and release notes: update them to the implemented behavior after delivery. This planning document does not preemptively change the current specification.

## 11. Acceptance Criteria

1. Two browsers see the same profile list and the same active pointer after an activation change.
2. Concurrent edits by two Owners, concurrent activation of different profiles, and a profile being deactivated during editing have deterministic compare-and-swap outcomes without draft loss.
3. Switching between different cards with identical contents advances the global version. Repeated saves with no changes do not create unnecessary revisions.
4. New profiles do not change subscriptions by default. A successful save of the active profile affects subsequent subscription generation. Failed transactions leave no partial writes.
5. Purpose-group → region-group nesting works without self-references or cycles. Group and rule references remain valid after renames.
6. Users with different permissions receive different members. Multiple inbounds on one server, refreshed imports, and relay endpoints remain distinct. Endpoint references cannot grant unauthorized access.
7. US-group filters affect actual YAML output. Exclusions work even without inclusion keywords. Empty groups after matching or format filtering follow emptyPolicy.
8. The UI contains no name-hash latency, fixed success counts, invented endpoints, or false live selections. Unknown values remain unknown.
9. Inactive profiles and unsaved drafts can be previewed without persistence or token issuance. The selected user and all warnings are visible.
10. Real subscriptions and corresponding previews share generation logic. Validate YAML with the real Mihomo core, covering rules, nested groups, DIRECT/REJECT, and providers.
11. Provider contents do not carry routing actions. Shared service-set references are queryable, and deletion/disable protection includes client and node-policy references.
12. URI, Base64, and sing-box fragments retain their current scope. Legacy Clash continues reporting and omitting unsupported items rather than silently converting them.
13. Older databases, repeated localStorage imports, and old/new backup restoration preserve the correct active configuration. Fields are not silently discarded.
14. Append change records for every iteration, including the request, all affected files, before/after behavior, API/migration changes, verification, and remaining work. Retain records of revisions and rework.

Existing test starting points: `rules/rules_test.go`, `rules_handler_test.go`, `format/clash/clash_test.go`, `format/clash/routing_test.go`, `platform/server/subscription_rules_test.go`, and `web/scripts/client-policy-selftest.mjs`. New tests should cover concurrency, authorization, real rendering, and restoration. Source-string assertions do not substitute for behavior verification.

After implementation, run the project's Linux `scripts/check.sh` and the applicable real-core suite. Local frontend checks do not replace those gates.

## 12. Verification Performed During the Original Review

- `npm.cmd --prefix web run typecheck`: passed.
- `node web/scripts/client-policy-selftest.mjs`: all 20 checks passed, including template DTO round trips and runtime-unknown semantics.
- Production build, Go tests, Linux gates, the real Mihomo suite, and an authenticated browser walkthrough were not run.
- These results establish that existing type checks and self-tests passed. They do not establish that the new UI already has backend persistence, telemetry, or draft-preview support.

The English translation is a documentation-only change. The checks above were performed during the original review and were not rerun for translation.

## 13. Review Decisions (Approved With Amendments)

Reviewed against `258c1c7` (the UI baseline committed on `feat/v1.4-tunnels`). The plan is approved. Where this section differs from §§1–12, this section wins. The amendments remove duplicate logic and endpoints; they do not add scope.

1. **Active pointer.** Store only `{profileId}` under a new `system_settings` key (`client_policy_active`). Drop `profileRevision`: every write goes through one service, so the `client_rules` snapshot always equals the active profile's template, and restore validation compares the two directly. Keep the pointer out of `rules.Stored`, because older binaries decode that row strictly and would reject an unknown field after a downgrade.
2. **One save path.** Refactor `rules.Store.Save` into a transaction-aware function that profile saves, activation and the legacy PUT all call. Activation passes a flag that advances the revision even when the template is unchanged (§4.2). No second copy of validate-compare-write.
3. **Local import.** Add a nullable unique `import_key` column (legacy local ID plus content digest). `POST /client-policy-profiles` accepts an optional `importKey`; a repeat returns the existing profile instead of creating a duplicate.
4. **Endpoint references need no new Origin field.** `proxy.Origin` already carries stable identity: published inbounds have `Source=published, Ref=<inbound id>`; relay endpoints have `Relayed=true, Ref=<rule id>`; imports have `Source=import:<source id>, Ref=<name|protocol key>`. A single `rules` helper derives `@endpoint:inbound:<id>`, `@endpoint:relay:<id>` and `@endpoint:import:<source id>:<key>` from an Origin, and both Expand and the candidate listing use that helper. Cloned client-chain hops are not candidates.
5. **Filtering lives only in Expand.** `includeKeywords`/`excludeKeywords` are evaluated by the backend alone. The page stops filtering names itself and shows the members the server resolves, which also fixes the separator bug that split on the letter `s`.
6. **One empty-group rule.** Expand and the Clash exporter both call one `rules` helper that applies `emptyPolicy` and produces the warning. They do not each keep their own fallback.
7. **Fewer endpoints.** Drop `POST /client-policy-profiles/validate` and `/resolve`. Preview already validates (it returns `invalid_rules` with field paths) and returns the resolved members of each group alongside the YAML, warnings and preview context. Save validates as it does today. With no eligible preview user, preview still validates the structure and returns the no-user diagnostic.
8. **One render path.** `render` takes the template as an argument. `Render`, `Preview` and the new draft/profile preview all call it, so draft preview and real subscription output cannot drift apart.
9. **No wholesale page split.** `ClientPolicyPage.vue` is restructured only where a phase changes behavior. Unrelated extraction is not part of this work.
10. **Code style.** No defensive re-checks of state that a single code path already guarantees. Validation stays at the API boundary (strict decode plus `rules.Validate`).

Implementation runs phase by phase (A → D). Each phase is reviewed, committed locally and gated on the Linux test machine (`scripts/check.sh` plus the real-core suite) before the next phase starts.

## 14. Implementation Status and Remaining Items

Status on November 5, 2026: Phases A–D are implemented and committed locally on `feat/v1.4-tunnels`; nothing is pushed.

| Commit | Content |
| --- | --- |
| `42491ab` | Phase A: profile store, activation, legacy API sync, local import, backup (Linux gate passed) |
| `c708c35` | Phase B: `@endpoint` members, keyword filters, `emptyPolicy`, candidate listing |
| `6cfc6c5` | Phase C: draft and saved-profile preview through the one render path; rule navigation |
| `33d893c` | Phase D: service-set references and delete protection, paginated selectors, release notes |
| `1bbb836` | `mihomo -t` case for nested, filtered, endpoint and empty-REJECT groups |

### 14.1 Verification Still Open

1. **Linux gate for Phases B–D.** `scripts/check.sh` plus the real-core suite on `1bbb836` is still running. Only Phase A (`42491ab`) has passed so far.
2. **Mihomo routing behavior (§11 item 10).** The real mihomo check only covers config acceptance (`mihomo -t`). No real-core test sends traffic through a purpose group → region group → endpoint path or confirms that an empty REJECT group actually blocks. Extend `routing_test.go` (`TestMihomoRoutesProxyDirectAndReject`) with a nested-group and empty-REJECT case.
3. **Authenticated browser walkthrough.** Not performed. Walk through the following in a browser:
   - profile cards: create, edit, activate and delete
   - a 409 conflict that keeps the draft
   - the import preview for localStorage cards
   - region-group keywords and "Add selected endpoints"
   - "Resolve members" and the YAML preview context
   - the filtered "Edit rules" view
   - rule-set references and delete blocking
4. **Two-Owner and restore scenarios (§11 items 1, 2, 13).** These are covered by Go tests only. Check them by hand:
   - two browsers seeing the same activation
   - repeating the localStorage import
   - restoring an archive from before 0132 and an archive that includes it

### 14.2 Open Product Decisions

1. **Preview user choice.** The API accepts `userId`, but the page only uses automatic selection, which it labels as automatic. §7's "advanced preview context" for picking another user is not built yet. Decide whether it is needed.
2. **Batch input when editing a rule (§6.1).** Today batch expansion happens only when creating rules; editing an existing rule replaces it as a single rule. Confirm that this is the intended behavior, then document it.
3. **Disabling a referenced rule set.** Since Phase D, saving a profile that has an *enabled* RULE-SET rule pointing at a *disabled* service set is rejected. Disabling a set used by the active profile therefore blocks re-saving that profile until the rule is disabled or the set is re-enabled. Confirm this, or relax it to a warning.

### 14.3 Known Limitations (Accepted, Documented)

1. A profile save validates service-set references through another module's read, so it can race with a concurrent service-set delete (one-line comment in `policy.Repository.DeleteService`). A rendering warning covers the effect.
2. The page loads profile details with one GET per profile (at most 50). The list endpoint deliberately omits templates.
3. When no preview user exists, the preview returns only the diagnostic. It does not check whether the requested format is known.
4. Every subscription render that includes panel service rules looks up each set's enabled state, so that previews can warn.

### 14.4 Documentation Still Open

- `docs/architecture.md` and `docs/protocol-support-matrix.md` do not describe the profile library, `@endpoint` members, keyword filters or `emptyPolicy` yet (§10 last item). AGENTS.md, `docs/changelog.md` and `docs/release-notes-1.4.0.md` are updated.
- `docs/v1-acceptance.md` has no entry for the items in §14.1 yet.

### 14.5 Out of Scope (Unchanged From §9)

Client telemetry and remote control, per-user profile assignment, arbitrary YAML-field editing, and panel-side fetching of remote rule content.
