# Архитектура task-hunter

```text
cron/admin API -> collection_jobs -> worker
                                  ├─ gotd/td Telegram range reader
                                  ├─ Codeforces direct URL adapter
                                  ├─ LeetCode direct URL adapter
                                  └─ CodeRun direct URL adapter
                                            |
                                            v
                              tasks-it internal batch ingestion
                                            |
                                            v
                              task_candidates -> moderation -> published programming task
```

## Владение данными

`task-hunter` хранит только параметры и результаты jobs, безопасные ошибки источников, lease и Telegram checkpoint. `tasks-it` хранит candidate payload, immutable provenance и опубликованные версии задач. Сервисы не читают таблицы друг друга.

## Надёжность

Job имеет статусы `queued`, `running`, `succeeded`, `partial`, `failed`. Источники выполняются независимо. Lease продлевается активным worker; просроченный lease позволяет продолжить job после рестарта. Уникальный idempotency key исключает повторную постановку manual и cron jobs.

Checkpoint обновляется только после подтверждённого `imported` или `duplicate` ответа tasks-it и только для scheduled/bootstrap job. При лимите он продвигается до последнего реально подтверждённого message ID. Ручной исторический диапазон checkpoint не меняет.

## Безопасность

Website URL сначала приводится к канонической HTTPS-ссылке известного хоста. Адаптер заново строит исходящий запрос и запрещает redirect, поэтому произвольный SSRF endpoint недоступен. Service tokens и Telegram session не логируются. Runtime-контейнер не выполняет миграции и работает без root.
