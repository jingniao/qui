import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const source = readFileSync(new URL('../src/modules/proxy/pages/ProxyNodeListPage.vue', import.meta.url), 'utf8');
const loadBlock = source.match(/async function loadData\(\)[\s\S]*?\n}/)[0];
const compile = code => ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText;

async function scenario({ failed = [], delayedInbounds = false } = {}) {
  const errors = [], loaded = [], reads = [];
  const loading = { value: false }, servers = { value: [] }, inbounds = { value: [] };
  const store = { manualNodes: [{ id: 'confirmed-import' }], subscriptions: [{ id: 'confirmed-source' }] };
  let releaseInbounds;
  const read = async (kind, values) => {
    reads.push(kind);
    if (failed.includes(kind)) throw new Error(`${kind} unavailable`);
    if (kind === 'inbounds' && delayedInbounds) await new Promise(resolve => { releaseInbounds = resolve; });
    return { items: values };
  };
  const load = new Function('loading', 'proxyNodeStore', 'choices', 'inventory', 'servers', 'serverInbounds', 'loadNodeRelays', 'canvasAPI', 'isOwner', 'loadImports', 'nodeGroups', 'loadCanvasSchemes', 'showToast', compile(`let serverRefreshGeneration = 0, loadAgain = false, publishStateDisposed = false;\n${loadBlock}\nreturn loadData;`))(
    loading, store, async fetch => (await fetch(1, 100)).items,
    { readServers: async () => (await read('servers', [{ id: 'server' }])).items,
      readInbounds: async () => (await read('inbounds', [{ id: 'entry' }])).items,
      readGroups: async () => (await read('groups', [])).items },
    servers, inbounds, () => {},
    { list: () => read('schemes', [{ id: 'scheme' }]) },
    { value: true }, () => read('imports', []), { value: [] },
    async (context, schemes) => { loaded.push({ context, schemes }); },
    message => errors.push(message));
  const pending = load();
  for (let i = 0; i < 10; i++) await Promise.resolve();
  assert.deepEqual(new Set(reads), new Set(['servers', 'inbounds', 'schemes', 'imports', 'groups']));
  if (delayedInbounds) {
    assert.deepEqual(store.manualNodes.map(node => node.id), ['confirmed-import'], 'refresh keeps confirmed imports while resources are pending');
    assert.deepEqual(store.subscriptions.map(source => source.id), ['confirmed-source']);
    assert.equal(loaded.length, 0, 'scheme resolution waits for inbounds');
    releaseInbounds();
  }
  await pending;
  assert.equal(loading.value, false);
  return { loaded, errors, servers, inbounds };
}

const isolated = await scenario({ failed: ['imports', 'groups'], delayedInbounds: true });
assert.equal(isolated.loaded.length, 1, 'import/group failure must not suppress relay nesting');
assert.deepEqual(isolated.loaded[0].context.inbounds, [{ id: 'entry' }]);
assert.deepEqual(isolated.loaded[0].schemes, [{ id: 'scheme' }]);
assert.equal(isolated.errors.length, 2);
assert.match(isolated.errors[0], /导入节点读取失败：imports unavailable/);
assert.match(isolated.errors[1], /节点分组读取失败：groups unavailable/);
const missingSchemes = await scenario({ failed: ['schemes'] });
assert.equal(missingSchemes.loaded.length, 0);
assert.deepEqual(missingSchemes.servers.value, [{ id: 'server' }]);
assert.deepEqual(missingSchemes.inbounds.value, [{ id: 'entry' }]);
assert.deepEqual(missingSchemes.errors, ['中转方案读取失败：schemes unavailable']);
console.log('PASS independent scheme/import/group reads and deferred nesting after inbound arrival');

const grid = source.slice(source.indexOf('<!-- Row 1:'), source.indexOf('<!-- Row 3:'));
const title = grid.slice(0, grid.indexOf('<!-- Row 2:'));
const secondLine = grid.slice(grid.indexOf('<!-- Row 2:'));
assert.doesNotMatch(title, /node\.protocol/);
assert.ok(secondLine.indexOf('node.protocol') < secondLine.indexOf('<EntryRouteSummary'));
assert.match(secondLine, /v-if="node\.sourceType !== 'self_hosted'"[^>]*>\{\{ node\.sourceName \}\}/);
assert.equal((source.match(/:show-type="false"/g) || []).length, 2, 'both views hide route type badges');
assert.match(source, /<InboundDeleteDialog ref="inboundDeleteDialog" @changed="queueDraftRefresh"/);
assert.match(source, /inboundDeleteDialog\.value\?\.prompt\(\{ id: node\.inboundId, name: node\.name \}\)/);
assert.doesNotMatch(source, /inboundDeleteConflict|proxyAPI\.deleteInbound|publishNode\(node\.sourceId/);
console.log('PASS protocol second-line placement, imported source badges, shared cascade deletion dialog');
