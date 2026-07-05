package links

import (
	"database/sql"
	"log/slog"
	"strings"

	"github.com/perber/wiki/internal/core/tree"
)

func (s *LinksStore) GetBacklinksForPage(pageID tree.PageID) ([]Backlink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT from_page_id, to_page_id, from_title, to_kind FROM links WHERE to_page_id = ? and broken = 0`, pageID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := linksCloseRows(rows); err != nil {
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
			b.ToPageID = tree.PageIDFromString(toPageID.String)
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
		if err := linksCloseRows(rows); err != nil {
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
			o.ToPageID = tree.PageIDFromString(toPageID.String)
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
		if err := linksCloseRows(rows); err != nil {
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
			outgoing.ToPageID = tree.PageIDFromString(toPageID.String)
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
		if err := linksCloseRows(rows); err != nil {
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
		match.ToPath = tree.RoutePathFromString(toPath).Clean()
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
	storedKind := TargetKindFromNodeKind(rootKind).Stored()
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
		if err := linksCloseRows(rows); err != nil {
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
		match.ToPath = tree.RoutePathFromString(toPath).Clean()
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
		if err := linksCloseRows(rows); err != nil {
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
	storedKind := TargetKindFromNodeKind(rootKind).Stored()
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
		if err := linksCloseRows(rows); err != nil {
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

func (s *LinksStore) GetBrokenIncomingForPath(toPath tree.RoutePath) ([]Backlink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT from_page_id, to_page_id, from_title, to_kind
		FROM links
		WHERE to_path = ? AND broken = 1
		ORDER BY from_title ASC
	`, toPath.WikiPath())
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := linksCloseRows(rows); err != nil {
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
			b.ToPageID = tree.PageIDFromString(toPageID.String)
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

func (s *LinksStore) GetBrokenIncomingForPathAndKind(toPath tree.RoutePath, toKind tree.NodeKind) ([]Backlink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT from_page_id, to_page_id, from_title, to_kind
		FROM links
		WHERE to_path = ? AND to_kind IN (?, ?) AND broken = 1
		ORDER BY from_title ASC
	`, toPath.WikiPath(), TargetKindFromNodeKind(toKind).Stored(), unknownStoredTargetKind)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := linksCloseRows(rows); err != nil {
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
			b.ToPageID = tree.PageIDFromString(toPageID.String)
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

func (s *LinksStore) HealLinksForPathAndKind(toPath string, toKind tree.NodeKind, pageID tree.PageID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	storedKind := TargetKindFromNodeKind(toKind).Stored()
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
