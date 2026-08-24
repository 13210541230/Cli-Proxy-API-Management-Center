import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { Input } from '@/components/ui/Input';
import { Select } from '@/components/ui/Select';
import { useNotificationStore } from '@/stores';
import type { NotificationType } from '@/types';
import { quotaLimitsApi } from '@/services/api/quotaLimits';
import { quotaPauseApi, type DowngradeEntry } from '@/services/api/quotaPause';
import { enterpriseKeysApi } from '@/services/api/enterpriseKeys';
import type { EnterpriseKeyBinding } from '@/types/enterpriseKey';
import { quotaKeyHash } from '@/utils/apiKeyHash';
import styles from './QuotaDowngradePage.module.scss';

const DEFAULT_FALLBACK_MODEL = 'gpt-5.6-luna';

const formatTime = (value: string): string => (value ? new Date(value).toLocaleString() : '-');

const buildBindingLabel = (userName: string, email?: string): string =>
  email ? `${userName} (${email})` : userName;

export function QuotaDowngradePage() {
  const { t } = useTranslation();
  const { showNotification } = useNotificationStore();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [exceededAction, setExceededAction] = useState<'pause' | 'downgrade'>('pause');
  const [fallbackModel, setFallbackModel] = useState(DEFAULT_FALLBACK_MODEL);
  const [downgradedEntries, setDowngradedEntries] = useState<DowngradeEntry[]>([]);
  const [bindings, setBindings] = useState<Map<string, EnterpriseKeyBinding>>(new Map());
  const [actionLoading, setActionLoading] = useState(false);

  const loadData = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const [quotaConfig, bindingResponse, downgradeResponse] = await Promise.all([
        quotaLimitsApi.getConfig(),
        enterpriseKeysApi.listKeyBindings(),
        quotaPauseApi.listDowngraded(),
      ]);
      setExceededAction(quotaConfig.exceeded_action || 'pause');
      setFallbackModel(quotaConfig.fallback_model || DEFAULT_FALLBACK_MODEL);

      const nextBindings = new Map<string, EnterpriseKeyBinding>();
      for (const binding of bindingResponse.items ?? []) {
        if (binding.apiKey) {
          nextBindings.set(quotaKeyHash(binding.apiKey).toLowerCase(), binding);
        }
      }
      setBindings(nextBindings);
      setDowngradedEntries(downgradeResponse.entries ?? []);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : t('quota_downgrade.load_failed'));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  const resolveUser = useCallback(
    (keyHash: string): string => {
      const normalized = keyHash.toLowerCase();
      const exact = bindings.get(normalized);
      if (exact) return buildBindingLabel(exact.userName, exact.email);
      const prefixMatch = Array.from(bindings.entries()).find(([hash]) => hash.startsWith(normalized));
      return prefixMatch
        ? buildBindingLabel(prefixMatch[1].userName, prefixMatch[1].email)
        : keyHash;
    },
    [bindings]
  );

  const sortedDowngradedEntries = useMemo(
    () =>
      [...downgradedEntries].sort(
        (left, right) => new Date(right.downgraded_at).getTime() - new Date(left.downgraded_at).getTime()
      ),
    [downgradedEntries]
  );

  const handleSave = async () => {
    const model = fallbackModel.trim();
    if (exceededAction === 'downgrade' && !model) {
      showNotification(t('quota_downgrade.fallback_model_required'), 'error' as NotificationType);
      return;
    }

    setSaving(true);
    try {
      await quotaLimitsApi.updateConfig({
        exceeded_action: exceededAction,
        fallback_model: model || DEFAULT_FALLBACK_MODEL,
      });
      showNotification(t('quota_downgrade.save_success'), 'success' as NotificationType);
    } catch (err: unknown) {
      showNotification(err instanceof Error ? err.message : t('quota_downgrade.save_failed'), 'error' as NotificationType);
    } finally {
      setSaving(false);
    }
  };

  const handleResume = async (keyHash: string) => {
    setActionLoading(true);
    try {
      await quotaPauseApi.resumeDowngradeKey(keyHash);
      setDowngradedEntries((current) => current.filter((entry) => entry.key_hash !== keyHash));
      showNotification(t('quota_downgrade.restore_success'), 'success' as NotificationType);
    } catch (err: unknown) {
      showNotification(err instanceof Error ? err.message : t('quota_downgrade.restore_failed'), 'error' as NotificationType);
    } finally {
      setActionLoading(false);
    }
  };

  if (loading) return <div className={styles.loading}>{t('common.loading')}</div>;
  if (error) return <div className={styles.error}>{error}</div>;

  return (
    <div className={styles.container}>
      <div className={styles.header}>
        <h1>{t('quota_downgrade.title')}</h1>
        <Button onClick={() => void handleSave()} disabled={saving}>
          {saving ? t('common.saving') : t('common.save')}
        </Button>
      </div>

      <Card className={styles.section}>
        <h2>{t('quota_downgrade.policy_title')}</h2>
        <div className={styles.fieldRow}>
          <label>{t('quota_downgrade.exceeded_action')}</label>
          <Select
            value={exceededAction}
            onChange={(value) => setExceededAction(value as 'pause' | 'downgrade')}
            options={[
              { value: 'pause', label: t('quota_downgrade.action_pause') },
              { value: 'downgrade', label: t('quota_downgrade.action_downgrade') },
            ]}
          />
        </div>
        {exceededAction === 'downgrade' && (
          <div className={styles.fieldRow}>
            <label>{t('quota_downgrade.fallback_model')}</label>
            <Input
              value={fallbackModel}
              onChange={(event) => setFallbackModel(event.target.value)}
              placeholder={t('quota_downgrade.fallback_model_placeholder')}
            />
          </div>
        )}
      </Card>

      <Card className={styles.section}>
        <div className={styles.sectionHeader}>
          <h2>{t('quota_downgrade.active_title')}</h2>
          <Button size="sm" variant="secondary" onClick={() => void loadData()} disabled={actionLoading}>
            {t('common.refresh')}
          </Button>
        </div>
        {sortedDowngradedEntries.length === 0 ? (
          <div className={styles.empty}>{t('quota_downgrade.no_active')}</div>
        ) : (
          <table className={styles.table}>
            <thead>
              <tr>
                <th>{t('quota_downgrade.user')}</th>
                <th>{t('quota_downgrade.model')}</th>
                <th>{t('quota_downgrade.reason')}</th>
                <th>{t('quota_downgrade.downgraded_at')}</th>
                <th>{t('quota_downgrade.restore_at')}</th>
                <th>{t('common.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {sortedDowngradedEntries.map((entry) => (
                <tr key={entry.key_hash}>
                  <td>
                    <div>{resolveUser(entry.key_hash)}</div>
                    <span className={styles.valueHash}>{entry.key_hash}</span>
                  </td>
                  <td>{entry.fallback_model}</td>
                  <td>{entry.reason || t('quota_downgrade.spend_limit_exceeded')}</td>
                  <td>{formatTime(entry.downgraded_at)}</td>
                  <td>{entry.expires_at ? formatTime(entry.expires_at) : t('quota_downgrade.permanent')}</td>
                  <td>
                    <Button
                      size="sm"
                      variant="secondary"
                      disabled={actionLoading}
                      onClick={() => void handleResume(entry.key_hash)}
                    >
                      {t('quota_downgrade.restore_original_model')}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>

    </div>
  );
}
