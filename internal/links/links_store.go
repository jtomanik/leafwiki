package links

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/perber/wiki/internal/core/tree"
	_ "modernc.org/sqlite" // Import SQLite driver
)

type LinksStore struct {
	mu           sync.Mutex
	storageDir   string
	databaseFile string
	db           *sql.DB
}

const maxOutgoingLinksQueryArgs = 900

type PageLinkUpdate struct {
	FromPageID tree.PageID
	FromTitle  string
	ToPath     tree.RoutePath
	ToKind     tree.NodeKind
	Targets    []TargetLink
}

func linksDatabasePath(storageDir string, filename string) string {
	normalizedStorageDir := filepath.FromSlash(strings.ReplaceAll(storageDir, `\`, `/`))
	return filepath.Join(normalizedStorageDir, filename)
}

func NewLinksStore(storageDir string) (*LinksStore, error) {
	s := &LinksStore{
		storageDir:   storageDir,
		databaseFile: "links.db",
	}

	if err := s.Connect(); err != nil {
		return nil, err
	}

	if err := s.ensureSchema(); err != nil {
		_ = s.db.Close()
		s.db = nil
		if !linksIsSQLiteRecoverableError(err) {
			return nil, err
		}
		slog.Default().Warn("links database corrupt, removing and retrying", "error", err)
		linksRemoveSQLiteFiles(linksDatabasePath(s.storageDir, s.databaseFile))
		if err2 := s.Connect(); err2 != nil {
			return nil, err2
		}
		if err2 := s.ensureSchema(); err2 != nil {
			_ = s.db.Close()
			s.db = nil
			return nil, err2
		}
	}

	return s, nil
}

func (s *LinksStore) Connect() error {
	// Database is already open and connected
	if s.db != nil {
		return nil
	}
	// Connect to the database
	db, err := linksSQLOpen("sqlite", linksDatabasePath(s.storageDir, s.databaseFile))
	if err != nil {
		return err
	}
	s.db = db
	return nil
}

func (s *LinksStore) ensureSchema() error {
	err := s.Connect()
	if err != nil {
		return err
	}
	if err := s.ensureLinksTable(); err != nil {
		return err
	}
	_, err = s.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_links_to_page_id ON links(to_page_id);
		CREATE INDEX IF NOT EXISTS idx_links_to_path ON links(to_path);
		CREATE INDEX IF NOT EXISTS idx_links_to_path_kind ON links(to_path, to_kind);
		CREATE INDEX IF NOT EXISTS idx_links_to_path_kind_from_page_id ON links(to_path, to_kind, from_page_id);
		CREATE INDEX IF NOT EXISTS idx_links_broken ON links(broken);
	`)
	return err
}

func (s *LinksStore) ensureLinksTable() error {
	exists, err := s.linksTableExists()
	if err != nil {
		return err
	}
	if !exists {
		_, err = s.db.Exec(linksTableSchemaSQL("links"))
		return err
	}

	columns, err := s.linksTableColumns()
	if err != nil {
		return err
	}
	if linksTableKindAware(columns) {
		return nil
	}
	return s.migrateLinksTableToKindAware(columns)
}

type linksTableColumn struct {
	Name string
	PK   int
}

func (s *LinksStore) linksTableExists() (bool, error) {
	var name string
	err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'links'`).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *LinksStore) linksTableColumns() ([]linksTableColumn, error) {
	rows, err := s.db.Query(`PRAGMA table_info(links)`)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := linksCloseRows(rows); err != nil {
			slog.Default().Error("could not close rows", "error", err)
		}
	}()

	var columns []linksTableColumn
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns = append(columns, linksTableColumn{Name: name, PK: pk})
	}
	return columns, rows.Err()
}

func linksTableKindAware(columns []linksTableColumn) bool {
	pk := map[string]int{}
	for _, column := range columns {
		pk[column.Name] = column.PK
	}
	return pk["from_page_id"] > 0 && pk["to_path"] > 0 && pk["to_kind"] > 0
}

func linksTableHasColumn(columns []linksTableColumn, name string) bool {
	for _, column := range columns {
		if column.Name == name {
			return true
		}
	}
	return false
}

func (s *LinksStore) migrateLinksTableToKindAware(columns []linksTableColumn) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	kindExpr := fmt.Sprintf("'%s'", defaultStoredTargetKind)
	if linksTableHasColumn(columns, "to_kind") {
		kindExpr = fmt.Sprintf("COALESCE(NULLIF(to_kind, ''), '%s')", defaultStoredTargetKind)
	}
	_, err = tx.Exec(linksTableSchemaSQL("links_migration") + fmt.Sprintf(`
		INSERT OR REPLACE INTO links_migration(from_page_id, to_page_id, to_path, to_kind, from_title, broken)
		SELECT from_page_id, to_page_id, to_path, %s, from_title, broken FROM links;
		DROP TABLE links;
		ALTER TABLE links_migration RENAME TO links;
	`, kindExpr))
	if err != nil {
		rbErr := tx.Rollback()
		if rbErr != nil {
			return errors.Join(err, rbErr)
		}
		return err
	}
	return tx.Commit()
}

func linksTableSchemaSQL(tableName string) string {
	return fmt.Sprintf(`
        CREATE TABLE IF NOT EXISTS %s (
            from_page_id TEXT NOT NULL,
            to_page_id   TEXT,
			to_path	  	 TEXT NOT NULL,
			to_kind      TEXT NOT NULL DEFAULT '%s',
            from_title   TEXT,
			broken 	     INTEGER NOT NULL DEFAULT 0,
            PRIMARY KEY (from_page_id, to_path, to_kind)
        );
	`, tableName, defaultStoredTargetKind)
}

// DeleteOutgoingLinks removes all links originating from the given page.
