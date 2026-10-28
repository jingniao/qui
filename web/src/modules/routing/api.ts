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

export interface ServiceInput {
  name: string;
  enabled: boolean;
  description: string;
  domains: string[];
  suffixes: string[];
  cidrs: string[];
}

export interface ServiceSet extends ServiceInput {
  id: string;
  builtin: boolean;
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface SimulationRequest {
  nodeId: string;
  userId: string;
  inboundId: string;
  domain: string;
  targetIp: string;
  port: number;
  network: string;
  time?: string;
}

export interface Decision {
  selectionStrategy?: string;
  candidatePaths?: string[][];
  matched: boolean;
  policyId: string;
  policyName: string;
  priority: number;
  action: Action;
  reason: string;
  path?: string[];
  outboundProtocol?: string;
  finalEgressNodeId?: string;
  logging?: PolicyLogging;
}

export const policyAPI = {
  list: (page = 1, keyword = '', pageSize = 20) =>
    request<Page<PolicyRecord>>(`/policies?page=${page}&pageSize=${pageSize}&keyword=${encodeURIComponent(keyword)}`),
  get: (id: string) => request<PolicyRecord>(`/policies/${id}`),
  save: (id: string | null, value: PolicyInput) =>
    request<PolicyRecord>(id ? `/policies/${id}` : '/policies', id ? 'PUT' : 'POST', value),
  delete: (id: string) => request(`/policies/${id}`, 'DELETE'),
  services: (page = 1, size = 20) => request<Page<ServiceSet>>(`/service-sets?page=${page}&pageSize=${size}`),
  saveService: (id: string | null, value: ServiceInput) =>
    request<ServiceSet>(id ? `/service-sets/${id}` : '/service-sets', id ? 'PUT' : 'POST', value),
  deleteService: (id: string) => request(`/service-sets/${id}`, 'DELETE'),
  simulate: (value: SimulationRequest) => request<Decision>('/policies/simulate', 'POST', value)
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
  groups: { name: string; type: string; members: string[]; testUrl: string; intervalSeconds: number }[];
  providers: RuleTemplateProvider[];
  rules: { id: string; type: string; payload: string; target: string; noResolve: boolean; enabled: boolean; priority: number; description: string }[];
}

export interface StoredRuleTemplate {
  template: RuleTemplate;
  revision: number;
  updatedAt: string;
  updatedBy: string;
}

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
  { id: 'whitelist', name: '白名单模式', desc: '仅指定站点走代理，其余全部 DIRECT 国内直连。', recommended: false }
];

const DEFAULT_TEST_URL = 'http://www.gstatic.com/generate_204';

export function toConfig(stored: StoredRuleTemplate): ClientPolicyConfig {
  const template = stored.template;
  return {
    version: stored.revision,
    updatedAt: stored.updatedAt,
    updatedBy: stored.updatedBy,
    activePreset: template.preset,
    settings: { mode: template.client.mode, mixedPort: template.client.mixedPort, allowLan: template.client.allowLan, logLevel: template.client.logLevel },
    proxyGroups: template.groups.map(group => ({
      name: group.name,
      type: group.type,
      proxies: group.members.join(', '),
      url: group.testUrl || undefined,
      interval: group.intervalSeconds || undefined
    })),
    rules: template.rules.map(rule => ({ ...rule, type: rule.type as ClientRuleType })),
    extra: { ipv6: template.client.ipv6, dns: template.dns, providers: template.providers }
  };
}

export function fromConfig(config: ClientPolicyConfig): RuleTemplate {
  return {
    preset: config.activePreset || '',
    client: { ...config.settings, mixedPort: Number(config.settings.mixedPort), ipv6: config.extra.ipv6 },
    dns: config.extra.dns,
    groups: config.proxyGroups.map(group => {
      const testing = group.type !== 'select';
      return {
        name: group.name.trim(),
        type: group.type,
        members: String(group.proxies || '').split(',').map(item => item.trim()).filter(Boolean),
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

// The browser-only template of earlier builds. It never reached a client; the
// page offers it once as an unsaved draft and forgets it after a save.
const LEGACY_POLICY_KEY = 'qui_client_policy_config_v1';
const LEGACY_CLASH_KEY = 'qui_clash_rules_config';

export const clientPolicyAPI = {
  get: async (): Promise<ClientPolicyConfig> => toConfig(await request<StoredRuleTemplate>('/subscription-rules')),
  save: async (config: ClientPolicyConfig): Promise<ClientPolicyConfig> =>
    toConfig(await request<StoredRuleTemplate>('/subscription-rules', 'PUT', { template: fromConfig(config), expectedRevision: config.version })),
  presets: () => request<{ items: RulePreset[] }>('/subscription-rules/presets'),
  legacyDraft(): Pick<ClientPolicyConfig, 'settings' | 'proxyGroups' | 'rules'> | null {
    try {
      const raw = localStorage.getItem(LEGACY_POLICY_KEY);
      if (!raw) return null;
      const parsed = JSON.parse(raw);
      if (!parsed || !Array.isArray(parsed.rules) || !Array.isArray(parsed.proxyGroups) || !parsed.settings) return null;
      return {
        settings: parsed.settings,
        proxyGroups: parsed.proxyGroups.map((group: any) => ({ name: String(group?.name || ''), type: String(group?.type || 'select'), proxies: String(group?.proxies || ''), url: group?.url, interval: group?.interval })),
        rules: parsed.rules.map((rule: any, index: number) => ({
          id: String(rule?.id || `cr-${index + 1}`),
          // The old page offered PORT with comma lists; mihomo's rule is DST-PORT with '/'.
          type: (rule?.type === 'PORT' ? 'DST-PORT' : rule?.type) as ClientRuleType,
          payload: rule?.type === 'PORT' ? String(rule?.payload || '').replace(/\s*,\s*/g, '/') : String(rule?.payload || ''),
          target: String(rule?.target || 'DIRECT'),
          noResolve: !!rule?.noResolve,
          enabled: rule?.enabled !== false,
          priority: Number(rule?.priority) || (index + 1) * 5,
          description: rule?.description ? String(rule.description) : ''
        }))
      };
    } catch {
      return null;
    }
  },
  forgetLegacyDraft() {
    localStorage.removeItem(LEGACY_POLICY_KEY);
    localStorage.removeItem(LEGACY_CLASH_KEY);
  }
};

// 客户端动作语义说明
export const clientActionLabels: Record<string, { label: string; desc: string; tone: string }> = {
  DIRECT: {
    label: '用户设备直连 (DIRECT)',
    desc: '用户客户端直接发起连接，不经任何代理服务器，免流量且低延迟。',
    tone: 'text-emerald-700 bg-emerald-50 border-emerald-200'
  },
  REJECT: {
    label: '客户端阻断 (REJECT)',
    desc: '客户端本地直接丢弃连接请求，阻断广告追踪或恶意请求。',
    tone: 'text-red-700 bg-red-50 border-red-200'
  },
  PROXY: {
    label: '策略组 · PROXY',
    desc: '交由用户在客户端自行选择的服务器节点出站。',
    tone: 'text-blue-700 bg-blue-50 border-blue-200'
  },
  AUTO: {
    label: '策略组 · 自动测速 (AUTO)',
    desc: '客户端自动测速 URL-Test，优选延迟最低的节点转发。',
    tone: 'text-purple-700 bg-purple-50 border-purple-200'
  },
  FALLBACK: {
    label: '策略组 · 故障转移 (FALLBACK)',
    desc: '主节点不可用时自动按顺序切换至备用可用节点。',
    tone: 'text-amber-700 bg-amber-50 border-amber-200'
  }
};

// ==========================================
// 客户端策略模拟运算
// ==========================================
//
// The simulation follows the client's own first-match order. What only the
// client can know at run time is reported as such instead of being guessed: a
// GEOIP or GEOSITE lookup (the client's databases), an IP rule for a domain
// target (the client resolves the name itself) and an external rule provider
// (fetched by the client).

export interface ClientSimulationResult {
  matched: boolean;
  runtime: boolean;
  ruleIndex: number;
  matchedRule: ClientRule | null;
  target: string;
  actionLabel: string;
  actionDesc: string;
  reason: string;
  details: string[];
}

function parseIPv4(value: string): bigint | null {
  const parts = value.split('.');
  if (parts.length !== 4) return null;
  let result = 0n;
  for (const part of parts) {
    if (!/^\d{1,3}$/.test(part) || Number(part) > 255) return null;
    result = (result << 8n) + BigInt(Number(part));
  }
  return result;
}

function parseIPv6(value: string): bigint | null {
  let text = value.toLowerCase();
  if (text.startsWith('[') && text.endsWith(']')) text = text.slice(1, -1);
  if (!text.includes(':')) return null;
  let tail: bigint | null = null;
  const lastColon = text.lastIndexOf(':');
  if (text.slice(lastColon + 1).includes('.')) {
    tail = parseIPv4(text.slice(lastColon + 1));
    if (tail === null) return null;
    text = text.slice(0, lastColon) + ':0:0';
  }
  const halves = text.split('::');
  if (halves.length > 2) return null;
  const head = halves[0] ? halves[0].split(':') : [];
  const rest = halves.length === 2 && halves[1] ? halves[1].split(':') : [];
  const missing = 8 - head.length - rest.length;
  if ((halves.length === 1 && missing !== 0) || missing < 0) return null;
  const groups = [...head, ...Array(halves.length === 2 ? missing : 0).fill('0'), ...rest];
  if (groups.length !== 8) return null;
  let result = 0n;
  for (const group of groups) {
    if (!/^[0-9a-f]{1,4}$/.test(group)) return null;
    result = (result << 16n) + BigInt(parseInt(group, 16));
  }
  if (tail !== null) result = (result & ~0xffffffffn) | tail;
  return result;
}

interface ParsedAddress { family: 4 | 6; value: bigint }

function parseAddress(value: string): ParsedAddress | null {
  const v4 = parseIPv4(value);
  if (v4 !== null) return { family: 4, value: v4 };
  const v6 = parseIPv6(value);
  return v6 === null ? null : { family: 6, value: v6 };
}

// cidrContains is exact prefix arithmetic for both families. A bare address is
// a host prefix.
export function cidrContains(cidr: string, address: ParsedAddress): boolean {
  const [base, length] = cidr.trim().split('/');
  const network = parseAddress(base);
  if (!network || network.family !== address.family) return false;
  const bits = network.family === 4 ? 32 : 128;
  const prefix = length === undefined ? bits : Number(length);
  if (!Number.isInteger(prefix) || prefix < 0 || prefix > bits) return false;
  const shift = BigInt(bits - prefix);
  return (network.value >> shift) === (address.value >> shift);
}

const privateRanges = ['10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16', '127.0.0.0/8', '169.254.0.0/16', '100.64.0.0/10', 'fc00::/7', 'fe80::/10', '::1/128'];

function portMatches(payload: string, port: number): boolean {
  return payload.split('/').some(part => {
    const [low, high] = part.split('-').map(Number);
    return high === undefined ? port === low : port >= low && port <= high;
  });
}

function domainMatches(domain: string, suffix: string): boolean {
  const value = suffix.toLowerCase().replace(/^\./, '');
  return domain === value || domain.endsWith('.' + value);
}

export function simulateClientPolicy(
  targetAddress: string,
  targetPort: number,
  config: ClientPolicyConfig,
  serviceSets: ServiceSet[] = []
): ClientSimulationResult {
  const addr = (targetAddress || '').trim().toLowerCase();
  const port = targetPort || 443;
  const address = parseAddress(addr);
  const sortedRules = [...(config?.rules || [])].filter(rule => rule.enabled).sort((a, b) => a.priority - b.priority);
  const describe = (target: string) => clientActionLabels[target] || {
    label: target.startsWith('node:') ? '指定节点' : `策略组 / 目标: ${target}`,
    desc: target.startsWith('node:') ? '经由该用户订阅中的指定节点转发。' : `交由客户端中的 [${target}] 处理。`,
    tone: 'text-neutral-800 bg-neutral-100 border-neutral-300'
  };
  const runtime = (rule: ClientRule, index: number, why: string): ClientSimulationResult => ({
    matched: false,
    runtime: true,
    ruleIndex: index + 1,
    matchedRule: rule,
    target: '由客户端运行时决定',
    actionLabel: '客户端运行时决定',
    actionDesc: why,
    reason: `第 ${index + 1} 条已启用规则 (${rule.type} ${rule.payload || ''}) 是否命中，只有客户端在运行时才能确定；命中则走 ${rule.target}，否则继续向下匹配。`,
    details: [`执行顺序: 优先级 #${rule.priority}（第 ${index + 1} 条已启用规则）`, `规则类型: ${rule.type}`, `命中时的动作: ${rule.target}`]
  });

  for (let i = 0; i < sortedRules.length; i++) {
    const r = sortedRules[i];
    const payload = (r.payload || '').trim();
    let hit = false;
    let hitReason = '';
    switch (r.type) {
      case 'MATCH':
        hit = true;
        hitReason = '未命中前方更高优先级规则，命中最后的 MATCH 兜底规则。';
        break;
      case 'DST-PORT':
        if (portMatches(payload, port)) {
          hit = true;
          hitReason = `目标端口 ${port} 命中 DST-PORT [${payload}]。`;
        }
        break;
      case 'DOMAIN':
        if (!address && addr === payload.toLowerCase()) {
          hit = true;
          hitReason = `目标域名 ${addr} 精确匹配 DOMAIN [${payload}]。`;
        }
        break;
      case 'DOMAIN-SUFFIX':
        if (!address && domainMatches(addr, payload)) {
          hit = true;
          hitReason = `目标域名 ${addr} 命中 DOMAIN-SUFFIX [${payload}]。`;
        }
        break;
      case 'DOMAIN-KEYWORD':
        if (!address && addr.includes(payload.toLowerCase())) {
          hit = true;
          hitReason = `目标域名 ${addr} 包含关键字 DOMAIN-KEYWORD [${payload}]。`;
        }
        break;
      case 'IP-CIDR':
      case 'IP-CIDR6':
        if (address) {
          if (cidrContains(payload, address)) {
            hit = true;
            hitReason = `目标 IP ${addr} 属于 ${r.type} [${payload}] 网段。`;
          }
        } else if (!r.noResolve) {
          return runtime(r, i, '目标是域名，该 IP 规则会让客户端在运行时解析域名后再判断。');
        }
        break;
      case 'GEOIP':
        if (address && ['LAN', 'PRIVATE'].includes(payload.toUpperCase())) {
          if (privateRanges.some(range => cidrContains(range, address))) {
            hit = true;
            hitReason = `目标 IP ${addr} 属于私有/局域网地址。`;
          }
        } else if (address || !r.noResolve) {
          return runtime(r, i, 'GEOIP 由客户端自带的地理数据库判断，结果在运行时才确定。');
        }
        break;
      case 'GEOSITE':
        if (!address) {
          return runtime(r, i, 'GEOSITE 由客户端自带的域名数据库判断，结果在运行时才确定。');
        }
        break;
      case 'RULE-SET': {
        const id = payload.startsWith('service:') ? payload.slice('service:'.length) : '';
        const set = id ? serviceSets.find(item => item.id === id) : undefined;
        if (!set) {
          return runtime(r, i, id ? '引用的服务规则不存在或未加载。' : '外部规则集由客户端自行下载，内容在运行时才确定。');
        }
        if (address) {
          if ((set.cidrs || []).some(cidr => cidrContains(cidr, address))) {
            hit = true;
            hitReason = `目标 IP ${addr} 命中服务规则【${set.name}】中的 IP 段。`;
          }
        } else if ((set.domains || []).some(domain => domain.toLowerCase() === addr) || (set.suffixes || []).some(suffix => domainMatches(addr, suffix))) {
          hit = true;
          hitReason = `目标域名 ${addr} 命中服务规则【${set.name}】。`;
        } else if ((set.cidrs || []).length) {
          return runtime(r, i, '该服务规则含 IP 段，客户端会在运行时解析域名后再判断。');
        }
        break;
      }
    }
    if (hit) {
      const meta = describe(r.target);
      return {
        matched: true,
        runtime: false,
        ruleIndex: i + 1,
        matchedRule: r,
        target: r.target,
        actionLabel: meta.label,
        actionDesc: meta.desc,
        reason: hitReason,
        details: [
          `执行顺序: 优先级 #${r.priority}（第 ${i + 1} 条已启用规则）`,
          `规则类型: ${r.type}`,
          `匹配参数: ${payload || '任意'}`,
          `目标动作: ${r.target}`,
          '客户端生效机制: 用户设备本地 Clash/Mihomo 路由内核直接裁决。'
        ]
      };
    }
  }
  return {
    matched: false,
    runtime: false,
    ruleIndex: 0,
    matchedRule: null,
    target: 'DIRECT',
    actionLabel: '无匹配规则',
    actionDesc: '模板中没有命中的规则。保存的模板总以 MATCH 结尾，这种情况只会出现在未保存的编辑里。',
    reason: '遍历全部已启用规则均未命中。',
    details: ['未命中规则列表中的任何条件。']
  };
}
