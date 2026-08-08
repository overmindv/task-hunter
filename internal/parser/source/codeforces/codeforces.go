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
	"sync"
	"time"

	"diploma/internal/parser/domain"
	"diploma/internal/parser/source"
)

// httpClient — интерфейс HTTP-клиента для мокания в тестах.
type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Collector собирает задачи с Codeforces.
type Collector struct {
	id          domain.SourceID
	client      httpClient
	language    string   // "ru" или "en"
	lastIDs    map[string]int // contestId+index → последний обработанный ID
	rateLimiter *time.Ticker
	userAgent   string
	baseURL     string
	mu          sync.Mutex
}

// NewCollector создаёт коллектор Codeforces.
func NewCollector(id domain.SourceID, client httpClient) *Collector {
	return &Collector{
		id:          id,
		client:      client,
		language:    "en",
		lastIDs:    make(map[string]int),
		rateLimiter: time.NewTicker(2100 * time.Millisecond), // 2.1s > 2s requirement
		userAgent:   "diploma-parser/1.0",
		baseURL:     "https://codeforces.com",
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
	Status string       `json:"status"`
	Result cfResult     `json:"result"`
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
	defer resp.Body.Close()

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
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	return string(body), nil
}

// problemToRawTask преобразует cfProblem в RawTask.
func (c *Collector) problemToRawTask(p cfProblem, html string) domain.RawTask {
	// URL задачи
	problemURL := fmt.Sprintf("%s/problemset/problem/%d/%s", c.baseURL, p.ContestID, p.Index)

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
	}
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
