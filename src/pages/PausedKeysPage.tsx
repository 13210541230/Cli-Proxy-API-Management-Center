import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { useNotificationStore } from '@/stores';
import type { NotificationType } from '@/types';
import { quotaPauseApi, type PauseEntry } from '@/services/api/quotaPause';
import styles from './PausedKeysPage.module.scss';

export function PausedKeysPage() {
  const { t } = useTranslation();
  const { showNotification } = useNotificationStore();

  const [entries, setEntries] = useState<PauseEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  // Manual pause form
  const [pauseModalOpen, setPauseModalOpen] = useState(false);
  const [pauseKeyHash, setPauseKeyHash] = useState('');
  const [pauseReason, setPauseReason] = useState('');
  const [pauseDurationSec, setPauseDurationSec] = useState('3600');

  const fetchEntries = async () => {
    setLoading(true);
    setError('');
    try {
      const data = await quotaPauseApi.listPaused();
      setEntries(data.entries ?? []);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void fetchEntries();
  }, []);

  const handlePause = async () => {
    if (!pauseKeyHash.trim()) {
			showNotification(t('quota_pause.key_hash_required'), 'error' as NotificationType);
      return;
    }
    try {
      const secs = parseInt(pauseDurationSec, 10) || 0;
      await quotaPauseApi.pauseKey(pauseKeyHash.trim(), pauseReason.trim(), secs > 0 ? secs : undefined);
			showNotification(t('quota_pause.pause_success'), 'success' as NotificationType);
      setPauseModalOpen(false);
      setPauseKeyHash('');
      setPauseReason('');
      setPauseDurationSec('3600');
      void fetchEntries();
    } catch (err: unknown) {
			showNotification(err instanceof Error ? err.message : 'Failed', 'error' as NotificationType);
    }
  };

  const handleResume = async (keyHash: string) => {
    try {
      await quotaPauseApi.resumeKey(keyHash);
			showNotification(t('quota_pause.resume_success'), 'success' as NotificationType);
      void fetchEntries();
    } catch (err: unknown) {
			showNotification(err instanceof Error ? err.message : 'Failed', 'error' as NotificationType);
    }
  };

  const formatTime = (iso: string) => {
    if (!iso) return '-';
    return new Date(iso).toLocaleString();
  };

  return (
    <div className={styles.container}>
      <div className={styles.header}>
        <h1>{t('quota_pause.title')}</h1>
        <Button onClick={() => setPauseModalOpen(true)}>{t('quota_pause.manual_pause')}</Button>
      </div>

      {error && <div className={styles.error}>{error}</div>}

      <Card className={styles.tableCard}>
        {loading ? (
          <div className={styles.loading}>{t('common.loading')}</div>
        ) : entries.length === 0 ? (
          <div className={styles.empty}>{t('quota_pause.no_paused_keys')}</div>
        ) : (
          <table className={styles.table}>
            <thead>
              <tr>
                <th>{t('quota_pause.key_hash')}</th>
                <th>{t('quota_pause.reason')}</th>
                <th>{t('quota_pause.paused_at')}</th>
                <th>{t('quota_pause.expires_at')}</th>
                <th>{t('common.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((entry) => (
                <tr key={entry.key_hash}>
                  <td className={styles.hashCell}>{entry.key_hash}</td>
                  <td>{entry.reason || '-'}</td>
                  <td>{formatTime(entry.paused_at)}</td>
                  <td>{entry.expires_at ? formatTime(entry.expires_at) : t('quota_pause.permanent')}</td>
                  <td>
				<Button size="sm" variant="secondary" onClick={() => handleResume(entry.key_hash)}>
                      {t('quota_pause.resume')}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>

      <Modal open={pauseModalOpen} onClose={() => setPauseModalOpen(false)} title={t('quota_pause.manual_pause')}>
        <div className={styles.form}>
          <label>{t('quota_pause.key_hash')}</label>
          <Input value={pauseKeyHash} onChange={(e) => setPauseKeyHash(e.target.value)} placeholder="abcd1234 or sk-xxx..." />

          <label>{t('quota_pause.reason')}</label>
          <Input value={pauseReason} onChange={(e) => setPauseReason(e.target.value)} placeholder={t('quota_pause.reason_placeholder')} />

          <label>{t('quota_pause.duration_seconds')}</label>
          <Input type="number" value={pauseDurationSec} onChange={(e) => setPauseDurationSec(e.target.value)} placeholder="3600" />

          <div className={styles.formActions}>
				<Button variant="secondary" onClick={() => setPauseModalOpen(false)}>{t('common.cancel')}</Button>
            <Button onClick={handlePause}>{t('quota_pause.confirm_pause')}</Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
