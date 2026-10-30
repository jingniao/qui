import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
const read = path => readFileSync(new URL('../src/' + path, import.meta.url), 'utf8');
assert.match(read('app/router.ts'), /router\.beforeEach\(setupNavigationGuards\)/, 'navigation guard must be registered');
assert.doesNotMatch(read('modules/nodes/components/serverMetricsModel.ts'), /Math\.sin|431\s*\*\s*1024|1021\s*\*\s*1024|processes\s*[:=]\s*111|111\s*processes/, 'metrics must never use fabricated samples');
const meta = read('modules/nodes/nodeMeta.ts');
const saveMeta = meta.slice(meta.indexOf('export async function saveNodeMeta'));
assert.doesNotMatch(saveMeta, /\.\.\.meta\b/, 'metadata request must whitelist API fields');
assert.doesNotMatch(read('modules/system/components/NodeVersionPanel.vue'), /\b[a-f0-9]{64}\b/i, 'core hashes must be supplied by Owner');
const proxy = read('modules/proxy/pages/ProxyNodeListPage.vue');
const saveSub = proxy.slice(proxy.indexOf('async function saveEditSub'), proxy.indexOf('function formatInterval'));
assert.doesNotMatch(saveSub, /userIds\s*:\s*\[\s*\]/, 'editing a source must preserve grants');
assert.doesNotMatch(read('app/apiClient.ts'), /if\s*\([^)]*csrf_failed[^)]*\)|retried\s*:\s*true|return\s+request<T>\(path,\s*method,\s*body/, 'mutations must not auto-retry CSRF failures');
for (const file of ['modules/proxy/pages/LineGroupPage.vue', 'modules/proxy/components/LineGroupPicker.vue']) {
  assert.doesNotMatch(read(file), /localStorage/, file + ' must use API membership');
}
assert.ok(!meta.includes('[ports:'), 'port ranges must use dedicated API fields');
assert.doesNotMatch(meta, /qui_node_ports_/, 'port ranges must not use browser storage');
console.log('All UI regression self-tests passed');

const tuningF2 = read('modules/nodes/components/detail/ServerTuning.vue');
assert.doesNotMatch(tuningF2, /localStorage|qui_node_tuning_|BBR 就绪|saveTuningLocally/,'tuning applied state must come from the backend');
assert.match(tuningF2,/nodeAPI\.tuning\(/); assert.match(tuningF2,/nodeAPI\.saveTuning\(/);
assert.match(tuningF2,/ConfirmDialog/); assert.match(tuningF2,/3000/); assert.match(tuningF2,/60000/);
const metricF2 = read('modules/nodes/components/ServerMetricsGrid.vue');
assert.match(metricF2,/nodeAPI\.metrics\(/); assert.match(metricF2,/60000/); assert.match(metricF2,/proxy-node-status/);
assert.doesNotMatch(metricF2,/history\.value|持久化历史即将支持/);
assert.match(read('modules/nodes/components/detail/ServerMonitoring.vue'),/v-model:hours="monitoringHours"/);
for (const path of ['modules/nodes/components/detail/ServerTuning.vue','modules/nodes/components/detail/serverTuningModel.ts','modules/nodes/components/ServerMetricsGrid.vue','modules/nodes/components/hostObservationModel.ts']) assert.doesNotMatch(read(path), /\?{3,}/, 'F2 Chinese labels must survive UTF-8: '+path);
console.log('PASS F2 UI: backend tuning/metrics wiring, no local applied state, linked range retained, UTF-8 labels');

// F4: shared tunnels are panel resources (K4), not a browser pool.
const canvasPage = read('modules/proxy/pages/RelayCanvasPage.vue');
const canvasStore = read('modules/proxy/proxyNodes.ts');
const tunnelLibrary = read('modules/proxy/components/relay/useCanvasTunnels.ts');
const proxyApi = read('modules/proxy/api.ts');
assert.equal((canvasPage.match(/qui_shared_tunnels_v1/g) || []).length, 1, 'the old local tunnel key may appear once, only for its removal');
assert.match(canvasPage, /localStorage\.removeItem\(LEGACY_SHARED_TUNNELS_KEY\)/, 'the old local tunnel pool must be removed once');
assert.doesNotMatch(canvasPage, /localStorage\.(getItem|setItem)\(LEGACY_SHARED_TUNNELS_KEY/, 'shared tunnels must never be read from or written to browser storage');
assert.doesNotMatch(canvasPage, /sharedTunnelPool|syncTunnelsFromScheme|applySharedTunnelToScheme|filteredSharedTunnels/, 'the browser-only tunnel pool must be gone');
assert.doesNotMatch(canvasStore, /qui_shared_tunnels_v1|sharedTunnelPool/, 'the canvas store must not keep tunnels locally');
for (const call of ['list(', 'delete(', 'legacyObjects(', 'adopt(']) {
  assert.ok(tunnelLibrary.includes('canvasTunnelAPI.' + call), 'the tunnel library must call canvasTunnelAPI.' + call);
}
assert.match(proxyApi, /'\/canvas-tunnels'/, 'canvas tunnel CRUD paths must come from the K4 API');
assert.match(proxyApi, /\/canvas-tunnels\/\$\{encodeURIComponent\(id\)\}\/adopt/, 'adoption must call the K4 adopt route');
assert.match(canvasPage, /canvasTunnelAPI\.(create|update)\(/, 'resource writes must go through canvasTunnelAPI');
assert.match(canvasPage, /tunnelId/, 'scheme connections must carry the tunnel reference');
assert.match(canvasPage, /v-if="canManageTunnels"/, 'tunnel library and legacy panel are Owner-only');
assert.match(canvasPage, /:draggable="currentScheme\.executionLocation === 'server'"/, 'client schemes must not accept shared tunnels');
assert.match(canvasPage, /客户端方案不支持共享隧道/, 'client schemes keep inline connections and say so');
assert.match(canvasPage, /planBlocked/, 'publish must be blocked while the plan reports conflicts');
assert.match(canvasPage, /planBlocked\)/, 'the publish button must use the blocked state');
assert.equal((canvasPage.match(/<TunnelPlanSummary/g) || []).length, 2, 'the plan summary is shown in the check dialog and in the publish dialog');
assert.match(read('modules/proxy/components/relay/TunnelEditorDrawer.vue'), /<TunnelPlanSummary/, 'the tunnel drawer keeps its plan summary');
assert.match(read('modules/proxy/components/relay/TunnelEditorDrawer.vue'), /saveResource/, 'shared tunnel edits are written through the K4 resource API');

console.log('PASS F4 UI: shared tunnels read from GET /canvas-tunnels, reference-only connections, Owner-only adoption, publish blocked on conflicts');

// F3: multi-core wiring (K3b panel + K3c installer flags). The page reads the
// per-core capability data, keeps no core state in the browser, never ships a
// digest of its own, and uses the real add/remove/upgrade APIs.
const versionPanel = read('modules/system/components/NodeVersionPanel.vue');
assert.match(versionPanel, /nodes\/coreSummary/, 'the cores page uses the shared per-core view');
assert.match(versionPanel, /capabilities\.cores|nodeCores\(/, 'installed cores come from capabilities.cores (old Agents: one core)');
assert.match(read('modules/nodes/coreSummary.ts'), /heartbeat\?\.cores/, 'per-core health comes from the heartbeat');
assert.doesNotMatch(versionPanel, /localStorage[^\n]*[Cc]ore/, 'cores must never be read from or written to browser storage');
assert.doesNotMatch(versionPanel, /\b[a-f0-9]{64}\b/i, 'the Owner pastes the SHA-256; the panel ships no digest literal');
assert.match(versionPanel, /coreUpdatesAPI\.queue\(/, '添加/升级内核 uses POST /core-updates');
assert.match(versionPanel, /coreUpdatesAPI\.remove\(/, '删除内核 uses the real remove-core API');
assert.match(versionPanel, /coreUpdatesAPI\.removals\(/, 'removal tasks come from the removal history endpoint');
assert.match(versionPanel, /confirmNodeId|coreUpdatesAPI\.remove/, 'the remove request confirms the node id through the API wrapper');
assert.match(versionPanel, /core_in_use/, 'core_in_use is explained with its inbound list');
assert.match(versionPanel, /last_core/, 'last_core is explained');
assert.match(versionPanel, /revision_mismatch/, 'revision_mismatch is explained');
assert.match(versionPanel, /移除中/, 'a component stays listed as 移除中 until capabilities confirm the removal');
assert.match(versionPanel, /multiCoreSupported\(/, '添加内核 is offered only for Agents that negotiated multi-core support');
assert.match(versionPanel, /coreBatchTargets/, 'batch upgrades are per component');
assert.match(versionPanel, /upgradeAllComponent/, 'a per-component batch entry exists');
assert.match(versionPanel, /result\?\.code|result\?\.installed_version|result\?\.code/, 'task results are shown from the backend, never assumed');
assert.match(versionPanel, /coreBatchComponentLocked/, 'the per-component batch keeps its component fixed');
console.log('PASS F3 CORES: per-core data, no local core state, real add/upgrade/remove APIs, refusals explained, per-component batch');

// The install command emits one repeatable --core flag per selected core (K3c)
// and keeps every other part of the command unchanged.
const nodesApiSource = read('modules/nodes/api.ts').replace(/^import .*;$/gm, '');
const nodesApiCode = ts.transpileModule(nodesApiSource, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const nodesApi = await import(`data:text/javascript;base64,${Buffer.from(nodesApiCode).toString('base64')}`);
const install = { version: '1.2.0', host: 'panel.example.com', httpPort: 443, grpcPort: 9443, caSha256: 'abcd', token: 'tok' };
const defaultCommand = nodesApi.installCommand(install);
assert.match(defaultCommand, / --core xray$/, 'the default selection stays xray');
assert.equal((defaultCommand.match(/--core /g) || []).length, 1);
assert.match(defaultCommand, /--version 1\.2\.0 --panel panel\.example\.com --grpc-port 9443 --ca-sha256 abcd --token tok/, 'the rest of the command is unchanged');
assert.doesNotMatch(defaultCommand, /--http-port/, '--http-port is still omitted for 443');
assert.match(nodesApi.installCommand({ ...install, cores: ['xray', 'sing-box'] }), /--core xray --core sing-box$/, 'one --core flag per selected core');
assert.match(nodesApi.installCommand({ ...install, cores: ['sing-box', 'sing-box'] }), /--core sing-box$/, 'the selection is deduplicated');
assert.match(nodesApi.installCommand({ ...install, httpPort: 8443 }), /--http-port 8443/, 'a non-443 panel port is still passed');
assert.throws(() => nodesApi.installCommand({ ...install, cores: [] }), /内核/, 'an empty selection is refused');
assert.throws(() => nodesApi.installCommand({ ...install, cores: ['gost'] }), /内核/, 'an unknown core name is refused');
console.log('PASS F3 INSTALL: repeated --core flags, xray default, deduplicated allowlist, rest of the command unchanged');

// The editor's local port check follows the Agent's node-wide numeric rule: a TCP
// and a UDP inbound can NOT share a port number (an earlier change allowed it).
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
const editorModelUrl = new URL('../src/modules/proxy/components/node-editor/nodeEditorModel.ts', import.meta.url);
const editorModel = await import(await tsModuleUrl(editorModelUrl));
const tcpRow = [{ id: 'tcp', nodeId: 'n1', port: 443, name: 'VLESS-TCP', protocol: 'vless', settings: {} }];
for (const protocol of ['hysteria2', 'tuic', 'wireguard', 'socks5', 'shadowsocks']) {
  assert.match(editorModel.portConflict({ nodeId: 'n1', protocol, settings: {}, port: 443 }, tcpRow), /占用/, protocol + ' must not reuse an occupied port number');
}
assert.equal(editorModel.portConflict({ nodeId: 'n1', protocol: 'hysteria2', settings: {}, port: 8443 }, tcpRow), '');
assert.match(read('modules/proxy/components/node-editor/nodeEditorModel.ts'), /node-wide numeric rule/i, 'the rule is documented at its single source of truth');
console.log('PASS F3 PORTS: TCP and UDP cannot share a port number (node-wide numeric rule)');
