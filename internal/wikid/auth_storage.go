package wikid

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	coreauth "github.com/perber/wiki/internal/core/auth"
)

type AuthStorageLayout struct {
	AuthDir    string
	UsersDB    string
	SessionsDB string
	APIKeysDB  string
	OAuthDir   string
}

type AuthStores struct {
	Users    *coreauth.UserStore
	Sessions *coreauth.SessionStore
	APIKeys  *coreauth.APIKeyStore
}

func AuthStoragePaths(dataDir string) AuthStorageLayout {
	dataDir = filepath.Clean(dataDir)
	wikidDir := filepath.Join(dataDir, ".leafwiki", "wikid")
	if filepath.Base(dataDir) == ".leafwiki" {
		wikidDir = filepath.Join(dataDir, "wikid")
	}
	authDir := filepath.Join(wikidDir, "auth")
	return AuthStorageLayout{
		AuthDir:    authDir,
		UsersDB:    filepath.Join(authDir, "users.db"),
		SessionsDB: filepath.Join(authDir, "sessions.db"),
		APIKeysDB:  filepath.Join(authDir, "api_keys.db"),
		OAuthDir:   filepath.Join(wikidDir, "oauth"),
	}
}

func CleanupLegacyAuthDBs(dataDir string) error {
	var joined error
	for _, name := range []string{"users.db", "sessions.db", "api_keys.db"} {
		path := filepath.Join(dataDir, name)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			joined = errors.Join(joined, fmt.Errorf("remove legacy auth DB %s: %w", path, err))
		}
	}
	return joined
}

func OpenAuthStores(dataDir string) (*AuthStores, error) {
	if err := CleanupLegacyAuthDBs(dataDir); err != nil {
		return nil, err
	}
	paths := AuthStoragePaths(dataDir)
	if err := os.MkdirAll(paths.AuthDir, 0o755); err != nil {
		return nil, fmt.Errorf("create wikid auth dir: %w", err)
	}
	if err := os.MkdirAll(paths.OAuthDir, 0o755); err != nil {
		return nil, fmt.Errorf("create wikid oauth dir: %w", err)
	}

	stores := &AuthStores{}
	var err error
	stores.Users, err = coreauth.NewUserStore(paths.AuthDir)
	if err != nil {
		return nil, fmt.Errorf("open wikid user store: %w", err)
	}
	stores.Sessions, err = coreauth.NewSessionStore(paths.AuthDir)
	if err != nil {
		_ = stores.Close()
		return nil, fmt.Errorf("open wikid session store: %w", err)
	}
	stores.APIKeys, err = coreauth.NewAPIKeyStore(paths.AuthDir)
	if err != nil {
		_ = stores.Close()
		return nil, fmt.Errorf("open wikid API key store: %w", err)
	}
	return stores, nil
}

func (s *AuthStores) Close() error {
	if s == nil {
		return nil
	}
	var joined error
	if s.Users != nil {
		joined = errors.Join(joined, s.Users.Close())
	}
	if s.Sessions != nil {
		joined = errors.Join(joined, s.Sessions.Close())
	}
	if s.APIKeys != nil {
		joined = errors.Join(joined, s.APIKeys.Close())
	}
	return joined
}
