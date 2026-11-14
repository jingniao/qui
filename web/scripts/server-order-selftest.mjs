import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const source = readFileSync(new URL('../src/modules/nodes/serverOrder.ts', import.meta.url), 'utf8');
const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const order = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);

const nodes = [{ id: 'a' }, { id: 'b', tags: ['香港', 'CN2'] }, { id: 'c', tags: ['CN2'] }, { id: 'd' }, { id: 'x', tags: ['CN2'], deletedAt: '2026-01-01' }];

// Saved order first, unknown (new) servers after in API order, deleted IDs skipped.
assert.deepEqual(order.applyServerOrder(nodes.slice(0, 4), ['c', 'gone', 'a']).map(n => n.id), ['c', 'a', 'b', 'd']);
assert.deepEqual(order.applyServerOrder(nodes.slice(0, 2), []).map(n => n.id), ['a', 'b']);

assert.deepEqual(order.moveServer(['a', 'b', 'c'], 'b', -1), ['b', 'a', 'c']);
assert.deepEqual(order.moveServer(['a', 'b', 'c'], 'c', 1), ['a', 'b', 'c']);
assert.deepEqual(order.moveServer(['a', 'b', 'c'], 'a', -1), ['a', 'b', 'c']);
assert.deepEqual(order.reorderServer(['a', 'b', 'c', 'd'], 'a', 'c'), ['b', 'c', 'a', 'd']);
assert.deepEqual(order.reorderServer(['a', 'b', 'c', 'd'], 'd', 'b'), ['a', 'd', 'b', 'c']);

const options = order.tagOptions(nodes);
assert.deepEqual(Object.fromEntries(options.tags.map(t => [t.tag, t.count])), { CN2: 2, 香港: 1 });
assert.equal(options.untagged, 2);
assert.equal(order.matchesTag(nodes[0], order.UNTAGGED), true);
assert.equal(order.matchesTag(nodes[1], order.UNTAGGED), false);
assert.equal(order.matchesTag(nodes[2], 'CN2'), true);
assert.equal(order.matchesTag(nodes[2], '香港'), false);
assert.equal(order.matchesTag(nodes[2], ''), true);

assert.deepEqual(order.addTag(['CN2'], ' 香港 ').tags, ['CN2', '香港']);
assert.deepEqual(order.addTag(['CN2'], 'cn2').tags, ['CN2']);
assert.ok(order.addTag([], 'a,b').error);
assert.ok(order.addTag([], '一二三四五六七八九十一二三四五六七八九十一').error);
assert.ok(order.addTag(Array.from({ length: 10 }, (_, i) => String(i)), 'new').error);

console.log('PASS server order and tags: saved order, moves, drag reorder, tag counts and filters, tag input rules');
