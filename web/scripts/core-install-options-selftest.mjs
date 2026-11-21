import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

async function tsModuleUrl(url, cache = new Map()) {
  if (cache.has(url.href)) return cache.get(url.href);
  let code = ts.transpileModule(readFileSync(url, 'utf8'), {
    compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext }
  }).outputText;
  // 为 selftest 提供轻量 vue ref mock
  const vueMock = `data:text/javascript;base64,${Buffer.from('export const ref = (v) => ({ value: v });').toString('base64')}`;
  code = code.replace(/from\s+['"]vue['"]/g, `from '${vueMock}'`);

  const specs = [...new Set([...code.matchAll(/from\s+'([^']+)'/g)].map(m => m[1]).filter(v => v.startsWith('.')))];
  for (const spec of specs) {
    const target = new URL(spec, url);
    if (!/\.tsx?$/.test(target.pathname)) target.pathname += '.ts';
    code = code.split(`from '${spec}'`).join(`from '${await tsModuleUrl(target, cache)}'`);
  }
  const moduleUrl = `data:text/javascript;base64,${Buffer.from(code).toString('base64')}`;
  cache.set(url.href, moduleUrl);
  return moduleUrl;
}

const optionsUrl = new URL('../src/modules/nodes/coreInstallOptions.ts', import.meta.url);
const mod = await import(await tsModuleUrl(optionsUrl));

// 1. 验证内核选项定义
assert.ok(Array.isArray(mod.CORE_INSTALL_OPTIONS), 'CORE_INSTALL_OPTIONS 必须是数组');
assert.equal(mod.CORE_INSTALL_OPTIONS.length, 3, '必须包含 xray、sing-box、gost 三项');
const gostOption = mod.CORE_INSTALL_OPTIONS.find(o => o.name === 'gost');
assert.ok(gostOption, '必须包含 GOST 选项');
assert.match(gostOption.hint, /仅 linux\/amd64/);
assert.match(gostOption.hint, /端口转发与隧道/);
assert.match(mod.installCoresDescription([]), /仅安装 Agent/);
assert.match(mod.installCoresDescription(['gost']), /纯转发节点/);
assert.equal(mod.installCoresDescription(['xray']), '');
assert.deepEqual(mod.sortInstallCores([]), []);
assert.deepEqual(mod.sortInstallCores(['gost']), ['gost']);
assert.deepEqual(mod.sortInstallCores(['gost', 'xray']), ['xray', 'gost']);
assert.deepEqual(mod.sortInstallCores(['gost', 'sing-box']), ['sing-box', 'gost']);
assert.deepEqual(mod.sortInstallCores(['gost', 'sing-box', 'xray']), ['xray', 'sing-box', 'gost']);
assert.deepEqual(mod.sortInstallCores(['xray', 'gost']), ['xray', 'gost']);

assert.equal(mod.isNodeReissuing('n1'), false);
mod.setReissuingNodeId('n1');
assert.equal(mod.isNodeReissuing('n1'), true);
assert.equal(mod.isNodeReissuing('n2'), false);
mod.setReissuingNodeId(null);
assert.equal(mod.isNodeReissuing('n1'), false);

assert.deepEqual(mod.reportedInstallCores({core_name:'sing-box'}), ['sing-box']);
assert.deepEqual(mod.reportedInstallCores({cores:[{name:'sing-box'}]}), ['sing-box']);
assert.deepEqual(mod.reportedInstallCores({cores:[{name:'gost'},{name:'xray'}]}), ['xray','gost']);
const singBoxPrimary = mod.reportedInstallCores({core_name:'sing-box', cores:[{name:'xray'},{name:'sing-box'},{name:'gost'}]});
assert.deepEqual(singBoxPrimary, ['sing-box','xray','gost']);
assert.deepEqual(mod.sortInstallCores(['gost','xray','sing-box'], singBoxPrimary[0]), singBoxPrimary);
assert.deepEqual(mod.sortInstallCores(['gost','xray'], singBoxPrimary[0]), ['xray','gost']);
assert.deepEqual(mod.reportedInstallCores({}), []);

console.log('PASS CORE INSTALL OPTIONS: selftest passed successfully');

const summary = await import(await tsModuleUrl(new URL('../src/modules/nodes/coreSummary.ts', import.meta.url)));
assert.equal(summary.nodeHasUserCore({}), true);
assert.equal(summary.nodeHasUserCore({ capabilities: { core_name: 'xray' } }), true);
assert.equal(summary.nodeHasUserCore({ capabilities: { core_optional_supported: true, cores: [] } }), false);
assert.equal(summary.nodeHasUserCore({ capabilities: { core_optional_supported: true, cores: [{ name: 'gost' }] } }), false);
assert.equal(summary.nodeHasUserCore({ capabilities: { core_optional_supported: true, cores: [{ name: 'gost' }, { name: 'sing-box' }] } }), true);
// Only the pure blocker mapping is exercised; its network imports must stay unused.
const blockerSource = readFileSync(new URL('../src/modules/nodes/coreRemovalBlockers.ts', import.meta.url), 'utf8').replace(/^import .*;$/gm, '');
const blockerCode = ts.transpileModule(blockerSource, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const blockers = await import('data:text/javascript;base64,' + Buffer.from(blockerCode).toString('base64'));
const fields = ['inboundIds', 'outboundIds', 'outboundGroupIds', 'proxyChainIds', 'relayIds', 'schemeIds'];
const groups = blockers.coreRemovalBlockers(Object.fromEntries(fields.map((key, i) => [key, ['id/' + i]])), 'node/a');
assert.deepEqual(groups.map(group => group.label), ['入站', '出站', '出口组', '代理链', '中转线路', '画布方案']);
assert.deepEqual(groups.map(group => group.items[0].id), fields.map((_, i) => 'id/' + i));
assert.deepEqual(groups.map(group => group.items[0].to), ['/nodes/node%2Fa?tab=nodes', '/nodes/node%2Fa?tab=nodes', '/nodes/node%2Fa?tab=nodes', undefined, '/proxy/relay', '/proxy/relay?schemeId=id%2F5']);
assert.deepEqual(blockers.coreRemovalBlockers({ inboundIds: [], outboundIds: null, unknown: ['x'] }, 'node'), []);
console.log('PASS optional cores: legacy classification and all six removal blocker fields');
