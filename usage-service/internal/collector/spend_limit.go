package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
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

func normalizePauseKeyHash(keyHash string) string {
	keyHash = strings.ToLower(strings.TrimSpace(keyHash))
	if len(keyHash) == 64 {
		return keyHash[:8]
	}
	return keyHash
}

const spendLimitExceededReason = "spend_limit_exceeded"

// 限额消费窗口按上海时区统计，到期时间必须使用相同时区避免跨日偏差。
var shanghaiLocation = func() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return location
}()

type pausedKey struct {
	KeyHash string `json:"key_hash"`
	Reason  string `json:"reason"`
	Expired bool   `json:"-"`
}

func (c *pauseClient) PauseKey(keyHash, reason string, expiresAt time.Time) error {
	expiresIn := int64(time.Until(expiresAt).Seconds())
	if expiresIn < 0 {
		expiresIn = 0
	}
	return c.doJSON(http.MethodPost, "/v0/management/quota/pause", map[string]any{
		"key_hash":           normalizePauseKeyHash(keyHash),
		"reason":             reason,
		"expires_in_seconds": expiresIn,
	}, nil)
}

func (c *pauseClient) ResumeKey(keyHash, expectedReason string) error {
	return c.doJSON(http.MethodPost, "/v0/management/quota/resume", map[string]any{
		"key_hash":        normalizePauseKeyHash(keyHash),
		"expected_reason": expectedReason,
	}, nil)
}

func (c *pauseClient) PausedKeys() ([]pausedKey, error) {
	var result struct {
		Entries []struct {
			KeyHash   string `json:"key_hash"`
			Reason    string `json:"reason"`
			ExpiresAt string `json:"expires_at"`
		} `json:"entries"`
	}
	if err := c.doJSON(http.MethodGet, "/v0/management/quota/paused", nil, &result); err != nil {
		return nil, err
	}
	entries := make([]pausedKey, 0, len(result.Entries))
	for _, entry := range result.Entries {
		paused := pausedKey{KeyHash: entry.KeyHash, Reason: entry.Reason}
		if entry.ExpiresAt != "" {
			if expiresAt, err := time.Parse(time.RFC3339, entry.ExpiresAt); err == nil {
				paused.Expired = !expiresAt.After(time.Now())
			}
		}
		entries = append(entries, paused)
	}
	return entries, nil
}

// doJSON 统一处理已认证的 CLIProxyAPI 限额管理请求，并将非 2xx 视为同步失败。
func (c *pauseClient) doJSON(method, path string, body any, result any) error {
	if c == nil || c.baseURL == "" || c.mgmtKey == "" {
		return fmt.Errorf("pause client not configured")
	}
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, strings.TrimRight(c.baseURL, "/")+path, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.mgmtKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("quota request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("quota request %s %s returned status %d", method, path, resp.StatusCode)
	}
	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode quota response: %w", err)
		}
	}
	return nil
}

// ReconcileSpendLimits 根据 usage-service 的持久化规则和当前消费，协调自动暂停状态。
func ReconcileSpendLimits(s *store.Store, client *pauseClient) error {
	ctx := context.Background()
	cfg, ok, err := s.LoadSpendLimitConfig(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	keys, err := s.QueryKeySpend(ctx)
	if err != nil {
		return fmt.Errorf("query key spend: %w", err)
	}
	// 未保存配置没有 usage-service 可恢复的自动暂停；其他情形必须读取暂停记录保护手动暂停。
	if !ok && len(keys) == 0 {
		return nil
	}
	paused, err := client.PausedKeys()
	if err != nil {
		return fmt.Errorf("list paused keys: %w", err)
	}
	automatic := make(map[string]bool, len(paused))
	keysByHash := make(map[string]store.KeySpend, len(keys)+len(paused))
	for _, key := range keys {
		if key.KeyHash != "" {
			keysByHash[normalizePauseKeyHash(key.KeyHash)] = key
		}
	}
	for _, entry := range paused {
		if entry.Reason != spendLimitExceededReason || entry.Expired {
			continue
		}
		keyHash := normalizePauseKeyHash(entry.KeyHash)
		automatic[keyHash] = true
		if _, exists := keysByHash[keyHash]; !exists {
			keysByHash[keyHash] = store.KeySpend{KeyHash: keyHash}
		}
	}

	now := time.Now()
	for keyHash, key := range keysByHash {
		limit := store.SpendLimit{}
		if ok && cfg.Enabled {
			// 必须复用配置的哈希匹配和覆盖优先级，避免在协调器复制规则。
			limit = cfg.LimitForKey(key.KeyHash)
		}
		exceeded, expiresAt := spendLimitExceeded(key, limit, now)
		if exceeded {
			log.Printf("spend-limit: pausing key %s (today=%dc weekly=%dc limit daily=%dc weekly=%dc)",
				keyHash, key.TodayCents, key.WeekCents, limit.DailyCents, limit.WeeklyCents)
			if err := client.PauseKey(key.KeyHash, spendLimitExceededReason, expiresAt); err != nil {
				return fmt.Errorf("pause key %s: %w", keyHash, err)
			}
			continue
		}
		if automatic[keyHash] {
			if err := client.ResumeKey(key.KeyHash, spendLimitExceededReason); err != nil {
				return fmt.Errorf("resume key %s: %w", keyHash, err)
			}
		}
	}
	return nil
}

func spendLimitExceeded(key store.KeySpend, limit store.SpendLimit, now time.Time) (bool, time.Time) {
	now = now.In(shanghaiLocation)
	if limit.DailyCents > 0 && key.TodayCents >= limit.DailyCents {
		return true, time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, shanghaiLocation)
	}
	if limit.WeeklyCents > 0 && key.WeekCents >= limit.WeeklyCents {
		daysUntilMonday := (8 - int(now.Weekday())) % 7
		if daysUntilMonday == 0 {
			daysUntilMonday = 7
		}
		return true, time.Date(now.Year(), now.Month(), now.Day()+daysUntilMonday, 0, 0, 0, 0, shanghaiLocation)
	}
	return false, time.Time{}
}

// CheckAndEnforceLimits 保持兼容调用；后台任务记录错误而不影响下一轮扫描。
func CheckAndEnforceLimits(s *store.Store, client *pauseClient) {
	if err := ReconcileSpendLimits(s, client); err != nil {
		log.Printf("spend-limit: reconcile failed: %v", err)
	}
}
