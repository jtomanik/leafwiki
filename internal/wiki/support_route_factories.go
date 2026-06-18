package wiki

import (
	"os"
	"path/filepath"

	coreimporter "github.com/perber/wiki/internal/importer"
	wikibranding "github.com/perber/wiki/internal/wiki/branding"
	wikiimporter "github.com/perber/wiki/internal/wiki/importer"
	wikilinks "github.com/perber/wiki/internal/wiki/links"
	wikiproperties "github.com/perber/wiki/internal/wiki/properties"
	wikisearch "github.com/perber/wiki/internal/wiki/search"
	wikitags "github.com/perber/wiki/internal/wiki/tags"
)

func (w *Wiki) buildSearchRoutes() *wikisearch.Routes {
	return wikisearch.NewRoutes(wikisearch.RoutesConfig{
		Search:            wikisearch.NewSearchUseCase(w.searchIndex, w.tags, w.tree),
		GetIndexingStatus: wikisearch.NewGetIndexingStatusUseCase(w.status),
		AuthService:       w.auth,
	})
}

func (w *Wiki) buildLinksRoutes() *wikilinks.Routes {
	return wikilinks.NewRoutes(wikilinks.RoutesConfig{
		GetLinkStatus: wikilinks.NewGetLinkStatusUseCase(w.links, w.tree),
		AuthService:   w.auth,
	})
}

func (w *Wiki) buildTagsRoutes() *wikitags.Routes {
	return wikitags.NewRoutes(wikitags.RoutesConfig{
		GetTags:        wikitags.NewGetTagsUseCase(w.tags),
		GetPagesByTags: wikitags.NewGetPagesByTagsUseCase(w.tags, w.tree, w.userResolver),
		AuthService:    w.auth,
	})
}

func (w *Wiki) buildPropertiesRoutes() *wikiproperties.Routes {
	return wikiproperties.NewRoutes(wikiproperties.RoutesConfig{
		GetPropertyKeys:    wikiproperties.NewGetPropertyKeysUseCase(w.props),
		GetPagesByProperty: wikiproperties.NewGetPagesByPropertyUseCase(w.props, w.tree, w.userResolver),
		AuthService:        w.auth,
	})
}

func (w *Wiki) buildBrandingRoutes() *wikibranding.Routes {
	return wikibranding.NewRoutes(wikibranding.RoutesConfig{
		GetBranding:     wikibranding.NewGetBrandingUseCase(w.branding),
		UpdateBranding:  wikibranding.NewUpdateBrandingUseCase(w.branding),
		UploadLogo:      wikibranding.NewUploadLogoUseCase(w.branding),
		DeleteLogo:      wikibranding.NewDeleteLogoUseCase(w.branding),
		UploadFavicon:   wikibranding.NewUploadFaviconUseCase(w.branding),
		DeleteFavicon:   wikibranding.NewDeleteFaviconUseCase(w.branding),
		BrandingService: w.branding,
		AuthService:     w.auth,
		Log:             w.log,
	})
}

func (w *Wiki) buildImporterRoutes(options *WikiOptions) *wikiimporter.Routes {
	importerDir := filepath.Join(w.storageDir, ".importer")
	if err := os.MkdirAll(importerDir, 0o755); err != nil {
		w.log.Warn("failed to create importer state directory", "path", importerDir, "error", err)
	}
	adapter := NewWikiImportAdapter(w)
	planner := coreimporter.NewPlanner(adapter, w.slug)
	store := coreimporter.NewPlanStore(filepath.Join(importerDir, "current-plan.json"))
	svc := coreimporter.NewImporterServiceWithOptions(planner, store, filepath.Join(importerDir, "workspaces"), options.MaxAssetUploadSizeBytes, coreimporter.ImporterServiceOptions{
		MarkdownLinkRootPrefix: options.MarkdownLinkRootPrefix,
	})
	return wikiimporter.NewRoutes(wikiimporter.RoutesConfig{
		CreatePlan:  wikiimporter.NewCreateImportPlanUseCase(svc),
		GetPlan:     wikiimporter.NewGetImportPlanUseCase(svc),
		Execute:     wikiimporter.NewExecuteImportUseCase(svc),
		ClearPlan:   wikiimporter.NewClearImportPlanUseCase(svc),
		AuthService: w.auth,
		Svc:         svc,
		Log:         w.log,
	})
}
