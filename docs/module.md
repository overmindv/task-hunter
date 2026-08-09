# Модуль парсинга задач (TaskCollector)

## 1. Назначение

TaskCollector — компонент образовательной платформы, отвечающий за сбор,
структурирование, классификацию и хранение задач по программированию
из внешних источников (веб-сайты, Telegram-каналы).

Задачи приводятся к единому формату, классифицируются по темам и сложности,
после чего становятся доступны пользователям через рекомендательную систему
или LMS.

## 2. Типы задач

Модуль работает со следующими типами задач:

| Тип | Описание | Ключевые слова |
|-----|----------|----------------|
| **Алгоритмы** | Сортировка, поиск, графы, DP | массив, дерево, граф, сортировка, DP, бинарный поиск, BFS, DFS |
| **Структуры данных** | Специализированные структуры | стек, очередь, хеш-таблица, heap, Trie, DSU, Fenwick |
| **Базы данных** | SQL-запросы, проектирование БД | SQL, SELECT, JOIN, индексы, нормализация, транзакции, ACID |
| **Бэкенд** | Серверная разработка | API, REST, HTTP, JWT, gRPC, middleware, микросервисы |
| **Инфраструктура** | Деплой, контейнеризация | Docker, Kubernetes, CI/CD, Terraform, Prometheus |
| **Тестирование** | Проверка качества кода | unit test, mock, stub, TDD, test coverage |
| **Ревью кода** | Анализ качества кода | code review, рефакторинг, code smell, SOLID, clean code |

## 3. Источники

| Источник | Протокол | Формат | Rate limit | Статус |
|----------|----------|--------|------------|--------|
| **Codeforces** | REST API + HTTP | JSON (API) + HTML (страница) | 1 req/2s | ✅ |
| **LeetCode** | GraphQL API | JSON (условие как HTML) | 1 req/3s | ✅ |
| **CodeRun (Яндекс)** | HTTP + HTML | HTML (goquery) | 1 req/1s | ✅ |
| **Telegram-каналы** | MTProto (gotd/td) | Текст + медиа | настраиваемый | 🚧 |

## 4. Поток данных

```
                    ┌──────────────────────────────────────┐
                    │           Планировщик (cron)          │
                    │    каждые N часов / ручной вызов      │
                    └──────────┬───────────────────────────┘
                               │
          ┌────────────────────┼────────────────────┐
          ▼                    ▼                    ▼
   ┌──────────┐         ┌──────────┐         ┌──────────┐
   │Codeforces│         │ LeetCode │         │ CodeRun  │
   │Collector │         │Collector │         │Collector │
   └─────┬────┘         └────┬─────┘         └────┬─────┘
         │                   │                    │
         └───────────────────┼────────────────────┘
                             ▼
                    ┌─────────────────┐
                    │  Source Manager  │
                    │  (CollectAll)    │
                    └────────┬────────┘
                             │ []RawTask
                             ▼
                    ┌─────────────────┐
                    │   Extractor     │ ← HTML/JSON → текст
                    ├─────────────────┤
                    │    Parser       │ ← выделение примеров, ограничений
                    ├─────────────────┤
                    │   Normalizer    │ ← унификация форматов
                    ├─────────────────┤
                    │   Classifier    │ ← тип (rule-based) + сложность
                    ├─────────────────┤
                    │   Validator     │ ← проверка целостности
                    └────────┬────────┘
                             │ domain.Task
                             ▼
                    ┌─────────────────┐
                    │   Repository    │ ← SaveIfNotDuplicate
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │   PostgreSQL    │
                    │ (tasks, tags,   │
                    │  examples)      │
                    └─────────────────┘
```

## 5. Этапы обработки

### 5.1 Сбор (Collect → RawTask)

Каждый коллектор имплементирует интерфейс:

```go
type Collector interface {
    ID() domain.SourceID
    Connect(ctx) error
    Collect(ctx) ([]RawTask, error)
    Close() error
}
```

#### Codeforces
1. GET-запрос к `https://codeforces.com/api/problemset.problems` (JSON)
2. Парсинг ответа: список задач с contestId, index, name, rating, tags
3. Для каждой задачи: GET страницы `https://codeforces.com/problemset/problem/{contestId}/{index}`
4. Упаковка в RawTask: HTML-код страницы + URL источника
5. Rate limit: 1 запрос в 2.1 секунды

#### LeetCode
1. POST-запрос к `https://leetcode.com/graphql` с GraphQL-запросом `problemsetQuestionList`
2. Парсинг: список задач с titleSlug, title, difficulty
3. Для каждой: POST с запросом `questionData(titleSlug)`, получение HTML-условия
4. Rate limit: 1 запрос в 3.1 секунды
5. User-Agent: браузерный (Mozilla/5.0 ... Chrome/...)

#### CodeRun (Яндекс)
1. GET-запрос к `https://coderun.yandex.ru/catalog`
2. Парсинг HTML через goquery (селектор `.problem-list .problem-item a.problem-title`)
3. Для каждой ссылки: GET страницы `https://coderun.yandex.ru/problem/{slug}`
4. Проверка наличия условия (селектор `h1.problem-title`)
5. Rate limit: 1 запрос в 1.1 секунды

#### Telegram
1. MTProto-соединение через библиотеку gotd/td
2. Разрешение username каналов в chatID + accessHash
3. Получение сообщений через `messages.GetHistory` (начиная с последнего обработанного ID)
4. Извлечение текста, вложений (код, изображения)
5. Генерация ссылки формата `https://t.me/{username}/{id}`

### 5.2 Обработка (Pipeline → Task)

Pipeline — последовательность процессоров:

1. **Extractor** — извлекает заголовок из HTML (h1/h2/h3 + классы `problem-statement`, `question-content`), текст условия
2. **Parser** — находит примеры ввода/вывода через регулярные выражения, выделяет ограничения и теги
3. **Normalizer** — нормализует Markdown-заголовки (### → ##), унифицирует русские/английские заголовки примеров, обрезает поля до лимитов
4. **Classifier** — rule-based: поиск ключевых слов в заголовке + описании + тегах; определяет тип и сложность
5. **Validator** — проверка обязательных полей (title, description, sourceURL, sourceHash), валидация длин

### 5.3 Классификация (Rule-Based)

**Тип задачи:** для каждого из 7 типов определён набор ключевых слов. Классификатор подсчитывает совпадения в тексте задачи. Тип с максимальным числом совпадений выбирается как итоговый. Если совпадений нет — `algorithm` (по умолчанию).

**Сложность:** трёхуровневая система:
- **Easy:** маркеры (простой, лёгкий, базовый, elementary, beginner) или короткое описание (< 300 символов)
- **Hard:** маркеры (сложный, продвинутый, LCA, heavy-light, max flow, NP-полн) или длинное описание (> 2000 символов)
- **Medium:** всё остальное (по умолчанию)

### 5.4 Хранение (Storage)

- PostgreSQL 16
- Генерация типизированных SQL-запросов через go-jet (codegen)
- Миграции через goose (файлы в `migrations/`)
- Дедупликация: SHA-256 хеш от `sourceID + sourceURL + содержимое`
- Транзакционное сохранение: задача + примеры ввода/вывода + теги

## 6. Планировщик

- Тип: cron (`robfig/cron/v3`)
- Расписание по умолчанию: `0 */6 * * *` (каждые 6 часов)
- Механизм: при старте подключает все источники, затем ожидает тиков cron
- Один цикл: `CollectAll()` → для каждой задачи `Pipeline.Run()` → `SaveIfNotDuplicate()`
- Graceful shutdown:
  1. Сигнал SIGTERM/SIGINT → закрытие канала остановки
  2. Ожидание завершения текущего цикла сбора
  3. Остановка cron-планировщика
  4. Закрытие соединения с БД
  5. Таймаут на всё: 30 секунд

## 7. Интеграция с другими сервисами

### 7.1 Через общую базу данных

TaskCollector может писать задачи в общую PostgreSQL, которую читают другие сервисы:

```sql
-- Получить все задачи-алгоритмы лёгкой сложности
SELECT * FROM tasks WHERE type = 'algorithm' AND difficulty = 'easy';

-- Получить задачи по тегу
SELECT t.* FROM tasks t
JOIN task_tags tt ON t.id = tt.task_id
WHERE tt.tag = 'sorting';

-- Статистика по источникам
SELECT source_id, COUNT(*) FROM tasks GROUP BY source_id;
```

### 7.2 Через HTTP API (предлагаемый эндпоинт)

```
GET  /api/v1/tasks?type=algorithm&difficulty=easy&limit=20&offset=0
GET  /api/v1/tasks/{uuid}
POST /api/v1/collect                          # Ручной триггер сбора
GET  /api/v1/sources                          # Статус источников
GET  /health                                  # Health check
```

### 7.3 Через события (Kafka)

При сохранении новой задачи публиковать событие в Kafka:

```json
{
  "event": "task.created",
  "payload": {
    "id": "uuid",
    "type": "algorithm",
    "difficulty": "easy",
    "source": "leetcode",
    "title": "Two Sum",
    "tags": ["array", "hash-table"],
    "created_at": "2026-08-08T12:00:00Z"
  }
}
```

### 7.4 Пример сценария: рекомендательная система

1. TaskCollector собирает задачи и пишет в `tasks` (общая БД)
2. Рекомендательная система читает задачи через SQL
3. Система анализирует прогресс студента и подбирает задачи
4. Студент решает задачу, результат пишется обратно в БД
5. TaskCollector не зависит от рекомендательной системы — они общаются только через данные

## 8. План развития

### Ближайшие улучшения
- HTTP/gRPC API для внешнего доступа
- Prometheus-метрики
- Dockerfile + Docker Compose
- Полнотекстовый поиск (tsvector)

### Среднесрочные
- ML-классификатор на Python (transformers)
- Kafka-интеграция
- Новые источники (HackerRank, Codewars)

### Долгосрочные
- Версионирование задач
- Кеширование (Redis)
- CI/CD (GitHub Actions)
