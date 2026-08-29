import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import {
  AmpcodeSection,
  ClaudeSection,
  CodexSection,
  GeminiSection,
  OpenAISection,
  VertexSection,
  ProviderNav,
  useProviderRecentRequests,
} from '@/components/providers';
import {
  hasDisableAllModelsRule,
  withDisableAllModelsRule,
  withoutDisableAllModelsRule,
} from '@/components/providers/utils';
import { usePageTransitionLayer } from '@/components/common/PageTransitionLayer';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { ampcodeApi, providersApi } from '@/services/api';
import { useAuthStore, useConfigStore, useNotificationStore, useThemeStore } from '@/stores';
import iconGemini from '@/assets/icons/gemini.svg';
import iconCodex from '@/assets/icons/codex.svg';
import iconClaude from '@/assets/icons/claude.svg';
import iconVertex from '@/assets/icons/vertex.svg';
import iconAmp from '@/assets/icons/amp.svg';
import iconOpenaiLight from '@/assets/icons/openai-light.svg';
import type { GeminiKeyConfig, OpenAIProviderConfig, ProviderKeyConfig } from '@/types';
import styles from './AiProvidersPage.module.scss';

type ProviderFilter = 'all' | 'gemini' | 'codex' | 'claude' | 'vertex' | 'ampcode' | 'openai';

export function AiProvidersPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { showNotification, showConfirmation } = useNotificationStore();
  const resolvedTheme = useThemeStore((state) => state.resolvedTheme);
  const connectionStatus = useAuthStore((state) => state.connectionStatus);

  const config = useConfigStore((state) => state.config);
  const fetchConfig = useConfigStore((state) => state.fetchConfig);
  const updateConfigValue = useConfigStore((state) => state.updateConfigValue);
  const clearCache = useConfigStore((state) => state.clearCache);
  const isCacheValid = useConfigStore((state) => state.isCacheValid);

  const hasMounted = useRef(false);
  const [loading, setLoading] = useState(() => !isCacheValid());
  const [error, setError] = useState('');

  const [geminiKeys, setGeminiKeys] = useState<GeminiKeyConfig[]>(
    () => config?.geminiApiKeys || []
  );
  const [codexConfigs, setCodexConfigs] = useState<ProviderKeyConfig[]>(
    () => config?.codexApiKeys || []
  );
  const [claudeConfigs, setClaudeConfigs] = useState<ProviderKeyConfig[]>(
    () => config?.claudeApiKeys || []
  );
  const [vertexConfigs, setVertexConfigs] = useState<ProviderKeyConfig[]>(
    () => config?.vertexApiKeys || []
  );
  const [openaiProviders, setOpenaiProviders] = useState<OpenAIProviderConfig[]>(
    () => config?.openaiCompatibility || []
  );

  const [configSwitchingKey, setConfigSwitchingKey] = useState<string | null>(null);
  const [providerFilter, setProviderFilter] = useState<ProviderFilter>('all');

  const disableControls = connectionStatus !== 'connected';
  const isSwitching = Boolean(configSwitchingKey);
  const pageTransitionLayer = usePageTransitionLayer();
  const isCurrentLayer = pageTransitionLayer ? pageTransitionLayer.status === 'current' : true;

  const { usageByProvider, loadRecentRequests, refreshRecentRequests } = useProviderRecentRequests({
    enabled: isCurrentLayer,
  });

  const providerItems = [
    { id: 'gemini' as const, label: 'Gemini', icon: iconGemini, count: geminiKeys.length },
    { id: 'codex' as const, label: 'Codex', icon: iconCodex, count: codexConfigs.length },
    { id: 'claude' as const, label: 'Claude', icon: iconClaude, count: claudeConfigs.length },
    { id: 'vertex' as const, label: 'Vertex', icon: iconVertex, count: vertexConfigs.length },
    { id: 'ampcode' as const, label: 'Ampcode', icon: iconAmp, count: config?.ampcode ? 1 : 0 },
    { id: 'openai' as const, label: 'OpenAI', icon: iconOpenaiLight, count: openaiProviders.length },
  ];
  const configuredRoutes = providerItems.reduce((total, item) => total + item.count, 0);
  const enabledRoutes = geminiKeys.filter((item) => !hasDisableAllModelsRule(item.excludedModels)).length
    + codexConfigs.filter((item) => !hasDisableAllModelsRule(item.excludedModels)).length
    + claudeConfigs.filter((item) => !hasDisableAllModelsRule(item.excludedModels)).length
    + vertexConfigs.filter((item) => !hasDisableAllModelsRule(item.excludedModels)).length
    + openaiProviders.filter((item) => item.disabled !== true).length
    + (config?.ampcode ? 1 : 0);
  const modelNames = new Set<string>();
  [...geminiKeys, ...codexConfigs, ...claudeConfigs, ...vertexConfigs, ...openaiProviders].forEach((item) => {
    item.models?.forEach((model) => {
      if (model.name) modelNames.add(model.name);
    });
  });
  const recentActivity = Array.from(usageByProvider.values()).reduce(
    (total, entries) => Array.from(entries.values()).reduce(
      (entryTotal, entry) => entryTotal + entry.success + entry.failed,
      total
    ),
    0
  );

  const getErrorMessage = (err: unknown) => {
    if (err instanceof Error) return err.message;
    if (typeof err === 'string') return err;
    return '';
  };

  const loadConfigs = useCallback(async () => {
    const hasValidCache = isCacheValid();
    if (!hasValidCache) {
      setLoading(true);
    }
    setError('');
    try {
      const [configResult, vertexResult, ampcodeResult, openaiResult] = await Promise.allSettled([
        fetchConfig(),
        providersApi.getVertexConfigs(),
        ampcodeApi.getAmpcode(),
        providersApi.getOpenAIProviders(),
      ]);

      if (configResult.status !== 'fulfilled') {
        throw configResult.reason;
      }

      const data = configResult.value;
      setGeminiKeys(data?.geminiApiKeys || []);
      setCodexConfigs(data?.codexApiKeys || []);
      setClaudeConfigs(data?.claudeApiKeys || []);
      setVertexConfigs(data?.vertexApiKeys || []);
      setOpenaiProviders(data?.openaiCompatibility || []);

      if (vertexResult.status === 'fulfilled') {
        setVertexConfigs(vertexResult.value || []);
        updateConfigValue('vertex-api-key', vertexResult.value || []);
        clearCache('vertex-api-key');
      }

      if (ampcodeResult.status === 'fulfilled') {
        updateConfigValue('ampcode', ampcodeResult.value);
        clearCache('ampcode');
      }

      if (openaiResult.status === 'fulfilled') {
        setOpenaiProviders(openaiResult.value || []);
        updateConfigValue('openai-compatibility', openaiResult.value || []);
        clearCache('openai-compatibility');
      }
    } catch (err: unknown) {
      const message = getErrorMessage(err) || t('notification.refresh_failed');
      setError(message);
    } finally {
      setLoading(false);
    }
  }, [clearCache, fetchConfig, isCacheValid, t, updateConfigValue]);

  useEffect(() => {
    if (hasMounted.current) return;
    hasMounted.current = true;
    loadConfigs();
  }, [loadConfigs]);

  useEffect(() => {
    if (!isCurrentLayer) return;
    void loadRecentRequests().catch(() => {});
  }, [isCurrentLayer, loadRecentRequests]);

  useEffect(() => {
    if (config?.geminiApiKeys) setGeminiKeys(config.geminiApiKeys);
    if (config?.codexApiKeys) setCodexConfigs(config.codexApiKeys);
    if (config?.claudeApiKeys) setClaudeConfigs(config.claudeApiKeys);
    if (config?.vertexApiKeys) setVertexConfigs(config.vertexApiKeys);
    if (config?.openaiCompatibility) setOpenaiProviders(config.openaiCompatibility);
  }, [
    config?.geminiApiKeys,
    config?.codexApiKeys,
    config?.claudeApiKeys,
    config?.vertexApiKeys,
    config?.openaiCompatibility,
  ]);

  const handleRecentRequestsRefresh = useCallback(async () => {
    await refreshRecentRequests();
  }, [refreshRecentRequests]);

  useHeaderRefresh(handleRecentRequestsRefresh, isCurrentLayer);

  const openEditor = useCallback(
    (path: string) => {
      navigate(path, { state: { fromAiProviders: true } });
    },
    [navigate]
  );

  const deleteGemini = async (index: number) => {
    const entry = geminiKeys[index];
    if (!entry) return;
    showConfirmation({
      title: t('ai_providers.gemini_delete_title', { defaultValue: 'Delete Gemini Key' }),
      message: t('ai_providers.gemini_delete_confirm'),
      variant: 'danger',
      confirmText: t('common.confirm'),
      onConfirm: async () => {
        try {
          await providersApi.deleteGeminiKey(entry.apiKey, entry.baseUrl);
          const next = geminiKeys.filter((_, idx) => idx !== index);
          setGeminiKeys(next);
          updateConfigValue('gemini-api-key', next);
          clearCache('gemini-api-key');
          showNotification(t('notification.gemini_key_deleted'), 'success');
        } catch (err: unknown) {
          const message = getErrorMessage(err);
          showNotification(`${t('notification.delete_failed')}: ${message}`, 'error');
        }
      },
    });
  };

  const setConfigEnabled = async (
    provider: 'gemini' | 'codex' | 'claude' | 'vertex',
    index: number,
    enabled: boolean
  ) => {
    if (provider === 'gemini') {
      const current = geminiKeys[index];
      if (!current) return;

      const switchingKey = `${provider}:${current.apiKey}`;
      setConfigSwitchingKey(switchingKey);

      const previousList = geminiKeys;
      const nextExcluded = enabled
        ? withoutDisableAllModelsRule(current.excludedModels)
        : withDisableAllModelsRule(current.excludedModels);
      const nextItem: GeminiKeyConfig = { ...current, excludedModels: nextExcluded };
      const nextList = previousList.map((item, idx) => (idx === index ? nextItem : item));

      setGeminiKeys(nextList);
      updateConfigValue('gemini-api-key', nextList);
      clearCache('gemini-api-key');

      try {
        await providersApi.saveGeminiKeys(nextList);
        showNotification(
          enabled ? t('notification.config_enabled') : t('notification.config_disabled'),
          'success'
        );
      } catch (err: unknown) {
        const message = getErrorMessage(err);
        setGeminiKeys(previousList);
        updateConfigValue('gemini-api-key', previousList);
        clearCache('gemini-api-key');
        showNotification(`${t('notification.update_failed')}: ${message}`, 'error');
      } finally {
        setConfigSwitchingKey(null);
      }
      return;
    }

    const source =
      provider === 'codex'
        ? codexConfigs
        : provider === 'claude'
          ? claudeConfigs
          : vertexConfigs;
    const current = source[index];
    if (!current) return;

    const switchingKey = `${provider}:${current.apiKey}`;
    setConfigSwitchingKey(switchingKey);

    const previousList = source;
    const nextExcluded = enabled
      ? withoutDisableAllModelsRule(current.excludedModels)
      : withDisableAllModelsRule(current.excludedModels);
    const nextItem: ProviderKeyConfig = { ...current, excludedModels: nextExcluded };
    const nextList = previousList.map((item, idx) => (idx === index ? nextItem : item));

    if (provider === 'codex') {
      setCodexConfigs(nextList);
      updateConfigValue('codex-api-key', nextList);
      clearCache('codex-api-key');
    } else if (provider === 'claude') {
      setClaudeConfigs(nextList);
      updateConfigValue('claude-api-key', nextList);
      clearCache('claude-api-key');
    } else {
      setVertexConfigs(nextList);
      updateConfigValue('vertex-api-key', nextList);
      clearCache('vertex-api-key');
    }

    try {
      if (provider === 'codex') {
        await providersApi.saveCodexConfigs(nextList);
      } else if (provider === 'claude') {
        await providersApi.saveClaudeConfigs(nextList);
      } else {
        await providersApi.saveVertexConfigs(nextList);
      }
      showNotification(
        enabled ? t('notification.config_enabled') : t('notification.config_disabled'),
        'success'
      );
    } catch (err: unknown) {
      const message = getErrorMessage(err);
      if (provider === 'codex') {
        setCodexConfigs(previousList);
        updateConfigValue('codex-api-key', previousList);
        clearCache('codex-api-key');
      } else if (provider === 'claude') {
        setClaudeConfigs(previousList);
        updateConfigValue('claude-api-key', previousList);
        clearCache('claude-api-key');
      } else {
        setVertexConfigs(previousList);
        updateConfigValue('vertex-api-key', previousList);
        clearCache('vertex-api-key');
      }
      showNotification(`${t('notification.update_failed')}: ${message}`, 'error');
    } finally {
      setConfigSwitchingKey(null);
    }
  };

  const setOpenAIProviderEnabled = async (index: number, enabled: boolean) => {
    const current = openaiProviders[index];
    if (!current) return;

    const switchingKey = `openai:${current.name}:${index}`;
    setConfigSwitchingKey(switchingKey);

    const previousList = openaiProviders;
    const nextItem: OpenAIProviderConfig = { ...current, disabled: !enabled };
    const nextList = previousList.map((item, idx) => (idx === index ? nextItem : item));

    setOpenaiProviders(nextList);
    updateConfigValue('openai-compatibility', nextList);
    clearCache('openai-compatibility');

    try {
      await providersApi.updateOpenAIProviderDisabled(index, !enabled);
      showNotification(
        enabled ? t('notification.config_enabled') : t('notification.config_disabled'),
        'success'
      );
    } catch (err: unknown) {
      const message = getErrorMessage(err);
      setOpenaiProviders(previousList);
      updateConfigValue('openai-compatibility', previousList);
      clearCache('openai-compatibility');
      showNotification(`${t('notification.update_failed')}: ${message}`, 'error');
    } finally {
      setConfigSwitchingKey(null);
    }
  };

  const deleteProviderEntry = async (type: 'codex' | 'claude', index: number) => {
    const source = type === 'codex' ? codexConfigs : claudeConfigs;
    const entry = source[index];
    if (!entry) return;
    showConfirmation({
      title: t(`ai_providers.${type}_delete_title`, { defaultValue: `Delete ${type === 'codex' ? 'Codex' : 'Claude'} Config` }),
      message: t(`ai_providers.${type}_delete_confirm`),
      variant: 'danger',
      confirmText: t('common.confirm'),
      onConfirm: async () => {
        try {
          if (type === 'codex') {
            await providersApi.deleteCodexConfig(entry.apiKey, entry.baseUrl);
            const next = codexConfigs.filter((_, idx) => idx !== index);
            setCodexConfigs(next);
            updateConfigValue('codex-api-key', next);
            clearCache('codex-api-key');
            showNotification(t('notification.codex_config_deleted'), 'success');
          } else {
            await providersApi.deleteClaudeConfig(entry.apiKey, entry.baseUrl);
            const next = claudeConfigs.filter((_, idx) => idx !== index);
            setClaudeConfigs(next);
            updateConfigValue('claude-api-key', next);
            clearCache('claude-api-key');
            showNotification(t('notification.claude_config_deleted'), 'success');
          }
        } catch (err: unknown) {
          const message = getErrorMessage(err);
          showNotification(`${t('notification.delete_failed')}: ${message}`, 'error');
        }
      },
    });
  };

  const deleteVertex = async (index: number) => {
    const entry = vertexConfigs[index];
    if (!entry) return;
    showConfirmation({
      title: t('ai_providers.vertex_delete_title', { defaultValue: 'Delete Vertex Config' }),
      message: t('ai_providers.vertex_delete_confirm'),
      variant: 'danger',
      confirmText: t('common.confirm'),
      onConfirm: async () => {
        try {
          await providersApi.deleteVertexConfig(entry.apiKey, entry.baseUrl);
          const next = vertexConfigs.filter((_, idx) => idx !== index);
          setVertexConfigs(next);
          updateConfigValue('vertex-api-key', next);
          clearCache('vertex-api-key');
          showNotification(t('notification.vertex_config_deleted'), 'success');
        } catch (err: unknown) {
          const message = getErrorMessage(err);
          showNotification(`${t('notification.delete_failed')}: ${message}`, 'error');
        }
      },
    });
  };

  const deleteOpenai = async (index: number) => {
    const entry = openaiProviders[index];
    if (!entry) return;
    showConfirmation({
      title: t('ai_providers.openai_delete_title', { defaultValue: 'Delete OpenAI Provider' }),
      message: t('ai_providers.openai_delete_confirm'),
      variant: 'danger',
      confirmText: t('common.confirm'),
      onConfirm: async () => {
        try {
          await providersApi.deleteOpenAIProvider(entry.name);
          const next = openaiProviders.filter((_, idx) => idx !== index);
          setOpenaiProviders(next);
          updateConfigValue('openai-compatibility', next);
          clearCache('openai-compatibility');
          showNotification(t('notification.openai_provider_deleted'), 'success');
        } catch (err: unknown) {
          const message = getErrorMessage(err);
          showNotification(`${t('notification.delete_failed')}: ${message}`, 'error');
        }
      },
    });
  };

  const showProvider = (provider: ProviderFilter) => providerFilter === 'all' || providerFilter === provider;

  const handleProviderFilter = (provider: ProviderFilter) => {
    setProviderFilter(provider);
    if (provider === 'all') {
      document.querySelector('.content')?.scrollTo({ top: 0, behavior: 'smooth' });
      return;
    }

    window.requestAnimationFrame(() => {
      document.getElementById(`provider-${provider}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    });
  };

  return (
    <div className={styles.container}>
      <header className={styles.pageHeader}>
        <div>
          <span className={styles.pageEyebrow}>{t('ai_providers.workspace_label')}</span>
          <h1 className={styles.pageTitle}>{t('ai_providers.title')}</h1>
          <p className={styles.pageSubtitle}>{t('ai_providers.workspace_desc')}</p>
        </div>
        <span className={`${styles.connectionBadge} ${connectionStatus === 'connected' ? styles.connectionGood : styles.connectionBad}`}>
          <span className={styles.connectionDot} />
          {connectionStatus === 'connected' ? t('common.connected') : t('common.disconnected')}
        </span>
      </header>

      <section className={styles.overviewGrid} aria-label={t('ai_providers.workspace_label')}>
        <div className={`${styles.overviewCard} ${styles.overviewCardAccent}`}>
          <span className={styles.overviewLabel}>{t('ai_providers.provider_types')}</span>
          <strong className={styles.overviewValue}>{providerItems.length}</strong>
          <span className={styles.overviewMeta}>{t('ai_providers.provider_types_desc')}</span>
        </div>
        <div className={styles.overviewCard}>
          <span className={styles.overviewLabel}>{t('ai_providers.configured_routes')}</span>
          <strong className={styles.overviewValue}>{configuredRoutes}</strong>
          <span className={styles.overviewMeta}>{t('ai_providers.configured_routes_desc')}</span>
        </div>
        <div className={styles.overviewCard}>
          <span className={styles.overviewLabel}>{t('ai_providers.enabled_routes')}</span>
          <strong className={styles.overviewValue}>{enabledRoutes}</strong>
          <span className={styles.overviewMeta}>{t('ai_providers.enabled_routes_desc')}</span>
        </div>
        <div className={styles.overviewCard}>
          <span className={styles.overviewLabel}>{t('ai_providers.recent_requests')}</span>
          <strong className={styles.overviewValue}>{recentActivity}</strong>
          <span className={styles.overviewMeta}>{t('ai_providers.recent_requests_desc')}</span>
        </div>
      </section>

      <nav className={styles.providerSwitcher} aria-label={t('ai_providers.provider_switcher')}>
        <button
          type="button"
          className={`${styles.providerSwitcherItem} ${providerFilter === 'all' ? styles.providerSwitcherItemActive : ''}`}
          onClick={() => handleProviderFilter('all')}
        >
          <span>{t('common.all')}</span>
          <span className={styles.providerSwitcherCount}>{configuredRoutes}</span>
        </button>
        {providerItems.map((provider) => (
          <button
            type="button"
            key={provider.id}
            className={`${styles.providerSwitcherItem} ${providerFilter === provider.id ? styles.providerSwitcherItemActive : ''}`}
            onClick={() => handleProviderFilter(provider.id)}
          >
            <img src={provider.icon} alt="" className={styles.providerSwitcherIcon} />
            <span>{provider.label}</span>
            <span className={styles.providerSwitcherCount}>{provider.count}</span>
          </button>
        ))}
      </nav>

      <div className={styles.content}>
        {error && <div className="error-box">{error}</div>}

        {showProvider('gemini') && <div id="provider-gemini">
          <GeminiSection
            configs={geminiKeys}
            usageByProvider={usageByProvider}
            loading={loading}
            disableControls={disableControls}
            isSwitching={isSwitching}
            onAdd={() => openEditor('/ai-providers/gemini/new')}
            onEdit={(index) => openEditor(`/ai-providers/gemini/${index}`)}
            onDelete={deleteGemini}
            onToggle={(index, enabled) => void setConfigEnabled('gemini', index, enabled)}
          />
        </div>}

        {showProvider('codex') && <div id="provider-codex">
          <CodexSection
            configs={codexConfigs}
            usageByProvider={usageByProvider}
            loading={loading}
            disableControls={disableControls}
            isSwitching={isSwitching}
            onAdd={() => openEditor('/ai-providers/codex/new')}
            onEdit={(index) => openEditor(`/ai-providers/codex/${index}`)}
            onDelete={(index) => void deleteProviderEntry('codex', index)}
            onToggle={(index, enabled) => void setConfigEnabled('codex', index, enabled)}
          />
        </div>}

        {showProvider('claude') && <div id="provider-claude">
          <ClaudeSection
            configs={claudeConfigs}
            usageByProvider={usageByProvider}
            loading={loading}
            disableControls={disableControls}
            isSwitching={isSwitching}
            onAdd={() => openEditor('/ai-providers/claude/new')}
            onEdit={(index) => openEditor(`/ai-providers/claude/${index}`)}
            onDelete={(index) => void deleteProviderEntry('claude', index)}
            onToggle={(index, enabled) => void setConfigEnabled('claude', index, enabled)}
          />
        </div>}

        {showProvider('vertex') && <div id="provider-vertex">
          <VertexSection
            configs={vertexConfigs}
            usageByProvider={usageByProvider}
            loading={loading}
            disableControls={disableControls}
            isSwitching={isSwitching}
            onAdd={() => openEditor('/ai-providers/vertex/new')}
            onEdit={(index) => openEditor(`/ai-providers/vertex/${index}`)}
            onDelete={deleteVertex}
            onToggle={(index, enabled) => void setConfigEnabled('vertex', index, enabled)}
          />
        </div>}

        {showProvider('ampcode') && <div id="provider-ampcode">
          <AmpcodeSection
            config={config?.ampcode}
            loading={loading}
            disableControls={disableControls}
            isSwitching={isSwitching}
            onEdit={() => openEditor('/ai-providers/ampcode')}
          />
        </div>}

        {showProvider('openai') && <div id="provider-openai">
          <OpenAISection
            configs={openaiProviders}
            usageByProvider={usageByProvider}
            loading={loading}
            disableControls={disableControls}
            isSwitching={isSwitching}
            resolvedTheme={resolvedTheme}
            onAdd={() => openEditor('/ai-providers/openai/new')}
            onEdit={(index) => openEditor(`/ai-providers/openai/${index}`)}
            onDelete={deleteOpenai}
            onToggle={(index, enabled) => void setOpenAIProviderEnabled(index, enabled)}
          />
        </div>}
      </div>

      <ProviderNav />
    </div>
  );
}
