import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { EmptyState } from '@/components/ui/EmptyState';
import { useAuthStore, usePluginStore } from '@/stores';
import { apiClient } from '@/services/api/client';
import {
  collectPluginResourceEntries,
  isPluginAPIRequestAllowed,
  PLUGIN_API_REQUEST_TYPE,
  PLUGIN_API_RESPONSE_TYPE,
  PLUGIN_RESOURCES_REFRESH_EVENT,
  resolvePluginAssetURL,
  toPluginAPIClientPath,
} from './pluginResources';
import styles from './PluginResourcePage.module.scss';

const decode = (value = '') => {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
};

const parseMenuIndex = (value = '') => {
  const index = Number.parseInt(value, 10);
  return Number.isInteger(index) && index >= 0 ? index : -1;
};

const getErrorMessage = (error: unknown) => (error instanceof Error ? error.message : 'Request failed');

export function PluginResourcePage() {
  const { t } = useTranslation();
  const { pluginId: rawPluginId, menuIndex: rawMenuIndex } = useParams<{ pluginId: string; menuIndex: string }>();
  const apiBase = useAuthStore((state) => state.apiBase);
  const connectionStatus = useAuthStore((state) => state.connectionStatus);
  const pluginStatus = usePluginStore((state) => state.pluginStatus);
  const plugins = usePluginStore((state) => state.plugins);
  const fetchPlugins = usePluginStore((state) => state.fetchPlugins);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const iframeRef = useRef<HTMLIFrameElement | null>(null);

  const pluginId = useMemo(() => decode(rawPluginId), [rawPluginId]);
  const menuIndex = useMemo(() => parseMenuIndex(rawMenuIndex), [rawMenuIndex]);
  const resource = useMemo(
    () => collectPluginResourceEntries(plugins).find((entry) => entry.pluginID === pluginId && entry.menuIndex === menuIndex),
    [menuIndex, pluginId, plugins],
  );
  const iframeSrc = resource ? resolvePluginAssetURL(resource.menu.path, apiBase) : '';

  const loadPlugins = useCallback(async () => {
    if (connectionStatus !== 'connected') {
      setLoading(false);
      setError(t('notification.connection_required', { defaultValue: 'A connection is required.' }));
      return;
    }
    setLoading(true);
    setError('');
    try {
      await fetchPlugins(true);
    } catch (loadError) {
      setError(getErrorMessage(loadError));
    } finally {
      setLoading(false);
    }
  }, [connectionStatus, fetchPlugins, t]);

  useEffect(() => {
    if (pluginStatus === 'idle' || pluginStatus === 'unavailable') {
      void loadPlugins();
    } else {
      setLoading(pluginStatus === 'loading');
    }
  }, [loadPlugins, pluginStatus]);

  useEffect(() => {
    const frame = iframeRef.current;
    if (!frame || !iframeSrc) return undefined;
    const expectedOrigin = new URL(iframeSrc, window.location.href).origin;

    const handlePluginRequest = async (event: MessageEvent) => {
      if (event.source !== frame.contentWindow || event.origin !== expectedOrigin) return;
      const message = event.data;
      if (!message || message.type !== PLUGIN_API_REQUEST_TYPE || typeof message.id !== 'string') return;
      const method = typeof message.method === 'string' ? message.method.toUpperCase() : '';
      const path = typeof message.path === 'string' ? message.path.trim() : '';
      const target = frame.contentWindow;
      const respond = (payload: Record<string, unknown>) => {
        target?.postMessage({ type: PLUGIN_API_RESPONSE_TYPE, id: message.id, ...payload }, expectedOrigin);
      };
      if (!isPluginAPIRequestAllowed(method, path, pluginId)) {
        respond({ ok: false, status: 400, error: '插件请求路径或方法不被允许' });
        return;
      }
      try {
        const response = await apiClient.requestRaw({
          method,
          url: toPluginAPIClientPath(path),
          data: message.body,
          validateStatus: () => true,
        });
        const status = Number(response.status || 0);
        if (status >= 200 && status < 300) {
          respond({ ok: true, status, data: response.data });
        } else {
          const responseData = response.data as { error?: { message?: string } | string; message?: string } | undefined;
          const detail = typeof responseData?.error === 'string'
            ? responseData.error
            : responseData?.error && typeof responseData.error === 'object'
              ? responseData.error.message
              : responseData?.message;
          respond({ ok: false, status, error: detail || `管理接口请求失败（HTTP ${status}）`, data: response.data });
        }
      } catch (requestError) {
        respond({ ok: false, status: 0, error: getErrorMessage(requestError) });
      }
    };

    window.addEventListener('message', handlePluginRequest);
    return () => window.removeEventListener('message', handlePluginRequest);
  }, [iframeSrc, pluginId]);

  useEffect(() => {
    const handleRefresh = () => void loadPlugins();
    window.addEventListener(PLUGIN_RESOURCES_REFRESH_EVENT, handleRefresh);
    return () => window.removeEventListener(PLUGIN_RESOURCES_REFRESH_EVENT, handleRefresh);
  }, [loadPlugins]);

  return (
    <div className={styles.page}>
      {loading ? (
        <div className={styles.state}>{t('common.loading')}</div>
      ) : error ? (
        <div className={styles.stateShell}>
          <EmptyState title={t('plugins.resource_unavailable', { defaultValue: 'Plugin workspace unavailable' })} description={error} />
        </div>
      ) : !resource ? (
        <div className={styles.stateShell}>
          <EmptyState title={t('plugins.resource_not_found', { defaultValue: 'Plugin workspace not found' })} description={t('plugins.resource_not_found_desc', { defaultValue: 'The plugin is disabled, the menu was removed, or this CPA version does not expose the resource.' })} />
        </div>
      ) : !iframeSrc ? (
        <div className={styles.stateShell}>
          <EmptyState title={t('plugins.resource_empty', { defaultValue: 'Plugin resource is empty' })} description={t('plugins.resource_empty_desc', { defaultValue: 'The plugin did not declare a loadable resource path.' })} />
        </div>
      ) : (
        <iframe
          ref={iframeRef}
          className={styles.frame}
          src={iframeSrc}
          title={resource.label}
          referrerPolicy="strict-origin-when-cross-origin"
          allow="clipboard-read; clipboard-write"
        />
      )}
    </div>
  );
}
