import { createServer } from 'vite';
import { fileURLToPath } from 'node:url';

const vite = await createServer({ root: fileURLToPath(new URL('..', import.meta.url)), configFile: false, logLevel: 'silent', server: { middlewareMode: true }, optimizeDeps: { noDiscovery: true } });
export const { retainedQuery, removePageItem } = await vite.ssrLoadModule('/src/app/retainedQuery.ts');
export const { APIError } = await vite.ssrLoadModule('/src/app/apiClient.ts');
export const { readTogether } = await vite.ssrLoadModule('/src/app/readTogether.ts');
await vite.close();
