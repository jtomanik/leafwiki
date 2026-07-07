package wiki

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	coreassets "github.com/perber/wiki/internal/core/assets"
	"github.com/perber/wiki/internal/core/revision"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/test_utils/matchers"
	wikiassets "github.com/perber/wiki/internal/wiki/assets"
	wikiauth "github.com/perber/wiki/internal/wiki/auth"
	wikibranding "github.com/perber/wiki/internal/wiki/branding"
	wikihealth "github.com/perber/wiki/internal/wiki/health"
	wikiimporter "github.com/perber/wiki/internal/wiki/importer"
	wikilinks "github.com/perber/wiki/internal/wiki/links"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
	wikioauth "github.com/perber/wiki/internal/wiki/oauth"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	wikipresence "github.com/perber/wiki/internal/wiki/presence"
	wikiproperties "github.com/perber/wiki/internal/wiki/properties"
	wikirevisions "github.com/perber/wiki/internal/wiki/revisions"
	wikisearch "github.com/perber/wiki/internal/wiki/search"
	wikitags "github.com/perber/wiki/internal/wiki/tags"
	wikiworkspacesyncroutes "github.com/perber/wiki/internal/wiki/workspacesync"
	"github.com/perber/wiki/internal/workspacesync"
)

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

type startupServiceState string

const (
	startupServicesUnset startupServiceState = "startup services unset"
	startupServicesReady startupServiceState = "startup services ready"
)

type startupWorkspaceFacade struct {
	StorageDir string
	RootDir    string
	Workspace  Workspace
	Services   startupServiceState
}

type routeSlotState string

const (
	routeSlotsUnset routeSlotState = "route slots unset"
	routeSlotsReady routeSlotState = "route slots ready"
)

type controlPlaneRouteRegistry struct {
	Frontd    []routeRegistryDomain
	Workspace routeSlotState
	MCP       routeSlotState
}

func exposeStartupWorkspaceFacade(workspace Workspace, storageDir, rootDir string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(startupWorkspaceFacadeFromWiki, Equal(startupWorkspaceFacade{
		StorageDir: storageDir,
		RootDir:    rootDir,
		Workspace:  workspace,
		Services:   startupServicesUnset,
	}))
}

func startupWorkspaceFacadeFromWiki(w *Wiki) startupWorkspaceFacade {
	if w == nil {
		return startupWorkspaceFacade{}
	}
	services := startupServicesReady
	if w.UserService() == nil && w.AuthService() == nil && w.APIKeyService() == nil && w.OAuthService() == nil {
		services = startupServicesUnset
	}
	return startupWorkspaceFacade{
		StorageDir: w.GetStorageDir(),
		RootDir:    w.GetRootDir(),
		Workspace:  w.Workspace(),
		Services:   services,
	}
}

func newRouteAssemblyWiki() *Wiki {
	ginkgo.GinkgoHelper()

	storageDir := wikiTestTempDir()
	slugService := tree.NewSlugService()
	return &Wiki{
		tree:       tree.NewTreeService(storageDir),
		slug:       slugService,
		asset:      coreassets.NewAssetService(storageDir, slugService),
		storageDir: storageDir,
		workspace: Workspace{
			ID:      newFixtureWorkspaceID("route-assembly"),
			DataDir: storageDir,
			RootDir: filepath.Join(storageDir, "root"),
		},
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func exposeControlPlaneRouteRegistry() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(controlPlaneRouteRegistryFromWiki, Equal(controlPlaneRouteRegistry{
		Frontd:    frontdRouteRegistryDomains(),
		Workspace: routeSlotsUnset,
		MCP:       routeSlotsUnset,
	}))
}

func controlPlaneRouteRegistryFromWiki(w *Wiki) controlPlaneRouteRegistry {
	if w == nil {
		return controlPlaneRouteRegistry{}
	}
	workspace := routeSlotsReady
	if w.pagesRoutes == nil && w.assetsRoutes == nil && w.workspaceSyncRoutes == nil {
		workspace = routeSlotsUnset
	}
	mcp := routeSlotsReady
	if w.mcpRoutes == nil {
		mcp = routeSlotsUnset
	}
	return controlPlaneRouteRegistry{
		Frontd:    routeRegistryDomains(w.FrontdRegistrars()),
		Workspace: workspace,
		MCP:       mcp,
	}
}

type routeRegistryDomain string

const (
	routeDomainUnknown       routeRegistryDomain = "unknown"
	routeDomainAssets        routeRegistryDomain = "assets"
	routeDomainAuth          routeRegistryDomain = "auth"
	routeDomainBranding      routeRegistryDomain = "branding"
	routeDomainHealth        routeRegistryDomain = "health"
	routeDomainImporter      routeRegistryDomain = "importer"
	routeDomainLinks         routeRegistryDomain = "links"
	routeDomainMCP           routeRegistryDomain = "mcp"
	routeDomainOAuth         routeRegistryDomain = "oauth"
	routeDomainPages         routeRegistryDomain = "pages"
	routeDomainPresence      routeRegistryDomain = "presence"
	routeDomainProperties    routeRegistryDomain = "properties"
	routeDomainRevisions     routeRegistryDomain = "revisions"
	routeDomainSearch        routeRegistryDomain = "search"
	routeDomainTags          routeRegistryDomain = "tags"
	routeDomainWorkspaceSync routeRegistryDomain = "workspace sync"
)

func applicationRouteRegistryDomains() []routeRegistryDomain {
	return []routeRegistryDomain{
		routeDomainAuth,
		routeDomainPages,
		routeDomainAssets,
		routeDomainRevisions,
		routeDomainSearch,
		routeDomainLinks,
		routeDomainTags,
		routeDomainProperties,
		routeDomainBranding,
		routeDomainImporter,
		routeDomainHealth,
		routeDomainWorkspaceSync,
		routeDomainPresence,
		routeDomainOAuth,
		routeDomainMCP,
	}
}

func frontdRouteRegistryDomains() []routeRegistryDomain {
	return []routeRegistryDomain{
		routeDomainAuth,
		routeDomainBranding,
		routeDomainHealth,
		routeDomainOAuth,
	}
}

func workspacedRouteRegistryDomains() []routeRegistryDomain {
	return []routeRegistryDomain{
		routeDomainPages,
		routeDomainAssets,
		routeDomainRevisions,
		routeDomainSearch,
		routeDomainLinks,
		routeDomainTags,
		routeDomainProperties,
		routeDomainImporter,
		routeDomainWorkspaceSync,
		routeDomainPresence,
	}
}

func exposeRouteRegistryDomains(domains ...routeRegistryDomain) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(routeRegistryDomains, Equal(domains))
}

func routeRegistryDomains(registrars []http.RouteRegistrar) []routeRegistryDomain {
	domains := make([]routeRegistryDomain, 0, len(registrars))
	for _, registrar := range registrars {
		domains = append(domains, routeRegistryDomainFor(registrar))
	}
	return domains
}

func routeRegistryDomainFor(registrar http.RouteRegistrar) routeRegistryDomain {
	switch registrar.(type) {
	case *wikiassets.Routes:
		return routeDomainAssets
	case *wikiauth.Routes:
		return routeDomainAuth
	case *wikibranding.Routes:
		return routeDomainBranding
	case *wikihealth.Routes:
		return routeDomainHealth
	case *wikiimporter.Routes:
		return routeDomainImporter
	case *wikilinks.Routes:
		return routeDomainLinks
	case *wikimcp.Routes:
		return routeDomainMCP
	case *wikioauth.Routes:
		return routeDomainOAuth
	case *wikipages.Routes:
		return routeDomainPages
	case *wikipresence.Routes:
		return routeDomainPresence
	case *wikiproperties.Routes:
		return routeDomainProperties
	case *wikirevisions.Routes:
		return routeDomainRevisions
	case *wikisearch.Routes:
		return routeDomainSearch
	case *wikitags.Routes:
		return routeDomainTags
	case *wikiworkspacesyncroutes.Routes:
		return routeDomainWorkspaceSync
	default:
		return routeDomainUnknown
	}
}

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

func matchWebPresenceUnavailable() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return MatchError(expectedWebPresenceUnavailable)
}

func matchAgentPresenceUnavailable() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return MatchError(expectedAgentPresenceUnavailable)
}

func havePageValidationFieldError(field matchers.ValidationField, code sharederrors.FieldErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(err error) *sharederrors.ValidationErrors {
		var validation *sharederrors.ValidationErrors
		if !errors.As(err, &validation) {
			return nil
		}
		return validation
	}, matchers.ContainFieldError(field, code, messageID))
}
