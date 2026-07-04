package wiki

import (
	"io"
	"log/slog"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/links"
	"github.com/perber/wiki/internal/properties"
	"github.com/perber/wiki/internal/search"
	"github.com/perber/wiki/internal/tags"
)

func haveFailedSearchInitializationStatus() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(searchInitializationObservationFromStatus, Equal(searchInitializationObservation{
		State:    searchInitializationFailed,
		Failures: searchInitializationFailuresRecorded,
		Finished: searchInitializationFinishedRecorded,
	}))
}

type searchInitializationState string

const (
	searchInitializationMissing searchInitializationState = "missing"
	searchInitializationFailed  searchInitializationState = "failed"
	searchInitializationOther   searchInitializationState = "other"
)

type searchInitializationFailureState string

const (
	searchInitializationFailuresMissing  searchInitializationFailureState = "no failures recorded"
	searchInitializationFailuresRecorded searchInitializationFailureState = "failures recorded"
)

type searchInitializationFinishedState string

const (
	searchInitializationFinishedMissing  searchInitializationFinishedState = "finish time missing"
	searchInitializationFinishedRecorded searchInitializationFinishedState = "finish time recorded"
)

type searchInitializationObservation struct {
	State    searchInitializationState
	Failures searchInitializationFailureState
	Finished searchInitializationFinishedState
}

func searchInitializationObservationFromStatus(status *search.IndexingStatus) searchInitializationObservation {
	if status == nil {
		return searchInitializationObservation{State: searchInitializationMissing}
	}
	state := searchInitializationOther
	if status.IsFailed() {
		state = searchInitializationFailed
	}
	failures := searchInitializationFailuresMissing
	if status.Failed > 0 {
		failures = searchInitializationFailuresRecorded
	}
	finished := searchInitializationFinishedMissing
	if !status.FinishedAt.IsZero() {
		finished = searchInitializationFinishedRecorded
	}
	return searchInitializationObservation{
		State:    state,
		Failures: failures,
		Finished: finished,
	}
}

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

func runNewWikiStartupError(configure func(error), mutateOptions ...func(*WikiOptions)) error {
	ginkgo.GinkgoHelper()

	restore := restoreWikiTestSeams()
	defer restore()
	configure(errFixtureWikiStartupFailed)

	options := &WikiOptions{
		StorageDir:          wikiTestTempDir(),
		AdminPassword:       "admin",
		JWTSecret:           "secret",
		AccessTokenTimeout:  time.Minute,
		RefreshTokenTimeout: time.Hour,
	}
	for _, mutate := range mutateOptions {
		mutate(options)
	}

	_, err := NewWiki(options)
	return err
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

func runInitAuthError(configure func(error)) error {
	ginkgo.GinkgoHelper()

	restore := restoreWikiTestSeams()
	defer restore()
	configure(errFixtureWikiAuthFailed)
	return newInitializerWiki().initAuth(&WikiOptions{
		AdminPassword:       "admin",
		JWTSecret:           "secret",
		AccessTokenTimeout:  time.Minute,
		RefreshTokenTimeout: time.Hour,
	})
}

func runInitializerError(run func(error) error) error {
	ginkgo.GinkgoHelper()

	restore := restoreWikiTestSeams()
	defer restore()
	return run(errFixtureWikiEdgeFailed)
}

func runCloseError(build func(error) *Wiki) error {
	ginkgo.GinkgoHelper()

	restore := restoreWikiTestSeams()
	defer restore()
	return build(errFixtureWikiCloseFailed).Close()
}

func newInitializerWiki() *Wiki {
	ginkgo.GinkgoHelper()

	storageDir := wikiTestTempDir()
	rootDir := filepath.Join(wikiTestTempDir(), "root")
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

	storageDir := wikiTestTempDir()
	rootDir := filepath.Join(wikiTestTempDir(), "root")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: storageDir, RootDir: rootDir})
	Expect(treeService.LoadTree()).To(Succeed())
	return treeService
}
