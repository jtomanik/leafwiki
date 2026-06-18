package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wikid"
)

type seededUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	APIKeyID string `json:"apiKeyId"`
	APIKey   string `json:"apiKey"`
}

type seedOutput struct {
	Admin        seededUser `json:"admin"`
	Editor       seededUser `json:"editor"`
	SecondEditor seededUser `json:"secondEditor"`
	Viewer       seededUser `json:"viewer"`
	Revoked      seededUser `json:"revoked"`
	Deleted      seededUser `json:"deleted"`
}

func main() {
	dataDir := flag.String("data-dir", "", "LeafWiki data directory")
	outputPath := flag.String("output", "", "JSON output path")
	runtimeStack := flag.String("runtime-stack", runtimeStackFromEnv(), "LeafWiki runtime stack for auth-store layout")
	flag.Parse()
	if *dataDir == "" || *outputPath == "" {
		fatalf("--data-dir and --output are required")
	}

	out, err := seedMCPAPIKeysForRuntime(*dataDir, *runtimeStack)
	if err != nil {
		fatalf("seed MCP API keys: %v", err)
	}

	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fatalf("marshal output: %v", err)
	}
	if err := os.WriteFile(*outputPath, raw, 0o600); err != nil {
		fatalf("write output: %v", err)
	}
}

func seedMCPAPIKeys(dataDir string) (seedOutput, error) {
	return seedMCPAPIKeysForRuntime(dataDir, runtimeStackFromEnv())
}

func runtimeStackFromEnv() string {
	if stack := strings.TrimSpace(os.Getenv("LEAFWIKI_RUN_MCP_RUNTIME_STACK")); stack != "" {
		return stack
	}
	if stack := strings.TrimSpace(os.Getenv("LEAFWIKI_RUNTIME_STACK")); stack != "" {
		return stack
	}
	return projectdaemon.RuntimeStackWikidFrontd
}

func seedMCPAPIKeysForRuntime(dataDir string, runtimeStack string) (seedOutput, error) {
	stores, err := openAuthStoresForRuntime(dataDir, runtimeStack)
	if err != nil {
		return seedOutput{}, err
	}
	defer stores.Close()
	users := coreauth.NewUserService(stores.Users)
	if err := users.InitDefaultAdmin("admin"); err != nil {
		return seedOutput{}, fmt.Errorf("init admin: %w", err)
	}

	apiKeys := coreauth.NewAPIKeyService(stores.APIKeys, users)

	admin, err := users.GetUserByUsername("admin")
	if err != nil {
		return seedOutput{}, fmt.Errorf("load admin: %w", err)
	}
	editor, err := createUser(users, "stdio-editor", "stdio-editor@example.com", coreauth.RoleEditor)
	if err != nil {
		return seedOutput{}, err
	}
	secondEditor, err := createUser(users, "stdio-second-editor", "stdio-second-editor@example.com", coreauth.RoleEditor)
	if err != nil {
		return seedOutput{}, err
	}
	viewer, err := createUser(users, "stdio-viewer", "stdio-viewer@example.com", coreauth.RoleViewer)
	if err != nil {
		return seedOutput{}, err
	}
	revoked, err := createUser(users, "stdio-revoked", "stdio-revoked@example.com", coreauth.RoleEditor)
	if err != nil {
		return seedOutput{}, err
	}
	deleted, err := createUser(users, "stdio-deleted", "stdio-deleted@example.com", coreauth.RoleEditor)
	if err != nil {
		return seedOutput{}, err
	}

	out := seedOutput{}
	if out.Admin, err = createKey(apiKeys, admin, "E2E STDIO admin"); err != nil {
		return seedOutput{}, err
	}
	if out.Editor, err = createKey(apiKeys, editor, "E2E STDIO editor"); err != nil {
		return seedOutput{}, err
	}
	if out.SecondEditor, err = createKey(apiKeys, secondEditor, "E2E STDIO second editor"); err != nil {
		return seedOutput{}, err
	}
	if out.Viewer, err = createKey(apiKeys, viewer, "E2E STDIO viewer"); err != nil {
		return seedOutput{}, err
	}
	if out.Revoked, err = createKey(apiKeys, revoked, "E2E STDIO revoked"); err != nil {
		return seedOutput{}, err
	}
	if out.Deleted, err = createKey(apiKeys, deleted, "E2E STDIO deleted"); err != nil {
		return seedOutput{}, err
	}
	if err := apiKeys.RevokeAPIKey(revoked.ID, out.Revoked.APIKeyID); err != nil {
		return seedOutput{}, fmt.Errorf("revoke seeded key: %w", err)
	}
	if err := users.DeleteUser(deleted.ID); err != nil {
		return seedOutput{}, fmt.Errorf("delete seeded user: %w", err)
	}
	return out, nil
}

func openAuthStoresForRuntime(dataDir string, runtimeStack string) (*wikid.AuthStores, error) {
	switch strings.TrimSpace(runtimeStack) {
	case projectdaemon.RuntimeStackLegacy:
		stores, err := openLegacyAuthStores(dataDir)
		if err != nil {
			return nil, fmt.Errorf("open legacy auth stores: %w", err)
		}
		return stores, nil
	case "", projectdaemon.RuntimeStackWikidFrontd:
		stores, err := wikid.OpenAuthStores(dataDir)
		if err != nil {
			return nil, fmt.Errorf("open wikid auth stores: %w", err)
		}
		return stores, nil
	default:
		return nil, fmt.Errorf("unsupported runtime stack %q", runtimeStack)
	}
}

func openLegacyAuthStores(dataDir string) (*wikid.AuthStores, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create legacy auth dir: %w", err)
	}
	stores := &wikid.AuthStores{}
	var err error
	stores.Users, err = coreauth.NewUserStore(dataDir)
	if err != nil {
		return nil, fmt.Errorf("open legacy user store: %w", err)
	}
	stores.Sessions, err = coreauth.NewSessionStore(dataDir)
	if err != nil {
		_ = stores.Close()
		return nil, fmt.Errorf("open legacy session store: %w", err)
	}
	stores.APIKeys, err = coreauth.NewAPIKeyStore(dataDir)
	if err != nil {
		_ = stores.Close()
		return nil, fmt.Errorf("open legacy API key store: %w", err)
	}
	return stores, nil
}

func createUser(users *coreauth.UserService, username, email, role string) (*coreauth.User, error) {
	user, err := users.CreateUser(username, email, "password", role)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", username, err)
	}
	return user, nil
}

func createKey(apiKeys *coreauth.APIKeyService, user *coreauth.User, name string) (seededUser, error) {
	created, err := apiKeys.CreateAPIKey(user.ID, name, user.ID)
	if err != nil {
		return seededUser{}, fmt.Errorf("create key for %s: %w", user.Username, err)
	}
	return seededUser{
		ID:       user.ID,
		Username: user.Username,
		Email:    user.Email,
		Role:     user.Role,
		APIKeyID: created.Key.ID,
		APIKey:   created.Secret,
	}, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
