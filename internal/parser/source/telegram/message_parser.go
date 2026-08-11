package telegram

import (
	"fmt"
	"strings"

	"github.com/overmindv/task-hunter/internal/parser/domain"
)

// ParseMessage преобразует сообщение Telegram в RawTask.
// channelUsername — username канала (без @), sourceID — идентификатор источника.
func ParseMessage(msg MessageInfo, channelUsername string, sourceID domain.SourceID) domain.RawTask {
	text := msg.Text

	// Если есть медиа-файл с текстом — добавляем его содержимое
	if msg.HasMedia && len(msg.MediaData) > 0 && msg.MediaType == "text" {
		if text != "" {
			text += "\n\n" + string(msg.MediaData)
		} else {
			text = string(msg.MediaData)
		}
	}

	rawTask := domain.RawTask{
		Source: domain.Source{
			ID:   sourceID,
			Name: channelUsername,
			Type: domain.SourceTypeTelegram,
		},
		RawContent:  []byte(text),
		SourceURL:   SourceURL(channelUsername, msg.ID),
		RetrievedAt: msg.Timestamp,
	}

	return rawTask
}

// SourceURL генерирует ссылку на пост в Telegram.
// Формат: https://t.me/{username}/{messageID}
func SourceURL(channelUsername string, messageID int) string {
	return fmt.Sprintf("https://t.me/%s/%d", channelUsername, messageID)
}

// HasCodeBlocks проверяет, содержит ли текст блоки кода (``` или `code`).
func HasCodeBlocks(text string) bool {
	return strings.Contains(text, "```") || strings.Contains(text, "`")
}

// TextLengthWithoutCode возвращает длину текста без блоков кода.
func TextLengthWithoutCode(text string) int {
	cleaned := text

	// Удаляем многострочные блоки кода
	for {
		start := strings.Index(cleaned, "```")
		if start < 0 {
			break
		}
		end := strings.Index(cleaned[start+3:], "```")
		if end < 0 {
			break
		}
		cleaned = cleaned[:start] + cleaned[start+3+end+3:]
	}

	// Удаляем инлайн-код
	for {
		start := strings.Index(cleaned, "`")
		if start < 0 {
			break
		}
		end := strings.Index(cleaned[start+1:], "`")
		if end < 0 {
			break
		}
		cleaned = cleaned[:start] + cleaned[start+1+end+1:]
	}

	return len(strings.TrimSpace(cleaned))
}

// HasMediaFile проверяет, является ли вложение текстовым файлом (код).
func HasMediaFile(msg MessageInfo) bool {
	return msg.HasMedia && msg.MediaType == "text" && len(msg.MediaData) > 0
}

// IsImageAttachment проверяет, является ли вложение изображением.
func IsImageAttachment(msg MessageInfo) bool {
	return msg.HasMedia && msg.MediaType == "photo"
}
