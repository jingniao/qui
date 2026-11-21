// Protocol identity colors live in tailwind.config.js; all badges use this map.
// New protocols receive an explicit assignment. Reserved colors stay unassigned.
export const PROTOCOL_BADGE_CLASSES: Readonly<Record<string, string>> = {
  vless: 'bg-protocol-vless/10 text-protocol-vless border-protocol-vless/20',
  vmess: 'bg-protocol-vmess/10 text-protocol-vmess border-protocol-vmess/20',
  trojan: 'bg-protocol-trojan/10 text-protocol-trojan border-protocol-trojan/20',
  shadowsocks: 'bg-protocol-shadowsocks/10 text-protocol-shadowsocks border-protocol-shadowsocks/20',
  wireguard: 'bg-protocol-wireguard/10 text-protocol-wireguard border-protocol-wireguard/20',
  socks5: 'bg-protocol-socks5/10 text-protocol-socks5 border-protocol-socks5/20',
  http: 'bg-protocol-http/10 text-protocol-http border-protocol-http/20',
  hysteria2: 'bg-protocol-hysteria2/10 text-protocol-hysteria2 border-protocol-hysteria2/20',
  tuic: 'bg-protocol-tuic/10 text-protocol-tuic border-protocol-tuic/20',
  forward: 'bg-protocol-forward/10 text-protocol-forward border-protocol-forward/20',
  direct: 'bg-protocol-direct/10 text-protocol-direct border-protocol-direct/20',
  block: 'bg-protocol-block/10 text-protocol-block border-protocol-block/20'
};

export const PROTOCOL_COLOR_ALIASES: Readonly<Record<string, string>> = {
  ss: 'shadowsocks',
  hy2: 'hysteria2',
  socks: 'socks5',
  'http connect': 'http',
  blackhole: 'block'
};

// Reserved colour slots not yet assigned to a protocol (docs/design-system.md §2.4):
// coral, mint, steel, ochre. Assign one only when a new protocol is registered.
