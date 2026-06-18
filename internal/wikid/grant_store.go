package wikid

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type GrantStore struct {
	path string
}

func NewGrantStore(path string) *GrantStore {
	return &GrantStore{path: path}
}

func (s *GrantStore) Load() (GrantDocument, error) {
	db, err := openWikidDB(s.path)
	if err != nil {
		return GrantDocument{}, err
	}
	defer db.Close()
	return loadGrantDocument(context.Background(), db)
}

func (s *GrantStore) Save(doc GrantDocument) error {
	if err := doc.Validate(); err != nil {
		return err
	}
	return withWikidImmediateTx(s.path, func(ctx context.Context, conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, `DELETE FROM workspace_grants`); err != nil {
			return err
		}
		for _, grant := range doc.Grants {
			if err := upsertGrant(ctx, conn, grant); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *GrantStore) Upsert(grant Grant) error {
	return withWikidImmediateTx(s.path, func(ctx context.Context, conn *sql.Conn) error {
		return upsertGrant(ctx, conn, grant)
	})
}

func (s *GrantStore) ReplaceSubjectGrants(subject string, grants []Grant) error {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return validateGrant(Grant{})
	}
	return withWikidImmediateTx(s.path, func(ctx context.Context, conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, `DELETE FROM workspace_grants WHERE subject = ?`, subject); err != nil {
			return err
		}
		for _, grant := range grants {
			grant.Subject = strings.TrimSpace(grant.Subject)
			if grant.Subject == "" {
				grant.Subject = subject
			}
			if grant.Subject != subject {
				return fmt.Errorf("replacement grant subject %q does not match %q", grant.Subject, subject)
			}
			if err := upsertGrant(ctx, conn, grant); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *GrantStore) GrantsForSubject(subject string) ([]Grant, error) {
	db, err := openWikidDB(s.path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`
		SELECT subject, workspace_id, role
		FROM workspace_grants
		WHERE subject = ?
		ORDER BY workspace_id
	`, strings.TrimSpace(subject))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var grants []Grant
	for rows.Next() {
		var grant Grant
		if err := rows.Scan(&grant.Subject, &grant.WorkspaceID, &grant.Role); err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return grants, nil
}

type grantQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadGrantDocument(ctx context.Context, q grantQuerier) (GrantDocument, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT subject, workspace_id, role
		FROM workspace_grants
		ORDER BY subject, workspace_id
	`)
	if err != nil {
		return GrantDocument{}, err
	}
	defer rows.Close()
	doc := NewGrantDocument()
	for rows.Next() {
		var grant Grant
		if err := rows.Scan(&grant.Subject, &grant.WorkspaceID, &grant.Role); err != nil {
			return GrantDocument{}, err
		}
		doc.Grants = append(doc.Grants, grant)
	}
	if err := rows.Err(); err != nil {
		return GrantDocument{}, err
	}
	if err := doc.Validate(); err != nil {
		return GrantDocument{}, err
	}
	return doc, nil
}

func upsertGrant(ctx context.Context, conn *sql.Conn, grant Grant) error {
	grant = normalizeGrant(grant)
	if err := validateGrant(grant); err != nil {
		return err
	}
	_, err := conn.ExecContext(ctx, `
		INSERT INTO workspace_grants (subject, workspace_id, role)
		VALUES (?, ?, ?)
		ON CONFLICT(subject, workspace_id) DO UPDATE SET
			role = excluded.role
	`, grant.Subject, grant.WorkspaceID, grant.Role)
	return err
}

func normalizeGrant(grant Grant) Grant {
	grant.Subject = strings.TrimSpace(grant.Subject)
	grant.WorkspaceID = strings.TrimSpace(grant.WorkspaceID)
	return grant
}
