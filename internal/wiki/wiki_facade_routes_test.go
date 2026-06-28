package wiki

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corebranding "github.com/perber/wiki/internal/branding"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	wikipresence "github.com/perber/wiki/internal/wiki/presence"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = ginkgo.Describe("wiki facade coverage", func() {
	ginkgo.It("workspace sync page and restore facade methods fail clearly when sync is disabled", func() {
		w := &Wiki{}
		ctx := context.Background()
		page := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("page-1")}}

		status := w.WorkspaceSyncStatus()
		Expect(status.Enabled).To(BeFalse())

		status, err := w.WorkspaceSyncRefresh(ctx, workspacesync.SyncRequest{})
		Expect(err).To(MatchError(ContainSubstring("workspace sync is not enabled")))
		Expect(status.Enabled).To(BeFalse())

		snapshotList, err := w.WorkspaceSyncSnapshots(ctx, 10)
		Expect(err).To(MatchError(ContainSubstring("workspace sync is not enabled")))
		Expect(snapshotList).To(BeNil())

		snapshots, err := w.WorkspaceSyncSnapshotPage(ctx, "", 10)
		Expect(err).To(MatchError(ContainSubstring("workspace sync is not enabled")))
		Expect(snapshots).To(Equal(workspacesync.SnapshotList{}))

		status, err = w.WorkspaceSyncRestoreWorkspace(ctx, "abc123", workspacesync.Actor{ID: "user-1"}, workspacesync.SourceWeb)
		Expect(err).To(MatchError(ContainSubstring("workspace sync is not enabled")))
		Expect(status.Enabled).To(BeFalse())

		revisions, err := w.WorkspaceSyncPageRevisions(ctx, page, "", 5)
		Expect(err).To(MatchError(ContainSubstring("workspace sync is not enabled")))
		Expect(revisions).To(Equal(workspacesync.PageRevisionList{}))

		revisionSnapshot, err := w.WorkspaceSyncPageRevision(ctx, page, revision.NewRevisionIDUnchecked("rev-1"))
		Expect(err).To(MatchError(ContainSubstring("workspace sync is not enabled")))
		Expect(revisionSnapshot).To(BeNil())

		restored, err := w.WorkspaceSyncRestorePageRevision(ctx, page, revision.NewRevisionIDUnchecked("rev-1"), workspacesync.Actor{ID: "user-1"}, workspacesync.SourceWeb)
		Expect(err).To(MatchError(ContainSubstring("workspace sync is not enabled")))
		Expect(restored).To(BeNil())
	})

	ginkgo.It("workspace sync facade methods delegate to the configured sync service", func() {
		t := ginkgo.GinkgoT()
		treeService := tree.NewTreeService(t.TempDir())
		Expect(treeService.LoadTree()).To(Succeed())
		kind := tree.NodeKindPage
		pageID, err := treeService.CreateNode(tree.UserIDFromString("author"), nil, "Synced", tree.SlugFromString("synced"), &kind)
		Expect(err).NotTo(HaveOccurred())
		page, err := treeService.GetPage(*pageID)
		Expect(err).NotTo(HaveOccurred())

		snapshot := workspacesync.Snapshot{ID: "commit-1", Message: "snapshot"}
		revisionSnapshot := &revision.RevisionSnapshot{Revision: &revision.Revision{ID: revision.NewRevisionIDUnchecked("rev-1")}}
		fake := &fakeWorkspaceSyncFacade{
			status:           workspacesync.SyncStatus{Enabled: true, LastCommitHash: "commit-1"},
			refreshStatus:    workspacesync.SyncStatus{Enabled: true, LastCommitHash: "commit-2"},
			snapshots:        []workspacesync.Snapshot{snapshot},
			snapshotPage:     workspacesync.SnapshotList{Snapshots: []workspacesync.Snapshot{snapshot}, NextCursor: "next"},
			restoreStatus:    workspacesync.SyncStatus{Enabled: true, LastCommitHash: "restore-1"},
			pageRevisions:    workspacesync.PageRevisionList{Revisions: []*revision.Revision{{ID: revision.NewRevisionIDUnchecked("rev-1")}}, NextCursor: "next-rev"},
			revisionSnapshot: revisionSnapshot,
		}
		w := &Wiki{tree: treeService, workspaceSync: fake}
		ctx := context.Background()

		Expect(w.WorkspaceSyncStatus()).To(Equal(fake.status))
		Expect(w.WorkspaceSyncRefresh(ctx, workspacesync.SyncRequest{Reason: workspacesync.ReasonExplicit})).To(Equal(fake.refreshStatus))
		Expect(w.WorkspaceSyncSnapshots(ctx, 5)).To(Equal(fake.snapshots))
		Expect(w.WorkspaceSyncSnapshotPage(ctx, "cursor", 6)).To(Equal(fake.snapshotPage))
		Expect(w.WorkspaceSyncRestoreWorkspace(ctx, "restore-target", workspacesync.Actor{ID: "actor"}, workspacesync.SourceMCP)).To(Equal(fake.restoreStatus))
		Expect(w.WorkspaceSyncPageRevisions(ctx, page, "rev-cursor", 7)).To(Equal(fake.pageRevisions))
		Expect(w.WorkspaceSyncPageRevision(ctx, page, revision.NewRevisionIDUnchecked("rev-1"))).To(Equal(revisionSnapshot))

		restored, err := w.WorkspaceSyncRestorePageRevision(ctx, page, revision.NewRevisionIDUnchecked("rev-2"), workspacesync.Actor{ID: "actor"}, workspacesync.SourceWeb)
		Expect(err).NotTo(HaveOccurred())
		Expect(restored.ID).To(Equal(page.ID))
		Expect(fake.restoredDocumentCommit).To(Equal(workspacesync.CommitHashFromRevisionID(revision.NewRevisionIDUnchecked("rev-2"))))
	})

	ginkgo.It("workspace sync restore page revision returns sync errors before reading the restored page", func() {
		expected := errors.New("restore failed")
		w := &Wiki{workspaceSync: &fakeWorkspaceSyncFacade{restoreDocumentErr: expected}}

		restored, err := w.WorkspaceSyncRestorePageRevision(
			context.Background(),
			&tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("page")}},
			revision.NewRevisionIDUnchecked("rev-1"),
			workspacesync.Actor{ID: "actor"},
			workspacesync.SourceWeb,
		)
		Expect(err).To(MatchError(expected))
		Expect(restored).To(BeNil())
	})

	ginkgo.It("workspace sync lifecycle helpers handle nil services and watcher failures", func() {
		(&Wiki{}).configureWorkspaceSyncRebuilder()
		(&Wiki{}).startWorkspaceSyncWatcher()

		fake := &fakeWorkspaceSyncFacade{startErr: errors.New("watcher failed")}
		w := &Wiki{
			workspaceSync: fake,
			log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		}

		w.configureWorkspaceSyncRebuilder()
		Expect(fake.afterSync).NotTo(BeNil())

		w.startWorkspaceSyncWatcher()
		Expect(fake.startCalled).To(BeTrue())
		Expect(w.workspaceSyncCancel).NotTo(BeNil())

		Expect(w.Close()).To(Succeed())
		Expect(fake.stopCalled).To(BeTrue())
	})

	ginkgo.It("route registrar accessors expose the expected route groups", func() {
		t := ginkgo.GinkgoT()
		w := createWikiTestInstance(t)
		defer closeWithErrorCheckForTest(t, w.Close)

		Expect(w.Registrars()).To(HaveLen(15))
		Expect(w.FrontdRegistrars()).To(HaveLen(4))
		Expect(w.WorkspacedRegistrars()).To(HaveLen(10))
	})

	ginkgo.It("MCP handlers and presence accessors are available through the facade", func() {
		t := ginkgo.GinkgoT()
		w := createWikiTestInstance(t)
		defer closeWithErrorCheckForTest(t, w.Close)

		Expect(w.MCPHTTPHandler(httpinternal.RouterOptions{})).NotTo(BeNil())
		Expect(w.PrivateMCPHTTPHandler(httpinternal.RouterOptions{})).NotTo(BeNil())
		Expect(w.ActorContextMCPHTTPHandler(httpinternal.RouterOptions{})).NotTo(BeNil())

		cfg := (&Wiki{storageDir: "storage"}).FrontendConfig()
		Expect(cfg.StorageDir).To(Equal("storage"))
		Expect(cfg.GetSiteName()).To(BeEmpty())
		Expect(cfg.GetFaviconFile()).To(BeEmpty())

		cfg = w.FrontendConfig()
		Expect(cfg.StorageDir).To(Equal(w.storageDir))
		Expect(cfg.GetSiteName()).To(Equal("LeafWiki"))
		Expect(cfg.GetFaviconFile()).To(BeEmpty())

		(&Wiki{}).SetRuntimeRoleHealth(nil, nil)
		webSessions, err := (&Wiki{}).WebPresenceSessions(&coreauth.User{ID: "viewer"})
		Expect(err).To(MatchError(ContainSubstring("web presence is unavailable")))
		Expect(webSessions).To(BeNil())

		w.webPresence = wikipresence.NewWebPresenceRegistry(time.Minute, nil)
		webSessions, err = w.WebPresenceSessions(&coreauth.User{ID: "viewer"})
		Expect(err).NotTo(HaveOccurred())
		Expect(webSessions).To(BeEmpty())

		agentSessions, err := (&Wiki{}).AgentPresenceSessions()
		Expect(err).To(MatchError(ContainSubstring("agent presence is unavailable")))
		Expect(agentSessions).To(BeNil())

		w.SetAgentPresenceRegistry(projectdaemon.NewAgentPresenceRegistry(time.Minute, nil))
		agentSessions, err = w.AgentPresenceSessions()
		Expect(err).NotTo(HaveOccurred())
		Expect(agentSessions).To(BeEmpty())
	})

	ginkgo.It("frontend and importer route helpers keep working when optional setup fails", func() {
		expectedBrandingErr := errors.New("branding unavailable")
		originalFrontendBrandingConfig := frontendBrandingConfig
		frontendBrandingConfig = func(*corebranding.BrandingService) (*corebranding.BrandingConfigResponse, error) {
			return nil, expectedBrandingErr
		}
		ginkgo.DeferCleanup(func() {
			frontendBrandingConfig = originalFrontendBrandingConfig
		})

		cfg := (&Wiki{branding: &corebranding.BrandingService{}}).FrontendConfig()
		Expect(cfg.GetSiteName()).To(BeEmpty())
		Expect(cfg.GetFaviconFile()).To(BeEmpty())

		frontendBrandingConfig = func(*corebranding.BrandingService) (*corebranding.BrandingConfigResponse, error) {
			return nil, nil
		}
		Expect(cfg.GetSiteName()).To(BeEmpty())
		Expect(cfg.GetFaviconFile()).To(BeEmpty())

		expectedMkdirErr := errors.New("mkdir unavailable")
		originalCreateImporterStateDir := createImporterStateDir
		var requestedPath string
		createImporterStateDir = func(path string, _ os.FileMode) error {
			requestedPath = path
			return expectedMkdirErr
		}
		ginkgo.DeferCleanup(func() {
			createImporterStateDir = originalCreateImporterStateDir
		})

		t := ginkgo.GinkgoT()
		w := &Wiki{
			storageDir: t.TempDir(),
			log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		}

		Expect(w.buildImporterRoutes(&WikiOptions{})).NotTo(BeNil())
		Expect(requestedPath).To(Equal(filepath.Join(w.storageDir, ".importer")))
	})

	ginkgo.It("workspace sync actor lookup preserves IDs and enriches known users", func() {
		t := ginkgo.GinkgoT()
		w := createWikiTestInstance(t)
		defer closeWithErrorCheckForTest(t, w.Close)

		actor := (&Wiki{}).workspaceSyncActorForUser(tree.UserIDFromString("external-user"))
		Expect(actor.ID.String()).To(Equal("external-user"))
		Expect(actor.Name).To(BeEmpty())
		Expect(actor.Email).To(BeEmpty())

		actor = w.workspaceSyncActorForUser(tree.UserIDFromString(""))
		Expect(actor.ID.String()).To(BeEmpty())
		Expect(actor.Name).To(BeEmpty())
		Expect(actor.Email).To(BeEmpty())

		actor = w.workspaceSyncActorForUser(tree.UserIDFromString("missing-user"))
		Expect(actor.ID.String()).To(Equal("missing-user"))
		Expect(actor.Name).To(BeEmpty())
		Expect(actor.Email).To(BeEmpty())

		user, err := w.user.CreateUser("syncactor", "syncactor@example.com", "password123", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())

		actor = w.workspaceSyncActorForUser(tree.UserIDFromString(user.ID))
		Expect(actor.ID.String()).To(Equal(user.ID))
		Expect(actor.Name).To(Equal("syncactor"))
		Expect(actor.Email).To(Equal("syncactor@example.com"))
	})

	ginkgo.It("import adapter delegates tree, page, and asset operations", func() {
		t := ginkgo.GinkgoT()
		w := createWikiTestInstance(t)
		defer closeWithErrorCheckForTest(t, w.Close)
		adapter := NewWikiImportAdapter(w)

		Expect(adapter.TreeHash()).NotTo(BeEmpty())

		rootLookup, err := adapter.LookupPagePath("")
		Expect(err).NotTo(HaveOccurred())
		Expect(rootLookup.Path).To(BeEmpty())
		Expect(rootLookup.Exists).To(BeFalse())

		sectionRootLookup, err := adapter.LookupPagePathForKind("", tree.NodeKindSection)
		Expect(err).NotTo(HaveOccurred())
		Expect(sectionRootLookup.Path).To(BeEmpty())

		_, err = adapter.LookupPagePath("../escape")
		Expect(err).To(HaveOccurred())
		_, err = adapter.LookupPagePathForKind("../escape", tree.NodeKindPage)
		Expect(err).To(HaveOccurred())

		pageKind := tree.NodeKindPage
		_, err = adapter.EnsurePath(tree.UserIDFromString("importer"), "../escape", "Escaped", &pageKind)
		Expect(err).To(HaveOccurred())

		_, err = adapter.EnsurePath(tree.UserIDFromString("importer"), "docs/untitled", " ", &pageKind)
		Expect(err).To(HaveOccurred())

		page, err := adapter.EnsurePath(tree.UserIDFromString("importer"), "docs/imported", "Imported", &pageKind)
		Expect(err).NotTo(HaveOccurred())
		Expect(page.CalculatePath()).To(Equal("/docs/imported"))

		lookup, err := adapter.LookupPagePath("docs/imported")
		Expect(err).NotTo(HaveOccurred())
		Expect(lookup.Exists).To(BeTrue())

		lookup, err = adapter.LookupPagePathForKind("docs/imported", tree.NodeKindPage)
		Expect(err).NotTo(HaveOccurred())
		Expect(lookup.Exists).To(BeTrue())

		found, err := adapter.FindByPath("docs/imported")
		Expect(err).NotTo(HaveOccurred())
		Expect(found.ID).To(Equal(page.ID))

		_, err = adapter.FindByPath(" ")
		Expect(err).To(HaveOccurred())

		_, err = adapter.UpdatePage(tree.UserIDFromString("importer"), tree.PageIDFromString("missing"), "Missing", tree.SlugFromString("missing"), nil, &pageKind)
		Expect(err).To(HaveOccurred())

		_, err = adapter.UpdatePage(tree.UserIDFromString("importer"), page.ID, "", tree.SlugFromString("imported"), nil, &pageKind)
		Expect(err).To(HaveOccurred())

		body := "Imported body"
		updated, err := adapter.UpdatePage(tree.UserIDFromString("importer"), page.ID, "Imported", tree.SlugFromString("imported"), &body, &pageKind)
		Expect(err).NotTo(HaveOccurred())
		Expect(updated.Content).To(Equal(body))

		_, err = adapter.ListAssets(tree.PageIDFromString("missing"))
		Expect(err).To(HaveOccurred())

		assetPath := filepath.Join(t.TempDir(), "asset.txt")
		Expect(os.WriteFile(assetPath, []byte("asset"), 0o644)).To(Succeed())
		file, err := os.Open(assetPath)
		Expect(err).NotTo(HaveOccurred())
		defer file.Close()

		url, err := adapter.UploadAsset(tree.UserIDFromString("importer"), page.ID, file, tree.AssetNameFromString("asset.txt"), shared.MaxBytes(1024))
		Expect(err).NotTo(HaveOccurred())
		Expect(url).To(ContainSubstring("asset.txt"))

		assets, err := adapter.ListAssets(page.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(assets).To(ContainElement(ContainSubstring("asset.txt")))

		_, err = adapter.UploadAsset(tree.UserIDFromString("importer"), tree.PageIDFromString("missing"), file, tree.AssetNameFromString("missing.txt"), shared.MaxBytes(1024))
		Expect(err).To(HaveOccurred())
	})
})

type fakeWorkspaceSyncFacade struct {
	status                 workspacesync.SyncStatus
	refreshStatus          workspacesync.SyncStatus
	snapshots              []workspacesync.Snapshot
	snapshotPage           workspacesync.SnapshotList
	restoreStatus          workspacesync.SyncStatus
	pageRevisions          workspacesync.PageRevisionList
	revisionSnapshot       *revision.RevisionSnapshot
	syncErr                error
	restoreDocumentErr     error
	restoredDocumentCommit workspacesync.CommitHash
	afterSync              func() error
	startErr               error
	startCalled            bool
	stopCalled             bool
}

func (f *fakeWorkspaceSyncFacade) Status() workspacesync.SyncStatus {
	return f.status
}

func (f *fakeWorkspaceSyncFacade) SyncNow(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
	return f.refreshStatus, f.syncErr
}

func (f *fakeWorkspaceSyncFacade) ListSnapshots(context.Context, workspacesync.SnapshotLimit) ([]workspacesync.Snapshot, error) {
	return f.snapshots, nil
}

func (f *fakeWorkspaceSyncFacade) ListSnapshotPage(context.Context, workspacesync.CommitHash, workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
	return f.snapshotPage, nil
}

func (f *fakeWorkspaceSyncFacade) RestoreWorkspaceWithSource(context.Context, workspacesync.CommitHash, workspacesync.Actor, workspacesync.Source) (workspacesync.SyncStatus, error) {
	return f.restoreStatus, nil
}

func (f *fakeWorkspaceSyncFacade) ListPageRevisions(context.Context, *tree.Page, string, workspacesync.PageRevisionLimit) (workspacesync.PageRevisionList, error) {
	return f.pageRevisions, nil
}

func (f *fakeWorkspaceSyncFacade) GetPageRevisionSnapshot(context.Context, *tree.Page, workspacesync.CommitHash) (*revision.RevisionSnapshot, error) {
	return f.revisionSnapshot, nil
}

func (f *fakeWorkspaceSyncFacade) RestoreDocumentWithSource(_ context.Context, _ *tree.Page, commitID workspacesync.CommitHash, _ workspacesync.Actor, _ workspacesync.Source) (workspacesync.SyncStatus, error) {
	f.restoredDocumentCommit = commitID
	return f.restoreStatus, f.restoreDocumentErr
}

func (f *fakeWorkspaceSyncFacade) SetAfterSync(fn func() error) {
	f.afterSync = fn
}

func (f *fakeWorkspaceSyncFacade) StartWatcher(context.Context) error {
	f.startCalled = true
	return f.startErr
}

func (f *fakeWorkspaceSyncFacade) StopWatcher() {
	f.stopCalled = true
}
