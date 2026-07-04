package mcp

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
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
		routes := &Routes{}

		for _, req := range []*sdkmcp.CallToolRequest{
			nil,
			{},
			{Extra: &sdkmcp.RequestExtra{}},
		} {
			_, err := routes.actorForRequest(req)
			Expect(err).To(matchLocalizedErrorCode(errCodeMCPTokenInfoMissing, sharederrors.MessageIDForCode(errCodeMCPTokenInfoMissing)))
		}
	})

	It("uses the private actor context header", func() {
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
		Expect(user).To(SatisfyAll(
			HaveField("ID", Equal("editor-1")),
			HaveField("Username", Equal("editor")),
			HaveField("Role", Equal(coreauth.RoleEditor)),
		))
	})

	It("uses a missing-token STDIO API key and reloads the current user", func() {
		userService, apiKeyService, editor := newMCPAuthServices()
		editorID := coreauth.UserIDFromString(editor.ID)
		created, err := apiKeyService.CreateAPIKey(editorID, "Native STDIO", editorID)
		Expect(err).NotTo(HaveOccurred())
		routes := &Routes{
			apiKeys:     apiKeyService,
			stdioAPIKey: created.Secret,
		}

		user, err := routes.actorForRequest(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(user).To(SatisfyAll(
			HaveField("Username", Equal("editor")),
			HaveField("Role", Equal(coreauth.RoleEditor)),
		))

		_, err = userService.UpdateUser(editorID, "editor", "editor@example.com", "", coreauth.RoleViewer)
		Expect(err).NotTo(HaveOccurred())
		user, err = routes.actorForRequest(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(user.Role).To(Equal(coreauth.RoleViewer))

		Expect(apiKeyService.RevokeAPIKey(editorID, created.Key.ID)).To(Succeed())
		_, err = routes.actorForRequest(nil)
		Expect(err).To(matchLocalizedErrorCode(errCodeMCPAuthenticatedUserNotFound, sharederrors.MessageIDForCode(errCodeMCPAuthenticatedUserNotFound)))
	})

	It("rejects viewer editor actors with a stable code", func() {
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

		_, err = routes.editorActorForRequest(req)

		Expect(err).To(matchLocalizedErrorCode(errCodeMCPEditorRoleRequired, sharederrors.MessageIDForCode(errCodeMCPEditorRoleRequired)))
	})

	It("preserves API key bearer verification storage errors", func() {
		_, apiKeyService, _, created, apiKeyDBPath := newMCPAPIKeyAuthFixture()
		blocker := beginExclusiveMCPTestSQLiteTransaction(apiKeyDBPath)
		DeferCleanup(blocker.rollback)
		routes := &Routes{apiKeys: apiKeyService}

		_, err := routes.verifyBearerToken(context.Background(), created.Secret, nil)

		Expect(err).NotTo(MatchError(sdkauth.ErrInvalidToken), "storage failure should not be classified as invalid-token")
		Expect(err).To(MatchError(errMCPAPIKeyVerifierFailed))
	})

	It("preserves missing-token API key storage errors", func() {
		_, apiKeyService, _, created, apiKeyDBPath := newMCPAPIKeyAuthFixture()
		blocker := beginExclusiveMCPTestSQLiteTransaction(apiKeyDBPath)
		DeferCleanup(blocker.rollback)
		routes := &Routes{
			apiKeys:     apiKeyService,
			stdioAPIKey: created.Secret,
		}

		_, err := routes.actorForRequest(nil)

		Expect(err).To(matchLocalizedErrorCode(errCodeMCPAuthenticatedUserLookupFailed, sharederrors.MessageIDForCode(errCodeMCPAuthenticatedUserLookupFailed)))
		Expect(err).To(matchSQLiteErrorCause())
	})
})

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

func mcpTestTempDir() string {
	GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-mcp-test-*")
	Expect(err).To(Succeed())
	DeferCleanup(os.RemoveAll, dir)
	return dir
}

func newMCPAuthServices() (*coreauth.UserService, *coreauth.APIKeyService, *coreauth.User) {
	GinkgoHelper()
	userService, apiKeyService, editor, _, _ := newMCPAPIKeyAuthFixture()
	return userService, apiKeyService, editor
}

func newMCPAPIKeyAuthFixture() (*coreauth.UserService, *coreauth.APIKeyService, *coreauth.User, *coreauth.APIKeyCreateResult, string) {
	GinkgoHelper()

	store, err := coreauth.NewUserStore(mcpTestTempDir())
	Expect(err).To(Succeed())
	DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})
	userService := coreauth.NewUserService(store)
	editor, err := userService.CreateUser("editor", "editor@example.com", "password", coreauth.RoleEditor)
	Expect(err).To(Succeed())

	apiKeyDir := mcpTestTempDir()
	apiKeyStore, err := coreauth.NewAPIKeyStore(apiKeyDir)
	Expect(err).To(Succeed())
	apiKeyService := coreauth.NewAPIKeyService(apiKeyStore, userService)
	DeferCleanup(func() {
		Expect(apiKeyService.Close()).To(Succeed())
	})
	editorID := coreauth.UserIDFromString(editor.ID)
	created, err := apiKeyService.CreateAPIKey(editorID, "Native STDIO", editorID)
	Expect(err).To(Succeed())
	return userService, apiKeyService, editor, created, filepath.Join(apiKeyDir, "api_keys.db")
}

type mcpTestSQLiteBlocker struct {
	db *sql.DB
}

func beginExclusiveMCPTestSQLiteTransaction(path string) mcpTestSQLiteBlocker {
	GinkgoHelper()
	db, err := sql.Open("sqlite", path)
	Expect(err).To(Succeed())
	DeferCleanup(func() {
		_, _ = db.Exec("ROLLBACK")
		_ = db.Close()
	})
	_, err = db.Exec("BEGIN EXCLUSIVE")
	Expect(err).To(Succeed())
	return mcpTestSQLiteBlocker{db: db}
}

func (b mcpTestSQLiteBlocker) rollback() {
	GinkgoHelper()
	_, err := b.db.Exec("ROLLBACK")
	Expect(err).To(Succeed())
}
