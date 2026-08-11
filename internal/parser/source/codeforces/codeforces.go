// Package codeforces реализует сбор задач с Codeforces через официальный API.
//
// API: https://codeforces.com/api/problemset.problems
// Страница задачи: https://codeforces.com/problemset/problem/{contestId}/{index}
//
// Требования API:
//   - Не более 1 запроса в 2 секунды
//   - User-Agent обязателен
package codeforces

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/overmindv/task-hunter/internal/parser/domain"
	"github.com/overmindv/task-hunter/internal/parser/source"
)

// httpClient — интерфейс HTTP-клиента для мокания в тестах.
type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Collector собирает задачи с Codeforces.
type Collector struct {
	id          domain.SourceID
	client      httpClient
	language    string         // "ru" или "en"
	lastIDs     map[string]int // contestId+index → последний обработанный ID
	rateLimiter *time.Ticker
	userAgent   string
	baseURL     string
	readerURL   string
	mu          sync.Mutex
}

// NewCollector создаёт коллектор Codeforces.
func NewCollector(id domain.SourceID, client httpClient) *Collector {
	return &Collector{
		id:          id,
		client:      client,
		language:    "en",
		lastIDs:     make(map[string]int),
		rateLimiter: time.NewTicker(2100 * time.Millisecond), // 2.1s > 2s requirement
		userAgent:   "diploma-parser/1.0",
		baseURL:     "https://codeforces.com",
		readerURL:   "https://r.jina.ai/http://codeforces.com",
	}
}

// WithMinInterval устанавливает минимальный интервал между запросами (для тестов).
func (c *Collector) WithMinInterval(d time.Duration) *Collector {
	c.rateLimiter.Stop()
	c.rateLimiter = time.NewTicker(d)
	return c
}

// WithLanguage устанавливает язык задач.
func (c *Collector) WithLanguage(lang string) *Collector {
	c.language = lang
	return c
}

// WithReaderURL меняет адрес fallback Reader API для тестов и self-hosted установки.
func (c *Collector) WithReaderURL(rawURL string) *Collector {
	c.readerURL = strings.TrimSuffix(rawURL, "/")

	return c
}

// ID возвращает идентификатор источника.
func (c *Collector) ID() domain.SourceID {
	return c.id
}

// Connect для Codeforces — no-op (HTTP без постоянного соединения).
func (c *Collector) Connect(_ context.Context) error {
	return nil
}

// Close освобождает ресурсы.
func (c *Collector) Close() error {
	c.rateLimiter.Stop()
	return nil
}

// --- API types ---

type cfProblemsResponse struct {
	Status string   `json:"status"`
	Result cfResult `json:"result"`
}

type cfResult struct {
	Problems []cfProblem `json:"problems"`
}

type cfProblem struct {
	ContestID int      `json:"contestId"`
	Index     string   `json:"index"`
	Name      string   `json:"name"`
	Rating    int      `json:"rating,omitempty"`
	Tags      []string `json:"tags"`
}

// Collect собирает задачи через Codeforces API.
func (c *Collector) Collect(ctx context.Context) ([]domain.RawTask, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 1. Получаем список задач
	problems, err := c.fetchProblems(ctx)
	if err != nil {
		return nil, fmt.Errorf("codeforces: fetch problems: %w", err)
	}

	if len(problems) == 0 {
		slog.Debug("codeforces: no problems found")
		return nil, nil
	}

	slog.Debug("codeforces: fetched problems", "count", len(problems))

	// 2. Для каждой задачи загружаем HTML-страницу
	var tasks []domain.RawTask
	for _, p := range problems {
		key := fmt.Sprintf("%d/%s", p.ContestID, p.Index)
		if _, seen := c.lastIDs[key]; seen {
			continue // уже обработали
		}

		html, err := c.fetchProblemPage(ctx, p.ContestID, p.Index)
		if err != nil {
			slog.Warn("codeforces: failed to fetch problem page",
				"contest_id", p.ContestID, "index", p.Index, "error", err)
			continue
		}

		rawTask := c.problemToRawTask(p, html)
		tasks = append(tasks, rawTask)
		c.lastIDs[key] = p.ContestID
	}

	return tasks, nil
}

// CollectURL загружает одну каноническую задачу Codeforces без обхода каталога.
func (c *Collector) CollectURL(ctx context.Context, rawURL string) (domain.RawTask, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "codeforces.com" {
		return domain.RawTask{}, fmt.Errorf("codeforces: invalid task URL")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "problemset" || parts[1] != "problem" {
		return domain.RawTask{}, fmt.Errorf("codeforces: unsupported task URL")
	}
	contestID, err := strconv.Atoi(parts[2])
	if err != nil || parts[3] == "" {
		return domain.RawTask{}, fmt.Errorf("codeforces: invalid problem identifier")
	}
	html, err := c.fetchProblemPage(ctx, contestID, parts[3])
	directTask := c.problemToRawTask(cfProblem{ContestID: contestID, Index: parts[3]}, html)
	if err == nil && directTask.Title != "" && directTask.Statement != "" && !detectBlockingPage(html) {
		return directTask, nil
	}

	markdown, readerErr := c.fetchReaderProblem(ctx, contestID, parts[3])
	if readerErr != nil {
		if err != nil {
			return domain.RawTask{}, fmt.Errorf("codeforces: direct request failed: %w; reader fallback failed: %v", err, readerErr)
		}

		return domain.RawTask{}, fmt.Errorf("codeforces: blocking page; reader fallback failed: %w", readerErr)
	}

	return c.readerProblemToRawTask(contestID, parts[3], markdown)
}

// fetchProblems получает список задач через API.
func (c *Collector) fetchProblems(ctx context.Context) ([]cfProblem, error) {
	<-c.rateLimiter.C

	apiURL := c.baseURL + "/api/problemset.problems"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var apiResp cfProblemsResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("parse json: %w (body: %s)", err, string(body[:min(len(body), 200)]))
	}

	if apiResp.Status != "OK" {
		return nil, fmt.Errorf("api status: %s", apiResp.Status)
	}

	return apiResp.Result.Problems, nil
}

// fetchProblemPage загружает HTML-страницу задачи.
func (c *Collector) fetchProblemPage(ctx context.Context, contestID int, index string) (string, error) {
	<-c.rateLimiter.C

	pageURL := fmt.Sprintf("%s/problemset/problem/%d/%s", c.baseURL, contestID, index)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	return string(body), nil
}

// fetchReaderProblem получает только блок условия через Reader API без API-ключа.
func (c *Collector) fetchReaderProblem(ctx context.Context, contestID int, index string) (string, error) {
	<-c.rateLimiter.C

	pageURL := fmt.Sprintf("%s/problemset/problem/%d/%s", c.readerURL, contestID, index)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", fmt.Errorf("create reader request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("X-Target-Selector", ".problem-statement")
	req.Header.Set("X-Return-Format", "markdown")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("reader request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("reader returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", fmt.Errorf("read reader response: %w", err)
	}
	if strings.TrimSpace(string(body)) == "" {
		return "", fmt.Errorf("reader returned empty statement")
	}

	return string(body), nil
}

// problemToRawTask преобразует cfProblem в RawTask.
func (c *Collector) problemToRawTask(p cfProblem, html string) domain.RawTask {
	// URL задачи
	problemURL := fmt.Sprintf("%s/problemset/problem/%d/%s", c.baseURL, p.ContestID, p.Index)
	title, statement, examples, constraints := parseProblemHTML(html)
	if title == "" {
		title = p.Name
	}

	// Теги как domain.Tag
	tags := make([]domain.Tag, len(p.Tags))
	for i, tag := range p.Tags {
		tags[i] = domain.Tag(tag)
	}

	return domain.RawTask{
		Source: domain.Source{
			ID:   c.id,
			Name: "Codeforces",
			Type: domain.SourceTypeWebsite,
		},
		RawContent:  []byte(html),
		SourceURL:   problemURL,
		RetrievedAt: time.Now(),
		Title:       title,
		Statement:   statement,
		Examples:    examples,
		Constraints: constraints,
		Difficulty:  mapRatingToDifficulty(p.Rating),
		Tags:        tags,
	}
}

// readerProblemToRawTask преобразует чистый Markdown fallback в структурированную задачу.
func (c *Collector) readerProblemToRawTask(contestID int, index, markdown string) (domain.RawTask, error) {
	title, statement, examples, constraints := parseReaderMarkdown(markdown)
	if title == "" || statement == "" {
		return domain.RawTask{}, fmt.Errorf("codeforces: reader response has no task statement")
	}

	return domain.RawTask{
		Source: domain.Source{
			ID:   c.id,
			Name: "Codeforces",
			Type: domain.SourceTypeWebsite,
		},
		RawContent:  []byte(markdown),
		SourceURL:   fmt.Sprintf("%s/problemset/problem/%d/%s", c.baseURL, contestID, index),
		RetrievedAt: time.Now(),
		Title:       title,
		Statement:   statement,
		Examples:    examples,
		Constraints: constraints,
	}, nil
}

// detectBlockingPage распознаёт Cloudflare и JavaScript proof-of-work страницы.
func detectBlockingPage(content string) bool {
	lower := strings.ToLower(content)

	return strings.Contains(lower, "cf-mitigated") ||
		strings.Contains(lower, "browser is being checked") ||
		strings.Contains(lower, "just a moment")
}

// parseReaderMarkdown разбирает ограниченный блок .problem-statement Codeforces.
func parseReaderMarkdown(markdown string) (string, string, []domain.Example, []string) {
	if index := strings.Index(markdown, "Markdown Content:"); index >= 0 {
		markdown = markdown[index+len("Markdown Content:"):]
	}
	lines := compactLines(markdown)
	if len(lines) == 0 {
		return "", "", nil, nil
	}
	title := lines[0]
	contentStart := indexAfter(lines, "stdout")
	if contentStart < 0 {
		contentStart = 1
	}
	examplesStart := indexOf(lines, "Examples", contentStart)
	statementEnd := len(lines)
	if examplesStart >= 0 {
		statementEnd = examplesStart
	}
	constraints := make([]string, 0, 2)
	if timeIndex := indexOf(lines, "time limit per test", 0); timeIndex >= 0 && timeIndex+1 < len(lines) {
		constraints = append(constraints, "time limit per test: "+lines[timeIndex+1])
	}
	if memoryIndex := indexOf(lines, "memory limit per test", 0); memoryIndex >= 0 && memoryIndex+1 < len(lines) {
		constraints = append(constraints, "memory limit per test: "+lines[memoryIndex+1])
	}

	examples := make([]domain.Example, 0)
	if examplesStart >= 0 {
		examples = parseReaderExamples(lines[examplesStart+1:])
	}

	return title, strings.Join(lines[contentStart:statementEnd], "\n\n"), examples, constraints
}

// parseProblemHTML извлекает условие, ограничения и примеры из настоящей страницы Codeforces.
func parseProblemHTML(content string) (string, string, []domain.Example, []string) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(content))
	if err != nil {
		return "", "", nil, nil
	}
	problem := doc.Find(".problem-statement").First()
	if problem.Length() == 0 {
		return "", "", nil, nil
	}
	title := cleanHTMLText(problem.Find(".title").First())
	statementNode := problem.Clone()
	statementNode.Find(".header, .input-specification, .output-specification, .sample-tests, .note").Remove()
	statement := cleanHTMLText(statementNode)
	constraints := make([]string, 0, 2)
	problem.Find(".time-limit, .memory-limit").Each(func(_ int, selection *goquery.Selection) {
		if value := cleanHTMLText(selection); value != "" {
			constraints = append(constraints, value)
		}
	})

	return title, statement, parseHTMLExamples(problem), constraints
}

// parseHTMLExamples сопоставляет входы и выходы примеров по их позиции.
func parseHTMLExamples(problem *goquery.Selection) []domain.Example {
	inputs := make([]string, 0)
	outputs := make([]string, 0)
	problem.Find(".sample-test .input pre").Each(func(_ int, selection *goquery.Selection) {
		inputs = append(inputs, cleanHTMLText(selection))
	})
	problem.Find(".sample-test .output pre").Each(func(_ int, selection *goquery.Selection) {
		outputs = append(outputs, cleanHTMLText(selection))
	})
	examples := make([]domain.Example, 0, min(len(inputs), len(outputs)))
	for index := 0; index < len(inputs) && index < len(outputs); index++ {
		if inputs[index] != "" && outputs[index] != "" {
			examples = append(examples, domain.Example{Input: inputs[index], Output: outputs[index]})
		}
	}

	return examples
}

// cleanHTMLText сохраняет переносы из pre и убирает лишние пробелы HTML.
func cleanHTMLText(selection *goquery.Selection) string {
	clone := selection.Clone()
	clone.Find("br").ReplaceWithHtml("\n")
	lines := make([]string, 0)
	for _, line := range strings.Split(clone.Text(), "\n") {
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			lines = append(lines, line)
		}
	}

	return strings.Join(lines, "\n")
}

// parseReaderExamples извлекает пары Input/Output после секции Examples.
func parseReaderExamples(lines []string) []domain.Example {
	examples := make([]domain.Example, 0)
	for index := 0; index < len(lines); {
		if !strings.EqualFold(lines[index], "Input") {
			index++
			continue
		}
		index++
		if index < len(lines) && strings.EqualFold(lines[index], "Copy") {
			index++
		}
		inputStart := index
		for index < len(lines) && !strings.EqualFold(lines[index], "Output") {
			index++
		}
		if index >= len(lines) {
			break
		}
		input := strings.Join(lines[inputStart:index], "\n")
		index++
		if index < len(lines) && strings.EqualFold(lines[index], "Copy") {
			index++
		}
		outputStart := index
		for index < len(lines) && !strings.EqualFold(lines[index], "Input") && !strings.EqualFold(lines[index], "Note") {
			index++
		}
		output := strings.Join(lines[outputStart:index], "\n")
		if strings.TrimSpace(input) != "" && strings.TrimSpace(output) != "" {
			examples = append(examples, domain.Example{Input: input, Output: output})
		}
	}

	return examples
}

// compactLines удаляет пустые строки Markdown и внешние пробелы.
func compactLines(value string) []string {
	result := make([]string, 0)
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			result = append(result, line)
		}
	}

	return result
}

// indexOf ищет точную строку без учёта регистра.
func indexOf(lines []string, value string, start int) int {
	for index := start; index < len(lines); index++ {
		if strings.EqualFold(lines[index], value) {
			return index
		}
	}

	return -1
}

// indexAfter возвращает позицию после найденной строки.
func indexAfter(lines []string, value string) int {
	index := indexOf(lines, value, 0)
	if index < 0 || index+1 >= len(lines) {
		return -1
	}

	return index + 1
}

// mapRatingToDifficulty преобразует рейтинг Codeforces в Difficulty.
func mapRatingToDifficulty(rating int) domain.Difficulty {
	if rating <= 0 {
		return domain.DifficultyUnknown
	}
	if rating < 1200 {
		return domain.DifficultyEasy
	}
	if rating <= 1600 {
		return domain.DifficultyMedium
	}
	return domain.DifficultyHard
}

// min возвращает минимальное из двух int.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Compile-time check.
var _ source.Collector = (*Collector)(nil)
