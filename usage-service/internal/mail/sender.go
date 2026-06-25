package mail

import (
	"fmt"
	"net/smtp"
	"strings"
)

type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	FromName string
}

type Sender struct {
	cfg Config
}

func NewSender(cfg Config) *Sender {
	return &Sender{cfg: cfg}
}

func (s *Sender) Send(to, subject, htmlBody string) error {
	if s.cfg.Host == "" || s.cfg.Port == 0 {
		return fmt.Errorf("mail: SMTP not configured")
	}
	fromName := s.cfg.FromName
	if fromName == "" {
		fromName = "API Management"
	}
	from := s.cfg.From
	if from == "" {
		from = s.cfg.Username
	}

	header := fmt.Sprintf("From: %s <%s>\r\n", fromName, from)
	header += fmt.Sprintf("To: %s\r\n", to)
	header += fmt.Sprintf("Subject: %s\r\n", subject)
	header += "MIME-Version: 1.0\r\n"
	header += "Content-Type: text/html; charset=UTF-8\r\n"
	header += "\r\n"

	msg := header + htmlBody

	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)

	return smtp.SendMail(addr, auth, from, []string{to}, []byte(msg))
}

// BuildAlertBody builds the HTML email body for a spend threshold alert.
// thresholdCents is the configured step (e.g. 5000 for $50), used for next-threshold hint.
func BuildAlertBody(userName string, todayCents, thresholdDollars, thresholdCents int64) string {
	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html><html><head><meta charset="UTF-8"><style>
body{font-family:Arial,sans-serif;padding:20px;color:#333}
h2{color:#d9534f}
table{border-collapse:collapse;width:100%;margin:12px 0}
th,td{border:1px solid #ddd;padding:8px;text-align:left}
th{background-color:#f5f5f5}
.hint{color:#888;font-size:13px;margin-top:16px}
</style></head><body>`)
	sb.WriteString(fmt.Sprintf(`<h2>API 额度使用告警</h2>
<p>用户 <strong>%s</strong>，您好：</p>
<p>您今日的 API 调用消费已触发 <strong>$%d</strong> 告警阈值，请合理分配 Token 使用。</p>
<table>
<tr><th>项目</th><th>数值</th></tr>
<tr><td>今日消费</td><td><strong>$%.2f</strong></td></tr>
<tr><td>触发阈值</td><td><strong>$%d</strong></td></tr>
</table>
<div class="hint">
<p>建议事项：</p>
<ul>
<li>简单任务请使用轻量模型（如 gpt-5.3-codex-spark、gpt-5.4-mini），降低 Token 消耗</li>
<li>检查是否有异常调用或重复请求</li>
<li>可在管理面板查看详细的用量明细和配额设置</li>
</ul>
</div>`, escapeHTML(userName), thresholdDollars, float64(todayCents)/100, thresholdDollars))

	// Next threshold hint using the configurable step
	nextThreshold := ((todayCents / thresholdCents) + 1) * thresholdCents
	sb.WriteString(fmt.Sprintf(`<p style="color:#999;font-size:12px">本邮件由系统自动发送。
今日下一个告警阈值：$%d（消费达 $%d 时触发）</p>`, nextThreshold/100, nextThreshold/100))
	sb.WriteString(`</body></html>`)
	return sb.String()
}

// BuildPoolExhaustedBody builds the HTML email body for pool quota exhaustion notification.
func BuildPoolExhaustedBody(windowType string, earliestReset string) string {
	var sb strings.Builder
	windowLabel := "5 小时额度"
	if windowType == "weekly" {
		windowLabel = "周额度"
	}
	sb.WriteString(`<!DOCTYPE html><html><head><meta charset="UTF-8"><style>
body{font-family:Arial,sans-serif;padding:20px;color:#333}
h2{color:#d9534f}
.hint{color:#888;font-size:13px;margin-top:16px}
</style></head><body>`)
	sb.WriteString(fmt.Sprintf(`<h2>API 服务池额度耗尽通知</h2>
<p>各位用户，您好：</p>
<p>当前 API 服务池中所有账号的 <strong>%s</strong> 已全部耗尽。</p>
<p>在额度刷新之前，部分请求可能无法正常处理。</p>
<table>
<tr><td>耗尽项</td><td><strong>%s</strong></td></tr>
<tr><td>预计重置时间</td><td><strong>%s</strong></td></tr>
</table>
<div class="hint">
<p>建议事项：</p>
<ul>
<li>请耐心等待额度自动刷新</li>
<li>简单任务请优先选用轻量模型，降低 Token 消耗</li>
</ul>
<p>本邮件由系统自动发送，请勿回复。</p>
</div>`, windowLabel, windowLabel, escapeHTML(earliestReset)))
	sb.WriteString(`</body></html>`)
	return sb.String()
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}
