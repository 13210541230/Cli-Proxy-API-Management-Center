import { create } from 'zustand';
import { pluginsApi } from '@/services/api/plugins';

const ENTERPRISE_ACCESS_AUDIT_ID = 'enterprise-access-audit';

type PluginCapabilityStatus = 'idle' | 'loading' | 'enabled' | 'disabled';

interface PluginState {
  enterpriseAccessAudit: PluginCapabilityStatus;
  fetchPlugins: (force?: boolean) => Promise<void>;
}

let inFlight: Promise<void> | null = null;
let fetchedAt = 0;
const CACHE_MS = 30_000;

export const usePluginStore = create<PluginState>((set) => ({
  enterpriseAccessAudit: 'idle',
  fetchPlugins: async (force = false) => {
    if (!force && fetchedAt > 0 && Date.now() - fetchedAt < CACHE_MS) return;
    if (inFlight) return inFlight;

    set({ enterpriseAccessAudit: 'loading' });
    inFlight = pluginsApi
      .list()
      .then((response) => {
        const plugin = response.plugins?.find((item) => item.id === ENTERPRISE_ACCESS_AUDIT_ID);
        fetchedAt = Date.now();
        set({ enterpriseAccessAudit: plugin?.effective_enabled ? 'enabled' : 'disabled' });
      })
      .catch(() => {
        fetchedAt = Date.now();
        set({ enterpriseAccessAudit: 'disabled' });
      })
      .finally(() => {
        inFlight = null;
      });

    return inFlight;
  },
}));

export const isEnterpriseAccessAuditEnabled = () =>
  usePluginStore.getState().enterpriseAccessAudit === 'enabled';
