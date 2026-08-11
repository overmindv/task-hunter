# task-hunter

Сервис Overmindv для асинхронного сбора кандидатов задач. `task-hunter` владеет очередью collection jobs и Telegram checkpoint'ами, а нормализованные кандидаты передаёт владельцу задач `tasks-it` через защищённый batch API.

## Потоки

- Cron в UTC (по умолчанию `0 */6 * * *`) только ставит Telegram job в PostgreSQL.
- При первом запуске канал без checkpoint получает bootstrap за последние 24 часа.
- Ручной admin job принимает allowlist Telegram-каналы и диапазон `[published_from,published_to)` до 31 дня либо до 20 прямых HTTPS-ссылок Codeforces, LeetCode и CodeRun.
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

Основные настройки: `PARSER_DATABASE_DSN`, `PARSER_TASKSIT_URL`, `PARSER_TASKSIT_TOKEN`, `PARSER_SECURITY_GATEWAYTOKEN`, `PARSER_TELEGRAM_APIID`, `PARSER_TELEGRAM_APIHASH`, `PARSER_TELEGRAM_SESSIONPATH`, `PARSER_TELEGRAM_CHANNELS`.

Для изолированного Compose-запуска скопируйте `.env.example` в `.env`, укажите адрес уже запущенного `tasks-it` и подготовьте Telegram session в volume. Полный стек с обоими сервисами, gateway и frontend поднимается из репозитория `infra`.

Подготовка закрытого MTProto session описана в [docs/telegram-session.md](docs/telegram-session.md). Интегрированный локальный запуск находится в репозитории `infra`.
