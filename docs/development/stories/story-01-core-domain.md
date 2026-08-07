# Story 1: Core Domain — модели данных, конфиг, миграции

**Для чего:** Заложить фундамент модуля: определить сущности, конфигурацию, схему БД и миграции, от которых будут зависеть все остальные компоненты.

---

## Task 1.1: Определить domain-модели задач

**Для чего:** Создать пакет `internal/parser/domain` с базовыми типами данных, которые будут использоваться во всём модуле.

**Подробное описание:**

1. Создать директорию `internal/parser/domain/`
2. Определить структуру `Task` с полями: `ID uuid.UUID`, `Title string`, `Description string`, `Examples []Example`, `Constraints []string`, `Source Source`, `SourceURL string`, `SourceHash string`, `Type TaskType`, `Difficulty Difficulty`, `Tags []Tag`, `CreatedAt`, `UpdatedAt`
3. Определить структуру `RawTask` для сырых данных от источников
4. Определить структуру `Source` с идентификатором, именем и типом
5. Определить структуру `Example` (Input, Output, Explanation)
6. Определить типы-перечисления: `TaskType`, `Difficulty`, `SourceType`, `SourceID` с константами
7. Определить тип `Tag` как `string`
8. Написать хелперы: `NewTask()`, `GenerateSourceHash()`
9. Реализовать метод `Validate() error` на `Task`, проверяющий обязательные поля
10. Все поля моделей документировать комментариями (godoc)

**Тесты:**
- **Unit:** создание Task с разными комбинациями полей
- **Unit:** валидация — корректная задача проходит, пустое описание — ошибка
- **Unit:** GenerateSourceHash выдаёт одинаковый хеш для одинаковых данных
- **Unit:** SourceID и TaskType константы не пересекаются

**Сторипоинты:** 2

---

## Task 1.2: Настроить конфигурацию модуля

**Для чего:** Создать пакет конфигурации, через который будут управляться источники, БД и расписание.

**Подробное описание:**

1. Создать директорию `config/` в корне проекта
2. Определить структуру `Config` (или `ParserConfig`)
3. В `Config` добавить:
   - `Sources []SourceConfig` — список источников
   - `Database DatabaseConfig` — DSN, пул соединений
   - `Schedule ScheduleConfig` — cron-выражения для сбора
4. `SourceConfig`: `ID`, `Enabled`, `Interval`, `Params map[string]string`
5. `DatabaseConfig`: `DSN`, `MaxConns`, `MinConns`, `MaxConnLifetime`
6. `ScheduleConfig`: `CollectCron string`
7. Реализовать загрузку конфига из переменных окружения через `envconfig`
8. Добавить префикс `PARSER_` для всех переменных (пример: `PARSER_DATABASE_DSN`)
9. Написать `config.go` с функцией `Load() (*Config, error)`
10. Добавить разумные значения по умолчанию (defaults)

**Тесты:**
- **Unit:** загрузка конфига из переменных окружения
- **Unit:** значения по умолчанию для незаданных полей
- **Unit:** ошибка при отсутствии обязательных полей (например, DSN)

**Сторипоинты:** 2

---

## Task 1.3: Настроить миграции и схему БД

**Для чего:** Создать SQL-миграции для таблиц tasks, examples, task_tags и настроить запуск миграций при старте.

**Подробное описание:**

1. Создать директорию `migrations/` в корне проекта
2. Установить `goose` как dependency
3. Написать `up`-миграцию (SQL-файл `001_create_tasks.up.sql`):
   - `CREATE TABLE tasks` со всеми полями из архитектуры
   - `CREATE TABLE examples` с внешним ключом на tasks
   - `CREATE TABLE task_tags` с составным ключом
   - Индексы: `source_id`, `type`, `difficulty`, `source_hash` (UNIQUE)
4. Написать `down`-миграцию (`001_create_tasks.down.sql` — DROP TABLE в обратном порядке)
5. Создать `internal/parser/storage/migrations.go`:
   - Функция `RunMigrations(dbURL string, migrationsDir string) error`
   - Использовать `goose` с PostgreSQL-драйвером
6. Проверить, что миграции идемпотентны — повторный запуск не ломает (version check)

**Тесты:**
- **Компонентный:** запуск миграций на тестовой PostgreSQL (testcontainers или docker compose)
- **Компонентный:** откат миграции и повторный накат
- **Компонентный:** UNIQUE-индекс на source_hash реально запрещает дубликаты
- **Unit:** проверка SQL-синтаксиса через линтер

**Сторипоинты:** 3
