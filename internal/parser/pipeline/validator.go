package pipeline

import (
	"context"
	"fmt"

	"github.com/overmindv/task-hunter/internal/parser/domain"
)

// Известные (валидные) идентификаторы источников.
var validSourceIDs = map[domain.SourceID]bool{
	domain.SourceTelegramAnalytics:  true,
	domain.SourceTelegramML:         true,
	domain.SourceTelegramAlgorithms: true,
	domain.SourceLeetCode:           true,
	domain.SourceCodeforces:         true,
	domain.SourceCodeRun:            true,
	domain.SourceManual:             true,
}

// MaxTitleLength — максимальная длина заголовка.
const MaxTitleLength = 200

// Validator проверяет целостность задачи после всех этапов обработки.
// Если валидация не пройдена — возвращает ошибку, задача не сохраняется.
type Validator struct{}

// NewValidator создаёт новый Validator.
func NewValidator() *Validator {
	return &Validator{}
}

// Process проверяет задачу.
func (v *Validator) Process(ctx context.Context, _ domain.RawTask, task *domain.Task) error {
	_ = ctx

	// 1. Обязательные поля
	if task.Description == "" {
		return fmt.Errorf("validator: description is required")
	}
	if task.SourceURL == "" {
		return fmt.Errorf("validator: source_url is required")
	}
	if task.Source.ID == "" {
		return fmt.Errorf("validator: source_id is required")
	}

	// 2. Валидный SourceID
	if !validSourceIDs[task.Source.ID] {
		return fmt.Errorf("validator: unknown source_id %q", task.Source.ID)
	}

	// 3. SourceHash (генерируем, если не задан)
	if task.SourceHash == "" {
		task.SourceHash = domain.GenerateSourceHash(task.Source.ID, task.SourceURL, []byte(task.Description))
	}

	// 4. Длина полей
	if len(task.Title) > MaxTitleLength {
		return fmt.Errorf("validator: title too long (%d chars, max %d)",
			len(task.Title), MaxTitleLength)
	}
	if len(task.Description) > MaxFieldLength {
		return fmt.Errorf("validator: description too long (%d chars, max %d)",
			len(task.Description), MaxFieldLength)
	}

	// 5. Валидация через task.Validate() (проверяет domain-правила)
	if err := task.Validate(); err != nil {
		return fmt.Errorf("validator: %w", err)
	}

	return nil
}

// Ensure Validator implements Processor.
var _ Processor = (*Validator)(nil)
