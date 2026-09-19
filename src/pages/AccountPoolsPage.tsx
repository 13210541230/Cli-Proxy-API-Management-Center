import { useEffect, useMemo, useState } from 'react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Select } from '@/components/ui/Select';
import { useNotificationStore } from '@/stores';
import { accountPoolsApi } from '@/services/api/accountPools';
import { authFilesApi } from '@/services/api/authFiles';
import { enterpriseKeysApi } from '@/services/api/enterpriseKeys';
import type { AuthFileItem } from '@/types/authFile';
import type { EnterpriseDepartment, EnterpriseKeyBinding } from '@/types/enterpriseKey';
import type { AccountPoolMember, AccountPoolSnapshot } from '@/types/accountPool';
import styles from './AccountPoolsPage.module.scss';

const authFileId = (file: AuthFileItem): string => String(file.authIndex ?? file.name);

export function AccountPoolsPage() {
  const { showNotification } = useNotificationStore();
  const [snapshot, setSnapshot] = useState<AccountPoolSnapshot | null>(null);
  const [authFiles, setAuthFiles] = useState<AuthFileItem[]>([]);
  const [departments, setDepartments] = useState<EnterpriseDepartment[]>([]);
  const [keyBindings, setKeyBindings] = useState<EnterpriseKeyBinding[]>([]);
  const [selectedPoolId, setSelectedPoolId] = useState('');
  const [selectedAuthIds, setSelectedAuthIds] = useState<string[]>([]);
  const [selectedDepartmentId, setSelectedDepartmentId] = useState('');
  const [selectedDepartmentPoolId, setSelectedDepartmentPoolId] = useState('');
  const [selectedDepartmentHashes, setSelectedDepartmentHashes] = useState<string[]>([]);
  const [newPoolId, setNewPoolId] = useState('');
  const [newPoolName, setNewPoolName] = useState('');
  const [busy, setBusy] = useState(false);

  const pools = snapshot?.policy.pools ?? [];
  const members = snapshot?.policy.members ?? [];
  const codexAuthFiles = useMemo(
    () => authFiles.filter((file) => file.type === 'codex' || file.provider === 'codex'),
    [authFiles]
  );

  const refresh = async () => {
    setBusy(true);
    try {
      const [nextSnapshot, authResponse, departmentResponse, bindingResponse] = await Promise.all([
        accountPoolsApi.getSnapshot(),
        authFilesApi.list(),
        enterpriseKeysApi.listDepartments(),
        enterpriseKeysApi.listKeyBindings(),
      ]);
      setSnapshot(nextSnapshot);
      setAuthFiles(authResponse.files);
      setDepartments(departmentResponse.items);
      setKeyBindings(bindingResponse.items);
      setSelectedPoolId((current) => current || nextSnapshot.policy.pools[0]?.id || '');
    } catch (error) {
      showNotification(error instanceof Error ? error.message : '账号池加载失败', 'error');
    } finally {
      setBusy(false);
    }
  };

  useEffect(() => {
    void refresh();
  }, []);

  useEffect(() => {
    setSelectedAuthIds(
      members.filter((member) => member.poolId === selectedPoolId).map((member) => member.authId)
    );
  }, [members, selectedPoolId]);

  const selectedPool = pools.find((pool) => pool.id === selectedPoolId);
  const departmentCandidates = keyBindings.filter(
    (binding) => binding.departmentId === selectedDepartmentId
  );
  const selectedDepartmentKeys = departmentCandidates.filter((binding) =>
    selectedDepartmentHashes.includes(binding.apiKeyHash)
  );

  useEffect(() => {
    setSelectedDepartmentHashes(departmentCandidates.map((binding) => binding.apiKeyHash));
  }, [selectedDepartmentId, keyBindings]);

  const syncPolicy = async () => {
    setBusy(true);
    try {
      setSnapshot(await accountPoolsApi.sync());
      showNotification('账号池策略已同步到 CPA', 'success');
    } catch (error) {
      showNotification(error instanceof Error ? error.message : '账号池策略同步失败', 'error');
    } finally {
      setBusy(false);
    }
  };

  const createPool = async () => {
    const id = newPoolId.trim();
    const name = newPoolName.trim();
    if (!id || !name) return;
    setBusy(true);
    try {
      const next = await accountPoolsApi.create({ id, name, provider: 'codex', enabled: true });
      setSnapshot(next);
      setSelectedPoolId(id);
      setNewPoolId('');
      setNewPoolName('');
      showNotification('账号池已创建', 'success');
    } catch (error) {
      showNotification(error instanceof Error ? error.message : '账号池创建失败', 'error');
    } finally {
      setBusy(false);
    }
  };

  const saveMembers = async () => {
    if (!selectedPool) return;
    setBusy(true);
    try {
      const items: AccountPoolMember[] = selectedAuthIds.map((authId, index) => ({
        poolId: selectedPool.id,
        authId,
        priority: index,
        enabled: true,
      }));
      setSnapshot(await accountPoolsApi.replaceMembers(selectedPool.id, items));
      showNotification('账号池成员已保存', 'success');
    } catch (error) {
      showNotification(error instanceof Error ? error.message : '账号池成员保存失败', 'error');
    } finally {
      setBusy(false);
    }
  };

  const assignDepartment = async () => {
    if (!selectedDepartmentId || !selectedDepartmentPoolId) return;
    setBusy(true);
    try {
      const next = await accountPoolsApi.replaceBindings(
        selectedDepartmentKeys.map((item) => ({
          apiKeyHash: item.apiKeyHash,
          poolId: selectedDepartmentPoolId,
        }))
      );
      setSnapshot(next);
      showNotification(`已为 ${selectedDepartmentKeys.length} 个用户分配账号池`, 'success');
    } catch (error) {
      showNotification(error instanceof Error ? error.message : '部门账号池分配失败', 'error');
    } finally {
      setBusy(false);
    }
  };

  const clearDepartment = async () => {
    if (!selectedDepartmentId) return;
    setBusy(true);
    try {
      const next = await accountPoolsApi.replaceBindings(
        selectedDepartmentKeys.map((item) => ({ apiKeyHash: item.apiKeyHash, poolId: '' }))
      );
      setSnapshot(next);
      showNotification(`已清除 ${selectedDepartmentKeys.length} 个用户的账号池绑定`, 'success');
    } catch (error) {
      showNotification(error instanceof Error ? error.message : '清除部门账号池绑定失败', 'error');
    } finally {
      setBusy(false);
    }
  };

  const togglePool = async () => {
    if (!selectedPool) return;
    setBusy(true);
    try {
      const next = await accountPoolsApi.update(selectedPool.id, {
        name: selectedPool.name,
        provider: selectedPool.provider,
        enabled: !selectedPool.enabled,
      });
      setSnapshot(next);
      showNotification(selectedPool.enabled ? '账号池已停用' : '账号池已启用', 'success');
    } catch (error) {
      showNotification(error instanceof Error ? error.message : '账号池状态更新失败', 'error');
    } finally {
      setBusy(false);
    }
  };

  const deletePool = async () => {
    if (!selectedPool) return;
    setBusy(true);
    try {
      const next = await accountPoolsApi.remove(selectedPool.id);
      setSnapshot(next);
      setSelectedPoolId(next.policy.pools[0]?.id || '');
      showNotification('账号池已删除', 'success');
    } catch (error) {
      showNotification(error instanceof Error ? error.message : '账号池删除失败', 'error');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className={styles.page}>
      <div className={styles.header}>
        <div>
          <h1>账号池管理</h1>
          <p>按账号池隔离 Codex 认证文件，并按部门批量绑定用户。</p>
        </div>
        <div className={styles.actionRow}>
          <Button variant="secondary" onClick={() => void refresh()} disabled={busy}>刷新</Button>
          <Button onClick={() => void syncPolicy()} disabled={busy}>同步 CPA</Button>
        </div>
      </div>

      <Card>
        <div className={styles.cardHeader}><h2>创建账号池</h2></div>
        <div className={styles.formRow}>
          <Input value={newPoolId} onChange={(event) => setNewPoolId(event.target.value)} placeholder="唯一标识，例如 engineering" />
          <Input value={newPoolName} onChange={(event) => setNewPoolName(event.target.value)} placeholder="显示名称" />
          <Button onClick={() => void createPool()} disabled={busy || !newPoolId.trim() || !newPoolName.trim()}>创建</Button>
        </div>
      </Card>

      <div className={styles.grid}>
        <Card>
          <div className={styles.cardHeader}><h2>账号池</h2><span>{snapshot?.status.exclusiveReady ? '已生效' : '等待 CPA 同步'}</span></div>
          <div className={styles.poolList}>
            {pools.length === 0 && <div className={styles.empty}>暂无账号池</div>}
            {pools.map((pool) => (
              <button key={pool.id} className={pool.id === selectedPoolId ? styles.poolActive : styles.pool} onClick={() => setSelectedPoolId(pool.id)}>
                <strong>{pool.name}</strong><small>{pool.id}</small>
              </button>
            ))}
          </div>
          {selectedPool && <div className={styles.actionRow}>
            <Button variant="secondary" onClick={() => void togglePool()} disabled={busy}>{selectedPool.enabled ? '停用账号池' : '启用账号池'}</Button>
            <Button variant="danger" onClick={() => void deletePool()} disabled={busy}>删除当前账号池</Button>
          </div>}
        </Card>

        <Card>
          <div className={styles.cardHeader}><h2>认证文件成员</h2><span>{codexAuthFiles.length} 个 Codex 文件</span></div>
          {!selectedPool && <div className={styles.empty}>请先创建或选择账号池</div>}
          {selectedPool && <>
            <div className={styles.authList}>
              {codexAuthFiles.map((file) => {
                const id = authFileId(file);
                return <label key={id} className={styles.checkbox}>
                  <input type="checkbox" checked={selectedAuthIds.includes(id)} onChange={(event) => setSelectedAuthIds((current) => event.target.checked ? [...current, id] : current.filter((item) => item !== id))} />
                  <span>{file.name}</span><small>{id}</small>
                </label>;
              })}
            </div>
            <Button onClick={() => void saveMembers()} disabled={busy}>保存成员</Button>
          </>}
        </Card>
      </div>

      <Card>
        <div className={styles.cardHeader}><h2>按部门分配账号池</h2><span>仅写入 API Key hash，不保存原始密钥</span></div>
        <div className={styles.formRow}>
          <Select
            value={selectedDepartmentId}
            onChange={setSelectedDepartmentId}
            placeholder="选择部门"
            options={departments.map((department) => ({ value: department.id, label: department.name }))}
          />
          <Select
            value={selectedDepartmentPoolId}
            onChange={setSelectedDepartmentPoolId}
            placeholder="选择账号池"
            options={pools.map((pool) => ({ value: pool.id, label: pool.name }))}
          />
          <Button onClick={() => void assignDepartment()} disabled={busy || !selectedDepartmentId || !selectedDepartmentPoolId}>批量分配</Button>
          <Button variant="secondary" onClick={() => void clearDepartment()} disabled={busy || !selectedDepartmentId}>清除绑定</Button>
        </div>
        <div className={styles.keySelection}>
          <label className={styles.checkbox}>
            <input
              type="checkbox"
              checked={departmentCandidates.length > 0 && selectedDepartmentKeys.length === departmentCandidates.length}
              onChange={(event) => setSelectedDepartmentHashes(event.target.checked ? departmentCandidates.map((binding) => binding.apiKeyHash) : [])}
            />
            <strong>全选当前部门用户</strong>
            <small>{selectedDepartmentKeys.length}/{departmentCandidates.length}</small>
          </label>
          {departmentCandidates.map((binding) => (
            <label key={binding.apiKeyHash} className={styles.checkbox}>
              <input
                type="checkbox"
                checked={selectedDepartmentHashes.includes(binding.apiKeyHash)}
                onChange={(event) => setSelectedDepartmentHashes((current) => event.target.checked ? [...current, binding.apiKeyHash] : current.filter((hash) => hash !== binding.apiKeyHash))}
              />
              <span>{binding.userName || '未命名用户'}</span>
              <small>{binding.apiKeyHash.slice(-8)}</small>
            </label>
          ))}
        </div>
        <div className={styles.hint}>部门只是批量筛选维度，运行时使用保存的 API Key hash 绑定。</div>
      </Card>

      <Card>
        <div className={styles.cardHeader}><h2>策略状态</h2><span>版本 {snapshot?.status.desiredVersion ?? 0}</span></div>
        <div className={styles.statusGrid}>
          <div>期望版本<strong>{snapshot?.status.desiredVersion ?? 0}</strong></div>
          <div>已应用版本<strong>{snapshot?.status.appliedVersion ?? 0}</strong></div>
          <div>活动版本<strong>{snapshot?.status.activeVersion ?? 0}</strong></div>
          <div>策略 hash<strong>{snapshot?.status.desiredHash?.slice(0, 12) || '-'}</strong></div>
        </div>
        {snapshot?.status.lastError && <div className={styles.error}>{snapshot.status.lastError}</div>}
      </Card>
    </div>
  );
}
