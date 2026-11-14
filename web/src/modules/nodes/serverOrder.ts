// Server list order and server-group tags (v1.5.5 items 3 and 4). Pure helpers,
// so the ordering rules have a selftest without a browser.

export const UNTAGGED = '__untagged';

interface Ordered { id: string; tags?: string[]; deletedAt?: string }

// applyServerOrder sorts by the saved order. Servers the order does not name
// (added since the save) follow in their API order, which is creation order;
// saved IDs of deleted servers are simply skipped.
export function applyServerOrder<T extends Ordered>(nodes: T[], ids: string[]): T[] {
  const rank = new Map(ids.map((id, index) => [id, index]));
  const known = nodes.filter(node => rank.has(node.id)).sort((a, b) => rank.get(a.id)! - rank.get(b.id)!);
  return [...known, ...nodes.filter(node => !rank.has(node.id))];
}

// moveServer swaps a server with its neighbour; a move past either end is a no-op.
export function moveServer(ids: string[], id: string, delta: -1 | 1): string[] {
  const from = ids.indexOf(id), to = from + delta;
  if (from < 0 || to < 0 || to >= ids.length) return ids;
  const next = [...ids];
  [next[from], next[to]] = [next[to], next[from]];
  return next;
}

// reorderServer moves one server to the position of another (drag and drop).
export function reorderServer(ids: string[], fromId: string, toId: string): string[] {
  const from = ids.indexOf(fromId), to = ids.indexOf(toId);
  if (from < 0 || to < 0 || from === to) return ids;
  const next = [...ids];
  next.splice(from, 1);
  next.splice(to, 0, fromId);
  return next;
}

// tagOptions lists every tag of the live servers with its count, by name, plus
// the number of untagged servers.
export function tagOptions(nodes: Ordered[]): { tags: { tag: string; count: number }[]; untagged: number } {
  const counts = new Map<string, number>();
  let untagged = 0;
  for (const node of nodes) {
    if (node.deletedAt) continue;
    if (!node.tags?.length) untagged++;
    for (const tag of node.tags || []) counts.set(tag, (counts.get(tag) || 0) + 1);
  }
  const tags = [...counts].map(([tag, count]) => ({ tag, count })).sort((a, b) => a.tag.localeCompare(b.tag, 'zh-CN'));
  return { tags, untagged };
}

export function matchesTag(node: Ordered, tag: string): boolean {
  if (!tag) return true;
  return tag === UNTAGGED ? !node.tags?.length : Boolean(node.tags?.includes(tag));
}

// normalizeTagInput mirrors the panel rule: trimmed, 1–20 characters, no comma
// or control character, case-insensitively unique, at most 10. It returns an
// error message instead of throwing so the editor can show it inline.
export function addTag(tags: string[], input: string): { tags: string[]; error?: string } {
  const tag = input.trim();
  if (!tag) return { tags };
  if ([...tag].length > 20 || /[,\u0000-\u001f\u007f]/.test(tag)) return { tags, error: '标签为 1–20 个字符，不能包含逗号' };
  if (tags.some(item => item.toLowerCase() === tag.toLowerCase())) return { tags };
  if (tags.length >= 10) return { tags, error: '每台服务器最多 10 个标签' };
  return { tags: [...tags, tag] };
}
