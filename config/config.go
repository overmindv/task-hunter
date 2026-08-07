// Package config отвечает за загрузку и хранение конфигурации модуля парсинга.
//
// Конфигурация загружается из переменных окружения через envconfig
// с префиксом PARSER_. Пример:
//
//	PARSER_DATABASE_DSN=postgres://localhost:5432/tasks
//	PARSER_SCHEDULE_COLLECTCRON=0 */6 * * *
//	PARSER_SOURCE_TELEGRAM_ANALYTICS_ENABLED=true
package config

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
)

// Config — корневая конфигурация модуля парсинга.
type Config struct {
	Database DatabaseConfig
	Schedule ScheduleConfig
	Sources  SourcesConfig `envconfig:"SOURCE"`
}

// DatabaseConfig — параметры подключения к PostgreSQL.
type DatabaseConfig struct {
	DSN             string        `envconfig:"DSN" default:""`
	MaxConns        int           `envconfig:"MAXCONNS" default:"10"`
	MinConns        int           `envconfig:"MINCONNS" default:"2"`
	MaxConnLifetime time.Duration `envconfig:"MAXCONNLIFETIME" default:"30m"`
}

// ScheduleConfig — параметры планировщика сбора задач.
type ScheduleConfig struct {
	// CollectCron — cron-выражение для запуска сбора задач.
	// По умолчанию: каждые 6 часов.
	CollectCron string `envconfig:"COLLECTCRON" default:"0 */6 * * *"`
}

// SourcesConfig — конфигурация всех источников.
type SourcesConfig struct {
	TelegramAnalytics  SourceConfig `envconfig:"TELEGRAM_ANALYTICS"`
	TelegramML         SourceConfig `envconfig:"TELEGRAM_ML"`
	TelegramAlgorithms SourceConfig `envconfig:"TELEGRAM_ALGORITHMS"`
	LeetCode           SourceConfig `envconfig:"LEETCODE"`
	Codeforces         SourceConfig `envconfig:"CODEFORCES"`
	CodeRun            SourceConfig `envconfig:"CODERUN"`
}

// SourceConfig — параметры конкретного источника задач.
type SourceConfig struct {
	Enabled  bool          `envconfig:"ENABLED" default:"false"`
	Interval time.Duration `envconfig:"INTERVAL" default:"1h"`
}

// ActiveSources возвращает список включённых источников с их ID и конфигурацией.
func (s SourcesConfig) ActiveSources() []ActiveSource {
	var active []ActiveSource

	add := func(id string, cfg SourceConfig) {
		if !cfg.Enabled {
			return
		}
		active = append(active, ActiveSource{
			ID:       id,
			Interval: cfg.Interval,
		})
	}

	add("telegram_analytics", s.TelegramAnalytics)
	add("telegram_ml", s.TelegramML)
	add("telegram_algorithms", s.TelegramAlgorithms)
	add("leetcode", s.LeetCode)
	add("codeforces", s.Codeforces)
	add("coderun", s.CodeRun)

	return active
}

// ActiveSource — включённый источник с готовыми к использованию параметрами.
type ActiveSource struct {
	ID       string
	Interval time.Duration
	// Params — специфичные для источника параметры (токены, URL, ...).
	// Добавляются по мере необходимости для конкретных коллекторов.
}

// Load загружает конфигурацию из переменных окружения.
// Префикс всех переменных: PARSER_.
// Возвращает ошибку, если обязательные поля не заданы.
func Load() (*Config, error) {
	var cfg Config

	if err := envconfig.Process("PARSER", &cfg); err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

// validate проверяет обязательные поля конфигурации.
func (c *Config) validate() error {
	if c.Database.DSN == "" {
		return fmt.Errorf("PARSER_DATABASE_DSN is required")
	}
	if c.Database.MaxConns <= 0 {
		return fmt.Errorf("PARSER_DATABASE_MAXCONNS must be positive")
	}
	if c.Database.MinConns <= 0 {
		return fmt.Errorf("PARSER_DATABASE_MINCONNS must be positive")
	}
	if c.Database.MinConns > c.Database.MaxConns {
		return fmt.Errorf("PARSER_DATABASE_MINCONNS (%d) must not exceed MAXCONNS (%d)",
			c.Database.MinConns, c.Database.MaxConns)
	}
	if c.Schedule.CollectCron == "" {
		return fmt.Errorf("PARSER_SCHEDULE_COLLECTCRON is required")
	}
	return nil
}
