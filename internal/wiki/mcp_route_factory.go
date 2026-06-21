package wiki

import (
	wikiassets "github.com/perber/wiki/internal/wiki/assets"
	wikilinks "github.com/perber/wiki/internal/wiki/links"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	wikiproperties "github.com/perber/wiki/internal/wiki/properties"
	wikisearch "github.com/perber/wiki/internal/wiki/search"
	wikitags "github.com/perber/wiki/internal/wiki/tags"
)

func (w *Wiki) buildMCPRoutes() *wikimcp.Routes {
	o := w.newPageOrchestrator()
	return wikimcp.NewRoutes(wikimcp.RoutesConfig{
		TreeService:  w.tree,
		UserResolver: w.userResolver,
		CreatePage:   wikipages.NewCreatePageUseCase(w.tree, w.slug, o, w.log),
		UpdatePage:   wikipages.NewUpdatePageUseCase(w.tree, w.slug, o, w.log),
		GetPage:      wikipages.NewGetPageUseCase(w.tree),
		FindByPath:   wikipages.NewFindByPathUseCase(w.tree),
		LookupPath:   wikipages.NewLookupPagePathUseCase(w.tree),
		ResolveLink:  wikipages.NewResolvePermalinkUseCase(w.tree),
		SuggestSlug:  wikipages.NewSuggestSlugUseCase(w.tree, w.slug),
		DeletePage:   wikipages.NewDeletePageUseCase(w.tree, w.asset, o, w.log),
		MovePage:     wikipages.NewMovePageUseCase(w.tree, o, w.log),
		SortPages:    wikipages.NewSortPagesUseCase(w.tree),
		EnsurePath:   wikipages.NewEnsurePathUseCase(w.tree, w.slug, o, w.log),
		ConvertPage:  wikipages.NewConvertPageUseCase(w.tree, o, w.log),
		CopyPage:     wikipages.NewCopyPageUseCase(w.tree, w.slug, o, w.asset, w.log),
		PreviewRef: wikipages.NewPreviewPageRefactorUseCaseWithOptions(w.tree, w.slug, w.links, w.log, wikipages.RefactorUseCaseOptions{
			MarkdownLinkRootPrefix: w.markdownLinkRootPrefix,
		}),
		ApplyRef: wikipages.NewApplyPageRefactorUseCaseWithOrchestratorAndOptions(w.tree, w.slug, w.links, o, w.log, wikipages.RefactorUseCaseOptions{
			MarkdownLinkRootPrefix: w.markdownLinkRootPrefix,
		}),
		Search:       wikisearch.NewSearchUseCase(w.searchIndex, w.tags, w.tree),
		SearchStatus: wikisearch.NewGetIndexingStatusUseCase(w.status),
		GetTags:      wikitags.NewGetTagsUseCase(w.tags),
		PagesByTags:  wikitags.NewGetPagesByTagsUseCase(w.tags, w.tree, w.userResolver),
		PropertyKeys: wikiproperties.NewGetPropertyKeysUseCase(w.props),
		PagesByProp:  wikiproperties.NewGetPagesByPropertyUseCase(w.props, w.tree, w.userResolver),
		LinkStatus:   wikilinks.NewGetLinkStatusUseCase(w.links, w.tree),
		UploadAsset:  wikiassets.NewUploadAssetUseCase(w.tree, w.asset, w.log),
		GetAsset:     wikiassets.NewGetAssetUseCase(w.tree, w.asset),
		GetAssets:    wikiassets.NewListAssetsUseCase(w.tree, w.asset),
		RenameAsset:  wikiassets.NewRenameAssetUseCase(w.tree, w.asset, w.log),
		DeleteAsset:  wikiassets.NewDeleteAssetUseCase(w.tree, w.asset, w.log),

		ListWorkspaceRevisions:   w.WorkspaceSyncPageRevisions,
		GetWorkspaceRevision:     w.WorkspaceSyncPageRevision,
		RestoreWorkspaceRevision: w.WorkspaceSyncRestorePageRevision,
		WorkspaceSyncStatus:      w.WorkspaceSyncStatus,
		WorkspaceSyncRefresh:     w.WorkspaceSyncRefresh,
		ListWorkspaceSnapshots:   w.WorkspaceSyncSnapshotPage,
		WorkspaceRootDir:         w.workspace.RootDir,
		WorkspaceDataDir:         w.workspace.DataDir,
		MarkdownLinkRootPrefix:   w.markdownLinkRootPrefix,
		WebPresenceProvider:      w.WebPresenceSessions,
		AgentPresenceProvider:    w.AgentPresenceSessions,
		UserService:              w.user,
		APIKeys:                  w.apiKeys,
		OAuthService:             w.oauth,
		WorkspaceID:              w.workspace.ID,
	})
}
