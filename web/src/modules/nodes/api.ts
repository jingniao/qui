import type { ReleaseEntry } from '../system/coreUpdatesApi';
export interface CollectorLossStatus {measured?:boolean;status:"ok"|"unavailable";generation?:number;dropped_allowed_observations?:number;dropped_blocked_observations?:number;rejected_observations?:number;persistence_failures?:number;unconfirmed_processes?:number;baseline_at?:{seconds?:number;nanos?:number};checkpoint_at?:{seconds?:number;nanos?:number};counters_saturated?:boolean;coverage_incomplete?:boolean;end_confirmed?:boolean;}
export interface TrafficSourceStatus {status:"busy"|"unavailable"|"unobserved"|"partial"|"covered"|"not_running"|"sampling_failed";core_epoch?:string;revision?:number;configured_sources?:number;observed_sources?:number;core_running?:boolean;observed_at?:{seconds?:number;nanos?:number};last_success_at?:{seconds?:number;nanos?:number};}
import { request, type Page } from '../../app/apiClient';
import type { PublicAddress } from './nodeAddress';
export interface CollectorStatus {persisted_loss?:CollectorLossStatus;process_id:string;status:'ok'|'observation_loss'|'persistence_degraded';pending_observations?:number;aggregated_rollups?:number;dropped_allowed_observations?:number;dropped_blocked_observations?:number;rejected_observations?:number;persistence_failures?:number;counters_saturated?:boolean;started_at?:{seconds?:number;nanos?:number};observed_at?:{seconds?:number;nanos?:number};last_flush_at?:{seconds?:number;nanos?:number};last_flush_success_at?:{seconds?:number;nanos?:number};}
// Outbound network settings of one node. The first two pin the proxy core's own
// dials (A); the last two are enforced by the host-network helper for every new
// connection of the server (B). An empty string means "not configured".
export interface NodeNetworkSettings {
  outboundSourceIp: string; outboundInterface: string; hostSourceIpv4: string; hostSourceIpv6: string;
}
// The body of PUT /api/v1/nodes/{id}: the node basics plus the outbound network.
// The two address fields are optional because they carry meaning only when the
// Owner changed them: a non-empty publicIp is stored as a manual value, and an
// omitted field keeps whatever the panel holds.
export interface NodeSettingsInput extends NodeNetworkSettings {
  id: string; name: string; enabled: boolean; publicIp?: string; publicIpSelected?: string; connectHost?: string; allowSubscription: boolean; maintenance: boolean;
}
export interface NodeView extends NodeNetworkSettings {
  id: string; name: string; status: 'pending' | 'online' | 'degraded' | 'offline' | 'applying' | 'rollback' | 'disabled' | 'error';
  agentUpdateSupported: boolean;
  // Server-group tags (v1.5.5 item 4); the panel always sends a list.
  tags?: string[];
  // The Agent can install GOST (Hello.gost_core_supported).
  gostCoreSupported?: boolean;
  enabled: boolean; allowSubscription: boolean; maintenance: boolean; hostname: string; os: string; agentVersion: string; publicIp: string;
  tuningAdviceCount?: number;
  // Where the public address came from: manual means the Owner typed it and the
  // Agent never replaces it, auto means the panel filled it from detection, and
  // an empty source means there is no address at all.
  publicIpSource: '' | 'manual' | 'auto' | 'selected';
  // ISO country code of publicIp from the panel's offline table, '' when unknown.
  countryCode?: string;
  publicAddresses?: PublicAddress[];
  publicIpSelected?: string;
  publicIpSelectionMissing?: boolean;
  // Client-facing address when the Owner set one (domain or IP literal); the
  // subscriptions and share links prefer it over publicIp.
  connectHost: string;
  lastHeartbeat: string; createdAt: string;
  // Set once the node was logically deleted; an empty string means "live".
  deletedAt: string;
  coreSelectionBlocked?:CoreName[];
  capabilities: { multi_core_supported?:boolean; cores?:CoreCapability[]; host?: HostInfo; tuningSupported?: boolean; core_name?: string; core_version?: string; singbox_user_stats?: boolean; tls_certificate_files_supported?: boolean; server_settings_contract_version?: number; server_settings_combinations?: ServerSettingsCombination[]; protocols?: { protocol: string; inbound?: boolean; outbound?: boolean; tcp?: boolean; udp?: boolean }[]; panel_catalogue_supported?: boolean; port_hop_supported?: boolean; };
  heartbeat: { bootId?: string; defaultRouteInterface?: string; cores?:CoreStatus[]; load1?: number; load5?: number; load15?: number; processCount?: string; tcpConnections?: string; udpSockets?: string; memoryUsedBytes?: string; swapUsedBytes?: string; diskUsedBytes?: string; tuningCurrent?: Record<string,string>; traffic_source_status?:TrafficSourceStatus; collector_status?:CollectorStatus; spool_status?: { failure_code?: "queue_unavailable"|"capacity_unavailable"|"capacity_invalid"|"access_head_unavailable"|"access_head_invalid"|"traffic_head_unavailable"|"traffic_head_invalid"|"observation_timeout"|"observation_interrupted"; measured?: boolean; payload_bytes?: number; disk_bytes?: number; disk_limit?: number; database_bytes?: number; database_limit?: number; pending_batches?: number; dropped_allowed_rollups?: number; status: "ok" | "near_capacity" | "capacity_exceeded" | "unavailable"; observed_at?: { seconds?: number; nanos?: number } }; cpu_percent?: number; memory_percent?: number; disk_percent?: number; active_connections?: number; active_connections_measured?: boolean; active_connections_scope?: string; active_connections_observed_at?: { seconds?: number; nanos?: number }; upload_bytes?: number; download_bytes?: number; current_revision?: number; successful_revision?: number; core_healthy?: boolean; core_error?: 'core_unhealthy' | 'accounting_interrupted' };
}
// The panel publishes the release version, the gRPC port and the digest of the
// certificate trust anchor so the register dialog can build the one-command
// install. A field is empty when the panel cannot supply it: a local build has
// no release version, development mode has no certificate to trust.
export interface InstallInfo { version: string; grpcPort: number; caSha256: string; }
export interface Enrollment { node: NodeView; enrollmentToken: string; expiresAt: string; install: InstallInfo; }
export interface RecoveryStatus { id:string; nodeId:string; previousFingerprint:string; newFingerprint?:string; owner:string; revokedOnIssue:boolean; status:string; redeployState:string; redeployRevision:number; redeployCode?:string; createdAt:string; expiresAt:string; consumedAt?:string; }
// The public repository the release archives and node-install.sh come from.
export const releaseRepository = 'jingniao/qui';
// The single line the register dialog shows: it installs the agent and the core,
// establishes trust in the panel certificate and enrolls the node. The script
// validates the host, ports, digest and token again on the node. The line starts
// by installing what the command needs: a stock Alpine node has neither bash nor
// curl, and a minimal Debian or Ubuntu install may have no curl. The prefix is a
// no-op on a host that already has them, and the trailing `;` keeps it valid in
// busybox ash, dash and bash. --no-insecure overrides a -k that a curl wrapper
// on the node may add, so the script is never fetched without verified TLS.
export function installCommand(input: { version: string; host: string; httpPort: number; grpcHost?: string; grpcPort: number; caSha256: string; token: string; core?: string; cores?: CoreName[]; webUpgrade?: boolean }): string {
  // `--core` is repeatable in the K3c installer: one flag per selected core. The
  // selection is deduplicated and allowlisted; the default remains ["xray"]. GOST
  // ships one linux/amd64 archive, so the installer itself refuses that flag on
  // any other platform.
  const selected = [...new Set(input.cores ?? (input.core ? [input.core] : ["xray"]))];
  if (!selected.length || selected.some(core => core !== 'xray' && core !== 'sing-box' && core !== 'gost')) throw new Error('请至少选择一个运行内核（xray、sing-box 或 gost）');
  const coreFlags = selected.map(core => ` --core ${core}`).join("");
  // Both ports are always passed explicitly so the command never depends on
  // node-install.sh's own defaults (22550 / 18880): a panel that keeps a different
  // port pair still installs a node that connects to the port it actually serves.
  const portFlags = ` --http-port ${input.httpPort} --grpc-port ${input.grpcPort}`;
  // --grpc-host is only emitted when it differs from --panel: the installer
  // defaults it to the panel address, so an identical value would be noise.
  const grpcHostFlag = input.grpcHost && input.grpcHost !== input.host ? ` --grpc-host ${input.grpcHost}` : '';
  const bootstrap = 'if command -v apk >/dev/null 2>&1; then apk add --no-cache bash curl ca-certificates; '
    + 'elif ! command -v curl >/dev/null 2>&1 && command -v apt-get >/dev/null 2>&1; then apt-get update -qq && apt-get install -y -qq curl ca-certificates; fi; ';
  return `${bootstrap}curl --no-insecure -fsSL https://github.com/${releaseRepository}/releases/download/v${input.version}/node-install.sh | bash -s -- --version ${input.version} --panel ${input.host}${grpcHostFlag}${portFlags} --ca-sha256 ${input.caSha256} --token ${input.token}${coreFlags}${input.webUpgrade ? ' --web-upgrade' : ''}`;
}

export interface HealthCheck { core?:CoreName|""; id:string;nodeId:string;revision:number;status:"queued"|"complete"|"expired";createdAt:string;deadline:string;result?:{healthy?:boolean;code:string;observed_at?:{seconds?:number;nanos?:number}}; }
export interface RestartTask {core?:CoreName|"";commandId:string;revision:number;status:"pending"|"success"|"failed";code:string;createdAt:string;}
export interface CertificateObservation {issuer?:string;fingerprint?:string;notBefore?:string;expiresAt?:string;status:string;integrity:'consistent'|'invalid';revokedAt?:string;}
export interface CertificateStatus {identitySnapshotDigest:string;signingCAFingerprint?:string;currentSigningAvailable:boolean;nodeId:string;enabled:boolean;observedAt:string;active:CertificateObservation|null;pending:(CertificateObservation&{createdAt:string;purpose:'renewal'|'key_rotation'|'unknown';sourceMatches:boolean})|null;}
export interface PublishedTopology {
  nodeId:string;revision:number;reportedRevision:number;observation:'unknown'|'disabled'|'fresh_control'|'stale_control'|'revision_mismatch';snapshot:'unpublished'|'available'|'unavailable';
  relays:{outboundId:string;targetNodeId:string;protocol:string;targetRevision:number;dependency:'unpublished'|'missing_node'|'disabled'|'unavailable'|'matched'|'mismatch';dependencyDetail?:string}[];
  chains:{id:string;node_ids:string[];outbound_ids:string[];failover:string;final_outbound_id?:string;backup_target_id?:string}[];
}
export interface EnrollmentStatus {nodeId:string;observedAt:string;code:string;summary:string;receiptState:string;originalFingerprint?:string;originalCreatedAt?:string;originalExpiresAt?:string;currentCertificate:CertificateObservation;currentIdentityMatches:boolean;currentCertificateValid:boolean;enabled:boolean;identityRevoked:boolean;lastSequence:number;pendingRenewal:boolean;tokenState:string;tokenExpiresAt?:string;centralReplayConditionsMatch:boolean;localInstallationConfirmed:boolean;stateRecoveryConfirmed:boolean;nextSteps:string[];}
// What a delete would do, and what still references the node.
export type DeletionMode = 'cancel' | 'delete' | 'deleted';
export type DeletionBlockerKind = 'inbound' | 'outbound' | 'outbound_group' | 'outbound_target' | 'proxy_chain' | 'policy' | 'deployment_batch';
export interface DeletionBlocker { kind: DeletionBlockerKind; id: string; name: string; }
export interface DeletionPreview { mode: DeletionMode; blockers: DeletionBlocker[]; groups: string[]; lastHeartbeat: string; online: boolean; }
export interface LatencyTargetsInput { custom: boolean; targetIds: string[]; }
export interface LatencyTargetsSettings extends LatencyTargetsInput { defaultTargetIds: string[]; }
// The Owner's saved order of the server list (v1.5.5 item 3). Revision 0 means
// nothing saved yet; ids may still name servers deleted since the save.
export interface ServerOrder { ids: string[]; revision: number; updatedAt?: string; updatedBy?: string; }
export const nodeAPI = {
  serverOrder: () => request<ServerOrder>('/system/server-order'),
  saveServerOrder: (ids: string[], expectedRevision: number) => request<ServerOrder>('/system/server-order', 'PUT', { ids, expectedRevision }),
  metrics: (id: string, hours = 1) => request<NodeMetricsResponse>(`/nodes/${encodeURIComponent(id)}/metrics?hours=${hours}`),
  tuning: (id: string) => request<NodeTuning>(`/nodes/${encodeURIComponent(id)}/tuning`),
  saveTuning: (id: string, input: NodeTuningInput) => request<{requestId:string;status:'pending'}>(`/nodes/${encodeURIComponent(id)}/tuning`, 'PUT', input),
  tuningRecommendations: (id: string) => request<RecommendationView>(`/nodes/${encodeURIComponent(id)}/tuning/recommendations`),
  dismissTuningRecommendation: (id: string, key: string, value: string) => request(`/nodes/${encodeURIComponent(id)}/tuning/recommendations/dismiss`, 'POST', { key, value }),
  tuningAutoApply: () => request<TuningAutoApplySettings>('/system/tuning-auto-apply'),
  saveTuningAutoApply: (enabled: boolean, expectedRevision: number) => request<TuningAutoApplySettings>('/system/tuning-auto-apply', 'PUT', { enabled, expectedRevision }),
  latencyTargets: (nodeId: string) => request<LatencyTargetsSettings>(`/nodes/${encodeURIComponent(nodeId)}/latency-targets`),
  saveLatencyTargets: (nodeId: string, input: LatencyTargetsInput) => request<LatencyTargetsSettings>(`/nodes/${encodeURIComponent(nodeId)}/latency-targets`, 'PUT', input),
  latency: (id: string, hours: number = 1) => request<LatencyResponse>(`/nodes/${encodeURIComponent(id)}/latency?hours=${hours}`),
  probeTargets: () => request<{ items: ProbeTarget[] }>('/probe-targets'),
  createProbeTarget: (target: { label: string; host: string; port: number; allowNonPublic?: boolean; scheduled?: boolean }) => request<ProbeTarget>('/probe-targets', 'POST', target),
  saveProbeTarget: (target: ProbeTarget) => request<ProbeTarget>(`/probe-targets/${encodeURIComponent(target.id)}`, 'PUT', {
    label: target.label, host: target.host, port: target.port, allowNonPublic: target.allowNonPublic, scheduled: target.scheduled
  }),
  deleteProbeTarget: (id: string) => request(`/probe-targets/${encodeURIComponent(id)}`, 'DELETE'),
	enrollmentStatus:(id:string)=>request<EnrollmentStatus>(`/nodes/${encodeURIComponent(id)}/enrollment-status`),
	certificateStatus:(id:string)=>request<CertificateStatus>(`/nodes/${encodeURIComponent(id)}/certificate-status`),
	issueRecovery:(id:string,body:{expiresInSeconds?:number;revokeNow?:boolean})=>request<Enrollment & {purpose:string}>(`/nodes/${encodeURIComponent(id)}/recovery-token`,'POST',body),
	recoveryStatus:(id:string)=>request<RecoveryStatus>(`/nodes/${encodeURIComponent(id)}/recovery-status`),
restartTasks:(id:string)=>request<{items:RestartTask[]}>(`/nodes/${encodeURIComponent(id)}/restart-core`),
restartCore:(id:string,expectedRevision:number,core:CoreName|""="")=>request<RestartTask>(`/nodes/${encodeURIComponent(id)}/restart-core`,"POST",{expectedRevision,confirmNodeId:id,core}),
healthChecks:(id:string)=>request<{items:HealthCheck[]}>(`/nodes/${encodeURIComponent(id)}/health-checks`),
requestHealth:(id:string,expectedRevision:number,core:CoreName|""="")=>request<HealthCheck>(`/nodes/${encodeURIComponent(id)}/health-checks`,"POST",{expectedRevision,core}),
  list: (page: number, keyword: string, deleted = false, pageSize = 20) => request<Page<NodeView>>(`/nodes?page=${page}&pageSize=${pageSize}&keyword=${encodeURIComponent(keyword)}${deleted ? '&deleted=1' : ''}`),
  get: (id: string) => request<NodeView>(`/nodes/${encodeURIComponent(id)}`),
  enroll: (name: string, connectHost?: string) => request<Enrollment>('/nodes/enrollment-token', 'POST', { name, ...(connectHost ? { connectHost } : {}) }),
  reissueEnrollment: (id: string) => request<Enrollment>(`/nodes/${encodeURIComponent(id)}/enrollment-token`, 'POST', {}),
  update: (node: NodeView, enabled: boolean) => request(`/nodes/${node.id}`, 'PUT', { name: node.name, enabled }),
  // The two address fields are sent only when they carry a value: an empty
  // string is a real instruction ("clear it, go back to detection"), so an
  // omitted field must stay absent instead of defaulting to one of the two.
  save: (node: NodeSettingsInput) => request(`/nodes/${node.id}`, 'PUT', {
    name: node.name, enabled: node.enabled,
    ...(node.publicIpSelected === undefined ? {} : { publicIpSelected: node.publicIpSelected }),
    ...(node.connectHost === undefined ? {} : { connectHost: node.connectHost }),
    allowSubscription: node.allowSubscription, maintenance: node.maintenance, outboundSourceIp: node.outboundSourceIp, outboundInterface: node.outboundInterface, hostSourceIpv4: node.hostSourceIpv4, hostSourceIpv6: node.hostSourceIpv6
  }),
  revoke: (id: string) => request(`/nodes/${id}/revoke-certificate`, 'POST'),
  deletionPreview: (id: string) => request<DeletionPreview>(`/nodes/${encodeURIComponent(id)}/deletion-preview`),
  machineTraffic: (id: string) => request<MachineTrafficSummary>(`/nodes/${encodeURIComponent(id)}/machine-traffic`),
  machineTrafficBatch: () => request<MachineTrafficBatch>('/nodes/machine-traffic'),
  machineTrafficSettings: (id: string) => request<MachineTrafficSettings>(`/nodes/${encodeURIComponent(id)}/machine-traffic/settings`),
  saveMachineTrafficSettings: (id: string, input: MachineTrafficSettings) => request<MachineTrafficSettings>(`/nodes/${encodeURIComponent(id)}/machine-traffic/settings`, 'PUT', input),
  deleteNode: (id: string) => request(`/nodes/${encodeURIComponent(id)}`, 'DELETE'),
  agentReleases: (refresh = false) => request<AgentReleaseVersion[]>(`/agent-releases${refresh ? '?refresh=1' : ''}`),
  importAgentRelease: (version: string) => request<ReleaseEntry[]>(`/agent-releases/${encodeURIComponent(version)}/import`, 'POST')
};

export interface ProbeTarget { id: string; label: string; host: string; port: number; allowNonPublic: boolean; scheduled: boolean; }
export interface LatencyPoint { at: string; medianMs?: number | null; avgMs: number | null; minMs: number | null; maxMs: number | null; sent: number; failed: number; samples?: number; }
export interface LatencyResponse {
  intervalSeconds: number;
  sampleSeconds?: number;
  targets: { id: string; label: string; host: string; port: number }[];
  series: { targetId: string; points: LatencyPoint[] }[];
}

// New camelCase observations use protobuf JSON: uint64 values are decimal strings.
export interface HostInfo {
  cpuModel?:string; cpuCores?:number; architecture?:string; kernelVersion?:string; memoryTotalBytes?:string; swapTotalBytes?:string; diskTotalBytes?:string; osPrettyName?:string;
  virtualization?: string;
  tuningKeys?: { key: string; present: boolean; writable: boolean }[];
  congestionAvailable?: string[];
  congestionLoadable?: string[];
  qdiscAvailable?: string[];
  qdiscLoadable?: string[];
  sysctlSources?: { key: string; file: string; value: string }[];
  fdLimits?: { name: string; soft: string | number; hard: string | number }[];
  tuning_keys?: { key: string; present: boolean; writable: boolean }[];
  congestion_available?: string[];
  congestion_loadable?: string[];
  qdisc_available?: string[];
  qdisc_loadable?: string[];
  sysctl_sources?: { key: string; file: string; value: string }[];
  fd_limits?: { name: string; soft: string | number; hard: string | number }[];
}

export interface RecommendationItem {
  key: string;
  value: string;
  reason: string;
  level: 'info' | 'suggested' | 'warning';
  rule: string;
  dismissed: boolean;
}

export interface RecommendationView {
  rulesVersion: string;
  items: RecommendationItem[];
}

export interface TuningAutoApplySettings {
  enabled: boolean;
  revision: number;
}

export interface NodeMetricPoint {
  at:string; cpuPercent:number|null; memoryPercent:number|null; diskPercent:number|null;
  memoryUsedBytes:number|null; swapUsedBytes:number|null; diskUsedBytes:number|null;
  load1:number|null; processCount:number|null; tcpConnections:number|null; udpSockets:number|null;
  activeConnections:number|null; rxBytesPerSecond:number|null; txBytesPerSecond:number|null;
  tcpRetransPercent?:number|null; samples:number;
}
export interface NodeMetricsResponse { intervalSeconds:number; sampleSeconds:60; points:NodeMetricPoint[]; }
export interface NodeTuningInput { settings:Record<string,string>; clear:boolean; confirmNodeId:string; }
// Machine traffic of one server: durable daily rows of one selected interface,
// bucketed in the panel zone. Byte totals are decimal strings so an int64 never
// passes through a rounded Number.
export type MachineTrafficMethod = 'sum' | 'outbound' | 'max';
export interface MachineTrafficSettings { method: MachineTrafficMethod; interfaceName: string; allowanceBytes: number | null; resetDay: number; }
export interface MachineTrafficSummary {
  nodeId: string; settings: MachineTrafficSettings;
  // Empty/unknown values are null: no interface resolved, no period rows, no cursor.
  interfaceName: string | null; timezone: string;
  periodStart: string; periodEnd: string; nextResetAt: string;
  rxBytes: string | null; txBytes: string | null; usedBytes: string | null;
  status: 'measured' | 'stale' | 'unknown'; lastObservedAt: string | null;
}
export interface MachineTrafficBatch { items: MachineTrafficSummary[]; timezone: string; }
export interface NodeTuning {
  supported:boolean; desired:Record<string,string>; requestId:string|null;
  status:'pending'|'applied'|'cleared'|'failed'|'unconfirmed';
  result:{requestId?:string;status?:string;keys?:{key?:string;value?:string;applied?:boolean;error?:string}[]}|null;
  requestedAt:string|null; updatedAt:string|null; current:Record<string,string>;
  initial?:Record<string,string>;
}

export type CoreName = 'xray' | 'sing-box' | 'gost';
export interface ServerSettingsCombination { protocol: string; direction: string; security: string; transport: string; flow?: string; }
export interface CoreCapability { name:CoreName; version?:string; protocols?:{protocol:string;inbound?:boolean;outbound?:boolean;tcp?:boolean;udp?:boolean}[]; singbox_user_stats?:boolean; forward_inbound_supported?:boolean; device_gate_supported?:boolean; tls_certificate_files_supported?:boolean; healthy?:boolean; observation_contract_version?:number; gost_contract_version?:number; server_settings_contract_version?:number; server_settings_combinations?:ServerSettingsCombination[]; }
export interface CoreStatus { name:CoreName; healthy:boolean; error?:string; started_at?:{seconds?:number;nanos?:number}; traffic_source_status?:TrafficSourceStatus; }

export interface AgentReleaseVersion {
  version: string;
  publishedAt: string;
  architectures: string[];
  imported: boolean;
}
