package codeforces

import (
	"context"
	"flag"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/overmindv/task-hunter/internal/parser/domain"
)

var update = flag.Bool("update", false, "update golden files")

// --- Mock HTTP client ---

type mockHTTPClient struct {
	responses map[string]string // url → body
	callCount int
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	m.callCount++

	body, ok := m.responses[req.URL.String()]
	if !ok {
		return &http.Response{
			StatusCode: 404,
			Body:       io.NopCloser(strings.NewReader("not found")),
		}, nil
	}

	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

// --- Tests ---

// TestNewCollector проверяет создание коллектора.
func TestNewCollector(t *testing.T) {
	c := NewCollector(domain.SourceCodeforces, http.DefaultClient)
	if c.ID() != domain.SourceCodeforces {
		t.Errorf("expected %s, got %s", domain.SourceCodeforces, c.ID())
	}
}

// TestMapRating_Unknown проверяет рейтинг 0.
func TestMapRating_Unknown(t *testing.T) {
	d := mapRatingToDifficulty(0)
	if d != domain.DifficultyUnknown {
		t.Errorf("expected Unknown, got %v", d)
	}
}

// TestMapRating_Easy проверяет лёгкие задачи.
func TestMapRating_Easy(t *testing.T) {
	tests := []struct {
		rating int
		want   domain.Difficulty
	}{
		{800, domain.DifficultyEasy},
		{1000, domain.DifficultyEasy},
		{1199, domain.DifficultyEasy},
	}
	for _, tc := range tests {
		t.Run(string(rune(tc.rating)), func(t *testing.T) {
			d := mapRatingToDifficulty(tc.rating)
			if d != tc.want {
				t.Errorf("rating %d: expected %v, got %v", tc.rating, tc.want, d)
			}
		})
	}
}

// TestMapRating_Medium проверяет средние задачи.
func TestMapRating_Medium(t *testing.T) {
	tests := []struct {
		rating int
		want   domain.Difficulty
	}{
		{1200, domain.DifficultyMedium},
		{1400, domain.DifficultyMedium},
		{1600, domain.DifficultyMedium},
	}
	for _, tc := range tests {
		t.Run(string(rune(tc.rating)), func(t *testing.T) {
			d := mapRatingToDifficulty(tc.rating)
			if d != tc.want {
				t.Errorf("rating %d: expected %v, got %v", tc.rating, tc.want, d)
			}
		})
	}
}

// TestMapRating_Hard проверяет сложные задачи.
func TestMapRating_Hard(t *testing.T) {
	tests := []struct {
		rating int
		want   domain.Difficulty
	}{
		{1601, domain.DifficultyHard},
		{2000, domain.DifficultyHard},
		{3500, domain.DifficultyHard},
	}
	for _, tc := range tests {
		t.Run(string(rune(tc.rating)), func(t *testing.T) {
			d := mapRatingToDifficulty(tc.rating)
			if d != tc.want {
				t.Errorf("rating %d: expected %v, got %v", tc.rating, tc.want, d)
			}
		})
	}
}

// TestProblemToRawTask проверяет преобразование API задачи в RawTask.
func TestProblemToRawTask(t *testing.T) {
	client := &mockHTTPClient{}
	c := NewCollector(domain.SourceCodeforces, client)

	html := readFixture(t, "problem_page.html")

	raw := c.problemToRawTask(cfProblem{
		ContestID: 4,
		Index:     "A",
		Name:      "Watermelon",
		Rating:    800,
		Tags:      []string{"brute force", "math"},
	}, html)

	got := formatRawTask(raw)
	goldenPath := filepath.Join("testdata", "problem_page.golden")

	if *update {
		writeGolden(t, goldenPath, got)
	}

	expected := readGolden(t, goldenPath)
	if got != expected {
		t.Errorf("problem to rawtask mismatch.\n  got:\n%s\n---\n  want:\n%s", got, expected)
	}
}

// TestCollect_MockAPI проверяет сбор задач через mock API.
func TestCollect_MockAPI(t *testing.T) {
	mock := &mockHTTPClient{}

	// API response с одной задачей
	apiResponse := `{
		"status": "OK",
		"result": {
			"problems": [
				{"contestId": 4, "index": "A", "name": "Watermelon", "rating": 800, "tags": ["math"]}
			]
		}
	}`

	problemHTML := `<html><body><div class="problem-statement"><div class="title">A. Watermelon</div><div class="statement"><p>Test</p></div></div></body></html>`

	mock.responses = map[string]string{
		"https://codeforces.com/api/problemset.problems": apiResponse,
		"https://codeforces.com/problemset/problem/4/A":  problemHTML,
	}

	c := NewCollector(domain.SourceCodeforces, mock)
	c.WithMinInterval(time.Millisecond)

	tasks, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	task := tasks[0]
	if task.Source.ID != domain.SourceCodeforces {
		t.Errorf("expected source %s, got %s", domain.SourceCodeforces, task.Source.ID)
	}
	if !strings.Contains(task.SourceURL, "4/A") {
		t.Errorf("expected URL containing '4/A', got %q", task.SourceURL)
	}
}

// TestCollect_Deduplication проверяет, что повторный вызов не создаёт дубликатов.
func TestCollect_Deduplication(t *testing.T) {
	mock := &mockHTTPClient{}

	apiResponse := `{
		"status": "OK",
		"result": {
			"problems": [
				{"contestId": 4, "index": "A", "name": "Watermelon", "rating": 800, "tags": ["math"]}
			]
		}
	}`

	problemHTML := `<html><body>Watermelon</body></html>`

	mock.responses = map[string]string{
		"https://codeforces.com/api/problemset.problems": apiResponse,
		"https://codeforces.com/problemset/problem/4/A":  problemHTML,
	}

	c := NewCollector(domain.SourceCodeforces, mock)
	c.WithMinInterval(time.Millisecond)

	tasks1, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("first collect: %v", err)
	}
	if len(tasks1) != 1 {
		t.Fatalf("expected 1 task on first call, got %d", len(tasks1))
	}

	// Повторный вызов — должны пропустить уже обработанную задачу
	tasks2, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("second collect: %v", err)
	}
	if len(tasks2) != 0 {
		t.Errorf("expected 0 tasks on second call (dedup), got %d", len(tasks2))
	}
}

// TestCollect_APIError проверяет обработку ошибки API.
func TestCollect_APIError(t *testing.T) {
	mock := &mockHTTPClient{}
	mock.responses = map[string]string{
		"https://codeforces.com/api/problemset.problems": `{"status": "FAILED"}`,
	}

	c := NewCollector(domain.SourceCodeforces, mock)
	c.WithMinInterval(time.Millisecond)

	_, err := c.Collect(context.Background())
	if err == nil {
		t.Fatal("expected error for failed API status")
	}
}

// TestRateLimiter проверяет, что rate limiter не пропускает слишком частые запросы.
func TestRateLimiter(t *testing.T) {
	mock := &mockHTTPClient{}
	mock.responses = map[string]string{
		"https://codeforces.com/api/problemset.problems": `{"status": "OK", "result": {"problems": []}}`,
	}

	c := NewCollector(domain.SourceCodeforces, mock)
	c.WithMinInterval(50 * time.Millisecond)

	start := time.Now()
	_, _ = c.fetchProblems(context.Background())
	_, _ = c.fetchProblems(context.Background())
	elapsed := time.Since(start)

	if elapsed < 100*time.Millisecond {
		t.Errorf("expected at least 100ms for 2 requests with 50ms interval, got %v", elapsed)
	}
}

// TestSourceImplements проверяет, что Collector реализует source.Collector.
func TestSourceImplements(t *testing.T) {
	c := NewCollector(domain.SourceCodeforces, http.DefaultClient)
	if err := c.Connect(context.Background()); err != nil {
		t.Errorf("Connect: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// --- helpers ---

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func writeGolden(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write golden %s: %v", path, err)
	}
}

func readGolden(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	return string(data)
}

func formatRawTask(raw domain.RawTask) string {
	var b strings.Builder
	b.WriteString("Source: " + string(raw.Source.ID) + "\n")
	b.WriteString("Name: " + raw.Source.Name + "\n")
	b.WriteString("URL: " + raw.SourceURL + "\n")
	b.WriteString("HTML length: " + fmtLen(len(raw.RawContent)) + "\n")
	return b.String()
}

func fmtLen(n int) string {
	return strings.TrimSpace(strings.Join(strings.Split(strings.Repeat("x", n), ""), ""))
}
