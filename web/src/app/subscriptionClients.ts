// 订阅格式面向用户的名称与适用客户端（v1.5.4 第 11、12 项）。用户详情的订阅区块
// 与用户门户的订阅页共用这一份，避免两处说法不一致。格式 ID 与后端导出器一致
// （GET /api/v1/subscription-formats）。

export interface SubscriptionClient {
  /** 导出器 ID，也是 ?format= 的取值 */
  format: string;
  /** 面向用户的名称 */
  label: string;
  /** 一句适用客户端说明 */
  clients: string;
}

export const SUBSCRIPTION_CLIENTS: SubscriptionClient[] = [
  { format: 'clash', label: 'Clash Meta（mihomo）', clients: '适用于 Clash Verge Rev、Mihomo Party、Clash Meta for Android、FlClash' },
  { format: 'sing-box', label: 'sing-box', clients: '适用于 sing-box 官方客户端（SFA／SFI／SFM）' },
  { format: 'base64', label: '通用订阅（Base64）', clients: '适用于 v2rayN、v2rayNG、Shadowrocket 等' },
  { format: 'uri', label: '通用链接列表', clients: '每行一条分享链接，可逐条导入' },
  { format: 'clash-legacy', label: '旧版 Clash（Premium 核心）', clients: '仅用于只支持旧版 Clash 核心的客户端，如 Clash for Windows；不含 VLESS、Hysteria2 等协议' },
];

/** 不带 ?format= 的链接：按客户端 User-Agent 识别，识别不了时返回系统默认格式。 */
export const AUTO_SUBSCRIPTION_LABEL = '自动识别（按客户端选择格式）';

export function subscriptionClient(format: string): SubscriptionClient {
  return SUBSCRIPTION_CLIENTS.find((item) => item.format === format) ?? { format, label: format, clients: '' };
}

/**
 * 按推荐顺序排列可用格式，系统默认格式置顶。`available` 为后端实际注册的格式 ID，
 * 不在其中的条目不显示。
 */
export function orderedSubscriptionClients(available: string[], defaultFormat?: string): SubscriptionClient[] {
  const known = SUBSCRIPTION_CLIENTS.filter((item) => available.includes(item.format));
  const extra = available.filter((id) => !SUBSCRIPTION_CLIENTS.some((item) => item.format === id)).map(subscriptionClient);
  const all = [...known, ...extra];
  const index = all.findIndex((item) => item.format === defaultFormat);
  if (index > 0) all.unshift(...all.splice(index, 1));
  return all;
}
