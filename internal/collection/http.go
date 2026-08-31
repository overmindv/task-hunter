package collection

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// HTTPHandler публикует health и защищённый admin API очереди.
type HTTPHandler struct {
	store           *Store
	logger          *slog.Logger
	gatewayToken    string
	allowedChannels []string
	allowedSet      map[string]struct{}
	defaultLimit    int
	sessionPath     string
}

// Router описывает минимальный контракт HTTP-роутера (parker.HTTPServer или *http.ServeMux в тестах).
type Router interface {
	Handle(pattern string, handler http.Handler)
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

// Register регистрирует защищённый admin API очереди на роутер parker.
// Liveness/readiness/metrics/middleware предоставляет parker.
func Register(router Router, store *Store, logger *slog.Logger, gatewayToken string, channels []string, defaultLimit int, sessionPath string) {
	allowedSet := make(map[string]struct{}, len(channels))
	allowedChannels := make([]string, 0, len(channels))

	for _, channel := range channels {
		normalized := strings.TrimPrefix(strings.TrimSpace(channel), "@")
		if normalized == "" {
			continue
		}
		allowedSet[normalized] = struct{}{}
		allowedChannels = append(allowedChannels, normalized)
	}

	handler := &HTTPHandler{
		store:           store,
		logger:          logger,
		gatewayToken:    gatewayToken,
		allowedChannels: allowedChannels,
		allowedSet:      allowedSet,
		defaultLimit:    defaultLimit,
		sessionPath:     sessionPath,
	}

	router.Handle("GET /v1/admin/collection-sources", handler.requireAdmin(http.HandlerFunc(handler.sources)))
	router.Handle("POST /v1/admin/collection-jobs", handler.requireAdmin(http.HandlerFunc(handler.createJob)))
	router.Handle("GET /v1/admin/collection-jobs", handler.requireAdmin(http.HandlerFunc(handler.listJobs)))
	router.Handle("GET /v1/admin/collection-jobs/{id}", handler.requireAdmin(http.HandlerFunc(handler.getJob)))
	router.Handle("POST /v1/admin/collection-jobs/{id}/acknowledge", handler.requireAdmin(http.HandlerFunc(handler.acknowledgeJob)))
}

// requireAdmin доверяет actor context только при корректном service token gateway.
func (h *HTTPHandler) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")

		if h.gatewayToken == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(h.gatewayToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Требуется авторизация")

			return
		}
		if !containsRole(r.Header.Get("X-User-Roles"), "admin") {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "Требуются права администратора")

			return
		}
		if _, err := uuid.Parse(r.Header.Get("X-User-ID")); err != nil {
			writeError(w, http.StatusUnauthorized, "INVALID_ACTOR", "Некорректный пользователь")

			return
		}

		next.ServeHTTP(w, r)
	})
}

// sources возвращает серверный allowlist, а не произвольные usernames.
func (h *HTTPHandler) sources(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"telegram_channels": h.allowedChannels,
		"website_sources":   []string{"codeforces", "leetcode", "coderun"},
	})
}

// createJob валидирует и ставит ручной сбор в очередь.
func (h *HTTPHandler) createJob(w http.ResponseWriter, r *http.Request) {
	var input CreateInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Некорректный JSON")

		return
	}

	if input.MaxItemsPerSource == 0 {
		input.MaxItemsPerSource = h.defaultLimit
	}

	for index := range input.TelegramChannels {
		input.TelegramChannels[index] = strings.TrimPrefix(strings.TrimSpace(input.TelegramChannels[index]), "@")
	}

	if err := ValidateCreateInput(input, h.allowedSet, time.Now().UTC()); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())

		return
	}

	actorID, _ := uuid.Parse(r.Header.Get("X-User-ID"))
	job, err := h.store.CreateManual(r.Context(), actorID, input)
	if err != nil {
		h.logger.Error("create collection job", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Не удалось создать задание")

		return
	}

	writeJSON(w, http.StatusAccepted, job)
}

// listJobs возвращает журнал или terminal jobs, ожидающие уведомления инициатора.
func (h *HTTPHandler) listJobs(w http.ResponseWriter, r *http.Request) {
	actorID, _ := uuid.Parse(r.Header.Get("X-User-ID"))
	limit := parseBoundedInt(r.URL.Query().Get("limit"), 20, 1, 100)
	offset := parseBoundedInt(r.URL.Query().Get("offset"), 0, 0, 100000)
	unread := r.URL.Query().Get("unread") == "true"

	items, err := h.store.List(r.Context(), actorID, unread, limit, offset)
	if err != nil {
		h.logger.Error("list collection jobs", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Не удалось получить задания")

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"limit":  limit,
		"offset": offset,
	})
}

// getJob возвращает детали задания вместе с результатами источников.
func (h *HTTPHandler) getJob(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Некорректный ID")

		return
	}

	job, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Задание не найдено")

		return
	}

	writeJSON(w, http.StatusOK, job)
}

// acknowledgeJob подтверждает уведомление только для инициатора manual job.
func (h *HTTPHandler) acknowledgeJob(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Некорректный ID")

		return
	}

	actorID, _ := uuid.Parse(r.Header.Get("X-User-ID"))
	if err := h.store.Acknowledge(r.Context(), id, actorID); err != nil {
		writeError(w, http.StatusConflict, "JOB_NOT_TERMINAL", "Уведомление нельзя подтвердить")

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// decodeJSON ограничивает размер тела и запрещает неизвестные поля.
func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request must contain one JSON object")
	}

	return nil
}

// containsRole проверяет CSV roles без нечётких совпадений.
func containsRole(raw, expected string) bool {
	for _, role := range strings.Split(raw, ",") {
		if strings.EqualFold(strings.TrimSpace(role), expected) {
			return true
		}
	}

	return false
}

// parseBoundedInt возвращает default для некорректного значения и ограничивает диапазон.
func parseBoundedInt(raw string, fallback, minValue, maxValue int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value < minValue || value > maxValue {
		return fallback
	}

	return value
}

// writeJSON пишет единый JSON response.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// writeError пишет машинный код и безопасное сообщение.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}
