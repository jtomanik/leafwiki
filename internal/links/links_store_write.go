package links

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/perber/wiki/internal/core/tree"
)

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
func (s *LinksStore) MarkLinksBrokenForPath(toPath tree.RoutePath) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		UPDATE links
		SET to_page_id = NULL,
		    broken    = 1
		WHERE to_path = ?
		  AND broken  = 0
	`, toPath.WikiPath())

	return err
}

// MarkLinksBrokenForPathAndKind marks links that point to an exact path and target kind as broken.
func (s *LinksStore) MarkLinksBrokenForPathAndKind(toPath tree.RoutePath, toKind tree.NodeKind) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		UPDATE links
		SET to_page_id = NULL,
		    broken    = 1
		WHERE to_path = ?
		  AND to_kind = ?
		  AND broken  = 0
	`, toPath.WikiPath(), TargetKindFromNodeKind(toKind).Stored())

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
func (s *LinksStore) MarkLinksBrokenForPrefixAndKind(oldPrefix string, rootKind tree.NodeKind) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	storedKind := TargetKindFromNodeKind(rootKind).Stored()
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
		if err := linksCloseStatement(stmt); err != nil {
			slog.Default().Error("could not close statement", "error", err)
		}
	}()

	for _, link := range toLinks {
		brokenInt := 0
		if link.Broken {
			brokenInt = 1
		}

		_, err := stmt.Exec(fromPageID, link.TargetPageID, link.TargetPagePath, link.TargetKind.Stored(), fromTitle, brokenInt)
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
		if err := linksCloseStatement(deleteStmt); err != nil {
			slog.Default().Error("could not close statement", "error", err)
		}
	}()

	insertStmt, err := tx.Prepare(`INSERT OR REPLACE INTO links(from_page_id, to_page_id, to_path, to_kind, from_title, broken) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement for batched link update: %w", err)
	}
	defer func() {
		if err := linksCloseStatement(insertStmt); err != nil {
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
		if err := linksCloseStatement(healPageStmt); err != nil {
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
		if err := linksCloseStatement(healSectionStmt); err != nil {
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
			if _, err := insertStmt.Exec(update.FromPageID, link.TargetPageID, link.TargetPagePath, link.TargetKind.Stored(), update.FromTitle, brokenInt); err != nil {
				return fmt.Errorf("failed to insert link from %s to %s: %w", update.FromPageID, link.TargetPageID, err)
			}
		}
	}

	for _, update := range updates {
		toKind := TargetKindFromNodeKind(update.ToKind).Stored()
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
