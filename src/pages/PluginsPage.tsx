import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Input } from '@/components/ui/Input';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { Modal } from '@/components/ui/Modal';
import { Select } from '@/components/ui/Select';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { IconExternalLink, IconRefreshCw, IconSettings, IconTrash2 } from '@/components/ui/icons';
import { pluginsApi } from '@/services/api/plugins';
import { useAuthStore, usePluginStore } from '@/stores';
import type {
  ManagementPluginConfig,
  ManagementPluginConfigField,
  ManagementPluginEntry,
} from '@/types/plugin';
import {
  collectPluginResourceEntries,
  getPluginTitle,
  notifyPluginResourcesChanged,
  resolvePluginAssetURL,
} from '@/features/plugins/pluginResources';
import styles from './PluginsPage.module.scss';

const errorMessage = (error: unknown) => (error instanceof Error ? error.message : 'Request failed');

const formatValue = (value: unknown): string => {
  if (value === undefined || value === null || value === '') return '—';
  if (typeof value === 'object') {
    try {
      return JSON.stringify(value);
    } catch {
      return String(value);
    }
  }
  return String(value);
};

const parseJsonValue = (value: string, fallback: unknown): unknown => {
  try {
    return JSON.parse(value);
  } catch {
    return fallback;
  }
};

export function PluginsPage() {
  const { t } = useTranslation();
  const plugins = usePluginStore((state) => state.plugins);
  const apiBase = useAuthStore((state) => state.apiBase);
  const pluginStatus = usePluginStore((state) => state.pluginStatus);
  const pluginsEnabled = usePluginStore((state) => state.pluginsEnabled);
  const fetchPlugins = usePluginStore((state) => state.fetchPlugins);
  const [query, setQuery] = useState('');
  const [actionId, setActionId] = useState('');
  const [error, setError] = useState('');
  const [configPlugin, setConfigPlugin] = useState<ManagementPluginEntry | null>(null);
  const [config, setConfig] = useState<ManagementPluginConfig>({});
  const [configLoading, setConfigLoading] = useState(false);
  const [configSaving, setConfigSaving] = useState(false);
  const [configError, setConfigError] = useState('');

  const loadPlugins = useCallback(async () => {
    setError('');
    try {
      await fetchPlugins(true);
    } catch (loadError) {
      setError(errorMessage(loadError));
    }
  }, [fetchPlugins]);

  useEffect(() => {
    if (pluginStatus === 'idle') void loadPlugins();
  }, [loadPlugins, pluginStatus]);

  const filteredPlugins = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    if (!normalized) return plugins;
    return plugins.filter((plugin) => {
      const searchText = [
        plugin.id,
        getPluginTitle(plugin),
        plugin.metadata?.author,
        ...(plugin.menus ?? []).flatMap((menu) => [menu.menu, menu.description]),
      ]
        .filter(Boolean)
        .join(' ')
        .toLowerCase();
      return searchText.includes(normalized);
    });
  }, [plugins, query]);

  const resources = useMemo(
    () => (pluginsEnabled === false ? [] : collectPluginResourceEntries(plugins)),
    [plugins, pluginsEnabled],
  );
  const enabledCount = plugins.filter((plugin) => plugin.effective_enabled).length;
  const configuredCount = plugins.filter((plugin) => plugin.configured).length;

  const handleToggle = async (plugin: ManagementPluginEntry) => {
    setActionId(plugin.id);
    setError('');
    try {
      await pluginsApi.updateEnabled(plugin.id, !plugin.enabled);
      await fetchPlugins(true);
      notifyPluginResourcesChanged();
    } catch (toggleError) {
      setError(errorMessage(toggleError));
    } finally {
      setActionId('');
    }
  };

  const handleDelete = async (plugin: ManagementPluginEntry) => {
    if (!window.confirm(t('plugins.delete_confirm', { name: getPluginTitle(plugin), defaultValue: `Delete ${getPluginTitle(plugin)}?` }))) return;
    setActionId(plugin.id);
    setError('');
    try {
      await pluginsApi.deletePlugin(plugin.id);
      await fetchPlugins(true);
      notifyPluginResourcesChanged();
    } catch (deleteError) {
      setError(errorMessage(deleteError));
    } finally {
      setActionId('');
    }
  };

  const openConfig = async (plugin: ManagementPluginEntry) => {
    setConfigPlugin(plugin);
    setConfig({});
    setConfigError('');
    setConfigLoading(true);
    try {
      setConfig(await pluginsApi.getConfig(plugin.id));
    } catch (configLoadError) {
      setConfigError(errorMessage(configLoadError));
    } finally {
      setConfigLoading(false);
    }
  };

  const saveConfig = async () => {
    if (!configPlugin) return;
    setConfigSaving(true);
    setConfigError('');
    try {
      await pluginsApi.putConfig(configPlugin.id, config);
      setConfigPlugin(null);
      await fetchPlugins(true);
      notifyPluginResourcesChanged();
    } catch (saveError) {
      setConfigError(errorMessage(saveError));
    } finally {
      setConfigSaving(false);
    }
  };

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div>
          <p className={styles.eyebrow}>{t('plugins.eyebrow', { defaultValue: 'EXTENSIONS' })}</p>
          <h1>{t('plugins.title', { defaultValue: 'Plugin Center' })}</h1>
          <p className={styles.description}>
            {t('plugins.description', {
              defaultValue: 'Manage plugin lifecycle and open plugin-provided workspaces without adding plugin-specific pages to the Management Center.',
            })}
          </p>
        </div>
        <Button variant="secondary" onClick={loadPlugins} loading={pluginStatus === 'loading'}>
          <IconRefreshCw size={16} /> {t('common.refresh')}
        </Button>
      </header>

      <section className={styles.stats} aria-label={t('plugins.overview', { defaultValue: 'Plugin overview' })}>
        <Card><span>{t('plugins.total', { defaultValue: 'Installed plugins' })}</span><strong>{plugins.length}</strong></Card>
        <Card><span>{t('plugins.enabled', { defaultValue: 'Enabled' })}</span><strong>{enabledCount}</strong></Card>
        <Card><span>{t('plugins.configured', { defaultValue: 'Configured' })}</span><strong>{configuredCount}</strong></Card>
        <Card><span>{t('plugins.resources', { defaultValue: 'Plugin workspaces' })}</span><strong>{resources.length}</strong></Card>
      </section>

      {pluginsEnabled === false && (
        <div className={styles.notice} role="status">
          {t('plugins.disabled_notice', { defaultValue: 'Plugin support is disabled by CPA. Installed plugins remain visible, but their workspaces are unavailable.' })}
        </div>
      )}
      {error && <div className={styles.error} role="alert">{error}</div>}

      <Card className={styles.listCard}>
        <div className={styles.toolbar}>
          <div>
            <h2>{t('plugins.installed_title', { defaultValue: 'Installed plugins' })}</h2>
            <p>{t('plugins.installed_desc', { defaultValue: 'Each plugin owns its UI. This page only provides generic lifecycle controls and navigation.' })}</p>
          </div>
          <Input
            aria-label={t('plugins.search', { defaultValue: 'Search plugins' })}
            placeholder={t('plugins.search_placeholder', { defaultValue: 'Search by name or menu' })}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </div>

        {pluginStatus === 'loading' && plugins.length === 0 ? (
          <div className={styles.state}><LoadingSpinner /> {t('common.loading')}</div>
        ) : pluginStatus === 'unavailable' && plugins.length === 0 ? (
          <EmptyState title={t('plugins.unavailable', { defaultValue: 'Plugin API unavailable' })} description={error || t('plugins.unavailable_desc', { defaultValue: 'The connected CPA version does not expose the plugin management API.' })} />
        ) : filteredPlugins.length === 0 ? (
          <EmptyState title={t('plugins.empty', { defaultValue: 'No plugins found' })} description={query ? t('plugins.empty_search', { defaultValue: 'Try a different search term.' }) : t('plugins.empty_desc', { defaultValue: 'Installed plugins will appear here when CPA exposes them.' })} />
        ) : (
          <div className={styles.grid}>
            {filteredPlugins.map((plugin) => {
              const title = getPluginTitle(plugin);
              const pluginResources = resources.filter((resource) => resource.pluginID === plugin.id);
              const busy = actionId === plugin.id;
              return (
                <article className={styles.pluginCard} key={plugin.id}>
                  <div className={styles.pluginHeader}>
                    <div className={styles.pluginIdentity}>
                      {plugin.logo ? <img src={resolvePluginAssetURL(plugin.logo, apiBase)} alt="" className={styles.logo} /> : <div className={styles.logoFallback}>{title.slice(0, 1).toUpperCase()}</div>}
                      <div><h3>{title}</h3><code>{plugin.id}</code></div>
                    </div>
                    <span className={`${styles.status} ${plugin.effective_enabled ? styles.statusEnabled : styles.statusDisabled}`}>
                      {plugin.effective_enabled ? t('common.enabled') : t('common.disabled')}
                    </span>
                  </div>
                  <div className={styles.meta}>
                    <span>{plugin.metadata?.version ? `v${plugin.metadata.version}` : t('plugins.version_unknown', { defaultValue: 'Version unknown' })}</span>
                    <span>{pluginResources.length} {t('plugins.menus', { defaultValue: 'menus' })}</span>
                    {plugin.metadata?.author && <span>{plugin.metadata.author}</span>}
                  </div>
                  <div className={styles.resourceList}>
                    {pluginResources.length > 0 ? pluginResources.map((resource) => (
                      <Link className={styles.resourceLink} to={resource.route} key={resource.route}>
                        <span><strong>{resource.label}</strong><small>{resource.description}</small></span>
                        <IconExternalLink size={15} />
                      </Link>
                    )) : <span className={styles.muted}>{t('plugins.no_menus', { defaultValue: 'No plugin workspace declared' })}</span>}
                  </div>
                  <div className={styles.actions}>
                    <ToggleSwitch checked={plugin.enabled} onChange={() => void handleToggle(plugin)} disabled={busy} ariaLabel={`${title} ${t('plugins.toggle', { defaultValue: 'enabled' })}`} label={plugin.enabled ? t('common.enabled') : t('common.disabled')} />
                    {plugin.config_fields && plugin.config_fields.length > 0 && <Button variant="secondary" size="sm" onClick={() => void openConfig(plugin)} disabled={busy}><IconSettings size={14} />{t('common.edit')}</Button>}
                    <Button variant="ghost" size="sm" onClick={() => void handleDelete(plugin)} disabled={busy} title={t('common.delete')}><IconTrash2 size={15} /></Button>
                  </div>
                </article>
              );
            })}
          </div>
        )}
      </Card>

      <Modal
        open={Boolean(configPlugin)}
        title={configPlugin ? t('plugins.config_title', { defaultValue: `Configure ${getPluginTitle(configPlugin)}` }) : undefined}
        onClose={() => { if (!configSaving) setConfigPlugin(null); }}
        closeDisabled={configSaving}
        width={640}
        footer={<><Button variant="secondary" onClick={() => setConfigPlugin(null)} disabled={configSaving}>{t('common.cancel')}</Button><Button onClick={() => void saveConfig()} loading={configSaving} disabled={configLoading}>{t('common.save')}</Button></>}
      >
        {configLoading ? <div className={styles.state}><LoadingSpinner /> {t('common.loading')}</div> : configError ? <div className={styles.error} role="alert">{configError}</div> : configPlugin && <div className={styles.configForm}>{(configPlugin.config_fields ?? []).map((field) => <PluginConfigFieldInput key={field.name} field={field} value={config[field.name]} onChange={(value) => setConfig((current) => ({ ...current, [field.name]: value }))} t={t} />)}</div>}
      </Modal>
    </div>
  );
}

function PluginConfigFieldInput({
  field,
  value,
  onChange,
  t,
}: {
  field: ManagementPluginConfigField;
  value: unknown;
  onChange: (value: unknown) => void;
  t: (key: string, options?: Record<string, unknown>) => string;
}) {
  const type = field.type.toLowerCase();
  const label = field.name.replace(/[_-]+/g, ' ');
  if (type === 'boolean') {
    return <ToggleSwitch checked={value === true} onChange={onChange} label={label} />;
  }
  if (type === 'enum' && field.enum_values?.length) {
    return <div className={styles.field}><label>{label}</label><Select value={String(value ?? '')} options={field.enum_values.map((option) => ({ value: option, label: option }))} onChange={onChange} placeholder={t('common.not_set', { defaultValue: 'Not set' })} /><small>{field.description}</small></div>;
  }
  if (type === 'array' || type === 'object') {
    const text = value === undefined ? '' : formatValue(value);
    return <div className={styles.field}><label>{label}</label><textarea className={styles.textarea} value={text} onChange={(event) => onChange(parseJsonValue(event.target.value, value))} /><small>{field.description || t('plugins.json_hint', { defaultValue: 'Enter valid JSON.' })}</small></div>;
  }
  return <Input label={label} type={type === 'number' || type === 'integer' ? 'number' : 'text'} value={value === undefined ? '' : String(value)} onChange={(event) => onChange(type === 'number' || type === 'integer' ? Number(event.target.value) : event.target.value)} hint={field.description} />;
}
