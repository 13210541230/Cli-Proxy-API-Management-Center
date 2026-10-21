/**
 * OpenCode Go 账户与额度 API
 */

import { apiClient } from './client';

export type OpenCodeWindow = {
  status?: string;
  percent: number;
  resets_at?: string;
};

export type OpenCodeQuota = {
  rolling: OpenCodeWindow;
  weekly: OpenCodeWindow;
  monthly: OpenCodeWindow;
  fetched_at?: string;
  exhausted: boolean;
};

export type OpenCodeAccount = {
  id: string;
  label?: string;
  provider: string;
  status: string;
  disabled: boolean;
  proxy_url?: string;
  api_key_hint?: string;
  quota?: OpenCodeQuota;
  quota_error?: string;
};

const normalizeWindow = (value: unknown): OpenCodeWindow => {
  const record = (value ?? {}) as Record<string, unknown>;
  const percent = Number(record.percent);
  return {
    status: typeof record.status === 'string' ? record.status : undefined,
    percent: Number.isFinite(percent) ? percent : 0,
    resets_at: typeof record.resets_at === 'string' ? record.resets_at : undefined,
  };
};

const normalizeQuota = (value: unknown): OpenCodeQuota | undefined => {
  if (!value || typeof value !== 'object') return undefined;
  const record = value as Record<string, unknown>;
  return {
    rolling: normalizeWindow(record.rolling),
    weekly: normalizeWindow(record.weekly),
    monthly: normalizeWindow(record.monthly),
    fetched_at: typeof record.fetched_at === 'string' ? record.fetched_at : undefined,
    exhausted: record.exhausted === true,
  };
};

const normalizeAccount = (value: unknown): OpenCodeAccount | null => {
  if (!value || typeof value !== 'object') return null;
  const record = value as Record<string, unknown>;
  const id = String(record.id ?? '').trim();
  if (!id) return null;
  return {
    id,
    label: typeof record.label === 'string' ? record.label : undefined,
    provider: String(record.provider ?? 'opencode'),
    status: String(record.status ?? ''),
    disabled: record.disabled === true,
    proxy_url: typeof record.proxy_url === 'string' ? record.proxy_url : undefined,
    api_key_hint: typeof record.api_key_hint === 'string' ? record.api_key_hint : undefined,
    quota: normalizeQuota(record.quota),
    quota_error: typeof record.quota_error === 'string' ? record.quota_error : undefined,
  };
};

export type OpenCodeModel = {
  id: string;
  protocol: string;
  context?: number;
  max_input?: number;
  max_output?: number;
  tool_call?: boolean;
  reasoning?: boolean;
};

export const openCodeApi = {
  async listModels(): Promise<{ models: OpenCodeModel[]; excluded: string[] }> {
    const payload = await apiClient.get<unknown>('/opencode/models');
    const record = (payload ?? {}) as Record<string, unknown>;
    const rawModels = Array.isArray(record.models) ? record.models : [];
    const models = rawModels.reduce<OpenCodeModel[]>((acc, item) => {
      if (!item || typeof item !== 'object') return acc;
      const entry = item as Record<string, unknown>;
      const id = String(entry.id ?? '').trim();
      if (!id) return acc;
      acc.push({
        id,
        protocol: String(entry.protocol ?? 'chat'),
        context: typeof entry.context === 'number' ? entry.context : undefined,
        max_input: typeof entry.max_input === 'number' ? entry.max_input : undefined,
        max_output: typeof entry.max_output === 'number' ? entry.max_output : undefined,
        tool_call: entry.tool_call === true,
        reasoning: entry.reasoning === true,
      });
      return acc;
    }, []);
    models.sort((left, right) => left.id.localeCompare(right.id));
    const excluded = Array.isArray(record.excluded)
      ? record.excluded.map((item) => String(item ?? '').trim()).filter(Boolean)
      : [];
    return { models, excluded };
  },

  async saveExcluded(excluded: string[]): Promise<{ excluded: string[]; published: number }> {
    const payload = await apiClient.put<unknown>('/opencode/models', { excluded });
    const record = (payload ?? {}) as Record<string, unknown>;
    const list = Array.isArray(record.excluded)
      ? record.excluded.map((item) => String(item ?? '').trim()).filter(Boolean)
      : [];
    return { excluded: list, published: typeof record.published === 'number' ? record.published : 0 };
  },

  async listAccounts(): Promise<OpenCodeAccount[]> {
    const payload = await apiClient.get<unknown>('/opencode/accounts');
    const list = Array.isArray(payload)
      ? payload
      : Array.isArray((payload as { accounts?: unknown[] })?.accounts)
        ? ((payload as { accounts?: unknown[] }).accounts ?? [])
        : [];
    return list
      .map(normalizeAccount)
      .filter((entry): entry is OpenCodeAccount => entry !== null)
      .sort((left, right) => left.id.localeCompare(right.id));
  },
};
