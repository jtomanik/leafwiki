package wikid

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/workspaceid"
)

const RegistrySchemaVersion = 1

type RegistryDocument struct {
	SchemaVersion int               `json:"schemaVersion"`
	Workspaces    []WorkspaceRecord `json:"workspaces"`
}

type WorkspaceRecord struct {
	ID                     string    `json:"id"`
	DisplayName            string    `json:"displayName"`
	DataDir                string    `json:"dataDir"`
	RootDir                string    `json:"rootDir"`
	MarkdownLinkRootPrefix string    `json:"markdownLinkRootPrefix,omitempty"`
	CreatedAt              time.Time `json:"createdAt"`
	UpdatedAt              time.Time `json:"updatedAt"`
}

func NewRegistryDocument() RegistryDocument {
	return RegistryDocument{SchemaVersion: RegistrySchemaVersion}
}

func (d RegistryDocument) Workspace(id string) (WorkspaceRecord, bool) {
	for _, workspace := range d.Workspaces {
		if workspace.ID == id {
			return workspace, true
		}
	}
	return WorkspaceRecord{}, false
}

func (d RegistryDocument) Validate() error {
	if d.SchemaVersion != RegistrySchemaVersion {
		return fmt.Errorf("registry schema version = %d, want %d", d.SchemaVersion, RegistrySchemaVersion)
	}
	ids := map[string]struct{}{}
	for _, workspace := range d.Workspaces {
		if err := validateWorkspaceRecord(workspace); err != nil {
			return err
		}
		if _, exists := ids[workspace.ID]; exists {
			return fmt.Errorf("duplicate workspace ID %q", workspace.ID)
		}
		ids[workspace.ID] = struct{}{}
	}
	return nil
}

func validateWorkspaceRecord(workspace WorkspaceRecord) error {
	if err := workspaceid.ValidateWorkspaceID(workspace.ID); err != nil {
		return err
	}
	if strings.TrimSpace(workspace.DataDir) == "" {
		return fmt.Errorf("workspace %q data dir is required", workspace.ID)
	}
	if strings.TrimSpace(workspace.RootDir) == "" {
		return fmt.Errorf("workspace %q root dir is required", workspace.ID)
	}
	prefix, err := markdownlinks.NormalizeMarkdownLinkRootPrefix(workspace.MarkdownLinkRootPrefix)
	if err != nil {
		return fmt.Errorf("workspace %q markdown link root prefix: %w", workspace.ID, err)
	}
	if workspace.MarkdownLinkRootPrefix != prefix {
		return fmt.Errorf("workspace %q markdown link root prefix must be normalized", workspace.ID)
	}
	return nil
}

func normalizeWorkspaceRecord(workspace WorkspaceRecord) WorkspaceRecord {
	workspace.ID = strings.TrimSpace(workspace.ID)
	workspace.DisplayName = strings.TrimSpace(workspace.DisplayName)
	workspace.DataDir = filepath.Clean(strings.TrimSpace(workspace.DataDir))
	workspace.RootDir = filepath.Clean(strings.TrimSpace(workspace.RootDir))
	workspace.MarkdownLinkRootPrefix = strings.TrimSpace(workspace.MarkdownLinkRootPrefix)
	return workspace
}

func canonicalRegistryPath(path string) (string, error) {
	absPath, err := filepath.Abs(filepath.Clean(strings.TrimSpace(path)))
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absPath)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	current := absPath
	var suffix []string
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(absPath), nil
		}
		suffix = append([]string{filepath.Base(current)}, suffix...)
		current = parent
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			parts := append([]string{resolved}, suffix...)
			return filepath.Clean(filepath.Join(parts...)), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
	}
}
