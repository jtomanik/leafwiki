package workspacesync

import (
	"io/fs"
	"time"

	. "github.com/onsi/ginkgo/v2"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

func workspaceSyncServiceHarness(store *fakeRevisionStore, treeService treeReconstructor) *Service {
	GinkgoHelper()
	return &Service{
		enabled: true,
		rootDir: "/workspace",
		tree:    treeService,
		store:   store,
		status:  SyncStatus{Enabled: true},
	}
}

func canonicalMarkdownMigrationRollbackResult(service *Service) (func() error, error) {
	GinkgoHelper()

	changed, rollback, err := service.migrateCanonicalMarkdownLinksLockedWithRollback()
	if err != nil {
		return nil, err
	}
	if !changed {
		return rollback, errCanonicalMarkdownMigrationUnchanged
	}
	return rollback, nil
}

func contentForPageAtCommitPathResult(rootDir string, page *tree.Page, preferredPath string, files map[string]string) (changedContentResult, error) {
	GinkgoHelper()

	content, relPath, ok := contentForPageAtCommitPath(rootDir, page, preferredPath, files)
	if !ok {
		return changedContentResult{}, errChangedContentMissing
	}
	return changedContentResult{
		Content: content,
		RelPath: relPath,
	}, nil
}

func preserveWorkspacesyncServiceSeams() {
	GinkgoHelper()
	previousNewFSWatcher := workspacesyncNewFSWatcher
	previousRewriteWriter := canonicalMarkdownRewriteWriter
	previousStat := workspacesyncOSStat
	previousNewIndex := workspacesyncNewMarkdownLinkIndex
	previousWalkDir := workspacesyncWalkDir
	previousRel := workspacesyncRel
	previousReadFile := workspacesyncReadFile
	previousCreateTemp := workspacesyncCreateTemp
	previousRemove := workspacesyncRemove
	previousRename := workspacesyncRename
	previousChmod := workspacesyncChmod
	previousWriteFile := workspacesyncWriteFile
	DeferCleanup(func() {
		workspacesyncNewFSWatcher = previousNewFSWatcher
		canonicalMarkdownRewriteWriter = previousRewriteWriter
		workspacesyncOSStat = previousStat
		workspacesyncNewMarkdownLinkIndex = previousNewIndex
		workspacesyncWalkDir = previousWalkDir
		workspacesyncRel = previousRel
		workspacesyncReadFile = previousReadFile
		workspacesyncCreateTemp = previousCreateTemp
		workspacesyncRemove = previousRemove
		workspacesyncRename = previousRename
		workspacesyncChmod = previousChmod
		workspacesyncWriteFile = previousWriteFile
	})
}

type workspacesyncServiceTempFile struct {
	name     string
	writeErr error
	closeErr error
}

func (f *workspacesyncServiceTempFile) Name() string {
	return f.name
}

func (f *workspacesyncServiceTempFile) Write([]byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return 0, nil
}

func (f *workspacesyncServiceTempFile) Close() error {
	return f.closeErr
}

type workspacesyncServiceDirEntry struct {
	name    string
	dir     bool
	infoErr error
}

func (e workspacesyncServiceDirEntry) Name() string {
	return e.name
}

func (e workspacesyncServiceDirEntry) IsDir() bool {
	return e.dir
}

func (e workspacesyncServiceDirEntry) Type() fs.FileMode {
	if e.dir {
		return fs.ModeDir
	}
	return 0
}

func (e workspacesyncServiceDirEntry) Info() (fs.FileInfo, error) {
	if e.infoErr != nil {
		return nil, e.infoErr
	}
	return workspacesyncServiceFileInfo{name: e.name, mode: 0o644}, nil
}

type workspacesyncServiceFileInfo struct {
	name string
	mode fs.FileMode
}

func (i workspacesyncServiceFileInfo) Name() string       { return i.name }
func (i workspacesyncServiceFileInfo) Size() int64        { return 0 }
func (i workspacesyncServiceFileInfo) Mode() fs.FileMode  { return i.mode }
func (i workspacesyncServiceFileInfo) ModTime() time.Time { return time.Time{} }
func (i workspacesyncServiceFileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i workspacesyncServiceFileInfo) Sys() any           { return nil }

func workspaceSyncServiceCommit(hash string, paths ...string) *gitrevisions.Commit {
	GinkgoHelper()
	commit := workspaceSyncServiceCommitValue(hash, paths...)
	return &commit
}

func workspaceSyncServiceCommitValue(hash string, paths ...string) gitrevisions.Commit {
	GinkgoHelper()
	return gitrevisions.Commit{
		Hash:                 CommitHashFromString(hash),
		BatchID:              "batch-" + hash,
		Message:              "commit " + hash,
		AuthorID:             gitrevisions.ParseActorID("author-" + hash),
		AuthorName:           "Author " + hash,
		AuthorEmail:          hash + "@example.test",
		CreatedAt:            time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC),
		Reason:               ReasonExplicit,
		Source:               SourceFilesystem,
		ChangedMarkdownCount: len(paths),
		ChangedMarkdownPaths: append([]string(nil), paths...),
		Created:              true,
	}
}
