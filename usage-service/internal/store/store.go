package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

type Setup struct {
	CPAUpstreamURL string `json:"cpaBaseUrl"`
	ManagementKey  string `json:"managementKey,omitempty"`
	Queue          string `json:"queue,omitempty"`
	PopSide        string `json:"popSide,omitempty"`
}

type ManagerConfig struct {
	CPAConnection        ManagerCPAConnectionConfig        `json:"cpaConnection"`
	Collector            ManagerCollectorConfig            `json:"collector"`
	ExternalUsageService ManagerExternalUsageServiceConfig `json:"externalUsageService"`
	UpdatedAtMS          int64                             `json:"updatedAtMs,omitempty"`
}

type ManagerCPAConnectionConfig struct {
	CPABaseURL    string `json:"cpaBaseUrl"`
	ManagementKey string `json:"managementKey,omitempty"`
}

type ManagerCollectorConfig struct {
	Enabled        *bool  `json:"enabled,omitempty"`
	CollectorMode  string `json:"collectorMode,omitempty"`
	Queue          string `json:"queue,omitempty"`
	PopSide        string `json:"popSide,omitempty"`
	BatchSize      int    `json:"batchSize,omitempty"`
	PollIntervalMS int    `json:"pollIntervalMs,omitempty"`
	QueryLimit     int    `json:"queryLimit,omitempty"`
	TLSSkipVerify  bool   `json:"tlsSkipVerify,omitempty"`
}

type ManagerExternalUsageServiceConfig struct {
	Enabled     bool   `json:"enabled"`
	ServiceBase string `json:"serviceBase,omitempty"`
}

type InsertResult struct {
	Inserted int `json:"inserted"`
	Skipped  int `json:"skipped"`
}

type ModelPrice struct {
	Prompt        float64 `json:"prompt"`
	Completion    float64 `json:"completion"`
	Cache         float64 `json:"cache"`
	Source        string  `json:"source,omitempty"`
	SourceModelID string  `json:"sourceModelId,omitempty"`
	RawJSON       string  `json:"rawJson,omitempty"`
	UpdatedAtMS   int64   `json:"updatedAtMs,omitempty"`
	SyncedAtMS    *int64  `json:"syncedAtMs,omitempty"`
}

type ModelPriceSyncResult struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
}

type APIKeyAlias struct {
	APIKeyHash  string `json:"apiKeyHash"`
	Alias       string `json:"alias"`
	UpdatedAtMS int64  `json:"updatedAtMs"`
}

// KeySpend holds aggregated spend (in cents) for a single API key.
type KeySpend struct {
	KeyHash    string
	TodayCents int64
	WeekCents  int64
}

// SpendLimitConfig holds the quota limits read from settings.
type SpendLimitConfig struct {
	Enabled      bool   `json:"enabled"`
	DailyCents   int64  `json:"daily_cents"`
	WeeklyCents  int64  `json:"weekly_cents"`
}

const spendLimitConfigKey = "quota_config"

const UngroupedDepartmentID = "__ungrouped__"

type EnterpriseDepartment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Prefix      string `json:"prefix"`
	SortOrder   int    `json:"sortOrder"`
	Enabled     bool   `json:"enabled"`
	System      bool   `json:"system"`
	UpdatedBy   string `json:"updatedBy,omitempty"`
	CreatedAtMS int64  `json:"createdAtMs"`
	UpdatedAtMS int64  `json:"updatedAtMs"`
}

type EnterpriseKeyBinding struct {
	APIKey               string `json:"apiKey"`
	APIKeyHash           string `json:"apiKeyHash"`
	UserName             string `json:"userName"`
	DepartmentID         string `json:"departmentId"`
	Source               string `json:"source"`
	DepartmentResolvedBy string `json:"departmentResolvedBy"`
	Email                string `json:"email"`
	UpdatedBy            string `json:"updatedBy,omitempty"`
	CreatedAtMS          int64  `json:"createdAtMs"`
	UpdatedAtMS          int64  `json:"updatedAtMs"`
}

type EnterpriseImportHistory struct {
	TaskID       string `json:"taskId"`
	CSVFileName  string `json:"csvFileName"`
	TotalRows    int    `json:"totalRows"`
	PassedRows   int    `json:"passedRows"`
	WarningRows  int    `json:"warningRows"`
	ErrorRows    int    `json:"errorRows"`
	ErrorDetails string `json:"errorDetails,omitempty"`
	Status       string `json:"status"`
	UpdatedBy    string `json:"updatedBy,omitempty"`
	CreatedAtMS  int64  `json:"createdAtMs"`
	UpdatedAtMS  int64  `json:"updatedAtMs"`
}

type Store struct {
	db *sql.DB
}

const managerConfigKey = "manager_config_v1"

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
		db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	store := &Store{db: db}
	db.SetMaxOpenConns(1)
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// PurgeEventsBefore deletes usage_events older than the given cutoff (timestamp_ms).
// Also vacuums the database to reclaim disk space.
func (s *Store) PurgeEventsBefore(ctx context.Context, cutoffMS int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `delete from usage_events where timestamp_ms < ?`, cutoffMS)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		// Checkpoint WAL so VACUUM can reclaim space in both main db and WAL
		_, _ = s.db.ExecContext(ctx, `pragma wal_checkpoint(TRUNCATE)`)
		if _, ve := s.db.ExecContext(ctx, `VACUUM`); ve != nil {
			log.Printf("cleanup: vacuum error (non-fatal): %v", ve)
		}
	}
	return n, nil
}

func (s *Store) init() error {
	statements := []string{
		`pragma journal_mode = WAL`,
		`pragma synchronous = FULL`,
		`pragma busy_timeout = 5000`,
		`pragma foreign_keys = ON`,
		`create table if not exists usage_events (
			id integer primary key autoincrement,
			request_id text,
			event_hash text not null unique,
			timestamp_ms integer not null,
			timestamp text not null,
			provider text,
			model text not null,
			endpoint text,
			method text,
			path text,
			auth_type text,
			auth_index text,
			source text,
			source_hash text,
			api_key_hash text,
			account_snapshot text,
			auth_label_snapshot text,
			auth_file_snapshot text,
			auth_provider_snapshot text,
			auth_snapshot_at_ms integer,
			input_tokens integer not null default 0,
			output_tokens integer not null default 0,
			reasoning_tokens integer not null default 0,
			cached_tokens integer not null default 0,
			cache_tokens integer not null default 0,
			total_tokens integer not null default 0,
			latency_ms integer,
			failed integer not null default 0,
			raw_json text,
			created_at_ms integer not null
		)`,
		`create index if not exists idx_usage_events_timestamp on usage_events(timestamp_ms)`,
		`create index if not exists idx_usage_events_request_id on usage_events(request_id)`,
		`create index if not exists idx_usage_events_model on usage_events(model)`,
		`create index if not exists idx_usage_events_auth_index on usage_events(auth_index)`,
		`create index if not exists idx_usage_events_endpoint on usage_events(endpoint)`,
	`create index if not exists idx_usage_events_api_key_hash on usage_events(api_key_hash)`,
		`create table if not exists dead_letter_events (
			id integer primary key autoincrement,
			payload text not null,
			error text not null,
			created_at_ms integer not null
		)`,
		`create table if not exists settings (
			key text primary key,
			value text not null,
			updated_at_ms integer not null
		)`,
		`create table if not exists model_prices (
			model text primary key,
			prompt_per_1m real not null,
			completion_per_1m real not null,
			cache_per_1m real not null,
			source text,
			source_model_id text,
			raw_json text,
			updated_at_ms integer not null,
			synced_at_ms integer
		)`,
		`create table if not exists api_key_aliases (
			api_key_hash text primary key,
			alias text not null,
			updated_at_ms integer not null
		)`,
		`create table if not exists enterprise_departments (
			id text primary key,
			name text not null,
			prefix text,
			sort_order integer not null default 0,
			enabled integer not null default 1,
			system integer not null default 0,
			updated_by text,
			created_at_ms integer not null,
			updated_at_ms integer not null
		)`,
		`create table if not exists enterprise_key_bindings (
			api_key text primary key,
			api_key_hash text not null default '',
			user_name text not null,
			department_id text not null,
			source text not null,
			department_resolved_by text not null,
			updated_by text,
			created_at_ms integer not null,
			updated_at_ms integer not null
		)`,
		`create index if not exists idx_enterprise_key_bindings_department_id on enterprise_key_bindings(department_id)`,
		`create table if not exists enterprise_import_history (
			task_id text primary key,
			total_rows integer not null,
			passed_rows integer not null,
			warning_rows integer not null,
			error_rows integer not null,
			status text not null,
			updated_by text,
			created_at_ms integer not null,
			updated_at_ms integer not null
		)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return err
		}
	}
	if err := s.ensureUsageEventSnapshotColumns(); err != nil {
		return err
	}
	if err := s.ensureEnterpriseSchema(); err != nil {
		return err
	}
	return nil
}

func (s *Store) ensureUsageEventSnapshotColumns() error {
	rows, err := s.db.Query(`pragma table_info(usage_events)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	existing := map[string]struct{}{}
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		existing[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	columns := []struct {
		name       string
		definition string
	}{
		{name: "account_snapshot", definition: "text"},
		{name: "auth_label_snapshot", definition: "text"},
		{name: "auth_file_snapshot", definition: "text"},
		{name: "auth_provider_snapshot", definition: "text"},
		{name: "auth_snapshot_at_ms", definition: "integer"},
	}
	for _, column := range columns {
		if _, ok := existing[column.name]; ok {
			continue
		}
		if _, err := s.db.Exec(fmt.Sprintf(
			`alter table usage_events add column %s %s`,
			column.name,
			column.definition,
		)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureEnterpriseSchema() error {
	bindingMigrations := []string{
		`alter table enterprise_key_bindings add column api_key_hash text not null default ''`,
		`alter table enterprise_key_bindings add column user_name text not null default ''`,
		`alter table enterprise_key_bindings add column department_id text not null default ''`,
		`alter table enterprise_key_bindings add column source text not null default ''`,
		`alter table enterprise_key_bindings add column department_resolved_by text not null default ''`,
		`alter table enterprise_key_bindings add column updated_by text`,
		`alter table enterprise_key_bindings add column created_at_ms integer not null default 0`,
		`alter table enterprise_key_bindings add column updated_at_ms integer not null default 0`,
		`alter table enterprise_key_bindings add column email text not null default ''`,
	}
	for _, stmt := range bindingMigrations {
		if _, err := s.db.Exec(stmt); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	if _, err := s.db.Exec(`create index if not exists idx_enterprise_key_bindings_api_key_hash on enterprise_key_bindings(api_key_hash)`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`create index if not exists idx_enterprise_key_bindings_user_name on enterprise_key_bindings(user_name, updated_at_ms)`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`create index if not exists idx_enterprise_key_bindings_department_id on enterprise_key_bindings(department_id)`); err != nil {
		return err
	}
	importHistoryMigrations := []string{
		`alter table enterprise_import_history add column total_rows integer not null default 0`,
		`alter table enterprise_import_history add column passed_rows integer not null default 0`,
		`alter table enterprise_import_history add column warning_rows integer not null default 0`,
		`alter table enterprise_import_history add column error_rows integer not null default 0`,
		`alter table enterprise_import_history add column status text not null default ''`,
		`alter table enterprise_import_history add column updated_by text`,
		`alter table enterprise_import_history add column created_at_ms integer not null default 0`,
		`alter table enterprise_import_history add column updated_at_ms integer not null default 0`,
		`alter table enterprise_import_history add column csv_filename text not null default ''`,
		`alter table enterprise_import_history add column error_details text not null default ''`,
	}
	for _, stmt := range importHistoryMigrations {
		if _, err := s.db.Exec(stmt); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	now := time.Now().UnixMilli()
	if _, err := s.db.Exec(`insert into enterprise_departments(
		id, name, prefix, sort_order, enabled, system, created_at_ms, updated_at_ms
	) values(?, ?, ?, ?, ?, ?, ?, ?)
	on conflict(id) do update set name=excluded.name, enabled=excluded.enabled, system=excluded.system, updated_at_ms=excluded.updated_at_ms`,
		UngroupedDepartmentID, "未分组", "", -1, 1, 1, now, now,
	); err != nil {
		return err
	}
	if _, err := s.db.Exec(`
		update enterprise_key_bindings
		set department_id = ?, updated_at_ms = ?
		where department_id = ''
		   or department_id not in (select id from enterprise_departments)
	`, UngroupedDepartmentID, now); err != nil {
		return err
	}
	if _, err := s.db.Exec(`
			update enterprise_key_bindings
		set user_name = '', updated_at_ms = ?
		where source = 'sync' and user_name like 'synced_%'
	`, now); err != nil {
		return err
	}
	return nil
}

func (s *Store) SaveSetup(ctx context.Context, setup Setup) error {
	if setup.CPAUpstreamURL == "" || setup.ManagementKey == "" {
		return errors.New("cpaBaseUrl and managementKey are required")
	}
	data, err := json.Marshal(setup)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(
		ctx,
		`insert into settings(key, value, updated_at_ms)
		 values('setup', ?, ?)
		 on conflict(key) do update set value = excluded.value, updated_at_ms = excluded.updated_at_ms`,
		string(data),
		time.Now().UnixMilli(),
	)
	return err
}

func (s *Store) LoadSetup(ctx context.Context) (Setup, bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `select value from settings where key = 'setup'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Setup{}, false, nil
	}
	if err != nil {
		return Setup{}, false, err
	}
	var setup Setup
	if err := json.Unmarshal([]byte(raw), &setup); err != nil {
		return Setup{}, false, err
	}
	return setup, true, nil
}

func (s *Store) SaveManagerConfig(ctx context.Context, cfg ManagerConfig) error {
	cfg.UpdatedAtMS = time.Now().UnixMilli()
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(
		ctx,
		`insert into settings(key, value, updated_at_ms)
		 values(?, ?, ?)
		 on conflict(key) do update set value = excluded.value, updated_at_ms = excluded.updated_at_ms`,
		managerConfigKey,
		string(data),
		cfg.UpdatedAtMS,
	)
	return err
}

func (s *Store) LoadManagerConfig(ctx context.Context) (ManagerConfig, bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `select value from settings where key = ?`, managerConfigKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ManagerConfig{}, false, nil
	}
	if err != nil {
		return ManagerConfig{}, false, err
	}
	var cfg ManagerConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return ManagerConfig{}, false, err
	}
	return cfg, true, nil
}

func (s *Store) LoadModelPrices(ctx context.Context) (map[string]ModelPrice, error) {
	rows, err := s.db.QueryContext(ctx, `select
		model, prompt_per_1m, completion_per_1m, cache_per_1m, source, source_model_id, raw_json,
		updated_at_ms, synced_at_ms
		from model_prices order by model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	prices := map[string]ModelPrice{}
	for rows.Next() {
		var model string
		var price ModelPrice
		var source, sourceModelID, rawJSON sql.NullString
		var syncedAt sql.NullInt64
		if err := rows.Scan(
			&model,
			&price.Prompt,
			&price.Completion,
			&price.Cache,
			&source,
			&sourceModelID,
			&rawJSON,
			&price.UpdatedAtMS,
			&syncedAt,
		); err != nil {
			return nil, err
		}
		price.Source = source.String
		price.SourceModelID = sourceModelID.String
		price.RawJSON = rawJSON.String
		if syncedAt.Valid {
			value := syncedAt.Int64
			price.SyncedAtMS = &value
		}
		prices[model] = price
	}
	return prices, rows.Err()
}

func (s *Store) SaveModelPrices(ctx context.Context, prices map[string]ModelPrice) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, `delete from model_prices`); err != nil {
		return err
	}
	if len(prices) == 0 {
		return tx.Commit()
	}

	stmt, err := tx.PrepareContext(ctx, `insert into model_prices (
		model, prompt_per_1m, completion_per_1m, cache_per_1m, source, source_model_id,
		raw_json, updated_at_ms, synced_at_ms
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UnixMilli()
	for model, price := range prices {
		if err := validateModelPrice(model, price); err != nil {
			return err
		}
		if _, err := stmt.ExecContext(
			ctx,
			model,
			price.Prompt,
			price.Completion,
			price.Cache,
			nullString(price.Source),
			nullString(price.SourceModelID),
			nullString(price.RawJSON),
			now,
			nullInt(price.SyncedAtMS),
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) UpsertSyncedModelPrices(ctx context.Context, prices map[string]ModelPrice) (ModelPriceSyncResult, error) {
	if len(prices) == 0 {
		return ModelPriceSyncResult{}, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ModelPriceSyncResult{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	stmt, err := tx.PrepareContext(ctx, `insert into model_prices (
		model, prompt_per_1m, completion_per_1m, cache_per_1m, source, source_model_id,
		raw_json, updated_at_ms, synced_at_ms
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?)
	on conflict(model) do update set
		prompt_per_1m = excluded.prompt_per_1m,
		completion_per_1m = excluded.completion_per_1m,
		cache_per_1m = excluded.cache_per_1m,
		source = excluded.source,
		source_model_id = excluded.source_model_id,
		raw_json = excluded.raw_json,
		updated_at_ms = excluded.updated_at_ms,
		synced_at_ms = excluded.synced_at_ms`)
	if err != nil {
		return ModelPriceSyncResult{}, err
	}
	defer stmt.Close()

	now := time.Now().UnixMilli()
	result := ModelPriceSyncResult{}
	for model, price := range prices {
		if err := validateModelPrice(model, price); err != nil {
			result.Skipped++
			continue
		}
		if price.Source == "" {
			price.Source = "sync"
		}
		if price.SourceModelID == "" {
			price.SourceModelID = model
		}
		price.UpdatedAtMS = now
		price.SyncedAtMS = &now
		if _, err := stmt.ExecContext(
			ctx,
			model,
			price.Prompt,
			price.Completion,
			price.Cache,
			nullString(price.Source),
			nullString(price.SourceModelID),
			nullString(price.RawJSON),
			now,
			now,
		); err != nil {
			return ModelPriceSyncResult{}, err
		}
		result.Imported++
	}
	if err := tx.Commit(); err != nil {
		return ModelPriceSyncResult{}, err
	}
	return result, nil
}

func (s *Store) LoadAPIKeyAliases(ctx context.Context) ([]APIKeyAlias, error) {
	rows, err := s.db.QueryContext(ctx, `select api_key_hash, alias, updated_at_ms
		from api_key_aliases
		order by alias collate nocase, api_key_hash`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	aliases := []APIKeyAlias{}
	for rows.Next() {
		var alias APIKeyAlias
		if err := rows.Scan(&alias.APIKeyHash, &alias.Alias, &alias.UpdatedAtMS); err != nil {
			return nil, err
		}
		aliases = append(aliases, alias)
	}
	return aliases, rows.Err()
}

func (s *Store) UpsertAPIKeyAliases(ctx context.Context, aliases []APIKeyAlias) error {
	if len(aliases) == 0 {
		return nil
	}
	now := time.Now().UnixMilli()
	normalizedAliases := make([]APIKeyAlias, 0, len(aliases))
	seenAliases := map[string]string{}
	for _, alias := range aliases {
		normalized, err := normalizeAPIKeyAlias(alias, now)
		if err != nil {
			return err
		}
		aliasKey := normalizeAPIKeyAliasUniqueKey(normalized.Alias)
		if existingHash, ok := seenAliases[aliasKey]; ok && existingHash != normalized.APIKeyHash {
			return errors.New("api key alias already exists")
		}
		seenAliases[aliasKey] = normalized.APIKeyHash
		normalizedAliases = append(normalizedAliases, normalized)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	stmt, err := tx.PrepareContext(ctx, `insert into api_key_aliases (
		api_key_hash, alias, updated_at_ms
	) values (?, ?, ?)
	on conflict(api_key_hash) do update set
		alias = excluded.alias,
		updated_at_ms = excluded.updated_at_ms`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	existingRows, err := tx.QueryContext(ctx, `select api_key_hash, alias from api_key_aliases`)
	if err != nil {
		return err
	}
	existingAliases := map[string]string{}
	for existingRows.Next() {
		var apiKeyHash string
		var alias string
		if err := existingRows.Scan(&apiKeyHash, &alias); err != nil {
			_ = existingRows.Close()
			return err
		}
		existingAliases[normalizeAPIKeyAliasUniqueKey(alias)] = apiKeyHash
	}
	if err := existingRows.Close(); err != nil {
		return err
	}
	if err := existingRows.Err(); err != nil {
		return err
	}

	for _, normalized := range normalizedAliases {
		aliasKey := normalizeAPIKeyAliasUniqueKey(normalized.Alias)
		if existingHash, ok := existingAliases[aliasKey]; ok && existingHash != normalized.APIKeyHash {
			return errors.New("api key alias already exists")
		}
		if _, err := stmt.ExecContext(
			ctx,
			normalized.APIKeyHash,
			normalized.Alias,
			normalized.UpdatedAtMS,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteAPIKeyAlias(ctx context.Context, apiKeyHash string) error {
	hash := strings.ToLower(strings.TrimSpace(apiKeyHash))
	if !validAPIKeyHash(hash) {
		return errors.New("valid apiKeyHash is required")
	}
	_, err := s.db.ExecContext(ctx, `delete from api_key_aliases where api_key_hash = ?`, hash)
	return err
}

func normalizeAPIKeyAlias(alias APIKeyAlias, now int64) (APIKeyAlias, error) {
	hash := strings.ToLower(strings.TrimSpace(alias.APIKeyHash))
	if !validAPIKeyHash(hash) {
		return APIKeyAlias{}, errors.New("valid apiKeyHash is required")
	}
	label := strings.TrimSpace(alias.Alias)
	if label == "" {
		return APIKeyAlias{}, errors.New("alias is required")
	}
	if len([]rune(label)) > 120 {
		return APIKeyAlias{}, errors.New("alias must be 120 characters or less")
	}
	if alias.UpdatedAtMS <= 0 {
		alias.UpdatedAtMS = now
	}
	alias.APIKeyHash = hash
	alias.Alias = label
	return alias, nil
}

func normalizeAPIKeyAliasUniqueKey(alias string) string {
	return strings.ToLower(strings.TrimSpace(alias))
}

func validAPIKeyHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') {
			continue
		}
		return false
	}
	return true
}

func validateModelPrice(model string, price ModelPrice) error {
	if model == "" {
		return errors.New("model is required")
	}
	if !validPriceValue(price.Prompt) || !validPriceValue(price.Completion) || !validPriceValue(price.Cache) {
		return fmt.Errorf("invalid model price for %s", model)
	}
	return nil
}

func validPriceValue(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (s *Store) InsertEvents(ctx context.Context, events []usage.Event) (InsertResult, error) {
	if len(events) == 0 {
		return InsertResult{}, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return InsertResult{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	stmt, err := tx.PrepareContext(ctx, `insert or ignore into usage_events (
		request_id, event_hash, timestamp_ms, timestamp, provider, model, endpoint, method, path,
		auth_type, auth_index, source, source_hash, api_key_hash,
		account_snapshot, auth_label_snapshot, auth_file_snapshot, auth_provider_snapshot, auth_snapshot_at_ms,
		input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_tokens, total_tokens,
		latency_ms, failed, raw_json, created_at_ms
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return InsertResult{}, err
	}
	defer stmt.Close()

	result := InsertResult{}
	for _, event := range events {
		failed := 0
		if event.Failed {
			failed = 1
		}
		res, err := stmt.ExecContext(
			ctx,
			nullString(event.RequestID),
			event.EventHash,
			event.TimestampMS,
			event.Timestamp,
			nullString(event.Provider),
			event.Model,
			nullString(event.Endpoint),
			nullString(event.Method),
			nullString(event.Path),
			nullString(event.AuthType),
			nullString(event.AuthIndex),
			nullString(event.Source),
			nullString(event.SourceHash),
			nullString(event.APIKeyHash),
			nullString(event.AccountSnapshot),
			nullString(event.AuthLabelSnapshot),
			nullString(event.AuthFileSnapshot),
			nullString(event.AuthProviderSnapshot),
			nullPositiveInt64(event.AuthSnapshotAtMS),
			event.InputTokens,
			event.OutputTokens,
			event.ReasoningTokens,
			event.CachedTokens,
			event.CacheTokens,
			event.TotalTokens,
			nullInt(event.LatencyMS),
			failed,
			nullString(event.RawJSON),
			event.CreatedAtMS,
		)
		if err != nil {
			return InsertResult{}, err
		}
		affected, _ := res.RowsAffected()
		if affected > 0 {
			result.Inserted++
		} else {
			result.Skipped++
		}
	}
	if err := tx.Commit(); err != nil {
		return InsertResult{}, err
	}
	return result, nil
}

func (s *Store) AddDeadLetter(ctx context.Context, payload string, parseErr error) error {
	_, err := s.db.ExecContext(
		ctx,
		`insert into dead_letter_events(payload, error, created_at_ms) values(?, ?, ?)`,
		payload,
		parseErr.Error(),
		time.Now().UnixMilli(),
	)
	return err
}

func (s *Store) RecentEvents(ctx context.Context, limit int) ([]usage.Event, error) {
	return s.RecentEventsFiltered(ctx, limit, nil, nil, "")
}

func (s *Store) RecentEventsFiltered(
	ctx context.Context,
	limit int,
	fromMS *int64,
	toMS *int64,
	apiKeyHash string,
) ([]usage.Event, error) {
	if limit <= 0 {
		limit = 50000
	}
	query := `select
		request_id, event_hash, timestamp_ms, timestamp, provider, model, endpoint, method, path,
		auth_type, auth_index, source, source_hash, api_key_hash,
		account_snapshot, auth_label_snapshot, auth_file_snapshot, auth_provider_snapshot, auth_snapshot_at_ms,
		input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_tokens, total_tokens,
		latency_ms, failed, raw_json, created_at_ms
		from usage_events`
	args := make([]any, 0, 4)
	where := make([]string, 0, 3)
	if fromMS != nil {
		where = append(where, "timestamp_ms >= ?")
		args = append(args, *fromMS)
	}
	if toMS != nil {
		where = append(where, "timestamp_ms <= ?")
		args = append(args, *toMS)
	}
	if strings.TrimSpace(apiKeyHash) != "" {
		where = append(where, "lower(api_key_hash) = ?")
		args = append(args, strings.ToLower(strings.TrimSpace(apiKeyHash)))
	}
	if len(where) > 0 {
		query += " where " + strings.Join(where, " and ")
	}
	query += " order by timestamp_ms desc, id desc limit ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]usage.Event, 0)
	for rows.Next() {
		var event usage.Event
		var requestID, provider, endpoint, method, path, authType, authIndex, source, sourceHash, apiKeyHash, accountSnapshot, authLabelSnapshot, authFileSnapshot, authProviderSnapshot, rawJSON sql.NullString
		var authSnapshotAt sql.NullInt64
		var latency sql.NullInt64
		var failed int
		if err := rows.Scan(
			&requestID,
			&event.EventHash,
			&event.TimestampMS,
			&event.Timestamp,
			&provider,
			&event.Model,
			&endpoint,
			&method,
			&path,
			&authType,
			&authIndex,
			&source,
			&sourceHash,
			&apiKeyHash,
			&accountSnapshot,
			&authLabelSnapshot,
			&authFileSnapshot,
			&authProviderSnapshot,
			&authSnapshotAt,
			&event.InputTokens,
			&event.OutputTokens,
			&event.ReasoningTokens,
			&event.CachedTokens,
			&event.CacheTokens,
			&event.TotalTokens,
			&latency,
			&failed,
			&rawJSON,
			&event.CreatedAtMS,
		); err != nil {
			return nil, err
		}
		event.RequestID = requestID.String
		event.Provider = provider.String
		event.Endpoint = endpoint.String
		event.Method = method.String
		event.Path = path.String
		event.AuthType = authType.String
		event.AuthIndex = authIndex.String
		event.Source = source.String
		event.SourceHash = sourceHash.String
		event.APIKeyHash = apiKeyHash.String
		event.AccountSnapshot = accountSnapshot.String
		event.AuthLabelSnapshot = authLabelSnapshot.String
		event.AuthFileSnapshot = authFileSnapshot.String
		event.AuthProviderSnapshot = authProviderSnapshot.String
		if authSnapshotAt.Valid {
			event.AuthSnapshotAtMS = authSnapshotAt.Int64
		}
		event.RawJSON = rawJSON.String
		event.Failed = failed != 0
		if latency.Valid {
			value := latency.Int64
			event.LatencyMS = &value
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) Counts(ctx context.Context) (events int64, deadLetters int64, err error) {
	if err = s.db.QueryRowContext(ctx, `select count(*) from usage_events`).Scan(&events); err != nil {
		return 0, 0, err
	}
	if err = s.db.QueryRowContext(ctx, `select count(*) from dead_letter_events`).Scan(&deadLetters); err != nil {
		return 0, 0, err
	}
	return events, deadLetters, nil
}

func (s *Store) ExportJSONL(ctx context.Context) ([]byte, error) {
	events, err := s.RecentEvents(ctx, 0)
	if err != nil {
		return nil, err
	}
	output := make([]byte, 0)
	for i := len(events) - 1; i >= 0; i-- {
		line, err := json.Marshal(events[i])
		if err != nil {
			return nil, err
		}
		output = append(output, line...)
		output = append(output, '\n')
	}
	return output, nil
}

func (s *Store) UpsertEnterpriseDepartments(ctx context.Context, items []EnterpriseDepartment) error {
	if len(items) == 0 {
		return nil
	}
	now := time.Now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	stmt, err := tx.PrepareContext(ctx, `insert into enterprise_departments (
		id, name, prefix, sort_order, enabled, system, updated_by, created_at_ms, updated_at_ms
	) values (?, ?, ?, ?, ?, ?, ?, ?, ?)
	on conflict(id) do update set
		name=excluded.name,
		prefix=excluded.prefix,
		sort_order=excluded.sort_order,
		enabled=excluded.enabled,
		system=excluded.system,
		updated_by=excluded.updated_by,
		updated_at_ms=excluded.updated_at_ms`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		name := strings.TrimSpace(item.Name)
		if id == "" || name == "" {
			return errors.New("department id and name are required")
		}
		createdAt := item.CreatedAtMS
		if createdAt <= 0 {
			createdAt = now
		}
		updatedAt := item.UpdatedAtMS
		if updatedAt <= 0 {
			updatedAt = now
		}
		enabled := 0
		if item.Enabled {
			enabled = 1
		}
		system := 0
		if item.System {
			system = 1
		}
		if _, err := stmt.ExecContext(ctx, id, name, strings.TrimSpace(item.Prefix), item.SortOrder, enabled, system, nullString(strings.TrimSpace(item.UpdatedBy)), createdAt, updatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) LoadEnterpriseDepartments(ctx context.Context) ([]EnterpriseDepartment, error) {
	rows, err := s.db.QueryContext(ctx, `select id, name, prefix, sort_order, enabled, system, updated_by, created_at_ms, updated_at_ms
		from enterprise_departments order by sort_order asc, id asc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]EnterpriseDepartment, 0)
	for rows.Next() {
		var item EnterpriseDepartment
		var enabled int
		var system int
		var prefix sql.NullString
		var updatedBy sql.NullString
		if err := rows.Scan(&item.ID, &item.Name, &prefix, &item.SortOrder, &enabled, &system, &updatedBy, &item.CreatedAtMS, &item.UpdatedAtMS); err != nil {
			return nil, err
		}
		item.Prefix = prefix.String
		item.Enabled = enabled != 0
		item.System = system != 0
		item.UpdatedBy = updatedBy.String
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DeleteEnterpriseDepartment(ctx context.Context, id string) error {
	departmentID := strings.TrimSpace(id)
	if departmentID == "" {
		return errors.New("department id is required")
	}
	now := time.Now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `update enterprise_key_bindings set department_id = ?, updated_at_ms = ? where department_id = ?`, UngroupedDepartmentID, now, departmentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `delete from enterprise_departments where id = ?`, departmentID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpsertEnterpriseKeyBindings(ctx context.Context, items []EnterpriseKeyBinding) error {
	if len(items) == 0 {
		return nil
	}
	now := time.Now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, `insert into enterprise_key_bindings (
		api_key, api_key_hash, user_name, department_id, source, department_resolved_by, email, updated_by, created_at_ms, updated_at_ms
	) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	on conflict(api_key) do update set
		api_key_hash=excluded.api_key_hash,
		user_name=excluded.user_name,
		department_id=excluded.department_id,
		source=excluded.source,
		department_resolved_by=excluded.department_resolved_by,
			email=excluded.email,
		updated_by=excluded.updated_by,
		updated_at_ms=excluded.updated_at_ms`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, item := range items {
		apiKey := strings.TrimSpace(item.APIKey)
		if apiKey == "" {
			return errors.New("apiKey is required")
		}
		source := strings.TrimSpace(item.Source)
		if source == "" {
			source = "manual"
		}
		userName := strings.TrimSpace(item.UserName)
		deptID := strings.TrimSpace(item.DepartmentID)
		if deptID == "" {
			if source == "manual" {
				return errors.New("departmentId is required")
			}
			deptID = UngroupedDepartmentID
		}
		if source == "manual" {
			if userName == "" {
				return errors.New("userName is required")
			}
		}
		resolvedBy := strings.TrimSpace(item.DepartmentResolvedBy)
		if resolvedBy == "" {
			resolvedBy = "manual"
		}
		createdAt := item.CreatedAtMS
		if createdAt <= 0 {
			createdAt = now
		}
		updatedAt := item.UpdatedAtMS
		if updatedAt <= 0 {
			updatedAt = now
		}
		sum := sha256.Sum256([]byte(apiKey))
		apiKeyHash := fmt.Sprintf("%x", sum)
		if _, err := stmt.ExecContext(ctx, apiKey, apiKeyHash, userName, deptID, source, resolvedBy, strings.TrimSpace(item.Email), nullString(strings.TrimSpace(item.UpdatedBy)), createdAt, updatedAt); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `insert into api_key_aliases(api_key_hash, alias, updated_at_ms)
			values(?, ?, ?)
			on conflict(api_key_hash) do update set alias=excluded.alias, updated_at_ms=excluded.updated_at_ms`, apiKeyHash, userName, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) LoadEnterpriseKeyBindings(ctx context.Context) ([]EnterpriseKeyBinding, error) {
	rows, err := s.db.QueryContext(ctx, `select api_key, api_key_hash, user_name, department_id, source, department_resolved_by, email, updated_by, created_at_ms, updated_at_ms
		from enterprise_key_bindings order by updated_at_ms desc, api_key asc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]EnterpriseKeyBinding, 0)
	for rows.Next() {
		var item EnterpriseKeyBinding
		var updatedBy sql.NullString
		if err := rows.Scan(&item.APIKey, &item.APIKeyHash, &item.UserName, &item.DepartmentID, &item.Source, &item.DepartmentResolvedBy, &item.Email, &updatedBy, &item.CreatedAtMS, &item.UpdatedAtMS); err != nil {
			return nil, err
		}
		item.UpdatedBy = updatedBy.String
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DeleteEnterpriseKeyBinding(ctx context.Context, apiKey string) error {
	trimmed := strings.TrimSpace(apiKey)
	if trimmed == "" {
		return errors.New("apiKey is required")
	}
	sum := sha256.Sum256([]byte(trimmed))
	hash := fmt.Sprintf("%x", sum)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `delete from enterprise_key_bindings where api_key = ?`, trimmed); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `delete from api_key_aliases where api_key_hash = ?`, hash); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteEnterpriseKeyBindings(ctx context.Context, apiKeys []string) error {
	if len(apiKeys) == 0 {
		return errors.New("apiKeys are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, apiKey := range apiKeys {
		trimmed := strings.TrimSpace(apiKey)
		if trimmed == "" {
			return errors.New("apiKey is required")
		}
		sum := sha256.Sum256([]byte(trimmed))
		hash := fmt.Sprintf("%x", sum)
		if _, err := tx.ExecContext(ctx, `delete from enterprise_key_bindings where api_key = ?`, trimmed); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `delete from api_key_aliases where api_key_hash = ?`, hash); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) AppendEnterpriseImportHistory(ctx context.Context, item EnterpriseImportHistory) error {
	now := time.Now().UnixMilli()
	createdAt := item.CreatedAtMS
	if createdAt <= 0 {
		createdAt = now
	}
	updatedAt := item.UpdatedAtMS
	if updatedAt <= 0 {
		updatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `insert into enterprise_import_history(
		task_id, csv_filename, total_rows, passed_rows, warning_rows, error_rows, error_details, status, updated_by, created_at_ms, updated_at_ms
	) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	on conflict(task_id) do update set
		csv_filename=excluded.csv_filename,
		total_rows=excluded.total_rows,
		passed_rows=excluded.passed_rows,
		warning_rows=excluded.warning_rows,
		error_rows=excluded.error_rows,
		error_details=excluded.error_details,
		status=excluded.status,
		updated_by=excluded.updated_by,
		updated_at_ms=excluded.updated_at_ms`,
		strings.TrimSpace(item.TaskID), strings.TrimSpace(item.CSVFileName), item.TotalRows, item.PassedRows, item.WarningRows, item.ErrorRows, strings.TrimSpace(item.ErrorDetails), strings.TrimSpace(item.Status), nullString(strings.TrimSpace(item.UpdatedBy)), createdAt, updatedAt)
	return err
}

func (s *Store) LoadEnterpriseImportHistory(ctx context.Context, limit int) ([]EnterpriseImportHistory, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `select task_id, csv_filename, total_rows, passed_rows, warning_rows, error_rows, error_details, status, updated_by, created_at_ms, updated_at_ms
		from enterprise_import_history order by updated_at_ms desc, task_id desc limit ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]EnterpriseImportHistory, 0)
	for rows.Next() {
		var item EnterpriseImportHistory
		var updatedBy sql.NullString
		if err := rows.Scan(&item.TaskID, &item.CSVFileName, &item.TotalRows, &item.PassedRows, &item.WarningRows, &item.ErrorRows, &item.ErrorDetails, &item.Status, &updatedBy, &item.CreatedAtMS, &item.UpdatedAtMS); err != nil {
			return nil, err
		}
		item.UpdatedBy = updatedBy.String
		items = append(items, item)
	}
	return items, rows.Err()
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullInt(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullPositiveInt64(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func (s Setup) String() string {
	return fmt.Sprintf("upstream=%s queue=%s popSide=%s", s.CPAUpstreamURL, s.Queue, s.PopSide)
}

// UsageReportModel contains per-model aggregated statistics.
type UsageReportModel struct {
	Model            string  `json:"model"`
	TotalTokens      int64   `json:"totalTokens"`
	Requests         int64   `json:"requests"`
	FailedRequests   int64   `json:"failedRequests"`
	CachedTokens     int64   `json:"cachedTokens"`
	TotalCacheTokens int64   `json:"totalCacheTokens"`
	CacheHitRate     float64 `json:"cacheHitRate"`
}

// UsageReportKeyRow contains per-api-key aggregated data with nested models.
type UsageReportKeyRow struct {
	APIKey           string             `json:"apiKey"`
	UserName         string             `json:"userName"`
	DepartmentID     string             `json:"departmentId"`
	DepartmentName   string             `json:"departmentName"`
	Email            string             `json:"email"`
	TotalTokens      int64              `json:"totalTokens"`
	TotalRequests    int64              `json:"totalRequests"`
	FailedRequests   int64              `json:"failedRequests"`
	CachedTokens     int64              `json:"cachedTokens"`
	TotalCacheTokens int64              `json:"totalCacheTokens"`
	CacheHitRate     float64            `json:"cacheHitRate"`
	Models           []UsageReportModel `json:"models"`
}

// UsageReport aggregates usage event data by api_key_hash and model over a
// [fromMS, toMS] timestamp range. It joins enterprise_key_bindings and
// enterprise_departments to expose user/dept/email fields. Results are sorted
// by department_name, user_name.
func (s *Store) UsageReport(ctx context.Context, fromMS, toMS int64) ([]UsageReportKeyRow, error) {
	query := `
		select
			ue.api_key_hash,

				coalesce(ekb.api_key, ''),
				ue.model,
			coalesce(ekb.user_name, ''),
			coalesce(ekb.department_id, ''),
			coalesce(ed.name, ''),
			coalesce(ekb.email, ''),
			coalesce(sum(ue.total_tokens), 0),
			count(*),
			coalesce(sum(ue.failed), 0),
			coalesce(sum(ue.cached_tokens), 0),
			coalesce(sum(ue.cache_tokens), 0),
			coalesce(sum(case when ue.cached_tokens > 0 then 1 else 0 end), 0)
		from usage_events ue
		left join enterprise_key_bindings ekb on ue.api_key_hash = ekb.api_key_hash
		left join enterprise_departments ed on ekb.department_id = ed.id
		where ue.timestamp_ms >= ? and ue.timestamp_ms <= ?
		group by ue.api_key_hash, ue.model, ekb.user_name, ekb.department_id, ed.name, ekb.email, ekb.api_key
		order by ed.name, ekb.user_name
	`
	rows, err := s.db.QueryContext(ctx, query, fromMS, toMS)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type flatRow struct {
		apiKeyHash       string
			apiKey           string
		model            string
		userName         string
		departmentID     string
		departmentName   string
		email            string
		totalTokens      int64
		totalRequests    int64
		failedRequests   int64
		cachedTokens     int64
		totalCacheTokens int64
		cacheHits        int64
	}
	var flats []flatRow
	for rows.Next() {
		var f flatRow
		if err := rows.Scan(
				&f.apiKeyHash,
				&f.apiKey,
				&f.model,
				&f.userName,
				&f.departmentID,
				&f.departmentName,
				&f.email,
				&f.totalTokens,
				&f.totalRequests,
				&f.failedRequests,
				&f.cachedTokens,
				&f.totalCacheTokens,
				&f.cacheHits,
			); err != nil {
				return nil, err
			}
			flats = append(flats, f)
		}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Group flat rows by api_key_hash into the nested key-row structure.
	type keyAccum struct {
		row        UsageReportKeyRow
		cacheHits  int64
	}
	keyIndex := map[string]int{}
	accums := make([]keyAccum, 0)
	for _, f := range flats {
		idx, ok := keyIndex[f.apiKeyHash]
		if !ok {
			idx = len(accums)
			keyIndex[f.apiKeyHash] = idx
			accums = append(accums, keyAccum{
				row: UsageReportKeyRow{
					APIKey:         f.apiKey,
					UserName:       f.userName,
					DepartmentID:   f.departmentID,
					DepartmentName: f.departmentName,
					Email:          f.email,
				},
			})
		}
		modelCacheHitRate := 0.0
		if f.totalRequests > 0 {
			modelCacheHitRate = float64(f.cacheHits) / float64(f.totalRequests)
		}
		modelEntry := UsageReportModel{
			Model:            f.model,
			TotalTokens:      f.totalTokens,
			Requests:         f.totalRequests,
			FailedRequests:   f.failedRequests,
			CachedTokens:     f.cachedTokens,
			TotalCacheTokens: f.totalCacheTokens,
			CacheHitRate:     modelCacheHitRate,
		}
		accums[idx].row.Models = append(accums[idx].row.Models, modelEntry)
		accums[idx].row.TotalTokens += f.totalTokens
		accums[idx].row.TotalRequests += f.totalRequests
		accums[idx].row.FailedRequests += f.failedRequests
		accums[idx].row.CachedTokens += f.cachedTokens
		accums[idx].row.TotalCacheTokens += f.totalCacheTokens
		accums[idx].cacheHits += f.cacheHits
	}

	// Compute per-key cache hit rate and return final rows.
	result := make([]UsageReportKeyRow, len(accums))
	for i, a := range accums {
		result[i] = a.row
		if a.row.TotalRequests > 0 {
			result[i].CacheHitRate = float64(a.cacheHits) / float64(a.row.TotalRequests)
		}
	}
	return result, nil
}


// SaveSpendLimitConfig persists the quota limit config to the settings table.
func (s *Store) SaveSpendLimitConfig(ctx context.Context, cfg SpendLimitConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`insert into settings(key, value, updated_at_ms)
		 values(?, ?, ?)
		 on conflict(key) do update set value = excluded.value, updated_at_ms = excluded.updated_at_ms`,
		spendLimitConfigKey, string(data), time.Now().UnixMilli(),
	)
	return err
}

// LoadSpendLimitConfig reads the quota limit config from the settings table.
// Returns (cfg, false, nil) when no config is stored.
func (s *Store) LoadSpendLimitConfig(ctx context.Context) (SpendLimitConfig, bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `select value from settings where key = ?`, spendLimitConfigKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return SpendLimitConfig{}, false, nil
	}
	if err != nil {
		return SpendLimitConfig{}, false, err
	}
	var cfg SpendLimitConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return SpendLimitConfig{}, false, err
	}
	return cfg, true, nil
}
// It joins usage_events with model_prices to compute cost = tokens * price_per_1m / 1000000 * 100.
func (s *Store) QueryKeySpend(ctx context.Context) ([]KeySpend, error) {
	todayQuery := `
		select ue.api_key_hash,
			coalesce(round(sum(
				cast(ue.input_tokens as real) * coalesce(mp.prompt_per_1m, 0)
				+ cast(ue.output_tokens as real) * coalesce(mp.completion_per_1m, 0)
				+ cast(ue.cached_tokens as real) * coalesce(mp.cache_per_1m, 0)
			) / 1000000.0 * 100), 0) as today_cents
		from usage_events ue
		left join model_prices mp on ue.model = mp.model
		where ue.api_key_hash != ''
		  and date(ue.timestamp_ms / 1000, 'unixepoch') = date('now')
		group by ue.api_key_hash
	`
	weekQuery := `
		select ue.api_key_hash,
			coalesce(round(sum(
				cast(ue.input_tokens as real) * coalesce(mp.prompt_per_1m, 0)
				+ cast(ue.output_tokens as real) * coalesce(mp.completion_per_1m, 0)
				+ cast(ue.cached_tokens as real) * coalesce(mp.cache_per_1m, 0)
			) / 1000000.0 * 100), 0) as week_cents
		from usage_events ue
		left join model_prices mp on ue.model = mp.model
		where ue.api_key_hash != ''
		  and strftime('%W', ue.timestamp_ms / 1000, 'unixepoch') = strftime('%W', 'now')
		group by ue.api_key_hash
	`

	todayMap := make(map[string]int64)
	todayRows, err := s.db.QueryContext(ctx, todayQuery)
	if err != nil {
		return nil, fmt.Errorf("query today spend: %w", err)
	}
	for todayRows.Next() {
		var hash string
		var cents int64
		if err := todayRows.Scan(&hash, &cents); err != nil {
			todayRows.Close()
			return nil, err
		}
		todayMap[hash] = cents
	}
	todayRows.Close()

	weekMap := make(map[string]int64)
	weekRows, err := s.db.QueryContext(ctx, weekQuery)
	if err != nil {
		return nil, fmt.Errorf("query week spend: %w", err)
	}
	for weekRows.Next() {
		var hash string
		var cents int64
		if err := weekRows.Scan(&hash, &cents); err != nil {
			weekRows.Close()
			return nil, err
		}
		weekMap[hash] = cents
	}
	weekRows.Close()

	allKeys := make(map[string]bool)
	for h := range todayMap {
		allKeys[h] = true
	}
	for h := range weekMap {
		allKeys[h] = true
	}

	result := make([]KeySpend, 0, len(allKeys))
	for h := range allKeys {
		result = append(result, KeySpend{
			KeyHash:    h,
			TodayCents: todayMap[h],
			WeekCents:  weekMap[h],
		})
	}
	return result, nil
}

