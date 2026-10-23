// Package store opens the SQLite database and applies the schema.
package store

import (
	"database/sql"
	"database/sql/driver"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"modernc.org/sqlite"
)

// lowlex_contains(text, needle) is a substring test that ignores case in every script,
// unlike LIKE, which folds ASCII only. needle must already be lower case.
func init() {
	sqlite.MustRegisterDeterministicScalarFunction("lowlex_contains", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		text, _ := args[0].(string)
		needle, _ := args[1].(string)
		if b, ok := args[0].([]byte); ok {
			text = string(b)
		}
		if strings.Contains(strings.ToLower(text), needle) {
			return int64(1), nil
		}
		return int64(0), nil
	})
}

//go:embed schema.sql
var schemaV1 string

//go:embed schema2.sql
var schemaV2 string

//go:embed schema3.sql
var schemaV3 string

// migrations[i] moves the database from user_version i to i+1.
var migrations = []string{schemaV1, schemaV2, schemaV3}

// Open creates the data directory if needed, opens lowlex.db and migrates it.
func Open(dataDir string) (*sql.DB, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dsn := "file:" + filepath.ToSlash(filepath.Join(dataDir, "lowlex.db")) +
		"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// WAL lets readers run beside the single writer; immediate transactions take the
	// write lock up front, so concurrent writers wait instead of failing mid-transaction.
	db.SetMaxOpenConns(8)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
