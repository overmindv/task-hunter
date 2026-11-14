# Story 8: Списки задач (problem lists)

**Для чего:** Уметь собирать задачи по ссылке на сборник (например, `https://leetcode.com/problem-list/oy4y273c/`) и создавать не разрозненные задачи, а именованный список задач в `tasks`, проходящий модерацию вместе с кандидатами.

---

## Task 8.1: Адаптер LeetCode для problem-list

**Для чего:** В `internal/parser/source/leetcode` добавить сбор целого списка задач по URL `https://leetcode.com/problem-list/<slug>/`.

**Подробное описание:**

1. Добавить `CollectProblemListURL(ctx, rawURL) ([]domain.RawTask, error)`:
   - Валидировать URL: хост `leetcode.com`, путь `/problem-list/{slug}`
   - GraphQL-операция `favoriteQuestionList(favoriteSlug: String!)` — все задачи списка одним ответом (`favoriteQuestionList { hasMore questions { title titleSlug difficulty topicTags { name } } }`)
   - Для каждой задачи запросить детали через существующий `questionData`
   - Пропускать задачи с пустым контентом
2. Rate limiting — тот же `rateLimiter`, что у обычных запросов.
3. Название списка анонимно недоступно (Cloudflare + эндпоинты списков требуют авторизацию) → имя по умолчанию = slug.

**Тесты:**
- **Unit (golden):** JSON-ответ `favoriteQuestionList` → список кратких задач
- **Unit:** `CollectProblemListURL` собирает детали каждой задачи
- **Unit:** невалидный URL problem-list отклоняется

**Сторипоинты:** 2

---

## Task 8.2: Связывание кандидатов со списком в collection

**Для чего:** Чтобы кандидаты из problem-list попадали в tasks уже со ссылкой на список.

**Подробное описание:**

1. `internal/collection/models.go`:
   - `ProblemListRef{Name, SourceID, SourceURL}`
   - `Candidate.ProblemList *ProblemListRef` (`json:"problem_list,omitempty"`)
   - `NormalizeWebsiteURL` распознаёт `https://leetcode.com/problem-list/{slug}` → source `leetcode`
2. `internal/collection/website.go`: `ReadURL` возвращает `WebsiteBatch{RawTasks, ProblemList}`; leetcode-ссылка на список маршрутизируется в `CollectProblemListURL`.
3. `internal/collection/worker.go`: при постановке кандидата проставлять `candidate.ProblemList` из батча; по-прежнему учитывать `MaxItemsPerSource`.

**Тесты:**
- **Unit:** нормализация URL problem-list
- **Unit (pipeline):** `TestCollectWebsiteProblemList` — кандидаты из списка приходят с `ProblemList`

**Сторипоинты:** 2

---

## Task 8.3: Сущность «список задач» в tasks

**Для чего:** Хранить именованные наборы задач и модерационные ссылки на них.

**Подробное описание:**

1. Миграция `problem_lists` (id, name, source_id, source_url, timestamps) + `problem_list_members` (FK на список и задачу) + `task_candidates.problem_list_id`.
2. `GetOrCreateProblemList` — idempotent по `source_url` (SELECT → INSERT с повторным чтением при конфликте).
3. `InsertProblemListMember` — position = MAX(position)+1, `ON CONFLICT DO NOTHING`.
4. `ListProblemLists` / `GetProblemListDetail` с числом опубликованных задач и ожидающих кандидатов.
5. В `ImportBatch` (в транзакции) кандидат получает `problem_list_id`; в `Approve` после публикации вставляется строка `problem_list_members`.
6. HTTP API: `GET /v1/admin/problem-lists`, `GET /v1/admin/problem-lists/{id}`, фильтр кандидатов `problem_list_id`.

**Тесты:**
- **Unit:** импорт кандидата с problem list ассоциирует его в транзакции
- **Unit:** сервис списков: list/get
- **Unit:** approve добавляет участника списка

**Сторипоинты:** 3

---

## Task 8.4: api-gateway и frontend

**Для чего:** Показать списки задач и фильтр кандидатов по списку в админке.

**Подробное описание:**

1. GraphQL schema: `ProblemList`, `ProblemListMember`, `ProblemListPage`, `TaskCandidate.problemListId`, фильтр по `problemListId`.
2. Резолверы `problemLists`/`problemList` (через `tasks` client), фильтр кандидатов.
3. Frontend: панель «Списки задач», выбор списка в очереди модерации, ссылка на список из кандидата.

**Тесты:**
- **Typecheck/lint** frontend; unit-тесты api-gateway.

**Сторипоинты:** 2
