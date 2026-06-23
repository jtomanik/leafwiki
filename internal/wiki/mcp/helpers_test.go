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
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
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
		} else {
			assertLocalizedErrorCode(t, err, "mcp_token_info_missing", "errors.mcp.token_info_missing")
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
	editorID := coreauth.NewUserIDUnchecked(editor.ID)
	created, err := apiKeyService.CreateAPIKey(editorID, "Native STDIO", editorID)
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

	if _, err := userService.UpdateUser(editorID, "editor", "editor@example.com", "", coreauth.RoleViewer); err != nil {
		t.Fatalf("UpdateUser role downgrade failed: %v", err)
	}
	user, err = routes.actorForRequest(nil)
	if err != nil {
		t.Fatalf("actorForRequest after role change failed: %v", err)
	}
	if user.Role != coreauth.RoleViewer {
		t.Fatalf("role after downgrade = %q, want viewer", user.Role)
	}

	if err := apiKeyService.RevokeAPIKey(editorID, created.Key.ID); err != nil {
		t.Fatalf("RevokeAPIKey failed: %v", err)
	}
	if _, err := routes.actorForRequest(nil); err == nil {
		t.Fatalf("actorForRequest after revoke succeeded, want authenticated user error")
	} else {
		assertLocalizedErrorCode(t, err, "mcp_authenticated_user_not_found", "errors.mcp.authenticated_user_not_found")
	}
}

func TestEditorActorForRequestRejectsViewerWithStableCode(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	encoded, err := projectdaemon.EncodeActorContext(projectdaemon.ActorContext{
		Version:     1,
		Issuer:      projectdaemon.ActorContextIssuerWikid,
		Subject:     "user:viewer-1",
		Username:    "viewer",
		Role:        coreauth.RoleViewer,
		WorkspaceID: "current",
		AuthMethod:  "oauth",
		IssuedAt:    now,
		ExpiresAt:   now.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("EncodeActorContext failed: %v", err)
	}
	header := http.Header{}
	header.Set(projectdaemon.ActorContextHeader, encoded)
	routes := &Routes{
		workspaceID:          "current",
		now:                  func() time.Time { return now.Add(time.Minute) },
		actorContextAllowed:  true,
		actorContextRequired: true,
	}
	req := &sdkmcp.CallToolRequest{Extra: &sdkmcp.RequestExtra{Header: header}}
	user, err := routes.editorActorForRequest(req)
	if err == nil {
		t.Fatalf("editorActorForRequest returned user %#v, want role error", user)
	}
	assertLocalizedErrorCode(t, err, "mcp_editor_role_required", "errors.mcp.editor_role_required")
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
	assertLocalizedErrorCode(t, err, "mcp_authenticated_user_lookup_failed", "errors.mcp.authenticated_user_lookup_failed")
	if !strings.Contains(err.Error(), "database") {
		t.Fatalf("actorForRequest error = %v, want authenticated-user storage failure context", err)
	}
}

func assertLocalizedErrorCode(t *testing.T, err error, code sharederrors.ErrorCode, messageID sharederrors.MessageID) {
	t.Helper()
	localized, ok := sharederrors.AsLocalizedError(err)
	if !ok {
		t.Fatalf("error = %T %v, want LocalizedError", err, err)
	}
	if localized.Code != code {
		t.Fatalf("code = %q, want %q", localized.Code, code)
	}
	if localized.MessageID != messageID {
		t.Fatalf("messageId = %q, want %q", localized.MessageID, messageID)
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
	editorID := coreauth.NewUserIDUnchecked(editor.ID)
	created, err := apiKeyService.CreateAPIKey(editorID, "Native STDIO", editorID)
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
