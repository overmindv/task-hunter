//go:build component

package component

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/overmindv/task-hunter/internal/parser/domain"
	"github.com/overmindv/task-hunter/internal/parser/storage"
)

// TestSaveAndGetByID проверяет сохранение и получение задачи.
func TestSaveAndGetByID(t *testing.T) {
	ctx := context.Background()
	_, repo := setupDB(t)

	task := newTestTask()
	if err := repo.Save(ctx, task); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	assertEqual(t, "title", task.Title, got.Title)
	assertEqual(t, "type", domain.TaskTypeAlgorithm, got.Type)
	assertEqual(t, "difficulty", domain.DifficultyEasy, got.Difficulty)
	assertEqual(t, "hash", task.SourceHash, got.SourceHash)

	if len(got.Examples) != 1 {
		t.Fatalf("expected 1 example, got %d", len(got.Examples))
	}
	assertEqual(t, "example.input", "nums = [1,2], target = 3", got.Examples[0].Input)
	assertEqual(t, "example.explanation", "Simple case", got.Examples[0].Explanation)

	if len(got.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(got.Tags))
	}
}

// TestSave_TaskWithoutExamples тестирует задачу без примеров.
func TestSave_TaskWithoutExamples(t *testing.T) {
	ctx := context.Background()
	_, repo := setupDB(t)

	task := newTestTask()
	task.Examples = nil

	if err := repo.Save(ctx, task); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Examples) != 0 {
		t.Errorf("expected 0 examples, got %d", len(got.Examples))
	}
}

// TestSave_TaskWithoutTags тестирует задачу без тегов.
func TestSave_TaskWithoutTags(t *testing.T) {
	ctx := context.Background()
	_, repo := setupDB(t)

	task := newTestTask()
	task.Tags = nil

	if err := repo.Save(ctx, task); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Tags) != 0 {
		t.Errorf("expected 0 tags, got %d", len(got.Tags))
	}
}

// TestSave_MultipleExamples тестирует задачу с несколькими примерами.
func TestSave_MultipleExamples(t *testing.T) {
	ctx := context.Background()
	_, repo := setupDB(t)

	task := newTestTask()
	task.Examples = []domain.Example{
		{Input: "in1", Output: "out1"},
		{Input: "in2", Output: "out2", Explanation: "exp2"},
		{Input: "in3", Output: "out3"},
	}

	if err := repo.Save(ctx, task); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if len(got.Examples) != 3 {
		t.Fatalf("expected 3 examples, got %d", len(got.Examples))
	}

	// Проверяем, что хотя бы один пример имеет Explanation
	hasExplanation := false
	for _, ex := range got.Examples {
		if ex.Explanation == "exp2" {
			hasExplanation = true
			break
		}
	}
	if !hasExplanation {
		t.Error("expected at least one example with explanation 'exp2'")
	}
}

// TestFindBySourceHash проверяет поиск существующей и отсутствующей задачи.
func TestFindBySourceHash(t *testing.T) {
	ctx := context.Background()
	_, repo := setupDB(t)

	// Сохраняем задачу
	task := newTestTask()
	if err := repo.Save(ctx, task); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Поиск существующей
	got, err := repo.FindBySourceHash(ctx, task.SourceHash)
	if err != nil {
		t.Fatalf("find existing: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil task")
	}

	// Поиск отсутствующей
	got, err = repo.FindBySourceHash(ctx, "nonexistent_hash")
	if err != nil {
		t.Fatalf("find missing: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for missing hash")
	}
}

// TestList проверяет List с разными сценариями.
func TestList(t *testing.T) {
	ctx := context.Background()
	_, repo := setupDB(t)

	// Сохраняем задачи разных типов
	tasks := []domain.Task{
		newTestTaskWith("hash_alg", "Algo Task", domain.TaskTypeAlgorithm, domain.DifficultyEasy),
		newTestTaskWith("hash_db", "DB Task", domain.TaskTypeDatabase, domain.DifficultyMedium),
		newTestTaskWith("hash_back", "Backend Task", domain.TaskTypeBackend, domain.DifficultyHard),
		newTestTaskWith("hash_infra", "Infra Task", domain.TaskTypeInfrastructure, domain.DifficultyMedium),
	}
	for _, task := range tasks {
		if err := repo.Save(ctx, task); err != nil {
			t.Fatalf("save %q: %v", task.Title, err)
		}
	}

	t.Run("no filter returns all", func(t *testing.T) {
		all, err := repo.List(ctx, storage.Filter{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(all) != 4 {
			t.Errorf("expected 4, got %d", len(all))
		}
	})

	t.Run("filter by type", func(t *testing.T) {
		dbType := domain.TaskTypeDatabase
		filtered, err := repo.List(ctx, storage.Filter{Type: &dbType})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(filtered) != 1 || filtered[0].Title != "DB Task" {
			t.Errorf("expected 1 DB Task, got %d: %v", len(filtered), filtered)
		}
	})

	t.Run("filter by difficulty", func(t *testing.T) {
		hard := domain.DifficultyHard
		filtered, err := repo.List(ctx, storage.Filter{Difficulty: &hard})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(filtered) != 1 || filtered[0].Title != "Backend Task" {
			t.Errorf("expected 1 hard task, got %d", len(filtered))
		}
	})

	t.Run("filter by source_id", func(t *testing.T) {
		src := domain.SourceCodeforces
		filtered, err := repo.List(ctx, storage.Filter{SourceID: &src})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(filtered) != 0 {
			t.Errorf("expected 0, got %d", len(filtered))
		}
	})

	t.Run("pagination", func(t *testing.T) {
		page1, err := repo.List(ctx, storage.Filter{Limit: 2, Offset: 0})
		if err != nil {
			t.Fatalf("page1: %v", err)
		}
		if len(page1) != 2 {
			t.Errorf("expected 2 on page1, got %d", len(page1))
		}

		page2, err := repo.List(ctx, storage.Filter{Limit: 2, Offset: 2})
		if err != nil {
			t.Fatalf("page2: %v", err)
		}
		if len(page2) != 2 {
			t.Errorf("expected 2 on page2, got %d", len(page2))
		}

		page3, err := repo.List(ctx, storage.Filter{Limit: 2, Offset: 4})
		if err != nil {
			t.Fatalf("page3: %v", err)
		}
		if len(page3) != 0 {
			t.Errorf("expected 0 on page3, got %d", len(page3))
		}
	})
}

// TestCount проверяет подсчёт задач.
func TestCount(t *testing.T) {
	ctx := context.Background()
	_, repo := setupDB(t)

	saveTestTasks(ctx, t, repo, 3)

	count, err := repo.Count(ctx, storage.Filter{})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3, got %d", count)
	}

	// Count с фильтром
	dbType := domain.TaskTypeDatabase
	// У нас все задачи типа Algorithm, так что Database даст 0
	countDB, err := repo.Count(ctx, storage.Filter{Type: &dbType})
	if err != nil {
		t.Fatalf("count db: %v", err)
	}
	if countDB != 0 {
		t.Errorf("expected 0 db tasks, got %d", countDB)
	}

	// Count с фильтром Algorithm
	algType := domain.TaskTypeAlgorithm
	countAlg, err := repo.Count(ctx, storage.Filter{Type: &algType})
	if err != nil {
		t.Fatalf("count alg: %v", err)
	}
	if countAlg != 3 {
		t.Errorf("expected 3 alg tasks, got %d", countAlg)
	}
}

// TestGetByID_NotFound проверяет ошибку для несуществующей задачи.
func TestGetByID_NotFound(t *testing.T) {
	ctx := context.Background()
	_, repo := setupDB(t)

	_, err := repo.GetByID(ctx, uuid.New().String())
	if err == nil {
		t.Fatal("expected error for non-existent task")
	}

	var notFound *storage.TaskNotFoundError
	if err != nil {
		notFound = err.(*storage.TaskNotFoundError) //nolint:errorlint
	}
	if notFound == nil {
		t.Errorf("expected *TaskNotFoundError, got %T: %v", err, err)
	}
}

// --- Task 2.2: SaveIfNotDuplicate ---

// TestSaveIfNotDuplicate_New проверяет сохранение новой задачи.
func TestSaveIfNotDuplicate_New(t *testing.T) {
	ctx := context.Background()
	_, repo := setupDB(t)

	task := newTestTask()
	added, err := storage.SaveIfNotDuplicate(ctx, task, repo)
	if err != nil {
		t.Fatalf("save new: %v", err)
	}
	if !added {
		t.Error("expected true for new task")
	}
}

// TestSaveIfNotDuplicate_Duplicate проверяет пропуск дубликата.
func TestSaveIfNotDuplicate_Duplicate(t *testing.T) {
	ctx := context.Background()
	_, repo := setupDB(t)

	task := newTestTask()
	first, err := storage.SaveIfNotDuplicate(ctx, task, repo)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if !first {
		t.Fatal("expected first to succeed")
	}

	second, err := storage.SaveIfNotDuplicate(ctx, task, repo)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second {
		t.Error("expected second to be skipped")
	}
}

// TestSaveIfNotDuplicate_SameContentDifferentSource проверяет,
// что одинаковый контент из разных источников — не дубликат.
func TestSaveIfNotDuplicate_SameContentDifferentSource(t *testing.T) {
	ctx := context.Background()
	_, repo := setupDB(t)

	// Задача из LeetCode
	taskLC := newTestTask()
	taskLC.Source = domain.Source{ID: domain.SourceLeetCode, Name: "LeetCode", Type: domain.SourceTypeWebsite}

	// Та же задача из Codeforces — другой source, поэтому другой хеш
	taskCF := newTestTask()
	taskCF.ID = uuid.New().String()
	taskCF.Source = domain.Source{ID: domain.SourceCodeforces, Name: "Codeforces", Type: domain.SourceTypeWebsite}
	taskCF.SourceURL = "https://codeforces.com/problemset/problem/1/A"
	// Явно не задаём SourceHash — он сгенерируется автоматически

	lcAdded, err := storage.SaveIfNotDuplicate(ctx, taskLC, repo)
	if err != nil {
		t.Fatalf("save lc: %v", err)
	}
	if !lcAdded {
		t.Fatal("expected leetcode task to be new")
	}

	cfAdded, err := storage.SaveIfNotDuplicate(ctx, taskCF, repo)
	if err != nil {
		t.Fatalf("save cf: %v", err)
	}
	if !cfAdded {
		t.Error("expected codeforces task with same content to be NOT a duplicate (different source)")
	}

	// Проверяем, что обе задачи в БД
	count, err := repo.Count(ctx, storage.Filter{})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 tasks, got %d", count)
	}
}

// TestSaveIfNotDuplicate_NoHash проверяет, что хеш генерируется автоматически.
func TestSaveIfNotDuplicate_NoHash(t *testing.T) {
	ctx := context.Background()
	_, repo := setupDB(t)

	task := newTestTask()
	task.SourceHash = "" // хеш не задан

	added, err := storage.SaveIfNotDuplicate(ctx, task, repo)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !added {
		t.Error("expected task added")
	}

	// Проверяем, что хеш был сгенерирован
	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.SourceHash == "" {
		t.Error("expected source_hash to be auto-generated")
	}
}

// TestSaveIfNotDuplicate_Concurrent проверяет конкурентную запись одинаковых задач.
func TestSaveIfNotDuplicate_Concurrent(t *testing.T) {
	ctx := context.Background()
	_, repo := setupDB(t)

	task := newTestTask()

	var wg sync.WaitGroup
	results := make(chan bool, 5)

	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			added, err := storage.SaveIfNotDuplicate(ctx, task, repo)
			if err == nil {
				results <- added
			}
		}()
	}

	wg.Wait()
	close(results)

	var addedCount int
	for r := range results {
		if r {
			addedCount++
		}
	}

	// Должна сохраниться ровно одна задача
	if addedCount != 1 {
		t.Errorf("expected exactly 1 save, got %d", addedCount)
	}

	count, err := repo.Count(ctx, storage.Filter{})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 task in db, got %d", count)
	}
}

// --- helpers ---

func newTestTask() domain.Task {
	return domain.Task{
		ID:          uuid.New().String(),
		Title:       "Two Sum",
		Description: "Given an array of integers nums and an integer target, return indices of the two numbers.",
		Examples: []domain.Example{
			{
				Input:       "nums = [1,2], target = 3",
				Output:      "[0, 1]",
				Explanation: "Simple case",
			},
		},
		Source: domain.Source{
			ID:   domain.SourceLeetCode,
			Name: "LeetCode",
			Type: domain.SourceTypeWebsite,
		},
		SourceURL:  "https://leetcode.com/problems/two-sum",
		SourceHash: "test_hash_" + uuid.New().String(),
		Type:       domain.TaskTypeAlgorithm,
		Difficulty: domain.DifficultyEasy,
		Tags:       []domain.Tag{"array", "hash-table"},
		CreatedAt:  time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
	}
}

func newTestTaskWith(hash, title string, tt domain.TaskType, d domain.Difficulty) domain.Task {
	task := newTestTask()
	task.ID = uuid.New().String()
	task.SourceHash = hash
	task.Title = title
	task.Type = tt
	task.Difficulty = d
	return task
}

func saveTestTasks(ctx context.Context, t *testing.T, repo *storage.PostgresRepository, n int) {
	t.Helper()
	for i := range n {
		task := newTestTask()
		task.ID = uuid.New().String()
		task.SourceHash = "test_hash_" + uuid.New().String()
		task.Title = fmt.Sprintf("Task %d", i+1)
		if err := repo.Save(ctx, task); err != nil {
			t.Fatalf("save task %d: %v", i, err)
		}
	}
}

func assertEqual[T comparable](t *testing.T, name string, want, got T) {
	t.Helper()
	if want != got {
		t.Errorf("%s: want %v, got %v", name, want, got)
	}
}
