package links

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/perber/wiki/internal/core/shared/sqliteutil"
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
	ToPath     string
	ToKind     string
	Targets    []TargetLink
}

const (
	defaultStoredTargetKind      = "page"
	sectionStoredTargetKind      = "section"
	unknownStoredTargetKind      = "unknown"
	nonCanonicalPageStoredTarget = "non_canonical_page"
)

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
		if !sqliteutil.IsSQLiteRecoverableError(err) {
			return nil, err
		}
		slog.Default().Warn("links database corrupt, removing and retrying", "error", err)
		sqliteutil.RemoveSQLiteFiles(linksDatabasePath(s.storageDir, s.databaseFile))
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
	db, err := sql.Open("sqlite", linksDatabasePath(s.storageDir, s.databaseFile))
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
		if err := rows.Close(); err != nil {
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
	kindExpr := "'" + defaultStoredTargetKind + "'"
	if linksTableHasColumn(columns, "to_kind") {
		kindExpr = "COALESCE(NULLIF(to_kind, ''), '" + defaultStoredTargetKind + "')"
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

func storedTargetKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case sectionStoredTargetKind:
		return sectionStoredTargetKind
	case unknownStoredTargetKind:
		return unknownStoredTargetKind
	case nonCanonicalPageStoredTarget:
		return nonCanonicalPageStoredTarget
	default:
		return defaultStoredTargetKind
	}
}

// DeleteOutgoingLinks removes all links originating from the given page.
// This is correct when the page itself is deleted (no source markdown left).
func (s *LinksStore) DeleteOutgoingLinks(fromPageID tree.PageID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM links WHERE from_page_id = ?`, fromPageID)
	return err
}

// MarkIncomingLinksBroken marks links pointing to the given page as broken,
// but keeps them (because the source markdown still contains them).
func (s *LinksStore) MarkIncomingLinksBroken(toPageID tree.PageID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		UPDATE links
		SET to_page_id = NULL,
		    broken    = 1
		WHERE to_page_id = ?
		  AND broken = 0
	`, toPageID)

	return err
}

// MarkLinksBrokenForPath marks links that point to an exact path as broken.
// Useful for delete/rename in strict mode.
func (s *LinksStore) MarkLinksBrokenForPath(toPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		UPDATE links
		SET to_page_id = NULL,
		    broken    = 1
		WHERE to_path = ?
		  AND broken  = 0
	`, toPath)

	return err
}

// MarkLinksBrokenForPathAndKind marks links that point to an exact path and target kind as broken.
func (s *LinksStore) MarkLinksBrokenForPathAndKind(toPath string, toKind string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		UPDATE links
		SET to_page_id = NULL,
		    broken    = 1
		WHERE to_path = ?
		  AND to_kind = ?
		  AND broken  = 0
	`, toPath, storedTargetKind(toKind))

	return err
}

// MarkLinksBrokenForPrefix marks links whose to_path is under the given prefix as broken.
// Boundary-safe: matches either the prefix itself, or prefix + "/...".
func (s *LinksStore) MarkLinksBrokenForPrefix(oldPrefix string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		UPDATE links
		SET to_page_id = NULL,
		    broken    = 1
		WHERE broken = 0
		  AND (
		    to_path = ?
		    OR to_path LIKE ? || '/%'
		  )
	`, oldPrefix, oldPrefix)

	return err
}

// MarkLinksBrokenForPrefixAndKind marks links under a moved/deleted subtree.
// Exact links to the subtree root must match the root kind; descendants remain path-bound.
func (s *LinksStore) MarkLinksBrokenForPrefixAndKind(oldPrefix string, rootKind string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	storedKind := storedTargetKind(rootKind)
	if storedKind != sectionStoredTargetKind {
		_, err := s.db.Exec(`
			UPDATE links
			SET to_page_id = NULL,
			    broken    = 1
			WHERE broken = 0
			  AND to_path = ?
			  AND to_kind = ?
		`, oldPrefix, storedKind)
		return err
	}

	_, err := s.db.Exec(`
		UPDATE links
		SET to_page_id = NULL,
		    broken    = 1
		WHERE broken = 0
		  AND (
		    (to_path = ? AND to_kind = ?)
		    OR to_path LIKE ? || '/%'
		  )
	`, oldPrefix, storedKind, oldPrefix)

	return err
}

func (s *LinksStore) AddLinks(fromPageID tree.PageID, fromTitle string, toLinks []TargetLink) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	// Clean up existing links to avoid duplicates for the same from_page_id
	_, err = tx.Exec(`DELETE FROM links WHERE from_page_id = ?`, fromPageID)
	if err != nil {
		rbErr := tx.Rollback()
		base := fmt.Errorf("failed to clear existing links for page %s", fromPageID)

		if rbErr != nil {
			return errors.Join(base, err, rbErr)
		}
		return errors.Join(base, err)
	}

	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO links(from_page_id, to_page_id, to_path, to_kind, from_title, broken) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		rbErr := tx.Rollback()
		base := fmt.Errorf("failed to prepare insert statement for links from page %s", fromPageID)

		if rbErr != nil {
			return errors.Join(base, err, rbErr)
		}
		return errors.Join(base, err)
	}
	defer func() {
		if err := stmt.Close(); err != nil {
			slog.Default().Error("could not close statement", "error", err)
		}
	}()

	for _, link := range toLinks {
		brokenInt := 0
		if link.Broken {
			brokenInt = 1
		}

		_, err := stmt.Exec(fromPageID, link.TargetPageID, link.TargetPagePath, storedTargetKind(link.TargetKind), fromTitle, brokenInt)
		if err != nil {
			rbErr := tx.Rollback()
			base := fmt.Errorf("failed to insert link from %s to %s", fromPageID, link.TargetPageID)

			if rbErr != nil {
				return errors.Join(base, err, rbErr)
			}
			return errors.Join(base, err)
		}
	}

	return tx.Commit()
}

func (s *LinksStore) ReplaceLinksAndHeal(updates []PageLinkUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	if err := s.replaceLinksAndHealTx(tx, updates); err != nil {
		rbErr := tx.Rollback()
		if rbErr != nil {
			return errors.Join(err, rbErr)
		}
		return err
	}

	return tx.Commit()
}

func (s *LinksStore) replaceLinksAndHealTx(tx *sql.Tx, updates []PageLinkUpdate) error {
	deleteStmt, err := tx.Prepare(`DELETE FROM links WHERE from_page_id = ?`)
	if err != nil {
		return fmt.Errorf("failed to prepare delete statement for batched link update: %w", err)
	}
	defer func() {
		if err := deleteStmt.Close(); err != nil {
			slog.Default().Error("could not close statement", "error", err)
		}
	}()

	insertStmt, err := tx.Prepare(`INSERT OR REPLACE INTO links(from_page_id, to_page_id, to_path, to_kind, from_title, broken) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement for batched link update: %w", err)
	}
	defer func() {
		if err := insertStmt.Close(); err != nil {
			slog.Default().Error("could not close statement", "error", err)
		}
	}()

	healPageStmt, err := tx.Prepare(`
		UPDATE links
		SET to_page_id = ?, broken = 0
		WHERE to_path = ? AND to_kind = ? AND broken = 1
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare heal statement for batched link update: %w", err)
	}
	defer func() {
		if err := healPageStmt.Close(); err != nil {
			slog.Default().Error("could not close statement", "error", err)
		}
	}()

	healSectionStmt, err := tx.Prepare(`
		UPDATE OR REPLACE links
		SET to_page_id = ?, to_kind = ?, broken = 0
		WHERE to_path = ?
		  AND ((to_kind = ? AND broken = 1) OR to_kind = ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare section heal statement for batched link update: %w", err)
	}
	defer func() {
		if err := healSectionStmt.Close(); err != nil {
			slog.Default().Error("could not close statement", "error", err)
		}
	}()

	for _, update := range updates {
		if _, err := deleteStmt.Exec(update.FromPageID); err != nil {
			return fmt.Errorf("failed to clear existing links for page %s: %w", update.FromPageID, err)
		}

		for _, link := range update.Targets {
			brokenInt := 0
			if link.Broken {
				brokenInt = 1
			}
			if _, err := insertStmt.Exec(update.FromPageID, link.TargetPageID, link.TargetPagePath, storedTargetKind(link.TargetKind), update.FromTitle, brokenInt); err != nil {
				return fmt.Errorf("failed to insert link from %s to %s: %w", update.FromPageID, link.TargetPageID, err)
			}
		}
	}

	for _, update := range updates {
		toKind := storedTargetKind(update.ToKind)
		if toKind == sectionStoredTargetKind {
			if _, err := healSectionStmt.Exec(update.FromPageID, toKind, update.ToPath, toKind, unknownStoredTargetKind); err != nil {
				return fmt.Errorf("failed to heal links for path %s: %w", update.ToPath, err)
			}
			continue
		}
		if _, err := healPageStmt.Exec(update.FromPageID, update.ToPath, toKind); err != nil {
			return fmt.Errorf("failed to heal links for path %s: %w", update.ToPath, err)
		}
	}

	return nil
}

func (s *LinksStore) GetBacklinksForPage(pageID tree.PageID) ([]Backlink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT from_page_id, to_page_id, from_title, to_kind FROM links WHERE to_page_id = ? and broken = 0`, pageID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Error("could not close rows", "error", err)
		}
	}()

	var backlinks []Backlink
	for rows.Next() {
		var b Backlink
		var toPageID sql.NullString
		if err := rows.Scan(&b.FromPageID, &toPageID, &b.FromTitle, &b.ToKind); err != nil {
			return nil, err
		}
		if toPageID.Valid {
			b.ToPageID = tree.NewPageIDUnchecked(toPageID.String)
		} else {
			b.ToPageID = ""
		}
		backlinks = append(backlinks, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return backlinks, nil
}

func (s *LinksStore) GetOutgoingLinksForPage(pageID tree.PageID) ([]Outgoing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
        SELECT from_page_id, to_page_id, to_path, to_kind, from_title, broken
        FROM links
        WHERE from_page_id = ?
    `, pageID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Error("could not close rows", "error", err)
		}
	}()

	var outgoings []Outgoing
	for rows.Next() {
		var o Outgoing
		var toPageID sql.NullString
		var brokenInt int

		if err := rows.Scan(&o.FromPageID, &toPageID, &o.ToPath, &o.ToKind, &o.FromTitle, &brokenInt); err != nil {
			return nil, err
		}

		if toPageID.Valid {
			o.ToPageID = tree.NewPageIDUnchecked(toPageID.String)
		} else {
			o.ToPageID = ""
		}

		o.Broken = brokenInt != 0
		outgoings = append(outgoings, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return outgoings, nil
}

func (s *LinksStore) GetOutgoingLinksForPages(pageIDs []tree.PageID) (map[tree.PageID][]Outgoing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(pageIDs) == 0 {
		return map[tree.PageID][]Outgoing{}, nil
	}

	outgoingByPageID := make(map[tree.PageID][]Outgoing, len(pageIDs))
	for start := 0; start < len(pageIDs); start += maxOutgoingLinksQueryArgs {
		end := start + maxOutgoingLinksQueryArgs
		if end > len(pageIDs) {
			end = len(pageIDs)
		}

		if err := s.appendOutgoingLinksForPageBatch(outgoingByPageID, pageIDs[start:end]); err != nil {
			return nil, err
		}
	}

	return outgoingByPageID, nil
}

func (s *LinksStore) appendOutgoingLinksForPageBatch(outgoingByPageID map[tree.PageID][]Outgoing, pageIDs []tree.PageID) error {
	placeholders := strings.TrimRight(strings.Repeat("?,", len(pageIDs)), ",")
	args := make([]any, 0, len(pageIDs))
	for _, pageID := range pageIDs {
		args = append(args, pageID)
	}

	rows, err := s.db.Query(`
        SELECT from_page_id, to_page_id, to_path, to_kind, from_title, broken
        FROM links
        WHERE from_page_id IN (`+placeholders+`)
        ORDER BY from_page_id
    `, args...)
	if err != nil {
		return err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Error("could not close rows", "error", err)
		}
	}()

	for rows.Next() {
		var outgoing Outgoing
		var toPageID sql.NullString
		var brokenInt int

		if err := rows.Scan(&outgoing.FromPageID, &toPageID, &outgoing.ToPath, &outgoing.ToKind, &outgoing.FromTitle, &brokenInt); err != nil {
			return err
		}

		if toPageID.Valid {
			outgoing.ToPageID = tree.NewPageIDUnchecked(toPageID.String)
		}
		outgoing.Broken = brokenInt != 0
		outgoingByPageID[outgoing.FromPageID] = append(outgoingByPageID[outgoing.FromPageID], outgoing)
	}

	return rows.Err()
}

func (s *LinksStore) GetRefactorMatchesForPrefix(oldPrefix tree.RoutePath) ([]RefactorLinkMatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldPrefixPath := oldPrefix.WikiPath()
	rows, err := s.db.Query(`
		SELECT from_page_id, from_title, to_path, to_kind, broken
		FROM links
		WHERE to_path = ? OR to_path LIKE ?
	`, oldPrefixPath, oldPrefixPath+"/%")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Error("could not close rows", "error", err)
		}
	}()

	var matches []RefactorLinkMatch
	for rows.Next() {
		var match RefactorLinkMatch
		var toPath string
		var brokenInt int
		if err := rows.Scan(&match.FromPageID, &match.FromTitle, &toPath, &match.ToKind, &brokenInt); err != nil {
			return nil, err
		}
		match.ToPath = tree.NewRoutePathUnchecked(toPath).Clean()
		match.Broken = brokenInt == 1
		matches = append(matches, match)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return matches, nil
}

func (s *LinksStore) GetRefactorMatchesForPrefixAndKind(oldPrefix tree.RoutePath, rootKind tree.NodeKind) ([]RefactorLinkMatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldPrefixPath := oldPrefix.WikiPath()
	storedKind := storedTargetKind(string(rootKind))
	query := `
		SELECT from_page_id, from_title, to_path, to_kind, broken
		FROM links
		WHERE to_path = ? AND (to_kind = ? OR (to_kind = ? AND broken = 0))
	`
	args := []any{oldPrefixPath, storedKind, unknownStoredTargetKind}
	if storedKind == sectionStoredTargetKind {
		query = `
			SELECT from_page_id, from_title, to_path, to_kind, broken
			FROM links
			WHERE (to_path = ? AND to_kind = ?) OR to_path LIKE ?
		`
		args = []any{oldPrefixPath, storedKind, oldPrefixPath + "/%"}
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Error("could not close rows", "error", err)
		}
	}()

	var matches []RefactorLinkMatch
	for rows.Next() {
		var match RefactorLinkMatch
		var toPath string
		var brokenInt int
		if err := rows.Scan(&match.FromPageID, &match.FromTitle, &toPath, &match.ToKind, &brokenInt); err != nil {
			return nil, err
		}
		match.ToPath = tree.NewRoutePathUnchecked(toPath).Clean()
		match.Broken = brokenInt == 1
		matches = append(matches, match)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return matches, nil
}

func (s *LinksStore) GetRefactorSourcePageIDsForPrefix(oldPrefix tree.RoutePath) ([]tree.PageID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldPrefixPath := oldPrefix.WikiPath()
	rows, err := s.db.Query(`
		SELECT DISTINCT from_page_id
		FROM links
		WHERE to_path = ? OR to_path LIKE ?
	`, oldPrefixPath, oldPrefixPath+"/%")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Error("could not close rows", "error", err)
		}
	}()

	var pageIDs []tree.PageID
	for rows.Next() {
		var pageID tree.PageID
		if err := rows.Scan(&pageID); err != nil {
			return nil, err
		}
		pageIDs = append(pageIDs, pageID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return pageIDs, nil
}

func (s *LinksStore) GetRefactorSourcePageIDsForPrefixAndKind(oldPrefix tree.RoutePath, rootKind tree.NodeKind) ([]tree.PageID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldPrefixPath := oldPrefix.WikiPath()
	storedKind := storedTargetKind(string(rootKind))
	query := `
		SELECT DISTINCT from_page_id
		FROM links
		WHERE to_path = ? AND (to_kind = ? OR (to_kind = ? AND broken = 0))
	`
	args := []any{oldPrefixPath, storedKind, unknownStoredTargetKind}
	if storedKind == sectionStoredTargetKind {
		query = `
			SELECT DISTINCT from_page_id
			FROM links
			WHERE (to_path = ? AND to_kind = ?) OR to_path LIKE ?
		`
		args = []any{oldPrefixPath, storedKind, oldPrefixPath + "/%"}
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Error("could not close rows", "error", err)
		}
	}()

	var pageIDs []tree.PageID
	for rows.Next() {
		var pageID tree.PageID
		if err := rows.Scan(&pageID); err != nil {
			return nil, err
		}
		pageIDs = append(pageIDs, pageID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return pageIDs, nil
}

func (s *LinksStore) GetBrokenIncomingForPath(toPath string) ([]Backlink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT from_page_id, to_page_id, from_title, to_kind
		FROM links
		WHERE to_path = ? AND broken = 1
		ORDER BY from_title ASC
	`, toPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Error("could not close rows", "error", err)
		}
	}()

	var backlinks []Backlink
	for rows.Next() {
		var b Backlink
		var toPageID sql.NullString
		if err := rows.Scan(&b.FromPageID, &toPageID, &b.FromTitle, &b.ToKind); err != nil {
			return nil, err
		}
		if toPageID.Valid {
			b.ToPageID = tree.NewPageIDUnchecked(toPageID.String)
		} else {
			b.ToPageID = ""
		}
		b.Broken = true
		backlinks = append(backlinks, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return backlinks, nil
}

func (s *LinksStore) GetBrokenIncomingForPathAndKind(toPath string, toKind string) ([]Backlink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT from_page_id, to_page_id, from_title, to_kind
		FROM links
		WHERE to_path = ? AND to_kind IN (?, ?) AND broken = 1
		ORDER BY from_title ASC
	`, toPath, storedTargetKind(toKind), unknownStoredTargetKind)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Error("could not close rows", "error", err)
		}
	}()

	var backlinks []Backlink
	for rows.Next() {
		var b Backlink
		var toPageID sql.NullString
		if err := rows.Scan(&b.FromPageID, &toPageID, &b.FromTitle, &b.ToKind); err != nil {
			return nil, err
		}
		if toPageID.Valid {
			b.ToPageID = tree.NewPageIDUnchecked(toPageID.String)
		} else {
			b.ToPageID = ""
		}
		b.Broken = true
		backlinks = append(backlinks, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return backlinks, nil
}

func (s *LinksStore) HealLinksForPath(toPath string, pageID tree.PageID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		UPDATE links
		SET to_page_id = ?, broken = 0
		WHERE to_path = ? AND broken = 1
	`, pageID, toPath)

	return err
}

func (s *LinksStore) HealLinksForPathAndKind(toPath string, toKind string, pageID tree.PageID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	storedKind := storedTargetKind(toKind)
	if storedKind == sectionStoredTargetKind {
		_, err := s.db.Exec(`
			UPDATE OR REPLACE links
			SET to_page_id = ?, to_kind = ?, broken = 0
			WHERE to_path = ?
			  AND ((to_kind = ? AND broken = 1) OR to_kind = ?)
		`, pageID, storedKind, toPath, storedKind, unknownStoredTargetKind)
		return err
	}

	_, err := s.db.Exec(`
		UPDATE links
		SET to_page_id = ?, broken = 0
		WHERE to_path = ? AND to_kind = ? AND broken = 1
	`, pageID, toPath, storedKind)

	return err
}

func (s *LinksStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM links`)
	return err
}

func (s *LinksStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		err := s.db.Close()
		if err != nil {
			return err
		}
		s.db = nil
	}
	return nil
}

func (s *LinksStore) GetDB() *sql.DB {
	if s.db == nil {
		return nil
	}
	return s.db
}
