// Synthetic-only integration cases; refuses to operate on the single-account live profile.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { spawnSync } from 'node:child_process';
const profile = 'D:/C_projects/CLIProxyAPI-Suite_7.3.12_windows_amd64/modeltrace-validation/local';
const config = fs.readFileSync(`${profile}/config.yaml`, 'utf8');
assert(config.includes('http://127.0.0.1:18328'), 'Synthetic upstream must be explicitly configured.');
const key = fs.readFileSync(`${profile}/management-key.txt`, 'utf8').trim();
const base = 'http://127.0.0.1:18327/v0/management';
async function api(route, method = 'GET', body) {
  const response = await fetch(base + route, { method, headers: { Authorization: `Bearer ${key}`, 'Content-Type': 'application/json' }, body: body && JSON.stringify(body) });
  const value = await response.json();
  assert(response.ok, `Management returned ${response.status}.`);
  return value;
}
const { files } = await api('/auth-files');
assert(files.some((file) => file.name === 'codex-isolated-a.json'));
assert(files.some((file) => file.name === 'codex-isolated-b.json'));
const scenarios = [
  { mode: 'partial', target: 'a', model: 'gpt-5.5', status: 'partial', accepted: 2 },
  { mode: 'failed', target: 'b', model: 'gpt-5.5', status: 'failed', accepted: 0 },
];
for (const scenario of scenarios) {
  const control = await fetch('http://127.0.0.1:18328/fixture', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ reset: true, upstream_mode: scenario.mode }) });
  assert(control.ok);
  const target = files.find((file) => file.name === `codex-isolated-${scenario.target}.json`);
  await api('/auth-files/modeltrace', 'POST', { auth_index: target.auth_index, model: scenario.model });
  const deadline = Date.now() + 15000;
  let state;
  do {
    state = (await api('/auth-files/modeltrace')).states[target.auth_index];
    if (!state.running) break;
    await new Promise((resolve) => setTimeout(resolve, 100));
  } while (Date.now() < deadline);
  assert(!state.running, 'Synthetic run exceeded its bounded deadline.');
  assert.equal(state.latest.status, scenario.status);
  assert.equal(state.latest.verdict, 'inconclusive');
  const { record } = await api(`/auth-files/modeltrace/record?${new URLSearchParams({ auth_index: target.auth_index, id: state.latest.id })}`);
  assert.equal(record.samples.filter((sample) => sample.accepted).length, scenario.accepted);
  const fixture = await (await fetch('http://127.0.0.1:18328/fixture')).json();
  assert.equal(fixture.upstreamCalls, 3, 'Must not automatically retry challenges.');
  assert(fixture.requests.every((request) => request.credential_label === scenario.target), 'Must not fall back to another credential.');
  const result = spawnSync(process.execPath, ['scripts/verify-modeltrace-runtime.mjs', 'local', scenario.status, scenario.target], { encoding: 'utf8' });
  assert.equal(result.status, 0, result.stderr);
  process.stdout.write(result.stdout);
}
console.log('Synthetic partial/failure/no-fallback/no-retry cases: PASS.');
