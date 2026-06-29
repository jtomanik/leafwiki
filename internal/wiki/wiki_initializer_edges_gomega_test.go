package wiki

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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

var _ = ginkgo.Describe("wiki initializer edge coverage", func() {
	ginkgo.It("returns each NewWiki startup orchestration error", func() {
		_, err := NewWiki(&WikiOptions{
			StorageDir:       ginkgo.GinkgoT().TempDir(),
			WorkspaceOnly:    true,
			ControlPlaneOnly: true,
		})
		Expect(err).To(MatchError("workspace-only and control-plane-only modes cannot be combined"))

		expectNewWikiStartupError(func(expected error) {
			newWikiEnsureWorkspaceDirs = func(Workspace) error { return expected }
		})
		expectNewWikiStartupError(func(expected error) {
			installFastNewWikiStartup()
			newWikiInitAuth = func(*Wiki, *WikiOptions) error { return expected }
		})
		expectNewWikiStartupError(func(expected error) {
			installFastNewWikiStartup()
			newWikiInitOAuth = func(*Wiki, *WikiOptions) error { return expected }
		})
		expectNewWikiStartupError(func(expected error) {
			installFastNewWikiStartup()
			newWikiInitBranding = func(*Wiki) error { return expected }
		}, func(options *WikiOptions) {
			options.ControlPlaneOnly = true
		})
		expectNewWikiStartupError(func(expected error) {
			installFastNewWikiStartup()
			newWikiInitCoreServices = func(*Wiki, *WikiOptions) error { return expected }
		})
		expectNewWikiStartupError(func(expected error) {
			installFastNewWikiStartup()
			newWikiInitLinkService = func(*Wiki) error { return expected }
		})
		expectNewWikiStartupError(func(expected error) {
			installFastNewWikiStartup()
			newWikiInitTagsService = func(*Wiki) error { return expected }
		})
		expectNewWikiStartupError(func(expected error) {
			installFastNewWikiStartup()
			newWikiInitProperties = func(*Wiki) error { return expected }
		})
		expectNewWikiStartupError(func(expected error) {
			installFastNewWikiStartup()
			newWikiInitSearch = func(*Wiki) error { return expected }
		})
		expectNewWikiStartupError(func(expected error) {
			installFastNewWikiStartup()
			newWikiInitBranding = func(*Wiki) error { return expected }
		})
		expectNewWikiStartupError(func(expected error) {
			installFastNewWikiStartup()
			newWikiEnsureWelcomePage = func(*Wiki) error { return expected }
		})
	})

	ginkgo.It("surfaces workspace directory creation failures", func() {
		expected := errors.New("mkdir failed")
		restore := restoreWikiTestSeams()
		ginkgo.DeferCleanup(restore)
		wikiMkdirAll = func(string, os.FileMode) error {
			return expected
		}
		Expect(ensureWorkspaceDirs(Workspace{DataDir: "data", RootDir: "root"})).To(MatchError(ContainSubstring("create data dir")))

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
		Expect(ensureWorkspaceDirs(Workspace{DataDir: "data", RootDir: "root"})).To(MatchError(ContainSubstring("create root dir")))
	})

	ginkgo.It("surfaces auth and oauth initializer dependency failures", func() {
		expectInitAuthError(func(expected error) {
			newWikiUserStore = func(string) (*coreauth.UserStore, error) { return nil, expected }
		})
		expectInitAuthError(func(expected error) {
			newWikiAPIKeyStore = func(string) (*coreauth.APIKeyStore, error) { return nil, expected }
		})
		expectInitAuthError(func(expected error) {
			wikiInitDefaultAdmin = func(*coreauth.UserService, string) error { return expected }
		})
		expectInitAuthError(func(expected error) {
			newWikiUserResolver = func(*coreauth.UserService) (*coreauth.UserResolver, error) { return nil, expected }
		})
		expectInitAuthError(func(expected error) {
			newWikiSessionStore = func(string) (*coreauth.SessionStore, error) { return nil, expected }
		})

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

		expectInitializerError(func(expected error) error {
			newWikiLinksStore = func(string) (*links.LinksStore, error) { return nil, expected }
			return newInitializerWiki().initLinkService()
		}, "failed to init links store")
		expectInitializerError(func(expected error) error {
			newWikiTagsStore = func(string) (*tags.TagsStore, error) { return nil, expected }
			return newInitializerWiki().initTagsService()
		}, "failed to init tags store")
		expectInitializerError(func(expected error) error {
			newWikiPropertiesStore = func(string) (*properties.PropertiesStore, error) { return nil, expected }
			return newInitializerWiki().initPropertiesService()
		}, "failed to init properties store")
		expectInitializerError(func(expected error) error {
			newWikiSQLiteIndex = func(string) (*search.SQLiteIndex, error) { return nil, expected }
			return newInitializerWiki().initSearch()
		}, "failed to init search index")
		expectInitializerError(func(expected error) error {
			newWikiBrandingService = func(string) (*branding.BrandingService, error) { return nil, expected }
			return newInitializerWiki().initBranding()
		}, "failed to init branding service")
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
		Eventually(func() bool {
			return w.status.IsFailed()
		}).WithTimeout(time.Second).Should(BeTrue())
	})

	ginkgo.It("surfaces rebuild and welcome-page failure paths", func() {
		expectInitializerError(func(expected error) error {
			wikiLinksIndexAllPages = func(*links.LinkService) error { return expected }
			return newInitializerWiki().rebuildDerivedIndexes()
		}, "rebuild links")
		expectInitializerError(func(expected error) error {
			wikiRebuildTagsProperties = func(*Wiki) error { return expected }
			return newInitializerWiki().rebuildDerivedIndexes()
		}, "rebuild tags/properties")
		expectInitializerError(func(expected error) error {
			wikiSearchIndexAllPages = func(*pagesave.SearchIndexSideEffect) error { return expected }
			return newInitializerWiki().rebuildDerivedIndexes()
		}, "rebuild search")

		expectInitializerError(func(expected error) error {
			wikiCreateWelcomePage = func(*Wiki, tree.UserID, *tree.NodeKind) (*wikipages.CreatePageOutput, error) {
				return nil, expected
			}
			return newInitializerWiki().EnsureWelcomePage()
		}, "edge failed")
		expectInitializerError(func(expected error) error {
			wikiCreateWelcomePage = func(*Wiki, tree.UserID, *tree.NodeKind) (*wikipages.CreatePageOutput, error) {
				return &wikipages.CreatePageOutput{Page: &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("welcome")}}}, nil
			}
			wikiGetWelcomePage = func(*tree.TreeService, tree.PageID) (*tree.Page, error) { return nil, expected }
			return newInitializerWiki().EnsureWelcomePage()
		}, "edge failed")
		expectInitializerError(func(expected error) error {
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
		}, "edge failed")
	})

	ginkgo.It("covers rebuild tag/property warning branches and close error paths", func() {
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

		expectInitializerError(func(expected error) error {
			wikiTagsClearIndex = func(*tags.TagsService) error { return expected }
			return newInitializerWiki().rebuildTagsAndProperties()
		}, "edge failed")
		expectInitializerError(func(expected error) error {
			wikiPropertiesClearIndex = func(*properties.PropertiesService) error { return expected }
			return newInitializerWiki().rebuildTagsAndProperties()
		}, "edge failed")
		expectInitializerError(func(expected error) error {
			wikiTreeWalkNodes = func(*tree.TreeService, func(tree.PageID) error) error { return expected }
			return newInitializerWiki().rebuildTagsAndProperties()
		}, "edge failed")

		expectCloseError(func(expected error) *Wiki {
			wikiCloseUserService = func(*coreauth.UserService) error { return expected }
			return &Wiki{user: &coreauth.UserService{}}
		})
		expectCloseError(func(expected error) *Wiki {
			wikiCloseAPIKeyService = func(*coreauth.APIKeyService) error { return expected }
			return &Wiki{apiKeys: &coreauth.APIKeyService{}}
		})
		expectCloseError(func(expected error) *Wiki {
			wikiCloseSearchIndex = func(*search.SQLiteIndex) error { return expected }
			return &Wiki{searchIndex: &search.SQLiteIndex{}}
		})

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

func restoreWikiTestSeams() func() {
	ginkgo.GinkgoHelper()

	ensureWorkspaceDirs := newWikiEnsureWorkspaceDirs
	initAuth := newWikiInitAuth
	initOAuth := newWikiInitOAuth
	initCoreServices := newWikiInitCoreServices
	initLinkService := newWikiInitLinkService
	initTagsService := newWikiInitTagsService
	initProperties := newWikiInitProperties
	bootstrapIndexes := newWikiBootstrapIndexes
	initSearch := newWikiInitSearch
	initBranding := newWikiInitBranding
	ensureWelcomePage := newWikiEnsureWelcomePage
	mkdirAll := wikiMkdirAll
	userStore := newWikiUserStore
	apiKeyStore := newWikiAPIKeyStore
	initDefaultAdmin := wikiInitDefaultAdmin
	userResolver := newWikiUserResolver
	sessionStore := newWikiSessionStore
	oauthService := newWikiOAuthService
	treeService := newWikiTreeService
	workspaceSync := newWikiWorkspaceSync
	linksStore := newWikiLinksStore
	linksIndexAllPages := wikiLinksIndexAllPages
	tagsStore := newWikiTagsStore
	propertiesStore := newWikiPropertiesStore
	rebuildTagsProperties := wikiRebuildTagsProperties
	tagsClearIndex := wikiTagsClearIndex
	propertiesClearIndex := wikiPropertiesClearIndex
	treeWalkNodes := wikiTreeWalkNodes
	treeGetPages := wikiTreeGetPages
	tagsIndexPageContent := wikiTagsIndexPageContent
	propsIndexPageContent := wikiPropsIndexPageContent
	sqliteIndex := newWikiSQLiteIndex
	searchIndexAllPages := wikiSearchIndexAllPages
	brandingService := newWikiBrandingService
	closeUserService := wikiCloseUserService
	closeAPIKeyService := wikiCloseAPIKeyService
	closeLinksService := wikiCloseLinksService
	closeSearchIndex := wikiCloseSearchIndex
	createWelcomePage := wikiCreateWelcomePage
	getWelcomePage := wikiGetWelcomePage
	updateWelcomePage := wikiUpdateWelcomePage

	return func() {
		newWikiEnsureWorkspaceDirs = ensureWorkspaceDirs
		newWikiInitAuth = initAuth
		newWikiInitOAuth = initOAuth
		newWikiInitCoreServices = initCoreServices
		newWikiInitLinkService = initLinkService
		newWikiInitTagsService = initTagsService
		newWikiInitProperties = initProperties
		newWikiBootstrapIndexes = bootstrapIndexes
		newWikiInitSearch = initSearch
		newWikiInitBranding = initBranding
		newWikiEnsureWelcomePage = ensureWelcomePage
		wikiMkdirAll = mkdirAll
		newWikiUserStore = userStore
		newWikiAPIKeyStore = apiKeyStore
		wikiInitDefaultAdmin = initDefaultAdmin
		newWikiUserResolver = userResolver
		newWikiSessionStore = sessionStore
		newWikiOAuthService = oauthService
		newWikiTreeService = treeService
		newWikiWorkspaceSync = workspaceSync
		newWikiLinksStore = linksStore
		wikiLinksIndexAllPages = linksIndexAllPages
		newWikiTagsStore = tagsStore
		newWikiPropertiesStore = propertiesStore
		wikiRebuildTagsProperties = rebuildTagsProperties
		wikiTagsClearIndex = tagsClearIndex
		wikiPropertiesClearIndex = propertiesClearIndex
		wikiTreeWalkNodes = treeWalkNodes
		wikiTreeGetPages = treeGetPages
		wikiTagsIndexPageContent = tagsIndexPageContent
		wikiPropsIndexPageContent = propsIndexPageContent
		newWikiSQLiteIndex = sqliteIndex
		wikiSearchIndexAllPages = searchIndexAllPages
		newWikiBrandingService = brandingService
		wikiCloseUserService = closeUserService
		wikiCloseAPIKeyService = closeAPIKeyService
		wikiCloseLinksService = closeLinksService
		wikiCloseSearchIndex = closeSearchIndex
		wikiCreateWelcomePage = createWelcomePage
		wikiGetWelcomePage = getWelcomePage
		wikiUpdateWelcomePage = updateWelcomePage
	}
}

func expectNewWikiStartupError(configure func(error), mutateOptions ...func(*WikiOptions)) {
	ginkgo.GinkgoHelper()

	restore := restoreWikiTestSeams()
	defer restore()
	expected := errors.New("startup failed")
	configure(expected)

	options := &WikiOptions{
		StorageDir:          ginkgo.GinkgoT().TempDir(),
		AdminPassword:       "admin",
		JWTSecret:           "secret",
		AccessTokenTimeout:  time.Minute,
		RefreshTokenTimeout: time.Hour,
	}
	for _, mutate := range mutateOptions {
		mutate(options)
	}

	_, err := NewWiki(options)
	Expect(err).To(MatchError(expected))
}

func installFastNewWikiStartup() {
	ginkgo.GinkgoHelper()

	newWikiInitAuth = func(*Wiki, *WikiOptions) error { return nil }
	newWikiInitOAuth = func(*Wiki, *WikiOptions) error { return nil }
	newWikiInitCoreServices = func(w *Wiki, _ *WikiOptions) error {
		w.tree = newLoadedInitializerTree()
		w.slug = tree.NewSlugService()
		return nil
	}
	newWikiInitLinkService = func(*Wiki) error { return nil }
	newWikiInitTagsService = func(*Wiki) error { return nil }
	newWikiInitProperties = func(*Wiki) error { return nil }
	newWikiBootstrapIndexes = func(*Wiki) {}
	newWikiInitSearch = func(*Wiki) error { return nil }
	newWikiInitBranding = func(*Wiki) error { return nil }
	newWikiEnsureWelcomePage = func(*Wiki) error { return nil }
}

func expectInitAuthError(configure func(error)) {
	ginkgo.GinkgoHelper()

	restore := restoreWikiTestSeams()
	defer restore()
	expected := errors.New("auth failed")
	configure(expected)
	err := newInitializerWiki().initAuth(&WikiOptions{
		AdminPassword:       "admin",
		JWTSecret:           "secret",
		AccessTokenTimeout:  time.Minute,
		RefreshTokenTimeout: time.Hour,
	})
	Expect(err).To(MatchError(expected))
}

func expectInitializerError(run func(error) error, message string) {
	ginkgo.GinkgoHelper()

	restore := restoreWikiTestSeams()
	defer restore()
	expected := errors.New("edge failed")
	err := run(expected)
	Expect(err).To(MatchError(ContainSubstring(message)))
}

func expectCloseError(build func(error) *Wiki) {
	ginkgo.GinkgoHelper()

	restore := restoreWikiTestSeams()
	defer restore()
	expected := errors.New("close failed")
	Expect(build(expected).Close()).To(MatchError(expected))
}

func newInitializerWiki() *Wiki {
	ginkgo.GinkgoHelper()

	storageDir := ginkgo.GinkgoT().TempDir()
	rootDir := filepath.Join(ginkgo.GinkgoT().TempDir(), "root")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: storageDir, RootDir: rootDir})
	Expect(treeService.LoadTree()).To(Succeed())
	slugService := tree.NewSlugService()

	linksStore, err := links.NewLinksStore(storageDir)
	Expect(err).NotTo(HaveOccurred())
	tagsStore, err := tags.NewTagsStore(storageDir)
	Expect(err).NotTo(HaveOccurred())
	propsStore, err := properties.NewPropertiesStore(storageDir)
	Expect(err).NotTo(HaveOccurred())
	searchIndex, err := search.NewSQLiteIndex(storageDir)
	Expect(err).NotTo(HaveOccurred())

	return &Wiki{
		tree:        treeService,
		slug:        slugService,
		links:       links.NewLinkService(storageDir, treeService, linksStore),
		tags:        tags.NewTagsService(tagsStore),
		props:       properties.NewPropertiesService(propsStore),
		searchIndex: searchIndex,
		storageDir:  storageDir,
		workspace:   Workspace{ID: "default", DataDir: storageDir, RootDir: rootDir},
		log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func newLoadedInitializerTree() *tree.TreeService {
	ginkgo.GinkgoHelper()

	storageDir := ginkgo.GinkgoT().TempDir()
	rootDir := filepath.Join(ginkgo.GinkgoT().TempDir(), "root")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: storageDir, RootDir: rootDir})
	Expect(treeService.LoadTree()).To(Succeed())
	return treeService
}
