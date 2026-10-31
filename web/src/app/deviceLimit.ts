export const deviceLimitHelp = [
  '设备按来源地址估算：IPv4 使用完整地址，IPv6 按 /64 合并，跨入口节点去重。同一 NAT / CGNAT 下多台设备算一台；双栈或切换网络可能暂时算多台，旧地址最多保留约 180 秒。',
  '超限地址可能继续工作约 5–20 秒，直到下一次上报和投递。',
  'Xray 只拦截新连接，已建立的连接继续使用；sing-box 约每 2 秒关闭超限连接。',
  '先到先得，最近出现的地址可能受限；可联系管理员提高上限或重置准入顺序。',
  '面板失联后 gate 在 3 分钟内过期，自动放行。旧 Agent、能力不足或观测失败的节点只能部分执行。',
  'WireGuard 一次只能在一台设备上使用，不计数、不限制。',
  '来源地址只在内存中处理，不存储、不记日志、不备份；管理员与用户界面统一掩码为 IPv4 /24、IPv6 /48。',
  '以下入站的 UDP 不受设备限制（TCP 仍受限制）：Xray 与 sing-box 的 SOCKS5，以及 sing-box 的 Shadowsocks、Hysteria2、TUIC。VLESS、Trojan 的 UDP 与 Xray 的 Shadowsocks UDP 受限制。'
];

const protocolNames: Record<string, string> = { socks5: 'SOCKS5', shadowsocks: 'Shadowsocks', hysteria2: 'Hysteria2', tuic: 'TUIC' };

/** Mirrors node.DeviceGateUDPUnsupported (internal/panel/node/multi_core.go). */
export function deviceGateUdpUnsupported(core: string | null | undefined, protocol: string): boolean {
  if (core === 'xray') return protocol === 'socks5';
  if (core === 'sing-box') return ['socks5', 'shadowsocks', 'hysteria2', 'tuic'].includes(protocol);
  return false;
}

/** The "不支持" line for protocols whose UDP the device limit cannot reach. */
export function udpUnsupportedText(protocols: string[] | null | undefined): string {
  if (!protocols?.length) return '';
  return '不支持：' + protocols.map((p) => protocolNames[p] ?? p).join('、') + ' 的 UDP 不受设备限制';
}
