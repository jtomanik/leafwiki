package wiki

import (
	"errors"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.It("TestValidateWorkspace_AllowsDefaultRootUnderDataDir", func() {
	t := ginkgo.GinkgoT()
	dataDir := filepath.Join(t.TempDir(), "data")

	if err := ValidateWorkspace(DefaultWorkspace(dataDir)); err != nil {
		t.Fatalf("ValidateWorkspace default root failed: %v", err)
	}
})

var _ = ginkgo.It("TestValidateWorkspace_RejectsRootDirContainingDataDir", func() {
	t := ginkgo.GinkgoT()
	rootDir := filepath.Join(t.TempDir(), "wiki")
	dataDir := filepath.Join(rootDir, "data")

	err := ValidateWorkspace(Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir})
	if err == nil {
		t.Fatalf("expected root dir containing data dir to be rejected")
	}
	if !errors.Is(err, ErrWorkspaceRootDirContainsDataDir) {
		t.Fatalf("unexpected error: %v", err)
	}
})

var _ = ginkgo.It("TestValidateWorkspace_RejectsRootDirInsideReservedDataDirState", func() {
	t := ginkgo.GinkgoT()
	dataDir := filepath.Join(t.TempDir(), "data")
	rootDir := filepath.Join(dataDir, "assets", "pages")

	err := ValidateWorkspace(Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir})
	if err == nil {
		t.Fatalf("expected root dir inside reserved app state to be rejected")
	}
	if !errors.Is(err, ErrWorkspaceRootDirInsideDataDirAppState) {
		t.Fatalf("unexpected error: %v", err)
	}
})

var _ = ginkgo.DescribeTable("TestValidateWorkspace_RejectsRootDirInsideFederatedControlDirs",
	func(reserved string) {
		t := ginkgo.GinkgoT()
		dataDir := filepath.Join(t.TempDir(), ".leafwiki")
		rootDir := filepath.Join(dataDir, reserved, "workspace")

		err := ValidateWorkspace(Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir})
		if err == nil {
			t.Fatalf("expected root dir inside %s control state to be rejected", reserved)
		}
		if !errors.Is(err, ErrWorkspaceRootDirInsideDataDirAppState) {
			t.Fatalf("unexpected error: %v", err)
		}
	},
	ginkgo.Entry("wikid", "wikid"),
	ginkgo.Entry("runtime", "runtime"),
)

var _ = ginkgo.It("TestValidateWorkspace_RejectsRootDirSymlinkContainingDataDir", func() {
	t := ginkgo.GinkgoT()
	baseDir := t.TempDir()
	rootTarget := filepath.Join(baseDir, "wiki")
	dataDir := filepath.Join(rootTarget, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	rootLink := filepath.Join(baseDir, "root-link")
	if err := os.Symlink(rootTarget, rootLink); err != nil {
		t.Fatalf("symlink root target: %v", err)
	}

	err := ValidateWorkspace(Workspace{ID: "default", DataDir: dataDir, RootDir: rootLink})
	if err == nil {
		t.Fatalf("expected symlinked root containing data dir to be rejected")
	}
	if !errors.Is(err, ErrWorkspaceRootDirContainsDataDir) {
		t.Fatalf("unexpected error: %v", err)
	}
})

var _ = ginkgo.It("TestValidateWorkspace_RejectsRootDirSymlinkInsideReservedDataDirState", func() {
	t := ginkgo.GinkgoT()
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootTarget := filepath.Join(dataDir, "assets", "pages")
	if err := os.MkdirAll(rootTarget, 0o755); err != nil {
		t.Fatalf("mkdir root target: %v", err)
	}
	rootLink := filepath.Join(baseDir, "root-link")
	if err := os.Symlink(rootTarget, rootLink); err != nil {
		t.Fatalf("symlink root target: %v", err)
	}

	err := ValidateWorkspace(Workspace{ID: "default", DataDir: dataDir, RootDir: rootLink})
	if err == nil {
		t.Fatalf("expected symlinked root inside app state to be rejected")
	}
	if !errors.Is(err, ErrWorkspaceRootDirInsideDataDirAppState) {
		t.Fatalf("unexpected error: %v", err)
	}
})
