// Read-only runtime assertions for the owned isolated CPA. Never starts a model request.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { createHash } from 'node:crypto';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const [mode = 'local', expected = 'inspect', label = 'a'] = process.argv.slice(2);
assert(['local', 'live'].includes(mode), 'Use an owned local or live profile.');
const root = 'D:/C_projects/CLIProxyAPI-Suite_7.3.12_windows_amd64/modeltrace-validation';
assert(fs.existsSync(path.join(root, '.modeltrace-validation-owner.json')), 'Ownership marker missing.');
const key = fs.readFileSync(path.join(root, mode, 'management-key.txt'), 'utf8').trim();
const endpoint = 'http://127.0.0.1:18327/v0/management';
async function get(route) {
  const response = await fetch(endpoint + route, { headers: { Authorization: `Bearer ${key}` } });
  assert(response.ok, `Read-only management request failed: ${response.status}`);
  return response.json();
}
const { files } = await get('/auth-files');
const candidates = files.filter((file) => file.type === 'codex' && !file.disabled);
if (mode === 'live') assert.equal(candidates.length, 1, 'Live scope must contain exactly one enabled Codex file.');
const target = mode === 'local'
  ? candidates.find((file) => file.name === `codex-isolated-${label}.json`)
  : candidates[0];
assert(target, 'Expected isolated credential missing.');
const state = await get('/auth-files/modeltrace');
const credential = state.states[target.auth_index] ?? {};
const latest = credential.latest;
const record = latest ? (await get(`/auth-files/modeltrace/record?${new URLSearchParams({
  auth_index: target.auth_index, id: latest.id,
})}`)).record : undefined;
const samples = record?.samples ?? [];
if (expected !== 'inspect') {
  if (expected === 'running') assert(credential.running, 'Expected a running task.');
  else {
    assert(!credential.running, 'Task should no longer be running.');
    assert.equal(record?.status, expected);
  }
}
if (record?.status === 'completed') {
  assert.equal(samples.length, 3);
  assert(samples.every((sample) => sample.accepted));
  assert.equal(record.attribution.used_outputs, 3);
  const expectedVerdict = record.attribution.probability < 0.8
    ? 'inconclusive' : record.attribution.prediction === 'gpt-5.6-luna' ? 'suspected' : 'consistent';
  assert.equal(record.verdict, expectedVerdict, 'Degradation verdict must follow the exact Luna policy.');
  assert.equal(latest.verdict, record.verdict, 'Card summary and evidence must use the same policy.');
  if (mode === 'local') {
    assert.equal(record.input_tokens, 42000);
    assert.equal(record.output_tokens, 2100);
  }
}
if (record && ['partial', 'failed', 'cancelled', 'interrupted'].includes(record.status)) {
  assert.equal(record.verdict, 'inconclusive', 'Incomplete execution must not imply degradation.');
}
const summary = {
  mode, credential: mode === 'local' ? target.name : 'single-enabled-live-codex',
  running: credential.running,
  record: record && {
    id: record.id, model: record.model, status: record.status, verdict: record.verdict,
    fingerprint_match: record.status === 'completed' && record.attribution?.used_outputs === 3 && record.attribution.probability >= 0.8
      ? record.attribution.prediction === record.model ? 'matched' : 'mismatched' : 'unknown_match',
    input_tokens: record.input_tokens, output_tokens: record.output_tokens,
    reasoning_tokens: record.reasoning_tokens, duration_ms: record.duration_ms,
    accepted_samples: samples.filter((sample) => sample.accepted).length,
    sample_counts: samples.map(({ expected_count, parsed_numbers, accepted }) => ({ expected_count, parsed_numbers, accepted })),
    attribution: record.attribution && {
      prediction: record.attribution.prediction, probability: record.attribution.probability,
      used_outputs: record.attribution.used_outputs,
    },
  },
};
if (mode === 'live') {
  const snapshot = JSON.parse(fs.readFileSync(path.join(root, mode, '.source-integrity.json'), 'utf8'));
  const hash = (file) => createHash('sha256').update(fs.readFileSync(file)).digest('hex');
  summary.source_integrity = {
    configuration_unchanged: hash(snapshot.config) === snapshot.config_sha256,
    credential_unchanged: hash(snapshot.auth) === snapshot.auth_sha256,
  };
  assert(Object.values(summary.source_integrity).every(Boolean), 'Source Suite changed since the isolated copy was prepared.');
}
if (mode === 'local') {
  const fixture = await (await fetch('http://127.0.0.1:18328/fixture')).json();
  summary.fixture_recent_requests = fixture.requests.filter((request) => request.credential_label);
  assert(summary.fixture_recent_requests.every((request) => ['a', 'b'].includes(request.credential_label)), 'Unexpected or disabled credential used.');
}
const project = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const output = path.join(project, '.ccg/plan/codex-quality-detection/artifacts/verification/frontend', `native-${mode}-${expected}-${mode === 'local' ? label : 'single'}.json`);
fs.mkdirSync(path.dirname(output), { recursive: true });
fs.writeFileSync(output, JSON.stringify(summary, null, 2));
console.log(JSON.stringify(summary, null, 2));
console.log('Runtime assertions: PASS. No model requests were started by this script.');
