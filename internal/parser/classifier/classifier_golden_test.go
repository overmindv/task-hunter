package classifier

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/overmindv/task-hunter/internal/parser/domain"
)

// GoldenTestResult — результат классификации для golden-файлов.
type GoldenTestResult struct {
	Type       string   `json:"type"`
	Difficulty string   `json:"difficulty"`
	Tags       []string `json:"tags"`
}

// runClassifierTest читает входной файл, классифицирует задачу и сверяет с golden.
func runClassifierTest(t *testing.T, classifier *RuleBasedClassifier, inputPath, goldenPath string) {
	t.Helper()

	// Читаем входной файл
	data, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatalf("read input %s: %v", inputPath, err)
	}

	// Первая строка — заголовок, остальное — описание
	parts := strings.SplitN(string(data), "\n", 2)
	title := strings.TrimSpace(parts[0])
	desc := ""
	if len(parts) > 1 {
		desc = strings.TrimSpace(parts[1])
	}

	task := &domain.Task{
		Title:       title,
		Description: desc,
	}

	ctx := context.Background()
	if err := classifier.Classify(ctx, task); err != nil {
		t.Fatalf("Classify: %v", err)
	}

	result := GoldenTestResult{
		Type:       task.Type.String(),
		Difficulty: task.Difficulty.String(),
		Tags:       tagStrings(task.Tags),
	}

	got := mustJSON(t, result)

	if *update {
		if err := os.WriteFile(goldenPath, []byte(got+"\n"), 0644); err != nil {
			t.Fatalf("write golden %s: %v", goldenPath, err)
		}
	}

	expectedData, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	expected := strings.TrimSpace(string(expectedData))

	if got != expected {
		t.Errorf("classification mismatch for %s.\n  got:      %s\n  expected: %s", inputPath, got, expected)
	}
}

// TestClassify_GoldenSubdirs проверяет классификацию на golden-файлах в поддиректориях.
func TestClassify_GoldenSubdirs(t *testing.T) {
	c := NewRuleBasedClassifier()

	// Директории с тестовыми данными по типам задач
	dirs := []string{
		"algorithms",
		"databases",
		"backend",
		"infrastructure",
		"testing",
		"codereview",
		"datastructures",
	}

	baseDir := "testdata"

	for _, dir := range dirs {
		dirPath := filepath.Join(baseDir, dir)

		// Проверяем, что директория существует
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			t.Fatalf("read dir %s: %v", dirPath, err)
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
				continue
			}

			name := strings.TrimSuffix(entry.Name(), ".txt")
			inputPath := filepath.Join(dirPath, entry.Name())
			goldenPath := filepath.Join(dirPath, name+".golden")

			t.Run(dir+"/"+name, func(t *testing.T) {
				runClassifierTest(t, c, inputPath, goldenPath)

				if entry.Name() == "graph_dfs.txt" {
					// Дополнительная проверка для демонстрации
				}
			})
		}
	}
}

// TestClassify_UpdateFlag проверяет, что -update перезаписывает golden-файлы.
func TestClassify_UpdateFlag(t *testing.T) {
	if !*update {
		t.Skip("skip: update flag is not set")
	}

	// Проверяем, что все golden-файлы существуют после обновления
	baseDir := "testdata"

	dirs := []string{
		"algorithms",
		"databases",
		"backend",
		"infrastructure",
		"testing",
		"codereview",
		"datastructures",
	}

	for _, dir := range dirs {
		dirPath := filepath.Join(baseDir, dir)
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			t.Fatalf("read dir %s: %v", dirPath, err)
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
				continue
			}

			name := strings.TrimSuffix(entry.Name(), ".txt")
			goldenPath := filepath.Join(dirPath, name+".golden")

			if _, err := os.Stat(goldenPath); os.IsNotExist(err) {
				t.Errorf("golden file not found after update: %s", goldenPath)
			}
		}
	}
}

// --- helpers ---

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(data)
}

func tagStrings(tags []domain.Tag) []string {
	if len(tags) == 0 {
		return nil
	}
	result := make([]string, len(tags))
	for i, tag := range tags {
		result[i] = string(tag)
	}
	sort.Strings(result)
	return result
}

// formatClassifyResult форматирует результат для существующих golden-тестов (не JSON).
func formatClassifyResult(task *domain.Task) string {
	return fmt.Sprintf("type: %s | difficulty: %s", task.Type.String(), task.Difficulty.String())
}
