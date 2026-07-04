package wiki

import (
	"github.com/perber/wiki/internal/core/auth"
	wikioauth "github.com/perber/wiki/internal/wiki/oauth"
)

func (w *Wiki) GetStorageDir() string {
	return w.storageDir
}

func (w *Wiki) GetRootDir() string {
	return w.workspace.RootDir
}

func (w *Wiki) Workspace() Workspace {
	return w.workspace
}

func (w *Wiki) UserService() *auth.UserService {
	return w.user
}

func (w *Wiki) AuthService() *auth.AuthService {
	return w.auth
}

func (w *Wiki) APIKeyService() *auth.APIKeyService {
	return w.apiKeys
}

func (w *Wiki) OAuthService() *wikioauth.Service {
	return w.oauth
}

func (w *Wiki) Close() error {
	if w.workspaceSyncCancel != nil {
		w.workspaceSyncCancel()
	}
	if w.workspaceSync != nil {
		w.workspaceSync.StopWatcher()
	}
	if w.status != nil {
		w.status.Finish()
	}
	if w.user != nil {
		if err := wikiCloseUserService(w.user); err != nil {
			return err
		}
	}
	if w.apiKeys != nil {
		if err := wikiCloseAPIKeyService(w.apiKeys); err != nil {
			return err
		}
	}

	if w.links != nil {
		if err := wikiCloseLinksService(w.links); err != nil {
			w.log.Error("error closing links", "error", err)
		}
	}

	if w.searchIndex != nil {
		return wikiCloseSearchIndex(w.searchIndex)
	}
	return nil
}
