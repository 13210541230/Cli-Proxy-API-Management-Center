import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Modal } from '@/components/ui/Modal';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { IconGithub, IconBookOpen, IconExternalLink, IconCode } from '@/components/ui/icons';
import {
  useAuthStore,
  useConfigStore,
  useNotificationStore,
  useModelsStore,
  useThemeStore,
  useUsageServiceStore,
} from '@/stores';
import {
  configApi,
  usageServiceApi,
  versionApi,
  type ManagerRuntimeStatus,
  type ManagerUpdateManifest,
} from '@/services/api';
import { apiKeysApi } from '@/services/api/apiKeys';
import { classifyModels } from '@/utils/models';
import { STORAGE_KEY_AUTH } from '@/utils/constants';
import { INLINE_LOGO_JPEG } from '@/assets/logoInline';
import iconGemini from '@/assets/icons/gemini.svg';
import iconClaude from '@/assets/icons/claude.svg';
import iconOpenaiLight from '@/assets/icons/openai-light.svg';
import iconOpenaiDark from '@/assets/icons/openai-dark.svg';
import iconQwen from '@/assets/icons/qwen.svg';
import iconKimiLight from '@/assets/icons/kimi-light.svg';
import iconKimiDark from '@/assets/icons/kimi-dark.svg';
import iconGlm from '@/assets/icons/glm.svg';
import iconGrok from '@/assets/icons/grok.svg';
import iconDeepseek from '@/assets/icons/deepseek.svg';
import iconMinimax from '@/assets/icons/minimax.svg';
import styles from './SystemPage.module.scss';

const MODEL_CATEGORY_ICONS: Record<string, string | { light: string; dark: string }> = {
  gpt: { light: iconOpenaiLight, dark: iconOpenaiDark },
  claude: iconClaude,
  gemini: iconGemini,
  qwen: iconQwen,
  kimi: { light: iconKimiLight, dark: iconKimiDark },
  glm: iconGlm,
  grok: iconGrok,
  deepseek: iconDeepseek,
  minimax: iconMinimax,
};

const parseVersionSegments = (version?: string | null) => {
  if (!version) return null;
  const cleaned = version.trim().replace(/^v/i, '');
  if (!cleaned) return null;
  const parts = cleaned
    .split(/[^0-9]+/)
    .filter(Boolean)
    .map((segment) => Number.parseInt(segment, 10))
    .filter(Number.isFinite);
  return parts.length ? parts : null;
};

const compareVersions = (latest?: string | null, current?: string | null) => {
  const latestParts = parseVersionSegments(latest);
  const currentParts = parseVersionSegments(current);
  if (!latestParts || !currentParts) return null;
  const length = Math.max(latestParts.length, currentParts.length);
  for (let i = 0; i < length; i++) {
    const l = latestParts[i] || 0;
    const c = currentParts[i] || 0;
    if (l > c) return 1;
    if (l < c) return -1;
  }
  return 0;
};

export function SystemPage() {
  const { t, i18n } = useTranslation();
  const { showNotification, showConfirmation } = useNotificationStore();
  const resolvedTheme = useThemeStore((state) => state.resolvedTheme);
  const auth = useAuthStore();
  const config = useConfigStore((state) => state.config);
  const fetchConfig = useConfigStore((state) => state.fetchConfig);
  const clearCache = useConfigStore((state) => state.clearCache);
  const updateConfigValue = useConfigStore((state) => state.updateConfigValue);

  const models = useModelsStore((state) => state.models);
  const modelsLoading = useModelsStore((state) => state.loading);
  const modelsError = useModelsStore((state) => state.error);
  const fetchModelsFromStore = useModelsStore((state) => state.fetchModels);

  const [modelStatus, setModelStatus] = useState<{
    type: 'success' | 'warning' | 'error' | 'muted';
    message: string;
  }>();
  const [requestLogModalOpen, setRequestLogModalOpen] = useState(false);
  const [requestLogDraft, setRequestLogDraft] = useState(false);
  const [requestLogTouched, setRequestLogTouched] = useState(false);
  const [requestLogSaving, setRequestLogSaving] = useState(false);
  const [checkingVersion, setCheckingVersion] = useState(false);
  const [runtimeStatus, setRuntimeStatus] = useState<ManagerRuntimeStatus | null>(null);
  const [runtimeLoading, setRuntimeLoading] = useState(false);
  const [runtimeAction, setRuntimeAction] = useState<'start' | 'stop' | null>(null);
  const [latestUpdate, setLatestUpdate] = useState<ManagerUpdateManifest | null>(null);
  const [updateAvailable, setUpdateAvailable] = useState(false);
  const [updateChecking, setUpdateChecking] = useState(false);
  const [updateAction, setUpdateAction] = useState<'download' | null>(null);

  const usageServiceEnabled = useUsageServiceStore((state) => state.enabled);
  const usageServiceBase = useUsageServiceStore((state) => state.serviceBase);
  const runtimeBase = usageServiceEnabled && usageServiceBase ? usageServiceBase : auth.apiBase;

  const apiKeysCache = useRef<string[]>([]);
  const versionTapCount = useRef(0);
  const versionTapTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const otherLabel = useMemo(
    () => (i18n.language?.toLowerCase().startsWith('zh') ? '其他' : 'Other'),
    [i18n.language]
  );
  const groupedModels = useMemo(() => classifyModels(models, { otherLabel }), [models, otherLabel]);
  const requestLogEnabled = config?.requestLog ?? false;
  const requestLogDirty = requestLogDraft !== requestLogEnabled;
  const canEditRequestLog = auth.connectionStatus === 'connected' && Boolean(config);

  const appVersion = __APP_VERSION__ || t('system_info.version_unknown');
  const apiVersion = auth.serverVersion || t('system_info.version_unknown');
  const buildTime = auth.serverBuildDate
    ? new Date(auth.serverBuildDate).toLocaleString(i18n.language)
    : t('system_info.version_unknown');

  const getIconForCategory = (categoryId: string): string | null => {
    const iconEntry = MODEL_CATEGORY_ICONS[categoryId];
    if (!iconEntry) return null;
    if (typeof iconEntry === 'string') return iconEntry;
    return resolvedTheme === 'dark' ? iconEntry.dark : iconEntry.light;
  };

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
    } catch (err) {
      console.warn('Auto loading API keys for models failed:', err);
      return [];
    }
  }, [config?.apiKeys]);

  const fetchModels = async ({ forceRefresh = false }: { forceRefresh?: boolean } = {}) => {
    if (auth.connectionStatus !== 'connected') {
      setModelStatus({
        type: 'warning',
        message: t('notification.connection_required'),
      });
      return;
    }

    if (!auth.apiBase) {
      showNotification(t('notification.connection_required'), 'warning');
      return;
    }

    if (forceRefresh) {
      apiKeysCache.current = [];
    }

    setModelStatus({ type: 'muted', message: t('system_info.models_loading') });
    try {
      const apiKeys = await resolveApiKeysForModels();
      const primaryKey = apiKeys[0];
      const list = await fetchModelsFromStore(auth.apiBase, primaryKey, forceRefresh);
      const hasModels = list.length > 0;
      setModelStatus({
        type: hasModels ? 'success' : 'warning',
        message: hasModels
          ? t('system_info.models_count', { count: list.length })
          : t('system_info.models_empty'),
      });
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : typeof err === 'string' ? err : '';
      const suffix = message ? `: ${message}` : '';
      const text = `${t('system_info.models_error')}${suffix}`;
      setModelStatus({ type: 'error', message: text });
    }
  };

  const handleClearLoginStorage = () => {
    showConfirmation({
      title: t('system_info.clear_login_title', { defaultValue: 'Clear Login Storage' }),
      message: t('system_info.clear_login_confirm'),
      variant: 'danger',
      confirmText: t('common.confirm'),
      onConfirm: () => {
        auth.logout();
        if (typeof localStorage === 'undefined') return;
        const keysToRemove = [STORAGE_KEY_AUTH, 'isLoggedIn', 'apiBase', 'apiUrl', 'managementKey'];
        keysToRemove.forEach((key) => localStorage.removeItem(key));
        showNotification(t('notification.login_storage_cleared'), 'success');
      },
    });
  };

  const openRequestLogModal = useCallback(() => {
    setRequestLogTouched(false);
    setRequestLogDraft(requestLogEnabled);
    setRequestLogModalOpen(true);
  }, [requestLogEnabled]);

  const handleInfoVersionTap = useCallback(() => {
    versionTapCount.current += 1;
    if (versionTapTimer.current) {
      clearTimeout(versionTapTimer.current);
    }

    if (versionTapCount.current >= 7) {
      versionTapCount.current = 0;
      versionTapTimer.current = null;
      openRequestLogModal();
      return;
    }

    versionTapTimer.current = setTimeout(() => {
      versionTapCount.current = 0;
      versionTapTimer.current = null;
    }, 1500);
  }, [openRequestLogModal]);

  const handleRequestLogClose = useCallback(() => {
    setRequestLogModalOpen(false);
    setRequestLogTouched(false);
  }, []);

  const handleRequestLogSave = async () => {
    if (!canEditRequestLog) return;
    if (!requestLogDirty) {
      setRequestLogModalOpen(false);
      return;
    }

    const previous = requestLogEnabled;
    setRequestLogSaving(true);
    updateConfigValue('request-log', requestLogDraft);

    try {
      await configApi.updateRequestLog(requestLogDraft);
      clearCache('request-log');
      showNotification(t('notification.request_log_updated'), 'success');
      setRequestLogModalOpen(false);
    } catch (error: unknown) {
      const message =
        error instanceof Error ? error.message : typeof error === 'string' ? error : '';
      updateConfigValue('request-log', previous);
      showNotification(
        `${t('notification.update_failed')}${message ? `: ${message}` : ''}`,
        'error'
      );
    } finally {
      setRequestLogSaving(false);
    }
  };

  const refreshRuntimeStatus = useCallback(async () => {
    if (!runtimeBase) {
      setRuntimeStatus(null);
      return;
    }
    setRuntimeLoading(true);
    try {
      const status = await usageServiceApi.getRuntimeStatus(runtimeBase, auth.managementKey);
      setRuntimeStatus(status);
    } catch {
      setRuntimeStatus(null);
    } finally {
      setRuntimeLoading(false);
    }
  }, [auth.managementKey, runtimeBase]);

  const handleRuntimeAction = useCallback(
    async (action: 'start' | 'stop') => {
      if (!runtimeBase) return;
      setRuntimeAction(action);
      try {
        const status =
          action === 'start'
            ? await usageServiceApi.startRuntime(runtimeBase, auth.managementKey)
            : await usageServiceApi.stopRuntime(runtimeBase, auth.managementKey);
        setRuntimeStatus(status);
        showNotification(
          action === 'start'
            ? t('system_info.local_runtime_started')
            : t('system_info.local_runtime_stopped'),
          'success'
        );
      } catch (error: unknown) {
        const message = error instanceof Error ? error.message : typeof error === 'string' ? error : '';
        showNotification(
          `${t('system_info.local_runtime_action_failed')}${message ? `: ${message}` : ''}`,
          'error'
        );
        await refreshRuntimeStatus();
      } finally {
        setRuntimeAction(null);
      }
    },
    [auth.managementKey, refreshRuntimeStatus, runtimeBase, showNotification, t]
  );

  useEffect(() => {
    if (auth.connectionStatus !== 'connected' || !runtimeBase) {
      setRuntimeStatus(null);
      return;
    }
    void refreshRuntimeStatus();
  }, [auth.connectionStatus, refreshRuntimeStatus, runtimeBase]);

  const handleUpdateCheck = useCallback(async () => {
    if (!runtimeBase) {
      showNotification(t('system_info.manager_version_check_error'), 'error');
      return;
    }
    setUpdateChecking(true);
    try {
      const manifest = await usageServiceApi.getLatestUpdate(runtimeBase, auth.managementKey);
      setLatestUpdate(manifest);
      const managerComparison = compareVersions(manifest.managerVersion, __APP_VERSION__);
      const cpaComparison = compareVersions(manifest.cpaVersion, auth.serverVersion);
      const available = managerComparison === 1 || cpaComparison === 1;
      setUpdateAvailable(available);
      if (available) {
        showNotification(
          t('system_info.suite_update_available', {
            cpaVersion: manifest.cpaVersion,
            managerVersion: manifest.managerVersion,
          }),
          'warning'
        );
      } else {
        showNotification(t('system_info.suite_is_latest'), 'success');
      }
    } catch (error: unknown) {
      setLatestUpdate(null);
      setUpdateAvailable(false);
      const message = error instanceof Error ? error.message : typeof error === 'string' ? error : '';
      showNotification(
        `${t('system_info.manager_version_check_error')}${message ? `: ${message}` : ''}`,
        'error'
      );
    } finally {
      setUpdateChecking(false);
    }
  }, [auth.managementKey, auth.serverVersion, runtimeBase, showNotification, t]);

    const handleUpdateNow = useCallback(async () => {
    if (!runtimeBase || !updateAvailable) return;
    setUpdateAction('download');
    try {
      const result = await usageServiceApi.downloadUpdate(runtimeBase, auth.managementKey);
      if (result.manifest) {
        setLatestUpdate(result.manifest);
      }
      setUpdateAvailable(false);
      const filePath = result.filePath || '';
      showNotification(
        filePath
          ? `${t('system_info.suite_update_download_success')} ${filePath}`
          : t('system_info.suite_update_download_success'),
        'success',
        8000
      );
      showNotification(t('system_info.suite_update_manual_hint'), 'info', 15000);
    } catch (error: unknown) {
      const message = error instanceof Error ? error.message : typeof error === 'string' ? error : '';
      showNotification(
        `${t('system_info.suite_update_failed')}${message ? `: ${message}` : ''}`,
        'error'
      );
    } finally {
      setUpdateAction(null);
    }
  }, [auth.managementKey, latestUpdate, runtimeBase, showNotification, t, updateAvailable]);

  const confirmSuiteUpdate = useCallback(() => {
    if (!latestUpdate || !updateAvailable) return;
    showConfirmation({
      title: t('system_info.suite_update_confirm_title', { defaultValue: 'Update CPA suite' }),
      message: t('system_info.suite_update_confirm_message', {
        defaultValue:
          'The CPA {{cpaVersion}} / CPA-Manager {{managerVersion}} package will be downloaded to the current program directory. After the download finishes, stop the services and replace the files manually.',
        cpaVersion: latestUpdate.cpaVersion,
        managerVersion: latestUpdate.managerVersion,
      }),
      variant: 'danger',
      confirmText: t('system_info.suite_update_now'),
      onConfirm: () => void handleUpdateNow(),
    });
  }, [handleUpdateNow, latestUpdate, showConfirmation, t, updateAvailable]);

  const handleVersionCheck = useCallback(async () => {
    setCheckingVersion(true);
    try {
      const data = await versionApi.checkLatest();
      const latestRaw = data?.['latest-version'] ?? data?.latest_version ?? data?.latest ?? '';
      const latest = typeof latestRaw === 'string' ? latestRaw : String(latestRaw ?? '');
      const comparison = compareVersions(latest, auth.serverVersion);

      if (!latest) {
        showNotification(t('system_info.version_check_error'), 'error');
        return;
      }

      if (comparison === null) {
        showNotification(t('system_info.version_current_missing'), 'warning');
        return;
      }

      if (comparison > 0) {
        showNotification(t('system_info.version_update_available', { version: latest }), 'warning');
      } else {
        showNotification(t('system_info.version_is_latest'), 'success');
      }
    } catch (error: unknown) {
      const message =
        error instanceof Error ? error.message : typeof error === 'string' ? error : '';
      const suffix = message ? `: ${message}` : '';
      showNotification(`${t('system_info.version_check_error')}${suffix}`, 'error');
    } finally {
      setCheckingVersion(false);
    }
  }, [auth.serverVersion, showNotification, t]);

  useEffect(() => {
    fetchConfig().catch(() => {
      // ignore
    });
  }, [fetchConfig]);

  useEffect(() => {
    if (requestLogModalOpen && !requestLogTouched) {
      setRequestLogDraft(requestLogEnabled);
    }
  }, [requestLogModalOpen, requestLogTouched, requestLogEnabled]);

  useEffect(() => {
    return () => {
      if (versionTapTimer.current) {
        clearTimeout(versionTapTimer.current);
      }
    };
  }, []);

  useEffect(() => {
    fetchModels();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [auth.connectionStatus, auth.apiBase]);

  return (
    <div className={styles.container}>
      <header className={styles.pageHeader}>
        <div>
          <span className={styles.pageEyebrow}>{t('system_info.workspace_label', { defaultValue: 'Operations workspace' })}</span>
          <h1 className={styles.pageTitle}>{t('system_info.title')}</h1>
          <p className={styles.pageDescription}>{t('system_info.workspace_desc', { defaultValue: 'Review runtime versions, model availability, links, and local management controls.' })}</p>
        </div>
        <span className={`${styles.connectionBadge} ${auth.connectionStatus === 'connected' ? styles.connectionGood : styles.connectionBad}`}>
          <span className={styles.connectionDot} />
          {auth.connectionStatus === 'connected' ? t('common.connected') : t('common.disconnected')}
        </span>
      </header>
      <div className={styles.content}>
        <Card className={styles.aboutCard}>
          <div className={styles.aboutHeader}>
            <img src={INLINE_LOGO_JPEG} alt="CPAMC" className={styles.aboutLogo} />
            <div className={styles.aboutTitle}>{t('system_info.about_title')}</div>
          </div>

          <div className={styles.aboutInfoGrid}>
            <div
              className={`${styles.infoTile} ${styles.tapTile}`}
              role="button"
              tabIndex={0}
              onClick={handleInfoVersionTap}
              onKeyDown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') {
                  event.preventDefault();
                  handleInfoVersionTap();
                }
              }}
            >
              <div className={styles.tileHeader}>
                <div className={styles.tileLabel}>{t('footer.version')}</div>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className={styles.tileAction}
                  onClick={(event) => {
                    event.stopPropagation();
                    void handleUpdateCheck();
                  }}
                  onKeyDown={(event) => event.stopPropagation()}
                  loading={updateChecking}
                  title={t('system_info.version_check_button')}
                  aria-label={t('system_info.version_check_button')}
                >
                  {t('system_info.version_check_button')}
                </Button>
              </div>
              <div className={styles.tileValue}>{appVersion}</div>
            </div>

            <div className={styles.infoTile}>
              <div className={styles.tileHeader}>
                <div className={styles.tileLabel}>{t('footer.api_version')}</div>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className={styles.tileAction}
                  onClick={() => void handleVersionCheck()}
                  loading={checkingVersion}
                  title={t('system_info.version_check_button')}
                  aria-label={t('system_info.version_check_button')}
                >
                  {t('system_info.version_check_button')}
                </Button>
              </div>
              <div className={styles.tileValue}>{apiVersion}</div>
            </div>

            <div className={styles.infoTile}>
              <div className={styles.tileLabel}>{t('footer.build_date')}</div>
              <div className={styles.tileValue}>{buildTime}</div>
            </div>

            <div className={styles.infoTile}>
              <div className={styles.tileLabel}>{t('connection.status')}</div>
              <div className={styles.tileValue}>{t(`common.${auth.connectionStatus}_status`)}</div>
              <div className={styles.tileSub}>{auth.apiBase || '-'}</div>
            </div>
          </div>
        </Card>

        {(runtimeLoading || runtimeStatus || latestUpdate || updateChecking) && (
          <Card
            title={t('system_info.local_runtime_title')}
            extra={
              <Button
                variant="secondary"
                size="sm"
                onClick={() => void refreshRuntimeStatus()}
                loading={runtimeLoading}
              >
                {t('common.refresh')}
              </Button>
            }
          >
            <p className={styles.sectionDescription}>
              {t('system_info.local_runtime_hint')}
            </p>
            {runtimeStatus && (
              <div className={styles.runtimePanel}>
                <div className={styles.runtimeDetails}>
                  <div>
                    <span>{t('system_info.local_runtime_state')}</span>
                    <strong>{t(`system_info.local_runtime_state_${runtimeStatus.state}`, { defaultValue: runtimeStatus.state })}</strong>
                  </div>
                  <div>
                    <span>{t('system_info.local_runtime_path')}</span>
                    <strong>{runtimeStatus.executablePath || t('system_info.version_unknown')}</strong>
                  </div>
                  {runtimeStatus.pid ? (
                    <div>
                      <span>{t('system_info.local_runtime_pid')}</span>
                      <strong>{runtimeStatus.pid}</strong>
                    </div>
                  ) : null}
                  <div>
                    <span>{t('system_info.local_runtime_health')}</span>
                    <strong>
                      {t(`system_info.local_runtime_health_${runtimeStatus.health || 'unknown'}`, {
                        defaultValue: runtimeStatus.health || t('system_info.local_runtime_health_unknown'),
                      })}
                    </strong>
                  </div>
                  {runtimeStatus.lastError ? (
                    <div className={styles.runtimeError}>
                      <span>{t('system_info.local_runtime_error')}</span>
                      <strong>{runtimeStatus.lastError}</strong>
                    </div>
                  ) : null}
                </div>
                {latestUpdate && (
                  <div className={styles.runtimeUpdateSummary}>
                    <span>{t('system_info.suite_latest_release')}</span>
                    <strong>
                      {latestUpdate.cpaVersion} / {latestUpdate.managerVersion}
                    </strong>
                  </div>
                )}
                <div className={styles.runtimeActions}>
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => void handleUpdateCheck()}
                    loading={updateChecking}
                  >
                    {t('system_info.suite_check_update')}
                  </Button>
                  <Button
                    size="sm"
                    onClick={confirmSuiteUpdate}
                    loading={updateAction !== null}
                    disabled={
                      !updateAvailable ||
                      !runtimeStatus.enabled ||
                      runtimeStatus.external === true ||
                      runtimeStatus.state === 'failed'
                    }
                  >
                    {t('system_info.suite_update_now')}
                  </Button>
                  <Button
                    size="sm"
                    onClick={() => void handleRuntimeAction('start')}
                    loading={runtimeAction === 'start'}
                    disabled={runtimeStatus.running || !runtimeStatus.enabled}
                  >
                    {t('system_info.local_runtime_start')}
                  </Button>
                  <Button
                    variant="danger"
                    size="sm"
                    onClick={() => void handleRuntimeAction('stop')}
                    loading={runtimeAction === 'stop'}
                    disabled={!runtimeStatus.running || runtimeStatus.external === true}
                  >
                    {t('system_info.local_runtime_stop')}
                  </Button>
                </div>
              </div>
            )}
          </Card>
        )}

        <Card title={t('system_info.quick_links_title')}>
          <p className={styles.sectionDescription}>{t('system_info.quick_links_desc')}</p>
          <div className={styles.quickLinks}>
            <a
              href="https://github.com/13210541230/CLIProxyAPI"
              target="_blank"
              rel="noopener noreferrer"
              className={styles.linkCard}
            >
              <div className={`${styles.linkIcon} ${styles.github}`}>
                <IconGithub size={22} />
              </div>
              <div className={styles.linkContent}>
                <div className={styles.linkTitle}>
                  {t('system_info.link_main_repo')}
                  <IconExternalLink size={14} />
                </div>
                <div className={styles.linkDesc}>{t('system_info.link_main_repo_desc')}</div>
              </div>
            </a>

            <a
              href="https://github.com/13210541230/Cli-Proxy-API-Management-Center"
              target="_blank"
              rel="noopener noreferrer"
              className={styles.linkCard}
            >
              <div className={`${styles.linkIcon} ${styles.github}`}>
                <IconCode size={22} />
              </div>
              <div className={styles.linkContent}>
                <div className={styles.linkTitle}>
                  {t('system_info.link_webui_repo')}
                  <IconExternalLink size={14} />
                </div>
                <div className={styles.linkDesc}>{t('system_info.link_webui_repo_desc')}</div>
              </div>
            </a>

            <a
              href="https://help.router-for.me/"
              target="_blank"
              rel="noopener noreferrer"
              className={styles.linkCard}
            >
              <div className={`${styles.linkIcon} ${styles.docs}`}>
                <IconBookOpen size={22} />
              </div>
              <div className={styles.linkContent}>
                <div className={styles.linkTitle}>
                  {t('system_info.link_docs')}
                  <IconExternalLink size={14} />
                </div>
                <div className={styles.linkDesc}>{t('system_info.link_docs_desc')}</div>
              </div>
            </a>
          </div>
        </Card>

        <Card
          title={t('system_info.models_title')}
          extra={
            <Button
              variant="secondary"
              size="sm"
              onClick={() => fetchModels({ forceRefresh: true })}
              loading={modelsLoading}
            >
              {t('common.refresh')}
            </Button>
          }
        >
          <p className={styles.sectionDescription}>{t('system_info.models_desc')}</p>
          {modelStatus && (
            <div className={`status-badge ${modelStatus.type}`}>{modelStatus.message}</div>
          )}
          {modelsError && <div className="error-box">{modelsError}</div>}
          {modelsLoading ? (
            <div className="hint">{t('common.loading')}</div>
          ) : models.length === 0 ? (
            <div className="hint">{t('system_info.models_empty')}</div>
          ) : (
            <div className="item-list">
              {groupedModels.map((group) => {
                const iconSrc = getIconForCategory(group.id);
                return (
                  <div key={group.id} className="item-row">
                    <div className="item-meta">
                      <div className={styles.groupTitle}>
                        {iconSrc && <img src={iconSrc} alt="" className={styles.groupIcon} />}
                        <span className="item-title">{group.label}</span>
                      </div>
                      <div className="item-subtitle">
                        {t('system_info.models_count', { count: group.items.length })}
                      </div>
                    </div>
                    <div className={styles.modelTags}>
                      {group.items.map((model) => (
                        <span
                          key={`${model.name}-${model.alias ?? 'default'}`}
                          className={styles.modelTag}
                          title={model.description || ''}
                        >
                          <span className={styles.modelName}>{model.name}</span>
                          {model.alias && <span className={styles.modelAlias}>{model.alias}</span>}
                        </span>
                      ))}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </Card>

        <Card title={t('system_info.clear_login_title')}>
          <p className={styles.sectionDescription}>{t('system_info.clear_login_desc')}</p>
          <div className={styles.clearLoginActions}>
            <Button variant="danger" onClick={handleClearLoginStorage}>
              {t('system_info.clear_login_button')}
            </Button>
          </div>
        </Card>
      </div>

      <Modal
        open={requestLogModalOpen}
        onClose={handleRequestLogClose}
        title={t('basic_settings.request_log_title')}
        footer={
          <>
            <Button variant="secondary" onClick={handleRequestLogClose} disabled={requestLogSaving}>
              {t('common.cancel')}
            </Button>
            <Button
              onClick={handleRequestLogSave}
              loading={requestLogSaving}
              disabled={!canEditRequestLog || !requestLogDirty}
            >
              {t('common.save')}
            </Button>
          </>
        }
      >
        <div className="request-log-modal">
          <div className="status-badge warning">{t('basic_settings.request_log_warning')}</div>
          <ToggleSwitch
            label={t('basic_settings.request_log_enable')}
            labelPosition="left"
            checked={requestLogDraft}
            disabled={!canEditRequestLog || requestLogSaving}
            onChange={(value) => {
              setRequestLogDraft(value);
              setRequestLogTouched(true);
            }}
          />
        </div>
      </Modal>
    </div>
  );
}
