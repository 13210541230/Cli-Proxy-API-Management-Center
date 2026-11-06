import { normalizeApiBase } from '@/utils/connection';
import { apiClient } from './client';

export interface ConfigV8Snapshot {
  yaml: string;
  etag: string;
}

const configV8Url = (apiBase: string, path: string): string =>
  `${normalizeApiBase(apiBase).replace(/\/+$/, '')}/v8/management${path}`;

export const configV8Api = {
  fetchConfigYaml: async (apiBase: string): Promise<ConfigV8Snapshot> => {
    const response = await apiClient.getRaw(configV8Url(apiBase, '/config.yaml'), {
      responseType: 'text',
      headers: { Accept: 'application/yaml, text/yaml, text/plain' },
    });
    const headerGetter = response.headers.get;
    const etag =
      typeof headerGetter === 'function'
        ? headerGetter.call(response.headers, 'etag')
        : response.headers['etag'];

    return {
      yaml: String(response.data ?? ''),
      etag: String(etag ?? ''),
    };
  },

  replaceConfigYaml: (
    apiBase: string,
    yaml: string,
    etag: string
  ): Promise<unknown> =>
    apiClient.put(configV8Url(apiBase, '/config.yaml'), yaml, {
      headers: {
        'Content-Type': 'application/yaml',
        Accept: 'application/json, text/plain, */*',
        'If-Match': etag,
      },
    }),
};
