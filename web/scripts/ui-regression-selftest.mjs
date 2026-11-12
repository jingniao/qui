import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
const read = path => readFileSync(new URL('../src/' + path, import.meta.url), 'utf8');
assert.match(read('app/router.ts'), /router\.beforeEach\(setupNavigationGuards\)/, 'navigation guard must be registered');
assert.doesNotMatch(read('modules/nodes/components/serverMetricsModel.ts'), /Math\.sin|431\s*\*\s*1024|1021\s*\*\s*1024|processes\s*[:=]\s*111|111\s*processes/, 'metrics must never use fabricated samples');
const meta = read('modules/nodes/nodeMeta.ts');
const saveMeta = meta.slice(meta.indexOf('export async function saveNodeMeta'));
assert.doesNotMatch(saveMeta, /\.\.\.meta\b/, 'metadata request must whitelist API fields');
// The panel never invents a digest: the vendor's published checksum is either
// read by GET /api/v1/core-releases (Xray .dgst, GitHub asset digest, our
// SHA256SUMS, the reviewed GOST pin served by the backend) or pasted by the Owner.
// No digest literal ships in the UI; the GOST pin may still appear verbatim.
const reviewedPin = '162feb43b1c284dbea599cdfe898169931af875233060dedc19432c7a9d18274';
const digestsOf = text => (text.match(/\b[a-f0-9]{64}\b/gi) || []).map(digest => digest.toLowerCase());
for (const digest of digestsOf(read('modules/system/components/NodeVersionPanel.vue'))) {
  assert.equal(digest, reviewedPin, 'core hashes must be supplied by Owner');
}
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
assert.match(metricF2,/nodeAPI\.metrics\(/); assert.match(metricF2,/60000/); assert.doesNotMatch(metricF2,/proxy-node-status/); // heartbeat events no longer trigger reads (v1.5.2 item 1)
assert.doesNotMatch(metricF2,/history\.value|持久化历史即将支持/);
assert.match(read('modules/nodes/components/detail/ServerMonitoring.vue'),/v-model:hours="monitoringHours"/);
for (const path of ['modules/nodes/components/detail/ServerTuning.vue','modules/nodes/components/detail/serverTuningModel.ts','modules/nodes/components/ServerMetricsGrid.vue','modules/nodes/components/hostObservationModel.ts']) assert.doesNotMatch(read(path), /\?{3,}/, 'F2 Chinese labels must survive UTF-8: '+path);
console.log('PASS F2 UI: backend tuning/metrics wiring, no local applied state, linked range retained, UTF-8 labels');

// U1b keeps canvas plans and legacy references while removing the shared library.
const canvasPage = read('modules/proxy/pages/RelayCanvasPage.vue');
const canvasStore = read('modules/proxy/proxyNodes.ts');
const proxyApi = read('modules/proxy/api.ts');
assert.doesNotMatch(canvasPage, /useCanvasTunnels|TunnelEditorDrawer|canManageTunnels|legacyChoice/);
assert.doesNotMatch(canvasStore, /qui_shared_tunnels_v1|sharedTunnelPool/);
assert.match(canvasPage, /tunnelId/);
assert.match(canvasPage, /planBlocked/);
assert.match(canvasPage, /planBlocked\)/);
assert.equal((canvasPage.match(/<TunnelPlanSummary/g) || []).length, 2);
assert.match(canvasPage, /将迁移到隧道管理/);
assert.match(read('modules/proxy/components/relay/CanvasConnectionDialog.vue'), /删除连线/);
console.log('PASS U1b canvas: library removed, legacy display, references and plan summaries retained');

// F3: multi-core wiring (K3b panel + K3c installer flags). The page reads the
// per-core capability data, keeps no core state in the browser, never ships a
// digest of its own, and uses the real add/remove/upgrade APIs.
const versionPanel = read('modules/system/components/NodeVersionPanel.vue');
assert.match(versionPanel, /nodes\/coreSummary/, 'the cores page uses the shared per-core view');
assert.match(versionPanel, /capabilities\.cores|nodeCores\(/, 'installed cores come from capabilities.cores (old Agents: one core)');
assert.match(read('modules/nodes/coreSummary.ts'), /heartbeat\?\.cores/, 'per-core health comes from the heartbeat');
assert.doesNotMatch(versionPanel, /localStorage[^\n]*[Cc]ore/, 'cores must never be read from or written to browser storage');
assert.deepEqual(digestsOf(versionPanel).filter(digest => digest !== reviewedPin), [], 'digests come from the release lookup or the Owner; only the reviewed GOST pin may ship as a literal');
// Official release lookup: both core forms pick a release instead of hard-coded
// presets, and the helper resolves each server's own archive and never a digest
// literal of its own.
const coreReleasesHelper = read('modules/system/coreReleases.ts');
assert.ok(coreReleasesHelper.includes('/core-releases?component='), 'the release lookup uses GET /api/v1/core-releases');
assert.deepEqual(digestsOf(coreReleasesHelper), [], 'the release helper carries no digest literal');
assert.ok(coreReleasesHelper.includes('export function pickAsset('), 'each server gets the archive for its architecture and C library');
assert.ok(coreReleasesHelper.includes('export function deriveAssetURL('), 'a hand-typed version rebuilds the URL in one shared place');
for (const file of ['modules/system/components/NodeVersionPanel.vue', 'modules/system/components/CoreUpdatesPanel.vue']) {
  const source = read(file);
  assert.ok(source.includes('coreReleasesAPI.list('), file + ' reads the official releases');
  assert.ok(source.includes('pickAsset('), file + ' fills the asset of the target server');
  assert.ok(source.includes('deriveAssetURL('), file + ' clears or rebuilds the URL when the version is typed by hand');
  assert.doesNotMatch(source, /摘要已自动填写，来自/, file + ' names the digest source instead of running it into the sentence');
  assert.doesNotMatch(source, /CORE_PRESETS|摘要不会自动填充/, file + ' no longer ships hard-coded presets or the old paste-only wording');
  assert.deepEqual(digestsOf(source).filter(digest => digest !== reviewedPin), [], file + ' ships no digest literal');
}
// pickAsset chooses the archive for the server's architecture and C library and
// never falls back to an archive built for another one; the qui build is offered
// to the Agents the backend accepts it for.
{
  const helperCode = ts.transpileModule(('const request = () => { throw new Error("no network"); };\n' + coreReleasesHelper.replace(/^import .*;$/gm, '')), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
  const helper = await import(`data:text/javascript;base64,${Buffer.from(helperCode).toString('base64')}`);
  const asset = (platform, libc, name) => ({ platform, libc, url: 'https://github.com/x/' + name, sha256: 'a'.repeat(64), digestSource: 'github' });
  const singbox = { version: '1.14.4', publishedAt: '', assets: [asset('linux/amd64', '', 'glibc-amd64'), asset('linux/arm64', '', 'glibc-arm64'), asset('linux/amd64', 'musl', 'musl-amd64')] };
  const xray = { version: '26.3.27', publishedAt: '', assets: [asset('linux/amd64', '', 'xray-amd64'), asset('linux/arm64', '', 'xray-arm64')] };
  const node = (architecture, osPrettyName, extra = {}) => ({ capabilities: { host: { architecture, osPrettyName }, ...extra } });
  assert.equal(helper.pickAsset(singbox, node('amd64', 'Debian GNU/Linux 12'))?.url.endsWith('glibc-amd64'), true, 'glibc amd64');
  assert.equal(helper.pickAsset(singbox, node('amd64', 'Alpine Linux v3.19'))?.url.endsWith('musl-amd64'), true, 'Alpine gets the musl build');
  assert.equal(helper.pickAsset(singbox, node('arm64', 'Alpine Linux v3.19')), null, 'no arm64 musl build: no guess');
  assert.equal(helper.pickAsset(singbox, node('', 'Debian')), null, 'unknown architecture: no guess');
  assert.equal(helper.pickAsset(xray, node('amd64', 'Alpine Linux v3.19'))?.url.endsWith('xray-amd64'), true, 'Xray is one static build for every libc');
  assert.equal(helper.pickAsset(xray, node('arm64', 'Ubuntu'))?.url.endsWith('xray-arm64'), true, 'arm64');
  assert.equal(helper.defaultCoreSource('sing-box', node('amd64', 'Debian', { multi_core_supported: true })), 'qui', 'adding sing-box defaults to the qui build where accepted');
  assert.equal(helper.defaultCoreSource('sing-box', node('amd64', 'Debian', { multi_core_supported: true, cores: [{ name: 'sing-box', singbox_user_stats: false }] })), 'official', 'an official sing-box keeps the official build');
  assert.equal(helper.defaultCoreSource('sing-box', node('amd64', 'Debian')), 'official', 'an Agent without qui support stays official');
  assert.equal(helper.defaultCoreSource('xray', node('amd64', 'Debian', { multi_core_supported: true })), 'official');
  // A version typed by hand never keeps the URL of the release picked before it:
  // it is rebuilt where the official naming rule allows it and cleared where it
  // does not (qui lives under a panel tag, GOST is one pin, unknown arch has no
  // asset), so a stale URL and digest cannot be submitted as a pair.
  assert.equal(helper.deriveAssetURL('xray', 'official', '26.11.1', node('amd64', 'Debian')), 'https://github.com/XTLS/Xray-core/releases/download/v26.11.1/Xray-linux-64.zip', 'xray amd64 is rebuilt');
  assert.equal(helper.deriveAssetURL('xray', 'official', '26.11.1', node('arm64', 'Debian')).endsWith('/v26.11.1/Xray-linux-arm64-v8a.zip'), true, 'xray arm64 is rebuilt');
  assert.equal(helper.deriveAssetURL('sing-box', 'official', '1.15.0', node('amd64', 'Alpine Linux v3.19')).endsWith('/sing-box-1.15.0-linux-amd64-musl.tar.gz'), true, 'Alpine keeps the musl name');
  assert.equal(helper.deriveAssetURL('sing-box', 'qui', '1.15.0', node('amd64', 'Debian')), '', 'the qui build sits under a panel tag: no derived URL');
  assert.equal(helper.deriveAssetURL('gost', 'official', '3.3.2', node('amd64', 'Debian')), '', 'GOST is one pinned archive: no derived URL');
  assert.equal(helper.deriveAssetURL('xray', 'official', '26.11.1', node('', 'Debian')), '', 'unknown architecture: no derived URL');
  assert.equal(helper.deriveAssetURL('xray', 'official', '  ', node('amd64', 'Debian')), '', 'an empty version derives nothing');
  console.log('PASS core releases: per-server archive by architecture and libc, no cross-platform guess, source defaults, typed-version URL rule');
}
// A batch upgrade resolves each server's own archive unless the form was edited.
assert.ok(versionPanel.includes('const perNode = !releaseFormManual(corePicker, coreForm)'), 'a picked release resolves the archive per server in a batch');
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

// The install command emits one repeatable --core flag per selected core (K3c) and
// always passes both ports explicitly, so the command never depends on
// node-install.sh's own defaults (22550 / 18880). --grpc-host appears only when
// the Agent gRPC address differs from the panel address, which the installer
// otherwise reuses.
const nodesApiSource = read('modules/nodes/api.ts').replace(/^import .*;$/gm, '');
const nodesApiCode = ts.transpileModule(nodesApiSource, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const nodesApi = await import(`data:text/javascript;base64,${Buffer.from(nodesApiCode).toString('base64')}`);
const install = { version: '1.2.0', host: 'panel.example.com', httpPort: 22550, grpcPort: 18880, caSha256: 'abcd', token: 'tok' };
const defaultCommand = nodesApi.installCommand(install);
assert.match(defaultCommand, / --core xray$/, 'the default selection stays xray');
assert.equal((defaultCommand.match(/--core /g) || []).length, 1);
assert.match(defaultCommand, /--version 1\.2\.0 --panel panel\.example\.com --http-port 22550 --grpc-port 18880 --ca-sha256 abcd --token tok/, 'both ports are always passed explicitly');
assert.match(nodesApi.installCommand({ ...install, httpPort: 443 }), /--http-port 443/, '443 is passed too: the command never relies on the installer default');
assert.match(nodesApi.installCommand({ ...install, cores: ['xray', 'sing-box'] }), /--core xray --core sing-box$/, 'one --core flag per selected core');
assert.match(nodesApi.installCommand({ ...install, cores: ['sing-box', 'sing-box'] }), /--core sing-box$/, 'the selection is deduplicated');
assert.match(nodesApi.installCommand({ ...install, httpPort: 8443 }), /--http-port 8443/, 'a custom panel port is passed');
assert.throws(() => nodesApi.installCommand({ ...install, cores: [] }), /内核/, 'an empty selection is refused');
assert.match(nodesApi.installCommand({ ...install, cores: ['xray', 'gost'] }), /--core xray --core gost$/, 'GOST joined the installer cores in v1.3');
assert.throws(() => nodesApi.installCommand({ ...install, cores: ['v2ray'] }), /内核/, 'an unknown core name is refused');
assert.match(nodesApi.installCommand({ ...install, grpcHost: 'grpc.example.com' }), /--panel panel\.example\.com --grpc-host grpc\.example\.com --http-port 22550 --grpc-port 18880/, 'a separate gRPC address is passed as --grpc-host');
assert.doesNotMatch(nodesApi.installCommand({ ...install, grpcHost: 'panel.example.com' }), /--grpc-host/, 'the installer already defaults --grpc-host to --panel, so an equal value is omitted');
assert.doesNotMatch(nodesApi.installCommand({ ...install, grpcHost: '' }), /--grpc-host/, 'an unset gRPC address keeps the panel address');
// Web upgrade is opt-in: the flag is emitted only when asked, and each dialog starts
// unchecked, resets on every new token and passes the choice into the command.
assert.doesNotMatch(defaultCommand, /--web-upgrade/, 'web upgrade is not enabled by default');
assert.doesNotMatch(nodesApi.installCommand({ ...install, webUpgrade: false }), /--web-upgrade/, 'an unchecked box adds no flag');
assert.match(nodesApi.installCommand({ ...install, webUpgrade: true }), / --core xray --web-upgrade$/, 'a checked box appends --web-upgrade');
const webUpgradeOption = read('modules/nodes/components/WebUpgradeOption.vue');
assert.match(webUpgradeOption, /<input v-model="enabled" type="checkbox"[^>]*\/>启用网页升级/, 'the shared option is the 启用网页升级 checkbox');
assert.match(webUpgradeOption, /class="mt-1 text-ios-tertiary"/, 'the helper line sits under the checkbox');
for (const [file, ref_] of [['modules/nodes/components/ServerEnrollment.vue', 'webUpgrade'], ['modules/nodes/components/NodeRecoveryPanel.vue', 'webUpgrade'], ['modules/system/components/NodeVersionPanel.vue', 'pendingWebUpgrade']]) {
  const source = read(file);
  assert.match(source, new RegExp(`<WebUpgradeOption [^>]*v-model="${ref_}"`), file + ' shows the web upgrade option');
  assert.match(source, new RegExp(`${ref_} = ref\\(false\\)`), file + ' starts unchecked');
  assert.match(source, new RegExp(`${ref_}\\.value = false;`), file + ' resets the choice for each new token');
  assert.match(source, new RegExp(`webUpgrade: ${ref_}\\.value`), file + ' passes the choice into installCommand');
}
console.log('PASS F3 INSTALL: repeated --core flags, xray default, deduplicated allowlist, both ports always explicit');

// The panel address of the install command is saved once in 系统设置 → 节点安装
// 连接地址 (system_settings key install_endpoint) instead of being edited per
// server; all three dialogs resolve it through one helper and fall back to the
// browser address when it is unset.
assert.match(read('modules/nodes/installEndpoint.ts'), /settingsAPI\.installEndpoint\(/, 'the install endpoint comes from the settings API');
assert.match(read('modules/nodes/installEndpoint.ts'), /window\.location\.hostname/, 'the fallback stays the browser address');
assert.match(read('modules/nodes/installEndpoint.ts'), /saved\.grpcHost/, 'the saved Agent gRPC address is resolved too');
// A ref stores a reactive proxy, so `enrollment.value !== item` is always true and
// silently discarded the saved install endpoint in the enrollment dialog.
assert.doesNotMatch(read('modules/nodes/components/ServerEnrollment.vue'), /enrollment\.value\s*!==\s*item/, 'the enrollment dialog must not compare the reactive ref with the raw enrollment');
for (const file of ['modules/nodes/components/ServerEnrollment.vue', 'modules/nodes/components/NodeRecoveryPanel.vue', 'modules/system/components/NodeVersionPanel.vue']) {
  const source = read(file);
  assert.match(source, /resolveInstallEndpoint\(/, file + ' must default to the saved install endpoint');
  assert.match(source, /grpcHost: /, file + ' must pass the resolved Agent gRPC address into the command');
  assert.doesNotMatch(source, /window\.location\.hostname/, file + ' must not read the browser address directly');
}
for (const file of ['modules/nodes/components/ServerEnrollment.vue', 'modules/nodes/components/NodeRecoveryPanel.vue']) {
  const source = read(file);
  assert.doesNotMatch(source, /v-model\.trim="host"/, file + ' must not offer a per-dialog panel address input');
  assert.doesNotMatch(source, /v-model\.number="port"/, file + ' must not offer a per-dialog panel port input');
  assert.doesNotMatch(source, /不写回设置/, file + ' must not describe one-off edits any more');
}
assert.match(read('modules/system/pages/SettingsPage.vue'), /saveInstallEndpoint\(/, '系统设置 saves the install endpoint');
assert.match(read('modules/system/pages/SettingsPage.vue'), /Agent gRPC 地址/, '系统设置 edits the Agent gRPC address');
assert.match(read('modules/system/pages/SettingsPage.vue'), /Agent gRPC 端口/, '系统设置 edits the Agent gRPC port');
console.log('PASS F3 ENDPOINT: one saved panel address feeds all three install dialogs, browser address only as fallback');

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
const addressHelperA = await import('data:text/javascript;base64,' + Buffer.from(ts.transpileModule(
  read('modules/nodes/nodeAddress.ts'),
  { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }
).outputText).toString('base64'));
const dashboardA = vueSubset('modules/dashboard/pages/DashboardPage.vue', [
  'isNodeOnline', 'nodeStatusLabel', 'isUserActive', 'getUserExpiryLabel', 'getUserExpiryBadgeClass',
  'activeUsersCount', 'warningUsersCount', 'adminOnlineNodesCount', 'healthyNodesCount', 'latestFailedRevisions', 'pendingIssues', 'formatGiB'
], {
  connectAddress: addressHelperA.connectAddress,
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
assert.match(read(gridPathA), /currentUnavailable \? '服务器离线，当前值不可用' : '暂无读数'/);
assert.ok(!read(gridPathA).includes('需要新版 Agent 上报'), 'misleading upgrade hint removed');
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
const versionLoading = { value: true };
let versionStatusReads = 0;
const versionStatus = vueSubset(versionPathA, ['nodeStatusChanged'], {
  loading: versionLoading, load: () => { versionStatusReads++; }
});
for (let i = 0; i < 1000; i++) versionStatus.nodeStatusChanged();
assert.equal(versionStatusReads, 0, 'SSE bursts must not supersede an in-flight version read');
versionLoading.value = false;
versionStatus.nodeStatusChanged();
assert.equal(versionStatusReads, 1, 'the next status event refreshes the idle version list');
const versionsA = vueSubset(versionPathA, ['detectArch', 'upgradeUnavailableReason']);
assert.equal(versionsA.detectArch({ capabilities: { host: { architecture: 'arm64' }, architecture: 'amd64' } }), 'arm64');
assert.equal(versionsA.detectArch({ capabilities: { architecture: 'amd64' } }), 'amd64');
assert.equal(versionsA.detectArch({ capabilities: {} }), '');
assert.match(versionsA.upgradeUnavailableReason(pendingA), /登记/);
assert.match(versionsA.upgradeUnavailableReason({ ...onlineA, agentVersion: '1.0.9', lastHeartbeat: '' }), /心跳/);
assert.equal((read(versionPathA).match(/v-if="(isOwner && )?!upgradeUnavailableReason\(node\)"/g) || []).length, 4, 'table and card upgrade actions are hidden before enrollment instead of disabled');
const tasksA = read('modules/system/pages/TaskRecordPage.vue');
assert.ok(tasksA.indexOf('Agent 签名发布目录') > tasksA.indexOf('共 <span'), 'release directory follows the header and filters');
assert.match(read('layouts/MainLayout.vue'), /pt-16 lg:pt-0/);
assert.match(read('layouts/MainLayout.vue'), /pt-16 lg:pt-8/);
console.log('PASS walkthrough A shell/settings/versions/tasks: mobile clearance, fixed core target, restart wording, enrollment guard, K2 architecture and header order');

// Follow-up review: exercise the actual upgrade guard with a fixed clock.
const upgradeNow = Date.now();
const liveUpgrade = { ...onlineA, agentVersion: '1.0.9', lastHeartbeat: new Date(upgradeNow).toISOString(), heartbeat: { current_revision: 7 } };
const reviewVersions = vueSubset(versionPathA, ['upgradeUnavailableReason'], { Date: { now: () => upgradeNow, parse: Date.parse } });
for (const status of ['online', 'degraded']) assert.equal(reviewVersions.upgradeUnavailableReason({ ...liveUpgrade, status }), '');
for (const patch of [
  { status: 'pending' }, { status: 'offline' }, { status: 'disabled' }, { status: 'applying' },
  { enabled: false }, { deletedAt: '2026-01-01' }, { agentVersion: '' }, { lastHeartbeat: '' },
  { lastHeartbeat: new Date(upgradeNow - 45000).toISOString() },
  { lastHeartbeat: new Date(upgradeNow + 5001).toISOString() }, { heartbeat: {} }
]) assert.ok(reviewVersions.upgradeUnavailableReason({ ...liveUpgrade, status: 'degraded', ...patch }), JSON.stringify(patch));
for (const offset of [-44999, 5000]) assert.equal(reviewVersions.upgradeUnavailableReason({ ...liveUpgrade, status: 'degraded', lastHeartbeat: new Date(upgradeNow + offset).toISOString() }), '');
console.log('PASS review 1: online/degraded upgrades, enrollment and heartbeat boundary guards');

const reviewNodeProps = { node: { capabilities: { core_name: 'xray', cores: [{ name: 'sing-box' }] } } };
const serverNodesPath = 'modules/nodes/components/detail/ServerNodes.vue';
// The badge text comes from the shared per-core view, so the run uses the real
// helper instead of a copy of the labels.
const coreSummary = await import(await tsModuleUrl(new URL('../src/modules/nodes/coreSummary.ts', import.meta.url)));
const reviewNodes = vueSubset(serverNodesPath, ['inboundCoreName'], { props: reviewNodeProps, isCoreName: coreSummary.isCoreName, coreLabel: coreSummary.coreLabel });
assert.equal(reviewNodes.inboundCoreName({ core: 'sing-box' }), 'sing-box');
assert.equal(reviewNodes.inboundCoreName({ core: 'xray' }), 'Xray');
assert.equal(reviewNodes.inboundCoreName({ core: 'gost' }), 'GOST');
assert.equal(reviewNodes.inboundCoreName({ core: '' }), 'Xray');
reviewNodeProps.node.capabilities.core_name = '';
assert.equal(reviewNodes.inboundCoreName({ core: '' }), 'sing-box');
reviewNodeProps.node.capabilities = {};
assert.equal(reviewNodes.inboundCoreName({ core: '' }), '');
assert.match(read(serverNodesPath), /<ProtocolBadge v-if="inboundCoreName\(item\)" :protocol="inboundCoreName\(item\)"/);
console.log('PASS review 2: inbound core badge, explicit core, primary/fallback core and unknown');

const previewReview = read('modules/users/pages/PortalPreviewPage.vue');
assert.equal((previewReview.match(/to="\/users"/g) || []).length, 1, 'toolbar keeps one return link');
assert.match(previewReview, /<PortalLayout[^>]*class="portal-preview-content" preview/);
assert.match(previewReview, /<style scoped>[\s\S]*\.portal-preview-content :deep\(header > a\[href="\/users"\]\)\s*\{\s*display: none;/);
assert.match(read('portal/PortalLayout.vue'), /<router-link v-if="preview" to="\/users"/);
assert.match(read('portal/PortalLayout.vue'), /<button v-else[^>]*@click="logout"/);
console.log('PASS review 3: preview-only duplicate header link hidden, toolbar and portal logout retained');

const clientActionsReview = read('modules/routing/pages/ClientPolicyPage.vue').split('<template #actions>')[1].split('</template>')[0];
assert.match(clientActionsReview, /新建配置/, 'header action provides new configuration trigger');
const clientPageFull = read('modules/routing/pages/ClientPolicyPage.vue');
assert.ok(clientPageFull.includes('saveAll'), 'save action retained in configuration dialog');
assert.ok(clientPageFull.includes('openCreateRuleDrawer'), 'rule creation retained in rules section');
console.log('PASS review 4: header action updated to new config, save and rule editor retained in dialog');

// Third round: execute the shared message mapping and API error path.
const apiReview3 = await import('data:text/javascript;base64,' + Buffer.from(ts.transpileModule(read('app/apiClient.ts'), {
  compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext }
}).outputText).toString('base64'));
assert.equal(apiReview3.translateAPIMessage('matched scope, address, port, network and active time window'), '已匹配生效范围、目标地址、端口、网络与生效时间');
assert.equal(apiReview3.translateAPIMessage('no matching policy; inbound default rule'), '未命中启用的策略，使用入站默认规则');
const matchContractReview3 = readFileSync(new URL('../../pkg/contracts/policy/match.go', import.meta.url), 'utf8');
for (const [, message] of matchContractReview3.matchAll(/errors.New\("([^"]+)"\)/g)) {
  assert.notEqual(apiReview3.translateAPIMessage(message), message, 'translate shared validation: ' + message);
}
for (const message of ['future backend reason', 'unknown validation message', '', 'toString']) assert.equal(apiReview3.translateAPIMessage(message), message);
const originalFetchReview3 = globalThis.fetch;
try {
  for (const [message, expected] of [['invalid domain', '域名无效'], ['invalid target IP', '目标 IP 无效'], ['future validation', 'future validation']]) {
    globalThis.fetch = async () => ({ ok: false, status: 400, json: async () => ({ error: { code: 'invalid_request', message } }) });
    await assert.rejects(apiReview3.request('/policies/simulate', 'POST', {}), error => error.message === expected);
  }
} finally { globalThis.fetch = originalFetchReview3; }
assert.match(read('modules/routing/pages/PolicySimulatorPage.vue'), /translateAPIMessage\(nodeDecision.reason/);
console.log('PASS third round 1: Chinese policy reasons/validation, API error path and unknown text fallback');

const detailReview3 = read('modules/nodes/pages/NodeDetailPage.vue');
const refreshSourceReview3 = detailReview3.slice(detailReview3.indexOf('const nodeRequests ='), detailReview3.indexOf('async function load()'));
const refreshCodeReview3 = ts.transpileModule(refreshSourceReview3 + '\nreturn { refreshNode, readNode, visibilityChanged, mount: () => mounted = true, unmount: () => mounted = false };', {
  compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.None }
}).outputText;
let resolveNodeReview3, nodeCallsReview3 = 0;
const documentReview3 = { visibilityState: 'visible' }, nodeRefReview3 = { value: { id: 'n', status: 'offline' } };
const refreshReview3 = new Function('nodeAPI', 'document', 'loading', 'route', 'generation', 'node', 'now', refreshCodeReview3)(
  { get: () => { nodeCallsReview3++; return new Promise(resolve => { resolveNodeReview3 = resolve; }); } },
  documentReview3, { value: false }, { name: 'NodeDetail', params: { id: 'n' } }, 1, nodeRefReview3, { value: 0 }
);
await refreshReview3.refreshNode();
assert.equal(nodeCallsReview3, 0, 'no reads before mount');
refreshReview3.mount();
documentReview3.visibilityState = 'hidden';
await refreshReview3.refreshNode();
assert.equal(nodeCallsReview3, 0, 'hidden pages do not poll');
documentReview3.visibilityState = 'visible';
const refreshPendingReview3 = refreshReview3.refreshNode();
await refreshReview3.refreshNode();
const sharedReadReview3 = refreshReview3.readNode('n');
assert.equal(nodeCallsReview3, 1, 'poll and full/SSE load share a pending read');
resolveNodeReview3({ id: 'n', status: 'online', lastHeartbeat: 'fresh' });
await Promise.all([refreshPendingReview3, sharedReadReview3]);
assert.equal(nodeRefReview3.value.status, 'online');
assert.equal(nodeRefReview3.value.lastHeartbeat, 'fresh');
refreshReview3.visibilityChanged();
assert.equal(nodeCallsReview3, 2, 'visible transition reads immediately');
refreshReview3.unmount();
resolveNodeReview3({ id: 'n', status: 'offline' });
await new Promise(resolve => setImmediate(resolve));
assert.equal(nodeRefReview3.value.status, 'online', 'unmounted view ignores pending result');
await refreshReview3.refreshNode();
assert.equal(nodeCallsReview3, 2);
assert.match(detailReview3, /setInterval\(refreshNode, 15000\)/);
assert.match(detailReview3, /addEventListener\('visibilitychange', visibilityChanged\)/);
assert.match(detailReview3, /clearInterval\(refreshTimer\)/);
assert.match(detailReview3, /removeEventListener\('visibilitychange', visibilityChanged\)/);
assert.match(detailReview3, /ServerSettings[^>]*:key="node.id"/);
assert.equal((detailReview3.match(/<ServerSettings\b/g) || []).length, 1, 'enrollment transitions keep the same settings instance');
const settingsReview3 = read('modules/nodes/components/detail/ServerSettings.vue');
assert.match(settingsReview3, /reactive<NodeSettingsInput>\(\{ \.\.\.props.node \}\)/);
assert.match(settingsReview3, /watch\(\(\) => props.node.id,/);
assert.doesNotMatch(settingsReview3, /watch\(\(\) => props.node\s*,/);
console.log('PASS third round 2: visible-only refresh, immediate resume, shared reads, unmount cleanup and independent settings edits');

const dashboardReview3 = read('modules/dashboard/pages/DashboardPage.vue');
const dashboardStatusReview3 = dashboardReview3.slice(dashboardReview3.indexOf('function isNodeOnline('), dashboardReview3.indexOf('const healthyNodesCount'));
const dashboardCodeReview3 = ts.transpileModule(dashboardStatusReview3 + '\nreturn { offlineNodesCount, pendingNodesCount, nodeStatusSubtitle };', {
  compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.None }
}).outputText;
const dashboardNodesReview3 = { value: [] };
const dashboardCountsReview3 = new Function('computed', 'allNodes', dashboardCodeReview3)(fn => ({ get value() { return fn(); } }), dashboardNodesReview3);
const dashboardNodeReview3 = (status, extra = {}) => ({ status, enabled: true, ...extra });
for (const [rows, subtitle, offline, pending] of [
  [[dashboardNodeReview3('offline'), dashboardNodeReview3('pending'), dashboardNodeReview3('pending')], '1 台离线 · 2 台待登记', 1, 2],
  [[dashboardNodeReview3('pending'), dashboardNodeReview3('pending')], '2 台待登记', 0, 2],
  [[dashboardNodeReview3('online'), dashboardNodeReview3('degraded')], '全部就绪', 0, 0],
  [[dashboardNodeReview3('offline')], '1 台离线', 1, 0],
  [[dashboardNodeReview3('offline', { enabled: false }), dashboardNodeReview3('online', { maintenance: true })], '1 台离线', 1, 0],
  [[], '全部就绪', 0, 0]
]) {
  dashboardNodesReview3.value = rows;
  assert.equal(dashboardCountsReview3.nodeStatusSubtitle.value, subtitle);
  assert.equal(dashboardCountsReview3.offlineNodesCount.value, offline);
  assert.equal(dashboardCountsReview3.pendingNodesCount.value, pending);
}
assert.match(dashboardReview3, /\{\{ nodeStatusSubtitle \}\}/);
console.log('PASS third round 3: offline/pending subtitles, ready/degraded, disabled and maintenance counts');

// ============================================================================
// Walkthrough E: desktop-width (1120 px) table/card squeezing and the logic bugs
// found in the same pass. Layout parts are source checks; the date, event,
// metrics, tuning and dashboard ordering parts run the real helpers.
// ============================================================================
function walkthroughEModule(path, replacements = []) {
  let source = read(path);
  for (const [from, to] of replacements) source = source.split(from).join(to);
  const js = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
  return import('data:text/javascript;base64,' + Buffer.from(js).toString('base64'));
}
globalThis.walkthroughAuth.timezone = 'Asia/Shanghai';

// (1) The last column of every DataTable is pinned to the right edge of the scroller.
const dataTableE = read('components/DataTable.vue');
assert.match(dataTableE, /:deep\(th:last-child\),\s*:deep\(td:last-child\)\s*\{[^}]*position: sticky;[^}]*right: 0;/, 'the last column is sticky on the right');
assert.match(dataTableE, /:deep\(td:last-child\)\s*\{\s*background-color: #ffffff;/, 'pinned body cells stay opaque');
assert.match(dataTableE, /:deep\(tbody tr:hover td:last-child\)/, 'the pinned cell follows the row hover');
assert.match(dataTableE, /dt-sticky-shadow/, 'the left shadow is toggleable while content scrolls under it');
assert.match(dataTableE, /@scroll\.passive="syncShadow"/);
assert.match(read('components/StatusBadge.vue'), /shrink-0 whitespace-nowrap/, 'badges never wrap one character per line');
console.log('PASS walkthrough E tables: sticky last column with opaque/hover surface and shadow, nowrap status badges');

// (4) Card grids: 2 columns at lg, 3 from xl, and the name keeps the available width.
assert.match(read('modules/nodes/components/ServerCardGrid.vue'), /grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-5/);
assert.match(read('modules/nodes/components/ServerCardGrid.vue'), /flex flex-wrap items-center gap-x-2 gap-y-1 min-w-0/);
assert.match(read('modules/system/components/NodeVersionPanel.vue'), /grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-5/);
// (10) An unavailable 升级 action is hidden, never a greyed-out button.
const versionPanelE = read('modules/system/components/NodeVersionPanel.vue');
assert.doesNotMatch(versionPanelE, /:disabled="!!upgradeUnavailableReason/, 'upgrade buttons are hidden rather than disabled');
assert.doesNotMatch(versionPanelE, /（已启用）|（不可用）/, 'unavailable core options are removed from the lists');
assert.match(versionPanelE, /v-for="name in addCoreOptions"/, 'only cores the server does not run yet are offered');
assert.match(versionPanelE, /v-if="!isCoreInstalled\('xray'\)"/);
// (5) The three 概况 panels use 2 columns below xl and readable values.
const systemPanelsE = read('modules/nodes/components/detail/ServerSystemPanels.vue');
assert.match(systemPanelsE, /grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3\.5 sm:gap-4 items-stretch/);
assert.match(systemPanelsE, /md:col-span-2 xl:col-span-1/);
assert.doesNotMatch(systemPanelsE, /tabular-nums truncate/, 'panel values wrap instead of being clipped');
// (12) Group members show the server (or import source) under the member name.
assert.match(read('modules/proxy/pages/LineGroupPage.vue'), /memberServerLabel\(node\)/);
// (14) Stale wording about a per-node ledger and device attribution.
assert.doesNotMatch(read('modules/system/pages/BackupPage.vue'), /节点账本/);
assert.match(read('modules/system/pages/BackupPage.vue'), /完整恢复后请核对用户额度与节点运行配置/);
assert.doesNotMatch(read('modules/access/pages/AccessRecordPage.vue'), /按凭据归属用户和设备/);
assert.match(read('modules/access/pages/AccessRecordPage.vue'), /按凭据归属用户/);
console.log('PASS walkthrough E layout: card columns, hidden unavailable actions, panel width, member server line, refreshed wording');


// (6) 最近事件 drops a code that repeats the status and translates known failures.
const detailE = await import(await tsModuleUrl(new URL('../src/modules/nodes/components/detail/serverDetailModel.ts', import.meta.url)));
assert.equal(detailE.eventResultLabel('success'), '', 'success only repeats 应用成功');
assert.equal(detailE.eventResultLabel(''), '');
assert.equal(detailE.eventResultLabel(undefined), '');
assert.equal(detailE.eventResultLabel('validation_failed'), '核心校验失败');
assert.equal(detailE.eventResultLabel('compile_failed'), '配置编译失败');
assert.equal(detailE.revisionEventText({ id: 6, status: 'success', summary: '手动发布当前配置', resultCode: 'success' }), '配置 #6 · 应用成功 · 手动发布当前配置');
assert.equal(detailE.revisionEventText({ id: 7, status: 'failed', summary: '手动发布当前配置', resultCode: 'validation_failed' }), '配置 #7 · 失败 · 手动发布当前配置 · 核心校验失败');
assert.equal(detailE.revisionEventText({ id: 8, status: 'failed', summary: '手动发布当前配置' }), '配置 #8 · 失败 · 手动发布当前配置');
const overviewE = read('modules/nodes/components/detail/ServerOverview.vue');
assert.match(overviewE, /revisionEventText\(rev\)/);
assert.doesNotMatch(overviewE, /rev\.resultCode \|\| ''/, 'raw English result codes are no longer appended');
console.log('PASS walkthrough E events: success code dropped, known failure codes translated, raw codes gone');

// (7) Charts show a compact range plus the min/max of the visible series.
const chartStartE = Date.parse('2026-10-30T01:34:00Z'); // 09:34 in Asia/Shanghai
assert.equal(metricsA.formatMetricRange(chartStartE, chartStartE + 3600000), '10/30 09:34 – 10:34', 'the same-day range repeats no date');
assert.equal(metricsA.formatMetricRange(Date.parse('2026-10-29T15:00:00Z'), Date.parse('2026-10-29T20:05:00Z')), '10/29 23:00 – 10/30 04:05', 'a cross-day range keeps both dates');
assert.equal(metricsA.formatMetricValue('cpu', 31.5), '31.5%');
assert.equal(metricsA.formatMetricValue('processes', 111), '111');
assert.equal(metricsA.formatMetricValue('rxRate', 1048576), '1.0 MiB/s');
assert.equal(metricsA.formatMetricValue('cpu', null), '—');
assert.equal(metricsA.metricRangeLabel([{ time: 0, cpu: 18.24 }, { time: 1, cpu: null }, { time: 2, cpu: 31.46 }], 'cpu'), '最低 18.2% · 最高 31.5%');
assert.equal(metricsA.metricRangeLabel([{ time: 0, cpu: null }], 'cpu'), '最低 — · 最高 —');
const gridE = read('modules/nodes/components/ServerMetricsGrid.vue');
assert.match(gridE, /metricRangeLabel\(points, card\.footerKey\)/);
assert.match(gridE, /formatMetricRange\(start\.value, end\.value\)/);
assert.doesNotMatch(gridE, /formatTime\(new Date\(start\)\.toISOString\(\)\)/, 'the full date is no longer printed twice');
// v1.5.1 item 8: Chart.js cards replace the hand-written SVG (the metric.key assertion above became card.footerKey).
const chartE = read('modules/nodes/components/MetricLineChart.vue');
assert.match(chartE, /from 'chart\.js';/); assert.doesNotMatch(chartE, /chart\.js\/auto/, 'register Chart.js piecemeal');
assert.match(chartE, /Chart\.register\(LineController, LineElement, PointElement, LinearScale, Filler, Tooltip, Legend\)/);
assert.match(chartE, /const FONT = \{ size: 12 \}/); assert.doesNotMatch(chartE, /size:\s*(?:[0-9]|1[01])\b/, 'chart fonts are at least 12px');
assert.match(chartE, /titleFont: FONT, bodyFont: FONT/); assert.ok((chartE.match(/font: FONT/g) || []).length >= 3, 'tick and legend fonts use FONT');
assert.match(chartE, /type: 'linear'/); assert.match(chartE, /spanGaps: false/);
assert.match(gridE, /defineAsyncComponent\(\(\) => import\('\.\/MetricLineChart\.vue'\)\)/, 'Chart.js loads lazily');
assert.doesNotMatch(gridE, /<svg|metricPath/, 'no hand-written SVG charts remain');
const cardE = id => metricsA.metricCards.find(c => c.id === id);
assert.deepEqual(metricsA.metricCards.map(c => c.id), ['cpu', 'memory', 'disk', 'network', 'connections', 'processes', 'retrans']);
assert.deepEqual(cardE('cpu').series.map(s => [s.key, s.axis ?? 'y']), [['cpu', 'y'], ['load', 'y1']]);
assert.equal(cardE('cpu').axes.y.max, 100); assert.equal(cardE('cpu').axes.y1.unit, 'load');
assert.deepEqual(cardE('network').series.map(s => s.key), ['rxRate', 'txRate']);
assert.deepEqual(cardE('connections').series.map(s => [s.key, !!s.optional]), [['tcp', false], ['udp', false], ['connections', true]]);
assert.deepEqual(cardE('memory').series.map(s => s.key), ['memoryBytes', 'swapBytes']);
const noneE = { time: 0, swapBytes: null, connections: null }, someE = { time: 1, swapBytes: 5, connections: 2 };
assert.deepEqual(metricsA.visibleSeries(cardE('memory'), [noneE]).map(s => s.key), ['memoryBytes']);
assert.deepEqual(metricsA.visibleSeries(cardE('connections'), [someE]).map(s => s.key), ['tcp', 'udp', 'connections']);
assert.match(gridE, /headline\(card\)/);
for (const gone of ['接收速率', '发送速率', '代理活跃会话', '1 分钟负载']) assert.ok(!gridE.includes(gone), 'merged card label lives in the model only: ' + gone);
assert.equal(metricsA.formatTimeFull(midnightA), '2026-10-30 00:05:09');
assert.equal(metricsA.formatXTick(midnightA, 3600000), '00:05'); assert.equal(metricsA.formatXTick(midnightA, 7 * 86400000), '10/30 00:05');
console.log('PASS walkthrough E charts: compact panel-timezone range, min/max of visible series, no duplicated timestamps');

// (9) An empty tuning draft value selects 不设置, and hints no longer duplicate placeholders.
const tuningModelE = await walkthroughEModule('modules/nodes/components/detail/serverTuningModel.ts');
const draftE = tuningModelE.tuningDraft({ 'net.ipv4.tcp_congestion_control': 'bbr' });
assert.equal(Object.keys(draftE).length, tuningModelE.tuningFields.length, 'every whitelisted key has a draft value');
assert.equal(draftE['net.core.default_qdisc'], '', 'an unset choice selects 不设置');
assert.equal(draftE['net.ipv4.tcp_congestion_control'], 'bbr');
assert.deepEqual(tuningModelE.tuningRequest('n', draftE), { settings: { 'net.ipv4.tcp_congestion_control': 'bbr' }, clear: false, confirmNodeId: 'n' });
const tuningE = read('modules/nodes/components/detail/ServerTuning.vue');
assert.match(tuningE, /tuningDraft\(result\.desired\)/);
assert.doesNotMatch(tuningE, /draft = \{ \.\.\.preset\.settings \}/, 'presets keep the complete draft');
assert.match(tuningE, /<option value="">不设置<\/option>/);
assert.doesNotMatch(tuningE, /field\.choices\?\.join\(' \/ '\)/, 'the hint no longer repeats the placeholder');
console.log('PASS walkthrough E tuning: empty draft selects 不设置, single hint line');


// (3) /users 到期时间 shows date + HH:mm in the panel timezone, on one line.
const timeE = await walkthroughEModule('app/time.ts', [["import { authState } from './auth';", 'const authState = globalThis.walkthroughAuth;']]);
assert.equal(timeE.formatMinute('2026-11-26T09:36:14Z'), '2026/11/26 17:36');
assert.equal(timeE.formatMinute(''), '');
assert.equal(timeE.formatMinute('not-a-date'), '');
globalThis.walkthroughAuth.timezone = 'America/Los_Angeles';
assert.equal(timeE.formatMinute('2026-11-26T09:36:14Z'), '2026/11/26 01:36', 'the cell follows the panel timezone');
globalThis.walkthroughAuth.timezone = 'Asia/Shanghai';
const usersListE = read('modules/users/pages/UserListPage.vue');
assert.match(usersListE, /formatMinute\(user\.expiresAt\)/);
assert.match(usersListE, /tabular-nums text-ios-tertiary whitespace-nowrap/);
assert.doesNotMatch(usersListE, /formatTime\(user\.expiresAt\) \}\}/, 'the seconds-bearing formatter is no longer printed in the cell');
const userDetailE = read('modules/users/pages/UserDetailPage.vue');
assert.match(userDetailE, /expiryParts\.date/, 'the expiry card shows the date on its own line');
assert.match(userDetailE, /expiryParts\.time/, 'and the time on a smaller second line');
const availableCardE = userDetailE.split('可用节点</h3>')[1].split('</section>')[0];
assert.match(availableCardE, /v-for="item in previewItems"/, 'the available-node card lists the subscription preview items');
assert.match(availableCardE, /\{\{ item\.name \}\}/, 'each row shows the subscription display name');
assert.doesNotMatch(availableCardE, /导入来源：|capabilities\?\.core_name|connectAddress/, 'the card shows no server information');
console.log('PASS walkthrough E expiry: minute precision in the panel timezone, nowrap cell, split detail card, available nodes from the preview');

// (11a) The dashboard date chip uses the panel timezone, not the browser's.
assert.equal(timeE.panelDateLabel(new Date('2026-10-29T17:00:00Z')), '10月30日 周五');
globalThis.walkthroughAuth.timezone = 'America/Los_Angeles';
assert.equal(timeE.panelDateLabel(new Date('2026-10-29T17:00:00Z')), '10月29日 周四');
globalThis.walkthroughAuth.timezone = 'Asia/Shanghai';
const dashboardE = read('modules/dashboard/pages/DashboardPage.vue');
assert.match(dashboardE, /panelDateLabel\(new Date\(\)\)/);
assert.doesNotMatch(dashboardE, /weekDays\[now\.getDay\(\)\]/, 'the browser-timezone weekday is gone');
// (11b) Ranking rows keep their natural height.
assert.doesNotMatch(dashboardE, /space-y-2 flex-1 flex flex-col justify-around/, 'ranking rows are not stretched');
// (11c) Over quota → expired → expiring within 7 days → usage %, disabled last.
const attentionE = [
  { id: 'heavy', status: 'active', expiresAt: '', trafficLimit: 100, usedTraffic: 90 },
  { id: 'disabled', status: 'disabled', expiresAt: '2020-01-01T00:00:00Z', trafficLimit: 100, usedTraffic: 100 },
  { id: 'soon', status: 'active', expiresAt: new Date(Date.now() + 2 * 86400000).toISOString(), trafficLimit: 100, usedTraffic: 5 },
  { id: 'expired', status: 'active', expiresAt: '2020-01-01T00:00:00Z', trafficLimit: 100, usedTraffic: 1 },
  { id: 'over', status: 'active', expiresAt: '2027-01-01T00:00:00Z', trafficLimit: 100, usedTraffic: 100 }
];
const rankedE = vueSubset('modules/dashboard/pages/DashboardPage.vue', ['userAttentionRank', 'topAttentionUsers'], { allUsers: { value: attentionE } });
assert.deepEqual(rankedE.topAttentionUsers.value.map(user => user.id), ['over', 'expired', 'soon', 'heavy'], 'expired/expiring users outrank plain usage, disabled users last');
assert.equal(rankedE.userAttentionRank({ status: 'disabled', trafficLimit: 0, usedTraffic: 0 }, Date.now()), 4);
console.log('PASS walkthrough E dashboard: panel-timezone date chip, compact ranking rows, attention ordering');

// The panel updates itself from 系统设置 → 系统信息: the card heads that tab, only
// offers the update when the server reports one, hides the action while a request is
// outstanding, polls the helper status and never claims success by itself.
const updatePanel = read('modules/system/components/PanelUpdatePanel.vue');
assert.match(updatePanel, /settingsAPI\.panelUpdate\(/, 'the card reads the panel update state');
assert.match(updatePanel, /settingsAPI\.startPanelUpdate\(/, 'the card submits the update request');
assert.match(updatePanel, /POLL_INTERVAL_MS = 3000/, 'the card polls every 3 s');
assert.match(updatePanel, /15 \* 60 \* 1000/, 'the card gives up polling after 15 minutes');
assert.match(updatePanel, /journalctl -u proxy-panel-update/, 'the timeout hint names the helper unit');
assert.match(updatePanel, /state\.value\.updateAvailable && !busy/, 'the update button needs an available update and no request in flight');
assert.match(updatePanel, /更新请求超过 10 分钟没有被服务器处理/, 'a stalled request says what to check and is offered again');
assert.match(updatePanel, /上次更新没有完成就被中断了/, 'an interrupted run says the server rolls back at boot');
assert.doesNotMatch(updatePanel, /localStorage/, 'the update state comes from the backend');
assert.match(read('modules/system/settingsApi.ts'), /panelUpdate: \(refresh/, 'the settings API exposes the update state');
assert.match(read('modules/system/pages/SettingsPage.vue'), /<PanelUpdatePanel v-if="isOwner && activeTab === 'info'" \/>/, '系统信息 heads with the panel update card');
console.log('PASS panel update: Owner-only card, confirmed request, helper polling, no local state');

// Owner report: in the expanded sidebar an open section must collapse when its
// header is clicked even if the current page lives outside it — it used to
// navigate to the section's first accessible page and stay open instead.
let sidebarSection = 'users';
const sidebarPushed = [];
const sidebarCollapsed = { value: false };
const sidebarA = vueSubset('components/AppSidebar.vue', ['openSections', 'handleSectionClick'], {
  ref: value => ({ value }),
  navSections: [
    { id: 'users', label: '用户管理', iconPath: '', path: '/users' },
    { id: 'servers', label: '服务器', iconPath: '', items: [
      { label: '服务器列表', path: '/nodes', iconPath: '' },
      { label: '版本与更新', path: '/nodes/versions', iconPath: '', ownerOnly: true }
    ] }
  ],
  router: { push: path => sidebarPushed.push(path) },
  isOwner: { value: true },
  isCollapsed: sidebarCollapsed,
  isSectionActive: id => id === sidebarSection
});
// A closed section still opens and navigates to its first accessible page.
sidebarA.openSections.value.servers = false;
sidebarA.handleSectionClick('servers');
assert.equal(sidebarA.openSections.value.servers, true, 'a closed section opens');
assert.deepEqual(sidebarPushed, ['/nodes'], 'a closed section outside the route navigates to its first accessible page');
// The reported bug: open + current page outside the section → collapse, never navigate.
sidebarPushed.length = 0;
sidebarA.handleSectionClick('servers');
assert.equal(sidebarA.openSections.value.servers, false, 'an open section collapses even when the current page is outside it');
assert.deepEqual(sidebarPushed, [], 'collapsing a section must not navigate');
// Open + current page inside the section → still just collapses.
sidebarA.openSections.value.servers = true;
sidebarSection = 'servers';
sidebarA.handleSectionClick('servers');
assert.equal(sidebarA.openSections.value.servers, false, 'an open section containing the current page also collapses');
assert.deepEqual(sidebarPushed, [], 'collapsing the active section must not navigate');
// The collapsed sidebar and sections with a direct path stay unchanged.
sidebarCollapsed.value = true;
sidebarA.openSections.value.servers = false;
sidebarA.handleSectionClick('servers');
assert.equal(sidebarA.openSections.value.servers, false, 'the collapsed sidebar does not expand sections');
assert.deepEqual(sidebarPushed, ['/nodes'], 'the collapsed sidebar navigates to the first accessible page');
sidebarPushed.length = 0;
sidebarA.handleSectionClick('users');
assert.deepEqual(sidebarPushed, ['/users'], 'a section with a direct path still navigates');
console.log('PASS sidebar: an open section collapses on header click even outside its pages, closed sections still open and navigate');

const toast = read('components/ToastContainer.vue');
assert.match(toast, /<Teleport to="body">/, 'toasts must be teleported after the dialogs');
assert.match(toast, /z-\[100\]/, 'toasts must sit above dialogs (z-50) and drawers (z-[60])');
for (const file of ['components/AppDialog.vue', 'components/ConfirmDialog.vue', 'components/FormDrawer.vue']) {
  const layers = [...read(file).matchAll(/\bz-(?:\[(\d+)\]|(\d+))/g)].map(m => Number(m[1] || m[2]));
  assert.ok(layers.every(z => z < 100), file + ' must stay below the toast layer');
}
console.log('PASS P1 TOAST: toasts above every dialog and drawer');

for (const file of ['modules/users/pages/UserListPage.vue', 'modules/users/pages/UserDetailPage.vue']) {
  assert.doesNotMatch(read(file), /allowedProtocols/, file + ' no longer edits or filters by protocol');
}
console.log('PASS P1 USERS: no per-user protocol control');

const nodeVersionPanel = read('modules/system/components/NodeVersionPanel.vue');
assert.doesNotMatch(nodeVersionPanel, /中央尚未配置发布公钥/, 'no warnings about unconfigured release keys');
assert.match(nodeVersionPanel, /importAgentRelease\(/, 'NodeVersionPanel imports official releases');
assert.match(nodeVersionPanel, /agentUpdateSupported/, 'reads agentUpdateSupported capability');
assert.match(nodeVersionPanel, /需在服务器上运行一次 qui-update --component agent 启用网页升级/, 'shows hint when Agent web upgrade is disabled');
const nodesApiP2 = read('modules/nodes/api.ts');
assert.match(nodesApiP2, /agentUpdateSupported:\s*boolean/, 'NodeView includes agent update support');
assert.match(nodesApiP2, /agentReleases/, 'nodeAPI exposes agentReleases');
assert.match(nodesApiP2, /importAgentRelease/, 'nodeAPI exposes importAgentRelease');
console.log('PASS P2 AGENT: one-click signed Agent upgrade and capability check');

// A13: the chosen release must match both version and platform.
const releaseFinderSource = nodeVersionPanel.slice(nodeVersionPanel.indexOf('function findAgentReleaseForNode('), nodeVersionPanel.indexOf('async function selectAgentVersionForUpgrade('));
const releaseFinderJs = ts.transpileModule(releaseFinderSource, { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText;
const findRelease = new Function('nodePlatform', releaseFinderJs + '\nreturn findAgentReleaseForNode;')(node => node.platform);
const release = (version, architecture = 'amd64', component = 'proxy-agent') => ({ signed: { manifest: { version, architecture, component, os: 'linux' } } });
const wrong = release('1.4.6'), correct = release('1.5.0');
assert.equal(findRelease({ platform: 'linux/amd64' }, '1.5.0', [wrong]), undefined);
assert.equal(findRelease({ platform: 'linux/amd64' }, '1.5.0', [wrong, release('1.5.0', 'arm64'), correct]), correct);
assert.equal(findRelease({}, '1.5.0', [correct]), undefined);
assert.equal(findRelease({ platform: 'linux/amd64' }, '1.5.0', [release('1.5.0', 'amd64', 'proxy-panel')]), undefined);
for (const file of ['components/NotificationBell.vue', 'modules/routing/pages/ClientPolicyPage.vue', 'modules/system/components/NotificationSettings.vue']) {
  for (const match of read(file).matchAll(/text-\[(\d+(?:\.\d+)?)px\]/g)) {
    assert.ok(Number(match[1]) >= 12, file + ' must not contain an arbitrary font size below 12px');
  }
}
console.log('PASS AUDIT A13/FONTS: exact release version/platform, notifications and external rule sources use at least 12px');

const flagCss = read('styles/main.css');
assert.match(flagCss, /@font-face\s*\{[^}]*font-family:\s*"Twemoji Country Flags"/, 'main.css declares the Twemoji Country Flags font');
assert.match(flagCss, /unicode-range:[^;]*U\+1F1E6-1F1FF/, 'flag font is limited to regional indicator code points');
assert.match(readFileSync(new URL('../tailwind.config.js', import.meta.url), 'utf8'), /sans:\s*\[\s*'"Twemoji Country Flags"'/, 'sans stack starts with the flag font');
assert.ok(readFileSync(new URL('../src/styles/fonts/TwemojiCountryFlags.woff2', import.meta.url)).length > 10000, 'flag font file is present');
console.log('PASS V1.5.1 FLAGS: self-hosted Twemoji flag font declared, range-limited and first in the sans stack');
