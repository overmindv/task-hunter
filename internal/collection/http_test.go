package collection

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testActorID = "11111111-1111-4111-8111-111111111111"

// TestHTTPHandlerProtectsAdminSources проверяет service token, роль и allowlist response.
func TestHTTPHandlerProtectsAdminSources(t *testing.T) {
	t.Parallel()

	handler := NewHTTPHandler(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), "gateway-token", []string{"allowed_channel"}, 100, "")
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/admin/collection-sources", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected unauthorized status: %d", unauthorized.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/admin/collection-sources", nil)
	request.Header.Set("Authorization", "Bearer gateway-token")
	request.Header.Set("X-User-ID", testActorID)
	request.Header.Set("X-User-Roles", "admin")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "allowed_channel") {
		t.Fatalf("unexpected sources response: status=%d body=%s", response.Code, response.Body.String())
	}
}

// TestHTTPHandlerRejectsUnsafeURLBeforeStorage проверяет SSRF-валидацию до обращения к БД.
func TestHTTPHandlerRejectsUnsafeURLBeforeStorage(t *testing.T) {
	t.Parallel()

	handler := NewHTTPHandler(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), "gateway-token", nil, 100, "")
	request := httptest.NewRequest(http.MethodPost, "/v1/admin/collection-jobs", strings.NewReader(`{
        "idempotency_key":"22222222-2222-4222-8222-222222222222",
        "website_urls":["https://127.0.0.1/problem"],
        "max_items_per_source":100
    }`))
	request.Header.Set("Authorization", "Bearer gateway-token")
	request.Header.Set("X-User-ID", testActorID)
	request.Header.Set("X-User-Roles", "admin")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "INVALID_INPUT") {
		t.Fatalf("unexpected validation response: status=%d body=%s", response.Code, response.Body.String())
	}
}
