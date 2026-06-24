package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// syncSpendLimitConfig fetches the quota config from CPA upstream and saves it to local DB.
// CPA returns: {"enabled":true, "default":{"daily_cents":15000,"weekly_cents":50000}, ...}
// We extract and persist the default limits into local settings, bridging the gap
// between QuotaLimitsPage (PUT /quota/config → CPA yaml) and spend limit checker.
func syncSpendLimitConfig(ctx context.Context, s *store.Store, baseURL, mgmtKey string) {
	if baseURL == "" || mgmtKey == "" {
		return
	}
	url := fmt.Sprintf("%s/v0/management/quota/config", baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		log.Printf("spend-limit: sync request error: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+mgmtKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("spend-limit: sync fetch error: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("spend-limit: sync returned status %d", resp.StatusCode)
		return
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("spend-limit: sync read error: %v", err)
		return
	}

		var remoteCfg struct {
			Enabled  bool `json:"enabled"`
			Default  struct {
				DailyCents  int64 `json:"daily_cents"`
				WeeklyCents int64 `json:"weekly_cents"`
			} `json:"default"`
		}
		if err := json.Unmarshal(raw, &remoteCfg); err != nil {
			log.Printf("spend-limit: sync parse error: %v", err)
			return
		}

		localCfg := store.SpendLimitConfig{
			Enabled:     remoteCfg.Enabled,
			DailyCents:  remoteCfg.Default.DailyCents,
			WeeklyCents: remoteCfg.Default.WeeklyCents,
		}
	if err := s.SaveSpendLimitConfig(ctx, localCfg); err != nil {
		log.Printf("spend-limit: sync save error: %v", err)
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
	if cfg.DailyCents <= 0 && cfg.WeeklyCents <= 0 {
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

		exceeded := false
		var expiresAt time.Time

		if cfg.DailyCents > 0 && k.TodayCents >= cfg.DailyCents {
			exceeded = true
			// Resume at next day 00:00
			expiresAt = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		} else if cfg.WeeklyCents > 0 && k.WeekCents >= cfg.WeeklyCents {
			exceeded = true
			// Resume at next Monday 00:00
			daysUntilMonday := (8 - int(now.Weekday())) % 7
			if daysUntilMonday == 0 {
				daysUntilMonday = 7
			}
			expiresAt = time.Date(now.Year(), now.Month(), now.Day()+daysUntilMonday, 0, 0, 0, 0, now.Location())
		}

		if exceeded {
			log.Printf("spend-limit: pausing key %s (today=%dc weekly=%dc limit daily=%dc weekly=%dc)",
				k.KeyHash[:4], k.TodayCents, k.WeekCents, cfg.DailyCents, cfg.WeeklyCents)
			if err := pauseClient.PauseKey(k.KeyHash, "spend_limit_exceeded", expiresAt); err != nil {
				log.Printf("spend-limit: failed to pause key %s: %v", k.KeyHash[:4], err)
			}
		}
	}
}

