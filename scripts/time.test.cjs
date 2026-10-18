const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require('../web/node_modules/typescript');

const source = fs.readFileSync(path.join(__dirname, '../web/src/app/time.ts'), 'utf8');
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText;
function load(timezone) {
  const exports = {};
  const authState = { timezone };
  vm.runInNewContext(compiled, { exports, require: name => {
    if (name !== './auth') throw new Error('Unexpected dependency');
    return { authState };
  }, Intl, Date });
  return exports;
}
test('expiry input uses configured timezone instead of host timezone', () => {
  const shanghai = load('Asia/Shanghai');
  assert.equal(shanghai.utcInput('2026-10-01T18:30'), '2026-10-01T10:30:00.000Z');
  assert.equal(shanghai.localInput('2026-10-01T10:30:00Z'), '2026-10-01T18:30');
  const tokyo = load('Asia/Tokyo');
  assert.equal(tokyo.utcInput('2026-10-01T18:30'), '2026-10-01T09:30:00.000Z');
});
test('daylight saving offsets and nonexistent local times', () => {
  const la = load('America/Los_Angeles');
  assert.equal(la.utcInput('2026-07-01T12:00'), '2026-07-01T19:00:00.000Z');
  assert.equal(la.utcInput('2026-01-01T12:00'), '2026-01-01T20:00:00.000Z');
  assert.throws(() => la.utcInput('2026-03-08T02:30'));
});
test('invalid dates and unlimited expiry', () => {
  const utc = load('UTC');
  assert.equal(utc.utcInput(''), '');
  assert.throws(() => utc.utcInput('2026-02-31T12:00'));
  assert.throws(() => utc.utcInput('not-a-time'));
  assert.equal(utc.localInput(''), '');
});
