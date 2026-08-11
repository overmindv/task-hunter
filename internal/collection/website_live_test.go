package collection

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestLiveWebsiteExamples проверяет реальные ссылки поддерживаемых сайтов по явному флагу.
func TestLiveWebsiteExamples(t *testing.T) {
	if os.Getenv("RUN_LIVE_WEBSITE_TESTS") != "1" {
		t.Skip("set RUN_LIVE_WEBSITE_TESTS=1")
	}
	reader := NewDirectWebsiteReader(&http.Client{Timeout: 40 * time.Second})
	defer reader.Close()
	worker := NewWorker(nil, nil, nil, nil, "live-test", time.Second, time.Minute)
	tests := []struct {
		source string
		url    string
	}{
		{source: "coderun", url: "https://coderun.yandex.ru/problem/knight-move"},
		{source: "leetcode", url: "https://leetcode.com/problems/two-sum"},
		{source: "codeforces", url: "https://codeforces.com/problemset/problem/1/A"},
	}
	for _, test := range tests {
		t.Run(test.source, func(t *testing.T) {
			raw, err := reader.ReadURL(context.Background(), test.source, test.url)
			if err != nil {
				t.Fatal(err)
			}
			if raw.Title == "" || raw.Statement == "" || len(raw.Examples) == 0 {
				t.Fatalf("incomplete parsed task: title=%q statement=%d examples=%d", raw.Title, len(raw.Statement), len(raw.Examples))
			}
			candidate, err := worker.normalize(context.Background(), uuid.New(), test.source, raw.SourceURL, raw, nil, 0)
			if err != nil {
				t.Fatalf("normalize tasks-it candidate: %v", err)
			}
			if candidate.Difficulty != "easy" && candidate.Difficulty != "medium" && candidate.Difficulty != "hard" {
				t.Fatalf("tasks-it would reject difficulty %q", candidate.Difficulty)
			}
		})
	}
}
