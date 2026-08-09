# ---- Build stage ----
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Копируем модули для кеширования зависимостей
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходный код
COPY . .

# Сборка бинарника
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /app/parser ./cmd/parser

# ---- Run stage ----
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Копируем бинарник
COPY --from=builder /app/parser .

# Копируем миграции
COPY --from=builder /app/migrations ./migrations

# Пользователь без root
RUN adduser -D -h /app parser
USER parser

# Точка входа
ENTRYPOINT ["/app/parser"]
