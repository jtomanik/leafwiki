package wiki

import (
	"strings"

	wikiassets "github.com/perber/wiki/internal/wiki/assets"
	wikiauth "github.com/perber/wiki/internal/wiki/auth"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	"github.com/perber/wiki/internal/wiki/pagesave"
	wikirevisions "github.com/perber/wiki/internal/wiki/revisions"
	"github.com/perber/wiki/internal/workspacesync"
)

func (w *Wiki) newPageOrchestrator() *pagesave.PageSaveOrchestrator {
	effects := make([]pagesave.PageSideEffect, 0, 6)
	if w.workspaceSync != nil {
		effects = append(effects, pagesave.NewWorkspaceSyncSideEffectWithActorLookup(w.workspaceSync, w.log, w.workspaceSyncActorForUser))
	}
	effects = append(effects,
		pagesave.NewSearchIndexSideEffect(w.searchIndex, w.tree, w.log),
		pagesave.NewLinkIndexSideEffect(w.links, w.log),
		pagesave.NewTagsSideEffect(w.tags, w.log),
		pagesave.NewPropertiesSideEffect(w.props, w.log),
	)
	return pagesave.NewPageSaveOrchestrator(effects...)
}

func (w *Wiki) workspaceSyncActorForUser(userID string) workspacesync.Actor {
	actor := workspacesync.Actor{ID: userID}
	if w.user == nil || strings.TrimSpace(userID) == "" {
		return actor
	}
	user, err := w.user.GetUserByID(userID)
	if err != nil || user == nil {
		return actor
	}
	actor.Name = user.Username
	actor.Email = user.Email
	return actor
}

func (w *Wiki) buildPagesRoutes() *wikipages.Routes {
	o := w.newPageOrchestrator()
	return wikipages.NewRoutes(wikipages.RoutesConfig{
		TreeService:      w.tree,
		CreatePage:       wikipages.NewCreatePageUseCase(w.tree, w.slug, o, w.log),
		UpdatePage:       wikipages.NewUpdatePageUseCase(w.tree, w.slug, o, w.log),
		DeletePage:       wikipages.NewDeletePageUseCase(w.tree, w.asset, o, w.log),
		MovePage:         wikipages.NewMovePageUseCase(w.tree, o, w.log),
		ConvertPage:      wikipages.NewConvertPageUseCase(w.tree, o, w.log),
		CopyPage:         wikipages.NewCopyPageUseCase(w.tree, w.slug, o, w.asset, w.log),
		GetPage:          wikipages.NewGetPageUseCase(w.tree),
		FindByPath:       wikipages.NewFindByPathUseCase(w.tree),
		LookupPath:       wikipages.NewLookupPagePathUseCase(w.tree),
		ResolvePermalink: wikipages.NewResolvePermalinkUseCase(w.tree),
		SortPages:        wikipages.NewSortPagesUseCase(w.tree),
		EnsurePath:       wikipages.NewEnsurePathUseCase(w.tree, w.slug, o, w.log),
		SuggestSlug:      wikipages.NewSuggestSlugUseCase(w.tree, w.slug),
		PreviewRefactor: wikipages.NewPreviewPageRefactorUseCaseWithOptions(w.tree, w.slug, w.links, w.log, wikipages.RefactorUseCaseOptions{
			MarkdownLinkRootPrefix: w.markdownLinkRootPrefix,
		}),
		ApplyRefactor: wikipages.NewApplyPageRefactorUseCaseWithOrchestratorAndOptions(w.tree, w.slug, w.links, o, w.log, wikipages.RefactorUseCaseOptions{
			MarkdownLinkRootPrefix: w.markdownLinkRootPrefix,
		}),
		UserResolver: w.userResolver,
		AuthService:  w.auth,
	})
}

func (w *Wiki) buildAuthRoutes() *wikiauth.Routes {
	return wikiauth.NewRoutes(wikiauth.RoutesConfig{
		Login:             wikiauth.NewLoginUseCase(w.auth),
		Logout:            wikiauth.NewLogoutUseCase(w.auth),
		RefreshToken:      wikiauth.NewRefreshTokenUseCase(w.auth),
		CreateUser:        wikiauth.NewCreateUserUseCase(w.user, w.userResolver, w.log),
		UpdateUser:        wikiauth.NewUpdateUserUseCase(w.user, w.userResolver, w.log),
		ChangeOwnPassword: wikiauth.NewChangeOwnPasswordUseCase(w.user),
		DeleteUser:        wikiauth.NewDeleteUserUseCase(w.user, w.userResolver, w.log),
		GetUsers:          wikiauth.NewGetUsersUseCase(w.user),
		GetUserByID:       wikiauth.NewGetUserByIDUseCase(w.user),
		CreateAPIKey:      wikiauth.NewCreateAPIKeyUseCase(w.apiKeys, w.user),
		ListAPIKeys:       wikiauth.NewListAPIKeysUseCase(w.apiKeys),
		RevokeAPIKey:      wikiauth.NewRevokeAPIKeyUseCase(w.apiKeys),
		AuthService:       w.auth,
	})
}

func (w *Wiki) buildAssetsRoutes() *wikiassets.Routes {
	return wikiassets.NewRoutes(wikiassets.RoutesConfig{
		Upload:      wikiassets.NewUploadAssetUseCase(w.tree, w.asset, w.log),
		List:        wikiassets.NewListAssetsUseCase(w.tree, w.asset),
		Rename:      wikiassets.NewRenameAssetUseCase(w.tree, w.asset, w.log),
		Delete:      wikiassets.NewDeleteAssetUseCase(w.tree, w.asset, w.log),
		AuthService: w.auth,
		AssetsDir:   w.asset.GetAssetsDir(),
		Log:         w.log,
	})
}

func (w *Wiki) buildRevisionsRoutes() *wikirevisions.Routes {
	return wikirevisions.NewRoutes(wikirevisions.RoutesConfig{
		ListWorkspaceRevisions:   w.WorkspaceSyncPageRevisions,
		GetWorkspaceRevision:     w.WorkspaceSyncPageRevision,
		RestoreWorkspaceRevision: w.WorkspaceSyncRestorePageRevision,
		UserResolver:             w.userResolver,
		AuthService:              w.auth,
		TreeService:              w.tree,
	})
}
