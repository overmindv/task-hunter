package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestTask_Validate_Valid проверяет, что корректная задача проходит валидацию.
func TestTask_Validate_Valid(t *testing.T) {
	task := validTask()
	if err := task.Validate(); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

// TestTask_Validate_EmptyTitle проверяет ошибку при пустом заголовке.
func TestTask_Validate_EmptyTitle(t *testing.T) {
	task := validTask()
	task.Title = ""
	if err := task.Validate(); err == nil {
		t.Error("expected error for empty title, got nil")
	}
}

// TestTask_Validate_EmptyDescription проверяет ошибку при пустом описании.
func TestTask_Validate_EmptyDescription(t *testing.T) {
	task := validTask()
	task.Description = ""
	if err := task.Validate(); err == nil {
		t.Error("expected error for empty description, got nil")
	}
}

// TestTask_Validate_EmptySourceURL проверяет ошибку при отсутствии ссылки.
func TestTask_Validate_EmptySourceURL(t *testing.T) {
	task := validTask()
	task.SourceURL = ""
	if err := task.Validate(); err == nil {
		t.Error("expected error for empty source_url, got nil")
	}
}

// TestTask_Validate_EmptySourceHash проверяет ошибку при отсутствии хеша.
func TestTask_Validate_EmptySourceHash(t *testing.T) {
	task := validTask()
	task.SourceHash = ""
	if err := task.Validate(); err == nil {
		t.Error("expected error for empty source_hash, got nil")
	}
}

// TestTask_Validate_EmptySourceID проверяет ошибку при отсутствии идентификатора источника.
func TestTask_Validate_EmptySourceID(t *testing.T) {
	task := validTask()
	task.Source.ID = ""
	if err := task.Validate(); err == nil {
		t.Error("expected error for empty source_id, got nil")
	}
}

// TestGenerateSourceHash_Consistent проверяет, что одинаковые данные дают одинаковый хеш.
func TestGenerateSourceHash_Consistent(t *testing.T) {
	h1 := GenerateSourceHash(SourceLeetCode, "https://leetcode.com/problems/two-sum", []byte("Two Sum Problem"))
	h2 := GenerateSourceHash(SourceLeetCode, "https://leetcode.com/problems/two-sum", []byte("Two Sum Problem"))

	if h1 != h2 {
		t.Errorf("expected identical hashes for identical input, got %s vs %s", h1, h2)
	}
}

// TestGenerateSourceHash_DifferentSources проверяет, что одинаковый контент
// из разных источников даёт разные хеши.
func TestGenerateSourceHash_DifferentSources(t *testing.T) {
	h1 := GenerateSourceHash(SourceLeetCode, "https://leetcode.com/problems/two-sum", []byte("Two Sum Problem"))
	h2 := GenerateSourceHash(SourceCodeforces, "https://codeforces.com/problemset/problem/1/A", []byte("Two Sum Problem"))

	if h1 == h2 {
		t.Error("expected different hashes for different sources with same content")
	}
}

// TestGenerateSourceHash_DifferentContent проверяет, что разный контент даёт разные хеши.
func TestGenerateSourceHash_DifferentContent(t *testing.T) {
	h1 := GenerateSourceHash(SourceLeetCode, "https://leetcode.com/problems/two-sum", []byte("Two Sum Problem"))
	h2 := GenerateSourceHash(SourceLeetCode, "https://leetcode.com/problems/two-sum", []byte("Three Sum Problem"))

	if h1 == h2 {
		t.Error("expected different hashes for different content")
	}
}

// TestTaskType_String проверяет строковое представление типов задач.
func TestTaskType_String(t *testing.T) {
	tests := []struct {
		tt       TaskType
		expected string
	}{
		{TaskTypeAlgorithm, "algorithm"},
		{TaskTypeDataStructures, "data_structures"},
		{TaskTypeDatabase, "database"},
		{TaskTypeBackend, "backend"},
		{TaskTypeInfrastructure, "infrastructure"},
		{TaskTypeTesting, "testing"},
		{TaskTypeCodeReview, "code_review"},
		{TaskType(100), "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			if got := tc.tt.String(); got != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, got)
			}
		})
	}
}

// TestDifficulty_String проверяет строковое представление сложности.
func TestDifficulty_String(t *testing.T) {
	tests := []struct {
		d        Difficulty
		expected string
	}{
		{DifficultyUnknown, "unknown"},
		{DifficultyEasy, "easy"},
		{DifficultyMedium, "medium"},
		{DifficultyHard, "hard"},
		{Difficulty(100), "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			if got := tc.d.String(); got != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, got)
			}
		})
	}
}

// TestSourceID_Constants проверяет, что все константы SourceID уникальны.
func TestSourceID_Constants(t *testing.T) {
	ids := []SourceID{
		SourceTelegramAnalytics,
		SourceTelegramML,
		SourceTelegramAlgorithms,
		SourceLeetCode,
		SourceCodeforces,
		SourceCodeRun,
		SourceManual,
	}

	seen := make(map[SourceID]bool)
	for _, id := range ids {
		if seen[id] {
			t.Errorf("duplicate SourceID: %s", id)
		}
		seen[id] = true
	}

	// Проверяем, что константы покрывают все источники
	expectedCount := 7
	if len(ids) != expectedCount {
		t.Errorf("expected %d SourceID constants, got %d", expectedCount, len(ids))
	}
}

// TestTaskType_Constants проверяет, что все константы TaskType не пересекаются.
func TestTaskType_Constants(t *testing.T) {
	types := []TaskType{
		TaskTypeAlgorithm,
		TaskTypeDataStructures,
		TaskTypeDatabase,
		TaskTypeBackend,
		TaskTypeInfrastructure,
		TaskTypeTesting,
		TaskTypeCodeReview,
	}

	seen := make(map[TaskType]bool)
	for _, tt := range types {
		if seen[tt] {
			t.Errorf("duplicate TaskType: %d", tt)
		}
		seen[tt] = true
	}
}

// TestGenerateSourceHashFromTask проверяет корректность генерации хеша из задачи.
func TestGenerateSourceHashFromTask(t *testing.T) {
	task := validTask()
	hash := GenerateSourceHashFromTask(task)

	if hash == "" {
		t.Error("expected non-empty hash")
	}

	// Повторный вызов должен дать тот же хеш
	hash2 := GenerateSourceHashFromTask(task)
	if hash != hash2 {
		t.Errorf("expected same hash on repeated call, got %s vs %s", hash, hash2)
	}
}

// validTask создаёт корректную задачу для тестов.
func validTask() *Task {
	return &Task{
		ID:          uuid.New().String(),
		Title:       "Two Sum",
		Description: "Given an array of integers nums and an integer target, return indices of the two numbers such that they add up to target.",
		Examples: []Example{
			{
				Input:       "nums = [2,7,11,15], target = 9",
				Output:      "[0, 1]",
				Explanation: "Because nums[0] + nums[1] == 9, we return [0, 1].",
			},
		},
		Constraints: []string{"2 <= nums.length <= 10^4", "-10^9 <= nums[i] <= 10^9"},
		Source: Source{
			ID:   SourceLeetCode,
			Name: "LeetCode",
			Type: SourceTypeWebsite,
		},
		SourceURL:  "https://leetcode.com/problems/two-sum",
		SourceHash: "abc123",
		Type:       TaskTypeAlgorithm,
		Difficulty: DifficultyEasy,
		Tags:       []Tag{"array", "hash-table"},
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}
