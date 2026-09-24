import { beforeEach, describe, expect, it, vi } from 'vitest';

const { modelsApiMock } = vi.hoisted(() => ({
  modelsApiMock: {
    fetchModels: vi.fn(),
    fetchStaticModels: vi.fn(),
  },
}));

vi.mock('@/services/api/models', () => ({
  modelsApi: modelsApiMock,
}));

import { useModelsStore } from './useModelsStore';

describe('useModelsStore', () => {
  beforeEach(() => {
    modelsApiMock.fetchModels.mockReset();
    modelsApiMock.fetchStaticModels.mockReset();
    useModelsStore.setState({ models: [], loading: false, error: null, cache: null });
  });

  it('falls back to the static catalog when the live model endpoint is rate limited', async () => {
    modelsApiMock.fetchModels.mockRejectedValue({ response: { status: 429 } });
    modelsApiMock.fetchStaticModels.mockResolvedValue([{ name: 'gpt-5' }]);

    const models = await useModelsStore.getState().fetchModels('http://127.0.0.1:8317', 'client-key', true);

    expect(models).toEqual([{ name: 'gpt-5' }]);
    expect(useModelsStore.getState().error).toBeNull();
    expect(modelsApiMock.fetchStaticModels).toHaveBeenCalledOnce();
  });
});
