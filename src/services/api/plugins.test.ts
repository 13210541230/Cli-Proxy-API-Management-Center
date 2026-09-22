import { beforeEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from './client';
import {
  nestPluginConfigKeys,
  normalizePluginList,
  normalizePluginStoreList,
  pluginsApi,
  readPluginConfigValue,
} from './plugins';

vi.mock('./client', () => ({
  apiClient: {
    get: vi.fn(),
    patch: vi.fn(),
    put: vi.fn(),
    post: vi.fn(),
    delete: vi.fn(),
  },
}));

describe('pluginsApi', () => {
  beforeEach(() => vi.clearAllMocks());

  it('normalizes CPA plugin metadata and accepts camelCase compatibility fields', () => {
    const response = normalizePluginList({
      pluginsEnabled: true,
      plugins: [
        {
          id: 'demo/plugin',
          registered: true,
          enabled: true,
          effectiveEnabled: true,
          metadata: { name: 'Demo', version: '1.0.0' },
          configFields: [{ name: 'enabled_mode', type: 'enum', enumValues: ['safe', 'fast'] }],
          menus: [{ path: '/v0/resource/plugins/demo/page', menu: 'Workspace' }],
        },
      ],
    });

    expect(response).toEqual({
      plugins_enabled: true,
      plugins: [
        {
          id: 'demo/plugin',
          registered: true,
          enabled: true,
          effective_enabled: true,
          configured: false,
          supports_oauth: false,
          path: undefined,
          oauth_provider: undefined,
          logo: undefined,
          config_fields: [
            {
              name: 'enabled_mode',
              type: 'enum',
              enum_values: ['safe', 'fast'],
              description: undefined,
            },
          ],
          menus: [
            { path: '/v0/resource/plugins/demo/page', menu: 'Workspace', description: undefined },
          ],
          metadata: {
            name: 'Demo',
            version: '1.0.0',
            author: undefined,
            github_repository: undefined,
            logo: undefined,
            config_fields: [],
          },
        },
      ],
    });
  });

  it('normalizes plugin store entries and compatibility field names', () => {
    expect(
      normalizePluginStoreList({
        pluginsEnabled: true,
        pluginsDir: 'plugins',
        sources: [{ id: 'official', name: 'Official', url: 'https://example.test/registry.json' }],
        plugins: [
          {
            id: 'demo',
            name: 'Demo Plugin',
            version: '1.2.0',
            installed: true,
            installedVersion: '1.0.0',
            updateAvailable: true,
            platforms: [{ goos: 'windows', goarch: 'amd64' }],
          },
        ],
      })
    ).toEqual({
      plugins_enabled: true,
      plugins_dir: 'plugins',
      sources: [{ id: 'official', name: 'Official', url: 'https://example.test/registry.json' }],
      source_errors: [],
      plugins: [
        {
          store_id: '',
          source_id: '',
          source_name: '',
          source_url: '',
          id: 'demo',
          name: 'Demo Plugin',
          description: '',
          author: '',
          version: '1.2.0',
          repository: '',
          install_type: '',
          auth_required: false,
          auth_configured: false,
          platforms: [{ goos: 'windows', goarch: 'amd64' }],
          logo: undefined,
          homepage: undefined,
          license: undefined,
          tags: [],
          installed: true,
          installed_version: '1.0.0',
          installed_source_id: undefined,
          install_source_status: undefined,
          path: undefined,
          configured: false,
          registered: false,
          enabled: false,
          effective_enabled: false,
          update_available: true,
        },
      ],
    });
  });

  it('uses the generic CPA management endpoints for plugin lifecycle and config operations', async () => {
    vi.mocked(apiClient.patch).mockResolvedValue({});
    vi.mocked(apiClient.put).mockResolvedValue({});
    vi.mocked(apiClient.delete).mockResolvedValue({});
    vi.mocked(apiClient.post).mockResolvedValue({ status: 'installed' });
    vi.mocked(apiClient.get).mockResolvedValue({ mode: 'safe' });

    await pluginsApi.updateEnabled('demo/plugin', false);
    await pluginsApi.getConfig('demo/plugin');
    await pluginsApi.putConfig('demo/plugin', { mode: 'fast' });
    await pluginsApi.patchConfig('demo/plugin', { enabled: true });
    await pluginsApi.deletePlugin('demo/plugin');
    await pluginsApi.listStore();
    await pluginsApi.installFromStore('demo/plugin', '1.2.3');

    expect(apiClient.patch).toHaveBeenNthCalledWith(1, '/plugins/demo%2Fplugin/enabled', {
      enabled: false,
    });
    expect(apiClient.get).toHaveBeenCalledWith('/plugins/demo%2Fplugin/config');
    expect(apiClient.put).toHaveBeenCalledWith('/plugins/demo%2Fplugin/config', { mode: 'fast' });
    expect(apiClient.patch).toHaveBeenNthCalledWith(2, '/plugins/demo%2Fplugin/config', {
      enabled: true,
    });
    expect(apiClient.delete).toHaveBeenCalledWith('/plugins/demo%2Fplugin');
    expect(apiClient.get).toHaveBeenCalledWith('/plugin-store', { timeout: 90_000 });
    expect(apiClient.post).toHaveBeenCalledWith(
      '/plugin-store/demo%2Fplugin/install',
      { version: '1.2.3' },
      { timeout: 180_000 }
    );
  });

  it('nests dotted ConfigField keys when saving plugin config', async () => {
    vi.mocked(apiClient.put).mockResolvedValue({});
    await pluginsApi.putConfig('enterprise-access-audit', {
      enabled: true,
      'account_pool.enabled': true,
      'account_pool.data_dir': 'pool-dir',
    });
    expect(apiClient.put).toHaveBeenCalledWith('/plugins/enterprise-access-audit/config', {
      enabled: true,
      account_pool: { enabled: true, data_dir: 'pool-dir' },
    });
  });

  it('nests dotted keys without clobbering an existing nested block', () => {
    expect(
      nestPluginConfigKeys({
        account_pool: { reserve_seconds: 7 },
        'account_pool.enabled': true,
      })
    ).toEqual({ account_pool: { reserve_seconds: 7, enabled: true } });
  });

  it('reads config values from both flat and nested shapes', () => {
    expect(readPluginConfigValue({ 'account_pool.enabled': true }, 'account_pool.enabled')).toBe(true);
    expect(
      readPluginConfigValue({ account_pool: { enabled: true } }, 'account_pool.enabled')
    ).toBe(true);
    expect(
      readPluginConfigValue({ account_pool: { enabled: true } }, 'account_pool.data_dir')
    ).toBeUndefined();
    expect(readPluginConfigValue({ enabled: true }, 'enabled')).toBe(true);
  });
});
