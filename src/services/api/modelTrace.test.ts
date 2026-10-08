import { beforeEach, describe, expect, it, vi } from 'vitest';

const { client } = vi.hoisted(() => ({
  client: { get: vi.fn(), post: vi.fn(), delete: vi.fn() },
}));
vi.mock('./client', () => ({ apiClient: client }));
import { modelTraceApi } from './modelTrace';

beforeEach(() => vi.resetAllMocks());

describe('native ModelTrace API', () => {
  it('loads one batched state with a cancellable subscription', async () => {
    const signal = new AbortController().signal;
    const state = { bank_revision: 'bank', models: [], states: {} };
    client.get.mockResolvedValue(state);
    expect(await modelTraceApi.state(signal)).toEqual(state);
    expect(client.get).toHaveBeenCalledWith('/auth-files/modeltrace', { signal });
  });

  it('starts only the selected credential without tokens or batch flags', async () => {
    const run = { id: 'run-a', model: 'gpt-5.5', done: 0, total: 3 };
    client.post.mockResolvedValue({ run });
    expect(await modelTraceApi.start('account-a', 'gpt-5.5')).toEqual(run);
    expect(client.post).toHaveBeenCalledWith('/auth-files/modeltrace', {
      auth_index: 'account-a', model: 'gpt-5.5',
    }, { signal: undefined });
  });

  it('cancels by auth index using query parameters', async () => {
    client.delete.mockResolvedValue({ cancelled: true });
    await modelTraceApi.cancel('account/a?x');
    expect(client.delete).toHaveBeenCalledWith('/auth-files/modeltrace', {
      params: { auth_index: 'account/a?x' }, signal: undefined,
    });
  });

  it('fetches evidence for a specific record and credential', async () => {
    const record = { id: 'record-a', status: 'completed' };
    const signal = new AbortController().signal;
    client.get.mockResolvedValue({ record });
    expect(await modelTraceApi.record('account-a', 'record-a', signal)).toEqual(record);
    expect(client.get).toHaveBeenCalledWith('/auth-files/modeltrace/record', {
      params: { auth_index: 'account-a', id: 'record-a' }, signal,
    });
  });

  it('propagates capacity and storage errors rather than returning fake success', async () => {
    client.post.mockRejectedValue(new Error('busy'));
    await expect(modelTraceApi.start('a', 'gpt-5.5')).rejects.toThrow('busy');
  });
});
