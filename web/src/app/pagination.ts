export async function allPages<T extends { items: unknown[]; total: number }>(fetchPage: (page: number) => Promise<T>): Promise<T> {
  const first = await fetchPage(1);
  const items = [...first.items];
  for (let page = 2; items.length < first.total; page++) {
    const next = await fetchPage(page);
    if (!next.items.length) throw new Error('分页读取不完整，请重试');
    items.push(...next.items);
  }
  return { ...first, items } as T;
}
