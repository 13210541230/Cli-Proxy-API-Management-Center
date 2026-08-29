import { beforeEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from './client';
import { normalizePluginList, pluginsApi } from './plugins';

vi.mock('./client', () => ({
  apiClient: {
    get: vi.fn(),
    patch: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

describe('pluginsApi', () => {
  beforeEach(() => vi.clearAllMocks());

  it('normalizes CPA plugin metadata and accepts camelCase compatibility fields', () => {
    const response = normalizePluginList({
      pluginsEnabled: true,
      plugins: [{
        id: 'demo/plugin',
        registered: true,
        enabled: true,
        effectiveEnabled: true,
        metadata: { name: 'Demo', version: '1.0.0' },
        configFields: [{ name: 'enabled_mode', type: 'enum', enumValues: ['safe', 'fast'] }],
        menus: [{ path: '/v0/resource/plugins/demo/page', menu: 'Workspace' }],
      }],
    });

    expect(response).toEqual({
      plugins_enabled: true,
      plugins: [{
        id: 'demo/plugin',
        registered: true,
        enabled: true,
        effective_enabled: true,
        configured: false,
        supports_oauth: false,
        path: undefined,
        oauth_provider: undefined,
        logo: undefined,
        config_fields: [{ name: 'enabled_mode', type: 'enum', enum_values: ['safe', 'fast'], description: undefined }],
        menus: [{ path: '/v0/resource/plugins/demo/page', menu: 'Workspace', description: undefined }],
        metadata: {
          name: 'Demo',
          version: '1.0.0',
          author: undefined,
          github_repository: undefined,
          logo: undefined,
          config_fields: [],
        },
      }],
    });
  });

  it('uses the generic CPA management endpoints for plugin lifecycle and config operations', async () => {
    vi.mocked(apiClient.patch).mockResolvedValue({});
    vi.mocked(apiClient.put).mockResolvedValue({});
    vi.mocked(apiClient.delete).mockResolvedValue({});
    vi.mocked(apiClient.get).mockResolvedValue({ mode: 'safe' });

    await pluginsApi.updateEnabled('demo/plugin', false);
    await pluginsApi.getConfig('demo/plugin');
    await pluginsApi.putConfig('demo/plugin', { mode: 'fast' });
    await pluginsApi.patchConfig('demo/plugin', { enabled: true });
    await pluginsApi.deletePlugin('demo/plugin');

    expect(apiClient.patch).toHaveBeenNthCalledWith(1, '/plugins/demo%2Fplugin/enabled', { enabled: false });
    expect(apiClient.get).toHaveBeenCalledWith('/plugins/demo%2Fplugin/config');
    expect(apiClient.put).toHaveBeenCalledWith('/plugins/demo%2Fplugin/config', { mode: 'fast' });
    expect(apiClient.patch).toHaveBeenNthCalledWith(2, '/plugins/demo%2Fplugin/config', { enabled: true });
    expect(apiClient.delete).toHaveBeenCalledWith('/plugins/demo%2Fplugin');
  });
});
