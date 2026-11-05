# Protocol Support Completion Plan: External Subscriptions, Self-Hosted Nodes, and Failure Diagnostics

Status: **reviewed 2026-11-05 — approved with the amendments in [§14](#14-review-decisions-2026-11-05). The §14.5 refresh fix is implemented, and its commit `650b349` passed the Linux gate (`check` 1980 s, `cores` 572 s, total 2552 s, END 2026-11-05T09:30:12Z), independently verified from the gate log; everything else is still not implemented. S0 changed no production code; S1 is now in progress in a separate worktree, and no S1 completion is claimed.** §14 overrides earlier sections where they differ. Owner decisions (§14.3): both tracks in parallel; **all of this plan ships in v1.4.0** (both tracks and the refresh fix, see §14.7); duplicate `name|protocol` entries keep the first and reject the rest (§14.6). Prepared on November 4, 2026, America/Los_Angeles.

This is the English version of [the Chinese plan](subscription-import-compatibility-plan.md), including its update dated `2026-11-04T23:15:43-08:00`. Self-hosted xhttp / Reality support is integrated into the scope, implementation phases, and acceptance criteria. The translation preserves the source document's scope and technical identifiers.

Review baseline: `feat/v1.4-tunnels`, HEAD `c708c35`, database migration head `0132`. This document describes planned work, not delivered protocol support. It does not change functionality, refresh sources, or reconfigure running nodes or clients.

## 1. Objectives and Scope

Cover these three workflows while reusing the existing models, validation, encryption, deployment, and subscription code:

| Workstream | Planned work | Intended result |
| --- | --- | --- |
| External subscriptions | Parse, encrypt, persist, and export Shadowsocks 2022, VLESS Reality, and VLESS xhttp | Supported entries retain their complete configuration and work with compatible clients; unsupported entries have precise explanations |
| Self-hosted nodes | Implement Xray VLESS xhttp / Reality first; adapt sing-box independently according to actual version capabilities | Settings can be edited and saved, the Agent can render and apply them, and users receive matching published subscriptions |
| Failure diagnostics | External fetching/parsing/export, plus self-hosted validation/capability/deployment errors | Identify the failing layer without treating a successful save or status write as a successful connection |

External workflow: URI/Base64/Clash input → typed parsing → encrypted persistence → format-specific export.

Self-hosted workflow: editor → backend configuration/validation → DesiredState → native Agent rendering/validation/application → published subscription.

Both workflows share client parameter mappings and export rules. Server-side settings for self-hosted nodes are modeled separately in Section 13.

The previous read-only inspection received HTTP 200 and decoded 16 endpoints from Base64: 3 using `2022-blake3-aes-256-gcm`, plus 13 VLESS Reality endpoints consisting of 11 xhttp and 2 grpc entries. This describes that inspection only; the external source may change. The planning review did not fetch it again.

Initial acceptance should use sanitized, synthetic equivalents of those 16 entries. Real subscription URLs, tokens, UUIDs, passwords, and complete source responses must not be copied into code, fixtures, logs, or this plan.

Product boundaries:

- Include external import compatibility and self-hosted VLESS xhttp / Reality. The self-hosted workflow requires backend, DesiredState, core capability, Agent, and frontend support; adding dropdown entries alone is insufficient.
- The initial self-hosted target is an Xray VLESS inbound, successful deployment, and its corresponding user subscription. Self-hosted outbounds are enabled only for validated combinations covered by the same settings contract. Canvas/internal relays do not automatically switch to new combinations.
- Self-hosted Shadowsocks 2022 requires server-side multi-user key and hidden-identity handling. This plan initially covers imported SS2022 only; self-hosted SS2022 is a separate scope and is not enabled merely because cipher validation is reusable.
- Import capability is distinct from output-format/client capability. A fully preserved endpoint may be exported only to formats that support it, with explicit omission reasons elsewhere.
- Imported endpoints retain their existing exclusion from Agent deployments, account metering, and device admission enforcement.
- This document plans scope; it does not execute production changes, replace user endpoints, weaken transport security, automatically upgrade production cores, or enable insecure TLS.

## 2. Current Gaps and Reusable Code

| Layer | Current gap | Reuse and extension |
| --- | --- | --- |
| Source fetching | `importer/fetch.go` reduces DNS, HTTP, TLS, timeout, and other failures to ErrFetch | Preserve HTTPS, public-address validation, per-hop IP pinning, and 2 MiB/15-second limits; add typed errors |
| Input parsing | `importer/parse.go` uses strict allowlists; unknown parameters skip entries; SS2022 is absent from the cipher list | Keep the URI/Base64/Clash parsers; add typed fields and per-entry diagnostics |
| Neutral model | `subscription/proxy` lacks Reality, xhttp, and client TLS fingerprint fields | Add optional structures instead of opaque raw-configuration passthrough |
| Encrypted persistence | `importer.Store.replace` encrypts neutral-model JSON | Reuse Vault, transactional replacement, and existing source authorization |
| URI export | `format/uri` rejects reality and lacks its parameter output | Add complete mappings and the required SS URI encoding rules |
| Mihomo export | `format/clash` filters by protocol name and lacks Reality/uTLS/xhttp fields | Extend combination checks, typed YAML, and omission diagnostics |
| sing-box export | Ordinary TLS and some transports are supported; some unsupported entries abort the whole document | Add supported fields and omit individually unrepresentable entries with warnings under the existing Exporter contract |
| Refresh state | Fetch/parse failures write a generic last_error and return writeErr, which can be interpreted as success | Return a business result distinguishing success, partial success, failure, and persistence errors |
| Frontend | A 204 refresh response produces a success message; the count reflects cached rows only | Consume the business result and display recognized/imported/skipped counts and details |
| Self-hosted editor | `NodeProtocolFields.vue` exposes only tcp/ws/grpc and none/tls | Show xhttp/Reality and their fields according to actual core capabilities |
| Shared settings/capabilities | ProtocolSettings, ValidateSettings, and the capability matrix lack the new combinations | Add typed server/client settings and capability contracts, reusing common validation |
| Agent rendering/application | Xray/sing-box adapters do not render the new settings | Reuse compilation, native validation, multi-core application, health checks, revisions, and rollback |
| Backup/restore | `backup/validateImports` already validates encrypted payloads | Extend it for the new models and diagnostic fields |

Keep external import work in the existing subscription/importer, proxy, and format modules. Extend panel/proxy, desiredstate, agent/core, and node-editor for self-hosted support rather than adding a parallel deployment service. Share field specifications, codecs, and validation that does not contain server-private material. Section 13 defines the role-specific structures.

## 3. Phase 0: Verify Target Cores and Protocol Combinations

The current real-core suite references Mihomo `1.19.33`, sing-box `1.14.3`, and Xray `26.3.27`. Pin the actual binaries, versions, build tags, and SHA256 values during implementation. Avoid an unspecified claim that “the latest version supports it.”

Static evidence available at the planning baseline:

- `docs/archive/v1.1-phase0-findings.md` records Mihomo 1.19.33 accepting an SS2022 AES-128 configuration. That does not establish AES-256, Reality, or actual connection success for this work.
- Locally cached sing-box **1.14.2** source contains Reality/uTLS and SS2022 models, but its V2Ray transport enum does not include xhttp. This is not the pinned 1.14.3 version, so it is a verification lead only.
- Local Xray 26.3.27 source can help verify Reality and xhttp parameters. Server-side implementation alone does not establish every client combination as valid.

Obtain evidence for each cell:

| Configuration | URI/Base64 | Mihomo YAML | sing-box fragment | Legacy Clash |
| --- | --- | --- | --- | --- |
| SS2022 AES-256 | Target support; verify SIP002 and keys | Target support; native validation and connection testing | Target support; native validation and connection testing | Unverified by default; check the pinned version rather than inheriting the ordinary SS support flag |
| SS2022 AES-128 | Implement and verify alongside AES-256 | Same requirement | Same requirement | Same restriction |
| SS2022 ChaCha20 | Include after verifying actual client support | Version-specific | Version-specific | Omit unless verified |
| VLESS Reality + TCP | Preserve semantics | Verify exact fields and fingerprint handling | Verify Reality/uTLS fields | Retain existing VLESS omission |
| VLESS Reality + grpc | Preserve semantics and verify combination validity | Verify independently of TCP | Verify independently of TCP | Omit |
| VLESS Reality + xhttp | Preserve semantics and test a compatible client | Verify pinned-version support first; otherwise report a format omission | Verify pinned-version support first; otherwise report a format omission | Omit |

Validate protocol/security/transport/flow/cipher jointly, rather than checking only VLESS or Shadowsocks. If a pinned client lacks xhttp, still complete neutral-model and URI fidelity. Using all xhttp endpoints in that client requires verified support in a suitable version. Do not convert xhttp to ws/grpc or remove required parameters.

Phase 0 delivers a version/field/native-validation/connection-results matrix. Mark a cell as supported only after collecting its evidence.

For self-hosted nodes, also build a core × version/settings-contract × inbound/outbound × security/transport/flow matrix. At minimum, verify Xray VLESS Reality+TCP, Reality+xhttp, TLS+xhttp, and applicable Vision combinations individually. Recognizing a share URI does not establish server-side validity, and unknown capabilities on an older Agent must not be treated as supported.

## 4. Protocol Models and Parsing

### 4.1 Neutral Model

Prefer optional fields in `internal/panel/subscription/proxy`:

```text
Security = none | tls | reality

TLS
  Preserve existing ServerName / ALPN / PinnedCA and related fields
  ClientFingerprint       Client TLS/uTLS fingerprint

RealityOptions
  PublicKey
  ShortID
  SpiderX                 Only usable in formats supporting that semantic

Transport
  Preserve Type / Path / ServiceName
  Add supported Host semantics
  XHTTPOptions            Typed mode, host, path, and verified extensions
```

The existing Mihomo `fingerprint` output pins certificates. URI `fp` and Mihomo `client-fingerprint` identify the TLS client fingerprint. Keep these concepts separate; a Reality public key is not a certificate fingerprint either.

Phase 0 determines the exact Reality/xhttp field mappings. Preserve all recognized parameters that affect connections through serialization round trips. If an unsupported extension affects connectivity, return an explicit diagnostic rather than retaining arbitrary maps for silent passthrough.

### 4.2 VLESS / Reality

- Parse UUID, flow, security, SNI, and the client fingerprint correctly.
- Handle `pbk`, `sid`, `spx`, and `fp`; validate public-key encoding/length, Short ID format/length, fingerprint values, and protocol combinations.
- Normalize the known aliases `sni` and `servername`. Conflicting values produce a field-conflict error rather than an arbitrary choice.
- Accept `encryption=none` as a recognized VLESS parameter. Validate other values instead of ignoring them.
- Distinguish omitted fields from explicit empty values. Whether an empty Short ID is legal follows the pinned core's rules.
- Interpret grpc `serviceName`, possible `mode` aliases, and xhttp `mode` in their own transport contexts.
- Validate Reality+grpc, Reality+xhttp, and flow combinations independently. Invalid combinations remain failures rather than being accepted to increase import counts.

### 4.3 xhttp

- Parse typed host/path/mode options and verify the target version's support for values such as auto, packet-up, stream-up, and stream-one.
- Keep transport host separate from TLS server name. Handle URL encoding, empty paths, IPv6 addresses, and repeated parameters correctly.
- Maintain explicit support, size, and nesting-depth limits for `extra` and advanced parameters. Reject entries that cannot yet be fully represented, with a specific reason.
- Being able to persist an input does not mean every exporter can represent it. Record the differences in the format matrix.

### 4.4 Shadowsocks 2022

- Prioritize the observed `2022-blake3-aes-256-gcm`, alongside AES-128 where validation can be shared. Enable ChaCha20 according to version evidence.
- Validate Base64 encoding and decoded key length by cipher: 16 bytes for AES-128, 32 bytes for AES-256, and the relevant specification for other methods. A nonempty password check is insufficient.
- If multi-user identity-key chains are supported, validate every segment's structure and length. Otherwise report that limitation explicitly; do not take the final segment and claim full support.
- Accept verified Base64-userinfo and plaintext/percent-encoded URI variants, including correct handling of `:`, `+`, `/`, `=`, and escaping in passwords. Base64URL userinfo stays an **input** compatibility path, never an output form for SS2022.
- Export SS2022 in the SIP002 **plain** form: `ss://` + percent-encoded `method:password` + `@host:port` (+ escaped tag). SIP002 says that for AEAD-2022 the userinfo *"MUST NOT be encoded with Base64URL"* and that *"method and password MUST be percent encoded"*; the password itself is the standard padded Base64 PSK defined by SIP022. S0 pinned that wording and quoted it in [the evidence report](v1.4-s0-evidence.md) §7; percent-encoding the identity separator is a decision S2 must confirm with a URI-consuming client. Non-SS2022 entries keep their existing SIP002 form.
- Preserve the cipher name after normalization; never downgrade it to an ordinary Shadowsocks method.

### 4.5 Strict, Explainable Unknown-Field Handling

Normalize known aliases. A bounded list of metadata known to have no runtime effect may be ignored with a diagnostic. Unknown security or transport parameters reject the entry and identify the field name.

Produce a separate result for each input entry. Base64 decoding reuses URI parsing. Extend Clash input with reality-opts, client-fingerprint, and supported xhttp structures while retaining type, count, and size limits.

Use shared normalization and validation for pasted nodes, subscription URLs, and Clash documents. Do not relax only one entry point's allowlist.

## 5. Format Export and Overall Availability

1. `format/uri`: export complete VLESS Reality/xhttp/grpc and SS2022 settings. Base64 wraps the same URI output.
2. `format/clash`: add reality-opts, client-fingerprint, and transports supported by the pinned Mihomo version. Evaluate cipher/security/transport separately for legacy Clash; ordinary SS support does not imply every cipher is supported.
3. `format/singbox`: add verified Reality, uTLS, and SS2022 mappings. Unsupported xhttp entries produce per-entry unrepresentable diagnostics.
4. Reuse `format.Output.Warnings`, the neutral model, and the subscription Registry. Add a common protocol-combination capability helper rather than a complex plugin system.
5. Under the existing Exporter contract, omit valid but unrepresentable endpoints with warnings. One unsupported endpoint must not fail the entire subscription containing other usable endpoints. Internal data corruption remains an error, not a normal omission.
6. After omissions, preserve member filtering, client-chain dependency cleanup, and emptyPolicy. Do not leave references to missing proxies or replace an explicitly configured REJECT empty-group policy with DIRECT.
7. Owner previews distinguish “N endpoints imported” from “M endpoints available in this output format.” Import counts do not establish compatibility with every format.

When a format cannot represent an entire source, explain why and identify compatible formats to the Owner. Preserve existing authorization and ordinary subscription response contracts. Do not insert sensitive diagnostics into raw URI/YAML documents.

## 6. Structured Failure Diagnostics

### 6.1 Separate Workflow Results

An import-refresh result describes fetching, decoding, parsing, and committing the cache. A format-preview result describes whether the selected client format can represent stored endpoints. These require distinct explanations.

Self-hosted nodes continue using field validation and deployment ApplyResult. Saving a draft, successfully applying a deployment, and establishing an actual client connection are different states. Do not report deployment errors as source-refresh failures or present manual connection-test results as ordinary Agent health observations.

Proposed refresh result:

```json
{
  "sourceId": "opaque-id",
  "outcome": "partial",
  "stage": "parse",
  "code": "import_partial",
  "counts": {
    "total": 16,
    "accepted": 5,
    "rejected": 10,
    "duplicates": 1,
    "cached": 5,
    "usable": 5
  },
  "attemptedAt": "UTC timestamp",
  "lastSuccessAt": "UTC timestamp or null",
  "diagnostics": [
    {
      "entryIndex": 3,
      "protocol": "vless",
      "stage": "parse",
      "code": "unsupported_transport_option",
      "field": "transport.mode",
      "message": "This transport option is not supported yet"
    }
  ],
  "diagnosticsTruncated": false
}
```

This illustrates the structure, not the current source's result. Define total = accepted + rejected + duplicates. Accepted counts the deduplicated entries retained successfully in this attempt. An entry may have several reasons, but count the entry only once; simultaneous Reality and xhttp diagnostics must not double the rejected total.

Define cached count, previous successful cache count, entries recognized this attempt, and currently usable count separately. If downloading did not complete, total is null/unknown rather than evidence that the source has no nodes.

### 6.2 Error Categories

| Stage | Example codes | User-facing explanation |
| --- | --- | --- |
| Input | invalid_source_url | A valid HTTPS subscription URL is required |
| DNS | source_dns_failed | The source hostname could not be resolved |
| Address/redirect | source_address_rejected / source_redirect_rejected | The source or redirect does not meet public-address requirements |
| Connection/TLS | source_connect_failed / source_tls_failed | Connection or certificate validation failed |
| Deadline | source_timeout | Fetching timed out; this attempt did not update the cache |
| HTTP | source_http_error | The source returned a status such as 401, 403, 429, or 5xx |
| Size | source_body_too_large | The response exceeded the permitted size |
| Decoding/document | invalid_base64 / unsupported_document / invalid_document | Invalid Base64, a returned webpage, or an unrecognized document format |
| Endpoint fields | invalid_uuid / invalid_ss2022_key / invalid_reality_key / conflicting_alias | Identify the invalid field without echoing its raw value |
| Endpoint capabilities | unsupported_protocol / unsupported_security / unsupported_transport / unsupported_parameter | Identify the unsupported protocol, transport, or parameter name |
| All entries skipped | no_supported_entries | Entries were recognized, but none could be imported; provide category counts |
| Persistence | import_persist_failed | Cache persistence failed; an incomplete result was not marked usable |
| Format export | format_option_unsupported | The endpoint is stored, but the selected format cannot represent its configuration |
| Self-hosted validation | invalid_reality_settings / invalid_xhttp_settings / invalid_protocol_combination | Identify invalid fields, mutually exclusive options, or illegal combinations; retain editor input |
| Self-hosted capability | core_feature_unsupported / agent_contract_unsupported | The selected core/Agent has not declared the required capability; saving or deployment is unavailable |
| Self-hosted deployment | Existing native-validation, application, health, and rollback codes | Identify the failing phase rather than claiming the node was created successfully |

Preserve typed causes inside Fetcher and Parser. Convert them at Owner API/log boundaries into fixed message templates, safe codes, and permitted numeric information. Never expose raw `err.Error()`, URLs, Location headers, request/response bodies, or tokens embedded in HTTP-library errors.

Diagnostics default to entry index and protocol. If names are needed, remove control characters and bound their length; do not write untrusted names directly to logs. Full public-key values need not be echoed either.

Persist only the latest result per source, with bounded detail count and size: suggested limits are 50 items and 32 KiB. Retain aggregate counts and a truncation flag. Full history would require an explicit retention design, not unlimited storage of raw failures.

## 7. Refresh API, State, and Cache Semantics

### 7.1 Correct Success and Failure Results First

Change Refresh from returning only an error to returning a business result plus an error, or add a compatible wrapper. Manual and scheduled refreshes share the implementation. Successfully recording failure state is not a successful refresh.

- Complete success: HTTP 200 with outcome=ok.
- Partial success: HTTP 200 with outcome=partial and imported/skipped counts.
- Fetch failure: HTTP 502 according to a fixed mapping; timeouts return 504, with a safe result summary.
- Content fetched but invalid/unrecognized or containing no acceptable entries: HTTP 422 with diagnostics.
- Invalid input returns 400; internal persistence failure returns 500; missing sources have explicit not_found semantics.
- User cancellation or a completed request context must not be mislabeled as unsupported protocol. If the task did not commit, preserve committed state.
- Change the frontend refresh return type from void to a structured result. Preserve failure results in APIError.details for display. GET sources also returns the latest result so scheduled-refresh outcomes can be inspected later.

Keep POST `/subscription-sources/{id}/refresh`; do not introduce a state-changing GET. Source creation/editing may still save first and then refresh. If refreshing fails, state “Source saved; fetch/parsing failed,” retain its ID and editing context, and avoid messages claiming either that nothing was saved or that the initial fetch completed successfully.

### 7.2 Preserve Existing last_status Gating

Current `Proxies` requires last_status=ok, while the frontend treats any lastError as a refresh failure. Simply writing last_status=partial would disable every partially successful import under existing code.

Recommended initial behavior:

- At least one accepted endpoint and a successful commit: last_status=ok and last_error empty. The new result's outcome may be ok or partial, and the frontend uses outcome for warnings.
- No accepted endpoints or a fetch failure: last_status=failed and a safe last_error summary; the new result preserves the specific category.
- Retain last_fetch_at as the latest attempt time for compatibility, label it “Last attempt,” and add last_success_at for the latest successful refresh.

### 7.3 Cache Handling

Preserve the current conservative policy: failed sources do not participate in subscriptions. Continuing to serve old endpoints after a failure is outside this change.

- Complete/partial success: transactionally replace the cache with this attempt's accepted entries and update state and diagnostics.
- Complete failure: retain previous successful cache rows for diagnostics, but mark the source failed so subscriptions do not use them. If no prior cache exists, say so explicitly.
- Persist failure state and diagnostics consistently. A failed commit must not leave a mixture of old and new cache contents.
- After a URL change, pending state must not present cached data from the previous URL as a result from the new source.
- Subscription requests remain readers of encrypted caches. Fetching belongs to explicit refreshes and the existing scheduler.
- Do not silently change intervals or introduce unlimited retries. Preserve existing scheduler lifecycle and cancellation behavior.

## 8. UI Messages and Interaction

The source list distinguishes saved-source state, last attempt, last success, entries imported/skipped this attempt, cached entries, and currently usable entries.

| Situation | Suggested message |
| --- | --- |
| Saved but not fetched | Source saved; waiting to fetch endpoints |
| Success | Updated: 16 endpoints imported |
| Partial success | Imported 5, skipped 10, duplicates 1; view reasons |
| HTTP success but no acceptable entries | Subscription fetched; 16 entries recognized, 0 importable; view compatibility reasons |
| Timeout with prior cache | Fetch timed out; 8 cached endpoints retained and currently excluded from subscriptions |
| Failure without prior cache | Refresh failed; no usable endpoints are available |
| Unsupported output format | Imported; the current sing-box format omits 11 xhttp endpoints; view compatible formats |

These counts are illustrative. Actual counts come from the server. “Stored encrypted” describes URL storage only, not successful import. Authorized-user count describes grants, not endpoint availability.

The “View reasons” dialog groups network, parsing, and output-format diagnostics, showing aggregate counts, entry indexes, codes, and corrective guidance without raw sensitive URLs. Toast messages follow the business outcome rather than HTTP success alone.

Pasted-node imports use the same diagnostics. Partial success identifies the retained supported entries. With no accepted entries, retain editor contents so users can correct and retry.

Self-hosted editor requirements:

- After selecting VLESS, show transport/security choices supported by the selected core. Explain unavailable or unknown capabilities, and enforce them again in the backend.
- xhttp exposes host/path/mode. Reality exposes permitted SNI values, handshake target, Short IDs, and key state. Do not route Reality through ordinary TLS's mandatory certificate-upload validation.
- Public keys may be copied for clients. Private keys are hidden by default and excluded from ordinary queries, revision diffs, and previews. Saving ordinary parameters does not rotate keys.
- Combination changes handle dependent and mutually exclusive fields while preserving recoverable input. Do not silently substitute another security or transport setting.
- After saving, say “Draft saved; deploy to apply.” After successful deployment, instruct clients to update their subscriptions. Deployment success does not mean every client has updated or completed a handshake.

## 9. Data Compatibility and File Scope

Add necessary fields to subscription_sources, such as latest-result JSON and last_success_at. The planning baseline has migration head 0132. Use the next unused migration at implementation time; never edit applied migrations.

Keep old last_status/last_error/last_fetch_at values readable. Historical failures without diagnostics display “Previous refresh failed; reason was not recorded.” Do not invent a Reality or network cause retroactively. Migrations do not fetch live subscriptions.

Persist optional protocol structures in existing encrypted payloads; do not introduce plaintext password columns. Update cache validation, backup-payload validation, and restore compatibility. Older programs must not silently discard fields they cannot represent. Document minimum compatible versions and backup requirements for downgrades.

Self-hosted settings reuse encrypted inbound/outbound columns and encrypted Revisions; do not add a plaintext Reality-private-key column. Verify private/server and public/client boundaries during saving, diffs, backup, and restoration. Older Agents and restore tools explicitly reject unknown settings instead of reverting to none/tcp. If protobuf capabilities change, add compatible fields and regenerate contracts. Add database migrations only for actual schema/state requirements.

Expected files:

- `subscription/proxy/`: Reality/xhttp/client-fingerprint models and shared parameter validation.
- `subscription/importer/parse.go` and `fetch.go`: compatibility, normalization, typed failures, and per-entry results.
- `subscription/importer/store.go` and `handler.go`: business results, persisted diagnostics, and API mappings.
- `subscription/format/uri`, `clash`, and `singbox`: complete export, combination checks, and per-entry omissions.
- `subscription/service.go`, preview handlers, and client-policy candidate/chain/group handling: diagnostic propagation with correct authorization and dependencies.
- `panel/system/backup/imports.go` and related restore validation: new payload and result checks.
- `web/src/modules/proxy/importApi.ts`, `proxyNodes.ts`, and `ProxyNodeListPage.vue`: types, source-management state, and details.
- Subscription/client-policy previews: capability information and warnings, reusing existing components.
- `pkg/contracts/desiredstate/`, `proto/agent.proto` where necessary, and generated code: self-hosted settings/capability contracts while preserving historical-configuration reading.
- `internal/panel/proxy/`, `internal/panel/node/`, and `internal/platform/server/`: save/deploy gates, capabilities, and dependency wiring, reusing Snapshot/Deployment.
- `internal/agent/core/xray/`, `singbox/`, `capability/`, and necessary coordination: rendering and capability reporting through existing application/rollback paths.
- `web/src/modules/proxy/components/node-editor/`, protocol/API types, and capability views: forms, dependencies, validation, errors, and confirmation summaries.
- Backup/restore tests for inbounds/outbounds, Revisions, keys, and node configuration: consistency of new settings and deployment history.
- New migrations, tests, AGENTS.md, the protocol matrix, operations documentation, release drafts, and development records.

Reuse Client Policy Phase B's stable imported references: source ID plus `name|protocol`. Parameter changes must not grant additional endpoints or silently retarget a client chain. Preserve authorization, stable identity, and content-change checks.

## 10. Implementation Sequence and Deliverables

| Phase | Work | Delivery gate |
| --- | --- | --- |
| S0: Versions and fixtures | Verify cores, URI specifications, combinations, and sanitized fixtures | Identify usable output formats for each configuration using evidence |
| S1: Diagnostics first | Typed errors, business results, counts, migrations, cache semantics, and UI messages | Existing unsupported entries have clear explanations; failures no longer appear successful |
| S2: SS2022 | Keys/URI encoding, persistence, export, and partial success | Three synthetic AES-256 entries round-trip and pass actual connection tests |
| S3: Reality | Security fields, fingerprints, aliases, valid TCP/grpc combinations, and export | Supported combinations retain all parameters; invalid combinations have precise reasons |
| S4: xhttp | Typed parameters, capability checks, URI fidelity, and supported-format export | Eleven synthetic xhttp entries retain their semantics; unsupported formats report omissions; a compatible client passes actual connection tests |
| M1: Self-hosted contracts/capabilities | Server-side Reality/xhttp models, shared validation, direction/version/combination capabilities, and older-Agent gates | Capability matrix, saving, and deployment agree; unknown support cannot pass |
| M2: Xray end-to-end support | Keys, Xray rendering, native validation, deployment/health/rollback, and published-subscription mapping | A temporary self-hosted VLESS node accepts real client connections, with correct rollback and credential boundaries |
| M3: Editor and protocol errors | xhttp/Reality choices, fields, key state, combination dependencies, and layered errors | Users can save, deploy, and obtain correct subscriptions, beyond merely seeing options |
| M4: sing-box adaptation | Verified Reality/transport combinations on a pinned version | Every enabled combination has native-validation and connection evidence for sing-box; unsupported xhttp stays unavailable |
| S5: Combined regression/documentation | Imports, self-hosting, authorization, dependencies, empty groups, old/new Agents, backup/restore, real cores, and documentation | Both workflows meet their declared capabilities with complete gate results and evidence |

Dependencies: S0 precedes all protocol implementation; S1 can ship independently first. S2–S4 establish client models and format capabilities. M1 → M2 → M3 delivers self-hosted Xray support, with M2/M3 depending on the corresponding client export work. M4 reuses M1, but Xray results do not substitute for sing-box acceptance. Run S5 after the declared scope is complete; external import completion does not establish self-hosted completion.

Append development records for each phase: request, files, before/after behavior, field/migration changes, verification commands/results, remaining work, and rework. Update the supported-capability matrix only after the corresponding feature passes.

S0 status (2026-11-05, revision 3): evidence collected; review rounds 1 and 2 did
not pass — those rounds stay on record as historical failures — and their
corrections are applied — revision 2 re-ran the two dropped
cells and replaced remembered SIP002 wording with the quoted specification;
revision 3 makes `S0-GR-02` a **second valid no-flow gRPC representative entry**
(Vision + gRPC stays only as `S0-NEG-VISION-GRPC`), states the two mihomo cells as
"first attempt failed, later attempt succeeded, cause undetermined", and describes
the sing-box xhttp cell as a native config rejection with no HTTP attempted.
**An independent review passed on 2026-11-05 for the evidence and manifest scope
only — not full protocol acceptance. The documentation commit is local on
`feat/v1.4-import-s0`, and the Linux gate on that commit was still pending when it
was created.** The report is
[v1.4 S0 evidence](v1.4-s0-evidence.md) and the sanitized fixture manifest is
[v1.4 S0 fixtures](v1.4-s0-fixtures.md) (16 **valid** representative entries — 3
SS2022 AES-256, 11 Reality+xhttp, 2 Reality+grpc — with negative, compatibility
and duplicate fixtures named separately); the development record is the last entry
of the [change log](subscription-import-compatibility-change-log.md). The
measurements pin Xray 26.3.27, sing-box 1.14.3 and mihomo 1.19.33 and establish
support **per (combination, client) cell**, never per protocol: 35 matrix records
in `20261105T090351Z`, two of them re-executed because the first pass printed but
did not persist them.

What rounds 1 and 2 corrected, and what S1–S4 must now assume:

- the canonical SS2022 export is `ss://` + percent-encoded plain
  `method:password`; SIP002 forbids Base64URL userinfo for AEAD-2022, so
  Base64URL is an input compatibility path only. No probe exercised URI text, so
  the encoding rule rests on the quoted specification (evidence §7);
- the capability table is a per-client grid: mihomo was measured with xhttp
  `auto` only, Reality+TCP without Vision and Reality+gRPC have no mihomo or
  sing-box cell, gRPC and the xhttp modes other than `auto` are Xray-only,
  AES-128 identity is sing-box-only, chacha20 single-key is mihomo-only;
- the two representative gRPC rows are **valid no-flow entries**, and the only
  Vision + gRPC case is the negative fixture `S0-NEG-VISION-GRPC`; a `serviceName`
  that must be percent-encoded in the URI (`S0-GR-02`) is a codec decision, like
  the SS2022 identity separator, not a measured result;
- the Vision + gRPC/xhttp failures are the **client's** VLESS outbound rejecting
  the flow, with zero origin hits — not a server rejection; the sing-box xhttp cell
  is a different case (native client-configuration rejection, no HTTP attempted);
- the first mihomo Reality dial failed and its cause is **undetermined**, so mihomo
  positives are labelled later-attempt positives (first attempt failed, second
  succeeded) and no first-attempt guarantee exists.

Still open before S0 can be called complete: a passing independent review of this
revision, then a Linux gate on the commit that carries it. S0 changed no production
code, migration, API or UI — its output is documentation and measurements only. S1
is now in progress in a separate worktree, and no S1 completion is claimed here.

## 11. Acceptance Criteria

1. Use synthetic equivalents of 3 SS2022 AES-256, 11 Reality+xhttp, and 2 Reality+grpc entries. Record parsing, persistence, output, and target-core results individually. Valid combinations preserve required parameters; invalid ones receive accurate explanations.
2. Import, duplicate, and rejection counts agree. Empty documents, HTML pages, Base64 failures, and entirely unsupported subscriptions have distinct results.
3. Valid/invalid/empty keys, identity-key chains, conflicting Reality parameters, duplicates, unknown security fields, and oversized extra options have explicit outcomes.
4. URI → neutral model → encrypted storage → readback → URI/Base64/supported YAML/JSON preserves semantics. Existing endpoints retain compatibility.
5. A format-specific omission does not fail the entire subscription. Chains, groups, and emptyPolicy have no dangling references or unexpected direct fallback.
6. Distinguish HTTP 401/403/429/5xx, DNS, TLS, timeout, size, redirect, and private-address rejection. Preserve existing SSRF tests, with no sensitive URLs in errors.
7. Success, partial success, complete failure, prior cache, initial fetch, and post-URL-change outcomes match the UI. Failures do not produce success toasts.
8. Scheduled and manual refreshes share diagnostics. Interruption, cancellation, and concurrency do not damage cache state; recording failure successfully is not a successful refresh.
9. Logs, API diagnostics, and test snapshots do not leak tokens, passwords, UUIDs, key chains, complete URIs, or response bodies. Detail counts and sizes are bounded.
10. Old/new database and backup restoration preserve protocol fields, source status, authorization, and stable references, without automatic refresh or activation side effects.
11. Validate native configurations with pinned Xray/sing-box/Mihomo versions. Test real handshakes and HTTP requests with temporary credentials and controlled servers; real subscription credentials are not CI fixtures.
12. Distinguish configuration acceptance from connection success. Syntax checks do not replace Reality handshakes, SS2022 key validation, or xhttp requests.
13. Run Linux `scripts/check.sh` and the applicable real-core suites under AGENTS.md. Perform browser checks of source management and format previews; retain evidence before declaring completion.
14. The self-hosted editor, ProtocolSettings, capability matrix, Agent reporting/rendering, and published subscription agree for each combination. Older Agents and unknown capabilities cannot save/deploy unsupported settings.
15. Generate and encrypt Reality private keys securely. Ordinary saves do not rotate keys. Explicit rotation, redeployment, and rollback behavior are defined. Private keys stay out of client subscriptions, logs, and previews.
16. Declared Xray Reality/xhttp combinations pass user authentication and HTTP requests on real temporary nodes. Invalid parameters, port conflicts, native-validation failures, and health failures use existing failure/rollback paths without false success.
17. Verify sing-box combinations independently. Insufficient capabilities are rejected by both frontend and backend without silently switching core, security, or transport.
18. Self-hosted drafts do not enter subscriptions prematurely. After successful deployment, users receive public-key/SNI/transport/credential values matching the listener. Backup/restore and historical revisions retain private material without automatic deployment.

Reuse `importer/parse_test.go`, `fetch_test.go`, `store_test.go`, handler tests, `format/*` tests, `platform/server` integration tests, and `backup/imports_test.go`. Add meaningful behavior tests for new protocols and semantics rather than source-string assertions.

For self-hosted support, reuse desiredstate validation, panel/proxy save/snapshot tests, Agent Xray/sing-box rendering and real-core tests, multi-core application/reconciliation rollback tests, and existing node-editor self-tests. Connection fixtures use controlled targets and temporary keys without affecting production nodes.

## 12. Original Planning Work Record

This section retains the original review scope. The overall scope is now defined by Sections 1, 10, 11, and 13, together with the latest development-log entries.

- Reviewed the importer, neutral proxy model, URI/Mihomo/sing-box exporters, messages, APIs, backup validation, and version evidence.
- Confirmed the branch already includes committed Client Policy Phase B and migration head 0132. The plan uses that baseline rather than earlier uncommitted-state assumptions.
- Added the plan and development log without changing functionality, migrations, the database, or runtime configuration.
- Did not refetch a real subscription or run tests, builds, or real-core validation. Phase 0 still requires the evidence listed in its matrix.

## 13. Self-Hosted xhttp / Reality Implementation Design

Self-hosted support is included in M1–M4 and joins external-import phases S0–S4 for S5 acceptance. This section details server models, capability gates, private material, and deployment paths. All capabilities remain proposed.

### 13.1 Current Gaps

- `web/src/modules/proxy/components/node-editor/NodeProtocolFields.vue` offers tcp/ws/grpc transports and none/tls security only.
- `pkg/contracts/desiredstate/ProtocolSettings` has neither server-side Reality nor xhttp structures. ValidateSettings rejects other security/transports, and Vision currently requires TCP TLS.
- `/protocol-support` declares none/tls for VLESS. A core reporting VLESS support does not establish every security/transport combination.
- Xray/sing-box Agent adapters do not render the new fields. Frontend-only options would fail when saved, deployed, or applied.

### 13.2 Relationship Between the Workflows

```text
External endpoints:
subscription parsing → client model → encrypted cache → client subscription export

Self-hosted nodes:
editor → backend inbound model/validation → DesiredState
       → Agent rendering/native validation/application → user subscription export
```

Reuse parameter specifications, combination checks, and client export across both workflows. Self-hosting additionally needs server models, key management, capability negotiation, and deployment rollback. Completing imports alone does not make node-creation options usable.

### 13.3 Recommended Implementation Order

1. **Start with self-hosted Xray VLESS.** Verify Reality+TCP, Reality+xhttp, TLS+xhttp, and other supported combinations on the pinned version. Arbitrary combinations found in external subscriptions are not automatically valid server configurations.
2. **Extend shared contracts and validation.** Add typed optional ProtocolSettings for xhttp host/path/mode and Reality handshake target, permitted server names, Short IDs, and keys. Validate flow/TLS/transport combinations and mutual exclusions. Reuse existing validation, Vault, and revisions without opaque JSON passthrough.
3. **Add capabilities by core, direction, and settings contract/version.** Extend existing capability matrices/reports to identify supported inbound/outbound combinations. Older Agents lacking the contract cannot select or receive the settings. Neither a VLESS flag nor version text alone establishes support.
4. **Implement keys and rendering.** Reality uses X25519 key pairs. If the panel generates them, use a cryptographically secure server-side random source and encrypted persistence. Lists, logs, previews, and plans exclude private keys. The Agent receives private material needed for deployment through existing secure file, validation, health, and rollback mechanisms. Subscriptions include only the client public key/Short ID/SNI/fingerprint, never the server private key or complete internal configuration.
5. **Connect the editor.** Under VLESS, expose security and transport options according to the selected core's capabilities, with Reality fields/key generation and xhttp fields. Explain unsupported choices. Handle mutually exclusive fields during switching without silently losing input or submitting hidden stale parameters.
6. **Verify user subscriptions.** Map successfully published settings to the neutral client model and reuse URI/Mihomo/sing-box export. Continue requiring authorization and published credentials; drafts must not enter subscriptions.

Adapt self-hosted sing-box Reality independently under the same capability framework, according to the pinned core's actual support. Enable xhttp only where that core supports it. GOST does not carry these user-protocol settings. If self-hosted SS2022 is added later, it also requires server-side user-key and hidden-identity handling; imported-cipher allowlists alone cannot establish completion.

Model boundaries: inbound DesiredState includes server-private fields. Node outbounds and subscription/proxy contain only client connection parameters. Share public portions, validators, and codecs, but never serialize the complete server structure into a share URI. Map Reality handshake targets/server names/Short IDs separately from client SNI/public key/short ID. Validate ordinary TLS certificates, Reality keys, and Vision flow by supported combination; do not require certificate uploads for every Reality configuration through the old TLS branch.

Recommended key lifecycle: generate on explicit creation/generation, not ordinary edits. Rotation is a separate user action that explains the need for clients to update subscriptions. Save its draft and apply through existing deployment. Deployment failure restores the previous successful revision and its keys; a new draft key must not overwrite an older Revision. Preserve existing credential authorization and Snapshot logic without changing hidden user identities.

### 13.4 Self-Hosted Completion Criteria

- UI, server validation, capabilities, Agent behavior, and client subscriptions agree for each combination.
- Unsupported settings are not sent to old Agents. Invalid native configurations fail validation and follow rollback behavior.
- Temporary nodes can generate valid keys, deploy, accept real client connections, and verify user authentication and existing measurement coverage.
- Redeployment, rollback, and backup/restore after xhttp/Reality changes preserve the right configuration and private material.
- URI/YAML/JSON output matches the actual server listener, with visible support differences and omission reasons.
- Verification uses real cores, controlled services, and temporary credentials, rather than merely asserting that a dropdown exists.

This document update changes planning and records only. It does not change the node editor, backend, Agent, production nodes, or database.

Translation verification: section coverage, technical identifiers, phase names, and Markdown structure were checked against the Chinese plan. Functional tests and core checks were not run for this documentation-only translation.

## 14. Review Decisions (2026-11-05)

Reviewed against `feat/v1.4-tunnels` HEAD `5387802` (client policy Phases C and D landed after the `c708c35` baseline). The gap descriptions in §2 and §13.1 were re-checked and still hold:

- `Fetcher` collapses every failure into `ErrFetch`.
- SS2022 ciphers are absent from the `parse.go` allowlist.
- `proxy.TLS`/`proxy.Transport` have no Reality, uTLS or xhttp fields.
- `format/uri` rejects security other than none/tls.
- `format/singbox` returns an error for the whole document on unsupported TLS.
- `desiredstate.ValidateSettings` ties Vision to TCP+TLS.
- The node editor offers tcp/ws/grpc and none/tls only.
- **`Store.Refresh` returns the result of the failure-status write.** A failed fetch or parse therefore reaches the handler as `nil`, and the page shows a success toast. This is a present-day bug, not only a missing feature.

The plan is approved with the amendments below. Where they conflict with earlier sections, this section wins.

### 14.1 Amendments

1. **Two deliverables, declared separately.**
   - **Import track:** S0–S4, plus S1 diagnostics.
   - **Self-hosted track:** M1–M4.
   - They share the client-side model and codecs from S3/S4, so M2 starts only after the S3 neutral-model fields are settled. They are released and accepted separately, and S5 runs once per track. Finishing one track says nothing about the other.

2. **Stable import keys must be unique within a source.**
   - Today `replace` deduplicates only by content hash. Two entries with the same `name|protocol` (for example, an xhttp and a grpc variant sharing a name) are both stored.
   - `node_group_imports.proxy_key` and `@endpoint:import:<source>:<name|protocol>` then select both. Phase B assumed one key names one endpoint.
   - New types will make this collision common. From S1 on:
     - the first entry with a given key is kept;
     - later ones are rejected with `duplicate_endpoint_key` and counted under `duplicates`, separately from content duplicates;
     - the diagnostic says which entry was kept.
   - This changes current behavior (both are imported today). Record it in the release notes.

3. **The per-entry omission contract (§5 item 5) applies to every exporter path, self-hosted endpoints included.** Internal corruption stays an error. §5 item 6 needs no new mechanism:
   - Empty-group handling after format filtering already exists (`rules.EmptyGroup`, used by `Expand` and the Clash exporter). Reuse it.
   - The sing-box output is an outbound fragment without groups, so item 6 applies to mihomo and legacy Clash only.

4. **v1.4 relay endpoints.** Linked relay rules copy the target inbound's protocol fields into relayed subscription endpoints (AGENTS.md §10.3). Once self-hosted Reality/xhttp inbounds exist:
   - Relayed endpoints must carry the same Reality/xhttp client fields, with the original SNI kept.
   - The relay network filter must classify the transport correctly. Restrict the first release to TCP-carried xhttp modes, or classify an HTTP/3 mode as UDP.
   - Add both to M2 acceptance.
   - The protocol matrix and release notes currently say "Reality is not implemented and relays add no support." Update them when M2 ships.

5. **Device limit and metering for xhttp.** xhttp is often deployed behind a CDN. The node then sees CDN edge addresses, so the source-address device estimate (AGENTS.md §9.2) merges or scatters clients.
   - Document this as a limitation.
   - M2 acceptance verifies user traffic stats and the device gate for Reality/xhttp on a direct connection, and records the row in the §9.2 UDP/coverage table.

6. **sing-box xhttp.** qui sing-box builds are unmodified upstream, so qui will not patch xhttp in. M4 covers Reality only unless the pinned upstream gains xhttp, as §3 Phase 0 shows.

7. **Reality handshake target.**
   - The node connects to the configured target for every unauthenticated probe.
   - The editor requires the Owner to enter it, with no built-in default site.
   - The panel validates only host:port syntax. Agent native validation is the final gate, consistent with AGENTS.md §13.1.

8. **Release placement (superseded by 14.7).** All work in this plan lands on `feat/v1.4-tunnels` and ships in v1.4.0. Migrations take the next unused numbers after 0132, assigned in merge order (14.7).

9. **Process.** Same as the client-policy work:
   - one phase per delegation;
   - review before each local commit;
   - a Linux gate per phase commit;
   - change-log entries per phase;
   - no live refetch of the real source without Owner approval;
   - sanitized fixtures only.

### 14.2 Docs to Update When Each Part Ships

- AGENTS.md:
  - §10.3 (import formats, refresh results)
  - §12 (VLESS security/transport options)
  - §9.2 (coverage table)
  - §26.9 (migration row)
  - §31 (backup row for the new source fields)
- `docs/protocol-support-matrix.md`.
- The release-notes line about missing Reality support.

### 14.3 Owner Decisions (2026-11-05)

| # | Question | Decision |
| --- | --- | --- |
| R1 | Self-hosted track now, or import track only? | **Both tracks run in parallel** (see 14.4). |
| R2 | Fix the refresh false-success bug in v1.4.0, ahead of S1? | **Yes, fix in v1.4.0** (scope in 14.5). |
| R3 | Duplicate `name|protocol` keys: keep the first, reject the rest? | **Yes — confirmed by the Owner 2026-11-05** (14.6). |
| R4 | Target version. | **v1.4.0 for both tracks** (changed from v1.5.0 on 2026-11-05; see 14.7). |

### 14.4 Running the Two Tracks in Parallel (R1)

This replaces the "M2 starts only after S3" ordering in 14.1 item 1.

- **Can run side by side from the start:**
  - S0 evidence and S1 diagnostics (import track).
  - M1 contracts, settings and capability gates (self-hosted track).
  - These touch disjoint code: `subscription/importer` and the refresh UI, versus `desiredstate`, the capability reports and the Agent.
- **One shared step:** the client-side Reality/xhttp fields in `subscription/proxy`, together with the URI/Mihomo/sing-box codecs. Both tracks use them: imports read and write them, and self-hosted subscriptions export through them.
  - Define them once, in S3/S4.
  - M2's subscription mapping consumes them and must not add a second copy.
  - If M2 reaches that point first, it waits for, or takes over, that one step. Two parallel definitions are not allowed.
- **Each track still has its own acceptance (S5).** Both ship in v1.4.0, but finishing the import track does not imply the self-hosted track is complete, and the reverse is also true.
- **Branches:** see 14.7.

### 14.5 v1.4.0 Refresh Fix (R2)

This is the first step of the v1.4.0 work, ahead of S1. **Status: implemented on `feat/v1.4-tunnels` as `650b349`; the Linux gate on that commit passed and was independently verified — `check` 1980 s, `cores` 572 s, `GATE 650b349 PASS total=2552s`, END 2026-11-05T09:30:12Z.** Minimal scope:

1. `Store.Refresh`: after recording `last_status='failed'`, return the fetch or parse error, not the result of the status write.
2. Refresh handler: map `ErrFetch`/`ErrParse` to a distinct code, `import_refresh_failed`, with a message saying the fetch or parse failed and the source's nodes are excluded from subscriptions. Other errors keep `import_unavailable`.
3. Source page: reload the source list even when the refresh fails (all three callers: add, edit with a new URL, manual refresh), so the saved source and its failed status show up. The error toast replaces the success toast.
4. Update the existing store test, which currently expects `nil` from a failed refresh, so that it expects the error.
5. The scheduled refresh ignores the error, as it does today.

Everything else (typed causes, counts, partial success, 502/504/422, persisted diagnostics) follows in S1, also for v1.4.0. Record the fix in the change log, and run a Linux gate on its commit.

Implementation status (2026-11-05): items 1–5 are implemented as written — `Store.Refresh` returns the fetch or parse error after recording the failure, the handler answers `import_refresh_failed`, the three page callers reload the source list on failure, and the store test now expects the error. New coverage: `internal/panel/subscription/importer/refresh_test.go` and `web/scripts/import-refresh-selftest.mjs`, the latter invoked from `scripts/check.sh`. The Linux gate ran on `650b349` and passed (numbers above), so this status is "implemented and gate-verified". It is still **not** an A1 result: the browser walkthrough of the source page against a real failing source remains outstanding, and item 3 is covered in the gate only by the injected-state frontend self-test.

### 14.6 Duplicate Keys: Keep the First (R3, Confirmed)

Node groups (`proxy_key`) and client-policy members (`@endpoint:import:…`) save the key `name|protocol` and expect it to name exactly one endpoint.

| Option | Effect | Problem |
| --- | --- | --- |
| **Keep the first, reject later ones with `duplicate_endpoint_key` (chosen)** | Every saved reference keeps pointing at one endpoint. The rejected entry shows up in the diagnostics with the kept entry's index. | Rejected entries do not reach users. The source owner, or the Owner using a paste import, must rename one of them. |
| Keep all (today's behavior) | Nothing is dropped. | One reference silently grants or selects several endpoints. Phase B's "one key, one endpoint" assumption breaks, and new transport variants make this common. |
| Rename automatically (for example, append `#2`) | Nothing is dropped. | Suffixes depend on the order of entries in the source. When the provider reorders entries, saved references silently move to a different endpoint. |
| Add transport/security to the key | Variants become distinct. | Changes the key format of every existing `proxy_key` and `@endpoint:import` reference. Needs a migration, and the key still changes whenever the provider changes transport. |

"First" means first in the order of the fetched document. Release notes must state that entries imported today under a duplicate key will drop to one after upgrading.

### 14.7 Everything Ships in v1.4.0 (Owner Decision, 2026-11-05)

The Owner moved all remaining work into v1.4.0: the refresh fix (14.5), the import track (S0–S4, S1) and the self-hosted track (M1–M4), each accepted through its own S5. This replaces the v1.5.0 target and the "after v1.4.0" placement.

- **The release waits for the work.**
  - The v1.4.0 RC (handoff C1) and A1 acceptance start only after both tracks pass S5.
  - A1 covers them on the hosts: real clients using imported SS2022/Reality/xhttp entries, and a self-hosted Xray Reality/xhttp node with relayed subscription entries (14.1 item 4).
  - The tunnel work already passed its Linux gate (A1a `8134314`). Every later commit needs its own gate, and the RC is built from the final gated commit.
- **Branches.**
  - Each track works on a short-lived branch off `feat/v1.4-tunnels` and merges back after its phase passes review and the Linux gate. Gates run one at a time, alternating between the tracks.
  - The 14.5 fix goes directly on `feat/v1.4-tunnels`; it is committed as `650b349` and its Linux gate passed.
- **Migrations.** Numbers continue after 0132 in merge order. A track that needs a migration takes the next free number when it merges, and renumbers if the other track merged first. Applied migrations are never edited.
- **Agent compatibility.** v1.4.0 Agents report the new self-hosted capability, while v1.3.1 Agents cannot receive the new settings (14.1 and §13.3 item 3). The upgrade order stays panel first, then Agents.
- **Docs to update at release.**
  - The v1.4.0 release notes: new protocols, the refresh fix, duplicate-key behavior, the CDN device-limit limitation, and removal of "Reality is not implemented".
  - AGENTS.md "当前状态" and the sections in 14.2.
  - The v1.4 plan.
  - `docs/handoff.md` (C1 and §4.2 now wait on this work).
