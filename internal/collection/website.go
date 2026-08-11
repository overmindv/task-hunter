package collection

import (
	"context"
	"fmt"
	"net/http"

	"github.com/overmindv/task-hunter/internal/parser/domain"
	"github.com/overmindv/task-hunter/internal/parser/source/codeforces"
	"github.com/overmindv/task-hunter/internal/parser/source/coderun"
	"github.com/overmindv/task-hunter/internal/parser/source/leetcode"
)

// WebsiteReader загружает только одну заранее проверенную каноническую ссылку.
type WebsiteReader interface {
	ReadURL(ctx context.Context, sourceID, rawURL string) (domain.RawTask, error)
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

// ReadURL получает ровно одну задачу и никогда не обходит каталог сайта.
func (r *DirectWebsiteReader) ReadURL(ctx context.Context, sourceID, rawURL string) (domain.RawTask, error) {
	switch sourceID {
	case "codeforces":
		return r.codeforces.CollectURL(ctx, rawURL)
	case "leetcode":
		return r.leetcode.CollectURL(ctx, rawURL)
	case "coderun":
		return r.coderun.CollectURL(ctx, rawURL)
	default:
		return domain.RawTask{}, fmt.Errorf("unsupported website source %q", sourceID)
	}
}

// Close останавливает rate limiter адаптеров.
func (r *DirectWebsiteReader) Close() {
	_ = r.codeforces.Close()
	_ = r.leetcode.Close()
	_ = r.coderun.Close()
}
