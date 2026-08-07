package storage

import (
	"context"
	"errors"
	"testing"

	"diploma/internal/parser/domain"
)

// mockRepo — минимальная реализация Repository для unit-тестов.
type mockRepo struct {
	tasks map[string]*domain.Task
}

func (m *mockRepo) Save(_ context.Context, task domain.Task) error {
	m.tasks[task.SourceHash] = &task
	return nil
}

func (m *mockRepo) FindBySourceHash(_ context.Context, hash string) (*domain.Task, error) {
	t, ok := m.tasks[hash]
	if !ok {
		return nil, nil
	}
	return t, nil
}

func (m *mockRepo) List(_ context.Context, filter Filter) ([]domain.Task, error) {
	return nil, errors.New("not implemented")
}

func (m *mockRepo) GetByID(_ context.Context, id string) (*domain.Task, error) {
	return nil, errors.New("not implemented")
}

func (m *mockRepo) Count(_ context.Context, filter Filter) (int, error) {
	return 0, errors.New("not implemented")
}

// TestSaveIfNotDuplicate_Unit_New проверяет сохранение новой задачи через mock.
func TestSaveIfNotDuplicate_Unit_New(t *testing.T) {
	ctx := context.Background()
	repo := &mockRepo{tasks: make(map[string]*domain.Task)}

	task := domain.Task{
		Title:      "Test",
		SourceHash: domain.GenerateSourceHash("test_source", "http://example.com", []byte("test")),
	}

	added, err := SaveIfNotDuplicate(ctx, task, repo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !added {
		t.Error("expected true, got false")
	}
}

// TestSaveIfNotDuplicate_Unit_Dup проверяет пропуск дубликата через mock.
func TestSaveIfNotDuplicate_Unit_Dup(t *testing.T) {
	ctx := context.Background()
	repo := &mockRepo{tasks: make(map[string]*domain.Task)}

	task := domain.Task{
		Title:      "Test",
		SourceHash: "same_hash",
	}

	first, _ := SaveIfNotDuplicate(ctx, task, repo)
	second, err := SaveIfNotDuplicate(ctx, task, repo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !first {
		t.Error("expected first to succeed")
	}
	if second {
		t.Error("expected second to be duplicate")
	}
}

// TestSaveIfNotDuplicate_Unit_AutoHash проверяет автогенерацию хеша.
func TestSaveIfNotDuplicate_Unit_AutoHash(t *testing.T) {
	ctx := context.Background()
	repo := &mockRepo{tasks: make(map[string]*domain.Task)}

	task := domain.Task{
		Title:       "Test",
		Source:      domain.Source{ID: "test_source"},
		SourceURL:   "http://example.com",
		Description: "test description",
	}

	added, err := SaveIfNotDuplicate(ctx, task, repo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !added {
		t.Error("expected true")
	}

	// Проверяем, что хеш был сгенерирован — у сохранённой задачи в моке
	for _, tsk := range repo.tasks {
		if tsk.SourceHash == "" {
			t.Error("expected source_hash to be auto-generated in stored task")
		}
	}
}

// TestEnsureFilter проверяет значения по умолчанию.
func TestEnsureFilter(t *testing.T) {
	f := EnsureFilter(Filter{Limit: 0})
	if f.Limit != defaultLimit {
		t.Errorf("expected default limit %d, got %d", defaultLimit, f.Limit)
	}

	f = EnsureFilter(Filter{Limit: 999})
	if f.Limit != defaultMaxLimit {
		t.Errorf("expected max limit %d, got %d", defaultMaxLimit, f.Limit)
	}

	f = EnsureFilter(Filter{Offset: -1})
	if f.Offset != 0 {
		t.Errorf("expected 0 offset, got %d", f.Offset)
	}
}
