package pages

import (
	"context"
	"log/slog"
	"sort"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/links"
)

const (
	RefactorKindRename = "rename"
	RefactorKindMove   = "move"
)

// RefactorPreviewInput is the input for PreviewPageRefactorUseCase.
type RefactorPreviewInput struct {
	PageID      tree.PageID
	Kind        string
	Title       string
	Slug        tree.Slug
	Content     *string
	NewParentID *tree.PageID
}

// RefactorPreview is the result of a refactor preview operation.
type RefactorPreview struct {
	Kind           string                 `json:"kind"`
	PageID         tree.PageID            `json:"pageId"`
	OldPath        tree.RoutePath         `json:"oldPath"`
	NewPath        string                 `json:"newPath"`
	AffectedPages  []RefactorAffectedPage `json:"affectedPages"`
	Counts         RefactorPreviewCounts  `json:"counts"`
	WarningDetails []RefactorWarning      `json:"warnings"`
}

// RefactorPreviewCounts holds aggregated counts for the preview.
type RefactorPreviewCounts struct {
	AffectedPages int `json:"affectedPages"`
	MatchedLinks  int `json:"matchedLinks"`
}

// RefactorAffectedPage describes a page that has links affected by the refactor.
type RefactorAffectedPage struct {
	FromPageID     tree.PageID       `json:"fromPageId"`
	FromTitle      string            `json:"fromTitle"`
	FromPath       string            `json:"fromPath"`
	MatchedPaths   []string          `json:"matchedPaths"`
	WarningDetails []RefactorWarning `json:"warnings"`
}

type RefactorWarning struct {
	MessageID sharederrors.MessageID `json:"messageId"`
	Message   string                 `json:"message"`
}

// RefactorApplyInput extends the preview with apply options.
type RefactorApplyInput struct {
	UserID  tree.UserID
	Source  string
	Version tree.PageVersion
	RefactorPreviewInput
	RewriteLinks bool
}

// PreviewPageRefactorUseCase computes what would change if a refactor is applied.
type PreviewPageRefactorUseCase struct {
	tree                   refactorPreviewTree
	slug                   *tree.SlugService
	refactorLinks          refactorLinkFinder
	log                    *slog.Logger
	markdownLinkRootPrefix string
}

// NewPreviewPageRefactorUseCase constructs a PreviewPageRefactorUseCase.
func NewPreviewPageRefactorUseCase(
	t *tree.TreeService,
	s *tree.SlugService,
	l *links.LinkService,
	log *slog.Logger,
) *PreviewPageRefactorUseCase {
	return NewPreviewPageRefactorUseCaseWithOptions(t, s, l, log, RefactorUseCaseOptions{})
}

type RefactorUseCaseOptions struct {
	MarkdownLinkRootPrefix string
}

func NewPreviewPageRefactorUseCaseWithOptions(
	t *tree.TreeService,
	s *tree.SlugService,
	l *links.LinkService,
	log *slog.Logger,
	opts RefactorUseCaseOptions,
) *PreviewPageRefactorUseCase {
	var finder refactorLinkFinder
	if l != nil {
		finder = l
	}
	return &PreviewPageRefactorUseCase{tree: t, slug: s, refactorLinks: finder, log: log, markdownLinkRootPrefix: opts.MarkdownLinkRootPrefix}
}

// Execute computes the refactor preview without making changes.
func (uc *PreviewPageRefactorUseCase) Execute(_ context.Context, in RefactorPreviewInput) (*RefactorPreview, error) {
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

	page, err := uc.tree.GetPage(in.PageID)
	if err != nil {
		return nil, err
	}

	oldPath := page.CalculateRoutePath()
	newRoutePath, err := uc.computeTargetPath(page, in)
	if err != nil {
		return nil, err
	}

	excludeIDs := subtreeIDSet(page.PageNode)
	affectedPages, matchedLinks, err := uc.getAffectedPages(oldPath, page.Kind, excludeIDs)
	if err != nil {
		return nil, err
	}

	return &RefactorPreview{
		Kind:          in.Kind,
		PageID:        in.PageID,
		OldPath:       oldPath,
		NewPath:       newRoutePath.WikiPath(),
		AffectedPages: affectedPages,
		Counts: RefactorPreviewCounts{
			AffectedPages: len(affectedPages),
			MatchedLinks:  matchedLinks,
		},
		WarningDetails: collectPreviewWarnings(affectedPages),
	}, nil
}

func (uc *PreviewPageRefactorUseCase) computeTargetPath(page *tree.Page, in RefactorPreviewInput) (tree.RoutePath, error) {
	switch in.Kind {
	case RefactorKindRename:
		ve := sharederrors.NewValidationErrors()
		if in.Title == "" {
			ve.AddWithCode("title", FieldCodePageTitleRequired, MessageIDPageTitleRequired)
		}
		if err := in.Slug.Validate(); err != nil {
			ve.AddWithCode("slug", FieldCodePageSlugInvalid, MessageIDPageSlugInvalid)
		}
		if ve.HasErrors() {
			return "", ve
		}
		if page.Parent != nil {
			return page.Parent.CalculateRoutePath().Child(in.Slug), nil
		}
		return in.Slug.RoutePath(), nil

	case RefactorKindMove:
		var parentID tree.PageID
		if in.NewParentID != nil {
			parentID = *in.NewParentID
		}
		parentRoutePath, err := uc.resolveParentRoutePath(parentID)
		if err != nil {
			return "", err
		}
		return parentRoutePath.Child(page.Slug), nil

	default:
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodePageInvalidRefactorKind, nil)
	}
}

func (uc *PreviewPageRefactorUseCase) resolveParentRoutePath(parentID tree.PageID) (tree.RoutePath, error) {
	if parentID == "" || parentID == tree.RootPageID {
		return "", nil
	}
	parent, err := uc.tree.GetPage(parentID)
	if err != nil {
		return "", err
	}
	return parent.CalculateRoutePath(), nil
}

func (uc *PreviewPageRefactorUseCase) getAffectedPages(oldPath tree.RoutePath, rootKind tree.NodeKind, excludeIDs map[tree.PageID]struct{}) ([]RefactorAffectedPage, int, error) {
	if uc.refactorLinks == nil {
		return []RefactorAffectedPage{}, 0, nil
	}
	matches, err := uc.refactorLinks.GetRefactorMatchesForPrefixAndKind(oldPath, rootKind)
	if err != nil {
		return nil, 0, err
	}

	grouped := make(map[tree.PageID]*RefactorAffectedPage)
	totalMatches := 0
	for _, match := range matches {
		if _, excluded := excludeIDs[match.FromPageID]; excluded {
			continue
		}
		fromPath := ""
		if page, err := uc.tree.GetPage(match.FromPageID); err == nil && page != nil {
			fromPath = page.CalculatePath()
		}
		item, ok := grouped[match.FromPageID]
		if !ok {
			item = &RefactorAffectedPage{
				FromPageID: match.FromPageID,
				FromTitle:  match.FromTitle,
				FromPath:   fromPath,
			}
			grouped[match.FromPageID] = item
		}
		matchedPath := match.ToPath.WikiPath()
		if !containsString(item.MatchedPaths, matchedPath) {
			item.MatchedPaths = append(item.MatchedPaths, matchedPath)
		}
		totalMatches++
	}

	engine := links.NewMarkdownRefactorEngineWithOptions(links.MarkdownRefactorOptions{MarkdownLinkRootPrefix: uc.markdownLinkRootPrefix})
	items := make([]RefactorAffectedPage, 0, len(grouped))
	for _, item := range grouped {
		sourcePage, err := uc.tree.GetPage(item.FromPageID)
		if err != nil {
			return nil, 0, err
		}
		rules := []links.RewriteRule{{OldPath: oldPath, NewPath: oldPath, Kind: links.TargetKindFromNodeKind(rootKind)}}
		result := engine.RewriteWithSourceKind(sourcePage.Content, sourcePage.CalculateRoutePath(), links.MarkdownSourceKindFromNodeKind(sourcePage.Kind), rules)
		for _, w := range result.Warnings {
			warning := RefactorWarning{MessageID: w.MessageID, Message: w.Message}
			if !containsRefactorWarning(item.WarningDetails, warning) {
				item.WarningDetails = append(item.WarningDetails, warning)
			}
		}
		sort.Strings(item.MatchedPaths)
		sortRefactorWarnings(item.WarningDetails)
		item.MatchedPaths = ensureStrings(item.MatchedPaths)
		item.WarningDetails = ensureRefactorWarnings(item.WarningDetails)
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].FromTitle == items[j].FromTitle {
			return items[i].FromPath < items[j].FromPath
		}
		return items[i].FromTitle < items[j].FromTitle
	})
	return items, totalMatches, nil
}

// ─── ApplyPageRefactorUseCase ────────────────────────────────────────────────

// ApplyPageRefactorUseCase applies a rename or move with optional link rewriting.
