import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  IconKey,
  IconBot,
  IconFileText,
  IconSatellite,
  IconRefreshCw,
  IconChartLine
} from '@/components/ui/icons';
import { useAuthStore, useConfigStore, useModelsStore, useUsageServiceStore } from '@/stores';
import { apiKeysApi, providersApi, authFilesApi } from '@/services/api';
import {
  isUsageServiceId,
  usageServiceApi,
  type UsageAnalyticsMetric,
  type UsageAnalyticsModelStat,
  type UsageAnalyticsTimelineItem,
  type UsageServiceStatus
} from '@/services/api/usageService';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import styles from './DashboardPage.module.scss';

interface QuickStat {
  label: string;
  value: number | string;
  icon: ReactNode;
  path: string;
  loading?: boolean;
  sublabel?: string;
}

interface ProviderStats {
  gemini: number | null;
  codex: number | null;
  claude: number | null;
  openai: number | null;
}

type TimeOfDay = 'morning' | 'afternoon' | 'evening' | 'night';
type UsageSnapshotState = 'loading' | 'ready' | 'unavailable' | 'error';

type DashboardUsageSnapshot = {
  state: UsageSnapshotState;
  summary: UsageAnalyticsMetric | null;
  timeline: UsageAnalyticsTimelineItem[];
  modelStats: UsageAnalyticsModelStat[];
  status: UsageServiceStatus | null;
  error: string;
  refreshedAt: number | null;
};

const EMPTY_USAGE_SNAPSHOT: DashboardUsageSnapshot = {
  state: 'unavailable',
  summary: null,
  timeline: [],
  modelStats: [],
  status: null,
  error: '',
  refreshedAt: null
};

const EMPTY_ANALYTICS_METRIC: UsageAnalyticsMetric = {
  requests: 0,
  successes: 0,
  failures: 0,
  input_tokens: 0,
  output_tokens: 0,
  reasoning_tokens: 0,
  cached_tokens: 0,
  cache_tokens: 0,
  total_tokens: 0,
  latency_sum_ms: 0,
  latency_samples: 0,
  zero_token_calls: 0,
  last_seen_ms: 0,
  cost_usd: 0
};

const readMetricNumber = (value: unknown): number =>
  typeof value === 'number' && Number.isFinite(value) ? value : 0;

const formatCompactNumber = (value: number, locale: string): string =>
  new Intl.NumberFormat(locale, { notation: 'compact', maximumFractionDigits: 1 }).format(value);

const formatCurrency = (value: number, locale: string): string =>
  new Intl.NumberFormat(locale, {
    style: 'currency',
    currency: 'USD',
    maximumFractionDigits: 2
  }).format(value);

const formatRelativeTime = (timestampMs: number | null, locale: string): string => {
  if (!timestampMs) return '—';
  const elapsedSeconds = Math.max(0, Math.round((Date.now() - timestampMs) / 1000));
  if (elapsedSeconds < 60) return new Intl.RelativeTimeFormat(locale, { numeric: 'auto' }).format(-elapsedSeconds, 'second');
  const elapsedMinutes = Math.round(elapsedSeconds / 60);
  if (elapsedMinutes < 60) return new Intl.RelativeTimeFormat(locale, { numeric: 'auto' }).format(-elapsedMinutes, 'minute');
  return new Intl.RelativeTimeFormat(locale, { numeric: 'auto' }).format(-Math.round(elapsedMinutes / 60), 'hour');
};

function getTimeOfDay(): TimeOfDay {
  const hour = new Date().getHours();
  if (hour >= 5 && hour < 12) return 'morning';
  if (hour >= 12 && hour < 17) return 'afternoon';
  if (hour >= 17 && hour < 21) return 'evening';
  return 'night';
}

export function DashboardPage() {
  const { t, i18n } = useTranslation();
  const connectionStatus = useAuthStore((state) => state.connectionStatus);
  const serverVersion = useAuthStore((state) => state.serverVersion);
  const serverBuildDate = useAuthStore((state) => state.serverBuildDate);
  const apiBase = useAuthStore((state) => state.apiBase);
  const config = useConfigStore((state) => state.config);

  const models = useModelsStore((state) => state.models);
  const modelsLoading = useModelsStore((state) => state.loading);
  const fetchModelsFromStore = useModelsStore((state) => state.fetchModels);

  const [stats, setStats] = useState<{
    apiKeys: number | null;
    authFiles: number | null;
  }>({
    apiKeys: null,
    authFiles: null
  });

  const [providerStats, setProviderStats] = useState<ProviderStats>({
    gemini: null,
    codex: null,
    claude: null,
    openai: null
  });

  const [loading, setLoading] = useState(true);
  const [usageSnapshot, setUsageSnapshot] = useState<DashboardUsageSnapshot>(EMPTY_USAGE_SNAPSHOT);
  const usageRequestIdRef = useRef(0);
  const usageServiceEnabled = useUsageServiceStore((state) => state.enabled);
  const usageServiceBase = useUsageServiceStore((state) => state.serviceBase);
  const managementKey = useAuthStore((state) => state.managementKey);

  // Time-of-day state for dynamic greeting
  const [timeOfDay, setTimeOfDay] = useState<TimeOfDay>(getTimeOfDay);
  const [currentTime, setCurrentTime] = useState(() => new Date());

  const apiKeysCache = useRef<string[]>([]);

  useEffect(() => {
    apiKeysCache.current = [];
  }, [apiBase, config?.apiKeys]);

  // Update time every 60 seconds
  useEffect(() => {
    const id = setInterval(() => {
      setTimeOfDay(getTimeOfDay());
      setCurrentTime(new Date());
    }, 60_000);
    return () => clearInterval(id);
  }, []);

  const normalizeApiKeyList = (input: unknown): string[] => {
    if (!Array.isArray(input)) return [];
    const seen = new Set<string>();
    const keys: string[] = [];

    input.forEach((item) => {
      const record =
        item !== null && typeof item === 'object' && !Array.isArray(item)
          ? (item as Record<string, unknown>)
          : null;
      const value =
        typeof item === 'string'
          ? item
          : record
            ? (record['api-key'] ?? record['apiKey'] ?? record.key ?? record.Key)
            : '';
      const trimmed = String(value ?? '').trim();
      if (!trimmed || seen.has(trimmed)) return;
      seen.add(trimmed);
      keys.push(trimmed);
    });

    return keys;
  };

  const resolveApiKeysForModels = useCallback(async () => {
    if (apiKeysCache.current.length) {
      return apiKeysCache.current;
    }

    const configKeys = normalizeApiKeyList(config?.apiKeys);
    if (configKeys.length) {
      apiKeysCache.current = configKeys;
      return configKeys;
    }

    try {
      const list = await apiKeysApi.list();
      const normalized = normalizeApiKeyList(list);
      if (normalized.length) {
        apiKeysCache.current = normalized;
      }
      return normalized;
    } catch {
      return [];
    }
  }, [config?.apiKeys]);

  const fetchModels = useCallback(async () => {
    if (connectionStatus !== 'connected' || !apiBase) {
      return;
    }

    try {
      const apiKeys = await resolveApiKeysForModels();
      const primaryKey = apiKeys[0];
      await fetchModelsFromStore(apiBase, primaryKey);
    } catch {
      // Ignore model fetch errors on dashboard
    }
  }, [connectionStatus, apiBase, resolveApiKeysForModels, fetchModelsFromStore]);

  const refreshUsageSnapshot = useCallback(async () => {
    const requestId = usageRequestIdRef.current + 1;
    usageRequestIdRef.current = requestId;

    if (connectionStatus !== 'connected' || !apiBase) {
      setUsageSnapshot(EMPTY_USAGE_SNAPSHOT);
      return;
    }

    setUsageSnapshot((previous) => ({ ...previous, state: 'loading', error: '' }));
    const serviceBase = usageServiceEnabled && usageServiceBase ? usageServiceBase : apiBase;

    try {
      const info = await usageServiceApi.getInfo(serviceBase);
      if (!isUsageServiceId(info.service)) {
        if (usageRequestIdRef.current === requestId) {
          setUsageSnapshot(EMPTY_USAGE_SNAPSHOT);
        }
        return;
      }

      const now = Date.now();
      const [statusResult, analyticsResult] = await Promise.allSettled([
        usageServiceApi.getStatus(serviceBase, managementKey),
        usageServiceApi.getAnalytics(
          serviceBase,
          managementKey,
          {
            from_ms: now - 24 * 60 * 60 * 1000,
            to_ms: now,
            include: ['summary', 'timeline', 'model_stats']
          }
        )
      ]);

      if (usageRequestIdRef.current !== requestId) return;

      const status = statusResult.status === 'fulfilled' ? statusResult.value : null;
      const analytics = analyticsResult.status === 'fulfilled' ? analyticsResult.value : null;
      const analyticsError = analyticsResult.status === 'rejected'
        ? analyticsResult.reason instanceof Error
          ? analyticsResult.reason.message
          : String(analyticsResult.reason)
        : '';

      setUsageSnapshot({
        state: analytics ? 'ready' : status ? 'ready' : 'error',
        summary: analytics?.summary ?? null,
        timeline: analytics?.timeline ?? [],
        modelStats: analytics?.model_stats ?? [],
        status,
        error: analyticsError,
        refreshedAt: Date.now()
      });
    } catch (error) {
      if (usageRequestIdRef.current !== requestId) return;
      setUsageSnapshot({
        ...EMPTY_USAGE_SNAPSHOT,
        state: 'error',
        error: error instanceof Error ? error.message : String(error)
      });
    }
  }, [apiBase, connectionStatus, managementKey, usageServiceBase, usageServiceEnabled]);

  useHeaderRefresh(refreshUsageSnapshot, connectionStatus === 'connected');

  useEffect(() => {
    void refreshUsageSnapshot();
    return () => {
      usageRequestIdRef.current += 1;
    };
  }, [refreshUsageSnapshot]);

  useEffect(() => {
    const fetchStats = async () => {
      setLoading(true);
      try {
        const [keysRes, filesRes, geminiRes, codexRes, claudeRes, openaiRes] = await Promise.allSettled([
          apiKeysApi.list(),
          authFilesApi.list(),
          providersApi.getGeminiKeys(),
          providersApi.getCodexConfigs(),
          providersApi.getClaudeConfigs(),
          providersApi.getOpenAIProviders()
        ]);

        setStats({
          apiKeys: keysRes.status === 'fulfilled' ? keysRes.value.length : null,
          authFiles: filesRes.status === 'fulfilled' ? filesRes.value.files.length : null
        });

        setProviderStats({
          gemini: geminiRes.status === 'fulfilled' ? geminiRes.value.length : null,
          codex: codexRes.status === 'fulfilled' ? codexRes.value.length : null,
          claude: claudeRes.status === 'fulfilled' ? claudeRes.value.length : null,
          openai: openaiRes.status === 'fulfilled' ? openaiRes.value.length : null
        });
      } finally {
        setLoading(false);
      }
    };

    if (connectionStatus === 'connected') {
      fetchStats();
      fetchModels();
    } else {
      setLoading(false);
    }
  }, [connectionStatus, fetchModels]);

  // Calculate total provider keys only when all provider stats are available.
  const providerStatsReady =
    providerStats.gemini !== null &&
    providerStats.codex !== null &&
    providerStats.claude !== null &&
    providerStats.openai !== null;
  const hasProviderStats =
    providerStats.gemini !== null ||
    providerStats.codex !== null ||
    providerStats.claude !== null ||
    providerStats.openai !== null;
  const totalProviderKeys = providerStatsReady
    ? (providerStats.gemini ?? 0) +
      (providerStats.codex ?? 0) +
      (providerStats.claude ?? 0) +
      (providerStats.openai ?? 0)
    : 0;

  const quickStats: QuickStat[] = [
    {
      label: t('dashboard.management_keys'),
      value: stats.apiKeys ?? '-',
      icon: <IconKey size={24} />,
      path: '/config',
      loading: loading && stats.apiKeys === null,
      sublabel: t('nav.config_management')
    },
    {
      label: t('nav.ai_providers'),
      value: loading ? '-' : providerStatsReady ? totalProviderKeys : '-',
      icon: <IconBot size={24} />,
      path: '/ai-providers',
      loading: loading,
      sublabel: hasProviderStats
        ? t('dashboard.provider_keys_detail', {
            gemini: providerStats.gemini ?? '-',
            codex: providerStats.codex ?? '-',
            claude: providerStats.claude ?? '-',
            openai: providerStats.openai ?? '-'
          })
        : undefined
    },
    {
      label: t('nav.auth_files'),
      value: stats.authFiles ?? '-',
      icon: <IconFileText size={24} />,
      path: '/auth-files',
      loading: loading && stats.authFiles === null,
      sublabel: t('dashboard.oauth_credentials')
    },
    {
      label: t('dashboard.available_models'),
      value: modelsLoading ? '-' : models.length,
      icon: <IconSatellite size={24} />,
      path: '/system',
      loading: modelsLoading,
      sublabel: t('dashboard.available_models_desc')
    }
  ];

  const routingStrategyRaw = config?.routingStrategy?.trim() || '';
  const routingStrategyDisplay = !routingStrategyRaw
    ? '-'
    : routingStrategyRaw === 'round-robin'
      ? t('basic_settings.routing_strategy_round_robin')
      : routingStrategyRaw === 'fill-first'
        ? t('basic_settings.routing_strategy_fill_first')
        : routingStrategyRaw;
  const routingStrategyBadgeClass = !routingStrategyRaw
    ? styles.configBadgeUnknown
    : routingStrategyRaw === 'round-robin'
      ? styles.configBadgeRoundRobin
      : routingStrategyRaw === 'fill-first'
        ? styles.configBadgeFillFirst
        : styles.configBadgeUnknown;

  // Derived time-based values
  const greetingKey = `dashboard.greeting_${timeOfDay}`;
  const caringKey = `dashboard.caring_${timeOfDay}`;

  const formattedDate = currentTime.toLocaleDateString(i18n.language, {
    weekday: 'long',
    year: 'numeric',
    month: 'long',
    day: 'numeric'
  });

  const formattedTime = currentTime.toLocaleTimeString(i18n.language, {
    hour: '2-digit',
    minute: '2-digit'
  });

  const hasUsageData = usageSnapshot.summary !== null;
  const usageMetric = usageSnapshot.summary ?? EMPTY_ANALYTICS_METRIC;
  const usageRequests = readMetricNumber(usageMetric.requests);
  const usageSuccessRate = usageRequests > 0
    ? readMetricNumber(usageMetric.successes) / usageRequests
    : null;
  const usageTimeline = usageSnapshot.timeline
    .filter((item) => Number.isFinite(item.bucket_ms))
    .slice(-24);
  const maxTimelineRequests = Math.max(
    1,
    ...usageTimeline.map((item) => readMetricNumber(item.requests))
  );
  const topModels = [...usageSnapshot.modelStats]
    .sort((left, right) => readMetricNumber(right.requests) - readMetricNumber(left.requests))
    .slice(0, 5);
  const maxModelRequests = Math.max(
    1,
    ...topModels.map((item) => readMetricNumber(item.requests))
  );
  const collector = usageSnapshot.status?.collector;
  const collectorHasError = Boolean(collector?.lastError);
  const usageServiceStateKey = usageSnapshot.state === 'ready'
    ? 'dashboard.status_healthy'
    : usageSnapshot.state === 'loading'
      ? 'dashboard.status_loading'
      : usageSnapshot.state === 'error'
        ? 'dashboard.status_attention'
        : 'dashboard.status_unavailable';
  const usageServiceTone = usageSnapshot.state === 'ready'
    ? styles.healthGood
    : usageSnapshot.state === 'error'
      ? styles.healthBad
      : styles.healthWarn;
  const collectorStateKey = collectorHasError
    ? 'dashboard.status_attention'
    : collector
      ? 'dashboard.status_healthy'
      : 'dashboard.status_unavailable';
  const collectorTone = collectorHasError
    ? styles.healthBad
    : collector
      ? styles.healthGood
      : styles.healthWarn;

  return (
    <div className={styles.dashboard}>
      {/* Decorative background orbs */}
      <div className={styles.backgroundOrbs} aria-hidden="true">
        <div className={styles.orb1} />
        <div className={styles.orb2} />
      </div>

      {/* Hero welcome section */}
      <section className={styles.hero}>
        <span className={styles.heroWatermark} aria-hidden="true">
          OVERVIEW
        </span>
        <div className={styles.heroContent}>
          <span className={styles.heroGreeting}>{t(greetingKey)}</span>
          <h1 className={styles.heroTitle}>{t('dashboard.welcome_back')}</h1>
          <p className={styles.heroCaring}>{t(caringKey)}</p>
        </div>
        <div className={styles.heroMeta}>
          <div className={styles.dateTimeBlock}>
            <span className={styles.time}>{formattedTime}</span>
            <span className={styles.date}>{formattedDate}</span>
          </div>
          <div className={styles.connectionPill}>
            <span
              className={`${styles.statusDot} ${
                connectionStatus === 'connected'
                  ? styles.connected
                  : connectionStatus === 'connecting'
                    ? styles.connecting
                    : styles.disconnected
              }`}
            />
            <span className={styles.pillText}>
              {serverVersion
                ? `v${serverVersion.trim().replace(/^[vV]+/, '')}`
                : t(
                    connectionStatus === 'connected'
                      ? 'common.connected'
                      : connectionStatus === 'connecting'
                        ? 'common.connecting'
                        : 'common.disconnected'
                  )}
            </span>
          </div>
          {serverBuildDate && (
            <span className={styles.buildDate}>
              {new Date(serverBuildDate).toLocaleDateString(i18n.language)}
            </span>
          )}
        </div>
      </section>

      {/* Plus-style usage snapshot */}
      <section className={styles.snapshotSection}>
        <div className={styles.sectionHeader}>
          <div>
            <h2 className={styles.sectionHeading}>{t('dashboard.usage_snapshot')}</h2>
            <p className={styles.sectionDescription}>{t('dashboard.last_24_hours')}</p>
          </div>
          <button
            type="button"
            className={styles.refreshButton}
            onClick={() => void refreshUsageSnapshot()}
            disabled={usageSnapshot.state === 'loading'}
          >
            <IconRefreshCw size={15} className={usageSnapshot.state === 'loading' ? styles.spinning : undefined} />
            {t('common.refresh')}
          </button>
        </div>
        <div className={styles.metricGrid}>
          <div className={`${styles.metricCard} ${styles.metricCardAccent}`}>
            <span className={styles.metricLabel}>{t('dashboard.requests')}</span>
            <strong className={styles.metricValue}>
              {usageSnapshot.state === 'loading'
                ? '…'
                : hasUsageData
                  ? formatCompactNumber(usageRequests, i18n.language)
                  : '—'}
            </strong>
            <span className={styles.metricMeta}>{t('dashboard.last_24_hours')}</span>
          </div>
          <div className={styles.metricCard}>
            <span className={styles.metricLabel}>{t('dashboard.success_rate')}</span>
            <strong className={styles.metricValue}>
              {usageSuccessRate === null ? '—' : `${(usageSuccessRate * 100).toFixed(1)}%`}
            </strong>
            <span className={styles.metricMeta}>
              {hasUsageData
                ? `${formatCompactNumber(readMetricNumber(usageMetric.failures), i18n.language)} ${t('dashboard.failures')}`
                : '—'}
            </span>
          </div>
          <div className={styles.metricCard}>
            <span className={styles.metricLabel}>{t('dashboard.total_tokens')}</span>
            <strong className={styles.metricValue}>
              {hasUsageData
                ? formatCompactNumber(readMetricNumber(usageMetric.total_tokens), i18n.language)
                : '—'}
            </strong>
            <span className={styles.metricMeta}>
              {hasUsageData
                ? `${formatCompactNumber(readMetricNumber(usageMetric.cached_tokens), i18n.language)} ${t('dashboard.cached_tokens')}`
                : '—'}
            </span>
          </div>
          <div className={styles.metricCard}>
            <span className={styles.metricLabel}>{t('dashboard.estimated_cost')}</span>
            <strong className={styles.metricValue}>
              {hasUsageData ? formatCurrency(readMetricNumber(usageMetric.cost_usd), i18n.language) : '—'}
            </strong>
            <span className={styles.metricMeta}>
              {usageSnapshot.refreshedAt ? formatRelativeTime(usageSnapshot.refreshedAt, i18n.language) : '—'}
            </span>
          </div>
        </div>
      </section>

      <section className={styles.dashboardGrid}>
        <div className={styles.panelCard}>
          <div className={styles.panelHeader}>
            <div>
              <h2 className={styles.panelTitle}>{t('dashboard.traffic_trend')}</h2>
              <p className={styles.panelSubtitle}>{t('dashboard.hourly_requests')}</p>
            </div>
            <IconChartLine size={18} className={styles.panelIcon} />
          </div>
          {usageTimeline.length > 0 ? (
            <div className={styles.chart} role="img" aria-label={t('dashboard.traffic_trend')}>
              {usageTimeline.map((point) => {
                const requests = readMetricNumber(point.requests);
                const height = Math.max(6, (requests / maxTimelineRequests) * 100);
                const timeLabel = new Date(point.bucket_ms).toLocaleTimeString(i18n.language, {
                  hour: '2-digit',
                  minute: '2-digit'
                });
                return (
                  <div className={styles.chartColumn} key={point.bucket_ms} title={`${timeLabel}: ${requests}`}>
                    <span className={styles.chartBar} style={{ height: `${height}%` }} />
                    <span className={styles.chartLabel}>{timeLabel}</span>
                  </div>
                );
              })}
            </div>
          ) : (
            <div className={styles.panelEmpty}>{t('dashboard.traffic_empty')}</div>
          )}
          <Link to="/monitoring" className={styles.panelLink}>{t('dashboard.view_monitoring')} →</Link>
        </div>

        <div className={styles.panelCard}>
          <div className={styles.panelHeader}>
            <div>
              <h2 className={styles.panelTitle}>{t('dashboard.model_activity')}</h2>
              <p className={styles.panelSubtitle}>{t('dashboard.top_models')}</p>
            </div>
            <IconBot size={18} className={styles.panelIcon} />
          </div>
          {topModels.length > 0 ? (
            <div className={styles.modelList}>
              {topModels.map((model) => {
                const requests = readMetricNumber(model.requests);
                return (
                  <div className={styles.modelRow} key={model.model}>
                    <div className={styles.modelRowMeta}>
                      <span className={styles.modelName} title={model.model}>{model.model}</span>
                      <span className={styles.modelRequests}>{formatCompactNumber(requests, i18n.language)}</span>
                    </div>
                    <div className={styles.modelTrack}>
                      <span className={styles.modelFill} style={{ width: `${(requests / maxModelRequests) * 100}%` }} />
                    </div>
                  </div>
                );
              })}
            </div>
          ) : (
            <div className={styles.panelEmpty}>{t('dashboard.model_activity_empty')}</div>
          )}
          <Link to="/monitoring" className={styles.panelLink}>{t('dashboard.view_monitoring')} →</Link>
        </div>

        <div className={`${styles.panelCard} ${styles.healthPanel}`}>
          <div className={styles.panelHeader}>
            <div>
              <h2 className={styles.panelTitle}>{t('dashboard.operational_health')}</h2>
              <p className={styles.panelSubtitle}>{t('dashboard.service_status')}</p>
            </div>
            <span className={`${styles.healthSummaryDot} ${usageServiceTone}`} />
          </div>
          <div className={styles.healthList}>
            <div className={styles.healthRow}>
              <span>{t('dashboard.cpa_connection')}</span>
              <span className={`${styles.statusBadge} ${connectionStatus === 'connected' ? styles.healthGood : styles.healthBad}`}>
                <span className={styles.statusDotSmall} />
                {connectionStatus === 'connected' ? t('common.connected') : t('common.disconnected')}
              </span>
            </div>
            <div className={styles.healthRow}>
              <span>{t('dashboard.usage_service')}</span>
              <span className={`${styles.statusBadge} ${usageServiceTone}`}>
                <span className={styles.statusDotSmall} />
                {t(usageServiceStateKey)}
              </span>
            </div>
            <div className={styles.healthRow}>
              <span>{t('dashboard.collector')}</span>
              <span className={`${styles.statusBadge} ${collectorTone}`}>
                <span className={styles.statusDotSmall} />
                {t(collectorStateKey)}
              </span>
            </div>
            <div className={styles.healthRow}>
              <span>{t('dashboard.pending_events')}</span>
              <strong>{collector?.pendingItems ?? '—'}</strong>
            </div>
            <div className={styles.healthRow}>
              <span>{t('dashboard.last_inserted')}</span>
              <strong>{formatRelativeTime(collector?.lastInsertedAt ?? null, i18n.language)}</strong>
            </div>
          </div>
          {collector?.lastError && <p className={styles.healthError}>{collector.lastError}</p>}
          <Link to="/logs" className={styles.panelLink}>{t('dashboard.view_logs')} →</Link>
        </div>
      </section>

      {/* Bento stats grid */}
      <section className={styles.statsSection}>
        <h2 className={styles.sectionHeading}>{t('dashboard.system_overview')}</h2>
        <div className={styles.bentoGrid}>
          {quickStats.map((stat, index) => (
            <Link
              key={stat.path}
              to={stat.path}
              className={`${styles.bentoCard} ${index === 0 ? styles.bentoLarge : ''}`}
              style={{ animationDelay: `${index * 80}ms` }}
            >
              <div className={styles.bentoIcon}>{stat.icon}</div>
              <div className={styles.bentoContent}>
                <span className={styles.bentoValue}>
                  {stat.loading ? '...' : stat.value}
                </span>
                <span className={styles.bentoLabel}>{stat.label}</span>
                {stat.sublabel && !stat.loading && (
                  <span className={styles.bentoSublabel}>{stat.sublabel}</span>
                )}
              </div>
            </Link>
          ))}
        </div>
      </section>

      {/* Config pills section */}
      {config && (
        <section className={styles.configSection}>
          <h2 className={styles.sectionHeading}>{t('dashboard.current_config')}</h2>
          <div className={styles.configPillGrid}>
            <div className={styles.configPill}>
              <span className={styles.configPillLabel}>{t('basic_settings.debug_enable')}</span>
              <span className={`${styles.configPillValue} ${config.debug ? styles.on : styles.off}`}>
                {config.debug ? t('common.yes') : t('common.no')}
              </span>
            </div>
            <div className={styles.configPill}>
              <span className={styles.configPillLabel}>{t('basic_settings.logging_to_file_enable')}</span>
              <span className={`${styles.configPillValue} ${config.loggingToFile ? styles.on : styles.off}`}>
                {config.loggingToFile ? t('common.yes') : t('common.no')}
              </span>
            </div>
            <div className={styles.configPill}>
              <span className={styles.configPillLabel}>{t('basic_settings.retry_count_label')}</span>
              <span className={styles.configPillValue}>{config.requestRetry ?? 0}</span>
            </div>
            <div className={styles.configPill}>
              <span className={styles.configPillLabel}>{t('basic_settings.ws_auth_enable')}</span>
              <span className={`${styles.configPillValue} ${config.wsAuth ? styles.on : styles.off}`}>
                {config.wsAuth ? t('common.yes') : t('common.no')}
              </span>
            </div>
            <div className={styles.configPill}>
              <span className={styles.configPillLabel}>{t('dashboard.routing_strategy')}</span>
              <span className={`${styles.configBadge} ${routingStrategyBadgeClass}`}>
                {routingStrategyDisplay}
              </span>
            </div>
            {config.proxyUrl && (
              <div className={`${styles.configPill} ${styles.configPillWide}`}>
                <span className={styles.configPillLabel}>{t('basic_settings.proxy_url_label')}</span>
                <span className={styles.configPillMono}>{config.proxyUrl}</span>
              </div>
            )}
          </div>
          <Link to="/config" className={styles.viewMoreLink}>
            {t('dashboard.edit_settings')} →
          </Link>
        </section>
      )}
    </div>
  );
}
