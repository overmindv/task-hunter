//go:build component

// Package component содержит компонентные тесты, требующие реальную БД.
package component

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/overmindv/task-hunter/internal/parser/storage"
)

const migrationsDir = "../../migrations"

var testDBURL = componentDSN()

// componentDSN возвращает явный DSN или совместимый локальный default.
func componentDSN() string {
	if value := os.Getenv("COMPONENT_TEST_DSN"); value != "" {
		return value
	}

	return "postgres://postgres:postgres@localhost:5433/task_hunter_test?sslmode=disable"
}

// setupDB подготавливает чистое состояние БД: откатывает и накатывает миграции.
// Возвращает *sql.DB и репозиторий.
func setupDB(t *testing.T) (*sql.DB, *storage.PostgresRepository) {
	t.Helper()

	// Откатываем и накатываем для чистого состояния
	_ = storage.RunMigrationsDown(testDBURL, migrationsDir)
	if err := storage.RunMigrations(testDBURL, migrationsDir); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}

	db := storage.MustOpenDB(testDBURL)
	t.Cleanup(func() { db.Close() })

	return db, storage.NewPostgresRepository(db)
}
