package storage

import (
	"context"
	"time"

	"diploma/internal/parser/domain"
)

// Repository — интерфейс для работы с задачами в БД.
type Repository interface {
	// Save сохраняет задачу вместе с примерами и тегами в одной транзакции.
	Save(ctx context.Context, task domain.Task) error

	// FindBySourceHash ищет задачу по хешу содержимого (для дедупликации).
	FindBySourceHash(ctx context.Context, hash string) (*domain.Task, error)

	// List возвращает список задач с фильтрацией и пагинацией.
	List(ctx context.Context, filter Filter) ([]domain.Task, error)

	// GetByID возвращает задачу по её UUID.
	GetByID(ctx context.Context, id string) (*domain.Task, error)

	// Count возвращает количество задач по фильтру (для пагинации).
	Count(ctx context.Context, filter Filter) (int, error)
}

// Filter — параметры фильтрации и пагинации при запросе списка задач.
type Filter struct {
	Type       *domain.TaskType
	Difficulty *domain.Difficulty
	SourceID   *domain.SourceID
	Tags       []domain.Tag
	Limit      int
	Offset     int
}

// SaveIfNotDuplicate сохраняет задачу, если её ещё нет в БД.
// Возвращает true, если задача сохранена (новая), false — если уже существовала.
func SaveIfNotDuplicate(ctx context.Context, task domain.Task, repo Repository) (bool, error) {
	// Генерируем хеш, если не задан
	if task.SourceHash == "" {
		task.SourceHash = domain.GenerateSourceHashFromTask(&task)
	}

	existing, err := repo.FindBySourceHash(ctx, task.SourceHash)
	if err != nil {
		return false, err
	}
	if existing != nil {
		return false, nil // дубликат, не сохраняем
	}

	if err := repo.Save(ctx, task); err != nil {
		return false, err
	}
	return true, nil
}

// defaultLimit возвращает лимит пагинации по умолчанию.
const defaultLimit = 20

// defaultMaxLimit — максимальный лимит, который можно запросить.
const defaultMaxLimit = 100

// EnsureFilter заполняет значения по умолчанию для фильтра.
func EnsureFilter(f Filter) Filter {
	if f.Limit <= 0 {
		f.Limit = defaultLimit
	}
	if f.Limit > defaultMaxLimit {
		f.Limit = defaultMaxLimit
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	return f
}

// TaskNotFoundError — ошибка, если задача не найдена.
type TaskNotFoundError struct {
	ID string
}

func (e *TaskNotFoundError) Error() string {
	return "task not found: " + e.ID
}

// DuplicateTaskError — ошибка, если задача с таким хешем уже существует.
type DuplicateTaskError struct {
	Hash string
}

func (e *DuplicateTaskError) Error() string {
	return "duplicate task with source_hash: " + e.Hash
}

// TimeNow — функция для получения текущего времени.
// Переопределяется в тестах.
var TimeNow = time.Now
