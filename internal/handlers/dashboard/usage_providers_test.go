package dashboard

import (
	"context"
	"strings"
	"testing"
	"time"

	"9router/proxy/internal/models"
)

func testUsageConn(provider, authType string) *models.ProviderConnection {
	return &models.ProviderConnection{ID: "t", Provider: provider, AuthType: authType}
}

func TestUsageResetTime(t *testing.T) {
	if got := usageResetTime(nil); got != "" {
		t.Errorf("nil: got %q", got)
	}
	// Unix seconds → RFC3339.
	if got := usageResetTime(float64(1700000000)); !strings.HasPrefix(got, "2023-11-14") {
		t.Errorf("seconds: got %q", got)
	}
	// Unix millis.
	if got := usageResetTime(float64(1700000000000)); !strings.HasPrefix(got, "2023-11-14") {
		t.Errorf("millis: got %q", got)
	}
	// Numeric string.
	if got := usageResetTime("1700000000"); !strings.HasPrefix(got, "2023-11-14") {
		t.Errorf("numeric string: got %q", got)
	}
	// ISO passthrough.
	if got := usageResetTime("2026-01-02T03:04:05Z"); got != "2026-01-02T03:04:05Z" {
		t.Errorf("iso: got %q", got)
	}
	if got := usageResetTime("not-a-date"); got != "" {
		t.Errorf("garbage: got %q", got)
	}
}

func TestMaskConnectionName(t *testing.T) {
	short := "Work account"
	if got := maskConnectionName(short); got != short {
		t.Errorf("short names untouched, got %q", got)
	}
	long := "abcdefghijklmnopqrstuvwxyz0123456789ABCD"
	if got := maskConnectionName(long); got != "abcdefgh***" {
		t.Errorf("token-like masked, got %q", got)
	}
	// Long but not token-like stays.
	human := "This is a fairly long connection name"
	if got := maskConnectionName(human); got != human {
		t.Errorf("human name untouched, got %q", got)
	}
}

func TestParseGroqDurationMs(t *testing.T) {
	ms, ok := parseGroqDurationMs("2m59.56s")
	if !ok || ms < 179000 || ms > 180000 {
		t.Errorf("2m59.56s: got %d,%v", ms, ok)
	}
	if _, ok := parseGroqDurationMs(""); ok {
		t.Errorf("empty must not parse")
	}
	if _, ok := parseGroqDurationMs("tomorrow"); ok {
		t.Errorf("garbage must not parse")
	}
}

func TestGrokMakeQuota(t *testing.T) {
	q := grokMakeQuota(30, 100, "")
	if q["used"] != 30.0 || q["total"] != 100.0 || q["remainingPercentage"] != 70.0 {
		t.Errorf("unexpected quota: %v", q)
	}
	q = grokMakeQuota(5, 0, "")
	if q["total"] != 0 || q["unlimited"] != true {
		t.Errorf("zero total must be unlimited row: %v", q)
	}
}

func TestUsageQuotaShape(t *testing.T) {
	q := usageQuota(25, 100, "2026-01-01T00:00:00Z")
	if q["remainingPercentage"] != 75.0 || q["resetAt"] != "2026-01-01T00:00:00Z" || q["unlimited"] != false {
		t.Errorf("unexpected: %v", q)
	}
}

func TestGitHubUsagePaid(t *testing.T) {
	res := parseGitHubUsage(map[string]any{
		"copilot_plan": "enterprise", "quota_reset_date": "2026-10-01",
		"quota_snapshots": map[string]any{
			"chat": map[string]any{"entitlement": float64(0), "remaining": float64(0), "unlimited": true},
			"premium_interactions": map[string]any{
				"entitlement": float64(75000), "remaining": float64(56796), "percent_remaining": float64(75.7),
			},
		},
	})
	if res.plan != "enterprise" || len(res.quotas) != 2 {
		t.Fatalf("unexpected plan/quotas: %+v", res)
	}
	chat := res.quotas["chat"].(map[string]any)
	if chat["unlimited"] != true || chat["resetAt"] == nil {
		t.Errorf("unlimited chat quota: %v", chat)
	}
	premium := res.quotas["premium_interactions"].(map[string]any)
	if premium["used"] != float64(18204) || premium["total"] != float64(75000) || premium["remaining"] != float64(56796) || premium["remainingPercentage"] != float64(75.7) {
		t.Errorf("premium quota: %v", premium)
	}
}

func TestGitHubUsageFreeAndMissingToken(t *testing.T) {
	res := parseGitHubUsage(map[string]any{
		"access_type_sku": "free", "limited_user_reset_date": "2026-10-01",
		"monthly_quotas":      map[string]any{"chat": float64(50), "completions": float64(2000)},
		"limited_user_quotas": map[string]any{"chat": float64(12)},
	})
	if res.plan != "free" || len(res.quotas) != 2 {
		t.Fatalf("unexpected free quotas: %+v", res)
	}
	chat := res.quotas["chat"].(map[string]any)
	if chat["used"] != float64(12) || chat["remainingPercentage"] != float64(76) {
		t.Errorf("free chat quota: %v", chat)
	}
	missing, ok := fetchProviderUsage(context.Background(), "github", nil)
	if !ok || missing.message == "" || len(missing.quotas) != 0 {
		t.Errorf("missing GitHub token: %+v (handled=%v)", missing, ok)
	}
}

func TestCodebuddyIsRefill(t *testing.T) {
	// DeductionEndTime is unix ms; gap >2d vs cycle end = refill.
	refill := map[string]any{
		"CycleEndTime":     "2026-09-30 23:59:59",
		"DeductionEndTime": float64(2049542858000),
	}
	if !codebuddyIsRefill(refill) {
		t.Errorf("far-expiry pack must be refill")
	}
	bonus := map[string]any{
		"CycleEndTime":     "2026-09-26 21:27:37",
		"DeductionEndTime": float64(1790429257000),
	}
	if codebuddyIsRefill(bonus) {
		t.Errorf("same-day-expiry pack must be bonus")
	}
}

func TestUsageResetTimeNaiveLocal(t *testing.T) {
	// Naive datetimes parse in server-local zone (mirrors JS new Date(str)).
	got := usageResetTime("2026-09-30 23:59:59")
	want := time.Date(2026, 9, 30, 23, 59, 59, 0, time.Local).UTC().Format(time.RFC3339)
	if got != want {
		t.Errorf("naive local: got %q want %q", got, want)
	}
}

func TestIsUsageEligibleConnection(t *testing.T) {
	cases := []struct {
		provider, authType string
		want               bool
	}{
		{"codex", "oauth", true},
		{"kimi", "apikey", true},
		{"kimi", "oauth", true},
		{"openai", "apikey", false},
		{"deepseek", "apikey", true},
		{"comfyui", "apikey", false},
	}
	for _, c := range cases {
		conn := testUsageConn(c.provider, c.authType)
		if got := isUsageEligibleConnection(conn); got != c.want {
			t.Errorf("%s/%s: got %v want %v", c.provider, c.authType, got, c.want)
		}
	}
}
