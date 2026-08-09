.PHONY: build run test lint fmt clean docker-build docker-up docker-down \
        migrate-up migrate-down migrate-status codegen codegraph help

# --- Переменные ---
APP_NAME     := task-collector
CMD_DIR      := ./cmd/parser
BIN_DIR      := ./bin
BINARY       := $(BIN_DIR)/parser
DOCKER_TAG   := $(APP_NAME):latest

GO           := go
GOFLAGS      := -ldflags="-s -w"
GOTESTFLAGS  := -count=1 -timeout=60s
GOLANGCILINT := golangci-lint

# --- Сборка ---

build: ## Собрать бинарник
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -o $(BINARY) $(CMD_DIR)

build-all: ## Собрать все пакеты (без бинарника)
	$(GO) build ./...

# --- Запуск ---

run: ## Запустить приложение (требует PARSER_DATABASE_DSN)
	$(GO) run $(CMD_DIR)

run-dev: ## Запустить с PostgreSQL в Docker
	@echo "Starting PostgreSQL..."
	@docker compose up -d postgres
	@echo "Waiting for PostgreSQL to be ready..."
	@sleep 3
	$(GO) run $(CMD_DIR)

# --- Тестирование ---

test: ## Запустить все тесты
	$(GO) test $(GOTESTFLAGS) ./... ./tests/...

test-unit: ## Запустить unit-тесты
	$(GO) test $(GOTESTFLAGS) ./internal/...

test-component: ## Запустить компонентные тесты (требуют БД)
	$(GO) test $(GOTESTFLAGS) ./tests/...

test-coverage: ## Запустить тесты с отчётом о покрытии
	$(GO) test $(GOTESTFLAGS) -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

test-update: ## Обновить golden-файлы
	$(GO) test -update ./internal/parser/classifier/...
	$(GO) test -update ./internal/parser/source/codeforces/...
	$(GO) test -update ./internal/parser/source/coderun/...

# --- Линтинг ---

lint: ## Запустить golangci-lint
	$(GOLANGCILINT) run ./...

lint-fast: ## Быстрый линтинг (без внешних проверок)
	$(GOLANGCILINT) run --fast ./...

# --- Форматирование ---

fmt: ## Отформатировать код
	$(GO) fmt ./...

fmt-check: ## Проверить форматирование
	test -z "$(shell $(GO) fmt ./...)"

# --- Docker ---

docker-build: ## Собрать Docker-образ
	docker build -t $(DOCKER_TAG) .

docker-up: ## Запустить все сервисы (PostgreSQL + парсер)
	docker compose up -d --build

docker-down: ## Остановить все сервисы
	docker compose down -v

docker-logs: ## Посмотреть логи
	docker compose logs -f

docker-restart: ## Перезапустить сервисы
	docker compose restart

# --- Миграции ---

migrate-up: ## Накатить миграции
	goose -dir migrations postgres "$(PARSER_DATABASE_DSN)" up

migrate-down: ## Откатить миграции
	goose -dir migrations postgres "$(PARSER_DATABASE_DSN)" down

migrate-status: ## Статус миграций
	goose -dir migrations postgres "$(PARSER_DATABASE_DSN)" status

migrate-create: ## Создать новую миграцию (usage: make migrate-create NAME=<name>)
	goose -dir migrations create $(NAME) sql

# --- Codegen ---

codegen: ## Сгенерировать код go-jet
	jet -source=postgres -dsn="$(PARSER_DATABASE_DSN)" -path=internal/parser/storage/jet

codegraph: ## Синхронизировать CodeGraph
	codegraph sync

# --- Очистка ---

clean: ## Очистить временные файлы
	rm -rf $(BIN_DIR) coverage.out coverage.html
	$(GO) clean -cache

# --- Справка ---

help: ## Показать эту справку
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
