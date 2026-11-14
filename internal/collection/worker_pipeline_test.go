package collection

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/overmindv/task-hunter/internal/parser/domain"
)

// TestWorkerNormalizeClassifiesDifficulty проверяет допустимую сложность для tasks.
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
		t.Fatalf("tasks would reject candidate: %#v", candidate)
	}
}

// stubWebsiteReader возвращает предзаданный батч, как DirectWebsiteReader.
type stubWebsiteReader struct {
	batch WebsiteBatch
}

func (s *stubWebsiteReader) ReadURL(_ context.Context, _, _ string) (WebsiteBatch, error) {
	return s.batch, nil
}

func (s *stubWebsiteReader) Close() {}

// TestCollectWebsiteProblemList проверяет разворачивание problem-list в кандидатов:
// каждый кандидат получает свой SourceURL и ссылку на список, лимит ограничивает охват.
func TestCollectWebsiteProblemList(t *testing.T) {
	worker := NewWorker(nil, nil, &stubWebsiteReader{
		batch: WebsiteBatch{
			RawTasks: []domain.RawTask{
				{Source: domain.Source{ID: domain.SourceLeetCode, Name: "LeetCode", Type: domain.SourceTypeWebsite}, SourceURL: "https://leetcode.com/problems/two-sum", Statement: "Find two numbers in the array."},
				{Source: domain.Source{ID: domain.SourceLeetCode, Name: "LeetCode", Type: domain.SourceTypeWebsite}, SourceURL: "https://leetcode.com/problems/add-two-numbers", Statement: "Add two numbers."},
				{Source: domain.Source{ID: domain.SourceLeetCode, Name: "LeetCode", Type: domain.SourceTypeWebsite}, SourceURL: "https://leetcode.com/problems/valid-parentheses", Statement: "Validate parentheses."},
			},
			ProblemList: &ProblemListRef{
				Name:      "oy4y273c",
				SourceID:  "leetcode",
				SourceURL: "https://leetcode.com/problem-list/oy4y273c",
			},
		},
	}, nil, "test", time.Second, time.Minute)

	job := Job{ID: uuid.New(), MaxItemsPerSource: 2}
	candidates, err := worker.collectWebsite(context.Background(), job, JobSource{})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates (limit), got %d", len(candidates))
	}
	if candidates[0].SourceURL != "https://leetcode.com/problems/two-sum" {
		t.Fatalf("unexpected first source url %q", candidates[0].SourceURL)
	}
	for _, candidate := range candidates {
		if candidate.ProblemList == nil || candidate.ProblemList.SourceURL != "https://leetcode.com/problem-list/oy4y273c" {
			t.Fatalf("problem list not propagated: %#v", candidate.ProblemList)
		}
	}
}
