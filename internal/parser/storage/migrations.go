// Package storage предоставляет слой работы с базой данных.
package storage

import (
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"

	// Драйвер PostgreSQL для database/sql (используется goose и go-jet).
	_ "github.com/jackc/pgx/v5/stdlib"
)

const driverName = "pgx"

// RunMigrations применяет все миграции из указанной директории.
// При повторном вызове goose пропускает уже накаченные миграции
// (сверяет по таблице goose_db_version).
func RunMigrations(dbURL string, migrationsDir string) error {
	db, err := sql.Open(driverName, dbURL)
	if err != nil {
		return fmt.Errorf("open db for migrations: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("ping db for migrations: %w", err)
	}

	if err := goose.Up(db, migrationsDir); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}

// RunMigrationsDown откатывает все миграции.
// Используется в тестах для очистки БД.
func RunMigrationsDown(dbURL string, migrationsDir string) error {
	db, err := sql.Open(driverName, dbURL)
	if err != nil {
		return fmt.Errorf("open db for rollback: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := goose.DownTo(db, migrationsDir, 0); err != nil {
		return fmt.Errorf("rollback migrations: %w", err)
	}

	return nil
}

// MustOpenDB открывает подключение к БД или паникует.
// Используется в тестах для быстрого получения *sql.DB.
func MustOpenDB(dbURL string) *sql.DB {
	db, err := sql.Open(driverName, dbURL)
	if err != nil {
		panic(fmt.Errorf("open db: %w", err))
	}
	return db
}
