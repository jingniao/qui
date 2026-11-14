import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

// v1.5.5 item 19: the flag before a server name comes from the country the
// panel resolves from the public IP (node.countryCode, offline DB-IP table).
const read = path => readFileSync(new URL('../src/' + path, import.meta.url), 'utf8');
const metaSource = read('modules/nodes/nodeMeta.ts')
  .replace("import { reactive } from 'vue';", 'const reactive = value => value;')
  .replace("import { request } from '../../app/apiClient';", 'const request = async () => ({});');
const code = ts.transpileModule(metaSource, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
globalThis.localStorage = { getItem: () => null, removeItem() {}, setItem() {} };
const meta = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);

// Priority: the Owner's region, then the IP country, then a name keyword.
{
  const de = meta.resolveCountryInfo({ countryCode: 'DE', name: 'node-1' });
  assert.deepEqual([de.code, de.flag, de.source, de.name], ['DE', '🇩🇪', 'ip_geo', '德国']);
  assert.equal(meta.countryTitle(de), '德国（按公网 IP 识别）');

  const corrected = meta.resolveCountryInfo({ region: '香港', countryCode: 'DE', name: 'node-1' });
  assert.deepEqual([corrected.code, corrected.source], ['HK', 'admin_region'], 'the Owner region corrects a wrong lookup');

  const unmatchedRegion = meta.resolveCountryInfo({ region: '机房 A', countryCode: 'JP' });
  assert.deepEqual([unmatchedRegion.code, unmatchedRegion.source], ['JP', 'ip_geo'], 'free-text region falls back to the IP');

  const byName = meta.resolveCountryInfo({ countryCode: '', name: 'Tokyo-01' });
  assert.deepEqual([byName.code, byName.source], ['JP', 'name_match']);
  assert.equal(meta.countryTitle(byName), '日本（按服务器名称识别）');

  const unknown = meta.resolveCountryInfo({ name: 'node-1' });
  assert.deepEqual([unknown.code, unknown.flag, unknown.source], ['', '🌐', 'fallback']);
  assert.equal(meta.countryTitle(unknown), '未知地区');

  for (const bad of ['d', 'DEU', '1A', 'ZZZ']) {
    assert.equal(meta.resolveCountryInfo({ countryCode: bad }).source, 'fallback', `invalid code ${bad}`);
  }
  assert.equal(meta.resolveCountryInfo({ countryCode: 'de' }).code, 'DE', 'codes are case-insensitive');
}

// Codes outside the curated table get a localised name, never a blank one.
{
  const iceland = meta.resolveCountryInfo({ countryCode: 'IS' });
  assert.equal(iceland.flag, '🇮🇸');
  assert.ok(iceland.name && iceland.name !== '未知地区', `name for IS: ${iceland.name}`);
  assert.equal(meta.countryName('HK'), '中国香港', 'the curated table wins over Intl names');
}

// The dead browser-side lookup is gone and every caller passes the panel code.
{
  const all = metaSource;
  assert.doesNotMatch(all, /qui_ip_geo_|triggerIpLookup|onGeoResolved/, 'no browser-side IP lookup or cache');
  for (const path of [
    'modules/nodes/components/serverCardValues.ts',
    'modules/nodes/components/serverListModel.ts',
    'modules/nodes/components/detail/ServerOverview.vue',
    'modules/system/components/NodeVersionPanel.vue',
    'modules/proxy/pages/ProxyNodeListPage.vue',
  ]) {
    const source = read(path);
    assert.match(source, /countryCode: (node|server|props\.node)\.countryCode/, `${path} passes the panel country code`);
    assert.doesNotMatch(source, /resolveCountryInfo\([^{)]/, `${path} uses the object form`);
  }
  assert.doesNotMatch(read('modules/nodes/components/ServerCardGrid.vue'), /IP归属识别/, 'the card tooltip names the real source');
  assert.match(read('modules/nodes/components/ServerInventory.vue'), /row\.country\.flag/, 'the list view shows the flag too');
  assert.match(read('modules/system/pages/SettingsPage.vue'), /IP Geolocation by[\s\S]*db-ip\.com/, 'CC BY attribution for DB-IP');
}

console.log('PASS server flags: region > IP country > name, localised names, no browser lookup, all callers and attribution');
