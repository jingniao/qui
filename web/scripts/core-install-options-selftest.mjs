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
assert.match(gostOption.hint, /需同时选择 Xray 或 sing-box/);

// 2. 验证校验规则：只选 GOST 不允许生成
const onlyGost = mod.validateInstallCores(['gost']);
assert.equal(onlyGost.valid, false, '只选 GOST 必须校验失败');
assert.ok(onlyGost.error, '只选 GOST 必须返回错误说明');
assert.match(onlyGost.error, /需同时选择 Xray 或 sing-box/);

// 3. 验证校验规则：空列表校验失败
const empty = mod.validateInstallCores([]);
assert.equal(empty.valid, false, '空列表必须校验失败');
assert.ok(empty.error, '空列表必须返回错误提示');

// 4. 验证校验规则：合法选择
assert.equal(mod.validateInstallCores(['xray']).valid, true);
assert.equal(mod.validateInstallCores(['sing-box']).valid, true);
assert.equal(mod.validateInstallCores(['xray', 'gost']).valid, true);
assert.equal(mod.validateInstallCores(['sing-box', 'gost']).valid, true);
assert.equal(mod.validateInstallCores(['xray', 'sing-box', 'gost']).valid, true);

// 5. 验证排序规则：GOST 永远排在最后（安装器要求 GOST 不能是首项）
assert.deepEqual(mod.sortInstallCores(['gost', 'xray']), ['xray', 'gost']);
assert.deepEqual(mod.sortInstallCores(['gost', 'sing-box']), ['sing-box', 'gost']);
assert.deepEqual(mod.sortInstallCores(['gost', 'sing-box', 'xray']), ['xray', 'sing-box', 'gost']);
assert.deepEqual(mod.sortInstallCores(['xray', 'gost']), ['xray', 'gost']);

// 6. 验证安装按钮签发状态管理
assert.equal(mod.isNodeReissuing('n1'), false);
mod.setReissuingNodeId('n1');
assert.equal(mod.isNodeReissuing('n1'), true);
assert.equal(mod.isNodeReissuing('n2'), false);
mod.setReissuingNodeId(null);
assert.equal(mod.isNodeReissuing('n1'), false);

console.log('PASS CORE INSTALL OPTIONS: selftest passed successfully');
