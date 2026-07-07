package links

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	corelinks "github.com/perber/wiki/internal/links"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

var _ = ginkgo.Describe("link route handlers", ginkgo.Label("unit"), func() {
	ginkgo.It("constructs routes with the configured link status use case", func() {
		uc := &GetLinkStatusUseCase{}

		Expect(NewRoutes(RoutesConfig{GetLinkStatus: uc}).getLinkStatus).To(Equal(linkStatusExecutor(uc)))
	})

	ginkgo.It("registers the public link status endpoint without auth middleware", func() {
		gin.SetMode(gin.TestMode)
		engine := gin.New()

		(&Routes{getLinkStatus: newLinkStatusExecutor(&corelinks.LinkStatusResult{})}).RegisterRoutes(httpinternal.RouterContext{
			Base: engine.Group(""),
			Opts: httpinternal.RouterOptions{PublicAccess: true},
		})

		Expect(linkRegisteredRoutes(engine)).To(ContainElement("GET /api/pages/:id/links"))
	})

	ginkgo.It("registers the private link status endpoint behind auth middleware", func() {
		gin.SetMode(gin.TestMode)
		engine := gin.New()

		(&Routes{getLinkStatus: newLinkStatusExecutor(&corelinks.LinkStatusResult{})}).RegisterRoutes(httpinternal.RouterContext{
			Base: engine.Group(""),
			Opts: httpinternal.RouterOptions{AuthDisabled: true},
		})

		Expect(linkRegisteredRoutes(engine)).To(ContainElement("GET /api/pages/:id/links"))
	})

	ginkgo.It("writes link status from the direct handler", func() {
		ctx, rec := newLinkUnitContext(newFixturePageID("page-1"))
		routes := &Routes{getLinkStatus: newLinkStatusExecutor(&corelinks.LinkStatusResult{
			Counts: corelinks.LinkStatusCounts{Outgoings: 2},
		})}

		routes.handleGetLinkStatus(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(decodeLinkStatusResponse(rec)).To(haveLinkStatusCounts(2, 0))
	})

	ginkgo.It("writes structured link errors from the direct handler", func() {
		ctx, rec := newLinkUnitContext(newFixturePageID("missing"))
		routes := &Routes{getLinkStatus: linkStatusExecutorFunc(func(context.Context, GetLinkStatusInput) (*GetLinkStatusOutput, error) {
			return nil, sharederrors.NewLocalizedErrorFromCode(ErrCodeLinkPageNotFound, tree.ErrPageNotFound)
		})}

		routes.handleGetLinkStatus(ctx)

		Expect(rec).To(testmatchers.HaveHTTPStructuredError(
			http.StatusNotFound,
			ErrCodeLinkPageNotFound,
			sharederrors.MessageIDForCode(ErrCodeLinkPageNotFound),
		))
	})
})

var _ = ginkgo.Describe("link error responses", ginkgo.Label("unit"), func() {
	ginkgo.It("writes explicit structured link status errors", func() {
		rec := newLinkErrorResponseRecorder(func(ctx *gin.Context) {
			respondWithLinkStatusError(ctx, http.StatusServiceUnavailable, ErrCodeLinkUnavailable, "ignored", "ignored")
		})

		Expect(rec).To(testmatchers.HaveHTTPStructuredError(
			http.StatusServiceUnavailable,
			ErrCodeLinkUnavailable,
			sharederrors.MessageIDForCode(ErrCodeLinkUnavailable),
		))
	})

	ginkgo.It("writes localized and internal structured link errors", func() {
		localized := newLinkErrorResponseRecorder(func(ctx *gin.Context) {
			respondWithLinkError(ctx, ErrLinkServiceUnavailable)
		})
		Expect(localized).To(testmatchers.HaveHTTPStructuredError(
			http.StatusServiceUnavailable,
			ErrCodeLinkUnavailable,
			sharederrors.MessageIDForCode(ErrCodeLinkUnavailable),
		))

		internal := newLinkErrorResponseRecorder(func(ctx *gin.Context) {
			respondWithLinkError(ctx, errors.New("link store failed"))
		})
		Expect(internal).To(testmatchers.HaveHTTPStructuredError(
			http.StatusInternalServerError,
			ErrCodeLinkInternalError,
			sharederrors.MessageIDForCode(ErrCodeLinkInternalError),
		))
	})
})

var _ = ginkgo.Describe("link use case adapters", ginkgo.Label("unit"), func() {
	ginkgo.It("constructs use cases with available concrete services", func() {
		linkSvc := &corelinks.LinkService{}
		treeSvc := &tree.TreeService{}
		statusUC := NewGetLinkStatusUseCase(linkSvc, treeSvc)

		Expect(statusUC).To(matchLinkStatusUseCaseDependencies(linkSvc, treeSvc))
		Expect(NewGetBacklinksUseCase(linkSvc)).To(matchBacklinksUseCaseDependencies(linkSvc))
		Expect(NewGetOutgoingLinksUseCase(linkSvc)).To(matchOutgoingLinksUseCaseDependencies(linkSvc))
		Expect(NewReindexLinksUseCase(linkSvc)).To(matchReindexLinksUseCaseDependencies(linkSvc))
	})

	ginkgo.It("returns link status for the requested page route", func() {
		pageID := newFixturePageID("page-1")
		status := &corelinks.LinkStatusResult{Counts: corelinks.LinkStatusCounts{Outgoings: 1}}
		linkSvc := &recordingLinkStatusService{status: status}
		treeSvc := stubLinkPageGetter{
			page: &tree.Page{PageNode: &tree.PageNode{ID: pageID, Slug: newFixtureSlug("docs"), Title: "Docs", Kind: tree.NodeKindPage}},
		}

		out, err := newGetLinkStatusUseCase(linkSvc, treeSvc).Execute(context.Background(), GetLinkStatusInput{PageID: pageID})

		Expect(err).To(Succeed())
		Expect(out.Status).To(Equal(status))
		Expect(linkSvc).To(recordLinkStatusLookup(pageID, tree.RoutePathFromString("docs")))
	})

	ginkgo.It("maps missing pages to localized link-page-not-found errors", func() {
		linkSvc := &recordingLinkStatusService{status: &corelinks.LinkStatusResult{}}
		treeSvc := stubLinkPageGetter{err: tree.ErrPageNotFound}

		out, err := newGetLinkStatusUseCase(linkSvc, treeSvc).Execute(context.Background(), GetLinkStatusInput{PageID: newFixturePageID("missing")})

		Expect(out).To(BeNil())
		Expect(err).To(testmatchers.MatchLocalizedError(ErrCodeLinkPageNotFound, sharederrors.MessageIDForCode(ErrCodeLinkPageNotFound)))
	})

	ginkgo.It("returns non-not-found page lookup failures unchanged", func() {
		expectedErr := errors.New("tree lookup failed")
		linkSvc := &recordingLinkStatusService{status: &corelinks.LinkStatusResult{}}
		treeSvc := stubLinkPageGetter{err: expectedErr}

		out, err := newGetLinkStatusUseCase(linkSvc, treeSvc).Execute(context.Background(), GetLinkStatusInput{PageID: newFixturePageID("page-1")})

		Expect(out).To(BeNil())
		Expect(err).To(MatchError(expectedErr))
	})

	ginkgo.It("returns link status service failures unchanged", func() {
		expectedErr := errors.New("status lookup failed")
		linkSvc := &recordingLinkStatusService{err: expectedErr}
		treeSvc := stubLinkPageGetter{
			page: &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("page-1"), Slug: newFixtureSlug("docs"), Kind: tree.NodeKindPage}},
		}

		out, err := newGetLinkStatusUseCase(linkSvc, treeSvc).Execute(context.Background(), GetLinkStatusInput{PageID: newFixturePageID("page-1")})

		Expect(out).To(BeNil())
		Expect(err).To(MatchError(expectedErr))
	})

	ginkgo.It("returns backlink and outgoing results from the link service", func() {
		pageID := newFixturePageID("page-1")
		backlinks := &corelinks.BacklinkResult{Count: 1}
		outgoing := &corelinks.OutgoingResult{Count: 2}
		linkSvc := &recordingLinkReadService{backlinks: backlinks, outgoing: outgoing}

		backlinkOut, err := newGetBacklinksUseCase(linkSvc).Execute(context.Background(), GetBacklinksInput{PageID: pageID})
		Expect(err).To(Succeed())
		Expect(backlinkOut.Result).To(Equal(backlinks))

		outgoingOut, err := newGetOutgoingLinksUseCase(linkSvc).Execute(context.Background(), GetOutgoingLinksInput{PageID: pageID})
		Expect(err).To(Succeed())
		Expect(outgoingOut.Result).To(Equal(outgoing))
	})

	ginkgo.It("returns backlink and outgoing service failures unchanged", func() {
		backlinkErr := errors.New("backlinks failed")
		outgoingErr := errors.New("outgoing failed")
		linkSvc := &recordingLinkReadService{backlinksErr: backlinkErr, outgoingErr: outgoingErr}

		backlinkOut, err := newGetBacklinksUseCase(linkSvc).Execute(context.Background(), GetBacklinksInput{PageID: newFixturePageID("page-1")})
		Expect(backlinkOut).To(BeNil())
		Expect(err).To(MatchError(backlinkErr))

		outgoingOut, err := newGetOutgoingLinksUseCase(linkSvc).Execute(context.Background(), GetOutgoingLinksInput{PageID: newFixturePageID("page-1")})
		Expect(outgoingOut).To(BeNil())
		Expect(err).To(MatchError(outgoingErr))
	})

	ginkgo.It("delegates reindexing failures from the link service", func() {
		expectedErr := errors.New("reindex failed")

		Expect(newReindexLinksUseCase(recordingLinkReindexService{err: expectedErr}).Execute(context.Background())).To(MatchError(expectedErr))
	})
})

type linkStatusExecutorFunc func(context.Context, GetLinkStatusInput) (*GetLinkStatusOutput, error)

func (fn linkStatusExecutorFunc) Execute(ctx context.Context, in GetLinkStatusInput) (*GetLinkStatusOutput, error) {
	return fn(ctx, in)
}

func newLinkStatusExecutor(status *corelinks.LinkStatusResult) linkStatusExecutor {
	return linkStatusExecutorFunc(func(context.Context, GetLinkStatusInput) (*GetLinkStatusOutput, error) {
		return &GetLinkStatusOutput{Status: status}, nil
	})
}

func newLinkUnitContext(pageID tree.PageID) (*gin.Context, *httptest.ResponseRecorder) {
	ginkgo.GinkgoHelper()

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, linkStatusPath(pageID), nil)
	ctx.Params = gin.Params{{Key: "id", Value: pageID.MetadataValue()}}
	return ctx, rec
}

func linkRegisteredRoutes(engine *gin.Engine) []string {
	routes := engine.Routes()
	out := make([]string, 0, len(routes))
	for _, route := range routes {
		out = append(out, route.Method+" "+route.Path)
	}
	return out
}

func decodeLinkStatusResponse(rec *httptest.ResponseRecorder) *corelinks.LinkStatusResult {
	ginkgo.GinkgoHelper()

	var body corelinks.LinkStatusResult
	Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed(), rec.Body.String())
	return &body
}

func newLinkErrorResponseRecorder(write func(*gin.Context)) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	write(ctx)
	return rec
}

type recordingLinkStatusService struct {
	status *corelinks.LinkStatusResult
	err    error

	seenPageID tree.PageID
	seenPath   tree.RoutePath
}

func (svc *recordingLinkStatusService) GetLinkStatusForPage(pageID tree.PageID, pagePath tree.RoutePath) (*corelinks.LinkStatusResult, error) {
	svc.seenPageID = pageID
	svc.seenPath = pagePath
	if svc.err != nil {
		return nil, svc.err
	}
	return svc.status, nil
}

type stubLinkPageGetter struct {
	page *tree.Page
	err  error
}

func (svc stubLinkPageGetter) GetPage(tree.PageID) (*tree.Page, error) {
	if svc.err != nil {
		return nil, svc.err
	}
	return svc.page, nil
}

func recordLinkStatusLookup(pageID tree.PageID, routePath tree.RoutePath) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(observedLinkStatusLookup, Equal(linkStatusLookupObservation{
		PageID: pageID,
		Path:   routePath,
	}))
}

type linkStatusLookupObservation struct {
	PageID tree.PageID
	Path   tree.RoutePath
}

func observedLinkStatusLookup(svc *recordingLinkStatusService) linkStatusLookupObservation {
	return linkStatusLookupObservation{
		PageID: svc.seenPageID,
		Path:   svc.seenPath,
	}
}

type linkStatusUseCaseDependencies struct {
	Links any
	Tree  any
}

func matchLinkStatusUseCaseDependencies(linkSvc any, treeSvc any) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(uc *GetLinkStatusUseCase) linkStatusUseCaseDependencies {
		if uc == nil {
			return linkStatusUseCaseDependencies{}
		}
		return linkStatusUseCaseDependencies{Links: uc.links, Tree: uc.tree}
	}, Equal(linkStatusUseCaseDependencies{Links: linkSvc, Tree: treeSvc}))
}

func matchBacklinksUseCaseDependencies(linkSvc any) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(uc *GetBacklinksUseCase) any {
		if uc == nil {
			return nil
		}
		return uc.links
	}, Equal(linkSvc))
}

func matchOutgoingLinksUseCaseDependencies(linkSvc any) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(uc *GetOutgoingLinksUseCase) any {
		if uc == nil {
			return nil
		}
		return uc.links
	}, Equal(linkSvc))
}

func matchReindexLinksUseCaseDependencies(linkSvc any) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(uc *ReindexLinksUseCase) any {
		if uc == nil {
			return nil
		}
		return uc.links
	}, Equal(linkSvc))
}

type recordingLinkReadService struct {
	backlinks    *corelinks.BacklinkResult
	outgoing     *corelinks.OutgoingResult
	backlinksErr error
	outgoingErr  error
}

func (svc *recordingLinkReadService) GetBacklinksForPage(tree.PageID) (*corelinks.BacklinkResult, error) {
	if svc.backlinksErr != nil {
		return nil, svc.backlinksErr
	}
	return svc.backlinks, nil
}

func (svc *recordingLinkReadService) GetOutgoingLinksForPage(tree.PageID) (*corelinks.OutgoingResult, error) {
	if svc.outgoingErr != nil {
		return nil, svc.outgoingErr
	}
	return svc.outgoing, nil
}

type recordingLinkReindexService struct {
	err error
}

func (svc recordingLinkReindexService) IndexAllPages() error {
	return svc.err
}
