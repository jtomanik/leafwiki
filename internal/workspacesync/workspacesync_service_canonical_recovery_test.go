package workspacesync

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/core/tree"
)

var errCanonicalMarkdownMigrationUnchanged = errors.New("canonical markdown migration did not change files")

var _ = Describe("workspace sync canonical migration recovery", Label("unit"), func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("rolls back canonical migration status for nil, failed, tree-less, reconstruct-failed, and successful rollbacks", func() {
		service := &Service{status: SyncStatus{LastError: "primary"}}
		service.rollbackCanonicalMarkdownMigrationLocked(nil)
		Expect(service.status.LastError).To(Equal("primary"))

		rollbackErr := errors.New("rollback failed")
		service.rollbackCanonicalMarkdownMigrationLocked(func() error { return rollbackErr })
		Expect(service.status).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"LastError":        Not(Equal("primary")),
			"ValidationErrors": BeNil(),
		}))

		service = &Service{status: SyncStatus{LastError: "primary"}}
		service.rollbackCanonicalMarkdownMigrationLocked(func() error { return nil })
		Expect(service.status.LastError).To(Equal("primary"))

		reconstructRollbackErr := errors.New("reconstruct rollback failed")
		reconstructTree := &fakeTreeReconstructor{err: reconstructRollbackErr}
		service = &Service{tree: reconstructTree, status: SyncStatus{LastError: "primary"}}
		service.rollbackCanonicalMarkdownMigrationLocked(func() error { return nil })
		Expect(service.status).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"LastError":        Not(Equal("primary")),
			"ValidationErrors": Not(BeEmpty()),
		}))
		Expect(reconstructTree.reconstructCount()).To(Equal(1))

		service = &Service{tree: &fakeTreeReconstructor{}, status: SyncStatus{LastError: "primary", ValidationErrors: []ValidationError{{Path: "old"}}}}
		service.rollbackCanonicalMarkdownMigrationLocked(func() error { return nil })
		Expect(service.status).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"LastError":        Equal("primary"),
			"ValidationErrors": BeNil(),
		}))
	})

	It("rolls back canonical migration when reconstruction and filesystem seams fail", func() {
		preserveWorkspacesyncServiceSeams()

		rootDir := workspaceSyncTempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("[B](/docs/b)\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "b.md"), []byte("# B\n"), 0o644)).To(Succeed())
		secondReconstructErr := errors.New("second reconstruct failed")
		service := workspaceSyncServiceHarness(
			&fakeRevisionStore{capture: workspaceSyncServiceCommit("migration-second-reconstruct")},
			&fakeTreeReconstructor{errs: []error{nil, secondReconstructErr, nil}},
		)
		service.rootDir = rootDir
		_, err := service.SyncNow(ctx, SyncRequest{Reason: ReasonExplicit, Source: SourceFilesystem, Actor: PublicEditorActor()})
		Expect(err).To(Succeed())
		Expect(readFileStringGinkgo(filepath.Join(rootDir, "docs", "a.md"))).To(SatisfyAll(
			ContainSubstring("[B](/docs/b)\n"),
			Not(ContainSubstring("[B](/docs/b.md)")),
		))
		Expect(service.tree.(*fakeTreeReconstructor).reconstructCount()).To(Equal(3))

		service = &Service{rootDir: ""}
		rollback, err := canonicalMarkdownMigrationRollbackResult(service)
		Expect(err).To(MatchError(errCanonicalMarkdownMigrationUnchanged))
		Expect(rollback).To(BeNil())

		rootFile := filepath.Join(workspaceSyncTempDir(), "root.md")
		Expect(os.WriteFile(rootFile, []byte("# Root\n"), 0o644)).To(Succeed())
		service = &Service{rootDir: rootFile}
		rollback, err = canonicalMarkdownMigrationRollbackResult(service)
		Expect(err).To(MatchError(errCanonicalMarkdownMigrationUnchanged))
		Expect(rollback).To(BeNil())

		service = &Service{rootDir: "/workspace"}
		statErr := errors.New("stat failed")
		workspacesyncOSStat = func(string) (os.FileInfo, error) {
			return nil, statErr
		}
		_, err = canonicalMarkdownMigrationRollbackResult(service)
		Expect(err).To(MatchError(statErr))

		workspacesyncOSStat = os.Stat
		indexErr := errors.New("index failed")
		workspacesyncNewMarkdownLinkIndex = func(string, markdownlinks.Options) (*markdownlinks.Index, error) {
			return nil, indexErr
		}
		service.rootDir = workspaceSyncTempDir()
		_, err = canonicalMarkdownMigrationRollbackResult(service)
		Expect(err).To(MatchError(indexErr))

		workspacesyncNewMarkdownLinkIndex = markdownlinks.NewIndexFromRootWithOptions
		walkErr := errors.New("walk failed")
		workspacesyncWalkDir = func(root string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(root, "bad.md"), workspacesyncServiceDirEntry{name: "bad.md"}, walkErr)
		}
		_, err = canonicalMarkdownMigrationRollbackResult(service)
		Expect(err).To(MatchError(walkErr))

		workspacesyncWalkDir = func(root string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(root, "bad.md"), workspacesyncServiceDirEntry{name: "bad.md"}, nil)
		}
		relErr := errors.New("rel failed")
		workspacesyncRel = func(string, string) (string, error) {
			return "", relErr
		}
		_, err = canonicalMarkdownMigrationRollbackResult(service)
		Expect(err).To(MatchError(relErr))

		workspacesyncRel = filepath.Rel
		workspacesyncWalkDir = func(root string, fn fs.WalkDirFunc) error {
			Expect(fn(filepath.Join(root, ".hidden"), workspacesyncServiceDirEntry{name: ".hidden", dir: true}, nil)).To(Equal(filepath.SkipDir))
			Expect(fn(filepath.Join(root, "notes.txt"), workspacesyncServiceDirEntry{name: "notes.txt"}, nil)).To(Succeed())
			return nil
		}
		rollback, err = canonicalMarkdownMigrationRollbackResult(service)
		Expect(err).To(MatchError(errCanonicalMarkdownMigrationUnchanged))
		Expect(rollback).To(BeNil())

		index := markdownlinks.NewIndexWithOptions([]markdownlinks.Entry{
			{Kind: markdownlinks.EntryKindSection},
			{Kind: markdownlinks.EntryKindPage, RoutePath: tree.RoutePathFromString("b"), ContentPath: tree.MarkdownPathFromString("b.md")},
		}, markdownlinks.Options{})
		workspacesyncNewMarkdownLinkIndex = func(string, markdownlinks.Options) (*markdownlinks.Index, error) {
			return index, nil
		}
		workspacesyncWalkDir = func(root string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(root, "a.md"), workspacesyncServiceDirEntry{name: "a.md"}, nil)
		}
		readErr := errors.New("read failed")
		workspacesyncReadFile = func(string) ([]byte, error) {
			return nil, readErr
		}
		_, err = canonicalMarkdownMigrationRollbackResult(service)
		Expect(err).To(MatchError(readErr))

		workspacesyncReadFile = func(string) ([]byte, error) {
			return []byte("[B](/b)\n"), nil
		}
		infoErr := errors.New("info failed")
		workspacesyncWalkDir = func(root string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(root, "a.md"), workspacesyncServiceDirEntry{name: "a.md", infoErr: infoErr}, nil)
		}
		_, err = canonicalMarkdownMigrationRollbackResult(service)
		Expect(err).To(MatchError(infoErr))
	})

	It("cleans temporary canonical rewrite files and reports rollback failures", func() {
		preserveWorkspacesyncServiceSeams()
		rewrite := canonicalMarkdownRewrite{Path: "/workspace/a.md", Original: []byte("old"), Content: []byte("new"), Mode: 0o644}

		createTempErr := errors.New("create temp failed")
		workspacesyncCreateTemp = func(string, string) (workspacesyncTempFile, error) {
			return nil, createTempErr
		}
		Expect(writeCanonicalMarkdownRewritesAtomically([]canonicalMarkdownRewrite{rewrite})).To(MatchError(createTempErr))

		removed := []string{}
		workspacesyncRemove = func(path string) error {
			removed = append(removed, path)
			return nil
		}
		writeErr := errors.New("write failed")
		workspacesyncCreateTemp = func(string, string) (workspacesyncTempFile, error) {
			return &workspacesyncServiceTempFile{name: "/workspace/.a.tmp", writeErr: writeErr}, nil
		}
		Expect(writeCanonicalMarkdownRewritesAtomically([]canonicalMarkdownRewrite{rewrite})).To(MatchError(writeErr))
		Expect(removed).To(ContainElement("/workspace/.a.tmp"))

		closeErr := errors.New("close failed")
		workspacesyncCreateTemp = func(string, string) (workspacesyncTempFile, error) {
			return &workspacesyncServiceTempFile{name: "/workspace/.a.tmp", closeErr: closeErr}, nil
		}
		Expect(writeCanonicalMarkdownRewritesAtomically([]canonicalMarkdownRewrite{rewrite})).To(MatchError(closeErr))

		workspacesyncCreateTemp = func(string, string) (workspacesyncTempFile, error) {
			return &workspacesyncServiceTempFile{name: "/workspace/.a.tmp"}, nil
		}
		chmodErr := errors.New("chmod failed")
		workspacesyncChmod = func(string, os.FileMode) error {
			return chmodErr
		}
		Expect(writeCanonicalMarkdownRewritesAtomically([]canonicalMarkdownRewrite{rewrite})).To(MatchError(chmodErr))

		workspacesyncChmod = func(string, os.FileMode) error {
			return nil
		}
		tempNames := []string{"/workspace/.a.tmp", "/workspace/.b.tmp"}
		workspacesyncCreateTemp = func(string, string) (workspacesyncTempFile, error) {
			name := tempNames[0]
			tempNames = tempNames[1:]
			return &workspacesyncServiceTempFile{name: name}, nil
		}
		renameCalls := 0
		renameErr := errors.New("rename failed")
		workspacesyncRename = func(string, string) error {
			renameCalls++
			if renameCalls == 2 {
				return renameErr
			}
			return nil
		}
		rollbackWriteErr := errors.New("rollback write failed")
		workspacesyncWriteFile = func(string, []byte, os.FileMode) error {
			return rollbackWriteErr
		}
		err := writeCanonicalMarkdownRewritesAtomically([]canonicalMarkdownRewrite{
			rewrite,
			{Path: "/workspace/b.md", Original: []byte("old b"), Content: []byte("new b"), Mode: 0o644},
		})
		Expect(err).To(MatchError(rollbackWriteErr))

		Expect(rollbackCanonicalMarkdownRewrites([]canonicalMarkdownRewrite{rewrite})).To(MatchError(rollbackWriteErr))
	})

})
