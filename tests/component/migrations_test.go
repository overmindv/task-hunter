package component

import (
	"database/sql"
	"testing"

	"diploma/internal/parser/storage"
)

// TestMigrations_Run проверяет накат миграций.
func TestMigrations_Run(t *testing.T) {
	// Чистая БД
	_ = storage.RunMigrationsDown(testDBURL, migrationsDir)
	if err := storage.RunMigrations(testDBURL, migrationsDir); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	db := storage.MustOpenDB(testDBURL)
	defer db.Close()

	checkTableExists(t, db, "tasks")
	checkTableExists(t, db, "examples")
	checkTableExists(t, db, "task_tags")
}

// TestMigrations_Idempotent проверяет идемпотентность миграций.
func TestMigrations_Idempotent(t *testing.T) {
	_ = storage.RunMigrationsDown(testDBURL, migrationsDir)
	if err := storage.RunMigrations(testDBURL, migrationsDir); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := storage.RunMigrations(testDBURL, migrationsDir); err != nil {
		t.Fatalf("second run (idempotent): %v", err)
	}
}

// TestMigrations_Rollback проверяет откат и повторный накат.
func TestMigrations_Rollback(t *testing.T) {
	_ = storage.RunMigrationsDown(testDBURL, migrationsDir)

	// Накат
	if err := storage.RunMigrations(testDBURL, migrationsDir); err != nil {
		t.Fatalf("up: %v", err)
	}

	db := storage.MustOpenDB(testDBURL)
	defer db.Close()

	// Откат
	if err := storage.RunMigrationsDown(testDBURL, migrationsDir); err != nil {
		t.Fatalf("down: %v", err)
	}

	checkTableNotExists(t, db, "tasks")

	// Повторный накат
	if err := storage.RunMigrations(testDBURL, migrationsDir); err != nil {
		t.Fatalf("re-up: %v", err)
	}

	checkTableExists(t, db, "tasks")
}

// TestMigrations_UniqueHash проверяет UNIQUE constraint на source_hash.
func TestMigrations_UniqueHash(t *testing.T) {
	_ = storage.RunMigrationsDown(testDBURL, migrationsDir)
	if err := storage.RunMigrations(testDBURL, migrationsDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	db := storage.MustOpenDB(testDBURL)
	defer db.Close()

	_, err := db.Exec(`
		INSERT INTO tasks (id, title, description, source_id, source_url, source_hash, type, difficulty)
		VALUES (gen_random_uuid(), 'T1', 'D1', 'leetcode', 'url1', 'hash123', 0, 1)
	`)
	if err != nil {
		t.Fatalf("insert first: %v", err)
	}

	_, err = db.Exec(`
		INSERT INTO tasks (id, title, description, source_id, source_url, source_hash, type, difficulty)
		VALUES (gen_random_uuid(), 'T2', 'D2', 'leetcode', 'url2', 'hash123', 0, 1)
	`)
	if err == nil {
		t.Fatal("expected unique constraint error, got nil")
	}
}

// TestMigrations_CascadeDelete проверяет каскадное удаление.
func TestMigrations_CascadeDelete(t *testing.T) {
	_ = storage.RunMigrationsDown(testDBURL, migrationsDir)
	if err := storage.RunMigrations(testDBURL, migrationsDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	db := storage.MustOpenDB(testDBURL)
	defer db.Close()

	_, err := db.Exec(`
		INSERT INTO tasks (id, title, description, source_id, source_url, source_hash, type, difficulty)
		VALUES ('00000000-0000-0000-0000-000000000001', 'T', 'D', 'lc', 'u', 'h1', 0, 1)
	`)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}

	_, err = db.Exec(`
		INSERT INTO examples (task_id, input, output)
		VALUES ('00000000-0000-0000-0000-000000000001', 'in', 'out')
	`)
	if err != nil {
		t.Fatalf("insert example: %v", err)
	}

	_, err = db.Exec(`DELETE FROM tasks WHERE id = '00000000-0000-0000-0000-000000000001'`)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM examples WHERE task_id = '00000000-0000-0000-0000-000000000001'`).Scan(&count)
	if err != nil {
		t.Fatalf("count examples: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 examples after cascade, got %d", count)
	}
}

// --- helpers ---

func checkTableExists(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	var exists bool
	err := db.QueryRow(`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)`, name).Scan(&exists)
	if err != nil {
		t.Fatalf("check %s exists: %v", name, err)
	}
	if !exists {
		t.Fatalf("table %s not found", name)
	}
}

func checkTableNotExists(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	var exists bool
	err := db.QueryRow(`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)`, name).Scan(&exists)
	if err != nil {
		t.Fatalf("check %s not exists: %v", name, err)
	}
	if exists {
		t.Fatalf("table %s still exists", name)
	}
}
