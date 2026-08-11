package coderun

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/overmindv/task-hunter/internal/parser/domain"
)

// TestProblemToStructuredRawTask проверяет динамическое условие и примеры CodeRun.
func TestProblemToStructuredRawTask(t *testing.T) {
	statementURL := "https://contest-problem-files.s3-private.mds.yandex.net/statement"
	inputURL := "https://contest-problem-files.s3-private.mds.yandex.net/input"
	outputURL := "https://contest-problem-files.s3-private.mds.yandex.net/output"
	page := fmt.Sprintf(`<html><body><main><h1>Ход конём</h1><dl><dt>Ограничение времени</dt><dd>1 с</dd></dl></main><script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"values":{"problem":{"slug":"knight-move","title":"Ход конём","difficulty":"EASY","tags":["dp"],"statements":{"renderedStatements":{"url":%q},"samples":[{"input":{"s3File":{"url":%q}},"output":{"s3File":{"url":%q}}}]}}}}}}</script></body></html>`, statementURL, inputURL, outputURL)
	mock := &mockHTTPClient{responses: map[string]mockResponse{
		statementURL: {statusCode: 200, body: `{"legend":"<p>Найдите число маршрутов.</p>","inputFormat":"<p>Даны N и M.</p>","outputFormat":"<p>Выведите ответ.</p>"}`},
		inputURL:     {statusCode: 200, body: "3 3\n"},
		outputURL:    {statusCode: 200, body: "2\n"},
	}}
	collector := NewCollector(domain.SourceCodeRun, mock).WithMinInterval(time.Millisecond)
	raw, err := collector.problemToStructuredRawTask(context.Background(), catalogProblem{Slug: "knight-move"}, page)
	if err != nil {
		t.Fatal(err)
	}
	if raw.Title != "Ход конём" || raw.Difficulty != domain.DifficultyEasy || !strings.Contains(raw.Statement, "Входные данные") {
		t.Fatalf("unexpected task: %#v", raw)
	}
	if len(raw.Examples) != 1 || raw.Examples[0].Input != "3 3" || raw.Examples[0].Output != "2" {
		t.Fatalf("unexpected examples: %#v", raw.Examples)
	}
	if len(raw.Constraints) != 1 || raw.Tags[0] != "dp" {
		t.Fatalf("unexpected metadata: constraints=%#v tags=%#v", raw.Constraints, raw.Tags)
	}
}
