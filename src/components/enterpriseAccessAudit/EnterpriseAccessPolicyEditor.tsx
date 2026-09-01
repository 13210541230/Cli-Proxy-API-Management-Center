import { useEffect, useMemo, useState } from 'react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { MultiSelectDropdown } from '@/components/ui/MultiSelectDropdown';
import { Select } from '@/components/ui/Select';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { normalizeEnterprisePolicyModels } from '@/utils/enterpriseAccessAudit';
import { buildEnterprisePolicyEditorDraft } from './policyDraft';
import styles from './EnterpriseAccessPolicyEditor.module.scss';

export interface EnterpriseAccessPolicyEditorTarget {
  label: string;
  keyHashes: string[];
}

interface EnterpriseAccessPolicyEditorProps {
  open: boolean;
  mode: 'single' | 'batch';
  target: EnterpriseAccessPolicyEditorTarget | null;
  initialDeniedModels: string[];
  initialAuditEnabled: boolean | null;
  initialModelsMixed?: boolean;
  suggestions?: string[];
  saving?: boolean;
  onClose: () => void;
  onSave: (
    deniedModels: string[],
    auditEnabled: boolean | null,
    modelsChanged: boolean,
    auditChanged: boolean
  ) => void | Promise<void>;
}

export function EnterpriseAccessPolicyEditor({
  open,
  mode,
  target,
  initialDeniedModels,
  initialAuditEnabled,
  initialModelsMixed = false,
  suggestions = [],
  saving = false,
  onClose,
  onSave,
}: EnterpriseAccessPolicyEditorProps) {
  const [deniedModels, setDeniedModels] = useState<string[]>([]);
  const [modelInput, setModelInput] = useState('');
  const [auditEnabled, setAuditEnabled] = useState(true);
  const [batchAuditMode, setBatchAuditMode] = useState<'unchanged' | 'enabled' | 'disabled'>('unchanged');
  const [inputError, setInputError] = useState('');
  const [saveError, setSaveError] = useState('');
  const [modelsDirty, setModelsDirty] = useState(false);

  const targetKey = target?.keyHashes.join(',') ?? '';
  useEffect(() => {
    if (!open) return;
    // Reset the draft when a different row or modal session is opened.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setDeniedModels(normalizeEnterprisePolicyModels(initialDeniedModels));
    setModelsDirty(false);
    setModelInput('');
    setInputError('');
    setSaveError('');
    setAuditEnabled(initialAuditEnabled !== false);
    setBatchAuditMode(initialAuditEnabled === null ? 'unchanged' : initialAuditEnabled ? 'enabled' : 'disabled');
  }, [initialAuditEnabled, initialDeniedModels, open, targetKey]);

  const modelOptions = useMemo(() => {
    try {
      return normalizeEnterprisePolicyModels(suggestions);
    } catch {
      // Suggestions are optional and never make explicit model entry unavailable.
      return [];
    }
  }, [suggestions]);
  const modelOptionSet = useMemo(() => new Set(modelOptions), [modelOptions]);
  const selectedCatalogModels = useMemo(
    () => deniedModels.filter((model) => modelOptionSet.has(model)),
    [deniedModels, modelOptionSet]
  );

  const addModel = (value: string) => {
    try {
      const model = normalizeEnterprisePolicyModels([value])[0];
      setDeniedModels((current) => normalizeEnterprisePolicyModels([...current, model]));
      setModelsDirty(true);
      setModelInput('');
      setInputError('');
    } catch (error) {
      setInputError(error instanceof Error ? error.message : '模型 ID 无效');
    }
  };

  const removeModel = (model: string) => {
    setDeniedModels((current) => current.filter((item) => item !== model));
    setModelsDirty(true);
  };

  const handleInputKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== 'Enter') return;
    event.preventDefault();
    addModel(modelInput);
  };

  const handleSave = async () => {
    try {
      const draft = buildEnterprisePolicyEditorDraft(
        deniedModels,
        mode === 'single'
          ? auditEnabled
          : batchAuditMode === 'enabled'
            ? true
            : batchAuditMode === 'disabled'
              ? false
              : null
      );
      setSaveError('');
      const modelsChanged = initialModelsMixed
        ? modelsDirty
        : JSON.stringify(draft.deniedModels) !== JSON.stringify(normalizeEnterprisePolicyModels(initialDeniedModels));
      const selectedAudit = mode === 'single' ? auditEnabled : batchAuditMode === 'enabled' ? true : batchAuditMode === 'disabled' ? false : null;
      const auditChanged =
        mode === 'single'
          ? selectedAudit !== (initialAuditEnabled ?? true)
          : batchAuditMode !== 'unchanged' &&
            (initialAuditEnabled === null || selectedAudit !== initialAuditEnabled);
      await onSave(draft.deniedModels, draft.auditEnabled, modelsChanged, auditChanged);
    } catch (error) {
      setSaveError(error instanceof Error ? error.message : '策略保存失败');
    }
  };

  const title = mode === 'single' ? '编辑 Key 策略' : '批量设置 Key 策略';
  const auditValue = mode === 'single' ? auditEnabled : batchAuditMode !== 'disabled';

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={title}
      width={620}
      closeDisabled={saving}
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={saving}>
            取消
          </Button>
          <Button onClick={handleSave} loading={saving}>
            保存策略
          </Button>
        </>
      }
    >
      <div className={styles.content}>
        <div className={styles.target}>目标：{target?.label ?? '-'}（{target?.keyHashes.length ?? 0} 个 Key）</div>
        <div className={styles.fieldHint}>
          仅支持精确模型 ID。下拉列表支持批量勾选当前 CPA 支持的模型；输入会自动去空格、转小写、去重并排序，未列出的动态模型也可以直接输入。
        </div>
        {modelOptions.length > 0 && (
          <div className={styles.catalogField}>
            <label className={styles.catalogLabel} htmlFor="enterprise-policy-models">从当前 CPA 支持的模型中选择</label>
            <MultiSelectDropdown
              id="enterprise-policy-models"
              values={selectedCatalogModels}
              options={modelOptions.map((model) => ({ value: model, label: model }))}
              onChange={(selected) => {
                setDeniedModels((current) => normalizeEnterprisePolicyModels([
                  ...current.filter((model) => !modelOptionSet.has(model)),
                  ...selected,
                ]));
                setModelsDirty(true);
              }}
              placeholder="请选择要禁止的模型（可多选）"
              searchPlaceholder="搜索当前支持的模型"
              emptyText="没有匹配的支持模型"
              selectAllText="全选当前模型"
              clearText="清空当前选择"
              ariaLabel="批量选择禁止模型"
              disabled={saving}
            />
          </div>
        )}
        <div className={styles.addRow}>
          <Input
            label="禁止模型 ID"
            value={modelInput}
            onChange={(event) => {
              setModelInput(event.target.value);
              setInputError('');
            }}
            onKeyDown={handleInputKeyDown}
            placeholder="也可以手动输入动态模型 ID"
            error={inputError}
          />
          <Button variant="secondary" onClick={() => addModel(modelInput)} disabled={!modelInput.trim() || saving}>
            添加
          </Button>
        </div>
        <div className={styles.modelList} aria-label="禁止模型列表">
          {deniedModels.map((model) => (
            <div className={styles.modelItem} key={model}>
              <span className={styles.modelName}>{model}</span>
              <Button variant="ghost" size="sm" onClick={() => removeModel(model)} disabled={saving} aria-label={`移除 ${model}`}>
                移除
              </Button>
            </div>
          ))}
          {deniedModels.length === 0 && <div className={styles.empty}>当前不禁止任何模型（全量模型可用）</div>}
        </div>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => {
            setDeniedModels([]);
            setModelsDirty(true);
          }}
          disabled={saving || (deniedModels.length === 0 && !initialModelsMixed)}
        >
          清空禁止列表
        </Button>
        {mode === 'single' ? (
          <ToggleSwitch
            checked={auditValue}
            onChange={setAuditEnabled}
            label="启用用户文本审计"
            ariaLabel="启用用户文本审计"
            disabled={saving}
          />
        ) : (
          <Select
            value={batchAuditMode}
            options={[
              { value: 'unchanged', label: '审计开关：不修改' },
              { value: 'enabled', label: '审计开关：全部启用' },
              { value: 'disabled', label: '审计开关：全部停用' },
            ]}
            onChange={(value) => setBatchAuditMode(value as typeof batchAuditMode)}
            ariaLabel="批量审计开关"
            disabled={saving}
          />
        )}
        {saveError && <div className="error-box">{saveError}</div>}
      </div>
    </Modal>
  );
}
