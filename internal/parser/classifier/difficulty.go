package classifier

import (
	"strings"
	"unicode/utf8"

	"github.com/overmindv/task-hunter/internal/parser/domain"
)

// DifficultyAnalyzer определяет сложность задачи на основе текста.
type DifficultyAnalyzer struct {
	easyKeywords []string
	hardKeywords []string
}

// NewDifficultyAnalyzer создаёт анализатор сложности.
func NewDifficultyAnalyzer() *DifficultyAnalyzer {
	return &DifficultyAnalyzer{
		easyKeywords: []string{
			"простой", "лёгк", "базов", "элементар",
			"введение", "easy", "beginner", "прост",
		},
		hardKeywords: []string{
			"сложн", "продвинут", "lca", "heavy-light",
			"suffix array", "max flow", "np-полн",
			"np-hard", "эксперт", "advanced", "hard",
			"heavy light", "suffix automaton",
		},
	}
}

// Analyze определяет сложность задачи по описанию.
//
// Стратегия:
//  1. Поиск ключевых слов сложности (easy/hard) в тексте
//  2. Анализ длины описания (короткое → Easy, длинное → склоняется к Hard)
//  3. Default: Medium
func (a *DifficultyAnalyzer) Analyze(title, description string, tags []domain.Tag) domain.Difficulty {
	text := strings.ToLower(title + " " + description)
	for _, tag := range tags {
		text += " " + strings.ToLower(string(tag))
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return domain.DifficultyMedium
	}

	// Подсчитываем совпадения с easy и hard маркерами
	easyCount := countKeywordMatches(text, a.easyKeywords)
	hardCount := countKeywordMatches(text, a.hardKeywords)

	// Высокий приоритет у hard маркеров
	if hardCount > easyCount {
		return domain.DifficultyHard
	}

	// Easy маркеры
	if easyCount > hardCount {
		return domain.DifficultyEasy
	}

	// Если маркеры равны или их нет — анализ длины
	descLen := utf8.RuneCountInString(description)

	switch {
	case descLen == 0:
		return domain.DifficultyMedium
	case descLen < 300:
		return domain.DifficultyEasy
	case descLen > 2000:
		return domain.DifficultyHard
	default:
		return domain.DifficultyMedium
	}
}
