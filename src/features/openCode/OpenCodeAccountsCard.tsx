/**
 * OpenCode Go 账户与模型暴露管理卡片（嵌于 OAuth 登录面板）。
 */

import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { authFilesApi, openCodeApi } from '@/services/api';
import type { OpenCodeAccount, OpenCodeModel } from '@/services/api';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { Input } from '@/components/ui/Input';
import styles from './OpenCodeAccountsCard.module.scss';

type FormState = {
  label: string;
  apiKey: string;
  proxyUrl: string;
};

const emptyForm: FormState = { label: '', apiKey: '', proxyUrl: '' };

const slugify = (value: string): string => {
  const normalized = value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, '-')
    .replace(/^-+|-+$/g, '');
  return normalized || 'account';
};

export function OpenCodeAccountsCard() {
  const { t } = useTranslation();
  const [accounts, setAccounts] = useState<OpenCodeAccount[]>([]);
  const [models, setModels] = useState<OpenCodeModel[]>([]);
  const [excluded, setExcluded] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [form, setForm] = useState<FormState>(emptyForm);
  const [formError, setFormError] = useState('');
  const [saving, setSaving] = useState(false);
  const [busyId, setBusyId] = useState('');
  const [savingModels, setSavingModels] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const [accountList, modelCatalog] = await Promise.all([
        openCodeApi.listAccounts(),
        openCodeApi.listModels(),
      ]);
      setAccounts(accountList);
      setModels(modelCatalog.models);
      setExcluded(modelCatalog.excluded.map((item) => item.toLowerCase()));
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : t('opencode.load_failed'));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void load();
  }, [load]);

  const isExposed = (id: string) => !excluded.includes(id.toLowerCase());

  const persistExcluded = async (next: string[]) => {
    setSavingModels(true);
    setError('');
    try {
      const saved = await openCodeApi.saveExcluded(next);
      setExcluded(saved.excluded.map((item) => item.toLowerCase()));
      setNotice(t('opencode.models_saved'));
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : t('opencode.models_failed'));
    } finally {
      setSavingModels(false);
    }
  };

  const toggleModel = (id: string, expose: boolean) => {
    const key = id.toLowerCase();
    const next = expose
      ? excluded.filter((item) => item !== key)
      : Array.from(new Set([...excluded, key]));
    void persistExcluded(next);
  };

  const handleAdd = async () => {
    const apiKey = form.apiKey.trim();
    if (!apiKey) {
      setFormError(t('opencode.need_key'));
      return;
    }
    setSaving(true);
    setFormError('');
    setNotice('');
    try {
      const name = `opencode-${slugify(form.label || 'account')}-${Date.now().toString(36)}.json`;
      const payload: Record<string, unknown> = { type: 'opencode', api_key: apiKey };
      if (form.label.trim()) payload.label = form.label.trim();
      if (form.proxyUrl.trim()) payload.proxy_url = form.proxyUrl.trim();
      await authFilesApi.saveJsonObject(name, payload);
      setForm(emptyForm);
      setNotice(t('opencode.added', { name }));
      await load();
    } catch (err: unknown) {
      setFormError(err instanceof Error ? err.message : t('opencode.add_failed'));
    } finally {
      setSaving(false);
    }
  };

  const handleToggleAccount = async (account: OpenCodeAccount) => {
    setBusyId(account.id);
    try {
      await authFilesApi.setStatusWithFallback(account.id, !account.disabled);
      await load();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : t('opencode.toggle_failed'));
    } finally {
      setBusyId('');
    }
  };

  const handleDelete = async (account: OpenCodeAccount) => {
    if (!window.confirm(t('opencode.confirm_delete', { name: account.id }))) return;
    setBusyId(account.id);
    try {
      await authFilesApi.deleteFileByName(account.id);
      setNotice(t('opencode.deleted', { name: account.id }));
      await load();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : t('opencode.delete_failed'));
    } finally {
      setBusyId('');
    }
  };

  const exposedCount = models.filter((model) => isExposed(model.id)).length;

  return (
    <Card
      title={<span className={styles.cardTitle}>{t('opencode.title')}</span>}
      extra={
        <Button variant="secondary" size="sm" onClick={() => void load()} disabled={loading}>
          {loading ? t('opencode.loading') : t('opencode.refresh')}
        </Button>
      }
    >
      <div className={styles.cardBody}>
        <p className={styles.cardHint}>{t('opencode.subtitle')}</p>
        <p className={styles.cardHint}>{t('opencode.quota_moved_hint')}</p>

        {error && <div className={styles.errorBox}>{error}</div>}
        {notice && <div className={styles.noticeBox}>{notice}</div>}

        <section className={styles.section}>
          <h3 className={styles.sectionTitle}>
            {t('opencode.accounts_title')}
            <span className={styles.sectionMeta}>{t('opencode.count', { count: accounts.length })}</span>
          </h3>

          <div className={styles.formGrid}>
            <Input
              label={t('opencode.label')}
              placeholder={t('opencode.label_placeholder')}
              value={form.label}
              onChange={(event) => setForm((prev) => ({ ...prev, label: event.target.value }))}
            />
            <Input
              label={t('opencode.api_key')}
              placeholder="oc_sk_..."
              hint={t('opencode.api_key_hint')}
              value={form.apiKey}
              onChange={(event) => setForm((prev) => ({ ...prev, apiKey: event.target.value }))}
            />
            <Input
              label={t('opencode.proxy_url')}
              placeholder="http://127.0.0.1:7897"
              hint={t('opencode.proxy_url_hint')}
              value={form.proxyUrl}
              onChange={(event) => setForm((prev) => ({ ...prev, proxyUrl: event.target.value }))}
            />
          </div>
          {formError && <div className={styles.formError}>{formError}</div>}
          <div className={styles.formActions}>
            <Button onClick={() => void handleAdd()} loading={saving} disabled={saving}>
              {saving ? t('opencode.adding') : t('opencode.add')}
            </Button>
            <span className={styles.hint}>{t('opencode.restart_hint')}</span>
          </div>

          {accounts.length === 0 ? (
            !loading && <div className={styles.emptyBox}>{t('opencode.empty')}</div>
          ) : (
            <div className={styles.accountList}>
              {accounts.map((account) => (
                <div key={account.id} className={styles.accountRow}>
                  <div className={styles.accountHead}>
                    <div>
                      <div className={styles.accountLabel}>{account.label || account.id}</div>
                      <div className={styles.accountMeta}>
                        {account.id}
                        {account.proxy_url ? ` · ${account.proxy_url}` : ''}
                      </div>
                    </div>
                    <span className={`${styles.badge} ${account.disabled ? styles.badgeOff : styles.badgeOn}`}>
                      {account.disabled ? t('opencode.disabled') : t('opencode.enabled')}
                    </span>
                  </div>

                  {account.quota?.exhausted && (
                    <div className={styles.exhausted}>{t('opencode.exhausted')}</div>
                  )}

                  <div className={styles.accountActions}>
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={busyId === account.id}
                      onClick={() => void handleToggleAccount(account)}
                    >
                      {account.disabled ? t('opencode.enable') : t('opencode.disable')}
                    </Button>
                    <Button
                      variant="danger"
                      size="sm"
                      disabled={busyId === account.id}
                      onClick={() => void handleDelete(account)}
                    >
                      {t('opencode.remove')}
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </section>

        <section className={styles.section}>
          <h3 className={styles.sectionTitle}>
            {t('opencode.models_title')}
            <span className={styles.sectionMeta}>
              {t('opencode.models_count', { exposed: exposedCount, total: models.length })}
            </span>
          </h3>
          <p className={styles.cardHint}>{t('opencode.models_hint')}</p>

          {models.length === 0 ? (
            <div className={styles.emptyBox}>{t('opencode.models_empty')}</div>
          ) : (
            <div className={styles.modelGrid}>
              {models.map((model) => {
                const exposed = isExposed(model.id);
                return (
                  <label key={model.id} className={styles.modelItem}>
                    <input
                      type="checkbox"
                      checked={exposed}
                      disabled={savingModels}
                      onChange={(event) => toggleModel(model.id, event.target.checked)}
                    />
                    <span className={styles.modelName}>{model.id}</span>
                    <span className={styles.modelProtocol}>{model.protocol}</span>
                  </label>
                );
              })}
            </div>
          )}
        </section>
      </div>
    </Card>
  );
}
