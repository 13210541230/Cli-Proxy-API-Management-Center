import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Select } from '@/components/ui/Select';
import { useNotificationStore } from '@/stores';
import type { NotificationType } from '@/types';
import { quotaLimitsApi, type QuotaConfig, type SpendLimitEntry } from '@/services/api/quotaLimits';
import styles from './QuotaLimitsPage.module.scss';

export function QuotaLimitsPage() {
  const { t } = useTranslation();
  const { showNotification } = useNotificationStore();

  const [_config, setConfig] = useState<QuotaConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  // Editable fields
  const [enabled, setEnabled] = useState(false);
  const [dailyCents, setDailyCents] = useState('15000');
  const [weeklyCents, setWeeklyCents] = useState('50000');
  const [overrides, setOverrides] = useState<SpendLimitEntry[]>([]);
  const [overrideModalOpen, setOverrideModalOpen] = useState(false);
  const [editingOverride, setEditingOverride] = useState<SpendLimitEntry | null>(null);

  const loadConfig = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const data = await quotaLimitsApi.getConfig();
      setConfig(data);
      setEnabled(data.enabled);
      setDailyCents(String(data.default.daily_cents));
      setWeeklyCents(String(data.default.weekly_cents));
      setOverrides(data.overrides ?? []);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to load config');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadConfig();
  }, [loadConfig]);

  const handleSave = async () => {
    setSaving(true);
    try {
      await quotaLimitsApi.updateConfig({
        enabled,
        default: { daily_cents: parseInt(dailyCents, 10) || 0, weekly_cents: parseInt(weeklyCents, 10) || 0 },
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
    if (!editingOverride) return;
    setOverrides((prev) => {
      const idx = prev.findIndex(
        (o) => o.apply_to === editingOverride.apply_to && o.apply_value === editingOverride.apply_value
      );
      if (idx >= 0) {
        const next = [...prev];
        next[idx] = editingOverride;
        return next;
      }
      return [...prev, editingOverride];
    });
    setOverrideModalOpen(false);
    setEditingOverride(null);
  };

  const deleteOverride = (entry: SpendLimitEntry) => {
    setOverrides((prev) => prev.filter((o) => o !== entry));
  };

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
              {overrides.map((o) => (
                <tr key={`${o.apply_to}-${o.apply_value}`}>
                  <td>{o.apply_to}</td>
                  <td>{o.apply_value}</td>
                  <td>{o.daily_cents}</td>
                  <td>{o.weekly_cents}</td>
                  <td>
					<Button size="sm" variant="secondary" onClick={() => deleteOverride(o)}>
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
            <label>{t('quota_limits.apply_to')}</label>
			<Select
				value={editingOverride.apply_to}
				onChange={(value) => setEditingOverride({ ...editingOverride, apply_to: value })}
				options={[
				  { value: 'global', label: t('quota_limits.apply_global') },
                { value: 'global', label: t('quota_limits.apply_global') },
                { value: 'api-key', label: t('quota_limits.apply_api_key') },
              ]}
            />
            <label>{t('quota_limits.apply_value')}</label>
            <Input
              value={editingOverride.apply_value}
              onChange={(e) => setEditingOverride({ ...editingOverride, apply_value: e.target.value })}
              placeholder={editingOverride.apply_to === 'api-key' ? t('quota_limits.key_hash_placeholder') : '-'}
            />
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
              <Button onClick={saveOverride}>{t('common.save')}</Button>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}
