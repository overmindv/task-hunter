// Package telegram реализует сбор задач из Telegram-каналов через MTProto.
//
// Использует библиотеку gotd/td (https://github.com/gotd/td) для работы
// с MTProto-протоколом Telegram.
//
// Для работы требуется:
//   - API ID и API Hash (регистрация на https://my.telegram.org/apps)
//   - Номер телефона для аутентификации
//   - Файл сессии для повторного подключения без кода
//
// Первый запуск: передать номер телефона → ввести код из Telegram → сессия сохранится.
// Последующие: сессия восстанавливается из файла.
package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/overmindv/task-hunter/internal/parser/domain"
	"github.com/overmindv/task-hunter/internal/parser/source"
)

// Message — абстракция сообщения Telegram для тестирования.
// Реальный тип — tg.Message из gotd/td, но для тестов используем этот.

// tgClient — интерфейс MTProto-клиента, чтобы мокать в тестах.
type tgClient interface {
	// Connect устанавливает соединение с Telegram.
	Connect(ctx context.Context) error

	// Disconnect закрывает соединение.
	Disconnect(ctx context.Context) error

	// ResolveChannel получает информацию о канале по username.
	ResolveChannel(ctx context.Context, username string) (channelID int64, accessHash int64, err error)

	// GetMessages получает сообщения из канала после указанного ID.
	// Если lastID == 0 — получает последние N сообщений.
	GetMessages(ctx context.Context, channelID, accessHash int64, lastID int) ([]MessageInfo, error)
}

// MessageInfo — информация о сообщении из Telegram.
type MessageInfo struct {
	ID        int
	Text      string
	Timestamp time.Time
	HasMedia  bool
	MediaType string // "photo", "document", "text"
	MediaData []byte // Содержимое медиа (если текст/код)
}

// RateLimitInfo — информация о лимитах запросов.
type RateLimitInfo struct {
	MinInterval time.Duration // Минимальный интервал между запросами
}

// Collector собирает задачи из Telegram-канала.
// Реализует интерфейс source.Collector.
type Collector struct {
	id         domain.SourceID
	client     tgClient
	channels   []string // usernames каналов (без @)
	channelIDs map[string]struct {
		ID         int64
		AccessHash int64
	}
	lastMessageIDs map[string]int // channelID → последний обработанный message_id
	minInterval    time.Duration  // минимальный интервал между запросами
	rateLimiter    *time.Ticker
	mu             sync.Mutex
}

// DefaultMinInterval — интервал между запросами к Telegram по умолчанию.
const DefaultMinInterval = time.Second

// NewCollector создаёт нового Collector для Telegram.
//
// Параметры конфигурации (cfg.Params):
//   - api_id — API ID приложения (обязательный)
//   - api_hash — API Hash приложения (обязательный)
//   - phone — номер телефона для аутентификации
//   - session_file — путь к файлу сессии (опционально)
//   - channels — список каналов через запятую (обязательный)
func NewCollector(id domain.SourceID, client tgClient, channels []string) (*Collector, error) {
	if len(channels) == 0 {
		return nil, fmt.Errorf("telegram: at least one channel is required")
	}

	return &Collector{
		id:       id,
		client:   client,
		channels: channels,
		channelIDs: make(map[string]struct {
			ID         int64
			AccessHash int64
		}),
		lastMessageIDs: make(map[string]int),
		minInterval:    DefaultMinInterval,
	}, nil
}

// WithMinInterval устанавливает минимальный интервал между запросами к Telegram.
// Используется в тестах для ускорения.
func (c *Collector) WithMinInterval(d time.Duration) *Collector {
	c.minInterval = d
	if c.rateLimiter != nil {
		c.rateLimiter.Stop()
	}
	c.rateLimiter = time.NewTicker(d)
	return c
}

// ID возвращает идентификатор источника.
func (c *Collector) ID() domain.SourceID {
	return c.id
}

// Connect устанавливает соединение с Telegram и разрешает каналы.
func (c *Collector) Connect(ctx context.Context) error {
	slog.Info("telegram: connecting", "channels", c.channels)

	if c.rateLimiter == nil {
		c.rateLimiter = time.NewTicker(c.minInterval)
	}

	if err := c.client.Connect(ctx); err != nil {
		return fmt.Errorf("telegram: connect: %w", err)
	}

	// Разрешаем каналы
	for _, ch := range c.channels {
		<-c.rateLimiter.C
		channelID, accessHash, err := c.client.ResolveChannel(ctx, ch)
		if err != nil {
			slog.Warn("telegram: failed to resolve channel, skipping",
				"channel", ch, "error", err)
			continue
		}

		c.mu.Lock()
		c.channelIDs[ch] = struct {
			ID         int64
			AccessHash int64
		}{
			ID: channelID, AccessHash: accessHash,
		}
		c.mu.Unlock()

		slog.Info("telegram: resolved channel", "channel", ch, "id", channelID)
	}

	if len(c.channelIDs) == 0 {
		return fmt.Errorf("telegram: no channels could be resolved")
	}

	return nil
}

// Collect собирает новые сообщения из каналов.
func (c *Collector) Collect(ctx context.Context) ([]domain.RawTask, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return nil, fmt.Errorf("telegram: client is nil, call Connect first")
	}

	var allTasks []domain.RawTask

	for ch, info := range c.channelIDs {
		lastID := c.lastMessageIDs[ch]

		<-c.rateLimiter.C

		messages, err := c.client.GetMessages(ctx, info.ID, info.AccessHash, lastID)
		if err != nil {
			slog.Warn("telegram: failed to get messages",
				"channel", ch, "error", err)
			continue
		}

		for _, msg := range messages {
			// Каждое сообщение → RawTask
			rawTask := domain.RawTask{
				Source: domain.Source{
					ID:   c.id,
					Name: ch,
					Type: domain.SourceTypeTelegram,
				},
				RawContent:  []byte(msg.Text),
				SourceURL:   fmt.Sprintf("https://t.me/%s/%d", ch, msg.ID),
				RetrievedAt: msg.Timestamp,
			}
			allTasks = append(allTasks, rawTask)

			// Обновляем lastMessageID
			if msg.ID > c.lastMessageIDs[ch] {
				c.lastMessageIDs[ch] = msg.ID
			}
		}

		if len(messages) > 0 {
			slog.Debug("telegram: collected messages",
				"channel", ch,
				"count", len(messages),
				"last_id", c.lastMessageIDs[ch],
			)
		}
	}

	return allTasks, nil
}

// Close закрывает соединение и освобождает ресурсы.
func (c *Collector) Close() error {
	if c.rateLimiter != nil {
		c.rateLimiter.Stop()
	}

	if err := c.client.Disconnect(context.Background()); err != nil {
		return fmt.Errorf("telegram: disconnect: %w", err)
	}
	return nil
}

// Ensure Collector implements source.Collector at compile time.
var _ source.Collector = (*Collector)(nil)
