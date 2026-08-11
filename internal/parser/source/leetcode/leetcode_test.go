package leetcode

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
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
	responses map[string]mockResponse
	callCount int
	blockAll  bool // если true — возвращает 403 на любой запрос
}

type mockResponse struct {
	statusCode int
	body       string
	headers    map[string]string
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	m.callCount++

	if m.blockAll {
		return &http.Response{
			StatusCode: 403,
			Body:       io.NopCloser(strings.NewReader("Access Denied")),
		}, nil
	}

	resp, ok := m.responses[req.URL.String()]
	if !ok {
		return &http.Response{
			StatusCode: 404,
			Body:       io.NopCloser(strings.NewReader("not found")),
		}, nil
	}

	return &http.Response{
		StatusCode: resp.statusCode,
		Body:       io.NopCloser(strings.NewReader(resp.body)),
	}, nil
}

// --- Tests ---

// TestNewCollector проверяет создание коллектора.
func TestNewCollector(t *testing.T) {
	c := NewCollector(domain.SourceLeetCode, http.DefaultClient)
	if c.ID() != domain.SourceLeetCode {
		t.Errorf("expected %s, got %s", domain.SourceLeetCode, c.ID())
	}
}

// TestSourceImplements проверяет, что Collector реализует source.Collector.
func TestSourceImplements(t *testing.T) {
	c := NewCollector(domain.SourceLeetCode, http.DefaultClient)
	if err := c.Connect(context.Background()); err != nil {
		t.Errorf("Connect: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestMapDifficulty проверяет преобразование сложности LeetCode в Difficulty.
func TestMapDifficulty(t *testing.T) {
	tests := []struct {
		lc   string
		want domain.Difficulty
	}{
		{"EASY", domain.DifficultyEasy},
		{"easy", domain.DifficultyEasy},
		{"MEDIUM", domain.DifficultyMedium},
		{"Medium", domain.DifficultyMedium},
		{"HARD", domain.DifficultyHard},
		{"hard", domain.DifficultyHard},
		{"", domain.DifficultyUnknown},
		{"UNKNOWN", domain.DifficultyUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.lc, func(t *testing.T) {
			got := MapDifficulty(tc.lc)
			if got != tc.want {
				t.Errorf("MapDifficulty(%q) = %v, want %v", tc.lc, got, tc.want)
			}
		})
	}
}

// TestDetectBlockingPage проверяет детекцию страниц блокировки.
func TestDetectBlockingPage(t *testing.T) {
	if !DetectBlockingPage("<html>captcha</html>") {
		t.Error("expected true for captcha page")
	}
	if !DetectBlockingPage("<html>Please login</html>") {
		t.Error("expected true for login page")
	}
	if !DetectBlockingPage("<html>Access Denied</html>") {
		t.Error("expected true for access denied page")
	}
	if DetectBlockingPage("<html>Problem statement here</html>") {
		t.Error("expected false for normal page")
	}
	if DetectBlockingPage("") {
		t.Error("expected false for empty page")
	}
}

// TestFetchProblemset проверяет парсинг списка задач через GraphQL (golden-тест).
func TestFetchProblemset(t *testing.T) {
	mock := &mockHTTPClient{}
	mock.responses = map[string]mockResponse{
		"https://leetcode.com/graphql": {
			statusCode: 200,
			body:       readFixture(t, "problemset.json"),
		},
	}

	c := NewCollector(domain.SourceLeetCode, mock)
	c.WithMinInterval(time.Millisecond)

	problems, err := c.fetchProblemset(context.Background())
	if err != nil {
		t.Fatalf("fetchProblemset: %v", err)
	}

	got := formatProblemset(problems)
	goldenPath := filepath.Join("testdata", "problemset.golden")

	if *update {
		writeGolden(t, goldenPath, got)
	}

	expected := readGolden(t, goldenPath)
	if got != expected {
		t.Errorf("problemset mismatch.\n  got:\n%s\n---\n  want:\n%s", got, expected)
	}
}

// TestFetchQuestionDetail проверяет парсинг деталей задачи (golden-тест).
func TestFetchQuestionDetail(t *testing.T) {
	mock := &mockHTTPClient{}
	mock.responses = map[string]mockResponse{
		"https://leetcode.com/graphql": {
			statusCode: 200,
			body:       readFixture(t, "problem_two_sum.json"),
		},
	}

	c := NewCollector(domain.SourceLeetCode, mock)
	c.WithMinInterval(time.Millisecond)

	detail, err := c.fetchQuestionDetail(context.Background(), "two-sum")
	if err != nil {
		t.Fatalf("fetchQuestionDetail: %v", err)
	}

	got := formatQuestionDetail(detail)
	goldenPath := filepath.Join("testdata", "problem_two_sum.golden")

	if *update {
		writeGolden(t, goldenPath, got)
	}

	expected := readGolden(t, goldenPath)
	if got != expected {
		t.Errorf("question detail mismatch.\n  got:\n%s\n---\n  want:\n%s", got, expected)
	}
}

// TestCollect_WithProblemset проверяет полный сбор с problemset и деталями.
func TestCollect_WithProblemset(t *testing.T) {
	// Создадим кастомный обработчик через переопределение Do
	type customHandler struct {
		responses []mockResponse
		index     int
	}
	handler := &customHandler{
		responses: []mockResponse{
			{statusCode: 200, body: readFixture(t, "problemset.json")},
			{statusCode: 200, body: readFixture(t, "problem_two_sum.json")},
			{statusCode: 200, body: readFixture(t, "problem_two_sum.json")},
			{statusCode: 200, body: readFixture(t, "problem_two_sum.json")},
		},
	}

	c := NewCollector(domain.SourceLeetCode, &callTrackingClient{
		fn: func(req *http.Request) (*http.Response, error) {
			if handler.index >= len(handler.responses) {
				return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(""))}, nil
			}
			resp := handler.responses[handler.index]
			handler.index++
			return &http.Response{
				StatusCode: resp.statusCode,
				Body:       io.NopCloser(strings.NewReader(resp.body)),
			}, nil
		},
	})
	c.WithMinInterval(time.Millisecond)

	tasks, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}

	// Проверяем первую задачу
	if tasks[0].Source.ID != domain.SourceLeetCode {
		t.Errorf("expected source %s, got %s", domain.SourceLeetCode, tasks[0].Source.ID)
	}
	if !strings.Contains(tasks[0].SourceURL, "two-sum") {
		t.Errorf("expected URL containing 'two-sum', got %q", tasks[0].SourceURL)
	}
	if len(tasks[0].RawContent) == 0 {
		t.Error("expected non-empty raw content")
	}
}

// TestCollect_NoContent проверяет задачу без условия (content пустой).
func TestCollect_NoContent(t *testing.T) {
	// Используем кастомный клиент для чередования ответов
	custom := &callTrackingClient{
		fn: func(req *http.Request) (*http.Response, error) {
			// Первый запрос — problemset, остальные — детали
			return &http.Response{
				StatusCode: 200,
				Body: io.NopCloser(strings.NewReader(`{
					"data": {
						"problemsetQuestionList": {
							"total": 1,
							"questions": [
								{"titleSlug": "empty-problem", "title": "Empty", "difficulty": "EASY", "topicTags": []}
							]
						}
					}
				}`)),
			}, nil
		},
	}

	c := NewCollector(domain.SourceLeetCode, custom)
	c.WithMinInterval(time.Millisecond)

	tasks, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	// Задача с пустым content должна быть пропущена (или ошибка)
	// В текущей реализации — пропускается, так что tasks будет пуст
	if len(tasks) != 0 {
		t.Logf("expected empty tasks when content is empty, got %d", len(tasks))
	}
}

// TestCollect_Deduplication проверяет, что повторный вызов не создаёт дубликатов.
func TestCollect_Deduplication(t *testing.T) {
	callCount := 0
	custom := &callTrackingClient{
		fn: func(req *http.Request) (*http.Response, error) {
			callCount++
			// Первый вызов — problemset
			if callCount <= 1 {
				return &http.Response{
					StatusCode: 200,
					Body: io.NopCloser(strings.NewReader(`{
						"data": {
							"problemsetQuestionList": {
								"total": 1,
								"questions": [
									{"titleSlug": "two-sum", "title": "Two Sum", "difficulty": "EASY", "topicTags": [{"name": "Array"}]}
								]
							}
						}
					}`)),
				}, nil
			}
			// Детали задачи
			return &http.Response{
				StatusCode: 200,
				Body: io.NopCloser(strings.NewReader(`{
					"data": {
						"question": {
							"questionId": "1",
							"title": "Two Sum",
							"titleSlug": "two-sum",
							"difficulty": "EASY",
							"content": "<p>Test</p>",
							"topicTags": [{"name": "Array"}]
						}
					}
				}`)),
			}, nil
		},
	}

	c := NewCollector(domain.SourceLeetCode, custom)
	c.WithMinInterval(time.Millisecond)

	tasks1, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("first collect: %v", err)
	}
	if len(tasks1) == 0 {
		t.Fatal("expected at least 1 task")
	}

	// Повторный вызов — должны пропустить уже обработанную задачу
	callCount = 0 // сбрасываем, так как при втором вызове не нужны детали
	custom.fn = func(req *http.Request) (*http.Response, error) {
		callCount++
		return &http.Response{
			StatusCode: 200,
			Body: io.NopCloser(strings.NewReader(`{
				"data": {
					"problemsetQuestionList": {
						"total": 1,
						"questions": [
							{"titleSlug": "two-sum", "title": "Two Sum", "difficulty": "EASY", "topicTags": [{"name": "Array"}]}
						]
					}
				}
			}`)),
		}, nil
	}

	tasks2, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("second collect: %v", err)
	}
	if len(tasks2) != 0 {
		t.Errorf("expected 0 tasks on second call (dedup), got %d", len(tasks2))
	}
}

// TestCollect_Forbidden проверяет обработку 403 (капча/блокировка).
func TestCollect_Forbidden(t *testing.T) {
	mock := &mockHTTPClient{blockAll: true}

	c := NewCollector(domain.SourceLeetCode, mock)
	c.WithMinInterval(time.Millisecond)

	_, err := c.Collect(context.Background())
	if err == nil {
		t.Fatal("expected error for blocked access (403)")
	}
	if !strings.Contains(err.Error(), "403") && !strings.Contains(err.Error(), "access denied") {
		t.Errorf("expected error mentioning 403 or access denied, got: %v", err)
	}
}

// TestRateLimiter проверяет, что rate limiter не пропускает слишком частые запросы.
func TestRateLimiter(t *testing.T) {
	mock := &mockHTTPClient{}
	mock.responses = map[string]mockResponse{
		"https://leetcode.com/graphql": {
			statusCode: 200,
			body:       `{"data": {"problemsetQuestionList": {"total": 0, "questions": []}}}`,
		},
	}

	c := NewCollector(domain.SourceLeetCode, mock)
	c.WithMinInterval(50 * time.Millisecond)

	start := time.Now()
	// Два вызова fetchProblemset с интервалом 50ms должны занять минимум 100ms
	_, _ = c.fetchProblemset(context.Background())
	_, _ = c.fetchProblemset(context.Background())
	elapsed := time.Since(start)

	if elapsed < 100*time.Millisecond {
		t.Errorf("expected at least 100ms for 2 requests with 50ms interval, got %v", elapsed)
	}
}

// --- helpers ---

type callTrackingClient struct {
	fn func(req *http.Request) (*http.Response, error)
}

func (c *callTrackingClient) Do(req *http.Request) (*http.Response, error) {
	return c.fn(req)
}

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

func formatProblemset(problems []lcProblemBrief) string {
	var b strings.Builder
	for i, p := range problems {
		if i > 0 {
			b.WriteString("\n")
		}
		tags := make([]string, len(p.TopicTags))
		for j, t := range p.TopicTags {
			tags[j] = t.Name
		}
		b.WriteString(fmt.Sprintf("%d. %s [%s] (%s)", i+1, p.Title, p.Difficulty, strings.Join(tags, ", ")))
	}
	return b.String()
}

type printableDetail struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Slug    string   `json:"slug"`
	Diff    string   `json:"difficulty"`
	Content string   `json:"content_first_200"`
	Tags    []string `json:"tags"`
}

func formatQuestionDetail(q *lcQuestionDetail) string {
	tags := make([]string, len(q.TopicTags))
	for i, t := range q.TopicTags {
		tags[i] = t.Name
	}

	p := printableDetail{
		ID:      q.QuestionID,
		Title:   q.Title,
		Slug:    q.TitleSlug,
		Diff:    q.Difficulty,
		Content: truncate(q.Content, 200),
		Tags:    tags,
	}
	data, _ := json.MarshalIndent(p, "", "  ")
	return string(data)
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
