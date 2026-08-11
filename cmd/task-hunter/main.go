// task-hunter — сервис сбора и нормализации кандидатов задач.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/overmindv/task-hunter/config"
	"github.com/overmindv/task-hunter/internal/collection"
)

// main загружает конфигурацию и завершает процесс с ненулевым кодом при ошибке.
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(logger); err != nil {
		logger.Error("task-hunter stopped with error", "error", err)
		os.Exit(1)
	}
}

// run связывает HTTP, scheduler и worker и гарантирует порядок graceful shutdown.
func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load task-hunter config: %w", err)
	}

	if err := cfg.ValidateRuntime(); err != nil {
		return fmt.Errorf("validate task-hunter runtime config: %w", err)
	}

	db, err := openDB(cfg)
	if err != nil {
		return err
	}
	defer db.Close()

	store := collection.NewStore(db)

	telegramReader := collection.NewMTProtoReader(cfg.Telegram.APIID, cfg.Telegram.APIHash, cfg.Telegram.SessionPath)
	websiteReader := collection.NewDirectWebsiteReader(&http.Client{Timeout: 30 * time.Second})
	defer websiteReader.Close()

	sink := collection.NewTasksITClient(cfg.TasksIT.URL, cfg.TasksIT.Token, cfg.TasksIT.Timeout, cfg.TasksIT.MaxRetries)
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("read worker hostname: %w", err)
	}

	worker := collection.NewWorker(store, telegramReader, websiteReader, sink, hostname, cfg.Worker.PollInterval, cfg.Worker.Lease)
	scheduler := collection.NewScheduler(store, cfg.Telegram.Channels, cfg.Worker.DefaultLimit, cfg.Worker.Bootstrap, cfg.Schedule.CollectCron)

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := scheduler.Start(rootCtx); err != nil {
		return err
	}

	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		worker.Run(rootCtx)
	}()

	handler := collection.NewHTTPHandler(store, logger, cfg.Security.GatewayToken, cfg.Telegram.Channels, cfg.Worker.DefaultLimit, cfg.Telegram.SessionPath)
	server := &http.Server{
		Addr:         cfg.HTTP.Address,
		Handler:      handler,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  60 * time.Second,
	}

	serverError := make(chan error, 1)
	go func() {
		logger.Info("task-hunter HTTP server started", "address", cfg.HTTP.Address)
		serverError <- server.ListenAndServe()
	}()

	var runErr error
	select {
	case <-rootCtx.Done():
	case err := <-serverError:
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = fmt.Errorf("serve task-hunter HTTP: %w", err)
		}
	}
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown task-hunter HTTP: %w", err)
	}
	if err := scheduler.Stop(shutdownCtx); err != nil {
		return err
	}

	select {
	case <-workerDone:
	case <-shutdownCtx.Done():
		return fmt.Errorf("wait task-hunter worker: %w", shutdownCtx.Err())
	}

	return runErr
}

// openDB настраивает PostgreSQL pool без выполнения миграций в runtime-контейнере.
func openDB(cfg *config.Config) (*sql.DB, error) {
	db, err := sql.Open("pgx", cfg.Database.DSN)
	if err != nil {
		return nil, fmt.Errorf("open task-hunter database: %w", err)
	}

	db.SetMaxOpenConns(cfg.Database.MaxConns)
	db.SetMaxIdleConns(cfg.Database.MinConns)
	db.SetConnMaxLifetime(cfg.Database.MaxConnLifetime)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("ping task-hunter database: %w", err)
	}

	return db, nil
}
