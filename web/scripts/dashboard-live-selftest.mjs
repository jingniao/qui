import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const source = readFileSync(new URL('../src/modules/dashboard/liveSummary.ts', import.meta.url), 'utf8');
const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const { liveSummary } = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);

const base = { activeConnections: 128, onlineUsers: 3, todayBlocked: 42, measuredNodes: 6, expectedNodes: 6 };
assert.deepEqual(liveSummary(base), { connections: '128', onlineUsers: '3', blocked: '42', partial: false });
// Measured zero stays zero; it is not "unknown".
assert.deepEqual(liveSummary({ ...base, activeConnections: 0, onlineUsers: 0, todayBlocked: 0 }), { connections: '0', onlineUsers: '0', blocked: '0', partial: false });
// A partial measurement is never shown as a number.
assert.deepEqual(liveSummary({ ...base, activeConnections: null, onlineUsers: null, measuredNodes: 4 }), { connections: '—', onlineUsers: '—', blocked: '42', partial: true });

const page = readFileSync(new URL('../src/modules/dashboard/pages/DashboardPage.vue', import.meta.url), 'utf8');
assert.match(page, /当前连接[\s\S]*在线用户[\s\S]*今日阻断[\s\S]*部分节点未测/);
console.log('PASS dashboard live summary: values, measured zero, partial measurement shown as — with notice');
