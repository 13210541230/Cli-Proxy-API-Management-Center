/**
 * 模型列表状态管理（带缓存）
 */

import { create } from 'zustand';
import { quotaPauseApi } from '@/services/api/quotaPause';
import { modelsApi } from '@/services/api/models';
import { CACHE_EXPIRY_MS } from '@/utils/constants';
import { quotaKeyHash } from '@/utils/apiKeyHash';
import type { ModelInfo } from '@/utils/models';

interface ModelsCache {
  data: ModelInfo[];
  timestamp: number;
  apiBase: string;
  apiKey: string;
}

interface ModelsState {
  models: ModelInfo[];
  loading: boolean;
  error: string | null;
  cache: ModelsCache | null;

  fetchModels: (apiBase: string, apiKey?: string, forceRefresh?: boolean) => Promise<ModelInfo[]>;
  fetchModelsWithApiKeys: (apiBase: string, apiKeys: string[], forceRefresh?: boolean) => Promise<ModelInfo[]>;
  clearCache: () => void;
  isCacheValid: (apiBase: string, apiKey?: string) => boolean;
}

export const useModelsStore = create<ModelsState>((set, get) => ({
  models: [],
  loading: false,
  error: null,
  cache: null,

  fetchModels: async (apiBase, apiKey, forceRefresh = false) => {
    const { cache, isCacheValid } = get();
    const apiKeyScope = apiKey?.trim() || '';

    // 检查缓存
    if (!forceRefresh && isCacheValid(apiBase, apiKeyScope) && cache) {
      set({ models: cache.data, error: null });
      return cache.data;
    }

    set({ loading: true, error: null });

    try {
      const list = await modelsApi.fetchModels(apiBase, apiKeyScope || undefined);
      const now = Date.now();

      set({
        models: list,
        loading: false,
        cache: { data: list, timestamp: now, apiBase, apiKey: apiKeyScope }
      });

      return list;
    } catch (error: unknown) {
      const message =
        error instanceof Error ? error.message : typeof error === 'string' ? error : 'Failed to fetch models';
      set({
        error: message,
        loading: false,
        models: []
      });
      throw error;
    }
  },

  fetchModelsWithApiKeys: async (apiBase, apiKeys, forceRefresh = false) => {
    const uniqueKeys = Array.from(new Set(apiKeys.map((key) => key.trim()).filter(Boolean)));
    let pausedHashes = new Set<string>();
    try {
      const response = await quotaPauseApi.listPaused();
      pausedHashes = new Set(
        (response.entries ?? [])
          .map((entry) => String(entry.key_hash ?? '').trim().toLowerCase().slice(0, 8))
          .filter(Boolean),
      );
    } catch {
      // If the optional pause projection is unavailable, try the configured keys in order.
    }

    const activeKeys = uniqueKeys.filter((key) => !pausedHashes.has(quotaKeyHash(key)));
    const orderedKeys = activeKeys.length > 0 ? activeKeys : uniqueKeys;
    if (orderedKeys.length === 0) {
      return get().fetchModels(apiBase, undefined, forceRefresh);
    }

    let lastError: unknown;
    for (const apiKey of orderedKeys) {
      try {
        return await get().fetchModels(apiBase, apiKey, forceRefresh);
      } catch (error: unknown) {
        lastError = error;
        const status =
          error && typeof error === 'object'
            ? Number(
                (error as { response?: { status?: unknown }; status?: unknown }).response?.status ??
                  (error as { status?: unknown }).status ??
                  0,
              )
            : 0;
        if (![401, 403, 429].includes(status)) {
          throw error;
        }
      }
    }

    throw lastError instanceof Error ? lastError : new Error('No valid API key could fetch models');
  },

  clearCache: () => {
    set({ cache: null, models: [] });
  },

  isCacheValid: (apiBase, apiKey) => {
    const { cache } = get();
    if (!cache) return false;
    if (cache.apiBase !== apiBase) return false;
    const apiKeyScope = apiKey?.trim() || '';
    if ((cache.apiKey || '') !== apiKeyScope) return false;
    return Date.now() - cache.timestamp < CACHE_EXPIRY_MS;
  }
}));
