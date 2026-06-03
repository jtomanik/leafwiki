package mcp

import (
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	coreauth "github.com/perber/wiki/internal/core/auth"
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

func newMCPAuthServices(t *testing.T) (*coreauth.UserService, *coreauth.APIKeyService, *coreauth.User) {
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

	apiKeyStore, err := coreauth.NewAPIKeyStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewAPIKeyStore failed: %v", err)
	}
	apiKeyService := coreauth.NewAPIKeyService(apiKeyStore, userService)
	t.Cleanup(func() {
		if err := apiKeyService.Close(); err != nil {
			t.Fatalf("close api key service: %v", err)
		}
	})
	return userService, apiKeyService, editor
}
