package wikid

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/perber/wiki/internal/workspaceid"
)

type RegistryStore struct {
	path string
}

func NewRegistryStore(path string) *RegistryStore {
	return &RegistryStore{path: path}
}

func (s *RegistryStore) Load() (doc RegistryDocument, err error) {
	db, err := openWikidDB(s.path)
	if err != nil {
		return RegistryDocument{}, err
	}
	defer func() {
		err = errors.Join(err, wikidCloseDB(db))
	}()
	return loadRegistryDocument(context.Background(), db)
}

func (s *RegistryStore) Save(doc RegistryDocument) error {
	if err := doc.Validate(); err != nil {
		return err
	}
	return withWikidImmediateTx(s.path, func(ctx context.Context, conn *sql.Conn) error {
		return saveRegistryDocument(ctx, conn, doc)
	})
}

func (s *RegistryStore) Update(fn func(RegistryDocument) (RegistryDocument, error)) (doc RegistryDocument, err error) {
	err = withWikidImmediateTx(s.path, func(ctx context.Context, conn *sql.Conn) error {
		current, err := loadRegistryDocument(ctx, conn)
		if err != nil {
			return err
		}
		next, err := fn(current)
		if err != nil {
			return err
		}
		if err := saveRegistryDocument(ctx, conn, next); err != nil {
			return err
		}
		doc = next
		return nil
	})
	if err != nil {
		return RegistryDocument{}, err
	}
	return doc, nil
}

func (s *RegistryStore) RegisterWorkspaceWithResultAndGrants(
	workspace WorkspaceRecord,
	now func() time.Time,
	grants func(RegisterWorkspaceResult) ([]Grant, error),
) (result RegisterWorkspaceResult, err error) {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if err := workspace.ID.Validate(); err != nil {
		return RegisterWorkspaceResult{}, err
	}
	workspace = normalizeWorkspaceRecord(workspace)
	if err := validateWorkspaceRecord(workspace); err != nil {
		return RegisterWorkspaceResult{}, err
	}
	err = withWikidImmediateTx(s.path, func(ctx context.Context, conn *sql.Conn) error {
		doc, err := loadRegistryDocument(ctx, conn)
		if err != nil {
			return err
		}
		for _, existing := range doc.Workspaces {
			if sameWorkspaceLocation(existing, workspace) {
				existing.DisplayName = workspace.DisplayName
				existing.MarkdownLinkRootPrefix = workspace.MarkdownLinkRootPrefix
				existing.UpdatedAt = now()
				if err := upsertWorkspace(ctx, conn, existing); err != nil {
					return err
				}
				result = RegisterWorkspaceResult{Workspace: existing}
				return upsertWorkspaceGrantsForRegistration(ctx, conn, result, grants)
			}
			if cleanPath(existing.DataDir) == cleanPath(workspace.DataDir) {
				return fmt.Errorf("data directory is already in use by workspace %q: %w", existing.ID.String(), ErrWorkspaceDataDirAlreadyInUse)
			}
			if cleanPath(existing.RootDir) == cleanPath(workspace.RootDir) {
				return fmt.Errorf("root directory is already in use by workspace %q: %w", existing.ID.String(), ErrWorkspaceRootDirAlreadyInUse)
			}
		}
		result = RegisterWorkspaceResult{Workspace: workspace, Created: true}
		if err := upsertWorkspace(ctx, conn, workspace); err != nil {
			return err
		}
		return upsertWorkspaceGrantsForRegistration(ctx, conn, result, grants)
	})
	if err != nil {
		return RegisterWorkspaceResult{}, err
	}
	return result, nil
}

func upsertWorkspaceGrantsForRegistration(ctx context.Context, conn *sql.Conn, result RegisterWorkspaceResult, grants func(RegisterWorkspaceResult) ([]Grant, error)) error {
	if grants == nil {
		return nil
	}
	toUpsert, err := grants(result)
	if err != nil {
		return err
	}
	for _, grant := range toUpsert {
		if grant.WorkspaceID == "" {
			grant.WorkspaceID = result.Workspace.ID
		} else if err := grant.WorkspaceID.Validate(); err != nil {
			return fmt.Errorf("grant workspace ID: %w", err)
		}
		if err := upsertGrant(ctx, conn, grant); err != nil {
			return err
		}
	}
	return nil
}

type registryQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadRegistryDocument(ctx context.Context, q registryQuerier) (doc RegistryDocument, err error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, display_name, data_dir, root_dir, markdown_link_root_prefix, created_at, updated_at
		FROM workspaces
		ORDER BY id
	`)
	if err != nil {
		return RegistryDocument{}, err
	}
	defer func() {
		err = errors.Join(err, rows.Close())
	}()
	doc, err = loadRegistryRows(rows)
	if err != nil {
		return RegistryDocument{}, err
	}
	if err := rows.Err(); err != nil {
		return RegistryDocument{}, err
	}
	return doc, nil
}

func loadRegistryRows(rows wikidRows) (RegistryDocument, error) {
	doc := NewRegistryDocument()
	for rows.Next() {
		var workspace WorkspaceRecord
		var createdAt, updatedAt string
		if err := rows.Scan(
			&workspace.ID,
			&workspace.DisplayName,
			&workspace.DataDir,
			&workspace.RootDir,
			&workspace.MarkdownLinkRootPrefix,
			&createdAt,
			&updatedAt,
		); err != nil {
			return RegistryDocument{}, err
		}
		var err error
		workspace.CreatedAt, err = parseWikidTime(createdAt)
		if err != nil {
			return RegistryDocument{}, fmt.Errorf("parse workspace %q created_at: %w", workspace.ID.String(), err)
		}
		workspace.UpdatedAt, err = parseWikidTime(updatedAt)
		if err != nil {
			return RegistryDocument{}, fmt.Errorf("parse workspace %q updated_at: %w", workspace.ID.String(), err)
		}
		doc.Workspaces = append(doc.Workspaces, workspace)
	}
	if err := rows.Err(); err != nil {
		return RegistryDocument{}, err
	}
	if err := doc.Validate(); err != nil {
		return RegistryDocument{}, err
	}
	return doc, nil
}

func saveRegistryDocument(ctx context.Context, conn *sql.Conn, doc RegistryDocument) error {
	if err := doc.Validate(); err != nil {
		return err
	}
	keep := map[workspaceid.WorkspaceID]struct{}{}
	for _, workspace := range doc.Workspaces {
		workspace = normalizeWorkspaceRecord(workspace)
		if err := upsertWorkspace(ctx, conn, workspace); err != nil {
			return err
		}
		keep[workspace.ID] = struct{}{}
	}
	if len(keep) == 0 {
		_, err := conn.ExecContext(ctx, `DELETE FROM workspaces`)
		return err
	}
	placeholders := make([]string, 0, len(keep))
	args := make([]any, 0, len(keep))
	for id := range keep {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	_, err := conn.ExecContext(ctx, `DELETE FROM workspaces WHERE id NOT IN (`+strings.Join(placeholders, ",")+`)`, args...)
	return err
}

func upsertWorkspace(ctx context.Context, conn *sql.Conn, workspace WorkspaceRecord) error {
	workspace = normalizeWorkspaceRecord(workspace)
	if err := validateWorkspaceRecord(workspace); err != nil {
		return err
	}
	_, err := conn.ExecContext(ctx, `
		INSERT INTO workspaces (
			id, display_name, data_dir, root_dir, markdown_link_root_prefix, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			display_name = excluded.display_name,
			data_dir = excluded.data_dir,
			root_dir = excluded.root_dir,
			markdown_link_root_prefix = excluded.markdown_link_root_prefix,
			updated_at = excluded.updated_at
	`, workspace.ID, workspace.DisplayName, workspace.DataDir, workspace.RootDir, workspace.MarkdownLinkRootPrefix, wikidTimeString(workspace.CreatedAt), wikidTimeString(workspace.UpdatedAt))
	return err
}

func cleanPath(path string) string {
	return filepath.Clean(strings.TrimSpace(path))
}
