package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/overmindv/task-hunter/internal/parser/domain"
)

// Extractor извлекает чистый текст из сырых данных источника.
// В зависимости от типа источника применяет разные стратегии:
//   - Website: очищает HTML через goquery, оставляет читаемый текст
//   - Telegram: текст уже чистый, извлекает
//   - API: пытается распарсить JSON
type Extractor struct{}

// NewExtractor создаёт новый Extractor.
func NewExtractor() *Extractor {
	return &Extractor{}
}

// Process извлекает текст из сырых данных в зависимости от типа источника.
func (e *Extractor) Process(ctx context.Context, raw domain.RawTask, task *domain.Task) error {
	_ = ctx

	// Заполняем базовые поля из RawTask
	task.Source = raw.Source
	task.SourceURL = raw.SourceURL
	task.CreatedAt = raw.RetrievedAt
	task.UpdatedAt = raw.RetrievedAt
	task.SourceHash = domain.GenerateSourceHash(raw.Source.ID, raw.SourceURL, raw.RawContent)

	content := string(raw.RawContent)
	if content == "" {
		// Если контента нет — title всё равно извлекаем из источника
		task.Title = raw.Source.Name
		return nil
	}

	switch raw.Source.Type {
	case domain.SourceTypeWebsite:
		extracted := extractFromHTML(content)
		task.Title = extracted.title
		task.Description = extracted.text

	case domain.SourceTypeAPI:
		extracted := extractFromJSON(content)
		task.Title = extracted.title
		task.Description = extracted.text

	case domain.SourceTypeTelegram, domain.SourceTypeManual:
		// Текст уже чистый, просто первая строка — заголовок
		title, desc := extractTitleFromPlain(content)
		task.Title = title
		task.Description = desc

	default:
		task.Title = raw.Source.Name
		task.Description = content
	}

	return nil
}

// extractedContent — результат извлечения из источника.
type extractedContent struct {
	title string
	text  string
}

// extractFromHTML очищает HTML и извлекает заголовок и текст.
func extractFromHTML(htmlContent string) extractedContent {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		// Если HTML невалидный — пробуем как plain text
		title, desc := extractTitleFromPlain(htmlContent)
		return extractedContent{title: title, text: desc}
	}

	// Удаляем скрипты и стили
	doc.Find("script, style, nav, footer, header, aside").Remove()

	// Пробуем найти заголовок
	title := ""
	doc.Find("h1, h2, h3, .title, .question-title, .problem-statement>.title").Each(func(_ int, s *goquery.Selection) {
		if title == "" {
			title = strings.TrimSpace(s.Text())
		}
	})

	// Извлекаем основной текст
	text := ""
	// Выбираем один наиболее специфичный контейнер. Объединение с body дублирует
	// условие и может захватить навигацию современного SPA.
	for _, selector := range []string{
		"#tab-description-panel",
		".problem-statement",
		".content-wrapper",
		".question-content",
		".statement",
	} {
		selection := doc.Find(selector).First()
		if selection.Length() == 0 {
			continue
		}
		text = extractTextFromNode(selection)
		if text != "" {
			break
		}
	}
	if text == "" {
		// Fallback: весь текст body
		text = strings.TrimSpace(doc.Find("body").Text())
	}

	return extractedContent{title: title, text: text}
}

// extractTextFromNode извлекает читаемый текст из HTML-узла.
func extractTextFromNode(s *goquery.Selection) string {
	var parts []string

	s.Contents().Each(func(_ int, child *goquery.Selection) {
		switch goquery.NodeName(child) {
		case "p", "div", "li", "h1", "h2", "h3", "h4", "h5", "h6":
			text := strings.TrimSpace(child.Text())
			if text != "" {
				parts = append(parts, text)
			}
		case "br":
			parts = append(parts, "\n")
		case "pre", "code":
			text := strings.TrimSpace(child.Text())
			if text != "" {
				parts = append(parts, "`"+text+"`")
			}
		default:
			text := strings.TrimSpace(child.Text())
			if text != "" {
				parts = append(parts, text)
			}
		}
	})

	return strings.Join(parts, "\n")
}

// extractFromJSON пытается извлечь структурированные данные из JSON.
func extractFromJSON(jsonContent string) extractedContent {
	// Простой парсер: если JSON — берём поля "name" и "content"
	// Полноценный JSON-парсинг будет добавлен при работе с API
	title := extractJSONStringField(jsonContent, "name")
	if title == "" {
		title = extractJSONStringField(jsonContent, "title")
	}

	text := extractJSONStringField(jsonContent, "content")
	if text == "" {
		text = extractJSONStringField(jsonContent, "description")
	}

	return extractedContent{title: title, text: text}
}

// extractJSONStringField извлекает значение строкового поля из JSON.
func extractJSONStringField(json, field string) string {
	// Простейший парсер: ищем "field": "value"
	// Для полноценного парсинга использовать encoding/json
	marker := fmt.Sprintf(`"%s"`, field)
	idx := strings.Index(json, marker)
	if idx < 0 {
		return ""
	}

	// Ищем двоеточие после поля
	afterField := json[idx+len(marker):]
	colonIdx := strings.Index(afterField, ":")
	if colonIdx < 0 {
		return ""
	}

	valueStart := afterField[colonIdx+1:]
	valueStart = strings.TrimSpace(valueStart)

	// Пропускаем кавычку
	if strings.HasPrefix(valueStart, `"`) {
		valueStart = valueStart[1:]
		endIdx := strings.Index(valueStart, `"`)
		if endIdx >= 0 {
			return valueStart[:endIdx]
		}
	}

	return ""
}

// extractTitleFromPlain извлекает заголовок из plain text (первая строка).
func extractTitleFromPlain(text string) (title string, description string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ""
	}

	lines := strings.SplitN(text, "\n", 2)
	firstLine := strings.TrimSpace(lines[0])

	// Если строка начинается с ## или # — это заголовок markdown
	firstLine = strings.TrimLeft(firstLine, "# ")
	firstLine = strings.TrimSpace(firstLine)

	if len(lines) == 1 {
		return "", text // без заголовка, весь текст — описание
	}

	desc := strings.TrimSpace(lines[1])
	return firstLine, desc
}

// Ensure Extractor implements Processor at compile time.
var _ Processor = (*Extractor)(nil)
