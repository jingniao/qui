import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

// The pure helpers of the certificate settings card; the module's request wrapper is
// replaced by a stub because the helpers never call it.
const source = readFileSync(new URL('../src/modules/system/certificatesApi.ts', import.meta.url), 'utf8').replace(/import \{ request \}[^\n]*\n/, 'const request = () => { throw new Error("unused"); };\n');
const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const api = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);

assert.equal(api.deployWindowError(3, 6), '');
assert.equal(api.deployWindowError(22, 4), '', 'a window may wrap midnight');
assert.match(api.deployWindowError(5, 5), /不能相同/);
for (const bad of [[-1, 4], [3, 24], [1.5, 4], [NaN, 4]]) assert.match(api.deployWindowError(...bad), /0～23/);
assert.deepEqual(api.parseDomainLines(' Node.Example.com \r\n\n*.Example.com\n'), ['node.example.com', '*.example.com']);
assert.deepEqual(api.parseDomainLines(''), []);
assert.equal(api.certificatesInProgress([{ status: 'valid' }, { status: 'failed' }]), false);
assert.equal(api.certificatesInProgress([{ status: 'valid' }, { status: 'issuing' }]), true);
assert.equal(api.certificatesInProgress([{ status: 'pending' }]), true);
assert.equal(api.CERTIFICATE_STATUS_LABELS.issuing, '签发中');
console.log('PASS CERTIFICATES: deploy window, domain lines, polling condition, status labels');
