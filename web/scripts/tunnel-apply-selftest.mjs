import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const read = path => readFileSync(new URL('../src/' + path, import.meta.url), 'utf8');
const source = read('modules/tunnels/applyTunnels.ts').replace(/^import .*;\r?\n/gm, '').replaceAll('export ', '');
const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.None } }).outputText;
class APIError extends Error { constructor(status, code) { super(code); this.status = status; this.code = code; } }
const resultLabels = { applied: '已应用', unchanged: '无变更', failed: '失败', rolled_back: '已回滚', skipped: '已跳过', publishing: '发布中' };
function load(tunnelAPI) {
  return new Function('APIError', 'nodeAPI', 'confirmedRevision', 'needsBaseline', 'publishBaselineAndWait', 'gostInstallRelease', 'coreUpdatesAPI', 'tunnelAPI', 'tunnelError', 'resultLabels',
    code + '\nreturn { applyTunnelChanges, applyProblem };')(APIError, {}, () => 0, () => false, async () => {}, async () => ({}), {}, tunnelAPI, e => e.message, resultLabels);
}
const selection = { tunnelIds: [], ruleIds: ['r'] };
const ok = { ok: true, conflicts: [], warnings: [], servers: [] };

// A refused plan never publishes and names the conflict.
{
  const calls = [];
  const m = load({ plan: async () => { calls.push('plan'); return { ok: false, message: '端口冲突', conflicts: [{ nodeId: 'n', port: 80, usedBy: 'x' }], warnings: [], servers: [{ nodeId: 'n', nodeName: '香港' }] }; }, publish: async () => calls.push('publish') });
  await assert.rejects(m.applyTunnelChanges(selection), /端口冲突；香港:80 已被 x 占用/);
  assert.deepEqual(calls, ['plan']);
}
// GOST disappearing between plan and publish re-plans once; a second refusal surfaces.
{
  const calls = [];
  let refuse = 1;
  const m = load({ plan: async () => { calls.push('plan'); return ok; }, publish: async () => { calls.push('publish'); if (refuse-- > 0) throw new APIError(409, 'gost_install_required'); return { status: 'success', servers: [] }; } });
  assert.equal((await m.applyTunnelChanges(selection)).status, 'success');
  assert.deepEqual(calls, ['plan', 'publish', 'plan', 'publish']);
  const always = load({ plan: async () => ok, publish: async () => { throw new APIError(409, 'gost_install_required'); } });
  await assert.rejects(always.applyTunnelChanges(selection), /gost_install_required/);
}
// Other start errors are reported as-is (v1.5.5 hid them behind 发布中止，未下发).
{
  const m = load({ plan: async () => ok, publish: async () => { throw new APIError(409, 'node_busy'); } });
  await assert.rejects(m.applyTunnelChanges(selection), /node_busy/);
}
// Result summary.
{
  const { applyProblem } = load({});
  assert.equal(applyProblem({ status: 'success', servers: [{ nodeName: 'a', status: 'applied' }, { nodeName: 'b', status: 'unchanged' }] }), '');
  assert.equal(applyProblem({ status: 'failed', servers: [{ nodeName: 'a', status: 'rolled_back', message: '启动失败' }, { nodeName: 'b', status: 'skipped' }] }), 'a：启动失败；b：已跳过');
  assert.match(applyProblem({ status: 'publishing', servers: [] }), /仍在应用/);
}
// The page has no separate publish step any more.
const page = read('modules/tunnels/pages/TunnelsPage.vue');
assert.doesNotMatch(page, /PublishFlow|>发布</);
assert.match(page, /@saved="selection => \{ editor = false; apply\(selection/);
assert.match(read('modules/tunnels/components/TunnelEditor.vue'), /confirm-text="保存并应用"/);
assert.match(read('modules/tunnels/components/RuleEditor.vue'), /confirm-text="保存并应用"/);
console.log('PASS tunnel apply: refused plan, GOST re-plan once, start errors surfaced, result summary, no publish step on the page');
