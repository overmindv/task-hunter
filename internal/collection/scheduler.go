package collection

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

// Scheduler ставит Telegram jobs в очередь и не выполняет сбор самостоятельно.
type Scheduler struct {
	store      *Store
	channels   []string
	limit      int
	bootstrap  time.Duration
	cron       *cron.Cron
	cronExpr   string
	jobTimeout time.Duration
}

// NewScheduler создаёт UTC-планировщик с пятикомпонентным cron parser.
func NewScheduler(store *Store, channels []string, limit int, bootstrap time.Duration, cronExpr string) *Scheduler {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

	return &Scheduler{
		store:      store,
		channels:   append([]string(nil), channels...),
		limit:      limit,
		bootstrap:  bootstrap,
		cron:       cron.New(cron.WithLocation(time.UTC), cron.WithParser(parser)),
		cronExpr:   cronExpr,
		jobTimeout: 30 * time.Second,
	}
}

// Start ставит bootstrap при отсутствии checkpoint и запускает cron.
func (s *Scheduler) Start(ctx context.Context) error {
	if len(s.channels) == 0 {
		return nil
	}

	if err := s.enqueueBootstrap(ctx); err != nil {
		return fmt.Errorf("enqueue bootstrap collection: %w", err)
	}

	if _, err := s.cron.AddFunc(s.cronExpr, func() {
		jobCtx, cancel := context.WithTimeout(context.Background(), s.jobTimeout)
		defer cancel()
		if err := s.enqueueScheduled(jobCtx, time.Now().UTC()); err != nil {
			slog.Error("enqueue scheduled collection", "error", err)
		}
	}); err != nil {
		return fmt.Errorf("register collection cron: %w", err)
	}

	s.cron.Start()

	return nil
}

// Stop прекращает новые cron ticks и ожидает текущую enqueue-функцию.
func (s *Scheduler) Stop(ctx context.Context) error {
	if len(s.channels) == 0 {
		return nil
	}

	stopped := s.cron.Stop()

	select {
	case <-stopped.Done():
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop collection scheduler: %w", ctx.Err())
	}
}

// enqueueBootstrap создаёт один job на последние bootstrap часов только для каналов без checkpoint.
func (s *Scheduler) enqueueBootstrap(ctx context.Context) error {
	missing := make([]string, 0, len(s.channels))
	for _, channel := range s.channels {
		checkpoint, err := s.store.GetCheckpoint(ctx, channel)
		if err != nil {
			return fmt.Errorf("get checkpoint for %s: %w", channel, err)
		}
		if checkpoint == nil {
			missing = append(missing, channel)
		}
	}

	if len(missing) == 0 {
		return nil
	}

	sort.Strings(missing)
	now := time.Now().UTC()
	key := uuid.NewSHA1(uuid.NameSpaceOID, []byte("task-hunter/bootstrap/"+strings.Join(missing, ",")))

	job, err := s.store.CreateScheduled(ctx, "bootstrap", key, missing, now.Add(-s.bootstrap), now, s.limit)
	if err != nil {
		return fmt.Errorf("create bootstrap job: %w", err)
	}

	// Первый запуск без подготовленной MTProto session может завершить bootstrap ошибкой.
	// После устранения причины следующий restart повторяет тот же идемпотентный job.
	if job.Status == JobFailed {
		if err := s.store.RequeueFailed(ctx, job.ID); err != nil {
			return fmt.Errorf("requeue failed bootstrap job: %w", err)
		}
	}

	return nil
}

// enqueueScheduled создаёт один job на cron slot; unique key защищает реплики от дублей.
func (s *Scheduler) enqueueScheduled(ctx context.Context, now time.Time) error {
	slot := now.UTC().Truncate(time.Minute)
	key := uuid.NewSHA1(uuid.NameSpaceOID, []byte("task-hunter/scheduled/"+slot.Format(time.RFC3339)))

	_, err := s.store.CreateScheduled(ctx, "scheduled", key, s.channels, slot.Add(-31*24*time.Hour), slot, s.limit)
	if err != nil {
		return fmt.Errorf("create scheduled job: %w", err)
	}

	return nil
}
