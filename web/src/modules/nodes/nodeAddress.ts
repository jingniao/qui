// Node addresses as the panel and the Agent agree on them (AGENTS.md §11.1).
// The client address a subscription uses is the Owner's connect host when one is
// set, otherwise the public address the Agent detected or the Owner typed. An
// empty result means the node has no dialable address at all, so its inbounds
// stay out of every subscription; no other value may stand in for it.

export interface NodeAddress {
  publicIp?: string;
  connectHost?: string;
}

// The one definition of "the address a client dials". Relay, tunnel and canvas
// endpoints keep using the public IP, because those are addresses the nodes reach
// each other on.
export function connectAddress(node: NodeAddress | null | undefined): string {
  if (!node) return '';
  return (node.connectHost || '').trim() || (node.publicIp || '').trim();
}

// The address fields the Owner actually changed, ready to merge into the node
// update body. An omitted field keeps the stored value, which is what keeps a
// detected address detected: a non-empty public IP is stored as a manual value
// that Agent detection never overwrites, so echoing the loaded value back on an
// unrelated save would pin it forever.
export function addressChanges(loaded: NodeAddress, next: NodeAddress): NodeAddress {
  const changes: NodeAddress = {};
  const publicIp = next.publicIp?.trim();
  if (publicIp !== undefined && publicIp !== (loaded.publicIp || '').trim()) changes.publicIp = publicIp;
  const connectHost = next.connectHost?.trim();
  if (connectHost !== undefined && connectHost !== (loaded.connectHost || '').trim()) changes.connectHost = connectHost;
  return changes;
}

// The panel's rule for a connect host: an empty value, an IP literal, or a host
// name made of [A-Za-z0-9.-] labels that do not start or end with a hyphen - no
// scheme, port or brackets. IPv6 is allowed here (unlike the install command),
// because it is only ever shown to a client.
export function validConnectHost(value: string): boolean {
  const text = value.trim();
  if (text === '') return true;
  if (validIPLiteral(text)) return true;
  if (text.length > 253) return false;
  return text.split('.').every(label => /^[A-Za-z0-9-]{1,63}$/.test(label) && !label.startsWith('-') && !label.endsWith('-'));
}

function validIPLiteral(value: string): boolean {
  if (/^\d{1,3}(\.\d{1,3}){3}$/.test(value)) return value.split('.').every(part => Number(part) <= 255);
  try {
    return value.includes(':') && new URL(`http://[${value}]/`).hostname.startsWith('[');
  } catch {
    return false;
  }
}
