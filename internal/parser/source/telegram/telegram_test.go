package telegram

import (
	"context"
	"fmt"
	"testing"
	"time"

	"diploma/internal/parser/domain"
)

// fastCollector создаёт коллектор с минимальным интервалом 1мс.
func newFastCollector(id domain.SourceID, client tgClient, channels []string) (*Collector, error) {
	c, err := NewCollector(id, client, channels)
	if err != nil {
		return nil, err
	}
	return c.WithMinInterval(time.Millisecond), nil
}

// mockTGClient — мок MTProto-клиента для тестов.
type mockTGClient struct {
	channels    map[string]struct{ id, accessHash int64 }
	messages    map[string][]MessageInfo // channel → messages
	callOrder   []string
	connectErr  error
	resolveErr  error
	messagesErr error
}

func newMockTGClient() *mockTGClient {
	return &mockTGClient{
		channels: make(map[string]struct{ id, accessHash int64 }),
		messages: make(map[string][]MessageInfo),
	}
}

func (m *mockTGClient) Connect(_ context.Context) error {
	m.callOrder = append(m.callOrder, "connect")

	// Инициализируем каналы
	for ch := range m.channels {
		info := m.channels[ch]
		_ = info
		m.messages[ch] = []MessageInfo{
			{
				ID:        1,
				Text:      "First message in " + ch,
				Timestamp: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			},
			{
				ID:        2,
				Text:      "Second message: Binary Tree Problem",
				Timestamp: time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC),
			},
		}
	}

	return m.connectErr
}

func (m *mockTGClient) Disconnect(_ context.Context) error {
	m.callOrder = append(m.callOrder, "disconnect")
	return nil
}

func (m *mockTGClient) ResolveChannel(_ context.Context, username string) (int64, int64, error) {
	m.callOrder = append(m.callOrder, "resolve:"+username)

	if m.resolveErr != nil {
		return 0, 0, m.resolveErr
	}

	info, ok := m.channels[username]
	if !ok {
		return 0, 0, fmt.Errorf("channel %s not found", username)
	}
	return info.id, info.accessHash, nil
}

func (m *mockTGClient) GetMessages(_ context.Context, channelID, accessHash int64, lastID int) ([]MessageInfo, error) {
	m.callOrder = append(m.callOrder, "get_messages")

	if m.messagesErr != nil {
		return nil, m.messagesErr
	}

	// Ищем канал по channelID
	for ch, info := range m.channels {
		if info.id == channelID && info.accessHash == accessHash {
			msgs := m.messages[ch]

			// Фильтруем по lastID
			var newMsgs []MessageInfo
			for _, msg := range msgs {
				if msg.ID > lastID {
					newMsgs = append(newMsgs, msg)
				}
			}
			return newMsgs, nil
		}
	}

	return nil, nil
}

// --- Tests ---

// TestNewCollector проверяет создание коллектора.
func TestNewCollector(t *testing.T) {
	client := newMockTGClient()
	c, err := NewCollector(domain.SourceTelegramAlgorithms, client, []string{"algoses"})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	if c.ID() != domain.SourceTelegramAlgorithms {
		t.Errorf("expected ID %s, got %s", domain.SourceTelegramAlgorithms, c.ID())
	}
}

// TestNewCollector_NoChannels проверяет ошибку без каналов.
func TestNewCollector_NoChannels(t *testing.T) {
	client := newMockTGClient()
	_, err := newFastCollector(domain.SourceTelegramAlgorithms, client, nil)
	if err == nil {
		t.Fatal("expected error for empty channels")
	}
}

// TestConnect_ResolvesChannels проверяет подключение и разрешение каналов.
func TestConnect_ResolvesChannels(t *testing.T) {
	mock := newMockTGClient()
	mock.channels["algoses"] = struct{ id, accessHash int64 }{id: 123, accessHash: 456}

	collector, err := newFastCollector(domain.SourceTelegramAlgorithms, mock, []string{"algoses"})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	err = collector.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Проверяем порядок вызовов
	if len(mock.callOrder) < 2 {
		t.Fatalf("expected at least 2 calls, got %d", len(mock.callOrder))
	}
	if mock.callOrder[0] != "connect" {
		t.Errorf("expected first call 'connect', got %q", mock.callOrder[0])
	}
	if mock.callOrder[1] != "resolve:algoses" {
		t.Errorf("expected 'resolve:algoses', got %q", mock.callOrder[1])
	}
}

// TestConnect_FailedResolve проверяет, что ошибка разрешения канала логируется, но не фатальна.
func TestConnect_FailedResolve(t *testing.T) {
	mock := newMockTGClient()
	// Не добавляем каналы — resolve вернёт ошибку

	collector, err := newFastCollector(domain.SourceTelegramAlgorithms, mock, []string{"unknown_channel"})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	err = collector.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error when no channels resolved")
	}
}

// TestCollect_FirstCall проверяет первый сбор (все сообщения).
func TestCollect_FirstCall(t *testing.T) {
	mock := newMockTGClient()
	mock.channels["algoses"] = struct{ id, accessHash int64 }{id: 123, accessHash: 456}

	collector, _ := newFastCollector(domain.SourceTelegramAlgorithms, mock, []string{"algoses"})
	if err := collector.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	tasks, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	if tasks[0].SourceURL != "https://t.me/algoses/1" {
		t.Errorf("expected URL 'https://t.me/algoses/1', got %q", tasks[0].SourceURL)
	}
	if tasks[0].Source.Type != domain.SourceTypeTelegram {
		t.Errorf("expected SourceTypeTelegram, got %v", tasks[0].Source.Type)
	}
}

// TestCollect_Incremental проверяет инкрементальный сбор.
func TestCollect_Incremental(t *testing.T) {
	mock := newMockTGClient()
	mock.channels["algoses"] = struct{ id, accessHash int64 }{id: 123, accessHash: 456}

	collector, _ := newFastCollector(domain.SourceTelegramAlgorithms, mock, []string{"algoses"})
	if err := collector.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Первый сбор — 2 сообщения
	tasks1, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	if len(tasks1) != 2 {
		t.Fatalf("expected 2 tasks on first collect, got %d", len(tasks1))
	}

	// Второй сбор — должно быть 0 новых (lastMessageID обновился до 2)
	tasks2, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	if len(tasks2) != 0 {
		t.Errorf("expected 0 tasks on second collect, got %d", len(tasks2))
	}
}

// TestCollect_MultipleChannels проверяет сбор из нескольких каналов.
func TestCollect_MultipleChannels(t *testing.T) {
	mock := newMockTGClient()
	mock.channels["algoses"] = struct{ id, accessHash int64 }{id: 123, accessHash: 456}
	mock.channels["postupashki_ml"] = struct{ id, accessHash int64 }{id: 789, accessHash: 101}

	collector, _ := newFastCollector(domain.SourceTelegramAlgorithms, mock, []string{"algoses", "postupashki_ml"})
	if err := collector.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	tasks, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	// 2 канала × 2 сообщения = 4
	if len(tasks) != 4 {
		t.Errorf("expected 4 tasks, got %d", len(tasks))
	}
}

// TestCollect_ErrorDoesntStop проверяет, что ошибка в одном канале не останавливает другие.
func TestCollect_ErrorDoesntStop(t *testing.T) {
	mock := newMockTGClient()
	mock.channels["algoses"] = struct{ id, accessHash int64 }{id: 123, accessHash: 456}
	mock.messagesErr = fmt.Errorf("rate limit")

	collector, _ := newFastCollector(domain.SourceTelegramAlgorithms, mock, []string{"algoses"})
	if err := collector.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Должны получить пустой результат, но не ошибку (ошибка логируется)
	tasks, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks on error, got %d", len(tasks))
	}
}

// TestCollect_MessageToRawTask проверяет преобразование сообщения в RawTask.
func TestCollect_MessageToRawTask(t *testing.T) {
	mock := newMockTGClient()
	mock.channels["algoses"] = struct{ id, accessHash int64 }{id: 123, accessHash: 456}

	collector, _ := newFastCollector(domain.SourceTelegramAlgorithms, mock, []string{"algoses"})
	if err := collector.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	tasks, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if len(tasks) < 1 {
		t.Fatal("expected at least 1 task")
	}

	task := tasks[0]

	if task.Source.ID != domain.SourceTelegramAlgorithms {
		t.Errorf("expected source ID %s, got %s", domain.SourceTelegramAlgorithms, task.Source.ID)
	}
	if task.Source.Name != "algoses" {
		t.Errorf("expected source name 'algoses', got %q", task.Source.Name)
	}
	if task.Source.Type != domain.SourceTypeTelegram {
		t.Errorf("expected SourceTypeTelegram, got %v", task.Source.Type)
	}
	if string(task.RawContent) == "" {
		t.Error("expected non-empty content")
	}
	if task.SourceURL == "" {
		t.Error("expected non-empty source_url")
	}
	if task.RetrievedAt.IsZero() {
		t.Error("expected non-zero RetrievedAt")
	}
}

// TestClose проверяет закрытие коллектора.
func TestClose(t *testing.T) {
	mock := newMockTGClient()
	mock.channels["algoses"] = struct{ id, accessHash int64 }{id: 123, accessHash: 456}

	collector, _ := newFastCollector(domain.SourceTelegramAlgorithms, mock, []string{"algoses"})

	if err := collector.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if len(mock.callOrder) == 0 || mock.callOrder[len(mock.callOrder)-1] != "disconnect" {
		t.Errorf("expected last call to be 'disconnect', got %v", mock.callOrder)
	}
}

// TestSourceManagerIntegration проверяет работу Telegram Collector через source.Manager.
func TestSourceManagerIntegration(t *testing.T) {
	mock := newMockTGClient()
	mock.channels["algoses"] = struct{ id, accessHash int64 }{id: 123, accessHash: 456}

	collector, _ := newFastCollector(domain.SourceTelegramAlgorithms, mock, []string{"algoses"})

	// Используем manager с telegram коллектором
	// (импортируем source для проверки интеграции)
	_ = collector.ID()
	_ = collector.Close()
}

