package coderun

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"diploma/internal/parser/domain"
)

var update = flag.Bool("update", false, "update golden files")

// --- Mock HTTP client ---

type mockHTTPClient struct {
	responses    map[string]mockResponse
	callCount    int
	defaultError error
}

type mockResponse struct {
	statusCode int
	body       string
	headers    map[string]string
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	m.callCount++

	if m.defaultError != nil {
		return nil, m.defaultError
	}

	resp, ok := m.responses[req.URL.String()]
	if !ok {
		return &http.Response{
			StatusCode: 404,
			Body:       io.NopCloser(strings.NewReader("not found")),
		}, nil
	}

	header := http.Header{}
	for k, v := range resp.headers {
		header.Set(k, v)
	}

	return &http.Response{
		StatusCode: resp.statusCode,
		Body:       io.NopCloser(strings.NewReader(resp.body)),
		Header:     header,
	}, nil
}

// --- Tests ---

// TestNewCollector проверяет создание коллектора.
func TestNewCollector(t *testing.T) {
	c := NewCollector(domain.SourceCodeRun, http.DefaultClient)
	if c.ID() != domain.SourceCodeRun {
		t.Errorf("expected %s, got %s", domain.SourceCodeRun, c.ID())
	}
}

// TestSourceImplements проверяет, что Collector реализует source.Collector.
func TestSourceImplements(t *testing.T) {
	c := NewCollector(domain.SourceCodeRun, http.DefaultClient)
	if err := c.Connect(context.Background()); err != nil {
		t.Errorf("Connect: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestParseCatalog проверяет парсинг страницы каталога (golden-тест).
func TestParseCatalog(t *testing.T) {
	mock := &mockHTTPClient{}
	mock.responses = map[string]mockResponse{
		"https://coderun.yandex.ru/catalog": {
			statusCode: 200,
			body:       readFixture(t, "catalog.html"),
		},
	}

	c := NewCollector(domain.SourceCodeRun, mock)
	c.WithMinInterval(time.Millisecond)

	problems, err := c.fetchCatalog(context.Background())
	if err != nil {
		t.Fatalf("fetchCatalog: %v", err)
	}

	got := formatCatalogProblems(problems)
	goldenPath := filepath.Join("testdata", "catalog.golden")

	if *update {
		writeGolden(t, goldenPath, got)
	}

	expected := readGolden(t, goldenPath)
	if got != expected {
		t.Errorf("catalog parsing mismatch.\n  got:\n%s\n---\n  want:\n%s", got, expected)
	}
}

// TestParseProblemPage проверяет парсинг страницы задачи (golden-тест).
func TestParseProblemPage(t *testing.T) {
	mock := &mockHTTPClient{}
	mock.responses = map[string]mockResponse{
		"https://coderun.yandex.ru/catalog": {
			statusCode: 200,
			body:       readFixture(t, "catalog.html"),
		},
		"https://coderun.yandex.ru/problem/median-out-of-three": {
			statusCode: 200,
			body:       readFixture(t, "problem_median.html"),
		},
		"https://coderun.yandex.ru/problem/two-sum": {
			statusCode: 200,
			body:       readFixture(t, "problem_median.html"),
		},
		"https://coderun.yandex.ru/problem/binary-search": {
			statusCode: 200,
			body:       readFixture(t, "problem_median.html"),
		},
	}

	c := NewCollector(domain.SourceCodeRun, mock)
	c.WithMinInterval(time.Millisecond)

	tasks, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks from catalog, got %d", len(tasks))
	}

	// Проверяем преобразование задачи в RawTask
	task := tasks[0]
	if task.Source.ID != domain.SourceCodeRun {
		t.Errorf("expected source %s, got %s", domain.SourceCodeRun, task.Source.ID)
	}
	if !strings.Contains(task.SourceURL, "median-out-of-three") {
		t.Errorf("expected URL containing 'median-out-of-three', got %q", task.SourceURL)
	}

	got := formatRawTask(task)
	goldenPath := filepath.Join("testdata", "problem_median.golden")

	if *update {
		writeGolden(t, goldenPath, got)
	}

	expected := readGolden(t, goldenPath)
	if got != expected {
		t.Errorf("problem page mismatch.\n  got:\n%s\n---\n  want:\n%s", got, expected)
	}
}

// TestCollect_404 проверяет обработку 404 (пропуск задачи).
func TestCollect_404(t *testing.T) {
	mock := &mockHTTPClient{}
	mock.responses = map[string]mockResponse{
		"https://coderun.yandex.ru/catalog": {
			statusCode: 200,
			body:       readFixture(t, "catalog.html"),
		},
		"https://coderun.yandex.ru/problem/median-out-of-three": {
			statusCode: 404,
			body:       "not found",
		},
		"https://coderun.yandex.ru/problem/two-sum": {
			statusCode: 200,
			body:       readFixture(t, "problem_median.html"),
		},
		"https://coderun.yandex.ru/problem/binary-search": {
			statusCode: 200,
			body:       readFixture(t, "problem_median.html"),
		},
	}

	c := NewCollector(domain.SourceCodeRun, mock)
	c.WithMinInterval(time.Millisecond)

	tasks, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	// Должны получить 2 задачи (одна с 404 пропущена)
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks (1 skipped due to 404), got %d", len(tasks))
	}
}

// TestCollect_EmptyHTML проверяет обработку страницы без условия задачи.
func TestCollect_EmptyHTML(t *testing.T) {
	mock := &mockHTTPClient{}
	mock.responses = map[string]mockResponse{
		"https://coderun.yandex.ru/catalog": {
			statusCode: 200,
			body:       readFixture(t, "catalog.html"),
		},
		"https://coderun.yandex.ru/problem/median-out-of-three": {
			statusCode: 200,
			body:       "<html><body>Login required</body></html>",
		},
		"https://coderun.yandex.ru/problem/two-sum": {
			statusCode: 200,
			body:       readFixture(t, "problem_median.html"),
		},
		"https://coderun.yandex.ru/problem/binary-search": {
			statusCode: 200,
			body:       readFixture(t, "problem_median.html"),
		},
	}

	c := NewCollector(domain.SourceCodeRun, mock)
	c.WithMinInterval(time.Millisecond)

	tasks, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	// Должны получить 2 задачи (одна без statement пропущена, но не создаёт ошибку)
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks (1 skipped due to empty statement), got %d", len(tasks))
	}
}

// TestCollect_APICatalogError проверяет ошибку каталога.
func TestCollect_APICatalogError(t *testing.T) {
	mock := &mockHTTPClient{}
	mock.responses = map[string]mockResponse{
		"https://coderun.yandex.ru/catalog": {
			statusCode: 500,
			body:       "internal error",
		},
	}

	c := NewCollector(domain.SourceCodeRun, mock)
	c.WithMinInterval(time.Millisecond)

	_, err := c.Collect(context.Background())
	if err == nil {
		t.Fatal("expected error for failed catalog request")
	}
}

// TestRateLimiter проверяет, что rate limiter не пропускает слишком частые запросы.
func TestRateLimiter(t *testing.T) {
	mock := &mockHTTPClient{}
	mock.responses = map[string]mockResponse{
		"https://coderun.yandex.ru/catalog": {
			statusCode: 200,
			body:       `<html><body><ul class="problem-list"></ul></body></html>`,
		},
	}

	c := NewCollector(domain.SourceCodeRun, mock)
	c.WithMinInterval(50 * time.Millisecond)

	start := time.Now()
	_, _ = c.fetchCatalog(context.Background())
	_, _ = c.fetchCatalog(context.Background())
	elapsed := time.Since(start)

	if elapsed < 100*time.Millisecond {
		t.Errorf("expected at least 100ms for 2 requests with 50ms interval, got %v", elapsed)
	}
}

// TestHasProblemStatement проверяет детекцию условия задачи.
func TestHasProblemStatement(t *testing.T) {
	if !hasProblemStatement(readFixture(t, "problem_median.html")) {
		t.Error("expected true for valid problem page")
	}
	if hasProblemStatement("<html><body>Login</body></html>") {
		t.Error("expected false for page without problem statement")
	}
	if hasProblemStatement("") {
		t.Error("expected false for empty string")
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

func formatCatalogProblems(problems []catalogProblem) string {
	var b strings.Builder
	for i, p := range problems {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(fmt.Sprintf("%d. %s [%s] → /problem/%s", i+1, p.Title, p.Difficulty, p.Slug))
	}
	return b.String()
}

func formatRawTask(raw domain.RawTask) string {
	var b strings.Builder
	b.WriteString("Source: " + string(raw.Source.ID) + "\n")
	b.WriteString("Name: " + raw.Source.Name + "\n")
	b.WriteString("URL: " + raw.SourceURL + "\n")
	b.WriteString("Content length: " + fmt.Sprint(len(raw.RawContent)) + "\n")
	return b.String()
}
