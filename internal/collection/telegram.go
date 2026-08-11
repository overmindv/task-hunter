package collection

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

// TelegramMessage содержит только поля публикации, необходимые worker'у.
type TelegramMessage struct {
	ID          int64
	PublishedAt time.Time
	Text        string
	URL         string
}

// TelegramReader постранично читает сообщения разрешённого канала.
type TelegramReader interface {
	ReadRange(ctx context.Context, channel string, from, to time.Time, afterID int64, limit int) ([]TelegramMessage, error)
}

// MTProtoReader читает Telegram через авторизованную gotd/td session.
type MTProtoReader struct {
	apiID       int
	apiHash     string
	sessionPath string
}

// NewMTProtoReader создаёт MTProto-клиент, не выполняя сетевых запросов.
func NewMTProtoReader(apiID int, apiHash, sessionPath string) *MTProtoReader {
	return &MTProtoReader{
		apiID:       apiID,
		apiHash:     apiHash,
		sessionPath: sessionPath,
	}
}

// ReadRange возвращает не более limit текстовых публикаций диапазона [from,to) в порядке message ID.
func (r *MTProtoReader) ReadRange(ctx context.Context, channel string, from, to time.Time, afterID int64, limit int) ([]TelegramMessage, error) {
	if r.apiID == 0 || r.apiHash == "" || r.sessionPath == "" {
		return nil, fmt.Errorf("telegram MTProto configuration is incomplete")
	}

	client := telegram.NewClient(r.apiID, r.apiHash, telegram.Options{
		SessionStorage: &telegram.FileSessionStorage{Path: r.sessionPath},
	})

	items := make([]TelegramMessage, 0, limit)
	err := client.Run(ctx, func(runCtx context.Context) error {
		status, err := client.Auth().Status(runCtx)
		if err != nil {
			return fmt.Errorf("read Telegram authorization status: %w", err)
		}
		if !status.Authorized {
			return fmt.Errorf("telegram session is not authorized")
		}
		resolved, err := client.API().ContactsResolveUsername(runCtx, &tg.ContactsResolveUsernameRequest{
			Username: strings.TrimPrefix(channel, "@"),
		})
		if err != nil {
			return fmt.Errorf("resolve Telegram channel: %w", err)
		}
		peer, err := resolvedChannel(resolved)
		if err != nil {
			return fmt.Errorf("resolve Telegram channel peer: %w", err)
		}

		offsetID := 0
		for {
			// История Telegram идёт от новых сообщений к старым. Читаем диапазон до нижней
			// границы целиком, чтобы затем взять самые старые limit сообщений и не перескочить
			// checkpoint через ещё не обработанные публикации.
			pageLimit := 100
			response, err := client.API().MessagesGetHistory(runCtx, &tg.MessagesGetHistoryRequest{
				Peer:       peer,
				OffsetID:   offsetID,
				OffsetDate: int(to.Unix()),
				Limit:      pageLimit,
				MinID:      int(afterID),
			})
			if err != nil {
				return fmt.Errorf("read Telegram channel history: %w", err)
			}
			messages := telegramMessages(response)
			if len(messages) == 0 {
				break
			}
			oldestID := 0
			beforeRange := false
			for _, class := range messages {
				message, ok := class.(*tg.Message)
				if !ok {
					continue
				}
				if oldestID == 0 || message.ID < oldestID {
					oldestID = message.ID
				}
				publishedAt := time.Unix(int64(message.Date), 0).UTC()
				if publishedAt.Before(from) {
					beforeRange = true
					continue
				}
				if !publishedAt.Before(to) || int64(message.ID) <= afterID || strings.TrimSpace(message.Message) == "" {
					continue
				}
				items = append(items, TelegramMessage{
					ID:          int64(message.ID),
					PublishedAt: publishedAt,
					Text:        message.Message,
					URL:         fmt.Sprintf("https://t.me/%s/%d", strings.TrimPrefix(channel, "@"), message.ID),
				})
			}
			if len(items) > limit {
				sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
				items = items[:limit]
			}
			if oldestID == 0 || beforeRange || len(messages) < pageLimit {
				break
			}
			offsetID = oldestID
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect Telegram channel %s: %w", channel, err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	if len(items) > limit {
		items = items[:limit]
	}

	return items, nil
}

// resolvedChannel извлекает input peer канала без хранения peer-кэша на диске.
func resolvedChannel(resolved *tg.ContactsResolvedPeer) (*tg.InputPeerChannel, error) {
	for _, chat := range resolved.Chats {
		channel, ok := chat.(*tg.Channel)
		if !ok {
			continue
		}

		return &tg.InputPeerChannel{
			ChannelID:  channel.ID,
			AccessHash: channel.AccessHash,
		}, nil
	}

	return nil, fmt.Errorf("resolved Telegram peer is not a channel")
}

// telegramMessages унифицирует три варианта ответа messages.getHistory.
func telegramMessages(response tg.MessagesMessagesClass) []tg.MessageClass {
	type messagesGetter interface {
		GetMessages() []tg.MessageClass
	}
	getter, ok := response.(messagesGetter)
	if !ok {
		return nil
	}

	return getter.GetMessages()
}
