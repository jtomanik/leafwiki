package mcp

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/projectdaemon"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	sqlite "modernc.org/sqlite"
)

var _ = Describe("actor/request helpers", func() {
	It("rejects missing token info by default", func() {
		t := GinkgoT()
		routes := &Routes{}

		for _, req := range []*sdkmcp.CallToolRequest{
			nil,
			{},
			{Extra: &sdkmcp.RequestExtra{}},
		} {
			if user, err := routes.actorForRequest(req); err == nil {
				t.Fatalf("actorForRequest(%#v) returned user %#v, want missing-token error", req, user)
			} else {
				Expect(err).To(matchLocalizedErrorCode(errCodeMCPTokenInfoMissing, sharederrors.MessageIDForCode(errCodeMCPTokenInfoMissing)))
			}
		}
	})

	It("uses the private actor context header", func() {
		t := GinkgoT()
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
		Expect(err).NotTo(HaveOccurred())

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

		Expect(err).NotTo(HaveOccurred())
		if user.ID != "editor-1" || user.Username != "editor" || user.Role != coreauth.RoleEditor {
			t.Fatalf("actor = %#v, want private actor context user", user)
		}
	})

	It("uses a missing-token STDIO API key and reloads the current user", func() {
		t := GinkgoT()
		userService, apiKeyService, editor := newMCPAuthServices(t)
		editorID := newFixtureUserID(editor.ID)
		created, err := apiKeyService.CreateAPIKey(editorID, "Native STDIO", editorID)
		Expect(err).NotTo(HaveOccurred())
		routes := &Routes{
			apiKeys:     apiKeyService,
			stdioAPIKey: created.Secret,
		}

		user, err := routes.actorForRequest(nil)
		Expect(err).NotTo(HaveOccurred())
		if user.Username != "editor" || user.Role != coreauth.RoleEditor {
			t.Fatalf("user = %#v, want editor role", user)
		}

		_, err = userService.UpdateUser(editorID, "editor", "editor@example.com", "", coreauth.RoleViewer)
		Expect(err).NotTo(HaveOccurred())
		user, err = routes.actorForRequest(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(user.Role).To(Equal(coreauth.RoleViewer))

		Expect(apiKeyService.RevokeAPIKey(editorID, created.Key.ID)).To(Succeed())
		if _, err := routes.actorForRequest(nil); err == nil {
			t.Fatalf("actorForRequest after revoke succeeded, want authenticated user error")
		} else {
			Expect(err).To(matchLocalizedErrorCode(errCodeMCPAuthenticatedUserNotFound, sharederrors.MessageIDForCode(errCodeMCPAuthenticatedUserNotFound)))
		}
	})

	It("rejects viewer editor actors with a stable code", func() {
		t := GinkgoT()
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
		Expect(err).NotTo(HaveOccurred())
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
		Expect(err).To(matchLocalizedErrorCode(errCodeMCPEditorRoleRequired, sharederrors.MessageIDForCode(errCodeMCPEditorRoleRequired)))
	})

	It("preserves API key bearer verification storage errors", func() {
		t := GinkgoT()
		_, apiKeyService, _, created, apiKeyDBPath := newMCPAPIKeyAuthFixture(t)
		blocker := beginExclusiveMCPTestSQLiteTransaction(t, apiKeyDBPath)
		DeferCleanup(blocker.rollback, t)
		routes := &Routes{apiKeys: apiKeyService}

		_, err := routes.verifyBearerToken(context.Background(), created.Secret, nil)

		Expect(err).To(HaveOccurred())
		Expect(err).NotTo(MatchError(sdkauth.ErrInvalidToken), "storage failure should not be classified as invalid-token")
		Expect(err).To(MatchError(errMCPAPIKeyVerifierFailed))
	})

	It("preserves missing-token API key storage errors", func() {
		t := GinkgoT()
		_, apiKeyService, _, created, apiKeyDBPath := newMCPAPIKeyAuthFixture(t)
		blocker := beginExclusiveMCPTestSQLiteTransaction(t, apiKeyDBPath)
		DeferCleanup(blocker.rollback, t)
		routes := &Routes{
			apiKeys:     apiKeyService,
			stdioAPIKey: created.Secret,
		}

		_, err := routes.actorForRequest(nil)

		Expect(err).To(HaveOccurred())
		Expect(err).To(matchLocalizedErrorCode(errCodeMCPAuthenticatedUserLookupFailed, sharederrors.MessageIDForCode(errCodeMCPAuthenticatedUserLookupFailed)))
		Expect(err).To(matchSQLiteErrorCause())
	})
})

type mcpHelperT interface {
	Helper()
	Fatalf(format string, args ...any)
	TempDir() string
}

func matchLocalizedErrorCode(code sharederrors.ErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	GinkgoHelper()
	return testmatchers.MatchLocalizedError(code, messageID)
}

func matchSQLiteErrorCause() types.GomegaMatcher {
	GinkgoHelper()
	return Satisfy(func(err error) bool {
		var sqliteErr *sqlite.Error
		return errors.As(err, &sqliteErr)
	})
}

func newMCPAuthServices(t mcpHelperT) (*coreauth.UserService, *coreauth.APIKeyService, *coreauth.User) {
	t.Helper()
	userService, apiKeyService, editor, _, _ := newMCPAPIKeyAuthFixture(t)
	return userService, apiKeyService, editor
}

func newMCPAPIKeyAuthFixture(t mcpHelperT) (*coreauth.UserService, *coreauth.APIKeyService, *coreauth.User, *coreauth.APIKeyCreateResult, string) {
	t.Helper()

	store, err := coreauth.NewUserStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewUserStore failed: %v", err)
	}
	DeferCleanup(func() {
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
	DeferCleanup(func() {
		if err := apiKeyService.Close(); err != nil {
			t.Fatalf("close api key service: %v", err)
		}
	})
	editorID := newFixtureUserID(editor.ID)
	created, err := apiKeyService.CreateAPIKey(editorID, "Native STDIO", editorID)
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}
	return userService, apiKeyService, editor, created, filepath.Join(apiKeyDir, "api_keys.db")
}

type mcpTestSQLiteBlocker struct {
	db *sql.DB
}

func beginExclusiveMCPTestSQLiteTransaction(t mcpHelperT, path string) mcpTestSQLiteBlocker {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open blocking connection: %v", err)
	}
	DeferCleanup(func() {
		_, _ = db.Exec("ROLLBACK")
		_ = db.Close()
	})
	if _, err := db.Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("begin blocking transaction: %v", err)
	}
	return mcpTestSQLiteBlocker{db: db}
}

func (b mcpTestSQLiteBlocker) rollback(t mcpHelperT) {
	t.Helper()
	if _, err := b.db.Exec("ROLLBACK"); err != nil {
		t.Fatalf("rollback blocking transaction: %v", err)
	}
}
