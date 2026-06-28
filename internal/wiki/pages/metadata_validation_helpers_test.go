package pages

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/markdown"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/http/dto"
)

var _ = ginkgo.Describe("metadata and validation helpers", func() {
	ginkgo.It("registers public, private, and refactor page route sets", func() {
		publicRouter := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{PublicAccess: true, DisableFrontendRoutes: true},
		)
		Expect(publicRouter).NotTo(BeNil())

		privateRouter := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{AuthDisabled: true, EnableLinkRefactor: true, DisableFrontendRoutes: true},
		)
		Expect(privateRouter).NotTo(BeNil())
	})

	ginkgo.It("enriches public page metadata from canonical page metadata", func() {
		page := &dto.Page{Node: &dto.Node{ID: "page-1"}}

		raw, err := markdown.RenderPageDocument(markdown.PageDocument{
			Body: "Body",
			Metadata: markdown.PageMetadata{
				Version: 1,
				Page:    markdown.PageMetadataPage{ID: "page-1", Title: "Page"},
				Tags:    []string{" Alpha ", "alpha", "Beta"},
				Fields: map[string]interface{}{
					"status":    " draft ",
					"count":     3,
					"title":     "reserved",
					"multiline": "one\ntwo",
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		EnrichPageMetadata(page, func(id tree.PageID) (string, error) {
			Expect(id).To(Equal(tree.PageIDFromString("page-1")))
			return raw, nil
		})

		Expect(page.Tags).To(Equal([]string{"alpha", "beta"}))
		Expect(page.Properties).To(Equal(map[string]string{"status": "draft"}))
	})

	ginkgo.It("extracts normalized tags and string properties from metadata", func() {
		tags := normalizeMetadataTags([]interface{}{"One", " one ", 42, "Two", ""})
		Expect(tags).To(Equal([]string{"one", "two"}))
		Expect(normalizeMetadataTags("not-a-list")).To(BeEmpty())

		extractedTags, properties := ExtractPageMetadataFromPageMetadata(markdown.PageMetadata{
			Tags: []string{"New", "new", "Done"},
			Fields: map[string]interface{}{
				"owner": " Alice ",
				"tags":  "reserved",
				"score": 7,
				"empty": "   ",
			},
		})

		Expect(extractedTags).To(Equal([]string{"new", "done"}))
		Expect(properties).To(Equal(map[string]string{"owner": "Alice"}))
	})

	ginkgo.It("handles metadata enrichment and patch no-op edges", func() {
		EnrichPageMetadata(nil, func(tree.PageID) (string, error) {
			ginkgo.Fail("readPageRaw should not be called for a nil API page")
			return "", nil
		})

		page := &dto.Page{Node: &dto.Node{ID: "page-1"}}
		EnrichPageMetadata(page, func(tree.PageID) (string, error) {
			return "", errors.New("read failed")
		})
		Expect(page.Tags).To(BeEmpty())
		Expect(page.Properties).To(BeEmpty())

		current, err := markdown.RenderPageDocument(markdown.PageDocument{
			Body: "Body",
			Metadata: markdown.PageMetadata{
				Version: 1,
				Page:    markdown.PageMetadataPage{ID: "page-1", Title: "Page"},
				Tags:    []string{"keep"},
				Fields: map[string]interface{}{
					"status": "draft",
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		rendered, err := BuildMarkdownWithPublicMetadataPatch(current, tree.PageIDFromString("page-1"), " Page ", PublicMetadataPatch{}, "Body")
		Expect(err).NotTo(HaveOccurred())
		doc, _, err := markdown.ParsePageDocument(rendered)
		Expect(err).NotTo(HaveOccurred())
		Expect(doc.Metadata.Tags).To(Equal([]string{"keep"}))
		Expect(doc.Metadata.Fields).To(Equal(map[string]interface{}{"status": "draft"}))

		meta := ApplyPublicMetadata(markdown.PageMetadata{Fields: map[string]interface{}{"status": "draft"}}, map[string]string{"status": "draft"}, nil, nil)
		Expect(meta.Fields).To(BeNil())
	})

	ginkgo.It("builds markdown with a public metadata patch while preserving private fields", func() {
		current, err := markdown.RenderPageDocument(markdown.PageDocument{
			Body: "Old body",
			Metadata: markdown.PageMetadata{
				Version: 1,
				Page:    markdown.PageMetadataPage{ID: "old-id", Title: "Old"},
				Tags:    []string{"old"},
				Fields: map[string]interface{}{
					"status":       "old",
					"private_flag": true,
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		rendered, err := BuildMarkdownWithPublicMetadataPatch(current, tree.PageIDFromString("page-2"), " New Title ", PublicMetadataPatch{
			TagsPresent:       true,
			Tags:              []string{"New", "new", "Done"},
			PropertiesPresent: true,
			Properties:        map[string]string{"status": "published"},
		}, "New body")
		Expect(err).NotTo(HaveOccurred())

		doc, _, err := markdown.ParsePageDocument(rendered)
		Expect(err).NotTo(HaveOccurred())
		Expect(doc.Body).To(Equal("New body"))
		Expect(doc.Metadata.Page.ID).To(Equal("page-2"))
		Expect(doc.Metadata.Page.Title).To(Equal("New Title"))
		Expect(doc.Metadata.Tags).To(Equal([]string{"new", "done"}))
		Expect(doc.Metadata.Fields).To(Equal(map[string]interface{}{
			"private_flag": true,
			"status":       "published",
		}))
	})

	ginkgo.It("applies partial metadata patches with normalized tags and validated property removals", func() {
		tags, properties, err := ApplyMetadataPatch(
			[]string{"Alpha", "beta"},
			map[string]string{"status": "draft", "owner": "alice"},
			MetadataPatch{
				AddTags:          []string{" Gamma ", "alpha"},
				RemoveTags:       []string{" beta "},
				SetProperties:    map[string]string{"status": "published"},
				RemoveProperties: []string{"owner"},
			},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(tags).To(Equal([]string{"alpha", "gamma"}))
		Expect(properties).To(Equal(map[string]string{"status": "published"}))

		_, _, err = ApplyMetadataPatch(nil, nil, MetadataPatch{RemoveProperties: []string{" leafwiki_hidden "}})
		Expect(err).To(HaveOccurred())
		var validationErr *sharederrors.ValidationErrors
		Expect(errors.As(err, &validationErr)).To(BeTrue())
	})

	ginkgo.It("validates and normalizes route path and page kind inputs", func() {
		routePath, kind, err := NormalizePagePathInput(" /Docs/Index.md ", "section")
		Expect(err).NotTo(HaveOccurred())
		Expect(routePath).To(Equal(tree.RoutePath("Docs")))
		Expect(kind).To(Equal(tree.NodeKindSection))

		_, _, err = NormalizePagePathInput("docs/page.md", "section")
		Expect(err).To(HaveOccurred())
		_, ok := sharederrors.AsLocalizedError(err)
		Expect(ok).To(BeTrue())

		kind, err = ValidatePageKind(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(kind).To(Equal(tree.NodeKindPage))

		_, err = ValidatePageRoutePath(" ")
		expectPageLocalizedCode(err, ErrCodePageMissingPath)

		_, err = ValidatePageRoutePath("../escape")
		expectPageLocalizedCode(err, ErrCodePageInvalidPath)

		Expect(MarkdownContentPathForRoute(tree.RoutePath("docs/page"), tree.NodeKindPage)).To(Equal(tree.MarkdownPath("docs/page.md")))

		validatedParent, err := ValidateMoveParentID("root")
		Expect(err).NotTo(HaveOccurred())
		Expect(validatedParent).To(Equal("root"))

		_, err = ValidateMoveParentID(" parent ")
		expectPageLocalizedCode(err, ErrCodePageInvalidParentID)

		parent := "parent-1"
		optionalParent, err := ValidateOptionalParentID(&parent)
		Expect(err).NotTo(HaveOccurred())
		Expect(optionalParent).NotTo(BeNil())
		Expect(*optionalParent).To(Equal("parent-1"))

		typedParent := tree.PageIDFromString("parent-1")
		optionalSemanticParent, err := ValidateOptionalSemanticParentID(&typedParent)
		Expect(err).NotTo(HaveOccurred())
		Expect(optionalSemanticParent).NotTo(BeNil())
		Expect(*optionalSemanticParent).To(Equal(typedParent))

		_, err = ValidateSemanticRoutePath(" ")
		var validationErr *sharederrors.ValidationErrors
		Expect(errors.As(err, &validationErr)).To(BeTrue())
		expectValidationField(validationErr, "path")

		_, err = ValidateRoutePathValue(tree.RoutePath(""))
		Expect(errors.As(err, &validationErr)).To(BeTrue())
		expectValidationField(validationErr, "path")

		id := tree.PageIDFromString("page-1")
		Expect(optionalPageIDString(nil)).To(BeNil())
		optionalID := optionalPageIDString(&id)
		Expect(optionalID).NotTo(BeNil())
		Expect(*optionalID).To(Equal("page-1"))
	})

	ginkgo.It("handles README markdown path fallback routing", func() {
		pageRoute, sectionRoute, ok := ReadmeMarkdownPathFallbackRoutes(" docs/README.md ")
		Expect(ok).To(BeTrue())
		Expect(pageRoute).To(Equal("docs/README"))
		Expect(sectionRoute).To(Equal("docs"))

		_, _, ok = ReadmeMarkdownPathFallbackRoutes("docs/page.md")
		Expect(ok).To(BeFalse())

		input, ok, err := NormalizeReadmeMarkdownPathFallbackInput("README.md", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(input.PageRoute).To(Equal("README"))
		Expect(input.SectionRoute).To(BeEmpty())
		Expect(input.TryPage).To(BeTrue())
		Expect(input.TrySection).To(BeTrue())

		input, ok, err = NormalizeReadmeMarkdownPathFallbackInput("docs/README.md", string(tree.NodeKindSection))
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(input.TryPage).To(BeFalse())
		Expect(input.TrySection).To(BeTrue())

		_, ok, err = NormalizeReadmeMarkdownPathFallbackInput("docs/README.md", "bad-kind")
		Expect(ok).To(BeTrue())
		expectPageLocalizedCode(err, ErrCodePageInvalidKind)

		rootDir := ginkgo.GinkgoT().TempDir()
		Expect(ReadmeFallbackSectionIsActive("", "docs")).To(BeFalse())
		Expect(ReadmeFallbackSectionIsActive(rootDir, "missing")).To(BeFalse())
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs", "child"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "README.md"), []byte("# Docs"), 0o644)).To(Succeed())
		Expect(ReadmeFallbackSectionIsActive(rootDir, "docs")).To(BeTrue())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "index.md"), []byte("# Index"), 0o644)).To(Succeed())
		Expect(ReadmeFallbackSectionIsActive(rootDir, "docs")).To(BeFalse())

		pageOut := &FindByPathOutput{Page: &tree.Page{PageNode: &tree.PageNode{ID: tree.PageIDFromString("readme")}}}
		out, handled, err := FindReadmeMarkdownPathFallback("docs/README.md", string(tree.NodeKindPage), ReadmeMarkdownPathFallbackLookup{
			FindByPath: func(in FindByPathInput) (*FindByPathOutput, error) {
				Expect(in.RoutePath).To(Equal(tree.RoutePath("docs/README")))
				Expect(in.Kind).To(Equal(tree.NodeKindPage))
				return pageOut, nil
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(handled).To(BeTrue())
		Expect(out).To(BeIdenticalTo(pageOut))

		_, handled, err = FindReadmeMarkdownPathFallback("docs/page.md", "", ReadmeMarkdownPathFallbackLookup{})
		Expect(err).NotTo(HaveOccurred())
		Expect(handled).To(BeFalse())

		_, handled, err = FindReadmeMarkdownPathFallback("docs/README.md", string(tree.NodeKindPage), ReadmeMarkdownPathFallbackLookup{
			FindByPath: func(FindByPathInput) (*FindByPathOutput, error) {
				return nil, tree.ErrPageNotFound
			},
		})
		Expect(handled).To(BeTrue())
		Expect(err).To(MatchError(tree.ErrPageNotFound))
	})

	ginkgo.It("maps page-domain errors to localized details and statuses", func() {
		detail, status, ok := PageErrorDetailForError(tree.ErrPageNotFound)
		Expect(ok).To(BeTrue())
		Expect(status).To(Equal(http.StatusNotFound))
		Expect(detail.Code).To(Equal(ErrCodePageNotFound))
		Expect(detail.MessageID).To(Equal(sharederrors.MessageID("errors.page.not_found")))

		localized := sharederrors.NewLocalizedErrorFromCode(ErrCodePageVersionConflict, nil)
		detail, status, ok = PageErrorDetailForError(localized)
		Expect(ok).To(BeTrue())
		Expect(status).To(Equal(http.StatusConflict))
		Expect(detail.Code).To(Equal(ErrCodePageVersionConflict))

		_, _, ok = PageErrorDetailForError(errors.New("outside pages"))
		Expect(ok).To(BeFalse())

		cases := []struct {
			err    error
			status int
			code   sharederrors.ErrorCode
		}{
			{tree.ErrParentNotFound, http.StatusNotFound, ErrCodePageParentNotFound},
			{tree.ErrPageHasChildren, http.StatusBadRequest, ErrCodePageHasChildren},
			{tree.ErrPageAlreadyExists, http.StatusBadRequest, ErrCodePageSlugConflict},
			{tree.ErrMovePageCircularReference, http.StatusBadRequest, ErrCodePageCircularMove},
			{tree.ErrPageCannotBeMovedToItself, http.StatusBadRequest, ErrCodePageCannotMoveToSelf},
			{tree.ErrConvertNotAllowed, http.StatusBadRequest, ErrCodePageConvertNotAllowed},
			{tree.ErrVersionRequired, http.StatusBadRequest, ErrCodePageVersionRequired},
			{tree.ErrTreeNotLoaded, http.StatusInternalServerError, ErrCodePageInternalError},
		}
		for _, tc := range cases {
			tc := tc
			detail, status, ok = PageErrorDetailForError(tc.err)
			Expect(ok).To(BeTrue())
			Expect(status).To(Equal(tc.status))
			Expect(detail.Code).To(Equal(tc.code))
		}

		Expect(pageErrorStatus(ErrCodePageVersionConflict)).To(Equal(http.StatusConflict))
		Expect(pageErrorStatus(ErrCodePageInternalError)).To(Equal(http.StatusInternalServerError))
	})

	ginkgo.It("writes structured page errors for localized, validation, sentinel, and fallback failures", func() {
		rec := respondWithPageErrorRecorder(sharederrors.NewLocalizedErrorFromCode(ErrCodePageInvalidRequest, nil))
		expectPageErrorResponse(rec, http.StatusBadRequest, ErrCodePageInvalidRequest)

		validationErr := sharederrors.NewValidationErrors()
		validationErr.AddWithCode("title", FieldCodePageTitleRequired, MessageIDPageTitleRequired)
		rec = respondWithPageErrorRecorder(validationErr)
		Expect(rec.Code).To(Equal(http.StatusBadRequest), rec.Body.String())
		Expect(rec.Body.String()).To(ContainSubstring(pageValidationErrorCode))

		rec = respondWithPageErrorRecorder(tree.ErrPageNotFound)
		expectPageErrorResponse(rec, http.StatusNotFound, ErrCodePageNotFound)

		rec = respondWithPageErrorRecorder(errors.New("boom"))
		expectPageErrorResponse(rec, http.StatusInternalServerError, ErrCodePageInternalError)
	})
})

func expectPageLocalizedCode(err error, code sharederrors.ErrorCode) {
	ginkgo.GinkgoHelper()

	var localized *sharederrors.LocalizedError
	Expect(errors.As(err, &localized)).To(BeTrue(), "error = %T %v", err, err)
	Expect(localized.Code).To(Equal(code))
}

func expectValidationField(err *sharederrors.ValidationErrors, field string) {
	ginkgo.GinkgoHelper()

	Expect(err.Errors).To(ContainElement(HaveField("Field", field)))
}

func respondWithPageErrorRecorder(err error) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	respondWithPageError(c, err)
	c.Writer.WriteHeaderNow()
	return rec
}

func expectPageErrorResponse(rec *httptest.ResponseRecorder, status int, code sharederrors.ErrorCode) {
	ginkgo.GinkgoHelper()

	Expect(rec.Code).To(Equal(status), rec.Body.String())
	var body PageErrorResponse
	Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
	Expect(body.Error.Code).To(Equal(code))
}
