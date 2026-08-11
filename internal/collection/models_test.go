package collection

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestNormalizeWebsiteURL проверяет канонические ссылки и SSRF-варианты.
func TestNormalizeWebsiteURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		url     string
		source  string
		wantErr bool
	}{
		{name: "codeforces", url: "https://codeforces.com/problemset/problem/4/A", source: "codeforces"},
		{name: "codeforces contest", url: "https://codeforces.com/contest/4/problem/a?locale=ru", source: "codeforces"},
		{name: "leetcode", url: "https://leetcode.com/problems/two-sum/", source: "leetcode"},
		{name: "leetcode description", url: "https://www.leetcode.com/problems/two-sum/description/?envType=daily", source: "leetcode"},
		{name: "coderun", url: "https://coderun.yandex.ru/problem/median", source: "coderun"},
		{name: "http", url: "http://leetcode.com/problems/two-sum", wantErr: true},
		{name: "userinfo", url: "https://codeforces.com@127.0.0.1/problemset/problem/4/A", wantErr: true},
		{name: "subdomain", url: "https://codeforces.com.example.org/problemset/problem/4/A", wantErr: true},
		{name: "port", url: "https://leetcode.com:8443/problems/two-sum", wantErr: true},
		{name: "encoded slash", url: "https://leetcode.com/problems/two-sum%2Fadmin", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, _, err := NormalizeWebsiteURL(test.url)
			if test.wantErr && err == nil {
				t.Fatal("expected validation error")
			}
			if !test.wantErr && (err != nil || source != test.source) {
				t.Fatalf("unexpected result source=%q err=%v", source, err)
			}
		})
	}
}

// TestValidateCreateInput проверяет allowlist, временные границы и лимиты.
func TestValidateCreateInput(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	from := now.Add(-24 * time.Hour)
	input := CreateInput{
		IdempotencyKey:    uuid.New(),
		TelegramChannels:  []string{"algoses"},
		PublishedFrom:     &from,
		PublishedTo:       &now,
		MaxItemsPerSource: 100,
	}
	if err := ValidateCreateInput(input, map[string]struct{}{"algoses": {}}, now); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	tooOld := now.Add(-32 * 24 * time.Hour)
	input.PublishedFrom = &tooOld
	if err := ValidateCreateInput(input, map[string]struct{}{"algoses": {}}, now); err == nil {
		t.Fatal("expected 31-day range error")
	}
	input.PublishedFrom = &from
	input.TelegramChannels = []string{"unknown"}
	if err := ValidateCreateInput(input, map[string]struct{}{"algoses": {}}, now); err == nil {
		t.Fatal("expected allowlist error")
	}
}
