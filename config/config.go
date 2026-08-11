// Package config отвечает за загрузку и проверку конфигурации task-hunter.
//
// Конфигурация загружается из переменных окружения через envconfig
// с префиксом PARSER_. Пример:
//
//	PARSER_DATABASE_DSN=postgres://localhost:5432/task_hunter
//	PARSER_SCHEDULE_COLLECTCRON=0 */6 * * *
//	PARSER_TELEGRAM_CHANNELS=channel_one,channel_two
package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

// Config — корневая конфигурация модуля парсинга.
type Config struct {
	Database DatabaseConfig
	Schedule ScheduleConfig
	Sources  SourcesConfig `envconfig:"SOURCE"`
	HTTP     HTTPConfig
	TasksIT  TasksITConfig `envconfig:"TASKSIT"`
	Security SecurityConfig
	Worker   WorkerConfig
	Telegram TelegramConfig
}

// HTTPConfig задаёт внутренний HTTP API task-hunter.
type HTTPConfig struct {
	Address      string        `envconfig:"ADDRESS" default:":8080"`
	ReadTimeout  time.Duration `envconfig:"READTIMEOUT" default:"10s"`
	WriteTimeout time.Duration `envconfig:"WRITETIMEOUT" default:"20s"`
}

// TasksITConfig задаёт защищённый внутренний клиент владельца задач.
type TasksITConfig struct {
	URL        string        `envconfig:"URL" default:"http://tasks-it:8080"`
	Token      string        `envconfig:"TOKEN" default:""`
	Timeout    time.Duration `envconfig:"TIMEOUT" default:"15s"`
	MaxRetries int           `envconfig:"MAXRETRIES" default:"3"`
}

// SecurityConfig содержит токен доверенного gateway.
type SecurityConfig struct {
	GatewayToken string `envconfig:"GATEWAYTOKEN" default:""`
}

// WorkerConfig управляет персистентным worker очереди.
type WorkerConfig struct {
	PollInterval time.Duration `envconfig:"POLLINTERVAL" default:"2s"`
	Lease        time.Duration `envconfig:"LEASE" default:"10m"`
	Bootstrap    time.Duration `envconfig:"BOOTSTRAP" default:"24h"`
	DefaultLimit int           `envconfig:"DEFAULTLIMIT" default:"100"`
}

// TelegramConfig задаёт MTProto и allowlist каналов.
type TelegramConfig struct {
	APIID       int      `envconfig:"APIID" default:"0"`
	APIHash     string   `envconfig:"APIHASH" default:""`
	SessionPath string   `envconfig:"SESSIONPATH" default:"/var/lib/task-hunter/telegram.session"`
	Channels    []string `envconfig:"CHANNELS" default:"analytic_postupashki,postupashki_ml,algoses"`
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
	for index := range cfg.Telegram.Channels {
		cfg.Telegram.Channels[index] = strings.TrimPrefix(strings.TrimSpace(cfg.Telegram.Channels[index]), "@")
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

// ValidateRuntime проверяет обязательные секреты и параметры интегрированного сервиса.
func (c *Config) ValidateRuntime() error {
	if c.TasksIT.URL == "" || c.TasksIT.Token == "" {
		return fmt.Errorf("PARSER_TASKSIT_URL and PARSER_TASKSIT_TOKEN are required")
	}
	parsedTasksITURL, err := url.Parse(c.TasksIT.URL)
	if err != nil || parsedTasksITURL.Host == "" || (parsedTasksITURL.Scheme != "http" && parsedTasksITURL.Scheme != "https") {
		return fmt.Errorf("PARSER_TASKSIT_URL must be an absolute HTTP(S) URL")
	}
	if c.TasksIT.Timeout <= 0 || c.TasksIT.MaxRetries < 0 || c.TasksIT.MaxRetries > 10 {
		return fmt.Errorf("tasks-it timeout must be positive and max retries must be between 0 and 10")
	}
	if c.Security.GatewayToken == "" {
		return fmt.Errorf("PARSER_SECURITY_GATEWAYTOKEN is required")
	}
	if c.Telegram.APIID == 0 || c.Telegram.APIHash == "" || c.Telegram.SessionPath == "" || len(c.Telegram.Channels) == 0 {
		return fmt.Errorf("Telegram API credentials, session path and channels are required")
	}
	if c.Worker.DefaultLimit < 1 || c.Worker.DefaultLimit > 500 {
		return fmt.Errorf("PARSER_WORKER_DEFAULTLIMIT must be between 1 and 500")
	}
	if c.Worker.PollInterval <= 0 || c.Worker.Lease <= 0 || c.Worker.Bootstrap <= 0 {
		return fmt.Errorf("worker durations must be positive")
	}
	if c.HTTP.ReadTimeout <= 0 || c.HTTP.WriteTimeout <= 0 {
		return fmt.Errorf("HTTP timeouts must be positive")
	}

	seenChannels := make(map[string]struct{}, len(c.Telegram.Channels))
	for _, rawChannel := range c.Telegram.Channels {
		channel := strings.TrimPrefix(strings.TrimSpace(rawChannel), "@")
		if channel == "" {
			return fmt.Errorf("Telegram channels must not be empty")
		}
		if _, exists := seenChannels[channel]; exists {
			return fmt.Errorf("Telegram channel %q is duplicated", channel)
		}
		seenChannels[channel] = struct{}{}
	}

	return nil
}
