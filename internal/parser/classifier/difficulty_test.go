package classifier

import (
	"context"
	"testing"

	"diploma/internal/parser/domain"
)

// TestClassifyDifficulty проверяет определение сложности через RuleBasedClassifier.

func TestClassifyDifficulty_EasyByMarkers(t *testing.T) {
	c := NewRuleBasedClassifier()
	ctx := context.Background()

	task := &domain.Task{
		Title:       "Простая задача",
		Description: "Самая простая задача на базовые операции. Подойдёт для начинающих.",
	}

	if err := c.Classify(ctx, task); err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if task.Difficulty != domain.DifficultyEasy {
		t.Errorf("expected Easy, got %s", task.Difficulty.String())
	}
}

func TestClassifyDifficulty_HardByMarkers(t *testing.T) {
	c := NewRuleBasedClassifier()
	ctx := context.Background()

	task := &domain.Task{
		Title:       "Сложная задача",
		Description: "Это продвинутая задача на LCA и heavy-light декомпозицию. Требуется реализовать max flow алгоритм.",
	}

	if err := c.Classify(ctx, task); err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if task.Difficulty != domain.DifficultyHard {
		t.Errorf("expected Hard, got %s", task.Difficulty.String())
	}
}

func TestClassifyDifficulty_MediumByDefault(t *testing.T) {
	c := NewRuleBasedClassifier()
	ctx := context.Background()

	task := &domain.Task{
		Title: "Обычная задача",
		Description: "Реализуйте функцию, которая принимает на вход список целых чисел и возвращает их сумму. " +
			"Используйте итеративный подход. Обработайте случай пустых данных. " +
			"Напишите проверки для корректности работы функции. " +
			"Это стандартная задача на операции с данными. " +
			"Добавим ещё несколько предложений, чтобы длина описания была достаточной, " +
			"но при этом она оставалась в диапазоне средних значений.",
	}

	if err := c.Classify(ctx, task); err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if task.Difficulty != domain.DifficultyMedium {
		t.Errorf("expected Medium, got %s", task.Difficulty.String())
	}
}

func TestClassifyDifficulty_EasyByLength(t *testing.T) {
	c := NewRuleBasedClassifier()
	ctx := context.Background()

	task := &domain.Task{
		Title:       "A+B",
		Description: "Найдите сумму двух чисел.",
	}

	if err := c.Classify(ctx, task); err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if task.Difficulty != domain.DifficultyEasy {
		t.Errorf("expected Easy (short description), got %s", task.Difficulty.String())
	}
}

func TestClassifyDifficulty_HardByLength(t *testing.T) {
	c := NewRuleBasedClassifier()
	ctx := context.Background()

	// Создаём длинное описание (> 2000 символов)
	longDesc := make([]byte, 2500)
	for i := range longDesc {
		longDesc[i] = 'x'
	}

	task := &domain.Task{
		Title:       "Complex problem",
		Description: string(longDesc),
	}

	if err := c.Classify(ctx, task); err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if task.Difficulty != domain.DifficultyHard {
		t.Errorf("expected Hard (long description), got %s", task.Difficulty.String())
	}
}

func TestClassifyDifficulty_EmptyDescription(t *testing.T) {
	c := NewRuleBasedClassifier()
	ctx := context.Background()

	task := &domain.Task{
		Title:       "",
		Description: "",
	}

	if err := c.Classify(ctx, task); err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if task.Difficulty != domain.DifficultyMedium {
		t.Errorf("expected Medium for empty description, got %s", task.Difficulty.String())
	}
}

// TestDifficultyAnalyzer_Direct проверяет DifficultyAnalyzer напрямую.

func TestDifficultyAnalyzer_EasyMarker(t *testing.T) {
	a := NewDifficultyAnalyzer()
	d := a.Analyze("Простая задача", "Базовый пример для начинающих", nil)
	if d != domain.DifficultyEasy {
		t.Errorf("expected Easy, got %s", d.String())
	}
}

func TestDifficultyAnalyzer_HardMarker(t *testing.T) {
	a := NewDifficultyAnalyzer()
	d := a.Analyze("", "LCA и heavy-light декомпозиция", nil)
	if d != domain.DifficultyHard {
		t.Errorf("expected Hard, got %s", d.String())
	}
}

func TestDifficultyAnalyzer_MediumByDefault(t *testing.T) {
	a := NewDifficultyAnalyzer()
	// Описание средней длины (300-2000 символов), без маркеров
	desc := "Напишите программу, которая читает данные из источника и выводит результат. " +
		"Программа должна корректно обрабатывать входные форматы. " +
		"Реализуйте функцию валидации: если данные некорректны, выведите сообщение. " +
		"Если в данных есть пропуски, обработайте их с использованием значения по умолчанию. " +
		"Это рядовая задача на работу с данными и стандартными операциями. " +
		"Добавим ещё несколько слов, чтобы перевалить за отметку в триста символов " +
		"и убедиться, что классификатор корректно определяет средний уровень."
	d := a.Analyze("Task", desc, nil)
	if d != domain.DifficultyMedium {
		t.Errorf("expected Medium, got %s", d.String())
	}
}

func TestDifficultyAnalyzer_EmptyTask(t *testing.T) {
	a := NewDifficultyAnalyzer()
	d := a.Analyze("", "", nil)
	if d != domain.DifficultyMedium {
		t.Errorf("expected Medium for empty, got %s", d.String())
	}
}

func TestDifficultyAnalyzer_EasyMarkerOverridesLength(t *testing.T) {
	a := NewDifficultyAnalyzer()
	// Длинный текст, но с easy маркерами
	longEasy := make([]byte, 2500)
	copy(longEasy, "Это базовая задача для начинающих. ")
	for i := 15; i < len(longEasy); i++ {
		longEasy[i] = 'x'
	}

	d := a.Analyze("Простая тема", string(longEasy), nil)
	if d != domain.DifficultyEasy {
		t.Errorf("expected Easy (markers override length), got %s", d.String())
	}
}
