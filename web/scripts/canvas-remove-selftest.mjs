import assert from 'node:assert/strict';
import { readFileSync, existsSync } from 'node:fs';

const dialog = readFileSync(new URL('../src/modules/proxy/components/relay/CanvasRemoveDialog.vue', import.meta.url), 'utf8');
const page = readFileSync(new URL('../src/modules/proxy/pages/RelayCanvasPage.vue', import.meta.url), 'utf8');
const api = readFileSync(new URL('../src/modules/proxy/api.ts', import.meta.url), 'utf8');
assert.match(dialog, /title="停用并移除" max-width="sm"/);
assert.match(dialog, /停用并移除「\{\{ scheme.name \}\}」？撤除期间相关用户连接会短暂中断。/);
assert.match(dialog, /@click="close">取消<\/button>/);
assert.match(dialog, /'停用并移除'/);
assert.match(page, /const canRemoveScheme = computed\(\(\) => !!currentScheme.value.id && currentScheme.value.executionLocation === 'server' && !!currentScheme.value.serverUpdatedAt\)/);
assert.match(page, /@removed="onSchemeRemoved"/);
assert.match(page, /result.pendingNodes.map\(node => node.name\).join\('、'\)/);
assert.match(page, /已停用并移除方案\$\{pending\}/);
assert.match(page, /await reloadSchemes\(\)/);
for (const source of [dialog, page, api]) assert.doesNotMatch(source, /removePlan|removeStatus|restoreTargets|acknowledgeChanges|legacyResources|canvasLegacyAPI|仅删除画布/);
for (const name of ['canvasRemove.ts', 'canvasRemovePolling.ts', 'legacyResources.ts', 'components/relay/LegacyResourcesPanel.vue']) {
  assert.equal(existsSync(new URL('../src/modules/proxy/' + name, import.meta.url)), false);
}
console.log('PASS canvas removal: one small confirmation, server saved schemes visible, pending cleanup notice, old flows removed');
