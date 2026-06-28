package pages

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/assets"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/links"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

type routesSpecDeps struct {
	tree   *tree.TreeService
	routes *Routes
}

type routePageJSON struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Slug       string            `json:"slug"`
	Path       string            `json:"path"`
	Version    string            `json:"version"`
	Content    string            `json:"content"`
	Kind       tree.NodeKind     `json:"kind"`
	Tags       []string          `json:"tags"`
	Properties map[string]string `json:"properties"`
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
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(rec.Body.String()).To(ContainSubstring(`"title":"Docs"`))

		rec = performRoutesRequest(http.MethodGet, "/api/pages/"+guide.ID.String(), "", gin.Params{{Key: "id", Value: guide.ID.String()}}, nil, deps.routes.handleGetPage)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		page := decodeRoutesJSON[routePageJSON](rec)
		Expect(page.Title).To(Equal("Guide"))
		Expect(page.Path).To(Equal("docs/guide"))

		rec = performRoutesRequest(http.MethodGet, "/api/pages/by-path?path=docs/guide&kind=page", "", nil, nil, deps.routes.handleGetByPath)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		page = decodeRoutesJSON[routePageJSON](rec)
		Expect(page.ID).To(Equal(guide.ID.String()))

		rec = performRoutesRequest(http.MethodGet, "/api/pages/lookup?path=docs/guide&kind=page", "", nil, nil, deps.routes.handleLookupPath)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		lookup := decodeRoutesJSON[tree.PathLookup](rec)
		Expect(lookup.Path).To(Equal(tree.RoutePath("docs/guide")))
		Expect(lookup.Exists).To(BeTrue())

		rec = performRoutesRequest(http.MethodGet, "/api/pages/permalink/"+guide.ID.String(), "", gin.Params{{Key: "id", Value: guide.ID.String()}}, nil, deps.routes.handleResolvePermalink)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		target := decodeRoutesJSON[tree.PermalinkTarget](rec)
		Expect(target.ID).To(Equal(guide.ID))
		Expect(target.Path).To(Equal("docs/guide"))

		rec = performRoutesRequest(http.MethodGet, "/api/pages/slug-suggestion?title=Guide&parentId="+docs.ID.String()+"&currentId="+guide.ID.String(), "", nil, nil, deps.routes.handleSuggestSlug)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(rec.Body.String()).To(ContainSubstring(`"slug":"guide"`))
	})

	ginkgo.It("creates and updates pages through JSON handlers with public metadata", func() {
		deps := newRoutesSpecDeps()

		rec := performRoutesRequest(http.MethodPost, "/api/pages", `{"title":"Route Page","slug":"route-page","kind":"page"}`, nil, routesSpecUser(), deps.routes.handleCreate)
		Expect(rec.Code).To(Equal(http.StatusCreated), rec.Body.String())
		created := decodeRoutesJSON[routePageJSON](rec)
		Expect(created.Title).To(Equal("Route Page"))
		Expect(created.Kind).To(Equal(tree.NodeKindPage))

		rec = performRoutesRequest(
			http.MethodPut,
			"/api/pages/"+created.ID,
			`{"version":"`+created.Version+`","title":"Route Page Updated","slug":"route-page","content":"Body","tags":["Docs","Published"],"properties":{"status":"draft"}}`,
			gin.Params{{Key: "id", Value: created.ID}},
			routesSpecUser(),
			deps.routes.handleUpdate,
		)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		updated := decodeRoutesJSON[routePageJSON](rec)
		Expect(updated.Title).To(Equal("Route Page Updated"))
		Expect(updated.Content).To(Equal("Body"))
		Expect(updated.Tags).To(Equal([]string{"docs", "published"}))
		Expect(updated.Properties).To(Equal(map[string]string{"status": "draft"}))

		rendered, err := BuildMarkdownWithPublicMetadata("page-1", " Page ", []string{"One", "one"}, map[string]string{"status": "done"}, "Rendered body")
		Expect(err).NotTo(HaveOccurred())
		doc, _, err := markdown.ParsePageDocument(rendered)
		Expect(err).NotTo(HaveOccurred())
		Expect(doc.Body).To(Equal("Rendered body"))
		Expect(doc.Metadata.Page.Title).To(Equal("Page"))
		Expect(doc.Metadata.Tags).To(Equal([]string{"one"}))
		Expect(doc.Metadata.Fields).To(Equal(map[string]interface{}{"status": "done"}))
	})

	ginkgo.It("drives mutating handlers for ensure, copy, move, sort, convert, and delete", func() {
		deps := newRoutesSpecDeps()
		source := deps.createPage("Source", "source", tree.NodeKindPage, nil)
		archive := deps.createPage("Archive", "archive", tree.NodeKindSection, nil)

		rec := performRoutesRequest(http.MethodPost, "/api/pages/ensure", `{"path":"docs/created","title":"Created","kind":"page"}`, nil, routesSpecUser(), deps.routes.handleEnsurePath)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		ensured := decodeRoutesJSON[routePageJSON](rec)
		Expect(ensured.Path).To(Equal("docs/created"))

		rec = performRoutesRequest(
			http.MethodPost,
			"/api/pages/copy/"+source.ID.String(),
			`{"title":"Copy","slug":"copy"}`,
			gin.Params{{Key: "id", Value: source.ID.String()}},
			routesSpecUser(),
			deps.routes.handleCopy,
		)
		Expect(rec.Code).To(Equal(http.StatusCreated), rec.Body.String())
		copied := decodeRoutesJSON[routePageJSON](rec)

		rec = performRoutesRequest(
			http.MethodPut,
			"/api/pages/"+copied.ID+"/move",
			`{"version":"`+copied.Version+`","parentId":"`+archive.ID.String()+`"}`,
			gin.Params{{Key: "id", Value: copied.ID}},
			routesSpecUser(),
			deps.routes.handleMove,
		)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(rec.Body.String()).To(ContainSubstring(string(MessageIDAPIPagesMoveSuccess)))

		rec = performRoutesRequest(
			http.MethodPut,
			"/api/pages/"+archive.ID.String()+"/sort",
			`{"orderedIds":["`+copied.ID+`"]}`,
			gin.Params{{Key: "id", Value: archive.ID.String()}},
			routesSpecUser(),
			deps.routes.handleSort,
		)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(rec.Body.String()).To(ContainSubstring(string(MessageIDAPIPagesSortSuccess)))

		copiedPage, err := deps.tree.GetPage(tree.PageIDFromString(copied.ID))
		Expect(err).NotTo(HaveOccurred())
		rec = performRoutesRequest(
			http.MethodPost,
			"/api/pages/convert/"+copied.ID,
			`{"targetKind":"section","version":"`+copiedPage.Version().String()+`"}`,
			gin.Params{{Key: "id", Value: copied.ID}},
			routesSpecUser(),
			deps.routes.handleConvert,
		)
		Expect(rec.Code).To(Equal(http.StatusNoContent), rec.Body.String())

		copiedPage, err = deps.tree.GetPage(tree.PageIDFromString(copied.ID))
		Expect(err).NotTo(HaveOccurred())
		rec = performRoutesRequest(
			http.MethodDelete,
			"/api/pages/"+copied.ID+"?version="+copiedPage.Version().String(),
			"",
			gin.Params{{Key: "id", Value: copied.ID}},
			routesSpecUser(),
			deps.routes.handleDelete,
		)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(rec.Body.String()).To(ContainSubstring(string(MessageIDAPIPagesDeleteSuccess)))
	})

	ginkgo.It("wires refactor preview and apply handlers to the use cases", func() {
		deps := newRoutesSpecDeps()
		page := deps.createPage("Old", "old", tree.NodeKindPage, nil)

		rec := performRoutesRequest(
			http.MethodPost,
			"/api/pages/"+page.ID.String()+"/refactor/preview",
			`{"kind":"rename","title":"New","slug":"new"}`,
			gin.Params{{Key: "id", Value: page.ID.String()}},
			nil,
			deps.routes.handleRefactorPreview,
		)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(rec.Body.String()).To(ContainSubstring(`"oldPath":"old"`))
		Expect(rec.Body.String()).To(ContainSubstring(`"newPath":"/new"`))

		rec = performRoutesRequest(
			http.MethodPost,
			"/api/pages/"+page.ID.String()+"/refactor/apply",
			`{"version":"`+page.Version().String()+`","kind":"rename","title":"New","slug":"new","rewriteLinks":false}`,
			gin.Params{{Key: "id", Value: page.ID.String()}},
			routesSpecUser(),
			deps.routes.handleRefactorApply,
		)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		renamed := decodeRoutesJSON[routePageJSON](rec)
		Expect(renamed.Title).To(Equal("New"))
		Expect(renamed.Path).To(Equal("new"))
	})

	ginkgo.It("returns structured errors for route validation failures", func() {
		deps := newRoutesSpecDeps()
		page := deps.createPage("Convertible", "convertible", tree.NodeKindPage, nil)

		rec := performRoutesRequest(http.MethodGet, "/api/pages/by-path", "", nil, nil, deps.routes.handleGetByPath)
		expectPageErrorResponse(rec, http.StatusBadRequest, ErrCodePageMissingPath)

		rec = performRoutesRequest(http.MethodGet, "/api/pages/permalink/%20", "", gin.Params{{Key: "id", Value: " "}}, nil, deps.routes.handleResolvePermalink)
		expectPageErrorResponse(rec, http.StatusBadRequest, ErrCodePageMissingID)

		rec = performRoutesRequest(http.MethodGet, "/api/pages/slug-suggestion?title=%20%20%20", "", nil, nil, deps.routes.handleSuggestSlug)
		expectPageErrorResponse(rec, http.StatusBadRequest, ErrCodePageMissingTitle)

		rec = performRoutesRequest(http.MethodPost, "/api/pages", `{`, nil, routesSpecUser(), deps.routes.handleCreate)
		expectPageErrorResponse(rec, http.StatusBadRequest, ErrCodePageInvalidRequest)

		rec = performRoutesRequest(
			http.MethodPost,
			"/api/pages/convert/"+page.ID.String(),
			`{"targetKind":"folder","version":"`+page.Version().String()+`"}`,
			gin.Params{{Key: "id", Value: page.ID.String()}},
			routesSpecUser(),
			deps.routes.handleConvert,
		)
		expectPageErrorResponse(rec, http.StatusBadRequest, ErrCodePageInvalidTargetKind)
	})
})

func newRoutesSpecDeps() *routesSpecDeps {
	ginkgo.GinkgoHelper()

	storageDir := ginkgo.GinkgoT().TempDir()
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
	target string,
	body string,
	params gin.Params,
	user *coreauth.User,
	handler func(*gin.Context),
) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, target, bytes.NewBufferString(body))
	if body != "" {
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
