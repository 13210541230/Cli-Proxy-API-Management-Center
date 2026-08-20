package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

type quotaHTTPError struct {
	method string
	path   string
	status int
	body   string
}

func (e *quotaHTTPError) Error() string {
	if e.body != "" {
		return fmt.Sprintf("quota request %s %s returned status %d: %s", e.method, e.path, e.status, e.body)
	}
	return fmt.Sprintf("quota request %s %s returned status %d", e.method, e.path, e.status)
}

func isRetryableQuotaError(err error) bool {
	var quotaErr *quotaHTTPError
	if !errors.As(err, &quotaErr) {
		return false
	}
	if quotaErr.path == "/v0/management/quota/paused" && quotaErr.status == http.StatusInternalServerError {
		// The upstream quota handler currently maps a transient SQLite/store
		// failure to 500. This GET is safe to retry and the response is only
		// used for reconciliation, never for an end-user request.
		return true
	}
	switch quotaErr.status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, http.StatusTooManyRequests:
		return true
	default:
		return false
	}
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
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		result.Entries = nil
		err = c.doJSON(http.MethodGet, "/v0/management/quota/paused", nil, &result)
		if err == nil {
			break
		}
		if attempt == 0 && isRetryableQuotaError(err) {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	entries := make([]pausedKey, 0, len(result.Entries))
	for _, entry := range result.Entries {
		paused := pausedKey{KeyHash: entry.KeyHash, Reason: entry.Reason}
		if entry.ExpiresAt != "" {
			if expiresAt, err := time.Parse(time.RFC3339, entry.ExpiresAt); err == nil && !expiresAt.IsZero() {
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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return &quotaHTTPError{
			method: method,
			path:   path,
			status: resp.StatusCode,
			body:   strings.TrimSpace(string(body)),
		}
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
	// 没有持久化规则时保留旧的恢复语义，但只做索引支持的存在性检查，
	// 不再为一次恢复动作聚合整张 usage_events 表。
	if !ok {
		hasSpend, err := s.HasCurrentKeySpend(ctx)
		if err != nil {
			return fmt.Errorf("check key spend: %w", err)
		}
		if !hasSpend {
			return nil
		}
	}
	paused, err := client.PausedKeys()
	if err != nil {
		return fmt.Errorf("list paused keys: %w", err)
	}
	automatic := make(map[string]bool, len(paused))
	if !cfg.Enabled {
		for _, entry := range paused {
			if entry.Reason != spendLimitExceededReason || entry.Expired {
				continue
			}
			keyHash := normalizePauseKeyHash(entry.KeyHash)
			automatic[keyHash] = true
		}
		for keyHash := range automatic {
			if err := client.ResumeKey(keyHash, spendLimitExceededReason); err != nil {
				return fmt.Errorf("resume key %s: %w", keyHash, err)
			}
		}
		return nil
	}

	keys, err := s.QueryKeySpend(ctx)
	if err != nil {
		return fmt.Errorf("query key spend: %w", err)
	}
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
