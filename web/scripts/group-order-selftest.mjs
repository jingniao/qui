import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const source = readFileSync(new URL('../src/modules/routing/groupOrder.ts', import.meta.url), 'utf8');
const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const { recommendGroupOrder } = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
const group = (name, type = 'select', proxies = '') => ({ name, type, proxies });
const names = groups => groups.map(item => item.name);
const a = group('a'), b = group('b'), c = group('c', 'url-test', 'a,b');
assert.deepEqual(names(recommendGroupOrder([b, a, c], {}, [])), ['b', 'c', 'a']);
assert.deepEqual(recommendGroupOrder([], {}, []), []);
assert.deepEqual(names(recommendGroupOrder([c], {}, [])), ['c']);
const groups = [group('🇯🇵 日本', 'select', '🚀 节点选择'), group('AI', 'select', '🚀 节点选择'),
  group('🐟 漏网之鱼', 'select', '🚀 节点选择'), group('♻️ 自动选择', 'url-test'),
  group('🇺🇸 美国'), group('视频', 'select', '🚀 节点选择'), group('🚀 节点选择', 'select', '♻️ 自动选择')];
const categories = { '🇯🇵 日本': 'region', '🇺🇸 美国': 'region', AI: 'service' };
const rules = [{ type: 'MATCH', target: '🐟 漏网之鱼' }];
const before = JSON.stringify(groups);
const result = recommendGroupOrder(groups, categories, rules);
assert.deepEqual(names(result), ['🚀 节点选择', '♻️ 自动选择', 'AI', '视频', '🇯🇵 日本', '🇺🇸 美国', '🐟 漏网之鱼']);
assert.deepEqual(recommendGroupOrder(result, categories, rules), result);
assert.equal(JSON.stringify(groups), before);
assert.ok(result.every(item => groups.includes(item)));
console.log('PASS group order: ties, missing tiers, stable recommended order, seven groups, unchanged contents');
