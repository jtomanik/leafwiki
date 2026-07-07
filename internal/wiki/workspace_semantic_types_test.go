package wiki

import (
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = ginkgo.Describe("workspace identity validation", ginkgo.Label("unit"), func() {
	ginkgo.It("preserves validated semantic workspace IDs on normalized workspaces", func() {
		workspace := NormalizeWorkspace(Workspace{
			ID:      newFixtureWorkspaceID("docs"),
			DataDir: filepath.Join(wikiTestTempDir(), "data"),
		})
		var _ workspaceid.WorkspaceID = workspace.ID

		invalid := NormalizeWorkspace(Workspace{
			ID:      newFixtureWorkspaceID("Docs"),
			DataDir: filepath.Join(wikiTestTempDir(), "data"),
		})
		Expect(ValidateWorkspace(invalid)).To(WithTransform(workspaceid.WorkspaceIDErrorCode, Equal(workspaceid.ErrCodeWorkspaceIDInvalid)))
	})

	ginkgo.It("rejects whitespace-padded workspace IDs before normalization", func() {
		workspace := Workspace{
			ID:      newFixtureWorkspaceID(" docs "),
			DataDir: filepath.Join(wikiTestTempDir(), "data"),
		}

		err := ValidateWorkspace(workspace)
		Expect(err).To(WithTransform(workspaceid.WorkspaceIDErrorCode, Equal(workspaceid.ErrCodeWorkspaceIDWhitespace)))
	})
})
