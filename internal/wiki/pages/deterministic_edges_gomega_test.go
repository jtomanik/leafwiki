package pages

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/core/assets"
	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/http/dto"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

const pageMetadataWhitespacePropertyKeyFixture = " key "

var _ = ginkgo.Describe("page metadata and route helper contracts", func() {
	ginkgo.It("rejects invalid markdown section targets and metadata patch fields", ginkgo.Label("unit"), func() {
		_, err := ReplaceMarkdownSection("# Page\n", []string{" "}, 0, "")
		Expect(err).To(MatchError(ErrSectionHeadingPathRequired))

		content := "# Page\n\n## Repeat\none\n\n## Repeat\ntwo\n"
		_, err = ReplaceMarkdownSection(content, []string{"Repeat"}, 3, "replacement")
		Expect(err).To(MatchError(ErrSectionHeadingNotFound))

		replaced, err := ReplaceMarkdownSection(content, []string{"Repeat"}, 1, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(replaced).NotTo(ContainSubstring("one"))
		Expect(replaced).To(ContainSubstring("two"))

		Expect("~~").To(BeRejectedMarkdownFence())

		Expect("####### too deep").To(BeRejectedMarkdownHeading())
		Expect("#    ").To(BeRejectedMarkdownHeading())

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

	ginkgo.It("normalizes metadata and resolves README fallback pages", ginkgo.Label("unit"), func() {
		page := &dto.Page{Node: &dto.Node{ID: "page-1"}}
		EnrichPageMetadata(page, func(tree.PageID) (string, error) {
			return "<!-- leafwiki malformed\n-->\nbody", nil
		})
		Expect(page).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Tags":       BeEmpty(),
			"Properties": BeEmpty(),
		})))

		_, err := BuildMarkdownWithPublicMetadataPatch("<!-- leafwiki malformed\n-->\nbody", newFixturePageID("page-1"), "Page", PublicMetadataPatch{}, "body")
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

		rootDir := pagesTempDir()
		Expect(os.WriteFile(filepath.Join(rootDir, "README.md"), []byte("# Root"), 0o644)).To(Succeed())
		rootPage := &tree.Page{PageNode: &tree.PageNode{ID: tree.RootPageID, Title: "Root", Slug: newFixtureSlug("root"), Kind: tree.NodeKindSection}}
		out, err := requireReadmeMarkdownPathFallback("README.md", tree.NodeKindSection, ReadmeMarkdownPathFallbackLookup{
			RootDir: rootDir,
			RootPage: func() (*tree.Page, error) {
				return rootPage, nil
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.Page).To(BeIdenticalTo(rootPage))

		Expect(os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "README.md"), []byte("# Docs"), 0o644)).To(Succeed())
		sectionPage := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("docs"), Title: "Docs", Slug: newFixtureSlug("docs"), Kind: tree.NodeKindSection}}
		out, err = requireReadmeMarkdownPathFallback("docs/README.md", tree.NodeKindSection, ReadmeMarkdownPathFallbackLookup{
			RootDir: rootDir,
			FindByPath: func(in FindByPathInput) (*FindByPathOutput, error) {
				Expect(in).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"RoutePath": Equal(newFixtureRoutePath("docs")),
					"Kind":      Equal(tree.NodeKindSection),
				}))
				return &FindByPathOutput{Page: sectionPage}, nil
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.Page).To(BeIdenticalTo(sectionPage))
	})

	ginkgo.It("validates page lookup permalink slug and error contracts", ginkgo.Label("integration"), func() {
		deps := newRoutesSpecDeps()
		docs := deps.createPage("Docs", newFixtureSlug("docs"), tree.NodeKindSection, nil)
		guide := deps.createPage("Guide", newFixtureSlug("guide"), tree.NodeKindPage, &docs.ID)
		Expect(guide).NotTo(BeNil())

		findByPath := NewFindByPathUseCase(deps.tree)
		_, err := findByPath.Execute(context.Background(), FindByPathInput{})
		Expect(err).To(HavePageValidationField("path"))

		lookup, err := NewLookupPagePathUseCase(deps.tree).Execute(context.Background(), LookupPagePathInput{Path: newFixtureRoutePath("docs/guide")})
		Expect(err).NotTo(HaveOccurred())
		Expect(lookup.Lookup).To(gstruct.PointTo(HaveExistingRoutePathLookup(newFixtureRoutePath("docs/guide"))))

		_, err = NewLookupPagePathUseCase(deps.tree).Execute(context.Background(), LookupPagePathInput{})
		Expect(err).To(HavePageValidationField("path"))

		_, err = NewResolvePermalinkUseCase(deps.tree).Execute(context.Background(), ResolvePermalinkInput{ID: newFixturePageID("missing")})
		Expect(err).To(MatchError(tree.ErrPageNotFound))

		_, err = NewSuggestSlugUseCase(deps.tree, tree.NewSlugService()).Execute(context.Background(), SuggestSlugInput{ParentID: newFixturePageID("missing"), Title: "Child"})
		Expect(err).To(MatchError(tree.ErrPageNotFound))

		validated, err := ValidateRefactorKind(RefactorKindRename)
		Expect(err).NotTo(HaveOccurred())
		Expect(validated).To(Equal(RefactorKindRename))
		_, err = ValidateRefactorKind("copy")
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidRefactorKind))

		parent, err := ValidateSemanticMoveParentID(tree.RootPageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(parent).To(Equal(tree.RootPageID))
		_, err = ValidateSemanticMoveParentID(newFixturePageID(" parent "))
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidParentID))

		optionalParent, err := ValidateOptionalParentID(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(optionalParent).To(BeNil())

		optionalSemanticParent, err := ValidateOptionalSemanticParentID(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(optionalSemanticParent).To(BeNil())

		_, err = ValidateSemanticRoutePath("../escape")
		Expect(err).To(HavePageValidationField("path"))

		Expect(pageErrorStatus(newFixtureErrorCode("unknown"))).To(Equal(http.StatusInternalServerError))
	})

	ginkgo.It("normalizes route input for root README metadata and versions", ginkgo.Label("integration"), func() {
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

		Expect("docs/README.md").To(HaveReadmeMarkdownFallbackRoutes("docs/README", "docs"))

		fallback, err := requireReadmeMarkdownPathFallbackInput("docs/README.md", tree.NodeKindPage)
		Expect(err).NotTo(HaveOccurred())
		Expect(fallback).To(HavePageOnlyReadmeMarkdownFallback("docs/README", "docs"))

		_, err = requireReadmeMarkdownPathFallback("docs/README.md", tree.NodeKindSection, ReadmeMarkdownPathFallbackLookup{
			RootDir: "",
		})
		Expect(err).To(MatchError(tree.ErrPageNotFound))

		Expect(ReadmeFallbackSectionIsActive("", "")).To(BeFalse())
		rootDir := pagesTempDir()
		Expect(os.WriteFile(filepath.Join(rootDir, "README.md"), []byte("# Root"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "index.md"), []byte("# Index"), 0o644)).To(Succeed())
		Expect(ReadmeFallbackSectionIsActive(rootDir, "")).To(BeFalse())

		Expect(sanitizeSemanticClientVersion(newFixtureRawPageVersion("\x00"))).To(BeEmpty())
		Expect(sanitizeSemanticClientVersion(tree.PageVersionFromString("v1"))).To(Equal(newFixturePageVersion("v1")))

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

	ginkgo.It("deduplicates and sorts refactor warnings deterministically", ginkgo.Label("unit"), func() {
		first := RefactorWarning{MessageID: newFixtureMessageID("warnings.refactor.a"), Message: "beta"}
		second := RefactorWarning{MessageID: newFixtureMessageID("warnings.refactor.a"), Message: "alpha"}
		third := RefactorWarning{MessageID: newFixtureMessageID("warnings.refactor.b"), Message: "alpha"}

		warnings := collectPreviewWarnings([]RefactorAffectedPage{
			{WarningDetails: []RefactorWarning{third, first}},
			{WarningDetails: []RefactorWarning{first, second}},
		})

		Expect(warnings).To(Equal([]RefactorWarning{second, first, third}))
		Expect(warnings).To(ContainElement(first))
		Expect(warnings).NotTo(ContainElement(RefactorWarning{MessageID: first.MessageID, Message: "missing"}))
		Expect([]string{"old", "new"}).To(ContainElement("old"))
		Expect([]string{"old", "new"}).NotTo(ContainElement("missing"))
		Expect(ensureStrings(nil)).To(BeEmpty())
		Expect(ensureStrings([]string{"x"})).To(Equal([]string{"x"}))
		Expect(ensureRefactorWarnings(nil)).To(BeEmpty())
		Expect(ensureRefactorWarnings([]RefactorWarning{first})).To(Equal([]RefactorWarning{first}))
	})

	ginkgo.It("rejects invalid direct page use-case requests", ginkgo.Label("integration"), func() {
		deps := newRoutesSpecDeps()
		userID := newFixtureUserID("pages-direct-user")

		_, err := deps.routes.copyPage.Execute(context.Background(), CopyPageInput{
			UserID:       userID,
			SourcePageID: newFixturePageID("missing"),
			Title:        "",
			Slug:         newFixtureSlug("bad slug"),
		})
		Expect(err).To(HavePageValidationFields("title", "slug"))

		badParent := newFixturePageID(" parent ")
		_, err = deps.routes.copyPage.Execute(context.Background(), CopyPageInput{
			UserID:         userID,
			SourcePageID:   newFixturePageID("missing"),
			TargetParentID: &badParent,
			Title:          "Copy",
			Slug:           newFixtureSlug("copy"),
		})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidParentID))

		err = deps.routes.deletePage.Execute(context.Background(), DeletePageInput{UserID: userID, ID: tree.RootPageID})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageRootOperation))

		err = deps.routes.convertPage.Execute(context.Background(), ConvertPageInput{UserID: userID, ID: newFixturePageID("")})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageRootOperation))

		err = deps.routes.movePage.Execute(context.Background(), MovePageInput{UserID: userID, ID: tree.RootPageID})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageRootOperation))

		_, err = deps.routes.ensurePath.Execute(context.Background(), EnsurePathInput{UserID: userID, TargetPath: newFixtureRoutePath(""), TargetTitle: " "})
		Expect(err).To(HavePageValidationFields("path", "title"))
	})

	ginkgo.It("records direct mutation events for create, update, and delete use cases", ginkgo.Label("integration"), func() {
		deps := newRoutesSpecDeps()
		recorder := &recordingPageSaveEffect{}
		orchestrator := pagesave.NewPageSaveOrchestrator(recorder)
		slugger := tree.NewSlugService()
		log := slog.Default()
		userID := newFixtureUserID("pages-event-user")
		kind := tree.NodeKindPage

		createUC := NewCreatePageUseCase(deps.tree, slugger, orchestrator, log)
		created, err := createUC.Execute(context.Background(), CreatePageInput{
			UserID: userID,
			Source: pagesave.PageMutationSourceMCP,
			Title:  "Original",
			Slug:   newFixtureSlug("original"),
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
			Slug:    newFixtureSlug("renamed"),
			Content: &content,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(updated.Page.Title).To(Equal("Renamed"))
		updatedEvent := SatisfyAll(
			matchPageSaveEvent(gstruct.Fields{
				"Operation":     Equal(pagesave.PageOperationUpdate),
				"OldPath":       Equal(newFixtureRoutePath("original")),
				"AffectedPages": HaveLen(1),
			}),
			HavePageSaveContentChange(),
			HavePageSaveSlugChange(),
			HavePageSaveTitleChange(),
		)
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
			"OldPath":       Equal(newFixtureRoutePath("renamed")),
			"AffectedPages": HaveLen(1),
		})
		Expect(recorder.events).To(HaveExactElements(createdEvent, updatedEvent, deletedEvent))
	})

	ginkgo.It("configures refactor use cases and helper defaults", ginkgo.Label("integration"), func() {
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
		Expect(applyUC.runBulkContentUpdateSideEffects(newFixtureUserID("editor"), "test", nil)).To(Succeed())
		Expect(recorder.events).To(BeEmpty())

		page := deps.createPage("Helper", newFixtureSlug("helper"), tree.NodeKindPage, nil)
		Expect(applyUC.runBulkContentUpdateSideEffects(newFixtureUserID("editor"), "test", []*tree.Page{page})).To(Succeed())
		Expect(recorder.events).To(HaveExactElements(SatisfyAll(
			matchPageSaveEvent(gstruct.Fields{
				"Operation":     Equal(pagesave.PageOperationUpdate),
				"AffectedPages": Equal([]*tree.Page{page}),
			}),
			HavePageSaveContentChange(),
		)))

		Expect(applyUC.loadPagesByID(nil, "unused")).To(BeEmpty())
		loaded := applyUC.loadPagesByID([]tree.PageID{page.ID, newFixturePageID("missing")}, "missing")
		Expect(loaded).To(HaveKey(page.ID))
		Expect(loaded).NotTo(HaveKey(newFixturePageID("missing")))

		ordered := applyUC.loadPagesInOrder([]tree.PageID{newFixturePageID("missing"), page.ID}, "missing")
		Expect(ordered).To(Equal([]*tree.Page{page}))

		Expect(planNodeKind(nil)).To(Equal(tree.NodeKindPage))
		Expect(planNodeKind([]pathChangeSnapshot{{Kind: tree.NodeKindSection, RootPage: true}})).To(Equal(tree.NodeKindSection))
		Expect(snapshotPage(nil)).To(BeNil())
		noNode := &tree.Page{Content: "body"}
		Expect(snapshotPage(noNode)).To(BeIdenticalTo(noNode))
	})

	ginkgo.It("returns structured route errors for malformed payloads", ginkgo.Label("integration"), func() {
		deps := newRoutesSpecDeps()
		page := deps.createPage("Payload", newFixtureSlug("payload"), tree.NodeKindPage, nil)

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
		Expect(*semanticPageIDPtr(&rawParent)).To(Equal(newFixturePageID(" parent ")))
		Expect(semanticPageIDs([]string{"one", "two"})).To(Equal([]tree.PageID{newFixturePageID("one"), newFixturePageID("two")}))

		_, err := ValidateSuggestSlugTitle("!!!")
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidTitle))
		Expect(ValidatePageMetadataInput([]string{"alpha", "beta"}, map[string]string{"owner": "alice"})).To(Succeed())
	})
})
