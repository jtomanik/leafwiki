package wikid

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/perber/wiki/internal/core/shared/sqliteutil"

	_ "modernc.org/sqlite"
)

const wikidSQLiteBusyTimeoutMS = 5000

func openWikidDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create wikid db directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create wikid db: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close wikid db handle: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("secure wikid db: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := initializeWikidDB(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func initializeWikidDB(db *sql.DB) error {
	ctx := context.Background()
	statements := []string{
		fmt.Sprintf("PRAGMA busy_timeout = %d", wikidSQLiteBusyTimeoutMS),
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		`CREATE TABLE IF NOT EXISTS workspaces (
			id TEXT PRIMARY KEY,
			display_name TEXT NOT NULL,
			data_dir TEXT NOT NULL UNIQUE,
			root_dir TEXT NOT NULL UNIQUE,
			markdown_link_root_prefix TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(data_dir, root_dir)
		)`,
		`CREATE TABLE IF NOT EXISTS workspace_grants (
			subject TEXT NOT NULL,
			workspace_id TEXT NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('viewer', 'editor', 'admin')),
			PRIMARY KEY(subject, workspace_id),
			FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
		)`,
	}
	for _, stmt := range statements {
		if _, err := execWikidSQLiteWithLockRetry(ctx, db, stmt); err != nil {
			return fmt.Errorf("initialize wikid db: %w", err)
		}
	}
	return nil
}

func withWikidImmediateTx(path string, fn func(context.Context, *sql.Conn) error) (err error) {
	db, err := openWikidDB(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := conn.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	if _, err := execWikidSQLiteWithLockRetry(ctx, conn, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()
	if err := fn(ctx, conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

type wikidSQLiteExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func execWikidSQLiteWithLockRetry(ctx context.Context, execer wikidSQLiteExecer, query string, args ...any) (sql.Result, error) {
	deadline := time.Now().Add(time.Duration(wikidSQLiteBusyTimeoutMS) * time.Millisecond)
	delay := 10 * time.Millisecond
	for {
		result, err := execer.ExecContext(ctx, query, args...)
		if err == nil || !sqliteutil.IsSQLiteTransientLockError(err) || time.Now().After(deadline) {
			return result, err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		if delay < 100*time.Millisecond {
			delay *= 2
		}
	}
}

func wikidTimeString(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseWikidTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}
