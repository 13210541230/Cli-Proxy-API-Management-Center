import { beforeEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from './client';
import { providersApi } from './providers';
import { providersWorkbenchApi } from './providerWorkbench';

vi.mock('./client', () => ({
  apiClient: { delete: vi.fn() },
}));

vi.mock('./providers', () => ({
  providersApi: {
    getGeminiKeys: vi.fn(),
    saveGeminiKeys: vi.fn(),
    getOpenAIProviders: vi.fn(),
    saveOpenAIProviders: vi.fn(),
  },
}));

describe('providersWorkbenchApi', () => {
  beforeEach(() => vi.clearAllMocks());

  it('adds a provider to the current list', async () => {
    const existing = [{ apiKey: 'old-key', baseUrl: 'https://one.example' }];
    vi.mocked(providersApi.getGeminiKeys).mockResolvedValue(existing);
    vi.mocked(providersApi.saveGeminiKeys).mockResolvedValue();
    const added = { apiKey: 'new-key', baseUrl: 'https://two.example' };

    await providersWorkbenchApi.createGeminiKey(added);

    expect(providersApi.saveGeminiKeys).toHaveBeenCalledWith([...existing, added]);
  });

  it('updates by API key and base URL and refuses stale selectors', async () => {
    const existing = [
      { apiKey: 'same-key', baseUrl: 'https://one.example' },
      { apiKey: 'same-key', baseUrl: 'https://two.example' },
    ];
    vi.mocked(providersApi.getGeminiKeys).mockResolvedValue(existing);
    vi.mocked(providersApi.saveGeminiKeys).mockResolvedValue();
    const updated = { apiKey: 'same-key', baseUrl: 'https://changed.example' };

    await providersWorkbenchApi.updateGeminiKey('same-key', 'https://two.example', updated);

    expect(providersApi.saveGeminiKeys).toHaveBeenCalledWith([existing[0], updated]);
    await expect(
      providersWorkbenchApi.updateGeminiKey('missing', undefined, updated)
    ).rejects.toThrow('Provider configuration changed');
  });

  it('deletes OpenAI entries by their stable source index', async () => {
    vi.mocked(apiClient.delete).mockResolvedValue({});

    await providersWorkbenchApi.deleteOpenAIProvider(3);

    expect(apiClient.delete).toHaveBeenCalledWith('/openai-compatibility?index=3');
  });
});
