# Task-Hunter — Сервис сбора и классификации задач по программированию

Сервис автоматически собирает задачи по программированию из внешних источников (веб-сайты, Telegram-каналы), приводит их к единому формату, классифицирует по типу и сложности, и сохраняет в БД.

---

## 1. Функционал

- **Сбор задач** — автоматический парсинг задач с Codeforces, LeetCode, CodeRun и Telegram-каналов
- **Унификация** — приведение задач из разных источников к единому формату (условие, примеры ввода/вывода, ограничения)
- **Дедупликация** — исключение повторных задач по хешу содержимого
- **Классификация** — rule-based определение типа задачи (алгоритмы, SQL, бэкенд, инфраструктура, тестирование, ревью кода, структуры данных)
- **Оценка сложности** — автоматическое определение сложности (Easy / Medium / Hard) на основе ключевых слов и длины описания
- **Планировщик** — сбор по cron-расписанию (по умолчанию каждые 6 часов) с graceful shutdown
- **Обработка ошибок** — сбой одного источника не влияет на сбор с остальных; ошибка обработки одной задачи не блокирует остальные
- **Логирование** — структурированное логирование (JSON) каждого этапа обработки

---

## 2. Типы задач

Сервис определяет 7 типов задач:

| Тип | Что включает |
|-----|-------------|
| **Алгоритмы** | Сортировка, поиск, графы, динамическое программирование, BFS/DFS |
| **Структуры данных** | Стеки, очереди, хеш-таблицы, деревья отрезков, DSU |
| **Базы данных** | SQL-запросы, JOIN, индексы, нормализация, транзакции |
| **Бэкенд** | REST API, аутентификация, JWT, gRPC, middleware |
| **Инфраструктура** | Docker, Kubernetes, CI/CD, Terraform, мониторинг |
| **Тестирование** | Unit-тесты, mock/stub, test coverage, TDD |
| **Ревью кода** | Code review, рефакторинг, code smell, SOLID, clean code |

---

## 3. Источники

| Источник | Тип доступа | Статус |
|----------|------------|--------|
| **[Codeforces](https://codeforces.com)** | REST API (`problemset.problems`) + HTML | ✅ Реализован |
| **[LeetCode](https://leetcode.com)** | GraphQL API (`/graphql`) | ✅ Реализован |
| **[CodeRun](https://coderun.yandex.ru)** | HTTP + HTML (goquery) | ✅ Реализован |
| **@analytic_postupashki** (Telegram) | MTProto (gotd/td) | 🚧 Базовая реализация |
| **@postupashki_ml** (Telegram) | MTProto (gotd/td) | 🚧 Базовая реализация |
| **@algoses** (Telegram) | MTProto (gotd/td) | 🚧 Базовая реализация |

Разрешения на сбор задач от всех источников получены. При сохранении указывается ссылка на оригинал задачи.

---

## 4. Как это работает

### 4.1 Жизненный цикл задачи

```
Источники → Коллекторы → RawTask → Пайплайн → Task → База данных
```

### 4.2 Этапы обработки

1. **Сбор (Collectors)**
   - Каждый источник имеет свой адаптер (коллектор), который знает, как получить данные
   - Codeforces: GET-запрос к API → JSON со списком задач → для каждой загрузка HTML-страницы
   - LeetCode: POST-запрос к GraphQL → JSON с деталями задачи (HTML-условие)
   - CodeRun: GET-запрос к каталогу → парсинг HTML (goquery) → для каждой ссылки загрузка страницы
   - Telegram: MTProto-соединение → получение новых сообщений из каналов

2. **Обработка (Pipeline)**
   - **Extractor** — извлекает заголовок и условие из сырого HTML/JSON/текста
   - **Parser** — выделяет примеры ввода/вывода, ограничения, теги
   - **Normalizer** — нормализует заголовки (### → ##), унифицирует язык примеров (Ввод: → Input:), удаляет дубликаты тегов
   - **Classifier** — определяет тип задачи (rule-based) и сложность (текстовые маркеры + длина описания)
   - **Validator** — проверяет обязательные поля, генерирует хеш для дедупликации

3. **Сохранение (Storage)**
   - PostgreSQL с типизированными запросами (go-jet codegen)
   - Проверка дубликатов по хешу (SHA-256 от sourceID + URL + содержимое)
   - Транзакционное сохранение задачи, примеров и тегов

### 4.3 Планировщик

- Запускается по cron-расписанию (по умолчанию: `0 */6 * * *` — каждые 6 часов)
- При старте подключается ко всем источникам и запускает бесконечный цикл ожидания
- На каждый тик cron выполняет полный цикл: сбор → пайплайн → сохранение
- Graceful shutdown: при получении SIGTERM/SIGINT дожидается завершения текущего сбора (таймаут 30 секунд), закрывает соединения с БД и источниками
- Устойчивость: ошибка одного источника или одной задачи не прерывает общий процесс

---

## 5. Запуск и проверка

### 5.1 Требования

- Go 1.26+
- PostgreSQL (локально или в Docker)
- Make (опционально)

### 5.2 Переменные окружения

| Переменная | Обязательная | По умолчанию | Описание |
|-----------|-------------|-------------|----------|
| `PARSER_DATABASE_DSN` | ✅ | — | DSN подключения к PostgreSQL |
| `PARSER_DATABASE_MAXCONNS` | | `10` | Макс. соединений с БД |
| `PARSER_SCHEDULE_COLLECTCRON` | | `0 */6 * * *` | Cron-расписание сбора |
| `PARSER_SOURCE_*_ENABLED` | | `false` | Включение источников (см. ниже) |

**Включение источников:**

```bash
# Включить LeetCode, Codeforces, CodeRun
export PARSER_SOURCE_LEETCODE_ENABLED=true
export PARSER_SOURCE_CODEFORCES_ENABLED=true
export PARSER_SOURCE_CODERUN_ENABLED=true
```

### 5.3 Запуск

#### Через Docker Compose (рекомендуемый способ)

```bash
# Поднять PostgreSQL + парсер
make docker-up

# Посмотреть логи
make docker-logs

# Остановить
make docker-down
```

#### Через Make + локальная БД

```bash
# 1. Поднять только PostgreSQL
make run-dev

# 2. Или вручную:
docker run -d --name parser-pg \
  -e POSTGRES_USER=parser \
  -e POSTGRES_PASSWORD=parser \
  -e POSTGRES_DB=tasks \
  -p 5432:5432 postgres:16

# 3. Настройка окружения
export PARSER_DATABASE_DSN="postgres://parser:parser@localhost:5432/tasks?sslmode=disable"
export PARSER_SOURCE_LEETCODE_ENABLED=true
export PARSER_SOURCE_CODEFORCES_ENABLED=true
export PARSER_SOURCE_CODERUN_ENABLED=true

# 4. Запуск сервиса
make run
# Или: go run .
# Или: go run ./cmd/parser
```

#### Через Docker Compose + все сервисы

```bash
# Сборка образа и запуск
make docker-up

# В docker-compose.yml включены все источники:
# PARSER_SOURCE_LEETCODE_ENABLED=true
# PARSER_SOURCE_CODEFORCES_ENABLED=true
# PARSER_SOURCE_CODERUN_ENABLED=true
```

### 5.4 Проверка работы

```bash
# 1. Запустить разовый сбор (без cron)
# Для этого можно установить cron на каждую минуту:
export PARSER_SCHEDULE_COLLECTCRON="* * * * *"
go run .

# 2. Проверить тесты
go test ./... ./tests/...

# 3. Проверить миграции
goose -dir migrations postgres "$PARSER_DATABASE_DSN" status

# 4. Подключиться к БД и проверить данные
psql "$PARSER_DATABASE_DSN" -c "SELECT COUNT(*) FROM tasks;"
psql "$PARSER_DATABASE_DSN" -c "SELECT title, type, difficulty FROM tasks LIMIT 10;"
```

### 5.5 Запуск тестов

```bash
# Все тесты
go test ./... ./tests/...

# Unit-тесты
go test ./internal/...

# Компонентные тесты (требуют PostgreSQL)
export PARSER_DATABASE_DSN="postgres://parser:parser@localhost:5432/diploma_test?sslmode=disable"
go test ./tests/...

# С обновлением golden-файлов
go test -update ./internal/parser/classifier/...
go test -update ./internal/parser/source/codeforces/...
go test -update ./internal/parser/source/coderun/...
```

---

## 6. Интеграция с другими сервисами

Сервис спроектирован как независимый модуль, который можно интегрировать с любыми потребителями через общую базу данных или API.

### 6.1 Через общую базу данных (рекомендуемый способ)

Самый простой и надёжный способ — настроить сервис на запись в общую БД, к которой имеют доступ другие сервисы (рекомендательная система, LMS, трекер студентов):

```
TaskCollector (пишет) → PostgreSQL ← Рекомендательная система (читает)
```

**Что нужно сделать:**
1. Развернуть TaskCollector рядом с БД рекомендательной системы
2. Настроить `PARSER_DATABASE_DSN` на общую базу данных
3. Миграции создадут таблицу `tasks` (если её нет)
4. Сервис-потребитель читает задачи через SQL-запросы или API

**Преимущества:** минимальная связность, нет зависимостей по времени, простота.

### 6.2 Через HTTP API (если нужна изоляция)

На данный момент HTTP API не реализован, но архитектура позволяет его легко добавить:

```
TaskCollector (gRPC/REST API) ← Рекомендательная система (запрашивает)
```

**Что нужно сделать:**
1. Добавить HTTP-сервер на порт 8080 с эндпоинтами:
   - `GET /api/v1/tasks` — список задач с фильтрацией по типу/сложности/источнику
   - `GET /api/v1/tasks/{id}` — детали задачи
   - `POST /api/v1/collect` — ручной запуск сбора (триггер)
   - `GET /api/v1/health` — проверка состояния сервиса
2. Добавить gRPC-сервер на порт 8082 с соответствующими proto-файлами
3. Документировать API через Swagger/OpenAPI

### 6.3 Через события (Message Queue)

Для асинхронной интеграции можно добавить Kafka:

```
TaskCollector → Kafka (топик tasks.created) → Рекомендательная система
```

**Что нужно сделать:**
1. Добавить продюсер в `app.go` после сохранения задачи
2. В `Scheduler.RunOnce` после `storage.SaveIfNotDuplicate` отправлять событие
3. Настроить топик `tasks.created` с ключом по типу задачи
4. Сервис-потребитель подписывается на топик и получает новые задачи в реальном времени

### 6.4 Пример использования в рекомендательной системе

Система может:
- Получать задачи через SQL: `SELECT * FROM tasks WHERE type = 'algorithm' AND difficulty = 'easy' ORDER BY created_at DESC`
- Фильтровать по тегам: `SELECT t.* FROM tasks t JOIN task_tags tt ON t.id = tt.task_id WHERE tt.tag IN ('sorting', 'array')`
- Получать статистику: `SELECT type, difficulty, COUNT(*) FROM tasks GROUP BY type, difficulty`

---

## 7. Что можно улучшить

### 7.1 Архитектура

- [ ] **HTTP/gRPC API** — реализовать REST/gRPC-сервер для доступа к задачам и ручного триггера сбора
- [ ] **Kafka-продюсер** — отправлять события о новых задачах в Kafka для асинхронной интеграции
- [ ] **Prometheus-метрики** — добавить `/metrics` для мониторинга количества собранных задач, времени обработки, ошибок
- [ ] **OpenTelemetry** — трассировка запросов между этапами пайплайна

### 7.2 Коллекторы

- [ ] **LeetCode: использовать реальный GraphQL** — тестовая заглушка работает на моках, нужна интеграция с живым API LeetCode
- [ ] **LeetCode: GraphQL-запрос деталей** — разделить запрос problemset (список) и question detail (контент) в реальном использовании
- [ ] **Telegram: MTProto-аутентификация** — требуется авторизация через Telegram API, доработка потока подключения
- [ ] **Добавить новые источники** — HackerRank, Codewars, AtCoder, Kattis
- [ ] **RSS/Atom-ленты** — некоторые платформы предоставляют RSS

### 7.3 Классификация

- [ ] **ML-модель** — заменить rule-based классификацию на NLP-модель (Python, transformers) при росте количества задач и типов
- [ ] **Определение тегов** — извлекать теги не только из источника, но и из текста задачи (ключевые слова)
- [ ] **Улучшить определение сложности** — анализировать не только текст, но и technical debt (количество примеров, наличие сложных конструкций)
- [ ] **Обратная связь** — учитывать результаты пользователей (сколько решили, время решения) для уточнения сложности

### 7.4 Хранение

- [ ] **Кеширование** — добавить Redis для кеша списка задач (уменьшить нагрузку на PostgreSQL)
- [ ] **Полнотекстовый поиск** — tsvector-индексы для поиска по тексту задач
- [ ] **Версионирование** — хранить историю изменений задачи (если источник обновляет условие)
- [ ] **Денормализация для чтения** — материализованные представления для частых запросов

### 7.5 DevOps

- [ ] **Dockerfile** — контейнеризация сервиса для деплоя
- [ ] **Docker Compose** — поднятие сервиса + PostgreSQL одной командой
- [ ] **CI/CD** — GitHub Actions для тестов, линтинга, сборки образа
- [ ] **Configuration Management** — поддержка файлов конфигурации (YAML/TOML) в дополнение к env

### 7.6 Тестирование

- [ ] **Интеграционные тесты** — тесты с реальными HTTP-запросами к источникам (с моками на уровне сети)
- [ ] **Тесты с testcontainers** — компонентные тесты с PostgreSQL в контейнере
- [ ] **Фаззинг** — fuzz-тесты для парсера HTML (устойчивость к некорректной разметке)

---

## Структура проекта

```
.
├── main.go                         # Точка входа
├── cmd/parser/main.go              # Альтернативная точка входа
├── config/config.go                # Конфигурация (envconfig)
├── migrations/                     # SQL-миграции (goose)
├── internal/parser/
│   ├── app.go                      # Приложение (инициализация, запуск, shutdown)
│   ├── scheduler.go                # Планировщик по cron
│   ├── domain/models.go            # Модели данных (Task, RawTask, Source, ...)
│   ├── pipeline/                   # Пайплайн обработки
│   │   ├── pipeline.go             # Оркестратор пайплайна
│   │   ├── extractor.go            # Извлечение из сырых данных
│   │   ├── parser.go               # Парсинг примеров и ограничений
│   │   ├── normalizer.go           # Нормализация форматов
│   │   ├── validator.go            # Валидация целостности
│   │   └── classifier.go ->  ├── classifier/  # Классификация
│   ├── classifier/                 # Rule-based классификатор
│   ├── source/                     # Адаптеры источников
│   │   ├── source.go               # Интерфейсы Collector + Manager
│   │   ├── codeforces/             # Codeforces (API)
│   │   ├── coderun/                # CodeRun (HTTP + goquery)
│   │   ├── leetcode/               # LeetCode (GraphQL)
│   │   └── telegram/               # Telegram (MTProto)
│   └── storage/                    # Слой хранения
│       ├── repository.go           # Интерфейс Repository
│       ├── postgres.go             # PostgreSQL (go-jet)
│       └── migrations.go           # Goose wrapper
├── tests/component/                # Компонентные тесты
└── docs/                           # Документация
    ├── module.md                   # Описание модуля
    ├── architecture.md             # Архитектура
    └── development/                # Детальные story
```

---

## Технологический стек

| Компонент | Технология |
|-----------|-----------|
| **Язык** | Go 1.26 |
| **База данных** | PostgreSQL 16 |
| **SQL-генерация** | go-jet (codegen) |
| **Миграции** | goose |
| **HTTP** | net/http |
| **HTML-парсинг** | goquery (PuerkitoBio) |
| **Telegram** | gotd/td (MTProto) |
| **Планировщик** | robfig/cron/v3 |
| **Логирование** | log/slog (JSON) |
| **Конфиг** | envconfig |
| **Тестирование** | golden files, table-driven tests |
