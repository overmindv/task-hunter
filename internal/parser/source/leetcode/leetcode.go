// Package leetcode реализует сбор задач с LeetCode через GraphQL API.
//
// LeetCode использует динамическую загрузку (React), поэтому напрямую
// парсить HTML-страницы невозможно. Используется публичный GraphQL-эндпоинт:
//
//	POST https://leetcode.com/graphql
//
// Запрос списка задач:
//
//	query problemsetQuestionList($limit: Int, $skip: Int) {
//	  problemsetQuestionList: questionList(
//	    categorySlug: "", limit: $limit, skip: $skip
//	  ) {
//	    total: totalNum
//	    questions: data {
//	      title titleSlug difficulty topicTags { name }
//	    }
//	  }
//	}
//
// Запрос деталей задачи:
//
//	query questionData($titleSlug: String!) {
//	  question(titleSlug: $titleSlug) {
//	    questionId title titleSlug difficulty content topicTags { name }
//	  }
//	}
package leetcode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
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

// Collector собирает задачи с LeetCode.
type Collector struct {
	id          domain.SourceID
	client      httpClient
	lastSlugs   map[string]struct{} // slug задачи для дедупликации
	rateLimiter *time.Ticker
	userAgent   string
	baseURL     string
	mu          sync.Mutex
}

// NewCollector создаёт коллектор LeetCode.
func NewCollector(id domain.SourceID, client httpClient) *Collector {
	return &Collector{
		id:          id,
		client:      client,
		lastSlugs:   make(map[string]struct{}),
		rateLimiter: time.NewTicker(3100 * time.Millisecond), // 3.1s > 3s requirement
		userAgent:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		baseURL:     "https://leetcode.com",
	}
}

// WithMinInterval устанавливает минимальный интервал между запросами (для тестов).
func (c *Collector) WithMinInterval(d time.Duration) *Collector {
	c.rateLimiter.Stop()
	c.rateLimiter = time.NewTicker(d)
	return c
}

// WithUserAgent устанавливает User-Agent.
func (c *Collector) WithUserAgent(ua string) *Collector {
	c.userAgent = ua
	return c
}

// ID возвращает идентификатор источника.
func (c *Collector) ID() domain.SourceID {
	return c.id
}

// Connect для LeetCode — no-op (HTTP без постоянного соединения).
func (c *Collector) Connect(_ context.Context) error {
	return nil
}

// Close освобождает ресурсы.
func (c *Collector) Close() error {
	c.rateLimiter.Stop()
	return nil
}

// --- GraphQL types ---

type gqlRequest struct {
	Query     string `json:"query"`
	Variables any    `json:"variables,omitempty"`
}

type gqlProblemsetResponse struct {
	Data struct {
		ProblemsetQuestionList struct {
			Total     int              `json:"total"`
			Questions []lcProblemBrief `json:"questions"`
		} `json:"problemsetQuestionList"`
	} `json:"data"`
	Errors []gqlError `json:"errors,omitempty"`
}

type gqlQuestionResponse struct {
	Data struct {
		Question *lcQuestionDetail `json:"question"`
	} `json:"data"`
	Errors []gqlError `json:"errors,omitempty"`
}

type gqlError struct {
	Message string `json:"message"`
}

type lcProblemBrief struct {
	TitleSlug  string  `json:"titleSlug"`
	Title      string  `json:"title"`
	Difficulty string  `json:"difficulty"`
	TopicTags  []lcTag `json:"topicTags"`
}

type lcQuestionDetail struct {
	QuestionID       string  `json:"questionId"`
	Title            string  `json:"title"`
	TitleSlug        string  `json:"titleSlug"`
	Difficulty       string  `json:"difficulty"`
	Content          string  `json:"content"`
	TopicTags        []lcTag `json:"topicTags"`
	ExampleTestcases string  `json:"exampleTestcases"`
	SampleTestCase   string  `json:"sampleTestCase"`
}

type lcTag struct {
	Name string `json:"name"`
}

// --- GraphQL queries ---

const problemsetQuery = `query problemsetQuestionList($limit: Int, $skip: Int) {
  problemsetQuestionList: questionList(
    categorySlug: "", limit: $limit, skip: $skip
  ) {
    total: totalNum
    questions: data {
      title
      titleSlug
      difficulty
      topicTags { name }
    }
  }
}`

const questionQuery = `query questionData($titleSlug: String!) {
  question(titleSlug: $titleSlug) {
    questionId
    title
    titleSlug
    difficulty
    content
    topicTags { name }
	exampleTestcases
	sampleTestCase
  }
}`

// --- Collect ---

// Collect собирает задачи с LeetCode.
func (c *Collector) Collect(ctx context.Context) ([]domain.RawTask, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 1. Получаем список задач
	problems, err := c.fetchProblemset(ctx)
	if err != nil {
		return nil, fmt.Errorf("leetcode: fetch problemset: %w", err)
	}

	if len(problems) == 0 {
		slog.Debug("leetcode: no problems found")
		return nil, nil
	}

	slog.Debug("leetcode: fetched problems", "count", len(problems))

	// 2. Для каждой задачи загружаем детали
	var tasks []domain.RawTask
	for _, p := range problems {
		if _, seen := c.lastSlugs[p.TitleSlug]; seen {
			continue // уже обработали
		}

		detail, err := c.fetchQuestionDetail(ctx, p.TitleSlug)
		if err != nil {
			slog.Warn("leetcode: failed to fetch question detail",
				"slug", p.TitleSlug, "error", err)
			continue
		}

		// Проверяем, что задача содержит условие
		if detail.Content == "" {
			slog.Warn("leetcode: question has no content, skipping", "slug", p.TitleSlug)
			continue
		}

		rawTask := c.problemToRawTask(detail)
		tasks = append(tasks, rawTask)
		c.lastSlugs[p.TitleSlug] = struct{}{}
	}

	return tasks, nil
}

// CollectURL загружает одну задачу LeetCode по каноническому slug.
func (c *Collector) CollectURL(ctx context.Context, rawURL string) (domain.RawTask, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "leetcode.com" {
		return domain.RawTask{}, fmt.Errorf("leetcode: invalid task URL")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "problems" || parts[1] == "" {
		return domain.RawTask{}, fmt.Errorf("leetcode: unsupported task URL")
	}
	detail, err := c.fetchQuestionDetail(ctx, parts[1])
	if err != nil {
		return domain.RawTask{}, fmt.Errorf("leetcode: fetch direct task: %w", err)
	}
	if detail.Content == "" || DetectBlockingPage(detail.Content) {
		return domain.RawTask{}, fmt.Errorf("leetcode: task content is unavailable")
	}

	return c.problemToRawTask(detail), nil
}

// fetchProblemset получает список задач через GraphQL.
func (c *Collector) fetchProblemset(ctx context.Context) ([]lcProblemBrief, error) {
	<-c.rateLimiter.C

	body, err := c.doGQL(ctx, gqlRequest{
		Query: problemsetQuery,
		Variables: map[string]int{
			"limit": 50,
			"skip":  0,
		},
	})
	if err != nil {
		return nil, err
	}

	var resp gqlProblemsetResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse problemset response: %w", err)
	}

	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("graphql errors: %v", resp.Errors)
	}

	return resp.Data.ProblemsetQuestionList.Questions, nil
}

// fetchQuestionDetail получает детали задачи через GraphQL.
func (c *Collector) fetchQuestionDetail(ctx context.Context, titleSlug string) (*lcQuestionDetail, error) {
	<-c.rateLimiter.C

	body, err := c.doGQL(ctx, gqlRequest{
		Query: questionQuery,
		Variables: map[string]string{
			"titleSlug": titleSlug,
		},
	})
	if err != nil {
		return nil, err
	}

	var resp gqlQuestionResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse question response: %w", err)
	}

	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("graphql errors: %v", resp.Errors)
	}

	if resp.Data.Question == nil {
		return nil, fmt.Errorf("question not found: %s", titleSlug)
	}

	return resp.Data.Question, nil
}

// doGQL выполняет GraphQL-запрос.
func (c *Collector) doGQL(ctx context.Context, req gqlRequest) ([]byte, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/graphql", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Origin", c.baseURL)
	httpReq.Header.Set("User-Agent", c.userAgent)
	httpReq.Header.Set("Referer", c.baseURL+"/problemset/")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("access denied (403) — possible captcha or IP ban")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	return body, nil
}

// problemToRawTask преобразует lcQuestionDetail в RawTask.
func (c *Collector) problemToRawTask(q *lcQuestionDetail) domain.RawTask {
	problemURL := fmt.Sprintf("%s/problems/%s", c.baseURL, q.TitleSlug)
	tags := make([]domain.Tag, len(q.TopicTags))
	for i, t := range q.TopicTags {
		tags[i] = domain.Tag(t.Name)
	}
	statement, examples, constraints := parseQuestionContent(q.Content)

	return domain.RawTask{
		Source: domain.Source{
			ID:   c.id,
			Name: "LeetCode",
			Type: domain.SourceTypeWebsite,
		},
		RawContent:  []byte(q.Content),
		SourceURL:   problemURL,
		RetrievedAt: time.Now(),
		Title:       q.Title,
		Statement:   statement,
		Examples:    examples,
		Constraints: constraints,
		Difficulty:  MapDifficulty(q.Difficulty),
		Tags:        tags,
	}
}

// parseQuestionContent извлекает условие, открытые примеры и ограничения из HTML LeetCode.
func parseQuestionContent(content string) (string, []domain.Example, []string) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(content))
	if err != nil {
		return strings.TrimSpace(content), nil, nil
	}

	examples := make([]domain.Example, 0)
	doc.Find("pre").Each(func(_ int, selection *goquery.Selection) {
		if example, ok := parseExample(selection.Text()); ok {
			examples = append(examples, example)
		}
	})

	constraints := make([]string, 0)
	doc.Find("ul").EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		previous := strings.ToLower(cleanText(selection.Prev().Text()))
		if !strings.Contains(previous, "constraints") {
			return true
		}
		selection.Find("li").Each(func(_ int, item *goquery.Selection) {
			if value := cleanText(item.Text()); value != "" {
				constraints = append(constraints, value)
			}
		})

		return false
	})

	parts := make([]string, 0)
	doc.Find("body").Children().EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		value := cleanText(selection.Text())
		lower := strings.ToLower(value)
		if strings.HasPrefix(lower, "example ") || lower == "constraints:" || lower == "constraints" {
			return false
		}
		if value != "" {
			parts = append(parts, value)
		}

		return true
	})

	return strings.Join(parts, "\n\n"), examples, constraints
}

// parseExample разбирает один открытый блок Input/Output.
func parseExample(value string) (domain.Example, bool) {
	lower := strings.ToLower(value)
	inputIndex := strings.Index(lower, "input:")
	outputIndex := strings.Index(lower, "output:")
	if inputIndex < 0 || outputIndex <= inputIndex {
		return domain.Example{}, false
	}
	input := value[inputIndex+len("input:") : outputIndex]
	output := value[outputIndex+len("output:"):]
	explanation := ""
	if explanationIndex := strings.Index(strings.ToLower(output), "explanation:"); explanationIndex >= 0 {
		explanation = output[explanationIndex+len("explanation:"):]
		output = output[:explanationIndex]
	}

	return domain.Example{
		Input:       strings.TrimSpace(input),
		Output:      strings.TrimSpace(output),
		Explanation: strings.TrimSpace(explanation),
	}, strings.TrimSpace(input) != "" && strings.TrimSpace(output) != ""
}

// cleanText схлопывает служебные HTML-пробелы, сохраняя читаемый текст.
func cleanText(value string) string {
	value = strings.ReplaceAll(value, "\u00a0", " ")

	return strings.Join(strings.Fields(value), " ")
}

// MapDifficulty преобразует строковую сложность LeetCode в domain.Difficulty.
func MapDifficulty(d string) domain.Difficulty {
	switch strings.ToUpper(d) {
	case "EASY":
		return domain.DifficultyEasy
	case "MEDIUM":
		return domain.DifficultyMedium
	case "HARD":
		return domain.DifficultyHard
	default:
		return domain.DifficultyUnknown
	}
}

// DetectBlockingPage проверяет, является ли HTML страницей блокировки (капча, login wall).
func DetectBlockingPage(html string) bool {
	lower := strings.ToLower(html)
	return strings.Contains(lower, "captcha") ||
		strings.Contains(lower, "cf-browser-verification") ||
		strings.Contains(lower, "access denied") ||
		strings.Contains(lower, "please login")
}

// Compile-time check.
var _ source.Collector = (*Collector)(nil)
