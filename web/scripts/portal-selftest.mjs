// Portal API boundary and session/CSRF contract. No network or panel is used.
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import ts from 'typescript';
import { parse } from '@vue/compiler-sfc';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../src/portal');
const prefix = '/api/v1/portal/';
function files(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap(e => e.isDirectory() ? files(path.join(dir, e.name)) : [path.join(dir, e.name)]);
}
function inspect(source, name, allowTransport = false) {
  const failures = [];
  const ast = ts.createSourceFile(name, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
  function visit(node) {
    if (ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier)) {
      const target = node.moduleSpecifier.text;
      if (target.includes('apiClient') || target.includes('/modules/') || target.endsWith('/app/auth')) failures.push('admin import');
    }
    if (ts.isCallExpression(node)) {
      const callee = ts.isIdentifier(node.expression) ? node.expression.text :
        ts.isPropertyAccessExpression(node.expression) ? node.expression.name.text : '';
      if (callee === 'request' || callee === 'fetch') {
        const arg = node.arguments[0];
        const head = arg && (ts.isStringLiteral(arg) || ts.isNoSubstitutionTemplateLiteral(arg)) ? arg.text :
          arg && ts.isTemplateExpression(arg) ? arg.head.text : '';
        // Only the guarded, typed transport in api.ts may forward its path.
        let owner = node.parent;
        while (owner && !ts.isFunctionDeclaration(owner)) owner = owner.parent;
        const transport = allowTransport && callee === 'fetch' && arg && ts.isIdentifier(arg) && arg.text === 'path' &&
          owner?.name?.text === 'request';
        if (!head.startsWith(prefix) && !transport) failures.push(callee + ' outside portal');
      }
    }
    ts.forEachChild(node, visit);
  }
  visit(ast);
  return failures;
}

for (const file of files(root).filter(f => /\.(ts|vue)$/.test(f))) {
  const text = readFileSync(file, 'utf8');
  const allowTransport = path.basename(file) === 'api.ts';
  if (file.endsWith('.vue')) {
    const { descriptor } = parse(text);
    for (const script of [descriptor.script, descriptor.scriptSetup].filter(Boolean)) {
      assert.deepEqual(inspect(script.content, file), [], file);
    }
    assert.doesNotMatch(descriptor.template?.content || '', /\b(?:request|fetch)\s*\(/, file + ' template network call');
  } else assert.deepEqual(inspect(text, file, allowTransport), [], file);
  assert.doesNotMatch(text, /(?:clash|sing-box|shadowrocket):\/\//, file + ' unverified import scheme');
}
console.log('PASS A7.1 all portal request/fetch targets are portal-only; no admin client or import links');
for (const bad of [
  "request('/api/v1/users')", "fetch('/api/v1/auth/session')",
  'request(path)', "window.fetch('/api/v1/nodes')",
  'request(`/api/v1/${module}/me`)', "import { request } from '../app/apiClient'"
]) assert.ok(inspect(bad, 'negative.ts').length, 'scanner missed: ' + bad);
assert.deepEqual(inspect("request('/api/v1/portal/me')", 'positive.ts'), []);
console.log('PASS boundary scanner refuses admin, dynamic and qualified-fetch negative fixtures');

const source = readFileSync(path.join(root, 'api.ts'), 'utf8');
assert.match(source, /if \(!path\.startsWith\('\/api\/v1\/portal\/'\)\) throw/);
assert.match(source, /path: `\/api\/v1\/portal\/\$\{string\}`/);
const js = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText
  .replace("import { reactive } from 'vue';", 'const reactive = value => value;');
const api = await import('data:text/javascript;base64,' + Buffer.from(js).toString('base64'));
const originalFetch = globalThis.fetch;
const originalWindow = globalThis.window;
const calls = [], redirects = [];
globalThis.window = { location: { assign: value => redirects.push(value) } };
globalThis.fetch = async (url, options) => {
  assert.ok(url.startsWith(prefix), url);
  calls.push({ url, ...options });
  return new Response(JSON.stringify({ isAuthenticated: true, csrfToken: 'portal-csrf', currentUser: { id: 'self' }, items: [] }), { status: 200 });
};
try {
  await api.portalAPI.login('user', 'test-only');
  assert.equal(calls[0].headers['X-CSRF-Token'], undefined);
  await api.portalAPI.session();
  await api.portalAPI.me(); await api.portalAPI.nodes(); await api.portalAPI.subscription();
  await api.portalAPI.traffic('24h'); await api.portalAPI.traffic('7d'); await api.portalAPI.traffic('30d');
  await api.portalAPI.reset(); await api.portalAPI.password('test-only', 'changed-test-only');
  await api.portalAPI.logout();
  assert.equal(calls.length, 11);
  for (const call of calls) {
    assert.equal(call.credentials, 'same-origin');
    assert.equal(call.cache, 'no-store');
    if (call.method !== 'GET' && !call.url.endsWith('/auth/login')) assert.equal(call.headers['X-CSRF-Token'], 'portal-csrf');
    assert.ok(!call.url.includes('userId='));
  }
  assert.equal(api.portalState.session, null);
  globalThis.fetch = async () => new Response('{"error":{"code":"unauthorized"}}', { status: 401 });
  await assert.rejects(api.portalAPI.me());
  assert.deepEqual(redirects, ['/portal/login']);
  // A wrong current password is a 401 from a live session: report it, stay signed in.
  api.portalState.session = { isAuthenticated: true, csrfToken: 'portal-csrf' };
  await assert.rejects(api.portalAPI.password('wrong-test-only', 'changed-test-only'), /当前密码错误/);
  assert.deepEqual(redirects, ['/portal/login']);
  assert.ok(api.portalState.session);
  api.portalState.session = null;
  console.log('PASS portal login/session/read/reset/password/logout use separate CSRF and expire to portal login');
} finally { globalThis.fetch = originalFetch; globalThis.window = originalWindow; }
const guardSource = readFileSync(path.join(root, 'guard.ts'), 'utf8');
const stubURL = 'data:text/javascript;base64,' + Buffer.from('export const portalAPI = { session: () => globalThis.portalSessionProbe() };').toString('base64');
const guardJS = ts.transpileModule(guardSource, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText
  .replace("from './api'", 'from ' + JSON.stringify(stubURL));
const { portalGuard } = await import('data:text/javascript;base64,' + Buffer.from(guardJS).toString('base64'));
let probes = 0;
globalThis.portalSessionProbe = async () => { probes++; throw new Error('unauthorized'); };
try {
  assert.equal(await portalGuard({ path: '/portal/login' }), true);
  assert.equal(probes, 0);
  assert.deepEqual(await portalGuard({ path: '/portal/overview' }), { path: '/portal/login' });
  globalThis.portalSessionProbe = async () => ({ isAuthenticated: true });
  assert.equal(await portalGuard({ path: '/portal/account' }), true);
  console.log('PASS portal guard admits login and checks its own session for protected pages');
} finally { delete globalThis.portalSessionProbe; }
const mainSource = readFileSync(path.join(root, '../main.ts'), 'utf8');
assert.match(mainSource, /authenticated && !path\.startsWith\('\/portal\/'\)/);
assert.match(mainSource, /if \(!router\.currentRoute\.value\.path\.startsWith\('\/portal\/'\)\)/);
console.log('PASS portal routes disconnect admin SSE and ignore admin-login redirects');

// A nested <template> without v-if/v-else/v-for/v-slot renders as an inert
// native <template>, hiding its children (the viewMode removal left three).
function bareTemplates(node, out = []) {
  for (const child of node.children || []) {
    if (child.type === 1 && child.tag === 'template' && child.props.length === 0) out.push(child.loc.start.line);
    bareTemplates(child, out);
  }
  return out;
}
const bare = files(path.resolve(root, '..')).filter(file => file.endsWith('.vue')).flatMap(file => {
  const { descriptor } = parse(readFileSync(file, 'utf8'), { filename: file });
  return descriptor.template ? bareTemplates(descriptor.template.ast).map(line => `${path.relative(root, file)}:${line}`) : [];
});
assert.deepEqual(bare, []);
assert.deepEqual(bareTemplates(parse('<template><div><template><b/></template></div></template>').descriptor.template.ast), [1]);
console.log('PASS no Vue page wraps content in a directive-less <template>');

// MainLayout renders pages inside <transition mode="out-in">. A routed page
// whose template has several roots (or a root v-if) never finishes leaving,
// so the next page never mounts and the screen stays blank until a refresh.
function rootProblem(source) {
  const ast = parse(source).descriptor.template?.ast;
  if (!ast) return 'no template';
  const kids = ast.children.filter(child => !(child.type === 2 && !child.content.trim()) && child.type !== 3);
  if (kids.length !== 1 || kids[0].type !== 1 || kids[0].tag === 'template') return `${kids.length} root nodes`;
  if (kids[0].props.some(prop => prop.type === 7 && ['if', 'else-if', 'else', 'for'].includes(prop.name))) return 'conditional root';
  return '';
}
const src = path.resolve(root, '..');
const routerSource = readFileSync(path.join(src, 'app/router.ts'), 'utf8');
const routed = [...new Set([...routerSource.matchAll(/import\('\.\.\/([^']+\.vue)'\)/g)].map(match => match[1]))];
assert.ok(routed.length > 20);
const multiRoot = routed.map(file => [file, rootProblem(readFileSync(path.join(src, file), 'utf8'))]).filter(([, problem]) => problem);
assert.deepEqual(multiRoot, []);
assert.equal(rootProblem('<template><A /><B v-if="x" /></template>'), '2 root nodes');
assert.equal(rootProblem('<!-- c --><template>\n  <div />\n</template>'), '');
console.log('PASS every routed page has exactly one element root (out-in transitions)');
console.log('All portal self-tests passed');
