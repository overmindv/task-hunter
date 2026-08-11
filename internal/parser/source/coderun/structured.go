package coderun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/overmindv/task-hunter/internal/parser/domain"
)

const maxResourceSize = 2 << 20

var errEmbeddedProblemNotFound = errors.New("embedded CodeRun problem not found")

type nextDataEnvelope struct {
	Props struct {
		PageProps struct {
			Values map[string]json.RawMessage `json:"values"`
		} `json:"pageProps"`
	} `json:"props"`
}

type embeddedProblem struct {
	Slug       string   `json:"slug"`
	Title      string   `json:"title"`
	Difficulty string   `json:"difficulty"`
	Tags       []string `json:"tags"`
	Statements struct {
		Rendered resourceLink `json:"renderedStatements"`
		Samples  []struct {
			Input  resourceContainer `json:"input"`
			Output resourceContainer `json:"output"`
		} `json:"samples"`
	} `json:"statements"`
}

type resourceContainer struct {
	S3File resourceLink `json:"s3File"`
}

type resourceLink struct {
	URL string `json:"url"`
}

type renderedStatement struct {
	Legend       string  `json:"legend"`
	InputFormat  string  `json:"inputFormat"`
	OutputFormat string  `json:"outputFormat"`
	Notes        *string `json:"notes"`
}

// problemToStructuredRawTask загружает динамическое условие и открытые примеры CodeRun.
func (c *Collector) problemToStructuredRawTask(ctx context.Context, problem catalogProblem, pageHTML string) (domain.RawTask, error) {
	embedded, err := parseEmbeddedProblem(pageHTML, problem.Slug)
	if errors.Is(err, errEmbeddedProblemNotFound) {
		return c.problemToRawTask(problem, pageHTML), nil
	}
	if err != nil {
		return domain.RawTask{}, fmt.Errorf("parse embedded problem: %w", err)
	}

	statementBody, err := c.fetchResource(ctx, embedded.Statements.Rendered.URL)
	if err != nil {
		return domain.RawTask{}, fmt.Errorf("fetch rendered statement: %w", err)
	}
	statement, err := parseRenderedStatement(statementBody)
	if err != nil {
		return domain.RawTask{}, fmt.Errorf("parse rendered statement: %w", err)
	}
	examples, err := c.fetchExamples(ctx, embedded)
	if err != nil {
		return domain.RawTask{}, fmt.Errorf("fetch examples: %w", err)
	}

	tags := make([]domain.Tag, 0, len(embedded.Tags))
	for _, tag := range embedded.Tags {
		tags = append(tags, domain.Tag(tag))
	}
	stableContent, err := json.Marshal(struct {
		Statement string           `json:"statement"`
		Examples  []domain.Example `json:"examples"`
	}{Statement: statement, Examples: examples})
	if err != nil {
		return domain.RawTask{}, fmt.Errorf("marshal stable source content: %w", err)
	}

	return domain.RawTask{
		Source: domain.Source{
			ID:   c.id,
			Name: "CodeRun",
			Type: domain.SourceTypeWebsite,
		},
		RawContent:  stableContent,
		SourceURL:   fmt.Sprintf("%s/problem/%s", c.baseURL, problem.Slug),
		RetrievedAt: time.Now(),
		Title:       embedded.Title,
		Statement:   statement,
		Examples:    examples,
		Constraints: parseRuntimeLimits(pageHTML),
		Difficulty:  mapDifficulty(embedded.Difficulty),
		Tags:        tags,
	}, nil
}

// parseEmbeddedProblem находит данные нужной задачи в __NEXT_DATA__.
func parseEmbeddedProblem(pageHTML, slug string) (embeddedProblem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(pageHTML))
	if err != nil {
		return embeddedProblem{}, fmt.Errorf("parse page html: %w", err)
	}
	payload := strings.TrimSpace(doc.Find("#__NEXT_DATA__").First().Text())
	if payload == "" {
		return embeddedProblem{}, errEmbeddedProblemNotFound
	}

	var envelope nextDataEnvelope
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		return embeddedProblem{}, fmt.Errorf("decode next data: %w", err)
	}
	for _, raw := range envelope.Props.PageProps.Values {
		var problem embeddedProblem
		if err := json.Unmarshal(raw, &problem); err != nil {
			continue
		}
		if problem.Slug == slug && problem.Statements.Rendered.URL != "" {
			return problem, nil
		}
	}

	return embeddedProblem{}, errEmbeddedProblemNotFound
}

// fetchExamples загружает только опубликованные примеры ввода и вывода.
func (c *Collector) fetchExamples(ctx context.Context, problem embeddedProblem) ([]domain.Example, error) {
	examples := make([]domain.Example, 0, len(problem.Statements.Samples))
	for index, sample := range problem.Statements.Samples {
		input, err := c.fetchResource(ctx, sample.Input.S3File.URL)
		if err != nil {
			return nil, fmt.Errorf("fetch sample %d input: %w", index+1, err)
		}
		output, err := c.fetchResource(ctx, sample.Output.S3File.URL)
		if err != nil {
			return nil, fmt.Errorf("fetch sample %d output: %w", index+1, err)
		}
		examples = append(examples, domain.Example{
			Input:  strings.TrimSpace(string(input)),
			Output: strings.TrimSpace(string(output)),
		})
	}

	return examples, nil
}

// fetchResource читает только HTTPS-файлы из доверенного хранилища CodeRun.
func (c *Collector) fetchResource(ctx context.Context, rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || !isTrustedResourceHost(parsed.Hostname()) {
		return nil, fmt.Errorf("untrusted resource URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create resource request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("resource request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("resource returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResourceSize+1))
	if err != nil {
		return nil, fmt.Errorf("read resource: %w", err)
	}
	if len(body) > maxResourceSize {
		return nil, fmt.Errorf("resource exceeds %d bytes", maxResourceSize)
	}

	return body, nil
}

// isTrustedResourceHost ограничивает вторичные запросы хранилищем Яндекса.
func isTrustedResourceHost(host string) bool {
	host = strings.ToLower(host)

	return host == "contest-problem-files.s3.mds.yandex.net" || host == "contest-problem-files.s3-private.mds.yandex.net"
}

// parseRenderedStatement собирает читаемое условие из JSON-фрагментов CodeRun.
func parseRenderedStatement(body []byte) (string, error) {
	var rendered renderedStatement
	if err := json.Unmarshal(body, &rendered); err != nil {
		return "", fmt.Errorf("decode rendered statement: %w", err)
	}
	parts := []string{htmlFragmentText(rendered.Legend)}
	if value := htmlFragmentText(rendered.InputFormat); value != "" {
		parts = append(parts, "Входные данные\n"+value)
	}
	if value := htmlFragmentText(rendered.OutputFormat); value != "" {
		parts = append(parts, "Выходные данные\n"+value)
	}
	if rendered.Notes != nil {
		if value := htmlFragmentText(*rendered.Notes); value != "" {
			parts = append(parts, "Примечание\n"+value)
		}
	}

	return strings.Join(nonEmpty(parts), "\n\n"), nil
}

// htmlFragmentText очищает HTML и дублирующую визуальную часть KaTeX.
func htmlFragmentText(fragment string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return strings.TrimSpace(fragment)
	}
	doc.Find(".katex-html, annotation, script, style").Remove()

	return strings.Join(strings.Fields(doc.Text()), " ")
}

// parseRuntimeLimits извлекает видимые ограничения времени и памяти.
func parseRuntimeLimits(pageHTML string) []string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(pageHTML))
	if err != nil {
		return nil
	}
	limits := make([]string, 0, 2)
	doc.Find("dl dt").Each(func(_ int, term *goquery.Selection) {
		name := strings.Join(strings.Fields(term.Text()), " ")
		value := strings.Join(strings.Fields(term.Next().Text()), " ")
		if name != "" && value != "" {
			limits = append(limits, name+": "+value)
		}
	})

	return limits
}

// mapDifficulty переводит сложность CodeRun во внутреннее значение.
func mapDifficulty(value string) domain.Difficulty {
	switch strings.ToUpper(strings.TrimSpace(value)) {
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

// nonEmpty удаляет пустые части условия.
func nonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}

	return result
}
