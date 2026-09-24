import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('./client', () => ({
  apiClient: {
    get: vi.fn(),
  },
}));

import { apiClient } from './client';
import { modelsApi } from './models';

describe('modelsApi.fetchStaticModels', () => {
  beforeEach(() => {
    vi.mocked(apiClient.get).mockReset();
  });

  it('aggregates static model definitions across provider channels', async () => {
    vi.mocked(apiClient.get).mockImplementation(async (url) => {
      if (url.endsWith('/codex')) {
        return { models: [{ id: 'gpt-5' }, { id: 'shared-model' }] } as never;
      }
      if (url.endsWith('/claude')) {
        return { models: [{ id: 'claude-sonnet' }, { id: 'shared-model' }] } as never;
      }
      return { models: [] } as never;
    });

    const models = await modelsApi.fetchStaticModels();

    expect(models.map((model) => model.name)).toEqual(['claude-sonnet', 'shared-model', 'gpt-5']);
    expect(apiClient.get).toHaveBeenCalledTimes(11);
  });
});
