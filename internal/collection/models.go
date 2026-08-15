// Package collection реализует персистентную очередь сбора задач.
package collection

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// JobStatus описывает состояние асинхронного задания.
type JobStatus string

const (
	// JobQueued означает, что задание ожидает worker.
	JobQueued JobStatus = "queued"
	// JobRunning означает, что задание обрабатывается.
	JobRunning JobStatus = "running"
	// JobSucceeded означает полное успешное завершение.
	JobSucceeded JobStatus = "succeeded"
	// JobPartial означает частичный успех источников.
	JobPartial JobStatus = "partial"
	// JobFailed означает отсутствие успешных источников.
	JobFailed JobStatus = "failed"
)

// SourceStatus описывает результат отдельного входа задания.
type SourceStatus string

const (
	// SourceQueued означает, что источник ожидает обработки.
	SourceQueued SourceStatus = "queued"
	// SourceRunning означает, что источник обрабатывается.
	SourceRunning SourceStatus = "running"
	// SourceSucceeded означает успешную обработку источника.
	SourceSucceeded SourceStatus = "succeeded"
	// SourceFailed означает ошибку отдельного источника.
	SourceFailed SourceStatus = "failed"
	// SourceTruncated означает достижение лимита элементов.
	SourceTruncated SourceStatus = "truncated"
)

// Job хранит параметры, прогресс и уведомление о сборе.
type Job struct {
	ID                       uuid.UUID   `json:"id"`
	Trigger                  string      `json:"trigger"`
	RequestedBy              *uuid.UUID  `json:"requested_by,omitempty"`
	IdempotencyKey           uuid.UUID   `json:"idempotency_key"`
	PublishedFrom            *time.Time  `json:"published_from,omitempty"`
	PublishedTo              *time.Time  `json:"published_to,omitempty"`
	MaxItemsPerSource        int         `json:"max_items_per_source"`
	Status                   JobStatus   `json:"status"`
	CollectedTotal           int         `json:"collected_total"`
	ImportedTotal            int         `json:"imported_total"`
	DuplicatesTotal          int         `json:"duplicates_total"`
	InvalidTotal             int         `json:"invalid_total"`
	ErrorCount               int         `json:"error_count"`
	ErrorMessage             string      `json:"error_message,omitempty"`
	NotificationAcknowledged bool        `json:"notification_acknowledged"`
	StartedAt                *time.Time  `json:"started_at,omitempty"`
	FinishedAt               *time.Time  `json:"finished_at,omitempty"`
	CreatedAt                time.Time   `json:"created_at"`
	UpdatedAt                time.Time   `json:"updated_at"`
	Sources                  []JobSource `json:"sources"`
}

// JobSource хранит один Telegram-канал или website URL.
type JobSource struct {
	ID              uuid.UUID    `json:"id"`
	JobID           uuid.UUID    `json:"job_id"`
	Kind            string       `json:"kind"`
	SourceID        string       `json:"source_id"`
	URL             string       `json:"url,omitempty"`
	Status          SourceStatus `json:"status"`
	CollectedTotal  int          `json:"collected_total"`
	ImportedTotal   int          `json:"imported_total"`
	DuplicatesTotal int          `json:"duplicates_total"`
	InvalidTotal    int          `json:"invalid_total"`
	ErrorMessage    string       `json:"error_message,omitempty"`
}

// CreateInput описывает ручной запуск администратора.
type CreateInput struct {
	IdempotencyKey    uuid.UUID  `json:"idempotency_key"`
	TelegramChannels  []string   `json:"telegram_channels"`
	PublishedFrom     *time.Time `json:"published_from"`
	PublishedTo       *time.Time `json:"published_to"`
	WebsiteURLs       []string   `json:"website_urls"`
	MaxItemsPerSource int        `json:"max_items_per_source"`
}

// Checkpoint хранит последний успешно импортированный Telegram message.
type Checkpoint struct {
	SourceID        string
	LastMessageID   int64
	LastPublishedAt time.Time
}

// Candidate представляет нормализованный payload для tasks.
type Candidate struct {
	ExternalID        string     `json:"external_id"`
	SourceID          string     `json:"source_id"`
	SourceName        string     `json:"source_name"`
	SourceURL         string     `json:"source_url"`
	SourceHash        string     `json:"source_hash"`
	SourcePublishedAt *time.Time `json:"source_published_at"`
	RetrievedAt       time.Time  `json:"retrieved_at"`
	CollectionJobID   uuid.UUID  `json:"collection_job_id"`
	Title             string     `json:"title"`
	Statement         string     `json:"statement"`
	Difficulty        string     `json:"difficulty"`
	Tags              []string   `json:"tags"`
	Examples          []Example  `json:"examples"`
	Constraints       []string   `json:"constraints"`
	MessageID         int64      `json:"-"`
}

// Example хранит открытый пример задачи.
type Example struct {
	Input       string `json:"input"`
	Output      string `json:"output"`
	Explanation string `json:"explanation"`
}

// ValidateCreateInput проверяет диапазон, лимиты, allowlist и website URL.
func ValidateCreateInput(input CreateInput, allowedChannels map[string]struct{}, now time.Time) error {
	if input.IdempotencyKey == uuid.Nil {
		return fmt.Errorf("idempotency_key обязателен")
	}
	if len(input.TelegramChannels) == 0 && len(input.WebsiteURLs) == 0 {
		return fmt.Errorf("нужно выбрать Telegram-канал или передать URL")
	}
	if len(input.WebsiteURLs) > 20 {
		return fmt.Errorf("можно передать не более 20 URL")
	}
	if input.MaxItemsPerSource < 1 || input.MaxItemsPerSource > 500 {
		return fmt.Errorf("max_items_per_source должен быть от 1 до 500")
	}
	if len(input.TelegramChannels) > 0 {
		if input.PublishedFrom == nil || input.PublishedTo == nil {
			return fmt.Errorf("для Telegram обязателен диапазон публикации")
		}
		if !input.PublishedFrom.Before(*input.PublishedTo) || input.PublishedTo.After(now.Add(time.Minute)) {
			return fmt.Errorf("некорректный диапазон публикации")
		}
		if input.PublishedTo.Sub(*input.PublishedFrom) > 31*24*time.Hour {
			return fmt.Errorf("диапазон Telegram не должен превышать 31 день")
		}
	}
	for _, channel := range input.TelegramChannels {
		channel = strings.TrimPrefix(strings.TrimSpace(channel), "@")
		if _, ok := allowedChannels[channel]; !ok {
			return fmt.Errorf("telegram-канал %q не входит в allowlist", channel)
		}
	}
	for _, rawURL := range input.WebsiteURLs {
		if _, _, err := NormalizeWebsiteURL(rawURL); err != nil {
			return fmt.Errorf("validate website URL: %w", err)
		}
	}

	return nil
}

// NormalizeWebsiteURL валидирует URL и возвращает источник с канонической ссылкой.
func NormalizeWebsiteURL(rawURL string) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || strings.ToLower(parsed.Scheme) != "https" || parsed.User != nil || parsed.Port() != "" {
		return "", "", fmt.Errorf("некорректный HTTPS URL %q", rawURL)
	}

	host := strings.ToLower(parsed.Hostname())
	host = strings.TrimPrefix(host, "www.")
	parts := splitURLPath(parsed.Path)

	switch host {
	case "codeforces.com":
		if len(parts) == 4 && parts[0] == "problemset" && parts[1] == "problem" && isDigits(parts[2]) && isProblemIndex(parts[3]) {
			return "codeforces", fmt.Sprintf("https://codeforces.com/problemset/problem/%s/%s", parts[2], strings.ToUpper(parts[3])), nil
		}
		if len(parts) == 4 && parts[0] == "contest" && isDigits(parts[1]) && parts[2] == "problem" && isProblemIndex(parts[3]) {
			return "codeforces", fmt.Sprintf("https://codeforces.com/problemset/problem/%s/%s", parts[1], strings.ToUpper(parts[3])), nil
		}
	case "leetcode.com":
		if len(parts) >= 2 && len(parts) <= 3 && parts[0] == "problems" && isSlug(parts[1]) && (len(parts) == 2 || parts[2] == "description") {
			return "leetcode", "https://leetcode.com/problems/" + strings.ToLower(parts[1]), nil
		}
	case "coderun.yandex.ru":
		if len(parts) == 2 && parts[0] == "problem" && isSlug(parts[1]) {
			return "coderun", "https://coderun.yandex.ru/problem/" + strings.ToLower(parts[1]), nil
		}
	}

	return "", "", fmt.Errorf("URL не принадлежит поддерживаемому источнику")
}

// splitURLPath возвращает непустые сегменты пути без завершающего слеша.
func splitURLPath(path string) []string {
	result := make([]string, 0)
	for _, part := range strings.Split(strings.Trim(path, "/"), "/") {
		if part != "" {
			result = append(result, part)
		}
	}

	return result
}

// isSlug разрешает безопасные slug из букв, цифр, дефиса и подчёркивания.
func isSlug(value string) bool {
	if value == "" {
		return false
	}
	for _, symbol := range value {
		if (symbol < 'a' || symbol > 'z') && (symbol < 'A' || symbol > 'Z') && (symbol < '0' || symbol > '9') && symbol != '-' && symbol != '_' {
			return false
		}
	}

	return true
}

// isDigits проверяет числовой идентификатор соревнования.
func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, symbol := range value {
		if symbol < '0' || symbol > '9' {
			return false
		}
	}

	return true
}

// isProblemIndex проверяет короткий буквенно-цифровой индекс задачи.
func isProblemIndex(value string) bool {
	return len(value) <= 8 && isSlug(value)
}
