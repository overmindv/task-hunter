package collection

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

type searchTransport func(*http.Request) (*http.Response, error)

func (f searchTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestTaskSearchCachesAndFilters(t *testing.T) {
	calls := 0
	s := newTaskSearch()
	s.client.Transport = searchTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://codeforces.com/api/problemset.problems" {
			t.Fatal("unexpected host")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"status":"OK","result":{"problems":[{"contestId":1,"index":"A","name":"Graph","rating":800,"tags":["graphs"]},{"contestId":2,"index":"../x","name":"Graph","tags":["graphs"]}]}}`))}, nil
	})
	for i := 0; i < 2; i++ {
		out, err := s.search(context.Background(), "графы")
		if err != nil || len(out) != 1 || out[0].URL != "https://codeforces.com/problemset/problem/1/A" {
			t.Fatal(out, err)
		}
	}
	if calls != 1 {
		t.Fatal("cache not used")
	}
	out, err := s.search(context.Background(), "NLP")
	if err != nil || len(out) != 0 {
		t.Fatal("no match confused with failure")
	}
}
func TestTaskSearchFailureCooldownAndInput(t *testing.T) {
	s := newTaskSearch()
	calls := 0
	s.client.Transport = searchTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("private upstream details"))}, nil
	})
	for i := 0; i < 2; i++ {
		if _, err := s.search(context.Background(), "graphs"); err == nil {
			t.Fatal("failure hidden")
		}
	}
	if calls != 1 || time.Until(s.retryAt) <= 0 {
		t.Fatal("no cooldown")
	}
	w := httptest.NewRecorder()
	s.serve(w, httptest.NewRequest("GET", "/?q=", nil))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}
func TestTaskSearchRejectsUnauthenticated(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, nil, nil, "token", nil, 1, "")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/v1/admin/task-search?q=graphs", nil))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}

func TestLiveTaskSearch(t *testing.T) {
	if os.Getenv("TASK_SEARCH_LIVE_TEST") != "true" {
		t.Skip("manual public catalog smoke")
	}
	out, err := newTaskSearch().search(context.Background(), "graphs")
	if err != nil || len(out) == 0 {
		t.Fatalf("public catalog unavailable: %v", err)
	}
	t.Logf("Public catalog returned %d canonical results", len(out))
}
