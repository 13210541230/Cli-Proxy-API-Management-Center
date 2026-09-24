import { beforeEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from './client';
import { providersApi } from './providers';

vi.mock('./client', () => ({
  apiClient: {
    get: vi.fn(),
    put: vi.fn(),
    patch: vi.fn(),
    post: vi.fn(),
    delete: vi.fn()
  }
}));

type Json = Record<string, unknown>;

const putPayload = (): Json[] => vi.mocked(apiClient.put).mock.calls[0]?.[1] as Json[];

describe('providersApi PUT preservation merge', () => {
  beforeEach(() => vi.clearAllMocks());

  it('keeps unmodeled OpenAI provider fields and nested entry weight on save', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({
      'openai-compatibility': [
        {
          name: 'local-openai',
          'base-url': 'http://localhost:8080/v1',
          'request-retry': 3,
          'support-prompt-cache-key': true,
          'api-key-entries': [{ 'api-key': 'public', weight: 2 }],
          models: [{ name: 'ling-3.0-flash-fin-free', alias: '' }]
        }
      ]
    });
    vi.mocked(apiClient.put).mockResolvedValue({});

    await providersApi.saveOpenAIProviders([
      {
        name: 'local-openai',
        baseUrl: 'http://localhost:9090/v1',
        apiKeyEntries: [{ apiKey: 'public' }],
        disableCooling: true,
        models: [{ name: 'ling-3.0-flash-fin-free', alias: '' }]
      }
    ]);

    expect(apiClient.put).toHaveBeenCalledTimes(1);
    const item = putPayload()[0];
    expect(item['base-url']).toBe('http://localhost:9090/v1'); // modeled edit applied
    expect(item['request-retry']).toBe(3); // unmodeled field survives
    expect(item['support-prompt-cache-key']).toBe(true); // unmodeled field survives
    expect(item['disable-cooling']).toBe(true); // modeled field emitted by serializer
    const entries = item['api-key-entries'] as Json[];
    expect(entries[0].weight).toBe(2); // unmodeled nested entry field survives
  });

  it('clears managed keys the serializer no longer emits', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({
      'openai-compatibility': [
        { name: 'p', 'base-url': 'http://old', prefix: 'team/', 'api-key-entries': [{ 'api-key': 'k' }] }
      ]
    });
    vi.mocked(apiClient.put).mockResolvedValue({});

    await providersApi.saveOpenAIProviders([
      { name: 'p', baseUrl: 'http://new', prefix: '', apiKeyEntries: [{ apiKey: 'k' }] }
    ]);

    const item = putPayload()[0];
    expect(item['base-url']).toBe('http://new');
    expect('prefix' in item).toBe(false); // cleared managed key removed
  });

  it('falls back to identity matching when the list length changes', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({
      'openai-compatibility': [
        { name: 'alpha', 'base-url': 'http://a', 'request-retry': 5, 'api-key-entries': [{ 'api-key': 'ka' }] },
        { name: 'beta', 'base-url': 'http://b', 'request-retry': 7, 'api-key-entries': [{ 'api-key': 'kb' }] }
      ]
    });
    vi.mocked(apiClient.put).mockResolvedValue({});

    await providersApi.saveOpenAIProviders([
      { name: 'beta', baseUrl: 'http://b2', apiKeyEntries: [{ apiKey: 'kb' }] }
    ]);

    const item = putPayload()[0];
    expect(item.name).toBe('beta');
    expect(item['base-url']).toBe('http://b2');
    expect(item['request-retry']).toBe(7); // matched by name, unmodeled field survives
  });

  it('preserves unmodeled model fields while applying workbench model edits', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({
      'gemini-api-key': [
        {
          'api-key': 'key',
          models: [{ name: 'gemini-test', alias: 'old', providerSpecific: { flag: true } }]
        }
      ]
    });
    vi.mocked(apiClient.put).mockResolvedValue({});

    await providersApi.saveGeminiKeys([
      {
        apiKey: 'key',
        models: [{ name: 'gemini-test', alias: 'new', thinking: { level: 'high' }, image: true }]
      }
    ]);

    const models = putPayload()[0].models as Json[];
    expect(models[0].alias).toBe('new');
    expect(models[0].image).toBe(true);
    expect(models[0].thinking).toEqual({ level: 'high' });
    expect(models[0].providerSpecific).toEqual({ flag: true });
  });
});
