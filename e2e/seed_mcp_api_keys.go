package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	coreauth "github.com/perber/wiki/internal/core/auth"
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
	Admin   seededUser `json:"admin"`
	Editor  seededUser `json:"editor"`
	Viewer  seededUser `json:"viewer"`
	Revoked seededUser `json:"revoked"`
	Deleted seededUser `json:"deleted"`
}

func main() {
	dataDir := flag.String("data-dir", "", "LeafWiki data directory")
	outputPath := flag.String("output", "", "JSON output path")
	flag.Parse()
	if *dataDir == "" || *outputPath == "" {
		fatalf("--data-dir and --output are required")
	}

	usersStore, err := coreauth.NewUserStore(*dataDir)
	if err != nil {
		fatalf("create user store: %v", err)
	}
	defer usersStore.Close()
	users := coreauth.NewUserService(usersStore)
	if err := users.InitDefaultAdmin("admin"); err != nil {
		fatalf("init admin: %v", err)
	}

	apiKeyStore, err := coreauth.NewAPIKeyStore(*dataDir)
	if err != nil {
		fatalf("create api key store: %v", err)
	}
	apiKeys := coreauth.NewAPIKeyService(apiKeyStore, users)
	defer apiKeys.Close()

	admin, err := users.GetUserByUsername("admin")
	if err != nil {
		fatalf("load admin: %v", err)
	}
	editor := createUser(users, "stdio-editor", "stdio-editor@example.com", coreauth.RoleEditor)
	viewer := createUser(users, "stdio-viewer", "stdio-viewer@example.com", coreauth.RoleViewer)
	revoked := createUser(users, "stdio-revoked", "stdio-revoked@example.com", coreauth.RoleEditor)
	deleted := createUser(users, "stdio-deleted", "stdio-deleted@example.com", coreauth.RoleEditor)

	out := seedOutput{
		Admin:   createKey(apiKeys, admin, "E2E STDIO admin"),
		Editor:  createKey(apiKeys, editor, "E2E STDIO editor"),
		Viewer:  createKey(apiKeys, viewer, "E2E STDIO viewer"),
		Revoked: createKey(apiKeys, revoked, "E2E STDIO revoked"),
		Deleted: createKey(apiKeys, deleted, "E2E STDIO deleted"),
	}
	if err := apiKeys.RevokeAPIKey(revoked.ID, out.Revoked.APIKeyID); err != nil {
		fatalf("revoke seeded key: %v", err)
	}
	if err := users.DeleteUser(deleted.ID); err != nil {
		fatalf("delete seeded user: %v", err)
	}

	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fatalf("marshal output: %v", err)
	}
	if err := os.WriteFile(*outputPath, raw, 0o600); err != nil {
		fatalf("write output: %v", err)
	}
}

func createUser(users *coreauth.UserService, username, email, role string) *coreauth.User {
	user, err := users.CreateUser(username, email, "password", role)
	if err != nil {
		fatalf("create %s: %v", username, err)
	}
	return user
}

func createKey(apiKeys *coreauth.APIKeyService, user *coreauth.User, name string) seededUser {
	created, err := apiKeys.CreateAPIKey(user.ID, name, user.ID)
	if err != nil {
		fatalf("create key for %s: %v", user.Username, err)
	}
	return seededUser{
		ID:       user.ID,
		Username: user.Username,
		Email:    user.Email,
		Role:     user.Role,
		APIKeyID: created.Key.ID,
		APIKey:   created.Secret,
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
