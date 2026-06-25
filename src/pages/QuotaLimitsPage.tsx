import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Select } from '@/components/ui/Select';
import { useNotificationStore } from '@/stores';
import type { NotificationType } from '@/types';
import { quotaLimitsApi, type QuotaConfig, type SpendLimitEntry } from '@/services/api/quotaLimits';
import { enterpriseKeysApi } from '@/services/api/enterpriseKeys';
import styles from './QuotaLimitsPage.module.scss';

interface KeyDisplayOption {
  hash: string;
  label: string;
}

const buildBindingLabel = (userName: string, email?: string): string =>
  email ? `${userName} (${email})` : userName;

export function QuotaLimitsPage() {
  const { t } = useTranslation();
  const { showNotification } = useNotificationStore();

  const [_config, setConfig] = useState<QuotaConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const [enabled, setEnabled] = useState(false);
  const [dailyCents, setDailyCents] = useState('15000');
  const [weeklyCents, setWeeklyCents] = useState('50000');
  const [overrides, setOverrides] = useState<SpendLimitEntry[]>([]);
  const [overrideModalOpen, setOverrideModalOpen] = useState(false);
  const [editingOverride, setEditingOverride] = useState<SpendLimitEntry | null>(null);
  const [hashToDisplay, setHashToDisplay] = useState<Record<string, string>>({});

  const loadConfig = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const data = await quotaLimitsApi.getConfig();
      setConfig(data);
      setEnabled(data.enabled);
      setDailyCents(String(data.default.daily_cents));
      setWeeklyCents(String(data.default.weekly_cents));
      setOverrides((data.overrides ?? []).filter((entry) => entry.apply_to === 'api-key'));
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to load config');
    } finally {
      setLoading(false);
    }
  }, []);

  const loadKeyBindings = useCallback(async () => {
    try {
      const data = await enterpriseKeysApi.listKeyBindings();
      const next: Record<string, string> = {};
      for (const binding of data.items ?? []) {
        if (binding.apiKeyHash && binding.userName) {
          next[binding.apiKeyHash] = buildBindingLabel(binding.userName, binding.email);
        }
      }
      setHashToDisplay(next);
    } catch {
      setHashToDisplay({});
    }
  }, []);

  useEffect(() => {
    void loadConfig();
    void loadKeyBindings();
  }, [loadConfig, loadKeyBindings]);

  const handleSave = async () => {
    setSaving(true);
    try {
      await quotaLimitsApi.updateConfig({
        enabled,
        default: {
          daily_cents: parseInt(dailyCents, 10) || 0,
          weekly_cents: parseInt(weeklyCents, 10) || 0,
        },
        overrides,
      });
      showNotification(t('quota_limits.save_success'), 'success' as NotificationType);
      void loadConfig();
    } catch (err: unknown) {
      showNotification(err instanceof Error ? err.message : 'Failed', 'error' as NotificationType);
    } finally {
      setSaving(false);
    }
  };

  const openNewOverride = () => {
    setEditingOverride({ apply_to: 'api-key', apply_value: '', daily_cents: 0, weekly_cents: 0 });
    setOverrideModalOpen(true);
  };

  const saveOverride = () => {
    if (!editingOverride || !editingOverride.apply_value) return;
    const entry = { ...editingOverride, apply_to: 'api-key' };
    setOverrides((prev) => {
      const idx = prev.findIndex((item) => item.apply_value === entry.apply_value);
      if (idx >= 0) {
        const next = [...prev];
        next[idx] = entry;
        return next;
      }
      return [...prev, entry];
    });
    setOverrideModalOpen(false);
    setEditingOverride(null);
  };

  const deleteOverride = (entry: SpendLimitEntry) => {
    setOverrides((prev) => prev.filter((item) => item !== entry));
  };

  const resolveDisplay = (entry: SpendLimitEntry): string =>
    hashToDisplay[entry.apply_value] || entry.apply_value;

  const keyOptions = useMemo<KeyDisplayOption[]>(
    () =>
      Object.entries(hashToDisplay).map(([hash, label]) => ({
        hash,
        label,
      })),
    [hashToDisplay]
  );

  if (loading) return <div className={styles.loading}>{t('common.loading')}</div>;
  if (error) return <div className={styles.error}>{error}</div>;

  return (
    <div className={styles.container}>
      <div className={styles.header}>
        <h1>{t('quota_limits.title')}</h1>
        <Button onClick={handleSave} disabled={saving}>
          {saving ? t('common.saving') : t('common.save')}
        </Button>
      </div>

      <Card className={styles.section}>
        <h2>{t('quota_limits.global_settings')}</h2>
        <div className={styles.fieldRow}>
          <label className={styles.toggle}>
            <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
            <span>{t('quota_limits.enabled')}</span>
          </label>
        </div>
        <div className={styles.fieldRow}>
          <label>{t('quota_limits.daily_cents')}</label>
          <Input type="number" value={dailyCents} onChange={(e) => setDailyCents(e.target.value)} />
        </div>
        <div className={styles.fieldRow}>
          <label>{t('quota_limits.weekly_cents')}</label>
          <Input type="number" value={weeklyCents} onChange={(e) => setWeeklyCents(e.target.value)} />
        </div>
      </Card>

      <Card className={styles.section}>
        <div className={styles.sectionHeader}>
          <h2>{t('quota_limits.overrides')}</h2>
          <Button size="sm" onClick={openNewOverride}>{t('quota_limits.add_override')}</Button>
        </div>
        {overrides.length === 0 ? (
          <div className={styles.empty}>{t('quota_limits.no_overrides')}</div>
        ) : (
          <table className={styles.table}>
            <thead>
              <tr>
                <th>{t('quota_limits.apply_to')}</th>
                <th>{t('quota_limits.apply_value')}</th>
                <th>{t('quota_limits.daily_cents')}</th>
                <th>{t('quota_limits.weekly_cents')}</th>
                <th>{t('common.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {overrides.map((entry) => (
                <tr key={entry.apply_value}>
                  <td>{t('quota_limits.apply_api_key')}</td>
                  <td className={styles.valueCell}>
                    {resolveDisplay(entry)}
                    <span className={styles.valueHash}>{entry.apply_value}</span>
                  </td>
                  <td>{entry.daily_cents}</td>
                  <td>{entry.weekly_cents}</td>
                  <td>
                    <Button size="sm" variant="secondary" onClick={() => deleteOverride(entry)}>
                      {t('common.delete')}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>

      <Modal
        open={overrideModalOpen}
        onClose={() => setOverrideModalOpen(false)}
        title={t('quota_limits.edit_override')}
      >
        {editingOverride && (
          <div className={styles.form}>
            <label>{t('quota_limits.apply_api_key')}</label>
            {keyOptions.length > 0 ? (
              <Select
                value={editingOverride.apply_value}
                onChange={(value) => setEditingOverride({ ...editingOverride, apply_value: value })}
                options={[
                  { value: '', label: t('quota_limits.select_user_placeholder') },
                  ...keyOptions.map((option) => ({ value: option.hash, label: option.label })),
                ]}
              />
            ) : (
              <div className={styles.empty}>{t('quota_limits.no_key_bindings')}</div>
            )}
            <label>{t('quota_limits.daily_cents')}</label>
            <Input
              type="number"
              value={String(editingOverride.daily_cents)}
              onChange={(e) => setEditingOverride({ ...editingOverride, daily_cents: parseInt(e.target.value, 10) || 0 })}
            />
            <label>{t('quota_limits.weekly_cents')}</label>
            <Input
              type="number"
              value={String(editingOverride.weekly_cents)}
              onChange={(e) => setEditingOverride({ ...editingOverride, weekly_cents: parseInt(e.target.value, 10) || 0 })}
            />
            <div className={styles.formActions}>
              <Button variant="secondary" onClick={() => setOverrideModalOpen(false)}>{t('common.cancel')}</Button>
              <Button onClick={saveOverride} disabled={!editingOverride.apply_value}>{t('common.save')}</Button>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}
