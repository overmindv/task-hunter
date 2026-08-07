# Архитектура модуля парсинга задач

## 1. Общая структура

```
diploma/
├── cmd/
│   └── parser/              # Точка входа для модуля парсинга
│       └── main.go
├── internal/
│   └── parser/
│       ├── domain/           # Модели данных (сущности)
│       ├── source/           # Адаптеры источников
│       ├── pipeline/         # Пайплайн обработки
│       ├── classifier/      # Классификация задач
│       └── storage/          # Слой работы с БД
├── pkg/
│   └── ...                   # Переиспользуемые утилиты
├── config/
│   └── config.go             # Конфигурация
├── docs/
│   ├── module.md
│   ├── architecture.md
│   └── development/
└── go.mod
```

## 2. Пакет `domain` — модели данных

```go
package domain

// Task — нормализованная задача в едином формате.
type Task struct {
    ID          uuid.UUID
    Title       string
    Description string          // Условие задачи
    Examples    []Example       // Примеры входов/выходов
    Constraints []string        // Ограничения
    Source      Source          // Источник
    SourceURL   string          // Ссылка на оригинал
    SourceHash  string          // Хеш содержимого (для дедупликации)
    Type        TaskType        // Тип задачи
    Difficulty  Difficulty      // Сложность
    Tags        []Tag           // Тематические теги
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// RawTask — сырые данные от адаптера источника до обработки.
type RawTask struct {
    Source      Source
    RawContent  []byte          // Оригинальное содержимое
    SourceURL   string
    RetrievedAt time.Time
}

// Source — идентификатор источника.
type Source struct {
    ID   SourceID
    Name string
    Type SourceType  // telegram | website | manual
}

// Example — пример ввода/вывода.
type Example struct {
    Input  string
    Output string
    Explanation string  // опционально
}

type TaskType int
const (
    TaskTypeAlgorithm TaskType = iota
    TaskTypeDataStructures
    TaskTypeDatabase
    TaskTypeBackend
    TaskTypeInfrastructure
    TaskTypeTesting
    TaskTypeCodeReview
)

type Difficulty int
const (
    DifficultyUnknown Difficulty = iota
    DifficultyEasy
    DifficultyMedium
    DifficultyHard
)

type Tag string

type SourceID string
const (
    SourceTelegramAnalytics  SourceID = "telegram_analytics"
    SourceTelegramML         SourceID = "telegram_ml"
    SourceTelegramAlgorithms SourceID = "telegram_algorithms"
    SourceLeetCode           SourceID = "leetcode"
    SourceCodeforces         SourceID = "codeforces"
    SourceCodeRun            SourceID = "coderun"
    SourceManual             SourceID = "manual"
)
```

## 3. Пакет `source` — адаптеры источников

Каждый источник реализует интерфейс:

```go
package source

// Collector — интерфейс для сбора задач из источника.
type Collector interface {
    // ID возвращает уникальный идентификатор источника.
    ID() domain.SourceID

    // Collect собирает новые задачи с source-а.
    // Возвращает список сырых задач и ошибку.
    // При повторном вызове возвращает только новые/обновлённые задачи.
    Collect(ctx context.Context) ([]domain.RawTask, error)
}
```

### Реализации:

| Структура | Источник | Механизм |
|---|---|---|
| `TelegramCollector` | Telegram-каналы | Telegram Bot API / SDK |
| `LeetCodeCollector` | leetcode.com | HTTP + парсинг HTML |
| `CodeforcesCollector` | codeforces.com | Codeforces API |
| `CodeRunCollector` | coderun.yandex.ru | HTTP + парсинг HTML |
| `ManualCollector` | Ручной ввод | HTTP-эндпоинт (внешний) |

### Фабрика:

```go
// Manager управляет всеми подключёнными коллекторами.
type Manager struct {
    collectors map[domain.SourceID]Collector
}

func NewManager(cfg config.SourceConfig) *Manager

// CollectAll собирает задачи со всех активных источников.
func (m *Manager) CollectAll(ctx context.Context) []domain.RawTask
```

## 4. Пакет `pipeline` — пайплайн обработки

Пайплайн — последовательность процессоров, каждый реализует интерфейс:

```go
package pipeline

// Processor обрабатывает сырую задачу и возвращает нормализованную.
type Processor interface {
    Process(ctx context.Context, raw domain.RawTask) (domain.Task, error)
}
```

### Этапы пайплайна:

```
RawTask → [Extractor] → [Parser] → [Normalizer] → [Classifier] → [Validator] → Task
```

1. **Extractor** — выделяет содержимое из формата источника
   - Для Telegram: извлекает текст сообщения, прикреплённые файлы
   - Для HTML: очищает разметку, оставляет чистый текст
   - Для API: десериализует JSON/XML

2. **Parser** — разбирает текст задачи на поля:
   - Заголовок
   - Условие
   - Примеры (ввод → вывод)
   - Ограничения
   - Теги из источника

3. **Normalizer** — приводит к единому формату:
   - Единый стиль форматирования (Markdown)
   - Приведение примеров к единому виду
   - Удаление лишних пробелов, пустых строк
   - Очистка от мусора

4. **Classifier** — определяет:
   - Тип задачи (алгоритмы, БД, бэкенд ...)
   - Сложность (лёгкая, средняя, сложная)
   - Теги (через анализ ключевых слов / NLP)

5. **Validator** — проверяет:
   - Наличие обязательных полей (условие, хотя бы один пример)
   - Целостность данных
   - Отсутствие дубликатов (по SourceHash)

### Оркестрация:

```go
// Pipeline собирает процессоры и запускает их последовательно.
type Pipeline struct {
    processors []Processor
}

func NewPipeline(extractor, parser, normalizer, classifier, validator Processor) *Pipeline

// Run прогоняет сырую задачу через все процессоры.
func (p *Pipeline) Run(ctx context.Context, raw domain.RawTask) (domain.Task, error)
```

## 5. Пакет `classifier` — классификация

```go
package classifier

type Classifier interface {
    Classify(ctx context.Context, task *domain.Task) error  // заполняет Type, Difficulty, Tags
}
```

Стратегии классификации:
- **RuleBased** (Go) — набор правил по ключевым словам (регулярные выражения), реализуется сразу
- **MLBased** (Python, опционально) — NLP-модель для более точной классификации, подключается как sidecar при необходимости

## 6. Пакет `storage` — хранение

```go
package storage

type Repository interface {
    Save(ctx context.Context, task domain.Task) error
    FindBySourceHash(ctx context.Context, hash string) (*domain.Task, error)  // для дедупликации
    List(ctx context.Context, filter Filter) ([]domain.Task, error)
    GetByID(ctx context.Context, id uuid.UUID) (*domain.Task, error)
}
```

Реализация: **PostgreSQL** через **go-jet** — codegen-библиотека, генерирующая типизированные Go-типы из схемы БД.

**Процесс:**
1. Миграции через **goose** (SQL-файлы в `migrations/`)
2. Накатить миграции на dev-БД
3. Запустить `jet` codegen — сгенерировать модели таблиц в `internal/parser/storage/jet/`
4. Использовать сгенерированные типы в реализации `Repository`

### Схема БД:

```sql
CREATE TABLE tasks (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title       TEXT NOT NULL,
    description TEXT NOT NULL,
    source_id   TEXT NOT NULL,          -- domain.SourceID
    source_url  TEXT NOT NULL,
    source_hash TEXT NOT NULL UNIQUE,   -- для дедупликации
    type        INT NOT NULL,           -- domain.TaskType
    difficulty  INT NOT NULL DEFAULT 0, -- domain.Difficulty
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE examples (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id     UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    input       TEXT NOT NULL,
    output      TEXT NOT NULL,
    explanation TEXT
);

CREATE TABLE task_tags (
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    tag     TEXT NOT NULL,
    PRIMARY KEY (task_id, tag)
);

CREATE INDEX idx_tasks_source_id ON tasks(source_id);
CREATE INDEX idx_tasks_type ON tasks(type);
CREATE INDEX idx_tasks_difficulty ON tasks(difficulty);
CREATE INDEX idx_tasks_source_hash ON tasks(source_hash);
```

## 7. Пакет `config` — конфигурация

```go
type Config struct {
    Sources  []SourceConfig    // какие источники включены и их параметры
    Database DatabaseConfig    // подключение к БД
    Schedule ScheduleConfig    // расписание сбора
}

type SourceConfig struct {
    ID       domain.SourceID
    Enabled  bool
    Interval time.Duration     // как часто собирать
    Params   map[string]string // параметры (токены, URL, ...)
}
```

## 8. Диаграмма потоков (текстовая)

```
          ┌─────────────────────────────────────────────────┐
          │                  Scheduler                       │
          │  запускает CollectAll по расписанию             │
          └─────────────┬───────────────────────────────────┘
                        │
          ┌─────────────▼───────────────────────────────────┐
          │              Source Manager                      │
          │  ┌──────────┐ ┌──────────┐ ┌──────────────────┐ │
          │  │Telegram  │ │LeetCode  │ │Codeforces/CodeRun│ │
          │  │Collector │ │Collector │ │Collector         │ │
          │  └─────┬────┘ └─────┬────┘ └────────┬─────────┘ │
          └────────┼────────────┼───────────────┼───────────┘
                   │            │               │
          ┌────────▼────────────▼───────────────▼───────────┐
          │              Pipeline (последовательно)          │
          │  ┌──────────┐ ┌──────┐ ┌─────────┐ ┌──────────┐ │
          │  │Extractor │→│Parser│→│Normalizer│→│Classifier│ │
          │  └──────────┘ └──────┘ └─────────┘ └─────┬────┘ │
          │                                          │      │
          │  ┌──────────────┐                         │      │
          │  │  Validator   │←────────────────────────┘      │
          │  │(дедупликация)│                                 │
          │  └──────┬───────┘                                 │
          └─────────┼─────────────────────────────────────────┘
                    │
          ┌─────────▼─────────────────────────────────────────┐
          │                  Storage (PostgreSQL)              │
          │  tasks │ examples │ task_tags                     │
          └───────────────────────────────────────────────────┘
```

## 9. Принципы и ограничения

- **Инверсия зависимостей** — domain не зависит от внешних пакетов
- **Расширяемость** — новый источник = новая реализация Collector
- **Отказоустойчивость** — ошибка в одном источнике не роняет другие
- **Идемпотентность** — дедупликация по `source_hash`, повторный запуск безопасен
- **Контекст** — все операции принимают `context.Context` для отмены и таймаутов
