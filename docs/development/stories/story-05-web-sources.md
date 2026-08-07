# Story 5: Web Sources — адаптеры веб-сайтов

**Для чего:** Реализовать сбор задач из веб-источников: Codeforces (через API), CodeRun (через HTTP), LeetCode (через HTTP + HTML).

---

## Task 5.1: Реализовать коллектор Codeforces через API

**Для чего:** Создать пакет `internal/parser/source/codeforces`, реализующий сбор задач через официальный Codeforces API.

**Подробное описание:**

1. Создать директорию `internal/parser/source/codeforces/`
2. Реализовать структуру `Collector` (имплементирует `source.Collector`)
3. Использовать Codeforces API:
   - `https://codeforces.com/api/problemset.problems` — список задач
   - `https://codeforces.com/problemset/problem/{contestId}/{index}` — страница задачи
4. Метод `Collect(ctx) ([]domain.RawTask, error)`:
   - Получить список задач через API
   - Фильтровать: только задачи на русском/английском (из конфига)
   - Для каждой задачи загрузить HTML-страницу для полного контента
   - Упаковать в `RawTask` (HTML-код + metadata)
5. Маппинг полей:
   - contestId + index → SourceURL
   - name → title
   - rating → difficulty (800- → easy, 1200- → medium, 1600+ → hard)
   - tags → Tags
6. Rate limiting: не более 1 запроса в 2 секунды (требование Codeforces API)
7. Конфигурация через `SourceConfig.Params`: `language` (ru/en)

**Тесты:**
- **Компонентный:** запрос к API (если доступен) с проверкой формата ответа
- **Unit (golden):** парсинг HTML-страницы задачи в RawTask
- **Unit:** маппинг rating → Difficulty корректен
- **Unit:** rate limiter не пропускает больше N запросов в секунду

**Сторипоинты:** 2

---

## Task 5.2: Реализовать коллектор CodeRun через HTTP

**Для чего:** Создать пакет `internal/parser/source/coderun`, реализующий сбор задач с CodeRun (Яндекс) через HTTP и парсинг HTML.

**Подробное описание:**

1. Создать директорию `internal/parser/source/coderun/`
2. Реализовать структуру `Collector` (имплементирует `source.Collector`)
3. Определить список URL-адресов для сбора (из конфига `Params.urls`)
   - Пример: `https://coderun.yandex.ru/catalog`
4. Метод `Collect(ctx) ([]domain.RawTask, error)`:
   - Загрузить страницу каталога задач
   - Распарсить список задач через `goquery`
   - Для каждой задачи перейти на страницу задачи
   - Извлечь: заголовок, условие, примеры, ограничения
   - Упаковать в `RawTask`
5. Обработка ошибок:
   - 404 — пропустить задачу, залогировать
   - 429 — подождать Retry-After
   - HTML без задачи — ошибка, не создавать пустую RawTask
6. User-Agent: настраиваемый через конфиг

**Тесты:**
- **Unit (golden):** парсинг HTML-страницы каталога → список URL задач
- **Unit (golden):** парсинг HTML-страницы задачи → RawTask
- **Unit:** обработка 404 (пропуск)
- **Unit:** обработка пустого HTML (ошибка)

**Сторипоинты:** 2

---

## Task 5.3: Реализовать коллектор LeetCode через HTTP

**Для чего:** Создать пакет `internal/parser/source/leetcode`, реализующий сбор задач с LeetCode через парсинг HTML-страниц.

**Подробное описание:**

1. Создать директорию `internal/parser/source/leetcode/`
2. Реализовать структуру `Collector` (имплементирует `source.Collector`)
3. Метод `Collect(ctx) ([]domain.RawTask, error)`:
   - Использовать список задач из `https://leetcode.com/problemset/`
   - Распарсить список через `goquery`
   - Для каждой задачи перейти на страницу задачи
   - Извлечь: заголовок, условие, примеры, ограничения, сложность
   - Упаковать в `RawTask`
4. LeetCode использует динамическую загрузку (React) — возможно, понадобится:
   - Парсить статическую разметку (meta-теги, schema.org)
   - Или использовать JSON-эндпоинты (GraphQL), если обнаружится
5. SourceHash: генерировать из URL + заголовка
6. Rate limiting: не более 1 запроса в 3 секунды
7. User-Agent: браузерный (чтобы не блокировали)

**Тесты:**
- **Unit (golden):** парсинг страницы списка задач
- **Unit (golden):** парсинг страницы задачи (с примерами и ограничениями)
- **Unit:** определение сложности (Easy/Medium/Hard → Difficulty)
- **Unit:** обработка капчи/блокировки (если HTML — страница с reCAPTCHA)
- **Unit:** задача без примеров (некоторые задачи LeetCode)

**Сторипоинты:** 3
