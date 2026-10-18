export interface CollectorLossStatus {measured?:boolean;status:"ok"|"unavailable";generation?:number;dropped_allowed_observations?:number;dropped_blocked_observations?:number;rejected_observations?:number;persistence_failures?:number;unconfirmed_processes?:number;baseline_at?:{seconds?:number;nanos?:number};checkpoint_at?:{seconds?:number;nanos?:number};counters_saturated?:boolean;coverage_incomplete?:boolean;end_confirmed?:boolean;}
export interface TrafficSourceStatus {status:"busy"|"unavailable"|"unobserved"|"partial"|"covered"|"not_running"|"sampling_failed";core_epoch?:string;revision?:number;configured_sources?:number;observed_sources?:number;core_running?:boolean;observed_at?:{seconds?:number;nanos?:number};last_success_at?:{seconds?:number;nanos?:number};}
import { request, type Page } from '../../app/apiClient';
export interface CollectorStatus {persisted_loss?:CollectorLossStatus;process_id:string;status:'ok'|'observation_loss'|'persistence_degraded';pending_observations?:number;aggregated_rollups?:number;dropped_allowed_observations?:number;dropped_blocked_observations?:number;rejected_observations?:number;persistence_failures?:number;counters_saturated?:boolean;started_at?:{seconds?:number;nanos?:number};observed_at?:{seconds?:number;nanos?:number};last_flush_at?:{seconds?:number;nanos?:number};last_flush_success_at?:{seconds?:number;nanos?:number};}
// Outbound network settings of one node. The first two pin the proxy core's own
// dials (A); the last two are enforced by the host-network helper for every new
// connection of the server (B). An empty string means "not configured".
export interface NodeNetworkSettings {
  outboundSourceIp: string; outboundInterface: string; hostSourceIpv4: string; hostSourceIpv6: string;
}
// The body of PUT /api/v1/nodes/{id}: the node basics plus the outbound network.
export interface NodeSettingsInput extends NodeNetworkSettings {
  id: string; name: string; enabled: boolean; publicIp: string; allowSubscription: boolean; maintenance: boolean;
}
export interface NodeView extends NodeNetworkSettings {
  id: string; name: string; status: 'pending' | 'online' | 'degraded' | 'offline' | 'applying' | 'rollback' | 'disabled' | 'error';
  enabled: boolean; allowSubscription: boolean; maintenance: boolean; hostname: string; os: string; agentVersion: string; publicIp: string; lastHeartbeat: string; createdAt: string;
  capabilities: { core_name?: string; core_version?: string; protocols?: { protocol: string; inbound?: boolean; outbound?: boolean; tcp?: boolean; udp?: boolean }[] };
  heartbeat: { traffic_source_status?:TrafficSourceStatus; collector_status?:CollectorStatus; spool_status?: { failure_code?: "queue_unavailable"|"capacity_unavailable"|"capacity_invalid"|"access_head_unavailable"|"access_head_invalid"|"traffic_head_unavailable"|"traffic_head_invalid"|"observation_timeout"|"observation_interrupted"; measured?: boolean; payload_bytes?: number; disk_bytes?: number; disk_limit?: number; database_bytes?: number; database_limit?: number; pending_batches?: number; dropped_allowed_rollups?: number; status: "ok" | "near_capacity" | "capacity_exceeded" | "unavailable"; observed_at?: { seconds?: number; nanos?: number } }; cpu_percent?: number; memory_percent?: number; disk_percent?: number; active_connections?: number; active_connections_measured?: boolean; active_connections_scope?: string; active_connections_observed_at?: { seconds?: number; nanos?: number }; upload_bytes?: number; download_bytes?: number; current_revision?: number; successful_revision?: number; core_healthy?: boolean; core_error?: 'core_unhealthy' | 'accounting_interrupted' };
}
// The panel publishes the release version, the gRPC port and the digest of the
// certificate trust anchor so the register dialog can build the one-command
// install. A field is empty when the panel cannot supply it: a local build has
// no release version, development mode has no certificate to trust.
export interface InstallInfo { version: string; grpcPort: number; caSha256: string; }
export interface Enrollment { node: NodeView; enrollmentToken: string; expiresAt: string; install: InstallInfo; }
// The public repository the release archives and node-install.sh come from.
export const releaseRepository = 'jingniao/qui';
// The single line the register dialog shows: it installs the agent and the core,
// establishes trust in the panel certificate and enrolls the node. The script
// validates the host, ports, digest and token again on the node.
export function installCommand(input: { version: string; host: string; httpPort: number; grpcPort: number; caSha256: string; token: string; core: string }): string {
  const port = input.httpPort === 443 ? '' : ` --http-port ${input.httpPort}`;
  return `curl -fsSL https://github.com/${releaseRepository}/releases/download/v${input.version}/node-install.sh | bash -s -- --version ${input.version} --panel ${input.host}${port} --grpc-port ${input.grpcPort} --ca-sha256 ${input.caSha256} --token ${input.token} --core ${input.core}`;
}

export interface HealthCheck { id:string;nodeId:string;revision:number;status:"queued"|"complete"|"expired";createdAt:string;deadline:string;result?:{healthy?:boolean;code:string;observed_at?:{seconds?:number;nanos?:number}}; }
export interface RestartTask {commandId:string;revision:number;status:"pending"|"success"|"failed";code:string;createdAt:string;}
export interface CertificateObservation {issuer?:string;fingerprint?:string;notBefore?:string;expiresAt?:string;status:string;integrity:'consistent'|'invalid';revokedAt?:string;}
export interface CertificateStatus {identitySnapshotDigest:string;signingCAFingerprint?:string;currentSigningAvailable:boolean;nodeId:string;enabled:boolean;observedAt:string;active:CertificateObservation|null;pending:(CertificateObservation&{createdAt:string;purpose:'renewal'|'key_rotation'|'unknown';sourceMatches:boolean})|null;}
export interface PublishedTopology {
  nodeId:string;revision:number;reportedRevision:number;observation:'unknown'|'disabled'|'fresh_control'|'stale_control'|'revision_mismatch';snapshot:'unpublished'|'available'|'unavailable';
  relays:{outboundId:string;targetNodeId:string;protocol:string;targetRevision:number;dependency:'unpublished'|'missing_node'|'disabled'|'unavailable'|'matched'|'mismatch';dependencyDetail?:string}[];
  chains:{id:string;node_ids:string[];outbound_ids:string[];failover:string;final_outbound_id?:string;backup_target_id?:string}[];
}
export interface EnrollmentStatus {nodeId:string;observedAt:string;code:string;summary:string;receiptState:string;originalFingerprint?:string;originalCreatedAt?:string;originalExpiresAt?:string;currentCertificate:CertificateObservation;currentIdentityMatches:boolean;currentCertificateValid:boolean;enabled:boolean;identityRevoked:boolean;lastSequence:number;pendingRenewal:boolean;tokenState:string;tokenExpiresAt?:string;centralReplayConditionsMatch:boolean;localInstallationConfirmed:boolean;stateRecoveryConfirmed:boolean;nextSteps:string[];}
export const nodeAPI = {
	enrollmentStatus:(id:string)=>request<EnrollmentStatus>(`/nodes/${encodeURIComponent(id)}/enrollment-status`),
	certificateStatus:(id:string)=>request<CertificateStatus>(`/nodes/${encodeURIComponent(id)}/certificate-status`),
restartTasks:(id:string)=>request<{items:RestartTask[]}>(`/nodes/${encodeURIComponent(id)}/restart-core`),
restartCore:(id:string,expectedRevision:number)=>request<RestartTask>(`/nodes/${encodeURIComponent(id)}/restart-core`,"POST",{expectedRevision,confirmNodeId:id}),
healthChecks:(id:string)=>request<{items:HealthCheck[]}>(`/nodes/${encodeURIComponent(id)}/health-checks`),
requestHealth:(id:string,expectedRevision:number)=>request<HealthCheck>(`/nodes/${encodeURIComponent(id)}/health-checks`,"POST",{expectedRevision}),
  list: (page: number, keyword: string) => request<Page<NodeView>>(`/nodes?page=${page}&pageSize=20&keyword=${encodeURIComponent(keyword)}`),
  get: (id: string) => request<NodeView>(`/nodes/${encodeURIComponent(id)}`),
  enroll: (name: string) => request<Enrollment>('/nodes/enrollment-token', 'POST', { name }),
  update: (node: NodeView, enabled: boolean) => request(`/nodes/${node.id}`, 'PUT', { name: node.name, enabled }),
  save: (node: NodeSettingsInput) => request(`/nodes/${node.id}`, 'PUT', { name: node.name, enabled: node.enabled, publicIp: node.publicIp, allowSubscription: node.allowSubscription, maintenance: node.maintenance, outboundSourceIp: node.outboundSourceIp, outboundInterface: node.outboundInterface, hostSourceIpv4: node.hostSourceIpv4, hostSourceIpv6: node.hostSourceIpv6 }),
  revoke: (id: string) => request(`/nodes/${id}/revoke-certificate`, 'POST')
};


