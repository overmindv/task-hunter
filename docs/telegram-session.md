# Подготовка Telegram session

`task-hunter` не выполняет интерактивную авторизацию при запуске. MTProto session создаётся один раз локально и помещается в закрытый writable volume. Файл session эквивалентен учётным данным: его нельзя коммитить, отправлять в логи или хранить в ConfigMap/Secret в открытом виде.

## Создание session

```bash
mkdir -p .local/telegram
chmod 700 .local/telegram
export TELEGRAM_API_ID=123456
export TELEGRAM_API_HASH='значение с my.telegram.org'
export TELEGRAM_PHONE='+79990000000'
export TELEGRAM_SESSION_PATH="$PWD/.local/telegram/telegram.session"
# Только если на аккаунте включён пароль 2FA:
export TELEGRAM_2FA_PASSWORD='пароль'
go run ./cmd/telegram-session
chmod 600 .local/telegram/telegram.session
```

Код подтверждения вводится в терминале и не сохраняется. После успешной проверки удалите из shell environment `TELEGRAM_PHONE` и `TELEGRAM_2FA_PASSWORD`.

## Docker Compose

В полном локальном стеке session создаётся одной интерактивной командой из каталога `infra`:

```bash
TELEGRAM_PHONE=+79990000000 make telegram-login
```

Перед этим заполните `TASK_HUNTER_TELEGRAM_API_ID` и `TASK_HUNTER_TELEGRAM_API_HASH` в `infra/.env`, получив их в разделе **API development tools** на `https://my.telegram.org`. После создания session включите `TASK_HUNTER_TELEGRAM_ENABLED=true` и выполните `make up`.

## Kubernetes

После создания pod скопируйте session в PVC и перезапустите Deployment:

```bash
POD="$(kubectl -n overmindv get pod -l app=task-hunter -o jsonpath='{.items[0].metadata.name}')"
kubectl -n overmindv cp .local/telegram/telegram.session "$POD:/var/lib/task-hunter/telegram.session"
kubectl -n overmindv exec "$POD" -- chmod 600 /var/lib/task-hunter/telegram.session
kubectl -n overmindv rollout restart deployment/task-hunter
```

Проверка выполняется по `/ready`, который остаётся `503`, пока session-файл отсутствует или пуст, затем через ручной job на одном allowlist-канале. Если bootstrap успел завершиться ошибкой до подготовки session, перезапустите `task-hunter`: failed bootstrap будет безопасно возвращён в очередь. При компрометации session завершите все другие сессии в Telegram, удалите файл из volume и создайте его заново.
