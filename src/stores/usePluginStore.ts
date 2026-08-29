import { create } from 'zustand';
import { pluginsApi } from '@/services/api/plugins';
import type { ManagementPluginEntry } from '@/types/plugin';

export type PluginCapabilityStatus = 'idle' | 'loading' | 'ready' | 'unavailable';
export type EnterpriseAccessAuditCapability = 'idle' | 'loading' | 'enabled' | 'disabled';

interface PluginStoreState {
  pluginsEnabled: boolean | null;
  plugins: ManagementPluginEntry[];
  pluginStatus: PluginCapabilityStatus;
  enterpriseAccessAudit: EnterpriseAccessAuditCapability;
  fetchPlugins: (force?: boolean) => Promise<ManagementPluginEntry[]>;
  clearPlugins: () => void;
}

let fetchPromise: Promise<ManagementPluginEntry[]> | null = null;

export const usePluginStore = create<PluginStoreState>((set, get) => ({
  pluginsEnabled: null,
  plugins: [],
  pluginStatus: 'idle',
  enterpriseAccessAudit: 'idle',

  fetchPlugins: async (force = false) => {
    if (!force && get().pluginStatus === 'ready') return get().plugins;
    if (fetchPromise) return fetchPromise;

    set({ pluginStatus: 'loading' });
    fetchPromise = pluginsApi
      .list()
      .then((response) => {
        const plugins = response.plugins ?? [];
        const audit = plugins.find((plugin) => plugin.id === 'enterprise-access-audit');
        set({
          plugins,
          pluginsEnabled: response.plugins_enabled ?? true,
          pluginStatus: 'ready',
          enterpriseAccessAudit: audit?.effective_enabled ? 'enabled' : 'disabled',
        });
        return plugins;
      })
      .catch(() => {
        set({
          plugins: [],
          pluginsEnabled: null,
          pluginStatus: 'unavailable',
          enterpriseAccessAudit: 'disabled',
        });
        return [];
      })
      .finally(() => {
        fetchPromise = null;
      });

    return fetchPromise;
  },

  clearPlugins: () => {
    fetchPromise = null;
    set({
      pluginsEnabled: null,
      plugins: [],
      pluginStatus: 'idle',
      enterpriseAccessAudit: 'idle',
    });
  },
}));

export const isEnterpriseAccessAuditEnabled = () =>
  usePluginStore.getState().enterpriseAccessAudit === 'enabled';
