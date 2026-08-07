package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/google/uuid"

	"diploma/internal/parser/domain"

	// Сгенерированные go-jet модели и таблицы
	"diploma/internal/parser/storage/jet/diploma_test/public/model"
	"diploma/internal/parser/storage/jet/diploma_test/public/table"
)

// PostgresRepository — реализация Repository на PostgreSQL через go-jet.
type PostgresRepository struct {
	db *sql.DB
}

// NewPostgresRepository создаёт новый репозиторий.
func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// Save сохраняет задачу с примерами и тегами в одной транзакции.
func (r *PostgresRepository) Save(ctx context.Context, task domain.Task) error {
	taskID := uuid.MustParse(task.ID)
	now := TimeNow()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // если commit прошёл, rollback — noop

	// Вставляем задачу
	taskModel := model.Tasks{
		ID:          taskID,
		Title:       task.Title,
		Description: task.Description,
		SourceID:    string(task.Source.ID),
		SourceURL:   task.SourceURL,
		SourceHash:  task.SourceHash,
		Type:        int32(task.Type),
		Difficulty:  int32(task.Difficulty),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	insertTask := table.Tasks.INSERT(
		table.Tasks.ID,
		table.Tasks.Title,
		table.Tasks.Description,
		table.Tasks.SourceID,
		table.Tasks.SourceURL,
		table.Tasks.SourceHash,
		table.Tasks.Type,
		table.Tasks.Difficulty,
		table.Tasks.CreatedAt,
		table.Tasks.UpdatedAt,
	).MODEL(taskModel)

	if _, err := insertTask.Exec(tx); err != nil {
		return fmt.Errorf("insert task: %w", err)
	}

	// Вставляем примеры
	if len(task.Examples) > 0 {
		exampleModels := make([]model.Examples, len(task.Examples))
		for i, ex := range task.Examples {
			exampleModels[i] = model.Examples{
				ID:          uuid.New(),
				TaskID:      taskID,
				Input:       ex.Input,
				Output:      ex.Output,
				Explanation: nullableString(ex.Explanation),
			}
		}

		insertExamples := table.Examples.INSERT(
			table.Examples.ID,
			table.Examples.TaskID,
			table.Examples.Input,
			table.Examples.Output,
			table.Examples.Explanation,
		).MODELS(exampleModels)

		if _, err := insertExamples.Exec(tx); err != nil {
			return fmt.Errorf("insert examples: %w", err)
		}
	}

	// Вставляем теги
	if len(task.Tags) > 0 {
		tagModels := make([]model.TaskTags, len(task.Tags))
		for i, tag := range task.Tags {
			tagModels[i] = model.TaskTags{
				TaskID: taskID,
				Tag:    string(tag),
			}
		}

		insertTags := table.TaskTags.INSERT(
			table.TaskTags.TaskID,
			table.TaskTags.Tag,
		).MODELS(tagModels)

		if _, err := insertTags.Exec(tx); err != nil {
			return fmt.Errorf("insert tags: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}

// FindBySourceHash ищет задачу по хешу. Возвращает nil, если не найдена.
func (r *PostgresRepository) FindBySourceHash(ctx context.Context, hash string) (*domain.Task, error) {
	stmt := table.Tasks.SELECT(table.Tasks.AllColumns).
		WHERE(table.Tasks.SourceHash.EQ(postgres.String(hash))).
		LIMIT(1)

	var dest []model.Tasks
	if err := stmt.QueryContext(ctx, r.db, &dest); err != nil {
		return nil, fmt.Errorf("find by source_hash: %w", err)
	}

	if len(dest) == 0 {
		return nil, nil
	}

	return taskFromModel(&dest[0]), nil
}

// List возвращает задачи с фильтрацией и пагинацией.
func (r *PostgresRepository) List(ctx context.Context, filter Filter) ([]domain.Task, error) {
	filter = EnsureFilter(filter)

	stmt := table.Tasks.SELECT(table.Tasks.AllColumns).
		ORDER_BY(table.Tasks.CreatedAt.DESC()).
		LIMIT(int64(filter.Limit)).
		OFFSET(int64(filter.Offset))

	// Добавляем WHERE только если есть условия фильтрации
	if conditions := buildConditions(filter); len(conditions) > 0 {
		stmt = stmt.WHERE(postgres.AND(conditions...))
	}

	var dest []model.Tasks
	if err := stmt.QueryContext(ctx, r.db, &dest); err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}

	tasks := make([]domain.Task, len(dest))
	for i, m := range dest {
		tasks[i] = *taskFromModel(&m)
	}

	return tasks, nil
}

// GetByID возвращает задачу по ID вместе с примерами и тегами.
func (r *PostgresRepository) GetByID(ctx context.Context, id string) (*domain.Task, error) {
	taskUUID, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid task id: %w", err)
	}

	// Получаем задачу
	stmt := table.Tasks.SELECT(table.Tasks.AllColumns).
		WHERE(table.Tasks.ID.EQ(postgres.UUID(taskUUID)))

	var tasks []model.Tasks
	if err := stmt.QueryContext(ctx, r.db, &tasks); err != nil {
		return nil, fmt.Errorf("get task by id: %w", err)
	}
	if len(tasks) == 0 {
		return nil, &TaskNotFoundError{ID: id}
	}

	task := taskFromModel(&tasks[0])

	// Получаем примеры
	task.Examples = r.loadExamples(ctx, taskUUID)

	// Получаем теги
	task.Tags = r.loadTags(ctx, taskUUID)

	return task, nil
}

// Count возвращает количество задач по фильтру.
func (r *PostgresRepository) Count(ctx context.Context, filter Filter) (int, error) {
	stmt := table.Tasks.SELECT(postgres.COUNT(postgres.STAR))

	// Добавляем WHERE только если есть условия фильтрации
	if conditions := buildConditions(filter); len(conditions) > 0 {
		stmt = stmt.WHERE(postgres.AND(conditions...))
	}

	var dest []struct {
		Count int64
	}
	if err := stmt.QueryContext(ctx, r.db, &dest); err != nil {
		return 0, fmt.Errorf("count tasks: %w", err)
	}

	if len(dest) == 0 {
		return 0, nil
	}
	return int(dest[0].Count), nil
}

// --- helpers ---

// loadExamples загружает примеры для задачи.
func (r *PostgresRepository) loadExamples(ctx context.Context, taskID uuid.UUID) []domain.Example {
	stmt := table.Examples.SELECT(table.Examples.AllColumns).
		WHERE(table.Examples.TaskID.EQ(postgres.UUID(taskID))).
		ORDER_BY(table.Examples.ID.ASC())

	var dest []model.Examples
	if err := stmt.QueryContext(ctx, r.db, &dest); err != nil {
		return nil
	}

	examples := make([]domain.Example, len(dest))
	for i, m := range dest {
		examples[i] = domain.Example{
			Input:       m.Input,
			Output:      m.Output,
			Explanation: fromNullableString(m.Explanation),
		}
	}
	return examples
}

// loadTags загружает теги для задачи.
func (r *PostgresRepository) loadTags(ctx context.Context, taskID uuid.UUID) []domain.Tag {
	stmt := table.TaskTags.SELECT(table.TaskTags.AllColumns).
		WHERE(table.TaskTags.TaskID.EQ(postgres.UUID(taskID))).
		ORDER_BY(table.TaskTags.Tag.ASC())

	var dest []model.TaskTags
	if err := stmt.QueryContext(ctx, r.db, &dest); err != nil {
		return nil
	}

	tags := make([]domain.Tag, len(dest))
	for i, m := range dest {
		tags[i] = domain.Tag(m.Tag)
	}
	return tags
}

// taskFromModel преобразует model.Tasks в domain.Task.
func taskFromModel(m *model.Tasks) *domain.Task {
	return &domain.Task{
		ID:          m.ID.String(),
		Title:       m.Title,
		Description: m.Description,
		Source: domain.Source{
			ID: domain.SourceID(m.SourceID),
		},
		SourceURL:  m.SourceURL,
		SourceHash: m.SourceHash,
		Type:       domain.TaskType(m.Type),
		Difficulty: domain.Difficulty(m.Difficulty),
		CreatedAt:  m.CreatedAt,
		UpdatedAt:  m.UpdatedAt,
	}
}

// buildConditions строит список условий WHERE из фильтра.
func buildConditions(filter Filter) []postgres.BoolExpression {
	var conditions []postgres.BoolExpression

	if filter.Type != nil {
		conditions = append(conditions,
			table.Tasks.Type.EQ(postgres.Int32(int32(*filter.Type))))
	}
	if filter.Difficulty != nil {
		conditions = append(conditions,
			table.Tasks.Difficulty.EQ(postgres.Int32(int32(*filter.Difficulty))))
	}
	if filter.SourceID != nil {
		conditions = append(conditions,
			table.Tasks.SourceID.EQ(postgres.String(string(*filter.SourceID))))
	}

	return conditions
}

// nullableString возвращает *string для опционального поля.
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// fromNullableString преобразует *string в string.
func fromNullableString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Compile-time check: PostgresRepository реализует Repository.
var _ Repository = (*PostgresRepository)(nil)
