import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const source = readFileSync(new URL('../src/modules/proxy/legacyResources.ts', import.meta.url), 'utf8');
const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const { legacyResourceSections, legacyResourceActions, legacyResourceCount, legacyReferrersText, legacyEmptyTunnelToDelete, legacyDefaultSelection } = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
const group = { id: 'g', bindingIds: ['b'], tunnelIds: ['t'], bindings: [], tunnels: [], referrers: [], canReattach: true, canRemove: true, reattachReasons: [], removeReasons: [] };
assert.equal(legacyEmptyTunnelToDelete({ ...group, empty: true, canReattach: false }), 't');
assert.equal(legacyEmptyTunnelToDelete({ ...group, empty: true, canRemove: false }), undefined);
assert.equal(legacyEmptyTunnelToDelete(group), undefined);
const resources = { detached: [group], orphanTunnels: [{ ...group, id: 'o' }], unowned: [{ kind: 'inbound', id: 'i', name: '历史监听', candidateTunnels: ['t'] }] };
assert.deepEqual(legacyResourceSections(resources).map(section => section.label), ['已脱离方案的入口', '无方案引用的通道', '归属待核实']);
assert.equal(legacyResourceCount(resources), 3);
assert.equal(legacyResourceCount({ detached: [], orphanTunnels: [], unowned: [] }), 0);
assert.deepEqual(legacyResourceActions(group), { showReattach: true, showRemove: true, partial: false, reasons: [] });
assert.deepEqual(legacyResourceActions({ ...group, canRemove: false, removeReasons: ['仍被策略「办公」引用'] }), { showReattach: true, showRemove: false, partial: false, reasons: ['无法完整撤除：仍被策略「办公」引用'] });
assert.deepEqual(legacyResourceActions({ ...group, canReattach: false, canRemove: false, reattachReasons: ['缺少出口端点'], removeReasons: ['无法还原布局'] }), { showReattach: false, showRemove: false, partial: false, reasons: ['无法重新挂接：缺少出口端点', '无法完整撤除：无法还原布局'] });
assert.equal(legacyResourceActions({ ...group, canReattach: false }).showRemove, false);
assert.equal(legacyReferrersText([{ kind: 'policy', id: 'p', name: '办公' }, { kind: 'outbound', id: 'o', name: '外部出口' }]), '分流策略：办公；出站：外部出口');
assert.equal(legacyResourceSections(resources)[2].items[0].name, '历史监听');
// Two parallel channels: the whole group cannot be restored, so the page offers parts,
// preselecting the entry and the channel its current outbound belongs to.
const parallel = { ...group, canReattach: false, canRemove: false, bindingIds: ['b'], tunnelIds: ['old', 'used'],
  bindings: [{ id: 'b', currentDefaultOutbound: 'out-used' }],
  tunnels: [{ id: 'old', objects: [{ kind: 'outbound', id: 'out-old' }] }, { id: 'used', objects: [{ kind: 'inbound', id: 'in-used' }, { kind: 'outbound', id: 'out-used' }] }] };
assert.equal(legacyResourceActions(parallel).partial, true);
assert.deepEqual(legacyDefaultSelection(parallel), ['binding:b', 'tunnel:used']);
assert.deepEqual(legacyDefaultSelection({ ...parallel, bindings: [] }), ['tunnel:old']);
assert.equal(legacyResourceActions({ ...parallel, tunnels: [parallel.tunnels[0]], bindings: [] }).partial, false, 'a single resource has no parts');
assert.equal(legacyResourceActions({ ...parallel, empty: true }).partial, false);
console.log('PASS canvas legacy: group labels, resource count, action visibility, blocker reasons, referrers and part-by-part selection');
