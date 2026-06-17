package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wikid"
)

func TestSeedMCPAPIKeysWritesLegacyAuthStoreWhenExplicitlyRequested(t *testing.T) {
	dataDir := t.TempDir()

	seeds, err := seedMCPAPIKeysForRuntime(dataDir, projectdaemon.RuntimeStackLegacy)
	if err != nil {
		t.Fatalf("seed MCP API keys: %v", err)
	}

	stores, err := openLegacyAuthStores(dataDir)
	if err != nil {
		t.Fatalf("open legacy auth stores: %v", err)
	}
	defer stores.Close()
	users := coreauth.NewUserService(stores.Users)
	apiKeys := coreauth.NewAPIKeyService(stores.APIKeys, users)
	defer apiKeys.Close()

	verified, err := apiKeys.VerifyAPIKey(seeds.Editor.APIKey)
	if err != nil {
		t.Fatalf("verify seeded editor API key from legacy auth store: %v", err)
	}
	if verified.User.Username != seeds.Editor.Username {
		t.Fatalf("verified username = %q, want %q", verified.User.Username, seeds.Editor.Username)
	}
}

func TestSeedMCPAPIKeysWritesWikidAuthStoreByDefault(t *testing.T) {
	t.Setenv("LEAFWIKI_RUNTIME_STACK", "")
	t.Setenv("LEAFWIKI_RUN_MCP_RUNTIME_STACK", "")
	dataDir := t.TempDir()

	seeds, err := seedMCPAPIKeys(dataDir)
	if err != nil {
		t.Fatalf("seed MCP API keys: %v", err)
	}

	assertWikidSeededAPIKey(t, dataDir, seeds)
}

func TestSeedMCPAPIKeysUsesRunMCPRuntimeStackPrecedence(t *testing.T) {
	t.Setenv("LEAFWIKI_RUNTIME_STACK", projectdaemon.RuntimeStackLegacy)
	t.Setenv("LEAFWIKI_RUN_MCP_RUNTIME_STACK", projectdaemon.RuntimeStackWikidFrontd)
	dataDir := t.TempDir()

	seeds, err := seedMCPAPIKeys(dataDir)
	if err != nil {
		t.Fatalf("seed MCP API keys: %v", err)
	}

	assertWikidSeededAPIKey(t, dataDir, seeds)
}

func TestSeedMCPAPIKeysWritesWikidAuthStore(t *testing.T) {
	dataDir := t.TempDir()

	seeds, err := seedMCPAPIKeysForRuntime(dataDir, projectdaemon.RuntimeStackWikidFrontd)
	if err != nil {
		t.Fatalf("seed MCP API keys: %v", err)
	}

	assertWikidSeededAPIKey(t, dataDir, seeds)
}

func assertWikidSeededAPIKey(t *testing.T, dataDir string, seeds seedOutput) {
	t.Helper()
	for _, name := range []string{"users.db", "sessions.db", "api_keys.db"} {
		if _, err := os.Stat(filepath.Join(dataDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("legacy auth DB %s stat err = %v, want not exist", name, err)
		}
	}

	stores, err := wikid.OpenAuthStores(dataDir)
	if err != nil {
		t.Fatalf("open wikid auth stores: %v", err)
	}
	defer stores.Close()
	users := coreauth.NewUserService(stores.Users)
	apiKeys := coreauth.NewAPIKeyService(stores.APIKeys, users)
	defer apiKeys.Close()

	verified, err := apiKeys.VerifyAPIKey(seeds.Editor.APIKey)
	if err != nil {
		t.Fatalf("verify seeded editor API key from wikid auth store: %v", err)
	}
	if verified.User.Username != seeds.Editor.Username {
		t.Fatalf("verified username = %q, want %q", verified.User.Username, seeds.Editor.Username)
	}
}
