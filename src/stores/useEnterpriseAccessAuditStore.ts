import { create } from 'zustand';
import { enterpriseAccessAuditApi } from '@/services/api/enterpriseAccessAudit';
import type {
  EnterpriseAccessAuditBatchPayload,
  EnterpriseAccessAuditPolicy,
  EnterpriseAccessAuditSinglePayload,
  EnterpriseAccessAuditState,
} from '@/types/enterpriseAccessAudit';
import { normalizeEnterprisePolicyHash } from '@/utils/enterpriseAccessAudit';

interface EnterpriseAccessAuditStore extends EnterpriseAccessAuditState {
  loadPolicies: (keyHashes?: string[]) => Promise<void>;
  updatePolicy: (payload: EnterpriseAccessAuditSinglePayload) => Promise<void>;
  updatePoliciesBatch: (payload: EnterpriseAccessAuditBatchPayload) => Promise<void>;
  clearError: () => void;
}

const isUnavailableError = (error: unknown): boolean => {
  const status = (error as { status?: number } | null)?.status;
  return status === undefined || status === 404 || status === 405 || status === 501 || status >= 500;
};

const errorMessage = (error: unknown): string =>
  error instanceof Error ? error.message : '企业 Key 策略请求失败';

export const useEnterpriseAccessAuditStore = create<EnterpriseAccessAuditStore>((set, get) => {
  let requestVersion = 0;

  return {
    policies: {},
    status: 'idle',
    error: null,
    mutating: false,

    loadPolicies: async (keyHashes) => {
      const version = ++requestVersion;
      set({ status: 'loading', error: null });
      try {
        const response = await enterpriseAccessAuditApi.listPolicies(keyHashes);
        if (version !== requestVersion) return;
        const policies: Record<string, EnterpriseAccessAuditPolicy> = {};
        for (const policy of response.policies ?? []) {
          const hash = normalizeEnterprisePolicyHash(policy.key_hash);
          if (!hash) continue;
          policies[hash] = {
            ...policy,
            key_hash: hash,
            denied_models: Array.isArray(policy.denied_models) ? policy.denied_models : [],
            audit_enabled: policy.audit_enabled !== false,
          };
        }
        set({ policies: { ...get().policies, ...policies }, status: 'ready', error: null });
      } catch (error) {
        if (version !== requestVersion) return;
        set({ status: isUnavailableError(error) ? 'unavailable' : 'error', error: errorMessage(error) });
        throw error;
      }
    },

    updatePolicy: async (payload) => {
      set({ mutating: true, error: null });
      try {
        await enterpriseAccessAuditApi.updatePolicy(payload);
        await get().loadPolicies([payload.key_hash]);
      } catch (error) {
        set({ error: errorMessage(error) });
        throw error;
      } finally {
        set({ mutating: false });
      }
    },

    updatePoliciesBatch: async (payload) => {
      set({ mutating: true, error: null });
      try {
        await enterpriseAccessAuditApi.updatePoliciesBatch(payload);
        await get().loadPolicies(payload.key_hashes);
      } catch (error) {
        set({ error: errorMessage(error) });
        throw error;
      } finally {
        set({ mutating: false });
      }
    },

    clearError: () => set({ error: null }),
  };
});
