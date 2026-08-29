import { beforeEach, describe, expect, it, vi } from 'vitest';
import { pluginsApi } from '@/services/api/plugins';
import { usePluginStore } from './usePluginStore';

vi.mock('@/services/api/plugins', () => ({
  pluginsApi: {
    list: vi.fn(),
  },
}));

const listPlugins = vi.mocked(pluginsApi.list);

describe('usePluginStore', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    usePluginStore.setState({
      plugins: [],
      pluginsEnabled: null,
      pluginStatus: 'idle',
      enterpriseAccessAudit: 'idle',
    });
  });

  it('stores the normalized generic plugin list', async () => {
    listPlugins.mockResolvedValueOnce({
      plugins_enabled: true,
      plugins: [
        { id: 'example', registered: true, enabled: true, effective_enabled: true },
      ],
    });

    await usePluginStore.getState().fetchPlugins(true);

    expect(usePluginStore.getState().plugins).toHaveLength(1);
    expect(usePluginStore.getState().plugins[0].id).toBe('example');
    expect(usePluginStore.getState().pluginStatus).toBe('ready');
  });

  it('enables the enterprise policy capability only for an effective plugin', async () => {
    listPlugins.mockResolvedValueOnce({
      plugins_enabled: true,
      plugins: [
        { id: 'enterprise-access-audit', registered: true, enabled: true, effective_enabled: true },
      ],
    });

    await usePluginStore.getState().fetchPlugins(true);

    expect(usePluginStore.getState().enterpriseAccessAudit).toBe('enabled');
  });

  it('hides the enterprise policy capability when the plugin is absent or disabled', async () => {
    listPlugins.mockResolvedValueOnce({
      plugins_enabled: true,
      plugins: [
        { id: 'enterprise-access-audit', registered: true, enabled: false, effective_enabled: false },
      ],
    });

    await usePluginStore.getState().fetchPlugins(true);

    expect(usePluginStore.getState().enterpriseAccessAudit).toBe('disabled');
  });

  it('fails closed when the plugin endpoint is unavailable', async () => {
    listPlugins.mockRejectedValueOnce(new Error('not available'));

    await usePluginStore.getState().fetchPlugins(true);

    expect(usePluginStore.getState().plugins).toEqual([]);
    expect(usePluginStore.getState().pluginStatus).toBe('unavailable');
  });
});
