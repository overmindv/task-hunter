// Парсер задач — точка входа.
//
// Запуск: go run .
// Или: go run ./cmd/parser
//
// Конфигурация: переменные окружения с префиксом PARSER_.
// Пример:
//
//	export PARSER_DATABASE_DSN=postgres://localhost:5432/tasks
//	export PARSER_SCHEDULE_COLLECTCRON="0 */6 * * *"
//	go run .
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"diploma/config"
	"diploma/internal/parser"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	if err := parser.RunApp(ctx, cfg); err != nil {
		slog.Error("parser: fatal error", "error", err)
		os.Exit(1)
	}
}
