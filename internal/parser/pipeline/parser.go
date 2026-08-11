package pipeline

import (
	"context"
	"regexp"
	"strings"

	"github.com/overmindv/task-hunter/internal/parser/domain"
)

// Шаблоны для поиска примеров в тексте задачи
var (
	// Пример: Input: ... Output: ... или Ввод: ... Вывод: ...
	exampleRe = regexp.MustCompile(`(?i)(?:input|ввод)\s*:\s*(.+?)\s*(?:output|вывод)\s*:\s*(.+?)(?:\n|$)`)

	// Полная строка примера: **Example 1:** или **Пример 1:**
	exampleHeaderRe = regexp.MustCompile(`(?i)(?:\*\*)?(?:example|пример)\s+\d+\s*(?::|\.)\s*(?:\*\*)?`)

	// Поиск ограничений: Constraints: / Ограничения:
	constraintsHeaderRe = regexp.MustCompile(`(?i)(?:constraints|ограничения)\s*:`)

	// Отдельное ограничение: 1 <= N <= 100 или -10^9 <= nums[i] <= 10^9
	constraintLineRe = regexp.MustCompile(`[\d^]+` + "\\s*(?:<|≤|<=)" + `[\s\S]*?(?:<|≤|<=)` + `[\s\S]+$`)

	// Поиск тегов: Tags: или Теги:
	tagsHeaderRe = regexp.MustCompile(`(?i)(?:tags|теги)\s*:`)
)

// Parser разбирает текст задачи на структурированные поля:
// заголовок, условие, примеры, ограничения, теги.
type Parser struct{}

// NewParser создаёт новый Parser.
func NewParser() *Parser {
	return &Parser{}
}

// Process анализирует текст задачи и заполняет поля task.
func (p *Parser) Process(ctx context.Context, raw domain.RawTask, task *domain.Task) error {
	_ = ctx

	if task.Description == "" {
		return nil // нечего парсить
	}

	text := task.Description

	// 1. Извлекаем заголовок, если ещё не задан
	if task.Title == "" {
		task.Title = extractTitle(text)
	}

	// 2. Извлекаем примеры
	examples := extractExamples(text)
	if len(examples) > 0 {
		task.Examples = examples
	}

	// 3. Извлекаем ограничения
	task.Constraints = extractConstraints(text)

	// 4. Извлекаем теги из текста (если есть метка Tags:)
	tags := extractTags(text)
	if len(tags) > 0 {
		task.Tags = tags
	}

	// 5. Очищаем описание от служебных секций
	task.Description = cleanDescription(text)

	if raw.SourceURL != "" {
		task.SourceURL = raw.SourceURL
	}

	return nil
}

// extractTitle извлекает заголовок из первой строки текста.
func extractTitle(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	lines := strings.SplitN(text, "\n", 2)
	firstLine := strings.TrimSpace(lines[0])

	// Удаляем маркдаун-заголовки
	firstLine = strings.TrimLeft(firstLine, "# ")
	firstLine = strings.TrimSpace(firstLine)

	return firstLine
}

// extractExamples находит примеры в тексте задачи.
func extractExamples(text string) []domain.Example {
	var examples []domain.Example

	// Ищем примеры по шаблону Input/Output
	matches := exampleRe.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		examples = append(examples, domain.Example{
			Input:  strings.TrimSpace(match[1]),
			Output: strings.TrimSpace(match[2]),
		})
	}

	return examples
}

// extractConstraints находит ограничения в тексте задачи.
func extractConstraints(text string) []string {
	var constraints []string

	// Ищем секцию Constraints:
	loc := constraintsHeaderRe.FindStringIndex(text)
	if loc == nil {
		// Пробуем искать строки с неравенствами
		lines := strings.Split(text, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if constraintLineRe.MatchString(line) {
				constraints = append(constraints, line)
			}
		}
		return constraints
	}

	// Берём текст после "Constraints:" до следующей секции или конца
	afterHeader := text[loc[1]:]
	endMarkers := []string{"\n\n##", "\n\n**", "\n\nTags:", "\n\nТеги:"}
	endPos := len(afterHeader)

	for _, marker := range endMarkers {
		if idx := strings.Index(afterHeader, marker); idx >= 0 && idx < endPos {
			endPos = idx
		}
	}

	constraintText := strings.TrimSpace(afterHeader[:endPos])
	lines := strings.Split(constraintText, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			// Убираем маркдаун-разметку списков
			line = strings.TrimLeft(line, "- *")
			line = strings.TrimSpace(line)
			if line != "" {
				constraints = append(constraints, line)
			}
		}
	}

	return constraints
}

// extractTags извлекает теги из текста задачи.
func extractTags(text string) []domain.Tag {
	loc := tagsHeaderRe.FindStringIndex(text)
	if loc == nil {
		return nil
	}

	afterHeader := text[loc[1]:]
	endPos := strings.Index(afterHeader, "\n\n")
	if endPos < 0 {
		endPos = len(afterHeader)
	}

	tagText := afterHeader[:endPos]
	tagText = strings.TrimSpace(tagText)

	// Разделяем по запятым или пробелам
	var tags []domain.Tag
	for _, part := range strings.Split(tagText, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			tags = append(tags, domain.Tag(part))
		}
	}

	return tags
}

// cleanDescription удаляет из описания служебные секции
// (примеры, ограничения, теги), оставляя только условие задачи.
func cleanDescription(text string) string {
	// Удаляем секцию с ограничениями
	if loc := constraintsHeaderRe.FindStringIndex(text); loc != nil {
		text = strings.TrimSpace(text[:loc[0]])
	}

	// Удаляем секцию с тегами
	if loc := tagsHeaderRe.FindStringIndex(text); loc != nil {
		text = strings.TrimSpace(text[:loc[0]])
	}

	// Удаляем заголовок из описания (он теперь в task.Title)
	return text
}
