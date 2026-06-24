package wiki

import (
	"context"
	"log/slog"
	"mime/multipart"

	"github.com/perber/wiki/internal/core/assets"
	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"
	wikiassets "github.com/perber/wiki/internal/wiki/assets"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

// WikiImportAdapter implements the importer.ImporterWiki interface using
// the wiki's internal services directly via use cases.
type WikiImportAdapter struct {
	tree      *tree.TreeService
	slug      *tree.SlugService
	asset     *assets.AssetService
	pageSaves *pagesave.PageSaveOrchestrator
	log       *slog.Logger
}

// NewWikiImportAdapter constructs an importer adapter backed by the wiki's
// internal services.
func NewWikiImportAdapter(w *Wiki) *WikiImportAdapter {
	return &WikiImportAdapter{
		tree:      w.tree,
		slug:      w.slug,
		asset:     w.asset,
		pageSaves: w.newPageOrchestrator(),
		log:       w.log,
	}
}

func (a *WikiImportAdapter) TreeHash() string {
	return a.tree.TreeHash()
}

func (a *WikiImportAdapter) LookupPagePath(path tree.RoutePath) (*tree.PathLookup, error) {
	routePath := path.Clean()
	if routePath.IsRoot() {
		return &tree.PathLookup{Path: "", Exists: false, Segments: []tree.PathSegment{}}, nil
	}
	var err error
	routePath, err = routePath.Validate()
	if err != nil {
		return nil, err
	}
	return a.tree.LookupPagePath(routePath)
}

func (a *WikiImportAdapter) LookupPagePathForKind(path tree.RoutePath, kind tree.NodeKind) (*tree.PathLookup, error) {
	routePath := path.Clean()
	if routePath.IsRoot() && kind == tree.NodeKindSection {
		return &tree.PathLookup{Path: "", Exists: false, Segments: []tree.PathSegment{}}, nil
	}
	var err error
	routePath, err = routePath.Validate()
	if err != nil {
		return nil, err
	}
	return a.tree.LookupPagePathForKind(routePath, kind)
}

func (a *WikiImportAdapter) FindByPath(route string) (*tree.Page, error) {
	routePath, err := wikipages.ValidateSemanticRoutePath(route)
	if err != nil {
		return nil, err
	}
	return a.tree.FindPageByRoutePath(routePath)
}

func (a *WikiImportAdapter) ListAssets(pageID tree.PageID) ([]string, error) {
	page, err := a.tree.FindPageByID(pageID)
	if err != nil {
		return nil, err
	}
	return a.asset.ListAssetsForPage(page)
}

func (a *WikiImportAdapter) orchestrator() *pagesave.PageSaveOrchestrator {
	return a.pageSaves
}

func (a *WikiImportAdapter) EnsurePath(userID tree.UserID, targetPath tree.RoutePath, title string, kind *tree.NodeKind) (*tree.Page, error) {
	routePath, err := targetPath.Validate()
	if err != nil {
		return nil, err
	}
	out, err := wikipages.NewEnsurePathUseCase(a.tree, a.slug, a.orchestrator(), a.log).Execute(
		context.Background(),
		wikipages.EnsurePathInput{UserID: userID, TargetPath: routePath, TargetTitle: title, Kind: kind},
	)
	if err != nil {
		return nil, err
	}
	return out.Page, nil
}

func (a *WikiImportAdapter) UpdatePage(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, kind *tree.NodeKind) (*tree.Page, error) {
	current, err := a.tree.GetPage(id)
	if err != nil {
		return nil, err
	}
	out, err := wikipages.NewUpdatePageUseCase(a.tree, a.slug, a.orchestrator(), a.log).Execute(
		context.Background(),
		wikipages.UpdatePageInput{
			UserID:     userID,
			ID:         id,
			Version:    current.Version(),
			Title:      title,
			Slug:       slug,
			Content:    content,
			Kind:       kind,
			FromImport: true,
		},
	)
	if err != nil {
		return nil, err
	}
	return out.Page, nil
}

func (a *WikiImportAdapter) UploadAsset(userID tree.UserID, pageID tree.PageID, file multipart.File, filename tree.AssetName, byteCap shared.MaxBytes) (string, error) {
	out, err := wikiassets.NewUploadAssetUseCase(a.tree, a.asset, a.log).Execute(
		context.Background(),
		wikiassets.UploadAssetInput{UserID: userID, PageID: pageID, File: file, Filename: filename, ByteCap: byteCap},
	)
	if err != nil {
		return "", err
	}
	return out.URL, nil
}
