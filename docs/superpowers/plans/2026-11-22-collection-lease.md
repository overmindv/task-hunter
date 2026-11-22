# OVM-235 — отмена сбора после потери аренды

Исполнитель: Alexander Vasyukov. Изолированная ветка codex/ovm-235-collection-lease.

## Причина и решение
Worker передаёт исходный context в обработку; ошибка RenewLease только логируется. StartSource/FinishSource/FinishJob/UpsertCheckpoint не проверяют владельца. Просроченную аренду можно продлить. Исправляем существующее поведение, публичные API не меняем.

Каждый Claim получает уникальный token в существующем lease_owner (owner/UUID). Token передаётся worker внутри Job с json:"-". Все изменения состояния выполняются в транзакции с блокировкой строки job, проверкой token, running и непросроченной аренды. Потеря аренды или ошибка продления отменяет общий processing context; запрос продления ограничен по времени. Старый worker не завершает job/источник и не двигает checkpoint. Миграция не нужна. Старые бинарники не соблюдают fencing: rollout требует остановить старые workers перед запуском новых.

## Коммит
`[OVM-235] fence collection writes and cancel lost leases`: модели, store, worker, component-тесты, Makefile и этот документ — 6 файлов. Изначальные две группы объединены: смена внутренних сигнатур и отмена worker образуют один проверяемый срез. Makefile включает новые component-тесты в существующий CI target.

## Проверки
Baseline collection tests; регрессия до исправления; go test -race ./...; component в отдельном временном PostgreSQL; make lint; go build ./...; независимый review. Не использовать общий стек/БД. Интеграционные проверки — только локальные, без вызова внешних источников. Публичные контракты tasks/frontend/gateway неизменны.

## Ограничения
Уже принятый удалённым tasks HTTP-запрос нельзя отозвать; импорт остаётся идемпотентным. Fencing гарантирует локальное состояние очереди и checkpoints. Отдельный checkout не содержит sibling docs; использован исходный harness /Users/alevasyukovv/Desktop/Overmindv/docs/harness/README.md, специализированной спецификации collection lease в нём не найдено.

## Результат и проверки — 2026-11-22
- Baseline `go test ./internal/collection/...` — PASS.
- RED: TestExpiredLeaseCannotRenew воспроизводил оживление истёкшей аренды. После исправления PASS.
- RED: TestWorkerCancelsAfterRenewalFailure воспроизводил отсутствие отмены при смене owner/ошибке БД. После исправления PASS.
- PostgreSQL 16, отдельный контейнер ovm235-postgres и отдельные схемы: expiry, повторный owner, stale checkpoint/source/job writes, конкурентный finish, отмена чтения и импорта, timeout блокировки продления, ожидание lock до expiry, успешное продление и завершение — PASS с race detector.
- `go test -race ./...`, `make test-component GOTESTFLAGS='-race -count=1 -timeout=120s'` и `go build ./...` — exit 0 на Go 1.26.11 (также проверены на установленном Go 1.27.1).
- `make lint GOLANGCILINT=/tmp/ovm235-tools/golangci-lint` и lint с `--build-tags=component ./internal/collection` — exit 0, 0 issues; версия 2.11.4 как в CI, PATH с Go 1.26.11. Системный 2.15/Go 1.27 давал отличающиеся замечания; старый линтер не читает export data Go 1.27. Настройки проекта не ослаблялись.
- Независимый review не нашёл ошибок; оба предложенных теста успешного продления и expiry за блокировкой добавлены.
- Внешние источники/Telegram/tasks по сети не вызывались. Публичные API и миграции не менялись.
- Ветка локальная, не опубликована и не слита; следующий шаг — review/PR и CI, затем merge. Для rollout остановить старые workers, затем запускать новую версию. Уже принятый удалённой стороной импорт не отзывается, повторы дедуплицируются существующим контрактом.
