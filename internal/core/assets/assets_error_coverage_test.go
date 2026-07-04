package assets

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = Describe("asset service failure behavior", Label("unit"), func() {
	It("panics when storage or assets directories cannot be created", func() {
		storageBlocker := filepath.Join(tempAssetDir(), "storage-blocker")
		Expect(os.WriteFile(storageBlocker, []byte("not a directory"), 0o600)).To(Succeed())
		Expect(storageBlocker).To(BeARegularFile())

		Expect(func() {
			NewAssetService(filepath.Join(storageBlocker, "child"), tree.NewSlugService())
		}).To(Panic())
		Expect(storageBlocker).To(BeARegularFile())
		Expect(os.Stat(filepath.Join(storageBlocker, "child"))).Error().To(MatchError(syscall.ENOTDIR))

		storageDir := tempAssetDir()
		assetsPath := filepath.Join(storageDir, "assets")
		Expect(os.WriteFile(assetsPath, []byte("not a directory"), 0o600)).To(Succeed())
		Expect(assetsPath).To(BeARegularFile())

		Expect(func() {
			NewAssetService(storageDir, tree.NewSlugService())
		}).To(Panic())
		Expect(storageDir).To(BeADirectory())
		Expect(assetsPath).To(BeARegularFile())
	})

	It("returns localized upload failures for blocked page paths and stream errors", func() {
		permissionService := NewAssetService(tempAssetDir(), tree.NewSlugService())
		permissionAssetsDir := permissionService.GetAssetsDir()
		Expect(os.Chmod(permissionAssetsDir, 0o555)).To(Succeed())
		DeferCleanup(func() {
			Expect(os.Chmod(permissionAssetsDir, 0o755)).To(Succeed())
		})
		_, err := permissionService.SaveAssetForPage(&tree.PageNode{ID: "permission-denied"}, newTestMultipartFile([]byte("asset")), assetName("asset.png"), testAssetMaxBytes)
		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetUploadFailed))
		Expect(os.Chmod(permissionAssetsDir, 0o755)).To(Succeed())

		service := NewAssetService(tempAssetDir(), tree.NewSlugService())
		Expect(os.WriteFile(filepath.Join(service.GetAssetsDir(), "blocked"), []byte("file"), 0o600)).To(Succeed())

		_, err = service.SaveAssetForPage(&tree.PageNode{ID: "blocked/child"}, newTestMultipartFile([]byte("asset")), assetName("asset.png"), testAssetMaxBytes)
		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetUploadFailed))

		_, err = service.SaveAssetForPage(&tree.PageNode{ID: "stream-error"}, failingMultipartFile{Reader: bytes.NewReader([]byte("asset"))}, assetName("asset.png"), shared.MaxBytes(1024))
		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetUploadFailed))
	})

	It("handles malformed asset entries for list, read, and delete operations", func() {
		service := NewAssetService(tempAssetDir(), tree.NewSlugService())

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
		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetReadFailed))

		err = service.DeleteAsset(&tree.PageNode{ID: "missing-page"}, assetName("missing.txt"))
		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetNotFound))

		deletePage := &tree.PageNode{ID: "delete-page"}
		Expect(os.MkdirAll(filepath.Join(service.GetAssetsDir(), deletePage.ID.String(), "blocked.txt", "child"), 0o755)).To(Succeed())
		err = service.DeleteAsset(deletePage, assetName("blocked.txt"))
		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetDeleteFailed))
	})

	It("returns localized rename failures for missing pages, target stat errors, and rename syscall errors", func() {
		service := NewAssetService(tempAssetDir(), tree.NewSlugService())

		_, err := service.RenameAsset(&tree.PageNode{ID: "missing-page"}, assetName("old.txt"), assetName("new.txt"))
		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetNotFound))

		statErrorPage := &tree.PageNode{ID: "stat-error-page"}
		writeAssetFile(service, statErrorPage, "old.txt", []byte("old"))
		statErrorDir := filepath.Join(service.GetAssetsDir(), statErrorPage.ID.String())
		Expect(os.Symlink("new.txt", filepath.Join(statErrorDir, "new.txt"))).To(Succeed())
		_, err = service.RenameAsset(statErrorPage, assetName("old.txt"), assetName("new.txt"))
		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetRenameFailed))

		renameErrorPage := &tree.PageNode{ID: "rename-error-page"}
		Expect(os.MkdirAll(filepath.Join(service.GetAssetsDir(), renameErrorPage.ID.String()), 0o755)).To(Succeed())
		tooLongName := strings.Repeat("a", 4096) + ".txt"
		_, err = service.RenameAsset(renameErrorPage, assetName(tooLongName), assetName("renamed.txt"))
		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetRenameFailed))
	})

	It("returns copy failures for malformed source and target filesystem states", func() {
		service := NewAssetService(tempAssetDir(), tree.NewSlugService())

		source := &tree.PageNode{ID: "source"}
		writeAssetFile(service, source, "one.txt", []byte("one"))
		assetsDir := service.GetAssetsDir()
		Expect(os.Chmod(assetsDir, 0o555)).To(Succeed())
		DeferCleanup(func() {
			Expect(os.Chmod(assetsDir, 0o755)).To(Succeed())
		})
		err := service.CopyAllAssets(source, &tree.PageNode{ID: "blocked-target"})
		Expect(err).To(Satisfy(wrapsAssetPathError))
		Expect(os.Chmod(assetsDir, 0o755)).To(Succeed())

		sourceFilePage := &tree.PageNode{ID: "source-file"}
		Expect(os.WriteFile(filepath.Join(service.GetAssetsDir(), sourceFilePage.ID.String()), []byte("not a directory"), 0o600)).To(Succeed())
		err = service.CopyAllAssets(sourceFilePage, &tree.PageNode{ID: "target"})
		Expect(err).To(Satisfy(wrapsAssetPathError))

		danglingSource := &tree.PageNode{ID: "dangling-source"}
		danglingDir := filepath.Join(service.GetAssetsDir(), danglingSource.ID.String())
		Expect(os.MkdirAll(danglingDir, 0o755)).To(Succeed())
		Expect(os.Symlink("missing.txt", filepath.Join(danglingDir, "broken.txt"))).To(Succeed())
		err = service.CopyAllAssets(danglingSource, &tree.PageNode{ID: "dangling-target"})
		Expect(err).To(Satisfy(wrapsAssetPathError))

		directSourceDir := filepath.Join(tempAssetDir(), "direct-source")
		Expect(os.MkdirAll(directSourceDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(directSourceDir, "asset.txt"), []byte("asset"), 0o600)).To(Succeed())
		entries, err := os.ReadDir(directSourceDir)
		Expect(err).NotTo(HaveOccurred())
		targetFile := filepath.Join(tempAssetDir(), "target-file")
		Expect(os.WriteFile(targetFile, []byte("not a directory"), 0o600)).To(Succeed())
		err = service.copySingleAsset(directSourceDir, targetFile, entries[0])
		Expect(err).To(Satisfy(wrapsAssetPathError))

		copyErrorSourceDir := filepath.Join(tempAssetDir(), "copy-error-source")
		copyErrorTargetDir := filepath.Join(tempAssetDir(), "copy-error-target")
		Expect(os.MkdirAll(filepath.Join(copyErrorSourceDir, "nested"), 0o755)).To(Succeed())
		Expect(os.MkdirAll(copyErrorTargetDir, 0o755)).To(Succeed())
		entries, err = os.ReadDir(copyErrorSourceDir)
		Expect(err).NotTo(HaveOccurred())
		err = service.copySingleAsset(copyErrorSourceDir, copyErrorTargetDir, entries[0])
		Expect(err).To(Satisfy(wrapsAssetPathError))
	})

	It("logs asset copy close failures", func() {
		records := &assetFileCloseLogRecords{}
		logger := slog.New(records)
		closeErr := errors.New("close failed")

		logAssetFileClose(logger, "failed to close source file", "/tmp/source.txt", closeErrorAssetFile{err: closeErr})

		Expect(records.Events).To(ConsistOf(matchAssetFileCloseWarning(
			"failed to close source file",
			"/tmp/source.txt",
			closeErr,
		)))
	})
})

type assetFileCloseLogEvent struct {
	Level   slog.Level
	Message string
	Attrs   map[string]any
}

type assetFileCloseLogRecords struct {
	Events []assetFileCloseLogEvent
}

func (r *assetFileCloseLogRecords) Enabled(context.Context, slog.Level) bool {
	return true
}

func (r *assetFileCloseLogRecords) Handle(_ context.Context, record slog.Record) error {
	event := assetFileCloseLogEvent{
		Level:   record.Level,
		Message: record.Message,
		Attrs:   map[string]any{},
	}
	record.Attrs(func(attr slog.Attr) bool {
		event.Attrs[attr.Key] = attr.Value.Any()
		return true
	})
	r.Events = append(r.Events, event)
	return nil
}

func (r *assetFileCloseLogRecords) WithAttrs([]slog.Attr) slog.Handler {
	return r
}

func (r *assetFileCloseLogRecords) WithGroup(string) slog.Handler {
	return r
}

func matchAssetFileCloseWarning(message, filePath string, closeErr error) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Level":   Equal(slog.LevelWarn),
		"Message": Equal(message),
		"Attrs": SatisfyAll(
			HaveKeyWithValue("file", filePath),
			HaveKeyWithValue("error", BeIdenticalTo(closeErr)),
		),
	})
}

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

func wrapsAssetPathError(err error) bool {
	var pathErr *os.PathError
	return errors.As(err, &pathErr)
}
