/**
 * OpenCode Go 额度区块：在配额管理页统一展示各账户的三档额度。
 */

import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { openCodeApi } from '@/services/api';
import type { OpenCodeAccount, OpenCodeWindow } from '@/services/api';
import { Button } from '@/components/ui/Button';
import { IconRefreshCw } from '@/components/ui/icons';
import styles from './OpenCodeQuotaSection.module.scss';

const formatReset = (value?: string): string => {
  if (!value) return '';
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return '';
  return parsed.toLocaleString();
};

const tone = (percent: number): 'ok' | 'warn' | 'danger' => {
  if (percent >= 100) return 'danger';
  if (percent >= 80) return 'warn';
  return 'ok';
};

function QuotaBar({ label, quota }: { label: string; quota?: OpenCodeWindow }) {
  const { t } = useTranslation();
  const percent = Math.max(0, Math.min(100, Number(quota?.percent ?? 0)));
  const reset = formatReset(quota?.resets_at);

  return (
    <div className={styles.barRow}>
      <span className={styles.barLabel}>{label}</span>
      <div className={styles.barTrack}>
        <div className={`${styles.barFill} ${styles[tone(percent)]}`} style={{ width: `${percent}%` }} />
      </div>
      <span className={styles.barValue}>{percent.toFixed(percent % 1 === 0 ? 0 : 1)}%</span>
      <span className={styles.barReset}>{reset ? `${t('opencode.resets')} ${reset}` : ''}</span>
    </div>
  );
}

export function OpenCodeQuotaSection({ searchQuery = '' }: { searchQuery?: string }) {
  const { t } = useTranslation();
  const [accounts, setAccounts] = useState<OpenCodeAccount[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      setAccounts(await openCodeApi.listAccounts());
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : t('opencode.load_failed'));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void load();
  }, [load]);

  useHeaderRefresh(load);

  const query = searchQuery.trim().toLowerCase();
  const visible = query
    ? accounts.filter(
        (account) =>
          account.id.toLowerCase().includes(query) ||
          (account.label ?? '').toLowerCase().includes(query)
      )
    : accounts;

  return (
    <section className={styles.section} aria-label={t('opencode.title')}>
      <header className={styles.header}>
        <div>
          <h2 className={styles.title}>{t('opencode.title')}</h2>
          <p className={styles.subtitle}>{t('opencode.quota_subtitle')}</p>
        </div>
        <Button variant="secondary" size="sm" onClick={() => void load()} disabled={loading}>
          <IconRefreshCw size={14} />
          {loading ? t('opencode.loading') : t('opencode.refresh')}
        </Button>
      </header>

      {error && <div className={styles.errorBox}>{error}</div>}

      {!error && visible.length === 0 && !loading && (
        <div className={styles.emptyBox}>{t('opencode.empty')}</div>
      )}

      <div className={styles.grid}>
        {visible.map((account) => (
          <article key={account.id} className={styles.card}>
            <div className={styles.cardHead}>
              <div>
                <div className={styles.cardTitle}>{account.label || account.id}</div>
                <div className={styles.cardMeta}>{account.id}</div>
              </div>
              <span className={`${styles.badge} ${account.disabled ? styles.badgeOff : styles.badgeOn}`}>
                {account.disabled ? t('opencode.disabled') : t('opencode.enabled')}
              </span>
            </div>

            {account.quota ? (
              <div className={styles.bars}>
                <QuotaBar label={t('opencode.rolling')} quota={account.quota.rolling} />
                <QuotaBar label={t('opencode.weekly')} quota={account.quota.weekly} />
                <QuotaBar label={t('opencode.monthly')} quota={account.quota.monthly} />
                {account.quota.exhausted && (
                  <div className={styles.exhausted}>{t('opencode.exhausted')}</div>
                )}
              </div>
            ) : (
              <div className={styles.missing}>
                {account.quota_error
                  ? `${t('opencode.quota_error')}: ${account.quota_error}`
                  : t('opencode.quota_pending')}
              </div>
            )}
          </article>
        ))}
      </div>
    </section>
  );
}
