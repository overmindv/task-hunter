package classifier

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"diploma/internal/parser/domain"
)

var update = flag.Bool("update", false, "update golden files")

// --- Golden file tests ---

type classifierTestCase struct {
	name      string
	title     string
	desc      string
	tags      []domain.Tag
	checkOnly string // if non-empty, checks that expected type string is contained
}

func TestClassify_Golden(t *testing.T) {
	c := NewRuleBasedClassifier()
	ctx := context.Background()

	tests := []classifierTestCase{
		{
			name:      "sql_task",
			title:     readTitle(t, "sql_task.txt"),
			desc:      readBody(t, "sql_task.txt"),
			checkOnly: "database",
		},
		{
			name:      "sorting_task",
			title:     readTitle(t, "sorting_task.txt"),
			desc:      readBody(t, "sorting_task.txt"),
			checkOnly: "algorithm",
		},
		{
			name:      "docker_task",
			title:     readTitle(t, "docker_task.txt"),
			desc:      readBody(t, "docker_task.txt"),
			checkOnly: "infrastructure",
		},
		{
			name:      "plain_task",
			title:     readTitle(t, "plain_task.txt"),
			desc:      readBody(t, "plain_task.txt"),
			checkOnly: "algorithm",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			task := &domain.Task{
				Title:       tc.title,
				Description: tc.desc,
				Tags:        tc.tags,
			}

			if err := c.Classify(ctx, task); err != nil {
				t.Fatalf("Classify: %v", err)
			}

			got := formatClassifyResult(task)
			goldenPath := filepath.Join("testdata", tc.name+".golden")

			if *update {
				writeGolden(t, goldenPath, got)
			}

			expected := readGolden(t, goldenPath)
			if got != expected {
				t.Errorf("classification mismatch for %q.\n  got:  %s\n  want: %s", tc.name, got, expected)
			}
		})
	}
}

// --- Unit tests ---

// TestClassify_MixedKeywords проверяет выбор типа при совпадении с несколькими.
func TestClassify_MixedKeywords(t *testing.T) {
	c := NewRuleBasedClassifier()
	ctx := context.Background()

	// Задача с SQL (Database) и REST API (Backend) — выбирается тот, у кого больше совпадений
	task := &domain.Task{
		Title: "REST API для базы данных",
		Description: "Спроектируйте REST API для работы с таблицами. " +
			"Используйте HTTP методы: GET, POST, PUT, DELETE. " +
			"Добавьте JOIN запросы и индексы для ускорения. " +
			"Настройте авторизацию через JWT токены.",
	}

	if err := c.Classify(ctx, task); err != nil {
		t.Fatalf("Classify: %v", err)
	}

	// В тексте много ключевых слов и Database (sql, таблиц, join, индекс, запрос)
	// и Backend (api, rest, http, jwt, авторизация, запрос).
	// Должен выбрать тот, у кого больше совпадений.
	t.Logf("mixed task classified as: %s", task.Type.String())

	if task.Type != domain.TaskTypeDatabase && task.Type != domain.TaskTypeBackend {
		t.Errorf("expected Database or Backend for mixed task, got %s", task.Type.String())
	}
}

// TestClassify_EmptyDescription проверяет пустое описание.
func TestClassify_EmptyDescription(t *testing.T) {
	c := NewRuleBasedClassifier()
	ctx := context.Background()

	task := &domain.Task{
		Title:       "",
		Description: "",
	}

	err := c.Classify(ctx, task)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if task.Type != domain.TaskTypeAlgorithm {
		t.Errorf("expected TaskTypeAlgorithm for empty task, got %s", task.Type.String())
	}
}

// TestClassify_NilTask проверяет, что передача nil не вызывает паники.
func TestClassify_NilTask(t *testing.T) {
	c := NewRuleBasedClassifier()
	ctx := context.Background()

	err := c.Classify(ctx, nil)
	if err != nil {
		t.Fatalf("Classify with nil: %v", err)
	}
}

// TestClassify_WithTags проверяет классификацию с учётом тегов.
func TestClassify_WithTags(t *testing.T) {
	c := NewRuleBasedClassifier()
	ctx := context.Background()

	// Задача без явного текста, но с тегами
	task := &domain.Task{
		Title:       "Implement a stack",
		Description: "Implement...",
		Tags:        []domain.Tag{"stack", "queue", "heap"},
	}

	if err := c.Classify(ctx, task); err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if task.Type != domain.TaskTypeDataStructures {
		t.Errorf("expected TaskTypeDataStructures for stack/queue/heap tags, got %s", task.Type.String())
	}
}

// TestClassify_PipelineProcessor проверяет, что RuleBasedClassifier работает как pipeline.Processor.
func TestClassify_PipelineProcessor(t *testing.T) {
	c := NewRuleBasedClassifier()
	ctx := context.Background()

	task := &domain.Task{
		Title:       "Docker container",
		Description: "Настройте Docker контейнер для приложения.",
	}

	var raw domain.RawTask
	err := c.Process(ctx, raw, task)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if task.Type != domain.TaskTypeInfrastructure {
		t.Errorf("expected TaskTypeInfrastructure, got %s", task.Type.String())
	}
}

// --- helpers ---

func readTitle(t *testing.T, name string) string {
	t.Helper()
	data := readFixture(t, name)
	parts := strings.SplitN(string(data), "\n", 2)
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

func readBody(t *testing.T, name string) string {
	t.Helper()
	data := readFixture(t, name)
	parts := strings.SplitN(string(data), "\n", 2)
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
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
