package links

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	corelinks "github.com/perber/wiki/internal/links"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var _ = ginkgo.Describe("link use cases", func() {
	ginkgo.It("nil link services return ErrLinkServiceUnavailable for read use cases", func() {
		_, err := NewGetLinkStatusUseCase(nil, nil).Execute(context.Background(), GetLinkStatusInput{PageID: newFixturePageID("page-1")})
		Expect(err).To(MatchError(ErrLinkServiceUnavailable))

		_, err = NewGetBacklinksUseCase(nil).Execute(context.Background(), GetBacklinksInput{PageID: newFixturePageID("page-1")})
		Expect(err).To(MatchError(ErrLinkServiceUnavailable))

		_, err = NewGetOutgoingLinksUseCase(nil).Execute(context.Background(), GetOutgoingLinksInput{PageID: newFixturePageID("page-1")})
		Expect(err).To(MatchError(ErrLinkServiceUnavailable))
	})

	ginkgo.It("ReindexLinksUseCase is a no-op when the link service is unavailable", func() {
		Expect(NewReindexLinksUseCase(nil).Execute(context.Background())).To(Succeed())
	})

	ginkgo.It("GetLinkStatusUseCase maps missing pages to localized link-page-not-found errors", func() {
		dataDir := tempLinksDir()
		rootDir := tempLinksDir()
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{
			DataDir: dataDir,
			RootDir: rootDir,
		})
		Expect(treeService.LoadTree()).To(Succeed())

		_, err := NewGetLinkStatusUseCase(&corelinks.LinkService{}, treeService).Execute(context.Background(), GetLinkStatusInput{PageID: newFixturePageID("missing")})
		Expect(err).To(testmatchers.MatchLocalizedError(ErrCodeLinkPageNotFound, sharederrors.MessageIDForCode(ErrCodeLinkPageNotFound)))
	})

	ginkgo.It("GetLinkStatusUseCase returns non-not-found tree lookup errors directly", func() {
		treeService := tree.NewTreeService(tempLinksDir())

		out, err := NewGetLinkStatusUseCase(&corelinks.LinkService{}, treeService).Execute(context.Background(), GetLinkStatusInput{PageID: newFixturePageID("page-1")})

		Expect(out).To(BeNil())
		Expect(err).To(MatchError(tree.ErrTreeNotLoaded))
	})

	ginkgo.It("GetLinkStatusUseCase returns link status for an indexed page", func() {
		fixture := newWikiLinksFixture()
		uc := NewGetLinkStatusUseCase(fixture.links, fixture.tree)

		out, err := uc.Execute(context.Background(), GetLinkStatusInput{PageID: fixture.sourceID})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Status).To(haveLinkStatusForIndexedPage(fixture.targetID, tree.RoutePathFromString("/missing")))
	})

	ginkgo.It("GetBacklinksUseCase returns backlinks from the link service", func() {
		fixture := newWikiLinksFixture()

		out, err := NewGetBacklinksUseCase(fixture.links).Execute(context.Background(), GetBacklinksInput{PageID: fixture.targetID})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Result).To(haveBacklinksResult(1, fixture.sourceID))
	})

	ginkgo.It("GetOutgoingLinksUseCase returns outgoing links from the link service", func() {
		fixture := newWikiLinksFixture()

		out, err := NewGetOutgoingLinksUseCase(fixture.links).Execute(context.Background(), GetOutgoingLinksInput{PageID: fixture.sourceID})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Result).To(haveOutgoingLinksResult(2, fixture.targetID))
	})

	ginkgo.It("returns backing link store errors from read use cases", func() {
		fixture := newWikiLinksFixture()
		dropLinksTable(fixture.dataDir)

		status, err := NewGetLinkStatusUseCase(fixture.links, fixture.tree).Execute(context.Background(), GetLinkStatusInput{PageID: fixture.sourceID})
		Expect(status).To(BeNil())
		Expect(err).To(matchSQLitePrimaryError())

		backlinks, err := NewGetBacklinksUseCase(fixture.links).Execute(context.Background(), GetBacklinksInput{PageID: fixture.targetID})
		Expect(backlinks).To(BeNil())
		Expect(err).To(matchSQLitePrimaryError())

		outgoing, err := NewGetOutgoingLinksUseCase(fixture.links).Execute(context.Background(), GetOutgoingLinksInput{PageID: fixture.sourceID})
		Expect(outgoing).To(BeNil())
		Expect(err).To(matchSQLitePrimaryError())
	})

	ginkgo.It("ReindexLinksUseCase delegates to the link service when available", func() {
		fixture := newWikiLinksFixture()

		Expect(NewReindexLinksUseCase(fixture.links).Execute(context.Background())).To(Succeed())
	})
})

var _ = ginkgo.Describe("link routes", func() {
	ginkgo.It("serves link status through the public route", func() {
		fixture := newWikiLinksFixture()
		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{
				GetLinkStatus: NewGetLinkStatusUseCase(fixture.links, fixture.tree),
			})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{PublicAccess: true, DisableFrontendRoutes: true},
		)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, linkStatusPath(fixture.sourceID), nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		var body corelinks.LinkStatusResult
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body).To(haveLinkStatusCounts(1, 1))
	})

	ginkgo.It("requires authentication for the private link status route", func() {
		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{DisableFrontendRoutes: true},
		)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, linkStatusPath(newFixturePageID("page-1")), nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), rec.Body.String())
	})

	ginkgo.It("returns structured errors from the public link status route", func() {
		fixture := newWikiLinksFixture()
		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{
				GetLinkStatus: NewGetLinkStatusUseCase(fixture.links, fixture.tree),
			})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{PublicAccess: true, DisableFrontendRoutes: true},
		)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, linkStatusPath(newFixturePageID("missing")), nil))

		Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusNotFound, ErrCodeLinkPageNotFound, sharederrors.MessageIDForCode(ErrCodeLinkPageNotFound)))
	})
})

type wikiLinksFixture struct {
	dataDir  string
	tree     *tree.TreeService
	links    *corelinks.LinkService
	sourceID tree.PageID
	targetID tree.PageID
}

func newWikiLinksFixture() wikiLinksFixture {
	ginkgo.GinkgoHelper()
	dataDir := tempLinksDir()
	Expect(os.WriteFile(
		dataDir+"/schema.json",
		[]byte(fmt.Sprintf(`{"version":%d}`, tree.CurrentSchemaVersion)),
		0o644,
	)).To(Succeed())
	treeService := tree.NewTreeService(dataDir)
	Expect(treeService.LoadTree()).To(Succeed())
	pageKind := tree.NodeKindPage
	sourceID, err := treeService.CreateNode("user-1", nil, "Source", "source", &pageKind)
	Expect(err).NotTo(HaveOccurred())
	targetID, err := treeService.CreateNode("user-1", nil, "Target", "target", &pageKind)
	Expect(err).NotTo(HaveOccurred())

	store, err := corelinks.NewLinksStore(dataDir)
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})
	linkService := corelinks.NewLinkService(dataDir, treeService, store)
	sourcePage, err := treeService.GetPage(*sourceID)
	Expect(err).NotTo(HaveOccurred())
	Expect(linkService.UpdateLinksForPage(sourcePage, "[target](/target.md) [missing](/missing)")).To(Succeed())

	return wikiLinksFixture{dataDir: dataDir, tree: treeService, links: linkService, sourceID: *sourceID, targetID: *targetID}
}

func tempLinksDir() string {
	ginkgo.GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-links-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func dropLinksTable(dataDir string) {
	ginkgo.GinkgoHelper()
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "links.db"))
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(db.Close()).To(Succeed())
	})
	_, err = db.Exec("DROP TABLE links")
	Expect(err).NotTo(HaveOccurred())
}

func haveLinkStatusForIndexedPage(targetID tree.PageID, missingPath tree.RoutePath) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return SatisfyAll(
		haveLinkStatusCounts(1, 1),
		HaveValue(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Outgoings":       ContainElement(haveOutgoingLinkToPage(targetID)),
			"BrokenOutgoings": ContainElement(haveBrokenOutgoingLinkToPath(missingPath)),
		})),
	)
}

func haveLinkStatusCounts(outgoings, brokenOutgoings int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return HaveValue(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Counts": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Outgoings":       Equal(outgoings),
			"BrokenOutgoings": Equal(brokenOutgoings),
		}),
	}))
}

func haveBacklinksResult(count int, fromPageID tree.PageID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return HaveValue(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Count":     Equal(count),
		"Backlinks": ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{"FromPageID": Equal(fromPageID)})),
	}))
}

func haveOutgoingLinksResult(count int, targetID tree.PageID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return HaveValue(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Count": Equal(count),
		"Outgoings": SatisfyAll(
			ContainElement(haveOutgoingLinkToPage(targetID)),
			ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{"Broken": BeTrue()})),
		),
	}))
}

func haveOutgoingLinkToPage(targetID tree.PageID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"ToPageID": Equal(targetID),
	})
}

func haveBrokenOutgoingLinkToPath(path tree.RoutePath) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"ToPath": Equal(path),
	})
}

func matchSQLitePrimaryError() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(err error) int {
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) {
			return -1
		}
		return sqliteErr.Code() & 0xFF
	}, Equal(sqlite3.SQLITE_ERROR))
}

func linkStatusPath(pageID tree.PageID) string {
	return "/api/pages/" + pageID.MetadataValue() + "/links"
}
