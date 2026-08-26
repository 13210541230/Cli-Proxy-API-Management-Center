import { renderToStaticMarkup } from 'react-dom/server';
import type { ReactNode } from 'react';
import { act, create } from 'react-test-renderer';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { TFunction } from 'i18next';
import { RequestAuditPage, RequestAuditView } from './RequestAuditPage';
import { enterpriseAccessAuditApi } from '@/services/api/enterpriseAccessAudit';
import { enterpriseKeysApi } from '@/services/api/enterpriseKeys';

const { auditTestTranslation } = vi.hoisted(() => ({
  auditTestTranslation: (key: string) => key,
}));

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: auditTestTranslation }),
}));

vi.mock('@/services/api/enterpriseAccessAudit', () => ({
  enterpriseAccessAuditApi: {
    listAudit: vi.fn(),
    getAuditDetail: vi.fn(),
    getSettings: vi.fn(),
  },
}));

vi.mock('@/services/api/enterpriseKeys', () => ({
  enterpriseKeysApi: {
    listKeyMetadata: vi.fn(),
    listDepartments: vi.fn(),
  },
}));

vi.mock('@/components/ui/Modal', () => ({
  Modal: ({ open, title, children }: { open: boolean; title: string; children: ReactNode }) =>
    open ? <div data-testid="audit-modal"><h2>{title}</h2>{children}</div> : null,
}));
import {
  buildAuditEnterpriseMetadata,
  EMPTY_AUDIT_FILTERS,
  filterAuditRecords,
  hasAuditEnterpriseMetadataFilters,
  joinAuditEnterpriseMetadata,
  normalizeAuditPagination,
  paginateAuditRecords,
} from './requestAuditState';
import type { RequestAuditFilters } from './requestAuditState';
import type { EnterpriseAccessAuditRecord } from '@/types/enterpriseAccessAudit';

const t = ((key: string, options?: Record<string, unknown>) => {
  const copy: Record<string, string> = {
    'common.refresh': 'Refresh',
    'common.previous': 'Previous',
    'common.next': 'Next',
    'common.unknown_error': 'Unknown error',
    'request_audit.eyebrow': 'Manual review',
    'request_audit.title': 'Request Audit',
    'request_audit.description': 'Bounded user text',
    'request_audit.retention_title': 'Retention',
    'request_audit.retention_value': 'Retained {{days}} days',
    'request_audit.retention_unavailable': 'Retention unavailable',
    'request_audit.exclusions_title': 'Exclusions',
    'request_audit.exclusions_body': 'Realtime/WebSocket, image, video, count_tokens',
    'request_audit.manual_only_title': 'Manual only',
    'request_audit.manual_only_body': 'No automatic classification',
    'request_audit.filters_title': 'Filters',
    'request_audit.clear_filters': 'Clear filters',
    'request_audit.from': 'From',
    'request_audit.to': 'To',
    'request_audit.key_hash': 'Key hash',
    'request_audit.model': 'Model',
    'request_audit.source_format': 'Source format',
    'request_audit.outcome': 'Outcome',
    'request_audit.all_sources': 'All sources',
    'request_audit.all_outcomes': 'All outcomes',
    'request_audit.outcome_success': 'Success',
    'request_audit.outcome_failed': 'Failed',
    'request_audit.outcome_rejected': 'Rejected',
    'request_audit.username': 'Username',
    'request_audit.email': 'Email',
    'request_audit.department': 'Department',
    'request_audit.loading': 'Loading audit records...',
    'request_audit.unavailable': 'Plugin unavailable',
    'request_audit.metadata_unavailable': 'Enterprise metadata unavailable',
    'request_audit.error': 'Audit error: {{message}}',
    'request_audit.empty': 'No stored audit records',
    'request_audit.no_matches': 'No matching records',
    'request_audit.time': 'Time',
    'request_audit.owner': 'Enterprise user',
    'request_audit.unknown_owner': 'Unknown enterprise key',
    'request_audit.inspect': 'Inspect',
    'request_audit.status': 'Status',
    'request_audit.action': 'Action',
    'request_audit.request_id': 'Request ID',
    'request_audit.pagination': 'Showing {{from}}–{{to}} of {{total}} records',
    'request_audit.page': 'per page',
    'request_audit.page_size': 'Page size',
    'request_audit.detail_title': 'Audit detail',
    'request_audit.detail_loading': 'Loading detail',
    'request_audit.detail_error': 'Detail error: {{message}}',
    'request_audit.user_text': 'Stored user text',
    'request_audit.text_unavailable': 'User text unavailable',
    'request_audit.text_unavailable_reason': 'Reason: {{reason}}',
    'request_audit.reason_not_recorded': 'not recorded',
    'request_audit.text_empty': 'Stored user text is empty',
    'request_audit.text_truncated': 'Text was truncated',
  };
  let value = copy[key] ?? key;
  for (const [name, replacement] of Object.entries(options ?? {})) {
    value = value.replace(`{{${name}}}`, String(replacement));
  }
  return value;
}) as TFunction;

const baseRecord = (overrides: Partial<EnterpriseAccessAuditRecord> = {}): EnterpriseAccessAuditRecord => ({
  id: 1,
  key_hash: 'abcdef12',
  created_at: '2026-01-01T00:00:00Z',
  model: 'gpt-5',
  source_format: 'openai',
  request_id: 'request-1',
  outcome: 'success',
  status_code: 200,
  text: 'hello user',
  text_available: true,
  text_truncated: false,
  ...overrides,
});

const viewProps = (overrides: Partial<Parameters<typeof RequestAuditView>[0]> = {}) => ({
  t,
  status: 'ready' as const,
  error: null,
  metadataError: null,
  records: [],
  pagination: { page: 1, page_size: 25, total: 0, has_next: false },
  filters: EMPTY_AUDIT_FILTERS,
  pageSize: 25,
  settings: { retention_days: 30, default_audit_enabled: true, max_text_bytes: 4096 },
  detail: null,
  detailStatus: 'idle' as const,
  detailError: null,
  onFilterChange: () => {},
  onClearFilters: () => {},
  onRefresh: () => {},
  onPageChange: () => {},
  onPageSizeChange: () => {},
  onOpenDetail: () => {},
  onCloseDetail: () => {},
  ...overrides,
});

describe('RequestAuditPage helpers and view', () => {
  it('joins records to username, email, and department without using a raw key', () => {
    const metadata = buildAuditEnterpriseMetadata(
      [{ apiKeyHash: 'ABCDEF12', userName: 'Alice', email: 'alice@example.com', departmentId: 'eng' }],
      [{ id: 'eng', name: 'Engineering', prefix: 'eng', sortOrder: 0, enabled: true, system: false, createdAtMs: 0, updatedAtMs: 0 }]
    );
    const [record] = joinAuditEnterpriseMetadata([baseRecord()], metadata);
    expect(record.enterprise).toEqual({ userName: 'Alice', email: 'alice@example.com', department: 'Engineering' });
    expect(JSON.stringify(record)).not.toContain('raw-secret');
  });

  it('filters joined metadata and normalizes server paging', () => {
    const records = joinAuditEnterpriseMetadata([baseRecord(), baseRecord({ id: 2, key_hash: 'abcdef13', model: 'claude-3' })], new Map([
      ['abcdef12', { userName: 'Alice', email: 'alice@example.com', department: 'Engineering' }],
      ['abcdef13', { userName: 'Bob', email: 'bob@example.com', department: 'Sales' }],
    ]));
    const filters: RequestAuditFilters = { ...EMPTY_AUDIT_FILTERS, username: 'alice', department: 'engineering', security_signal: 'cyber_policy' };
    expect(filterAuditRecords(records, filters).map((record) => record.id)).toEqual([]);
    const cyberRecords = joinAuditEnterpriseMetadata([baseRecord({ security_signal: 'cyber_policy' })], new Map());
    expect(filterAuditRecords(cyberRecords, { ...EMPTY_AUDIT_FILTERS, security_signal: 'cyber_policy' }).map((record) => record.id)).toEqual([1]);
    expect(normalizeAuditPagination({ page: 2, page_size: 10, total: 21, has_next: true }, 1, 25)).toEqual({ page: 2, page_size: 10, total: 21, has_next: true });
    expect(normalizeAuditPagination(undefined, 1, 25).total).toBe(0);
    expect(hasAuditEnterpriseMetadataFilters(filters)).toBe(true);
    expect(paginateAuditRecords(records, 2, 1).map((record) => record.id)).toEqual([2]);
  });

  it('renders distinct loading, empty, error, retention, and exclusion states', () => {
    const loading = renderToStaticMarkup(<RequestAuditView {...viewProps({ status: 'loading' })} />);
    const empty = renderToStaticMarkup(<RequestAuditView {...viewProps()} />);
    const error = renderToStaticMarkup(<RequestAuditView {...viewProps({ status: 'error', error: 'network down' })} />);
    const unavailable = renderToStaticMarkup(<RequestAuditView {...viewProps({ status: 'unavailable' })} />);
    expect(loading).toContain('Loading audit records...');
    expect(empty).toContain('No stored audit records');
    expect(error).toContain('Audit error: network down');
    expect(unavailable).toContain('Plugin unavailable');
    const detailLoading = renderToStaticMarkup(<RequestAuditView {...viewProps({ detailStatus: 'loading' })} />);
    expect(detailLoading).toContain('Loading detail');
    expect(empty).toContain('Retained 30 days');
    expect(empty).toContain('Realtime/WebSocket, image, video, count_tokens');
  });

  it('renders numeric-token detail as unavailable rather than an ordinary empty message', () => {
    const html = renderToStaticMarkup(
      <RequestAuditView
        {...viewProps({
          detailStatus: 'ready',
          detail: baseRecord({ text: '', text_available: false, text_unavailable_reason: 'numeric_prompt' }),
        })}
      />
    );
    expect(html).toContain('User text unavailable');
    expect(html).toContain('Reason: numeric_prompt');
    expect(html).not.toContain('Stored user text is empty');
    expect(html).not.toContain('raw');
  });

  describe('mounted page lifecycle', () => {
    beforeEach(() => {
      vi.clearAllMocks();
      vi.mocked(enterpriseAccessAuditApi.listAudit).mockResolvedValue({
        records: [baseRecord()],
        pagination: { page: 1, page_size: 25, total: 1, has_next: false },
      });
      vi.mocked(enterpriseAccessAuditApi.getSettings).mockResolvedValue({
        retention_days: 30,
        default_audit_enabled: true,
        max_text_bytes: 4096,
      });
      vi.mocked(enterpriseAccessAuditApi.getAuditDetail).mockResolvedValue(
        baseRecord({ text: '', text_available: false, text_unavailable_reason: 'numeric_prompt' })
      );
      vi.mocked(enterpriseKeysApi.listKeyMetadata).mockResolvedValue({
        items: [{ apiKeyHash: 'abcdef12', userName: 'Alice', email: 'alice@example.com', departmentId: 'eng' }],
      });
      vi.mocked(enterpriseKeysApi.listDepartments).mockResolvedValue({
        items: [{ id: 'eng', name: 'Engineering', prefix: 'eng', sortOrder: 0, enabled: true, system: false, createdAtMs: 0, updatedAtMs: 0 }],
      });
    });

    it('loads audit records and opens numeric-token detail through the real page', async () => {
      let renderer: ReturnType<typeof create>;
      await act(async () => {
        renderer = create(<RequestAuditPage />);
        await new Promise((resolve) => setTimeout(resolve, 50));
      });
      expect(enterpriseKeysApi.listKeyMetadata).toHaveBeenCalledTimes(1);
      expect(enterpriseAccessAuditApi.listAudit).toHaveBeenCalled();
      const inspectButton = renderer!.root.findAllByType('button').find((button) =>
        button.findAllByType('span').some((span) => span.children.includes('request_audit.inspect'))
      );
      expect(inspectButton).toBeDefined();
      await act(async () => {
        inspectButton!.props.onClick();
        await Promise.resolve();
      });
      expect(enterpriseAccessAuditApi.getAuditDetail).toHaveBeenCalledWith(1);
      renderer!.unmount();
    });

    it('renders the unavailable state when the audit endpoint is unavailable', async () => {
      vi.mocked(enterpriseAccessAuditApi.listAudit).mockRejectedValue(Object.assign(new Error('missing plugin'), { status: 404 }));
      let renderer: ReturnType<typeof create>;
      await act(async () => {
        renderer = create(<RequestAuditPage />);
        await new Promise((resolve) => setTimeout(resolve, 50));
      });
      expect(renderer!.toJSON()).toBeTruthy();
      expect(JSON.stringify(renderer!.toJSON())).toContain('request_audit.unavailable');
      renderer!.unmount();
    });
  });
});
