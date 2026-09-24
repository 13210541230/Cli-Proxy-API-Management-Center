/**
 * AI 提供商相关 API
 */

import { apiClient } from './client';
import {
  normalizeGeminiKeyConfig,
  normalizeOpenAIProvider,
  normalizeProviderKeyConfig
} from './transformers';
import type {
  GeminiKeyConfig,
  OpenAIProviderConfig,
  ProviderKeyConfig,
  ApiKeyEntry,
  ModelAlias
} from '@/types';

const serializeHeaders = (headers?: Record<string, string>) => (headers && Object.keys(headers).length ? headers : undefined);

const isRecord = (value: unknown): value is Record<string, unknown> =>
  value !== null && typeof value === 'object' && !Array.isArray(value);

const extractArrayPayload = (data: unknown, key: string): unknown[] => {
  if (Array.isArray(data)) return data;
  if (!isRecord(data)) return [];
  const candidate = data[key] ?? data.items ?? data.data ?? data;
  return Array.isArray(candidate) ? candidate : [];
};

const buildProviderDeleteQuery = (apiKey: string, baseUrl?: string) => {
  const params = new URLSearchParams();
  params.set('api-key', apiKey.trim());
  params.set('base-url', (baseUrl ?? '').trim());
  return `?${params.toString()}`;
};

const serializeModelAliases = (models?: ModelAlias[]) =>
  Array.isArray(models)
    ? models
        .map((model) => {
          if (!model?.name) return null;
          const payload: Record<string, unknown> = { name: model.name };
          if (model.alias && model.alias !== model.name) {
            payload.alias = model.alias;
          }
          if (model.priority !== undefined) {
            payload.priority = model.priority;
          }
          if (model.testModel) {
            payload['test-model'] = model.testModel;
          }
          if (model.image) payload.image = true;
          if (model.thinking) payload.thinking = model.thinking;
          return payload;
        })
        .filter(Boolean)
    : undefined;

const serializeApiKeyEntry = (entry: ApiKeyEntry) => {
  const payload: Record<string, unknown> = { 'api-key': entry.apiKey };
  if (entry.proxyUrl) payload['proxy-url'] = entry.proxyUrl;
  if (entry.weight !== undefined) payload.weight = entry.weight;
  const headers = serializeHeaders(entry.headers);
  if (headers) payload.headers = headers;
  return payload;
};

const serializeProviderKey = (config: ProviderKeyConfig) => {
  const payload: Record<string, unknown> = { 'api-key': config.apiKey };
  if (config.priority !== undefined) payload.priority = config.priority;
  if (config.weight !== undefined) payload.weight = config.weight;
  if (config.prefix?.trim()) payload.prefix = config.prefix.trim();
  if (config.baseUrl) payload['base-url'] = config.baseUrl;
  if (config.websockets !== undefined) payload.websockets = config.websockets;
  if (config.proxyUrl) payload['proxy-url'] = config.proxyUrl;
  const headers = serializeHeaders(config.headers);
  if (headers) payload.headers = headers;
  const models = serializeModelAliases(config.models);
  if (models && models.length) payload.models = models;
  if (config.excludedModels && config.excludedModels.length) {
    payload['excluded-models'] = config.excludedModels;
  }
  if (config.disableCooling) payload['disable-cooling'] = true;
  if (config.cloak) {
    const cloakPayload: Record<string, unknown> = {};
    const mode = config.cloak.mode?.trim();
    if (mode) cloakPayload.mode = mode;
    if (config.cloak.strictMode !== undefined) cloakPayload['strict-mode'] = config.cloak.strictMode;
    if (config.cloak.sensitiveWords && config.cloak.sensitiveWords.length) {
      cloakPayload['sensitive-words'] = config.cloak.sensitiveWords;
    }
    if (config.cloak.cacheUserId) cloakPayload['cache-user-id'] = true;
    if (Object.keys(cloakPayload).length) {
      payload.cloak = cloakPayload;
    }
  }
  if (config.fingerprintProfile?.trim()) payload['fingerprint-profile'] = config.fingerprintProfile.trim();
  return payload;
};

const serializeVertexModelAliases = (models?: ModelAlias[]) =>
  Array.isArray(models)
    ? models
        .map((model) => {
          const name = typeof model?.name === 'string' ? model.name.trim() : '';
          const alias = typeof model?.alias === 'string' ? model.alias.trim() : '';
          if (!name || !alias) return null;
          return { name, alias, ...(model.thinking ? { thinking: model.thinking } : {}) };
        })
        .filter(Boolean)
    : undefined;

const serializeVertexKey = (config: ProviderKeyConfig) => {
  const payload: Record<string, unknown> = { 'api-key': config.apiKey };
  if (config.priority !== undefined) payload.priority = config.priority;
  if (config.weight !== undefined) payload.weight = config.weight;
  if (config.prefix?.trim()) payload.prefix = config.prefix.trim();
  if (config.baseUrl) payload['base-url'] = config.baseUrl;
  if (config.proxyUrl) payload['proxy-url'] = config.proxyUrl;
  const headers = serializeHeaders(config.headers);
  if (headers) payload.headers = headers;
  const models = serializeVertexModelAliases(config.models);
  if (models && models.length) payload.models = models;
  if (config.excludedModels && config.excludedModels.length) {
    payload['excluded-models'] = config.excludedModels;
  }
  return payload;
};

const serializeGeminiKey = (config: GeminiKeyConfig) => {
  const payload: Record<string, unknown> = { 'api-key': config.apiKey };
  if (config.priority !== undefined) payload.priority = config.priority;
  if (config.weight !== undefined) payload.weight = config.weight;
  if (config.disableCooling) payload['disable-cooling'] = true;
  if (config.prefix?.trim()) payload.prefix = config.prefix.trim();
  if (config.baseUrl) payload['base-url'] = config.baseUrl;
  if (config.proxyUrl) payload['proxy-url'] = config.proxyUrl;
  const headers = serializeHeaders(config.headers);
  if (headers) payload.headers = headers;
  const models = serializeModelAliases(config.models);
  if (models && models.length) payload.models = models;
  if (config.excludedModels && config.excludedModels.length) {
    payload['excluded-models'] = config.excludedModels;
  }
  return payload;
};

const serializeOpenAIProvider = (provider: OpenAIProviderConfig) => {
  const payload: Record<string, unknown> = {
    name: provider.name,
    'base-url': provider.baseUrl,
    'api-key-entries': Array.isArray(provider.apiKeyEntries)
      ? provider.apiKeyEntries.map((entry) => serializeApiKeyEntry(entry))
      : []
  };
  if (provider.prefix?.trim()) payload.prefix = provider.prefix.trim();
  if (provider.disabled !== undefined) payload.disabled = provider.disabled;
  const headers = serializeHeaders(provider.headers);
  if (headers) payload.headers = headers;
  const models = serializeModelAliases(provider.models);
  if (models && models.length) payload.models = models;
  if (provider.priority !== undefined) payload.priority = provider.priority;
  if (provider.testModel) payload['test-model'] = provider.testModel;
  if (provider.disableCooling !== undefined) payload['disable-cooling'] = provider.disableCooling;
  return payload;
};

// ---- Preservation merge for full-list PUT saves ----
// CPA replaces whole credential/provider lists on PUT while serializers only emit the
// fields this panel models. Fields added by newer CLIProxyAPI versions (request-retry,
// weight, request-scoped-errors, support-prompt-cache-key, ...) would therefore be
// wiped from config.yaml by every save. Merge each outgoing item over the raw server
// copy: unmanaged keys survive untouched, managed keys follow the serializer
// (present = set, absent = cleared), and lists of equal length merge positionally so
// identity edits (renames, key rotation) keep their unmodeled fields as well.

type SavedMergeSpec = {
  managed: string[];
  identity?: string;
  nest?: { key: string; managed: string[]; identity?: string };
};

const mergeSavedList = (
  rawList: unknown,
  sentList: Record<string, unknown>[],
  spec: SavedMergeSpec
): Record<string, unknown>[] => {
  const raws = Array.isArray(rawList) ? rawList : [];
  const mergeOne = (raw: unknown, sent: Record<string, unknown>): Record<string, unknown> => {
    const base = isRecord(raw) ? raw : {};
    const merged: Record<string, unknown> = { ...base, ...sent };
    for (const key of spec.managed) {
      if (!(key in sent)) delete merged[key];
    }
    if (spec.nest && Array.isArray(sent[spec.nest.key])) {
      const rawEntries = isRecord(base) && Array.isArray(base[spec.nest.key]) ? base[spec.nest.key] : [];
      merged[spec.nest.key] = mergeSavedList(rawEntries, sent[spec.nest.key] as Record<string, unknown>[], {
        managed: spec.nest.managed,
        identity: spec.nest.identity
      });
    }
    if (Array.isArray(sent.models)) {
      const rawModels = isRecord(base) && Array.isArray(base.models) ? base.models : [];
      merged.models = mergeSavedList(rawModels, (sent.models as unknown[]).filter(isRecord), {
        managed: ['name', 'alias', 'priority', 'test-model', 'image', 'thinking'],
        identity: 'name'
      });
    }
    return merged;
  };
  if (raws.length === sentList.length) {
    return sentList.map((sent, index) => mergeOne(raws[index], sent));
  }
  if (spec.identity) {
    const used = new Set<number>();
    return sentList.map((sent) => {
      const idx = raws.findIndex(
        (raw, index) => !used.has(index) && isRecord(raw) && raw[spec.identity as string] === sent[spec.identity as string]
      );
      if (idx >= 0) {
        used.add(idx);
        return mergeOne(raws[idx], sent);
      }
      return mergeOne(undefined, sent);
    });
  }
  return sentList.map((sent) => mergeOne(undefined, sent));
};

const fetchRawList = async (path: string, alias: string): Promise<unknown[]> => {
  try {
    const data = await apiClient.get(path);
    return extractArrayPayload(data, alias);
  } catch {
    return [];
  }
};

const getProviderKeyConfigs = async <T>(
  path: string,
  alias: string,
  normalize: (item: unknown) => T | null
): Promise<T[]> => {
  const data = await apiClient.get(path);
  return extractArrayPayload(data, alias).map(normalize).filter((item): item is T => item !== null);
};

const saveProviderKeyConfigs = async <T>(
  path: string,
  alias: string,
  configs: T[],
  serialize: (config: T) => Record<string, unknown>,
  managed: string[]
) => {
  const raw = await fetchRawList(path, alias);
  const sent = configs.map(serialize);
  await apiClient.put(path, mergeSavedList(raw, sent, { managed, identity: 'api-key' }));
};

// Keys owned by each serializer: present in its output = set, absent = cleared.
const PROVIDER_KEY_MANAGED = [
  'api-key', 'priority', 'weight', 'prefix', 'base-url', 'websockets', 'proxy-url', 'headers', 'models',
  'excluded-models', 'disable-cooling', 'cloak', 'fingerprint-profile'
];
const SIMPLE_KEY_MANAGED = [
  'api-key', 'priority', 'weight', 'prefix', 'base-url', 'proxy-url', 'headers', 'models',
  'excluded-models', 'disable-cooling'
];
const OPENAI_PROVIDER_MANAGED = [
  'name', 'base-url', 'api-key-entries', 'prefix', 'disabled', 'headers', 'models', 'priority',
  'test-model', 'disable-cooling'
];
const OPENAI_ENTRY_MANAGED = ['api-key', 'proxy-url', 'headers'];

export const providersApi = {
  async getGeminiKeys(): Promise<GeminiKeyConfig[]> {
    const data = await apiClient.get('/gemini-api-key');
    const list = extractArrayPayload(data, 'gemini-api-key');
    return list.map((item) => normalizeGeminiKeyConfig(item)).filter(Boolean) as GeminiKeyConfig[];
  },

  async saveGeminiKeys(configs: GeminiKeyConfig[]) {
    const raw = await fetchRawList('/gemini-api-key', 'gemini-api-key');
    const sent = configs.map((item) => serializeGeminiKey(item));
    await apiClient.put('/gemini-api-key', mergeSavedList(raw, sent, { managed: SIMPLE_KEY_MANAGED, identity: 'api-key' }));
  },

  updateGeminiKey: (index: number, value: GeminiKeyConfig) =>
    apiClient.patch('/gemini-api-key', { index, value: serializeGeminiKey(value) }),

  deleteGeminiKey: (apiKey: string, baseUrl?: string) =>
    apiClient.delete(`/gemini-api-key${buildProviderDeleteQuery(apiKey, baseUrl)}`),

  getInteractionsKeys: () =>
    getProviderKeyConfigs('/interactions-api-key', 'interactions-api-key', normalizeGeminiKeyConfig),
  saveInteractionsKeys: (configs: GeminiKeyConfig[]) =>
    saveProviderKeyConfigs('/interactions-api-key', 'interactions-api-key', configs, serializeGeminiKey, SIMPLE_KEY_MANAGED),
  updateInteractionsKey: (index: number, value: GeminiKeyConfig) =>
    apiClient.patch('/interactions-api-key', { index, value: serializeGeminiKey(value) }),
  deleteInteractionsKey: (apiKey: string, baseUrl?: string) =>
    apiClient.delete(`/interactions-api-key${buildProviderDeleteQuery(apiKey, baseUrl)}`),

  async getCodexConfigs(): Promise<ProviderKeyConfig[]> {
    const data = await apiClient.get('/codex-api-key');
    const list = extractArrayPayload(data, 'codex-api-key');
    return list.map((item) => normalizeProviderKeyConfig(item)).filter(Boolean) as ProviderKeyConfig[];
  },

  async saveCodexConfigs(configs: ProviderKeyConfig[]) {
    const raw = await fetchRawList('/codex-api-key', 'codex-api-key');
    const sent = configs.map((item) => serializeProviderKey(item));
    await apiClient.put('/codex-api-key', mergeSavedList(raw, sent, { managed: PROVIDER_KEY_MANAGED, identity: 'api-key' }));
  },

  updateCodexConfig: (index: number, value: ProviderKeyConfig) =>
    apiClient.patch('/codex-api-key', { index, value: serializeProviderKey(value) }),

  deleteCodexConfig: (apiKey: string, baseUrl?: string) =>
    apiClient.delete(`/codex-api-key${buildProviderDeleteQuery(apiKey, baseUrl)}`),

  getMetaConfigs: () =>
    getProviderKeyConfigs('/meta-api-key', 'meta-api-key', normalizeProviderKeyConfig),
  saveMetaConfigs: (configs: ProviderKeyConfig[]) =>
    saveProviderKeyConfigs('/meta-api-key', 'meta-api-key', configs, serializeProviderKey, PROVIDER_KEY_MANAGED),
  updateMetaConfig: (index: number, value: ProviderKeyConfig) =>
    apiClient.patch('/meta-api-key', { index, value: serializeProviderKey(value) }),
  deleteMetaConfig: (apiKey: string, baseUrl?: string) =>
    apiClient.delete(`/meta-api-key${buildProviderDeleteQuery(apiKey, baseUrl)}`),

  getXAIConfigs: () =>
    getProviderKeyConfigs('/xai-api-key', 'xai-api-key', normalizeProviderKeyConfig),
  saveXAIConfigs: (configs: ProviderKeyConfig[]) =>
    saveProviderKeyConfigs('/xai-api-key', 'xai-api-key', configs, serializeProviderKey, PROVIDER_KEY_MANAGED),
  updateXAIConfig: (index: number, value: ProviderKeyConfig) =>
    apiClient.patch('/xai-api-key', { index, value: serializeProviderKey(value) }),
  deleteXAIConfig: (apiKey: string, baseUrl?: string) =>
    apiClient.delete(`/xai-api-key${buildProviderDeleteQuery(apiKey, baseUrl)}`),

  async getClaudeConfigs(): Promise<ProviderKeyConfig[]> {
    const data = await apiClient.get('/claude-api-key');
    const list = extractArrayPayload(data, 'claude-api-key');
    return list.map((item) => normalizeProviderKeyConfig(item)).filter(Boolean) as ProviderKeyConfig[];
  },

  async saveClaudeConfigs(configs: ProviderKeyConfig[]) {
    const raw = await fetchRawList('/claude-api-key', 'claude-api-key');
    const sent = configs.map((item) => serializeProviderKey(item));
    await apiClient.put('/claude-api-key', mergeSavedList(raw, sent, { managed: PROVIDER_KEY_MANAGED, identity: 'api-key' }));
  },

  updateClaudeConfig: (index: number, value: ProviderKeyConfig) =>
    apiClient.patch('/claude-api-key', { index, value: serializeProviderKey(value) }),

  deleteClaudeConfig: (apiKey: string, baseUrl?: string) =>
    apiClient.delete(`/claude-api-key${buildProviderDeleteQuery(apiKey, baseUrl)}`),

  async getVertexConfigs(): Promise<ProviderKeyConfig[]> {
    const data = await apiClient.get('/vertex-api-key');
    const list = extractArrayPayload(data, 'vertex-api-key');
    return list.map((item) => normalizeProviderKeyConfig(item)).filter(Boolean) as ProviderKeyConfig[];
  },

  async saveVertexConfigs(configs: ProviderKeyConfig[]) {
    const raw = await fetchRawList('/vertex-api-key', 'vertex-api-key');
    const sent = configs.map((item) => serializeVertexKey(item));
    await apiClient.put('/vertex-api-key', mergeSavedList(raw, sent, { managed: SIMPLE_KEY_MANAGED, identity: 'api-key' }));
  },

  updateVertexConfig: (index: number, value: ProviderKeyConfig) =>
    apiClient.patch('/vertex-api-key', { index, value: serializeVertexKey(value) }),

  deleteVertexConfig: (apiKey: string, baseUrl?: string) =>
    apiClient.delete(`/vertex-api-key${buildProviderDeleteQuery(apiKey, baseUrl)}`),

  async getOpenAIProviders(): Promise<OpenAIProviderConfig[]> {
    const data = await apiClient.get('/openai-compatibility');
    const list = extractArrayPayload(data, 'openai-compatibility');
    return list.map((item, index) => normalizeOpenAIProvider(item, index)).filter(Boolean) as OpenAIProviderConfig[];
  },

  async saveOpenAIProviders(providers: OpenAIProviderConfig[]) {
    const raw = await fetchRawList('/openai-compatibility', 'openai-compatibility');
    const sent = providers.map((item) => serializeOpenAIProvider(item));
    await apiClient.put(
      '/openai-compatibility',
      mergeSavedList(raw, sent, {
        managed: OPENAI_PROVIDER_MANAGED,
        identity: 'name',
        nest: { key: 'api-key-entries', managed: OPENAI_ENTRY_MANAGED, identity: 'api-key' }
      })
    );
  },

  updateOpenAIProvider: (index: number, value: OpenAIProviderConfig) =>
    apiClient.patch('/openai-compatibility', { index, value: serializeOpenAIProvider(value) }),

  updateOpenAIProviderDisabled: (index: number, disabled: boolean) =>
    apiClient.patch('/openai-compatibility', { index, value: { disabled } }),

  deleteOpenAIProvider: (name: string) =>
    apiClient.delete(`/openai-compatibility?name=${encodeURIComponent(name)}`)
};
