package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/store"
)

// pauseClient calls CLIProxyAPI's /v0/management/quota/pause endpoint.
type pauseClient struct {
	baseURL string
	mgmtKey string
	client  *http.Client
}

func newPauseClient(baseURL, mgmtKey string) *pauseClient {
	return &pauseClient{
		baseURL: baseURL,
		mgmtKey: mgmtKey,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *pauseClient) PauseKey(keyHash, reason string, expiresAt time.Time) error {
	if c == nil || c.baseURL == "" || c.mgmtKey == "" {
		return fmt.Errorf("pause client not configured")
	}
	expiresIn := int64(time.Until(expiresAt).Seconds())
	if expiresIn < 0 {
		expiresIn = 0
	}
	body := map[string]any{
		"key_hash":           keyHash,
		"reason":             reason,
		"expires_in_seconds": expiresIn,
	}
	data, _ := json.Marshal(body)

	url := fmt.Sprintf("%s/v0/management/quota/pause", c.baseURL)
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.mgmtKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("pause request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pause returned status %d", resp.StatusCode)
	}
	return nil
}

// CheckAndEnforceLimits queries all keys' spend and pauses any that exceed limits.
// It is safe to call repeatedly — keys already paused will not be re-paused
// (the pause call is idempotent INSERT OR REPLACE).
func CheckAndEnforceLimits(s *store.Store, pauseClient *pauseClient) {
	ctx := context.Background()

	cfg, ok, err := s.LoadSpendLimitConfig(ctx)
	if err != nil {
		log.Printf("spend-limit: load config failed: %v", err)
		return
	}
	if !ok || !cfg.Enabled {
		return
	}
	defaultLimit := cfg.DefaultLimit()
	if defaultLimit.DailyCents <= 0 && defaultLimit.WeeklyCents <= 0 && len(cfg.Overrides) == 0 {
		return
	}

	keys, err := s.QueryKeySpend(ctx)
	if err != nil {
		log.Printf("spend-limit: query key spend failed: %v", err)
		return
	}

	now := time.Now()
	for _, k := range keys {
		if k.KeyHash == "" {
			continue
		}

		limit := cfg.LimitForKey(k.KeyHash)
		exceeded := false
		var expiresAt time.Time

		if limit.DailyCents > 0 && k.TodayCents >= limit.DailyCents {
			exceeded = true
			expiresAt = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		} else if limit.WeeklyCents > 0 && k.WeekCents >= limit.WeeklyCents {
			exceeded = true
			daysUntilMonday := (8 - int(now.Weekday())) % 7
			if daysUntilMonday == 0 {
				daysUntilMonday = 7
			}
			expiresAt = time.Date(now.Year(), now.Month(), now.Day()+daysUntilMonday, 0, 0, 0, 0, now.Location())
		}

		if exceeded {
			log.Printf("spend-limit: pausing key %s (today=%dc weekly=%dc limit daily=%dc weekly=%dc)",
				k.KeyHash[:4], k.TodayCents, k.WeekCents, limit.DailyCents, limit.WeeklyCents)
			if err := pauseClient.PauseKey(k.KeyHash, "spend_limit_exceeded", expiresAt); err != nil {
				log.Printf("spend-limit: failed to pause key %s: %v", k.KeyHash[:4], err)
			}
		}
	}
}
