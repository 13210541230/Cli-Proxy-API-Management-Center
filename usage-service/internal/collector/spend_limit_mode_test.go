package collector

import (
	"testing"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/store"
)

func TestSpendLimitExceededForTokens(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, shanghaiLocation)
	key := store.KeySpend{
		KeyHash:     "key-a",
		TodayTokens: 1000,
		WeekTokens:  4000,
	}
	limit := store.SpendLimit{DailyTokens: 1000, WeeklyTokens: 5000}

	exceeded, expiresAt := spendLimitExceededForMode(key, limit, store.SpendLimitModeTokens, now)
	if !exceeded {
		t.Fatal("token daily limit should be exceeded")
	}
	want := time.Date(2026, 6, 26, 0, 0, 0, 0, shanghaiLocation)
	if !expiresAt.Equal(want) {
		t.Fatalf("expires_at = %v, want %v", expiresAt, want)
	}
}

func TestSpendLimitCostModeIgnoresTokenTotals(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, shanghaiLocation)
	key := store.KeySpend{KeyHash: "key-a", TodayTokens: 1000000, WeekTokens: 1000000}
	limit := store.SpendLimit{DailyTokens: 1, WeeklyTokens: 1}

	exceeded, _ := spendLimitExceededForMode(key, limit, store.SpendLimitModeCost, now)
	if exceeded {
		t.Fatal("cost mode must not enforce token limits")
	}
}
