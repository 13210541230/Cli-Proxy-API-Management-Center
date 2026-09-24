import { beforeEach, describe, expect, it, vi } from 'vitest';

const { modelsApiMock, quotaPauseApiMock } = vi.hoisted(() => ({
  modelsApiMock: {
    fetchModels: vi.fn(),
  },
  quotaPauseApiMock: {
    listPaused: vi.fn(),
  },
}));

vi.mock('@/services/api/models', () => ({
  modelsApi: modelsApiMock,
}));

vi.mock('@/services/api/quotaPause', () => ({
  quotaPauseApi: quotaPauseApiMock,
}));

import { quotaKeyHash } from '@/utils/apiKeyHash';
import { useModelsStore } from './useModelsStore';

describe('useModelsStore', () => {
  beforeEach(() => {
    modelsApiMock.fetchModels.mockReset();
    quotaPauseApiMock.listPaused.mockReset();
    quotaPauseApiMock.listPaused.mockResolvedValue({ entries: [] });
    useModelsStore.setState({ models: [], loading: false, error: null, cache: null });
  });

  it('tries an unpaused API key before paused keys', async () => {
    const pausedKey = 'paused-key';
    const activeKey = 'active-key';
    quotaPauseApiMock.listPaused.mockResolvedValue({
      entries: [{ key_hash: quotaKeyHash(pausedKey) }],
    });
    modelsApiMock.fetchModels.mockResolvedValue([{ name: 'gpt-5' }]);

    const models = await useModelsStore
      .getState()
      .fetchModelsWithApiKeys('http://127.0.0.1:8317', [pausedKey, activeKey], true);

    expect(models).toEqual([{ name: 'gpt-5' }]);
    expect(modelsApiMock.fetchModels).toHaveBeenCalledWith(
      'http://127.0.0.1:8317',
      activeKey,
    );
  });

  it('tries the next key when a candidate returns 429', async () => {
    modelsApiMock.fetchModels
      .mockRejectedValueOnce({ response: { status: 429 } })
      .mockResolvedValueOnce([{ name: 'gpt-5' }]);

    const models = await useModelsStore
      .getState()
      .fetchModelsWithApiKeys('http://127.0.0.1:8317', ['first-key', 'second-key'], true);

    expect(models).toEqual([{ name: 'gpt-5' }]);
    expect(modelsApiMock.fetchModels).toHaveBeenNthCalledWith(
      1,
      'http://127.0.0.1:8317',
      'first-key',
    );
    expect(modelsApiMock.fetchModels).toHaveBeenNthCalledWith(
      2,
      'http://127.0.0.1:8317',
      'second-key',
    );
  });
});
