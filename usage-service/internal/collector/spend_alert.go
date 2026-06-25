package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/seakee/cpa-manager/usage-service/internal/mail"
	"github.com/seakee/cpa-manager/usage-service/internal/store"
)

// CheckUserSpendAlerts queries all users' daily spend and sends emails for any
// new threshold milestones crossed ($ThresholdCents, 2x, 3x, ...).
func CheckUserSpendAlerts(s *store.Store, sender *mail.Sender, thresholdCents int64) {
	if sender == nil || thresholdCents <= 0 {
		return
	}

	ctx := context.Background()

	users, err := s.QueryUserSpend(ctx)
	if err != nil {
		log.Printf("spend-alert: QueryUserSpend failed: %v", err)
		return
	}
	if len(users) == 0 {
		return
	}

	notified, err := s.LoadUserAlertThresholds(ctx)
	if err != nil {
		log.Printf("spend-alert: LoadUserAlertThresholds failed: %v", err)
		return
	}

	for _, u := range users {
		if u.Email == "" {
			continue
		}
		maxMultiple := u.TodayCents / thresholdCents
		if maxMultiple == 0 {
			continue
		}

		userNotified := notified[u.UserName]
		for multiple := int64(1); multiple <= maxMultiple; multiple++ {
			threshold := multiple * thresholdCents
			if userNotified[threshold] {
				continue
			}

			thresholdDollars := threshold / 100
			subject := "API 额度使用提醒"
			body := mail.BuildAlertBody(u.UserName, u.TodayCents, thresholdDollars, thresholdCents)

			if err := sender.Send(u.Email, subject, body); err != nil {
				log.Printf("spend-alert: failed to send alert to %s (%s): %v",
					u.UserName, u.Email, err)
				continue
			}
			log.Printf("spend-alert: sent $%d alert to %s <%s> (today=$%d)",
				thresholdDollars, u.UserName, u.Email, u.TodayCents)

			if err := s.RecordUserAlert(ctx, u.UserName, threshold); err != nil {
				log.Printf("spend-alert: RecordUserAlert failed for %s threshold=%d: %v",
					u.UserName, threshold, err)
			}
		}
	}
}

// accountQuota holds Codex quota data for a single account fetched via CPA proxy.
type accountQuota struct {
	account       string
	fiveHourUsed  float64
	fiveHourReset string
	weeklyUsed    float64
	weeklyReset   string
	err           string
}

// poolQuotaChecker checks whether all accounts in the pool have exhausted their
// 5-hour or weekly Codex quota windows and sends notification emails.
type poolQuotaChecker struct {
	store       *store.Store
	sender      *mail.Sender
	cpaBaseURL  string
	mgmtKey     string
}

func newPoolQuotaChecker(s *store.Store, sender *mail.Sender, cpaBaseURL, mgmtKey string) *poolQuotaChecker {
	return &poolQuotaChecker{
		store:      s,
		sender:     sender,
		cpaBaseURL: strings.TrimRight(cpaBaseURL, "/"),
		mgmtKey:    mgmtKey,
	}
}

// codexUsageResponse mirrors the Codex /backend-api/wham/usage response shape.
type codexUsageResponse struct {
	RateLimit *struct {
		PrimaryWindow   *codexWindow `json:"primary_window"`
		SecondaryWindow *codexWindow `json:"secondary_window"`
	} `json:"rate_limit"`
}

type codexWindow struct {
	UsedPercent  *float64 `json:"used_percent"`
	LimitSeconds *float64 `json:"limit_window_seconds"`
	ResetAt      *float64 `json:"reset_at"`
}

// CheckPoolQuota runs one round of pool quota exhaustion detection.
func (c *poolQuotaChecker) Check(ctx context.Context) {
	if c.sender == nil || c.cpaBaseURL == "" || c.mgmtKey == "" {
		return
	}

	// 1. Get distinct auth_indices from recent successful usage_events
	authIndices, err := c.store.LoadDistinctAuthIndices(ctx)
	if err != nil {
		log.Printf("pool-quota: LoadDistinctAuthIndices failed: %v", err)
		return
	}
	if len(authIndices) == 0 {
		return
	}

	// Rate limit: only check up to 50 accounts per run
	if len(authIndices) > 50 {
		log.Printf("pool-quota: %d auth indices exceeds limit 50, skipping check to avoid false positives", len(authIndices))
		return
	}

	// 2. For each auth_index, query Codex quota via CPA proxy API
	results := make([]accountQuota, 0, len(authIndices))
	for _, ai := range authIndices {
		q := c.queryAccountQuota(ctx, ai)
		results = append(results, q)
	}

	// 3. Aggregate: find if ALL accounts have 5-hour or weekly at >= 100%
	var total5h, exhausted5h int
	var totalWeek, exhaustedWeek int
	var earliest5hReset, earliestWeekReset string
	var any5hSuccess, anyWeekSuccess bool

	for _, r := range results {
		if r.err != "" {
			log.Printf("pool-quota: account %s error: %s", r.account, r.err)
			continue
		}
		// 5-hour
		if r.fiveHourUsed >= 0 {
			total5h++
			any5hSuccess = true
			if r.fiveHourUsed >= 100 {
				exhausted5h++
			}
			if earliest5hReset == "" || r.fiveHourReset < earliest5hReset {
				earliest5hReset = r.fiveHourReset
			}
		}
		// Weekly
		if r.weeklyUsed >= 0 {
			totalWeek++
			anyWeekSuccess = true
			if r.weeklyUsed >= 100 {
				exhaustedWeek++
			}
			if earliestWeekReset == "" || r.weeklyReset < earliestWeekReset {
				earliestWeekReset = r.weeklyReset
			}
		}
	}

	// 4. Send alerts if all accounts exhausted
	allEmails, err := c.store.LoadAllUserEmails(ctx)
	if err != nil {
		log.Printf("pool-quota: LoadAllUserEmails failed: %v", err)
		return
	}
	if len(allEmails) == 0 {
		return
	}
	// Build recipient list (deduplicate)
	emailSet := make(map[string]string)
	for _, email := range allEmails {
		if email != "" {
			emailSet[email] = email
		}
	}

	c.notifyIfExhausted(ctx, "five_hour", any5hSuccess, total5h, exhausted5h, earliest5hReset, emailSet)
	c.notifyIfExhausted(ctx, "weekly", anyWeekSuccess, totalWeek, exhaustedWeek, earliestWeekReset, emailSet)
}

func (c *poolQuotaChecker) notifyIfExhausted(ctx context.Context, windowType string, anySuccess bool, total, exhausted int, earliestReset string, emails map[string]string) {
	if !anySuccess || total == 0 || exhausted < total {
		// Clear pool quota alert so next full exhaustion re-triggers
		existing, _ := c.store.LoadPoolQuotaAlert(ctx, windowType)
		if existing != nil {
			if err := c.store.UpsertPoolQuotaAlert(ctx, windowType, 0, 0); err != nil {
				log.Printf("pool-quota: clear PoolQuotaAlert(%s) failed: %v", windowType, err)
			}
		}
		return
	}

	// Check if already notified
	existing, err := c.store.LoadPoolQuotaAlert(ctx, windowType)
	if err != nil {
		log.Printf("pool-quota: LoadPoolQuotaAlert(%s) failed: %v", windowType, err)
		return
	}
	if existing != nil && existing.NotifiedAtMS > 0 {
		// Already notified — skip, but re-notify after reset clears the record
		return
	}

	if earliestReset == "" {
		earliestReset = "未知"
	}

	now := time.Now().UnixMilli()
	subject := "API 服务额度耗尽通知"
	body := mail.BuildPoolExhaustedBody(windowType, earliestReset)

	for _, email := range emails {
		if err := c.sender.Send(email, subject, body); err != nil {
			log.Printf("pool-quota: send to %s failed: %v", email, err)
			continue
		}
		log.Printf("pool-quota: sent %s exhaustion alert to %s", windowType, email)
	}

	if err := c.store.UpsertPoolQuotaAlert(ctx, windowType, now, now); err != nil {
		log.Printf("pool-quota: UpsertPoolQuotaAlert(%s) failed: %v", windowType, err)
	}
}

// queryAccountQuota calls CPA proxy to query Codex usage for a single auth_index.
func (c *poolQuotaChecker) queryAccountQuota(ctx context.Context, authIndex string) accountQuota {
	result := accountQuota{account: authIndex, fiveHourUsed: -1, weeklyUsed: -1}

	// Build the CPA api-call request
	apiCallPayload := map[string]any{
		"auth_index": authIndex,
		"method":     "GET",
		"url":        "https://chatgpt.com/backend-api/wham/usage",
	}
	body, _ := json.Marshal(apiCallPayload)

	apiURL := fmt.Sprintf("%s/v0/management/api-call", c.cpaBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(string(body)))
	if err != nil {
		result.err = fmt.Sprintf("create request: %v", err)
		return result
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.mgmtKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		result.err = fmt.Sprintf("request failed: %v", err)
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.err = fmt.Sprintf("status %d", resp.StatusCode)
		return result
	}

	// CPA api-call wraps the response: { status_code, header, body }
	var apiResp struct {
		StatusCode int             `json:"status_code"`
		Body       json.RawMessage `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		result.err = fmt.Sprintf("decode api-call response: %v", err)
		return result
	}

	if apiResp.StatusCode < 200 || apiResp.StatusCode >= 300 {
		result.err = fmt.Sprintf("upstream status %d", apiResp.StatusCode)
		return result
	}

	// Parse the Codex usage response
	var usage codexUsageResponse
	if err := json.Unmarshal(apiResp.Body, &usage); err != nil {
		result.err = fmt.Sprintf("parse codex usage: %v", err)
		return result
	}

	if usage.RateLimit == nil {
		result.err = "no rate_limit in response"
		return result
	}

	// Extract 5-hour and weekly windows
	rl := usage.RateLimit
	if rl.PrimaryWindow != nil && rl.PrimaryWindow.LimitSeconds != nil && *rl.PrimaryWindow.LimitSeconds == float64(18000) {
		// This is a 5-hour window
		if rl.PrimaryWindow.UsedPercent != nil {
			result.fiveHourUsed = *rl.PrimaryWindow.UsedPercent
		}
		if rl.PrimaryWindow.ResetAt != nil {
			result.fiveHourReset = epochToTimeStr(int64(*rl.PrimaryWindow.ResetAt))
		}
	}
	if rl.SecondaryWindow != nil && rl.SecondaryWindow.LimitSeconds != nil && *rl.SecondaryWindow.LimitSeconds == float64(604800) {
		// This is a weekly window
		if rl.SecondaryWindow.UsedPercent != nil {
			result.weeklyUsed = *rl.SecondaryWindow.UsedPercent
		}
		if rl.SecondaryWindow.ResetAt != nil {
			result.weeklyReset = epochToTimeStr(int64(*rl.SecondaryWindow.ResetAt))
		}
	}

	return result
}

func epochToTimeStr(epochSec int64) string {
	t := time.Unix(epochSec, 0)
	return t.Format("2006-01-02 15:04 MST")
}
