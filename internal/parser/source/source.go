// Package source предоставляет интерфейсы и фабрику для адаптеров источников задач.
package source

import (
	"context"
	"fmt"

	"github.com/overmindv/task-hunter/internal/parser/domain"
)

// Collector — интерфейс для сбора задач из внешнего источника.
type Collector interface {
	// ID возвращает уникальный идентификатор источника.
	ID() domain.SourceID

	// Connect устанавливает соединение с источником.
	// Для Telegram: аутентификация + присоединение к каналам.
	// Для веб-сайтов: может быть no-op (HTTP-запросы без постоянного соединения).
	Connect(ctx context.Context) error

	// Collect собирает новые задачи с момента последнего вызова.
	// При первом вызове собирает все доступные (ограничено лимитом).
	// При повторных — только новые с момента последнего сбора.
	Collect(ctx context.Context) ([]domain.RawTask, error)

	// Close закрывает соединение с источником.
	Close() error
}

// Manager управляет всеми подключёнными коллекторами.
type Manager struct {
	collectors map[domain.SourceID]Collector
}

// NewManager создаёт Manager с заданными коллекторами.
func NewManager(collectors ...Collector) *Manager {
	m := &Manager{collectors: make(map[domain.SourceID]Collector)}
	for _, c := range collectors {
		m.collectors[c.ID()] = c
	}
	return m
}

// CollectAll собирает задачи со всех коллекторов.
// Ошибка в одном коллекторе не останавливает сбор с остальных.
func (m *Manager) CollectAll(ctx context.Context) []CollectResult {
	var results []CollectResult

	for id, c := range m.collectors {
		tasks, err := c.Collect(ctx)
		results = append(results, CollectResult{
			SourceID: id,
			Tasks:    tasks,
			Err:      err,
		})
	}

	return results
}

// ConnectAll подключает все коллекторы.
func (m *Manager) ConnectAll(ctx context.Context) error {
	for id, c := range m.collectors {
		if err := c.Connect(ctx); err != nil {
			return fmt.Errorf("connect %s: %w", id, err)
		}
	}
	return nil
}

// CloseAll закрывает все коллекторы.
func (m *Manager) CloseAll() error {
	for id, c := range m.collectors {
		if err := c.Close(); err != nil {
			return fmt.Errorf("close %s: %w", id, err)
		}
	}
	return nil
}

// CollectResult — результат сбора с одного источника.
type CollectResult struct {
	SourceID domain.SourceID
	Tasks    []domain.RawTask
	Err      error
}
