// Package domain содержит базовые модели данных модуля парсинга задач.
package domain

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// TaskType — тип задачи по тематике.
type TaskType int

const (
	// TaskTypeAlgorithm обозначает алгоритмическую задачу.
	TaskTypeAlgorithm TaskType = iota // Алгоритмы
	// TaskTypeDataStructures обозначает задачу по структурам данных.
	TaskTypeDataStructures // Структуры данных
	// TaskTypeDatabase обозначает задачу по базам данных.
	TaskTypeDatabase // Базы данных
	// TaskTypeBackend обозначает backend-задачу.
	TaskTypeBackend // Бэкенд
	// TaskTypeInfrastructure обозначает инфраструктурную задачу.
	TaskTypeInfrastructure // Поднятие инфраструктуры
	// TaskTypeTesting обозначает задачу по тестированию.
	TaskTypeTesting // Тестирование
	// TaskTypeCodeReview обозначает задачу на ревью кода.
	TaskTypeCodeReview // Ревью кода
)

// String возвращает человекочитаемое название типа задачи.
func (t TaskType) String() string {
	switch t {
	case TaskTypeAlgorithm:
		return "algorithm"
	case TaskTypeDataStructures:
		return "data_structures"
	case TaskTypeDatabase:
		return "database"
	case TaskTypeBackend:
		return "backend"
	case TaskTypeInfrastructure:
		return "infrastructure"
	case TaskTypeTesting:
		return "testing"
	case TaskTypeCodeReview:
		return "code_review"
	default:
		return "unknown"
	}
}

// Difficulty — уровень сложности задачи.
type Difficulty int

const (
	// DifficultyUnknown означает отсутствие точной сложности источника.
	DifficultyUnknown Difficulty = iota // Не определена
	// DifficultyEasy обозначает лёгкую задачу.
	DifficultyEasy // Лёгкая
	// DifficultyMedium обозначает задачу средней сложности.
	DifficultyMedium // Средняя
	// DifficultyHard обозначает сложную задачу.
	DifficultyHard // Сложная
)

// String возвращает человекочитаемое название сложности.
func (d Difficulty) String() string {
	switch d {
	case DifficultyEasy:
		return "easy"
	case DifficultyMedium:
		return "medium"
	case DifficultyHard:
		return "hard"
	default:
		return "unknown"
	}
}

// SourceType — тип источника задач.
type SourceType int

const (
	// SourceTypeTelegram обозначает сообщение Telegram.
	SourceTypeTelegram SourceType = iota // Telegram-канал
	// SourceTypeWebsite обозначает HTML или Markdown сайта.
	SourceTypeWebsite // Веб-сайт
	// SourceTypeAPI обозначает ответ публичного API.
	SourceTypeAPI // API (публичный)
	// SourceTypeManual обозначает ручной ввод.
	SourceTypeManual // Ручной ввод
)

// String возвращает человекочитаемое название типа источника.
func (s SourceType) String() string {
	switch s {
	case SourceTypeTelegram:
		return "telegram"
	case SourceTypeWebsite:
		return "website"
	case SourceTypeAPI:
		return "api"
	case SourceTypeManual:
		return "manual"
	default:
		return "unknown"
	}
}

// SourceID — уникальный идентификатор источника.
type SourceID string

const (
	// SourceTelegramAnalytics обозначает канал аналитических задач.
	SourceTelegramAnalytics SourceID = "telegram_analytics" // @analytic_postupashki
	// SourceTelegramML обозначает канал ML-задач.
	SourceTelegramML SourceID = "telegram_ml" // @postupashki_ml
	// SourceTelegramAlgorithms обозначает канал алгоритмов.
	SourceTelegramAlgorithms SourceID = "telegram_algorithms" // @algoses
	// SourceLeetCode обозначает leetcode.com.
	SourceLeetCode SourceID = "leetcode" // leetcode.com
	// SourceCodeforces обозначает codeforces.com.
	SourceCodeforces SourceID = "codeforces" // codeforces.com
	// SourceCodeRun обозначает coderun.yandex.ru.
	SourceCodeRun SourceID = "coderun" // coderun.yandex.ru
	// SourceManual обозначает ручной источник.
	SourceManual SourceID = "manual" // Ручной ввод
)

// Tag — тематический тег задачи.
type Tag string

// Source — информация об источнике задачи.
type Source struct {
	ID   SourceID   // Уникальный идентификатор источника
	Name string     // Человекочитаемое название
	Type SourceType // Тип источника
}

// Example — пример ввода/вывода к задаче.
type Example struct {
	Input       string // Входные данные
	Output      string // Ожидаемый вывод
	Explanation string // Пояснение (опционально)
}

// RawTask — сырые данные, полученные от адаптера источника.
// Используется как входной формат для пайплайна обработки.
type RawTask struct {
	Source      Source     // Источник
	RawContent  []byte     // Оригинальное содержимое (текст, HTML, JSON)
	SourceURL   string     // Прямая ссылка на оригинал
	RetrievedAt time.Time  // Когда получены данные
	Title       string     // Заголовок, извлечённый адаптером источника
	Statement   string     // Условие, извлечённое адаптером источника
	Examples    []Example  // Открытые примеры из источника
	Constraints []string   // Ограничения из источника
	Difficulty  Difficulty // Сложность из источника
	Tags        []Tag      // Теги из источника
}

// Task — нормализованная задача в едином формате платформы.
type Task struct {
	ID          string     // UUID задачи
	Title       string     // Заголовок задачи
	Description string     // Условие задачи
	Examples    []Example  // Примеры ввода/вывода
	Constraints []string   // Ограничения на входные данные
	Source      Source     // Источник происхождения
	SourceURL   string     // Прямая ссылка на оригинал
	SourceHash  string     // Хеш содержимого (для дедупликации)
	Type        TaskType   // Тип задачи
	Difficulty  Difficulty // Сложность
	Tags        []Tag      // Тематические теги
	CreatedAt   time.Time  // Когда задача создана
	UpdatedAt   time.Time  // Когда задача обновлена
}

// Validate проверяет обязательные поля задачи.
// Возвращает ошибку, если задача не проходит валидацию.
func (t *Task) Validate() error {
	if t.Title == "" {
		return fmt.Errorf("title is required")
	}
	if t.Description == "" {
		return fmt.Errorf("description is required")
	}
	if t.SourceURL == "" {
		return fmt.Errorf("source_url is required")
	}
	if t.SourceHash == "" {
		return fmt.Errorf("source_hash is required")
	}
	if t.Source.ID == "" {
		return fmt.Errorf("source_id is required")
	}
	return nil
}

// GenerateSourceHash создаёт SHA-256 хеш содержимого задачи.
// Комбинация: sourceID + sourceURL + содержание задачи.
// Используется для дедупликации: одинаковый контент из разных источников
// получает разные хеши (из-за sourceID), одинаковый контент из одного
// источника — одинаковый хеш.
func GenerateSourceHash(sourceID SourceID, sourceURL string, content []byte) string {
	data := fmt.Sprintf("%s|%s|%s", sourceID, sourceURL, string(content))
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash[:16]) // первые 16 байт — достаточная уникальность
}

// GenerateSourceHashFromTask создаёт хеш для задачи.
func GenerateSourceHashFromTask(t *Task) string {
	return GenerateSourceHash(t.Source.ID, t.SourceURL, []byte(t.Description))
}
