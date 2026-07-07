package pages

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/gin-gonic/gin"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/links"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

var _ = ginkgo.Describe("page refactor route planning", func() {
	ginkgo.It("returns route errors for unauthorized and invalid requests", ginkgo.Label("integration"), func() {
		deps := newRoutesSpecDeps()
		page := deps.createPage("Page", newFixtureSlug("page"), tree.NodeKindPage, nil)

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

	ginkgo.It("validates refactor planning outcomes for invalid requests", ginkgo.Label("integration"), func() {
		deps := newRoutesSpecDeps()
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		slug := tree.NewSlugService()
		preview := NewPreviewPageRefactorUseCase(deps.tree, slug, nil, log)
		apply := NewApplyPageRefactorUseCaseWithOrchestrator(deps.tree, slug, nil, nil, log)
		ctx := context.Background()

		section := deps.createPage("Docs", newFixtureSlug("docs"), tree.NodeKindSection, nil)
		page := deps.createPage("Page", newFixtureSlug("page"), tree.NodeKindPage, nil)
		child := deps.createPage("Child", newFixtureSlug("child"), tree.NodeKindPage, &section.ID)

		_, err := preview.Execute(ctx, RefactorPreviewInput{Kind: "copy"})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidRefactorKind))
		badParentID := newFixturePageID(" parent ")
		_, err = preview.Execute(ctx, RefactorPreviewInput{Kind: RefactorKindMove, NewParentID: &badParentID})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidParentID))
		_, err = preview.Execute(ctx, RefactorPreviewInput{Kind: RefactorKindRename, PageID: newFixturePageID("missing"), Title: "New", Slug: newFixtureSlug("new")})
		Expect(err).To(MatchError(tree.ErrPageNotFound))

		_, err = preview.computeTargetPath(page, RefactorPreviewInput{Kind: RefactorKindRename, Title: "", Slug: newFixtureSlug("bad slug")})
		Expect(err).To(HavePageValidationFields("title", "slug"))

		routePath, err := preview.computeTargetPath(page, RefactorPreviewInput{Kind: RefactorKindRename, Title: "Renamed", Slug: newFixtureSlug("renamed")})
		Expect(err).NotTo(HaveOccurred())
		Expect(routePath).To(Equal(newFixtureRoutePath("renamed")))
		routePath, err = preview.computeTargetPath(child, RefactorPreviewInput{Kind: RefactorKindRename, Title: "Child Two", Slug: newFixtureSlug("child-two")})
		Expect(err).NotTo(HaveOccurred())
		Expect(routePath).To(Equal(newFixtureRoutePath("docs/child-two")))
		_, err = preview.computeTargetPath(page, RefactorPreviewInput{Kind: RefactorKindMove, NewParentID: ptrPageID(newFixturePageID("missing"))})
		Expect(err).To(MatchError(tree.ErrPageNotFound))
		_, err = preview.computeTargetPath(page, RefactorPreviewInput{Kind: "copy"})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidRefactorKind))

		routePath, err = preview.resolveParentRoutePath(newFixturePageID(""))
		Expect(err).NotTo(HaveOccurred())
		Expect(routePath).To(BeEmpty())
		routePath, err = preview.resolveParentRoutePath(tree.RootPageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(routePath).To(BeEmpty())
		_, err = preview.resolveParentRoutePath(newFixturePageID("missing"))
		Expect(err).To(MatchError(tree.ErrPageNotFound))

		affected, matched, err := preview.getAffectedPages(page.CalculateRoutePath(), page.Kind, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(affected).To(BeEmpty())
		Expect(matched).To(BeZero())

		_, err = apply.Execute(ctx, RefactorApplyInput{RefactorPreviewInput: RefactorPreviewInput{Kind: "copy"}})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidRefactorKind))
		_, err = apply.Execute(ctx, RefactorApplyInput{RefactorPreviewInput: RefactorPreviewInput{Kind: RefactorKindMove, NewParentID: &badParentID}})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidParentID))
		_, err = apply.Execute(ctx, RefactorApplyInput{RefactorPreviewInput: RefactorPreviewInput{Kind: RefactorKindRename, PageID: newFixturePageID("missing"), Title: "New", Slug: newFixtureSlug("new")}})
		Expect(err).To(MatchError(tree.ErrPageNotFound))
		_, err = apply.Execute(ctx, RefactorApplyInput{
			Version: tree.PageVersionFromString("stale"),
			RefactorPreviewInput: RefactorPreviewInput{
				Kind:   RefactorKindRename,
				PageID: page.ID,
				Title:  "New",
				Slug:   newFixtureSlug("new"),
			},
		})
		Expect(err).To(MatchError(tree.ErrVersionConflict))

		Expect(validateRefactorVersion(&tree.Page{}, newFixturePageVersion(""))).To(Succeed())
		Expect(validateRefactorVersion(page, newFixturePageVersion(""))).To(MatchError(tree.ErrVersionRequired))
		Expect(validateRefactorVersion(page, newFixtureRawPageVersion("\x00"))).To(Succeed())

		newContent := "updated content"
		snapshots, err := apply.captureSnapshots(page, RefactorApplyInput{RefactorPreviewInput: RefactorPreviewInput{PageID: page.ID, Content: &newContent}})
		Expect(err).NotTo(HaveOccurred())
		Expect(snapshots).To(ContainElement(HaveField("Content", newContent)))
		_, err = apply.captureSnapshots(&tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("missing"), Kind: tree.NodeKindPage}}, RefactorApplyInput{RefactorPreviewInput: RefactorPreviewInput{PageID: newFixturePageID("missing")}})
		Expect(err).To(MatchError(tree.ErrPageNotFound))

		Expect(apply.rewriteIncomingLinks(RefactorApplyInput{}, &applyRefactorPlan{})).To(Succeed())
		Expect(apply.rewriteAffectedPages(newFixtureUserID("user"), "test", nil, nil, nil)).To(Succeed())
		Expect(apply.rewritePathChangedSubtree(newFixtureUserID("user"), "test", nil, newFixtureRoutePath("old"), newFixtureRoutePath("new"))).To(Succeed())
		Expect(apply.runBulkContentUpdateSideEffects(newFixtureUserID("user"), "test", nil)).To(Succeed())
		Expect(planNodeKind([]pathChangeSnapshot{{Kind: tree.NodeKindSection}})).To(Equal(tree.NodeKindPage))
		Expect(ensureStrings(nil)).To(BeEmpty())
		Expect(ensureRefactorWarnings(nil)).To(BeEmpty())
		warnings := collectPreviewWarnings([]RefactorAffectedPage{
			{WarningDetails: []RefactorWarning{{MessageID: newFixtureMessageID("b"), Message: "two"}, {MessageID: newFixtureMessageID("a"), Message: "one"}, {MessageID: newFixtureMessageID("a"), Message: "one"}}},
		})
		Expect(warnings).To(Equal([]RefactorWarning{{MessageID: newFixtureMessageID("a"), Message: "one"}, {MessageID: newFixtureMessageID("b"), Message: "two"}}))
	})

	ginkgo.It("builds refactor plans around link and rewrite failures", ginkgo.Label("unit"), func() {
		ctx := context.Background()
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		slug := tree.NewSlugService()
		userID := newFixtureUserID("refactor-user")

		rootless := testFixturePage(newFixturePageID("rootless"), "Rootless", newFixtureSlug("rootless"), tree.NodeKindPage)
		rootlessTree := fakeTreeWithPages(rootless)
		preview := &PreviewPageRefactorUseCase{tree: rootlessTree, slug: slug, log: log}
		routePath, err := preview.computeTargetPath(rootless, RefactorPreviewInput{Kind: RefactorKindRename, Title: "Renamed", Slug: newFixtureSlug("renamed")})
		Expect(err).NotTo(HaveOccurred())
		Expect(routePath).To(Equal(newFixtureRoutePath("renamed")))

		_, err = preview.Execute(ctx, RefactorPreviewInput{Kind: RefactorKindRename, PageID: rootless.ID, Title: "", Slug: newFixtureSlug("bad slug")})
		Expect(err).To(HavePageValidationFields("title", "slug"))

		linkErr := errors.New("link match query failed")
		previewWithLinkErr := &PreviewPageRefactorUseCase{
			tree:          rootlessTree,
			slug:          slug,
			refactorLinks: &fakeRefactorLinks{err: linkErr},
			log:           log,
		}
		_, err = previewWithLinkErr.Execute(ctx, RefactorPreviewInput{Kind: RefactorKindRename, PageID: rootless.ID, Title: "Renamed", Slug: newFixtureSlug("renamed")})
		Expect(err).To(MatchError(linkErr))

		excluded := testFixturePage(newFixturePageID("excluded"), "Excluded", newFixtureSlug("excluded"), tree.NodeKindPage)
		first := testFixturePage(newFixturePageID("first"), "Same", newFixtureSlug("b"), tree.NodeKindPage)
		first.Content = "[bad][ref]\n\n[ref]: /old"
		second := testFixturePage(newFixturePageID("second"), "Same", newFixtureSlug("a"), tree.NodeKindPage)
		second.Content = "[bad][ref]\n\n[ref]: /old"
		third := testFixturePage(newFixturePageID("third"), "Alpha", newFixtureSlug("c"), tree.NodeKindPage)
		third.Content = "[bad][ref]\n\n[ref]: /old"
		matches := []links.RefactorLinkMatch{
			{FromPageID: excluded.ID, FromTitle: excluded.Title, ToPath: newFixtureRoutePath("old"), ToKind: links.TargetKindPage},
			{FromPageID: first.ID, FromTitle: "Same", ToPath: newFixtureRoutePath("old"), ToKind: links.TargetKindPage},
			{FromPageID: first.ID, FromTitle: "Same", ToPath: newFixtureRoutePath("old"), ToKind: links.TargetKindPage},
			{FromPageID: second.ID, FromTitle: "Same", ToPath: newFixtureRoutePath("old/child"), ToKind: links.TargetKindSection},
			{FromPageID: third.ID, FromTitle: "Alpha", ToPath: newFixtureRoutePath("old/other"), ToKind: links.TargetKindPage},
		}
		previewWithMatches := &PreviewPageRefactorUseCase{
			tree:          fakeTreeWithPages(excluded, first, second, third),
			slug:          slug,
			refactorLinks: &fakeRefactorLinks{matches: matches},
			log:           log,
		}
		affected, matched, err := previewWithMatches.getAffectedPages(newFixtureRoutePath("old"), tree.NodeKindPage, map[tree.PageID]struct{}{excluded.ID: {}})
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
			refactorLinks: &fakeRefactorLinks{matches: []links.RefactorLinkMatch{{FromPageID: first.ID, FromTitle: first.Title, ToPath: newFixtureRoutePath("old")}}},
			log:           log,
		}
		_, _, err = previewWithSourceErr.getAffectedPages(newFixtureRoutePath("old"), tree.NodeKindPage, nil)
		Expect(err).To(MatchError(sourceReadErr))

		planPage := testFixturePage(newFixturePageID("plan"), "Plan", newFixtureSlug("old"), tree.NodeKindPage)
		planChild := testFixtureChildPage(planPage, newFixturePageID("plan-child"), "Plan Child", newFixtureSlug("child"), tree.NodeKindPage)
		planPage.Children = []*tree.PageNode{planChild.PageNode}
		planAffected := testFixturePage(newFixturePageID("plan-affected"), "Plan Affected", newFixtureSlug("affected"), tree.NodeKindPage)
		applyTree := fakeTreeWithPages(planPage, planChild, planAffected)
		applyPreview := &PreviewPageRefactorUseCase{tree: applyTree, slug: slug, log: log}
		apply := &ApplyPageRefactorUseCase{tree: applyTree, slug: slug, preview: applyPreview, log: log, orchestrator: pagesave.NewPageSaveOrchestrator()}

		_, err = apply.buildApplyPlan(RefactorApplyInput{RefactorPreviewInput: RefactorPreviewInput{Kind: RefactorKindRename, PageID: planPage.ID, Title: "", Slug: newFixtureSlug("bad slug")}})
		Expect(err).To(HavePageValidationFields("title", "slug"))

		apply.refactorLinks = &fakeRefactorLinks{err: linkErr}
		_, err = apply.buildApplyPlan(RefactorApplyInput{RewriteLinks: true, RefactorPreviewInput: RefactorPreviewInput{Kind: RefactorKindRename, PageID: planPage.ID, Title: "New", Slug: newFixtureSlug("new")}})
		Expect(err).To(MatchError(linkErr))

		apply.refactorLinks = &fakeRefactorLinks{matches: []links.RefactorLinkMatch{
			{FromPageID: planChild.ID, FromTitle: planChild.Title, ToPath: newFixtureRoutePath("old"), ToKind: links.TargetKindUnknown},
			{FromPageID: planAffected.ID, FromTitle: planAffected.Title, ToPath: newFixtureRoutePath("old"), ToKind: links.TargetKindUnknown},
		}}
		plan, err := apply.buildApplyPlan(RefactorApplyInput{RewriteLinks: true, RefactorPreviewInput: RefactorPreviewInput{Kind: RefactorKindRename, PageID: planPage.ID, Title: "New", Slug: newFixtureSlug("new")}})
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.affectedPageIDs).To(Equal([]tree.PageID{planAffected.ID}))
		Expect(plan.legacyPageLinkSourceIDs).To(HaveKey(planAffected.ID))

		fallbackSnapshotID := newFixturePageID("fallback-snapshot")
		captureTree := &pageUseCaseFakeTree{getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
			Expect(ids).To(Equal([]tree.PageID{fallbackSnapshotID}))
			return []*tree.Page{testPage(fallbackSnapshotID, "Fallback", newFixtureSlug("fallback"), tree.NodeKindPage)}, []error{nil}
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
		Expect(nilPageApply.loadPagesByID([]tree.PageID{newFixturePageID("nil-page")}, "nil page")).To(BeEmpty())
		Expect(nilPageApply.loadPagesInOrder(nil, "empty")).To(BeNil())
		Expect(nilPageApply.loadPagesInOrder([]tree.PageID{newFixturePageID("nil-page")}, "nil page")).To(BeEmpty())

		missingRewriteTree := &pageUseCaseFakeTree{getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
			return []*tree.Page{nil}, []error{tree.ErrPageNotFound}
		}}
		missingRewriteApply := &ApplyPageRefactorUseCase{tree: missingRewriteTree, log: log, orchestrator: pagesave.NewPageSaveOrchestrator()}
		Expect(missingRewriteApply.rewriteAffectedPages(userID, "test", []tree.PageID{newFixturePageID("missing")}, []links.RewriteRule{{OldPath: newFixtureRoutePath("old"), NewPath: newFixtureRoutePath("new"), Kind: links.TargetKindPage}}, nil)).To(Succeed())

		rewritePage := testFixturePage(newFixturePageID("rewrite-page"), "Rewrite Page", newFixtureSlug("rewrite-page"), tree.NodeKindPage)
		rewritePage.Content = "[Old](/old.md)"
		rewrittenPage := testFixturePage(newFixturePageID("rewrite-page"), "Rewrite Page", newFixtureSlug("rewrite-page"), tree.NodeKindPage)
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
		Expect(rewriteApply.rewriteAffectedPages(userID, "test", []tree.PageID{rewritePage.ID}, []links.RewriteRule{{OldPath: newFixtureRoutePath("old"), NewPath: newFixtureRoutePath("new"), Kind: links.TargetKindPage}}, map[tree.PageID]struct{}{rewritePage.ID: {}})).To(Succeed())
		Expect(rewriteUpdates).To(Equal(1))

		noRewriteTree := &pageUseCaseFakeTree{getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
			return []*tree.Page{rewrittenPage}, []error{nil}
		}}
		noRewriteApply := &ApplyPageRefactorUseCase{tree: noRewriteTree, log: log, orchestrator: pagesave.NewPageSaveOrchestrator()}
		Expect(noRewriteApply.rewriteAffectedPages(userID, "test", []tree.PageID{rewrittenPage.ID}, []links.RewriteRule{{OldPath: newFixtureRoutePath("old"), NewPath: newFixtureRoutePath("new"), Kind: links.TargetKindPage}}, nil)).To(Succeed())

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
		Expect(bulkErrApply.rewriteAffectedPages(userID, "test", []tree.PageID{rewritePage.ID}, []links.RewriteRule{{OldPath: newFixtureRoutePath("old"), NewPath: newFixtureRoutePath("new"), Kind: links.TargetKindPage}}, nil)).To(Succeed())

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
		Expect(sideEffectApply.rewriteAffectedPages(userID, "test", []tree.PageID{rewritePage.ID}, []links.RewriteRule{{OldPath: newFixtureRoutePath("old"), NewPath: newFixtureRoutePath("new"), Kind: links.TargetKindPage}}, nil)).To(MatchError(sideEffectErr))

		snapshot := pathChangeSnapshot{PageID: rewritePage.ID, OldPath: newFixtureRoutePath("old/current"), Content: "snapshot content", Kind: tree.NodeKindPage, RootPage: true}
		missingSubtreeApply := &ApplyPageRefactorUseCase{tree: missingRewriteTree, log: log, orchestrator: pagesave.NewPageSaveOrchestrator()}
		Expect(missingSubtreeApply.rewritePathChangedSubtree(userID, "test", []pathChangeSnapshot{snapshot}, newFixtureRoutePath("old"), newFixtureRoutePath("new"))).To(Succeed())

		subtreeBulkErrApply := &ApplyPageRefactorUseCase{tree: bulkErrTree, log: log, orchestrator: pagesave.NewPageSaveOrchestrator()}
		Expect(subtreeBulkErrApply.rewritePathChangedSubtree(userID, "test", []pathChangeSnapshot{snapshot}, newFixtureRoutePath("old"), newFixtureRoutePath("new"))).To(Succeed())

		subtreeSideEffectApply := &ApplyPageRefactorUseCase{
			tree:         sideEffectTree,
			log:          log,
			orchestrator: pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: sideEffectErr}),
		}
		Expect(subtreeSideEffectApply.rewritePathChangedSubtree(userID, "test", []pathChangeSnapshot{snapshot}, newFixtureRoutePath("old"), newFixtureRoutePath("new"))).To(MatchError(sideEffectErr))
	})

})
