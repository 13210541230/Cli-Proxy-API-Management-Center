import { apiClient } from './client';
import type {
  ManagementPluginConfig,
  ManagementPluginDeleteResponse,
  ManagementPluginConfigField,
  ManagementPluginEntry,
  ManagementPluginListResponse,
  ManagementPluginMenu,
  ManagementPluginInstallResponse,
  ManagementPluginMetadata,
  ManagementPluginStoreEntry,
  ManagementPluginStoreListResponse,
  ManagementPluginStorePlatform,
  ManagementPluginStoreSource,
  ManagementPluginStoreSourceError,
} from '@/types/plugin';

type RecordValue = Record<string, unknown>;

const PLUGIN_MARKET_REQUEST_TIMEOUT_MS = 90_000;
const PLUGIN_INSTALL_REQUEST_TIMEOUT_MS = 180_000;

const isRecord = (value: unknown): value is RecordValue =>
  value !== null && typeof value === 'object' && !Array.isArray(value);

const asString = (value: unknown): string =>
  value === undefined || value === null ? '' : String(value);
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
    github_repository:
      asString(value.github_repository ?? value.githubRepository).trim() || undefined,
    logo: asString(value.logo).trim() || undefined,
    config_fields: normalizeConfigFields(value.config_fields ?? value.configFields),
  };
  return Object.values(metadata).some((item) =>
    Array.isArray(item) ? item.length > 0 : Boolean(item)
  )
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
    config_fields: configFields.length > 0 ? configFields : (metadata?.config_fields ?? []),
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

const normalizePlatform = (value: unknown): ManagementPluginStorePlatform | null => {
  if (!isRecord(value)) return null;
  const goos = asString(value.goos).trim();
  const goarch = asString(value.goarch).trim();
  return goos && goarch ? { goos, goarch } : null;
};

const normalizeStoreSource = (value: unknown): ManagementPluginStoreSource | null => {
  if (!isRecord(value)) return null;
  const id = asString(value.id).trim();
  const name = asString(value.name).trim();
  const url = asString(value.url).trim();
  return id && name && url ? { id, name, url } : null;
};

const normalizeStoreSourceError = (value: unknown): ManagementPluginStoreSourceError | null => {
  if (!isRecord(value)) return null;
  return {
    source_id: asString(value.source_id ?? value.sourceId).trim(),
    source_name: asString(value.source_name ?? value.sourceName).trim(),
    source_url: asString(value.source_url ?? value.sourceUrl).trim(),
    message: asString(value.message).trim(),
  };
};

const normalizeStoreEntry = (value: unknown): ManagementPluginStoreEntry | null => {
  if (!isRecord(value)) return null;
  const id = asString(value.id).trim();
  if (!id) return null;
  return {
    store_id: asString(value.store_id ?? value.storeId).trim(),
    source_id: asString(value.source_id ?? value.sourceId).trim(),
    source_name: asString(value.source_name ?? value.sourceName).trim(),
    source_url: asString(value.source_url ?? value.sourceUrl).trim(),
    id,
    name: asString(value.name).trim() || id,
    description: asString(value.description).trim(),
    author: asString(value.author).trim(),
    version: asString(value.version).trim(),
    repository: asString(value.repository).trim(),
    install_type: asString(value.install_type ?? value.installType).trim(),
    auth_required: asBoolean(value.auth_required ?? value.authRequired),
    auth_configured: asBoolean(value.auth_configured ?? value.authConfigured),
    platforms: Array.isArray(value.platforms)
      ? value.platforms
          .map(normalizePlatform)
          .filter((platform): platform is ManagementPluginStorePlatform => Boolean(platform))
      : [],
    logo: asString(value.logo).trim() || undefined,
    homepage: asString(value.homepage).trim() || undefined,
    license: asString(value.license).trim() || undefined,
    tags: Array.isArray(value.tags)
      ? value.tags.map((tag) => asString(tag).trim()).filter(Boolean)
      : [],
    installed: asBoolean(value.installed),
    installed_version: asString(value.installed_version ?? value.installedVersion).trim(),
    installed_source_id:
      asString(value.installed_source_id ?? value.installedSourceId).trim() || undefined,
    install_source_status:
      asString(value.install_source_status ?? value.installSourceStatus).trim() || undefined,
    path: asString(value.path).trim() || undefined,
    configured: asBoolean(value.configured),
    registered: asBoolean(value.registered),
    enabled: asBoolean(value.enabled),
    effective_enabled: asBoolean(value.effective_enabled ?? value.effectiveEnabled),
    update_available: asBoolean(value.update_available ?? value.updateAvailable),
  };
};

export const normalizePluginStoreList = (value: unknown): ManagementPluginStoreListResponse => {
  const source = isRecord(value) ? value : {};
  const sourceErrors = source.source_errors ?? source.sourceErrors;
  return {
    plugins_enabled: asBoolean(source.plugins_enabled ?? source.pluginsEnabled, true),
    plugins_dir: asString(source.plugins_dir ?? source.pluginsDir).trim(),
    sources: Array.isArray(source.sources)
      ? source.sources
          .map(normalizeStoreSource)
          .filter((item): item is ManagementPluginStoreSource => Boolean(item))
      : [],
    source_errors: Array.isArray(sourceErrors)
      ? sourceErrors
          .map(normalizeStoreSourceError)
          .filter((item): item is ManagementPluginStoreSourceError => Boolean(item))
      : [],
    plugins: Array.isArray(source.plugins)
      ? source.plugins
          .map(normalizeStoreEntry)
          .filter((item): item is ManagementPluginStoreEntry => Boolean(item))
      : [],
  };
};

export const pluginsApi = {
  async list(): Promise<ManagementPluginListResponse> {
    return normalizePluginList(await apiClient.get('/plugins'));
  },

  async listStore(): Promise<ManagementPluginStoreListResponse> {
    return normalizePluginStoreList(
      await apiClient.get('/plugin-store', { timeout: PLUGIN_MARKET_REQUEST_TIMEOUT_MS })
    );
  },

  installFromStore: (id: string, version?: string): Promise<ManagementPluginInstallResponse> =>
    apiClient.post<ManagementPluginInstallResponse>(
      `/plugin-store/${encodeURIComponent(id)}/install`,
      version ? { version } : undefined,
      { timeout: PLUGIN_INSTALL_REQUEST_TIMEOUT_MS }
    ),

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
