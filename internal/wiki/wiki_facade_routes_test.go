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
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	corebranding "github.com/perber/wiki/internal/branding"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/shared"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	wikipresence "github.com/perber/wiki/internal/wiki/presence"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = ginkgo.Describe("wiki facade route and service behavior", func() {
	ginkgo.It("workspace sync page and restore facade methods fail clearly when sync is disabled", ginkgo.Label("unit"), func() {
		w := &Wiki{}
		ctx := context.Background()
		page := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("page-1")}}

		status := w.WorkspaceSyncStatus()
		Expect(status).To(haveWorkspaceSyncMode(workspaceSyncModeDisabled))

		status, err := w.WorkspaceSyncRefresh(ctx, workspacesync.SyncRequest{})
		Expect(err).To(matchWorkspaceSyncDisabled())
		Expect(status).To(haveWorkspaceSyncMode(workspaceSyncModeDisabled))

		snapshotList, err := w.WorkspaceSyncSnapshots(ctx, 10)
		Expect(err).To(matchWorkspaceSyncDisabled())
		Expect(snapshotList).To(BeNil())

		snapshots, err := w.WorkspaceSyncSnapshotPage(ctx, newFixtureCommitHash(""), 10)
		Expect(err).To(matchWorkspaceSyncDisabled())
		Expect(snapshots).To(Equal(workspacesync.SnapshotList{}))

		status, err = w.WorkspaceSyncRestoreWorkspace(ctx, newFixtureCommitHash("abc123"), workspacesync.Actor{ID: workspacesync.ActorIDFromUserID(newFixtureUserID("user-1"))}, workspacesync.SourceWeb)
		Expect(err).To(matchWorkspaceSyncDisabled())
		Expect(status).To(haveWorkspaceSyncMode(workspaceSyncModeDisabled))

		revisions, err := w.WorkspaceSyncPageRevisions(ctx, page, "", 5)
		Expect(err).To(matchWorkspaceSyncDisabled())
		Expect(revisions).To(Equal(workspacesync.PageRevisionList{}))

		revisionSnapshot, err := w.WorkspaceSyncPageRevision(ctx, page, newFixtureRevisionID("rev-1"))
		Expect(err).To(matchWorkspaceSyncDisabled())
		Expect(revisionSnapshot).To(BeNil())

		restored, err := w.WorkspaceSyncRestorePageRevision(ctx, page, newFixtureRevisionID("rev-1"), workspacesync.Actor{ID: workspacesync.ActorIDFromUserID(newFixtureUserID("user-1"))}, workspacesync.SourceWeb)
		Expect(err).To(matchWorkspaceSyncDisabled())
		Expect(restored).To(BeNil())
	})

	ginkgo.It("workspace sync facade methods delegate to the configured sync service", ginkgo.Label("unit"), func() {
		treeService := tree.NewTreeService(wikiTestTempDir())
		Expect(treeService.LoadTree()).To(Succeed())
		kind := tree.NodeKindPage
		pageID, err := treeService.CreateNode(newFixtureUserID("author"), nil, "Synced", newFixtureSlug("synced"), &kind)
		Expect(err).NotTo(HaveOccurred())
		page, err := treeService.GetPage(*pageID)
		Expect(err).NotTo(HaveOccurred())

		snapshot := workspacesync.Snapshot{ID: newFixtureCommitHash("commit-1"), Message: "snapshot"}
		revisionSnapshot := &revision.RevisionSnapshot{Revision: &revision.Revision{ID: newFixtureRevisionID("rev-1")}}
		fake := &fakeWorkspaceSyncFacade{
			status:           workspacesync.SyncStatus{Enabled: true, LastCommitHash: newFixtureCommitHash("commit-1")},
			refreshStatus:    workspacesync.SyncStatus{Enabled: true, LastCommitHash: newFixtureCommitHash("commit-2")},
			snapshots:        []workspacesync.Snapshot{snapshot},
			snapshotPage:     workspacesync.SnapshotList{Snapshots: []workspacesync.Snapshot{snapshot}, NextCursor: newFixtureCommitHash("next")},
			restoreStatus:    workspacesync.SyncStatus{Enabled: true, LastCommitHash: newFixtureCommitHash("restore-1")},
			pageRevisions:    workspacesync.PageRevisionList{Revisions: []*revision.Revision{{ID: newFixtureRevisionID("rev-1")}}, NextCursor: "next-rev"},
			revisionSnapshot: revisionSnapshot,
		}
		w := &Wiki{tree: treeService, workspaceSync: fake}
		ctx := context.Background()

		Expect(w.WorkspaceSyncStatus()).To(Equal(fake.status))
		Expect(w.WorkspaceSyncRefresh(ctx, workspacesync.SyncRequest{Reason: workspacesync.ReasonExplicit})).To(Equal(fake.refreshStatus))
		Expect(w.WorkspaceSyncSnapshots(ctx, 5)).To(Equal(fake.snapshots))
		Expect(w.WorkspaceSyncSnapshotPage(ctx, newFixtureCommitHash("cursor"), 6)).To(Equal(fake.snapshotPage))
		Expect(w.WorkspaceSyncRestoreWorkspace(ctx, newFixtureCommitHash("restore-target"), workspacesync.Actor{ID: workspacesync.ActorIDFromUserID(newFixtureUserID("actor"))}, workspacesync.SourceMCP)).To(Equal(fake.restoreStatus))
		Expect(w.WorkspaceSyncPageRevisions(ctx, page, "rev-cursor", 7)).To(Equal(fake.pageRevisions))
		Expect(w.WorkspaceSyncPageRevision(ctx, page, newFixtureRevisionID("rev-1"))).To(Equal(revisionSnapshot))

		restored, err := w.WorkspaceSyncRestorePageRevision(ctx, page, newFixtureRevisionID("rev-2"), workspacesync.Actor{ID: workspacesync.ActorIDFromUserID(newFixtureUserID("actor"))}, workspacesync.SourceWeb)
		Expect(err).NotTo(HaveOccurred())
		Expect(restored.ID).To(Equal(page.ID))
		Expect(fake.restoredDocumentCommit).To(Equal(workspacesync.CommitHashFromRevisionID(newFixtureRevisionID("rev-2"))))
	})

	ginkgo.It("workspace sync restore page revision returns sync errors before reading the restored page", ginkgo.Label("unit"), func() {
		expected := errors.New("restore failed")
		w := &Wiki{workspaceSync: &fakeWorkspaceSyncFacade{restoreDocumentErr: expected}}

		restored, err := w.WorkspaceSyncRestorePageRevision(
			context.Background(),
			&tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("page")}},
			newFixtureRevisionID("rev-1"),
			workspacesync.Actor{ID: workspacesync.ActorIDFromUserID(newFixtureUserID("actor"))},
			workspacesync.SourceWeb,
		)
		Expect(err).To(MatchError(expected))
		Expect(restored).To(BeNil())
	})

	ginkgo.It("workspace sync lifecycle helpers handle nil services and watcher failures", ginkgo.Label("unit"), func() {
		(&Wiki{}).configureWorkspaceSyncRebuilder()
		(&Wiki{}).startWorkspaceSyncWatcher()

		fake := &fakeWorkspaceSyncFacade{startErr: errors.New("watcher failed")}
		w := &Wiki{
			workspaceSync: fake,
			log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		}

		w.configureWorkspaceSyncRebuilder()
		w.startWorkspaceSyncWatcher()
		Expect(w.workspaceSyncCancel).NotTo(BeNil())

		Expect(w.Close()).To(Succeed())
		Expect(fake).To(haveWorkspaceSyncLifecycleObserved())
	})

	ginkgo.It("route registrar accessors expose the expected route groups", ginkgo.Label("integration"), func() {
		w := createWikiTestInstance()
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)

		Expect(w.Registrars()).To(HaveLen(15))
		Expect(w.FrontdRegistrars()).To(HaveLen(4))
		Expect(w.WorkspacedRegistrars()).To(HaveLen(10))
	})

	ginkgo.It("MCP handlers and presence accessors are available through the facade", ginkgo.Label("integration"), func() {
		w := createWikiTestInstance()
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)

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
		webSessions, err := (&Wiki{}).WebPresenceSessions(&coreauth.User{ID: newFixtureUserID("viewer")})
		expectWebPresenceUnavailable(err)
		Expect(webSessions).To(BeNil())

		w.webPresence = wikipresence.NewWebPresenceRegistry(time.Minute, nil)
		webSessions, err = w.WebPresenceSessions(&coreauth.User{ID: newFixtureUserID("viewer")})
		Expect(err).NotTo(HaveOccurred())
		Expect(webSessions).To(BeEmpty())

		agentSessions, err := (&Wiki{}).AgentPresenceSessions()
		expectAgentPresenceUnavailable(err)
		Expect(agentSessions).To(BeNil())

		w.SetAgentPresenceRegistry(projectdaemon.NewAgentPresenceRegistry(time.Minute, nil))
		agentSessions, err = w.AgentPresenceSessions()
		Expect(err).NotTo(HaveOccurred())
		Expect(agentSessions).To(BeEmpty())
	})

	ginkgo.It("frontend and importer route helpers keep working when optional setup fails", ginkgo.Label("unit"), func() {
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
			var missingBrandingConfig *corebranding.BrandingConfigResponse
			return missingBrandingConfig, nil
		}
		cfg = (&Wiki{branding: &corebranding.BrandingService{}}).FrontendConfig()
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
		w := &Wiki{
			storageDir: wikiTestTempDir(),
			log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		}

		Expect(w.buildImporterRoutes(&WikiOptions{})).NotTo(BeNil())
		Expect(requestedPath).To(Equal(filepath.Join(w.storageDir, ".importer")))
	})

	ginkgo.It("workspace sync actor lookup preserves IDs and enriches known users", ginkgo.Label("integration"), func() {
		w := createWikiTestInstance()
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)

		externalUserID := newFixtureUserID("external-user")
		actor := (&Wiki{}).workspaceSyncActorForUser(externalUserID)
		Expect(actor).To(haveWorkspaceSyncActor(workspacesync.ActorIDFromUserID(externalUserID), BeEmpty(), BeEmpty()))

		actor = w.workspaceSyncActorForUser(newFixtureUserID(""))
		Expect(actor).To(haveWorkspaceSyncActor(workspacesync.ActorIDFromUserID(newFixtureUserID("")), BeEmpty(), BeEmpty()))

		missingUserID := newFixtureUserID("missing-user")
		actor = w.workspaceSyncActorForUser(missingUserID)
		Expect(actor).To(haveWorkspaceSyncActor(workspacesync.ActorIDFromUserID(missingUserID), BeEmpty(), BeEmpty()))

		user, err := w.user.CreateUser("syncactor", "syncactor@example.com", "password123", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())

		knownUserID := tree.UserIDFromString(user.ID)
		actor = w.workspaceSyncActorForUser(knownUserID)
		Expect(actor).To(haveWorkspaceSyncActor(workspacesync.ActorIDFromUserID(knownUserID), Equal("syncactor"), Equal("syncactor@example.com")))
	})

	ginkgo.It("import adapter delegates tree, page, and asset operations", ginkgo.Label("integration"), func() {
		w := createWikiTestInstance()
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)
		adapter := NewWikiImportAdapter(w)

		Expect(adapter.TreeHash()).NotTo(BeEmpty())

		rootLookup, err := adapter.LookupPagePath(newFixtureRoutePath(""))
		Expect(err).NotTo(HaveOccurred())
		Expect(rootLookup).To(haveMissingWikiPathLookup(newFixtureRoutePath("")))

		sectionRootLookup, err := adapter.LookupPagePathForKind(newFixtureRoutePath(""), tree.NodeKindSection)
		Expect(err).NotTo(HaveOccurred())
		Expect(sectionRootLookup.Path).To(BeEmpty())

		_, err = adapter.LookupPagePath(newFixtureRoutePath("../escape"))
		Expect(err).To(MatchError(tree.ErrInvalidRoutePath))
		_, err = adapter.LookupPagePathForKind(newFixtureRoutePath("../escape"), tree.NodeKindPage)
		Expect(err).To(MatchError(tree.ErrInvalidRoutePath))

		pageKind := tree.NodeKindPage
		_, err = adapter.EnsurePath(newFixtureUserID("importer"), newFixtureRoutePath("../escape"), "Escaped", &pageKind)
		Expect(err).To(MatchError(tree.ErrInvalidRoutePath))

		_, err = adapter.EnsurePath(newFixtureUserID("importer"), newFixtureRoutePath("docs/untitled"), " ", &pageKind)
		Expect(err).To(havePageValidationFieldError("title", wikipages.FieldCodePageTitleRequired, wikipages.MessageIDPageTitleRequired))

		page, err := adapter.EnsurePath(newFixtureUserID("importer"), newFixtureRoutePath("docs/imported"), "Imported", &pageKind)
		Expect(err).NotTo(HaveOccurred())
		Expect(page.CalculatePath()).To(Equal("/docs/imported"))

		lookup, err := adapter.LookupPagePath(newFixtureRoutePath("docs/imported"))
		Expect(err).NotTo(HaveOccurred())
		Expect(lookup).To(haveExistingWikiPathLookup(newFixtureRoutePath("docs/imported")))

		lookup, err = adapter.LookupPagePathForKind(newFixtureRoutePath("docs/imported"), tree.NodeKindPage)
		Expect(err).NotTo(HaveOccurred())
		Expect(lookup).To(haveExistingWikiPathLookup(newFixtureRoutePath("docs/imported")))

		found, err := adapter.FindByPath("docs/imported")
		Expect(err).NotTo(HaveOccurred())
		Expect(found.ID).To(Equal(page.ID))

		_, err = adapter.FindByPath(" ")
		Expect(err).To(havePageValidationFieldError("path", wikipages.FieldCodePagePathRequired, wikipages.MessageIDPagePathRequired))

		_, err = adapter.UpdatePage(newFixtureUserID("importer"), newFixturePageID("missing"), "Missing", newFixtureSlug("missing"), nil, &pageKind)
		Expect(err).To(MatchError(tree.ErrPageNotFound))

		_, err = adapter.UpdatePage(newFixtureUserID("importer"), page.ID, "", newFixtureSlug("imported"), nil, &pageKind)
		Expect(err).To(havePageValidationFieldError("title", wikipages.FieldCodePageTitleRequired, wikipages.MessageIDPageTitleRequired))

		body := "Imported body"
		updated, err := adapter.UpdatePage(newFixtureUserID("importer"), page.ID, "Imported", newFixtureSlug("imported"), &body, &pageKind)
		Expect(err).NotTo(HaveOccurred())
		Expect(updated.Content).To(Equal(body))

		_, err = adapter.ListAssets(newFixturePageID("missing"))
		Expect(err).To(MatchError(tree.ErrPageNotFound))

		assetPath := filepath.Join(wikiTestTempDir(), "asset.txt")
		Expect(os.WriteFile(assetPath, []byte("asset"), 0o644)).To(Succeed())
		file, err := os.Open(assetPath)
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			Expect(file.Close()).To(Succeed())
		})

		url, err := adapter.UploadAsset(newFixtureUserID("importer"), page.ID, file, newFixtureAssetName("asset.txt"), shared.MaxBytes(1024))
		Expect(err).NotTo(HaveOccurred())
		Expect(url).To(ContainSubstring("asset.txt"))

		assets, err := adapter.ListAssets(page.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(assets).To(ContainElement(ContainSubstring("asset.txt")))

		_, err = adapter.UploadAsset(newFixtureUserID("importer"), newFixturePageID("missing"), file, newFixtureAssetName("missing.txt"), shared.MaxBytes(1024))
		Expect(err).To(MatchError(tree.ErrPageNotFound))
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

var (
	expectedWorkspaceSyncDisabledError = errors.New("workspace sync is not enabled")
	expectedWebPresenceUnavailable     = errors.New("web presence is unavailable")
	expectedAgentPresenceUnavailable   = errors.New("agent presence is unavailable")
)

type workspaceSyncMode string

const (
	workspaceSyncModeEnabled  workspaceSyncMode = "enabled"
	workspaceSyncModeDisabled workspaceSyncMode = "disabled"
)

func haveWorkspaceSyncMode(mode workspaceSyncMode) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(status workspacesync.SyncStatus) workspaceSyncMode {
		if status.Enabled {
			return workspaceSyncModeEnabled
		}
		return workspaceSyncModeDisabled
	}, Equal(mode))
}

type workspaceSyncLifecycleObservation string

const (
	workspaceSyncLifecycleMissing  workspaceSyncLifecycleObservation = "missing"
	workspaceSyncLifecycleObserved workspaceSyncLifecycleObservation = "rebuilder configured and watcher stopped"
	workspaceSyncLifecyclePartial  workspaceSyncLifecycleObservation = "partial"
)

func haveWorkspaceSyncLifecycleObserved() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(workspaceSyncLifecycleFromFacade, Equal(workspaceSyncLifecycleObserved))
}

func workspaceSyncLifecycleFromFacade(fake *fakeWorkspaceSyncFacade) workspaceSyncLifecycleObservation {
	if fake == nil {
		return workspaceSyncLifecycleMissing
	}
	if fake.afterSync != nil && fake.startCalled && fake.stopCalled {
		return workspaceSyncLifecycleObserved
	}
	return workspaceSyncLifecyclePartial
}

func haveWorkspaceSyncActor(id workspacesync.ActorID, name types.GomegaMatcher, email types.GomegaMatcher) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"ID":    Equal(id),
		"Name":  name,
		"Email": email,
	})
}

func haveMissingWikiPathLookup(path tree.RoutePath) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(lookup *tree.PathLookup) wikiPathLookupObservation {
		return wikiPathLookupFromResult(lookup)
	}, Equal(wikiPathLookupObservation{Path: path, State: wikiPathLookupMissing}))
}

func haveExistingWikiPathLookup(path tree.RoutePath) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(lookup *tree.PathLookup) wikiPathLookupObservation {
		return wikiPathLookupFromResult(lookup)
	}, Equal(wikiPathLookupObservation{Path: path, State: wikiPathLookupExisting}))
}

type wikiPathLookupState string

const (
	wikiPathLookupUnavailable wikiPathLookupState = "unavailable"
	wikiPathLookupMissing     wikiPathLookupState = "missing"
	wikiPathLookupExisting    wikiPathLookupState = "existing"
)

type wikiPathLookupObservation struct {
	Path  tree.RoutePath
	State wikiPathLookupState
}

func wikiPathLookupFromResult(lookup *tree.PathLookup) wikiPathLookupObservation {
	if lookup == nil {
		return wikiPathLookupObservation{State: wikiPathLookupUnavailable}
	}
	state := wikiPathLookupMissing
	if lookup.Exists {
		state = wikiPathLookupExisting
	}
	return wikiPathLookupObservation{
		Path:  lookup.Path,
		State: state,
	}
}

func matchWorkspaceSyncDisabled() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return MatchError(expectedWorkspaceSyncDisabledError)
}

func havePageValidationFieldError(field testmatchers.ValidationField, code sharederrors.FieldErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(err error) *sharederrors.ValidationErrors {
		var validation *sharederrors.ValidationErrors
		if !errors.As(err, &validation) {
			return nil
		}
		return validation
	}, testmatchers.ContainFieldError(field, code, messageID))
}

func expectWebPresenceUnavailable(err error) {
	ginkgo.GinkgoHelper()
	Expect(err).To(MatchError(expectedWebPresenceUnavailable))
}

func expectAgentPresenceUnavailable(err error) {
	ginkgo.GinkgoHelper()
	Expect(err).To(MatchError(expectedAgentPresenceUnavailable))
}
