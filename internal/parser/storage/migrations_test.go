package storage

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestRunMigrations проверяет накат миграций на тестовой БД.
// Требует PostgreSQL (через переменную PARSER_TEST_DATABASE_DSN).
func TestRunMigrations(t *testing.T) {
	dbURL := getTestDSN(t)

	err := RunMigrations(dbURL, "../../../migrations")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Проверяем, что таблицы созданы
	db := MustOpenDB(dbURL)
	defer db.Close()

	checkTableExists(t, db, "tasks")
	checkTableExists(t, db, "examples")
	checkTableExists(t, db, "task_tags")
}

// TestMigrations_Idempotent проверяет, что повторный запуск миграций не вызывает ошибку.
func TestMigrations_Idempotent(t *testing.T) {
	dbURL := getTestDSN(t)

	// Первый накат
	if err := RunMigrations(dbURL, "../../../migrations"); err != nil {
		t.Fatalf("first run failed: %v", err)
	}

	// Второй накат — должен пройти без ошибки (goose пропускает готовые)
	if err := RunMigrations(dbURL, "../../../migrations"); err != nil {
		t.Fatalf("second run (idempotent) failed: %v", err)
	}
}

// TestMigrations_Rollback проверяет откат и повторный накат миграций.
func TestMigrations_Rollback(t *testing.T) {
	dbURL := getTestDSN(t)

	// Накат
	if err := RunMigrations(dbURL, "../../../migrations"); err != nil {
		t.Fatalf("up migration failed: %v", err)
	}

	// Откат
	if err := RunMigrationsDown(dbURL, "../../../migrations"); err != nil {
		t.Fatalf("down migration failed: %v", err)
	}

	// После отката таблиц быть не должно
	db := MustOpenDB(dbURL)
	defer db.Close()

	checkTableNotExists(t, db, "tasks")
	checkTableNotExists(t, db, "examples")
	checkTableNotExists(t, db, "task_tags")

	// Повторный накат
	if err := RunMigrations(dbURL, "../../../migrations"); err != nil {
		t.Fatalf("re-up migration failed: %v", err)
	}

	checkTableExists(t, db, "tasks")
}

// TestMigrations_UniqueHash проверяет, что source_hash UNIQUE работает.
func TestMigrations_UniqueHash(t *testing.T) {
	dbURL := getTestDSN(t)

	if err := RunMigrations(dbURL, "../../../migrations"); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	db := MustOpenDB(dbURL)
	defer db.Close()

	ctx := context.Background()

	// Вставляем первую задачу
	_, err := db.ExecContext(ctx, `
		INSERT INTO tasks (id, title, description, source_id, source_url, source_hash, type, difficulty)
		VALUES (gen_random_uuid(), 'Test', 'Desc', 'leetcode', 'url1', 'hash123', 0, 1)
	`)
	if err != nil {
		t.Fatalf("insert first task failed: %v", err)
	}

	// Вставляем вторую с тем же хешем — должно упасть на constraint
	_, err = db.ExecContext(ctx, `
		INSERT INTO tasks (id, title, description, source_id, source_url, source_hash, type, difficulty)
		VALUES (gen_random_uuid(), 'Test2', 'Desc2', 'leetcode', 'url2', 'hash123', 0, 1)
	`)
	if err == nil {
		t.Fatal("expected unique constraint error for duplicate source_hash, got nil")
	}
}

// TestMigrations_CascadeDelete проверяет каскадное удаление при удалении задачи.
func TestMigrations_CascadeDelete(t *testing.T) {
	dbURL := getTestDSN(t)

	if err := RunMigrations(dbURL, "../../../migrations"); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	db := MustOpenDB(dbURL)
	defer db.Close()

	ctx := context.Background()

	// Вставляем задачу с примером и тегом
	_, err := db.ExecContext(ctx, `
		INSERT INTO tasks (id, title, description, source_id, source_url, source_hash, type, difficulty)
		VALUES ('00000000-0000-0000-0000-000000000001', 'Test', 'Desc', 'leetcode', 'url', 'unique_hash', 0, 1)
	`)
	if err != nil {
		t.Fatalf("insert task failed: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO examples (task_id, input, output)
		VALUES ('00000000-0000-0000-0000-000000000001', 'in', 'out')
	`)
	if err != nil {
		t.Fatalf("insert example failed: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO task_tags (task_id, tag)
		VALUES ('00000000-0000-0000-0000-000000000001', 'array')
	`)
	if err != nil {
		t.Fatalf("insert tag failed: %v", err)
	}

	// Удаляем задачу — примеры и теги должны удалиться каскадно
	_, err = db.ExecContext(ctx, `DELETE FROM tasks WHERE id = '00000000-0000-0000-0000-000000000001'`)
	if err != nil {
		t.Fatalf("delete task failed: %v", err)
	}

	// Проверяем, что примеры удалились
	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM examples WHERE task_id = '00000000-0000-0000-0000-000000000001'`).Scan(&count)
	if err != nil {
		t.Fatalf("count examples failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 examples after cascade delete, got %d", count)
	}

	// Проверяем, что теги удалились
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_tags WHERE task_id = '00000000-0000-0000-0000-000000000001'`).Scan(&count)
	if err != nil {
		t.Fatalf("count tags failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 tags after cascade delete, got %d", count)
	}
}

// --- helpers ---

// getTestDSN возвращает DSN тестовой БД или пропускает тест.
func getTestDSN(t *testing.T) string {
	t.Helper()

	// Можно задать через PARSER_TEST_DATABASE_DSN или использовать дефолтную для тестов
	// Для локального запуска: export PARSER_TEST_DATABASE_DSN=postgres://localhost:5432/diploma_test?sslmode=disable
	// TODO: заменить на testcontainers для CI
	dbURL := "postgres://postgres:postgres@localhost:5433/diploma_test?sslmode=disable"
	t.Logf("using test database: %s", dbURL)
	return dbURL
}

func checkTableExists(t *testing.T, db *sql.DB, tableName string) {
	t.Helper()

	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = $1
		)
	`, tableName).Scan(&exists)
	if err != nil {
		t.Fatalf("check table %s exists: %v", tableName, err)
	}
	if !exists {
		t.Fatalf("table %s does not exist after migration", tableName)
	}
}

func checkTableNotExists(t *testing.T, db *sql.DB, tableName string) {
	t.Helper()

	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = $1
		)
	`, tableName).Scan(&exists)
	if err != nil {
		t.Fatalf("check table %s not exists: %v", tableName, err)
	}
	if exists {
		t.Fatalf("table %s still exists after rollback", tableName)
	}
}
