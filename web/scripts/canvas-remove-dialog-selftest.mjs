import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const source = readFileSync(new URL('../src/modules/proxy/canvasRemovePolling.ts', import.meta.url), 'utf8');
const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const { waitCanvasRemoval } = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
let polls = 0;
const delays = [];
assert.equal(await waitCanvasRemoval('s', {
  removeStatus: async () => ({ state: ++polls === 3 ? 'succeeded' : 'removing' }),
  get: async () => assert.fail('scheme read must not block behind active removal')
}, async ms => { delays.push(ms); }), 's');
assert.deepEqual(delays, [2000, 2000, 2000]);
await assert.rejects(waitCanvasRemoval('s', { removeStatus: async () => ({ state: 'failed', error: { message: '节点「德国」应用失败' } }) }, async () => {}), /节点「德国」应用失败/);
assert.equal(await waitCanvasRemoval('s', { removeStatus: async () => ({ state: 'idle' }), get: async () => { throw { status: 404 }; } }, async () => {}), 's');
await assert.rejects(waitCanvasRemoval('s', { removeStatus: async () => ({ state: 'idle' }), get: async () => { throw new Error('读取失败'); } }, async () => {}), /读取失败/);
let time = 0;
await assert.rejects(waitCanvasRemoval('s', { removeStatus: async () => ({ state: 'removing' }) }, async () => { time += 3600000; }, () => time), /一小时/);
const dialog = readFileSync(new URL('../src/modules/proxy/components/relay/CanvasRemoveDialog.vue', import.meta.url), 'utf8');
assert.match(dialog, /result.status === 'removing'/);
assert.match(dialog, /waitCanvasRemoval\(result.schemeId, canvasAPI\)/);
assert.match(dialog, /撤除进行中，可关闭此窗口，完成后列表会自动刷新/);
assert.match(dialog, /emit\('removing', completion\)/);
assert.match(dialog, /emit\('removing', completion\);\s*try\s*\{\s*await completion/);
const page = readFileSync(new URL('../src/modules/proxy/pages/RelayCanvasPage.vue', import.meta.url), 'utf8');
assert.match(page, /@removing="onRemovalPending"/);
assert.match(page, /onSchemeRemoved\(await completion\)/);
console.log('PASS canvas remove dialog: 202 polling, hidden completion, failure, restart 404 and one-hour deadline');
