package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"diploma/internal/parser/domain"
)

// testDB возвращает подключение к тестовой БД.
func testDB(t *testing.T) *PostgresRepository {
	t.Helper()

	// Применяем миграции перед каждым тестом
	dbURL := "postgres://postgres:postgres@localhost:5433/diploma_test?sslmode=disable"
	migDir := "../../../migrations"

	// Откатываем и накатываем для чистого состояния
	_ = RunMigrationsDown(dbURL, migDir)
	if err := RunMigrations(dbURL, migDir); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}

	db := MustOpenDB(dbURL)
	t.Cleanup(func() { db.Close() })

	return NewPostgresRepository(db)
}

// TestSaveAndGetByID проверяет сохранение задачи и её получение по ID.
func TestSaveAndGetByID(t *testing.T) {
	ctx := context.Background()
	repo := testDB(t)

	task := newTestTask()
	if err := repo.Save(ctx, task); err != nil {
		t.Fatalf("save task: %v", err)
	}

	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil task")
	}

	// Проверяем поля
	if got.Title != task.Title {
		t.Errorf("expected title %q, got %q", task.Title, got.Title)
	}
	if got.Description != task.Description {
		t.Errorf("expected description %q, got %q", task.Description, got.Description)
	}
	if got.SourceURL != task.SourceURL {
		t.Errorf("expected url %q, got %q", task.SourceURL, got.SourceURL)
	}
	if got.Type != domain.TaskTypeAlgorithm {
		t.Errorf("expected type %v, got %v", domain.TaskTypeAlgorithm, got.Type)
	}
	if got.Difficulty != domain.DifficultyEasy {
		t.Errorf("expected difficulty %v, got %v", domain.DifficultyEasy, got.Difficulty)
	}
	if got.SourceHash != task.SourceHash {
		t.Errorf("expected hash %q, got %q", task.SourceHash, got.SourceHash)
	}

	// Проверяем примеры
	if len(got.Examples) != 1 {
		t.Fatalf("expected 1 example, got %d", len(got.Examples))
	}
	if got.Examples[0].Input != "nums = [1,2], target = 3" {
		t.Errorf("unexpected example input: %q", got.Examples[0].Input)
	}
	if got.Examples[0].Explanation != "Simple case" {
		t.Errorf("unexpected explanation: %q", got.Examples[0].Explanation)
	}

	// Проверяем теги
	if len(got.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(got.Tags))
	}
	if got.Tags[0] != "array" || got.Tags[1] != "hash-table" {
		t.Errorf("unexpected tags: %v", got.Tags)
	}
}

// TestFindBySourceHash_Found проверяет поиск существующей задачи по хешу.
func TestFindBySourceHash_Found(t *testing.T) {
	ctx := context.Background()
	repo := testDB(t)

	task := newTestTask()
	if err := repo.Save(ctx, task); err != nil {
		t.Fatalf("save task: %v", err)
	}

	got, err := repo.FindBySourceHash(ctx, task.SourceHash)
	if err != nil {
		t.Fatalf("find by source_hash: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil task")
	}
	if got.ID != task.ID {
		t.Errorf("expected ID %q, got %q", task.ID, got.ID)
	}
}

// TestFindBySourceHash_NotFound проверяет поиск отсутствующей задачи.
func TestFindBySourceHash_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := testDB(t)

	got, err := repo.FindBySourceHash(ctx, "nonexistent_hash")
	if err != nil {
		t.Fatalf("find by source_hash: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent hash")
	}
}

// TestList_NoFilter проверяет List без фильтрации.
func TestList_NoFilter(t *testing.T) {
	ctx := context.Background()
	repo := testDB(t)

	saveTasks(ctx, t, repo, 3)

	tasks, err := repo.List(ctx, Filter{})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(tasks))
	}
}

// TestList_WithTypeFilter проверяет фильтрацию по типу задачи.
func TestList_WithTypeFilter(t *testing.T) {
	ctx := context.Background()
	repo := testDB(t)

	// Сохраняем задачи разных типов
	t1 := newTestTask()
	t1.ID = uuid.New().String()
	t1.SourceHash = "hash_backend"
	t1.Type = domain.TaskTypeBackend
	t1.Title = "Backend Task"

	t2 := newTestTask()
	t2.ID = uuid.New().String()
	t2.SourceHash = "hash_db"
	t2.Type = domain.TaskTypeDatabase
	t2.Title = "DB Task"

	t3 := newTestTask()
	t3.ID = uuid.New().String()
	t3.SourceHash = "hash_alg"
	t3.Title = "Algo Task"

	for _, task := range []domain.Task{t1, t2, t3} {
		if err := repo.Save(ctx, task); err != nil {
			t.Fatalf("save task %q: %v", task.Title, err)
		}
	}

	// Фильтр по TaskTypeDatabase
	dbType := domain.TaskTypeDatabase
	tasks, err := repo.List(ctx, Filter{Type: &dbType})
	if err != nil {
		t.Fatalf("list with filter: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task with type database, got %d", len(tasks))
	}
	if tasks[0].Title != "DB Task" {
		t.Errorf("expected 'DB Task', got '%s'", tasks[0].Title)
	}
}

// TestList_Pagination проверяет пагинацию.
func TestList_Pagination(t *testing.T) {
	ctx := context.Background()
	repo := testDB(t)

	saveTasks(ctx, t, repo, 5)

	// Страница 1: 2 задачи
	tasks, err := repo.List(ctx, Filter{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks on page 1, got %d", len(tasks))
	}

	// Страница 2: 2 задачи
	tasks, err = repo.List(ctx, Filter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("list page 2: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks on page 2, got %d", len(tasks))
	}

	// Страница 3: 1 задача
	tasks, err = repo.List(ctx, Filter{Limit: 2, Offset: 4})
	if err != nil {
		t.Fatalf("list page 3: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 task on page 3, got %d", len(tasks))
	}
}

// TestCount проверяет подсчёт количества задач.
func TestCount(t *testing.T) {
	ctx := context.Background()
	repo := testDB(t)

	saveTasks(ctx, t, repo, 3)

	count, err := repo.Count(ctx, Filter{})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3, got %d", count)
	}
}

// TestSaveIfNotDuplicate_New проверяет сохранение новой задачи.
func TestSaveIfNotDuplicate_New(t *testing.T) {
	ctx := context.Background()
	repo := testDB(t)

	task := newTestTask()
	added, err := SaveIfNotDuplicate(ctx, task, repo)
	if err != nil {
		t.Fatalf("save if not duplicate: %v", err)
	}
	if !added {
		t.Error("expected true (new task), got false")
	}
}

// TestSaveIfNotDuplicate_Duplicate проверяет пропуск дубликата.
func TestSaveIfNotDuplicate_Duplicate(t *testing.T) {
	ctx := context.Background()
	repo := testDB(t)

	task := newTestTask()
	first, err := SaveIfNotDuplicate(ctx, task, repo)
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	if !first {
		t.Fatal("expected first save to succeed")
	}

	second, err := SaveIfNotDuplicate(ctx, task, repo)
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if second {
		t.Error("expected second save to be skipped (duplicate)")
	}
}

// TestGetByID_NotFound проверяет получение несуществующей задачи.
func TestGetByID_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := testDB(t)

	_, err := repo.GetByID(ctx, uuid.New().String())
	if err == nil {
		t.Fatal("expected error for non-existent task")
	}

	var notFound *TaskNotFoundError
	if asNotFound(err) {
		notFound = err.(*TaskNotFoundError) //nolint:errorlint
	}
	if notFound == nil {
		t.Errorf("expected *TaskNotFoundError, got %T: %v", err, err)
	}
}

// TestDefaultLimit проверяет лимит пагинации по умолчанию.
func TestDefaultLimit(t *testing.T) {
	f := EnsureFilter(Filter{Limit: 0})
	if f.Limit != defaultLimit {
		t.Errorf("expected default limit %d, got %d", defaultLimit, f.Limit)
	}
}

// TestMaxLimit проверяет максимальный лимит пагинации.
func TestMaxLimit(t *testing.T) {
	f := EnsureFilter(Filter{Limit: 999})
	if f.Limit != defaultMaxLimit {
		t.Errorf("expected max limit %d, got %d", defaultMaxLimit, f.Limit)
	}
}

// --- helpers ---

// newTestTask создаёт задачу с фиксированными данными для тестов.
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
	}
}

// saveTasks сохраняет N тестовых задач.
func saveTasks(ctx context.Context, t *testing.T, repo *PostgresRepository, n int) {
	t.Helper()

	for i := range n {
		task := newTestTask()
		task.ID = uuid.New().String()
		task.SourceHash = fmt.Sprintf("test_hash_%d_%s", i, uuid.New().String())
		task.Title = fmt.Sprintf("Task %d", i+1)

		if err := repo.Save(ctx, task); err != nil {
			t.Fatalf("save task %d: %v", i, err)
		}
	}
}

func asNotFound(err error) bool {
	_, ok := err.(*TaskNotFoundError) //nolint:errorlint
	return ok
}

// Override TimeNow для детерминированных тестов.
func init() {
	TimeNow = func() time.Time {
		return time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	}
}
