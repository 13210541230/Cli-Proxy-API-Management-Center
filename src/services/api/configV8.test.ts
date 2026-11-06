import { beforeEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from './client';
import { configV8Api } from './configV8';

vi.mock('./client', () => ({
  apiClient: {
    getRaw: vi.fn(),
    put: vi.fn(),
  },
}));

describe('configV8Api', () => {
  beforeEach(() => vi.clearAllMocks());

  it('reads the canonical v8 YAML view and its ETag from a v0 management base', async () => {
    vi.mocked(apiClient.getRaw).mockResolvedValue({
      data: 'config-version: 8\nserver: {port: 8317}\n',
      headers: { get: () => '"source-revision"' },
    } as never);

    await expect(configV8Api.fetchConfigYaml('https://cpa.example/prefix/v0/management')).resolves.toEqual({
      yaml: 'config-version: 8\nserver: {port: 8317}\n',
      etag: '"source-revision"',
    });
    expect(apiClient.getRaw).toHaveBeenCalledWith(
      'https://cpa.example/prefix/v8/management/config.yaml',
      expect.objectContaining({
        responseType: 'text',
        headers: { Accept: 'application/yaml, text/yaml, text/plain' },
      })
    );
  });

  it('replaces the v8 YAML document guarded by the preview ETag', async () => {
    vi.mocked(apiClient.put).mockResolvedValue({ status: 'ok' });
    const yaml = 'config-version: 8\nserver:\n  port: 9000\n';

    await configV8Api.replaceConfigYaml(
      'https://cpa.example/prefix/v0/management',
      yaml,
      '"source-revision"'
    );

    expect(apiClient.put).toHaveBeenCalledWith(
      'https://cpa.example/prefix/v8/management/config.yaml',
      yaml,
      {
        headers: {
          'Content-Type': 'application/yaml',
          Accept: 'application/json, text/plain, */*',
          'If-Match': '"source-revision"',
        },
      }
    );
  });
});
