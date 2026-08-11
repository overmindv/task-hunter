package collection

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ImportResult описывает ответ tasks-it для одного кандидата.
type ImportResult struct {
	ExternalID  string `json:"external_id"`
	CandidateID string `json:"candidate_id"`
	Status      string `json:"status"`
	Message     string `json:"message"`
}

// CandidateSink принимает нормализованные кандидаты у worker.
type CandidateSink interface {
	Import(ctx context.Context, candidates []Candidate) ([]ImportResult, error)
}

// TasksITClient отправляет защищённые batch-запросы владельцу задач.
type TasksITClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
	maxRetries int
}

// NewTasksITClient создаёт клиент internal ingestion API.
func NewTasksITClient(baseURL, token string, timeout time.Duration, maxRetries int) *TasksITClient {
	return &TasksITClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: &http.Client{Timeout: timeout},
		maxRetries: maxRetries,
	}
}

// Import отправляет до 100 кандидатов и повторяет только временные ошибки.
func (c *TasksITClient) Import(ctx context.Context, candidates []Candidate) ([]ImportResult, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	if len(candidates) > 100 {
		return nil, fmt.Errorf("tasks-it batch exceeds 100 candidates")
	}

	payload := map[string]any{"items": candidates}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal candidates batch: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		results, retry, callErr := c.doImport(ctx, body)
		if callErr == nil {
			return results, nil
		}
		lastErr = callErr
		if !retry || attempt == c.maxRetries {
			break
		}

		timer := time.NewTimer(time.Duration(1<<attempt) * 200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()

			return nil, fmt.Errorf("wait tasks-it retry: %w", ctx.Err())
		case <-timer.C:
		}
	}

	return nil, fmt.Errorf("import candidates to tasks-it: %w", lastErr)
}

// doImport выполняет одну HTTP-попытку и сообщает, допустим ли retry.
func (c *TasksITClient) doImport(ctx context.Context, body []byte) ([]ImportResult, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/internal/task-candidates/batch", bytes.NewReader(body))
	if err != nil {
		return nil, false, fmt.Errorf("create tasks-it request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("call tasks-it: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, true, fmt.Errorf("read tasks-it response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests, fmt.Errorf("tasks-it returned HTTP %d", resp.StatusCode)
	}

	var response struct {
		Items []ImportResult `json:"items"`
	}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, false, fmt.Errorf("decode tasks-it response: %w", err)
	}

	return response.Items, false, nil
}
