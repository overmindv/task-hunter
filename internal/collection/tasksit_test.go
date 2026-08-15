package collection

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

// TestTasksClientRetriesTemporaryFailure проверяет ограниченный retry для 5xx.
func TestTasksClientRetriesTemporaryFailure(t *testing.T) {
	t.Parallel()

	client := NewTasksClient("http://tasks.local", "service-token", time.Second, 1)
	attempts := 0
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		if request.Header.Get("Authorization") != "Bearer service-token" {
			t.Fatalf("service token is missing")
		}
		if attempts == 1 {
			return response(http.StatusServiceUnavailable, `{}`), nil
		}

		return response(http.StatusOK, `{"items":[{"external_id":"source:1","status":"imported"}]}`), nil
	})

	results, err := client.Import(context.Background(), []Candidate{{ExternalID: "source:1"}})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || len(results) != 1 || results[0].Status != "imported" {
		t.Fatalf("unexpected retry result: attempts=%d results=%+v", attempts, results)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip выполняет HTTP-вызов без сети.
func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

// response создаёт закрываемый HTTP response для клиента.
func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}
