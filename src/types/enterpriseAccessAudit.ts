export interface EnterpriseAccessAuditPolicy {
  key_hash: string;
  denied_models: string[];
  audit_enabled: boolean;
  updated_at: number;
}

export interface EnterpriseAccessAuditPoliciesResponse {
  policies: EnterpriseAccessAuditPolicy[];
}

export interface EnterpriseAccessAuditBatchPayload {
  key_hashes: string[];
  denied_models: string[];
  audit_enabled: boolean | null;
}

export interface EnterpriseAccessAuditSinglePayload {
  key_hash: string;
  denied_models?: string[];
  audit_enabled?: boolean;
}

export type EnterpriseAccessAuditStatus = 'idle' | 'loading' | 'ready' | 'error' | 'unavailable';

export interface EnterpriseAccessAuditState {
  policies: Record<string, EnterpriseAccessAuditPolicy>;
  status: EnterpriseAccessAuditStatus;
  error: string | null;
  mutating: boolean;
}
