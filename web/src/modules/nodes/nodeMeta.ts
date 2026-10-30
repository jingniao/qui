import { reactive } from 'vue';
import { request } from '../../app/apiClient';

export interface NodeMeta {
  remark?: string;
  region?: string;
  renewDate?: string; // e.g. '2026-12-31'
  renewCurrency?: string; // 'CNY' | 'USD' | 'EUR' | 'HKD' | 'GBP' | 'JPY' | 'TWD' | 'SGD'
  renewPrice?: string | number; // e.g. '120' or 120
  renewCycle?: string; // e.g. '/年' | '/月' | '/季' | '/半年' | '/2年' | '/3年' | '一次性'
  renewNoticeDays?: number;
  portRangeStart?: number | null;
  portRangeEnd?: number | null;
}

export const CURRENCY_LIST = [
  { code: 'CNY', symbol: '¥', label: '¥ CNY' },
  { code: 'USD', symbol: '$', label: '$ USD' },
  { code: 'EUR', symbol: '€', label: '€ EUR' },
  { code: 'HKD', symbol: 'HK$', label: 'HK$ HKD' },
  { code: 'GBP', symbol: '£', label: '£ GBP' },
  { code: 'JPY', symbol: '¥', label: '¥ JPY' },
  { code: 'TWD', symbol: 'NT$', label: 'NT$ TWD' },
  { code: 'SGD', symbol: 'S$', label: 'S$ SGD' }
] as const;

export const CURRENCY_SYMBOLS: Record<string, string> = {
  CNY: '¥',
  USD: '$',
  EUR: '€',
  HKD: 'HK$',
  GBP: '£',
  JPY: '¥',
  TWD: 'NT$',
  SGD: 'S$'
};

export const CYCLE_LIST = ['/年', '/月', '/季', '/半年', '/2年', '/3年', '一次性'] as const;

const metadata = reactive<Record<string, NodeMeta>>({});
export function getNodeMeta(nodeId: string): NodeMeta { return metadata[nodeId] || {}; }

export function cleanRemark(remark?: string): string { return (remark || '').trim(); }

export function formatPortRange(meta?: NodeMeta | null): string {
  return meta?.portRangeStart && meta?.portRangeEnd ? meta.portRangeStart+' - '+meta.portRangeEnd : '—';
}

export function getNodePortRange(nodeId?: string): { start: number; end: number } | null {
  const meta = nodeId ? metadata[nodeId] : undefined;
  return meta?.portRangeStart && meta?.portRangeEnd ? { start: meta.portRangeStart, end: meta.portRangeEnd } : null;
}

export function getRandomPortForNode(
  nodeId?: string,
  isPortBusy?: (port: number) => boolean,
  random = Math.random
): number {
  const range = nodeId ? getNodePortRange(nodeId) : null;
  const min = (range && range.start >= 1 && range.start <= 65535) ? range.start : 10000;
  const max = (range && range.end >= min && range.end <= 65535) ? range.end : 65535;
  const span = Math.max(1, max - min + 1);

  const startOffset = Math.floor(random() * span);
  for (let i = 0; i < span; i++) {
    const port = min + ((startOffset + i) % span);
    if (!isPortBusy || !isPortBusy(port)) {
      return port;
    }
  }
  return min + Math.floor(random() * span);
}

export async function loadNodeMeta(nodeId: string, _owner: boolean): Promise<NodeMeta> {
  let result = await request<{ meta: NodeMeta; present: boolean }>('/nodes/'+encodeURIComponent(nodeId)+'/meta');
  // Preserve the existing one-time financial metadata import; port ranges are API-owned.
  if (_owner) {
    const key = 'qui_node_meta_' + nodeId, raw = localStorage.getItem(key);
    if (raw && !result.present) {
      const legacy = JSON.parse(raw) as NodeMeta;
      const price = parseRenewPrice(legacy.renewPrice, legacy.renewCurrency, legacy.renewCycle);
      result = await request('/nodes/'+encodeURIComponent(nodeId)+'/meta', 'PUT', {
        region: legacy.region ?? '', remark: legacy.remark ?? '', renewDate: legacy.renewDate ?? '',
        renewCycle: price.cycle, renewCurrency: price.currency, renewPrice: price.price,
        renewNoticeDays: legacy.renewNoticeDays ?? 7, importOnly: true
      });
    }
    if (raw) localStorage.removeItem(key);
  }
  metadata[nodeId] = result.meta;
  return result.meta;
}

export async function saveNodeMeta(nodeId: string, meta: NodeMeta): Promise<void> {
  const start = meta.portRangeStart ?? null, end = meta.portRangeEnd ?? null;
  if ((start === null) !== (end === null) || (start !== null && end !== null &&
      (!Number.isInteger(start) || !Number.isInteger(end) || start < 1 || end > 65535 || start > end))) {
    throw new Error('请同时填写有效的起始和结束端口（1–65535），或全部留空');
  }
  const result = await request<{ meta: NodeMeta }>('/nodes/'+encodeURIComponent(nodeId)+'/meta', 'PUT', {
    region: meta.region ?? '', remark: (meta.remark || '').trim(),
    renewDate: meta.renewDate ?? '', renewCycle: meta.renewCycle ?? '',
    renewCurrency: meta.renewCurrency ?? '', renewPrice: String(meta.renewPrice ?? ''),
    renewNoticeDays: meta.renewNoticeDays ?? 7, portRangeStart: start, portRangeEnd: end
  });
  metadata[nodeId] = result.meta;
}

export function parseRenewPrice(
  rawPrice?: string | number,
  rawCurrency?: string,
  rawCycle?: string
): { currency: string; price: string; cycle: string } {
  let currency = rawCurrency || 'CNY';
  let cycle = rawCycle || '/年';
  let priceStr = typeof rawPrice === 'number' ? String(rawPrice) : (rawPrice || '').trim();

  if (!priceStr) {
    return { currency, price: '', cycle };
  }

  if (!rawCurrency) {
    if (priceStr.includes('$') && !priceStr.toUpperCase().includes('HK$') && !priceStr.toUpperCase().includes('S$') && !priceStr.toUpperCase().includes('NT$')) {
      currency = 'USD';
    } else if (priceStr.toUpperCase().includes('HK$')) {
      currency = 'HKD';
    } else if (priceStr.includes('€') || priceStr.toUpperCase().includes('EUR')) {
      currency = 'EUR';
    } else if (priceStr.includes('£') || priceStr.toUpperCase().includes('GBP')) {
      currency = 'GBP';
    } else if (priceStr.toUpperCase().includes('NT$')) {
      currency = 'TWD';
    } else if (priceStr.toUpperCase().includes('S$')) {
      currency = 'SGD';
    } else if (priceStr.includes('￥') || priceStr.includes('¥') || priceStr.toUpperCase().includes('CNY')) {
      currency = 'CNY';
    }
  }

  if (!rawCycle) {
    for (const c of CYCLE_LIST) {
      if (priceStr.includes(c)) {
        cycle = c;
        break;
      }
    }
  }

  const cleanNum = priceStr.replace(/[^0-9.]/g, '');
  return { currency, price: cleanNum, cycle };
}

export function formatRenewPrice(meta?: NodeMeta): string {
  if (!meta) return '';
  const parsed = parseRenewPrice(meta.renewPrice, meta.renewCurrency, meta.renewCycle);
  if (!parsed.price) {
    return meta.renewPrice ? String(meta.renewPrice) : '';
  }
  const symbol = CURRENCY_SYMBOLS[parsed.currency] || parsed.currency;
  return `${symbol}${parsed.price} ${parsed.cycle}`;
}

export function calculateRemainingDays(renewDate?: string): { days: number | null; label: string; isExpired: boolean; isNear: boolean } {
  if (!renewDate) {
    return { days: null, label: '—', isExpired: false, isNear: false };
  }
  let target: number;
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(renewDate);
  if (m) {
    target = new Date(+m[1], +m[2] - 1, +m[3]).getTime();
  } else {
    target = new Date(renewDate).getTime();
  }
  if (isNaN(target)) {
    return { days: null, label: '—', isExpired: false, isNear: false };
  }
  const now = new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const diffDays = Math.ceil((target - today) / (1000 * 60 * 60 * 24));

  if (diffDays < 0) {
    return { days: diffDays, label: `已逾期 ${Math.abs(diffDays)} 天`, isExpired: true, isNear: true };
  }
  if (diffDays === 0) {
    return { days: 0, label: '今日到期', isExpired: false, isNear: true };
  }
  return {
    days: diffDays,
    label: `剩 ${diffDays} 天`,
    isExpired: false,
    isNear: diffDays <= 7
  };
}

export function calculateMonthlyExpenditure(meta?: NodeMeta): { value: string; currency: string; symbol: string } | null {
  if (!meta) return null;
  const parsed = parseRenewPrice(meta.renewPrice, meta.renewCurrency, meta.renewCycle);
  const num = parseFloat(parsed.price);
  if (!Number.isFinite(num) || num <= 0) return null;
  let months = 1;
  const cycle = parsed.cycle;
  if (cycle.includes('3年')) months = 36;
  else if (cycle.includes('2年')) months = 24;
  else if (cycle.includes('年')) months = 12;
  else if (cycle.includes('半年')) months = 6;
  else if (cycle.includes('季')) months = 3;
  else if (cycle.includes('月')) months = 1;
  else if (cycle.includes('一次性')) return null;
  const monthly = num / months;
  const symbol = CURRENCY_SYMBOLS[parsed.currency] || parsed.currency;
  return { value: monthly.toFixed(2), currency: parsed.currency, symbol };
}

export function calculateResidualValue(meta?: NodeMeta): { value: string; currency: string; symbol: string } | null {
  if (!meta || !meta.renewDate) return null;
  const remaining = calculateRemainingDays(meta.renewDate);
  if (remaining.days === null || remaining.days <= 0) return null;
  const parsed = parseRenewPrice(meta.renewPrice, meta.renewCurrency, meta.renewCycle);
  const num = parseFloat(parsed.price);
  if (!Number.isFinite(num) || num <= 0) return null;
  let totalDays = 30;
  const cycle = parsed.cycle;
  if (cycle.includes('3年')) totalDays = 365 * 3;
  else if (cycle.includes('2年')) totalDays = 365 * 2;
  else if (cycle.includes('年')) totalDays = 365;
  else if (cycle.includes('半年')) totalDays = 182.5;
  else if (cycle.includes('季')) totalDays = 91.25;
  else if (cycle.includes('月')) totalDays = 30;
  else if (cycle.includes('一次性')) return null;
  const daily = num / totalDays;
  const residual = Math.max(0, remaining.days * daily);
  const symbol = CURRENCY_SYMBOLS[parsed.currency] || parsed.currency;
  return { value: residual.toFixed(2), currency: parsed.currency, symbol };
}

// ================= Country Flag & Geo Recognition =================

export function countryCodeToFlag(code: string): string {
  if (!code || code.length !== 2) return '🌐';
  const upper = code.toUpperCase();
  const first = upper.charCodeAt(0) - 65 + 0x1F1E6;
  const second = upper.charCodeAt(1) - 65 + 0x1F1E6;
  if (first < 0x1F1E6 || first > 0x1F1FF || second < 0x1F1E6 || second > 0x1F1FF) return '🌐';
  return String.fromCodePoint(first, second);
}

export interface CountryInfo {
  code: string;
  name: string;
  flag: string;
  source: 'admin_region' | 'ip_geo' | 'hostname_match' | 'fallback';
}

const REGION_RULES: Array<{ code: string; name: string; patterns: string[] }> = [
  { code: 'HK', name: '中国香港', patterns: ['香港', 'hong kong', 'hongkong', 'hkg', 'hkt'] },
  { code: 'MO', name: '中国澳门', patterns: ['澳门', 'macau', 'macao'] },
  { code: 'TW', name: '中国台湾', patterns: ['台湾', 'taiwan', '台北', 'taipei', '高雄', 'tpe'] },
  { code: 'JP', name: '日本', patterns: ['日本', 'japan', '东京', 'tokyo', '大阪', 'osaka', '名古屋', 'nagoya', 'nrt', 'hnd', 'kix'] },
  { code: 'KR', name: '韩国', patterns: ['韩国', 'korea', '首尔', 'seoul', '釜山', 'icn'] },
  { code: 'SG', name: '新加坡', patterns: ['新加坡', 'singapore', '狮城', 'sin'] },
  { code: 'US', name: '美国', patterns: ['美国', 'usa', 'united states', '美西', '美东', '洛杉矶', 'los angeles', 'lax', '圣何塞', 'san jose', 'sjc', '硅谷', 'silicon valley', '西雅图', 'seattle', 'sea', '纽约', 'new york', 'nyc', 'jfk', '芝加哥', 'chicago', 'ord', '达拉斯', 'dallas', 'dfw', '波特兰', 'pdx', '凤凰城', 'phoenix', 'phx', '弗吉尼亚', 'virginia', '迈阿密', 'miami', 'mia', '旧金山', 'san francisco', 'sfo', '华盛顿', 'iad'] },
  { code: 'GB', name: '英国', patterns: ['英国', 'united kingdom', '伦敦', 'london', 'lhr', '曼彻斯特', 'manchester'] },
  { code: 'DE', name: '德国', patterns: ['德国', 'germany', '法兰克福', 'frankfurt', 'fra', '柏林', 'berlin', '杜塞尔多夫'] },
  { code: 'FR', name: '法国', patterns: ['法国', 'france', '巴黎', 'paris', 'cdg', '马赛'] },
  { code: 'CA', name: '加拿大', patterns: ['加拿大', 'canada', '多伦多', 'toronto', 'yyz', '温哥华', 'vancouver', 'yvr', '蒙特利尔', 'montreal'] },
  { code: 'AU', name: '澳大利亚', patterns: ['澳大利亚', '澳洲', 'australia', '悉尼', 'sydney', 'syd', '墨尔本', 'melbourne', 'mel'] },
  { code: 'RU', name: '俄罗斯', patterns: ['俄罗斯', 'russia', '莫斯科', 'moscow', '圣彼得堡', '伯力', '海参崴', '符拉迪沃斯托克'] },
  { code: 'CN', name: '中国大陆', patterns: ['中国', 'china', '北京', '上海', '广州', '深圳', '杭州', '成都', '武汉', '南京', '重庆', '西安', '天津', '大陆', '内地'] },
  { code: 'NL', name: '荷兰', patterns: ['荷兰', 'netherlands', '阿姆斯特丹', 'amsterdam', 'ams'] },
  { code: 'MY', name: '马来西亚', patterns: ['马来西亚', '大马', 'malaysia', '吉隆坡', 'kuala lumpur', 'kul'] },
  { code: 'TH', name: '泰国', patterns: ['泰国', 'thailand', '曼谷', 'bangkok', 'bkk'] },
  { code: 'VN', name: '越南', patterns: ['越南', 'vietnam', '胡志明', '河内', 'sgn', 'han'] },
  { code: 'PH', name: '菲律宾', patterns: ['菲律宾', 'philippines', '马尼拉', 'manila', 'mnl'] },
  { code: 'ID', name: '印度尼西亚', patterns: ['印尼', '印度尼西亚', 'indonesia', '雅加达', 'jakarta', 'cgk'] },
  { code: 'IN', name: '印度', patterns: ['印度', 'india', '孟买', '新德里', 'del', 'bom'] },
  { code: 'TR', name: '土耳其', patterns: ['土耳其', 'turkey', '伊斯坦布尔', 'istanbul', 'ist'] },
  { code: 'AE', name: '阿联酋', patterns: ['阿联酋', 'uae', '迪拜', 'dubai', 'dxb', '阿布扎比'] },
  { code: 'BR', name: '巴西', patterns: ['巴西', 'brazil', '圣保罗', 'sao paulo'] },
  { code: 'AR', name: '阿根廷', patterns: ['阿根廷', 'argentina', '布宜诺斯艾利斯'] },
  { code: 'ZA', name: '南非', patterns: ['南非', 'south africa', '约翰内斯堡'] },
  { code: 'CH', name: '瑞士', patterns: ['瑞士', 'switzerland', '苏黎世', 'zurich', '日内瓦', 'geneva'] },
  { code: 'SE', name: '瑞典', patterns: ['瑞典', 'sweden', '斯德哥尔摩', 'stockholm'] },
  { code: 'NO', name: '挪威', patterns: ['挪威', 'norway', '奥斯陆', 'oslo'] },
  { code: 'FI', name: '芬兰', patterns: ['芬兰', 'finland', '赫尔辛基', 'helsinki'] },
  { code: 'IE', name: '爱尔兰', patterns: ['爱尔兰', 'ireland', '都柏林', 'dublin'] },
  { code: 'IT', name: '意大利', patterns: ['意大利', 'italy', '米兰', 'milan', '罗马', 'rome'] },
  { code: 'ES', name: '西班牙', patterns: ['西班牙', 'spain', '马德里', 'madrid', '巴塞罗那', 'barcelona'] },
  { code: 'PL', name: '波兰', patterns: ['波兰', 'poland', '华沙', 'warsaw'] },
  { code: 'NZ', name: '新西兰', patterns: ['新西兰', 'new zealand', '奥克兰', 'auckland'] }
];

function matchTextRule(text?: string): { code: string; name: string } | null {
  if (!text) return null;
  const lower = text.toLowerCase().trim();
  if (!lower) return null;

  if (lower.length === 2) {
    const directRule = REGION_RULES.find(r => r.code.toLowerCase() === lower);
    if (directRule) return { code: directRule.code, name: directRule.name };
  }

  for (const rule of REGION_RULES) {
    for (const pat of rule.patterns) {
      const p = pat.toLowerCase();
      if (/[\u4e00-\u9fa5]/.test(p)) {
        if (lower.includes(p)) {
          return { code: rule.code, name: rule.name };
        }
        continue;
      }
      if (p.length <= 3) {
        const re = new RegExp('(^|[^a-z])' + p.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '([^a-z]|$)');
        if (re.test(lower)) {
          return { code: rule.code, name: rule.name };
        }
        continue;
      }
      if (lower.includes(p)) {
        return { code: rule.code, name: rule.name };
      }
    }
  }
  return null;
}

const isPrivateIp = (ip: string) => {
  return (
    ip === '127.0.0.1' ||
    ip === 'localhost' ||
    ip.startsWith('10.') ||
    ip.startsWith('192.168.') ||
    /^172\.(1[6-9]|2\d|3[0-1])\./.test(ip) ||
    ip.startsWith('169.254.') ||
    ip === '::1'
  );
};

// Listeners for async IP geo updates
const geoListeners: Array<(ip: string, code: string) => void> = [];
export function onGeoResolved(listener: (ip: string, code: string) => void): () => void {
  geoListeners.push(listener);
  return () => {
    const idx = geoListeners.indexOf(listener);
    if (idx >= 0) geoListeners.splice(idx, 1);
  };
}

export function triggerIpLookup(_ip?: string): void {}

/**
 * Resolve country info for a server.
 * Priority:
 * 1. Admin specified region (Highest priority)
 * 2. IP Geolocation (if recognized or cached)
 * 3. Server name or hostname keyword match
 * 4. Fallback default
 */
export function resolveCountryInfo(
  adminRegion?: string,
  ip?: string,
  name?: string,
  hostname?: string
): CountryInfo {
  // 1. Admin specified region (Highest Priority)
  if (adminRegion && adminRegion.trim()) {
    const match = matchTextRule(adminRegion);
    if (match) {
      return {
        code: match.code,
        name: match.name,
        flag: countryCodeToFlag(match.code),
        source: 'admin_region'
      };
    }
  }

  // 2. IP Geolocation lookup
  const cleanIp = (ip || '').trim();
  if (cleanIp && !isPrivateIp(cleanIp)) {
    const cachedCode = localStorage.getItem(`qui_ip_geo_${cleanIp}`);
    if (cachedCode && cachedCode.length === 2) {
      const rule = REGION_RULES.find(r => r.code === cachedCode.toUpperCase());
      return {
        code: cachedCode.toUpperCase(),
        name: rule?.name || cachedCode.toUpperCase(),
        flag: countryCodeToFlag(cachedCode),
        source: 'ip_geo'
      };
    }
    triggerIpLookup(cleanIp);
  }

  // 3. Fallback: server name or hostname match
  const nameMatch = matchTextRule(name) || matchTextRule(hostname);
  if (nameMatch) {
    return {
      code: nameMatch.code,
      name: nameMatch.name,
      flag: countryCodeToFlag(nameMatch.code),
      source: 'hostname_match'
    };
  }

  // 4. Default fallback
  return {
    code: '',
    name: '未知地区',
    flag: '🌐',
    source: 'fallback'
  };
}
