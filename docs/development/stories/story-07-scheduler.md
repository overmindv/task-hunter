# Story 7: Scheduler — планировщик и точка входа

**Для чего:** Интегрировать все компоненты модуля: точка входа, планировщик сбора по расписанию, graceful shutdown, метрики, логирование.

---

## Task 7.1: Реализовать планировщик сбора по расписанию

**Для чего:** Создать менеджер, который по cron-расписанию запускает сбор задач со всех источников и прогоняет через пайплайн.

**Подробное описание:**

1. Создать `internal/parser/scheduler.go`:
   - Структура `Scheduler`
   - Поля: `sourceManager *source.Manager`, `pipeline *pipeline.Pipeline`, `repo storage.Repository`
2. Конструктор: `NewScheduler(sm, pl, repo) *Scheduler`
3. Метод `RunOnce(ctx) error`:
   - Вызвать `sourceManager.CollectAll(ctx)`
   - Для каждого `RawTask`:
     - Вызвать `pipeline.Run(ctx, raw)`
     - Вызвать `storage.SaveIfNotDuplicate(ctx, task, repo)`
     - Логировать: количество собранных, успешно обработанных, дубликатов, ошибок
   - Вернуть сводку (aggregate error — не останавливает обработку при единичных ошибках)
4. Метод `Start(ctx, cronExpr string) error`:
   - Запустить `robfig/cron/v3` с указанным выражением
   - При каждом тике вызывать `RunOnce`
   - Поддерживать `Stop() error` для остановки
5. Graceful shutdown:
   - Дождаться завершения текущего сбора
   - Остановить cron

**Тесты:**
- **Компонентный:** запуск Scheduler с моками Collector и Pipeline
- **Компонентный:** RunOnce: 2 raw → 2 task → 2 save, проверка вызовов
- **Unit:** Scheduler.Stop не блокирует больше N секунд
- **Unit:** ошибка в одном RawTask не останавливает обработку остальных

**Сторипоинты:** 2

---

## Task 7.2: Реализовать точку входа и graceful shutdown

**Для чего:** Создать `cmd/parser/main.go` — точку входа для модуля парсинга, собирающую все компоненты, загружающую конфиг и запускающую scheduler.

**Подробное описание:**

1. Создать `cmd/parser/main.go`
2. Логика:
   1. Загрузить конфиг через `config.Load()`
   2. Настроить структурированное логирование через `slog`:
      - JSON-формат (для прода)
      - Уровень из конфига (debug/info/warn/error)
   3. Подключиться к PostgreSQL через `database/sql` с драйвером PostgreSQL
   4. Запустить миграции через `goose.Up(db, migrationsDir)`
   5. Создать репозиторий: `storage.NewPostgresRepository(db)`
   6. Создать менеджер источников: `source.NewManager(cfg.Sources)`
   7. Создать пайплайн: `pipeline.NewDefaultPipeline()`
   8. Создать scheduler: `scheduler.NewScheduler(sm, pl, repo)`
   9. Запустить scheduler в фоне по cron-расписанию
   10. Graceful shutdown:
       - Перехват SIGTERM, SIGINT (через `os/signal`)
       - Сигнал → отмена контекста → остановка scheduler → закрытие пула БД
       - Таймаут на остановку: 30 секунд
3. Добавить `main.go` в корне (если нужно) как точку входа, вызывающую `cmd/parser`
4. Обработка ошибок на каждом шаге инициализации

**Тесты:**
- **Компонентный:** main.go с мокированными зависимостями (testcontainers для БД)
- **Компонентный:** graceful shutdown — отправка SIGTERM, проверка что scheduler остановился
- **Unit:** загрузка конфига + инициализация всех компонентов (через интерфейсы)

**Сторипоинты:** 2
