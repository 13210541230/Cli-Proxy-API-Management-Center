import { apiClient } from './client';
import { providersApi } from './providers';
import type { GeminiKeyConfig, OpenAIProviderConfig, ProviderKeyConfig } from '@/types';

const appendAndSave = async <T>(
  get: () => Promise<T[]>,
  save: (items: T[]) => Promise<void>,
  item: T
) => save([...(await get()), item]);

const replaceByIdentity = async <T extends { apiKey: string; baseUrl?: string }>(
  get: () => Promise<T[]>,
  save: (items: T[]) => Promise<void>,
  apiKey: string,
  baseUrl: string | undefined,
  value: T
) => {
  const items = await get();
  const index = items.findIndex(
    (item) =>
      item.apiKey.trim() === apiKey.trim() &&
      (item.baseUrl ?? '').trim() === (baseUrl ?? '').trim()
  );
  if (index < 0) throw new Error('Provider configuration changed; refresh and try again.');
  items[index] = value;
  await save(items);
};

export const providersWorkbenchApi = {
  createGeminiKey: (value: GeminiKeyConfig) =>
    appendAndSave(providersApi.getGeminiKeys, providersApi.saveGeminiKeys, value),
  updateGeminiKey: (apiKey: string, baseUrl: string | undefined, value: GeminiKeyConfig) =>
    replaceByIdentity(providersApi.getGeminiKeys, providersApi.saveGeminiKeys, apiKey, baseUrl, value),
  deleteGeminiKey: providersApi.deleteGeminiKey,

  getInteractionsKeys: providersApi.getInteractionsKeys,
  createInteractionsKey: (value: GeminiKeyConfig) =>
    appendAndSave(providersApi.getInteractionsKeys, providersApi.saveInteractionsKeys, value),
  updateInteractionsKey: (apiKey: string, baseUrl: string | undefined, value: GeminiKeyConfig) =>
    replaceByIdentity(providersApi.getInteractionsKeys, providersApi.saveInteractionsKeys, apiKey, baseUrl, value),
  deleteInteractionsKey: providersApi.deleteInteractionsKey,

  createCodexConfig: (value: ProviderKeyConfig) =>
    appendAndSave(providersApi.getCodexConfigs, providersApi.saveCodexConfigs, value),
  updateCodexConfig: (apiKey: string, baseUrl: string | undefined, value: ProviderKeyConfig) =>
    replaceByIdentity(providersApi.getCodexConfigs, providersApi.saveCodexConfigs, apiKey, baseUrl, value),
  deleteCodexConfig: providersApi.deleteCodexConfig,

  getMetaConfigs: providersApi.getMetaConfigs,
  createMetaConfig: (value: ProviderKeyConfig) =>
    appendAndSave(providersApi.getMetaConfigs, providersApi.saveMetaConfigs, value),
  updateMetaConfig: (apiKey: string, baseUrl: string | undefined, value: ProviderKeyConfig) =>
    replaceByIdentity(providersApi.getMetaConfigs, providersApi.saveMetaConfigs, apiKey, baseUrl, value),
  deleteMetaConfig: providersApi.deleteMetaConfig,

  getXAIConfigs: providersApi.getXAIConfigs,
  createXAIConfig: (value: ProviderKeyConfig) =>
    appendAndSave(providersApi.getXAIConfigs, providersApi.saveXAIConfigs, value),
  updateXAIConfig: (apiKey: string, baseUrl: string | undefined, value: ProviderKeyConfig) =>
    replaceByIdentity(providersApi.getXAIConfigs, providersApi.saveXAIConfigs, apiKey, baseUrl, value),
  deleteXAIConfig: providersApi.deleteXAIConfig,

  createClaudeConfig: (value: ProviderKeyConfig) =>
    appendAndSave(providersApi.getClaudeConfigs, providersApi.saveClaudeConfigs, value),
  updateClaudeConfig: (apiKey: string, baseUrl: string | undefined, value: ProviderKeyConfig) =>
    replaceByIdentity(providersApi.getClaudeConfigs, providersApi.saveClaudeConfigs, apiKey, baseUrl, value),
  deleteClaudeConfig: providersApi.deleteClaudeConfig,

  getVertexConfigs: providersApi.getVertexConfigs,
  createVertexConfig: (value: ProviderKeyConfig) =>
    appendAndSave(providersApi.getVertexConfigs, providersApi.saveVertexConfigs, value),
  updateVertexConfig: (apiKey: string, baseUrl: string | undefined, value: ProviderKeyConfig) =>
    replaceByIdentity(providersApi.getVertexConfigs, providersApi.saveVertexConfigs, apiKey, baseUrl, value),
  deleteVertexConfig: providersApi.deleteVertexConfig,

  getOpenAIProviders: providersApi.getOpenAIProviders,
  createOpenAIProvider: (value: OpenAIProviderConfig) =>
    appendAndSave(providersApi.getOpenAIProviders, providersApi.saveOpenAIProviders, value),
  updateOpenAIProvider: (name: string, index: number, value: OpenAIProviderConfig) =>
    providersApi.updateOpenAIProvider(index, { ...value, name }),
  updateOpenAIProviderDisabled: providersApi.updateOpenAIProviderDisabled,
  deleteOpenAIProvider: (index: number) =>
    apiClient.delete(`/openai-compatibility?index=${encodeURIComponent(String(index))}`),
};
