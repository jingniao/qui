// A transient error in one member must not hide a later permission loss in
// another. Denials reject immediately, other failures wait for the whole batch.
export async function readTogether<T extends readonly unknown[]>(reads: readonly [...T]) {
  const results = await Promise.all(reads.map(async read => {
    try { return { ok: true as const, value: await read }; }
    catch (error) {
      const status = (error as { status?: number } | null)?.status;
      if (status === 401 || status === 403 || status === 404) throw error;
      return { ok: false as const, error };
    }
  }));
  return results.map(result => {
    if (!result.ok) throw result.error;
    return result.value;
  }) as { [K in keyof T]: Awaited<T[K]> };
}
