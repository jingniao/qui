import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const source = readFileSync(new URL('../src/modules/proxy/userEntries.ts', import.meta.url), 'utf8');
const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const { isUserEntry } = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);

assert.equal(isUserEntry({ internalRelay: true }), false);
assert.equal(isUserEntry({ internalRelay: false }), true);
assert.equal(isUserEntry({}), true);

// Visibility here depends only on the entry role; existing status rules stay separate.
const entries = Object.freeze([
  Object.freeze({ id: 'user', internalRelay: false, enabled: true, subscriptionVisible: true }),
  Object.freeze({ id: 'internal', internalRelay: true, enabled: true, subscriptionVisible: true }),
  Object.freeze({ id: 'disabled', internalRelay: false, enabled: false, subscriptionVisible: false }),
  Object.freeze({ id: 'imported' }),
]);
assert.deepEqual(entries.filter(isUserEntry).map(entry => entry.id), ['user', 'disabled', 'imported']);
assert.equal(entries.length, 4);
assert.equal(entries[1].internalRelay, true);
assert.deepEqual([entries[1]].filter(isUserEntry), []);
assert.deepEqual([].filter(isUserEntry), []);

console.log('PASS user entries: internal listeners excluded, ordinary and imported entries preserved, empty results, source data unchanged');
