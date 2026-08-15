//go:build !telegram

package collection

import (
	"context"
	"fmt"
	"time"
)

// TelegramMessage содержит только поля публикации, необходимые worker'у.
// Структура дублирует реальную (internal/collection/telegram.go), которая
// компилируется только с тегом //go:build telegram. Здесь живёт заглушка
// для сборок без Telegram: поля совпадают, чтобы Worker компилировался.
type TelegramMessage struct {
	ID          int64
	PublishedAt time.Time
	Text        string
	URL         string
}

// TelegramReader — интерфейс источника Telegram-сообщений. Реализация MTProto
// (internal/collection/telegram.go) доступна только с тегом telegram; в обычной
// сборке интерфейс остаётся, чтобы Worker/Scheduler компилировались без gotd.
type TelegramReader interface {
	ReadRange(ctx context.Context, channel string, from, to time.Time, afterID int64, limit int) ([]TelegramMessage, error)
}

// MTProtoReader — заглушка для сборок без поддержки Telegram.
// При активации telegram (--tags telegram) вместо неё компилируется настоящий
// MTProtoReader из internal/collection/telegram.go.
type MTProtoReader struct{}

// NewMTProtoReader возвращает заглушку: в этой сборке Telegram-сбор не собран.
func NewMTProtoReader(apiID int, apiHash, sessionPath string) *MTProtoReader {
	return &MTProtoReader{}
}

// ReadRange всегда возвращает ошибку: Telegram-поддержка не включена в сборку.
func (r *MTProtoReader) ReadRange(ctx context.Context, channel string, from, to time.Time, afterID int64, limit int) ([]TelegramMessage, error) {
	return nil, fmt.Errorf("telegram collection disabled: build task-hunter with -tags telegram to enable")
}
