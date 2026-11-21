import { request, type Page } from '../../app/apiClient';

export interface Scope {
  user_ids?: string[];
  node_ids?: string[];
  group_ids?: string[];
}

export interface Match {
  domains?: string[];
  suffixes?: string[];
  cidrs?: string[];
  ports?: string[];
  network?: string;
  time_range?: string;
  service_ids?: string[];
}

export interface Action {
  type: string;
  target_id?: string;
  failover?: string;
  backup_id?: string;
}

export interface PolicyLogging {
  log_domain: boolean;
  log_traffic: boolean;
  alert_on_block: boolean;
}

export interface PolicyInput {
  name: string;
  priority: number;
  enabled: boolean;
  scope: Scope;
  match: Match;
  action: Action;
  logging: PolicyLogging;
  timezone: string;
  remark: string;
}

export interface PolicyRecord extends PolicyInput {
  id: string;
  createdAt: string;
  updatedAt: string;
}

export interface ExternalSource {
  id: string;
  url: string;
  format: 'yaml' | 'text' | 'mrs' | 'native';
  behavior: 'classical' | 'domain' | 'ipcidr';
  intervalSeconds: number;
  nodeUsable: boolean;
  clientUsable: boolean;
  lastStatus: string;
  lastErrorCode: string;
  lastFetchAt?: string | null;
  lastSuccessAt?: string | null;
  entryCount: number;
  skippedCount: number;
  xrayUrl?: string;
  xrayTag?: string;
}

export interface SourceInput {
  id?: string;
  url: string;
  format: 'yaml' | 'text' | 'mrs' | 'native';
  behavior: 'classical' | 'domain' | 'ipcidr';
  intervalSeconds: number;
  nodeUsable?: boolean;
  clientUsable?: boolean;
  lastStatus?: string;
  lastErrorCode?: string;
  lastFetchAt?: string | null;
  lastSuccessAt?: string | null;
  entryCount?: number;
  skippedCount?: number;
  xrayUrl?: string;
  xrayTag?: string;
}

export interface ServiceInput {
  name: string;
  enabled: boolean;
  description: string;
  domains: string[];
  suffixes: string[];
  keywords?: string[];
  cidrs: string[];
  sources?: SourceInput[];
}

export interface ServiceReferences {
  nodePolicies: { id: string; name: string; enabled: boolean }[];
  clientProfiles: { id: string; name: string; active: boolean; rules: { id: string; enabled: boolean }[] }[];
}

export interface ServiceSet extends ServiceInput {
  references?: ServiceReferences;
  warning?: string;
  // An enabled node policy names this set, so the panel refreshes its sources daily (v1.5.2).
  autoRefresh?: boolean;
  id: string;
  builtin: boolean;
  version: number;
  sources: ExternalSource[];
  createdAt: string;
  updatedAt: string;
}

export const policyAPI = {
  list: (page = 1, keyword = '', pageSize = 20) =>
    request<Page<PolicyRecord>>(`/policies?page=${page}&pageSize=${pageSize}&keyword=${encodeURIComponent(keyword)}`),
  get: (id: string) => request<PolicyRecord>(`/policies/${id}`),
  save: (id: string | null, value: PolicyInput) =>
    request<PolicyRecord>(id ? `/policies/${id}` : '/policies', id ? 'PUT' : 'POST', value),
  delete: (id: string) => request(`/policies/${id}`, 'DELETE'),
  services: (page = 1, size = 20) => request<Page<ServiceSet>>(`/service-sets?page=${page}&pageSize=${size}`),
  serviceReferences: (id: string) => request<ServiceReferences>(`/service-sets/${id}/references`),
  saveService: (id: string | null, value: ServiceInput) =>
    request<ServiceSet & { references: ServiceReferences }>(id ? `/service-sets/${id}` : '/service-sets', id ? 'PUT' : 'POST', value),
  deleteService: (id: string) => request(`/service-sets/${id}`, 'DELETE'),
  refreshServiceSource: (id: string, sourceId: string) =>
    request<ExternalSource>(`/service-sets/${id}/sources/${sourceId}/refresh`, 'POST')
};

export function input(record: PolicyRecord): PolicyInput {
  return {
    name: record.name,
    priority: record.priority,
    enabled: record.enabled,
    scope: {
      user_ids: [...(record.scope.user_ids || [])],
      node_ids: [...(record.scope.node_ids || [])],
      group_ids: [...(record.scope.group_ids || [])]
    },
    match: {
      domains: [...(record.match.domains || [])],
      suffixes: [...(record.match.suffixes || [])],
      cidrs: [...(record.match.cidrs || [])],
      ports: [...(record.match.ports || [])],
      network: record.match.network || 'all',
      time_range: record.match.time_range || '',
      service_ids: [...(record.match.service_ids || [])]
    },
    action: { ...record.action },
    logging: record.logging ? { ...record.logging } : { log_domain: true, log_traffic: true, alert_on_block: false },
    timezone: record.timezone,
    remark: record.remark
  };
}

export function defaults(): PolicyInput {
  return {
    name: '',
    priority: 10,
    enabled: true,
    scope: { user_ids: [], node_ids: [], group_ids: [] },
    match: { domains: [], suffixes: [], cidrs: [], ports: [], network: 'all', time_range: '', service_ids: [] },
    action: { type: 'Block', target_id: '', failover: 'Block' },
    logging: { log_domain: true, log_traffic: true, alert_on_block: false },
    timezone: 'Asia/Shanghai',
    remark: ''
  };
}

// 节点端动作文案（明确区分“服务器直连”与客户端直连）
export const actionNames: Record<string, string> = {
  Direct: '服务器直连 (Direct)',
  Block: '服务器阻断 (Block)',
  Outbound: '指定出站',
  OutboundGroup: '指定出口组',
  ProxyChain: '指定多跳代理链'
};

// ==========================================
// 客户端策略 (Clash / Mihomo 体系)
// ==========================================
//
// The template lives on the server (`/api/v1/subscription-rules`) and is rendered
// into each user's Clash profile there; the browser keeps no copy and builds no
// YAML. The page keeps its own view model (comma-separated group members, a
// numeric priority per rule); `toConfig`/`fromConfig` translate it from and to
// the server's typed template, and the fields the page does not edit (DNS,
// external rule providers, IPv6) are carried through unchanged.

export type ClientRuleType = 'DOMAIN' | 'DOMAIN-SUFFIX' | 'DOMAIN-KEYWORD' | 'IP-CIDR' | 'IP-CIDR6' | 'DST-PORT' | 'RULE-SET' | 'GEOIP' | 'GEOSITE' | 'MATCH';
export type ClientActionTarget = 'DIRECT' | 'REJECT' | 'PROXY' | 'AUTO' | 'FALLBACK' | string;

export interface ClientSettings {
  mode: string;
  mixedPort: number;
  allowLan: boolean;
  logLevel: string;
}

export interface ClientProxyGroup {
  excludeEndpoints?: string[];
  includeKeywords?: string[];
  excludeKeywords?: string[];
  emptyPolicy?: "direct" | "reject";
  name: string;
  type: string;
  proxies: string;
  url?: string;
  interval?: number;
}

export interface ClientRule {
  id: string;
  type: ClientRuleType;
  payload: string;
  target: ClientActionTarget;
  noResolve?: boolean;
  enabled: boolean;
  priority: number;
  description?: string;
}

export interface RuleTemplateDNS {
  defaultNameservers?: string[];
  proxyServerNameservers?: string[];
  fakeIpFilter?: string[];
  fallbackFilter?: { geoip: boolean; geoipCode?: string; ipcidr?: string[]; domain?: string[] };
  enable: boolean;
  enhancedMode: string;
  nameservers: string[];
  fallback: string[];
}

export interface RuleTemplateProvider {
  name: string;
  behavior: string;
  format: string;
  url: string;
  intervalSeconds: number;
}

export interface ClientPolicyConfig {
  // version is the server revision the page last read; a save must send it back.
  version: number;
  updatedAt: string;
  updatedBy?: string;
  activePreset?: string;
  settings: ClientSettings;
  proxyGroups: ClientProxyGroup[];
  rules: ClientRule[];
  // Carried through unchanged: the page does not edit these.
  extra: { ipv6: boolean; dns: RuleTemplateDNS; providers: RuleTemplateProvider[] };
}

export interface RuleTemplate {
  preset: string;
  client: ClientSettings & { ipv6: boolean };
  dns: RuleTemplateDNS;
  groups: { excludeEndpoints?: string[]; includeKeywords?: string[]; excludeKeywords?: string[]; emptyPolicy?: "direct" | "reject"; name: string; type: string; members: string[]; testUrl: string; intervalSeconds: number }[];
  providers: RuleTemplateProvider[];
  rules: { id: string; type: string; payload: string; target: string; noResolve: boolean; enabled: boolean; priority: number; description: string }[];
}

export interface StoredRuleTemplate {
  template: RuleTemplate;
  revision: number;
  updatedAt: string;
  updatedBy: string;
}

export interface ClientPolicyCard { mode: string; mixedPort: number; groupCount: number; enabledRuleCount: number; }

export interface ClientPolicySummary {
  card: ClientPolicyCard;
  id: string;
  name: string;
  description: string;
  revision: number;
  createdAt: string;
  createdBy: string;
  updatedAt: string;
  updatedBy: string;
}

export interface ClientPolicyProfile extends ClientPolicySummary {
  template: RuleTemplate;
  uiMetadata: Record<string, unknown>;
}

export interface ClientPolicyResult {
  profile: ClientPolicyProfile;
  activeProfileId: string;
  activeRevision: number;
}

export interface ClientPolicyInput {
  name: string;
  description: string;
  template: RuleTemplate;
  uiMetadata: Record<string, unknown>;
}

export interface ClientPolicyCandidate {
  ref: string;
  name: string;
  protocol: string;
  originKind: 'inbound' | 'relay' | 'import';
  originName: string;
  nodeGroupIds: string[];
}

export interface ClientPolicyCandidates {
  items: ClientPolicyCandidate[];
  user?: { id: string; name: string };
  autoSelected: boolean;
  diagnostic?: string;
}

export interface ClientPolicyPreview {
  body: string;
  contentType: string;
  format: string;
  previewSource: 'draft' | 'saved';
  profileId?: string;
  profileRevision?: number;
  templateDigest: string;
  activeRevision: number;
  context: { user?: { id: string; name: string }; autoSelected: boolean };
  groups: { name: string; members: { kind: string; name: string; ref?: string }[] }[];
  warnings: { stage: string; ref: string; message: string }[];
  diagnostic?: string;
}

export type ClientPolicyPreviewInput = ({ template: RuleTemplate } | { profileId: string; profileRevision: number }) & { userId?: string; format?: string };

export const clientPolicyProfilesAPI = {
  preview: (input: ClientPolicyPreviewInput) => request<ClientPolicyPreview>('/client-policy-profiles/preview', 'POST', input),
  candidates: (userId?: string) => request<ClientPolicyCandidates>('/client-policy-candidates' + (userId ? '?userId=' + encodeURIComponent(userId) : '')),
  list: (page = 1, pageSize = 100) => request<{ items: ClientPolicySummary[]; total: number; page: number; pageSize: number; activeProfileId: string; activeRevision: number }>(`/client-policy-profiles?page=${page}&pageSize=${pageSize}`),
  get: (id: string) => request<ClientPolicyResult>('/client-policy-profiles/' + encodeURIComponent(id)),
  create: (input: ClientPolicyInput & { importKey?: string; activate?: boolean; expectedActiveRevision?: number }) => request<ClientPolicyResult>('/client-policy-profiles', 'POST', input),
  update: (id: string, input: ClientPolicyInput & { expectedRevision: number; expectedActiveRevision?: number }) => request<ClientPolicyResult>('/client-policy-profiles/' + encodeURIComponent(id), 'PUT', input),
  activate: (id: string, expectedRevision: number, expectedActiveRevision: number) => request<ClientPolicyResult>('/client-policy-profiles/' + encodeURIComponent(id) + '/activate', 'POST', { expectedRevision, expectedActiveRevision }),
  delete: (id: string, expectedRevision: number) => request<void>('/client-policy-profiles/' + encodeURIComponent(id) + '?expectedRevision=' + expectedRevision, 'DELETE')
};

export interface ClientPreset {
  id: string;
  name: string;
  desc: string;
  recommended?: boolean;
}

export interface RulePreset {
  id: string;
  name: string;
  description: string;
  recommended: boolean;
  template: RuleTemplate;
}

// The card texts of the built-in presets. The templates themselves come from the
// server (`/subscription-rules/presets`), so a preset applied here is exactly the
// one the server validates.
export const clientPresets: ClientPreset[] = [
  { id: 'standard', name: '经典分流', desc: '国内直连，常见海外服务走代理，局域网与私网直接通信。', recommended: true },
  { id: 'minimal', name: '基础代理', desc: '仅私有地址直连，其余流量均通过 PROXY 代理分流。', recommended: false },
  { id: 'adblock', name: '分流+去广告', desc: '在经典分流基础上增加常见广告域名与追踪域拦截 (REJECT)。', recommended: false },
  { id: 'whitelist', name: '白名单模式', desc: '仅指定站点走代理，其余全部 DIRECT 国内直连。', recommended: false },
  { id: 'meta-lite', name: '精简分流 · 屏蔽回国', desc: '节点选择、非中国与漏网之鱼三组；国内域名与国内 IP 一律拒绝，私网直连。', recommended: false },
  { id: 'meta-standard', name: '标准分流 · 屏蔽回国', desc: '在精简分流上增加 AI、YouTube、Google、Telegram、GitHub 分组；国内域名与国内 IP 一律拒绝。', recommended: false },
  { id: 'meta-full', name: '完整分流 · 屏蔽回国', desc: '按服务细分 20 个分组（广告、AI、流媒体、游戏、金融、云服务等）；国内域名与国内 IP 一律拒绝。', recommended: false }
];

const DEFAULT_TEST_URL = 'http://www.gstatic.com/generate_204';

// Quote members whose imported stable key contains the editor's separator.
export function formatGroupMembers(members: string[]): string {
  return members.map(member => /[,"\n]/.test(member) ? '"' + member.replace(/"/g, '""') + '"' : member).join(', ');
}

export function parseGroupMembers(value: string): string[] {
  const members: string[] = [];
  let member = '';
  let quoted = false;
  for (let index = 0; index < value.length; index++) {
    const char = value[index];
    if (char === '"') {
      if (quoted && value[index + 1] === '"') {
        member += '"';
        index++;
      } else {
        quoted = !quoted;
      }
    } else if (char === ',' && !quoted) {
      if (member.trim()) members.push(member.trim());
      member = '';
    } else {
      member += char;
    }
  }
  if (member.trim()) members.push(member.trim());
  return members;
}

function normalizeDNS(dns: RuleTemplateDNS): RuleTemplateDNS {
  const result = { ...dns };
  for (const key of ['defaultNameservers', 'proxyServerNameservers', 'fakeIpFilter'] as const) {
    if (!result[key]?.length) delete result[key];
  }
  if (result.fallbackFilter) {
    result.fallbackFilter = { ...result.fallbackFilter };
    for (const key of ['ipcidr', 'domain'] as const) {
      if (!result.fallbackFilter[key]?.length) delete result.fallbackFilter[key];
    }
    if (!result.fallbackFilter.geoipCode) delete result.fallbackFilter.geoipCode;
  }
  return result;
}

export function toConfig(stored: StoredRuleTemplate): ClientPolicyConfig {
  const template = stored.template;
  return {
    version: stored.revision,
    updatedAt: stored.updatedAt,
    updatedBy: stored.updatedBy,
    activePreset: template.preset,
    settings: { mode: template.client.mode, mixedPort: template.client.mixedPort, allowLan: template.client.allowLan, logLevel: template.client.logLevel },
    proxyGroups: template.groups.map(group => ({
      ...(group.excludeEndpoints?.length ? { excludeEndpoints: group.excludeEndpoints } : {}),
      includeKeywords: group.includeKeywords,
      excludeKeywords: group.excludeKeywords,
      emptyPolicy: group.emptyPolicy,
      name: group.name,
      type: group.type,
      proxies: formatGroupMembers(group.members),
      url: group.testUrl || undefined,
      interval: group.intervalSeconds || undefined
    })),
    rules: template.rules.map(rule => ({ ...rule, type: rule.type as ClientRuleType })),
    extra: { ipv6: template.client.ipv6, dns: normalizeDNS(template.dns), providers: template.providers }
  };
}

export function fromConfig(config: ClientPolicyConfig): RuleTemplate {
  return {
    preset: config.activePreset || '',
    client: { ...config.settings, mixedPort: Number(config.settings.mixedPort), ipv6: config.extra.ipv6 },
    dns: normalizeDNS(config.extra.dns),
    groups: config.proxyGroups.map(group => {
      const testing = group.type !== 'select';
      return {
        ...(group.includeKeywords?.length ? { includeKeywords: group.includeKeywords } : {}),
        ...(group.excludeKeywords?.length ? { excludeKeywords: group.excludeKeywords } : {}),
        ...(group.excludeEndpoints?.length ? { excludeEndpoints: group.excludeEndpoints } : {}),
        ...(group.emptyPolicy ? { emptyPolicy: group.emptyPolicy } : {}),
        name: group.name.trim(),
        type: group.type,
        members: parseGroupMembers(group.proxies),
        testUrl: testing ? group.url || DEFAULT_TEST_URL : '',
        intervalSeconds: testing ? Number(group.interval) || 300 : 0
      };
    }),
    providers: config.extra.providers,
    rules: config.rules.map(rule => ({
      id: rule.id,
      type: rule.type,
      payload: rule.type === 'MATCH' ? '' : rule.payload.trim(),
      target: rule.target,
      noResolve: !!rule.noResolve,
      enabled: rule.enabled,
      priority: Number(rule.priority),
      description: rule.description || ''
    }))
  };
}

export const clientPolicyAPI = {
  get: async (): Promise<ClientPolicyConfig> => toConfig(await request<StoredRuleTemplate>('/subscription-rules')),
  presets: () => request<{ items: RulePreset[]; recommendedDns?: RuleTemplateDNS }>('/subscription-rules/presets')
};

