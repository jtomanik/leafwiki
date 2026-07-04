package pages

import (
	"context"
	"log/slog"
	"sort"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/links"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

type ApplyPageRefactorUseCase struct {
	tree                   refactorApplyTree
	slug                   *tree.SlugService
	links                  *links.LinkService
	refactorLinks          refactorLinkFinder
	orchestrator           *pagesave.PageSaveOrchestrator
	log                    *slog.Logger
	preview                *PreviewPageRefactorUseCase
	markdownLinkRootPrefix string
}

// NewApplyPageRefactorUseCase constructs an ApplyPageRefactorUseCase.
func NewApplyPageRefactorUseCase(
	t *tree.TreeService,
	s *tree.SlugService,
	l *links.LinkService,
	log *slog.Logger,
) *ApplyPageRefactorUseCase {
	return NewApplyPageRefactorUseCaseWithOptions(t, s, l, log, RefactorUseCaseOptions{})
}

func NewApplyPageRefactorUseCaseWithOptions(
	t *tree.TreeService,
	s *tree.SlugService,
	l *links.LinkService,
	log *slog.Logger,
	opts RefactorUseCaseOptions,
) *ApplyPageRefactorUseCase {
	var finder refactorLinkFinder
	if l != nil {
		finder = l
	}
	uc := &ApplyPageRefactorUseCase{
		tree:                   t,
		slug:                   s,
		links:                  l,
		refactorLinks:          finder,
		log:                    log,
		markdownLinkRootPrefix: opts.MarkdownLinkRootPrefix,
		preview:                NewPreviewPageRefactorUseCaseWithOptions(t, s, l, log, opts),
	}
	uc.orchestrator = uc.defaultOrchestrator()
	return uc
}

// NewApplyPageRefactorUseCaseWithOrchestrator constructs an ApplyPageRefactorUseCase
// with the same page-save side effects used by the caller's mutation surface.
func NewApplyPageRefactorUseCaseWithOrchestrator(
	t *tree.TreeService,
	s *tree.SlugService,
	l *links.LinkService,
	o *pagesave.PageSaveOrchestrator,
	log *slog.Logger,
) *ApplyPageRefactorUseCase {
	uc := NewApplyPageRefactorUseCase(t, s, l, log)
	if o != nil {
		uc.orchestrator = o
	}
	return uc
}

func NewApplyPageRefactorUseCaseWithOrchestratorAndOptions(
	t *tree.TreeService,
	s *tree.SlugService,
	l *links.LinkService,
	o *pagesave.PageSaveOrchestrator,
	log *slog.Logger,
	opts RefactorUseCaseOptions,
) *ApplyPageRefactorUseCase {
	uc := NewApplyPageRefactorUseCaseWithOptions(t, s, l, log, opts)
	if o != nil {
		uc.orchestrator = o
	}
	return uc
}

// Execute applies the refactor operation to the page tree.
func (uc *ApplyPageRefactorUseCase) Execute(ctx context.Context, in RefactorApplyInput) (*tree.Page, error) {
	kind, err := ValidateRefactorKind(in.Kind)
	if err != nil {
		return nil, err
	}
	in.Kind = kind
	parentID, err := ValidateOptionalSemanticParentID(in.NewParentID)
	if err != nil {
		return nil, err
	}
	in.NewParentID = parentID
	in.Version = sanitizeSemanticClientVersion(in.Version)

	plan, err := uc.buildApplyPlan(in)
	if err != nil {
		return nil, err
	}
	if err := validateRefactorVersion(plan.page, in.Version); err != nil {
		return nil, err
	}

	snapshots, err := uc.captureSnapshots(plan.page, in)
	if err != nil {
		return nil, err
	}

	o := uc.pageOrchestrator()

	if in.Kind == RefactorKindRename {
		updateUC := &UpdatePageUseCase{tree: uc.tree, slug: uc.slug, orchestrator: o, log: uc.log}
		updated, err := updateUC.Execute(ctx, UpdatePageInput{
			UserID:  in.UserID,
			Source:  in.Source,
			ID:      in.PageID,
			Version: in.Version,
			Title:   in.Title,
			Slug:    in.Slug,
			Content: in.Content,
			Kind:    kindPage(),
		})
		if err != nil {
			return nil, err
		}
		if err := uc.rewriteIncomingLinks(in, plan); err != nil {
			return nil, err
		}
		if err := uc.rewritePathChangedSubtree(in.UserID, in.Source, snapshots, plan.oldPath, plan.newPath); err != nil {
			return nil, err
		}
		return uc.tree.GetPage(updated.Page.ID)
	}

	var moveParentID tree.PageID
	if in.NewParentID != nil {
		moveParentID = *in.NewParentID
	}
	moveUC := &MovePageUseCase{tree: uc.tree, orchestrator: o, log: uc.log}
	if err := moveUC.Execute(ctx, MovePageInput{UserID: in.UserID, Source: in.Source, ID: in.PageID, Version: in.Version, ParentID: moveParentID}); err != nil {
		return nil, err
	}
	if err := uc.rewriteIncomingLinks(in, plan); err != nil {
		return nil, err
	}
	if err := uc.rewritePathChangedSubtree(in.UserID, in.Source, snapshots, plan.oldPath, plan.newPath); err != nil {
		return nil, err
	}
	return uc.tree.GetPage(in.PageID)
}

func (uc *ApplyPageRefactorUseCase) defaultOrchestrator() *pagesave.PageSaveOrchestrator {
	return pagesave.NewPageSaveOrchestrator(
		pagesave.NewLinkIndexSideEffect(uc.links, uc.log),
	)
}

func (uc *ApplyPageRefactorUseCase) pageOrchestrator() *pagesave.PageSaveOrchestrator {
	if uc.orchestrator != nil {
		return uc.orchestrator
	}
	uc.orchestrator = uc.defaultOrchestrator()
	return uc.orchestrator
}

func (uc *ApplyPageRefactorUseCase) rewriteIncomingLinks(in RefactorApplyInput, plan *applyRefactorPlan) error {
	if !in.RewriteLinks {
		return nil
	}
	rules := []links.RewriteRule{{OldPath: plan.oldPath, NewPath: plan.newPath, Kind: links.TargetKindFromNodeKind(plan.page.Kind)}}
	return uc.rewriteAffectedPages(in.UserID, in.Source, plan.affectedPageIDs, rules, plan.legacyPageLinkSourceIDs)
}

type applyRefactorPlan struct {
	page                    *tree.Page
	oldPath                 tree.RoutePath
	newPath                 tree.RoutePath
	affectedPageIDs         []tree.PageID
	legacyPageLinkSourceIDs map[tree.PageID]struct{}
}

func (uc *ApplyPageRefactorUseCase) buildApplyPlan(in RefactorApplyInput) (*applyRefactorPlan, error) {
	page, err := uc.tree.GetPage(in.PageID)
	if err != nil {
		return nil, err
	}

	oldPath := page.CalculateRoutePath()
	newRoutePath, err := uc.preview.computeTargetPath(page, in.RefactorPreviewInput)
	if err != nil {
		return nil, err
	}

	plan := &applyRefactorPlan{
		page:    page,
		oldPath: oldPath,
		newPath: newRoutePath,
	}

	if !in.RewriteLinks || uc.refactorLinks == nil {
		return plan, nil
	}

	matches, err := uc.refactorLinks.GetRefactorMatchesForPrefixAndKind(oldPath, page.Kind)
	if err != nil {
		return nil, err
	}

	excludeIDs := subtreeIDSet(page.PageNode)
	seenPageIDs := make(map[tree.PageID]struct{}, len(matches))
	for _, match := range matches {
		if _, excluded := excludeIDs[match.FromPageID]; excluded {
			continue
		}
		if _, seen := seenPageIDs[match.FromPageID]; !seen {
			seenPageIDs[match.FromPageID] = struct{}{}
			plan.affectedPageIDs = append(plan.affectedPageIDs, match.FromPageID)
		}
		if page.Kind == tree.NodeKindPage && match.ToPath == oldPath && match.ToKind == links.TargetKindUnknown && !match.Broken {
			if plan.legacyPageLinkSourceIDs == nil {
				plan.legacyPageLinkSourceIDs = make(map[tree.PageID]struct{})
			}
			plan.legacyPageLinkSourceIDs[match.FromPageID] = struct{}{}
		}
	}

	return plan, nil
}

func validateRefactorVersion(page *tree.Page, version tree.PageVersion) error {
	if version.IsUnchecked() {
		return nil
	}
	pageVersion := page.Version()
	if pageVersion == "" {
		return nil
	}
	if version == "" {
		return tree.ErrVersionRequired
	}
	if pageVersion != version {
		return tree.ErrVersionConflict
	}
	return nil
}

type pathChangeSnapshot struct {
	PageID   tree.PageID
	OldPath  tree.RoutePath
	Content  string
	Kind     tree.NodeKind
	RootPage bool
}

func (uc *ApplyPageRefactorUseCase) captureSnapshots(page *tree.Page, in RefactorApplyInput) ([]pathChangeSnapshot, error) {
	ids := collectSubtreeIDs(page.PageNode)
	if len(ids) == 0 {
		ids = []tree.PageID{in.PageID}
	}
	pages, errs := uc.tree.GetPages(ids)
	snapshots := make([]pathChangeSnapshot, 0, len(ids))
	for i, p := range pages {
		if errs[i] != nil {
			return nil, errs[i]
		}
		content := p.Content
		if ids[i] == in.PageID && in.Content != nil {
			content = *in.Content
		}
		snapshots = append(snapshots, pathChangeSnapshot{
			PageID: p.ID, OldPath: p.CalculateRoutePath(), Content: content, Kind: p.Kind, RootPage: ids[i] == in.PageID,
		})
	}
	return snapshots, nil
}

func (uc *ApplyPageRefactorUseCase) rewriteAffectedPages(userID tree.UserID, source string, affectedPageIDs []tree.PageID, rules []links.RewriteRule, legacyPageLinkSourceIDs map[tree.PageID]struct{}) error {
	engine := links.NewMarkdownRefactorEngineWithOptions(links.MarkdownRefactorOptions{MarkdownLinkRootPrefix: uc.markdownLinkRootPrefix})

	type pending struct {
		page    *tree.Page
		content string
	}
	var items []pending
	var bulk []tree.BulkContentUpdate

	pagesByID := uc.loadPagesByID(affectedPageIDs, "failed to get page for link rewrite, skipping")

	for _, pageID := range affectedPageIDs {
		page, ok := pagesByID[pageID]
		if !ok {
			continue
		}
		pageRules := rules
		if _, hasLegacyPageLink := legacyPageLinkSourceIDs[pageID]; hasLegacyPageLink {
			pageRules = append([]links.RewriteRule{}, rules...)
			for _, rule := range rules {
				if rule.Kind == links.TargetKindPage {
					pageRules = append(pageRules, links.RewriteRule{
						OldPath:    rule.OldPath,
						NewPath:    rule.NewPath,
						Kind:       links.TargetKindSection,
						OutputKind: links.TargetKindPage,
					})
				}
			}
		}
		result := engine.RewriteWithSourceKind(page.Content, page.CalculateRoutePath(), links.MarkdownSourceKindFromNodeKind(page.Kind), pageRules)
		if result.Count() == 0 || result.Content == page.Content {
			continue
		}
		items = append(items, pending{page: page, content: result.Content})
		bulk = append(bulk, tree.BulkContentUpdate{ID: page.ID, Content: result.Content})
	}

	if len(bulk) == 0 {
		return nil
	}

	errs := uc.tree.BulkUpdateContent(userID, bulk)
	updatedIDs := make([]tree.PageID, 0, len(items))

	for i, item := range items {
		if errs[i] != nil {
			uc.log.Warn("failed to rewrite links in page", "pageID", item.page.ID, "error", errs[i])
			continue
		}
		updatedIDs = append(updatedIDs, item.page.ID)
	}

	updatedPages := uc.loadPagesInOrder(updatedIDs, "failed to get rewritten page for side effects")
	if err := uc.runBulkContentUpdateSideEffects(userID, source, updatedPages); err != nil {
		return err
	}
	return nil
}

func (uc *ApplyPageRefactorUseCase) rewritePathChangedSubtree(userID tree.UserID, source string, snapshots []pathChangeSnapshot, oldPath, newPath tree.RoutePath) error {
	engine := links.NewMarkdownRefactorEngineWithOptions(links.MarkdownRefactorOptions{MarkdownLinkRootPrefix: uc.markdownLinkRootPrefix})
	rules := []links.RewriteRule{{OldPath: oldPath, NewPath: newPath, Kind: links.TargetKindFromNodeKind(planNodeKind(snapshots))}}

	type pending struct {
		page    *tree.Page
		content string
	}
	var items []pending
	var bulk []tree.BulkContentUpdate

	pageIDs := make([]tree.PageID, 0, len(snapshots))
	for _, snap := range snapshots {
		pageIDs = append(pageIDs, snap.PageID)
	}
	pagesByID := uc.loadPagesByID(pageIDs, "failed to get subtree page for link rewrite, skipping")

	for _, snap := range snapshots {
		current, ok := pagesByID[snap.PageID]
		if !ok {
			continue
		}
		result := engine.RewriteRelativeLinksForPathChangeWithSourceKind(snap.Content, snap.OldPath, current.CalculateRoutePath(), links.MarkdownSourceKindFromNodeKind(snap.Kind), rules)
		if (result.Count() == 0 && snap.Content == current.Content) || result.Content == current.Content {
			continue
		}
		items = append(items, pending{page: current, content: result.Content})
		bulk = append(bulk, tree.BulkContentUpdate{ID: current.ID, Content: result.Content})
	}

	if len(bulk) == 0 {
		return nil
	}

	errs := uc.tree.BulkUpdateContent(userID, bulk)
	updatedIDs := make([]tree.PageID, 0, len(items))

	for i, item := range items {
		if errs[i] != nil {
			uc.log.Warn("failed to rewrite relative links in subtree page", "pageID", item.page.ID, "error", errs[i])
			continue
		}
		updatedIDs = append(updatedIDs, item.page.ID)
	}

	updatedPages := uc.loadPagesInOrder(updatedIDs, "failed to get rewritten subtree page for side effects")
	if err := uc.runBulkContentUpdateSideEffects(userID, source, updatedPages); err != nil {
		return err
	}
	return nil
}

func (uc *ApplyPageRefactorUseCase) runBulkContentUpdateSideEffects(userID tree.UserID, source string, pages []*tree.Page) error {
	if len(pages) == 0 {
		return nil
	}
	return uc.pageOrchestrator().Run(pagesave.PageSaveEvent{
		Operation:      pagesave.PageOperationUpdate,
		UserID:         userID,
		Source:         source,
		ContentChanged: true,
		AffectedPages:  pages,
		Summary:        "links rewritten",
	})
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func subtreeIDSet(node *tree.PageNode) map[tree.PageID]struct{} {
	ids := make(map[tree.PageID]struct{})
	for _, id := range collectSubtreeIDs(node) {
		ids[id] = struct{}{}
	}
	return ids
}

func planNodeKind(snapshots []pathChangeSnapshot) tree.NodeKind {
	for _, snap := range snapshots {
		if snap.RootPage {
			return snap.Kind
		}
	}
	return tree.NodeKindPage
}

func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

func ensureStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func ensureRefactorWarnings(values []RefactorWarning) []RefactorWarning {
	if values == nil {
		return []RefactorWarning{}
	}
	return values
}

func (uc *ApplyPageRefactorUseCase) loadPagesByID(ids []tree.PageID, warningMessage string) map[tree.PageID]*tree.Page {
	if len(ids) == 0 {
		return map[tree.PageID]*tree.Page{}
	}

	pages, errs := uc.tree.GetPages(ids)
	loaded := make(map[tree.PageID]*tree.Page, len(ids))
	for i, id := range ids {
		if errs[i] != nil {
			uc.log.Warn(warningMessage, "pageID", id, "error", errs[i])
			continue
		}
		if pages[i] == nil {
			continue
		}
		loaded[id] = pages[i]
	}

	return loaded
}

func (uc *ApplyPageRefactorUseCase) loadPagesInOrder(ids []tree.PageID, warningMessage string) []*tree.Page {
	if len(ids) == 0 {
		return nil
	}
	pages, errs := uc.tree.GetPages(ids)
	loaded := make([]*tree.Page, 0, len(ids))
	for i, id := range ids {
		if errs[i] != nil {
			uc.log.Warn(warningMessage, "pageID", id, "error", errs[i])
			continue
		}
		if pages[i] == nil {
			continue
		}
		loaded = append(loaded, pages[i])
	}
	return loaded
}

func collectPreviewWarnings(pages []RefactorAffectedPage) []RefactorWarning {
	var warnings []RefactorWarning
	for _, p := range pages {
		for _, w := range p.WarningDetails {
			if !containsRefactorWarning(warnings, w) {
				warnings = append(warnings, w)
			}
		}
	}
	sortRefactorWarnings(warnings)
	return ensureRefactorWarnings(warnings)
}

func containsRefactorWarning(warnings []RefactorWarning, warning RefactorWarning) bool {
	for _, existing := range warnings {
		if existing.MessageID == warning.MessageID && existing.Message == warning.Message {
			return true
		}
	}
	return false
}

func sortRefactorWarnings(warnings []RefactorWarning) {
	sort.Slice(warnings, func(i, j int) bool {
		if warnings[i].MessageID == warnings[j].MessageID {
			return warnings[i].Message < warnings[j].Message
		}
		return warnings[i].MessageID < warnings[j].MessageID
	})
}

func kindPage() *tree.NodeKind {
	k := tree.NodeKindPage
	return &k
}
