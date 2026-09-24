import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('axios', () => ({
  default: {
    create: vi.fn(() => ({
      interceptors: {
        request: { use: vi.fn() },
        response: { use: vi.fn() },
      },
    })),
    get: vi.fn(),
  },
}));

import axios from 'axios';
import { modelsApi } from './models';

describe('modelsApi.fetchModels', () => {
  beforeEach(() => {
    vi.mocked(axios.get).mockReset();
  });

  it('fetches the live model list with the selected API key', async () => {
    vi.mocked(axios.get).mockResolvedValue({
      data: { data: [{ id: 'gpt-5' }] },
    } as never);

    const models = await modelsApi.fetchModels('http://127.0.0.1:8317', 'client-key');

    expect(models.map((model) => model.name)).toEqual(['gpt-5']);
    expect(axios.get).toHaveBeenCalledWith('http://127.0.0.1:8317/v1/models', {
      headers: { Authorization: 'Bearer client-key' },
    });
  });
});
