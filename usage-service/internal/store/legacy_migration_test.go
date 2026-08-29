package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

type legacyStoreSnapshot struct {
	RawCount int64
	MaxID    int64
	Hashes   []string
	Tables   map[string][]string
}

func TestOpenMigratesLegacyDatabaseWithoutChangingProtectedData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy sqlite: %v", err)
	}
	legacy.SetMaxOpenConns(1)
	for _, statement := range legacySchemaStatements() {
		if _, err := legacy.Exec(statement); err != nil {
			legacy.Close()
			t.Fatalf("create legacy schema: %v\n%s", err, statement)
		}
	}
	if err := seedLegacyStore(legacy); err != nil {
		legacy.Close()
		t.Fatalf("seed legacy store: %v", err)
	}
	before, err := snapshotLegacyStore(legacy)
	if err != nil {
		legacy.Close()
		t.Fatalf("snapshot before migration: %v", err)
	}
	beforeSchema, err := snapshotLegacySchema(legacy)
	if err != nil {
		legacy.Close()
		t.Fatalf("schema snapshot before migration: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy sqlite: %v", err)
	}

	migrated, err := Open(path)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer migrated.Close()
	after, err := snapshotLegacyStore(migrated.db)
	if err != nil {
		t.Fatalf("snapshot after migration: %v", err)
	}
	afterSchema, err := snapshotLegacySchema(migrated.db)
	if err != nil {
		t.Fatalf("schema snapshot after migration: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("protected legacy data changed:\nbefore=%#v\nafter=%#v", before, after)
	}

	var derivedTables int
	if err := migrated.db.QueryRow(`select count(*) from sqlite_master where type = 'table' and name in ('usage_hourly_rollups', 'usage_daily_dimension_rollups', 'usage_rollup_state')`).Scan(&derivedTables); err != nil {
		t.Fatalf("check derived tables: %v", err)
	}
	if derivedTables != 3 {
		t.Fatalf("derived table count = %d, want 3", derivedTables)
	}
	for name, definition := range beforeSchema {
		// 当前真实启动迁移会为 import history 补 csv_filename/error_details 两列。
		if name == "table:enterprise_key_bindings" || name == "table:enterprise_import_history" {
			continue
		}
		if afterSchema[name] != definition {
			t.Fatalf("protected schema object %q changed:\nbefore=%q\nafter=%q", name, definition, afterSchema[name])
		}
	}
	for name := range afterSchema {
		if _, existed := beforeSchema[name]; !existed && name != "table:usage_hourly_rollups" && name != "table:usage_daily_dimension_rollups" && name != "table:usage_rollup_state" && name != "table:collector_pending_items" && name != "index:idx_usage_daily_dimension_lookup" && name != "index:idx_collector_pending_items_status_id" {
			t.Fatalf("unexpected schema object added: %q", name)
		}
	}
	var migratedColumns int
	if err := migrated.db.QueryRow(`select count(*) from pragma_table_info('enterprise_import_history') where name in ('csv_filename', 'error_details')`).Scan(&migratedColumns); err != nil {
		t.Fatalf("check import-history migrated columns: %v", err)
	}
	if migratedColumns != 2 {
		t.Fatalf("import-history migrated column count = %d, want 2", migratedColumns)
	}
	var rollupRows int
	if err := migrated.db.QueryRow(`select count(*) from usage_hourly_rollups`).Scan(&rollupRows); err != nil {
		t.Fatalf("check empty derived rows: %v", err)
	}
	if rollupRows != 0 {
		t.Fatalf("legacy migration unexpectedly populated rollups: %d", rollupRows)
	}
	if err := migrated.db.QueryRow(`select count(*) from usage_daily_dimension_rollups`).Scan(&rollupRows); err != nil {
		t.Fatalf("check empty daily rollups: %v", err)
	}
	if rollupRows != 0 {
		t.Fatalf("legacy migration unexpectedly populated daily rollups: %d", rollupRows)
	}

	setup, ok, err := migrated.LoadSetup(context.Background())
	if err != nil || !ok || setup.CPAUpstreamURL != "https://legacy.example" || setup.ManagementKey != "legacy-management-key" {
		t.Fatalf("legacy setup = (%#v, %t, %v)", setup, ok, err)
	}
	managerConfig, ok, err := migrated.LoadManagerConfig(context.Background())
	if err != nil || !ok || managerConfig.CPAConnection.CPABaseURL != "https://legacy.example" {
		t.Fatalf("legacy manager config = (%#v, %t, %v)", managerConfig, ok, err)
	}
}

func legacySchemaStatements() []string {
	return []string{
		`create table usage_events (
			id integer primary key autoincrement, request_id text, event_hash text not null unique,
			timestamp_ms integer not null, timestamp text not null, provider text, model text not null,
			endpoint text, method text, path text, auth_type text, auth_index text, source text,
			source_hash text, api_key_hash text, account_snapshot text, auth_label_snapshot text,
			auth_file_snapshot text, auth_provider_snapshot text, auth_snapshot_at_ms integer,
			input_tokens integer not null default 0, output_tokens integer not null default 0,
			reasoning_tokens integer not null default 0, cached_tokens integer not null default 0,
			cache_tokens integer not null default 0, total_tokens integer not null default 0,
			latency_ms integer, failed integer not null default 0, raw_json text, created_at_ms integer not null
		)`,
		`create index idx_usage_events_timestamp on usage_events(timestamp_ms)`,
		`create index idx_usage_events_request_id on usage_events(request_id)`,
		`create index idx_usage_events_model on usage_events(model)`,
		`create index idx_usage_events_auth_index on usage_events(auth_index)`,
		`create index idx_usage_events_endpoint on usage_events(endpoint)`,
		`create index idx_usage_events_api_key_hash on usage_events(api_key_hash)`,
		`create index idx_usage_events_spend_window on usage_events(failed, timestamp_ms, api_key_hash, model)`,
		`create table dead_letter_events (id integer primary key autoincrement, payload text not null, error text not null, created_at_ms integer not null)`,
		`create table settings (key text primary key, value text not null, updated_at_ms integer not null)`,
		`create table model_prices (
			model text primary key, prompt_per_1m real not null, completion_per_1m real not null,
			cache_per_1m real not null, source text, source_model_id text, raw_json text,
			updated_at_ms integer not null, synced_at_ms integer
		)`,
		`create table api_key_aliases (api_key_hash text primary key, alias text not null, updated_at_ms integer not null)`,
		`create table enterprise_departments (
			id text primary key, name text not null, prefix text, sort_order integer not null default 0,
			enabled integer not null default 1, system integer not null default 0, updated_by text,
			created_at_ms integer not null, updated_at_ms integer not null
		)`,
		`create table enterprise_key_bindings (
			api_key text primary key, api_key_hash text not null default '', user_name text not null,
			department_id text not null, source text not null, department_resolved_by text not null,
			updated_by text, created_at_ms integer not null, updated_at_ms integer not null
		)`,
		`create index idx_enterprise_key_bindings_api_key_hash on enterprise_key_bindings(api_key_hash)`,
		`create index idx_enterprise_key_bindings_user_name on enterprise_key_bindings(user_name, updated_at_ms)`,
		`create index idx_enterprise_key_bindings_department_id on enterprise_key_bindings(department_id)`,
		`create index idx_enterprise_key_bindings_source_user_name on enterprise_key_bindings(source, user_name)`,
		`create table enterprise_import_history (
			task_id text primary key, total_rows integer not null, passed_rows integer not null,
			warning_rows integer not null, error_rows integer not null, status text not null,
			updated_by text, created_at_ms integer not null, updated_at_ms integer not null
		)`,
		`create table pool_quota_alert_log (window_type text not null, exhausted_at_ms integer not null, notified_at_ms integer not null, primary key(window_type))`,
		`create table spend_alert_log (user_name text not null, threshold_cents integer not null, triggered_at_ms integer not null, notified_at_ms integer not null, primary key(user_name, threshold_cents))`,
	}
}

func seedLegacyStore(db *sql.DB) error {
	setup := Setup{CPAUpstreamURL: "https://legacy.example", ManagementKey: "legacy-management-key", Queue: "legacy-queue", PopSide: "left"}
	setupJSON, _ := json.Marshal(setup)
	manager := ManagerConfig{CPAConnection: ManagerCPAConnectionConfig{CPABaseURL: "https://legacy.example", ManagementKey: "legacy-management-key"}, Collector: ManagerCollectorConfig{Queue: "legacy-queue", PopSide: "left", BatchSize: 77}}
	managerJSON, _ := json.Marshal(manager)
	statements := []struct {
		query string
		args  []any
	}{
		{`insert into usage_events(event_hash, timestamp_ms, timestamp, model, endpoint, total_tokens, failed, created_at_ms) values(?, ?, ?, ?, ?, ?, ?, ?)`, []any{"legacy-event-hash", 1_700_000_000_000, "2023-11-14T22:13:20Z", "legacy-model", "POST /legacy", 42, 0, 1}},
		{`insert into settings(key, value, updated_at_ms) values(?, ?, ?)`, []any{"setup", string(setupJSON), 11}},
		{`insert into settings(key, value, updated_at_ms) values(?, ?, ?)`, []any{"manager_config_v1", string(managerJSON), 12}},
		{`insert into settings(key, value, updated_at_ms) values(?, ?, ?)`, []any{"quota_config", `{"enabled":true,"default":{"daily_cents":123,"weekly_cents":456}}`, 13}},
		{`insert into settings(key, value, updated_at_ms) values(?, ?, ?)`, []any{"quota_pause_config", `{"enabled":true,"default":{"daily_cents":10}}`, 14}},
		{`insert into settings(key, value, updated_at_ms) values(?, ?, ?)`, []any{"quota_downgrade_config", `{"enabled":true,"fallback_model":"legacy-fallback"}`, 15}},
		{`insert into settings(key, value, updated_at_ms) values(?, ?, ?)`, []any{"alert_config", `{"alertEnabled":true,"thresholdCents":321,"smtpHost":"legacy.smtp"}`, 16}},
		{`insert into model_prices(model, prompt_per_1m, completion_per_1m, cache_per_1m, source, source_model_id, raw_json, updated_at_ms, synced_at_ms) values(?, ?, ?, ?, ?, ?, ?, ?, ?)`, []any{"legacy-model", 1.2, 3.4, 0.5, "legacy", "legacy-model-id", `{"legacy":true}`, 21, 22}},
		{`insert into api_key_aliases(api_key_hash, alias, updated_at_ms) values(?, ?, ?)`, []any{strings.Repeat("a", 64), "legacy alias", 31}},
		{`insert into enterprise_departments(id, name, prefix, sort_order, enabled, system, updated_by, created_at_ms, updated_at_ms) values(?, ?, ?, ?, ?, ?, ?, ?, ?)`, []any{UngroupedDepartmentID, "未分组", "", -1, 1, 1, "legacy", 41, 42}},
		{`insert into enterprise_departments(id, name, prefix, sort_order, enabled, system, updated_by, created_at_ms, updated_at_ms) values(?, ?, ?, ?, ?, ?, ?, ?, ?)`, []any{"legacy-dept", "Legacy Dept", "ld", 1, 1, 0, "legacy", 43, 44}},
		{`insert into enterprise_key_bindings(api_key, api_key_hash, user_name, department_id, source, department_resolved_by, updated_by, created_at_ms, updated_at_ms) values(?, ?, ?, ?, ?, ?, ?, ?, ?)`, []any{"legacy-secret-key", strings.Repeat("b", 64), "legacy-user", "legacy-dept", "manual", "manual", "legacy", 51, 52}},
		// 迁移不得把空/未知部门或同步占位用户名静默归一化。
		{`insert into enterprise_key_bindings(api_key, api_key_hash, user_name, department_id, source, department_resolved_by, updated_by, created_at_ms, updated_at_ms) values(?, ?, ?, ?, ?, ?, ?, ?, ?)`, []any{"legacy-empty-dept", strings.Repeat("c", 64), "empty-dept-user", "", "manual", "manual", "legacy", 53, 54}},
		{`insert into enterprise_key_bindings(api_key, api_key_hash, user_name, department_id, source, department_resolved_by, updated_by, created_at_ms, updated_at_ms) values(?, ?, ?, ?, ?, ?, ?, ?, ?)`, []any{"legacy-unknown-dept", strings.Repeat("d", 64), "unknown-dept-user", "unknown-dept", "manual", "manual", "legacy", 55, 56}},
		{`insert into enterprise_key_bindings(api_key, api_key_hash, user_name, department_id, source, department_resolved_by, updated_by, created_at_ms, updated_at_ms) values(?, ?, ?, ?, ?, ?, ?, ?, ?)`, []any{"legacy-synced-user", strings.Repeat("e", 64), "synced_legacy-user", "legacy-dept", "sync", "sync", "legacy", 57, 58}},
		{`insert into enterprise_import_history(task_id, total_rows, passed_rows, warning_rows, error_rows, status, updated_by, created_at_ms, updated_at_ms) values(?, ?, ?, ?, ?, ?, ?, ?, ?)`, []any{"legacy-import", 10, 9, 1, 0, "completed", "legacy", 59, 60}},

		{`insert into pool_quota_alert_log(window_type, exhausted_at_ms, notified_at_ms) values(?, ?, ?)`, []any{"daily", 61, 62}},
		{`insert into spend_alert_log(user_name, threshold_cents, triggered_at_ms, notified_at_ms) values(?, ?, ?, ?)`, []any{"legacy-user", 100, 71, 72}},
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			return err
		}
	}
	return nil
}

func snapshotLegacyStore(db interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}) (legacyStoreSnapshot, error) {
	var snapshot legacyStoreSnapshot
	if err := db.QueryRow(`select count(*), coalesce(max(id), 0) from usage_events`).Scan(&snapshot.RawCount, &snapshot.MaxID); err != nil {
		return snapshot, err
	}
	hashes, err := snapshotRows(db, `select event_hash from usage_events order by id`)
	if err != nil {
		return snapshot, err
	}
	snapshot.Hashes = hashes
	snapshot.Tables = map[string][]string{}
	historyQuery := `select task_id, '', total_rows, passed_rows, warning_rows, error_rows, '', status, coalesce(updated_by,''), created_at_ms, updated_at_ms from enterprise_import_history order by task_id`
	if hasColumn(db, "enterprise_import_history", "csv_filename") && hasColumn(db, "enterprise_import_history", "error_details") {
		historyQuery = `select task_id, coalesce(csv_filename,''), total_rows, passed_rows, warning_rows, error_rows, coalesce(error_details,''), status, coalesce(updated_by,''), created_at_ms, updated_at_ms from enterprise_import_history order by task_id`
	}
	bindingQuery := `select api_key, api_key_hash, user_name, department_id, source, department_resolved_by, '', coalesce(updated_by,''), created_at_ms, updated_at_ms from enterprise_key_bindings order by api_key`
	if hasColumn(db, "enterprise_key_bindings", "email") {
		bindingQuery = `select api_key, api_key_hash, user_name, department_id, source, department_resolved_by, coalesce(email,''), coalesce(updated_by,''), created_at_ms, updated_at_ms from enterprise_key_bindings order by api_key`
	}
	queries := map[string]string{
		"settings":                  `select key, value, updated_at_ms from settings order by key`,
		"model_prices":              `select model, prompt_per_1m, completion_per_1m, cache_per_1m, coalesce(source,''), coalesce(source_model_id,''), coalesce(raw_json,''), updated_at_ms, coalesce(synced_at_ms,0) from model_prices order by model`,
		"api_key_aliases":           `select api_key_hash, alias, updated_at_ms from api_key_aliases order by api_key_hash`,
		"enterprise_departments":    `select id, name, coalesce(prefix,''), sort_order, enabled, system, coalesce(updated_by,''), created_at_ms, updated_at_ms from enterprise_departments order by id`,
		"enterprise_key_bindings":   bindingQuery,
		"enterprise_import_history": historyQuery,
		"pool_quota_alert_log":      `select window_type, exhausted_at_ms, notified_at_ms from pool_quota_alert_log order by window_type`,
		"spend_alert_log":           `select user_name, threshold_cents, triggered_at_ms, notified_at_ms from spend_alert_log order by user_name, threshold_cents`,
	}
	for name, query := range queries {
		rows, err := snapshotRows(db, query)
		if err != nil {
			return snapshot, fmt.Errorf("%s: %w", name, err)
		}
		snapshot.Tables[name] = rows
	}
	return snapshot, nil
}

func hasColumn(db interface {
	Query(string, ...any) (*sql.Rows, error)
}, table, column string) bool {
	rows, err := db.Query(`pragma table_info(` + table + `)`)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, columnType string
		var defaultValue any
		if rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk) == nil && name == column {
			return true
		}
	}
	return false
}

func snapshotLegacySchema(db interface {
	Query(string, ...any) (*sql.Rows, error)
}) (map[string]string, error) {
	rows, err := db.Query(`select type, name, coalesce(sql, '') from sqlite_master where name not like 'sqlite_%' order by type, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var objectType, name, definition string
		if err := rows.Scan(&objectType, &name, &definition); err != nil {
			return nil, err
		}
		result[objectType+":"+name] = definition
	}
	return result, rows.Err()
}

func snapshotRows(db interface {
	Query(string, ...any) (*sql.Rows, error)
}, query string, args ...any) ([]string, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := make([]string, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		for i, value := range values {
			if bytes, ok := value.([]byte); ok {
				values[i] = string(bytes)
			}
		}
		result = append(result, fmt.Sprintf("%v", values))
	}
	return result, rows.Err()
}
