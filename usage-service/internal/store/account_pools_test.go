package store

import (
	"context"
	"testing"
)

func TestAccountPoolSnapshotIsAtomicAndHashOnly(t *testing.T) {
	db, err := Open(t.TempDir() + "/usage.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()

	if err := db.UpsertAccountPool(ctx, AccountPool{ID: "engineering", Name: "Engineering", Provider: AccountPoolProviderCodex, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceAccountPoolMembers(ctx, "engineering", []AccountPoolMember{{AuthID: "auth-a", Priority: 10, Enabled: true}, {AuthID: "auth-b", Priority: 20, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetAccountPoolBindings(ctx, []AccountPoolBindingUpdate{{APIKeyHash: "abcdef12", PoolID: "engineering"}}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := db.LoadAccountPoolSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Policy.Provider != AccountPoolProviderCodex || snapshot.Policy.Hash == "" || snapshot.Policy.Version < 3 {
		t.Fatalf("policy = %#v, want codex version/hash", snapshot.Policy)
	}
	if len(snapshot.Policy.Pools) != 1 || len(snapshot.Policy.Members) != 2 || len(snapshot.Policy.Bindings) != 1 {
		t.Fatalf("policy contents = %#v, want one pool/two members/one binding", snapshot.Policy)
	}
	if snapshot.Policy.Bindings[0].APIKeyHash != "abcdef12" || snapshot.Policy.Bindings[0].PoolID != "engineering" {
		t.Fatalf("binding = %#v", snapshot.Policy.Bindings[0])
	}
}

func TestAccountPoolBindingCanBeClearedWithoutRawAPIKey(t *testing.T) {
	db, err := Open(t.TempDir() + "/usage.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.UpsertAccountPool(ctx, AccountPool{ID: "pool-a", Name: "Pool A", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetAccountPoolBindings(ctx, []AccountPoolBindingUpdate{{APIKeyHash: "1234abcd", PoolID: "pool-a"}}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetAccountPoolBindings(ctx, []AccountPoolBindingUpdate{{APIKeyHash: "1234abcd", PoolID: ""}}); err != nil {
		t.Fatal(err)
	}
	bindings, err := db.LoadAccountPoolBindings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 0 {
		t.Fatalf("bindings = %#v, want empty", bindings)
	}
}
