package wiki

import (
	"path/filepath"
	"testing"

	"github.com/perber/wiki/internal/workspaceid"
)

func TestWorkspaceCarriesValidatedSemanticWorkspaceID(t *testing.T) {
	t.Parallel()

	workspace := NormalizeWorkspace(Workspace{
		ID:      workspaceid.WorkspaceID("docs"),
		DataDir: filepath.Join(t.TempDir(), "data"),
	})
	var _ workspaceid.WorkspaceID = workspace.ID

	invalid := NormalizeWorkspace(Workspace{
		ID:      workspaceid.WorkspaceID("Docs"),
		DataDir: filepath.Join(t.TempDir(), "data"),
	})
	if err := ValidateWorkspace(invalid); err == nil {
		t.Fatal("ValidateWorkspace accepted invalid workspace ID")
	}
}

func TestValidateWorkspaceRejectsWhitespaceWorkspaceIDBeforeNormalization(t *testing.T) {
	t.Parallel()

	workspace := Workspace{
		ID:      workspaceid.WorkspaceID(" docs "),
		DataDir: filepath.Join(t.TempDir(), "data"),
	}

	err := ValidateWorkspace(workspace)
	if err == nil {
		t.Fatal("ValidateWorkspace accepted whitespace-padded workspace ID")
	}
	if got := workspaceid.WorkspaceIDErrorCode(err); got != workspaceid.ErrCodeWorkspaceIDWhitespace {
		t.Fatalf("workspace ID error code = %q, want %q", got, workspaceid.ErrCodeWorkspaceIDWhitespace)
	}
}
