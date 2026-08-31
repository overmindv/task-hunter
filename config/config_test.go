package config

import (
	"testing"
	"time"
)

// TestLoad_Success проверяет загрузку полной конфигурации.
func TestLoad_Success(t *testing.T) {
	t.Setenv("PARSER_DATABASE_DSN", "postgres://localhost:5432/tasks")
	t.Setenv("PARSER_DATABASE_MAXCONNS", "20")
	t.Setenv("PARSER_DATABASE_MINCONNS", "5")
	t.Setenv("PARSER_DATABASE_MAXCONNLIFETIME", "1h")
	t.Setenv("PARSER_SCHEDULE_COLLECTCRON", "0 */12 * * *")
	t.Setenv("PARSER_SOURCE_LEETCODE_ENABLED", "true")
	t.Setenv("PARSER_SOURCE_LEETCODE_INTERVAL", "30m")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.Database.DSN != "postgres://localhost:5432/tasks" {
		t.Errorf("expected DSN 'postgres://localhost:5432/tasks', got '%s'", cfg.Database.DSN)
	}
	if cfg.Database.MaxConns != 20 {
		t.Errorf("expected MaxConns 20, got %d", cfg.Database.MaxConns)
	}
	if cfg.Database.MinConns != 5 {
		t.Errorf("expected MinConns 5, got %d", cfg.Database.MinConns)
	}
	if cfg.Database.MaxConnLifetime != time.Hour {
		t.Errorf("expected MaxConnLifetime 1h, got %v", cfg.Database.MaxConnLifetime)
	}
	if cfg.Schedule.CollectCron != "0 */12 * * *" {
		t.Errorf("expected CollectCron '0 */12 * * *', got '%s'", cfg.Schedule.CollectCron)
	}

	active := cfg.Sources.ActiveSources()
	if len(active) != 1 {
		t.Fatalf("expected 1 active source, got %d", len(active))
	}
	if active[0].ID != "leetcode" {
		t.Errorf("expected source 'leetcode', got '%s'", active[0].ID)
	}
	if active[0].Interval != 30*time.Minute {
		t.Errorf("expected Interval 30m, got %v", active[0].Interval)
	}
}

// TestLoad_Defaults проверяет значения по умолчанию.
func TestLoad_Defaults(t *testing.T) {
	t.Setenv("PARSER_DATABASE_DSN", "postgres://localhost:5432/tasks")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.Database.MaxConns != 10 {
		t.Errorf("expected default MaxConns 10, got %d", cfg.Database.MaxConns)
	}
	if cfg.Database.MinConns != 2 {
		t.Errorf("expected default MinConns 2, got %d", cfg.Database.MinConns)
	}
	if cfg.Database.MaxConnLifetime != 30*time.Minute {
		t.Errorf("expected default MaxConnLifetime 30m, got %v", cfg.Database.MaxConnLifetime)
	}
	if cfg.Schedule.CollectCron != "0 */6 * * *" {
		t.Errorf("expected default CollectCron '0 */6 * * *', got '%s'", cfg.Schedule.CollectCron)
	}

	// Все источники по умолчанию выключены
	active := cfg.Sources.ActiveSources()
	if len(active) != 0 {
		t.Errorf("expected 0 active sources by default, got %d", len(active))
	}
}

// TestLoad_MissingDSN терпит отсутствие DSN: подключение к PostgreSQL владеет parker
// (DATABASE_URL), а PARSER_DATABASE_DSN используется лишь отдельным parser-бинарём.
func TestLoad_MissingDSN(t *testing.T) {
	_, err := Load()
	if err != nil {
		t.Fatalf("expected no error without DSN, got: %v", err)
	}
}

// TestLoad_EmptyDSN терпит пустой DSN по той же причине.
func TestLoad_EmptyDSN(t *testing.T) {
	t.Setenv("PARSER_DATABASE_DSN", "")

	_, err := Load()
	if err != nil {
		t.Fatalf("expected no error for empty DSN, got: %v", err)
	}
}

// TestLoad_InvalidMaxConns проверяет ошибку при MaxConns <= 0.
func TestLoad_InvalidMaxConns(t *testing.T) {
	t.Setenv("PARSER_DATABASE_DSN", "postgres://localhost:5432/tasks")
	t.Setenv("PARSER_DATABASE_MAXCONNS", "-1")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for negative MaxConns, got nil")
	}
}

// TestLoad_InvalidMinConns проверяет ошибку при MinConns > MaxConns.
func TestLoad_InvalidMinConns(t *testing.T) {
	t.Setenv("PARSER_DATABASE_DSN", "postgres://localhost:5432/tasks")
	t.Setenv("PARSER_DATABASE_MAXCONNS", "5")
	t.Setenv("PARSER_DATABASE_MINCONNS", "10")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for MinConns > MaxConns, got nil")
	}
}

// TestLoad_InvalidCron проверяет ошибку при пустом cron-выражении.
func TestLoad_InvalidCron(t *testing.T) {
	t.Setenv("PARSER_DATABASE_DSN", "postgres://localhost:5432/tasks")
	t.Setenv("PARSER_SCHEDULE_COLLECTCRON", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for empty cron, got nil")
	}
}

// TestActiveSources_Multiple проверяет несколько включённых источников.
func TestActiveSources_Multiple(t *testing.T) {
	t.Setenv("PARSER_DATABASE_DSN", "postgres://localhost:5432/tasks")
	t.Setenv("PARSER_SOURCE_LEETCODE_ENABLED", "true")
	t.Setenv("PARSER_SOURCE_CODEFORCES_ENABLED", "true")
	t.Setenv("PARSER_SOURCE_TELEGRAM_ANALYTICS_ENABLED", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	active := cfg.Sources.ActiveSources()
	if len(active) != 3 {
		t.Fatalf("expected 3 active sources, got %d", len(active))
	}

	// Проверяем ID источников
	ids := make(map[string]bool)
	for _, s := range active {
		ids[s.ID] = true
	}

	if !ids["leetcode"] {
		t.Error("expected leetcode in active sources")
	}
	if !ids["codeforces"] {
		t.Error("expected codeforces in active sources")
	}
	if !ids["telegram_analytics"] {
		t.Error("expected telegram_analytics in active sources")
	}
}

// TestValidateRuntimeWithoutTelegram проверяет локальный запуск без внешних credentials.
func TestValidateRuntimeWithoutTelegram(t *testing.T) {
	cfg := Config{
		Tasks: TasksConfig{
			URL:        "http://tasks:8080",
			Token:      "tasks-token",
			Timeout:    time.Second,
			MaxRetries: 3,
		},
		Security: SecurityConfig{
			GatewayToken: "gateway-token",
		},
		Worker: WorkerConfig{
			PollInterval: time.Second,
			Lease:        time.Minute,
			Bootstrap:    time.Hour,
			DefaultLimit: 100,
		},
		Telegram: TelegramConfig{
			Enabled: false,
		},
	}

	if err := cfg.ValidateRuntime(); err != nil {
		t.Fatalf("expected disabled Telegram to be valid, got: %v", err)
	}
}

// TestValidateRuntimeRequiresEnabledTelegram проверяет credentials включённого Telegram.
func TestValidateRuntimeRequiresEnabledTelegram(t *testing.T) {
	cfg := Config{
		Tasks: TasksConfig{
			URL:        "http://tasks:8080",
			Token:      "tasks-token",
			Timeout:    time.Second,
			MaxRetries: 3,
		},
		Security: SecurityConfig{
			GatewayToken: "gateway-token",
		},
		Worker: WorkerConfig{
			PollInterval: time.Second,
			Lease:        time.Minute,
			Bootstrap:    time.Hour,
			DefaultLimit: 100,
		},
		Telegram: TelegramConfig{
			Enabled: true,
		},
	}

	if err := cfg.ValidateRuntime(); err == nil {
		t.Fatal("expected enabled Telegram without credentials to fail")
	}
}
