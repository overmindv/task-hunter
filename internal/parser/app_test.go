package parser

import (
	"context"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"diploma/config"
)

// TestApp_NewApp проверяет создание App.
func TestApp_NewApp(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: "postgres://localhost:5432/test",
		},
		Schedule: config.ScheduleConfig{
			CollectCron: "0 */6 * * *",
		},
	}

	app := NewApp(cfg)
	if app == nil {
		t.Fatal("NewApp returned nil")
	}
	if app.cfg != cfg {
		t.Error("config not set")
	}
}

// TestApp_InitValidation проверяет валидацию конфига.
func TestApp_InitValidation(t *testing.T) {
	cfg := &config.Config{}
	app := NewApp(cfg)

	// Init должен вернуть ошибку из-за пустого DSN
	err := app.Init(context.Background())
	if err == nil {
		t.Error("expected error for empty DSN")
	}
	if !strings.Contains(err.Error(), "PARSER_DATABASE_DSN") && !strings.Contains(err.Error(), "PARSER_SCHEDULE_COLLECTCRON") {
		t.Errorf("expected error about missing config, got: %v", err)
	}
}

// TestApp_InitNoDB проверяет, что Init не паникует при отсутствии БД.
func TestApp_InitNoDB(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: "postgres://invalid:5432/nonexistent",
		},
		Schedule: config.ScheduleConfig{
			CollectCron: "0 */6 * * *",
		},
	}

	app := NewApp(cfg)

	// Init должна вернуть ошибку (БД недоступна), но не паниковать
	err := app.Init(context.Background())
	if err == nil {
		t.Skip("db is available, skipping")
	}
}

// TestMaskDSN проверяет маскирование пароля в DSN.
func TestMaskDSN(t *testing.T) {
	tests := []struct {
		input    string
		expected string // empty means should not contain original password
	}{
		{
			input: "postgres://user:secret@localhost:5432/db",
		},
		{
			input: "postgres://localhost:5432/db",
		},
		{
			input: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			masked := maskDSN(tc.input)

			// Должна быть непустая
			if tc.input != "" && masked == "" {
				t.Error("masked DSN is empty")
			}

			// Не должна содержать пароль в открытом виде
			if tc.input == "postgres://user:secret@localhost:5432/db" {
				if strings.Contains(masked, "secret") {
					t.Errorf("masked DSN still contains password: %s", masked)
				}
			}

			// Пустой DSN должен остаться пустым
			if tc.input == "" && masked != "" {
				t.Errorf("empty DSN should remain empty, got: %s", masked)
			}
		})
	}
}

// TestWaitSignal проверяет ожидание сигнала.
func TestWaitSignal(t *testing.T) {
	app := NewApp(&config.Config{})

	// Отправляем сигнал в фоне
	go func() {
		time.Sleep(50 * time.Millisecond)
		p, _ := os.FindProcess(os.Getpid())
		_ = p.Signal(syscall.SIGINT)
	}()

	// WaitSignal должна завершиться после получения сигнала
	done := make(chan struct{})
	go func() {
		app.WaitSignal()
		close(done)
	}()

	select {
	case <-done:
		// OK — сигнал принят
	case <-time.After(5 * time.Second):
		t.Fatal("WaitSignal did not return after SIGINT")
	}
}

// TestSignalStop проверяет, что SignalStop корректно закрывает канал.
// SignalStop отправляет сигнал через закрытие канала, что должно быть
// замечено select в методе Run().
func TestSignalStop(t *testing.T) {
	app := NewApp(&config.Config{})

	// SignalStop закрывает stopCh
	stopCh := make(chan struct{})
	app.stopCh = stopCh
	defer func() {
		app.stopCh = make(chan struct{})
	}()

	// В отдельной горутине ожидаем сигнал
	received := make(chan struct{})
	go func() {
		<-stopCh
		close(received)
	}()

	// Отправляем сигнал остановки
	app.SignalStop()

	// Проверяем, что сигнал получен (с таймаутом)
	select {
	case <-received:
		// OK
	case <-time.After(5 * time.Second):
		t.Fatal("SignalStop did not close stopCh")
	}
}

// TestApp_Shutdown проверяет graceful shutdown.
func TestApp_Shutdown(t *testing.T) {
	app := NewApp(&config.Config{})

	// Shutdown незапущенного приложения не должен паниковать
	err := app.Shutdown(1 * time.Second)
	if err != nil {
		t.Errorf("Shutdown error: %v", err)
	}
}

// TestApp_RunWithoutInit проверяет что Run без Init возвращает ошибку.
func TestApp_RunWithoutInit(t *testing.T) {
	app := NewApp(&config.Config{})

	err := app.Run(context.Background())
	if err == nil {
		t.Error("expected error when running without Init")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected 'not initialized' error, got: %v", err)
	}
}

// TestSetupLogging проверяет, что настройка логирования не паникует.
func TestSetupLogging(t *testing.T) {
	// Просто проверяем, что функция не паникует
	orig := os.Stdout
	os.Stdout = nil
	defer func() {
		os.Stdout = orig
		if r := recover(); r != nil {
			t.Errorf("setupLogging panicked: %v", r)
		}
	}()
	setupLogging()
}

// TestCreateCollector проверяет создание коллекторов.
func TestCreateCollector(t *testing.T) {
	cfg := &config.Config{
		Sources: config.SourcesConfig{
			Codeforces: config.SourceConfig{Enabled: true},
			CodeRun:    config.SourceConfig{Enabled: true},
			LeetCode:   config.SourceConfig{Enabled: true},
		},
	}

	app := NewApp(cfg)

	// Проверяем создание менеджера источников
	active := cfg.Sources.ActiveSources()

	if len(active) > 0 {
		// Проверяем, что createSourceManager не паникует
		sm := app.createSourceManager()
		if sm == nil {
			t.Error("createSourceManager returned nil")
		}
	}
}

// TestRunApp_InitError проверяет ошибку инициализации в RunApp.
func TestRunApp_InitError(t *testing.T) {
	cfg := &config.Config{}
	ctx := context.Background()

	err := RunApp(ctx, cfg)
	if err == nil {
		t.Error("expected error for empty config")
	}
}

// --- Concurrent safety tests ---

// TestApp_ConcurrentStop проверяет, что SignalStop и Shutdown безопасны при конкурентном доступе.
func TestApp_ConcurrentStop(t *testing.T) {
	app := NewApp(&config.Config{})

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		app.SignalStop()
	}()

	go func() {
		defer wg.Done()
		_ = app.Shutdown(1 * time.Second)
	}()

	wg.Wait()
}

// --- Benchmark ---

func BenchmarkMaskDSN(b *testing.B) {
	dsn := "postgres://user:supersecretpassword@localhost:5432/diploma"
	for i := 0; i < b.N; i++ {
		maskDSN(dsn)
	}
}
