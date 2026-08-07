// Package pipeline реализует конвейер обработки сырых данных в нормализованные задачи.
//
// Пайплайн — это последовательность процессоров (Processor), каждый из которых
// выполняет один этап обработки: извлечение, парсинг, нормализация, классификация, валидация.
//
// Использование:
//
//	pl := pipeline.NewPipeline()
//	pl.AddProcessor("extractor", extractor)
//	result, err := pl.Run(ctx, rawTask)
//	// result.Task — итоговая задача
//	// result.Stages — результаты каждого этапа
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"diploma/internal/parser/domain"
)

// Processor — интерфейс одного этапа обработки задачи.
//
// Process принимает сырые данные из источника и указатель на задачу,
// которая заполняется по мере прохождения этапов.
// Процессоры вызываются последовательно, каждый может читать и дополнять
// поля задачи.
type Processor interface {
	// Process обрабатывает сырые данные и наполняет задачу.
	// Если Process возвращает ошибку, выполнение пайплайна прекращается.
	Process(ctx context.Context, raw domain.RawTask, task *domain.Task) error
}

// ProcessorFunc — адаптер для использования функции как Processor.
type ProcessorFunc func(ctx context.Context, raw domain.RawTask, task *domain.Task) error

// Process вызывает функцию-обработчик.
func (f ProcessorFunc) Process(ctx context.Context, raw domain.RawTask, task *domain.Task) error {
	return f(ctx, raw, task)
}

// StageResult — результат выполнения одного этапа пайплайна.
type StageResult struct {
	Name     string        // Название этапа
	Duration time.Duration // Длительность выполнения
	Error    error         // Ошибка (если была)
}

// Result — полный результат выполнения пайплайна.
type Result struct {
	Task     domain.Task   // Итоговая задача
	Duration time.Duration // Общее время выполнения всех этапов
	Stages   []StageResult // Результаты по каждому этапу
}

// StageError — ошибка конкретного этапа с указанием его имени.
type StageError struct {
	Stage string
	Err   error
}

func (e *StageError) Error() string {
	return fmt.Sprintf("pipeline stage %q: %v", e.Stage, e.Err)
}

func (e *StageError) Unwrap() error {
	return e.Err
}

// Pipeline — конвейер обработки задач.
// Выполняет процессоры последовательно, логируя каждый этап.
type Pipeline struct {
	processors []Processor
	names      []string
}

// NewPipeline создаёт пустой пайплайн.
func NewPipeline() *Pipeline {
	return &Pipeline{}
}

// AddProcessor добавляет процессор в конец цепочки.
func (p *Pipeline) AddProcessor(name string, proc Processor) {
	p.processors = append(p.processors, proc)
	p.names = append(p.names, name)
}

// Run прогоняет сырые данные через все процессоры по порядку.
// Каждый процессор дополняет одну и ту же задачу.
// Если любой процессор возвращает ошибку — выполнение останавливается.
func (p *Pipeline) Run(ctx context.Context, raw domain.RawTask) (Result, error) {
	if len(p.processors) == 0 {
		return Result{}, errors.New("pipeline: no processors registered")
	}

	start := time.Now()
	var task domain.Task
	var stages []StageResult

	for i, proc := range p.processors {
		name := p.names[i]

		slog.Debug("pipeline: starting stage",
			"stage", name,
			"step", i+1,
			"total", len(p.processors),
		)

		stageStart := time.Now()
		err := proc.Process(ctx, raw, &task)
		stageDuration := time.Since(stageStart)

		if err != nil {
			slog.Warn("pipeline: stage failed",
				"stage", name,
				"duration", stageDuration,
				"error", err,
			)

			stages = append(stages, StageResult{
				Name:     name,
				Duration: stageDuration,
				Error:    err,
			})

			return Result{
				Duration: time.Since(start),
				Stages:   stages,
			}, &StageError{Stage: name, Err: err}
		}

		slog.Debug("pipeline: stage completed",
			"stage", name,
			"duration", stageDuration,
		)

		stages = append(stages, StageResult{
			Name:     name,
			Duration: stageDuration,
		})
	}

	totalDuration := time.Since(start)

	slog.Info("pipeline: completed",
		"total_duration", totalDuration,
		"stages", len(stages),
	)

	return Result{
		Task:     task,
		Duration: totalDuration,
		Stages:   stages,
	}, nil
}

// NewDefaultPipeline создаёт пайплайн со стандартной цепочкой процессоров.
// На данный момент содержит заглушки, которые будут заменены реальными
// реализациями в следующих задачах.
func NewDefaultPipeline() *Pipeline {
	p := NewPipeline()

	// Extractor — заполняет базовые поля из сырых данных
	p.AddProcessor("extractor", ProcessorFunc(func(_ context.Context, raw domain.RawTask, task *domain.Task) error {
		task.Source = raw.Source
		task.SourceURL = raw.SourceURL
		task.CreatedAt = raw.RetrievedAt
		task.UpdatedAt = raw.RetrievedAt
		task.SourceHash = domain.GenerateSourceHash(raw.Source.ID, raw.SourceURL, raw.RawContent)
		// Временный title, пока заглушка. Реальный экстрактор будет извлекать из контента.
		task.Title = raw.Source.Name
		return nil
	}))

	// Parser (заглушка) — заполняет описание из сырого контента
	p.AddProcessor("parser", ProcessorFunc(func(_ context.Context, raw domain.RawTask, task *domain.Task) error {
		if task.Description == "" && len(raw.RawContent) > 0 {
			task.Description = string(raw.RawContent)
		}
		return nil
	}))

	// Normalizer (заглушка)
	p.AddProcessor("normalizer", ProcessorFunc(func(_ context.Context, raw domain.RawTask, task *domain.Task) error {
		_ = raw
		return nil
	}))

	// Classifier (заглушка)
	p.AddProcessor("classifier", ProcessorFunc(func(_ context.Context, raw domain.RawTask, task *domain.Task) error {
		_ = raw
		return nil
	}))

	// Validator (заглушка)
	p.AddProcessor("validator", ProcessorFunc(func(_ context.Context, raw domain.RawTask, task *domain.Task) error {
		_ = raw
		return task.Validate()
	}))

	return p
}
