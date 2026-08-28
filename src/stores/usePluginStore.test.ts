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
    usePluginStore.setState({ enterpriseAccessAudit: 'idle' });
  });

  it('enables enterprise capabilities only for an effectively enabled plugin', async () => {
    listPlugins.mockResolvedValueOnce({
      plugins: [
        { id: 'enterprise-access-audit', registered: true, enabled: true, effective_enabled: true },
      ],
    });

    await usePluginStore.getState().fetchPlugins(true);

    expect(usePluginStore.getState().enterpriseAccessAudit).toBe('enabled');
  });

  it.each([
    [{ plugins: [] }],
    [{ plugins: [{ id: 'enterprise-access-audit', registered: true, enabled: false, effective_enabled: false }] }],
  ])('hides enterprise capabilities when the plugin is unavailable', async (response) => {
    listPlugins.mockResolvedValueOnce(response);

    await usePluginStore.getState().fetchPlugins(true);

    expect(usePluginStore.getState().enterpriseAccessAudit).toBe('disabled');
  });

  it('fails closed when the capability endpoint is unavailable', async () => {
    listPlugins.mockRejectedValueOnce(new Error('not available'));

    await usePluginStore.getState().fetchPlugins(true);

    expect(usePluginStore.getState().enterpriseAccessAudit).toBe('disabled');
  });
});
