package wiki

import (
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = ginkgo.It("TestWorkspaceCarriesValidatedSemanticWorkspaceID", func() {
	t := ginkgo.GinkgoT()

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
})

var _ = ginkgo.It("TestValidateWorkspaceRejectsWhitespaceWorkspaceIDBeforeNormalization", func() {
	t := ginkgo.GinkgoT()

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
})
