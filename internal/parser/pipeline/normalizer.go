package pipeline

import (
	"context"
	"sort"
	"strings"

	"github.com/overmindv/task-hunter/internal/parser/domain"
	"github.com/samber/lo"
)

// MaxFieldLength — максимальная длина текстовых полей задачи.
// При превышении поле обрезается.
const MaxFieldLength = 10000

// Normalizer приводит задачу к единому формату:
// - очищает пробелы
// - приводит Markdown-заголовки к единому уровню (##)
// - унифицирует язык примеров (Ввод: → Input:)
// - сортирует теги
// - удаляет пустые примеры
// - обрезает слишком длинные поля
type Normalizer struct{}

// NewNormalizer создаёт новый Normalizer.
func NewNormalizer() *Normalizer {
	return &Normalizer{}
}

// Process нормализует задачу.
func (n *Normalizer) Process(ctx context.Context, _ domain.RawTask, task *domain.Task) error {
	_ = ctx

	// 1. Trim пробелов
	task.Title = strings.TrimSpace(task.Title)
	task.Description = strings.TrimSpace(task.Description)
	task.SourceURL = strings.TrimSpace(task.SourceURL)
	task.SourceHash = strings.TrimSpace(task.SourceHash)

	// 2. Нормализация Markdown-заголовков
	task.Description = normalizeMarkdownHeaders(task.Description)

	// 3. Унификация примеров: Ввод: → Input:, Вывод: → Output:
	task.Description = unifyExampleLanguage(task.Description)

	// 4. Очистка примеров
	task.Examples = lo.Filter(task.Examples, func(ex domain.Example, _ int) bool {
		return strings.TrimSpace(ex.Input) != "" && strings.TrimSpace(ex.Output) != ""
	})

	// 5. Сортировка тегов по алфавиту
	if len(task.Tags) > 0 {
		task.Tags = lo.Uniq(task.Tags) // удаляем дубликаты
		sort.Slice(task.Tags, func(i, j int) bool {
			return strings.ToLower(string(task.Tags[i])) < strings.ToLower(string(task.Tags[j]))
		})
	}

	// 6. Обрезка полей
	task.Title = truncateField(task.Title, 200)
	task.Description = truncateField(task.Description, MaxFieldLength)
	task.SourceURL = truncateField(task.SourceURL, 500)

	// 7. Удаление пустых строк в начале/конце Constraints
	var nonEmptyConstraints []string
	for _, c := range task.Constraints {
		c = strings.TrimSpace(c)
		if c != "" {
			nonEmptyConstraints = append(nonEmptyConstraints, c)
		}
	}
	task.Constraints = nonEmptyConstraints

	return nil
}

// normalizeMarkdownHeaders приводит заголовки к единому уровню (##).
func normalizeMarkdownHeaders(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// ### → ##, #### → ## (опускаем до 2 уровня)
		if strings.HasPrefix(trimmed, "###") {
			trimmed = "##" + strings.TrimLeft(trimmed, "# ")
			lines[i] = strings.Repeat(" ", len(line)-len(strings.TrimLeft(line, " "))) + trimmed
		}
	}
	return strings.Join(lines, "\n")
}

// unifyExampleLanguage заменяет Ввод: → Input: и Вывод: → Output:.
func unifyExampleLanguage(text string) string {
	text = strings.ReplaceAll(text, "**Ввод:**", "**Input:**")
	text = strings.ReplaceAll(text, "**Вывод:**", "**Output:**")
	text = strings.ReplaceAll(text, "Ввод:", "**Input:**")
	text = strings.ReplaceAll(text, "Вывод:", "**Output:**")
	return text
}

// truncateField обрезает строку до указанной длины.
func truncateField(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Ensure Normalizer implements Processor.
var _ Processor = (*Normalizer)(nil)
