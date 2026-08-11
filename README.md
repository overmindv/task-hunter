# task-hunter

Сервис Overmindv для асинхронного сбора кандидатов задач. `task-hunter` владеет очередью collection jobs и Telegram checkpoint'ами, а нормализованные кандидаты передаёт владельцу задач `tasks-it` через защищённый batch API.

## Потоки

- Cron в UTC (по умолчанию `0 */6 * * *`) только ставит Telegram job в PostgreSQL.
- При первом запуске канал без checkpoint получает bootstrap за последние 24 часа.
- Ручной admin job принимает до 20 прямых HTTPS-ссылок Codeforces, LeetCode и CodeRun за один запуск либо опциональный диапазон Telegram.
- Ссылки с завершающим `/`, `www`, query-параметрами и Codeforces contest URL приводятся к одному каноническому виду до проверки дублей.
- LeetCode читается через GraphQL, CodeRun — через `__NEXT_DATA__` и открытые файлы условия, Codeforces при блокировке HTML использует Reader fallback.
- Worker арендует job через `FOR UPDATE SKIP LOCKED`, продлевает lease и изолирует ошибки источников.
- Кандидаты отправляются в `tasks-it` идемпотентными batch-запросами; HTML и полные Telegram-сообщения в БД task-hunter не сохраняются.

## HTTP API

Все `/v1/admin/*` routes требуют gateway bearer token, `X-User-ID` и admin в `X-User-Roles`.

- `GET /health`, `GET /ready`
- `GET /v1/admin/collection-sources`
- `POST /v1/admin/collection-jobs` — ответ `202 Accepted`
- `GET /v1/admin/collection-jobs`
- `GET /v1/admin/collection-jobs/{id}`
- `POST /v1/admin/collection-jobs/{id}/acknowledge`

## Локальная проверка

Миграции выполняются отдельным контейнером:

```bash
goose -dir migrations postgres "$PARSER_DATABASE_DSN" up
go test ./...
go vet ./...
go run ./cmd/task-hunter
```

PostgreSQL component tests запускаются отдельно командой `COMPONENT_TEST_DSN='postgres://…' make test-component`. Они откатывают и повторно накатывают миграции, поэтому DSN должен указывать только на выделенную тестовую БД.

Основные настройки: `PARSER_DATABASE_DSN`, `PARSER_TASKSIT_URL`, `PARSER_TASKSIT_TOKEN`, `PARSER_SECURITY_GATEWAYTOKEN` и `PARSER_TELEGRAM_ENABLED`. `PARSER_WEBSITE_CODEFORCESREADERURL` уже имеет публичное значение по умолчанию и не требует API-ключа. При включённом Telegram также требуются `PARSER_TELEGRAM_APIID`, `PARSER_TELEGRAM_APIHASH`, `PARSER_TELEGRAM_SESSIONPATH` и `PARSER_TELEGRAM_CHANNELS`.

Полный локальный стек запускается одной командой `make up` из репозитория `infra`. Telegram там по умолчанию выключен и не нужен для readiness; подключение MTProto описано в инфраструктурном README.

Подготовка закрытого MTProto session описана в [docs/telegram-session.md](docs/telegram-session.md). Интегрированный локальный запуск находится в репозитории `infra`.

Открытые примеры ввода и ожидаемого вывода сохраняются. Скрытые judge-тесты и эталонные решения сайты не публикуют, поэтому сервис их не извлекает и не пытается обходить ограничения источников.
