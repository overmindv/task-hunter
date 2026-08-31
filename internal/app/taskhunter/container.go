// Package taskhunter связывает паркер-инфраструктуру с бизнес-логикой task-hunter.
package taskhunter

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/overmindv/parker"

	"github.com/overmindv/task-hunter/config"
	"github.com/overmindv/task-hunter/internal/collection"
)

// telegramSessionReady проверяет подготовленную MTProto session для включённого Telegram.
func telegramSessionReady(sessionPath string) parker.HealthCheckFunc {
	return func(context.Context) error {
		info, err := os.Stat(sessionPath)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("telegram session not ready at %s", sessionPath)
		}

		return nil
	}
}

// Build регистрирует зависимости task-hunter на каркас parker.
// PostgreSQL, HTTP/health/metrics/logging предоставляет parker; здесь остаётся
// только очередь сбора: HTTP admin API, worker и cron-планировщик.
func Build(app *parker.App) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load task-hunter config: %w", err)
	}
	if err := cfg.ValidateRuntime(); err != nil {
		return fmt.Errorf("validate task-hunter runtime config: %w", err)
	}

	pool, err := app.Postgres()
	if err != nil {
		return fmt.Errorf("task-hunter postgres: %w", err)
	}

	store := collection.NewStore(pool)

	telegramReader := collection.NewMTProtoReader(cfg.Telegram.APIID, cfg.Telegram.APIHash, cfg.Telegram.SessionPath)
	websiteReader := collection.NewDirectWebsiteReader(&http.Client{Timeout: 30 * time.Second}).WithCodeforcesReaderURL(cfg.Website.CodeforcesReaderURL)

	sink := collection.NewTasksClient(cfg.Tasks.URL, cfg.Tasks.Token, cfg.Tasks.Timeout, cfg.Tasks.MaxRetries)
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("read worker hostname: %w", err)
	}

	worker := collection.NewWorker(store, telegramReader, websiteReader, sink, hostname, cfg.Worker.PollInterval, cfg.Worker.Lease)
	channels, telegramSessionPath := telegramRuntime(cfg)
	scheduler := collection.NewScheduler(store, channels, cfg.Worker.DefaultLimit, cfg.Worker.Bootstrap, cfg.Schedule.CollectCron)

	if telegramSessionPath != "" {
		app.AddHealthCheck("telegram-session", telegramSessionReady(telegramSessionPath))
	}

	collection.Register(app.HTTP(), store, app.Logger(), cfg.Security.GatewayToken, channels, cfg.Worker.DefaultLimit, telegramSessionPath)

	app.AddRunnable("collection-worker", func(ctx context.Context) error {
		worker.Run(ctx)
		websiteReader.Close()

		return nil
	})
	app.AddRunnable("collection-scheduler", schedulerRunnable(scheduler))

	return nil
}

// telegramRuntime отключает Telegram-задания в локальном режиме.
func telegramRuntime(cfg *config.Config) ([]string, string) {
	if !cfg.Telegram.Enabled {
		return nil, ""
	}

	return cfg.Telegram.Channels, cfg.Telegram.SessionPath
}

// schedulerRunnable запускает cron и ожидает отмены контекста перед выключением.
func schedulerRunnable(scheduler *collection.Scheduler) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := scheduler.Start(ctx); err != nil {
			return err
		}
		<-ctx.Done()

		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		return scheduler.Stop(stopCtx)
	}
}
