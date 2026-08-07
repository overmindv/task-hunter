# Story 4: Telegram Source — адаптер Telegram-каналов

**Для чего:** Реализовать сбор задач из Telegram-каналов через MTProto-клиент (`gotd/td`): подключение, чтение постов, преобразование в RawTask.

---

## Task 4.1: Реализовать MTProto-клиент для чтения каналов

**Для чего:** Создать пакет `internal/parser/source/telegram`, реализующий подключение к Telegram через MTProto и чтение новых сообщений из каналов.

**Подробное описание:**

1. Создать директорию `internal/parser/source/telegram/`
2. Определить структуру `Collector` (имплементирует `source.Collector`)
3. Поля:
   - `api *telegram.Client` (из `gotd/td`)
   - `channels []string` — список каналов
   - `sessionStorage` — для сохранения сессии (файл или in-memory)
   - `lastMessageIDs map[string]int` — ID последнего прочитанного сообщения (для инкрементального сбора)
4. Реализовать `NewCollector(cfg config.SourceConfig) (*Collector, error)`:
   - Создать клиент `gotd/td` с API ID и API Hash (из конфига)
   - Настроить хранение сессии через `gotd/td/session`
5. Реализовать метод `Connect(ctx) error`:
   - Аутентификация через MTProto
   - Присоединение к каналам (resolve username → chat)
6. Реализовать метод `Collect(ctx) ([]domain.RawTask, error)`:
   - Для каждого канала: получить новые сообщения (после `lastMessageID`)
   - Каждое сообщение → `domain.RawTask` (текст + вложения как байты)
   - Обновить `lastMessageID`
   - Поддерживать rate limiting (не чаще 1 запроса в секунду)
7. Хранить API ID / Hash и номера каналов в конфиге через `Params`

**Тесты:**
- **Компонентный:** с тестовым Telegram-аккаунтом или моком MTProto-сервера
- **Unit:** мок на интерфейс `gotd/td` проверяет вызов Collect без реального подключения
- **Unit:** парсинг ответа MTProto в RawTask
- **Unit:** корректное сохранение и восстановление lastMessageID

**Сторипоинты:** 3

---

## Task 4.2: Реализовать парсер сообщений Telegram в RawTask

**Для чего:** Создать утилиты для извлечения текста задачи из сообщений Telegram разных форматов (простой текст, файлы, форматирование).

**Подробное описание:**

1. В пакете `source/telegram/` создать файл `message_parser.go`
2. Реализовать `ParseMessage(msg *telegram.Message) domain.RawTask`:
   - Извлечение текста из сообщения (обычный текст)
   - Извлечение текста из форматированных сообщений (Markdown/HTML entities)
   - Обработка прикреплённых файлов:
     - Если файл — изображение → OCR не делаем, сохраняем только наличие
     - Если файл — текст/код → извлекаем содержимое
   - Заполнение `SourceURL` как ссылки на пост (`https://t.me/c/{chat_id}/{message_id}`)
   - Генерация временного `SourceHash` из text + message_id
3. Поддержка папки `source/telegram/testdata/` с golden-файлами:
   - Фикстуры: `message_simple.txt`, `message_with_code.txt`, `message_with_image.txt`

**Тесты:**
- **Unit (golden):** парсинг простого текстового сообщения
- **Unit (golden):** парсинг сообщения с Markdown-форматированием
- **Unit (golden):** парсинг сообщения с прикреплённым файлом
- **Unit:** корректная генерация SourceURL для разных форматов chat_id

**Сторипоинты:** 2
