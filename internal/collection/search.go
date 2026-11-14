package collection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type searchProblem struct {
	ContestID int      `json:"contestId"`
	Index     string   `json:"index"`
	Name      string   `json:"name"`
	Rating    int      `json:"rating"`
	Tags      []string `json:"tags"`
}
type searchResult struct {
	Title  string   `json:"title"`
	URL    string   `json:"url"`
	Rating int      `json:"rating"`
	Tags   []string `json:"tags"`
}
type taskSearch struct {
	mu       sync.Mutex
	client   *http.Client
	problems []searchProblem
	expires  time.Time
	retryAt  time.Time
}

var problemIndex = regexp.MustCompile(`^[A-Z][0-9]{0,2}$`)

// newTaskSearch создаёт общий кеш каталога; пользователь не управляет URL загрузки.
func newTaskSearch() *taskSearch {
	return &taskSearch{client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// search фильтрует официальный каталог по названию и тегам, не загружает условия задач.
func (s *taskSearch) search(ctx context.Context, query string) ([]searchResult, error) {
	if !s.mu.TryLock() {
		return nil, errors.New("search busy")
	}
	defer s.mu.Unlock()
	if time.Now().After(s.expires) {
		if time.Now().Before(s.retryAt) {
			return nil, errors.New("search unavailable")
		}
		// Даже при ошибке не повторяем внешний запрос при каждом вводе пользователя.
		s.retryAt = time.Now().Add(time.Minute)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://codeforces.com/api/problemset.problems", nil)
		if err != nil {
			return nil, errors.New("search unavailable")
		}
		req.Header.Set("User-Agent", "Overmindv-task-discovery/1.0")
		res, err := s.client.Do(req)
		if err != nil {
			return nil, errors.New("search unavailable")
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != 200 {
			return nil, errors.New("search unavailable")
		}
		data, err := io.ReadAll(io.LimitReader(res.Body, 16<<20+1))
		if err != nil || len(data) > 16<<20 {
			return nil, errors.New("search unavailable")
		}
		var body struct {
			Status string `json:"status"`
			Result struct {
				Problems []searchProblem `json:"problems"`
			} `json:"result"`
		}
		if json.Unmarshal(data, &body) != nil || body.Status != "OK" || len(body.Result.Problems) > 50000 {
			return nil, errors.New("search unavailable")
		}
		s.problems = body.Result.Problems
		s.expires = time.Now().Add(30 * time.Minute)
	}
	words := strings.Fields(strings.ToLower(query))
	aliases := map[string]string{"графы": "graphs", "граф": "graphs", "строки": "strings", "сортировка": "sortings", "деревья": "trees", "математика": "math", "динамика": "dp", "бинарный": "binary", "поиск": "search"}
	for i, w := range words {
		if a, ok := aliases[w]; ok {
			words[i] = a
		}
	}
	result := []searchResult{}
	for _, p := range s.problems {
		if p.ContestID <= 0 || !problemIndex.MatchString(p.Index) || p.Name == "" {
			continue
		}
		text := strings.ToLower(p.Name + " " + strings.Join(p.Tags, " "))
		matched := true
		for _, word := range words {
			if !strings.Contains(text, word) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		tags := p.Tags
		if tags == nil {
			tags = []string{}
		}
		result = append(result, searchResult{p.Name, fmt.Sprintf("https://codeforces.com/problemset/problem/%d/%s", p.ContestID, p.Index), p.Rating, tags})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Rating != result[j].Rating {
			return result[i].Rating < result[j].Rating
		}
		return result[i].URL < result[j].URL
	})
	if len(result) > 20 {
		result = result[:20]
	}
	return result, nil
}

// serve возвращает безопасную ошибку отдельно от пустого поиска.
func (s *taskSearch) serve(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || !utf8.ValidString(q) || utf8.RuneCountInString(q) > 100 {
		writeError(w, 400, "INVALID_INPUT", "Invalid query")
		return
	}
	result, err := s.search(r.Context(), q)
	if err != nil {
		writeError(w, 503, "SERVICE_UNAVAILABLE", "Search unavailable")
		return
	}
	writeJSON(w, 200, result)
}
