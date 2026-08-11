package collection

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/task-hunter/internal/parser/domain"
)

// TestWorkerNormalizeClassifiesDifficulty проверяет допустимую сложность для tasks-it.
func TestWorkerNormalizeClassifiesDifficulty(t *testing.T) {
	worker := NewWorker(nil, nil, nil, nil, "test", time.Second, time.Minute)
	raw := domain.RawTask{
		Source: domain.Source{
			ID:   domain.SourceLeetCode,
			Name: "LeetCode",
			Type: domain.SourceTypeWebsite,
		},
		RawContent:  []byte("<p>Find two numbers in the array.</p>"),
		SourceURL:   "https://leetcode.com/problems/two-sum",
		RetrievedAt: time.Now().UTC(),
		Title:       "Two Sum",
		Statement:   "Find two numbers in the array.",
	}
	candidate, err := worker.normalize(context.Background(), uuid.New(), "leetcode", raw.SourceURL, raw, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Difficulty == "unknown" {
		t.Fatalf("tasks-it would reject candidate: %#v", candidate)
	}
}
