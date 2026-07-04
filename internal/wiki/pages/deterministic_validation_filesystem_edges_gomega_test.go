package pages

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"syscall"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/gin-gonic/gin"
	"github.com/perber/wiki/internal/core/assets"
	"github.com/perber/wiki/internal/core/markdown"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

var _ = ginkgo.Describe("deterministic page helper edges", func() {
	ginkgo.It("returns validation README fallback and route error outcomes", ginkgo.Label("integration"), func() {
		deps := newRoutesSpecDeps()

		_, _, err := NormalizePagePathInput("docs/page.md", "folder")
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidKind))
		_, _, err = NormalizePagePathInput("../escape.md", "")
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidPath))
		badParent := " parent "
		_, err = ValidateOptionalParentID(&badParent)
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidParentID))

		_, err = requireReadmeMarkdownPathFallback("../README.md", tree.NodeKindSection, ReadmeMarkdownPathFallbackLookup{})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidPath))
		_, err = requireReadmeMarkdownPathFallback("../README.md", tree.NodeKindPage, ReadmeMarkdownPathFallbackLookup{})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidPath))
		rootDir := pagesTempDir()
		Expect(os.WriteFile(filepath.Join(rootDir, "README.md"), []byte("# Root"), 0o644)).To(Succeed())
		rootLookupErr := errors.New("root lookup failed")
		_, err = requireReadmeMarkdownPathFallback("README.md", tree.NodeKindSection, ReadmeMarkdownPathFallbackLookup{
			RootDir: rootDir,
			RootPage: func() (*tree.Page, error) {
				return nil, rootLookupErr
			},
		})
		Expect(err).To(MatchError(rootLookupErr))

		rec := performRoutesRequest(http.MethodGet, "/api/pages/by-path?path=missing", "", nil, nil, deps.routes.handleGetByPath)
		Expect(rec).To(HavePageErrorResponse(http.StatusNotFound, ErrCodePageNotFound), rec.Body.String())
		rec = performRoutesRequest(http.MethodGet, "/api/pages/by-path?path=docs/page.md&kind=section", "", nil, nil, deps.routes.handleGetByPath)
		Expect(rec).To(HavePageErrorResponse(http.StatusBadRequest, ErrCodePageInvalidKind), rec.Body.String())
		rec = performRoutesRequest(http.MethodGet, "/api/pages/permalink/missing", "", ginParams("id", "missing"), nil, deps.routes.handleResolvePermalink)
		Expect(rec).To(HavePageErrorResponse(http.StatusNotFound, ErrCodePageNotFound), rec.Body.String())
		unloadedTree := tree.NewTreeService(pagesTempDir())
		lookupUC := NewLookupPagePathUseCase(unloadedTree)
		_, err = lookupUC.Execute(context.Background(), LookupPagePathInput{Path: "missing"})
		Expect(err).To(MatchError(tree.ErrTreeNotLoaded))
		rec = performRoutesRequest(http.MethodGet, "/api/pages/lookup?path=missing", "", nil, nil, (&Routes{lookupPath: lookupUC}).handleLookupPath)
		Expect(rec).To(HavePageErrorResponse(http.StatusInternalServerError, ErrCodePageInternalError), rec.Body.String())

		rec = performRoutesRequest(http.MethodPost, "/api/pages", `{"title":"Route","slug":"route","kind":"folder"}`, nil, routesSpecUser(), deps.routes.handleCreate)
		Expect(rec).To(HavePageErrorResponse(http.StatusBadRequest, ErrCodePageInvalidKind), rec.Body.String())
		rec = performRoutesRequest(http.MethodPost, "/api/pages", `{"title":"Route","slug":"bad slug","kind":"page"}`, nil, routesSpecUser(), deps.routes.handleCreate)
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), rec.Body.String())
		Expect(rec).To(HaveHTTPBody(ContainSubstring(pageValidationErrorCode)))

		page := deps.createPage("Route Error", "route-error", tree.NodeKindPage, nil)
		rec = performRoutesRequest(
			http.MethodPut,
			pageRouteTarget(page.ID),
			routeBodyVersion(`{"version":"`, page.Version(), `","title":"Route Error","slug":"route-error","tags":[" spaced "]}`),
			ginPageIDParams(page.ID),
			routesSpecUser(),
			deps.routes.handleUpdate,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), rec.Body.String())
		Expect(rec).To(HaveHTTPBody(ContainSubstring(pageValidationErrorCode)))

		rec = performRoutesRequest(
			http.MethodPut,
			"/api/pages/missing",
			`{"version":"stale","title":"Missing","slug":"missing"}`,
			ginParams("id", "missing"),
			routesSpecUser(),
			deps.routes.handleUpdate,
		)
		Expect(rec).To(HavePageErrorResponse(http.StatusNotFound, ErrCodePageNotFound), rec.Body.String())
		rec = performRoutesRequest(http.MethodPost, "/api/pages/ensure", `{"path":"../escape","title":"Route","kind":"page"}`, nil, routesSpecUser(), deps.routes.handleEnsurePath)
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), rec.Body.String())
		Expect(rec).To(HaveHTTPBody(ContainSubstring(pageValidationErrorCode)))

		for _, tc := range []struct {
			name    string
			method  string
			target  string
			body    string
			params  gin.Params
			handler func(*gin.Context)
			status  int
			code    sharederrors.ErrorCode
		}{
			{name: "delete missing", method: http.MethodDelete, target: "/api/pages/missing?version=stale", params: ginParams("id", "missing"), handler: deps.routes.handleDelete, status: http.StatusNotFound, code: ErrCodePageNotFound},
			{name: "move missing", method: http.MethodPut, target: "/api/pages/missing/move", body: `{"version":"stale","parentId":"root"}`, params: ginParams("id", "missing"), handler: deps.routes.handleMove, status: http.StatusNotFound, code: ErrCodePageNotFound},
			{name: "sort missing", method: http.MethodPut, target: "/api/pages/missing/sort", body: `{"orderedIds":["missing"]}`, params: ginParams("id", "missing"), handler: deps.routes.handleSort, status: http.StatusNotFound, code: ErrCodePageParentNotFound},
			{name: "ensure invalid kind", method: http.MethodPost, target: "/api/pages/ensure", body: `{"path":"route","title":"Route","kind":"folder"}`, handler: deps.routes.handleEnsurePath, status: http.StatusBadRequest, code: ErrCodePageInvalidKind},
			{name: "convert missing", method: http.MethodPost, target: "/api/pages/convert/missing", body: `{"targetKind":"section","version":"stale"}`, params: ginParams("id", "missing"), handler: deps.routes.handleConvert, status: http.StatusNotFound, code: ErrCodePageNotFound},
			{name: "copy missing", method: http.MethodPost, target: "/api/pages/copy/missing", body: `{"title":"Missing","slug":"missing"}`, params: ginParams("id", "missing"), handler: deps.routes.handleCopy, status: http.StatusNotFound, code: ErrCodePageNotFound},
			{name: "refactor preview missing", method: http.MethodPost, target: "/api/pages/missing/refactor/preview", body: `{"kind":"rename","title":"Missing","slug":"missing"}`, params: ginParams("id", "missing"), handler: deps.routes.handleRefactorPreview, status: http.StatusNotFound, code: ErrCodePageNotFound},
			{name: "refactor apply missing", method: http.MethodPost, target: "/api/pages/missing/refactor/apply", body: `{"version":"stale","kind":"rename","title":"Missing","slug":"missing"}`, params: ginParams("id", "missing"), handler: deps.routes.handleRefactorApply, status: http.StatusNotFound, code: ErrCodePageNotFound},
		} {
			rec = performRoutesRequest(tc.method, tc.target, tc.body, tc.params, routesSpecUser(), tc.handler)
			Expect(rec).To(HavePageErrorResponse(tc.status, tc.code), rec.Body.String())
		}
	})

	ginkgo.It("returns filesystem metadata and asset mutation failures", ginkgo.Label("integration"), func() {
		deps := newRoutesSpecDeps()
		ctx := context.Background()
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		slug := tree.NewSlugService()

		section := deps.createPage("Route Section", "route-section", tree.NodeKindSection, nil)
		rec := performRoutesRequest(http.MethodGet, "/api/pages/by-path?path=route-section&kind=section", "", nil, nil, deps.routes.handleGetByPath)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		sectionJSON := decodeRoutesJSON[routePageJSON](rec)
		Expect(sectionJSON.ID).To(Equal(section.ID))

		docs := deps.createPage("Route Docs", "route-docs", tree.NodeKindSection, nil)
		readme := deps.createPage("README", "readme", tree.NodeKindPage, &docs.ID)
		readmeOut, err := deps.routes.findByPathInput(ctx, "route-docs/readme.md", tree.NodeKindPage)
		Expect(err).NotTo(HaveOccurred())
		Expect(readmeOut.Page.ID).To(Equal(readme.ID))
		Expect(os.WriteFile(filepath.Join(deps.tree.RootDir(), "README.md"), []byte("# Root README"), 0o644)).To(Succeed())
		rootOut, err := deps.routes.findByPathInput(ctx, "README.md", tree.NodeKindSection)
		Expect(err).NotTo(HaveOccurred())
		Expect(rootOut.Page.ID).To(Equal(tree.RootPageID))

		tagsOnly := deps.createPage("Tags Only", "tags-only", tree.NodeKindPage, nil)
		rec = performRoutesRequest(
			http.MethodPut,
			pageRouteTarget(tagsOnly.ID),
			routeBodyVersion(`{"version":"`, tagsOnly.Version(), `","title":"Tags Only","slug":"tags-only","tags":["ready"]}`),
			ginPageIDParams(tagsOnly.ID),
			routesSpecUser(),
			deps.routes.handleUpdate,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		updatedTagsOnly := decodeRoutesJSON[routePageJSON](rec)
		Expect(updatedTagsOnly.Tags).To(Equal([]string{"ready"}))

		rec = performRoutesRequest(http.MethodPut, pageRouteTarget(section.ID), `{`, ginPageIDParams(section.ID), routesSpecUser(), deps.routes.handleUpdate)
		Expect(rec).To(HavePageErrorResponse(http.StatusBadRequest, ErrCodePageInvalidRequest), rec.Body.String())

		missingRaw := deps.createPage("Missing Raw", "missing-raw", tree.NodeKindPage, nil)
		removePageMarkdown(deps, missingRaw)
		rec = performRoutesRequest(
			http.MethodPut,
			pageRouteTarget(missingRaw.ID),
			routeBodyVersion(`{"version":"`, missingRaw.Version(), `","title":"Missing Raw","slug":"missing-raw","tags":["ready"]}`),
			ginPageIDParams(missingRaw.ID),
			routesSpecUser(),
			deps.routes.handleUpdate,
		)
		Expect(rec).To(HavePageErrorResponse(http.StatusInternalServerError, ErrCodePageInternalError), rec.Body.String())

		malformedForParse := deps.createPage("Malformed Parse", "malformed-parse", tree.NodeKindPage, nil)
		writePageMarkdown(deps, malformedForParse, "<!-- leafwiki malformed\n-->\nbody")
		rec = performRoutesRequest(
			http.MethodPut,
			pageRouteTarget(malformedForParse.ID),
			routeBodyVersion(`{"version":"`, malformedForParse.Version(), `","title":"Malformed Parse","slug":"malformed-parse","tags":["ready"]}`),
			ginPageIDParams(malformedForParse.ID),
			routesSpecUser(),
			deps.routes.handleUpdate,
		)
		Expect(rec).To(HavePageErrorResponse(http.StatusInternalServerError, ErrCodePageInternalError), rec.Body.String())

		malformedForBuild := deps.createPage("Malformed Build", "malformed-build", tree.NodeKindPage, nil)
		writePageMarkdown(deps, malformedForBuild, "<!-- leafwiki malformed\n-->\nbody")
		rec = performRoutesRequest(
			http.MethodPut,
			pageRouteTarget(malformedForBuild.ID),
			routeBodyVersion(`{"version":"`, malformedForBuild.Version(), `","title":"Malformed Build","slug":"malformed-build","content":"Body","tags":["ready"]}`),
			ginPageIDParams(malformedForBuild.ID),
			routesSpecUser(),
			deps.routes.handleUpdate,
		)
		Expect(rec).To(HavePageErrorResponse(http.StatusInternalServerError, ErrCodePageInternalError), rec.Body.String())

		rendered, err := BuildMarkdownWithPublicMetadata("page-1", "Page", nil, nil, "Body")
		Expect(err).NotTo(HaveOccurred())
		doc, _, err := markdown.ParsePageDocument(rendered)
		Expect(err).NotTo(HaveOccurred())
		Expect(doc.Metadata.Fields).To(BeEmpty())

		err = ValidatePageMetadataInput([]string{""}, nil)
		Expect(err).To(HavePageValidationField("tags[0]"))
		_, _, err = ApplyMetadataPatch(nil, nil, MetadataPatch{SetProperties: map[string]string{"tags": "reserved"}})
		Expect(err).To(HavePageValidationField("properties.tags"))
		_, _, err = ApplyMetadataPatch(nil, nil, MetadataPatch{RemoveProperties: []string{"leafwiki_hidden"}})
		Expect(err).To(HavePageValidationField("removeProperties.leafwiki_hidden"))

		Expect(tree.ErrVersionConflict).To(HavePageErrorDetail(http.StatusConflict, ErrCodePageVersionConflict))
		Expect(pageErrorStatus(ErrCodePageNotFound)).To(Equal(http.StatusNotFound))

		rec = performRoutesRequest(http.MethodPost, "/api/pages/ensure", `{`, nil, routesSpecUser(), deps.routes.handleEnsurePath)
		Expect(rec).To(HavePageErrorResponse(http.StatusBadRequest, ErrCodePageInvalidRequest), rec.Body.String())
		rec = performRoutesRequest(http.MethodPost, "/api/pages/ensure", `{"path":"ensure-title","title":"   ","kind":"page"}`, nil, routesSpecUser(), deps.routes.handleEnsurePath)
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), rec.Body.String())
		Expect(rec).To(HaveHTTPBody(ContainSubstring(pageValidationErrorCode)))

		source := deps.createPage("Asset Source", "asset-source", tree.NodeKindPage, nil)
		assetService := assets.NewAssetService(deps.tree.RootDir(), slug)
		Expect(os.WriteFile(filepath.Join(assetService.GetAssetsDir(), source.ID.MetadataValue()), []byte("not a directory"), 0o644)).To(Succeed())
		_, err = NewCopyPageUseCase(deps.tree, slug, pagesave.NewPageSaveOrchestrator(), assetService, log).Execute(ctx, CopyPageInput{
			UserID:       tree.UserIDFromString("routes-test-user"),
			SourcePageID: source.ID,
			Title:        "Asset Copy",
			Slug:         tree.SlugFromString("asset-copy"),
		})
		Expect(err).To(MatchError(syscall.ENOTDIR))

		recursiveStale := deps.createPage("Recursive Stale", "recursive-stale", tree.NodeKindSection, nil)
		_ = deps.createPage("Recursive Stale Child", "recursive-stale-child", tree.NodeKindPage, &recursiveStale.ID)
		Expect(deps.routes.deletePage.Execute(ctx, DeletePageInput{ID: recursiveStale.ID, Version: tree.PageVersionFromString("stale"), Recursive: true})).To(MatchError(tree.ErrVersionConflict))
	})
})
