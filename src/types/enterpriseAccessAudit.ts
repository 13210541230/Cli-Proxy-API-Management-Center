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

export interface EnterpriseAccessAuditRecord {
  id: number;
  key_hash: string;
  created_at: string;
  model: string;
  source_format: string;
  request_id: string;
  outcome: string;
  status_code: number;
  text: string;
  text_available: boolean;
  text_unavailable_reason?: string;
  text_truncated: boolean;
  security_signal?: string;
}

export interface EnterpriseAccessAuditPagination {
  page: number;
  page_size: number;
  total: number;
  has_next: boolean;
}

export interface EnterpriseAccessAuditListResponse {
  records: EnterpriseAccessAuditRecord[];
  pagination: EnterpriseAccessAuditPagination;
}

export interface EnterpriseAccessAuditListParams {
  from?: string;
  to?: string;
  key_hash?: string;
  model?: string;
  source_format?: string;
  outcome?: string;
  security_signal?: string;
  page: number;
  page_size: number;
}

export interface EnterpriseAccessAuditSettings {
  retention_days: number;
  default_audit_enabled: boolean;
  max_text_bytes: number;
}

export type EnterpriseAccessAuditStatus = 'idle' | 'loading' | 'ready' | 'error' | 'unavailable';

export interface EnterpriseAccessAuditState {
  policies: Record<string, EnterpriseAccessAuditPolicy>;
  status: EnterpriseAccessAuditStatus;
  error: string | null;
  mutating: boolean;
}
