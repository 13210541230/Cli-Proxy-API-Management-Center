import { act, type ComponentProps, type ReactNode } from 'react';
import { create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { AuthFileItem } from '@/types';
import type { ModelTraceProgress, ModelTraceState } from '@/services/api/modelTrace';
import { CodexModelTraceSection } from './CodexModelTraceSection';

const mocks = vi.hoisted(() => ({
  state: vi.fn(), start: vi.fn(), cancel: vi.fn(), record: vi.fn(), models: vi.fn(),
  auth: { apiBase: 'manager-a', managementKey: 'fake-key', connectionStatus: 'connected' },
}));
vi.mock('@/services/api/modelTrace', () => ({ modelTraceApi: mocks }));
vi.mock('@/services/api/authFiles', () => ({ authFilesApi: { getModelsForAuthFile: mocks.models } }));
vi.mock('@/stores', () => ({ useAuthStore: (selector: (state: typeof mocks.auth) => unknown) => selector(mocks.auth) }));
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock('./QuotaSection', () => ({ QuotaSection: ({ files, renderCardStatus }: {
  files: AuthFileItem[]; renderCardStatus: (file: AuthFileItem) => ReactNode;
}) => <section>{files.filter((file) => file.type === 'codex').map((file) => <div key={file.name} data-file={file.name}>{renderCardStatus(file)}</div>)}</section> }));
vi.mock('@/components/ui/Modal', () => ({ Modal: ({ open, children, footer }: { open: boolean; children: ReactNode; footer: ReactNode }) => open ? <aside>{children}{footer}</aside> : null }));
vi.mock('@/components/ui/Button', () => ({ Button: ({ children, onClick, disabled }: ComponentProps<'button'>) => <button onClick={onClick} disabled={disabled}>{children}</button> }));
vi.mock('@/components/ui/Select', () => ({ Select: ({ options, value, onChange }: { options: Array<{ value: string; label: string }>; value: string; onChange: (value: string) => void }) => <select value={value} onChange={(event) => onChange(event.target.value)}>{options.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}</select> }));
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const files: AuthFileItem[] = [
  { name: 'a.json', type: 'codex', authIndex: 'a' },
  { name: 'b.json', type: 'codex', authIndex: 'b', disabled: true },
  { name: 'c.json', type: 'claude', authIndex: 'c' },
];
const state: ModelTraceState = { bank_revision: 'bank', models: [{ id: 'gpt-5.5', display_name: 'GPT 5.5' }], states: {} };
let renderer: ReactTestRenderer | undefined;
const mount = async () => {
  await act(async () => { renderer = create(<CodexModelTraceSection files={files} loading={false} disabled={false} />); });
  return renderer!.root;
};
const namedButton = (key: string) => renderer!.root.findAllByType('button').find((button) => button.children.join('') === key)!;
const cardButton = (file: string, key: string) => renderer!.root.findByProps({ 'data-file': file }).findAllByType('button').find((button) => button.children.join('') === key)!;
const run: ModelTraceProgress = { id: 'run-a', model: 'gpt-5.5', phase: 'collecting', done: 0, total: 3, started_at: '2026-10-08T00:00:00Z' };
const activeState: ModelTraceState = { ...state, states: { a: { running: run } } };
const deferred = <T,>() => {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
};

beforeEach(() => {
  vi.resetAllMocks();
  mocks.auth.apiBase = 'manager-a';
  mocks.state.mockResolvedValue(state);
  mocks.models.mockResolvedValue([{ id: 'gpt-5.5' }, { id: 'not-in-bank' }]);
  mocks.start.mockResolvedValue({ id: 'run-a', model: 'gpt-5.5', phase: 'collecting', done: 0, total: 3, started_at: '2026-10-08T00:00:00Z' });
});
afterEach(async () => {
  if (renderer) await act(async () => renderer!.unmount());
  renderer = undefined;
  vi.useRealTimers();
});

describe('quota-only Codex ModelTrace', () => {
  it('loads one batched state and keeps disabled credential start controls disabled', async () => {
    const root = await mount();
    expect(mocks.state).toHaveBeenCalledTimes(1);
    expect(root.findAllByProps({ 'data-file': 'c.json' })).toHaveLength(0);
    expect(root.findByProps({ 'data-file': 'b.json' }).findByType('button').props.disabled).toBe(true);
  });

  it('requires model/cost confirmation and sends only the clicked credential', async () => {
    await mount();
    await act(async () => namedButton('modeltrace.button').props.onClick());
    expect(mocks.start).not.toHaveBeenCalled();
    expect(renderer!.root.findAllByType('select')[0].findAllByType('option')).toHaveLength(1);
    expect(JSON.stringify(renderer!.toJSON())).toContain('modeltrace.cost');
    await act(async () => namedButton('modeltrace.start').props.onClick());
    expect(mocks.start).toHaveBeenCalledWith('a', 'gpt-5.5', expect.any(AbortSignal));
  });

  it('shows an upgrade notice without hiding quota cards when old CPA lacks the endpoint', async () => {
    mocks.state.mockRejectedValue(Object.assign(new Error('unsupported'), { status: 404 }));
    const root = await mount();
    expect(JSON.stringify(renderer!.toJSON())).toContain('modeltrace.upgrade');
    expect(root.findAllByProps({ 'data-file': 'a.json' })).toHaveLength(1);
    expect(namedButton('modeltrace.button').props.disabled).toBe(true);
  });

  it('keeps a paid start request alive through quota refresh and clears only its pending state', async () => {
    const request = deferred<ModelTraceProgress>();
    mocks.start.mockReturnValueOnce(request.promise);
    await mount();
    await act(async () => namedButton('modeltrace.button').props.onClick());
    await act(async () => namedButton('modeltrace.start').props.onClick());
    const signal = mocks.start.mock.calls[0][2] as AbortSignal;
    await act(async () => renderer!.update(<CodexModelTraceSection files={files} loading disabled={false} />));
    await act(async () => renderer!.update(<CodexModelTraceSection files={files} loading={false} disabled={false} />));
    expect(signal.aborted).toBe(false);
    await act(async () => request.resolve(run));
    await act(async () => namedButton('modeltrace.button').props.onClick());
    expect(namedButton('modeltrace.start').props.disabled).toBe(false);
    expect(mocks.start).toHaveBeenCalledTimes(1);
  });

  it('preserves confirmed running state and polling when the first post-start GET fails', async () => {
    vi.useFakeTimers();
    mocks.state.mockResolvedValueOnce(state).mockRejectedValueOnce(new Error('temporary 502')).mockResolvedValue(activeState);
    await mount();
    await act(async () => namedButton('modeltrace.button').props.onClick());
    await act(async () => namedButton('modeltrace.start').props.onClick());
    expect(cardButton('a.json', 'modeltrace.cancel')).toBeDefined();
    expect(mocks.state).toHaveBeenCalledTimes(2);
    await act(async () => vi.advanceTimersByTimeAsync(2500));
    expect(mocks.state).toHaveBeenCalledTimes(3);
    expect(cardButton('a.json', 'modeltrace.cancel')).toBeDefined();
  });

  it('tracks two simultaneous cancellations without clearing or aborting the other card', async () => {
    const activeFiles = files.map((file) => ({ ...file, disabled: false }));
    const requestA = deferred<{ cancelled: boolean }>();
    const requestB = deferred<{ cancelled: boolean }>();
    mocks.state.mockResolvedValue({ ...state, states: { a: { running: run }, b: { running: { ...run, id: 'run-b' } } } });
    mocks.cancel.mockImplementation((id: string) => id === 'a' ? requestA.promise : requestB.promise);
    await act(async () => { renderer = create(<CodexModelTraceSection files={activeFiles} loading={false} disabled={false} />); });
    await act(async () => cardButton('a.json', 'modeltrace.cancel').props.onClick());
    await act(async () => cardButton('b.json', 'modeltrace.cancel').props.onClick());
    expect(cardButton('a.json', 'modeltrace.cancel').props.disabled).toBe(true);
    expect(cardButton('b.json', 'modeltrace.cancel').props.disabled).toBe(true);
    const signalB = mocks.cancel.mock.calls[1][1] as AbortSignal;
    await act(async () => requestA.resolve({ cancelled: true }));
    expect(signalB.aborted).toBe(false);
    expect(cardButton('b.json', 'modeltrace.cancel').props.disabled).toBe(true);
    await act(async () => requestB.resolve({ cancelled: true }));
    expect(cardButton('b.json', 'modeltrace.cancel').props.disabled).toBe(false);
  });

  it('aborts mutation on disconnect but does not retain pending or publish its late result on reconnect', async () => {
    const request = deferred<ModelTraceProgress>();
    mocks.start.mockReturnValueOnce(request.promise);
    await mount();
    await act(async () => namedButton('modeltrace.button').props.onClick());
    await act(async () => namedButton('modeltrace.start').props.onClick());
    const signal = mocks.start.mock.calls[0][2] as AbortSignal;
    await act(async () => renderer!.update(<CodexModelTraceSection files={files} loading={false} disabled />));
    expect(signal.aborted).toBe(true);
    await act(async () => renderer!.update(<CodexModelTraceSection files={files} loading={false} disabled={false} />));
    await act(async () => namedButton('modeltrace.button').props.onClick());
    expect(namedButton('modeltrace.start').props.disabled).toBe(false);
    await act(async () => request.resolve(run));
    expect(namedButton('modeltrace.start').props.disabled).toBe(false);
    expect(cardButton('a.json', 'modeltrace.cancel')).toBeUndefined();
  });

  it('reconciles an uncertain POST outcome via GET retries without sending another paid POST', async () => {
    vi.useFakeTimers();
    mocks.start.mockRejectedValueOnce(new Error('POST response lost'));
    mocks.state.mockResolvedValueOnce(state).mockRejectedValueOnce(new Error('temporary 502')).mockResolvedValue(activeState);
    await mount();
    await act(async () => namedButton('modeltrace.button').props.onClick());
    await act(async () => namedButton('modeltrace.start').props.onClick());
    await act(async () => vi.advanceTimersByTimeAsync(2500));
    expect(mocks.state).toHaveBeenCalledTimes(3);
    expect(mocks.start).toHaveBeenCalledTimes(1);
    expect(cardButton('a.json', 'modeltrace.cancel')).toBeDefined();
    expect(namedButton('modeltrace.start').props.disabled).toBe(true);
  });

  it('aborts stale state subscriptions when switching management servers', async () => {
    let resolveOld!: (value: typeof state) => void;
    mocks.state.mockImplementationOnce(() => new Promise((resolve) => { resolveOld = resolve; }));
    await mount();
    const oldSignal = mocks.state.mock.calls[0][0] as AbortSignal;
    mocks.auth.apiBase = 'manager-b';
    await act(async () => renderer!.update(<CodexModelTraceSection files={files} loading={false} disabled={false} />));
    expect(oldSignal.aborted).toBe(true);
    await act(async () => resolveOld({ ...state, storage_error: 'old-server-error' }));
    expect(JSON.stringify(renderer!.toJSON())).not.toContain('old-server-error');
  });
});
