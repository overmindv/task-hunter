package pipeline

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/overmindv/task-hunter/internal/parser/domain"
)

// update flag для перезаписи golden-файлов: go test -update
var update = flag.Bool("update", false, "update golden files")

// --- Pipeline orchestration tests ---

func TestPipeline_RunSequential(t *testing.T) {
	ctx := context.Background()
	p := NewPipeline()

	var order []string

	p.AddProcessor("first", ProcessorFunc(func(_ context.Context, _ domain.RawTask, task *domain.Task) error {
		order = append(order, "first")
		task.Title = "first"
		return nil
	}))

	p.AddProcessor("second", ProcessorFunc(func(_ context.Context, _ domain.RawTask, task *domain.Task) error {
		if task.Title != "first" {
			t.Errorf("expected title 'first' from previous stage, got %q", task.Title)
		}
		order = append(order, "second")
		task.Description = "second"
		return nil
	}))

	raw := testRawTask()
	result, err := p.Run(ctx, raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(order) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(order))
	}
	if len(result.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(result.Stages))
	}
}

func TestPipeline_ErrorStopsChain(t *testing.T) {
	ctx := context.Background()
	p := NewPipeline()

	var calledAfterError bool

	p.AddProcessor("ok", ProcessorFunc(func(_ context.Context, _ domain.RawTask, task *domain.Task) error {
		task.Title = "ok"
		return nil
	}))

	p.AddProcessor("fails", ProcessorFunc(func(_ context.Context, _ domain.RawTask, _ *domain.Task) error {
		return errors.New("stage error")
	}))

	p.AddProcessor("never_called", ProcessorFunc(func(_ context.Context, _ domain.RawTask, _ *domain.Task) error {
		calledAfterError = true
		return nil
	}))

	_, err := p.Run(ctx, testRawTask())
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if calledAfterError {
		t.Error("processor after error should not be called")
	}

	var stageErr *StageError
	if !errors.As(err, &stageErr) {
		t.Errorf("expected *StageError, got %T", err)
	}
}

func TestPipeline_EmptyPipeline(t *testing.T) {
	_, err := NewPipeline().Run(context.Background(), testRawTask())
	if err == nil {
		t.Fatal("expected error for empty pipeline")
	}
}

func TestNewDefaultPipeline(t *testing.T) {
	p := NewDefaultPipeline()
	result, err := p.Run(context.Background(), testRawTask())
	if err != nil {
		t.Fatalf("default pipeline failed: %v", err)
	}
	if result.Task.Source.ID != domain.SourceLeetCode {
		t.Errorf("expected SourceLeetCode, got %v", result.Task.Source.ID)
	}
	if result.Task.SourceHash == "" {
		t.Error("expected source_hash to be generated")
	}
}

// --- Extractor tests ---

func TestExtractor_CodeforcesHTML(t *testing.T) {
	extractor := NewExtractor()
	raw := loadRawTask(t, "extractor_codeforces.html", domain.SourceCodeforces, domain.SourceTypeWebsite)

	task := &domain.Task{}
	err := extractor.Process(context.Background(), raw, task)
	if err != nil {
		t.Fatalf("extractor: %v", err)
	}

	got := task.Title + "\n---\n" + task.Description
	goldenPath := filepath.Join("testdata", "extractor_codeforces.golden")

	if *update {
		writeGolden(t, goldenPath, got)
	}

	if task.SourceHash == "" {
		t.Error("expected source_hash to be set")
	}

	expected := readGolden(t, goldenPath)
	if got != expected {
		t.Errorf("extracted content mismatch.\n  got:\n%s\n---\n  want:\n%s", got, expected)
	}
}

func TestExtractor_LeetCodeHTML(t *testing.T) {
	extractor := NewExtractor()
	raw := loadRawTask(t, "extractor_leetcode.html", domain.SourceLeetCode, domain.SourceTypeWebsite)

	task := &domain.Task{}
	err := extractor.Process(context.Background(), raw, task)
	if err != nil {
		t.Fatalf("extractor: %v", err)
	}

	got := task.Title + "\n---\n" + task.Description
	goldenPath := filepath.Join("testdata", "extractor_leetcode.golden")

	if *update {
		writeGolden(t, goldenPath, got)
	}

	expected := readGolden(t, goldenPath)
	if got != expected {
		t.Errorf("extracted content mismatch.\n  got:\n%s\n---\n  want:\n%s", got, expected)
	}
}

// TestExtractor_CodeRunCurrentLayout проверяет стабильную область новой SPA-разметки.
func TestExtractor_CodeRunCurrentLayout(t *testing.T) {
	extractor := NewExtractor()
	raw := domain.RawTask{
		Source: domain.Source{
			ID:   domain.SourceCodeRun,
			Name: "CodeRun",
			Type: domain.SourceTypeWebsite,
		},
		RawContent:  []byte(`<html><body><nav>Лишняя навигация</nav><main><div id="tab-description-panel"><h1 data-testid="problem-title">Калькулятор</h1><section><h2>Условие</h2><p>Вычислите значение выражения.</p></section></div><aside>Лишняя панель</aside></main></body></html>`),
		SourceURL:   "https://coderun.yandex.ru/problem/calculator",
		RetrievedAt: time.Now().UTC(),
	}
	task := &domain.Task{}
	if err := extractor.Process(context.Background(), raw, task); err != nil {
		t.Fatal(err)
	}
	if task.Title != "Калькулятор" || !strings.Contains(task.Description, "Вычислите значение выражения") {
		t.Fatalf("unexpected CodeRun extraction: title=%q description=%q", task.Title, task.Description)
	}
	if strings.Contains(task.Description, "Лишняя") {
		t.Fatalf("page chrome leaked into statement: %q", task.Description)
	}
}

func TestExtractor_JSON(t *testing.T) {
	extractor := NewExtractor()
	raw := loadRawTask(t, "extractor_api.json", domain.SourceCodeforces, domain.SourceTypeAPI)

	task := &domain.Task{}
	err := extractor.Process(context.Background(), raw, task)
	if err != nil {
		t.Fatalf("extractor: %v", err)
	}

	if task.Title != "Two Sum" {
		t.Errorf("expected title 'Two Sum', got %q", task.Title)
	}
	if task.Description == "" {
		t.Error("expected non-empty description")
	}
}

func TestExtractor_EmptyContent(t *testing.T) {
	extractor := NewExtractor()
	raw := domain.RawTask{
		Source:      domain.Source{ID: domain.SourceManual, Name: "Manual", Type: domain.SourceTypeManual},
		RawContent:  nil,
		SourceURL:   "https://manual.example.com",
		RetrievedAt: time.Now(),
	}

	task := &domain.Task{}
	err := extractor.Process(context.Background(), raw, task)
	if err != nil {
		t.Fatalf("extractor: %v", err)
	}
	if task.Title != "Manual" {
		t.Errorf("expected title 'Manual', got %q", task.Title)
	}
}

func TestExtractor_PlainText(t *testing.T) {
	extractor := NewExtractor()
	raw := domain.RawTask{
		Source:      domain.Source{ID: domain.SourceTelegramAlgorithms, Name: "Algo Channel", Type: domain.SourceTypeTelegram},
		RawContent:  []byte("Two Sum Problem\n\nGiven an array, find two numbers that add up to target."),
		SourceURL:   "https://t.me/algoses/123",
		RetrievedAt: time.Now(),
	}

	task := &domain.Task{}
	err := extractor.Process(context.Background(), raw, task)
	if err != nil {
		t.Fatalf("extractor: %v", err)
	}
	if !strings.Contains(task.Title, "Two Sum") {
		t.Errorf("expected title containing 'Two Sum', got %q", task.Title)
	}
	if task.Description == "" {
		t.Error("expected non-empty description")
	}
}

// --- Parser tests ---

func TestParser_FullText(t *testing.T) {
	parser := NewParser()
	task := &domain.Task{Description: readTestFile(t, "parser_full.txt")}

	err := parser.Process(context.Background(), testRawTask(), task)
	if err != nil {
		t.Fatalf("parser: %v", err)
	}

	if !strings.Contains(strings.ToLower(task.Title), "binary tree inorder") {
		t.Errorf("expected title containing 'Binary Tree Inorder', got %q", task.Title)
	}
	if len(task.Examples) == 0 {
		t.Error("expected at least one example")
	}
	if len(task.Constraints) == 0 {
		t.Error("expected at least one constraint")
	}
	if len(task.Tags) == 0 {
		t.Error("expected tags to be extracted")
	}
	if strings.Contains(task.Description, "Constraints:") {
		t.Error("description should not contain constraints section")
	}
}

func TestParser_OnlyDescription(t *testing.T) {
	parser := NewParser()
	task := &domain.Task{Description: readTestFile(t, "parser_only_description.txt")}

	err := parser.Process(context.Background(), testRawTask(), task)
	if err != nil {
		t.Fatalf("parser: %v", err)
	}
	if task.Description == "" {
		t.Error("expected non-empty description")
	}
}

func TestParser_EmptyText(t *testing.T) {
	parser := NewParser()
	task := &domain.Task{Description: ""}

	err := parser.Process(context.Background(), testRawTask(), task)
	if err != nil {
		t.Fatalf("expected no error for empty text, got: %v", err)
	}
}

func TestParser_SimpleText(t *testing.T) {
	parser := NewParser()
	task := &domain.Task{Description: readTestFile(t, "parser_simple.txt")}

	err := parser.Process(context.Background(), testRawTask(), task)
	if err != nil {
		t.Fatalf("parser: %v", err)
	}

	got := formatTaskSummary(task)
	goldenPath := filepath.Join("testdata", "parser_simple.golden")

	if *update {
		writeGolden(t, goldenPath, got)
	}

	expected := readGolden(t, goldenPath)
	if got != expected {
		t.Errorf("parser result mismatch.\n  got:\n%s\n---\n  want:\n%s", got, expected)
	}
}

// --- Normalizer tests ---

func TestNormalizer_Russian(t *testing.T) {
	normalizer := NewNormalizer()
	desc := readTestFile(t, "normalizer_russian.txt")

	task := &domain.Task{Description: desc}
	err := normalizer.Process(context.Background(), testRawTask(), task)
	if err != nil {
		t.Fatalf("normalizer: %v", err)
	}

	if strings.Contains(task.Description, "**Ввод:**") || strings.Contains(task.Description, "**Вывод:**") {
		t.Error("description should not contain Russian example headers after normalization")
	}
	if !strings.Contains(task.Description, "**Input:**") {
		t.Error("expected '**Input:**' after normalization")
	}
	if !strings.Contains(task.Description, "**Output:**") {
		t.Error("expected '**Output:**' after normalization")
	}
}

func TestNormalizer_RussianGolden(t *testing.T) {
	normalizer := NewNormalizer()
	desc := readTestFile(t, "normalizer_russian.txt")

	task := &domain.Task{Description: desc}
	err := normalizer.Process(context.Background(), testRawTask(), task)
	if err != nil {
		t.Fatalf("normalizer: %v", err)
	}

	got := task.Description
	goldenPath := filepath.Join("testdata", "normalizer_russian.golden")

	if *update {
		writeGolden(t, goldenPath, got)
	}

	expected := readGolden(t, goldenPath)
	if got != expected {
		t.Errorf("normalizer result mismatch.\n  got:\n%s\n---\n  want:\n%s", got, expected)
	}
}

func TestNormalizer_TagsSorting(t *testing.T) {
	normalizer := NewNormalizer()
	task := &domain.Task{
		Tags: []domain.Tag{"hash-table", "array", "two-pointers", "Array"},
	}

	err := normalizer.Process(context.Background(), testRawTask(), task)
	if err != nil {
		t.Fatalf("normalizer: %v", err)
	}

	for i := 1; i < len(task.Tags); i++ {
		prev := strings.ToLower(string(task.Tags[i-1]))
		curr := strings.ToLower(string(task.Tags[i]))
		if prev > curr {
			t.Errorf("tags not sorted: %s > %s at positions %d/%d", prev, curr, i-1, i)
		}
	}
}

func TestNormalizer_TagsDeduplication(t *testing.T) {
	normalizer := NewNormalizer()
	task := &domain.Task{
		Tags: []domain.Tag{"array", "array", "hash-table", "array"},
	}

	err := normalizer.Process(context.Background(), testRawTask(), task)
	if err != nil {
		t.Fatalf("normalizer: %v", err)
	}

	if len(task.Tags) != 2 {
		t.Errorf("expected 2 unique tags, got %d: %v", len(task.Tags), task.Tags)
	}
}

func TestNormalizer_EmptyExamplesRemoval(t *testing.T) {
	normalizer := NewNormalizer()
	task := &domain.Task{
		Examples: []domain.Example{
			{Input: "in1", Output: "out1"},
			{Input: "", Output: ""},
			{Input: "in3", Output: "out3"},
		},
	}

	err := normalizer.Process(context.Background(), testRawTask(), task)
	if err != nil {
		t.Fatalf("normalizer: %v", err)
	}

	if len(task.Examples) != 2 {
		t.Errorf("expected 2 examples after cleanup, got %d", len(task.Examples))
	}
}

// --- Validator tests ---

func TestValidator_Valid(t *testing.T) {
	validator := NewValidator()
	task := validTestTask()

	err := validator.Process(context.Background(), testRawTask(), task)
	if err != nil {
		t.Fatalf("expected valid task to pass, got: %v", err)
	}
}

func TestValidator_EmptyDescription(t *testing.T) {
	validator := NewValidator()
	task := validTestTask()
	task.Description = ""

	err := validator.Process(context.Background(), testRawTask(), task)
	if err == nil {
		t.Fatal("expected error for empty description, got nil")
	}
}

func TestValidator_EmptySourceURL(t *testing.T) {
	validator := NewValidator()
	task := validTestTask()
	task.SourceURL = ""

	err := validator.Process(context.Background(), testRawTask(), task)
	if err == nil {
		t.Fatal("expected error for empty source_url, got nil")
	}
}

func TestValidator_UnknownSourceID(t *testing.T) {
	validator := NewValidator()
	task := validTestTask()
	task.Source = domain.Source{ID: "unknown_source"}

	err := validator.Process(context.Background(), testRawTask(), task)
	if err == nil {
		t.Fatal("expected error for unknown source_id, got nil")
	}
}

func TestValidator_AutoHash(t *testing.T) {
	validator := NewValidator()
	task := validTestTask()
	task.SourceHash = ""

	err := validator.Process(context.Background(), testRawTask(), task)
	if err != nil {
		t.Fatalf("expected auto-hash to succeed, got: %v", err)
	}
	if task.SourceHash == "" {
		t.Error("expected source_hash to be generated")
	}
}

func TestValidator_TitleTooLong(t *testing.T) {
	validator := NewValidator()
	task := validTestTask()
	task.Title = string(make([]byte, MaxTitleLength+1))

	err := validator.Process(context.Background(), testRawTask(), task)
	if err == nil {
		t.Fatal("expected error for too long title, got nil")
	}
}

// --- helpers ---

func validTestTask() *domain.Task {
	return &domain.Task{
		Title:       "Two Sum",
		Description: "Given an array of integers nums and an integer target, return indices of the two numbers.",
		Source:      domain.Source{ID: domain.SourceLeetCode, Name: "LeetCode", Type: domain.SourceTypeWebsite},
		SourceURL:   "https://leetcode.com/problems/two-sum",
		SourceHash:  "test_hash",
		Type:        domain.TaskTypeAlgorithm,
		Difficulty:  domain.DifficultyEasy,
		Examples:    []domain.Example{{Input: "in", Output: "out"}},
		Tags:        []domain.Tag{"array"},
	}
}

// --- Integration test ---

func TestExtractorAndParserInPipeline(t *testing.T) {
	p := NewPipeline()
	p.AddProcessor("extractor", NewExtractor())
	p.AddProcessor("parser", NewParser())

	raw := loadRawTask(t, "extractor_codeforces.html", domain.SourceCodeforces, domain.SourceTypeWebsite)

	result, err := p.Run(context.Background(), raw)
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}

	if result.Task.Title == "" {
		t.Error("expected non-empty title after pipeline")
	}
	if result.Task.Description == "" {
		t.Error("expected non-empty description after pipeline")
	}
	if result.Task.SourceHash == "" {
		t.Error("expected source_hash after pipeline")
	}
}

// --- helpers ---

func testRawTask() domain.RawTask {
	return domain.RawTask{
		Source:      domain.Source{ID: domain.SourceLeetCode, Name: "LeetCode", Type: domain.SourceTypeWebsite},
		RawContent:  []byte("test content"),
		SourceURL:   "https://leetcode.com/problems/two-sum",
		RetrievedAt: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
	}
}

func loadRawTask(t *testing.T, filename string, sourceID domain.SourceID, sourceType domain.SourceType) domain.RawTask {
	t.Helper()
	content := readTestFile(t, filename)
	return domain.RawTask{
		Source: domain.Source{
			ID:   sourceID,
			Name: string(sourceID),
			Type: sourceType,
		},
		RawContent:  []byte(content),
		SourceURL:   "https://example.com/" + filename,
		RetrievedAt: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
	}
}

func readTestFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata/%s: %v", name, err)
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

func formatTaskSummary(task *domain.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\n", task.Title)
	fmt.Fprintf(&b, "Description: %s\n", truncate(task.Description, 100))
	fmt.Fprintf(&b, "Examples: %d\n", len(task.Examples))
	fmt.Fprintf(&b, "Constraints: %d\n", len(task.Constraints))
	fmt.Fprintf(&b, "Tags: %d\n", len(task.Tags))
	for _, ex := range task.Examples {
		fmt.Fprintf(&b, "  Input: %s\n", ex.Input)
		fmt.Fprintf(&b, "  Output: %s\n", ex.Output)
	}
	for _, c := range task.Constraints {
		fmt.Fprintf(&b, "  Constraint: %s\n", c)
	}
	for _, tag := range task.Tags {
		fmt.Fprintf(&b, "  Tag: %s\n", string(tag))
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
