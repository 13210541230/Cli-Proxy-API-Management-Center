import type { ReactNode } from 'react';
import { IconX } from '@/components/ui/icons';
import styles from './ExcludedModelRuleChip.module.scss';

/**
 * Exclusion chip with three source-specific styles, replacing near-duplicate markup in
 * `AuthFileDetailsSheet.module.scss` (`.excludedModelChip`) and
 * `AuthFilesOAuthExcludedEditPage.module.scss` (`.customRuleChip`).
 *
 * - `exact`: solid primary style for explicitly selected models; removable.
 * - `wildcard`: dashed style for models matched by a wildcard rule. No remove button is shown;
 *   the rule itself must be edited, and a remove button would promise an action it cannot perform.
 * - `unknown`: muted dashed style for exact rules absent from the catalog (for example retired IDs); removable.
 */
export type ExcludedModelChipVariant = 'exact' | 'wildcard' | 'unknown';

export interface ExcludedModelRuleChipProps {
  label: string;
  variant?: ExcludedModelChipVariant;
  /** Secondary detail, such as the rule that produced this chip. */
  detail?: string;
  /** Omit to hide the remove button. */
  onRemove?: () => void;
  removeAriaLabel?: string;
  disabled?: boolean;
  title?: string;
}

/** Shared wrapping container for chips. Exported so consumers do not duplicate flex-wrap styles. */
export function ExcludedModelChipRow({ children }: { children: ReactNode }) {
  return <div className={styles.chipRow}>{children}</div>;
}

export function ExcludedModelRuleChip({
  label,
  variant = 'exact',
  detail,
  onRemove,
  removeAriaLabel,
  disabled = false,
  title,
}: ExcludedModelRuleChipProps) {
  return (
    <span
      className={`${styles.chip} ${styles[variant]}`}
      title={title ?? (detail ? `${label} — ${detail}` : label)}
    >
      <span className={styles.label}>{label}</span>
      {detail ? <span className={styles.detail}>{detail}</span> : null}
      {onRemove ? (
        <button
          type="button"
          className={styles.remove}
          onClick={onRemove}
          disabled={disabled}
          aria-label={removeAriaLabel ?? label}
        >
          <IconX size={12} />
        </button>
      ) : null}
    </span>
  );
}
