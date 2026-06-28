package assets

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = Describe("asset service error branches", func() {
	It("panics when storage or assets directories cannot be created", func() {
		storageBlocker := filepath.Join(GinkgoT().TempDir(), "storage-blocker")
		Expect(os.WriteFile(storageBlocker, []byte("not a directory"), 0o600)).To(Succeed())

		Expect(func() {
			NewAssetService(filepath.Join(storageBlocker, "child"), tree.NewSlugService())
		}).To(PanicWith(ContainSubstring("could not create storage directory")))

		storageDir := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(storageDir, "assets"), []byte("not a directory"), 0o600)).To(Succeed())

		Expect(func() {
			NewAssetService(storageDir, tree.NewSlugService())
		}).To(PanicWith(ContainSubstring("could not create assets directory")))
	})

	It("returns localized upload failures for blocked page paths and stream errors", func() {
		permissionService := NewAssetService(GinkgoT().TempDir(), tree.NewSlugService())
		permissionAssetsDir := permissionService.GetAssetsDir()
		Expect(os.Chmod(permissionAssetsDir, 0o555)).To(Succeed())
		DeferCleanup(func() {
			Expect(os.Chmod(permissionAssetsDir, 0o755)).To(Succeed())
		})
		_, err := permissionService.SaveAssetForPage(&tree.PageNode{ID: "permission-denied"}, newTestMultipartFile([]byte("asset")), assetName("asset.png"), testAssetMaxBytes)
		assertLocalizedCode(err, ErrCodeAssetUploadFailed)
		Expect(os.Chmod(permissionAssetsDir, 0o755)).To(Succeed())

		service := NewAssetService(GinkgoT().TempDir(), tree.NewSlugService())
		Expect(os.WriteFile(filepath.Join(service.GetAssetsDir(), "blocked"), []byte("file"), 0o600)).To(Succeed())

		_, err = service.SaveAssetForPage(&tree.PageNode{ID: "blocked/child"}, newTestMultipartFile([]byte("asset")), assetName("asset.png"), testAssetMaxBytes)
		assertLocalizedCode(err, ErrCodeAssetUploadFailed)

		_, err = service.SaveAssetForPage(&tree.PageNode{ID: "stream-error"}, failingMultipartFile{Reader: bytes.NewReader([]byte("asset"))}, assetName("asset.png"), shared.MaxBytes(1024))
		assertLocalizedCode(err, ErrCodeAssetUploadFailed)
	})

	It("handles malformed asset entries for list, read, and delete operations", func() {
		service := NewAssetService(GinkgoT().TempDir(), tree.NewSlugService())

		Expect(service.DeleteAllAssetsForPage(&tree.PageNode{ID: "never-uploaded"})).To(Succeed())
		loopPage := &tree.PageNode{ID: "loop-page"}
		loopPath := filepath.Join(service.GetAssetsDir(), loopPage.ID.String())
		Expect(os.Symlink(loopPath, loopPath)).To(Succeed())
		Expect(service.DeleteAllAssetsForPage(loopPage)).To(Succeed())

		listPage := &tree.PageNode{ID: "list-page"}
		Expect(os.WriteFile(filepath.Join(service.GetAssetsDir(), listPage.ID.String()), []byte("not a directory"), 0o600)).To(Succeed())
		files, err := service.ListAssetsForPage(listPage)
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(BeEmpty())

		readPage := &tree.PageNode{ID: "read-page"}
		Expect(os.MkdirAll(filepath.Join(service.GetAssetsDir(), readPage.ID.String(), "note.txt"), 0o755)).To(Succeed())
		_, err = service.ReadAssetForPage(readPage, assetName("note.txt"))
		assertLocalizedCode(err, ErrCodeAssetReadFailed)

		err = service.DeleteAsset(&tree.PageNode{ID: "missing-page"}, assetName("missing.txt"))
		assertLocalizedCode(err, ErrCodeAssetNotFound)

		deletePage := &tree.PageNode{ID: "delete-page"}
		Expect(os.MkdirAll(filepath.Join(service.GetAssetsDir(), deletePage.ID.String(), "blocked.txt", "child"), 0o755)).To(Succeed())
		err = service.DeleteAsset(deletePage, assetName("blocked.txt"))
		assertLocalizedCode(err, ErrCodeAssetDeleteFailed)
	})

	It("returns localized rename failures for missing pages, target stat errors, and rename syscall errors", func() {
		service := NewAssetService(GinkgoT().TempDir(), tree.NewSlugService())

		_, err := service.RenameAsset(&tree.PageNode{ID: "missing-page"}, assetName("old.txt"), assetName("new.txt"))
		assertLocalizedCode(err, ErrCodeAssetNotFound)

		statErrorPage := &tree.PageNode{ID: "stat-error-page"}
		writeAssetFile(service, statErrorPage, "old.txt", []byte("old"))
		statErrorDir := filepath.Join(service.GetAssetsDir(), statErrorPage.ID.String())
		Expect(os.Symlink("new.txt", filepath.Join(statErrorDir, "new.txt"))).To(Succeed())
		_, err = service.RenameAsset(statErrorPage, assetName("old.txt"), assetName("new.txt"))
		assertLocalizedCode(err, ErrCodeAssetRenameFailed)

		renameErrorPage := &tree.PageNode{ID: "rename-error-page"}
		Expect(os.MkdirAll(filepath.Join(service.GetAssetsDir(), renameErrorPage.ID.String()), 0o755)).To(Succeed())
		tooLongName := strings.Repeat("a", 4096) + ".txt"
		_, err = service.RenameAsset(renameErrorPage, assetName(tooLongName), assetName("renamed.txt"))
		assertLocalizedCode(err, ErrCodeAssetRenameFailed)
	})

	It("returns copy failures for malformed source and target filesystem states", func() {
		service := NewAssetService(GinkgoT().TempDir(), tree.NewSlugService())

		source := &tree.PageNode{ID: "source"}
		writeAssetFile(service, source, "one.txt", []byte("one"))
		assetsDir := service.GetAssetsDir()
		Expect(os.Chmod(assetsDir, 0o555)).To(Succeed())
		DeferCleanup(func() {
			Expect(os.Chmod(assetsDir, 0o755)).To(Succeed())
		})
		err := service.CopyAllAssets(source, &tree.PageNode{ID: "blocked-target"})
		Expect(err).To(MatchError(ContainSubstring("could not create target asset path")))
		Expect(os.Chmod(assetsDir, 0o755)).To(Succeed())

		sourceFilePage := &tree.PageNode{ID: "source-file"}
		Expect(os.WriteFile(filepath.Join(service.GetAssetsDir(), sourceFilePage.ID.String()), []byte("not a directory"), 0o600)).To(Succeed())
		err = service.CopyAllAssets(sourceFilePage, &tree.PageNode{ID: "target"})
		Expect(err).To(MatchError(ContainSubstring("could not read source asset directory")))

		danglingSource := &tree.PageNode{ID: "dangling-source"}
		danglingDir := filepath.Join(service.GetAssetsDir(), danglingSource.ID.String())
		Expect(os.MkdirAll(danglingDir, 0o755)).To(Succeed())
		Expect(os.Symlink("missing.txt", filepath.Join(danglingDir, "broken.txt"))).To(Succeed())
		err = service.CopyAllAssets(danglingSource, &tree.PageNode{ID: "dangling-target"})
		Expect(err).To(MatchError(ContainSubstring("could not copy asset broken.txt")))

		directSourceDir := filepath.Join(GinkgoT().TempDir(), "direct-source")
		Expect(os.MkdirAll(directSourceDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(directSourceDir, "asset.txt"), []byte("asset"), 0o600)).To(Succeed())
		entries, err := os.ReadDir(directSourceDir)
		Expect(err).NotTo(HaveOccurred())
		targetFile := filepath.Join(GinkgoT().TempDir(), "target-file")
		Expect(os.WriteFile(targetFile, []byte("not a directory"), 0o600)).To(Succeed())
		err = service.copySingleAsset(directSourceDir, targetFile, entries[0])
		Expect(err).To(MatchError(ContainSubstring("could not create target asset file")))

		copyErrorSourceDir := filepath.Join(GinkgoT().TempDir(), "copy-error-source")
		copyErrorTargetDir := filepath.Join(GinkgoT().TempDir(), "copy-error-target")
		Expect(os.MkdirAll(filepath.Join(copyErrorSourceDir, "nested"), 0o755)).To(Succeed())
		Expect(os.MkdirAll(copyErrorTargetDir, 0o755)).To(Succeed())
		entries, err = os.ReadDir(copyErrorSourceDir)
		Expect(err).NotTo(HaveOccurred())
		err = service.copySingleAsset(copyErrorSourceDir, copyErrorTargetDir, entries[0])
		Expect(err).To(MatchError(ContainSubstring("could not copy asset file")))
	})

	It("logs asset copy close failures", func() {
		var logOutput bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logOutput, nil))

		logAssetFileClose(logger, "failed to close source file", "/tmp/source.txt", closeErrorAssetFile{err: errors.New("close failed")})

		Expect(logOutput.String()).To(ContainSubstring("failed to close source file"))
		Expect(logOutput.String()).To(ContainSubstring("/tmp/source.txt"))
		Expect(logOutput.String()).To(ContainSubstring("close failed"))
	})
})

type failingMultipartFile struct {
	*bytes.Reader
}

func (f failingMultipartFile) Read(_ []byte) (int, error) {
	return 0, errors.New("read failed")
}

func (f failingMultipartFile) Close() error {
	return nil
}

type closeErrorAssetFile struct {
	err error
}

func (f closeErrorAssetFile) Close() error {
	return f.err
}
