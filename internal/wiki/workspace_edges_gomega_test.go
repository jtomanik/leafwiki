package wiki

import (
	"errors"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type workspacePathContainment string

const (
	workspacePathContained    workspacePathContainment = "contained"
	workspacePathNotContained workspacePathContainment = "not-contained"
)

func workspacePathContainmentFor(parent, child string) workspacePathContainment {
	if pathContains(parent, child) {
		return workspacePathContained
	}
	return workspacePathNotContained
}

var _ = ginkgo.Describe("workspace validation edges", ginkgo.Label("unit"), func() {
	ginkgo.It("rejects missing data and equal root/data directories", func() {
		sameDir := filepath.Join(wikiTestTempDir(), "same")

		Expect(ValidateWorkspace(Workspace{ID: "default"})).To(MatchError(ErrWorkspaceDataDirRequired))
		Expect(ValidateWorkspace(Workspace{ID: "default", DataDir: sameDir, RootDir: sameDir})).To(MatchError(ErrWorkspaceRootDirEqualsDataDir))
	})

	ginkgo.It("surfaces absolute path failures from the resolver", func() {
		expected := errors.New("abs failed")
		restoreWorkspacePathSeams()
		resolveWorkspaceAbs = func(string) (string, error) {
			return "", expected
		}

		_, err := resolveWorkspacePath("docs")
		Expect(err).To(MatchError(expected))
	})

	ginkgo.It("surfaces data and root path resolution errors", func() {
		dataDir := filepath.Join(wikiTestTempDir(), "data")
		rootDir := filepath.Join(wikiTestTempDir(), "root")
		dataErr := errors.New("data resolution failed")
		rootErr := errors.New("root resolution failed")
		restoreWorkspacePathSeams()

		resolveWorkspaceEvalSymlinks = func(path string) (string, error) {
			if path == dataDir {
				return "", dataErr
			}
			return path, nil
		}

		err := ValidateWorkspace(Workspace{
			ID:      "default",
			DataDir: dataDir,
			RootDir: rootDir,
		})
		Expect(err).To(MatchError(dataErr))

		resolveWorkspaceEvalSymlinks = func(path string) (string, error) {
			if path == rootDir {
				return "", rootErr
			}
			return path, nil
		}

		err = ValidateWorkspace(Workspace{
			ID:      "default",
			DataDir: dataDir,
			RootDir: rootDir,
		})
		Expect(err).To(MatchError(rootErr))
	})

	ginkgo.It("keeps unresolved paths when no existing parent can be resolved", func() {
		restoreWorkspacePathSeams()
		resolveWorkspaceAbs = func(string) (string, error) {
			return string(filepath.Separator) + "missing", nil
		}
		resolveWorkspaceEvalSymlinks = func(string) (string, error) {
			return "", os.ErrNotExist
		}

		resolved, err := resolveWorkspacePath("missing")
		Expect(err).NotTo(HaveOccurred())
		Expect(resolved).To(Equal(string(filepath.Separator) + "missing"))
	})

	ginkgo.It("surfaces resolver failures while walking missing parents", func() {
		expected := errors.New("parent failed")
		restoreWorkspacePathSeams()
		resolveWorkspaceAbs = func(string) (string, error) {
			return filepath.Join(string(filepath.Separator), "missing", "leaf"), nil
		}
		callCount := 0
		resolveWorkspaceEvalSymlinks = func(string) (string, error) {
			callCount++
			if callCount == 1 {
				return "", os.ErrNotExist
			}
			return "", expected
		}

		_, err := resolveWorkspacePath("leaf")
		Expect(err).To(MatchError(expected))
	})

	ginkgo.It("treats paths with no relative form as not contained", func() {
		Expect(workspacePathContainmentFor(filepath.Join(wikiTestTempDir(), "parent"), "relative-child")).To(Equal(workspacePathNotContained))
	})
})

func restoreWorkspacePathSeams() {
	ginkgo.GinkgoHelper()

	originalAbs := resolveWorkspaceAbs
	originalEvalSymlinks := resolveWorkspaceEvalSymlinks
	ginkgo.DeferCleanup(func() {
		resolveWorkspaceAbs = originalAbs
		resolveWorkspaceEvalSymlinks = originalEvalSymlinks
	})
}
