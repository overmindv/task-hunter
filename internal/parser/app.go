package parser

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/overmindv/task-hunter/config"
	"github.com/overmindv/task-hunter/internal/parser/domain"
	"github.com/overmindv/task-hunter/internal/parser/pipeline"
	"github.com/overmindv/task-hunter/internal/parser/source"
	"github.com/overmindv/task-hunter/internal/parser/source/codeforces"
	"github.com/overmindv/task-hunter/internal/parser/source/coderun"
	"github.com/overmindv/task-hunter/internal/parser/source/leetcode"
	"github.com/overmindv/task-hunter/internal/parser/storage"
)

// App — приложение модуля парсинга.
type App struct {
	cfg    *config.Config
	db     *sql.DB
	sched  *Scheduler
	stopCh chan struct{}
}

// NewApp создаёт приложение с загруженной конфигурацией.
// Не инициализирует зависимости — для этого вызывайте Init.
func NewApp(cfg *config.Config) *App {
	return &App{
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}
}

// Init инициализирует все компоненты приложения.
func (a *App) Init(ctx context.Context) error {
	// 1. Настройка логирования
	setupLogging()

	slog.Info("app: initializing parser module")

	// 2. Подключение к БД
	db, err := a.openDB()
	if err != nil {
		return fmt.Errorf("app: open db: %w", err)
	}
	a.db = db

	// 3. Миграции
	slog.Info("app: running migrations")
	if err := storage.RunMigrations(a.cfg.Database.DSN, "migrations"); err != nil {
		return fmt.Errorf("app: migrations: %w", err)
	}

	// 4. Репозиторий
	repo := storage.NewPostgresRepository(db)

	// 5. Менеджер источников
	sm := a.createSourceManager()

	// 6. Пайплайн
	pl := pipeline.NewDefaultPipeline()

	// 7. Планировщик
	a.sched = NewScheduler(sm, pl, repo)

	slog.Info("app: initialization complete",
		"dsn", maskDSN(a.cfg.Database.DSN),
	)

	return nil
}

// Run запускает планировщик и ожидает сигнала остановки.
func (a *App) Run(ctx context.Context) error {
	if a.sched == nil {
		return fmt.Errorf("app: not initialized, call Init first")
	}

	cronExpr := a.cfg.Schedule.CollectCron
	slog.Info("app: starting scheduler",
		"cron", cronExpr,
	)

	// Запускаем планировщик в фоне
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- a.sched.Start(ctx, cronExpr)
	}()

	// Ожидаем сигнала остановки или завершения планировщика
	select {
	case err := <-doneCh:
		return err
	case <-a.stopCh:
		slog.Info("app: stopping...")
		return nil
	case <-ctx.Done():
		slog.Info("app: context canceled")
		return ctx.Err()
	}
}

// Shutdown выполняет graceful shutdown приложения.
func (a *App) Shutdown(timeout time.Duration) error {
	slog.Info("app: shutting down...")

	// Останавливаем планировщик
	if a.sched != nil {
		if err := a.sched.Stop(); err != nil {
			slog.Warn("app: scheduler stop error", "error", err)
		}
	}

	// Закрываем соединение с БД
	if a.db != nil {
		if err := a.db.Close(); err != nil {
			slog.Warn("app: db close error", "error", err)
		}
	}

	slog.Info("app: shutdown complete")
	return nil
}

// WaitSignal блокирует до получения сигнала SIGTERM или SIGINT.
func (a *App) WaitSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	slog.Info("app: received signal", "signal", sig)
}

// SignalStop отправляет сигнал остановки приложению.
func (a *App) SignalStop() {
	close(a.stopCh)
}

// openDB открывает подключение к PostgreSQL.
func (a *App) openDB() (*sql.DB, error) {
	if a.cfg.Database.DSN == "" {
		return nil, fmt.Errorf("PARSER_DATABASE_DSN is required")
	}

	db, err := sql.Open("pgx", a.cfg.Database.DSN)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(a.cfg.Database.MaxConns)
	db.SetMaxIdleConns(a.cfg.Database.MaxConns)
	db.SetConnMaxLifetime(a.cfg.Database.MaxConnLifetime)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return db, nil
}

// createSourceManager создаёт менеджер источников на основе конфигурации.
func (a *App) createSourceManager() *source.Manager {
	activeSources := a.cfg.Sources.ActiveSources()

	var collectors []source.Collector
	for _, s := range activeSources {
		c := a.createCollector(s)
		if c != nil {
			collectors = append(collectors, c)
		}
	}

	return source.NewManager(collectors...)
}

// createCollector создаёт коллектор для указанного источника.
func (a *App) createCollector(s config.ActiveSource) source.Collector {
	switch s.ID {
	case "telegram_analytics", "telegram_ml", "telegram_algorithms":
		slog.Warn("app: telegram collector not fully implemented, skipping", "source", s.ID)
		return nil
	case "leetcode":
		// Импорт избегает циклической зависимости
		return createLeetCodeCollector()
	case "codeforces":
		return createCodeforcesCollector()
	case "coderun":
		return createCodeRunCollector()
	default:
		slog.Warn("app: unknown source", "source", s.ID)
		return nil
	}
}

// setupLogging настраивает структурированное логирование.
func setupLogging() {
	// JSON-формат для прода
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
}

// maskDSN маскирует пароль в DSN для логирования.
func maskDSN(dsn string) string {
	// Простое маскирование: заменяем часть после :// и до @
	result := []byte(dsn)
	for i := 0; i < len(result); i++ {
		if i > 8 && result[i] == '@' {
			// Маскируем от :// до @
			for j := 8; j < i; j++ {
				if result[j] != ':' && result[j] != '/' {
					result[j] = '*'
				}
			}
			break
		}
	}
	return string(result)
}

// --- Функции для создания коллекторов ---

func createLeetCodeCollector() source.Collector {
	return leetcode.NewCollector(domain.SourceLeetCode, &http.Client{
		Timeout: 30 * time.Second,
	})
}

func createCodeforcesCollector() source.Collector {
	return codeforces.NewCollector(domain.SourceCodeforces, &http.Client{
		Timeout: 30 * time.Second,
	})
}

func createCodeRunCollector() source.Collector {
	return coderun.NewCollector(domain.SourceCodeRun, &http.Client{
		Timeout: 30 * time.Second,
	})
}

// RunApp — удобная функция для запуска приложения из main.
func RunApp(ctx context.Context, cfg *config.Config) error {
	app := NewApp(cfg)

	if err := app.Init(ctx); err != nil {
		return fmt.Errorf("init: %w", err)
	}

	// Graceful shutdown в отдельной горутине
	go func() {
		app.WaitSignal()
		app.SignalStop()
	}()

	if err := app.Run(ctx); err != nil {
		return fmt.Errorf("run: %w", err)
	}

	// Выполняем shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = app.Shutdown(30 * time.Second)
		close(done)
	}()

	select {
	case <-done:
		slog.Info("app: shutdown completed")
	case <-shutdownCtx.Done():
		slog.Warn("app: shutdown timeout")
	}

	return nil
}
