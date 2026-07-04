package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	coreauth "github.com/perber/wiki/internal/core/auth"
)

var _ = ginkgo.Describe("auth routes", ginkgo.Label("integration"), func() {
	ginkgo.It("handles admin user CRUD and own password changes", func() {
		fixture := newAuthRouteFixture()

		rec := performAuthHandlerRequest(fixture.routes.handleCreateUser, http.MethodPost, "/api/users", []byte(`{`), nil, fixture.admin, false)
		Expect(rec).To(matchAuthRouteError(http.StatusBadRequest, ErrCodeAuthInvalidRequest), rec.Body.String())

		rec = performAuthHandlerRequest(
			fixture.routes.handleCreateUser,
			http.MethodPost,
			"/api/users",
			jsonBody(gin.H{"username": "bad", "email": "not-email", "password": "short", "role": "invalid"}),
			nil,
			fixture.admin,
			false,
		)
		Expect(rec).To(matchAuthValidationResponse(), rec.Body.String())

		rec = performAuthHandlerRequest(
			fixture.routes.handleCreateUser,
			http.MethodPost,
			"/api/users",
			jsonBody(gin.H{"username": "new-user", "email": "new@example.test", "password": "password123", "role": coreauth.RoleViewer}),
			nil,
			fixture.admin,
			false,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusCreated))
		Expect(rec).To(matchAuthJSONBodyField("username", "new-user"))

		rec = performAuthHandlerRequest(
			fixture.routes.handleCreateUser,
			http.MethodPost,
			"/api/users",
			jsonBody(gin.H{"username": "new-user", "email": "new2@example.test", "password": "password123", "role": coreauth.RoleViewer}),
			nil,
			fixture.admin,
			false,
		)
		Expect(rec).To(matchAuthRouteError(http.StatusConflict, ErrCodeAuthUserAlreadyExists), rec.Body.String())

		rec = performAuthHandlerRequest(fixture.routes.handleGetUsers, http.MethodGet, "/api/users", nil, nil, fixture.admin, false)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(rec).To(matchAuthJSONArrayElement(HaveKeyWithValue("username", "admin")))

		errorRoutes := *fixture.routes
		errorRoutes.getUsers = NewGetUsersUseCase(setupUserServiceWithUnusableStorageDir())
		rec = performAuthHandlerRequest(errorRoutes.handleGetUsers, http.MethodGet, "/api/users", nil, nil, fixture.admin, false)
		Expect(rec).To(matchAuthRouteError(http.StatusInternalServerError, ErrCodeAuthInternalError), rec.Body.String())

		rec = performAuthHandlerRequest(
			fixture.routes.handleUpdateUser,
			http.MethodPut,
			"/api/users/"+fixture.editor.ID,
			jsonBody(gin.H{"username": "editor-updated", "email": "editor-updated@example.test", "role": coreauth.RoleAdmin}),
			gin.Params{{Key: "id", Value: fixture.editor.ID}},
			nil,
			false,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden))

		rec = performAuthHandlerRequest(
			fixture.routes.handleUpdateUser,
			http.MethodPut,
			"/api/users/"+fixture.editor.ID,
			[]byte(`{`),
			gin.Params{{Key: "id", Value: fixture.editor.ID}},
			fixture.admin,
			false,
		)
		Expect(rec).To(matchAuthRouteError(http.StatusBadRequest, ErrCodeAuthInvalidRequest), rec.Body.String())

		rec = performAuthHandlerRequest(
			fixture.routes.handleUpdateUser,
			http.MethodPut,
			"/api/users/"+fixture.editor.ID,
			jsonBody(gin.H{"username": "editor-updated", "email": "editor-updated@example.test", "role": coreauth.RoleAdmin}),
			gin.Params{{Key: "id", Value: fixture.editor.ID}},
			fixture.admin,
			false,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(rec).To(matchAuthJSONBodyField("role", string(coreauth.RoleAdmin)))

		rec = performAuthHandlerRequest(
			fixture.routes.handleUpdateUser,
			http.MethodPut,
			"/api/users/missing-user",
			jsonBody(gin.H{"username": "missing", "email": "missing@example.test"}),
			gin.Params{{Key: "id", Value: "missing-user"}},
			fixture.admin,
			false,
		)
		Expect(rec).To(matchAuthRouteError(http.StatusNotFound, ErrCodeAuthUserNotFound), rec.Body.String())

		deleteTarget, err := fixture.userService.CreateUser("delete-me", "delete@example.test", "password123", coreauth.RoleViewer)
		Expect(err).NotTo(HaveOccurred())
		rec = performAuthHandlerRequest(
			fixture.routes.handleDeleteUser,
			http.MethodDelete,
			"/api/users/"+deleteTarget.ID,
			nil,
			gin.Params{{Key: "id", Value: deleteTarget.ID}},
			fixture.admin,
			false,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusNoContent))

		rec = performAuthHandlerRequest(
			fixture.routes.handleDeleteUser,
			http.MethodDelete,
			"/api/users/"+fixture.admin.ID,
			nil,
			gin.Params{{Key: "id", Value: fixture.admin.ID}},
			fixture.admin,
			false,
		)
		Expect(rec).To(matchAuthRouteError(http.StatusBadRequest, ErrCodeAuthAdminCannotDelete), rec.Body.String())

		rec = performAuthHandlerRequest(fixture.routes.handleChangeOwnPassword, http.MethodPut, "/api/users/me/password", nil, nil, nil, false)
		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden))

		rec = performAuthHandlerRequest(fixture.routes.handleChangeOwnPassword, http.MethodPut, "/api/users/me/password", []byte(`{`), nil, fixture.editor, false)
		Expect(rec).To(matchAuthRouteError(http.StatusBadRequest, ErrCodeAuthInvalidRequest), rec.Body.String())

		rec = performAuthHandlerRequest(
			fixture.routes.handleChangeOwnPassword,
			http.MethodPut,
			"/api/users/me/password",
			jsonBody(gin.H{"oldPassword": "wrong-password", "newPassword": "short"}),
			nil,
			fixture.editor,
			false,
		)
		Expect(rec).To(matchAuthValidationResponse(), rec.Body.String())

		rec = performAuthHandlerRequest(
			fixture.routes.handleChangeOwnPassword,
			http.MethodPut,
			"/api/users/me/password",
			jsonBody(gin.H{"oldPassword": "password123", "newPassword": "new-password"}),
			nil,
			fixture.editor,
			false,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusNoContent))
	})

	ginkgo.It("handles admin and self API key flows", func() {
		fixture := newAuthRouteFixture()

		rec := performAuthHandlerRequest(
			fixture.routes.handleListUserAPIKeys,
			http.MethodGet,
			"/api/users/"+fixture.editor.ID+"/mcp-api-keys",
			nil,
			gin.Params{{Key: "id", Value: fixture.editor.ID}},
			fixture.admin,
			false,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(rec).To(HaveHTTPBody("[]"))

		rec = performAuthHandlerRequest(
			fixture.routes.handleCreateUserAPIKey,
			http.MethodPost,
			"/api/users/"+fixture.editor.ID+"/mcp-api-keys",
			jsonBody(gin.H{"name": "admin-created"}),
			gin.Params{{Key: "id", Value: fixture.editor.ID}},
			nil,
			false,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden))

		rec = performAuthHandlerRequest(
			fixture.routes.handleCreateUserAPIKey,
			http.MethodPost,
			"/api/users/"+fixture.editor.ID+"/mcp-api-keys",
			jsonBody(gin.H{"name": "admin-created"}),
			gin.Params{{Key: "id", Value: fixture.editor.ID}},
			fixture.admin,
			false,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusCreated))

		rec = performAuthHandlerRequest(
			fixture.routes.handleCreateUserAPIKey,
			http.MethodPost,
			"/api/users/"+fixture.editor.ID+"/mcp-api-keys",
			[]byte(`{`),
			gin.Params{{Key: "id", Value: fixture.editor.ID}},
			fixture.admin,
			false,
		)
		Expect(rec).To(matchAuthRouteError(http.StatusBadRequest, ErrCodeAuthInvalidRequest), rec.Body.String())

		created, err := fixture.apiKeys.CreateAPIKey(coreauth.UserIDFromString(fixture.editor.ID), "to-revoke", coreauth.UserIDFromString(fixture.admin.ID))
		Expect(err).NotTo(HaveOccurred())
		rec = performAuthHandlerRequest(
			fixture.routes.handleRevokeUserAPIKey,
			http.MethodDelete,
			authUserAPIKeyPath(fixture.editor.ID, created.Key.ID),
			nil,
			authUserAPIKeyParams(fixture.editor.ID, created.Key.ID),
			fixture.admin,
			false,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusNoContent))

		rec = performAuthHandlerRequest(fixture.routes.handleListOwnAPIKeys, http.MethodGet, "/api/users/me/mcp-api-keys", nil, nil, nil, false)
		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden))

		rec = performAuthHandlerRequest(fixture.routes.handleListOwnAPIKeys, http.MethodGet, "/api/users/me/mcp-api-keys", nil, nil, fixture.editor, false)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))

		errorRoutes := *fixture.routes
		errorRoutes.listAPIKeys = NewListAPIKeysUseCase(nil)
		rec = performAuthHandlerRequest(
			errorRoutes.handleListUserAPIKeys,
			http.MethodGet,
			"/api/users/"+fixture.editor.ID+"/mcp-api-keys",
			nil,
			gin.Params{{Key: "id", Value: fixture.editor.ID}},
			fixture.admin,
			false,
		)
		Expect(rec).To(matchAuthRouteError(http.StatusNotFound, ErrCodeAuthUserNotFound), rec.Body.String())

		rec = performAuthHandlerRequest(errorRoutes.handleListOwnAPIKeys, http.MethodGet, "/api/users/me/mcp-api-keys", nil, nil, fixture.editor, false)
		Expect(rec).To(matchAuthRouteError(http.StatusNotFound, ErrCodeAuthUserNotFound), rec.Body.String())

		rec = performAuthHandlerRequest(fixture.routes.handleCreateOwnAPIKey, http.MethodPost, "/api/users/me/mcp-api-keys", jsonBody(gin.H{"name": "self"}), nil, nil, false)
		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden))

		rec = performAuthHandlerRequest(fixture.routes.handleCreateOwnAPIKey, http.MethodPost, "/api/users/me/mcp-api-keys", jsonBody(gin.H{"name": "self"}), nil, fixture.editor, true)
		Expect(rec).To(matchAuthRouteError(http.StatusForbidden, ErrCodeAuthForbidden), rec.Body.String())

		rec = performAuthHandlerRequest(fixture.routes.handleCreateOwnAPIKey, http.MethodPost, "/api/users/me/mcp-api-keys", []byte(`{`), nil, fixture.editor, false)
		Expect(rec).To(matchAuthRouteError(http.StatusBadRequest, ErrCodeAuthInvalidRequest), rec.Body.String())

		rec = performAuthHandlerRequest(fixture.routes.handleCreateOwnAPIKey, http.MethodPost, "/api/users/me/mcp-api-keys", jsonBody(gin.H{"name": "self"}), nil, fixture.editor, false)
		Expect(rec).To(matchAuthValidationResponse(), rec.Body.String())

		rec = performAuthHandlerRequest(
			fixture.routes.handleCreateOwnAPIKey,
			http.MethodPost,
			"/api/users/me/mcp-api-keys",
			jsonBody(gin.H{"name": "self", "currentPassword": "password123"}),
			nil,
			fixture.editor,
			false,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusCreated))
		Expect(rec).To(HaveHTTPHeaderWithValue("Cache-Control", "no-store"))

		ownKey, err := fixture.apiKeys.CreateAPIKey(coreauth.UserIDFromString(fixture.editor.ID), "own-revoke", coreauth.UserIDFromString(fixture.editor.ID))
		Expect(err).NotTo(HaveOccurred())
		rec = performAuthHandlerRequest(fixture.routes.handleRevokeOwnAPIKey, http.MethodDelete, authOwnAPIKeyPath(ownKey.Key.ID), nil, authOwnAPIKeyParams(ownKey.Key.ID), nil, false)
		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden))

		rec = performAuthHandlerRequest(fixture.routes.handleRevokeOwnAPIKey, http.MethodDelete, authOwnAPIKeyPath(ownKey.Key.ID), nil, authOwnAPIKeyParams(ownKey.Key.ID), fixture.editor, false)
		Expect(rec).To(HaveHTTPStatus(http.StatusNoContent))

		errorRoutes = *fixture.routes
		errorRoutes.createAPIKey = NewCreateAPIKeyUseCase(nil, fixture.userService)
		rec = performAuthHandlerRequest(
			errorRoutes.handleCreateUserAPIKey,
			http.MethodPost,
			"/api/users/"+fixture.editor.ID+"/mcp-api-keys",
			jsonBody(gin.H{"name": "fails"}),
			gin.Params{{Key: "id", Value: fixture.editor.ID}},
			fixture.admin,
			false,
		)
		Expect(rec).To(matchAuthRouteError(http.StatusNotFound, ErrCodeAuthUserNotFound), rec.Body.String())

		errorRoutes = *fixture.routes
		errorRoutes.revokeAPIKey = NewRevokeAPIKeyUseCase(nil)
		rec = performAuthHandlerRequest(
			errorRoutes.handleRevokeUserAPIKey,
			http.MethodDelete,
			"/api/users/"+fixture.editor.ID+"/mcp-api-keys/missing-key",
			nil,
			gin.Params{{Key: "id", Value: fixture.editor.ID}, {Key: "keyId", Value: "missing-key"}},
			fixture.admin,
			false,
		)
		Expect(rec).To(matchAuthRouteError(http.StatusNotFound, ErrCodeAuthUserNotFound), rec.Body.String())

		rec = performAuthHandlerRequest(errorRoutes.handleRevokeOwnAPIKey, http.MethodDelete, "/api/users/me/mcp-api-keys/missing-key", nil, gin.Params{{Key: "keyId", Value: "missing-key"}}, fixture.editor, false)
		Expect(rec).To(matchAuthRouteError(http.StatusNotFound, ErrCodeAuthUserNotFound), rec.Body.String())
	})

	ginkgo.It("returns structured validation errors for missing and invalid auth use-case fields", func() {
		fixture := newAuthRouteFixture()

		_, err := fixture.routes.createUser.Execute(context.Background(), CreateUserInput{
			Username: "requires-fields",
			Role:     coreauth.RoleViewer,
		})
		Expect(err).To(matchAuthUseCaseValidationError(
			expectAuthFieldError(authValidationFieldEmail, FieldCodeAuthEmailRequired, MessageIDAuthEmailRequired),
			expectAuthFieldError(authValidationFieldPassword, FieldCodeAuthPasswordRequired, MessageIDAuthPasswordRequired),
		))

		_, err = fixture.routes.updateUser.Execute(context.Background(), UpdateUserInput{
			ID:               coreauth.UserIDFromString(fixture.editor.ID),
			Email:            "not-email",
			Role:             coreauth.RoleViewer,
			RequesterIsAdmin: true,
		})
		Expect(err).To(matchAuthUseCaseValidationError(
			expectAuthFieldError(authValidationFieldUsername, FieldCodeAuthUsernameRequired, MessageIDAuthUsernameRequired),
			expectAuthFieldError(authValidationFieldEmail, FieldCodeAuthEmailInvalid, MessageIDAuthEmailInvalid),
		))

		_, err = fixture.routes.updateUser.Execute(context.Background(), UpdateUserInput{
			ID:               coreauth.UserIDFromString(fixture.editor.ID),
			Username:         "editor-missing-email",
			RequesterIsAdmin: true,
		})
		Expect(err).To(matchAuthUseCaseValidationError(
			expectAuthFieldError(authValidationFieldEmail, FieldCodeAuthEmailRequired, MessageIDAuthEmailRequired),
		))

		err = fixture.routes.changeOwnPassword.Execute(context.Background(), ChangeOwnPasswordInput{
			UserID:      coreauth.UserIDFromString(fixture.editor.ID),
			OldPassword: "password123",
		})
		Expect(err).To(matchAuthUseCaseValidationError(
			expectAuthFieldError(authValidationFieldNewPassword, FieldCodeAuthNewPasswordRequired, MessageIDAuthNewPasswordRequired),
		))

		err = fixture.routes.changeOwnPassword.Execute(context.Background(), ChangeOwnPasswordInput{
			UserID:      coreauth.UserIDFromString(fixture.editor.ID),
			OldPassword: "password123",
			NewPassword: "short",
		})
		Expect(err).To(matchAuthUseCaseValidationError(
			expectAuthFieldError(authValidationFieldNewPassword, FieldCodeAuthNewPasswordTooShort, MessageIDAuthNewPasswordTooShort),
		))

		_, err = fixture.routes.getUserByID.Execute(context.Background(), GetUserByIDInput{ID: newFixtureUserID("missing-user")})
		Expect(err).To(MatchError(coreauth.ErrUserNotFound))

		_, err = fixture.routes.createAPIKey.Execute(context.Background(), CreateAPIKeyInput{
			UserID:          coreauth.UserIDFromString(fixture.editor.ID),
			Name:            strings.Repeat("x", maxAPIKeyNameLength+1),
			CreatedByUserID: coreauth.UserIDFromString(fixture.admin.ID),
		})
		Expect(err).To(matchAuthUseCaseValidationError(
			expectAuthFieldError(authValidationFieldName, FieldCodeAuthAPIKeyNameTooLong, MessageIDAuthAPIKeyNameTooLong),
		))
	})
})
