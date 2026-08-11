# ---- Build stage ----
FROM golang:1.26-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Копируем модули для кеширования зависимостей
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходный код
COPY . .

# Сборка бинарника
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o /app/task-hunter ./cmd/task-hunter
RUN GOBIN=/app go install -tags="no_clickhouse no_mssql no_mysql no_sqlite3 no_libsql no_ydb no_vertica" github.com/pressly/goose/v3/cmd/goose@v3.27.3

# ---- Run stage ----
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata wget

WORKDIR /app

# Копируем бинарник
COPY --from=builder /app/task-hunter /usr/local/bin/task-hunter
COPY --from=builder /app/goose /usr/local/bin/goose

# Копируем миграции
COPY --from=builder /app/migrations ./migrations

# Пользователь без root
RUN addgroup -S -g 10001 taskhunter \
    && adduser -S -D -H -u 10001 -G taskhunter taskhunter \
    && mkdir -p /var/lib/task-hunter \
    && chown -R taskhunter:taskhunter /var/lib/task-hunter
USER taskhunter

# Точка входа
EXPOSE 8080
ENTRYPOINT ["task-hunter"]
