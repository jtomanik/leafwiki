package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	wikiauth "github.com/perber/wiki/internal/wiki/auth"

	"github.com/perber/wiki/internal/core/assets"
)

var _ = Describe("HTTP router", Label("integration"), func() {
	It("lets administrators create list and revoke user mcp api keys", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		createUser := `{"username": "keyuser", "email": "keyuser@example.com", "password": "secretpassword", "role": "editor"}`
		userRec := authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(createUser))
		Expect(userRec).To(HaveHTTPStatus(http.StatusCreated), "create user = %d: %s", userRec.Code, userRec.Body.String())

		var user map[string]any
		{
			err := json.Unmarshal(userRec.Body.Bytes(), &user)
			Expect(err).NotTo(HaveOccurred(), "decode user: %v", err)
		}

		userID := user["id"].(string)

		createKey := authenticatedRequest(router, http.MethodPost, "/api/users/"+userID+"/mcp-api-keys", strings.NewReader(`{"name":"CLI"}`))
		Expect(createKey).To(HaveHTTPStatus(http.StatusCreated), "create api key = %d: %s", createKey.Code, createKey.Body.String())

		Expect(createKey).To(haveNoStoreHeaders())
		var created struct {
			Secret string         `json:"secret"`
			Key    map[string]any `json:"key"`
		}
		{
			err := json.Unmarshal(createKey.Body.Bytes(), &created)
			Expect(err).NotTo(HaveOccurred(), "decode created key: %v", err)
		}

		Expect(created).To(SatisfyAll(
			HaveField("Secret", HavePrefix("lwk_")),
			HaveField("Key", SatisfyAll(
				HaveKeyWithValue("id", Not(BeEmpty())),
				HaveKeyWithValue("userId", userID),
				HaveKeyWithValue("name", "CLI"),
				haveNullAPIKeyLifecycleMetadata(),
			)),
		), "created key response = %#v", created)

		key := created.Key
		keyID := key["id"].(string)

		listKeys := authenticatedRequest(router, http.MethodGet, "/api/users/"+userID+"/mcp-api-keys", nil)
		Expect(listKeys).To(HaveHTTPStatus(http.StatusOK), "list api keys = %d: %s", listKeys.Code, listKeys.Body.String())

		var listed []map[string]any
		{
			err := json.Unmarshal(listKeys.Body.Bytes(), &listed)
			Expect(err).NotTo(HaveOccurred(), "decode listed keys: %v", err)
		}

		Expect(listed).To(ConsistOf(SatisfyAll(
			HaveKeyWithValue("id", keyID),
			Not(HaveKey("secret")),
			Not(HaveKey("secretHash")),
			haveNullAPIKeyLifecycleMetadata(),
		)), "listed keys = %#v, want created key without secrets", listed)

		revoke := authenticatedRequest(router, http.MethodDelete, "/api/users/"+userID+"/mcp-api-keys/"+keyID, nil)
		Expect(revoke).To(HaveHTTPStatus(http.StatusNoContent), "revoke api key = %d: %s", revoke.Code, revoke.Body.String())

		listAfterRevoke := authenticatedRequest(router, http.MethodGet, "/api/users/"+userID+"/mcp-api-keys", nil)
		var after []map[string]any
		{
			err := json.Unmarshal(listAfterRevoke.Body.Bytes(), &after)
			Expect(err).NotTo(HaveOccurred(), "decode keys after revoke: %v", err)
		}
		Expect(after).To(HaveLen(0), "revoked key still listed: %#v", after)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("enforces permissions and validation for mcp api key routes", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		createEditor := `{"username": "editor-key-user", "email": "editor-key-user@example.com", "password": "secretpassword", "role": "editor"}`
		editorRec := authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(createEditor))
		var editor map[string]any
		{
			err := json.Unmarshal(editorRec.Body.Bytes(), &editor)
			Expect(err).NotTo(HaveOccurred(), "decode editor: %v", err)
		}

		editorID := editor["id"].(string)

		invalidName := authenticatedRequest(router, http.MethodPost, "/api/users/"+editorID+"/mcp-api-keys", strings.NewReader(`{"name":"   "}`))
		Expect(invalidName).To(HaveHTTPStatus(http.StatusBadRequest), "invalid name status = %d: %s", invalidName.Code, invalidName.Body.String())

		var validation struct {
			Error  string `json:"error"`
			Fields []struct {
				Field     string `json:"field"`
				Code      string `json:"code"`
				MessageID string `json:"messageId"`
				Message   string `json:"message"`
			} `json:"fields"`
		}
		{
			err := json.Unmarshal(invalidName.Body.Bytes(), &validation)
			Expect(err).NotTo(HaveOccurred(), "decode validation: %v", err)
		}

		Expect(validation.Fields).To(testmatchers.ContainFieldError(
			testmatchers.ValidationFieldName("name"),
			wikiauth.FieldCodeAuthAPIKeyNameRequired,
			wikiauth.MessageIDAuthAPIKeyNameRequired,
		), "validation body = %#v", validation)

		missingUser := authenticatedRequest(router, http.MethodPost, "/api/users/missing/mcp-api-keys", strings.NewReader(`{"name":"CLI"}`))
		Expect(missingUser).To(HaveHTTPStatus(http.StatusNotFound), "missing user create = %d: %s", missingUser.Code, missingUser.Body.String())

		emptyList := authenticatedRequest(router, http.MethodGet, "/api/users/"+editorID+"/mcp-api-keys", nil)
		Expect(emptyList).To(HaveHTTPStatus(http.StatusOK), "empty key list = %d: %s", emptyList.Code, emptyList.Body.String())
		Expect(strings.TrimSpace(emptyList.Body.String())).To(Equal("[]"), "empty key list body = %q, want []", emptyList.Body.String())

		asEditor := authenticatedRequestAs(router, "editor-key-user", "secretpassword", http.MethodPost, "/api/users/missing/mcp-api-keys", strings.NewReader(`{"name":"CLI"}`))
		Expect(asEditor).To(HaveHTTPStatus(http.StatusForbidden), "non-admin administer other user = %d: %s", asEditor.Code, asEditor.Body.String())

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("requires the current password and mcp scope for self-service api keys", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		createEditor := `{"username": "self-key-user", "email": "self-key-user@example.com", "password": "secretpassword", "role": "editor"}`
		authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(createEditor))

		wrongPassword := authenticatedRequestAs(router, "self-key-user", "secretpassword", http.MethodPost, "/api/users/me/mcp-api-keys", strings.NewReader(`{"name":"Self","currentPassword":"wrong"}`))
		Expect(wrongPassword).To(HaveHTTPStatus(http.StatusBadRequest), "wrong current password = %d: %s", wrongPassword.Code, wrongPassword.Body.String())

		createKey := authenticatedRequestAs(router, "self-key-user", "secretpassword", http.MethodPost, "/api/users/me/mcp-api-keys", strings.NewReader(`{"name":"Self","currentPassword":"secretpassword"}`))
		Expect(createKey).To(HaveHTTPStatus(http.StatusCreated), "self create api key = %d: %s", createKey.Code, createKey.Body.String())

		Expect(createKey).To(haveNoStoreHeaders())
		var created struct {
			Secret string         `json:"secret"`
			Key    map[string]any `json:"key"`
		}
		{
			err := json.Unmarshal(createKey.Body.Bytes(), &created)
			Expect(err).NotTo(HaveOccurred(), "decode self-created key: %v", err)
		}

		Expect(created).To(SatisfyAll(
			HaveField("Secret", Not(BeEmpty())),
			HaveField("Key", SatisfyAll(
				HaveKeyWithValue("id", Not(BeEmpty())),
				haveNullAPIKeyLifecycleMetadata(),
			)),
		), "self-created key response = %#v", created)

		secret := created.Secret
		key := created.Key
		keyID := key["id"].(string)

		list := authenticatedRequestAs(router, "self-key-user", "secretpassword", http.MethodGet, "/api/users/me/mcp-api-keys", nil)
		Expect(list).To(HaveHTTPStatus(http.StatusOK), "self list api keys = %d: %s", list.Code, list.Body.String())

		var listed []map[string]any
		{
			err := json.Unmarshal(list.Body.Bytes(), &listed)
			Expect(err).NotTo(HaveOccurred(), "decode self list: %v", err)
		}

		Expect(listed).To(ConsistOf(HaveKeyWithValue("id", keyID)), "self list = %#v, want own key", listed)

		req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
		req.Header.Set("Authorization", "Bearer "+secret)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), "api key authenticated protected normal HTTP API = %d, want 401", rec.Code)

		revoke := authenticatedRequestAs(router, "self-key-user", "secretpassword", http.MethodDelete, "/api/users/me/mcp-api-keys/"+keyID, nil)
		Expect(revoke).To(HaveHTTPStatus(http.StatusNoContent), "self revoke api key = %d: %s", revoke.Code, revoke.Body.String())

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rate limits self-service mcp api key creation", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		createEditor := `{"username": "rate-key-user", "email": "rate-key-user@example.com", "password": "secretpassword", "role": "editor"}`
		authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(createEditor))

		for i := 0; i < 10; i++ {
			rec := authenticatedRequestAs(router, "rate-key-user", "secretpassword", http.MethodPost, "/api/users/me/mcp-api-keys", strings.NewReader(`{"name":"Self","currentPassword":"wrong"}`))
			Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "wrong current password attempt %d = %d: %s", i+1, rec.Code, rec.Body.String())

		}
		limited := authenticatedRequestAs(router, "rate-key-user", "secretpassword", http.MethodPost, "/api/users/me/mcp-api-keys", strings.NewReader(`{"name":"Self","currentPassword":"wrong"}`))
		Expect(limited).To(HaveHTTPStatus(http.StatusTooManyRequests), "rate-limited self create = %d: %s", limited.Code, limited.Body.String())

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("disables self-service mcp api key creation for remote users", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)

		trustedProxies, err := authmw.ParseTrustedProxies("192.0.2.1")
		Expect(err).NotTo(HaveOccurred(), "ParseTrustedProxies failed: %v", err)

		router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
			PublicAccess:            false,
			AllowInsecure:           true,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			HideLinkMetadataSection: false,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			HTTPRemoteUser: httpinternal.HTTPRemoteUserConfig{
				Enabled:        true,
				HeaderName:     "Remote-User",
				TrustedProxies: trustedProxies,
				UserService:    w.UserService(),
			},
		})

		configReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		configReq.RemoteAddr = "192.0.2.1:1234"
		configRec := httptest.NewRecorder()
		router.ServeHTTP(configRec, configReq)
		Expect(configRec).To(HaveHTTPStatus(http.StatusOK), "remote-user config = %d: %s", configRec.Code, configRec.Body.String())

		csrfToken := configRec.Header().Get("X-CSRF-Token")
		cookies := configRec.Result().Cookies()
		if csrfToken == "" {
			for _, cookie := range cookies {
				if cookie.Name == "leafwiki_csrf" || cookie.Name == "__Host-leafwiki_csrf" {
					csrfToken = cookie.Value
					break
				}
			}
		}
		Expect(csrfToken).NotTo(BeEmpty(), "remote-user config did not issue CSRF token")

		listReq := httptest.NewRequest(http.MethodGet, "/api/users/me/mcp-api-keys", nil)
		listReq.RemoteAddr = "192.0.2.1:1234"
		listReq.Header.Set("Remote-User", "admin")
		for _, cookie := range cookies {
			listReq.AddCookie(cookie)
		}
		listRec := httptest.NewRecorder()
		router.ServeHTTP(listRec, listReq)
		Expect(listRec).To(HaveHTTPStatus(http.StatusOK), "remote-user self list = %d: %s", listRec.Code, listRec.Body.String())

		createReq := httptest.NewRequest(http.MethodPost, "/api/users/me/mcp-api-keys", strings.NewReader(`{"name":"Proxy"}`))
		createReq.RemoteAddr = "192.0.2.1:1234"
		createReq.Header.Set("Content-Type", "application/json")
		createReq.Header.Set("X-CSRF-Token", csrfToken)
		createReq.Header.Set("Remote-User", "admin")
		for _, cookie := range cookies {
			createReq.AddCookie(cookie)
		}
		createRec := httptest.NewRecorder()
		router.ServeHTTP(createRec, createReq)
		Expect(createRec).To(HaveHTTPStatus(http.StatusForbidden), "remote-user self create = %d: %s", createRec.Code, createRec.Body.String())

	})
})

type authDisabledSelfAPIKeyRoute struct {
	method string
	path   string
	body   string
}

var _ = DescribeTable("self-service mcp api key routes are blocked when auth is disabled", Label("integration"),
	func(tc authDisabledSelfAPIKeyRoute) {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)

		router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
			PublicAccess:            false,
			InjectCodeInHeader:      "",
			AllowInsecure:           true,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			HideLinkMetadataSection: false,
			AuthDisabled:            true,
		})

		configReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		configRec := httptest.NewRecorder()
		router.ServeHTTP(configRec, configReq)
		Expect(configRec).To(HaveHTTPStatus(http.StatusOK), "auth-disabled config = %d: %s", configRec.Code, configRec.Body.String())

		csrfToken := configRec.Header().Get("X-CSRF-Token")
		cookies := configRec.Result().Cookies()
		if csrfToken == "" {
			for _, cookie := range cookies {
				if cookie.Name == "leafwiki_csrf" || cookie.Name == "__Host-leafwiki_csrf" {
					csrfToken = cookie.Value
					break
				}
			}
		}
		Expect(csrfToken).NotTo(BeEmpty(), "auth-disabled config did not issue CSRF token")

		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if tc.method != http.MethodGet {
			req.Header.Set("X-CSRF-Token", csrfToken)
			for _, cookie := range cookies {
				req.AddCookie(cookie)
			}
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden), "%s auth-disabled self API-key route = %d: %s", tc.method, rec.Code, rec.Body.String())

		var authErr wikiauth.AuthErrorResponse
		{
			err := json.Unmarshal(rec.Body.Bytes(), &authErr)
			Expect(err).NotTo(HaveOccurred(), "decode auth-disabled error: %v", err)
		}
		Expect(authErr.Error).To(testmatchers.HaveErrorCode(wikiauth.ErrCodeAuthDisabled), "body=%s", rec.Body.String())

	},
	Entry("list", authDisabledSelfAPIKeyRoute{method: http.MethodGet, path: "/api/users/me/mcp-api-keys"}),
	Entry("create", authDisabledSelfAPIKeyRoute{method: http.MethodPost, path: "/api/users/me/mcp-api-keys", body: `{"name":"CLI","currentPassword":"admin"}`}),
	Entry("revoke", authDisabledSelfAPIKeyRoute{method: http.MethodDelete, path: "/api/users/me/mcp-api-keys/some-key"}),
)

var _ = Describe("HTTP router", Label("integration"), func() {
	It("deletes a user as an administrator", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Create user
		create := `{"username": "todelete", "email": "delete@example.com", "password": "secrepassword", "role": "editor"}`
		resp := authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(create))
		var user map[string]interface{}
		_ = json.Unmarshal(resp.Body.Bytes(), &user)

		// Delete user
		rec := authenticatedRequest(router, http.MethodDelete, "/api/users/"+user["id"].(string), nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusNoContent), "Expected 204 OK on delete, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("prevents deleting the administrator account", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Get default admin
		rec := authenticatedRequest(router, http.MethodGet, "/api/users", nil)
		var users []struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &users)
			Expect(err).NotTo(HaveOccurred(), "decode users: %v", err)
		}
		Expect(users).To(ContainElement(SatisfyAll(
			HaveField("ID", Not(BeEmpty())),
			HaveField("Role", "admin"),
		)), "No admin user found")

		var adminID string
		for _, u := range users {
			if u.Role == "admin" {
				adminID = u.ID
			}
		}

		// Attempt to delete the admin
		recDel := authenticatedRequest(router, http.MethodDelete, "/api/users/"+adminID, nil)
		Expect(recDel).To(HaveHTTPStatus(http.StatusBadRequest), "Expected 400 when deleting admin user, got %d", recDel.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("allows administrators through the admin middleware", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Default Admin create user should succeed
		body := `{"username": "mod", "email": "mod@example.com", "password": "secretpassword", "role": "editor"}`
		rec := authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(body))
		Expect(rec).To(HaveHTTPStatus(http.StatusCreated), "Expected 201 Created by admin, got %d", rec.Code)

	})
})
