package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/seakee/cpa-manager/usage-service/internal/usage"
)

func TestStorePersistsAccountSnapshot(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	_, err = db.InsertEvents(context.Background(), []usage.Event{
		{
			EventHash:            "event-1",
			TimestampMS:          1_778_000_000_000,
			Timestamp:            "2026-05-06T00:00:00Z",
			Model:                "gpt-test",
			Endpoint:             "POST /v1/chat/completions",
			AuthIndex:            "auth-1",
			APIKeyHash:           "api-key-hash-1",
			AccountSnapshot:      "alice@example.com",
			AuthLabelSnapshot:    "Alice",
			AuthFileSnapshot:     "alice.json",
			AuthProviderSnapshot: "codex",
			AuthSnapshotAtMS:     1_778_000_000_100,
			CreatedAtMS:          1_778_000_000_200,
		},
	})
	if err != nil {
		t.Fatalf("insert events: %v", err)
	}

	events, err := db.RecentEvents(context.Background(), 10)
	if err != nil {
		t.Fatalf("recent events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	event := events[0]
	if event.AccountSnapshot != "alice@example.com" {
		t.Fatalf("AccountSnapshot = %q", event.AccountSnapshot)
	}
	if event.AuthLabelSnapshot != "Alice" {
		t.Fatalf("AuthLabelSnapshot = %q", event.AuthLabelSnapshot)
	}
	if event.AuthFileSnapshot != "alice.json" {
		t.Fatalf("AuthFileSnapshot = %q", event.AuthFileSnapshot)
	}
	if event.AuthProviderSnapshot != "codex" {
		t.Fatalf("AuthProviderSnapshot = %q", event.AuthProviderSnapshot)
	}
	if event.AuthSnapshotAtMS != 1_778_000_000_100 {
		t.Fatalf("AuthSnapshotAtMS = %d", event.AuthSnapshotAtMS)
	}
	if event.APIKeyHash != "api-key-hash-1" {
		t.Fatalf("APIKeyHash = %q", event.APIKeyHash)
	}

	payload := usage.BuildPayload(events)
	detail := payload.APIs["POST /v1/chat/completions"].Models["gpt-test"].Details[0]
	if detail.APIKeyHash != "api-key-hash-1" {
		t.Fatalf("payload APIKeyHash = %q", detail.APIKeyHash)
	}
	if detail.AccountSnapshot != "alice@example.com" {
		t.Fatalf("payload AccountSnapshot = %q", detail.AccountSnapshot)
	}
	if detail.AuthProviderSnapshot != "codex" {
		t.Fatalf("payload AuthProviderSnapshot = %q", detail.AuthProviderSnapshot)
	}
}

func TestStoreAPIKeyAliases(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	const hash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := db.UpsertAPIKeyAliases(context.Background(), []APIKeyAlias{
		{APIKeyHash: hash, Alias: " Alice "},
	}); err != nil {
		t.Fatalf("upsert alias: %v", err)
	}

	aliases, err := db.LoadAPIKeyAliases(context.Background())
	if err != nil {
		t.Fatalf("load aliases: %v", err)
	}
	if len(aliases) != 1 {
		t.Fatalf("len(aliases) = %d, want 1", len(aliases))
	}
	if aliases[0].APIKeyHash != hash || aliases[0].Alias != "Alice" || aliases[0].UpdatedAtMS <= 0 {
		t.Fatalf("alias = %#v", aliases[0])
	}

	if err := db.UpsertAPIKeyAliases(context.Background(), []APIKeyAlias{
		{APIKeyHash: hash, Alias: "Team A"},
	}); err != nil {
		t.Fatalf("update alias: %v", err)
	}
	aliases, err = db.LoadAPIKeyAliases(context.Background())
	if err != nil {
		t.Fatalf("reload aliases: %v", err)
	}
	if len(aliases) != 1 || aliases[0].Alias != "Team A" {
		t.Fatalf("updated aliases = %#v", aliases)
	}

	const otherHash = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	if err := db.UpsertAPIKeyAliases(context.Background(), []APIKeyAlias{
		{APIKeyHash: otherHash, Alias: " team a "},
	}); err == nil || err.Error() != "api key alias already exists" {
		t.Fatalf("duplicate alias error = %v", err)
	}
	if err := db.UpsertAPIKeyAliases(context.Background(), []APIKeyAlias{
		{APIKeyHash: hash, Alias: "Alpha"},
		{APIKeyHash: otherHash, Alias: " alpha "},
	}); err == nil || err.Error() != "api key alias already exists" {
		t.Fatalf("batch duplicate alias error = %v", err)
	}

	if err := db.DeleteAPIKeyAlias(context.Background(), hash); err != nil {
		t.Fatalf("delete alias: %v", err)
	}
	aliases, err = db.LoadAPIKeyAliases(context.Background())
	if err != nil {
		t.Fatalf("load after delete: %v", err)
	}
	if len(aliases) != 0 {
		t.Fatalf("aliases after delete = %#v", aliases)
	}
}

func TestStoreEnterpriseMetadataPersistence(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	ctx := context.Background()
	if err := db.UpsertEnterpriseDepartments(ctx, []EnterpriseDepartment{
		{ID: "ungrouped", Name: "未分组", SortOrder: 0, Enabled: true, System: true, UpdatedBy: "system"},
		{ID: "dept_sh", Name: "上海总部", Prefix: "sh", SortOrder: 1, Enabled: true, UpdatedBy: "admin"},
	}); err != nil {
		t.Fatalf("upsert departments: %v", err)
	}

	departments, err := db.LoadEnterpriseDepartments(ctx)
	if err != nil {
		t.Fatalf("load departments: %v", err)
	}
	if len(departments) < 2 {
		t.Fatalf("len(departments) = %d, want >= 2", len(departments))
	}
	var foundShanghai bool
	for _, department := range departments {
		if department.ID == "dept_sh" && department.Prefix == "sh" {
			foundShanghai = true
			break
		}
	}
	if !foundShanghai {
		t.Fatalf("departments missing dept_sh: %#v", departments)
	}

	if err := db.UpsertEnterpriseKeyBindings(ctx, []EnterpriseKeyBinding{
		{
			APIKey:               "sh-abc123",
			UserName:             "zhangsan",
			DepartmentID:         "dept_sh",
			Source:               "import",
			DepartmentResolvedBy: "import",
			UpdatedBy:            "admin",
		},
	}); err != nil {
		t.Fatalf("upsert key bindings: %v", err)
	}

	bindings, err := db.LoadEnterpriseKeyBindings(ctx)
	if err != nil {
		t.Fatalf("load key bindings: %v", err)
	}
	if len(bindings) != 1 {
		t.Fatalf("len(bindings) = %d, want 1", len(bindings))
	}
	if bindings[0].APIKey != "sh-abc123" || bindings[0].DepartmentID != "dept_sh" {
		t.Fatalf("binding = %#v", bindings[0])
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(bindings[0].APIKeyHash) {
		t.Fatalf("binding APIKeyHash = %q", bindings[0].APIKeyHash)
	}
	expectedHash := fmt.Sprintf("%x", sha256.Sum256([]byte(bindings[0].APIKey)))
	if bindings[0].APIKeyHash != expectedHash {
		t.Fatalf("binding APIKeyHash = %q, want %q", bindings[0].APIKeyHash, expectedHash)
	}

	aliases, err := db.LoadAPIKeyAliases(ctx)
	if err != nil {
		t.Fatalf("load aliases: %v", err)
	}
	if len(aliases) != 1 || aliases[0].Alias != "zhangsan" || aliases[0].APIKeyHash != bindings[0].APIKeyHash {
		t.Fatalf("aliases = %#v", aliases)
	}

	if err := db.AppendEnterpriseImportHistory(ctx, EnterpriseImportHistory{
		TaskID:       "task-001",
		CSVFileName:  "enterprise-users.csv",
		TotalRows:    10,
		PassedRows:   9,
		WarningRows:  1,
		ErrorRows:    0,
		ErrorDetails: `[{"row":4,"reason":"duplicate user"}]`,
		Status:       "completed",
		UpdatedBy:    "admin",
	}); err != nil {
		t.Fatalf("append import history: %v", err)
	}

	history, err := db.LoadEnterpriseImportHistory(ctx, 10)
	if err != nil {
		t.Fatalf("load import history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("len(history) = %d, want 1", len(history))
	}
	if history[0].TaskID != "task-001" || history[0].Status != "completed" {
		t.Fatalf("history = %#v", history[0])
	}
	if history[0].CSVFileName != "enterprise-users.csv" {
		t.Fatalf("history CSVFileName = %q", history[0].CSVFileName)
	}
	if history[0].ErrorDetails != `[{"row":4,"reason":"duplicate user"}]` {
		t.Fatalf("history ErrorDetails = %q", history[0].ErrorDetails)
	}
}
