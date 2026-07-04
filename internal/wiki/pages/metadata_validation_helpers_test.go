package pages

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/core/markdown"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/http/dto"
	"github.com/perber/wiki/internal/localization"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

var _ = ginkgo.Describe("metadata and validation helpers", func() {
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
			Expect(id).To(Equal(tree.PageIDFromString("page-1")))
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

	ginkgo.It("handles metadata enrichment and patch no-op edges", ginkgo.Label("unit"), func() {
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

		rendered, err := BuildMarkdownWithPublicMetadataPatch(current, tree.PageIDFromString("page-1"), " Page ", PublicMetadataPatch{}, "Body")
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

		rendered, err := BuildMarkdownWithPublicMetadataPatch(current, tree.PageIDFromString("page-2"), " New Title ", PublicMetadataPatch{
			TagsPresent:       true,
			Tags:              []string{"New", "new", "Done"},
			PropertiesPresent: true,
			Properties:        map[string]string{"status": "published"},
		}, "New body")
		Expect(err).NotTo(HaveOccurred())

		doc, _, err := markdown.ParsePageDocument(rendered)
		Expect(err).NotTo(HaveOccurred())
		Expect(doc).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Body": Equal("New body"),
			"Metadata": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Page": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"ID":    Equal("page-2"),
					"Title": Equal("New Title"),
				}),
				"Tags": Equal([]string{"new", "done"}),
				"Fields": Equal(map[string]interface{}{
					"private_flag": true,
					"status":       "published",
				}),
			}),
		}))
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
		Expect(routePath).To(Equal(tree.RoutePath("Docs")))
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

		Expect(MarkdownContentPathForRoute(tree.RoutePath("docs/page"), tree.NodeKindPage)).To(Equal(tree.MarkdownPath("docs/page.md")))

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

		typedParent := tree.PageIDFromString("parent-1")
		optionalSemanticParent, err := ValidateOptionalSemanticParentID(&typedParent)
		Expect(err).NotTo(HaveOccurred())
		Expect(optionalSemanticParent).NotTo(BeNil())
		Expect(*optionalSemanticParent).To(Equal(typedParent))

		_, err = ValidateSemanticRoutePath(" ")
		Expect(err).To(HavePageValidationFieldError("path", FieldCodePagePathRequired, MessageIDPagePathRequired))

		_, err = ValidateRoutePathValue(tree.RoutePath(""))
		Expect(err).To(HavePageValidationFieldError("path", FieldCodePagePathRequired, MessageIDPagePathRequired))

		id := tree.PageIDFromString("page-1")
		Expect(optionalPageIDString(nil)).To(BeNil())
		optionalID := optionalPageIDString(&id)
		Expect(optionalID).NotTo(BeNil())
		Expect(*optionalID).To(Equal("page-1"))
	})

	ginkgo.It("handles README markdown path fallback routing", ginkgo.Label("unit"), func() {
		Expect(" docs/README.md ").To(HaveReadmeMarkdownFallbackRoutes("docs/README", "docs"))

		Expect("docs/page.md").To(BeIgnoredByReadmeMarkdownFallbackRoutes())

		input, err := requireReadmeMarkdownPathFallbackInput("README.md", "")
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

		pageOut := &FindByPathOutput{Page: &tree.Page{PageNode: &tree.PageNode{ID: tree.PageIDFromString("readme")}}}
		out, err := requireReadmeMarkdownPathFallback("docs/README.md", tree.NodeKindPage, ReadmeMarkdownPathFallbackLookup{
			FindByPath: func(in FindByPathInput) (*FindByPathOutput, error) {
				Expect(in).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"RoutePath": Equal(tree.RoutePath("docs/README")),
					"Kind":      Equal(tree.NodeKindPage),
				}))
				return pageOut, nil
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(BeIdenticalTo(pageOut))

		Expect(ignoreReadmeMarkdownPathFallback("docs/page.md", "", ReadmeMarkdownPathFallbackLookup{})).To(Succeed())

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

func MatchPageLocalizedCode(code sharederrors.ErrorCode) types.GomegaMatcher {
	return testmatchers.MatchLocalizedError(code, sharederrors.MessageIDForCode(code))
}

func pagesTempDir() string {
	ginkgo.GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-pages-test-*")
	Expect(err).To(Succeed())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func requireReadmeMarkdownPathFallbackInput(path string, kind tree.NodeKind) (ReadmeMarkdownPathFallbackInput, error) {
	input, handled, err := NormalizeReadmeMarkdownPathFallbackInput(path, kind)
	if err != nil {
		return input, err
	}
	if !handled {
		return input, errors.New("README fallback input was not handled")
	}
	return input, nil
}

func requireReadmeMarkdownPathFallbackRawInput(path string, kind string) (ReadmeMarkdownPathFallbackInput, error) {
	input, handled, err := NormalizeReadmeMarkdownPathFallbackRawInput(path, kind)
	if err != nil {
		return input, err
	}
	if !handled {
		return input, errors.New("README fallback input was not handled")
	}
	return input, nil
}

func requireReadmeMarkdownPathFallback(path string, kind tree.NodeKind, lookup ReadmeMarkdownPathFallbackLookup) (*FindByPathOutput, error) {
	out, handled, err := FindReadmeMarkdownPathFallback(path, kind, lookup)
	if err != nil {
		return out, err
	}
	if !handled {
		return out, errors.New("README fallback was not handled")
	}
	return out, nil
}

func ignoreReadmeMarkdownPathFallback(path string, kind tree.NodeKind, lookup ReadmeMarkdownPathFallbackLookup) error {
	out, handled, err := FindReadmeMarkdownPathFallback(path, kind, lookup)
	if err != nil {
		return err
	}
	if handled || out != nil {
		return errors.New("README fallback was handled")
	}
	return nil
}

func BeRejectedMarkdownFence() types.GomegaMatcher {
	return WithTransform(markdownFenceParseStateFor, Equal(markdownLineRejected))
}

func BeRejectedMarkdownHeading() types.GomegaMatcher {
	return WithTransform(markdownHeadingParseStateFor, Equal(markdownLineRejected))
}

type markdownLineParseState uint8

const (
	markdownLineRejected markdownLineParseState = iota
	markdownLineAccepted
)

func markdownFenceParseStateFor(line string) markdownLineParseState {
	_, _, matched := parseMarkdownFence(line)
	return markdownLineParseStateFor(matched)
}

func markdownHeadingParseStateFor(line string) markdownLineParseState {
	_, _, matched := parseMarkdownHeading(line)
	return markdownLineParseStateFor(matched)
}

func markdownLineParseStateFor(matched bool) markdownLineParseState {
	if matched {
		return markdownLineAccepted
	}
	return markdownLineRejected
}

func HaveReadmeMarkdownFallbackRoutes(pageRoute string, sectionRoute string) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(path string) (bool, error) {
		gotPageRoute, gotSectionRoute, matched := ReadmeMarkdownPathFallbackRoutes(path)
		return matched && gotPageRoute == pageRoute && gotSectionRoute == sectionRoute, nil
	})
}

func BeIgnoredByReadmeMarkdownFallbackRoutes() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(path string) (bool, error) {
		pageRoute, sectionRoute, matched := ReadmeMarkdownPathFallbackRoutes(path)
		return !matched && pageRoute == "" && sectionRoute == "", nil
	})
}

func ResolveCatalogMessage() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(messageID sharederrors.MessageID) (bool, error) {
		rendered := localization.English.Render(messageID, "")
		return !rendered.Missing && rendered.Err == nil, nil
	}).WithMessage("resolve a catalog message")
}

func HaveExistingRoutePathLookup(path tree.RoutePath) types.GomegaMatcher {
	return WithTransform(routePathLookupStateFor, Equal(routePathLookupState{
		Path:  path,
		State: routePathLookupExisting,
	}))
}

type routePathLookupExistence uint8

const (
	routePathLookupMissing routePathLookupExistence = iota
	routePathLookupExisting
)

type routePathLookupState struct {
	Path  tree.RoutePath
	State routePathLookupExistence
}

func routePathLookupStateFor(lookup tree.PathLookup) routePathLookupState {
	state := routePathLookupMissing
	if lookup.Exists {
		state = routePathLookupExisting
	}
	return routePathLookupState{
		Path:  lookup.Path,
		State: state,
	}
}

func HavePageOnlyReadmeMarkdownFallback(pageRoute string, sectionRoute string) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(input ReadmeMarkdownPathFallbackInput) (bool, error) {
		return input.PageRoute == pageRoute &&
			input.SectionRoute == sectionRoute &&
			input.TryPage &&
			!input.TrySection, nil
	}).WithMessage("describe page-only README markdown fallback")
}

func HavePageAndSectionReadmeMarkdownFallback(pageRoute string, sectionRoute string) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(input ReadmeMarkdownPathFallbackInput) (bool, error) {
		return input.PageRoute == pageRoute &&
			input.SectionRoute == sectionRoute &&
			input.TryPage &&
			input.TrySection, nil
	}).WithMessage("describe page-and-section README markdown fallback")
}

func HaveSectionReadmeMarkdownFallback(pageRoute string, sectionRoute string) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(input ReadmeMarkdownPathFallbackInput) (bool, error) {
		return input.PageRoute == pageRoute &&
			input.SectionRoute == sectionRoute &&
			!input.TryPage &&
			input.TrySection, nil
	}).WithMessage("describe section README markdown fallback")
}

func HavePageSaveContentChange() types.GomegaMatcher {
	return WithTransform(pageSaveChangeSetFor, HaveField("Content", Equal(pageSaveChangePresent)))
}

func HavePageSaveSlugChange() types.GomegaMatcher {
	return WithTransform(pageSaveChangeSetFor, HaveField("Slug", Equal(pageSaveChangePresent)))
}

func HavePageSaveTitleChange() types.GomegaMatcher {
	return WithTransform(pageSaveChangeSetFor, HaveField("Title", Equal(pageSaveChangePresent)))
}

type pageSaveChangeState uint8

const (
	pageSaveChangeAbsent pageSaveChangeState = iota
	pageSaveChangePresent
)

type pageSaveChangeSet struct {
	Content pageSaveChangeState
	Slug    pageSaveChangeState
	Title   pageSaveChangeState
}

func pageSaveChangeSetFor(event pagesave.PageSaveEvent) pageSaveChangeSet {
	return pageSaveChangeSet{
		Content: pageSaveChangeStateFor(event.ContentChanged),
		Slug:    pageSaveChangeStateFor(event.SlugChanged),
		Title:   pageSaveChangeStateFor(event.TitleChanged),
	}
}

func pageSaveChangeStateFor(changed bool) pageSaveChangeState {
	if changed {
		return pageSaveChangePresent
	}
	return pageSaveChangeAbsent
}

func HavePageErrorDetail(status int, code sharederrors.ErrorCode) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(err error) (bool, error) {
		detail, gotStatus, matched := PageErrorDetailForError(err)
		return matched &&
			gotStatus == status &&
			detail.Code == code &&
			detail.MessageID == sharederrors.MessageIDForCode(code), nil
	})
}

func BeIgnoredByPageErrorDetail() types.GomegaMatcher {
	return WithTransform(pageErrorDetailResolutionFor, Equal(pageErrorDetailIgnored))
}

type pageErrorDetailResolution uint8

const (
	pageErrorDetailIgnored pageErrorDetailResolution = iota
	pageErrorDetailResolved
)

func pageErrorDetailResolutionFor(err error) pageErrorDetailResolution {
	_, _, matched := PageErrorDetailForError(err)
	if matched {
		return pageErrorDetailResolved
	}
	return pageErrorDetailIgnored
}

func HavePageValidationFieldError(field testmatchers.ValidationField, code sharederrors.FieldErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	return WithTransform(func(err error) *sharederrors.ValidationErrors {
		var validation *sharederrors.ValidationErrors
		if !errors.As(err, &validation) {
			return nil
		}
		return validation
	}, ContainPageValidationFieldError(field, code, messageID))
}

func HavePageValidationField(field testmatchers.ValidationField) types.GomegaMatcher {
	return WithTransform(func(err error) *sharederrors.ValidationErrors {
		var validation *sharederrors.ValidationErrors
		if !errors.As(err, &validation) {
			return nil
		}
		return validation
	}, ContainPageValidationField(field))
}

func HavePageValidationFields(fields ...testmatchers.ValidationField) types.GomegaMatcher {
	matchers := make([]types.GomegaMatcher, 0, len(fields))
	for _, field := range fields {
		matchers = append(matchers, HavePageValidationField(field))
	}
	return SatisfyAll(matchers...)
}

func ContainPageValidationFieldError(field testmatchers.ValidationField, code sharederrors.FieldErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	return testmatchers.ContainFieldError(field, code, messageID)
}

func ContainPageValidationField(field testmatchers.ValidationField) types.GomegaMatcher {
	fieldName := field.String()
	return WithTransform(func(validation *sharederrors.ValidationErrors) []*sharederrors.FieldError {
		if validation == nil {
			return nil
		}
		return validation.Errors
	}, ContainElement(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Field": Equal(fieldName),
	}))))
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

func HavePageErrorResponse(status int, code sharederrors.ErrorCode) types.GomegaMatcher {
	return testmatchers.HaveHTTPStructuredError(status, code, sharederrors.MessageIDForCode(code))
}
