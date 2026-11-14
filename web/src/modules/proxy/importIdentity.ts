interface ImportIdentity {
  id: string;
  sourceId?: string;
  name: string;
  protocol: string;
  proxyKey?: string;
}

interface ImportMember { sourceId: string; proxyKey: string }
interface IdentitySource { id: string; name: string; kind?: string }

export function importProxyKey(proxy: { id: string; kind: 'content' | 'url'; proxyKey: string }): string {
  return proxy.kind === 'content' ? `id:${proxy.id}` : proxy.proxyKey;
}

export function resolveImportMember<T extends ImportIdentity>(member: ImportMember, sources: readonly IdentitySource[], proxies: readonly T[]): { name: string; invalid: boolean; proxy?: T } {
  const source = sources.find(item => item.id === member.sourceId);
  if (source && source.kind !== 'content' && member.proxyKey === '') {
    return { name: `${source.name}（整个来源）`, invalid: false };
  }
  const proxy = source && proxies.find(item => item.sourceId === member.sourceId
    && (source.kind === 'content' ? member.proxyKey === `id:${item.id}` : member.proxyKey === item.proxyKey));
  return proxy
    ? { name: proxy.name, invalid: false, proxy }
    : { name: `已失效：${member.proxyKey}`, invalid: true };
}

export function importDeletionState(preview: { groups: readonly unknown[]; clientPolicies: readonly unknown[]; canvases: readonly unknown[] }, removeReferences: boolean): { needsRemoval: boolean; canConfirm: boolean } {
  const needsRemoval = preview.groups.length > 0 || preview.clientPolicies.length > 0;
  return { needsRemoval, canConfirm: preview.canvases.length === 0 && (!needsRemoval || removeReferences) };
}
