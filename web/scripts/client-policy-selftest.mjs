// Self-test for the client policy simulator in web/src/modules/routing/api.ts.
//
// The module is transpiled in memory (ts.transpileModule) into a fresh
// temporary directory, with its API client import pointed at a stub, and the
// address math and the "decided by the client at runtime" outcomes are checked.
//
// Usage: node scripts/client-policy-selftest.mjs   (exit code 1 on any failure)

import { mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import ts from 'typescript';
import { isDeepStrictEqual } from 'node:util';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = readFileSync(path.resolve(here, '..', 'src', 'modules', 'routing', 'api.ts'), 'utf8');
const outDir = mkdtempSync(path.join(tmpdir(), 'client-policy-selftest-'));
const js = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext }, fileName: 'api.ts' }).outputText
  .replace("from '../../app/apiClient'", "from './apiClient.mjs'");
writeFileSync(path.join(outDir, 'apiClient.mjs'), `export const calls = [];
export function request(path, method = 'GET', body) { calls.push({ path, method, body }); return Promise.resolve({}); }
`);
writeFileSync(path.join(outDir, 'api.mjs'), js);
const api = await import(pathToFileURL(path.join(outDir, 'api.mjs')).href);
const client = await import(pathToFileURL(path.join(outDir, 'apiClient.mjs')).href);

let failures = 0;
function check(name, condition) {
  if (condition) {
    console.log(`PASS ${name}`);
  } else {
    failures++;
    console.log(`FAIL ${name}`);
  }
}

const v4 = value => ({ family: 4, value: value.split('.').reduce((acc, part) => (acc << 8n) + BigInt(Number(part)), 0n) });

check('10.1.2.3 in 10.0.0.0/8', api.cidrContains('10.0.0.0/8', v4('10.1.2.3')));
check('11.0.0.1 not in 10.0.0.0/8', !api.cidrContains('10.0.0.0/8', v4('11.0.0.1')));
check('172.20.0.1 in 172.16.0.0/12 (string prefixes got this wrong)', api.cidrContains('172.16.0.0/12', v4('172.20.0.1')));
check('172.32.0.1 not in 172.16.0.0/12', !api.cidrContains('172.16.0.0/12', v4('172.32.0.1')));
check('a bare address is a host prefix', api.cidrContains('203.0.113.7', v4('203.0.113.7')) && !api.cidrContains('203.0.113.7', v4('203.0.113.8')));

function config(rules) {
  return { version: 1, updatedAt: '', settings: { mode: 'rule', mixedPort: 7890, allowLan: true, logLevel: 'info' }, proxyGroups: [], rules, extra: { ipv6: false, dns: { enable: false, enhancedMode: '', nameservers: [], fallback: [] }, providers: [] } };
}
const rule = (priority, type, payload, target, extra = {}) => ({ id: `r${priority}`, type, payload, target, enabled: true, priority, ...extra });
const match = rule(999, 'MATCH', '', 'PROXY');

const v6 = api.simulateClientPolicy('2001:db8::1', 443, config([rule(1, 'IP-CIDR6', '2001:db8::/32', 'DIRECT'), match]));
check('IPv6 address matches IP-CIDR6', v6.matched && v6.target === 'DIRECT');
const v6miss = api.simulateClientPolicy('2001:db9::1', 443, config([rule(1, 'IP-CIDR6', '2001:db8::/32', 'DIRECT'), match]));
check('IPv6 outside the prefix falls through to MATCH', v6miss.matched && v6miss.target === 'PROXY');
const embedded = api.simulateClientPolicy('::ffff:10.0.0.1', 443, config([rule(1, 'IP-CIDR', '10.0.0.0/8', 'DIRECT'), match]));
check('an IPv4-mapped IPv6 address is not an IPv4 address', embedded.target === 'PROXY');
const port = api.simulateClientPolicy('example.com', 8050, config([rule(1, 'DST-PORT', '80/443/8000-8100', 'REJECT'), match]));
check('DST-PORT range', port.matched && port.target === 'REJECT');
const portMiss = api.simulateClientPolicy('example.com', 8443, config([rule(1, 'DST-PORT', '80/443/8000-8100', 'REJECT'), match]));
check('DST-PORT miss', portMiss.target === 'PROXY');
const resolve = api.simulateClientPolicy('example.com', 443, config([rule(1, 'IP-CIDR', '10.0.0.0/8', 'DIRECT'), match]));
check('a domain reaching an IP rule is decided at runtime', resolve.runtime && !resolve.matched);
const noResolve = api.simulateClientPolicy('example.com', 443, config([rule(1, 'IP-CIDR', '10.0.0.0/8', 'DIRECT', { noResolve: true }), match]));
check('no-resolve lets a domain skip the IP rule', !noResolve.runtime && noResolve.target === 'PROXY');
const geo = api.simulateClientPolicy('1.1.1.1', 443, config([rule(1, 'GEOIP', 'CN', 'DIRECT'), match]));
check('GEOIP CN is decided at runtime, not guessed from .cn', geo.runtime);
const lan = api.simulateClientPolicy('192.168.3.4', 443, config([rule(1, 'GEOIP', 'LAN', 'DIRECT'), match]));
check('GEOIP LAN matches a private address', lan.matched && lan.target === 'DIRECT');
const site = api.simulateClientPolicy('ads.example.com', 443, config([rule(1, 'GEOSITE', 'category-ads-all', 'REJECT'), match]));
check('GEOSITE is decided at runtime', site.runtime);
const suffix = api.simulateClientPolicy('chat.openai.com', 443, config([rule(1, 'DOMAIN-SUFFIX', 'openai.com', 'PROXY'), rule(2, 'MATCH', '', 'DIRECT')]));
check('DOMAIN-SUFFIX matches a subdomain', suffix.matched && suffix.target === 'PROXY');
const lookalike = api.simulateClientPolicy('notopenai.com', 443, config([rule(1, 'DOMAIN-SUFFIX', 'openai.com', 'PROXY'), rule(2, 'MATCH', '', 'DIRECT')]));
check('DOMAIN-SUFFIX does not match a lookalike', lookalike.target === 'DIRECT');
const service = api.simulateClientPolicy('api.example.org', 443, config([rule(1, 'RULE-SET', 'service:svc', 'REJECT'), match]), [{ id: 'svc', name: 'svc', domains: [], suffixes: ['example.org'], cidrs: [] }]);
check('a panel service rule set matches its suffix', service.matched && service.target === 'REJECT');
const external = api.simulateClientPolicy('api.example.org', 443, config([rule(1, 'RULE-SET', 'ads', 'REJECT'), match]));
check('an external rule provider is decided at runtime', external.runtime);

const template = api.fromConfig(api.toConfig({ revision: 3, updatedAt: '', updatedBy: 'owner', template: { preset: 'standard', client: { mode: 'rule', mixedPort: 7890, allowLan: true, logLevel: 'info', ipv6: true }, dns: { enable: true, enhancedMode: 'fake-ip', nameservers: ['223.5.5.5'], fallback: [] }, groups: [{ name: 'PROXY', type: 'select', members: ['AUTO', '@all', 'DIRECT'], testUrl: '', intervalSeconds: 0 }, { name: 'AUTO', type: 'url-test', members: ['@all'], testUrl: 'http://t.example/204', intervalSeconds: 60 }], providers: [{ name: 'ads', behavior: 'domain', format: 'text', url: 'https://r.example/a', intervalSeconds: 3600 }], rules: [{ id: 'm', type: 'MATCH', payload: '', target: 'PROXY', noResolve: false, enabled: true, priority: 999, description: '' }] } }));
check('the page view model round-trips the server template', template.client.ipv6 === true && template.dns.enable === true && template.providers.length === 1 && template.groups[0].members.join('|') === 'AUTO|@all|DIRECT' && template.groups[1].testUrl === 'http://t.example/204' && template.groups[1].intervalSeconds === 60);

const input = { name: 'profile', description: '', template, uiMetadata: { category: 'region' } };
await api.clientPolicyProfilesAPI.list(2, 20);
await api.clientPolicyProfilesAPI.get('profile/id');
await api.clientPolicyProfilesAPI.create({ ...input, importKey: 'legacy:hash' });
await api.clientPolicyProfilesAPI.create({ ...input, activate: true, expectedActiveRevision: 9 });
await api.clientPolicyProfilesAPI.update('profile/id', { ...input, expectedRevision: 3, expectedActiveRevision: 9 });
await api.clientPolicyProfilesAPI.activate('profile/id', 3, 9);
await api.clientPolicyProfilesAPI.delete('profile/id', 3);
check('profile listing paginates and detail escapes the ID', client.calls[0].path === '/client-policy-profiles?page=2&pageSize=20' && client.calls[1].path.endsWith('/profile%2Fid'));
check('legacy imports are idempotent and inactive', client.calls[2].method === 'POST' && client.calls[2].body.importKey === 'legacy:hash' && !client.calls[2].body.activate);
check('create-and-activate is a single API call', client.calls[3].body.activate && client.calls[3].body.expectedActiveRevision === 9);
check('profile save sends both independent versions and opaque metadata', client.calls[4].method === 'PUT' && client.calls[4].body.expectedRevision === 3 && client.calls[4].body.expectedActiveRevision === 9 && client.calls[4].body.uiMetadata.category === 'region');
check('activation uses its CAS endpoint without resending a template', client.calls[5].path.endsWith('/activate') && client.calls[5].body.expectedRevision === 3 && client.calls[5].body.expectedActiveRevision === 9 && !('template' in client.calls[5].body));
check('delete carries the profile version', client.calls[6].method === 'DELETE' && client.calls[6].path.endsWith('?expectedRevision=3'));
const page = readFileSync(path.resolve(here, '..', 'src', 'modules', 'routing', 'pages', 'ClientPolicyPage.vue'), 'utf8');
const conflictHandler = page.slice(page.indexOf('function profileFailure('), page.indexOf('function previewLegacyCards('));
check('conflicts retain drafts and display the current server version', conflictHandler.includes('error.details.activeRevision') && conflictHandler.includes('草稿已保留') && !conflictHandler.includes('load(') && !conflictHandler.includes('clientConfig.value ='));
check('cards no longer write localStorage and imports hash content', !page.includes('localStorage.setItem(') && page.includes("crypto.subtle.digest('SHA-256'") && page.includes('legacyCards.value.every'));
check('new profiles use the complete server preset', page.includes('template: JSON.parse(JSON.stringify(standard.template))') && page.includes('设为订阅模板，客户端更新订阅后生效'));
check('membership controls persist filters and display server authority', page.includes('setGroupKeywords') && page.includes('最终成员以服务器展开为准') && page.includes('实际出口未知'));
check('region matching is not duplicated in the page', !page.includes('groupKeywordFilters') && !page.includes('getMatchedNodesForGroup') && !page.includes('applyMatchedNodesToGroup'));
check('endpoint selection writes stable references', page.includes("addGroupMemberDirect(group, '@endpoint:' + ref)"));
check('group category metadata follows renames', page.includes('delete groupCategories.value[previous]') && page.includes('groupCategories.value[name] = category') && page.includes('uiMetadata: { ...draftMetadata.value, groupCategories:'));
check('region creation has persisted constraints and rejects empty sets', page.includes("includeKeywords: target.inc.split(',')") && page.includes("emptyPolicy: 'reject'"));
const membershipTemplate = structuredClone(template);
membershipTemplate.groups[0].includeKeywords = ['United States', 'US'];
membershipTemplate.groups[0].excludeKeywords = ['维护'];
membershipTemplate.groups[0].emptyPolicy = 'reject';
membershipTemplate.groups[0].members = ['@endpoint:inbound:one', '@endpoint:relay:rule', '@endpoint:import:src:US, \"quoted\" name|socks5'];
const roundTrip = api.fromConfig(api.toConfig({ template: membershipTemplate, revision: 1, updatedAt: '', updatedBy: '' }));
check('membership fields round-trip through the editor DTO', isDeepStrictEqual(roundTrip, membershipTemplate));
await api.clientPolicyProfilesAPI.candidates();
await api.clientPolicyProfilesAPI.candidates('user/id');
check('candidate API supports reported auto-selection and explicit users', client.calls.at(-2).path === '/client-policy-candidates' && client.calls.at(-1).path === '/client-policy-candidates?userId=user%2Fid');

await api.clientPolicyProfilesAPI.preview({ template: membershipTemplate, format: 'clash' });
await api.clientPolicyProfilesAPI.preview({ profileId: 'inactive', profileRevision: 4, userId: 'user' });
check('draft preview posts the full template without saving', client.calls.at(-2).path === '/client-policy-profiles/preview' && client.calls.at(-2).method === 'POST' && isDeepStrictEqual(client.calls.at(-2).body.template, membershipTemplate));
check('saved preview posts the profile revision and optional user', isDeepStrictEqual(client.calls.at(-1).body, { profileId: 'inactive', profileRevision: 4, userId: 'user' }));

// Execute the page's changed actions with reactive values and API boundaries
// replaced by small stubs. These tests check behavior, including draft retention.
const script = page.slice(page.indexOf('<script setup lang="ts">') + '<script setup lang="ts">'.length, page.indexOf('</script>'));
const ast = ts.createSourceFile('ClientPolicyPage.ts', script, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
function pageAction(name) {
  return ast.statements.find(statement => ts.isFunctionDeclaration(statement) && statement.name?.text === name).getText(ast);
}
const actions = ['loadPreview', 'resolveGroupMembers', 'handleGroupRuleAction', 'openCreateRuleDrawer', 'quickCreateRuleForService'].map(pageAction).join('\n');
const compiledActions = ts.transpileModule(actions, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.None } }).outputText;
const ref = value => ({ value });
const state = {
  previewGeneration: 0,
  isDirty: ref(true), editingPolicyId: ref('inactive'), clientConfig: ref(api.toConfig({ template: membershipTemplate, revision: 4, updatedAt: '', updatedBy: '' })),
  previewResult: ref(null), previewError: ref(''), previewFormat: ref('clash'), resolvedGroupName: ref(''),
  activeModule: ref('groups'), ruleSearchKeyword: ref('search'), ruleTargetFilter: ref(''), ruleTypeFilter: ref('DOMAIN'),
  ruleTargetGroupContext: ref(null), editingRule: ref(null), form: ref({}), showAdvancedRuleSettings: ref(true), drawerVisible: ref(false),
  fromConfig: api.fromConfig, clientPolicyProfilesAPI: { preview: async input => { client.calls.push(input); return { groups: [{ name: 'PROXY', members: [{ kind: 'builtin', name: 'REJECT' }] }] }; } }
};
const actionNames = ['loadPreview', 'resolveGroupMembers', 'handleGroupRuleAction', 'openCreateRuleDrawer', 'quickCreateRuleForService'];
const run = new Function(...Object.keys(state), compiledActions + '\nreturn {' + actionNames.join(',') + '};')(...Object.values(state));
await run.loadPreview();
check('dirty page previews its draft and keeps the profile untouched', isDeepStrictEqual(client.calls.at(-1).template, membershipTemplate) && !('profileId' in client.calls.at(-1)) && state.isDirty.value);
state.isDirty.value = false;
await run.loadPreview();
check('clean page previews its saved profile revision', isDeepStrictEqual(client.calls.at(-1), { profileId: 'inactive', profileRevision: 4, format: 'clash' }));
await run.resolveGroupMembers('PROXY');
check('region resolve uses the same preview and server members', state.resolvedGroupName.value === 'PROXY' && state.previewResult.value.groups[0].members[0].name === 'REJECT');
state.isDirty.value = true;
const retained = structuredClone(state.clientConfig.value);
state.clientPolicyProfilesAPI.preview = async () => { throw new Error('stale_client_policy'); };
await run.loadPreview();
check('preview failures retain the draft and clear old results', isDeepStrictEqual(state.clientConfig.value, retained) && state.isDirty.value && state.previewResult.value === null && state.previewError.value === 'stale_client_policy');
const pendingPreviews = [];
state.clientPolicyProfilesAPI.preview = () => new Promise(resolve => pendingPreviews.push(resolve));
const oldRequest = run.loadPreview();
const newRequest = run.loadPreview();
pendingPreviews[1]({ body: 'new draft' });
await newRequest;
pendingPreviews[0]({ body: 'old draft' });
await oldRequest;
check('a late preview response cannot replace the latest preview', state.previewResult.value.body === 'new draft');
for (const count of [0, 1, 3]) {
  state.clientConfig.value.rules = Array.from({ length: count }, (_, index) => rule(index, 'DOMAIN', 'example.com', 'PROXY'));
  state.drawerVisible.value = false;
  run.handleGroupRuleAction('PROXY');
  check(`group edit with ${count} rules always opens the filtered module`, state.activeModule.value === 'rules' && state.ruleTargetFilter.value === 'PROXY' && state.ruleSearchKeyword.value === '' && state.ruleTypeFilter.value === '' && !state.drawerVisible.value);
}
run.openCreateRuleDrawer();
check('add in a filtered empty state preselects the target group', state.form.value.target === 'PROXY' && state.ruleTargetGroupContext.value === 'PROXY' && state.drawerVisible.value);
run.quickCreateRuleForService({ id: 'svc', name: '服务' });
check('use rule set in routing preselects RULE-SET and its payload', state.form.value.type === 'RULE-SET' && state.form.value.payload === 'service:svc' && state.drawerVisible.value);
check('preview displays context, all warnings and resolved groups', page.includes('previewResult.context.autoSelected') && page.includes('warning.stage') && page.includes('warning.ref') && page.includes('warning.message') && page.includes('group in previewResult?.groups') && page.includes('previewResult?.diagnostic'));
check('filtered rules disable drag and offer view all', page.includes(':draggable="!isFiltered"') && page.includes('if (isFiltered.value) return;') && page.includes('查看全部'));


await api.policyAPI.serviceReferences('shared');
check('shared reference query uses the service-set endpoint', client.calls.at(-1).path === '/service-sets/shared/references');
const paginationSource = readFileSync(path.resolve(here, '..', 'src', 'app', 'pagination.ts'), 'utf8');
const paginationJS = ts.transpileModule(paginationSource.replace('export ', ''), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.None } }).outputText.replace('export ', '');
const allPages = new Function(paginationJS + '\nreturn allPages;')();
const visitedPages = [];
const everyItem = await allPages(async page => {
  visitedPages.push(page);
  return { items: Array.from({ length: page === 3 ? 5 : 100 }, (_, index) => (page - 1) * 100 + index), total: 205 };
});
check('selectors load all 205 results through existing pagination', everyItem.items.length === 205 && isDeepStrictEqual(visitedPages, [1, 2, 3]));
let incomplete = false;
try { await allPages(async page => ({ items: page === 1 ? [1] : [], total: 2 })); } catch { incomplete = true; }
check('incomplete pagination reports an error', incomplete);
check('node, group and rule-set selectors use complete pagination', page.includes("allPages(page => nodeAPI.list(page, ''))") && page.includes('allPages(page => groupAPI.list(page, 100))') && !page.includes('policyAPI.services(1, 100)'));
class TestAPIError extends Error { constructor(status, details) { super('规则集仍被引用'); this.status = status; this.details = details; } }
const sharedRefs = { nodePolicies: [{ id: 'node', name: '节点策略', enabled: false }], clientProfiles: [{ id: 'profile', name: '配置', active: false, rules: [{ id: 'off', enabled: false }] }] };
const shared = {
  editingService: ref(null), serviceReferences: ref(null), serviceReferencesError: ref(''), serviceNotice: ref(''), deletingService: ref(false),
  savingService: ref(false), serviceDrawerVisible: ref(false), serviceForm: {}, serviceDomainsText: ref(''), serviceSuffixesText: ref(''), serviceCidrsText: ref(''), serviceSets: ref([]),
  APIError: TestAPIError, allPages, showToast: () => {},
  policyAPI: { serviceReferences: async () => sharedRefs, deleteService: async () => { throw new TestAPIError(409, sharedRefs); }, saveService: async () => ({ name: '共享规则', references: sharedRefs, warning: '404；客户端可能继续使用缓存或报告错误' }), services: async () => ({ items: [], total: 0 }) }
};
const sharedActions = ['openEditService', 'cloneService', 'deleteService', 'saveService', 'lines'];
const sharedJS = ts.transpileModule(sharedActions.map(pageAction).join('\n'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.None } }).outputText;
const sharedRun = new Function(...Object.keys(shared), sharedJS + '\nreturn {' + sharedActions.join(',') + '};')(...Object.values(shared));
const set = { id: 'shared', name: '共享规则', enabled: true, domains: ['example.org'], suffixes: [], cidrs: [] };
await sharedRun.openEditService(set);
check('shared editor loads inactive profiles, disabled rules and node references', shared.serviceReferences.value === sharedRefs && shared.editingService.value.id === 'shared');
await sharedRun.deleteService();
check('delete 409 displays references and retains the shared editor', shared.serviceReferences.value === sharedRefs && shared.serviceReferencesError.value && shared.serviceDrawerVisible.value);
shared.serviceForm.enabled = false;
await sharedRun.saveService();
check('shared save immediately displays the returned disable warning', shared.serviceNotice.value.includes('缓存') && !shared.serviceDrawerVisible.value);
await sharedRun.openEditService(set, true);
check('clone creates an independent shared set without old references', shared.editingService.value === null && shared.serviceForm.name === '共享规则-副本' && shared.serviceReferences.value.clientProfiles.length === 0);
check('shared editor explains immediate save and cached-rule semantics', page.includes('放弃配置草稿不会撤销这里的保存') && page.includes('客户端可能继续使用缓存或报告错误') && page.includes('profile.active') && page.includes('rule.enabled') && page.includes('policy.enabled'));

if (failures) {
  console.log(`\n${failures} client policy self-test(s) failed`);
  process.exit(1);
}
console.log('\nAll client policy self-tests passed');
