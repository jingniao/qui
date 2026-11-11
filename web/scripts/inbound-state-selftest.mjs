import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const read = path => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8');
const list = read('modules/proxy/pages/ProxyNodeListPage.vue');

// The node list labels each inbound from its own publish state, not from the
// server's latest revision.
assert.match(list, /deploymentAPI\.inboundStates\(/);
assert.doesNotMatch(list, /serverPublishState|nodeRevisions/);
for (const label of ['已生效', '有未发布修改', '发布中', '未生效']) assert.ok(list.includes(`'${label}'`), label);
assert.match(list, /最近一次发布失败/);
assert.match(read('modules/system/deploymentApi.ts'), /inboundStates: .*\/inbound-states/);

// Hysteria2 and TUIC are QUIC, whatever the TCP transport field defaults to.
assert.doesNotMatch(list, /settings\?\.transport \|\| 'tcp'\}\}/);
const transport = list.match(/function transportLabel[\s\S]*?\n}\n/)[0];
assert.match(transport, /hysteria2/);
assert.match(transport, /tuic/);
assert.match(transport, /udp \+ quic/);

// One result-code label list, used by the node list, the history and the server page.
const shared = read('modules/system/revisionResult.ts');
const { outputText } = ts.transpileModule(shared, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } });
const labels = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
assert.equal(labels.revisionResult('apply_failed'), '应用失败');
assert.equal(labels.revisionResult('whatever'), 'whatever');
assert.equal(labels.revisionResult(''), '—');
for (const path of ['modules/proxy/pages/ProxyNodeListPage.vue', 'modules/system/pages/RevisionPage.vue', 'modules/nodes/components/detail/serverDetailModel.ts']) {
  assert.match(read(path), /from '(\.\.\/)+(system\/)?revisionResult'/, path);
  assert.doesNotMatch(read(path), /已应用且健康/, path);
}
