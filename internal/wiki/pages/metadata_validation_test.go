package pages

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/core/markdown"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/http/dto"
)

func markdownMetadataPageID(page markdown.PageMetadataPage) tree.PageID {
	return tree.PageIDFromString(page.ID)
}

type publicMetadataPatchDocumentProjection struct {
	Body   string
	PageID tree.PageID
	Title  string
	Tags   []string
	Fields map[string]interface{}
}

func matchPublicMetadataPatchDocument(body string, pageID tree.PageID, title string, tags []string, fields map[string]interface{}) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(publicMetadataPatchDocumentProjectionFor, Equal(publicMetadataPatchDocumentProjection{
		Body:   body,
		PageID: pageID,
		Title:  title,
		Tags:   tags,
		Fields: fields,
	}))
}

func publicMetadataPatchDocumentProjectionFor(doc markdown.PageDocument) publicMetadataPatchDocumentProjection {
	return publicMetadataPatchDocumentProjection{
		Body:   doc.Body,
		PageID: markdownMetadataPageID(doc.Metadata.Page),
		Title:  doc.Metadata.Page.Title,
		Tags:   doc.Metadata.Tags,
		Fields: doc.Metadata.Fields,
	}
}

var _ = ginkgo.Describe("page metadata validation", func() {
	ginkgo.It("registers public, private, and refactor page route sets", ginkgo.Label("integration"), func() {
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

	ginkgo.It("enriches public page metadata from canonical page metadata", ginkgo.Label("unit"), func() {
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
			Expect(id).To(Equal(newFixturePageID("page-1")))
			return raw, nil
		})

		Expect(page).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Tags":       Equal([]string{"alpha", "beta"}),
			"Properties": Equal(map[string]string{"status": "draft"}),
		})))
	})

	ginkgo.It("extracts normalized tags and string properties from metadata", ginkgo.Label("unit"), func() {
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

	ginkgo.It("preserves empty metadata when enrichment has no readable content", ginkgo.Label("unit"), func() {
		EnrichPageMetadata(nil, nil)

		page := &dto.Page{Node: &dto.Node{ID: "page-1"}}
		EnrichPageMetadata(page, func(tree.PageID) (string, error) {
			return "", errors.New("read failed")
		})
		Expect(page).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Tags":       BeEmpty(),
			"Properties": BeEmpty(),
		})))

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

		rendered, err := BuildMarkdownWithPublicMetadataPatch(current, newFixturePageID("page-1"), " Page ", PublicMetadataPatch{}, "Body")
		Expect(err).NotTo(HaveOccurred())
		doc, _, err := markdown.ParsePageDocument(rendered)
		Expect(err).NotTo(HaveOccurred())
		Expect(doc.Metadata).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Tags":   Equal([]string{"keep"}),
			"Fields": Equal(map[string]interface{}{"status": "draft"}),
		}))

		meta := ApplyPublicMetadata(markdown.PageMetadata{Fields: map[string]interface{}{"status": "draft"}}, map[string]string{"status": "draft"}, nil, nil)
		Expect(meta.Fields).To(BeNil())
	})

	ginkgo.It("builds markdown with a public metadata patch while preserving private fields", ginkgo.Label("unit"), func() {
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

		rendered, err := BuildMarkdownWithPublicMetadataPatch(current, newFixturePageID("page-2"), " New Title ", PublicMetadataPatch{
			TagsPresent:       true,
			Tags:              []string{"New", "new", "Done"},
			PropertiesPresent: true,
			Properties:        map[string]string{"status": "published"},
		}, "New body")
		Expect(err).NotTo(HaveOccurred())

		doc, _, err := markdown.ParsePageDocument(rendered)
		Expect(err).NotTo(HaveOccurred())
		Expect(doc).To(matchPublicMetadataPatchDocument(
			"New body",
			newFixturePageID("page-2"),
			"New Title",
			[]string{"new", "done"},
			map[string]interface{}{
				"private_flag": true,
				"status":       "published",
			},
		))
	})

	ginkgo.It("applies partial metadata patches with normalized tags and validated property removals", ginkgo.Label("unit"), func() {
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
		Expect(err).To(HavePageValidationFieldError("removeProperties. leafwiki_hidden ", FieldCodePagePropertyKeyWhitespace, MessageIDPagePropertyKeyWhitespace))
	})

	ginkgo.It("validates and normalizes route path and page kind inputs", ginkgo.Label("unit"), func() {
		routePath, kind, err := NormalizePagePathInput(" /Docs/Index.md ", "section")
		Expect(err).NotTo(HaveOccurred())
		Expect(routePath).To(Equal(newFixtureRoutePath("Docs")))
		Expect(kind).To(Equal(tree.NodeKindSection))

		_, _, err = NormalizePagePathInput("docs/page.md", "section")
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidKind))

		kind, err = ValidatePageKind(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(kind).To(Equal(tree.NodeKindPage))

		_, err = ValidatePageRoutePath(" ")
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageMissingPath))

		_, err = ValidatePageRoutePath("../escape")
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidPath))

		Expect(MarkdownContentPathForRoute(newFixtureRoutePath("docs/page"), tree.NodeKindPage)).To(Equal(newFixtureMarkdownPath("docs/page.md")))

		validatedParent, err := ValidateMoveParentID("root")
		Expect(err).NotTo(HaveOccurred())
		Expect(validatedParent).To(Equal("root"))

		_, err = ValidateMoveParentID(" parent ")
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidParentID))

		parent := "parent-1"
		optionalParent, err := ValidateOptionalParentID(&parent)
		Expect(err).NotTo(HaveOccurred())
		Expect(optionalParent).NotTo(BeNil())
		Expect(*optionalParent).To(Equal("parent-1"))

		typedParent := newFixturePageID("parent-1")
		optionalSemanticParent, err := ValidateOptionalSemanticParentID(&typedParent)
		Expect(err).NotTo(HaveOccurred())
		Expect(optionalSemanticParent).NotTo(BeNil())
		Expect(*optionalSemanticParent).To(Equal(typedParent))

		_, err = ValidateSemanticRoutePath(" ")
		Expect(err).To(HavePageValidationFieldError("path", FieldCodePagePathRequired, MessageIDPagePathRequired))

		_, err = ValidateRoutePathValue(newFixtureRoutePath(""))
		Expect(err).To(HavePageValidationFieldError("path", FieldCodePagePathRequired, MessageIDPagePathRequired))

		id := newFixturePageID("page-1")
		Expect(optionalPageIDString(nil)).To(BeNil())
		optionalID := optionalPageIDString(&id)
		Expect(optionalID).NotTo(BeNil())
		Expect(*optionalID).To(Equal("page-1"))
	})

	ginkgo.It("resolves README markdown path fallback routes", ginkgo.Label("unit"), func() {
		Expect(" docs/README.md ").To(HaveReadmeMarkdownFallbackRoutes("docs/README", "docs"))

		Expect("docs/page.md").To(BeIgnoredByReadmeMarkdownFallbackRoutes())

		input, err := requireReadmeMarkdownPathFallbackInput("README.md", newFixtureNodeKind(""))
		Expect(err).NotTo(HaveOccurred())
		Expect(input).To(HavePageAndSectionReadmeMarkdownFallback("README", ""))

		input, err = requireReadmeMarkdownPathFallbackInput("docs/README.md", tree.NodeKindSection)
		Expect(err).NotTo(HaveOccurred())
		Expect(input).To(HaveSectionReadmeMarkdownFallback("docs/README", "docs"))

		_, err = requireReadmeMarkdownPathFallbackRawInput("docs/README.md", "bad-kind")
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidKind))

		rootDir := pagesTempDir()
		Expect(ReadmeFallbackSectionIsActive("", "docs")).To(BeFalse())
		Expect(ReadmeFallbackSectionIsActive(rootDir, "missing")).To(BeFalse())
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs", "child"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "README.md"), []byte("# Docs"), 0o644)).To(Succeed())
		Expect(ReadmeFallbackSectionIsActive(rootDir, "docs")).To(BeTrue())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "index.md"), []byte("# Index"), 0o644)).To(Succeed())
		Expect(ReadmeFallbackSectionIsActive(rootDir, "docs")).To(BeFalse())

		pageOut := &FindByPathOutput{Page: &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("readme")}}}
		out, err := requireReadmeMarkdownPathFallback("docs/README.md", tree.NodeKindPage, ReadmeMarkdownPathFallbackLookup{
			FindByPath: func(in FindByPathInput) (*FindByPathOutput, error) {
				Expect(in).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"RoutePath": Equal(newFixtureRoutePath("docs/README")),
					"Kind":      Equal(tree.NodeKindPage),
				}))
				return pageOut, nil
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(BeIdenticalTo(pageOut))

		Expect(ignoreReadmeMarkdownPathFallback("docs/page.md", newFixtureNodeKind(""), ReadmeMarkdownPathFallbackLookup{})).To(Succeed())

		_, err = requireReadmeMarkdownPathFallback("docs/README.md", tree.NodeKindPage, ReadmeMarkdownPathFallbackLookup{
			FindByPath: func(FindByPathInput) (*FindByPathOutput, error) {
				return nil, tree.ErrPageNotFound
			},
		})
		Expect(err).To(MatchError(tree.ErrPageNotFound))
	})

	ginkgo.It("maps page-domain errors to localized details and statuses", ginkgo.Label("unit"), func() {
		Expect(tree.ErrPageNotFound).To(HavePageErrorDetail(http.StatusNotFound, ErrCodePageNotFound))

		localized := sharederrors.NewLocalizedErrorFromCode(ErrCodePageVersionConflict, nil)
		Expect(localized).To(HavePageErrorDetail(http.StatusConflict, ErrCodePageVersionConflict))

		Expect(errors.New("outside pages")).To(BeIgnoredByPageErrorDetail())

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
			Expect(tc.err).To(HavePageErrorDetail(tc.status, tc.code))
		}

		Expect(pageErrorStatus(ErrCodePageVersionConflict)).To(Equal(http.StatusConflict))
		Expect(pageErrorStatus(ErrCodePageInternalError)).To(Equal(http.StatusInternalServerError))
	})

	ginkgo.It("writes structured page errors for localized, validation, sentinel, and fallback failures", ginkgo.Label("integration"), func() {
		rec := respondWithPageErrorRecorder(sharederrors.NewLocalizedErrorFromCode(ErrCodePageInvalidRequest, nil))
		Expect(rec).To(HavePageErrorResponse(http.StatusBadRequest, ErrCodePageInvalidRequest), rec.Body.String())

		validationErr := sharederrors.NewValidationErrors()
		validationErr.AddWithCode("title", FieldCodePageTitleRequired, MessageIDPageTitleRequired)
		rec = respondWithPageErrorRecorder(validationErr)
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), rec.Body.String())
		Expect(rec).To(HaveHTTPBody(ContainSubstring(pageValidationErrorCode)))

		rec = respondWithPageErrorRecorder(tree.ErrPageNotFound)
		Expect(rec).To(HavePageErrorResponse(http.StatusNotFound, ErrCodePageNotFound), rec.Body.String())

		rec = respondWithPageErrorRecorder(errors.New("boom"))
		Expect(rec).To(HavePageErrorResponse(http.StatusInternalServerError, ErrCodePageInternalError), rec.Body.String())
	})
})
