// Package parser реализует планировщик сбора задач по расписанию.
//
// Scheduler запускает полный цикл: сбор сырых задач из источников →
// прогон через пайплайн → сохранение в репозиторий.
package parser

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/overmindv/task-hunter/internal/parser/domain"
	"github.com/overmindv/task-hunter/internal/parser/pipeline"
	"github.com/overmindv/task-hunter/internal/parser/source"
	"github.com/overmindv/task-hunter/internal/parser/storage"
)

// SchedulerSummary — сводка результатов одного цикла сбора.
type SchedulerSummary struct {
	SourcesTotal    int // Всего источников
	CollectedTotal  int // Всего собрано сырых задач
	ProcessedTotal  int // Успешно обработано пайплайном
	DuplicatesTotal int // Дубликатов (уже есть в БД)
	SavedTotal      int // Сохранено новых задач
	ErrorCount      int // Ошибок
	Duration        time.Duration
}

// Scheduler запускает сбор задач по расписанию.
type Scheduler struct {
	sourceManager *source.Manager
	pipeline      *pipeline.Pipeline
	repo          storage.Repository
	cron          *cron.Cron
	cronJobID     cron.EntryID
	mu            sync.Mutex
	running       bool
	stopCh        chan struct{}
}

// NewScheduler создаёт планировщик сбора задач.
func NewScheduler(sm *source.Manager, pl *pipeline.Pipeline, repo storage.Repository) *Scheduler {
	return &Scheduler{
		sourceManager: sm,
		pipeline:      pl,
		repo:          repo,
	}
}

// RunOnce выполняет один цикл сбора: коллекторы → пайплайн → сохранение.
// Возвращает сводку. Единичные ошибки не прерывают общий процесс.
func (s *Scheduler) RunOnce(ctx context.Context) (*SchedulerSummary, error) {
	start := time.Now()
	slog.Info("scheduler: starting collection cycle")

	// 1. Сбор со всех источников
	results := s.sourceManager.CollectAll(ctx)
	summary := &SchedulerSummary{
		SourcesTotal: len(results),
	}

	// Собираем все сырые задачи
	var allRaw []rawTaskWithSource
	for _, r := range results {
		if r.Err != nil {
			slog.Warn("scheduler: source error",
				"source_id", r.SourceID,
				"error", r.Err,
			)
			summary.ErrorCount++
			continue
		}
		for _, task := range r.Tasks {
			allRaw = append(allRaw, rawTaskWithSource{sourceID: r.SourceID, raw: task})
		}
		summary.CollectedTotal += len(r.Tasks)
	}

	slog.Info("scheduler: collected raw tasks",
		"collected", summary.CollectedTotal,
		"sources", summary.SourcesTotal,
		"errors", summary.ErrorCount,
	)

	// 2. Прогон через пайплайн и сохранение
	for _, rts := range allRaw {
		if err := ctx.Err(); err != nil {
			slog.Warn("scheduler: context cancelled, stopping collection", "error", err)
			break
		}

		result, err := s.pipeline.Run(ctx, rts.raw)
		if err != nil {
			slog.Warn("scheduler: pipeline error",
				"source_id", rts.sourceID,
				"error", err,
			)
			summary.ErrorCount++
			continue
		}

		summary.ProcessedTotal++

		// 3. Сохранение в репозиторий
		saved, err := storage.SaveIfNotDuplicate(ctx, result.Task, s.repo)
		if err != nil {
			slog.Warn("scheduler: save error",
				"source_id", rts.sourceID,
				"error", err,
			)
			summary.ErrorCount++
			continue
		}

		if saved {
			summary.SavedTotal++
		} else {
			summary.DuplicatesTotal++
		}
	}

	summary.Duration = time.Since(start)

	slog.Info("scheduler: collection cycle completed",
		"collected", summary.CollectedTotal,
		"processed", summary.ProcessedTotal,
		"saved", summary.SavedTotal,
		"duplicates", summary.DuplicatesTotal,
		"errors", summary.ErrorCount,
		"duration", summary.Duration,
	)

	return summary, nil
}

// Start запускает планировщик по cron-расписанию.
// cronExpr — стандартное 5-польное cron-выражение.
// Блокирует до вызова Stop() или ошибки.
func (s *Scheduler) Start(ctx context.Context, cronExpr string) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("scheduler: already running")
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.mu.Unlock()

	// Подключаем все коллекторы
	if err := s.sourceManager.ConnectAll(ctx); err != nil {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		return fmt.Errorf("scheduler: connect sources: %w", err)
	}

	// Создаём cron с поддержкой секунд (опция)
	c := cron.New(cron.WithParser(cron.NewParser(
		cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)))

	jobID, err := c.AddFunc(cronExpr, func() {
		// Запускаем сбор
		runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		summary, err := s.RunOnce(runCtx)
		if err != nil {
			slog.Error("scheduler: run cycle error", "error", err)
			return
		}
		_ = summary
	})
	if err != nil {
		c.Stop()
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		return fmt.Errorf("scheduler: invalid cron expression %q: %w", cronExpr, err)
	}

	s.cron = c
	s.cronJobID = jobID

	slog.Info("scheduler: started",
		"cron", cronExpr,
	)

	// Запускаем cron (не блокирует)
	c.Start()

	// Ожидаем сигнала остановки
	<-s.stopCh

	// Graceful shutdown
	slog.Info("scheduler: stopping...")
	stopCtx := c.Stop() // Stop возвращает контекст, ожидающий завершения текущего задания

	// Ждём завершения текущего задания с таймаутом
	select {
	case <-stopCtx.Done():
		slog.Info("scheduler: stopped gracefully")
	case <-time.After(30 * time.Second):
		slog.Warn("scheduler: stop timeout, forcing shutdown")
	}

	s.mu.Lock()
	s.running = false
	s.mu.Unlock()

	return nil
}

// Stop останавливает планировщик. Не блокирует дольше timeout.
func (s *Scheduler) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	// Сигналим остановку
	if s.stopCh != nil {
		close(s.stopCh)
	}

	return nil
}

// IsRunning возвращает true, если планировщик запущен.
func (s *Scheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// rawTaskWithSource связывает сырую задачу с источником.
type rawTaskWithSource struct {
	sourceID domain.SourceID
	raw      domain.RawTask
}

// Ensure interfaces are satisfied.
var _ = (*Scheduler)(nil)
