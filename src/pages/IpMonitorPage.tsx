import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAuthStore } from '@/stores';
import { ipApi, type IPActivity } from '@/services/api';
import styles from './IpMonitorPage.module.scss';

type TimeFilter = '5' | '15' | '30' | '60' | '1440' | 'all';

interface IPListEntry extends IPActivity {
  ip: string;
}

export function IpMonitorPage() {
  const { t } = useTranslation();
  const apiBase = useAuthStore((state) => state.apiBase);
  const connectionStatus = useAuthStore((state) => state.connectionStatus);

  const [timeFilter, setTimeFilter] = useState<TimeFilter>('5');
  const [ipList, setIpList] = useState<IPListEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [statistics, setStatistics] = useState<{
    total_ips: number;
    active_ips: number;
    total_requests: number;
    total_tokens: number;
  } | null>(null);
  const [cleanupResult, setCleanupResult] = useState<{
    removed: number;
    message: string;
  } | null>(null);

  const fetchData = useCallback(async () => {
    if (connectionStatus !== 'connected' || !apiBase) {
      setLoading(false);
      return;
    }

    setLoading(true);
    setError(null);

    try {
      // Fetch statistics
      const statsRes = await ipApi.getStatistics();
      setStatistics(statsRes);

      // Fetch IP list based on filter
      let ipsRes;
      if (timeFilter === 'all') {
        ipsRes = await ipApi.getAllIPs();
      } else {
        ipsRes = await ipApi.getActiveIPs(parseInt(timeFilter, 10));
      }

      // Handle different response structures (all=true returns "ips", others return "active_ips")
      const ipData = ipsRes.active_ips || ipsRes.ips || {};

      // Convert to array and sort by last_seen (most recent first)
      const ipArray: IPListEntry[] = Object.entries(ipData).map(
        ([ip, activity]) => ({
          ...activity,
          ip,
        })
      );

      ipArray.sort((a, b) => new Date(b.last_seen).getTime() - new Date(a.last_seen).getTime());
      setIpList(ipArray);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch IP data');
    } finally {
      setLoading(false);
    }
  }, [connectionStatus, apiBase, timeFilter]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const handleCleanup = async () => {
    if (!window.confirm(t('ip_monitor.cleanup_confirm'))) {
      return;
    }

    try {
      const result = await ipApi.cleanupInactiveIPs(24);
      setCleanupResult({
        removed: result.removed,
        message: result.message,
      });
      // Refresh data after cleanup
      await fetchData();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Cleanup failed');
    }
  };

  const formatDate = (dateStr: string) => {
    const date = new Date(dateStr);
    return date.toLocaleString();
  };

  const formatTokens = (tokens: number) => {
    if (tokens >= 1000000) {
      return `${(tokens / 1000000).toFixed(2)}M`;
    }
    if (tokens >= 1000) {
      return `${(tokens / 1000).toFixed(1)}K`;
    }
    return tokens.toString();
  };

  const timeFilterOptions: TimeFilter[] = ['5', '15', '30', '60', '1440', 'all'];

  return (
    <div className={styles.container}>
      <header className={styles.header}>
        <h1 className={styles.title}>{t('ip_monitor.title')}</h1>
        <p className={styles.subtitle}>{t('ip_monitor.description')}</p>
      </header>

      {/* Statistics Cards */}
      <section className={styles.statsSection}>
        <div className={styles.statCard}>
          <span className={styles.statValue}>{statistics?.total_ips ?? '...'}</span>
          <span className={styles.statLabel}>{t('ip_monitor.total_ips')}</span>
        </div>
        <div className={styles.statCard}>
          <span className={styles.statValue}>{statistics?.active_ips ?? '...'}</span>
          <span className={styles.statLabel}>{t('ip_monitor.active_ips')}</span>
        </div>
        <div className={styles.statCard}>
          <span className={styles.statValue}>{statistics?.total_requests?.toLocaleString() ?? '...'}</span>
          <span className={styles.statLabel}>{t('ip_monitor.total_requests')}</span>
        </div>
        <div className={styles.statCard}>
          <span className={styles.statValue}>{formatTokens(statistics?.total_tokens ?? 0)}</span>
          <span className={styles.statLabel}>{t('ip_monitor.total_tokens')}</span>
        </div>
      </section>

      {/* Controls */}
      <section className={styles.controlsSection}>
        <div className={styles.filterGroup}>
          <label className={styles.filterLabel}>{t('ip_monitor.time_filter')}</label>
          <div className={styles.filterButtons}>
            {timeFilterOptions.map((option) => (
              <button
                key={option}
                type="button"
                className={`${styles.filterButton} ${timeFilter === option ? styles.active : ''}`}
                onClick={() => setTimeFilter(option)}
              >
                {option === 'all' ? t('ip_monitor.all_time') : option === '1440' ? t('ip_monitor.last_hours', { hours: '24' }) : t('ip_monitor.last_minutes', { minutes: option })}
              </button>
            ))}
          </div>
        </div>

        <div className={styles.actionGroup}>
          <button
            type="button"
            className={styles.refreshButton}
            onClick={fetchData}
            disabled={loading}
          >
            {loading ? t('common.loading') : t('common.refresh')}
          </button>
          <button
            type="button"
            className={styles.cleanupButton}
            onClick={handleCleanup}
          >
            {t('ip_monitor.cleanup')}
          </button>
        </div>
      </section>

      {/* Cleanup Result */}
      {cleanupResult && (
        <div className={styles.cleanupResult}>
          {t('ip_monitor.cleanup_result', { count: cleanupResult.removed })}
          <button
            type="button"
            className={styles.closeButton}
            onClick={() => setCleanupResult(null)}
          >
            ×
          </button>
        </div>
      )}

      {/* Error Message */}
      {error && (
        <div className={styles.error}>
          {error}
          <button
            type="button"
            className={styles.closeButton}
            onClick={() => setError(null)}
          >
            ×
          </button>
        </div>
      )}

      {/* IP List Table */}
      <section className={styles.tableSection}>
        {loading ? (
          <div className={styles.loading}>{t('common.loading')}</div>
        ) : ipList.length === 0 ? (
          <div className={styles.empty}>{t('ip_monitor.no_data')}</div>
        ) : (
          <table className={styles.table}>
            <thead>
              <tr>
                <th>{t('ip_monitor.ip_address')}</th>
                <th>{t('ip_monitor.last_seen')}</th>
                <th>{t('ip_monitor.requests')}</th>
                <th>{t('ip_monitor.tokens')}</th>
                <th>{t('ip_monitor.failed')}</th>
                <th>{t('ip_monitor.api_keys')}</th>
              </tr>
            </thead>
            <tbody>
              {ipList.map((entry) => (
                <tr key={entry.ip}>
                  <td className={styles.ipCell}>
                    <span className={styles.ipAddress}>{entry.ip}</span>
                    {entry.failed_requests > 0 && (
                      <span className={styles.errorBadge}>
                        {entry.failed_requests}
                      </span>
                    )}
                  </td>
                  <td className={styles.dateCell}>{formatDate(entry.last_seen)}</td>
                  <td className={styles.numberCell}>{entry.total_requests.toLocaleString()}</td>
                  <td className={styles.numberCell}>{formatTokens(entry.total_tokens)}</td>
                  <td className={styles.numberCell}>
                    {entry.failed_requests > 0 ? (
                      <span className={styles.failedCount}>{entry.failed_requests}</span>
                    ) : (
                      '-'
                    )}
                  </td>
                  <td className={styles.keysCell}>
                    {Object.entries(entry.api_keys || {}).map(([key, count]) => (
                      <span key={key} className={styles.keyBadge} title={`${key}: ${count} requests`}>
                        {key.length > 12 ? `${key.substring(0, 12)}...` : key}
                      </span>
                    ))}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}
