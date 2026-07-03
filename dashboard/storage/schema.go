// Package storage implements SQLite-based metric storage.
package storage

import (
	"database/sql"
	"fmt"
	"time"

	"git.ardenone.com/jedarden/zai-proxy/dashboard/config"

	_ "modernc.org/sqlite"
)

// Schema manages the SQLite database schema.
type Schema struct {
	db *sql.DB
}

// NewSchema creates a new Schema manager.
func NewSchema(db *sql.DB) *Schema {
	return &Schema{db: db}
}

// Initialize creates the database schema if it doesn't exist.
func (s *Schema) Initialize() error {
	// Enable WAL mode for concurrent reads/writes
	if _, err := s.db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// High-resolution data (5-second intervals, 24h retention)
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS metrics_5s (
			ts INTEGER NOT NULL,
			variant TEXT NOT NULL,
			data TEXT NOT NULL,
			PRIMARY KEY (ts, variant)
		)
	`); err != nil {
		return fmt.Errorf("failed to create metrics_5s table: %w", err)
	}

	// Downsampled data (1-minute averages, 7-day retention)
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS metrics_1m (
			ts INTEGER NOT NULL,
			variant TEXT NOT NULL,
			data TEXT NOT NULL,
			PRIMARY KEY (ts, variant)
		)
	`); err != nil {
		return fmt.Errorf("failed to create metrics_1m table: %w", err)
	}

	// Indexes for range queries
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_5s_ts ON metrics_5s(ts)`); err != nil {
		return fmt.Errorf("failed to create idx_5s_ts: %w", err)
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_1m_ts ON metrics_1m(ts)`); err != nil {
		return fmt.Errorf("failed to create idx_1m_ts: %w", err)
	}

	return nil
}

// DDL returns the SQL statements to create the schema.
func (s *Schema) DDL() []string {
	return []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE IF NOT EXISTS metrics_5s (
			ts INTEGER NOT NULL,
			variant TEXT NOT NULL,
			data TEXT NOT NULL,
			PRIMARY KEY (ts, variant)
		)`,
		`CREATE TABLE IF NOT EXISTS metrics_1m (
			ts INTEGER NOT NULL,
			variant TEXT NOT NULL,
			data TEXT NOT NULL,
			PRIMARY KEY (ts, variant)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_5s_ts ON metrics_5s(ts)`,
		`CREATE INDEX IF NOT EXISTS idx_1m_ts ON metrics_1m(ts)`,
	}
}

// Config holds storage configuration.
type Config struct {
	DBPath      string        // Path to SQLite database file
	Retention5s time.Duration // Retention for 5s data (default 24h)
	Retention1m time.Duration // Retention for 1m data (default 168h = 7d)
}

// DefaultConfig returns the default storage configuration.
func DefaultConfig() Config {
	return Config{
		DBPath:      config.GetDBPath(),
		Retention5s: config.GetRetention5s(),
		Retention1m: config.GetRetention1m(),
	}
}
