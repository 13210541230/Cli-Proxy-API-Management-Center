import { apiClient } from './client';
import type {
  EnterpriseAccessAuditBatchPayload,
  EnterpriseAccessAuditPoliciesResponse,
  EnterpriseAccessAuditSinglePayload,
} from '@/types/enterpriseAccessAudit';
import { normalizeEnterprisePolicyHashes, normalizeEnterprisePolicyHash, normalizeEnterprisePolicyModels } from '@/utils/enterpriseAccessAudit';

const BASE = '/enterprise-access-audit';

export const enterpriseAccessAuditApi = {
  /** Load all persisted policies, or default rows for the supplied hashes. */
  listPolicies: (keyHashes?: string[]) => {
    const hashes = keyHashes ? normalizeEnterprisePolicyHashes(keyHashes) : undefined;
    return apiClient.get<EnterpriseAccessAuditPoliciesResponse>(`${BASE}/policies`, {
      params: hashes && hashes.length > 0 ? { key_hash: hashes } : undefined,
      paramsSerializer: hashes && hashes.length > 0 ? { indexes: null } : undefined,
    });
  },

  /** Replace one policy's explicitly supplied fields. */
  updatePolicy: (payload: EnterpriseAccessAuditSinglePayload) => {
    const keyHash = normalizeEnterprisePolicyHash(payload.key_hash);
    if (!keyHash) throw new Error('有效的 apiKeyHash 是必需的');
    const normalized: EnterpriseAccessAuditSinglePayload = { key_hash: keyHash };
    if (payload.denied_models !== undefined) {
      normalized.denied_models = normalizeEnterprisePolicyModels(payload.denied_models);
    }
    if (payload.audit_enabled !== undefined) normalized.audit_enabled = payload.audit_enabled;
    return apiClient.put<{ updated: string }>(`${BASE}/policy`, normalized);
  },

  /** Atomically replace the same model list and optional audit state for many hashes. */
  updatePoliciesBatch: (payload: EnterpriseAccessAuditBatchPayload) => {
    const keyHashes = normalizeEnterprisePolicyHashes(payload.key_hashes);
    if (keyHashes.length === 0) throw new Error('至少需要一个有效的 apiKeyHash');
    const normalized: EnterpriseAccessAuditBatchPayload = {
      key_hashes: keyHashes,
      denied_models: normalizeEnterprisePolicyModels(payload.denied_models),
      audit_enabled: payload.audit_enabled,
    };
    return apiClient.put<{ updated: string[] }>(`${BASE}/policies/batch`, normalized);
  },
};

export type { EnterpriseAccessAuditPolicy } from '@/types/enterpriseAccessAudit';
