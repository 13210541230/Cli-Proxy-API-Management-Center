import { normalizeApiBase } from '@/utils/connection';
import type { ManagementPluginEntry, ManagementPluginMenu } from '@/types/plugin';

export const PLUGIN_RESOURCES_REFRESH_EVENT = 'plugin-resources-refresh';
export const PLUGIN_API_REQUEST_TYPE = 'cpa-plugin-api-request';
export const PLUGIN_API_RESPONSE_TYPE = 'cpa-plugin-api-response';

export const toPluginAPIClientPath = (path: string): string =>
  path.trim().replace(/^\/v0\/management(?=\/|$)/, '') || '/';

export const isPluginAPIRequestAllowed = (method: string, path: string, pluginID: string): boolean => {
  const normalizedPluginID = pluginID.trim().replace(/^\/+|\/+$/g, '');
  const normalizedPath = path.trim();
  if (!normalizedPluginID || !normalizedPath) return false;
  const prefix = `/v0/management/${normalizedPluginID}`;
  return ['GET', 'POST', 'PUT', 'PATCH', 'DELETE'].includes(method.trim().toUpperCase()) &&
    (normalizedPath === prefix || normalizedPath.startsWith(`${prefix}/`));
};

export interface PluginResourceEntry {
  pluginID: string;
  pluginTitle: string;
  pluginLogo: string;
  menuIndex: number;
  menu: ManagementPluginMenu;
  label: string;
  description: string;
  route: string;
}

export const notifyPluginResourcesChanged = () => {
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new Event(PLUGIN_RESOURCES_REFRESH_EVENT));
  }
};

export const getPluginTitle = (plugin: ManagementPluginEntry): string =>
  plugin.metadata?.name?.trim() || plugin.id;

export const buildPluginResourceRoute = (pluginID: string, menuIndex: number): string =>
  `/plugin-pages/${encodeURIComponent(pluginID)}/${menuIndex}`;

export const resolvePluginAssetURL = (value: string, apiBase: string): string => {
  const trimmed = value.trim();
  if (!trimmed) return '';
  if (/^(https?:|data:|blob:)/i.test(trimmed)) return trimmed;
  if (!trimmed.startsWith('/')) return trimmed;
  const base = normalizeApiBase(apiBase);
  return base ? `${base}${trimmed}` : trimmed;
};

export const collectPluginResourceEntries = (
  plugins: ManagementPluginEntry[],
): PluginResourceEntry[] =>
  plugins.flatMap((plugin) => {
    if (!plugin.effective_enabled) return [];
    const pluginTitle = getPluginTitle(plugin);
    const pluginLogo = plugin.logo || plugin.metadata?.logo || '';
    return (plugin.menus ?? [])
      .map((menu, menuIndex): PluginResourceEntry | null => {
        const path = menu.path.trim();
        if (!path) return null;
        const label = menu.menu?.trim() || pluginTitle;
        return {
          pluginID: plugin.id,
          pluginTitle,
          pluginLogo,
          menuIndex,
          menu: { ...menu, path },
          label,
          description: menu.description?.trim() || pluginTitle,
          route: buildPluginResourceRoute(plugin.id, menuIndex),
        };
      })
      .filter((entry): entry is PluginResourceEntry => Boolean(entry));
  });
