package pages

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/core/assets"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/markdown"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/links"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

type routesSpecDeps struct {
	tree   *tree.TreeService
	routes *Routes
}

type routePageJSON struct {
	ID         tree.PageID       `json:"id"`
	Title      string            `json:"title"`
	Slug       tree.Slug         `json:"slug"`
	Path       tree.RoutePath    `json:"path"`
	Version    tree.PageVersion  `json:"version"`
	Content    string            `json:"content"`
	Kind       tree.NodeKind     `json:"kind"`
	Tags       []string          `json:"tags"`
	Properties map[string]string `json:"properties"`
}

type routeSuccessJSON struct {
	MessageID sharederrors.MessageID `json:"messageId"`
	Message   string                 `json:"message"`
}

func matchRoutePage(fields gstruct.Fields) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchExistingRoutePathLookup(path tree.RoutePath) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Path":   Equal(path),
		"Exists": BeTrue(),
	})
}

func matchRoutePermalinkTarget(id tree.PageID, path tree.RoutePath) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"ID": Equal(id),
		"Path": WithTransform(func(raw string) tree.RoutePath {
			return tree.RoutePathFromString(raw)
		}, Equal(path)),
	})
}

func matchRouteSuccessMessage(messageID sharederrors.MessageID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"MessageID": Equal(messageID),
	})
}

var _ = ginkgo.Describe("page route handlers", func() {
	ginkgo.BeforeEach(func() {
		gin.SetMode(gin.TestMode)
	})

	ginkgo.It("serves tree, page, path, lookup, permalink, and slug suggestion reads", func() {
		deps := newRoutesSpecDeps()
		docs := deps.createPage("Docs", "docs", tree.NodeKindSection, nil)
		guide := deps.createPage("Guide", "guide", tree.NodeKindPage, &docs.ID)

		rec := performRoutesRequest(http.MethodGet, "/api/tree?depth=bad", "", nil, nil, deps.routes.handleGetTree)
		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusOK),
			HaveHTTPBody(ContainSubstring(`"title":"Docs"`)),
		))

		rec = performRoutesRequest(http.MethodGet, pageRouteTarget(guide.ID), "", pageIDRouteParams(guide.ID), nil, deps.routes.handleGetPage)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		page := decodeRoutesJSON[routePageJSON](rec)
		Expect(page).To(matchRoutePage(gstruct.Fields{
			"Title": Equal("Guide"),
			"Path":  Equal(tree.RoutePath("docs/guide")),
		}))

		rec = performRoutesRequest(http.MethodGet, "/api/pages/by-path?path=docs/guide&kind=page", "", nil, nil, deps.routes.handleGetByPath)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		page = decodeRoutesJSON[routePageJSON](rec)
		Expect(page).To(matchRoutePage(gstruct.Fields{
			"ID": Equal(guide.ID),
		}))

		rec = performRoutesRequest(http.MethodGet, "/api/pages/lookup?path=docs/guide&kind=page", "", nil, nil, deps.routes.handleLookupPath)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		lookup := decodeRoutesJSON[tree.PathLookup](rec)
		Expect(lookup).To(matchExistingRoutePathLookup(tree.RoutePath("docs/guide")))

		rec = performRoutesRequest(http.MethodGet, permalinkRouteTarget(guide.ID), "", pageIDRouteParams(guide.ID), nil, deps.routes.handleResolvePermalink)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		target := decodeRoutesJSON[tree.PermalinkTarget](rec)
		Expect(target).To(matchRoutePermalinkTarget(guide.ID, tree.RoutePath("docs/guide")))

		rec = performRoutesRequest(http.MethodGet, slugSuggestionRouteTarget{parentID: docs.ID, currentID: guide.ID}, "", nil, nil, deps.routes.handleSuggestSlug)
		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusOK),
			HaveHTTPBody(ContainSubstring(`"slug":"guide"`)),
		))
	})

	ginkgo.It("creates and updates pages through JSON handlers with public metadata", func() {
		deps := newRoutesSpecDeps()

		rec := performRoutesRequest(http.MethodPost, "/api/pages", `{"title":"Route Page","slug":"route-page","kind":"page"}`, nil, routesSpecUser(), deps.routes.handleCreate)
		Expect(rec).To(HaveHTTPStatus(http.StatusCreated))
		created := decodeRoutesJSON[routePageJSON](rec)
		Expect(created).To(matchRoutePage(gstruct.Fields{
			"Title": Equal("Route Page"),
			"Kind":  Equal(tree.NodeKindPage),
		}))

		rec = performRoutesRequest(
			http.MethodPut,
			pageRouteTarget(created.ID),
			routeBodyVersion(`{"version":"`, created.Version, `","title":"Route Page Updated","slug":"route-page","content":"Body","tags":["Docs","Published"],"properties":{"status":"draft"}}`),
			pageIDRouteParams(created.ID),
			routesSpecUser(),
			deps.routes.handleUpdate,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		updated := decodeRoutesJSON[routePageJSON](rec)
		Expect(updated).To(matchRoutePage(gstruct.Fields{
			"Title":      Equal("Route Page Updated"),
			"Content":    Equal("Body"),
			"Tags":       Equal([]string{"docs", "published"}),
			"Properties": Equal(map[string]string{"status": "draft"}),
		}))

		rendered, err := BuildMarkdownWithPublicMetadata("page-1", " Page ", []string{"One", "one"}, map[string]string{"status": "done"}, "Rendered body")
		Expect(err).NotTo(HaveOccurred())
		doc, _, err := markdown.ParsePageDocument(rendered)
		Expect(err).NotTo(HaveOccurred())
		Expect(doc).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Body": Equal("Rendered body"),
			"Metadata": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Page": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"Title": Equal("Page"),
				}),
				"Tags":   Equal([]string{"one"}),
				"Fields": Equal(map[string]interface{}{"status": "done"}),
			}),
		}))
	})

	ginkgo.It("drives mutating handlers for ensure, copy, move, sort, convert, and delete", func() {
		deps := newRoutesSpecDeps()
		source := deps.createPage("Source", "source", tree.NodeKindPage, nil)
		archive := deps.createPage("Archive", "archive", tree.NodeKindSection, nil)

		rec := performRoutesRequest(http.MethodPost, "/api/pages/ensure", `{"path":"docs/created","title":"Created","kind":"page"}`, nil, routesSpecUser(), deps.routes.handleEnsurePath)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		ensured := decodeRoutesJSON[routePageJSON](rec)
		Expect(ensured).To(matchRoutePage(gstruct.Fields{
			"Path": Equal(tree.RoutePath("docs/created")),
		}))

		rec = performRoutesRequest(
			http.MethodPost,
			copyPageRouteTarget(source.ID),
			`{"title":"Copy","slug":"copy"}`,
			pageIDRouteParams(source.ID),
			routesSpecUser(),
			deps.routes.handleCopy,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusCreated))
		copied := decodeRoutesJSON[routePageJSON](rec)

		rec = performRoutesRequest(
			http.MethodPut,
			pageRouteTargetWithSuffix(copied.ID, "/move"),
			routeMoveBody(copied.Version, archive.ID),
			pageIDRouteParams(copied.ID),
			routesSpecUser(),
			deps.routes.handleMove,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		moveMessage := decodeRoutesJSON[routeSuccessJSON](rec)
		Expect(moveMessage).To(matchRouteSuccessMessage(MessageIDAPIPagesMoveSuccess))

		rec = performRoutesRequest(
			http.MethodPut,
			pageRouteTargetWithSuffix(archive.ID, "/sort"),
			routeSortBody(copied.ID),
			pageIDRouteParams(archive.ID),
			routesSpecUser(),
			deps.routes.handleSort,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		sortMessage := decodeRoutesJSON[routeSuccessJSON](rec)
		Expect(sortMessage).To(matchRouteSuccessMessage(MessageIDAPIPagesSortSuccess))

		copiedPage, err := deps.tree.GetPage(copied.ID)
		Expect(err).NotTo(HaveOccurred())
		rec = performRoutesRequest(
			http.MethodPost,
			convertPageRouteTarget(copiedPage.ID),
			routeBodyVersion(`{"targetKind":"section","version":"`, copiedPage.Version(), `"}`),
			pageIDRouteParams(copied.ID),
			routesSpecUser(),
			deps.routes.handleConvert,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusNoContent))

		copiedPage, err = deps.tree.GetPage(copied.ID)
		Expect(err).NotTo(HaveOccurred())
		rec = performRoutesRequest(
			http.MethodDelete,
			pageRouteTargetWithVersion(copiedPage.ID, copiedPage.Version()),
			"",
			pageIDRouteParams(copied.ID),
			routesSpecUser(),
			deps.routes.handleDelete,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		deleteMessage := decodeRoutesJSON[routeSuccessJSON](rec)
		Expect(deleteMessage).To(matchRouteSuccessMessage(MessageIDAPIPagesDeleteSuccess))
	})

	ginkgo.It("wires refactor preview and apply handlers to the use cases", func() {
		deps := newRoutesSpecDeps()
		page := deps.createPage("Old", "old", tree.NodeKindPage, nil)

		rec := performRoutesRequest(
			http.MethodPost,
			refactorPreviewRouteTarget(page.ID),
			`{"kind":"rename","title":"New","slug":"new"}`,
			pageIDRouteParams(page.ID),
			nil,
			deps.routes.handleRefactorPreview,
		)
		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusOK),
			HaveHTTPBody(SatisfyAll(
				ContainSubstring(`"oldPath":"old"`),
				ContainSubstring(`"newPath":"/new"`),
			)),
		))

		rec = performRoutesRequest(
			http.MethodPost,
			refactorApplyRouteTarget(page.ID),
			routeBodyVersion(`{"version":"`, page.Version(), `","kind":"rename","title":"New","slug":"new","rewriteLinks":false}`),
			pageIDRouteParams(page.ID),
			routesSpecUser(),
			deps.routes.handleRefactorApply,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		renamed := decodeRoutesJSON[routePageJSON](rec)
		Expect(renamed).To(matchRoutePage(gstruct.Fields{
			"Title": Equal("New"),
			"Path":  Equal(tree.RoutePath("new")),
		}))
	})

	ginkgo.It("returns structured errors for route validation failures", func() {
		deps := newRoutesSpecDeps()
		page := deps.createPage("Convertible", "convertible", tree.NodeKindPage, nil)

		rec := performRoutesRequest(http.MethodGet, "/api/pages/by-path", "", nil, nil, deps.routes.handleGetByPath)
		Expect(rec).To(HavePageErrorResponse(http.StatusBadRequest, ErrCodePageMissingPath), rec.Body.String())

		rec = performRoutesRequest(http.MethodGet, "/api/pages/permalink/%20", "", gin.Params{{Key: "id", Value: " "}}, nil, deps.routes.handleResolvePermalink)
		Expect(rec).To(HavePageErrorResponse(http.StatusBadRequest, ErrCodePageMissingID), rec.Body.String())

		rec = performRoutesRequest(http.MethodGet, "/api/pages/slug-suggestion?title=%20%20%20", "", nil, nil, deps.routes.handleSuggestSlug)
		Expect(rec).To(HavePageErrorResponse(http.StatusBadRequest, ErrCodePageMissingTitle), rec.Body.String())

		rec = performRoutesRequest(http.MethodPost, "/api/pages", `{`, nil, routesSpecUser(), deps.routes.handleCreate)
		Expect(rec).To(HavePageErrorResponse(http.StatusBadRequest, ErrCodePageInvalidRequest), rec.Body.String())

		rec = performRoutesRequest(
			http.MethodPost,
			convertPageRouteTarget(page.ID),
			routeBodyVersion(`{"targetKind":"folder","version":"`, page.Version(), `"}`),
			pageIDRouteParams(page.ID),
			routesSpecUser(),
			deps.routes.handleConvert,
		)
		Expect(rec).To(HavePageErrorResponse(http.StatusBadRequest, ErrCodePageInvalidTargetKind), rec.Body.String())
	})
})

func newRoutesSpecDeps() *routesSpecDeps {
	ginkgo.GinkgoHelper()

	storageDir := pagesTempDir()
	treeService := tree.NewTreeService(storageDir)
	Expect(treeService.LoadTree()).To(Succeed())

	slugService := tree.NewSlugService()
	assetService := assets.NewAssetService(storageDir, slugService)
	linkStore, err := links.NewLinksStore(storageDir)
	Expect(err).NotTo(HaveOccurred())
	linkService := links.NewLinkService(storageDir, treeService, linkStore)
	ginkgo.DeferCleanup(func() {
		Expect(linkService.Close()).To(Succeed())
	})

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	orchestrator := pagesave.NewPageSaveOrchestrator()
	routes := NewRoutes(RoutesConfig{
		TreeService:      treeService,
		CreatePage:       NewCreatePageUseCase(treeService, slugService, orchestrator, log),
		UpdatePage:       NewUpdatePageUseCase(treeService, slugService, orchestrator, log),
		DeletePage:       NewDeletePageUseCase(treeService, assetService, orchestrator, log),
		MovePage:         NewMovePageUseCase(treeService, orchestrator, log),
		ConvertPage:      NewConvertPageUseCase(treeService, orchestrator, log),
		CopyPage:         NewCopyPageUseCase(treeService, slugService, orchestrator, assetService, log),
		GetPage:          NewGetPageUseCase(treeService),
		FindByPath:       NewFindByPathUseCase(treeService),
		LookupPath:       NewLookupPagePathUseCase(treeService),
		ResolvePermalink: NewResolvePermalinkUseCase(treeService),
		SortPages:        NewSortPagesUseCase(treeService),
		EnsurePath:       NewEnsurePathUseCase(treeService, slugService, orchestrator, log),
		SuggestSlug:      NewSuggestSlugUseCase(treeService, slugService),
		PreviewRefactor:  NewPreviewPageRefactorUseCase(treeService, slugService, linkService, log),
		ApplyRefactor:    NewApplyPageRefactorUseCaseWithOrchestrator(treeService, slugService, linkService, orchestrator, log),
	})

	return &routesSpecDeps{tree: treeService, routes: routes}
}

func (d *routesSpecDeps) createPage(title string, slug string, kind tree.NodeKind, parentID *tree.PageID) *tree.Page {
	ginkgo.GinkgoHelper()

	out, err := d.routes.createPage.Execute(context.Background(), CreatePageInput{
		UserID:   tree.UserIDFromString("routes-test-user"),
		ParentID: parentID,
		Title:    title,
		Slug:     tree.SlugFromString(slug),
		Kind:     &kind,
	})
	Expect(err).NotTo(HaveOccurred())
	return out.Page
}

func performRoutesRequest(
	method string,
	target any,
	body any,
	params gin.Params,
	user *coreauth.User,
	handler func(*gin.Context),
) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	bodyReader, hasBody := newRoutesBodyReader(body)
	c.Request = newRoutesHTTPRequest(method, target, bodyReader)
	if hasBody {
		c.Request.Header.Set("Content-Type", "application/json")
	}
	c.Params = params
	if user != nil {
		c.Set("user", user)
	}

	handler(c)
	c.Writer.WriteHeaderNow()
	return rec
}

type pageIDRouteTarget struct {
	prefix string
	id     tree.PageID
	suffix string
}

type pageIDVersionRouteTarget struct {
	id      tree.PageID
	version tree.PageVersion
}

type slugSuggestionRouteTarget struct {
	parentID  tree.PageID
	currentID tree.PageID
}

type routeBodyWithVersion struct {
	prefix  string
	version tree.PageVersion
	suffix  string
}

type routeMoveRequestBody struct {
	version  tree.PageVersion
	parentID tree.PageID
}

type routeSortRequestBody struct {
	orderedIDs []tree.PageID
}

func newRoutesHTTPRequest(method string, target any, body *strings.Reader) *http.Request {
	ginkgo.GinkgoHelper()

	switch t := target.(type) {
	case string:
		return httptest.NewRequest(method, t, body)
	case pageIDRouteTarget:
		return httptest.NewRequest(method, t.prefix+t.id.String()+t.suffix, body)
	case pageIDVersionRouteTarget:
		return httptest.NewRequest(method, "/api/pages/"+t.id.String()+"?version="+t.version.String(), body)
	case slugSuggestionRouteTarget:
		return httptest.NewRequest(method, "/api/pages/slug-suggestion?title=Guide&parentId="+t.parentID.String()+"&currentId="+t.currentID.String(), body)
	default:
		panic("unsupported routes request target")
	}
}

func newRoutesBodyReader(body any) (*strings.Reader, bool) {
	ginkgo.GinkgoHelper()

	switch b := body.(type) {
	case nil:
		return strings.NewReader(""), false
	case string:
		return strings.NewReader(b), b != ""
	case routeBodyWithVersion:
		return strings.NewReader(b.prefix + b.version.String() + b.suffix), true
	case routeMoveRequestBody:
		return strings.NewReader(`{"version":"` + b.version.String() + `","parentId":"` + b.parentID.String() + `"}`), true
	case routeSortRequestBody:
		payload, err := json.Marshal(struct {
			OrderedIDs []tree.PageID `json:"orderedIds"`
		}{OrderedIDs: b.orderedIDs})
		Expect(err).To(Succeed())
		return strings.NewReader(string(payload)), true
	default:
		panic("unsupported routes request body")
	}
}

func pageRouteTarget(id tree.PageID) pageIDRouteTarget {
	return pageIDRouteTarget{prefix: "/api/pages/", id: id}
}

func pageRouteTargetWithSuffix(id tree.PageID, suffix string) pageIDRouteTarget {
	return pageIDRouteTarget{prefix: "/api/pages/", id: id, suffix: suffix}
}

func pageRouteTargetWithVersion(id tree.PageID, version tree.PageVersion) pageIDVersionRouteTarget {
	return pageIDVersionRouteTarget{id: id, version: version}
}

func convertPageRouteTarget(id tree.PageID) pageIDRouteTarget {
	return pageIDRouteTarget{prefix: "/api/pages/convert/", id: id}
}

func copyPageRouteTarget(id tree.PageID) pageIDRouteTarget {
	return pageIDRouteTarget{prefix: "/api/pages/copy/", id: id}
}

func permalinkRouteTarget(id tree.PageID) pageIDRouteTarget {
	return pageIDRouteTarget{prefix: "/api/pages/permalink/", id: id}
}

func refactorPreviewRouteTarget(id tree.PageID) pageIDRouteTarget {
	return pageIDRouteTarget{prefix: "/api/pages/", id: id, suffix: "/refactor/preview"}
}

func refactorApplyRouteTarget(id tree.PageID) pageIDRouteTarget {
	return pageIDRouteTarget{prefix: "/api/pages/", id: id, suffix: "/refactor/apply"}
}

func routeBodyVersion(prefix string, version tree.PageVersion, suffix string) routeBodyWithVersion {
	return routeBodyWithVersion{prefix: prefix, version: version, suffix: suffix}
}

func routeMoveBody(version tree.PageVersion, parentID tree.PageID) routeMoveRequestBody {
	return routeMoveRequestBody{version: version, parentID: parentID}
}

func routeSortBody(orderedIDs ...tree.PageID) routeSortRequestBody {
	return routeSortRequestBody{orderedIDs: orderedIDs}
}

func pageIDRouteParams(id tree.PageID) gin.Params {
	return gin.Params{{Key: "id", Value: id.String()}}
}

func routesSpecUser() *coreauth.User {
	return &coreauth.User{
		ID:       "routes-test-user",
		Username: "routes-test-user",
		Role:     coreauth.RoleEditor,
	}
}

func decodeRoutesJSON[T any](rec *httptest.ResponseRecorder) T {
	ginkgo.GinkgoHelper()

	var out T
	Expect(json.Unmarshal(rec.Body.Bytes(), &out)).To(Succeed(), rec.Body.String())
	return out
}
