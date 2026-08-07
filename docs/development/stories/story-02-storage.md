# Story 2: Storage — слой хранения PostgreSQL

**Для чего:** Реализовать интерфейс репозитория и его реализацию на PostgreSQL через **go-jet** (codegen-запросы), обеспечивающую CRUD-операции с задачами.

---

## Task 2.1: Настроить go-jet codegen и реализовать репозиторий

**Для чего:** Настроить go-jet для генерации типизированных моделей БД и реализовать интерфейс `Repository`.

**Подробное описание:**

1. Создать директорию `internal/parser/storage/`
2. Установить `go-jet` и настроить кодогенерацию:
   - Написать конфиг `.jetconfig.yaml` для генерации по dev-БД
   - Указать выходную директорию: `internal/parser/storage/jet/`
   - Выполнить `jet` codegen
3. Определить интерфейс `Repository`:
   - `Save(ctx, task) error` — сохранить задачу вместе с примерами и тегами (в одной транзакции)
   - `FindBySourceHash(ctx, hash) (*domain.Task, error)` — для дедупликации
   - `List(ctx, filter) ([]domain.Task, error)` — с фильтрацией по типу, сложности, источнику, пагинацией
   - `GetByID(ctx, id) (*domain.Task, error)` — получить по ID
   - `Count(ctx, filter) (int, error)` — количество задач по фильтру (для пагинации)
4. Реализовать `PostgresRepository`:
   - Использовать сгенерированные go-jet модели для таблиц
   - `Save` — INSERT (через go-jet) в tasks + examples + task_tags в одной транзакции
   - `FindBySourceHash` — SELECT ... WHERE source_hash = $1
   - `List` — SELECT с динамическим WHERE через go-jet, ORDER BY created_at DESC, LIMIT/OFFSET
   - `GetByID` — SELECT с JOIN на examples и task_tags
5. Структура `Filter`:
   - `Type *domain.TaskType`, `Difficulty *domain.Difficulty`, `SourceID *domain.SourceID`
   - `Tags []domain.Tag`, `Limit, Offset int`
6. Добавить `NewPostgresRepository(db *sql.DB) *PostgresRepository`

**Тесты:**
- **Компонентный:** через testcontainers с PostgreSQL — накат миграций goose, вставка задачи и проверка чтения
- **Компонентный:** поиск по source_hash возвращает существующую задачу
- **Компонентный:** List с фильтрами отдаёт только подходящие задачи
- **Компонентный:** вставка задачи с тегами — теги сохраняются и возвращаются при GetByID

**Сторипоинты:** 3 (добавлена настройка codegen)

---

## Task 2.2: Реализовать дедупликацию через source_hash

**Для чего:** Реализовать логику проверки дубликатов задач перед сохранением, чтобы повторный сбор не создавал копий.

**Подробное описание:**

1. В пакете `storage` (или отдельный файл `dedup.go`) реализовать функцию-обёртку:
   - `SaveIfNotDuplicate(ctx, task, repo) (isNew bool, err error)`
2. Логика:
   1. Вычислить `sourceHash` (если не задан — через `domain.GenerateSourceHash`)
   2. Проверить `FindBySourceHash`
   3. Если найден — вернуть `false, nil` (не сохраняем, задача уже есть)
   4. Если не найден — вызвать `repo.Save`, вернуть `true, nil`
3. Добавить метрику: счётчик пропущенных дубликатов (через `slog` с атрибутом `source_id`)
4. Написать тесты на пограничные случаи:
   - Одинаковый хеш, но разный source → не дубликат (хеш считается вместе с source)
   - null hash → ошибка валидации
   - Конкурентный вызов SaveIfNotDuplicate с одинаковыми задачами

**Тесты:**
- **Unit:** GenerateSourceHash даёт разные хеши для задач из разных источников
- **Компонентный:** первый вызов SaveIfNotDuplicate сохраняет, второй — пропускает
- **Компонентный:** вставка 2 задач с одинаковым хешем (но разными source_id) — обе сохраняются

**Сторипоинты:** 2 (добавлена интеграция с транзакциями go-jet)
