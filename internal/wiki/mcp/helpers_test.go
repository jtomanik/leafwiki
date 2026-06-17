package mcp

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/projectdaemon"
)

func TestActorForRequestRejectsMissingTokenInfoByDefault(t *testing.T) {
	t.Parallel()

	routes := &Routes{}
	for _, req := range []*sdkmcp.CallToolRequest{
		nil,
		{},
		{Extra: &sdkmcp.RequestExtra{}},
	} {
		if user, err := routes.actorForRequest(req); err == nil {
			t.Fatalf("actorForRequest(%#v) returned user %#v, want missing-token error", req, user)
		}
	}
}

func TestActorForRequestUsesPrivateActorContextHeader(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	encoded, err := projectdaemon.EncodeActorContext(projectdaemon.ActorContext{
		Version:     1,
		Issuer:      projectdaemon.ActorContextIssuerWikid,
		Subject:     "user:editor-1",
		Username:    "editor",
		Email:       "editor@example.com",
		Role:        coreauth.RoleEditor,
		WorkspaceID: "current",
		AuthMethod:  "oauth",
		IssuedAt:    now,
		ExpiresAt:   now.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("EncodeActorContext failed: %v", err)
	}

	routes := &Routes{
		workspaceID:          "current",
		now:                  func() time.Time { return now.Add(time.Minute) },
		actorContextAllowed:  true,
		actorContextRequired: true,
	}
	header := http.Header{}
	header.Set(projectdaemon.ActorContextHeader, encoded)
	req := &sdkmcp.CallToolRequest{
		Extra: &sdkmcp.RequestExtra{
			Header: header,
		},
	}
	user, err := routes.actorForRequest(req)
	if err != nil {
		t.Fatalf("actorForRequest with actor context failed: %v", err)
	}
	if user.ID != "editor-1" || user.Username != "editor" || user.Role != coreauth.RoleEditor {
		t.Fatalf("actor = %#v, want private actor context user", user)
	}
}

func TestActorForMissingTokenInfoUsesStdioAPIKeyAndReloadsCurrentUser(t *testing.T) {
	t.Parallel()

	userService, apiKeyService, editor := newMCPAuthServices(t)
	created, err := apiKeyService.CreateAPIKey(editor.ID, "Native STDIO", editor.ID)
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}
	routes := &Routes{
		apiKeys:     apiKeyService,
		stdioAPIKey: created.Secret,
	}

	user, err := routes.actorForRequest(nil)
	if err != nil {
		t.Fatalf("actorForRequest with stdio API key failed: %v", err)
	}
	if user.Username != "editor" || user.Role != coreauth.RoleEditor {
		t.Fatalf("user = %#v, want editor role", user)
	}

	if _, err := userService.UpdateUser(editor.ID, "editor", "editor@example.com", "", coreauth.RoleViewer); err != nil {
		t.Fatalf("UpdateUser role downgrade failed: %v", err)
	}
	user, err = routes.actorForRequest(nil)
	if err != nil {
		t.Fatalf("actorForRequest after role change failed: %v", err)
	}
	if user.Role != coreauth.RoleViewer {
		t.Fatalf("role after downgrade = %q, want viewer", user.Role)
	}

	if err := apiKeyService.RevokeAPIKey(editor.ID, created.Key.ID); err != nil {
		t.Fatalf("RevokeAPIKey failed: %v", err)
	}
	if _, err := routes.actorForRequest(nil); err == nil || !strings.Contains(err.Error(), "authenticated MCP user") {
		t.Fatalf("actorForRequest after revoke error = %v, want authenticated user error", err)
	}
}

func TestAPIKeyBearerVerificationPreservesStorageErrors(t *testing.T) {
	_, apiKeyService, _, created, apiKeyDBPath := newMCPAPIKeyAuthFixture(t)
	blocker := beginExclusiveMCPTestSQLiteTransaction(t, apiKeyDBPath)
	defer blocker.rollback(t)
	routes := &Routes{apiKeys: apiKeyService}

	_, err := routes.verifyBearerToken(context.Background(), created.Secret, nil)
	if err == nil {
		t.Fatalf("verifyBearerToken unexpectedly succeeded")
	}
	if errors.Is(err, sdkauth.ErrInvalidToken) {
		t.Fatalf("verifyBearerToken error = %v, want storage error not invalid-token classification", err)
	}
	if !strings.Contains(err.Error(), "api key verifier") && !strings.Contains(err.Error(), "database") {
		t.Fatalf("verifyBearerToken error = %v, want storage failure context", err)
	}
}

func TestActorForMissingTokenInfoPreservesAPIKeyStorageErrors(t *testing.T) {
	_, apiKeyService, _, created, apiKeyDBPath := newMCPAPIKeyAuthFixture(t)
	blocker := beginExclusiveMCPTestSQLiteTransaction(t, apiKeyDBPath)
	defer blocker.rollback(t)
	routes := &Routes{
		apiKeys:     apiKeyService,
		stdioAPIKey: created.Secret,
	}

	_, err := routes.actorForRequest(nil)
	if err == nil {
		t.Fatalf("actorForRequest unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), "authenticated MCP user not found") {
		t.Fatalf("actorForRequest error = %v, want storage failure not not-found classification", err)
	}
	if !strings.Contains(err.Error(), "authenticated MCP user") || !strings.Contains(err.Error(), "database") {
		t.Fatalf("actorForRequest error = %v, want authenticated-user storage failure context", err)
	}
}

func newMCPAuthServices(t *testing.T) (*coreauth.UserService, *coreauth.APIKeyService, *coreauth.User) {
	t.Helper()
	userService, apiKeyService, editor, _, _ := newMCPAPIKeyAuthFixture(t)
	return userService, apiKeyService, editor
}

func newMCPAPIKeyAuthFixture(t *testing.T) (*coreauth.UserService, *coreauth.APIKeyService, *coreauth.User, *coreauth.APIKeyCreateResult, string) {
	t.Helper()

	store, err := coreauth.NewUserStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewUserStore failed: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close user store: %v", err)
		}
	})
	userService := coreauth.NewUserService(store)
	editor, err := userService.CreateUser("editor", "editor@example.com", "password", coreauth.RoleEditor)
	if err != nil {
		t.Fatalf("CreateUser editor failed: %v", err)
	}

	apiKeyDir := t.TempDir()
	apiKeyStore, err := coreauth.NewAPIKeyStore(apiKeyDir)
	if err != nil {
		t.Fatalf("NewAPIKeyStore failed: %v", err)
	}
	apiKeyService := coreauth.NewAPIKeyService(apiKeyStore, userService)
	t.Cleanup(func() {
		if err := apiKeyService.Close(); err != nil {
			t.Fatalf("close api key service: %v", err)
		}
	})
	created, err := apiKeyService.CreateAPIKey(editor.ID, "Native STDIO", editor.ID)
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}
	return userService, apiKeyService, editor, created, filepath.Join(apiKeyDir, "api_keys.db")
}

type mcpTestSQLiteBlocker struct {
	db *sql.DB
}

func beginExclusiveMCPTestSQLiteTransaction(t *testing.T, path string) mcpTestSQLiteBlocker {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open blocking connection: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("ROLLBACK")
		_ = db.Close()
	})
	if _, err := db.Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("begin blocking transaction: %v", err)
	}
	return mcpTestSQLiteBlocker{db: db}
}

func (b mcpTestSQLiteBlocker) rollback(t *testing.T) {
	t.Helper()
	if _, err := b.db.Exec("ROLLBACK"); err != nil {
		t.Fatalf("rollback blocking transaction: %v", err)
	}
}
