import { apiClient } from './client';
import type {
  EnterpriseAccessAuditBatchPayload,
  EnterpriseAccessAuditListParams,
  EnterpriseAccessAuditListResponse,
  EnterpriseAccessAuditPoliciesResponse,
  EnterpriseAccessAuditRecord,
  EnterpriseAccessAuditSettings,
  EnterpriseAccessAuditSinglePayload,
} from '@/types/enterpriseAccessAudit';
import { normalizeEnterprisePolicyHashes, normalizeEnterprisePolicyHash, normalizeEnterprisePolicyModels } from '@/utils/enterpriseAccessAudit';

const BASE = '/enterprise-access-audit';
const AUDIT_QUERY_FIELDS: Array<keyof EnterpriseAccessAuditListParams> = [
  'from',
  'to',
  'key_hash',
  'model',
  'source_format',
  'outcome',
  'security_signal',
  'page',
  'page_size',
];

export function serializeEnterpriseAccessAuditQuery(params: Partial<EnterpriseAccessAuditListParams>): string {
  const query = new URLSearchParams();
  for (const field of AUDIT_QUERY_FIELDS) {
    const value = params[field];
    if (value === undefined || value === null || value === '') continue;
    query.set(field, String(value));
  }
  return query.toString();
}

const withQuery = (path: string, query: string): string => (query ? `${path}?${query}` : path);

export const enterpriseAccessAuditApi = {
  listPolicies: (keyHashes?: string[]) => {
    const hashes = keyHashes ? normalizeEnterprisePolicyHashes(keyHashes) : undefined;
    return apiClient.get<EnterpriseAccessAuditPoliciesResponse>(`${BASE}/policies`, {
      params: hashes && hashes.length > 0 ? { key_hash: hashes } : undefined,
      paramsSerializer: hashes && hashes.length > 0 ? { indexes: null } : undefined,
    });
  },

  updatePolicy: (payload: EnterpriseAccessAuditSinglePayload) => {
    const keyHash = normalizeEnterprisePolicyHash(payload.key_hash);
    if (!keyHash) throw new Error('有效的 apiKeyHash 是必需的');
    const normalized: EnterpriseAccessAuditSinglePayload = { key_hash: keyHash };
    if (payload.denied_models !== undefined) normalized.denied_models = normalizeEnterprisePolicyModels(payload.denied_models);
    if (payload.audit_enabled !== undefined) normalized.audit_enabled = payload.audit_enabled;
    return apiClient.put<{ updated: string }>(`${BASE}/policy`, normalized);
  },

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

  listAudit: (params: EnterpriseAccessAuditListParams) =>
    apiClient.get<EnterpriseAccessAuditListResponse>(
      withQuery(`${BASE}/audit`, serializeEnterpriseAccessAuditQuery(params))
    ),

  getAuditDetail: (id: number) =>
    apiClient.get<EnterpriseAccessAuditRecord>(
      withQuery(`${BASE}/audit/detail`, `id=${encodeURIComponent(String(id))}`)
    ),

  getSettings: () => apiClient.get<EnterpriseAccessAuditSettings>(`${BASE}/settings`),
};

export type { EnterpriseAccessAuditPolicy } from '@/types/enterpriseAccessAudit';
