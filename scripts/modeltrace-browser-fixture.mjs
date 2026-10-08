// Local-only UI fixture. It never forwards requests or calls a paid model.
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const port = Number(process.env.MODELTRACE_FIXTURE_PORT || 18328);
if (port === 18317) throw new Error('Production port 18317 must never be used for validation.');
const bank = [{ id: 'gpt-5.5', display_name: 'GPT 5.5' }, { id: 'gpt-5.4', display_name: 'GPT 5.4' }];
const files = [
  { name: 'codex-long-name-enterprise-shared-engineering-account@example.com.json', type: 'codex', provider: 'codex', auth_index: 'a', disabled: false, status: 'active' },
  { name: 'codex-previous-result.json', type: 'codex', provider: 'codex', auth_index: 'b', disabled: false, status: 'active' },
  { name: 'codex-disabled.json', type: 'codex', provider: 'codex', auth_index: 'c', disabled: true, status: 'disabled' },
  { name: 'claude-account.json', type: 'claude', provider: 'claude', auth_index: 'd', disabled: false },
];
const makeRecord = (id, verdict = 'consistent', status = 'completed') => ({
  id, time: new Date().toISOString(), model: 'gpt-5.5', status, verdict,
  bank_revision: 'fixture-bank-v1', duration_ms: 12500,
  input_tokens: 42000, output_tokens: 1800, reasoning_tokens: 900,
  attribution: { prediction: verdict === 'suspected' ? 'gpt-5.4' : 'gpt-5.5', probability: verdict === 'inconclusive' ? 0.54 : 0.96,
    used_outputs: status === 'partial' ? 1 : 3, family_prediction_name: 'GPT', family_probability: 0.99,
    results: [{ model: 'gpt-5.5', display_name: 'GPT 5.5', probability: 0.96 }, { model: 'gpt-5.4', display_name: 'GPT 5.4', probability: 0.04 }] },
  samples: [1, 2, 3].map((number) => ({ prompt: `Fixture challenge ${number}: select integers without tools.`, expected_count: 300, text: '21, 87, 164, 201, 355', parsed_numbers: 300, accepted: true })),
});
let states = { b: { latest: makeRecord('previous', 'suspected') } };
let polls = 0;
let hold = false;
let unavailable = false;
let upstreamMode = 'normal';
let upstreamCalls = 0;
const requests = [];
const bodyOf = async (req) => {
  let body = '';
  for await (const chunk of req) body += chunk;
  return body ? JSON.parse(body) : {};
};
const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, `http://127.0.0.1:${port}`);
  const send = (status, value) => {
    res.writeHead(status, { 'Content-Type': 'application/json', 'Access-Control-Allow-Origin': '*',
      'Access-Control-Allow-Headers': 'Authorization,Content-Type', 'Access-Control-Allow-Methods': 'GET,POST,DELETE,OPTIONS', 'X-CPA-VERSION': 'modeltrace-fixture' });
    res.end(JSON.stringify(value));
  };
  if (req.method === 'OPTIONS') return send(204, {});
  try {
    if (url.pathname === '/fixture') {
      if (req.method === 'GET') return send(200, { requests, states, upstreamMode, upstreamCalls });
      const body = await bodyOf(req);
      hold = Boolean(body.hold);
      unavailable = Boolean(body.unavailable);
      if (body.upstream_mode) upstreamMode = body.upstream_mode;
      if (body.reset) { states = { b: { latest: makeRecord('previous', 'suspected') } }; requests.length = 0; upstreamCalls = 0; }
      if (body.status) states.a = { latest: { ...makeRecord('scenario', body.verdict || 'inconclusive', body.status), ...(body.error ? { error: body.error } : {}) } };
      return send(200, { ok: true });
    }
    // Real isolated CPA can target this local Responses endpoint. No forwarding occurs.
    if (req.method === 'POST' && url.pathname.endsWith('/responses')) {
      const body = await bodyOf(req);
      const prompt = (body.input || []).filter((item) => item.role === 'user').slice(-1)
        .flatMap((item) => item.content || []).map((item) => item.text || '').join('\n');
      const match = prompt.match(/(\d{3})\s+个\s+1\s+到\s+355/);
      if (!match) throw new Error('The synthetic endpoint requires a ModelTrace integer challenge.');
      const count = Number(match[1]);
      const call = ++upstreamCalls;
      const tokenPrefix = 'Bearer isolated-synthetic-token-';
      const credentialLabel = (req.headers.authorization || '').startsWith(tokenPrefix)
        ? req.headers.authorization.slice(tokenPrefix.length) : 'unexpected';
      requests.push({ method: 'POST', path: url.pathname, model: body.model,
        credential_label: credentialLabel, expected_count: count });
      if (upstreamMode === 'failed' || (upstreamMode === 'partial' && call % 3 === 1)) {
        return send(503, { error: { message: 'Isolated synthetic upstream failure' } });
      }
      res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' });
      const id = `response-isolated-${call}`;
      res.write(`event: response.created\ndata: ${JSON.stringify({ type: 'response.created', response: { id, status: 'in_progress', model: body.model, output: [] } })}\n\n`);
      if (upstreamMode === 'hold') return;
      let seed = 12345 + call;
      const text = Array.from({ length: count }, () => {
        seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0;
        return String(1 + seed % 355);
      }).join(', ');
      res.write(`event: response.output_text.delta\ndata: ${JSON.stringify({ type: 'response.output_text.delta', output_index: 0, content_index: 0, delta: text })}\n\n`);
      const response = { id, object: 'response', status: 'completed', model: body.model,
        output: [{ id: `message-${call}`, type: 'message', role: 'assistant', status: 'completed',
          content: [{ type: 'output_text', text, annotations: [] }] }],
        usage: { input_tokens: 14000, output_tokens: 700, output_tokens_details: { reasoning_tokens: 0 } } };
      res.end(`event: response.completed\ndata: ${JSON.stringify({ type: 'response.completed', response })}\n\ndata: [DONE]\n\n`);
      return;
    }
    if (url.pathname.startsWith('/v0/management')) {
      requests.push({ method: req.method, path: url.pathname });
      const endpoint = url.pathname.replace('/v0/management', '');
      if (endpoint === '/auth-files/modeltrace') {
        if (unavailable) return send(404, { error: 'not supported' });
        if (req.method === 'POST') {
          const body = await bodyOf(req);
          requests.at(-1).body = body;
          if (body.auth_index === 'c') return send(409, { error: 'credential disabled' });
          const run = { id: 'fixture-run', model: body.model, phase: 'collecting', done: 0, total: 3, started_at: new Date().toISOString() };
          states[body.auth_index] = { ...states[body.auth_index], running: run };
          polls = 0;
          return send(202, { run });
        }
        if (req.method === 'DELETE') {
          const key = url.searchParams.get('auth_index');
          states[key] = { latest: makeRecord('cancelled', 'inconclusive', 'cancelled') };
          return send(200, { cancelled: true });
        }
        for (const [key, state] of Object.entries(states)) {
          if (!state.running || hold) continue;
          if (++polls < 3) state.running.done = polls;
          else states[key] = { latest: makeRecord('finished') };
        }
        return send(200, { bank_revision: 'fixture-bank-v1', models: bank, states });
      }
      if (endpoint === '/auth-files/modeltrace/record') {
        return send(200, { record: states[url.searchParams.get('auth_index')]?.latest });
      }
      if (endpoint === '/auth-files') return send(200, { files });
      if (endpoint === '/auth-files/models') return send(200, { models: [{ id: 'gpt-5.5' }, { id: 'custom-uncovered-model' }] });
      if (endpoint === '/config.yaml') { res.writeHead(200, { 'Content-Type': 'text/plain', 'Access-Control-Allow-Origin': '*' }); return res.end('port: 18328\n'); }
      if (endpoint === '/api-call') return send(200, { status_code: 200, body: JSON.stringify({ rate_limit: { primary_window: { used_percent: 15, reset_at: 1800000000 } } }) });
      if (endpoint === '/plugins') return send(200, { plugins: [] });
      if (endpoint === '/api-keys') return send(200, { 'api-keys': ['fixture-only'] });
      if (endpoint === '/usage') return send(200, { usage: {} });
      return send(200, {});
    }
    if (url.pathname === '/v1/models') return send(200, { data: bank.map((m) => ({ id: m.id, object: 'model' })) });
    const html = path.join(root, 'dist/index.html');
    if (!fs.existsSync(html)) return send(503, { error: 'Run npm run build first.' });
    res.writeHead(200, { 'Content-Type': 'text/html' });
    fs.createReadStream(html).pipe(res);
  } catch (error) { send(500, { error: error.message }); }
});
server.listen(port, '127.0.0.1', () => console.log(`ModelTrace UI fixture http://127.0.0.1:${port}`));
