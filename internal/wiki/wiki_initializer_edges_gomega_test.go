package wiki

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/branding"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/links"
	"github.com/perber/wiki/internal/properties"
	"github.com/perber/wiki/internal/search"
	"github.com/perber/wiki/internal/tags"
	wikioauth "github.com/perber/wiki/internal/wiki/oauth"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	"github.com/perber/wiki/internal/wiki/pagesave"
	"github.com/perber/wiki/internal/workspacesync"
)

var (
	errFixtureWikiStartupFailed = errors.New("startup failed")
	errFixtureWikiAuthFailed    = errors.New("auth failed")
	errFixtureWikiEdgeFailed    = errors.New("edge failed")
	errFixtureWikiCloseFailed   = errors.New("close failed")
)

var _ = ginkgo.Describe("wiki startup initialization behavior", ginkgo.Label("integration"), func() {
	ginkgo.It("returns each NewWiki startup orchestration error", func() {
		_, err := NewWiki(&WikiOptions{
			StorageDir:       wikiTestTempDir(),
			WorkspaceOnly:    true,
			ControlPlaneOnly: true,
		})
		Expect(err).To(MatchError(ErrWikiModeConflict))

		Expect(runNewWikiStartupError(func(expected error) {
			newWikiEnsureWorkspaceDirs = func(Workspace) error { return expected }
		})).To(MatchError(errFixtureWikiStartupFailed))
		Expect(runNewWikiStartupError(func(expected error) {
			installFastNewWikiStartup()
			newWikiInitAuth = func(*Wiki, *WikiOptions) error { return expected }
		})).To(MatchError(errFixtureWikiStartupFailed))
		Expect(runNewWikiStartupError(func(expected error) {
			installFastNewWikiStartup()
			newWikiInitOAuth = func(*Wiki, *WikiOptions) error { return expected }
		})).To(MatchError(errFixtureWikiStartupFailed))
		Expect(runNewWikiStartupError(func(expected error) {
			installFastNewWikiStartup()
			newWikiInitBranding = func(*Wiki) error { return expected }
		}, func(options *WikiOptions) {
			options.ControlPlaneOnly = true
		})).To(MatchError(errFixtureWikiStartupFailed))
		Expect(runNewWikiStartupError(func(expected error) {
			installFastNewWikiStartup()
			newWikiInitCoreServices = func(*Wiki, *WikiOptions) error { return expected }
		})).To(MatchError(errFixtureWikiStartupFailed))
		Expect(runNewWikiStartupError(func(expected error) {
			installFastNewWikiStartup()
			newWikiInitLinkService = func(*Wiki) error { return expected }
		})).To(MatchError(errFixtureWikiStartupFailed))
		Expect(runNewWikiStartupError(func(expected error) {
			installFastNewWikiStartup()
			newWikiInitTagsService = func(*Wiki) error { return expected }
		})).To(MatchError(errFixtureWikiStartupFailed))
		Expect(runNewWikiStartupError(func(expected error) {
			installFastNewWikiStartup()
			newWikiInitProperties = func(*Wiki) error { return expected }
		})).To(MatchError(errFixtureWikiStartupFailed))
		Expect(runNewWikiStartupError(func(expected error) {
			installFastNewWikiStartup()
			newWikiInitSearch = func(*Wiki) error { return expected }
		})).To(MatchError(errFixtureWikiStartupFailed))
		Expect(runNewWikiStartupError(func(expected error) {
			installFastNewWikiStartup()
			newWikiInitBranding = func(*Wiki) error { return expected }
		})).To(MatchError(errFixtureWikiStartupFailed))
		Expect(runNewWikiStartupError(func(expected error) {
			installFastNewWikiStartup()
			newWikiEnsureWelcomePage = func(*Wiki) error { return expected }
		})).To(MatchError(errFixtureWikiStartupFailed))
	})

	ginkgo.It("surfaces workspace directory creation failures", func() {
		expected := errors.New("mkdir failed")
		restore := restoreWikiTestSeams()
		ginkgo.DeferCleanup(restore)
		wikiMkdirAll = func(string, os.FileMode) error {
			return expected
		}
		Expect(ensureWorkspaceDirs(Workspace{DataDir: "data", RootDir: "root"})).To(MatchError(expected))

		restore()
		restore = restoreWikiTestSeams()
		ginkgo.DeferCleanup(restore)
		callCount := 0
		wikiMkdirAll = func(string, os.FileMode) error {
			callCount++
			if callCount == 2 {
				return expected
			}
			return nil
		}
		Expect(ensureWorkspaceDirs(Workspace{DataDir: "data", RootDir: "root"})).To(MatchError(expected))
	})

	ginkgo.It("surfaces auth and oauth initializer dependency failures", func() {
		Expect(runInitAuthError(func(expected error) {
			newWikiUserStore = func(string) (*coreauth.UserStore, error) { return nil, expected }
		})).To(MatchError(errFixtureWikiAuthFailed))
		Expect(runInitAuthError(func(expected error) {
			newWikiAPIKeyStore = func(string) (*coreauth.APIKeyStore, error) { return nil, expected }
		})).To(MatchError(errFixtureWikiAuthFailed))
		Expect(runInitAuthError(func(expected error) {
			wikiInitDefaultAdmin = func(*coreauth.UserService, string) error { return expected }
		})).To(MatchError(errFixtureWikiAuthFailed))
		Expect(runInitAuthError(func(expected error) {
			newWikiUserResolver = func(*coreauth.UserService) (*coreauth.UserResolver, error) { return nil, expected }
		})).To(MatchError(errFixtureWikiAuthFailed))
		Expect(runInitAuthError(func(expected error) {
			newWikiSessionStore = func(string) (*coreauth.SessionStore, error) { return nil, expected }
		})).To(MatchError(errFixtureWikiAuthFailed))

		expected := errors.New("oauth failed")
		restore := restoreWikiTestSeams()
		ginkgo.DeferCleanup(restore)
		newWikiOAuthService = func(wikioauth.ServiceConfig) (*wikioauth.Service, error) {
			return nil, expected
		}
		Expect((&Wiki{}).initOAuth(&WikiOptions{})).To(MatchError(expected))
	})

	ginkgo.It("surfaces core service and route service initializer failures", func() {
		expected := errors.New("sync open failed")
		restore := restoreWikiTestSeams()
		ginkgo.DeferCleanup(restore)
		newWikiWorkspaceSync = func(workspacesync.ServiceOptions) (workspaceSyncFacade, error) {
			return nil, expected
		}
		Expect(newInitializerWiki().initCoreServices(&WikiOptions{})).To(MatchError(expected))

		restore()
		restore = restoreWikiTestSeams()
		ginkgo.DeferCleanup(restore)
		expected = errors.New("startup sync failed")
		newWikiWorkspaceSync = func(workspacesync.ServiceOptions) (workspaceSyncFacade, error) {
			return &fakeWorkspaceSyncFacade{syncErr: expected}, nil
		}
		Expect(newInitializerWiki().initCoreServices(&WikiOptions{})).To(MatchError(expected))

		Expect(runInitializerError(func(expected error) error {
			newWikiLinksStore = func(string) (*links.LinksStore, error) { return nil, expected }
			return newInitializerWiki().initLinkService()
		})).To(MatchError(errFixtureWikiEdgeFailed))
		Expect(runInitializerError(func(expected error) error {
			newWikiTagsStore = func(string) (*tags.TagsStore, error) { return nil, expected }
			return newInitializerWiki().initTagsService()
		})).To(MatchError(errFixtureWikiEdgeFailed))
		Expect(runInitializerError(func(expected error) error {
			newWikiPropertiesStore = func(string) (*properties.PropertiesStore, error) { return nil, expected }
			return newInitializerWiki().initPropertiesService()
		})).To(MatchError(errFixtureWikiEdgeFailed))
		Expect(runInitializerError(func(expected error) error {
			newWikiSQLiteIndex = func(string) (*search.SQLiteIndex, error) { return nil, expected }
			return newInitializerWiki().initSearch()
		})).To(MatchError(errFixtureWikiEdgeFailed))
		Expect(runInitializerError(func(expected error) error {
			newWikiBrandingService = func(string) (*branding.BrandingService, error) { return nil, expected }
			return newInitializerWiki().initBranding()
		})).To(MatchError(errFixtureWikiEdgeFailed))
	})

	ginkgo.It("logs non-fatal bootstrap and indexing failures", func() {
		expected := errors.New("index failed")
		restore := restoreWikiTestSeams()
		ginkgo.DeferCleanup(restore)
		wikiLinksIndexAllPages = func(*links.LinkService) error { return expected }
		Expect(newInitializerWiki().initLinkService()).To(Succeed())

		restore()
		restore = restoreWikiTestSeams()
		ginkgo.DeferCleanup(restore)
		wikiRebuildTagsProperties = func(*Wiki) error { return expected }
		newInitializerWiki().bootstrapTagsAndProperties()

		restore()
		restore = restoreWikiTestSeams()
		ginkgo.DeferCleanup(restore)
		wikiSearchIndexAllPages = func(*pagesave.SearchIndexSideEffect) error { return expected }
		w := newInitializerWiki()
		Expect(w.initSearch()).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(w.status.Snapshot()).To(haveFailedSearchInitializationStatus())
		}).WithTimeout(time.Second).Should(Succeed())
	})

	ginkgo.It("surfaces rebuild and welcome-page failure paths", func() {
		Expect(runInitializerError(func(expected error) error {
			wikiLinksIndexAllPages = func(*links.LinkService) error { return expected }
			return newInitializerWiki().rebuildDerivedIndexes()
		})).To(MatchError(errFixtureWikiEdgeFailed))
		Expect(runInitializerError(func(expected error) error {
			wikiRebuildTagsProperties = func(*Wiki) error { return expected }
			return newInitializerWiki().rebuildDerivedIndexes()
		})).To(MatchError(errFixtureWikiEdgeFailed))
		Expect(runInitializerError(func(expected error) error {
			wikiSearchIndexAllPages = func(*pagesave.SearchIndexSideEffect) error { return expected }
			return newInitializerWiki().rebuildDerivedIndexes()
		})).To(MatchError(errFixtureWikiEdgeFailed))

		Expect(runInitializerError(func(expected error) error {
			wikiCreateWelcomePage = func(*Wiki, tree.UserID, *tree.NodeKind) (*wikipages.CreatePageOutput, error) {
				return nil, expected
			}
			return newInitializerWiki().EnsureWelcomePage()
		})).To(MatchError(errFixtureWikiEdgeFailed))
		Expect(runInitializerError(func(expected error) error {
			wikiCreateWelcomePage = func(*Wiki, tree.UserID, *tree.NodeKind) (*wikipages.CreatePageOutput, error) {
				return &wikipages.CreatePageOutput{Page: &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("welcome")}}}, nil
			}
			wikiGetWelcomePage = func(*tree.TreeService, tree.PageID) (*tree.Page, error) { return nil, expected }
			return newInitializerWiki().EnsureWelcomePage()
		})).To(MatchError(errFixtureWikiEdgeFailed))
		Expect(runInitializerError(func(expected error) error {
			wikiCreateWelcomePage = func(*Wiki, tree.UserID, *tree.NodeKind) (*wikipages.CreatePageOutput, error) {
				return &wikipages.CreatePageOutput{Page: &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("welcome")}}}, nil
			}
			wikiGetWelcomePage = func(*tree.TreeService, tree.PageID) (*tree.Page, error) {
				return &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("welcome"), Title: "Welcome", Slug: "welcome"}}, nil
			}
			wikiUpdateWelcomePage = func(*Wiki, tree.UserID, *tree.Page, *string, *tree.NodeKind) (*wikipages.UpdatePageOutput, error) {
				return nil, expected
			}
			return newInitializerWiki().EnsureWelcomePage()
		})).To(MatchError(errFixtureWikiEdgeFailed))
	})

	ginkgo.It("ignores per-page tag and property rebuild failures while preserving fatal rebuild and close errors", func() {
		expected := errors.New("edge failed")
		restore := restoreWikiTestSeams()
		ginkgo.DeferCleanup(restore)
		wikiTreeWalkNodes = func(_ *tree.TreeService, fn func(tree.PageID) error) error {
			Expect(fn(newFixturePageID("missing"))).To(Succeed())
			Expect(fn(newFixturePageID("indexed"))).To(Succeed())
			return nil
		}
		wikiTreeGetPages = func(*tree.TreeService, []tree.PageID) ([]*tree.Page, []error) {
			return []*tree.Page{
				nil,
				{PageNode: &tree.PageNode{ID: newFixturePageID("indexed"), Title: "Indexed", Slug: "indexed"}, RawContent: "body"},
			}, []error{expected, nil}
		}
		wikiTagsIndexPageContent = func(*tags.TagsService, tree.PageID, string) error { return expected }
		wikiPropsIndexPageContent = func(*properties.PropertiesService, tree.PageID, string) error { return expected }
		Expect(newInitializerWiki().rebuildTagsAndProperties()).To(Succeed())

		Expect(runInitializerError(func(expected error) error {
			wikiTagsClearIndex = func(*tags.TagsService) error { return expected }
			return newInitializerWiki().rebuildTagsAndProperties()
		})).To(MatchError(errFixtureWikiEdgeFailed))
		Expect(runInitializerError(func(expected error) error {
			wikiPropertiesClearIndex = func(*properties.PropertiesService) error { return expected }
			return newInitializerWiki().rebuildTagsAndProperties()
		})).To(MatchError(errFixtureWikiEdgeFailed))
		Expect(runInitializerError(func(expected error) error {
			wikiTreeWalkNodes = func(*tree.TreeService, func(tree.PageID) error) error { return expected }
			return newInitializerWiki().rebuildTagsAndProperties()
		})).To(MatchError(errFixtureWikiEdgeFailed))

		Expect(runCloseError(func(expected error) *Wiki {
			wikiCloseUserService = func(*coreauth.UserService) error { return expected }
			return &Wiki{user: &coreauth.UserService{}}
		})).To(MatchError(errFixtureWikiCloseFailed))
		Expect(runCloseError(func(expected error) *Wiki {
			wikiCloseAPIKeyService = func(*coreauth.APIKeyService) error { return expected }
			return &Wiki{apiKeys: &coreauth.APIKeyService{}}
		})).To(MatchError(errFixtureWikiCloseFailed))
		Expect(runCloseError(func(expected error) *Wiki {
			wikiCloseSearchIndex = func(*search.SQLiteIndex) error { return expected }
			return &Wiki{searchIndex: &search.SQLiteIndex{}}
		})).To(MatchError(errFixtureWikiCloseFailed))

		restore()
		restore = restoreWikiTestSeams()
		ginkgo.DeferCleanup(restore)
		wikiCloseLinksService = func(*links.LinkService) error { return expected }
		Expect((&Wiki{
			links: &links.LinkService{},
			log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		}).Close()).To(Succeed())
	})
})
