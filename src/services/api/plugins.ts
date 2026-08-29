import { apiClient } from './client';
import type {
  ManagementPluginConfig,
  ManagementPluginDeleteResponse,
  ManagementPluginConfigField,
  ManagementPluginEntry,
  ManagementPluginListResponse,
  ManagementPluginMenu,
  ManagementPluginMetadata,
} from '@/types/plugin';

type RecordValue = Record<string, unknown>;

const isRecord = (value: unknown): value is RecordValue =>
  value !== null && typeof value === 'object' && !Array.isArray(value);

const asString = (value: unknown): string => (value === undefined || value === null ? '' : String(value));
const asBoolean = (value: unknown, fallback = false): boolean =>
  typeof value === 'boolean' ? value : fallback;

const normalizeConfigField = (value: unknown): ManagementPluginConfigField | null => {
  if (!isRecord(value)) return null;
  const name = asString(value.name).trim();
  if (!name) return null;
  const enumValues = Array.isArray(value.enum_values)
    ? value.enum_values.map((item) => asString(item).trim()).filter(Boolean)
    : Array.isArray(value.enumValues)
      ? value.enumValues.map((item) => asString(item).trim()).filter(Boolean)
      : undefined;
  return {
    name,
    type: asString(value.type).trim() || 'string',
    ...(enumValues && enumValues.length > 0 ? { enum_values: enumValues } : {}),
    description: asString(value.description).trim() || undefined,
  };
};

const normalizeConfigFields = (value: unknown): ManagementPluginConfigField[] =>
  Array.isArray(value)
    ? value
        .map(normalizeConfigField)
        .filter((field): field is ManagementPluginConfigField => Boolean(field))
    : [];

const normalizeMenu = (value: unknown): ManagementPluginMenu | null => {
  if (!isRecord(value)) return null;
  const path = asString(value.path).trim();
  const menu = asString(value.menu).trim();
  if (!path) return null;
  return {
    path,
    menu: menu || undefined,
    description: asString(value.description).trim() || undefined,
  };
};

const normalizeMenus = (value: unknown): ManagementPluginMenu[] =>
  Array.isArray(value)
    ? value.map(normalizeMenu).filter((menu): menu is ManagementPluginMenu => Boolean(menu))
    : [];

const normalizeMetadata = (value: unknown): ManagementPluginMetadata | null => {
  if (!isRecord(value)) return null;
  const metadata: ManagementPluginMetadata = {
    name: asString(value.name).trim() || undefined,
    version: asString(value.version).trim() || undefined,
    author: asString(value.author).trim() || undefined,
    github_repository: asString(value.github_repository ?? value.githubRepository).trim() || undefined,
    logo: asString(value.logo).trim() || undefined,
    config_fields: normalizeConfigFields(value.config_fields ?? value.configFields),
  };
  return Object.values(metadata).some((item) => (Array.isArray(item) ? item.length > 0 : Boolean(item)))
    ? metadata
    : null;
};

const normalizePluginEntry = (value: unknown): ManagementPluginEntry | null => {
  if (!isRecord(value)) return null;
  const id = asString(value.id).trim();
  if (!id) return null;

  const metadata = normalizeMetadata(value.metadata);
  const configFields = normalizeConfigFields(value.config_fields ?? value.configFields);
  return {
    id,
    registered: asBoolean(value.registered),
    enabled: asBoolean(value.enabled, true),
    effective_enabled: asBoolean(value.effective_enabled ?? value.effectiveEnabled),
    configured: asBoolean(value.configured),
    supports_oauth: asBoolean(value.supports_oauth ?? value.supportsOAuth),
    oauth_provider: asString(value.oauth_provider ?? value.oauthProvider).trim() || undefined,
    path: asString(value.path).trim() || undefined,
    logo: asString(value.logo ?? metadata?.logo).trim() || undefined,
    config_fields: configFields.length > 0 ? configFields : metadata?.config_fields ?? [],
    menus: normalizeMenus(value.menus),
    metadata,
  };
};

export const normalizePluginList = (value: unknown): ManagementPluginListResponse => {
  const source = isRecord(value) ? value : {};
  const plugins = Array.isArray(source.plugins)
    ? source.plugins
        .map(normalizePluginEntry)
        .filter((plugin): plugin is ManagementPluginEntry => Boolean(plugin))
    : [];
  const rawEnabled = source.plugins_enabled ?? source.pluginsEnabled;
  return {
    ...(typeof rawEnabled === 'boolean' ? { plugins_enabled: rawEnabled } : {}),
    plugins,
  };
};

export const pluginsApi = {
  async list(): Promise<ManagementPluginListResponse> {
    return normalizePluginList(await apiClient.get('/plugins'));
  },

  updateEnabled: (id: string, enabled: boolean) =>
    apiClient.patch(`/plugins/${encodeURIComponent(id)}/enabled`, { enabled }),

  deletePlugin: async (id: string): Promise<ManagementPluginDeleteResponse> =>
    apiClient.delete(`/plugins/${encodeURIComponent(id)}`),

  getConfig: async (id: string): Promise<ManagementPluginConfig> => {
    const data = await apiClient.get(`/plugins/${encodeURIComponent(id)}/config`);
    return isRecord(data) ? data : {};
  },

  putConfig: (id: string, config: ManagementPluginConfig) =>
    apiClient.put(`/plugins/${encodeURIComponent(id)}/config`, config),

  patchConfig: (id: string, patch: ManagementPluginConfig) =>
    apiClient.patch(`/plugins/${encodeURIComponent(id)}/config`, patch),
};
