import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const source = path => readFileSync(new URL('../src/' + path, import.meta.url), 'utf8');
const url = code => `data:text/javascript;base64,${Buffer.from(ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } }).outputText).toString('base64')}`;
const apiURL = url(source('app/apiClient.ts'));
const { APIError } = await import(apiURL);
const { inboundDeleteConflict } = await import(url(source('modules/proxy/inboundDeleteConflict.ts').replace('../../app/apiClient', apiURL)));
for (const code of ['inbound_in_canvas_scheme', 'inbound_detached_entry']) {
  const conflict = inboundDeleteConflict(new APIError(409, code, '请先撤除', { schemes: [{ id: 'scheme', name: '方案' }] }));
  assert.ok(conflict, code);
  assert.equal(conflict.message, '请先撤除');
  assert.match(source('app/apiClient.ts'), new RegExp(code));
}
assert.equal(inboundDeleteConflict(new APIError(500, 'internal_error', '失败')), null);
assert.match(source('modules/proxy/components/InboundDeleteConflict.vue'), /\/proxy\/relay/);
const { ruleTargetLabel } = await import(url(source('modules/tunnels/model.ts')));
assert.equal(ruleTargetLabel({ mode: 'linked', inboundId: 'gone', host: '', port: 0 }), '目标已删除 · —');
assert.equal(ruleTargetLabel({ mode: 'linked', nodeName: '节点', inboundName: '入口', protocol: 'socks5', host: '1.2.3.4', port: 1080 }), '节点 · 入口 · SOCKS5 · 1.2.3.4:1080');
const page = source('modules/tunnels/pages/TunnelsPage.vue');
assert.match(page, /ruleTargetLabel\(r.target\)/);
assert.doesNotMatch(page, /r.target.protocol.toUpperCase\(\)/);
console.log('relay cleanup selftest passed');
