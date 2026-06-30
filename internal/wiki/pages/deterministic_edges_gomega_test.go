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
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/gin-gonic/gin"
	"github.com/perber/wiki/internal/core/assets"
	"github.com/perber/wiki/internal/core/markdown"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/http/dto"
	"github.com/perber/wiki/internal/links"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

const pageMetadataWhitespacePropertyKeyFixture = " key "

var _ = ginkgo.Describe("deterministic page helper edges", func() {
	ginkgo.It("covers markdown section parser and metadata patch edge branches", func() {
		_, err := ReplaceMarkdownSection("# Page\n", []string{" "}, 0, "")
		Expect(err).To(MatchError(ErrSectionHeadingPathRequired))

		content := "# Page\n\n## Repeat\none\n\n## Repeat\ntwo\n"
		_, err = ReplaceMarkdownSection(content, []string{"Repeat"}, 3, "replacement")
		Expect(err).To(MatchError(ErrSectionHeadingNotFound))

		replaced, err := ReplaceMarkdownSection(content, []string{"Repeat"}, 1, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(replaced).NotTo(ContainSubstring("one"))
		Expect(replaced).To(ContainSubstring("two"))

		_, _, ok := parseMarkdownFence("~~")
		Expect(ok).To(BeFalse())

		_, _, ok = parseMarkdownHeading("####### too deep")
		Expect(ok).To(BeFalse())
		_, _, ok = parseMarkdownHeading("#    ")
		Expect(ok).To(BeFalse())

		Expect(markdownIndentColumns("\tcode")).To(Equal(4))
		Expect(firstReplacementHeadingLevel([]string{"", "plain text"})).To(BeZero())
		Expect(firstReplacementHeadingLevel([]string{"", "  "})).To(BeZero())

		tags, properties, err := ApplyMetadataPatch(
			[]string{"old"},
			map[string]string{"keep": "yes"},
			MetadataPatch{
				SetTags:       []string{"New", "new"},
				SetProperties: map[string]string{"status": "done"},
			},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(tags).To(Equal([]string{"new"}))
		Expect(properties).To(Equal(map[string]string{"keep": "yes", "status": "done"}))

		_, _, err = ApplyMetadataPatch(nil, nil, MetadataPatch{RemoveProperties: []string{"", "tags", "title"}})
		Expect(err).To(HavePageValidationFields(
			"removeProperties.",
			"removeProperties.tags",
			"removeProperties.title",
		))
	})

	ginkgo.It("covers metadata extraction and README fallback branches", func() {
		page := &dto.Page{Node: &dto.Node{ID: "page-1"}}
		EnrichPageMetadata(page, func(tree.PageID) (string, error) {
			return "<!-- leafwiki malformed\n-->\nbody", nil
		})
		Expect(page).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Tags":       BeEmpty(),
			"Properties": BeEmpty(),
		})))

		_, err := BuildMarkdownWithPublicMetadataPatch("<!-- leafwiki malformed\n-->\nbody", tree.PageIDFromString("page-1"), "Page", PublicMetadataPatch{}, "body")
		Expect(err).To(MatchError(markdown.ErrMetadataParse))

		Expect(normalizeMetadataTags([]string{" Alpha ", "alpha", "Beta"})).To(Equal([]string{"alpha", "beta"}))
		tags, properties := ExtractPageMetadataFromPageMetadata(markdown.PageMetadata{
			Tags: []string{"Ready"},
			Fields: map[string]interface{}{
				"leafwiki_hidden": "reserved",
				"owner":           " Alice ",
			},
		})
		Expect(tags).To(Equal([]string{"ready"}))
		Expect(properties).To(Equal(map[string]string{"owner": "Alice"}))

		rootDir := ginkgo.GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(rootDir, "README.md"), []byte("# Root"), 0o644)).To(Succeed())
		rootPage := &tree.Page{PageNode: &tree.PageNode{ID: tree.RootPageID, Title: "Root", Slug: tree.SlugFromString("root"), Kind: tree.NodeKindSection}}
		out, handled, err := FindReadmeMarkdownPathFallback("README.md", tree.NodeKindSection, ReadmeMarkdownPathFallbackLookup{
			RootDir: rootDir,
			RootPage: func() (*tree.Page, error) {
				return rootPage, nil
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(handled).To(BeTrue())
		Expect(out.Page).To(BeIdenticalTo(rootPage))

		Expect(os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "README.md"), []byte("# Docs"), 0o644)).To(Succeed())
		sectionPage := &tree.Page{PageNode: &tree.PageNode{ID: tree.PageIDFromString("docs"), Title: "Docs", Slug: tree.SlugFromString("docs"), Kind: tree.NodeKindSection}}
		out, handled, err = FindReadmeMarkdownPathFallback("docs/README.md", tree.NodeKindSection, ReadmeMarkdownPathFallbackLookup{
			RootDir: rootDir,
			FindByPath: func(in FindByPathInput) (*FindByPathOutput, error) {
				Expect(in).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"RoutePath": Equal(tree.RoutePath("docs")),
					"Kind":      Equal(tree.NodeKindSection),
				}))
				return &FindByPathOutput{Page: sectionPage}, nil
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(handled).To(BeTrue())
		Expect(out.Page).To(BeIdenticalTo(sectionPage))
	})

	ginkgo.It("covers lookup, permalink, slug, and validation error branches", func() {
		deps := newRoutesSpecDeps()
		docs := deps.createPage("Docs", "docs", tree.NodeKindSection, nil)
		guide := deps.createPage("Guide", "guide", tree.NodeKindPage, &docs.ID)
		Expect(guide).NotTo(BeNil())

		findByPath := NewFindByPathUseCase(deps.tree)
		_, err := findByPath.Execute(context.Background(), FindByPathInput{})
		Expect(err).To(HavePageValidationField("path"))

		lookup, err := NewLookupPagePathUseCase(deps.tree).Execute(context.Background(), LookupPagePathInput{Path: "docs/guide"})
		Expect(err).NotTo(HaveOccurred())
		Expect(lookup.Lookup.Exists).To(BeTrue())

		_, err = NewLookupPagePathUseCase(deps.tree).Execute(context.Background(), LookupPagePathInput{})
		Expect(err).To(HavePageValidationField("path"))

		_, err = NewResolvePermalinkUseCase(deps.tree).Execute(context.Background(), ResolvePermalinkInput{ID: tree.PageIDFromString("missing")})
		Expect(err).To(MatchError(tree.ErrPageNotFound))

		_, err = NewSuggestSlugUseCase(deps.tree, tree.NewSlugService()).Execute(context.Background(), SuggestSlugInput{ParentID: tree.PageIDFromString("missing"), Title: "Child"})
		Expect(err).To(MatchError(tree.ErrPageNotFound))

		validated, err := ValidateRefactorKind(RefactorKindRename)
		Expect(err).NotTo(HaveOccurred())
		Expect(validated).To(Equal(RefactorKindRename))
		_, err = ValidateRefactorKind("copy")
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidRefactorKind))

		parent, err := ValidateSemanticMoveParentID(tree.RootPageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(parent).To(Equal(tree.RootPageID))
		_, err = ValidateSemanticMoveParentID(tree.PageID(" parent "))
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidParentID))

		optionalParent, err := ValidateOptionalParentID(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(optionalParent).To(BeNil())

		optionalSemanticParent, err := ValidateOptionalSemanticParentID(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(optionalSemanticParent).To(BeNil())

		_, err = ValidateSemanticRoutePath("../escape")
		Expect(err).To(HavePageValidationField("path"))

		Expect(pageErrorStatus(sharederrors.ErrorCode("unknown"))).To(Equal(http.StatusInternalServerError))
	})

	ginkgo.It("covers route input helpers for root, README, metadata, and versions", func() {
		deps := newRoutesSpecDeps()
		var noKind tree.NodeKind

		rootOut, err := deps.routes.findByPathInput(context.Background(), "", noKind)
		Expect(err).NotTo(HaveOccurred())
		Expect(rootOut.Page.ID).To(Equal(tree.RootPageID))

		rootOut, err = deps.routes.findByPathInput(context.Background(), "", tree.NodeKindSection)
		Expect(err).NotTo(HaveOccurred())
		Expect(rootOut.Page.ID).To(Equal(tree.RootPageID))

		_, err = deps.routes.findByPathInput(context.Background(), "", tree.NodeKindPage)
		Expect(err).To(MatchError(tree.ErrPageNotFound))

		_, err = deps.routes.findByPathRawInput(context.Background(), "", "folder")
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidKind))

		pageRoute, sectionRoute, ok := ReadmeMarkdownPathFallbackRoutes("docs/README.md")
		Expect(ok).To(BeTrue())
		Expect(pageRoute).To(Equal("docs/README"))
		Expect(sectionRoute).To(Equal("docs"))

		fallback, ok, err := NormalizeReadmeMarkdownPathFallbackInput("docs/README.md", tree.NodeKindPage)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(fallback).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"TryPage":    BeTrue(),
			"TrySection": BeFalse(),
		}))

		_, handled, err := FindReadmeMarkdownPathFallback("docs/README.md", tree.NodeKindSection, ReadmeMarkdownPathFallbackLookup{
			RootDir: "",
			FindByPath: func(FindByPathInput) (*FindByPathOutput, error) {
				ginkgo.Fail("section-only inactive README fallback should not query a page route")
				return nil, nil
			},
		})
		Expect(handled).To(BeTrue())
		Expect(err).To(MatchError(tree.ErrPageNotFound))

		Expect(ReadmeFallbackSectionIsActive("", "")).To(BeFalse())
		rootDir := ginkgo.GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(rootDir, "README.md"), []byte("# Root"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "index.md"), []byte("# Index"), 0o644)).To(Succeed())
		Expect(ReadmeFallbackSectionIsActive(rootDir, "")).To(BeFalse())

		Expect(sanitizeSemanticClientVersion(tree.PageVersion("\x00"))).To(BeEmpty())
		Expect(sanitizeSemanticClientVersion(tree.PageVersionFromString("v1"))).To(Equal(tree.PageVersion("v1")))

		err = ValidatePageMetadataInput(
			[]string{"alpha", "Alpha", " spaced "},
			map[string]string{
				"":                                       "blank",
				pageMetadataWhitespacePropertyKeyFixture: "space",
				"leafwiki_hidden":                        "reserved",
				"tags":                                   "reserved",
				"title":                                  "reserved",
			},
		)
		whitespacePropertyField := "properties." + pageMetadataWhitespacePropertyKeyFixture
		Expect(err).To(SatisfyAll(
			HavePageValidationFieldError("tags[1]", FieldCodePageTagDuplicate, MessageIDPageTagDuplicate),
			HavePageValidationFieldError("tags[2]", FieldCodePageTagWhitespace, MessageIDPageTagWhitespace),
			HavePageValidationFieldError("properties.", FieldCodePagePropertyKeyRequired, MessageIDPagePropertyKeyRequired),
			HavePageValidationFieldError(testmatchers.ValidationFieldName(whitespacePropertyField), FieldCodePagePropertyKeyWhitespace, MessageIDPagePropertyKeyWhitespace),
			HavePageValidationFieldError("properties.leafwiki_hidden", FieldCodePagePropertyKeyReserved, MessageIDPagePropertyKeyReservedPrefix),
			HavePageValidationFieldError("properties.tags", FieldCodePagePropertyKeyReserved, MessageIDPagePropertyKeyReserved),
			HavePageValidationFieldError("properties.title", FieldCodePagePropertyKeyReserved, MessageIDPagePropertyKeyReserved),
		))
	})

	ginkgo.It("deduplicates and sorts refactor warnings deterministically", func() {
		first := RefactorWarning{MessageID: sharederrors.MessageID("warnings.refactor.a"), Message: "beta"}
		second := RefactorWarning{MessageID: sharederrors.MessageID("warnings.refactor.a"), Message: "alpha"}
		third := RefactorWarning{MessageID: sharederrors.MessageID("warnings.refactor.b"), Message: "alpha"}

		warnings := collectPreviewWarnings([]RefactorAffectedPage{
			{WarningDetails: []RefactorWarning{third, first}},
			{WarningDetails: []RefactorWarning{first, second}},
		})

		Expect(warnings).To(Equal([]RefactorWarning{second, first, third}))
		Expect(containsRefactorWarning(warnings, first)).To(BeTrue())
		Expect(containsRefactorWarning(warnings, RefactorWarning{MessageID: first.MessageID, Message: "missing"})).To(BeFalse())
		Expect(containsString([]string{"old", "new"}, "old")).To(BeTrue())
		Expect(containsString([]string{"old", "new"}, "missing")).To(BeFalse())
		Expect(ensureStrings(nil)).To(BeEmpty())
		Expect(ensureStrings([]string{"x"})).To(Equal([]string{"x"}))
		Expect(ensureRefactorWarnings(nil)).To(BeEmpty())
		Expect(ensureRefactorWarnings([]RefactorWarning{first})).To(Equal([]RefactorWarning{first}))
	})

	ginkgo.It("covers direct use-case validation and root-operation guards", func() {
		deps := newRoutesSpecDeps()
		userID := tree.UserIDFromString("pages-direct-user")

		_, err := deps.routes.copyPage.Execute(context.Background(), CopyPageInput{
			UserID:       userID,
			SourcePageID: tree.PageIDFromString("missing"),
			Title:        "",
			Slug:         tree.SlugFromString("bad slug"),
		})
		Expect(err).To(HavePageValidationFields("title", "slug"))

		badParent := tree.PageID(" parent ")
		_, err = deps.routes.copyPage.Execute(context.Background(), CopyPageInput{
			UserID:         userID,
			SourcePageID:   tree.PageIDFromString("missing"),
			TargetParentID: &badParent,
			Title:          "Copy",
			Slug:           tree.SlugFromString("copy"),
		})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidParentID))

		err = deps.routes.deletePage.Execute(context.Background(), DeletePageInput{UserID: userID, ID: tree.RootPageID})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageRootOperation))

		err = deps.routes.convertPage.Execute(context.Background(), ConvertPageInput{UserID: userID, ID: ""})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageRootOperation))

		err = deps.routes.movePage.Execute(context.Background(), MovePageInput{UserID: userID, ID: tree.RootPageID})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageRootOperation))

		_, err = deps.routes.ensurePath.Execute(context.Background(), EnsurePathInput{UserID: userID, TargetPath: "", TargetTitle: " "})
		Expect(err).To(HavePageValidationFields("path", "title"))
	})

	ginkgo.It("records direct mutation events for create, update, and delete use cases", func() {
		deps := newRoutesSpecDeps()
		recorder := &recordingPageSaveEffect{}
		orchestrator := pagesave.NewPageSaveOrchestrator(recorder)
		slugger := tree.NewSlugService()
		log := slog.Default()
		userID := tree.UserIDFromString("pages-event-user")
		kind := tree.NodeKindPage

		createUC := NewCreatePageUseCase(deps.tree, slugger, orchestrator, log)
		created, err := createUC.Execute(context.Background(), CreatePageInput{
			UserID: userID,
			Source: pagesave.PageMutationSourceMCP,
			Title:  "Original",
			Slug:   tree.SlugFromString("original"),
			Kind:   &kind,
		})
		Expect(err).NotTo(HaveOccurred())
		createdEvent := matchPageSaveEvent(gstruct.Fields{
			"Operation": Equal(pagesave.PageOperationCreate),
			"After":     HaveField("ID", Equal(created.Page.ID)),
			"Summary":   Equal("page created"),
		})
		Expect(recorder.events).To(HaveExactElements(createdEvent))

		content := "Updated body"
		updateUC := NewUpdatePageUseCase(deps.tree, slugger, orchestrator, log)
		updated, err := updateUC.Execute(context.Background(), UpdatePageInput{
			UserID:  userID,
			Source:  pagesave.PageMutationSourceWeb,
			ID:      created.Page.ID,
			Version: created.Page.Version(),
			Title:   "Renamed",
			Slug:    tree.SlugFromString("renamed"),
			Content: &content,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(updated.Page.Title).To(Equal("Renamed"))
		updatedEvent := matchPageSaveEvent(gstruct.Fields{
			"Operation":      Equal(pagesave.PageOperationUpdate),
			"OldPath":        Equal(tree.RoutePath("original")),
			"ContentChanged": BeTrue(),
			"SlugChanged":    BeTrue(),
			"TitleChanged":   BeTrue(),
			"AffectedPages":  HaveLen(1),
		})
		Expect(recorder.events).To(HaveExactElements(createdEvent, updatedEvent))

		assetService := assets.NewAssetService(deps.tree.RootDir(), slugger)
		deleteUC := NewDeletePageUseCase(deps.tree, assetService, orchestrator, log)
		err = deleteUC.Execute(context.Background(), DeletePageInput{
			UserID:  userID,
			Source:  pagesave.PageMutationSourceWeb,
			ID:      updated.Page.ID,
			Version: updated.Page.Version(),
		})
		Expect(err).NotTo(HaveOccurred())
		deletedEvent := matchPageSaveEvent(gstruct.Fields{
			"Operation":     Equal(pagesave.PageOperationDelete),
			"Before":        HaveField("ID", Equal(updated.Page.ID)),
			"OldPath":       Equal(tree.RoutePath("renamed")),
			"AffectedPages": HaveLen(1),
		})
		Expect(recorder.events).To(HaveExactElements(createdEvent, updatedEvent, deletedEvent))
	})

	ginkgo.It("covers refactor constructors and internal helper branches", func() {
		deps := newRoutesSpecDeps()
		slugger := tree.NewSlugService()
		customOrchestrator := pagesave.NewPageSaveOrchestrator()

		applyUC := NewApplyPageRefactorUseCaseWithOrchestratorAndOptions(
			deps.tree,
			slugger,
			nil,
			customOrchestrator,
			slog.Default(),
			RefactorUseCaseOptions{MarkdownLinkRootPrefix: "/docs"},
		)
		Expect(applyUC.orchestrator).To(BeIdenticalTo(customOrchestrator))
		Expect(applyUC.markdownLinkRootPrefix).To(Equal("/docs"))
		Expect(applyUC.preview.markdownLinkRootPrefix).To(Equal("/docs"))

		defaulted := NewApplyPageRefactorUseCaseWithOrchestratorAndOptions(deps.tree, slugger, nil, nil, slog.Default(), RefactorUseCaseOptions{})
		Expect(defaulted.orchestrator).NotTo(BeNil())

		lazy := &ApplyPageRefactorUseCase{links: nil, log: slog.Default()}
		Expect(lazy.pageOrchestrator()).NotTo(BeNil())
		Expect(lazy.orchestrator).NotTo(BeNil())

		recorder := &recordingPageSaveEffect{}
		applyUC.orchestrator = pagesave.NewPageSaveOrchestrator(recorder)
		Expect(applyUC.runBulkContentUpdateSideEffects("editor", "test", nil)).To(Succeed())
		Expect(recorder.events).To(BeEmpty())

		page := deps.createPage("Helper", "helper", tree.NodeKindPage, nil)
		Expect(applyUC.runBulkContentUpdateSideEffects("editor", "test", []*tree.Page{page})).To(Succeed())
		Expect(recorder.events).To(HaveExactElements(matchPageSaveEvent(gstruct.Fields{
			"Operation":      Equal(pagesave.PageOperationUpdate),
			"ContentChanged": BeTrue(),
			"AffectedPages":  Equal([]*tree.Page{page}),
		})))

		Expect(applyUC.loadPagesByID(nil, "unused")).To(BeEmpty())
		loaded := applyUC.loadPagesByID([]tree.PageID{page.ID, tree.PageIDFromString("missing")}, "missing")
		Expect(loaded).To(HaveKey(page.ID))
		Expect(loaded).NotTo(HaveKey(tree.PageIDFromString("missing")))

		ordered := applyUC.loadPagesInOrder([]tree.PageID{tree.PageIDFromString("missing"), page.ID}, "missing")
		Expect(ordered).To(Equal([]*tree.Page{page}))

		Expect(planNodeKind(nil)).To(Equal(tree.NodeKindPage))
		Expect(planNodeKind([]pathChangeSnapshot{{Kind: tree.NodeKindSection, RootPage: true}})).To(Equal(tree.NodeKindSection))
		Expect(snapshotPage(nil)).To(BeNil())
		noNode := &tree.Page{Content: "body"}
		Expect(snapshotPage(noNode)).To(BeIdenticalTo(noNode))
	})

	ginkgo.It("covers route malformed-payload branches and semantic ID helpers", func() {
		deps := newRoutesSpecDeps()
		page := deps.createPage("Payload", "payload", tree.NodeKindPage, nil)

		rec := performRoutesRequest(
			http.MethodPut,
			pageRouteTargetWithSuffix(page.ID, "/move"),
			`{`,
			ginPageIDParams(page.ID),
			routesSpecUser(),
			deps.routes.handleMove,
		)
		Expect(rec).To(HavePageErrorResponse(http.StatusBadRequest, ErrCodePageInvalidPayload), rec.Body.String())

		rec = performRoutesRequest(
			http.MethodPut,
			pageRouteTargetWithSuffix(page.ID, "/sort"),
			`{`,
			ginPageIDParams(page.ID),
			routesSpecUser(),
			deps.routes.handleSort,
		)
		Expect(rec).To(HavePageErrorResponse(http.StatusBadRequest, ErrCodePageInvalidRequest), rec.Body.String())

		rec = performRoutesRequest(
			http.MethodPost,
			"/api/pages/ensure",
			`{"path":"docs","title":"Docs","kind":"folder"}`,
			nil,
			routesSpecUser(),
			deps.routes.handleEnsurePath,
		)
		Expect(rec).To(HavePageErrorResponse(http.StatusBadRequest, ErrCodePageInvalidKind), rec.Body.String())

		rec = performRoutesRequest(
			http.MethodPost,
			convertPageRouteTarget(page.ID),
			`{`,
			ginPageIDParams(page.ID),
			routesSpecUser(),
			deps.routes.handleConvert,
		)
		Expect(rec).To(HavePageErrorResponse(http.StatusBadRequest, ErrCodePageInvalidRequest), rec.Body.String())

		rec = performRoutesRequest(
			http.MethodPost,
			copyPageRouteTarget(page.ID),
			`{`,
			ginPageIDParams(page.ID),
			routesSpecUser(),
			deps.routes.handleCopy,
		)
		Expect(rec).To(HavePageErrorResponse(http.StatusBadRequest, ErrCodePageInvalidRequest), rec.Body.String())

		rec = performRoutesRequest(
			http.MethodPost,
			refactorPreviewRouteTarget(page.ID),
			`{`,
			ginPageIDParams(page.ID),
			nil,
			deps.routes.handleRefactorPreview,
		)
		Expect(rec).To(HavePageErrorResponse(http.StatusBadRequest, ErrCodePageInvalidRequest), rec.Body.String())

		rec = performRoutesRequest(
			http.MethodPost,
			refactorApplyRouteTarget(page.ID),
			`{`,
			ginPageIDParams(page.ID),
			routesSpecUser(),
			deps.routes.handleRefactorApply,
		)
		Expect(rec).To(HavePageErrorResponse(http.StatusBadRequest, ErrCodePageInvalidRequest), rec.Body.String())

		Expect(semanticPageIDPtr(nil)).To(BeNil())
		rawParent := " parent "
		Expect(*semanticPageIDPtr(&rawParent)).To(Equal(tree.PageIDFromString(" parent ")))
		Expect(semanticPageIDs([]string{"one", "two"})).To(Equal([]tree.PageID{tree.PageIDFromString("one"), tree.PageIDFromString("two")}))

		_, err := ValidateSuggestSlugTitle("!!!")
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidTitle))
		Expect(ValidatePageMetadataInput([]string{"alpha", "beta"}, map[string]string{"owner": "alice"})).To(Succeed())
	})

	ginkgo.It("covers additional route handler error and early-return branches", func() {
		deps := newRoutesSpecDeps()
		page := deps.createPage("Page", "page", tree.NodeKindPage, nil)

		rec := performRoutesRequest(http.MethodGet, "/api/tree", "", nil, nil, deps.routes.handleGetTree)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())

		rec = performRoutesRequest(http.MethodGet, "/api/pages/missing", "", ginParams("id", "missing"), nil, deps.routes.handleGetPage)
		Expect(rec).To(HavePageErrorResponse(http.StatusNotFound, ErrCodePageNotFound), rec.Body.String())

		rec = performRoutesRequest(http.MethodGet, "/api/pages/lookup?path=page&kind=folder", "", nil, nil, deps.routes.handleLookupPath)
		Expect(rec).To(HavePageErrorResponse(http.StatusBadRequest, ErrCodePageInvalidKind), rec.Body.String())

		rec = performRoutesRequest(http.MethodGet, "/api/pages/lookup?path=../escape", "", nil, nil, deps.routes.handleLookupPath)
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), rec.Body.String())
		Expect(rec).To(HaveHTTPBody(ContainSubstring("path")))

		rec = performRoutesRequest(http.MethodGet, "/api/pages/slug-suggestion?title=Child&parentId=missing", "", nil, nil, deps.routes.handleSuggestSlug)
		Expect(rec).To(HavePageErrorResponse(http.StatusNotFound, ErrCodePageNotFound), rec.Body.String())

		for _, tc := range []struct {
			name    string
			method  string
			target  any
			body    any
			params  gin.Params
			handler func(*gin.Context)
		}{
			{name: "create", method: http.MethodPost, target: "/api/pages", body: `{"title":"No User","slug":"no-user","kind":"page"}`, handler: deps.routes.handleCreate},
			{name: "update", method: http.MethodPut, target: pageRouteTarget(page.ID), body: routeBodyVersion(`{"version":"`, page.Version(), `","title":"No User","slug":"page"}`), params: ginPageIDParams(page.ID), handler: deps.routes.handleUpdate},
			{name: "delete", method: http.MethodDelete, target: pageRouteTargetWithVersion(page.ID, page.Version()), params: ginPageIDParams(page.ID), handler: deps.routes.handleDelete},
			{name: "move", method: http.MethodPut, target: pageRouteTargetWithSuffix(page.ID, "/move"), body: routeBodyVersion(`{"version":"`, page.Version(), `","parentId":"root"}`), params: ginPageIDParams(page.ID), handler: deps.routes.handleMove},
			{name: "ensure", method: http.MethodPost, target: "/api/pages/ensure", body: `{"path":"no-user","title":"No User","kind":"page"}`, handler: deps.routes.handleEnsurePath},
			{name: "convert", method: http.MethodPost, target: convertPageRouteTarget(page.ID), body: routeBodyVersion(`{"targetKind":"section","version":"`, page.Version(), `"}`), params: ginPageIDParams(page.ID), handler: deps.routes.handleConvert},
			{name: "copy", method: http.MethodPost, target: copyPageRouteTarget(page.ID), body: `{"title":"No User Copy","slug":"no-user-copy"}`, params: ginPageIDParams(page.ID), handler: deps.routes.handleCopy},
			{name: "refactor apply", method: http.MethodPost, target: refactorApplyRouteTarget(page.ID), body: routeBodyVersion(`{"version":"`, page.Version(), `","kind":"rename","title":"No User","slug":"no-user"}`), params: ginPageIDParams(page.ID), handler: deps.routes.handleRefactorApply},
		} {
			rec = performRoutesRequest(tc.method, tc.target, tc.body, tc.params, nil, tc.handler)
			Expect(rec).To(HaveHTTPStatus(http.StatusForbidden), tc.name)
		}
	})

	ginkgo.It("covers refactor use-case edge helpers", func() {
		deps := newRoutesSpecDeps()
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		slug := tree.NewSlugService()
		preview := NewPreviewPageRefactorUseCase(deps.tree, slug, nil, log)
		apply := NewApplyPageRefactorUseCaseWithOrchestrator(deps.tree, slug, nil, nil, log)
		ctx := context.Background()

		section := deps.createPage("Docs", "docs", tree.NodeKindSection, nil)
		page := deps.createPage("Page", "page", tree.NodeKindPage, nil)
		child := deps.createPage("Child", "child", tree.NodeKindPage, &section.ID)

		_, err := preview.Execute(ctx, RefactorPreviewInput{Kind: "copy"})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidRefactorKind))
		badParentID := tree.PageIDFromString(" parent ")
		_, err = preview.Execute(ctx, RefactorPreviewInput{Kind: RefactorKindMove, NewParentID: &badParentID})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidParentID))
		_, err = preview.Execute(ctx, RefactorPreviewInput{Kind: RefactorKindRename, PageID: tree.PageIDFromString("missing"), Title: "New", Slug: "new"})
		Expect(err).To(MatchError(tree.ErrPageNotFound))

		_, err = preview.computeTargetPath(page, RefactorPreviewInput{Kind: RefactorKindRename, Title: "", Slug: "bad slug"})
		Expect(err).To(HavePageValidationFields("title", "slug"))

		routePath, err := preview.computeTargetPath(page, RefactorPreviewInput{Kind: RefactorKindRename, Title: "Renamed", Slug: "renamed"})
		Expect(err).NotTo(HaveOccurred())
		Expect(routePath).To(Equal(tree.RoutePath("renamed")))
		routePath, err = preview.computeTargetPath(child, RefactorPreviewInput{Kind: RefactorKindRename, Title: "Child Two", Slug: "child-two"})
		Expect(err).NotTo(HaveOccurred())
		Expect(routePath).To(Equal(tree.RoutePath("docs/child-two")))
		_, err = preview.computeTargetPath(page, RefactorPreviewInput{Kind: RefactorKindMove, NewParentID: ptrPageID(tree.PageIDFromString("missing"))})
		Expect(err).To(MatchError(tree.ErrPageNotFound))
		_, err = preview.computeTargetPath(page, RefactorPreviewInput{Kind: "copy"})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidRefactorKind))

		routePath, err = preview.resolveParentRoutePath("")
		Expect(err).NotTo(HaveOccurred())
		Expect(routePath).To(BeEmpty())
		routePath, err = preview.resolveParentRoutePath(tree.RootPageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(routePath).To(BeEmpty())
		_, err = preview.resolveParentRoutePath(tree.PageIDFromString("missing"))
		Expect(err).To(MatchError(tree.ErrPageNotFound))

		affected, matched, err := preview.getAffectedPages(page.CalculateRoutePath(), page.Kind, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(affected).To(BeEmpty())
		Expect(matched).To(BeZero())

		_, err = apply.Execute(ctx, RefactorApplyInput{RefactorPreviewInput: RefactorPreviewInput{Kind: "copy"}})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidRefactorKind))
		_, err = apply.Execute(ctx, RefactorApplyInput{RefactorPreviewInput: RefactorPreviewInput{Kind: RefactorKindMove, NewParentID: &badParentID}})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidParentID))
		_, err = apply.Execute(ctx, RefactorApplyInput{RefactorPreviewInput: RefactorPreviewInput{Kind: RefactorKindRename, PageID: tree.PageIDFromString("missing"), Title: "New", Slug: "new"}})
		Expect(err).To(MatchError(tree.ErrPageNotFound))
		_, err = apply.Execute(ctx, RefactorApplyInput{
			Version: tree.PageVersionFromString("stale"),
			RefactorPreviewInput: RefactorPreviewInput{
				Kind:   RefactorKindRename,
				PageID: page.ID,
				Title:  "New",
				Slug:   "new",
			},
		})
		Expect(err).To(MatchError(tree.ErrVersionConflict))

		Expect(validateRefactorVersion(&tree.Page{}, "")).To(Succeed())
		Expect(validateRefactorVersion(page, "")).To(MatchError(tree.ErrVersionRequired))
		Expect(validateRefactorVersion(page, tree.PageVersion("\x00"))).To(Succeed())

		newContent := "updated content"
		snapshots, err := apply.captureSnapshots(page, RefactorApplyInput{RefactorPreviewInput: RefactorPreviewInput{PageID: page.ID, Content: &newContent}})
		Expect(err).NotTo(HaveOccurred())
		Expect(snapshots).To(ContainElement(HaveField("Content", newContent)))
		_, err = apply.captureSnapshots(&tree.Page{PageNode: &tree.PageNode{ID: tree.PageIDFromString("missing"), Kind: tree.NodeKindPage}}, RefactorApplyInput{RefactorPreviewInput: RefactorPreviewInput{PageID: tree.PageIDFromString("missing")}})
		Expect(err).To(MatchError(tree.ErrPageNotFound))

		Expect(apply.rewriteIncomingLinks(RefactorApplyInput{}, &applyRefactorPlan{})).To(Succeed())
		Expect(apply.rewriteAffectedPages("user", "test", nil, nil, nil)).To(Succeed())
		Expect(apply.rewritePathChangedSubtree("user", "test", nil, "old", "new")).To(Succeed())
		Expect(apply.runBulkContentUpdateSideEffects("user", "test", nil)).To(Succeed())
		Expect(planNodeKind([]pathChangeSnapshot{{Kind: tree.NodeKindSection}})).To(Equal(tree.NodeKindPage))
		Expect(ensureStrings(nil)).To(BeEmpty())
		Expect(ensureRefactorWarnings(nil)).To(BeEmpty())
		warnings := collectPreviewWarnings([]RefactorAffectedPage{
			{WarningDetails: []RefactorWarning{{MessageID: "b", Message: "two"}, {MessageID: "a", Message: "one"}, {MessageID: "a", Message: "one"}}},
		})
		Expect(warnings).To(Equal([]RefactorWarning{{MessageID: "a", Message: "one"}, {MessageID: "b", Message: "two"}}))
	})

	ginkgo.It("covers refactor seam branches for links, plans, and bulk rewrites", func() {
		ctx := context.Background()
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		slug := tree.NewSlugService()
		userID := tree.UserIDFromString("refactor-user")

		rootless := testFixturePage("rootless", "Rootless", "rootless", tree.NodeKindPage)
		rootlessTree := fakeTreeWithPages(rootless)
		preview := &PreviewPageRefactorUseCase{tree: rootlessTree, slug: slug, log: log}
		routePath, err := preview.computeTargetPath(rootless, RefactorPreviewInput{Kind: RefactorKindRename, Title: "Renamed", Slug: tree.SlugFromString("renamed")})
		Expect(err).NotTo(HaveOccurred())
		Expect(routePath).To(Equal(tree.RoutePath("renamed")))

		_, err = preview.Execute(ctx, RefactorPreviewInput{Kind: RefactorKindRename, PageID: rootless.ID, Title: "", Slug: tree.SlugFromString("bad slug")})
		Expect(err).To(HavePageValidationFields("title", "slug"))

		linkErr := errors.New("link match query failed")
		previewWithLinkErr := &PreviewPageRefactorUseCase{
			tree:          rootlessTree,
			slug:          slug,
			refactorLinks: &fakeRefactorLinks{err: linkErr},
			log:           log,
		}
		_, err = previewWithLinkErr.Execute(ctx, RefactorPreviewInput{Kind: RefactorKindRename, PageID: rootless.ID, Title: "Renamed", Slug: tree.SlugFromString("renamed")})
		Expect(err).To(MatchError(linkErr))

		excluded := testFixturePage("excluded", "Excluded", "excluded", tree.NodeKindPage)
		first := testFixturePage("first", "Same", "b", tree.NodeKindPage)
		first.Content = "[bad][ref]\n\n[ref]: /old"
		second := testFixturePage("second", "Same", "a", tree.NodeKindPage)
		second.Content = "[bad][ref]\n\n[ref]: /old"
		third := testFixturePage("third", "Alpha", "c", tree.NodeKindPage)
		third.Content = "[bad][ref]\n\n[ref]: /old"
		matches := []links.RefactorLinkMatch{
			{FromPageID: excluded.ID, FromTitle: excluded.Title, ToPath: tree.RoutePath("old"), ToKind: links.TargetKindPage},
			{FromPageID: first.ID, FromTitle: "Same", ToPath: tree.RoutePath("old"), ToKind: links.TargetKindPage},
			{FromPageID: first.ID, FromTitle: "Same", ToPath: tree.RoutePath("old"), ToKind: links.TargetKindPage},
			{FromPageID: second.ID, FromTitle: "Same", ToPath: tree.RoutePath("old/child"), ToKind: links.TargetKindSection},
			{FromPageID: third.ID, FromTitle: "Alpha", ToPath: tree.RoutePath("old/other"), ToKind: links.TargetKindPage},
		}
		previewWithMatches := &PreviewPageRefactorUseCase{
			tree:          fakeTreeWithPages(excluded, first, second, third),
			slug:          slug,
			refactorLinks: &fakeRefactorLinks{matches: matches},
			log:           log,
		}
		affected, matched, err := previewWithMatches.getAffectedPages("old", tree.NodeKindPage, map[tree.PageID]struct{}{excluded.ID: {}})
		Expect(err).NotTo(HaveOccurred())
		Expect(matched).To(Equal(4))
		Expect(affected).To(HaveExactElements(
			matchRefactorAffectedPage(gstruct.Fields{
				"FromTitle": Equal("Alpha"),
			}),
			matchRefactorAffectedPage(gstruct.Fields{
				"FromPath": Equal("/a"),
			}),
			matchRefactorAffectedPage(gstruct.Fields{
				"FromPath":       Equal("/b"),
				"MatchedPaths":   Equal([]string{"/old"}),
				"WarningDetails": Not(BeEmpty()),
			}),
		))

		sourceReadErr := errors.New("source page read failed")
		previewWithSourceErr := &PreviewPageRefactorUseCase{
			tree: &pageUseCaseFakeTree{getPageFunc: func(tree.PageID) (*tree.Page, error) {
				return nil, sourceReadErr
			}},
			slug:          slug,
			refactorLinks: &fakeRefactorLinks{matches: []links.RefactorLinkMatch{{FromPageID: first.ID, FromTitle: first.Title, ToPath: "old"}}},
			log:           log,
		}
		_, _, err = previewWithSourceErr.getAffectedPages("old", tree.NodeKindPage, nil)
		Expect(err).To(MatchError(sourceReadErr))

		planPage := testFixturePage("plan", "Plan", "old", tree.NodeKindPage)
		planChild := testFixtureChildPage(planPage, "plan-child", "Plan Child", "child", tree.NodeKindPage)
		planPage.Children = []*tree.PageNode{planChild.PageNode}
		planAffected := testFixturePage("plan-affected", "Plan Affected", "affected", tree.NodeKindPage)
		applyTree := fakeTreeWithPages(planPage, planChild, planAffected)
		applyPreview := &PreviewPageRefactorUseCase{tree: applyTree, slug: slug, log: log}
		apply := &ApplyPageRefactorUseCase{tree: applyTree, slug: slug, preview: applyPreview, log: log, orchestrator: pagesave.NewPageSaveOrchestrator()}

		_, err = apply.buildApplyPlan(RefactorApplyInput{RefactorPreviewInput: RefactorPreviewInput{Kind: RefactorKindRename, PageID: planPage.ID, Title: "", Slug: tree.SlugFromString("bad slug")}})
		Expect(err).To(HavePageValidationFields("title", "slug"))

		apply.refactorLinks = &fakeRefactorLinks{err: linkErr}
		_, err = apply.buildApplyPlan(RefactorApplyInput{RewriteLinks: true, RefactorPreviewInput: RefactorPreviewInput{Kind: RefactorKindRename, PageID: planPage.ID, Title: "New", Slug: tree.SlugFromString("new")}})
		Expect(err).To(MatchError(linkErr))

		apply.refactorLinks = &fakeRefactorLinks{matches: []links.RefactorLinkMatch{
			{FromPageID: planChild.ID, FromTitle: planChild.Title, ToPath: tree.RoutePath("old"), ToKind: links.TargetKindUnknown},
			{FromPageID: planAffected.ID, FromTitle: planAffected.Title, ToPath: tree.RoutePath("old"), ToKind: links.TargetKindUnknown},
		}}
		plan, err := apply.buildApplyPlan(RefactorApplyInput{RewriteLinks: true, RefactorPreviewInput: RefactorPreviewInput{Kind: RefactorKindRename, PageID: planPage.ID, Title: "New", Slug: tree.SlugFromString("new")}})
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.affectedPageIDs).To(Equal([]tree.PageID{planAffected.ID}))
		Expect(plan.legacyPageLinkSourceIDs).To(HaveKey(planAffected.ID))

		fallbackSnapshotID := tree.PageIDFromString("fallback-snapshot")
		captureTree := &pageUseCaseFakeTree{getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
			Expect(ids).To(Equal([]tree.PageID{fallbackSnapshotID}))
			return []*tree.Page{testPage(fallbackSnapshotID, "Fallback", tree.SlugFromString("fallback"), tree.NodeKindPage)}, []error{nil}
		}}
		captureApply := &ApplyPageRefactorUseCase{tree: captureTree, log: log}
		snapshots, err := captureApply.captureSnapshots(&tree.Page{}, RefactorApplyInput{RefactorPreviewInput: RefactorPreviewInput{PageID: fallbackSnapshotID}})
		Expect(err).NotTo(HaveOccurred())
		Expect(snapshots).To(HaveExactElements(matchPathChangeSnapshot(gstruct.Fields{
			"PageID": Equal(fallbackSnapshotID),
		})))

		nilPageTree := &pageUseCaseFakeTree{getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
			return []*tree.Page{nil}, []error{nil}
		}}
		nilPageApply := &ApplyPageRefactorUseCase{tree: nilPageTree, log: log}
		Expect(nilPageApply.loadPagesByID([]tree.PageID{tree.PageIDFromString("nil-page")}, "nil page")).To(BeEmpty())
		Expect(nilPageApply.loadPagesInOrder(nil, "empty")).To(BeNil())
		Expect(nilPageApply.loadPagesInOrder([]tree.PageID{tree.PageIDFromString("nil-page")}, "nil page")).To(BeEmpty())

		missingRewriteTree := &pageUseCaseFakeTree{getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
			return []*tree.Page{nil}, []error{tree.ErrPageNotFound}
		}}
		missingRewriteApply := &ApplyPageRefactorUseCase{tree: missingRewriteTree, log: log, orchestrator: pagesave.NewPageSaveOrchestrator()}
		Expect(missingRewriteApply.rewriteAffectedPages(userID, "test", []tree.PageID{tree.PageIDFromString("missing")}, []links.RewriteRule{{OldPath: "old", NewPath: "new", Kind: links.TargetKindPage}}, nil)).To(Succeed())

		rewritePage := testFixturePage("rewrite-page", "Rewrite Page", "rewrite-page", tree.NodeKindPage)
		rewritePage.Content = "[Old](/old.md)"
		rewrittenPage := testFixturePage("rewrite-page", "Rewrite Page", "rewrite-page", tree.NodeKindPage)
		rewriteUpdates := 0
		rewriteTree := &pageUseCaseFakeTree{
			getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
				return []*tree.Page{rewritePage}, []error{nil}
			},
			bulkUpdateContentFunc: func(tree.UserID, []tree.BulkContentUpdate) []error {
				rewriteUpdates++
				return []error{nil}
			},
		}
		rewriteApply := &ApplyPageRefactorUseCase{tree: rewriteTree, log: log, orchestrator: pagesave.NewPageSaveOrchestrator()}
		Expect(rewriteApply.rewriteAffectedPages(userID, "test", []tree.PageID{rewritePage.ID}, []links.RewriteRule{{OldPath: "old", NewPath: "new", Kind: links.TargetKindPage}}, map[tree.PageID]struct{}{rewritePage.ID: {}})).To(Succeed())
		Expect(rewriteUpdates).To(Equal(1))

		noRewriteTree := &pageUseCaseFakeTree{getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
			return []*tree.Page{rewrittenPage}, []error{nil}
		}}
		noRewriteApply := &ApplyPageRefactorUseCase{tree: noRewriteTree, log: log, orchestrator: pagesave.NewPageSaveOrchestrator()}
		Expect(noRewriteApply.rewriteAffectedPages(userID, "test", []tree.PageID{rewrittenPage.ID}, []links.RewriteRule{{OldPath: "old", NewPath: "new", Kind: links.TargetKindPage}}, nil)).To(Succeed())

		bulkErr := errors.New("bulk rewrite failed")
		bulkErrTree := &pageUseCaseFakeTree{
			getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
				return []*tree.Page{rewritePage}, []error{nil}
			},
			bulkUpdateContentFunc: func(tree.UserID, []tree.BulkContentUpdate) []error {
				return []error{bulkErr}
			},
		}
		bulkErrApply := &ApplyPageRefactorUseCase{tree: bulkErrTree, log: log, orchestrator: pagesave.NewPageSaveOrchestrator()}
		Expect(bulkErrApply.rewriteAffectedPages(userID, "test", []tree.PageID{rewritePage.ID}, []links.RewriteRule{{OldPath: "old", NewPath: "new", Kind: links.TargetKindPage}}, nil)).To(Succeed())

		sideEffectErr := errors.New("bulk side effect failed")
		sideEffectTree := &pageUseCaseFakeTree{
			getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
				return []*tree.Page{rewritePage}, []error{nil}
			},
			bulkUpdateContentFunc: func(tree.UserID, []tree.BulkContentUpdate) []error {
				return []error{nil}
			},
		}
		sideEffectApply := &ApplyPageRefactorUseCase{
			tree:         sideEffectTree,
			log:          log,
			orchestrator: pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: sideEffectErr}),
		}
		Expect(sideEffectApply.rewriteAffectedPages(userID, "test", []tree.PageID{rewritePage.ID}, []links.RewriteRule{{OldPath: "old", NewPath: "new", Kind: links.TargetKindPage}}, nil)).To(MatchError(sideEffectErr))

		snapshot := pathChangeSnapshot{PageID: rewritePage.ID, OldPath: tree.RoutePath("old/current"), Content: "snapshot content", Kind: tree.NodeKindPage, RootPage: true}
		missingSubtreeApply := &ApplyPageRefactorUseCase{tree: missingRewriteTree, log: log, orchestrator: pagesave.NewPageSaveOrchestrator()}
		Expect(missingSubtreeApply.rewritePathChangedSubtree(userID, "test", []pathChangeSnapshot{snapshot}, "old", "new")).To(Succeed())

		subtreeBulkErrApply := &ApplyPageRefactorUseCase{tree: bulkErrTree, log: log, orchestrator: pagesave.NewPageSaveOrchestrator()}
		Expect(subtreeBulkErrApply.rewritePathChangedSubtree(userID, "test", []pathChangeSnapshot{snapshot}, "old", "new")).To(Succeed())

		subtreeSideEffectApply := &ApplyPageRefactorUseCase{
			tree:         sideEffectTree,
			log:          log,
			orchestrator: pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: sideEffectErr}),
		}
		Expect(subtreeSideEffectApply.rewritePathChangedSubtree(userID, "test", []pathChangeSnapshot{snapshot}, "old", "new")).To(MatchError(sideEffectErr))
	})

	ginkgo.It("covers refactor apply execute exits and route fallback branches", func() {
		ctx := context.Background()
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		slug := tree.NewSlugService()
		userID := tree.UserIDFromString("refactor-execute-user")
		newApply := func(fakeTree *pageUseCaseFakeTree, effect pagesave.PageSideEffect) *ApplyPageRefactorUseCase {
			orchestrator := pagesave.NewPageSaveOrchestrator()
			if effect != nil {
				orchestrator = pagesave.NewPageSaveOrchestrator(effect)
			}
			preview := &PreviewPageRefactorUseCase{tree: fakeTree, slug: slug, log: log}
			return &ApplyPageRefactorUseCase{tree: fakeTree, slug: slug, preview: preview, log: log, orchestrator: orchestrator}
		}

		capturePage := testFixturePage("capture-exec", "Capture Exec", "capture-exec", tree.NodeKindPage)
		captureErr := errors.New("capture snapshots failed")
		captureTree := &pageUseCaseFakeTree{
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				return capturePage, nil
			},
			getPagesFunc: func([]tree.PageID) ([]*tree.Page, []error) {
				return []*tree.Page{nil}, []error{captureErr}
			},
		}
		_, err := newApply(captureTree, nil).Execute(ctx, RefactorApplyInput{
			UserID: userID,
			RefactorPreviewInput: RefactorPreviewInput{
				Kind:   RefactorKindRename,
				PageID: capturePage.ID,
				Title:  "Captured",
				Slug:   tree.SlugFromString("captured"),
			},
		})
		Expect(err).To(MatchError(captureErr))

		renameBefore := testFixturePage("rename-exec", "Rename Exec", "old", tree.NodeKindPage)
		renameAfter := testFixturePage("rename-exec", "Rename Exec", "new", tree.NodeKindPage)
		renameAfter.Content = "after rename"
		renameAffected := testFixturePage("rename-affected", "Rename Affected", "rename-affected", tree.NodeKindPage)
		renameAffected.Content = "[Old](/old.md)"
		renameRewriteErr := errors.New("rename incoming rewrite failed")
		renameUpdated := false
		renameTree := &pageUseCaseFakeTree{
			getPageFunc: func(id tree.PageID) (*tree.Page, error) {
				switch id {
				case renameBefore.ID:
					if renameUpdated {
						return renameAfter, nil
					}
					return renameBefore, nil
				case renameAffected.ID:
					return renameAffected, nil
				default:
					return nil, tree.ErrPageNotFound
				}
			},
			getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
				out := make([]*tree.Page, len(ids))
				errs := make([]error, len(ids))
				for i, id := range ids {
					switch id {
					case renameBefore.ID:
						if renameUpdated {
							out[i] = renameAfter
						} else {
							out[i] = renameBefore
						}
					case renameAffected.ID:
						out[i] = renameAffected
					default:
						errs[i] = tree.ErrPageNotFound
					}
				}
				return out, errs
			},
			updateNodeFunc: func(tree.UserID, tree.PageID, string, tree.Slug, *string, tree.PageVersion, bool) error {
				renameUpdated = true
				return nil
			},
			bulkUpdateContentFunc: func(tree.UserID, []tree.BulkContentUpdate) []error {
				return []error{nil}
			},
		}
		renameApply := newApply(renameTree, &failingSummaryPageSaveEffect{summary: "links rewritten", err: renameRewriteErr})
		renameApply.refactorLinks = &fakeRefactorLinks{matches: []links.RefactorLinkMatch{{FromPageID: renameAffected.ID, FromTitle: renameAffected.Title, ToPath: "old", ToKind: links.TargetKindPage}}}
		_, err = renameApply.Execute(ctx, RefactorApplyInput{
			UserID:       userID,
			RewriteLinks: true,
			RefactorPreviewInput: RefactorPreviewInput{
				Kind:   RefactorKindRename,
				PageID: renameBefore.ID,
				Title:  "Rename Exec",
				Slug:   tree.SlugFromString("new"),
			},
		})
		Expect(err).To(MatchError(renameRewriteErr))

		renameUpdated = false
		renamePathErr := errors.New("rename subtree rewrite failed")
		renamePathApply := newApply(renameTree, &failingSummaryPageSaveEffect{summary: "links rewritten", err: renamePathErr})
		_, err = renamePathApply.Execute(ctx, RefactorApplyInput{
			UserID: userID,
			RefactorPreviewInput: RefactorPreviewInput{
				Kind:   RefactorKindRename,
				PageID: renameBefore.ID,
				Title:  "Rename Exec",
				Slug:   tree.SlugFromString("new"),
			},
		})
		Expect(err).To(MatchError(renamePathErr))

		movePage := testFixturePage("move-exec", "Move Exec", "move-exec", tree.NodeKindPage)
		moveParent := testFixturePage("move-parent", "Move Parent", "move-parent", tree.NodeKindSection)
		moveAffected := testFixturePage("move-affected", "Move Affected", "move-affected", tree.NodeKindPage)
		moveAffected.Content = "[Move](/move-exec.md)"
		moveErr := errors.New("move failed")
		moveErrTree := &pageUseCaseFakeTree{
			getPageFunc: func(id tree.PageID) (*tree.Page, error) {
				if id == moveParent.ID {
					return moveParent, nil
				}
				return movePage, nil
			},
			getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
				return []*tree.Page{movePage}, []error{nil}
			},
			moveNodeFunc: func(tree.UserID, tree.PageID, tree.PageID, tree.PageVersion) error {
				return moveErr
			},
		}
		_, err = newApply(moveErrTree, nil).Execute(ctx, RefactorApplyInput{
			UserID: userID,
			RefactorPreviewInput: RefactorPreviewInput{
				Kind:        RefactorKindMove,
				PageID:      movePage.ID,
				NewParentID: ptrPageID(moveParent.ID),
			},
		})
		Expect(err).To(MatchError(moveErr))

		moveUpdated := false
		moveTree := &pageUseCaseFakeTree{
			getPageFunc: func(id tree.PageID) (*tree.Page, error) {
				switch id {
				case moveParent.ID:
					return moveParent, nil
				case moveAffected.ID:
					return moveAffected, nil
				default:
					return movePage, nil
				}
			},
			getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
				out := make([]*tree.Page, len(ids))
				errs := make([]error, len(ids))
				for i, id := range ids {
					switch id {
					case movePage.ID:
						if moveUpdated {
							movedPage := testPage(movePage.ID, movePage.Title, movePage.Slug, movePage.Kind)
							movedPage.Content = "after move"
							out[i] = movedPage
						} else {
							out[i] = movePage
						}
					case moveAffected.ID:
						out[i] = moveAffected
					default:
						errs[i] = tree.ErrPageNotFound
					}
				}
				return out, errs
			},
			moveNodeFunc: func(tree.UserID, tree.PageID, tree.PageID, tree.PageVersion) error {
				moveUpdated = true
				return nil
			},
			bulkUpdateContentFunc: func(tree.UserID, []tree.BulkContentUpdate) []error {
				Expect(moveUpdated).To(BeTrue())
				return []error{nil}
			},
		}
		moveRewriteErr := errors.New("move incoming rewrite failed")
		moveApply := newApply(moveTree, &failingSummaryPageSaveEffect{summary: "links rewritten", err: moveRewriteErr})
		moveApply.refactorLinks = &fakeRefactorLinks{matches: []links.RefactorLinkMatch{{FromPageID: moveAffected.ID, FromTitle: moveAffected.Title, ToPath: "move-exec", ToKind: links.TargetKindPage}}}
		_, err = moveApply.Execute(ctx, RefactorApplyInput{
			UserID:       userID,
			RewriteLinks: true,
			RefactorPreviewInput: RefactorPreviewInput{
				Kind:        RefactorKindMove,
				PageID:      movePage.ID,
				NewParentID: ptrPageID(moveParent.ID),
			},
		})
		Expect(err).To(MatchError(moveRewriteErr))

		moveUpdated = false
		movePathErr := errors.New("move subtree rewrite failed")
		movePathApply := newApply(moveTree, &failingSummaryPageSaveEffect{summary: "links rewritten", err: movePathErr})
		_, err = movePathApply.Execute(ctx, RefactorApplyInput{
			UserID: userID,
			RefactorPreviewInput: RefactorPreviewInput{
				Kind:        RefactorKindMove,
				PageID:      movePage.ID,
				NewParentID: ptrPageID(moveParent.ID),
			},
		})
		Expect(err).To(MatchError(movePathErr))

		deps := newRoutesSpecDeps()
		unloadedTree := tree.NewTreeService(ginkgo.GinkgoT().TempDir())
		var noKind tree.NodeKind
		_, err = (&Routes{treeService: unloadedTree}).findByPathInput(ctx, "", noKind)
		Expect(err).To(MatchError(tree.ErrTreeNotLoaded))

		docs := deps.createPage("Readme Route Docs", "readme-route-docs", tree.NodeKindSection, nil)
		readmeDir := filepath.Join(deps.tree.RootDir(), docs.CalculateRoutePath().FilesystemPath())
		Expect(os.MkdirAll(readmeDir, 0o755)).To(Succeed())
		if err := os.Remove(filepath.Join(readmeDir, "index.md")); err != nil {
			Expect(err).To(MatchError(os.ErrNotExist))
		}
		Expect(os.WriteFile(filepath.Join(readmeDir, "README.md"), []byte("# Readme Route Docs"), 0o644)).To(Succeed())
		sectionOut, err := deps.routes.findByPathInput(ctx, readmeLookupPathForRoute(docs.CalculateRoutePath()), tree.NodeKindSection)
		Expect(err).NotTo(HaveOccurred())
		Expect(sectionOut.Page.ID).To(Equal(docs.ID))
	})

	ginkgo.It("covers direct page mutation validation and error branches", func() {
		deps := newRoutesSpecDeps()
		ctx := context.Background()
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		slug := tree.NewSlugService()
		kindPage := tree.NodeKindPage
		kindSection := tree.NodeKindSection
		invalidKind := tree.NodeKind("folder")

		_, err := deps.routes.createPage.Execute(ctx, CreatePageInput{Title: "", Slug: tree.SlugFromString("bad slug"), Kind: &invalidKind})
		Expect(err).To(HavePageValidationFields("title", "kind", "slug"))

		badParentID := tree.PageIDFromString(" parent ")
		_, err = deps.routes.createPage.Execute(ctx, CreatePageInput{Title: "Bad Parent", Slug: tree.SlugFromString("bad-parent"), Kind: &kindPage, ParentID: &badParentID})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidParentID))
		_, err = deps.routes.createPage.Execute(ctx, CreatePageInput{Title: "Missing Parent", Slug: tree.SlugFromString("missing-parent"), Kind: &kindPage, ParentID: ptrPageID(tree.PageIDFromString("missing"))})
		Expect(err).To(MatchError(tree.ErrPageNotFound))
		existing := deps.createPage("Existing", "existing", tree.NodeKindPage, nil)
		_, err = deps.routes.createPage.Execute(ctx, CreatePageInput{Title: "Conflict", Slug: existing.Slug, Kind: &kindPage})
		Expect(err).To(MatchError(tree.ErrPageAlreadyExists))
		createFailure := errors.New("create side effect failed")
		_, err = NewCreatePageUseCase(deps.tree, slug, pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: createFailure}), log).Execute(ctx, CreatePageInput{
			UserID: tree.UserIDFromString("routes-test-user"),
			Title:  "Create Fails",
			Slug:   tree.SlugFromString("create-fails"),
			Kind:   &kindPage,
		})
		Expect(err).To(MatchError(createFailure))

		_, err = deps.routes.updatePage.Execute(ctx, UpdatePageInput{Title: "Bad Slug", Slug: tree.SlugFromString("bad slug")})
		Expect(err).To(HavePageValidationField("slug"))
		_, err = deps.routes.updatePage.Execute(ctx, UpdatePageInput{ID: tree.PageIDFromString("missing"), Title: "Missing", Slug: tree.SlugFromString("missing")})
		Expect(err).To(MatchError(tree.ErrPageNotFound))
		updatePage := deps.createPage("Update", "update", tree.NodeKindPage, nil)
		Expect(updatePage.Version()).NotTo(BeEmpty())
		_, err = deps.routes.updatePage.Execute(ctx, UpdatePageInput{
			ID:      updatePage.ID,
			Version: tree.PageVersionFromString("stale"),
			Title:   updatePage.Title,
			Slug:    updatePage.Slug,
		})
		Expect(err).To(MatchError(tree.ErrVersionConflict))
		updateFailure := errors.New("update side effect failed")
		_, err = NewUpdatePageUseCase(deps.tree, slug, pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: updateFailure}), log).Execute(ctx, UpdatePageInput{
			UserID:  tree.UserIDFromString("routes-test-user"),
			ID:      updatePage.ID,
			Version: updatePage.Version(),
			Title:   "Update Side Effect",
			Slug:    updatePage.Slug,
		})
		Expect(err).To(MatchError(updateFailure))

		Expect(deps.routes.deletePage.Execute(ctx, DeletePageInput{ID: tree.RootPageID})).To(MatchPageLocalizedCode(ErrCodePageRootOperation))
		Expect(deps.routes.deletePage.Execute(ctx, DeletePageInput{ID: tree.PageIDFromString("missing"), Version: tree.PageVersionFromString("stale")})).To(MatchError(tree.ErrPageNotFound))
		parent := deps.createPage("Delete Parent", "delete-parent", tree.NodeKindSection, nil)
		_ = deps.createPage("Delete Child", "delete-child", tree.NodeKindPage, &parent.ID)
		Expect(deps.routes.deletePage.Execute(ctx, DeletePageInput{ID: parent.ID, Version: parent.Version()})).To(MatchError(tree.ErrPageHasChildren))
		deletePage := deps.createPage("Delete Stale", "delete-stale", tree.NodeKindPage, nil)
		Expect(deps.routes.deletePage.Execute(ctx, DeletePageInput{ID: deletePage.ID, Version: tree.PageVersionFromString("stale")})).To(MatchError(tree.ErrVersionConflict))
		recursiveParent := deps.createPage("Recursive Delete", "recursive-delete", tree.NodeKindSection, nil)
		_ = deps.createPage("Recursive Child", "recursive-child", tree.NodeKindPage, &recursiveParent.ID)
		Expect(deps.routes.deletePage.Execute(ctx, DeletePageInput{ID: recursiveParent.ID, Version: recursiveParent.Version(), Recursive: true})).To(Succeed())
		deleteFailure := errors.New("delete side effect failed")
		deleteFailurePage := deps.createPage("Delete Failure", "delete-failure", tree.NodeKindPage, nil)
		Expect(NewDeletePageUseCase(deps.tree, assets.NewAssetService(ginkgo.GinkgoT().TempDir(), slug), pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: deleteFailure}), log).Execute(ctx, DeletePageInput{
			UserID:  tree.UserIDFromString("routes-test-user"),
			ID:      deleteFailurePage.ID,
			Version: deleteFailurePage.Version(),
		})).To(MatchError(deleteFailure))

		Expect(deps.routes.movePage.Execute(ctx, MovePageInput{ID: tree.RootPageID})).To(MatchPageLocalizedCode(ErrCodePageRootOperation))
		Expect(deps.routes.movePage.Execute(ctx, MovePageInput{ID: existing.ID, ParentID: badParentID, Version: existing.Version()})).To(MatchPageLocalizedCode(ErrCodePageInvalidParentID))
		moveDest := deps.createPage("Move Dest", "move-dest", tree.NodeKindSection, nil)
		Expect(deps.routes.movePage.Execute(ctx, MovePageInput{ID: tree.PageIDFromString("missing"), ParentID: moveDest.ID, Version: tree.PageVersionFromString("stale")})).To(MatchError(tree.ErrPageNotFound))
		movePage := deps.createPage("Move Stale", "move-stale", tree.NodeKindPage, nil)
		Expect(deps.routes.movePage.Execute(ctx, MovePageInput{ID: movePage.ID, ParentID: moveDest.ID, Version: tree.PageVersionFromString("stale")})).To(MatchError(tree.ErrVersionConflict))
		moveFailure := errors.New("move side effect failed")
		moveFailurePage := deps.createPage("Move Failure", "move-failure", tree.NodeKindPage, nil)
		Expect(NewMovePageUseCase(deps.tree, pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: moveFailure}), log).Execute(ctx, MovePageInput{
			UserID:   tree.UserIDFromString("routes-test-user"),
			ID:       moveFailurePage.ID,
			ParentID: moveDest.ID,
			Version:  moveFailurePage.Version(),
		})).To(MatchError(moveFailure))

		Expect(deps.routes.convertPage.Execute(ctx, ConvertPageInput{ID: tree.RootPageID})).To(MatchPageLocalizedCode(ErrCodePageRootOperation))
		Expect(deps.routes.convertPage.Execute(ctx, ConvertPageInput{ID: tree.PageIDFromString("missing"), Version: tree.PageVersionFromString("stale"), TargetKind: tree.NodeKindSection})).To(MatchError(tree.ErrPageNotFound))
		convertStale := deps.createPage("Convert Stale", "convert-stale", tree.NodeKindPage, nil)
		Expect(deps.routes.convertPage.Execute(ctx, ConvertPageInput{ID: convertStale.ID, Version: tree.PageVersionFromString("stale"), TargetKind: tree.NodeKindSection})).To(MatchError(tree.ErrVersionConflict))
		convertParent := deps.createPage("Convert Parent", "convert-parent", tree.NodeKindSection, nil)
		_ = deps.createPage("Convert Child", "convert-child", tree.NodeKindPage, &convertParent.ID)
		Expect(deps.routes.convertPage.Execute(ctx, ConvertPageInput{ID: convertParent.ID, Version: convertParent.Version(), TargetKind: tree.NodeKindPage})).To(MatchError(tree.ErrPageHasChildren))
		convertFailurePage := deps.createPage("Convert Failure", "convert-failure", tree.NodeKindPage, nil)
		convertFailure := errors.New("convert side effect failed")
		Expect(NewConvertPageUseCase(deps.tree, pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: convertFailure}), log).Execute(ctx, ConvertPageInput{
			UserID:     tree.UserIDFromString("routes-test-user"),
			ID:         convertFailurePage.ID,
			Version:    convertFailurePage.Version(),
			TargetKind: tree.NodeKindSection,
		})).To(MatchError(convertFailure))
		convertWithoutEffects := deps.createPage("Convert Without Effects", "convert-without-effects", tree.NodeKindPage, nil)
		Expect(NewConvertPageUseCase(deps.tree, nil, log).Execute(ctx, ConvertPageInput{ID: convertWithoutEffects.ID, Version: convertWithoutEffects.Version(), TargetKind: tree.NodeKindSection})).To(Succeed())

		_, err = deps.routes.copyPage.Execute(ctx, CopyPageInput{Title: "", Slug: tree.SlugFromString("bad slug")})
		Expect(err).To(HavePageValidationFields("title", "slug"))
		_, err = deps.routes.copyPage.Execute(ctx, CopyPageInput{SourcePageID: existing.ID, TargetParentID: &badParentID, Title: "Bad Parent", Slug: tree.SlugFromString("bad-parent")})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidParentID))
		_, err = deps.routes.copyPage.Execute(ctx, CopyPageInput{SourcePageID: tree.PageIDFromString("missing"), Title: "Missing", Slug: tree.SlugFromString("missing")})
		Expect(err).To(MatchError(tree.ErrPageNotFound))
		_, err = deps.routes.copyPage.Execute(ctx, CopyPageInput{SourcePageID: existing.ID, TargetParentID: ptrPageID(tree.PageIDFromString("missing")), Title: "Missing Parent", Slug: tree.SlugFromString("copy-missing-parent")})
		Expect(err).To(MatchError(tree.ErrParentNotFound), "error = %v", err)
		_, err = deps.routes.copyPage.Execute(ctx, CopyPageInput{SourcePageID: existing.ID, Title: "Conflict Copy", Slug: existing.Slug})
		Expect(err).To(MatchError(tree.ErrPageAlreadyExists))
		copyFailurePage := deps.createPage("Copy Failure", "copy-failure", tree.NodeKindPage, nil)
		copyFailure := errors.New("copy side effect failed")
		_, err = NewCopyPageUseCase(deps.tree, slug, pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: copyFailure}), assets.NewAssetService(ginkgo.GinkgoT().TempDir(), slug), log).Execute(ctx, CopyPageInput{
			UserID:       tree.UserIDFromString("routes-test-user"),
			SourcePageID: copyFailurePage.ID,
			Title:        "Copy Failure Result",
			Slug:         tree.SlugFromString("copy-failure-result"),
		})
		Expect(err).To(MatchError(copyFailure))

		_, err = deps.routes.ensurePath.Execute(ctx, EnsurePathInput{TargetPath: "", TargetTitle: ""})
		Expect(err).To(HavePageValidationFields("path", "title"))
		ensuredExisting := deps.createPage("Ensured Existing", "ensured-existing", tree.NodeKindPage, nil)
		ensureOut, err := deps.routes.ensurePath.Execute(ctx, EnsurePathInput{TargetPath: ensuredExisting.CalculateRoutePath(), TargetTitle: "Ignored", Kind: &kindPage})
		Expect(err).NotTo(HaveOccurred())
		Expect(ensureOut.Page.ID).To(Equal(ensuredExisting.ID))
		ensureFailure := errors.New("ensure side effect failed")
		_, err = NewEnsurePathUseCase(deps.tree, slug, pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: ensureFailure}), log).Execute(ctx, EnsurePathInput{
			UserID:      tree.UserIDFromString("routes-test-user"),
			TargetPath:  "ensure/failure",
			TargetTitle: "Failure",
			Kind:        &kindSection,
		})
		Expect(err).To(MatchError(ensureFailure))

		Expect(lookupFinalKindMatches(nil, tree.NodeKindPage)).To(BeFalse())
		Expect(lookupFinalKindMatches(&tree.PathLookup{}, tree.NodeKindPage)).To(BeFalse())
		Expect(collectSubtreeIDs(nil)).To(BeEmpty())
	})

	ginkgo.It("covers remaining validation, README fallback, and route error branches", func() {
		deps := newRoutesSpecDeps()

		_, _, err := NormalizePagePathInput("docs/page.md", "folder")
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidKind))
		_, _, err = NormalizePagePathInput("../escape.md", "")
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidPath))
		badParent := " parent "
		_, err = ValidateOptionalParentID(&badParent)
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidParentID))

		_, handled, err := FindReadmeMarkdownPathFallback("../README.md", tree.NodeKindSection, ReadmeMarkdownPathFallbackLookup{})
		Expect(handled).To(BeTrue())
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidPath))
		_, handled, err = FindReadmeMarkdownPathFallback("../README.md", tree.NodeKindPage, ReadmeMarkdownPathFallbackLookup{})
		Expect(handled).To(BeTrue())
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidPath))
		rootDir := ginkgo.GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(rootDir, "README.md"), []byte("# Root"), 0o644)).To(Succeed())
		rootLookupErr := errors.New("root lookup failed")
		_, handled, err = FindReadmeMarkdownPathFallback("README.md", tree.NodeKindSection, ReadmeMarkdownPathFallbackLookup{
			RootDir: rootDir,
			RootPage: func() (*tree.Page, error) {
				return nil, rootLookupErr
			},
		})
		Expect(handled).To(BeTrue())
		Expect(err).To(MatchError(rootLookupErr))

		rec := performRoutesRequest(http.MethodGet, "/api/pages/by-path?path=missing", "", nil, nil, deps.routes.handleGetByPath)
		Expect(rec).To(HavePageErrorResponse(http.StatusNotFound, ErrCodePageNotFound), rec.Body.String())
		rec = performRoutesRequest(http.MethodGet, "/api/pages/by-path?path=docs/page.md&kind=section", "", nil, nil, deps.routes.handleGetByPath)
		Expect(rec).To(HavePageErrorResponse(http.StatusBadRequest, ErrCodePageInvalidKind), rec.Body.String())
		rec = performRoutesRequest(http.MethodGet, "/api/pages/permalink/missing", "", ginParams("id", "missing"), nil, deps.routes.handleResolvePermalink)
		Expect(rec).To(HavePageErrorResponse(http.StatusNotFound, ErrCodePageNotFound), rec.Body.String())
		unloadedTree := tree.NewTreeService(ginkgo.GinkgoT().TempDir())
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

	ginkgo.It("covers page use-case post-mutation failure seams", func() {
		ctx := context.Background()
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		slug := tree.NewSlugService()
		userID := tree.UserIDFromString("fake-user")
		kindPage := tree.NodeKindPage
		kindSection := tree.NodeKindSection
		noEffects := pagesave.NewPageSaveOrchestrator()

		ginkgo.By("create returning an error after the node has been inserted")
		createdID := tree.PageIDFromString("created")
		createReadErr := errors.New("created page read failed")
		createTree := &pageUseCaseFakeTree{
			createNodeFunc: func(tree.UserID, *tree.PageID, string, tree.Slug, *tree.NodeKind) (*tree.PageID, error) {
				return &createdID, nil
			},
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				return nil, createReadErr
			},
		}
		_, err := (&CreatePageUseCase{tree: createTree, slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, CreatePageInput{
			UserID: userID,
			Title:  "Created",
			Slug:   tree.SlugFromString("created"),
			Kind:   &kindPage,
		})
		Expect(err).To(MatchError(createReadErr))

		ginkgo.By("convert returning an error after the conversion succeeds")
		convertReadErr := errors.New("converted page read failed")
		convertCalls := 0
		convertPage := testFixturePage("convert", "Convert", "convert", tree.NodeKindPage)
		convertTree := &pageUseCaseFakeTree{
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				convertCalls++
				if convertCalls == 1 {
					return convertPage, nil
				}
				return nil, convertReadErr
			},
			convertNodeFunc: func(tree.UserID, tree.PageID, tree.NodeKind, tree.PageVersion) error {
				return nil
			},
		}
		Expect((&ConvertPageUseCase{tree: convertTree, orchestrator: noEffects, log: log}).Execute(ctx, ConvertPageInput{
			UserID:     userID,
			ID:         convertPage.ID,
			TargetKind: tree.NodeKindSection,
		})).To(MatchError(convertReadErr))

		ginkgo.By("copy cleanup when the copied page cannot be fetched")
		sourcePage := testFixturePage("copy-source", "Copy Source", "copy-source", tree.NodeKindPage)
		copyID := tree.PageIDFromString("copy-created")
		copyFetchErr := errors.New("copied page read failed")
		cleanupDeletes := 0
		copyFetchCalls := 0
		copyFetchTree := &pageUseCaseFakeTree{
			getPageFunc: func(id tree.PageID) (*tree.Page, error) {
				copyFetchCalls++
				if copyFetchCalls == 1 {
					Expect(id).To(Equal(sourcePage.ID))
					return sourcePage, nil
				}
				Expect(id).To(Equal(copyID))
				return nil, copyFetchErr
			},
			createNodeFunc: func(tree.UserID, *tree.PageID, string, tree.Slug, *tree.NodeKind) (*tree.PageID, error) {
				return &copyID, nil
			},
			deleteNodeUncheckedVersionFunc: func(tree.UserID, tree.PageID, bool) error {
				cleanupDeletes++
				return nil
			},
		}
		_, err = (&CopyPageUseCase{tree: copyFetchTree, slug: slug, assets: &fakePageAssets{}, orchestrator: noEffects, log: log}).Execute(ctx, CopyPageInput{
			UserID:       userID,
			SourcePageID: sourcePage.ID,
			Title:        "Copy",
			Slug:         tree.SlugFromString("copy"),
		})
		Expect(err).To(MatchError(copyFetchErr))
		Expect(cleanupDeletes).To(Equal(1))

		ginkgo.By("copy asset cleanup when the copied content update fails")
		copyPage := testPage(copyID, "Copy", tree.SlugFromString("copy"), tree.NodeKindPage)
		updateErr := errors.New("copy content update failed")
		assetsAfterUpdateErr := &fakePageAssets{}
		copyUpdateCalls := 0
		copyUpdateTree := &pageUseCaseFakeTree{
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				copyUpdateCalls++
				if copyUpdateCalls == 1 {
					return sourcePage, nil
				}
				return copyPage, nil
			},
			createNodeFunc: func(tree.UserID, *tree.PageID, string, tree.Slug, *tree.NodeKind) (*tree.PageID, error) {
				return &copyID, nil
			},
			deleteNodeUncheckedVersionFunc: func(tree.UserID, tree.PageID, bool) error {
				return nil
			},
			updateNodeUncheckedVersionFunc: func(tree.UserID, tree.PageID, string, tree.Slug, *string, bool) error {
				return updateErr
			},
		}
		_, err = (&CopyPageUseCase{tree: copyUpdateTree, slug: slug, assets: assetsAfterUpdateErr, orchestrator: noEffects, log: log}).Execute(ctx, CopyPageInput{
			UserID:       userID,
			SourcePageID: sourcePage.ID,
			Title:        "Copy",
			Slug:         tree.SlugFromString("copy"),
		})
		Expect(err).To(MatchError(updateErr))
		Expect(pageAssetCallCounts(assetsAfterUpdateErr)).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Copy":   Equal(1),
			"Delete": Equal(1),
		}))

		ginkgo.By("copy returning an error when the final page fetch fails")
		finalReadErr := errors.New("final copy read failed")
		copyFinalCalls := 0
		copyFinalTree := &pageUseCaseFakeTree{
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				copyFinalCalls++
				switch copyFinalCalls {
				case 1:
					return sourcePage, nil
				case 2:
					return copyPage, nil
				default:
					return nil, finalReadErr
				}
			},
			createNodeFunc: func(tree.UserID, *tree.PageID, string, tree.Slug, *tree.NodeKind) (*tree.PageID, error) {
				return &copyID, nil
			},
			deleteNodeUncheckedVersionFunc: func(tree.UserID, tree.PageID, bool) error {
				return nil
			},
			updateNodeUncheckedVersionFunc: func(tree.UserID, tree.PageID, string, tree.Slug, *string, bool) error {
				return nil
			},
		}
		_, err = (&CopyPageUseCase{tree: copyFinalTree, slug: slug, assets: &fakePageAssets{}, orchestrator: noEffects, log: log}).Execute(ctx, CopyPageInput{
			UserID:       userID,
			SourcePageID: sourcePage.ID,
			Title:        "Copy",
			Slug:         tree.SlugFromString("copy"),
		})
		Expect(err).To(MatchError(finalReadErr))

		ginkgo.By("update returning an error after the update succeeds")
		updateBefore := testFixturePage("update", "Update", "update", tree.NodeKindPage)
		updateReadErr := errors.New("updated page read failed")
		updateCalls := 0
		updateTree := &pageUseCaseFakeTree{
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				updateCalls++
				if updateCalls == 1 {
					return updateBefore, nil
				}
				return nil, updateReadErr
			},
			updateNodeFunc: func(tree.UserID, tree.PageID, string, tree.Slug, *string, tree.PageVersion, bool) error {
				return nil
			},
		}
		_, err = (&UpdatePageUseCase{tree: updateTree, slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, UpdatePageInput{
			UserID: userID,
			ID:     updateBefore.ID,
			Title:  "Update",
			Slug:   tree.SlugFromString("update"),
		})
		Expect(err).To(MatchError(updateReadErr))

		ginkgo.By("update warning and continuing when a slug-change affected page fails to load")
		updateChild := testFixtureChildPage(updateBefore, "update-child", "Update Child", "child", tree.NodeKindPage)
		updateBefore.Children = []*tree.PageNode{updateChild.PageNode}
		updateAfter := testFixturePage("update", "Renamed", "renamed", tree.NodeKindPage)
		affectedErr := errors.New("affected update page failed")
		updateAffectedCalls := 0
		updateAffectedTree := &pageUseCaseFakeTree{
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				updateAffectedCalls++
				if updateAffectedCalls == 1 {
					return updateBefore, nil
				}
				return updateAfter, nil
			},
			updateNodeFunc: func(tree.UserID, tree.PageID, string, tree.Slug, *string, tree.PageVersion, bool) error {
				return nil
			},
			getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
				Expect(ids).To(Equal([]tree.PageID{updateBefore.ID, updateChild.ID}))
				return []*tree.Page{updateAfter, nil}, []error{nil, affectedErr}
			},
		}
		out, err := (&UpdatePageUseCase{tree: updateAffectedTree, slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, UpdatePageInput{
			UserID: userID,
			ID:     updateBefore.ID,
			Title:  "Renamed",
			Slug:   tree.SlugFromString("renamed"),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.Page).To(BeIdenticalTo(updateAfter))

		ginkgo.By("delete warning and continuing when recursive affected pages or assets fail")
		deleteParent := testFixturePage("delete-parent", "Delete Parent", "delete-parent", tree.NodeKindSection)
		deleteChild := testFixtureChildPage(deleteParent, "delete-child", "Delete Child", "child", tree.NodeKindPage)
		deleteParent.Children = []*tree.PageNode{deleteChild.PageNode}
		deleteAssets := &fakePageAssets{deleteErr: errors.New("asset delete failed")}
		deleteGetPagesErr := errors.New("recursive child read failed")
		deleteTree := &pageUseCaseFakeTree{
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				return deleteParent, nil
			},
			getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
				Expect(ids).To(Equal([]tree.PageID{deleteParent.ID, deleteChild.ID}))
				return []*tree.Page{deleteParent, nil}, []error{nil, deleteGetPagesErr}
			},
			deleteNodeFunc: func(tree.UserID, tree.PageID, bool, tree.PageVersion) error {
				return nil
			},
		}
		Expect((&DeletePageUseCase{tree: deleteTree, assets: deleteAssets, orchestrator: noEffects, log: log}).Execute(ctx, DeletePageInput{
			UserID:    userID,
			ID:        deleteParent.ID,
			Recursive: true,
		})).To(Succeed())
		Expect(deleteAssets.deleteCalls).To(Equal(1))

		ginkgo.By("delete returning recursive orchestrator errors")
		recursiveEffectErr := errors.New("recursive delete side effect failed")
		recursiveEffectTree := &pageUseCaseFakeTree{
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				return deleteParent, nil
			},
			getPagesFunc: func([]tree.PageID) ([]*tree.Page, []error) {
				return []*tree.Page{deleteParent, deleteChild}, []error{nil, nil}
			},
			deleteNodeFunc: func(tree.UserID, tree.PageID, bool, tree.PageVersion) error {
				return nil
			},
		}
		Expect((&DeletePageUseCase{
			tree:         recursiveEffectTree,
			assets:       &fakePageAssets{},
			orchestrator: pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: recursiveEffectErr}),
			log:          log,
		}).Execute(ctx, DeletePageInput{UserID: userID, ID: deleteParent.ID, Recursive: true})).To(MatchError(recursiveEffectErr))

		ginkgo.By("delete warning and continuing when non-recursive asset deletion fails")
		nonRecursiveAssets := &fakePageAssets{deleteErr: errors.New("non-recursive asset delete failed")}
		nonRecursiveTree := &pageUseCaseFakeTree{
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				return sourcePage, nil
			},
			deleteNodeFunc: func(tree.UserID, tree.PageID, bool, tree.PageVersion) error {
				return nil
			},
		}
		Expect((&DeletePageUseCase{tree: nonRecursiveTree, assets: nonRecursiveAssets, orchestrator: noEffects, log: log}).Execute(ctx, DeletePageInput{
			UserID: userID,
			ID:     sourcePage.ID,
		})).To(Succeed())
		Expect(nonRecursiveAssets.deleteCalls).To(Equal(1))

		ginkgo.By("move warning and continuing when affected pages fail to load")
		moveParent := testFixturePage("move", "Move", "move", tree.NodeKindSection)
		moveChild := testFixtureChildPage(moveParent, "move-child", "Move Child", "child", tree.NodeKindPage)
		moveParent.Children = []*tree.PageNode{moveChild.PageNode}
		moveAffectedErr := errors.New("moved child read failed")
		moveTree := &pageUseCaseFakeTree{
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				return moveParent, nil
			},
			moveNodeFunc: func(tree.UserID, tree.PageID, tree.PageID, tree.PageVersion) error {
				return nil
			},
			getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
				Expect(ids).To(Equal([]tree.PageID{moveParent.ID, moveChild.ID}))
				return []*tree.Page{moveParent, nil}, []error{nil, moveAffectedErr}
			},
		}
		Expect((&MovePageUseCase{tree: moveTree, orchestrator: noEffects, log: log}).Execute(ctx, MovePageInput{
			UserID:   userID,
			ID:       moveParent.ID,
			ParentID: tree.RootPageID,
		})).To(Succeed())

		ginkgo.By("ensure path lookup, existing-page, creation, and post-create failures")
		lookupErr := errors.New("lookup failed")
		_, err = (&EnsurePathUseCase{tree: &pageUseCaseFakeTree{
			lookupPagePathFunc: func(tree.RoutePath) (*tree.PathLookup, error) {
				return nil, lookupErr
			},
		}, slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, EnsurePathInput{
			UserID:      userID,
			TargetPath:  "lookup",
			TargetTitle: "Lookup",
			Kind:        &kindPage,
		})
		Expect(err).To(MatchError(lookupErr))

		finalID := tree.PageIDFromString("existing-final")
		finalKind := tree.NodeKindPage
		existingFinalReadErr := errors.New("existing final read failed")
		_, err = (&EnsurePathUseCase{tree: &pageUseCaseFakeTree{
			lookupPagePathFunc: func(routePath tree.RoutePath) (*tree.PathLookup, error) {
				return &tree.PathLookup{
					Path:   routePath,
					Exists: true,
					Segments: []tree.PathSegment{{
						Slug:   tree.SlugFromString("existing-final"),
						Exists: true,
						Kind:   &finalKind,
						ID:     &finalID,
					}},
				}, nil
			},
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				return nil, existingFinalReadErr
			},
		}, slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, EnsurePathInput{
			UserID:      userID,
			TargetPath:  "existing-final",
			TargetTitle: "Existing",
			Kind:        &kindPage,
		})
		Expect(err).To(MatchError(existingFinalReadErr))

		_, err = (&EnsurePathUseCase{tree: &pageUseCaseFakeTree{
			lookupPagePathFunc: func(routePath tree.RoutePath) (*tree.PathLookup, error) {
				return &tree.PathLookup{
					Path: routePath,
					Segments: []tree.PathSegment{{
						Slug:   tree.SlugFromString("bad slug"),
						Exists: false,
					}},
				}, nil
			},
		}, slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, EnsurePathInput{
			UserID:      userID,
			TargetPath:  "invalid-segment",
			TargetTitle: "Invalid Segment",
			Kind:        &kindPage,
		})
		Expect(err).To(HavePageValidationField("path"))

		ensureErr := errors.New("ensure failed")
		_, err = (&EnsurePathUseCase{tree: &pageUseCaseFakeTree{
			lookupPagePathFunc: func(routePath tree.RoutePath) (*tree.PathLookup, error) {
				return &tree.PathLookup{Path: routePath, CanCreate: true}, nil
			},
			ensurePagePathFunc: func(tree.UserID, tree.RoutePath, string, *tree.NodeKind) (*tree.EnsurePathResult, error) {
				return nil, ensureErr
			},
		}, slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, EnsurePathInput{
			UserID:      userID,
			TargetPath:  "ensure-fails",
			TargetTitle: "Ensure Fails",
			Kind:        &kindPage,
		})
		Expect(err).To(MatchError(ensureErr))

		resultNode := testFixturePage("ensure-result", "Ensure Result", "ensure-result", tree.NodeKindPage).PageNode
		createdNode := testFixturePage("ensure-created", "Ensure Created", "created", tree.NodeKindPage).PageNode
		resultReadErr := errors.New("result page read failed")
		_, err = (&EnsurePathUseCase{tree: fakeEnsureTree(resultNode, []*tree.PageNode{createdNode}, func(ids []tree.PageID) ([]*tree.Page, []error) {
			Expect(ids).To(Equal([]tree.PageID{resultNode.ID, createdNode.ID}))
			return []*tree.Page{nil, nil}, []error{resultReadErr, nil}
		}), slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, EnsurePathInput{
			UserID:      userID,
			TargetPath:  "ensure-result",
			TargetTitle: "Ensure Result",
			Kind:        &kindPage,
		})
		Expect(err).To(MatchError(resultReadErr))

		_, err = (&EnsurePathUseCase{tree: fakeEnsureTree(resultNode, []*tree.PageNode{createdNode}, func(ids []tree.PageID) ([]*tree.Page, []error) {
			Expect(ids).To(Equal([]tree.PageID{resultNode.ID, createdNode.ID}))
			return []*tree.Page{testPage(resultNode.ID, "Ensure Result", tree.SlugFromString("ensure-result"), tree.NodeKindPage), nil}, []error{nil, errors.New("created page read failed")}
		}), slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, EnsurePathInput{
			UserID:      userID,
			TargetPath:  "ensure-created-error",
			TargetTitle: "Ensure Created Error",
			Kind:        &kindPage,
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = (&EnsurePathUseCase{tree: fakeEnsureTree(resultNode, nil, func(ids []tree.PageID) ([]*tree.Page, []error) {
			Expect(ids).To(Equal([]tree.PageID{resultNode.ID}))
			return []*tree.Page{nil}, []error{nil}
		}), slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, EnsurePathInput{
			UserID:      userID,
			TargetPath:  "ensure-missing-result",
			TargetTitle: "Ensure Missing Result",
			Kind:        &kindPage,
		})
		Expect(err).To(MatchError(tree.ErrPageNotFound))

		_, err = (&EnsurePathUseCase{tree: fakeEnsureTree(resultNode, []*tree.PageNode{createdNode}, func(ids []tree.PageID) ([]*tree.Page, []error) {
			Expect(ids).To(Equal([]tree.PageID{resultNode.ID, createdNode.ID}))
			return []*tree.Page{testPage(resultNode.ID, "Ensure Result", tree.SlugFromString("ensure-result"), tree.NodeKindPage), nil}, []error{nil, nil}
		}), slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, EnsurePathInput{
			UserID:      userID,
			TargetPath:  "ensure-created-missing",
			TargetTitle: "Ensure Created Missing",
			Kind:        &kindPage,
		})
		Expect(err).NotTo(HaveOccurred())

		ensureSideEffectErr := errors.New("ensure side effect failed")
		_, err = (&EnsurePathUseCase{tree: fakeEnsureTree(resultNode, []*tree.PageNode{createdNode}, func(ids []tree.PageID) ([]*tree.Page, []error) {
			Expect(ids).To(Equal([]tree.PageID{resultNode.ID, createdNode.ID}))
			return []*tree.Page{
				testPage(resultNode.ID, "Ensure Result", tree.SlugFromString("ensure-result"), tree.NodeKindPage),
				testPage(createdNode.ID, "Ensure Created", tree.SlugFromString("created"), tree.NodeKindPage),
			}, []error{nil, nil}
		}), slug: slug, orchestrator: pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: ensureSideEffectErr}), log: log}).Execute(ctx, EnsurePathInput{
			UserID:      userID,
			TargetPath:  "ensure-effect-error",
			TargetTitle: "Ensure Effect Error",
			Kind:        &kindSection,
		})
		Expect(err).To(MatchError(ensureSideEffectErr))
	})

	ginkgo.It("covers filesystem-backed metadata and asset mutation error branches", func() {
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

		detail, status, ok := PageErrorDetailForError(tree.ErrVersionConflict)
		Expect(ok).To(BeTrue())
		Expect(status).To(Equal(http.StatusConflict))
		Expect(detail.Code).To(Equal(ErrCodePageVersionConflict))
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

func ptrPageID(id tree.PageID) *tree.PageID {
	return &id
}

func matchPageSaveEvent(fields gstruct.Fields) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchRefactorAffectedPage(fields gstruct.Fields) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchPathChangeSnapshot(fields gstruct.Fields) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

type recordingPageSaveEffect struct {
	events []pagesave.PageSaveEvent
}

func (e *recordingPageSaveEffect) Apply(event pagesave.PageSaveEvent) {
	e.events = append(e.events, event)
}

type failingPageSaveEffect struct {
	err error
}

func (e *failingPageSaveEffect) Apply(pagesave.PageSaveEvent) {}

func (e *failingPageSaveEffect) ApplyRequired(pagesave.PageSaveEvent) error {
	return e.err
}

type failingSummaryPageSaveEffect struct {
	summary string
	err     error
}

func (e *failingSummaryPageSaveEffect) Apply(pagesave.PageSaveEvent) {}

func (e *failingSummaryPageSaveEffect) ApplyRequired(event pagesave.PageSaveEvent) error {
	if event.Summary == e.summary {
		return e.err
	}
	return nil
}

func writePageMarkdown(deps *routesSpecDeps, page *tree.Page, raw string) {
	ginkgo.GinkgoHelper()

	path := pageMarkdownPath(deps, page)
	Expect(os.WriteFile(path, []byte(raw), 0o644)).To(Succeed())
}

func removePageMarkdown(deps *routesSpecDeps, page *tree.Page) {
	ginkgo.GinkgoHelper()

	Expect(os.Remove(pageMarkdownPath(deps, page))).To(Succeed())
}

func pageMarkdownPath(deps *routesSpecDeps, page *tree.Page) string {
	ginkgo.GinkgoHelper()

	rel, err := deps.tree.ContentPathForNode(page.PageNode)
	Expect(err).NotTo(HaveOccurred())
	return filepath.Join(deps.tree.RootDir(), filepath.FromSlash(rel))
}

func testFixturePage(id, title, slug string, kind tree.NodeKind) *tree.Page {
	return testPage(tree.PageIDFromString(id), title, tree.SlugFromString(slug), kind)
}

func testPage(id tree.PageID, title string, slug tree.Slug, kind tree.NodeKind) *tree.Page {
	node := &tree.PageNode{
		ID:    id,
		Title: title,
		Slug:  slug,
		Kind:  kind,
	}
	return &tree.Page{PageNode: node, Content: title + " body"}
}

func testFixtureChildPage(parent *tree.Page, id, title, slug string, kind tree.NodeKind) *tree.Page {
	page := testFixturePage(id, title, slug, kind)
	page.Parent = parent.PageNode
	return page
}

func fakeEnsureTree(resultPage *tree.PageNode, created []*tree.PageNode, getPages func([]tree.PageID) ([]*tree.Page, []error)) *pageUseCaseFakeTree {
	return &pageUseCaseFakeTree{
		lookupPagePathFunc: func(routePath tree.RoutePath) (*tree.PathLookup, error) {
			return &tree.PathLookup{Path: routePath, CanCreate: true}, nil
		},
		ensurePagePathFunc: func(tree.UserID, tree.RoutePath, string, *tree.NodeKind) (*tree.EnsurePathResult, error) {
			return &tree.EnsurePathResult{Page: resultPage, Created: created}, nil
		},
		getPagesFunc: getPages,
	}
}

func fakeTreeWithPages(pages ...*tree.Page) *pageUseCaseFakeTree {
	byID := make(map[tree.PageID]*tree.Page, len(pages))
	for _, page := range pages {
		byID[page.ID] = page
	}
	return &pageUseCaseFakeTree{
		getPageFunc: func(id tree.PageID) (*tree.Page, error) {
			page := byID[id]
			if page == nil {
				return nil, tree.ErrPageNotFound
			}
			return page, nil
		},
		getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
			out := make([]*tree.Page, len(ids))
			errs := make([]error, len(ids))
			for i, id := range ids {
				out[i] = byID[id]
				if out[i] == nil {
					errs[i] = tree.ErrPageNotFound
				}
			}
			return out, errs
		},
	}
}

type pageUseCaseFakeTree struct {
	findPageByIDFunc               func(tree.PageID) (*tree.PageNode, error)
	createNodeFunc                 func(tree.UserID, *tree.PageID, string, tree.Slug, *tree.NodeKind) (*tree.PageID, error)
	getPageFunc                    func(tree.PageID) (*tree.Page, error)
	updateNodeFunc                 func(tree.UserID, tree.PageID, string, tree.Slug, *string, tree.PageVersion, bool) error
	getPagesFunc                   func([]tree.PageID) ([]*tree.Page, []error)
	deleteNodeFunc                 func(tree.UserID, tree.PageID, bool, tree.PageVersion) error
	moveNodeFunc                   func(tree.UserID, tree.PageID, tree.PageID, tree.PageVersion) error
	convertNodeFunc                func(tree.UserID, tree.PageID, tree.NodeKind, tree.PageVersion) error
	deleteNodeUncheckedVersionFunc func(tree.UserID, tree.PageID, bool) error
	updateNodeUncheckedVersionFunc func(tree.UserID, tree.PageID, string, tree.Slug, *string, bool) error
	lookupPagePathFunc             func(tree.RoutePath) (*tree.PathLookup, error)
	ensurePagePathFunc             func(tree.UserID, tree.RoutePath, string, *tree.NodeKind) (*tree.EnsurePathResult, error)
	bulkUpdateContentFunc          func(tree.UserID, []tree.BulkContentUpdate) []error
}

func (f *pageUseCaseFakeTree) FindPageByID(id tree.PageID) (*tree.PageNode, error) {
	if f.findPageByIDFunc != nil {
		return f.findPageByIDFunc(id)
	}
	return nil, errors.New("unexpected FindPageByID")
}

func (f *pageUseCaseFakeTree) CreateNode(userID tree.UserID, parentID *tree.PageID, title string, slug tree.Slug, kind *tree.NodeKind) (*tree.PageID, error) {
	if f.createNodeFunc != nil {
		return f.createNodeFunc(userID, parentID, title, slug, kind)
	}
	return nil, errors.New("unexpected CreateNode")
}

func (f *pageUseCaseFakeTree) GetPage(id tree.PageID) (*tree.Page, error) {
	if f.getPageFunc != nil {
		return f.getPageFunc(id)
	}
	return nil, errors.New("unexpected GetPage")
}

func (f *pageUseCaseFakeTree) UpdateNode(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, version tree.PageVersion, fromImport bool) error {
	if f.updateNodeFunc != nil {
		return f.updateNodeFunc(userID, id, title, slug, content, version, fromImport)
	}
	return errors.New("unexpected UpdateNode")
}

func (f *pageUseCaseFakeTree) GetPages(ids []tree.PageID) ([]*tree.Page, []error) {
	if f.getPagesFunc != nil {
		return f.getPagesFunc(ids)
	}
	pages := make([]*tree.Page, len(ids))
	errs := make([]error, len(ids))
	for i, id := range ids {
		pages[i] = testPage(id, "Generated Page", tree.SlugFromString("generated-page"), tree.NodeKindPage)
	}
	return pages, errs
}

func (f *pageUseCaseFakeTree) DeleteNode(userID tree.UserID, id tree.PageID, recursive bool, version tree.PageVersion) error {
	if f.deleteNodeFunc != nil {
		return f.deleteNodeFunc(userID, id, recursive, version)
	}
	return errors.New("unexpected DeleteNode")
}

func (f *pageUseCaseFakeTree) MoveNode(userID tree.UserID, id tree.PageID, parentID tree.PageID, version tree.PageVersion) error {
	if f.moveNodeFunc != nil {
		return f.moveNodeFunc(userID, id, parentID, version)
	}
	return errors.New("unexpected MoveNode")
}

func (f *pageUseCaseFakeTree) ConvertNode(userID tree.UserID, id tree.PageID, kind tree.NodeKind, version tree.PageVersion) error {
	if f.convertNodeFunc != nil {
		return f.convertNodeFunc(userID, id, kind, version)
	}
	return errors.New("unexpected ConvertNode")
}

func (f *pageUseCaseFakeTree) DeleteNodeUncheckedVersion(userID tree.UserID, id tree.PageID, recursive bool) error {
	if f.deleteNodeUncheckedVersionFunc != nil {
		return f.deleteNodeUncheckedVersionFunc(userID, id, recursive)
	}
	return errors.New("unexpected DeleteNodeUncheckedVersion")
}

func (f *pageUseCaseFakeTree) UpdateNodeUncheckedVersion(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, fromImport bool) error {
	if f.updateNodeUncheckedVersionFunc != nil {
		return f.updateNodeUncheckedVersionFunc(userID, id, title, slug, content, fromImport)
	}
	return errors.New("unexpected UpdateNodeUncheckedVersion")
}

func (f *pageUseCaseFakeTree) LookupPagePath(routePath tree.RoutePath) (*tree.PathLookup, error) {
	if f.lookupPagePathFunc != nil {
		return f.lookupPagePathFunc(routePath)
	}
	return nil, errors.New("unexpected LookupPagePath")
}

func (f *pageUseCaseFakeTree) EnsurePagePath(userID tree.UserID, routePath tree.RoutePath, title string, kind *tree.NodeKind) (*tree.EnsurePathResult, error) {
	if f.ensurePagePathFunc != nil {
		return f.ensurePagePathFunc(userID, routePath, title, kind)
	}
	return nil, errors.New("unexpected EnsurePagePath")
}

func (f *pageUseCaseFakeTree) BulkUpdateContent(userID tree.UserID, updates []tree.BulkContentUpdate) []error {
	if f.bulkUpdateContentFunc != nil {
		return f.bulkUpdateContentFunc(userID, updates)
	}
	return make([]error, len(updates))
}

type fakeRefactorLinks struct {
	matches []links.RefactorLinkMatch
	err     error
}

func (f *fakeRefactorLinks) GetRefactorMatchesForPrefixAndKind(tree.RoutePath, tree.NodeKind) ([]links.RefactorLinkMatch, error) {
	return f.matches, f.err
}

type fakePageAssets struct {
	copyErr     error
	deleteErr   error
	copyCalls   int
	deleteCalls int
}

type pageAssetCalls struct {
	Copy   int
	Delete int
}

func pageAssetCallCounts(assets *fakePageAssets) pageAssetCalls {
	return pageAssetCalls{Copy: assets.copyCalls, Delete: assets.deleteCalls}
}

func (a *fakePageAssets) CopyAllAssets(*tree.PageNode, *tree.PageNode) error {
	a.copyCalls++
	return a.copyErr
}

func (a *fakePageAssets) DeleteAllAssetsForPage(*tree.PageNode) error {
	a.deleteCalls++
	return a.deleteErr
}

func ginParams(key string, value string) gin.Params {
	ginkgo.GinkgoHelper()
	return gin.Params{{Key: key, Value: value}}
}

func readmeLookupPathForRoute(routePath tree.RoutePath) string {
	return filepath.ToSlash(filepath.Join(routePath.FilesystemPath(), "README.md"))
}

func ginPageIDParams(id tree.PageID) gin.Params {
	ginkgo.GinkgoHelper()
	return gin.Params{{Key: "id", Value: id.String()}}
}
