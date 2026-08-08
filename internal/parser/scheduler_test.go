package parser

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"diploma/internal/parser/domain"
	"diploma/internal/parser/pipeline"
	"diploma/internal/parser/source"
	"diploma/internal/parser/storage"
)

// --- Mocks ---

type mockCollector struct {
	id     domain.SourceID
	tasks  []domain.RawTask
	err    error
	called bool
}

func (m *mockCollector) ID() domain.SourceID { return m.id }

func (m *mockCollector) Connect(_ context.Context) error { return nil }

func (m *mockCollector) Collect(_ context.Context) ([]domain.RawTask, error) {
	m.called = true
	return m.tasks, m.err
}

func (m *mockCollector) Close() error { return nil }

type mockPipeline struct {
	mu     sync.Mutex
	calls  int
	tasks  []pipeline.Result
	errs   []error
	callCh chan struct{} // сигнал при каждом вызове
}

func (m *mockPipeline) Run(_ context.Context, _ domain.RawTask) (pipeline.Result, error) {
	m.mu.Lock()
	idx := m.calls
	m.calls++
	m.mu.Unlock()

	if m.callCh != nil {
		select {
		case m.callCh <- struct{}{}:
		default:
		}
	}

	if idx < len(m.errs) && m.errs[idx] != nil {
		return pipeline.Result{}, m.errs[idx]
	}

	var t domain.Task
	if idx < len(m.tasks) {
		t = m.tasks[idx].Task
	}
	return pipeline.Result{Task: t}, nil
}

type mockRepository struct {
	mu       sync.Mutex
	saves    int
	skips    int
	err      error
	errAfter int // после N успешных вызовов возвращать ошибку
}

func (m *mockRepository) Save(_ context.Context, _ domain.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saves++

	if m.err != nil {
		after := m.errAfter
		if after > 0 && m.saves >= after {
			return m.err
		}
	}
	return nil
}

func (m *mockRepository) FindBySourceHash(_ context.Context, _ string) (*domain.Task, error) {
	return nil, nil
}

func (m *mockRepository) List(_ context.Context, _ storage.Filter) ([]domain.Task, error) {
	return nil, nil
}

func (m *mockRepository) GetByID(_ context.Context, _ string) (*domain.Task, error) {
	return nil, nil
}

func (m *mockRepository) Count(_ context.Context, _ storage.Filter) (int, error) {
	return 0, nil
}

// --- Tests ---

// TestRunOnce_Simple проверяет базовый цикл: 1 источник → 1 задача → сохранение.
func TestRunOnce_Simple(t *testing.T) {
	collector := &mockCollector{
		id: domain.SourceCodeforces,
		tasks: []domain.RawTask{
			{SourceURL: "https://codeforces.com/1"},
		},
	}
	sm := source.NewManager(collector)

	pl := &pipeline.Pipeline{}
	// Вставляем mock-процессор
	pl.AddProcessor("mock", pipeline.ProcessorFunc(func(_ context.Context, _ domain.RawTask, task *domain.Task) error {
		task.Title = "Task 1"
		task.SourceURL = "https://codeforces.com/1"
		return nil
	}))

	repo := &mockRepository{}

	s := NewScheduler(sm, pl, repo)
	summary, err := s.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if summary.CollectedTotal != 1 {
		t.Errorf("expected 1 collected, got %d", summary.CollectedTotal)
	}
	if summary.SavedTotal != 1 {
		t.Errorf("expected 1 saved, got %d", summary.SavedTotal)
	}
	if summary.ErrorCount != 0 {
		t.Errorf("expected 0 errors, got %d", summary.ErrorCount)
	}
}

// TestRunOnce_MultipleSources проверяет сбор из нескольких источников.
func TestRunOnce_MultipleSources(t *testing.T) {
	c1 := &mockCollector{
		id: domain.SourceCodeforces,
		tasks: []domain.RawTask{
			{SourceURL: "https://codeforces.com/1"},
			{SourceURL: "https://codeforces.com/2"},
		},
	}
	c2 := &mockCollector{
		id: domain.SourceLeetCode,
		tasks: []domain.RawTask{
			{SourceURL: "https://leetcode.com/1"},
		},
	}
	sm := source.NewManager(c1, c2)

	pl := &pipeline.Pipeline{}
	pl.AddProcessor("mock", pipeline.ProcessorFunc(func(_ context.Context, _ domain.RawTask, task *domain.Task) error {
		task.Title = "Task"
		return nil
	}))

	repo := &mockRepository{}

	s := NewScheduler(sm, pl, repo)
	summary, err := s.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if summary.CollectedTotal != 3 {
		t.Errorf("expected 3 collected, got %d", summary.CollectedTotal)
	}
	if summary.SavedTotal != 3 {
		t.Errorf("expected 3 saved, got %d", summary.SavedTotal)
	}
	if summary.DuplicatesTotal != 0 {
		t.Errorf("expected 0 duplicates, got %d", summary.DuplicatesTotal)
	}
}

// TestRunOnce_SourceError проверяет, что ошибка в одном источнике не блокирует другие.
func TestRunOnce_SourceError(t *testing.T) {
	c1 := &mockCollector{
		id:  domain.SourceCodeforces,
		err: errors.New("cf error"),
	}
	c2 := &mockCollector{
		id: domain.SourceLeetCode,
		tasks: []domain.RawTask{
			{SourceURL: "https://leetcode.com/1"},
		},
	}
	sm := source.NewManager(c1, c2)

	pl := &pipeline.Pipeline{}
	pl.AddProcessor("mock", pipeline.ProcessorFunc(func(_ context.Context, _ domain.RawTask, task *domain.Task) error {
		task.Title = "Task"
		return nil
	}))

	repo := &mockRepository{}

	s := NewScheduler(sm, pl, repo)
	summary, err := s.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if summary.CollectedTotal != 1 {
		t.Errorf("expected 1 collected (1 source with error), got %d", summary.CollectedTotal)
	}
	if summary.SavedTotal != 1 {
		t.Errorf("expected 1 saved, got %d", summary.SavedTotal)
	}
	if summary.ErrorCount == 0 {
		t.Error("expected at least 1 error from source")
	}
}

// TestRunOnce_PipelineError продолжает обработку других задач.
func TestRunOnce_PipelineError(t *testing.T) {
	collector := &mockCollector{
		id: domain.SourceCodeforces,
		tasks: []domain.RawTask{
			{SourceURL: "https://codeforces.com/1"},
			{SourceURL: "https://codeforces.com/2"},
		},
	}
	sm := source.NewManager(collector)

	pl := &pipeline.Pipeline{}
	callCount := 0
	pl.AddProcessor("mock", pipeline.ProcessorFunc(func(_ context.Context, raw domain.RawTask, task *domain.Task) error {
		callCount++
		if callCount == 1 {
			return errors.New("pipeline error on first task")
		}
		task.Title = "Task 2"
		task.SourceURL = raw.SourceURL
		return nil
	}))

	repo := &mockRepository{}

	s := NewScheduler(sm, pl, repo)
	summary, err := s.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if summary.ErrorCount < 1 {
		t.Error("expected at least 1 pipeline error")
	}
	if summary.SavedTotal != 1 {
		t.Errorf("expected 1 saved (second task), got %d", summary.SavedTotal)
	}
}

// TestRunOnce_Duplicate проверяет обработку дубликатов.
func TestRunOnce_Duplicate(t *testing.T) {
	collector := &mockCollector{
		id: domain.SourceCodeforces,
		tasks: []domain.RawTask{
			{SourceURL: "https://codeforces.com/1"},
			{SourceURL: "https://codeforces.com/2"},
		},
	}
	sm := source.NewManager(collector)

	pl := &pipeline.Pipeline{}
	pl.AddProcessor("mock", pipeline.ProcessorFunc(func(_ context.Context, _ domain.RawTask, task *domain.Task) error {
		task.Title = "Task"
		return nil
	}))

	s := NewScheduler(sm, pl, &mockRepository{})
	summary, err := s.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if summary.SavedTotal != 2 {
		t.Errorf("expected 2 saved (no duplicate detection), got %d", summary.SavedTotal)
	}
}

// TestRunOnce_ContextCancelled проверяет остановку при отмене контекста.
func TestRunOnce_ContextCancelled(t *testing.T) {
	ch := make(chan struct{})

	collector := &mockCollector{
		id: domain.SourceCodeforces,
		tasks: []domain.RawTask{
			{SourceURL: "https://codeforces.com/1"},
		},
	}
	sm := source.NewManager(collector)

	pl := &pipeline.Pipeline{}
	pl.AddProcessor("mock", pipeline.ProcessorFunc(func(_ context.Context, _ domain.RawTask, task *domain.Task) error {
		<-ch // блокируемся
		task.Title = "Task"
		return nil
	}))

	repo := &mockRepository{}
	s := NewScheduler(sm, pl, repo)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // контекст уже отменён

	summary, err := s.RunOnce(ctx)
	if err != nil {
		t.Errorf("RunOnce should handle cancelled context gracefully, got: %v", err)
	}
	_ = summary
	close(ch)
}

// TestStop_NoBlock проверяет, что Stop не блокирует долго.
func TestStop_NoBlock(t *testing.T) {
	collector := &mockCollector{id: domain.SourceCodeforces}
	sm := source.NewManager(collector)

	pl := &pipeline.Pipeline{}
	pl.AddProcessor("mock", pipeline.ProcessorFunc(func(_ context.Context, _ domain.RawTask, task *domain.Task) error {
		task.Title = "Task"
		return nil
	}))

	repo := &mockRepository{}
	s := NewScheduler(sm, pl, repo)

	// Запускаем в фоне
	ctx := context.Background()
	go func() {
		_ = s.Start(ctx, "@every 1h")
	}()

	// Ждём запуска
	time.Sleep(100 * time.Millisecond)

	if !s.IsRunning() {
		t.Fatal("expected scheduler to be running")
	}

	// Останавливаем
	start := time.Now()
	err := s.Stop()
	if err != nil {
		t.Errorf("Stop: %v", err)
	}

	// Stop должен сработать быстро
	if dur := time.Since(start); dur > 5*time.Second {
		t.Errorf("Stop took too long: %v", dur)
	}

	// Ждём завершения
	time.Sleep(200 * time.Millisecond)
	if s.IsRunning() {
		t.Error("expected scheduler to be stopped")
	}
}

// TestStart_InvalidCron проверяет обработку невалидного cron-выражения.
func TestStart_InvalidCron(t *testing.T) {
	collector := &mockCollector{id: domain.SourceCodeforces}
	sm := source.NewManager(collector)

	pl := &pipeline.Pipeline{}
	pl.AddProcessor("mock", pipeline.ProcessorFunc(func(_ context.Context, _ domain.RawTask, task *domain.Task) error {
		task.Title = "Task"
		return nil
	}))

	repo := &mockRepository{}
	s := NewScheduler(sm, pl, repo)

	err := s.Start(context.Background(), "invalid-cron")
	if err == nil {
		t.Error("expected error for invalid cron expression")
	}
}

// TestStop_Idempotent проверяет, что повторный Stop безопасен.
func TestStop_Idempotent(t *testing.T) {
	s := NewScheduler(nil, nil, nil)

	// Stop на незапущенном планировщике
	if err := s.Stop(); err != nil {
		t.Errorf("Stop on stopped scheduler: %v", err)
	}
}

// --- helpers ---

