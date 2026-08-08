// Package classifier реализует классификацию задач по типу и сложности.
//
// На данный момент реализован rule-based классификатор, определяющий тип задачи
// на основе ключевых слов в заголовке, описании и тегах.
// Архитектура предусматривает замену на ML-модель в будущем.
package classifier

import (
	"context"
	"strings"

	"diploma/internal/parser/domain"
)

// Classifier — интерфейс классификатора задач.
type Classifier interface {
	// Classify определяет тип задачи и заполняет task.Type и task.Tags.
	Classify(ctx context.Context, task *domain.Task) error
}

// RuleBasedClassifier определяет тип задачи по ключевым словам.
type RuleBasedClassifier struct {
	// rules — правила для каждого типа задачи.
	rules []typeRule
}

// typeRule содержит набор ключевых слов для одного типа задачи.
type typeRule struct {
	Type     domain.TaskType
	Keywords []string
}

// NewRuleBasedClassifier создаёт классификатор с правилами по умолчанию.
func NewRuleBasedClassifier() *RuleBasedClassifier {
	return &RuleBasedClassifier{
		rules: defaultRules(),
	}
}

// Process реализует pipeline.Processor.
func (c *RuleBasedClassifier) Process(ctx context.Context, _ domain.RawTask, task *domain.Task) error {
	return c.Classify(ctx, task)
}

// Classify определяет тип задачи по ключевым словам.
// Заполняет task.Type и добавляет теги на основе найденных совпадений.
func (c *RuleBasedClassifier) Classify(_ context.Context, task *domain.Task) error {
	if task == nil {
		return nil
	}

	// Собираем текст для анализа: заголовок + описание + теги
	text := strings.ToLower(task.Title + " " + task.Description)
	for _, tag := range task.Tags {
		text += " " + strings.ToLower(string(tag))
	}

	// Если текст пустой — ставим TaskTypeAlgorithm (без паники)
	if strings.TrimSpace(text) == "" {
		task.Type = domain.TaskTypeAlgorithm
		return nil
	}

	// Подсчитываем совпадения для каждого типа
	type matchCount struct {
		typ  domain.TaskType
		count int
	}

	var matches []matchCount
	for _, rule := range c.rules {
		count := countKeywordMatches(text, rule.Keywords)
		if count > 0 {
			matches = append(matches, matchCount{typ: rule.Type, count: count})
		}
	}

	// Если совпадений нет — TaskTypeAlgorithm по умолчанию
	if len(matches) == 0 {
		task.Type = domain.TaskTypeAlgorithm
		return nil
	}

	// Выбираем тип с наибольшим числом совпадений
	// При равенстве — первый по порядку (приоритет выше в списке правил)
	best := matches[0]
	for _, m := range matches[1:] {
		if m.count > best.count {
			best = m
		}
	}

	task.Type = best.typ
	return nil
}

// countKeywordMatches подсчитывает, сколько ключевых слов найдено в тексте.
func countKeywordMatches(text string, keywords []string) int {
	count := 0
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			count++
		}
	}
	return count
}

// defaultRules возвращает правила классификации по умолчанию.
func defaultRules() []typeRule {
	return []typeRule{
		{
			Type: domain.TaskTypeDatabase,
			Keywords: []string{
				"sql", "select", "join", "индекс", "нормализация",
				"транзакция", "acid", "nosql", "запрос", "таблиц",
				"insert", "update", "delete", "foreign key", "primary key",
			},
		},
		{
			Type: domain.TaskTypeBackend,
			Keywords: []string{
				"api", "rest", "http", "middleware", "аутентификация",
				"авторизация", "jwt", "grpc", "сервер", "эндпоинт",
				"handler", "роутинг", "запрос", "ответ", "микросервис",
			},
		},
		{
			Type: domain.TaskTypeInfrastructure,
			Keywords: []string{
				"docker", "kubernetes", "ci/cd", "деплой", "контейнер",
				"yaml", "dockerfile", "kubernetes", "пайплайн",
				"terraform", "ansible", "мониторинг", "prometheus",
			},
		},
		{
			Type: domain.TaskTypeTesting,
			Keywords: []string{
				"unit test", "mock", "stub", "test coverage",
				"tdd", "интеграционное тестирование", "юнит-тест",
				"тест", "assert", "test",
			},
		},
		{
			Type: domain.TaskTypeCodeReview,
			Keywords: []string{
				"code review", "рефакторинг", "code smell",
				"clean code", "solid", "рецензи", "код-ревью",
			},
		},
		{
			Type: domain.TaskTypeDataStructures,
			Keywords: []string{
				"стек", "очередь", "хеш-таблица", "heap", "куча",
				"trie", "union-find", "dsu", "дерево отрезков",
				"bit", "fenwick", "граф", "связный список",
				"linked list", "hash map", "hash set",
			},
		},
		{
			Type: domain.TaskTypeAlgorithm,
			Keywords: []string{
				"массив", "список", "дерево", "граф", "сортировка",
				"dp", "динамическое", "бинарный поиск", "bfs", "dfs",
				"o(n)", "o(log n)", "алгоритм", "рекурсия",
				"quick sort", "merge sort", "binary search",
				"two pointers", "two pointers", "жадн",
			},
		},
	}
}

