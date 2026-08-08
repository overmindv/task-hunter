// Package coderun реализует сбор задач с CodeRun (Яндекс) через HTTP и парсинг HTML.
//
// Каталог задач: https://coderun.yandex.ru/catalog
// Страница задачи: https://coderun.yandex.ru/problem/{slug}
//
// HTML-структура:
//   - Каталог: <ul class="problem-list"> → <li class="problem-item"> → <a class="problem-title" href="...">
//   - Задача: <h1 class="problem-title">, <div class="problem-statement">, <section class="input-output-specs">,
//     <section class="constraints">, <section class="examples">
package coderun

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"

	"diploma/internal/parser/domain"
	"diploma/internal/parser/source"
)

// httpClient — интерфейс HTTP-клиента для мокания в тестах.
type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Collector собирает задачи с CodeRun.
type Collector struct {
	id          domain.SourceID
	client      httpClient
	lastURLs    map[string]struct{} // URL для дедупликации
	rateLimiter *time.Ticker
	userAgent   string
	baseURL     string
	mu          sync.Mutex
}

// NewCollector создаёт коллектор CodeRun.
func NewCollector(id domain.SourceID, client httpClient) *Collector {
	return &Collector{
		id:          id,
		client:      client,
		lastURLs:    make(map[string]struct{}),
		rateLimiter: time.NewTicker(1100 * time.Millisecond), // ~1 запрос в секунду
		userAgent:   "Mozilla/5.0 (compatible; diploma-parser/1.0)",
		baseURL:     "https://coderun.yandex.ru",
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

// Connect для CodeRun — no-op (HTTP без постоянного соединения).
func (c *Collector) Connect(_ context.Context) error {
	return nil
}

// Close освобождает ресурсы.
func (c *Collector) Close() error {
	c.rateLimiter.Stop()
	return nil
}

// --- Типы для парсинга ---

// catalogProblem представляет задачу из каталога.
type catalogProblem struct {
	Title      string
	Slug       string
	Difficulty string
}

// Collect собирает задачи с CodeRun.
func (c *Collector) Collect(ctx context.Context) ([]domain.RawTask, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 1. Получаем список задач из каталога
	problems, err := c.fetchCatalog(ctx)
	if err != nil {
		return nil, fmt.Errorf("coderun: fetch catalog: %w", err)
	}

	if len(problems) == 0 {
		slog.Debug("coderun: no problems found in catalog")
		return nil, nil
	}

	slog.Debug("coderun: fetched catalog", "count", len(problems))

	// 2. Для каждой задачи загружаем страницу
	var tasks []domain.RawTask
	for _, p := range problems {
		if _, seen := c.lastURLs[p.Slug]; seen {
			continue // уже обработали
		}

		html, err := c.fetchProblemPage(ctx, p.Slug)
		if err != nil {
			if isNotFound(err) {
				slog.Warn("coderun: problem not found (404), skipping", "slug", p.Slug)
				continue
			}
			slog.Warn("coderun: failed to fetch problem page",
				"slug", p.Slug, "error", err)
			continue
		}

		// Проверяем, что HTML содержит условие задачи
		if !hasProblemStatement(html) {
			slog.Warn("coderun: problem page has no statement, skipping", "slug", p.Slug)
			continue
		}

		rawTask := c.problemToRawTask(p, html)
		tasks = append(tasks, rawTask)
		c.lastURLs[p.Slug] = struct{}{}
	}

	return tasks, nil
}

// fetchCatalog загружает и парсит страницу каталога задач.
func (c *Collector) fetchCatalog(ctx context.Context) ([]catalogProblem, error) {
	<-c.rateLimiter.C

	url := c.baseURL + "/catalog"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("catalog not found: %w", errNotFound{url: url})
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limited: %w", errRateLimited{retryAfter: parseRetryAfter(resp)})
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	var problems []catalogProblem
	doc.Find("ul.problem-list li.problem-item").Each(func(i int, s *goquery.Selection) {
		link := s.Find("a.problem-title")
		href, exists := link.Attr("href")
		if !exists || href == "" {
			return
		}

		// Извлекаем slug из href: /problem/{slug}
		slug := strings.TrimPrefix(href, "/problem/")

		title := strings.TrimSpace(link.Text())
		difficulty := strings.TrimSpace(s.Find("span.difficulty").Text())

		problems = append(problems, catalogProblem{
			Title:      title,
			Slug:       slug,
			Difficulty: difficulty,
		})
	})

	return problems, nil
}

// fetchProblemPage загружает HTML-страницу задачи.
func (c *Collector) fetchProblemPage(ctx context.Context, slug string) (string, error) {
	<-c.rateLimiter.C

	pageURL := fmt.Sprintf("%s/problem/%s", c.baseURL, slug)
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

	if resp.StatusCode == http.StatusNotFound {
		return "", errNotFound{url: pageURL}
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", errRateLimited{retryAfter: parseRetryAfter(resp)}
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d for %s", resp.StatusCode, pageURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	return string(body), nil
}

// problemToRawTask преобразует catalogProblem в RawTask.
func (c *Collector) problemToRawTask(p catalogProblem, html string) domain.RawTask {
	problemURL := fmt.Sprintf("%s/problem/%s", c.baseURL, p.Slug)

	return domain.RawTask{
		Source: domain.Source{
			ID:   c.id,
			Name: "CodeRun",
			Type: domain.SourceTypeWebsite,
		},
		RawContent:  []byte(html),
		SourceURL:   problemURL,
		RetrievedAt: time.Now(),
	}
}

// hasProblemStatement проверяет, содержит ли HTML условие задачи.
func hasProblemStatement(html string) bool {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return false
	}
	return doc.Find("h1.problem-title").Length() > 0
}

// --- Обработка ошибок ---

type errNotFound struct {
	url string
}

func (e errNotFound) Error() string {
	return fmt.Sprintf("not found: %s", e.url)
}

func isNotFound(err error) bool {
	return strings.Contains(err.Error(), "not found:")
}

type errRateLimited struct {
	retryAfter string
}

func (e errRateLimited) Error() string {
	return fmt.Sprintf("rate limited, retry after: %s", e.retryAfter)
}

func parseRetryAfter(resp *http.Response) string {
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		return ra
	}
	return "unknown"
}

// Compile-time check.
var _ source.Collector = (*Collector)(nil)
