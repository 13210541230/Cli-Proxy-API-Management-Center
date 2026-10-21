/**
 * OpenCode 模型列表：搜索 + 全选/全不选 + 批量应用，替代逐个勾选。
 */

import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { IconRefreshCw, IconSearch } from '@/components/ui/icons';
import { SelectionCheckbox } from '@/components/ui/SelectionCheckbox';
import type { OpenCodeModel } from '@/services/api';
import styles from './OpenCodeModelPicker.module.scss';

interface OpenCodeModelPickerProps {
  models: OpenCodeModel[];
  enabled: Set<string>;
  saving?: boolean;
  onReload: () => void;
  onApply: (enabledNames: string[]) => void;
  onClose: () => void;
}

export function OpenCodeModelPicker({
  models,
  enabled,
  saving,
  onReload,
  onApply,
  onClose,
}: OpenCodeModelPickerProps) {
  const { t } = useTranslation();
  const [search, setSearch] = useState('');
  const [selected, setSelected] = useState<Set<string>>(() => new Set(enabled));

  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase();
    if (!query) return models;
    return models.filter((model) => model.id.toLowerCase().includes(query));
  }, [models, search]);

  const allChecked = filtered.length > 0 && filtered.every((model) => selected.has(model.id));

  const toggle = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  // 全选只作用于当前筛选结果：搜索时批量操作不会误伤未显示模型。
  const toggleAll = () => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (allChecked) {
        filtered.forEach((model) => next.delete(model.id));
      } else {
        filtered.forEach((model) => next.add(model.id));
      }
      return next;
    });
  };

  const enabledCount = models.filter((model) => selected.has(model.id)).length;

  return (
    <div className={styles.panel}>
      <div className={styles.toolbar}>
        <div className={styles.searchWrap}>
          <span className={styles.searchIcon} aria-hidden="true">
            <IconSearch size={14} />
          </span>
          <input
            type="search"
            className={styles.search}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('opencode.models_search')}
            aria-label={t('opencode.models_search')}
          />
        </div>
        <button type="button" className={styles.reloadBtn} onClick={onReload} disabled={saving}>
          <IconRefreshCw size={14} />
          <span>{t('opencode.models_reload')}</span>
        </button>
      </div>

      <div className={styles.batchRow}>
        <SelectionCheckbox
          checked={allChecked}
          onChange={toggleAll}
          disabled={filtered.length === 0}
          label={<span className={styles.batchLabel}>{t('opencode.models_select_all')}</span>}
        />
        <span className={styles.count}>
          {t('opencode.models_count', { exposed: enabledCount, total: models.length })}
        </span>
      </div>

      {filtered.length === 0 ? (
        <div className={styles.empty}>{t('opencode.models_empty')}</div>
      ) : (
        <ul className={styles.list}>
          {filtered.map((model) => (
            <li key={model.id} className={styles.item}>
              <SelectionCheckbox
                checked={selected.has(model.id)}
                onChange={() => toggle(model.id)}
                label={
                  <span className={styles.nameGroup}>
                    <span className={styles.name}>{model.id}</span>
                    <span className={styles.protocol}>{model.protocol}</span>
                  </span>
                }
              />
            </li>
          ))}
        </ul>
      )}

      <div className={styles.footer}>
        <button type="button" className={styles.closeBtn} onClick={onClose} disabled={saving}>
          {t('opencode.models_close')}
        </button>
        <button
          type="button"
          className={styles.applyBtn}
          onClick={() => onApply([...selected])}
          disabled={saving}
        >
          {t('opencode.models_apply', { count: enabledCount })}
        </button>
      </div>
    </div>
  );
}
