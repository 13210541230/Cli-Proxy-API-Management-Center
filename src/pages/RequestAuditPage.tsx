import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { Input } from '@/components/ui/Input';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { Modal } from '@/components/ui/Modal';
import { Select } from '@/components/ui/Select';
import { IconRefreshCw } from '@/components/ui/icons';
import { enterpriseAccessAuditApi } from '@/services/api/enterpriseAccessAudit';
import { enterpriseKeysApi } from '@/services/api/enterpriseKeys';
import type {
  EnterpriseAccessAuditListParams,
  EnterpriseAccessAuditPagination,
  EnterpriseAccessAuditRecord,
  EnterpriseAccessAuditSettings,
} from '@/types/enterpriseAccessAudit';
import type { EnterpriseKeyMetadata } from '@/types/enterpriseKey';
import styles from './RequestAuditPage.module.scss';
import {
  buildAuditEnterpriseMetadata,
  EMPTY_AUDIT_FILTERS,
  filterAuditRecords,
  hasAuditEnterpriseMetadataFilters,
  joinAuditEnterpriseMetadata,
  normalizeAuditPagination,
  paginateAuditRecords,
} from './requestAuditState';
import type { AuditEnterpriseMetadata, JoinedAuditRecord, RequestAuditFilters } from './requestAuditState';
import { normalizeEnterprisePolicyHash } from '@/utils/enterpriseAccessAudit';

type AuditViewStatus = 'idle' | 'loading' | 'ready' | 'error' | 'unavailable';
type DetailStatus = 'idle' | 'loading' | 'ready' | 'error';

const toIsoTime = (value: string): string | undefined => {
  if (!value) return undefined;
  const timestamp = new Date(value).getTime();
  return Number.isFinite(timestamp) ? new Date(timestamp).toISOString() : undefined;
};

const isUnavailableError = (error: unknown): boolean => {
  const status = (error as { status?: number } | null)?.status;
  return status === undefined || status === 404 || status === 405 || status === 501 || status >= 500;
};

const errorMessage = (error: unknown): string => (error instanceof Error ? error.message : 'Request audit request failed');

interface RequestAuditViewProps {
  t: TFunction;
  status: AuditViewStatus;
  error: string | null;
  metadataError: string | null;
  records: JoinedAuditRecord[];
  pagination: EnterpriseAccessAuditPagination;
  filters: RequestAuditFilters;
  pageSize: number;
  settings: EnterpriseAccessAuditSettings | null;
  detail: EnterpriseAccessAuditRecord | null;
  detailEnterprise?: AuditEnterpriseMetadata;
  detailStatus: DetailStatus;
  detailError: string | null;
  onFilterChange: (field: keyof RequestAuditFilters, value: string) => void;
  onClearFilters: () => void;
  onRefresh: () => void;
  onPageChange: (page: number) => void;
  onPageSizeChange: (pageSize: number) => void;
  onOpenDetail: (id: number) => void;
  onCloseDetail: () => void;
}

const maskHash = (hash: string) => normalizeEnterprisePolicyHash(hash) || '—';
const formatTime = (value: string) => {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value || '—' : date.toLocaleString();
};

const outcomeClass = (outcome: string) => {
  if (outcome === 'success' || outcome === 'succeeded') return styles.success;
  if (outcome === 'rejected') return styles.rejected;
  return styles.failed;
};

const outcomeLabelKey = (outcome: string) => {
  if (outcome === 'success' || outcome === 'succeeded') return 'request_audit.outcome_success';
  return `request_audit.outcome_${outcome}`;
};

function AuditStatusMessage({ t, status, error }: Pick<RequestAuditViewProps, 't' | 'status' | 'error'>) {
  if (status === 'loading') {
    return <div className={styles.state}><LoadingSpinner /><span>{t('request_audit.loading')}</span></div>;
  }
  if (status === 'unavailable') {
    return <div className={`${styles.state} ${styles.warning}`} role="alert">{t('request_audit.unavailable')}</div>;
  }
  if (status === 'error') {
    return <div className={`${styles.state} ${styles.error}`} role="alert">{t('request_audit.error', { message: error || t('common.unknown_error') })}</div>;
  }
  return null;
}

export function RequestAuditView({
  t,
  status,
  error,
  metadataError,
  records,
  pagination,
  filters,
  pageSize,
  settings,
  detail,
  detailEnterprise,
  detailStatus,
  detailError,
  onFilterChange,
  onClearFilters,
  onRefresh,
  onPageChange,
  onPageSizeChange,
  onOpenDetail,
  onCloseDetail,
}: RequestAuditViewProps) {
  const hasFilters = Object.values(filters).some(Boolean);
  const firstItem = pagination.total === 0 ? 0 : (pagination.page - 1) * pagination.page_size + 1;
  const lastItem = Math.min(pagination.total, pagination.page * pagination.page_size);
  const detailOpen = detailStatus !== 'idle';

  return (
    <div className={styles.page}>
      <div className={styles.header}>
        <div>
          <p className={styles.eyebrow}>{t('request_audit.eyebrow')}</p>
          <h1>{t('request_audit.title')}</h1>
          <p className={styles.description}>{t('request_audit.description')}</p>
        </div>
        <Button variant="secondary" onClick={onRefresh} title={t('common.refresh')}>
          <IconRefreshCw size={16} /> {t('common.refresh')}
        </Button>
      </div>

      <div className={styles.noticeGrid}>
        <div className={styles.notice}>
          <strong>{t('request_audit.retention_title')}</strong>
          <span>{settings ? t('request_audit.retention_value', { days: settings.retention_days }) : t('request_audit.retention_unavailable')}</span>
          <span>{t('request_audit.expiry_notice')}</span>
          <span>{t('request_audit.disabled_audit_notice')}</span>
          {metadataError && <span role="status">{t('request_audit.metadata_unavailable')}</span>}
        </div>
        <div className={styles.notice}>
          <strong>{t('request_audit.exclusions_title')}</strong>
          <span>{t('request_audit.exclusions_body')}</span>
        </div>
        <div className={`${styles.notice} ${styles.neutralNotice}`}>
          <strong>{t('request_audit.manual_only_title')}</strong>
          <span>{t('request_audit.manual_only_body')}</span>
        </div>
      </div>

      <Card className={styles.filterCard}>
        <div className={styles.filterHeader}>
          <h2>{t('request_audit.filters_title')}</h2>
          {hasFilters && <Button variant="ghost" size="sm" onClick={onClearFilters}>{t('request_audit.clear_filters')}</Button>}
        </div>
        <div className={styles.filters}>
          <Input label={t('request_audit.from')} type="datetime-local" value={filters.from} onChange={(event) => onFilterChange('from', event.target.value)} />
          <Input label={t('request_audit.to')} type="datetime-local" value={filters.to} onChange={(event) => onFilterChange('to', event.target.value)} />
          <Input label={t('request_audit.key_hash')} value={filters.key_hash} onChange={(event) => onFilterChange('key_hash', event.target.value)} />
          <Input label={t('request_audit.model')} value={filters.model} onChange={(event) => onFilterChange('model', event.target.value)} />
          <div className={styles.selectField}><label>{t('request_audit.source_format')}</label><Select value={filters.source_format} onChange={(value) => onFilterChange('source_format', value)} options={[{ value: '', label: t('request_audit.all_sources') }, ...['openai', 'openai-response', 'codex', 'claude', 'gemini'].map((value) => ({ value, label: value }))]} ariaLabel={t('request_audit.source_format')} /></div>
          <div className={styles.selectField}><label>{t('request_audit.outcome')}</label><Select value={filters.outcome} onChange={(value) => onFilterChange('outcome', value)} options={[{ value: '', label: t('request_audit.all_outcomes') }, { value: 'succeeded', label: t('request_audit.outcome_success') }, { value: 'failed', label: t('request_audit.outcome_failed') }, { value: 'rejected', label: t('request_audit.outcome_rejected') }, { value: 'canceled', label: t('request_audit.outcome_canceled') }]} ariaLabel={t('request_audit.outcome')} /></div>
          <div className={styles.selectField}><label>{t('request_audit.security_signal')}</label><Select value={filters.security_signal} onChange={(value) => onFilterChange('security_signal', value)} options={[{ value: '', label: t('request_audit.all_security_signals') }, { value: 'cyber_policy', label: t('request_audit.security_signal_cyber_policy') }]} ariaLabel={t('request_audit.security_signal')} /></div>
          <Input label={t('request_audit.username')} value={filters.username} onChange={(event) => onFilterChange('username', event.target.value)} />
          <Input label={t('request_audit.email')} value={filters.email} onChange={(event) => onFilterChange('email', event.target.value)} />
          <Input label={t('request_audit.department')} value={filters.department} onChange={(event) => onFilterChange('department', event.target.value)} />
        </div>
      </Card>

      <Card className={styles.listCard}>
        <AuditStatusMessage t={t} status={status} error={error} />
        {status === 'ready' && (pagination.total === 0 || records.length === 0) ? (
          <div className={styles.empty}>{hasFilters ? t('request_audit.no_matches') : t('request_audit.empty')}</div>
        ) : status === 'ready' || records.length > 0 ? (
          <>
            <div className={styles.tableWrap}>
              <table className={styles.table}>
                <thead><tr>
                  <th>{t('request_audit.time')}</th><th>{t('request_audit.owner')}</th><th>{t('request_audit.key_hash')}</th><th>{t('request_audit.model')}</th><th>{t('request_audit.source_format')}</th><th>{t('request_audit.outcome')}</th><th>{t('request_audit.action')}</th>
                </tr></thead>
                <tbody>{records.map((record) => (
                  <tr key={record.id}>
                    <td>{formatTime(record.created_at)}</td>
                    <td><strong>{record.enterprise?.userName || t('request_audit.unknown_owner')}</strong><small>{record.enterprise?.email || record.enterprise?.department || '—'}</small></td>
                    <td><code>{maskHash(record.key_hash)}</code></td>
                    <td>{record.model || '—'}</td>
                    <td>{record.source_format || '—'}</td>
                    <td><span className={`${styles.outcome} ${outcomeClass(record.outcome)}`}>{t(outcomeLabelKey(record.outcome), { defaultValue: record.outcome || t('common.unknown_error') })}</span><small>{record.status_code || '—'}</small>{record.security_signal === 'cyber_policy' && <small className={styles.securitySignal}>{t('request_audit.security_signal_cyber_policy')}</small>}</td>
                    <td><Button size="sm" variant="ghost" onClick={() => onOpenDetail(record.id)}>{t('request_audit.inspect')}</Button></td>
                  </tr>
                ))}</tbody>
              </table>
            </div>
            <div className={styles.pagination}>
              <span>{t('request_audit.pagination', { from: firstItem, to: lastItem, total: pagination.total })}</span>
              <div className={styles.paginationActions}>
                <Select value={String(pageSize)} onChange={(value) => onPageSizeChange(Number(value))} options={[10, 25, 50, 100].map((value) => ({ value: String(value), label: `${value} / ${t('request_audit.page')}` }))} ariaLabel={t('request_audit.page_size')} fullWidth={false} />
                <Button size="sm" variant="ghost" disabled={pagination.page <= 1} onClick={() => onPageChange(pagination.page - 1)}>{t('common.previous')}</Button>
                <span>{pagination.page}</span>
                <Button size="sm" variant="ghost" disabled={!pagination.has_next} onClick={() => onPageChange(pagination.page + 1)}>{t('common.next')}</Button>
              </div>
            </div>
          </>
        ) : null}
      </Card>

      <Modal open={detailOpen} onClose={onCloseDetail} title={t('request_audit.detail_title')} width={760}>
        {detailStatus === 'loading' ? <div className={styles.state}><LoadingSpinner /><span>{t('request_audit.detail_loading')}</span></div> : detailStatus === 'error' ? <div className={`${styles.state} ${styles.error}`} role="alert">{t('request_audit.detail_error', { message: detailError || t('common.unknown_error') })}</div> : detail ? (
          <div className={styles.detail}>
            <dl className={styles.metadata}>
              <div><dt>{t('request_audit.owner')}</dt><dd>{detailEnterprise?.userName || t('request_audit.unknown_owner')}<small>{detailEnterprise?.email || detailEnterprise?.department || '—'}</small></dd></div>
              <div><dt>{t('request_audit.key_hash')}</dt><dd><code>{maskHash(detail.key_hash)}</code></dd></div>
              <div><dt>{t('request_audit.time')}</dt><dd>{formatTime(detail.created_at)}</dd></div>
              <div><dt>{t('request_audit.model')}</dt><dd>{detail.model || '—'}</dd></div>
              <div><dt>{t('request_audit.source_format')}</dt><dd>{detail.source_format || '—'}</dd></div>
              <div><dt>{t('request_audit.request_id')}</dt><dd><code>{detail.request_id || '—'}</code></dd></div>
              <div><dt>{t('request_audit.status')}</dt><dd><span className={`${styles.outcome} ${outcomeClass(detail.outcome)}`}>{t(outcomeLabelKey(detail.outcome), { defaultValue: detail.outcome || t('common.unknown_error') })}</span> {detail.status_code || '—'}{detail.security_signal === 'cyber_policy' && <small className={styles.securitySignal}>{t('request_audit.security_signal_cyber_policy')}</small>}</dd></div>
              {detail.security_signal === 'cyber_policy' && detail.security_message && <div><dt>{t('request_audit.upstream_message')}</dt><dd>{detail.security_message}</dd></div>}
            </dl>
            <div className={styles.textPanel}>
              <h3>{t('request_audit.user_text')}</h3>
              {!detail.text_available ? (
                <div className={styles.unavailableText}><strong>{t('request_audit.text_unavailable')}</strong><span>{t('request_audit.text_unavailable_reason', { reason: detail.text_unavailable_reason || t('request_audit.reason_not_recorded') })}</span></div>
              ) : detail.text ? (
                <pre>{detail.text}</pre>
              ) : <div className={styles.emptyText}>{t('request_audit.text_empty')}</div>}
              {detail.text_truncated && <div className={styles.truncated}>{t('request_audit.text_truncated')}</div>}
            </div>
          </div>
        ) : null}
      </Modal>
    </div>
  );
}

export function RequestAuditPage() {
  const { t } = useTranslation();
  const [keyMetadata, setKeyMetadata] = useState<EnterpriseKeyMetadata[]>([]);
  const [metadataError, setMetadataError] = useState<string | null>(null);
  const [filters, setFilters] = useState<RequestAuditFilters>(EMPTY_AUDIT_FILTERS);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);
  const [status, setStatus] = useState<AuditViewStatus>('idle');
  const [error, setError] = useState<string | null>(null);
  const [records, setRecords] = useState<EnterpriseAccessAuditRecord[]>([]);
  const [pagination, setPagination] = useState<EnterpriseAccessAuditPagination>({ page: 1, page_size: 25, total: 0, has_next: false });
  const [settings, setSettings] = useState<EnterpriseAccessAuditSettings | null>(null);
  const [detail, setDetail] = useState<EnterpriseAccessAuditRecord | null>(null);
  const [detailStatus, setDetailStatus] = useState<DetailStatus>('idle');
  const [detailError, setDetailError] = useState<string | null>(null);
  const requestVersion = useRef(0);
  const detailVersion = useRef(0);

  const metadata = useMemo(() => buildAuditEnterpriseMetadata(keyMetadata, []), [keyMetadata]);
  const serverParams = useMemo<EnterpriseAccessAuditListParams>(() => ({
    from: toIsoTime(filters.from),
    to: toIsoTime(filters.to),
    key_hash: filters.key_hash.trim() || undefined,
    model: filters.model.trim() || undefined,
    source_format: filters.source_format || undefined,
    outcome: filters.outcome || undefined,
    security_signal: filters.security_signal || undefined,
    page,
    page_size: pageSize,
  }), [filters.from, filters.to, filters.key_hash, filters.model, filters.source_format, filters.outcome, filters.security_signal, page, pageSize]);
  const metadataFilterKey = JSON.stringify([filters.username.trim(), filters.email.trim(), filters.department.trim()]);
  const metadataFilterActive = hasAuditEnterpriseMetadataFilters(filters);
  const joinedRecords = useMemo(() => joinAuditEnterpriseMetadata(records, metadata), [records, metadata]);
  const filteredRecords = useMemo(() => filterAuditRecords(joinedRecords, filters), [joinedRecords, filters]);
  const visibleRecords = useMemo(
    () => (metadataFilterActive ? paginateAuditRecords(filteredRecords, page, pageSize) : filteredRecords),
    [filteredRecords, metadataFilterActive, page, pageSize]
  );
  const displayPagination = useMemo(
    () => metadataFilterActive
      ? {
          page,
          page_size: pageSize,
          total: filteredRecords.length,
          has_next: page * pageSize < filteredRecords.length,
        }
      : pagination,
    [filteredRecords.length, metadataFilterActive, page, pageSize, pagination]
  );

  useEffect(() => {
    let active = true;
    void enterpriseKeysApi.listKeyMetadata().then((metadataResult) => {
      if (active) setKeyMetadata(metadataResult.items ?? []);
    }).catch((metadataRequestError) => {
      if (active) setMetadataError(errorMessage(metadataRequestError));
    });
    return () => {
      active = false;
    };
  }, []);

  const loadData = useCallback(async () => {
    const version = ++requestVersion.current;
    setStatus('loading');
    setError(null);
    const loadAuditRecords = async () => {
      if (!metadataFilterActive || metadataFilterKey === '["","",""]') return enterpriseAccessAuditApi.listAudit(serverParams);
      const allRecords: EnterpriseAccessAuditRecord[] = [];
      const fetchPageSize = 100;
      const now = Date.now();
      const configured = toIsoTime(filters.to);
      const configuredTime = configured ? new Date(configured).getTime() : now;
      const snapshotTo = new Date(Math.min(Number.isFinite(configuredTime) ? configuredTime : now, now)).toISOString();
      const snapshotParams = { ...serverParams, to: snapshotTo };
      let currentPage = 1;
      while (true) {
        const response = await enterpriseAccessAuditApi.listAudit({
          ...snapshotParams,
          page: currentPage,
          page_size: fetchPageSize,
        });
        allRecords.push(...(Array.isArray(response.records) ? response.records : []));
        if (!response.pagination?.has_next) {
          return {
            records: allRecords,
            pagination: { page: 1, page_size: fetchPageSize, total: allRecords.length, has_next: false },
          };
        }
        currentPage += 1;
      }
    };
    const [auditResult, settingsResult] = await Promise.allSettled([
      loadAuditRecords(),
      enterpriseAccessAuditApi.getSettings(),
    ]);
    if (version !== requestVersion.current) return;
    if (auditResult.status === 'rejected') {
      setStatus(isUnavailableError(auditResult.reason) ? 'unavailable' : 'error');
      setError(errorMessage(auditResult.reason));
      return;
    }
    setRecords(Array.isArray(auditResult.value.records) ? auditResult.value.records : []);
    setPagination(normalizeAuditPagination(auditResult.value.pagination, page, pageSize));
    if (settingsResult.status === 'fulfilled') setSettings(settingsResult.value);
    setStatus('ready');
    if (settingsResult.status === 'rejected' && isUnavailableError(settingsResult.reason)) {
      setError(t('request_audit.retention_unavailable'));
    }
  }, [filters.to, metadataFilterActive, metadataFilterKey, page, pageSize, serverParams, t]);

  useEffect(() => {
    const timer = setTimeout(() => {
      void loadData();
    }, 0);
    return () => clearTimeout(timer);
  }, [loadData]);

  const updateFilter = (field: keyof RequestAuditFilters, value: string) => {
    setFilters((current) => ({ ...current, [field]: value }));
    setPage(1);
  };

  const clearFilters = () => {
    setFilters(EMPTY_AUDIT_FILTERS);
    setPage(1);
  };

  const openDetail = async (id: number) => {
    const version = ++detailVersion.current;
    setDetail(null);
    setDetailError(null);
    setDetailStatus('loading');
    try {
      const result = await enterpriseAccessAuditApi.getAuditDetail(id);
      if (version !== detailVersion.current) return;
      setDetail(result);
      setDetailStatus('ready');
    } catch (detailRequestError) {
      if (version !== detailVersion.current) return;
      setDetailError(errorMessage(detailRequestError));
      setDetailStatus('error');
    }
  };

  const closeDetail = () => {
    detailVersion.current += 1;
    setDetailStatus('idle');
    setDetail(null);
    setDetailError(null);
  };

  return (
    <RequestAuditView
      t={t}
      status={status}
      error={error}
      metadataError={metadataError}
      records={visibleRecords}
      pagination={displayPagination}
      filters={filters}
      pageSize={pageSize}
      settings={settings}
      detail={detail}
      detailEnterprise={detail ? metadata.get(normalizeEnterprisePolicyHash(detail.key_hash)) : undefined}
      detailStatus={detailStatus}
      detailError={detailError}
      onFilterChange={updateFilter}
      onClearFilters={clearFilters}
      onRefresh={() => void loadData()}
      onPageChange={setPage}
      onPageSizeChange={(value) => { setPageSize(value); setPage(1); }}
      onOpenDetail={(id) => void openDetail(id)}
      onCloseDetail={closeDetail}
    />
  );
}
