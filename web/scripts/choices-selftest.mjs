import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
const source = readFileSync(new URL('../src/modules/proxy/api.ts', import.meta.url), 'utf8');
const fn = source.slice(source.indexOf('export async function choices<T>'), source.indexOf('export const failureNames'));
const code = ts.transpileModule(fn, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const { choices } = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
const pages = [];
let attempt = 0;
assert.deepEqual(await choices(async page => {
  pages.push(page);
  if (page === 1) attempt++;
  if (attempt === 1) return page === 1 ? { items: ['old'], total: 2 } : { items: ['changed'], total: 3 };
  return { items: ['fresh'], total: 1 };
}), ['fresh']);
assert.deepEqual(pages, [1, 2, 1]);
const shrinking = [];
await choices(async page => {
  shrinking.push(page);
  return { items: [page], total: page === 1 ? 2 : 1 };
}).then(() => assert.fail('unstable totals must fail'), error => assert.match(error.message, /选项列表发生变化/));
assert.deepEqual(shrinking, [1, 2, 1, 2, 1, 2], 'initial read plus at most two retries');
await assert.rejects(choices(async () => { throw new Error('读取失败'); }), /读取失败/);
assert.deepEqual(await choices(async () => ({ items: [], total: 0 })), []);
assert.deepEqual(await choices(async page => ({ items: [page], total: 2 })), [1, 2]);
console.log('PASS choices: changed totals restart page one, discard stale rows, two retries, read errors and stable pages');
