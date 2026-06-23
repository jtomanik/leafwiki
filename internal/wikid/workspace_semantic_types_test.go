package wikid

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/perber/wiki/internal/workspaceid"
)

func TestWorkspaceRuntimeModelsCarrySemanticWorkspaceID(t *testing.T) {
	workspaceID := workspaceid.WorkspaceID("home")

	doc := RegistryDocument{
		SchemaVersion: RegistrySchemaVersion,
		Workspaces: []WorkspaceRecord{{
			ID: workspaceID,
		}},
	}
	if got, ok := doc.Workspace(workspaceID); !ok || got.ID != workspaceID {
		t.Fatalf("workspace lookup = %#v, ok=%v", got, ok)
	}

	grant := Grant{Subject: "user:1", WorkspaceID: workspaceID, Role: GrantRoleViewer}
	var _ workspaceid.WorkspaceID = grant.WorkspaceID

	supervisor := NewWorkspaceSupervisor(WorkspaceSupervisorOptions{})
	supervisor.MarkReady(workspaceID, 123, "http://127.0.0.1:41001")
	status := supervisor.Status(workspaceID)
	if status.WorkspaceID != workspaceID || status.State != WorkspaceStateRunning {
		t.Fatalf("supervisor status = %#v, want typed workspace ID to remain running", status)
	}
}

func TestRegistryStoreRejectsWorkspaceIDWhitespaceBeforeNormalization(t *testing.T) {
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	store := NewRegistryStore(layout.DBPath)
	now := func() time.Time { return time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC) }

	_, err := store.RegisterWorkspaceWithResultAndGrants(WorkspaceRecord{
		ID:          workspaceid.WorkspaceID(" docs "),
		DisplayName: "Docs",
		DataDir:     filepath.Join(t.TempDir(), "docs-data"),
		RootDir:     filepath.Join(t.TempDir(), "docs-root"),
		CreatedAt:   now(),
		UpdatedAt:   now(),
	}, now, nil)
	if code := workspaceid.WorkspaceIDErrorCode(err); code != workspaceid.ErrCodeWorkspaceIDWhitespace {
		t.Fatalf("RegisterWorkspaceWithResultAndGrants error code = %q, want %q (err=%v)", code, workspaceid.ErrCodeWorkspaceIDWhitespace, err)
	}

	doc, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("Load failed: %v", loadErr)
	}
	if len(doc.Workspaces) != 0 {
		t.Fatalf("workspaces after rejected padded ID registration = %#v, want none", doc.Workspaces)
	}
}

func TestRegistryStoreRejectsSeededGrantWorkspaceIDWhitespaceBeforeNormalization(t *testing.T) {
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	store := NewRegistryStore(layout.DBPath)
	now := func() time.Time { return time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC) }

	_, err := store.RegisterWorkspaceWithResultAndGrants(WorkspaceRecord{
		ID:          workspaceid.WorkspaceID("docs"),
		DisplayName: "Docs",
		DataDir:     filepath.Join(t.TempDir(), "docs-data"),
		RootDir:     filepath.Join(t.TempDir(), "docs-root"),
		CreatedAt:   now(),
		UpdatedAt:   now(),
	}, now, func(registration RegisterWorkspaceResult) ([]Grant, error) {
		return []Grant{{
			Subject:     "user:agent",
			WorkspaceID: decodeWorkspaceIDForTest(t, " "+registration.Workspace.ID.StorageKey()+" "),
			Role:        GrantRoleViewer,
		}}, nil
	})
	if code := workspaceid.WorkspaceIDErrorCode(err); code != workspaceid.ErrCodeWorkspaceIDWhitespace {
		t.Fatalf("RegisterWorkspaceWithResultAndGrants seeded grant error code = %q, want %q (err=%v)", code, workspaceid.ErrCodeWorkspaceIDWhitespace, err)
	}

	doc, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("Load failed: %v", loadErr)
	}
	if len(doc.Workspaces) != 0 {
		t.Fatalf("workspaces after rejected seeded grant = %#v, want rollback", doc.Workspaces)
	}
	grants, loadErr := NewGrantStore(layout.DBPath).Load()
	if loadErr != nil {
		t.Fatalf("Load grants failed: %v", loadErr)
	}
	if len(grants.Grants) != 0 {
		t.Fatalf("grants after rejected seeded grant = %#v, want rollback", grants.Grants)
	}
}
