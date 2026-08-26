import type { EnterpriseAccessAuditPagination, EnterpriseAccessAuditRecord } from '@/types/enterpriseAccessAudit';
import type { EnterpriseDepartment, EnterpriseKeyMetadata } from '@/types/enterpriseKey';
import { normalizeEnterprisePolicyHash } from '@/utils/enterpriseAccessAudit';

export interface AuditEnterpriseMetadata {
  userName: string;
  email: string;
  department: string;
}

export interface JoinedAuditRecord extends EnterpriseAccessAuditRecord {
  enterprise?: AuditEnterpriseMetadata;
}

export interface RequestAuditFilters {
  from: string;
  to: string;
  key_hash: string;
  model: string;
  source_format: string;
  outcome: string;
  security_signal: string;
  username: string;
  email: string;
  department: string;
}

export const EMPTY_AUDIT_FILTERS: RequestAuditFilters = {
  from: '',
  to: '',
  key_hash: '',
  model: '',
  source_format: '',
  outcome: '',
  security_signal: '',
  username: '',
  email: '',
  department: '',
};

export function buildAuditEnterpriseMetadata(
  bindings: EnterpriseKeyMetadata[],
  departments: EnterpriseDepartment[]
): Map<string, AuditEnterpriseMetadata> {
  const departmentsById = new Map(departments.map((department) => [department.id, department.name]));
  const metadata = new Map<string, AuditEnterpriseMetadata>();
  for (const binding of bindings) {
    const hash = normalizeEnterprisePolicyHash(binding.apiKeyHash);
    if (!hash) continue;
    metadata.set(hash, {
      userName: binding.userName || '',
      email: binding.email || '',
      department: departmentsById.get(binding.departmentId) || binding.departmentId || '',
    });
  }
  return metadata;
}

export function joinAuditEnterpriseMetadata(
  records: EnterpriseAccessAuditRecord[],
  metadata: Map<string, AuditEnterpriseMetadata>
): JoinedAuditRecord[] {
  return records.map((record) => ({
    ...record,
    enterprise: metadata.get(normalizeEnterprisePolicyHash(record.key_hash)),
  }));
}

export function filterAuditRecords(
  records: JoinedAuditRecord[],
  filters: Pick<RequestAuditFilters, 'key_hash' | 'model' | 'source_format' | 'outcome' | 'security_signal' | 'username' | 'email' | 'department'>
): JoinedAuditRecord[] {
  const match = (value: string | undefined, query: string) => !query || (value || '').toLowerCase().includes(query.toLowerCase());
  return records.filter((record) => {
    const enterprise = record.enterprise;
    return (
      match(record.key_hash, filters.key_hash) &&
      match(record.model, filters.model) &&
      match(record.source_format, filters.source_format) &&
      match(record.outcome, filters.outcome) &&
      match(record.security_signal, filters.security_signal) &&
      match(enterprise?.userName, filters.username) &&
      match(enterprise?.email, filters.email) &&
      match(enterprise?.department, filters.department)
    );
  });
}

export function hasAuditEnterpriseMetadataFilters(filters: Pick<RequestAuditFilters, 'username' | 'email' | 'department'>): boolean {
  return Boolean(filters.username.trim() || filters.email.trim() || filters.department.trim());
}

export function paginateAuditRecords(records: JoinedAuditRecord[], page: number, pageSize: number): JoinedAuditRecord[] {
  const safePage = Math.max(1, page);
  const safePageSize = Math.max(1, pageSize);
  const start = (safePage - 1) * safePageSize;
  return records.slice(start, start + safePageSize);
}

export function normalizeAuditPagination(
  pagination: Partial<EnterpriseAccessAuditPagination> | undefined,
  fallbackPage: number,
  fallbackPageSize: number
): EnterpriseAccessAuditPagination {
  const page = pagination?.page;
  const pageSize = pagination?.page_size;
  const total = pagination?.total;
  return {
    page: Number.isFinite(page) && (page ?? 0) > 0 ? page ?? fallbackPage : fallbackPage,
    page_size: Number.isFinite(pageSize) && (pageSize ?? 0) > 0 ? pageSize ?? fallbackPageSize : fallbackPageSize,
    total: Number.isFinite(total) && (total ?? 0) >= 0 ? total ?? 0 : 0,
    has_next: pagination?.has_next === true,
  };
}
