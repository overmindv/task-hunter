# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project overview

Дипломный проект ВШЭ (НИУ ВШЭ) — сервис на Go. На данный момент кодовая база находится на начальном этапе разработки (стартовый шаблон GoLand).

Данный сервис предназначен для сбора и парсинга задач по программированию.

Типы задач:
- алгоритмы
- структуры данных
- базы данных
- бэкенд
- поднятие инфраструктуры
- тестирование
- ревью кода

Методы сбора задач:
- Парсинг задач с сайтов
- Парсинг задач из Телеграм-каналов

Одни из источников:
- Телеграм-канал Поступашек "Аналитика (Собеседования и Тестовые)": https://t.me/analytic_postupashki
- Телеграм-канал Поступашек "МЛ (Собеседования и Тестовые)": https://t.me/postupashki_ml
- Телеграм-канал "Алгоритмы - Собеседования, Олимпиады, ШАД": https://t.me/algoses
- Сайт Leetcode: https://leetcode.com
- Сайт Codeforces: https://codeforces.com
- Сайт CodeRun: https://coderun.yandex.ru

Важное уточнение - у меня есть доступ и разрешение на сбор этих задач, поэтому никакого плагиата и нарушений не будет. При этом необходимо указывать источник, откуда задачи были спарсены.

## Stack

- Go, Python
- PostgreSQL
- Docker
- Makefile
- CI github
- Swagger

При необходимости:
- Protobuf
- Kafka
- Redis
- Более подходящие БД

## Languages

- **Core — Go** (модуль парсинга, инфраструктура)
- **Python** (опционально — NLP/ML для классификатора в будущем)

## Libraries

| Библиотека | Назначение |
|---|---|
| `github.com/samber/lo` | Утилиты для работы со слайсами/мапами |
| `github.com/go-jet/jet/v2` | Типизированные SQL-запросы (codegen) |
| `github.com/pressly/goose` | Миграции БД |
| `log/slog` | Структурированное логирование |
| `github.com/gotd/td` | MTProto-клиент для Telegram |
| `github.com/PuerkitoBio/goquery` | HTML-парсинг |
| `github.com/robfig/cron/v3` | Планировщик по расписанию |

## Commands

- **Build:** `go build ./...`
- **Run:** `go run .`
- **Run parser module:** `go run ./cmd/parser`
- **Test all:** `go test ./...`
- **Run single test:** `go test -run <TestName> ./<package>`
- **Lint (golangci-lint):** `golangci-lint run ./...`
- **Format:** `gofmt -s -w .`
- **Goose migrate up:** `goose -dir migrations postgres "$PARSER_DATABASE_DSN" up`
- **Goose migrate down:** `goose -dir migrations postgres "$PARSER_DATABASE_DSN" down`
- **Go-jet codegen:** `jet -dsn "$PARSER_DATABASE_DSN" -path internal/parser/storage/jet`

## Tasks

Пример записи задач:

Описание: реализация хендлера checkBankruptcy
Для чего: для разделения на checkBlocks и checkBankruptcy с автоматическим отбитием PB2
Подробное описание:
TriggerAutoNext - шаг проверки банкроства (по аналогии с текущим checkBankruptcy):
1. В новом хендлере checkBankruptcy (ведет на общий CheckPriorityBankruptcy):
   1) В RPO - выходим для otherBanksRpo
   2) Дергаем FnsNewPriorityBankruptcyVerdict (возможно тут будет заглушка).
   3) BankruptcyVerdictManual - встаем в стейт MANUAL_CONTROL
   4) BankruptcyVerdictReject - отзыв ареста. Отправляем PB2 c кодом 48, причина = "Находится в процедуре банкротства". Выходим в sameState.
   5) BankruptcyVerdictAccept - выходим в BANKRUPTCY_CHECKED
   6) В stepReason указываем вердикт, причину из ответа FnsNewPriorityBankruptcyVerdict
2. В onManualConfirm:
   1) Добавляем новый тип по примеру ManualProcessingReasonBankruptcy. Выходим в стейт BANKRUPTCY_CHECKED.
3. В старом хендлере CheckBlocks удаляем обработку BlockTypeBankruptcy (под ФФ).
4. Меняем переход в дереве (под ФФ):
   - FnsDocumentState_STATE_FNS_DOCUMENT_PREPARED: service.checkBlocks (-> FnsDocumentState_STATE_FNS_BLOCKS_CHECKED),
   - FnsDocumentState_STATE_FNS_BLOCKS_CHECKED: service.checkBankruptcy(-> FnsDocumentState_STATE_FNS_BANKRUPTCY_CHECKED),
Сторипоинты: 3

Оценка сторипоинтов:
1 - Задача на пару часов
2 - Задача на 1 день
3 - Задача на 2 дня
5 - Задача на 3 дня
7 - Задача на 5 дней

Лучше, чтобы все задачи было 1, 2 или 3 сторипоинта. Если выше, то декомпозировать.

## Code structure

```
.
├── main.go        # Точка входа
├── go.mod         # Go module: diploma (Go 1.26)
├── .idea/         # Настройки GoLand
├── .claude/       # Конфигурация Claude Code
└── .codegraph/    # Граф кода для Claude Code
```

Проект использует Go 1.26, модуль `diploma`. По мере роста кодовой базы ожидается появление директорий для:

- `internal/` — внутренняя логика сервиса
- `cmd/` — точки входа (если их станет несколько)
- `pkg/` — переиспользуемые пакеты
- `api/` — описание API (protobuf, OpenAPI)
- `config/` — конфигурация
- Dockerfile, Makefile, CI-конфигурация

## Conventions

- Код на Go, стандартная идиоматика языка
- Имена в camelCase для локальных переменных, PascalCase для экспортируемых
- Ошибки возвращаются явно, паттерн `if err != nil`
- Ошибки оборачивать в `fmt.Errorf()`
- Пиши читаемые код, разделяй логические блоки пустыми строками, отмечай их функционал однострочным комментарием - важнее понять, не что делает код, а зачем
- Перед функцией, методом пиши небольшой комментарий, что они делают

## Testing

Есть 3 вида тестов:
- Unit-тесты - тестирование конкретной функции
- Компонентные тесты - тестирование нескольких функций в связке. Например, работа всего хэндлера (от вызова до получения итогово результата)
- Интеграционные тесты - тестирование взаимодействия нескольких сервисов сразу. Например, от фронтенда до финального результата. (В этом проекте, скорее всего, таких тестов не будет)
