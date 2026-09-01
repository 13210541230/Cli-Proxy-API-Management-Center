import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { IconChevronDown } from './icons';
import { SelectionCheckbox } from './SelectionCheckbox';
import styles from './MultiSelectDropdown.module.scss';

export interface MultiSelectDropdownOption {
  value: string;
  label: ReactNode;
  searchText?: string;
}

interface MultiSelectDropdownProps {
  values: ReadonlyArray<string>;
  options: ReadonlyArray<MultiSelectDropdownOption>;
  onChange: (values: string[]) => void;
  placeholder?: string;
  searchPlaceholder?: string;
  emptyText?: string;
  selectAllText?: string;
  clearText?: string;
  id?: string;
  ariaLabel?: string;
  disabled?: boolean;
  searchable?: boolean;
}

export function MultiSelectDropdown({
  values,
  options,
  onChange,
  placeholder = 'Select options',
  searchPlaceholder = 'Search...',
  emptyText = 'No options',
  selectAllText = 'Select all',
  clearText = 'Clear',
  id,
  ariaLabel,
  disabled = false,
  searchable = true,
}: MultiSelectDropdownProps) {
  const rootRef = useRef<HTMLDivElement | null>(null);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const selectedSet = useMemo(() => new Set(values), [values]);
  const filteredOptions = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase();
    if (!normalizedQuery) return options;
    return options.filter((option) => {
      const label = typeof option.label === 'string' ? option.label : '';
      const searchText = option.searchText ?? label;
      return `${option.value} ${searchText}`.toLowerCase().includes(normalizedQuery);
    });
  }, [options, query]);

  useEffect(() => {
    if (!open || disabled) return;
    const handleClickOutside = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [disabled, open]);

  const toggleValue = (value: string, checked: boolean) => {
    const next = new Set(selectedSet);
    if (checked) next.add(value);
    else next.delete(value);
    onChange(options.filter((option) => next.has(option.value)).map((option) => option.value));
  };

  const selectAllVisible = () => {
    const next = new Set(selectedSet);
    filteredOptions.forEach((option) => next.add(option.value));
    onChange(options.filter((option) => next.has(option.value)).map((option) => option.value));
  };

  const clearVisible = () => {
    const visibleValues = new Set(filteredOptions.map((option) => option.value));
    onChange(
      options
        .filter((option) => selectedSet.has(option.value) && !visibleValues.has(option.value))
        .map((option) => option.value)
    );
  };

  const selectedLabels = options
    .filter((option) => selectedSet.has(option.value))
    .map((option) => (typeof option.label === 'string' ? option.label : option.value));
  const triggerLabel =
    selectedLabels.length === 0
      ? placeholder
      : selectedLabels.length <= 2
        ? selectedLabels.join('、')
        : `已选择 ${selectedLabels.length} 项`;

  const toggleOpen = () => {
    if (open) setQuery('');
    setOpen((current) => !current);
  };

  return (
    <div className={styles.root} ref={rootRef}>
      <button
        type="button"
        id={id}
        className={styles.trigger}
        onClick={toggleOpen}
        disabled={disabled}
        aria-label={ariaLabel}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <span
          className={selectedLabels.length === 0 ? styles.placeholder : styles.triggerText}
          title={triggerLabel}
        >
          {triggerLabel}
        </span>
        {selectedLabels.length > 0 && <span className={styles.count}>{selectedLabels.length}</span>}
        <IconChevronDown size={16} />
      </button>

      {open && !disabled && (
        <div
          className={styles.dropdown}
          role="listbox"
          aria-multiselectable="true"
          aria-label={ariaLabel}
        >
          {searchable && (
            <input
              autoFocus
              className={styles.search}
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={searchPlaceholder}
              aria-label={searchPlaceholder}
            />
          )}
          <div className={styles.toolbar}>
            <button
              type="button"
              onClick={selectAllVisible}
              disabled={filteredOptions.length === 0}
            >
              {selectAllText}
            </button>
            <button type="button" onClick={clearVisible} disabled={selectedSet.size === 0}>
              {clearText}
            </button>
          </div>
          <div className={styles.options}>
            {filteredOptions.length === 0 ? (
              <div className={styles.empty}>{emptyText}</div>
            ) : (
              filteredOptions.map((option) => (
                <SelectionCheckbox
                  key={option.value}
                  checked={selectedSet.has(option.value)}
                  onChange={(checked) => toggleValue(option.value, checked)}
                  className={styles.option}
                  labelClassName={styles.optionLabel}
                  label={option.label}
                  title={option.value}
                />
              ))
            )}
          </div>
        </div>
      )}
    </div>
  );
}
