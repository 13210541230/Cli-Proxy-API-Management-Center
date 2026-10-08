import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Modal } from '@/components/ui/Modal';
import { Select } from '@/components/ui/Select';
import { useAuthStore } from '@/stores';
import { authFilesApi } from '@/services/api/authFiles';
import { modelTraceApi, type ModelTraceState, type ModelTraceRecord, type ModelTraceModel } from '@/services/api/modelTrace';
import type { AuthFileItem } from '@/types';
import { QuotaSection } from './QuotaSection';
import { CODEX_CONFIG, type QuotaSortMode } from './quotaConfigs';
import { getModelTraceLabel, getModelTraceMatch, hasActiveModelTrace, modelTraceAuthIndex, supportedModelTraceModels } from './modelTracePresentation';
import styles from './ModelTrace.module.scss';

type Scope = object;
type Mutation = { scope: Scope; authIndex: string; controller: AbortController };
type Selection = { scope: Scope; file: AuthFileItem; mode: 'start' | 'details' };
type Snapshot = { scope: Scope; data?: ModelTraceState; error?: string; unsupported?: boolean };
type DialogData = { file: AuthFileItem; models?: ModelTraceModel[]; model?: string; record?: ModelTraceRecord; loading: boolean; error?: string };
interface Props {
  files: AuthFileItem[];
  loading: boolean;
  disabled: boolean;
  searchQuery?: string;
  sortMode?: QuotaSortMode;
}
const messageOf = (error: unknown) => error instanceof Error ? error.message : String(error);
const formatTime = (value: string) => new Date(value).toLocaleString();

export function CodexModelTraceSection({ files, loading, disabled, searchQuery, sortMode }: Props) {
  const { t } = useTranslation();
  const apiBase = useAuthStore((state) => state.apiBase);
  const managementKey = useAuthStore((state) => state.managementKey);
  // An opaque identity prevents previous-server results from appearing during effect cleanup.
  const scope = useMemo(() => ({ apiBase, managementKey }), [apiBase, managementKey]);
  const [snapshot, setSnapshot] = useState<Snapshot>();
  const [refreshKey, setRefreshKey] = useState(0);
  const [selection, setSelection] = useState<Selection | null>(null);
  const [dialogData, setDialogData] = useState<DialogData>();
  const [pending, setPending] = useState<Mutation[]>([]);
  const [actionError, setActionError] = useState<{ scope: Scope; message: string }>();
  const subscription = useRef<AbortController | null>(null);
  const mutations = useRef(new Map<string, Mutation>());
  const retained = useRef<Snapshot | undefined>(undefined);
  const needsReconcile = useRef<Scope | undefined>(undefined);
  const remember = useCallback((next: Snapshot) => {
    retained.current = next;
    setSnapshot(next);
  }, []);
  const hasCodexFiles = files.some(CODEX_CONFIG.filterFn);
  const current = !disabled && snapshot?.scope === scope ? snapshot : undefined;
  const data = current?.data;
  const states = data?.states ?? {};
  const selected = !disabled && selection?.scope === scope ? selection : null;
  const selectedState = selected ? states[modelTraceAuthIndex(selected.file)] : undefined;
  const latestId = selectedState?.latest?.id;
  const bank = data?.models;

  // Query refreshes must not cancel paid mutations. Only the connection lifecycle owns them.
  useEffect(() => {
    const owned = new Map<string, Mutation>();
    mutations.current = owned;
    return () => {
      if (owned.size) needsReconcile.current = scope;
      owned.forEach((operation) => operation.controller.abort());
      owned.clear();
      setPending((previous) => previous.filter((operation) => operation.scope !== scope));
    };
  }, [scope, disabled]);

  useEffect(() => {
    if (disabled || loading || !hasCodexFiles) return;
    const controller = new AbortController();
    subscription.current = controller;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let last = retained.current?.scope === scope ? retained.current.data : undefined;
    const load = async () => {
      try {
        const next = await modelTraceApi.state(controller.signal);
        if (controller.signal.aborted) return;
        if (last?.bank_revision === next.bank_revision) next.models = last.models;
        last = next;
        if (needsReconcile.current === scope) needsReconcile.current = undefined;
        remember({ scope, data: next });
        if (hasActiveModelTrace(next.states)) timer = setTimeout(() => void load(), 2500);
      } catch (error: unknown) {
        if (controller.signal.aborted) return;
        const unsupported = (error as { status?: number }).status === 404;
        remember({ scope, data: last, error: messageOf(error), unsupported });
        if (!unsupported && ((last && hasActiveModelTrace(last.states)) || needsReconcile.current === scope)) {
          timer = setTimeout(() => void load(), 2500);
        }
      }
    };
    void load();
    return () => {
      controller.abort();
      if (subscription.current === controller) subscription.current = null;
      if (timer) clearTimeout(timer);
    };
  }, [scope, disabled, loading, hasCodexFiles, files, refreshKey, remember]);

  useEffect(() => {
    if (!selected || !bank) return;
    const controller = new AbortController();
    const { file, mode } = selected;
    setDialogData({ file, loading: true });
    const load = async () => {
      try {
        if (mode === 'details') {
          const record = await modelTraceApi.record(modelTraceAuthIndex(file), latestId, controller.signal);
          if (!controller.signal.aborted) setDialogData({ file, record, loading: false });
        } else {
          const catalog = await authFilesApi.getModelsForAuthFile(file.name);
          if (controller.signal.aborted) return;
          const models = supportedModelTraceModels(bank, catalog);
          setDialogData({ file, models, model: models[0]?.id ?? '', loading: false });
        }
      } catch (error: unknown) {
        if (!controller.signal.aborted) setDialogData({ file, loading: false, error: messageOf(error) });
      }
    };
    void load();
    return () => controller.abort();
  }, [selected, latestId, bank]);

  const refresh = () => {
    subscription.current?.abort();
    setRefreshKey((key) => key + 1);
  };
  const open = (file: AuthFileItem, mode: Selection['mode']) => {
    setActionError(undefined);
    setSelection({ scope, file, mode });
  };
  const beginMutation = (authIndex: string) => {
    if (disabled || mutations.current.has(authIndex)) return;
    const operation: Mutation = { scope, authIndex, controller: new AbortController() };
    mutations.current.set(authIndex, operation);
    setPending([...mutations.current.values()]);
    setActionError(undefined);
    return operation;
  };
  const isCurrentMutation = (operation: Mutation) => !operation.controller.signal.aborted && mutations.current.get(operation.authIndex) === operation;
  const finishMutation = (operation: Mutation) => {
    if (mutations.current.get(operation.authIndex) !== operation) return;
    mutations.current.delete(operation.authIndex);
    setPending([...mutations.current.values()]);
  };
  const reconcile = () => {
    needsReconcile.current = scope;
    refresh(); // GET only: a lost POST response may still represent an accepted paid task.
  };
  const dialog = dialogData?.file === selected?.file ? dialogData : undefined;
  const selectedBusy = pending.some((operation) => operation.scope === scope && operation.authIndex === (selected && modelTraceAuthIndex(selected.file)));
  const start = async () => {
    if (loading || current?.error || !selected || !dialog?.model || selectedState?.running) return;
    const authIndex = modelTraceAuthIndex(selected.file);
    const operation = beginMutation(authIndex);
    if (!operation) return;
    try {
      const run = await modelTraceApi.start(authIndex, dialog.model, operation.controller.signal);
      if (!isCurrentMutation(operation)) return;
      const previous = retained.current;
      if (previous?.scope === scope && previous.data) {
        remember({ scope, data: { ...previous.data, states: { ...previous.data.states, [authIndex]: { ...previous.data.states[authIndex], running: run } } } });
      }
      setSelection((previousSelection) => previousSelection?.scope === scope ? null : previousSelection);
      reconcile();
    } catch (error: unknown) {
      if (isCurrentMutation(operation)) {
        setActionError({ scope, message: messageOf(error) });
        reconcile();
      }
    } finally {
      finishMutation(operation);
    }
  };
  const cancel = async (file: AuthFileItem) => {
    const authIndex = modelTraceAuthIndex(file);
    const operation = beginMutation(authIndex);
    if (!operation) return;
    try {
      await modelTraceApi.cancel(authIndex, operation.controller.signal);
      if (isCurrentMutation(operation)) reconcile();
    } catch (error: unknown) {
      if (isCurrentMutation(operation)) {
        setActionError({ scope, message: messageOf(error) });
        reconcile();
      }
    } finally {
      finishMutation(operation);
    }
  };
  const actionMessage = actionError?.scope === scope ? actionError.message : undefined;
  const renderRecord = (record: ModelTraceRecord) => (
    <div className={styles.record}>
      <span className={`${styles.badge} ${styles[getModelTraceLabel({ latest: record })]}`}>
        {t(`modeltrace.${getModelTraceLabel({ latest: record })}`)}
      </span>
      <dl className={styles.metrics}>
        <div><dt>{t('modeltrace.model')}</dt><dd>{record.model}</dd></div>
        <div><dt>{t('modeltrace.inferred')}</dt><dd>{record.attribution?.prediction ?? '—'}</dd></div>
        <div className={styles.wide}><dt>{t('modeltrace.fingerprint')}</dt><dd>{t(`modeltrace.${getModelTraceMatch(record)}`)}</dd></div>
        <div><dt>{t('modeltrace.probability')}</dt><dd>{record.attribution ? `${(record.attribution.probability * 100).toFixed(1)}%` : '—'}</dd></div>
        <div><dt>{t('modeltrace.used')}</dt><dd>{record.attribution?.used_outputs ?? 0}/3</dd></div>
        <div><dt>{t('modeltrace.checked_at')}</dt><dd>{formatTime(record.time)}</dd></div>
        <div><dt>{t('modeltrace.duration')}</dt><dd>{(record.duration_ms / 1000).toFixed(1)}s</dd></div>
        <div className={styles.wide}><dt>{t('modeltrace.tokens')}</dt><dd>{[record.input_tokens, record.output_tokens, record.reasoning_tokens].map((value) => value.toLocaleString()).join(' / ')}</dd></div>
        <div className={styles.wide}><dt>{t('modeltrace.bank')}</dt><dd className={styles.revision}>{record.bank_revision}</dd></div>
      </dl>
      {record.error && <div className={styles.error} role="alert">{record.error}</div>}
      {record.storage_error && <div className={styles.error} role="alert">{t('modeltrace.save_failed', { message: record.storage_error })}</div>}
      {record.attribution?.results && <div><h4>{t('modeltrace.ranking')}</h4><ol className={styles.ranking}>
        {record.attribution.results.slice(0, 5).map((candidate) => <li key={candidate.model}><span>{candidate.display_name || candidate.model}</span><strong>{(candidate.probability * 100).toFixed(1)}%</strong></li>)}
      </ol></div>}
      {record.samples?.map((sample, index) => <details className={styles.sample} key={index}>
        <summary>{t('modeltrace.evidence', { number: index + 1, parsed: sample.parsed_numbers, expected: sample.expected_count })}</summary>
        {sample.error && <p className={styles.error}>{sample.error}</p>}
        <h5>{t('modeltrace.prompt')}</h5><pre>{sample.prompt}</pre>
        <h5>{t('modeltrace.raw_answer')}</h5><pre>{sample.text || '—'}</pre>
      </details>)}
    </div>
  );

  return <>
    <QuotaSection config={CODEX_CONFIG} files={files} loading={loading} disabled={disabled} searchQuery={searchQuery} sortMode={sortMode}
      renderCardStatus={(file) => {
        const authIndex = modelTraceAuthIndex(file);
        const state = states[authIndex];
        const label = getModelTraceLabel(state);
        const busy = pending.some((operation) => operation.scope === scope && operation.authIndex === authIndex);
        return <div className={styles.cardStatus}>
          <div className={styles.statusLine} aria-live="polite">
            <span className={styles.caption}>ModelTrace</span>
            <span className={`${styles.badge} ${styles[label]}`}>{t(`modeltrace.${label}`, { done: state?.running?.done, total: state?.running?.total })}</span>
          </div>
          {state?.latest && <div className={styles.meta}>
            {state.running
              ? t('modeltrace.previous', { label: t(`modeltrace.${getModelTraceLabel({ latest: state.latest })}`), time: formatTime(state.latest.time) })
              : <><span>{state.latest.model}{state.latest.attribution ? ` → ${state.latest.attribution.prediction}` : ''}</span><span>{formatTime(state.latest.time)}</span></>}
          </div>}
          {state?.latest && !state.running && <div className={styles.meta}>
            {t('modeltrace.fingerprint')}: {t(`modeltrace.${getModelTraceMatch(state.latest)}`)}
          </div>}
          {state?.latest?.error && !state.running && <p className={styles.inlineError}>{state.latest.error}</p>}
          <div className={styles.actions}>
            <Button variant="secondary" size="sm" onClick={() => open(file, 'start')}
              disabled={disabled || Boolean(file.disabled || file.unavailable || !authIndex || !data || current?.error || state?.running || busy)}>{t('modeltrace.button')}</Button>
            {state?.running && <Button variant="secondary" size="sm" disabled={disabled || busy || state.running.phase === 'cancelling'} onClick={() => void cancel(file)}>{t('modeltrace.cancel')}</Button>}
            {state?.latest && <Button variant="secondary" size="sm" disabled={disabled} onClick={() => open(file, 'details')}>{t('modeltrace.details')}</Button>}
          </div>
        </div>;
      }}
    />
    {(current?.error || data?.storage_error || actionMessage) && <div className={styles.notice} role="alert">
      <span>{current?.unsupported ? t('modeltrace.upgrade') : current?.error ? t('modeltrace.state_failed', { message: current.error }) : data?.storage_error ? t('modeltrace.save_failed', { message: data.storage_error }) : actionMessage}</span>
      <Button variant="secondary" size="sm" onClick={refresh} disabled={disabled || loading}>{t('modeltrace.retry')}</Button>
    </div>}
    <Modal open={Boolean(selected)} title={<span className={styles.dialogTitle} title={selected?.file.name}>{t('modeltrace.title', { name: selected?.file.name })}</span>}
      onClose={() => setSelection(null)} width={700} footer={<>
        <Button variant="secondary" onClick={() => setSelection(null)}>{t('common.close')}</Button>
        {selected?.mode === 'start' && <Button onClick={() => void start()}
          disabled={disabled || loading || Boolean(current?.error) || selectedBusy || dialog?.loading || !dialog?.model || Boolean(selectedState?.running)}>{t('modeltrace.start')}</Button>}
      </>}>
      <div className={styles.dialogBody}>
        {selected?.mode === 'start' && <>
          <div className={styles.cost}>{t('modeltrace.cost')}</div>
          <label className={styles.modelLabel}>{t('modeltrace.model')}</label>
          <Select value={dialog?.model ?? ''} options={(dialog?.models ?? []).map((model) => ({ value: model.id, label: model.display_name || model.id }))}
            onChange={(model) => setDialogData((previous) => previous ? { ...previous, model } : previous)}
            disabled={Boolean(dialog?.loading || !dialog?.models?.length)} ariaLabel={t('modeltrace.model')} fullWidth />
          {dialog?.loading ? <p className={styles.note}>{t('modeltrace.loading_models')}</p> : dialog?.models?.length === 0 && <p className={styles.note}>{t('modeltrace.no_models')}</p>}
        </>}
        <p className={styles.note}>{t('modeltrace.caveat')}</p>
        {selected?.mode === 'details' && (dialog?.loading ? <p>{t('modeltrace.loading_record')}</p> : dialog?.record && renderRecord(dialog.record))}
        {(dialog?.error || actionMessage) && <p className={styles.error} role="alert">{dialog?.error || actionMessage}</p>}
        <p className={styles.note}>{t('modeltrace.running_note')}</p>
      </div>
    </Modal>
  </>;
}
