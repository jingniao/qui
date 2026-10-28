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

const here = path.dirname(fileURLToPath(import.meta.url));
const source = readFileSync(path.resolve(here, '..', 'src', 'modules', 'routing', 'api.ts'), 'utf8');
const outDir = mkdtempSync(path.join(tmpdir(), 'client-policy-selftest-'));
const js = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext }, fileName: 'api.ts' }).outputText
  .replace("from '../../app/apiClient'", "from './apiClient.mjs'");
writeFileSync(path.join(outDir, 'apiClient.mjs'), 'export function request() { throw new Error("no network in the self-test"); }\n');
writeFileSync(path.join(outDir, 'api.mjs'), js);
const api = await import(pathToFileURL(path.join(outDir, 'api.mjs')).href);

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

if (failures) {
  console.log(`\n${failures} client policy self-test(s) failed`);
  process.exit(1);
}
console.log('\nAll client policy self-tests passed');
