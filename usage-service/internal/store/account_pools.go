package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const AccountPoolProviderCodex = "codex"

type AccountPool struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Provider    string `json:"provider"`
	Enabled     bool   `json:"enabled"`
	CreatedAtMS int64  `json:"createdAtMs"`
	UpdatedAtMS int64  `json:"updatedAtMs"`
}

type AccountPoolMember struct {
	PoolID   string `json:"poolId"`
	AuthID   string `json:"authId"`
	Priority int    `json:"priority"`
	Enabled  bool   `json:"enabled"`
}

type AccountPoolBinding struct {
	APIKeyHash  string `json:"apiKeyHash"`
	PoolID      string `json:"poolId"`
	UpdatedAtMS int64  `json:"updatedAtMs"`
}

type AccountPoolPolicyStatus struct {
	DesiredVersion int64  `json:"desiredVersion"`
	DesiredHash    string `json:"desiredHash"`
	AppliedVersion int64  `json:"appliedVersion"`
	AppliedHash    string `json:"appliedHash"`
	ActiveVersion  int64  `json:"activeVersion"`
	ActiveHash     string `json:"activeHash"`
	ExclusiveReady bool   `json:"exclusiveReady"`
	LastError      string `json:"lastError,omitempty"`
	UpdatedAtMS    int64  `json:"updatedAtMs"`
}

type AccountPoolPolicy struct {
	Version  int64                `json:"version"`
	Hash     string               `json:"hash"`
	Provider string               `json:"provider"`
	Pools    []AccountPool        `json:"pools"`
	Members  []AccountPoolMember  `json:"members"`
	Bindings []AccountPoolBinding `json:"bindings"`
}

type AccountPoolSnapshot struct {
	Policy AccountPoolPolicy       `json:"policy"`
	Status AccountPoolPolicyStatus `json:"status"`
}

type AccountPoolBindingUpdate struct {
	APIKeyHash string `json:"apiKeyHash"`
	PoolID     string `json:"poolId"`
}

func (s *Store) ensureAccountPoolSchema() error {
	statements := []string{
		`create table if not exists account_pools (id text primary key, name text not null, provider text not null default 'codex', enabled integer not null default 1, created_at_ms integer not null, updated_at_ms integer not null)`,
		`create table if not exists account_pool_members (pool_id text not null references account_pools(id) on delete cascade, auth_id text not null, priority integer not null default 0, enabled integer not null default 1, primary key(pool_id, auth_id))`,
		`create index if not exists idx_account_pool_members_auth_id on account_pool_members(auth_id)`,
		`create table if not exists account_pool_bindings (api_key_hash text primary key, pool_id text not null references account_pools(id) on delete cascade, updated_at_ms integer not null)`,
		`create index if not exists idx_account_pool_bindings_pool_id on account_pool_bindings(pool_id)`,
		`create table if not exists account_pool_policy_state (id integer primary key check(id = 1), desired_version integer not null default 0, desired_hash text not null default '', applied_version integer not null default 0, applied_hash text not null default '', active_version integer not null default 0, active_hash text not null default '', exclusive_ready integer not null default 0, last_error text not null default '', updated_at_ms integer not null default 0)`,
		`insert into account_pool_policy_state(id, updated_at_ms) values(1, 0) on conflict(id) do nothing`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) LoadAccountPools(ctx context.Context) ([]AccountPool, error) {
	rows, err := s.db.QueryContext(ctx, `select id, name, provider, enabled, created_at_ms, updated_at_ms from account_pools order by id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AccountPool, 0)
	for rows.Next() {
		var item AccountPool
		var enabled int
		if err := rows.Scan(&item.ID, &item.Name, &item.Provider, &enabled, &item.CreatedAtMS, &item.UpdatedAtMS); err != nil {
			return nil, err
		}
		item.Enabled = enabled != 0
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpsertAccountPool(ctx context.Context, item AccountPool) error {
	item.ID, item.Name, item.Provider = strings.TrimSpace(item.ID), strings.TrimSpace(item.Name), strings.ToLower(strings.TrimSpace(item.Provider))
	if item.ID == "" || item.Name == "" {
		return errors.New("account pool id and name are required")
	}
	if item.Provider == "" {
		item.Provider = AccountPoolProviderCodex
	}
	if item.Provider != AccountPoolProviderCodex {
		return fmt.Errorf("unsupported account pool provider %q", item.Provider)
	}
	return s.accountPoolWrite(ctx, func(tx *sql.Tx, now int64) error {
		_, err := tx.ExecContext(ctx, `insert into account_pools(id,name,provider,enabled,created_at_ms,updated_at_ms) values(?,?,?,?,?,?) on conflict(id) do update set name=excluded.name, provider=excluded.provider, enabled=excluded.enabled, updated_at_ms=excluded.updated_at_ms`, item.ID, item.Name, item.Provider, boolInt(item.Enabled), now, now)
		return err
	})
}

func (s *Store) DeleteAccountPool(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("account pool id is required")
	}
	return s.accountPoolWrite(ctx, func(tx *sql.Tx, _ int64) error {
		result, err := tx.ExecContext(ctx, `delete from account_pools where id=?`, id)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
}

func (s *Store) ReplaceAccountPoolMembers(ctx context.Context, poolID string, members []AccountPoolMember) error {
	poolID = strings.TrimSpace(poolID)
	if poolID == "" {
		return errors.New("account pool id is required")
	}
	return s.accountPoolWrite(ctx, func(tx *sql.Tx, _ int64) error {
		var exists int
		if err := tx.QueryRowContext(ctx, `select count(*) from account_pools where id=?`, poolID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return sql.ErrNoRows
		}
		if _, err := tx.ExecContext(ctx, `delete from account_pool_members where pool_id=?`, poolID); err != nil {
			return err
		}
		seen := map[string]struct{}{}
		for _, member := range members {
			authID := strings.TrimSpace(member.AuthID)
			if authID == "" {
				return errors.New("account pool member auth id is required")
			}
			if _, ok := seen[authID]; ok {
				continue
			}
			seen[authID] = struct{}{}
			if _, err := tx.ExecContext(ctx, `insert into account_pool_members(pool_id,auth_id,priority,enabled) values(?,?,?,?)`, poolID, authID, member.Priority, boolInt(member.Enabled)); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) SetAccountPoolBindings(ctx context.Context, updates []AccountPoolBindingUpdate) error {
	return s.accountPoolWrite(ctx, func(tx *sql.Tx, now int64) error {
		for _, update := range updates {
			hash := strings.ToLower(strings.TrimSpace(update.APIKeyHash))
			if hash == "" {
				return errors.New("api key hash is required")
			}
			poolID := strings.TrimSpace(update.PoolID)
			if poolID == "" {
				if _, err := tx.ExecContext(ctx, `delete from account_pool_bindings where api_key_hash=?`, hash); err != nil {
					return err
				}
				continue
			}
			var exists int
			if err := tx.QueryRowContext(ctx, `select count(*) from account_pools where id=?`, poolID).Scan(&exists); err != nil {
				return err
			}
			if exists == 0 {
				return fmt.Errorf("account pool %q not found", poolID)
			}
			if _, err := tx.ExecContext(ctx, `insert into account_pool_bindings(api_key_hash,pool_id,updated_at_ms) values(?,?,?) on conflict(api_key_hash) do update set pool_id=excluded.pool_id, updated_at_ms=excluded.updated_at_ms`, hash, poolID, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) LoadAccountPoolBindings(ctx context.Context) ([]AccountPoolBinding, error) {
	rows, err := s.db.QueryContext(ctx, `select api_key_hash,pool_id,updated_at_ms from account_pool_bindings order by api_key_hash`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AccountPoolBinding, 0)
	for rows.Next() {
		var item AccountPoolBinding
		if err := rows.Scan(&item.APIKeyHash, &item.PoolID, &item.UpdatedAtMS); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) loadAccountPoolMembers(ctx context.Context) ([]AccountPoolMember, error) {
	rows, err := s.db.QueryContext(ctx, `select pool_id,auth_id,priority,enabled from account_pool_members order by pool_id,priority asc,auth_id asc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AccountPoolMember, 0)
	for rows.Next() {
		var item AccountPoolMember
		var enabled int
		if err := rows.Scan(&item.PoolID, &item.AuthID, &item.Priority, &enabled); err != nil {
			return nil, err
		}
		item.Enabled = enabled != 0
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Store) loadAccountPoolStatus(ctx context.Context) (AccountPoolPolicyStatus, error) {
	var status AccountPoolPolicyStatus
	var ready int
	err := s.db.QueryRowContext(ctx, `select desired_version,desired_hash,applied_version,applied_hash,active_version,active_hash,exclusive_ready,last_error,updated_at_ms from account_pool_policy_state where id=1`).Scan(&status.DesiredVersion, &status.DesiredHash, &status.AppliedVersion, &status.AppliedHash, &status.ActiveVersion, &status.ActiveHash, &ready, &status.LastError, &status.UpdatedAtMS)
	status.ExclusiveReady = ready != 0
	return status, err
}
func (s *Store) LoadAccountPoolSnapshot(ctx context.Context) (AccountPoolSnapshot, error) {
	status, err := s.loadAccountPoolStatus(ctx)
	if err != nil {
		return AccountPoolSnapshot{}, err
	}
	pools, err := s.LoadAccountPools(ctx)
	if err != nil {
		return AccountPoolSnapshot{}, err
	}
	members, err := s.loadAccountPoolMembers(ctx)
	if err != nil {
		return AccountPoolSnapshot{}, err
	}
	bindings, err := s.LoadAccountPoolBindings(ctx)
	if err != nil {
		return AccountPoolSnapshot{}, err
	}
	return AccountPoolSnapshot{Policy: AccountPoolPolicy{Version: status.DesiredVersion, Hash: status.DesiredHash, Provider: AccountPoolProviderCodex, Pools: pools, Members: members, Bindings: bindings}, Status: status}, nil
}

func (s *Store) accountPoolWrite(ctx context.Context, mutate func(*sql.Tx, int64) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UnixMilli()
	if err := mutate(tx, now); err != nil {
		return err
	}
	if err := refreshAccountPoolPolicyTx(ctx, tx, now); err != nil {
		return err
	}
	return tx.Commit()
}
func refreshAccountPoolPolicyTx(ctx context.Context, tx *sql.Tx, now int64) error {
	pools, err := loadAccountPoolsTx(ctx, tx)
	if err != nil {
		return err
	}
	members, err := loadAccountPoolMembersTx(ctx, tx)
	if err != nil {
		return err
	}
	bindings, err := loadAccountPoolBindingsTx(ctx, tx)
	if err != nil {
		return err
	}
	var current int64
	if err := tx.QueryRowContext(ctx, `select desired_version from account_pool_policy_state where id=1`).Scan(&current); err != nil {
		return err
	}
	policy := AccountPoolPolicy{Version: current + 1, Provider: AccountPoolProviderCodex, Pools: pools, Members: members, Bindings: bindings}
	encoded, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(encoded)
	_, err = tx.ExecContext(ctx, `update account_pool_policy_state set desired_version=?,desired_hash=?,updated_at_ms=?,last_error='' where id=1`, policy.Version, hex.EncodeToString(digest[:]), now)
	return err
}
func loadAccountPoolsTx(ctx context.Context, tx *sql.Tx) ([]AccountPool, error) {
	rows, err := tx.QueryContext(ctx, `select id,name,provider,enabled,created_at_ms,updated_at_ms from account_pools order by id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AccountPool, 0)
	for rows.Next() {
		var item AccountPool
		var enabled int
		if err := rows.Scan(&item.ID, &item.Name, &item.Provider, &enabled, &item.CreatedAtMS, &item.UpdatedAtMS); err != nil {
			return nil, err
		}
		item.Enabled = enabled != 0
		items = append(items, item)
	}
	return items, rows.Err()
}
func loadAccountPoolMembersTx(ctx context.Context, tx *sql.Tx) ([]AccountPoolMember, error) {
	rows, err := tx.QueryContext(ctx, `select pool_id,auth_id,priority,enabled from account_pool_members order by pool_id,priority asc,auth_id asc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AccountPoolMember, 0)
	for rows.Next() {
		var item AccountPoolMember
		var enabled int
		if err := rows.Scan(&item.PoolID, &item.AuthID, &item.Priority, &enabled); err != nil {
			return nil, err
		}
		item.Enabled = enabled != 0
		items = append(items, item)
	}
	return items, rows.Err()
}
func loadAccountPoolBindingsTx(ctx context.Context, tx *sql.Tx) ([]AccountPoolBinding, error) {
	rows, err := tx.QueryContext(ctx, `select api_key_hash,pool_id,updated_at_ms from account_pool_bindings order by api_key_hash`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AccountPoolBinding, 0)
	for rows.Next() {
		var item AccountPoolBinding
		if err := rows.Scan(&item.APIKeyHash, &item.PoolID, &item.UpdatedAtMS); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) RecordAccountPoolPolicySync(ctx context.Context, desiredVersion int64, desiredHash string, success bool, syncErr string) error {
	desiredHash = strings.TrimSpace(desiredHash)
	if desiredVersion <= 0 || desiredHash == "" {
		return errors.New("desired account pool policy version and hash are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var currentVersion int64
	var currentHash string
	if err := tx.QueryRowContext(ctx, `select desired_version,desired_hash from account_pool_policy_state where id=1`).Scan(&currentVersion, &currentHash); err != nil {
		return err
	}
	if currentVersion != desiredVersion || currentHash != desiredHash {
		return fmt.Errorf("stale account pool policy publication: desired=%d/%s current=%d/%s", desiredVersion, desiredHash, currentVersion, currentHash)
	}
	now := time.Now().UnixMilli()
	if success {
		_, err = tx.ExecContext(ctx, `update account_pool_policy_state set applied_version=?,applied_hash=?,active_version=?,active_hash=?,exclusive_ready=1,last_error='',updated_at_ms=? where id=1`, desiredVersion, desiredHash, desiredVersion, desiredHash, now)
	} else {
		syncErr = strings.TrimSpace(syncErr)
		if len(syncErr) > 512 {
			syncErr = syncErr[:512]
		}
		_, err = tx.ExecContext(ctx, `update account_pool_policy_state set exclusive_ready=0,last_error=?,updated_at_ms=? where id=1`, syncErr, now)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
