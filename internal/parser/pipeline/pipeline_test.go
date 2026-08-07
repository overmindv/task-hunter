package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"diploma/internal/parser/domain"
)

// TestPipeline_RunSequential проверяет, что процессоры вызываются по порядку.
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

	p.AddProcessor("third", ProcessorFunc(func(_ context.Context, _ domain.RawTask, task *domain.Task) error {
		if task.Description != "second" {
			t.Errorf("expected description 'second' from previous stage, got %q", task.Description)
		}
		order = append(order, "third")
		task.Type = domain.TaskTypeAlgorithm
		return nil
	}))

	raw := testRawTask()
	result, err := p.Run(ctx, raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Проверяем порядок вызовов
	if len(order) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(order))
	}
	if order[0] != "first" || order[1] != "second" || order[2] != "third" {
		t.Errorf("wrong order: %v", order)
	}

	// Проверяем итоговую задачу
	if result.Task.Title != "first" {
		t.Errorf("expected title 'first', got %q", result.Task.Title)
	}
	if result.Task.Description != "second" {
		t.Errorf("expected description 'second', got %q", result.Task.Description)
	}

	// Проверяем Result
	if len(result.Stages) != 3 {
		t.Fatalf("expected 3 stage results, got %d", len(result.Stages))
	}
	if result.Stages[0].Name != "first" || result.Stages[1].Name != "second" || result.Stages[2].Name != "third" {
		t.Errorf("wrong stage names: %v", result.Stages)
	}
	if result.Duration <= 0 {
		t.Error("expected positive duration")
	}
}

// TestPipeline_ErrorStopsChain проверяет, что ошибка останавливает цепочку.
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

	raw := testRawTask()
	_, err := p.Run(ctx, raw)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if calledAfterError {
		t.Error("processor after error should not be called")
	}

	// Проверяем тип ошибки
	var stageErr *StageError
	if errors.As(err, &stageErr) {
		if stageErr.Stage != "fails" {
			t.Errorf("expected stage 'fails', got %q", stageErr.Stage)
		}
	} else {
		t.Errorf("expected *StageError, got %T: %v", err, err)
	}

	// Проверяем, что в Result только 2 этапа (ok + fails)
	// Проверяем, что у второго этапа есть ошибка
}

// TestPipeline_EmptyPipeline проверяет, что пустой пайплайн возвращает ошибку.
func TestPipeline_EmptyPipeline(t *testing.T) {
	ctx := context.Background()
	p := NewPipeline()

	raw := testRawTask()
	_, err := p.Run(ctx, raw)
	if err == nil {
		t.Fatal("expected error for empty pipeline")
	}
}

// TestPipeline_ResultOnError проверяет, что Result содержит частичный результат при ошибке.
func TestPipeline_ResultOnError(t *testing.T) {
	ctx := context.Background()
	p := NewPipeline()

	p.AddProcessor("first", ProcessorFunc(func(_ context.Context, _ domain.RawTask, task *domain.Task) error {
		task.Title = "partial"
		return nil
	}))

	p.AddProcessor("fails", ProcessorFunc(func(_ context.Context, _ domain.RawTask, _ *domain.Task) error {
		return errors.New("fail")
	}))

	raw := testRawTask()
	result, err := p.Run(ctx, raw)
	if err == nil {
		t.Fatal("expected error")
	}

	// Должен быть хотя бы один stage
	if len(result.Stages) == 0 {
		t.Error("expected at least 1 stage result")
	}

	// Первый этап успешен
	if result.Stages[0].Name != "first" {
		t.Errorf("expected first stage name 'first', got %q", result.Stages[0].Name)
	}
	if result.Stages[0].Error != nil {
		t.Errorf("expected no error on first stage, got %v", result.Stages[0].Error)
	}

	// Второй этап с ошибкой
	if len(result.Stages) >= 2 {
		if result.Stages[1].Error == nil {
			t.Error("expected error on second stage")
		}
	}

	if result.Duration <= 0 {
		t.Error("expected positive duration even on error")
	}
}

// TestPipeline_ContextCancellation проверяет, что контекст отмены прерывает пайплайн.
func TestPipeline_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := NewPipeline()

	p.AddProcessor("first", ProcessorFunc(func(_ context.Context, _ domain.RawTask, _ *domain.Task) error {
		cancel() // отменяем контекст
		return nil
	}))

	p.AddProcessor("second", ProcessorFunc(func(c context.Context, _ domain.RawTask, _ *domain.Task) error {
		select {
		case <-c.Done():
			return c.Err()
		default:
			return nil
		}
	}))

	raw := testRawTask()
	result, err := p.Run(ctx, raw)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}

	if len(result.Stages) < 2 {
		t.Errorf("expected at least 2 stages, got %d", len(result.Stages))
	}
}

// TestPipeline_ProcessorsBuildTask проверяет, что процессоры наполняют задачу.
func TestPipeline_ProcessorsBuildTask(t *testing.T) {
	ctx := context.Background()
	p := NewPipeline()

	p.AddProcessor("source", ProcessorFunc(func(_ context.Context, raw domain.RawTask, task *domain.Task) error {
		task.Source = raw.Source
		task.SourceURL = raw.SourceURL
		return nil
	}))

	p.AddProcessor("content", ProcessorFunc(func(_ context.Context, raw domain.RawTask, task *domain.Task) error {
		task.Title = "Extracted"
		task.Description = string(raw.RawContent)
		task.Type = domain.TaskTypeAlgorithm
		task.Difficulty = domain.DifficultyMedium
		return nil
	}))

	raw := testRawTask()
	result, err := p.Run(ctx, raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Task.Source.ID != domain.SourceLeetCode {
		t.Errorf("expected SourceLeetCode, got %v", result.Task.Source.ID)
	}
	if result.Task.Title != "Extracted" {
		t.Errorf("expected 'Extracted', got %q", result.Task.Title)
	}
	if result.Task.Description != "test content" {
		t.Errorf("expected 'test content', got %q", result.Task.Description)
	}
	if result.Task.Type != domain.TaskTypeAlgorithm {
		t.Errorf("expected Algorithm, got %v", result.Task.Type)
	}
}

// TestNewDefaultPipeline проверяет создание пайплайна по умолчанию.
func TestNewDefaultPipeline(t *testing.T) {
	p := NewDefaultPipeline()
	if p == nil {
		t.Fatal("expected non-nil pipeline")
	}

	raw := testRawTask()
	ctx := context.Background()

	result, err := p.Run(ctx, raw)
	if err != nil {
		t.Fatalf("default pipeline run failed: %v", err)
	}

	if result.Task.Source.ID != domain.SourceLeetCode {
		t.Errorf("expected SourceLeetCode, got %v", result.Task.Source.ID)
	}
	if result.Task.SourceHash == "" {
		t.Error("expected source_hash to be generated")
	}
}

// TestPipeline_StageTiming проверяет, что длительность этапов записывается.
func TestPipeline_StageTiming(t *testing.T) {
	ctx := context.Background()
	p := NewPipeline()

	p.AddProcessor("slow", ProcessorFunc(func(_ context.Context, _ domain.RawTask, _ *domain.Task) error {
		time.Sleep(time.Millisecond)
		return nil
	}))

	raw := testRawTask()
	result, err := p.Run(ctx, raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Stages) != 1 {
		t.Fatalf("expected 1 stage, got %d", len(result.Stages))
	}
	if result.Stages[0].Duration < time.Millisecond {
		t.Errorf("expected duration >= 1ms, got %v", result.Stages[0].Duration)
	}
	if result.Duration < result.Stages[0].Duration {
		t.Error("total duration should be >= stage duration")
	}
}

// --- helpers ---

func testRawTask() domain.RawTask {
	return domain.RawTask{
		Source: domain.Source{
			ID:   domain.SourceLeetCode,
			Name: "LeetCode",
			Type: domain.SourceTypeWebsite,
		},
		RawContent:  []byte("test content"),
		SourceURL:   "https://leetcode.com/problems/two-sum",
		RetrievedAt: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
	}
}
