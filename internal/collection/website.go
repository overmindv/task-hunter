package collection

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/overmindv/task-hunter/internal/parser/domain"
	"github.com/overmindv/task-hunter/internal/parser/source/codeforces"
	"github.com/overmindv/task-hunter/internal/parser/source/coderun"
	"github.com/overmindv/task-hunter/internal/parser/source/leetcode"
)

// WebsiteBatch — задачи, полученные по одной ссылке, и опциональный problem-list,
// из которого они собраны (для лinks вида /problem-list/{slug}).
type WebsiteBatch struct {
	RawTasks    []domain.RawTask
	ProblemList *ProblemListRef
}

// WebsiteReader загружает заранее проверенные канонические ссылки.
// Одна ссылка может вернуть несколько задач (leetcode problem-list).
type WebsiteReader interface {
	ReadURL(ctx context.Context, sourceID, rawURL string) (WebsiteBatch, error)
	Close()
}

// DirectWebsiteReader маршрутизирует ссылки в адаптеры известных сайтов.
type DirectWebsiteReader struct {
	codeforces *codeforces.Collector
	leetcode   *leetcode.Collector
	coderun    *coderun.Collector
}

// NewDirectWebsiteReader создаёт HTTP-адаптер с запретом перехода по redirect.
func NewDirectWebsiteReader(timeoutClient *http.Client) *DirectWebsiteReader {
	timeoutClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &DirectWebsiteReader{
		codeforces: codeforces.NewCollector(domain.SourceCodeforces, timeoutClient),
		leetcode:   leetcode.NewCollector(domain.SourceLeetCode, timeoutClient),
		coderun:    coderun.NewCollector(domain.SourceCodeRun, timeoutClient),
	}
}

// WithCodeforcesReaderURL настраивает fallback для заблокированных HTML-страниц.
func (r *DirectWebsiteReader) WithCodeforcesReaderURL(rawURL string) *DirectWebsiteReader {
	r.codeforces.WithReaderURL(rawURL)

	return r
}

// ReadURL получает задачи по ссылке. Одиночная ссылка задачи возвращает один
// элемент, problem-list — все задачи списка и ссылку на сам список.
func (r *DirectWebsiteReader) ReadURL(ctx context.Context, sourceID, rawURL string) (WebsiteBatch, error) {
	switch sourceID {
	case "codeforces":
		raw, err := r.codeforces.CollectURL(ctx, rawURL)
		if err != nil {
			return WebsiteBatch{}, err
		}

		return WebsiteBatch{RawTasks: []domain.RawTask{raw}}, nil
	case "leetcode":
		if isLeetCodeProblemListURL(rawURL) {
			tasks, err := r.leetcode.CollectProblemListURL(ctx, rawURL)
			if err != nil {
				return WebsiteBatch{}, err
			}

			return WebsiteBatch{
				RawTasks: tasks,
				ProblemList: &ProblemListRef{
					Name:      problemListName(rawURL),
					SourceID:  sourceID,
					SourceURL: rawURL,
				},
			}, nil
		}
		raw, err := r.leetcode.CollectURL(ctx, rawURL)
		if err != nil {
			return WebsiteBatch{}, err
		}

		return WebsiteBatch{RawTasks: []domain.RawTask{raw}}, nil
	case "coderun":
		raw, err := r.coderun.CollectURL(ctx, rawURL)
		if err != nil {
			return WebsiteBatch{}, err
		}

		return WebsiteBatch{RawTasks: []domain.RawTask{raw}}, nil
	default:
		return WebsiteBatch{}, fmt.Errorf("unsupported website source %q", sourceID)
	}
}

// Close останавливает rate limiter адаптеров.
func (r *DirectWebsiteReader) Close() {
	_ = r.codeforces.Close()
	_ = r.leetcode.Close()
	_ = r.coderun.Close()
}

// isLeetCodeProblemListURL проверяет каноническую ссылку leetcode problem-list.
func isLeetCodeProblemListURL(rawURL string) bool {
	return strings.HasPrefix(rawURL, "https://leetcode.com/problem-list/")
}

// problemListName выводит display-имя списка из slug до переименования админом.
func problemListName(rawURL string) string {
	return strings.Trim(strings.TrimPrefix(rawURL, "https://leetcode.com/problem-list/"), "/")
}
