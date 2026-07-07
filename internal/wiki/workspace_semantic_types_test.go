package wiki

import (
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
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

	ginkgo.It("resolves explicit workspace paths ahead of legacy storage directory options", func() {
		storageDir := filepath.Join(wikiTestTempDir(), "storage")
		dataDir := filepath.Join(wikiTestTempDir(), "workspace-data")
		rootDir := filepath.Join(wikiTestTempDir(), "workspace-root")

		workspace := resolveWorkspaceOptions(&WikiOptions{
			StorageDir: storageDir,
			Workspace: Workspace{
				ID:      newFixtureWorkspaceID("docs"),
				DataDir: dataDir,
				RootDir: rootDir,
			},
		})

		Expect(workspace).To(matchWorkspaceContract(workspaceContract{
			ID:      newFixtureWorkspaceID("docs"),
			DataDir: dataDir,
			RootDir: rootDir,
		}))
	})

	ginkgo.It("creates the configured data and content root directories", func() {
		workspace := Workspace{
			ID:      newFixtureWorkspaceID("docs"),
			DataDir: filepath.Join(wikiTestTempDir(), "data"),
			RootDir: filepath.Join(wikiTestTempDir(), "content"),
		}

		Expect(ensureWorkspaceDirs(workspace)).To(Succeed())

		Expect(workspace).To(haveWorkspaceDirectories())
	})
})

type workspaceContract struct {
	ID      workspaceid.WorkspaceID
	DataDir string
	RootDir string
}

func matchWorkspaceContract(want workspaceContract) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(workspaceContractFor, Equal(want))
}

func workspaceContractFor(workspace Workspace) workspaceContract {
	return workspaceContract{
		ID:      workspace.ID,
		DataDir: workspace.DataDir,
		RootDir: workspace.RootDir,
	}
}

type workspaceDirectoryState uint8

const (
	workspaceDirectoriesMissing workspaceDirectoryState = iota
	workspaceDirectoriesReady
)

func haveWorkspaceDirectories() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(workspaceDirectoryStateFor, Equal(workspaceDirectoriesReady))
}

func workspaceDirectoryStateFor(workspace Workspace) workspaceDirectoryState {
	if _, err := os.Stat(workspace.DataDir); err != nil {
		return workspaceDirectoriesMissing
	}
	if _, err := os.Stat(workspace.RootDir); err != nil {
		return workspaceDirectoriesMissing
	}
	return workspaceDirectoriesReady
}
