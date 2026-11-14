/** Only rearranges references; group contents and input arrays remain untouched. */
export function recommendGroupOrder<T extends { name: string; type: string; proxies: string }>(
  groups: readonly T[],
  categories: Readonly<Record<string, 'service' | 'region' | 'other'>>,
  rules: readonly { type: string; target: string }[],
): T[] {
  const matches = new Set(rules.filter(rule => rule.type === 'MATCH').map(rule => rule.target));
  const counts = new Map<string, number>();
  for (const group of groups) {
    for (const member of new Set(group.proxies.split(',').map(value => value.trim()))) {
      if (member !== group.name) counts.set(member, (counts.get(member) || 0) + 1);
    }
  }
  let main: T | undefined;
  for (const group of groups) {
    if (group.type === 'select' && !matches.has(group.name)
      && (!main || (counts.get(group.name) || 0) > (counts.get(main.name) || 0))) main = group;
  }
  const rank = (group: T) => matches.has(group.name) ? 4 : group === main ? 0
    : group.type === 'url-test' || group.type === 'fallback' ? 1
    : categories[group.name] === 'region' ? 3 : 2;
  return groups.map((group, index) => ({ group, index }))
    .sort((a, b) => rank(a.group) - rank(b.group) || a.index - b.index)
    .map(item => item.group);
}
