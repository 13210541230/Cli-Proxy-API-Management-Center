/**
 * Provider workbench view models for normalizing heterogeneous brand configs.
 */

import type { GeminiKeyConfig, OpenAIProviderConfig, ProviderKeyConfig } from '@/types';
import type { ThinkingLevel } from './thinkingLevels';

export type ProviderBrand =
  | 'gemini'
  | 'interactions'
  | 'codex'
  | 'meta'
  | 'xai'
  | 'claude'
  | 'vertex'
  | 'openaiCompatibility'
  | 'apikeyFun'
  | 'fennoAI'
  | 'qiniuCloud'
  | 'kimi';

export type SponsorProviderBrand = 'apikeyFun' | 'fennoAI' | 'qiniuCloud' | 'kimi';

export const PROVIDER_SORT_BY_VALUES = ['name', 'priority', 'recent-success'] as const;
export type ProviderSortBy = (typeof PROVIDER_SORT_BY_VALUES)[number];

export const SORT_DIR_VALUES = ['asc', 'desc'] as const;
export type SortDir = (typeof SORT_DIR_VALUES)[number];

export type ProviderResourceSelector =
  | { brand: 'gemini'; apiKey: string; baseUrl?: string; index: number }
  | { brand: 'interactions'; apiKey: string; baseUrl?: string; index: number }
  | { brand: 'codex'; apiKey: string; baseUrl?: string; index: number }
  | { brand: 'meta'; apiKey: string; baseUrl?: string; index: number }
  | { brand: 'xai'; apiKey: string; baseUrl?: string; index: number }
  | { brand: 'claude'; apiKey: string; baseUrl?: string; index: number }
  | { brand: 'vertex'; apiKey: string; baseUrl?: string; index: number }
  | { brand: 'openaiCompatibility'; name: string; index: number }
  | {
      brand: 'apikeyFun';
      openaiIndices: number[];
      claudeIndices: number[];
      codexIndices: number[];
      geminiIndices: number[];
    }
  | {
      brand: 'fennoAI';
      openaiIndices: number[];
      claudeIndices: number[];
      codexIndices: number[];
      geminiIndices: number[];
    }
  | {
      brand: 'qiniuCloud';
      openaiIndices: number[];
      claudeIndices: number[];
      codexIndices: number[];
      geminiIndices: number[];
    }
  | {
      brand: 'kimi';
      openaiIndices: number[];
      claudeIndices: number[];
      codexIndices: number[];
      geminiIndices: number[];
    };

export interface ProviderResourceFlags {
  cloakEnabled?: boolean;
  claudeCodeCliProfile?: boolean;
  websockets?: boolean;
  protocols?: string[];
}

export interface ProviderResource {
  /** Stable ID used for React keys and selection state. */
  id: string;
  brand: ProviderBrand;
  /** Index in the original configuration array. */
  originalIndex: number;
  /** Name shown in the table's key column (OpenAI name; null for other brands). */
  name: string | null;
  /** Fallback display text, such as a masked API key. */
  identifier: string;
  /** Masked API-key preview for display. */
  apiKeyPreview: string | null;
  /** API key used by selectors; null for OpenAI providers with multiple keys. */
  apiKey: string | null;
  authIndex: string | null;
  baseUrl: string | null;
  proxyUrl: string | null;
  prefix: string | null;
  modelCount: number;
  /** Deduplicated model names used for filtering and search. */
  models: string[];
  /** Sort priority; defaults to 0 when unset. */
  priority: number;
  headerCount: number;
  excludedModelCount: number;
  /** Relevant only to OpenAI; retained but not displayed for other brands. */
  apiKeyEntryCount: number;
  /** Whether this resource is disabled; the rule varies by brand. */
  disabled: boolean;
  /** Additional capability flags. */
  flags: ProviderResourceFlags;
  /** Selector used for delete and update operations. */
  selector: ProviderResourceSelector;
  /** Original raw config used to initialize the sheet form. */
  raw: unknown;
}

export interface ProviderGroup {
  id: ProviderBrand;
  resources: ProviderResource[];
}

export interface ProviderSnapshot {
  fetchedAt: string;
  groups: ProviderGroup[];
}

export interface SponsorProviderRaw {
  openai: Array<{ config: OpenAIProviderConfig; index: number }>;
  claude: Array<{ config: ProviderKeyConfig; index: number }>;
  codex: Array<{ config: ProviderKeyConfig; index: number }>;
  gemini: Array<{ config: GeminiKeyConfig; index: number }>;
}

/**
 * Shared sheet form values.
 * Gemini, Codex, Claude, Vertex, and OpenAI share the base fields and enable their own advanced options.
 */
export interface ModelEntryInput {
  name: string;
  alias?: string;
  priority?: number;
  testModel?: string;
  image?: boolean;
  /** Original backend value, preserved until the standard-level selector is changed. */
  thinkingJson?: string;
  thinkingLevels?: ThinkingLevel[];
  thinkingLevelsTouched?: boolean;
}

export type SponsorProtocol = 'openai' | 'codex' | 'claude' | 'gemini';

export interface SponsorKeyEntryInput {
  protocol: SponsorProtocol;
  apiKey: string;
  existingApiKey?: string;
  baseUrl: string;
  proxyUrl: string;
  prefix: string;
  disabled: boolean;
  disableCooling?: boolean;
  priority?: number;
  weight?: number;
  models: ModelEntryInput[];
}

export interface ApiKeyEntryInput {
  apiKey: string;
  existingApiKey?: string;
  proxyUrl: string;
  weight?: number;
  authIndex?: string;
}

export interface CloakInput {
  mode: string;
  strictMode: boolean;
  sensitiveWordsText: string;
  cacheUserId: boolean;
}

export interface ProviderEntryFormInput {
  /** For OpenAI creation, the key is supplied only through apiKeyEntries. */
  apiKey: string;
  /** Required for OpenAI and hidden for other brands. */
  name: string;
  baseUrl: string;
  proxyUrl: string;
  prefix: string;
  disabled: boolean;
  disableCooling?: boolean;
  priority?: number;
  weight?: number;

  /** Advanced section fields. */
  models: ModelEntryInput[];
  headers: Array<{ key: string; value: string }>;
  excludedModelsText: string;

  /** Codex-specific setting. */
  websockets?: boolean;
  /** Claude-specific setting. */
  cloak?: CloakInput;
  fingerprintProfile?: string;
  /** OpenAI persists this; Gemini/Claude use it for one-off connectivity tests. */
  testModel?: string;
  apiKeyEntries?: ApiKeyEntryInput[];
  /** APIKEY.FUN stores one grouped key per platform protocol. */
  sponsorKeyEntries?: SponsorKeyEntryInput[];
}
