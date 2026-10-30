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

// Walkthrough A: run the actual Vue helpers/computeds against focused fixtures.
function vueSubset(path, names, context = {}) {
  const script = read(path).split('<script setup lang="ts">')[1].split('</script>')[0];
  const ast = ts.createSourceFile(path, script, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
  const statements = ast.statements.filter(s => ts.isFunctionDeclaration(s) ? names.includes(s.name?.text) :
    ts.isVariableStatement(s) && s.declarationList.declarations.some(d => names.includes(d.name.getText(ast))));
  assert.equal(statements.length, names.length, 'each requested UI helper must exist');
  const source = statements.map(s => s.getText(ast)).join('\n') + '\nreturn {' + names.join(',') + '};';
  const js = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.None } }).outputText;
  return new Function('computed', ...Object.keys(context), js)(fn => ({ get value() { return fn(); } }), ...Object.values(context));
}
const userA = { status: 'active', expiresAt: '', trafficLimit: 100, usedTraffic: 1 };
const disabledA = { ...userA, status: 'disabled', expiresAt: new Date(Date.now() + 120 * 86400000).toISOString() };
const expiredA = { ...userA, expiresAt: '2000-01-01T00:00:00Z' };
const quotaA = { ...userA, usedTraffic: 100 };
const soonA = { ...userA, expiresAt: new Date(Date.now() + 3 * 86400000).toISOString() };
const onlineA = { id: 'online', name: '在线节点', status: 'online', enabled: true, heartbeat: { core_healthy: true } };
const pendingA = { ...onlineA, id: 'pending', name: '新服务器', status: 'pending', heartbeat: {} };
const revisionsA = [
  { id: 9, nodeId: 'recovered', status: 'success' }, { id: 2, nodeId: 'recovered', status: 'failed' },
  { id: 10, nodeId: 'online', status: 'failed' }, { id: 3, nodeId: 'online', status: 'failed' },
  { id: 8, nodeId: 'named', status: 'failed' }, { id: 11, nodeId: 'online', status: 'draft' },
  { id: 7, nodeId: 'long-id-without-name', resultCode: 'error' }
];
const dashboardA = vueSubset('modules/dashboard/pages/DashboardPage.vue', [
  'isNodeOnline', 'nodeStatusLabel', 'isUserActive', 'getUserExpiryLabel', 'getUserExpiryBadgeClass',
  'activeUsersCount', 'warningUsersCount', 'adminOnlineNodesCount', 'healthyNodesCount', 'latestFailedRevisions', 'pendingIssues', 'formatGiB'
], {
  allUsers: { value: [userA, disabledA, expiredA, quotaA, soonA, { ...disabledA, usedTraffic: 100 }] },
  allNodes: { value: [onlineA, pendingA, { ...onlineA, id: 'unknown', heartbeat: {} }] },
  allRevisions: { value: revisionsA }, dashboard: { value: { nodeNames: { named: '名称映射' } } }, unhealthyCoreLabel: () => ''
});
assert.equal(dashboardA.getUserExpiryLabel(disabledA), '已停用');
assert.equal(dashboardA.getUserExpiryLabel({ ...disabledA, usedTraffic: 100, expiresAt: expiredA.expiresAt }), '已停用');
assert.match(dashboardA.getUserExpiryBadgeClass(disabledA), /text-ios-secondary|rose/);
assert.equal(dashboardA.getUserExpiryLabel(expiredA), '已过期');
assert.equal(dashboardA.getUserExpiryLabel({ ...expiredA, usedTraffic: 100 }), '已过期');
assert.equal(dashboardA.getUserExpiryLabel(quotaA), '已超额');
assert.equal(dashboardA.activeUsersCount.value, 2);
assert.equal(dashboardA.warningUsersCount.value, 3, 'active warnings count once and exclude disabled accounts');
assert.equal(dashboardA.isNodeOnline(pendingA), false);
assert.equal(dashboardA.nodeStatusLabel(pendingA), '待登记');
assert.equal(dashboardA.isNodeOnline({ ...onlineA, status: 'degraded' }), true);
for (const overrides of [{ enabled: false }, { deletedAt: '2026-01-01' }, { maintenance: true }, { status: 'offline' }]) assert.equal(dashboardA.isNodeOnline({ ...onlineA, ...overrides }), false);
assert.equal(dashboardA.adminOnlineNodesCount.value, 2);
assert.equal(dashboardA.healthyNodesCount.value, 1, 'missing health is unknown');
assert.ok(dashboardA.pendingIssues.value.some(i => i.title === '节点待登记: 新服务器'));
assert.deepEqual(dashboardA.latestFailedRevisions(revisionsA).map(r => r.id), [10, 8, 7]);
const failedA = dashboardA.pendingIssues.value.filter(i => i.title.startsWith('配置发布失败'));
assert.deepEqual(failedA.map(i => i.tag), ['在线节点', '名称映射', '节点 #long-id-']);
assert.equal(failedA[0].link, '/nodes/online?tab=revisions');
assert.ok(!failedA.some(i => i.link.includes('recovered')));
console.log('PASS walkthrough A dashboard: disabled/expired/quota users, pending/degraded nodes, measured health, latest results and node links');

for (const path of ['modules/nodes/components/ServerMetricsGrid.vue', 'modules/nodes/components/detail/ServerTuning.vue', 'portal/PortalPage.vue']) assert.doesNotMatch(read(path), /\.toLocaleString\s*\(/, path + ' uses explicit Chinese time formatting');
assert.match(read('modules/nodes/components/ServerMetricsGrid.vue'), /formatTime\(/);
assert.match(read('modules/nodes/components/detail/ServerTuning.vue'), /formatTime\(/);
assert.match(read('portal/PortalPage.vue'), /Intl\.DateTimeFormat\('zh-CN'/);
globalThis.walkthroughAuth = { timezone: 'Asia/Shanghai' };
const hostCodeA = ts.transpileModule(read('modules/nodes/metrics.ts'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
globalThis.walkthroughHostMetrics = (await import('data:text/javascript;base64,' + Buffer.from(hostCodeA).toString('base64'))).hostMetrics;
const metricsSourceA = read('modules/nodes/components/serverMetricsModel.ts')
  .replace("import { authState } from '../../../app/auth';", 'const authState = globalThis.walkthroughAuth;')
  .replace("import { hostMetrics } from '../metrics';", 'const hostMetrics = globalThis.walkthroughHostMetrics;');
const metricsCodeA = ts.transpileModule(metricsSourceA, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const metricsA = await import('data:text/javascript;base64,' + Buffer.from(metricsCodeA).toString('base64'));
const midnightA = Date.parse('2026-10-29T16:05:09Z');
assert.equal(metricsA.formatTimeHMS(midnightA), '00:05:09');
assert.equal(metricsA.formatTimeHM(midnightA), '00:05');
assert.equal(metricsA.formatTimeMD(midnightA), '10/30');
globalThis.walkthroughAuth.timezone = 'America/Los_Angeles';
assert.equal(metricsA.formatTimeHMS(midnightA), '09:05:09');
assert.equal(metricsA.formatTimeMD(midnightA), '10/29');
console.log('PASS walkthrough A time: zh-CN, panel timezone and midnight rollover; portal uses browser timezone');

const gridPathA = 'modules/nodes/components/ServerMetricsGrid.vue';
const metricNowA = Date.now();
const metricPropsA = { node: { status: 'online', lastHeartbeat: new Date(metricNowA).toISOString(), heartbeat: { cpu_percent: 12 } }, now: metricNowA };
const metricStateA = vueSubset(gridPathA, ['currentUnavailable', 'current'], { props: metricPropsA, extractCurrentValues: metricsA.extractCurrentValues });
assert.equal(metricStateA.currentUnavailable.value, false);
assert.equal(metricStateA.current.value.cpu, 12);
metricPropsA.node.status = 'offline';
assert.equal(metricStateA.currentUnavailable.value, true);
assert.equal(metricStateA.current.value.cpu, null, 'fresh but disconnected samples are not current values');
metricPropsA.node.status = 'online'; metricPropsA.now += 45000;
assert.equal(metricStateA.currentUnavailable.value, true, 'stale heartbeat is unavailable even with history');
assert.equal(metricStateA.current.value.cpu, null);
assert.match(read(gridPathA), /currentUnavailable \? '服务器离线，当前值不可用' : '需要新版 Agent 上报'/);
assert.doesNotMatch(read(gridPathA), /需要 Agent 1\.2/);
const connectionCardA = read('modules/users/pages/UserDetailPage.vue').split('<!-- 连接控制 -->')[1].split('<!--')[0];
assert.doesNotMatch(connectionCardA, /formatResetCycle/);
const deviceBadgeA = connectionCardA.match(/\{\{ (user\.deviceLimit === 0[^\n]+) \}\}/)[1];
const badgeA = new Function('user', 'return ' + deviceBadgeA);
assert.equal(badgeA({ deviceLimit: 0 }), '不限设备');
assert.equal(badgeA({ deviceLimit: 3 }), '最多 3 个地址');
const { readdirSync } = await import('node:fs');
function checkGroupNamesA(dir) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const url = new URL(entry.name + (entry.isDirectory() ? '/' : ''), dir);
    if (entry.isDirectory()) checkGroupNamesA(url);
    else assert.ok(!readFileSync(url, 'utf8').includes('线路组'), 'old group label in ' + url.pathname);
  }
}
checkGroupNamesA(new URL('../src/', import.meta.url));
console.log('PASS walkthrough A metrics/users/naming: offline vs absent fields, device limit badge, no old group label in web/src');

const coreUpdatesA = read('modules/system/components/CoreUpdatesPanel.vue');
assert.match(read('modules/nodes/components/detail/ServerSettings.vue'), /:fixed-node-id="node.id"/);
assert.match(coreUpdatesA, /v-if="!fixedNodeId"/);
assert.match(coreUpdatesA, /nodeId = ref\(props.fixedNodeId \|\| ''\)/);
assert.doesNotMatch(read('modules/nodes/components/NodeRestartPanel.vue'), /授权账本|未知消费保护|剩余额度重新编译/);
const versionPathA = 'modules/system/components/NodeVersionPanel.vue';
const versionsA = vueSubset(versionPathA, ['detectArch', 'upgradeUnavailableReason']);
assert.equal(versionsA.detectArch({ capabilities: { host: { architecture: 'arm64' }, architecture: 'amd64' } }), 'arm64');
assert.equal(versionsA.detectArch({ capabilities: { architecture: 'amd64' } }), 'amd64');
assert.equal(versionsA.detectArch({ capabilities: {} }), '');
assert.match(versionsA.upgradeUnavailableReason(pendingA), /登记/);
assert.match(versionsA.upgradeUnavailableReason({ ...onlineA, agentVersion: '1.0.9', lastHeartbeat: '' }), /心跳/);
assert.equal((read(versionPathA).match(/:disabled="!!upgradeUnavailableReason\(node\)"/g) || []).length, 4, 'table and card upgrade actions are disabled before enrollment');
const tasksA = read('modules/system/pages/TaskRecordPage.vue');
assert.ok(tasksA.indexOf('Agent 签名发布目录') > tasksA.indexOf('共 <span'), 'release directory follows the header and filters');
assert.match(read('layouts/MainLayout.vue'), /pt-16 lg:pt-0/);
assert.match(read('layouts/MainLayout.vue'), /pt-16 lg:pt-8/);
console.log('PASS walkthrough A shell/settings/versions/tasks: mobile clearance, fixed core target, restart wording, enrollment guard, K2 architecture and header order');
