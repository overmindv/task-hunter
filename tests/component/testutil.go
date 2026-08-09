// Package component содержит компонентные тесты, требующие реальную БД.
package component

import (
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"diploma/internal/parser/storage"
)

const (
	testDBURL     = "postgres://postgres:postgres@localhost:5433/diploma_test?sslmode=disable"
	migrationsDir = "../../migrations"
)

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
