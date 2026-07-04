package wiki

import (
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("workspace directory validation", func() {
	ginkgo.It("accepts the default content root under the data directory", func() {
		dataDir := filepath.Join(wikiTestTempDir(), "data")

		Expect(ValidateWorkspace(DefaultWorkspace(dataDir))).To(Succeed())
	})

	ginkgo.It("rejects content roots that contain the data directory", func() {
		rootDir := filepath.Join(wikiTestTempDir(), "wiki")
		dataDir := filepath.Join(rootDir, "data")

		err := ValidateWorkspace(Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir})
		Expect(err).To(MatchError(ErrWorkspaceRootDirContainsDataDir))
	})

	ginkgo.It("rejects content roots inside reserved data-directory state", func() {
		dataDir := filepath.Join(wikiTestTempDir(), "data")
		rootDir := filepath.Join(dataDir, "assets", "pages")

		err := ValidateWorkspace(Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir})
		Expect(err).To(MatchError(ErrWorkspaceRootDirInsideDataDirAppState))
	})

	ginkgo.DescribeTable("reserved federated control directories",
		func(reserved string) {
			dataDir := filepath.Join(wikiTestTempDir(), ".leafwiki")
			rootDir := filepath.Join(dataDir, reserved, "workspace")

			err := ValidateWorkspace(Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir})
			Expect(err).To(MatchError(ErrWorkspaceRootDirInsideDataDirAppState))
		},
		ginkgo.Entry("rejects roots inside wikid control state", "wikid"),
		ginkgo.Entry("rejects roots inside runtime control state", "runtime"),
	)

	ginkgo.It("rejects symlinked content roots that contain the data directory", func() {
		baseDir := wikiTestTempDir()
		rootTarget := filepath.Join(baseDir, "wiki")
		dataDir := filepath.Join(rootTarget, "data")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		rootLink := filepath.Join(baseDir, "root-link")
		Expect(os.Symlink(rootTarget, rootLink)).To(Succeed())

		err := ValidateWorkspace(Workspace{ID: "default", DataDir: dataDir, RootDir: rootLink})
		Expect(err).To(MatchError(ErrWorkspaceRootDirContainsDataDir))
	})

	ginkgo.It("rejects symlinked content roots inside reserved data-directory state", func() {
		baseDir := wikiTestTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootTarget := filepath.Join(dataDir, "assets", "pages")
		Expect(os.MkdirAll(rootTarget, 0o755)).To(Succeed())
		rootLink := filepath.Join(baseDir, "root-link")
		Expect(os.Symlink(rootTarget, rootLink)).To(Succeed())

		err := ValidateWorkspace(Workspace{ID: "default", DataDir: dataDir, RootDir: rootLink})
		Expect(err).To(MatchError(ErrWorkspaceRootDirInsideDataDirAppState))
	})
})
