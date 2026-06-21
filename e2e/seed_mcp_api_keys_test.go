package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/wikid"
)

func TestSeedMCPAPIKeysWritesWikidAuthStoreByDefault(t *testing.T) {
	unsetEnvForTest(t, "LEAFWIKI_RUNTIME_STACK")
	unsetEnvForTest(t, "LEAFWIKI_RUN_MCP_RUNTIME_STACK")
	dataDir := t.TempDir()

	seeds, err := seedMCPAPIKeys(dataDir)
	if err != nil {
		t.Fatalf("seed MCP API keys: %v", err)
	}

	assertWikidSeededAPIKey(t, dataDir, seeds)
}

func TestSeedMCPAPIKeysRejectsRemovedRuntimeStackEnvironment(t *testing.T) {
	for _, name := range []string{
		"LEAFWIKI_RUNTIME_STACK",
		"LEAFWIKI_RUN_MCP_RUNTIME_STACK",
	} {
		t.Run(name, func(t *testing.T) {
			unsetEnvForTest(t, "LEAFWIKI_RUNTIME_STACK")
			unsetEnvForTest(t, "LEAFWIKI_RUN_MCP_RUNTIME_STACK")
			t.Setenv(name, "legacy")

			_, err := seedMCPAPIKeys(t.TempDir())
			if err == nil {
				t.Fatalf("seed MCP API keys succeeded with %s set, want error", name)
			}
			if !strings.Contains(err.Error(), "unknown environment variable: "+name) {
				t.Fatalf("error = %q, want unknown environment variable for %s", err, name)
			}
		})
	}
}

func TestSeedMCPAPIKeysWritesWikidAuthStore(t *testing.T) {
	dataDir := t.TempDir()

	seeds, err := seedMCPAPIKeys(dataDir)
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

func unsetEnvForTest(t *testing.T, name string) {
	t.Helper()
	value, ok := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(name, value)
		} else {
			_ = os.Unsetenv(name)
		}
	})
}
