//go:build telegram

package telegram

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/overmindv/task-hunter/internal/parser/domain"
)

var update = flag.Bool("update", false, "update golden files")

// TestParseMessage_Simple проверяет парсинг простого текстового сообщения.
func TestParseMessage_Simple(t *testing.T) {
	content := readFixture(t, "message_simple.txt")
	msg := MessageInfo{
		ID:        42,
		Text:      content,
		Timestamp: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
	}

	raw := ParseMessage(msg, "algoses", domain.SourceTelegramAlgorithms)

	got := formatRawTask(raw)
	goldenPath := filepath.Join("testdata", "message_simple.golden")

	if *update {
		writeGolden(t, goldenPath, got)
	}

	expected := readGolden(t, goldenPath)
	if got != expected {
		t.Errorf("simple message mismatch.\n  got:\n%s\n---\n  want:\n%s", got, expected)
	}

	if raw.SourceURL != "https://t.me/algoses/42" {
		t.Errorf("expected URL 'https://t.me/algoses/42', got %q", raw.SourceURL)
	}
	if raw.Source.ID != domain.SourceTelegramAlgorithms {
		t.Errorf("expected source %s, got %s", domain.SourceTelegramAlgorithms, raw.Source.ID)
	}
}

// TestParseMessage_WithCode проверяет парсинг сообщения с кодом.
func TestParseMessage_WithCode(t *testing.T) {
	content := readFixture(t, "message_with_code.txt")
	msg := MessageInfo{
		ID:        99,
		Text:      content,
		Timestamp: time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC),
	}

	raw := ParseMessage(msg, "algoses", domain.SourceTelegramAlgorithms)

	got := formatRawTask(raw)
	goldenPath := filepath.Join("testdata", "message_with_code.golden")

	if *update {
		writeGolden(t, goldenPath, got)
	}

	expected := readGolden(t, goldenPath)
	if got != expected {
		t.Errorf("code message mismatch.\n  got:\n%s\n---\n  want:\n%s", got, expected)
	}

	if !HasCodeBlocks(content) {
		t.Error("expected HasCodeBlocks to return true")
	}
}

// TestParseMessage_WithImage проверяет парсинг сообщения с изображением.
func TestParseMessage_WithImage(t *testing.T) {
	content := readFixture(t, "message_with_image.txt")
	msg := MessageInfo{
		ID:        77,
		Text:      content,
		Timestamp: time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
		HasMedia:  true,
		MediaType: "photo",
	}

	raw := ParseMessage(msg, "algoses", domain.SourceTelegramAlgorithms)

	// Изображение — без OCR, текст остаётся как есть
	if string(raw.RawContent) != content {
		t.Errorf("expected content unchanged for image message, got:\n%s", string(raw.RawContent))
	}

	if !IsImageAttachment(msg) {
		t.Error("expected IsImageAttachment to return true")
	}
}

// TestParseMessage_WithTextAttachment проверяет сообщение с текстовым файлом.
func TestParseMessage_WithTextAttachment(t *testing.T) {
	msg := MessageInfo{
		ID:        55,
		Text:      "Binary Search implementation:",
		Timestamp: time.Date(2024, 1, 15, 13, 0, 0, 0, time.UTC),
		HasMedia:  true,
		MediaType: "text",
		MediaData: []byte("def binary_search(arr, target):\n    left, right = 0, len(arr)-1\n    while left <= right:\n        mid = (left + right) // 2\n        if arr[mid] == target:\n            return mid\n        elif arr[mid] < target:\n            left = mid + 1\n        else:\n            right = mid - 1\n    return -1"),
	}

	raw := ParseMessage(msg, "algoses", domain.SourceTelegramAlgorithms)

	// Текст сообщения + содержимое файла
	if !strings.Contains(string(raw.RawContent), "Binary Search implementation") {
		t.Error("expected message text in content")
	}
	if !strings.Contains(string(raw.RawContent), "def binary_search") {
		t.Error("expected file content in RawTask")
	}

	if !HasMediaFile(msg) {
		t.Error("expected HasMediaFile to return true")
	}
}

// TestParseMessage_EmptyText проверяет пустое сообщение.
func TestParseMessage_EmptyText(t *testing.T) {
	msg := MessageInfo{
		ID:        1,
		Text:      "",
		Timestamp: time.Now(),
	}

	raw := ParseMessage(msg, "algoses", domain.SourceTelegramAlgorithms)

	if string(raw.RawContent) != "" {
		t.Errorf("expected empty content, got %q", string(raw.RawContent))
	}
	if raw.SourceURL != "https://t.me/algoses/1" {
		t.Errorf("expected URL 'https://t.me/algoses/1', got %q", raw.SourceURL)
	}
}

// TestSourceURL_Format проверяет разные форматы SourceURL.
func TestSourceURL_Format(t *testing.T) {
	tests := []struct {
		username string
		msgID    int
		expected string
	}{
		{"algoses", 42, "https://t.me/algoses/42"},
		{"analytic_postupashki", 1, "https://t.me/analytic_postupashki/1"},
		{"postupashki_ml", 999, "https://t.me/postupashki_ml/999"},
	}

	for _, tc := range tests {
		t.Run(tc.username, func(t *testing.T) {
			url := SourceURL(tc.username, tc.msgID)
			if url != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, url)
			}
		})
	}
}

// TestHasCodeBlocks проверяет детекцию блоков кода.
func TestHasCodeBlocks(t *testing.T) {
	if !HasCodeBlocks("text ```code``` more") {
		t.Error("expected true for text with code fences")
	}
	if !HasCodeBlocks("text `inline code` more") {
		t.Error("expected true for text with inline code")
	}
	if HasCodeBlocks("plain text without code") {
		t.Error("expected false for plain text")
	}
	if HasCodeBlocks("") {
		t.Error("expected false for empty text")
	}
}

// TestTextLengthWithoutCode проверяет подсчёт длины без кода.
func TestTextLengthWithoutCode(t *testing.T) {
	text := "Question\n```\nlong code block\n```\nMore text `inline` end"

	length := TextLengthWithoutCode(text)
	totalLen := len(text)

	if length >= totalLen {
		t.Errorf("expected length (%d) < total length (%d)", length, totalLen)
	}
	if length <= 0 {
		t.Errorf("expected positive length, got %d", length)
	}
}

// TestParseMessage_SameContent проверяет консистентность контента.
func TestParseMessage_SameContent(t *testing.T) {
	msg := MessageInfo{ID: 1, Text: "Same text", Timestamp: time.Now()}

	raw1 := ParseMessage(msg, "algoses", domain.SourceTelegramAlgorithms)
	raw2 := ParseMessage(msg, "algoses", domain.SourceTelegramAlgorithms)

	if string(raw1.RawContent) != string(raw2.RawContent) {
		t.Error("expected same content for same message parsed twice")
	}
	if raw1.SourceURL != raw2.SourceURL {
		t.Error("expected same URL for same message")
	}
}

// --- helpers ---

func formatRawTask(raw domain.RawTask) string {
	var b strings.Builder
	b.WriteString("Source: " + string(raw.Source.ID) + "\n")
	b.WriteString("Name: " + raw.Source.Name + "\n")
	b.WriteString("URL: " + raw.SourceURL + "\n")
	b.WriteString("Hash: not-set-on-rawtask\n")
	b.WriteString("ContentLen: " + fmtLen(len(raw.RawContent)) + "\n")
	b.WriteString("---\n")
	b.WriteString(string(raw.RawContent))
	return b.String()
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func writeGolden(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write golden %s: %v", path, err)
	}
}

func readGolden(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	return string(data)
}

func fmtLen(n int) string {
	return fmt.Sprintf("%d", n)
}
