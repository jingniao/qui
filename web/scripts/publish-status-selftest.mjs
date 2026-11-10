import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

async function tsModuleUrl(url, cache = new Map()) {
  if (cache.has(url.href)) return cache.get(url.href);
  let code = ts.transpileModule(readFileSync(url, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
  const specs = [...new Set([...code.matchAll(/from\s+'([^']+)'/g)].map(match => match[1]).filter(value => value.startsWith('.')))];
  for (const spec of specs) {
    const target = new URL(spec, url);
    if (!/\.tsx?$/.test(target.pathname)) target.pathname += '.ts';
    code = code.split(`from '${spec}'`).join(`from '${await tsModuleUrl(target, cache)}'`);
  }
  const moduleUrl = `data:text/javascript;base64,${Buffer.from(code).toString('base64')}`;
  cache.set(url.href, moduleUrl);
  return moduleUrl;
}

const progressUrl = new URL('../src/modules/system/publishProgress.ts', import.meta.url);
const progress = await import(await tsModuleUrl(progressUrl));

assert.deepEqual(progress.publishPhase('pending'), { label: '待服务器确认', tone: 'info', final: false });
assert.equal(progress.publishPhase('success').label, '已生效');
assert.equal(progress.publishPhase('rolled_back').final, true);
// 只认自己的修订：列表里先出现 #7 成功，#8 仍 pending
let seen = [];
const target = new EventTarget();
let clock = 0; const timers = [];
const list = async () => ({ items: [{ id: 7, status: 'success' }, { id: 8, status: 'pending' }], total: 2 });
const watch = progress.watchPublish('n1', 8, (phase, timedOut) => seen.push([phase.label, timedOut]),
  { list, now: () => clock, setTimer: (fn) => { timers.push(fn); return timers.length; }, clearTimer: () => {}, target });
target.dispatchEvent(new Event('proxy-deployment-status')); await new Promise(r => setTimeout(r, 0));
assert.deepEqual(seen.at(-1), ['待服务器确认', false]);
clock = progress.publishWatchTimeout + 1; timers.at(-1)(); await new Promise(r => setTimeout(r, 0));
assert.deepEqual(seen.at(-1), ['待服务器确认', true]);
watch.stop();
console.log('PASS P1 PUBLISH: phases, own revision only, timeout');

assert.equal(progress.serverPublishState(undefined, undefined).label, '尚未发布');
assert.equal(progress.serverPublishState({ id: 9, status: 'success' }, 9).label, '已生效');
assert.equal(progress.serverPublishState({ id: 9, status: 'success' }, 8).label, '已生效，等待服务器上报');
assert.equal(progress.serverPublishState({ id: 9, status: 'failed' }, 8).tone, 'danger');
console.log('PASS P1 SERVER PUBLISH STATE');
