import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const source = readFileSync(new URL('../src/modules/proxy/routeSummary.ts', import.meta.url), 'utf8');
const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const { routeSummaryLabel, routeSummaryPath, routeSummaryLink } = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);

assert.equal(routeSummaryLabel({ type: 'direct', path: [] }), '直连');
assert.equal(routeSummaryLabel({ type: 'tunnel', path: ['A', 'B'] }), '隧道转发');
assert.equal(routeSummaryPath({ type: 'tunnel', path: ['A', 'B'] }), 'A → B');
assert.equal(routeSummaryLink({ type: 'tunnel', path: ['A', 'B'] }), undefined);
assert.equal(routeSummaryLabel({ type: 'custom', path: [] }), '自定义出站');
const relay = Object.freeze({ type: 'relay', schemeId: 'scheme/a', path: Object.freeze(['A', 'M', 'B']) });
assert.equal(routeSummaryLabel(relay), '中转');
assert.equal(routeSummaryPath(relay), 'A → M → B');
assert.deepEqual(routeSummaryLink(relay), { path: '/proxy/relay', query: { schemeId: 'scheme/a' } });
assert.equal(routeSummaryLink({ type: 'relay', path: ['A'] }), undefined);
assert.equal(routeSummaryPath({ type: 'custom', path: [] }), '');
assert.equal(routeSummaryPath({ type: 'direct', path: ['A'] }), '');
assert.equal(routeSummaryLink({ type: 'direct', path: [] }), undefined);
console.log('PASS route summary: direct/custom/relay labels, multi-hop path, detached entry, scheme link, source data unchanged');
