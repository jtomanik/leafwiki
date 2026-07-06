package http_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("HTTP router", Label("integration"), func() {
	It("logs in with valid credentials", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `{"identifier": "admin", "password": "admin"}`
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK for valid login, got %d", rec.Code)

		Expect(readAuthenticatedSessionCredentials(rec)).To(haveAuthenticatedSessionCredentials())

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects login with invalid credentials", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `{"identifier": "admin", "password": "wrong"}`
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), "Expected 401 Unauthorized for wrong credentials, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("refreshes an authenticated session token", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		type authResponse struct {
			AccessTokenExpiresAt int64 `json:"accessTokenExpiresAt"`
		}

		// 1) Login
		loginBody := `{"identifier": "admin", "password": "admin"}`
		loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		router.ServeHTTP(loginRec, loginReq)
		Expect(loginRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on login, got %d", loginRec.Code)

		var loginPayload authResponse
		{
			err := json.Unmarshal(loginRec.Body.Bytes(), &loginPayload)
			Expect(err).NotTo(HaveOccurred(), "Expected valid login JSON response, got error: %v", err)
		}
		Expect(loginPayload.AccessTokenExpiresAt).To(BeNumerically(">", time.Now().Unix()), "Expected login response to include a future access token expiry, got %d", loginPayload.AccessTokenExpiresAt)

		credentials := readAuthenticatedSessionCredentials(loginRec)
		Expect(credentials).To(haveAuthenticatedSessionCredentials())

		// call refresh token endpoint with cookies from login
		req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh-token", nil)
		for _, c := range credentials.Cookies {
			req.AddCookie(c)
		}
		req.Header.Set("X-CSRF-Token", credentials.CSRFToken)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on refresh, got %d - %s", rec.Code, rec.Body.String())

		var refreshPayload authResponse
		{
			err := json.Unmarshal(rec.Body.Bytes(), &refreshPayload)
			Expect(err).NotTo(HaveOccurred(), "Expected valid refresh JSON response, got error: %v", err)
		}
		Expect(refreshPayload.AccessTokenExpiresAt).To(BeNumerically(">", time.Now().Unix()), "Expected refresh response to include a future access token expiry, got %d", refreshPayload.AccessTokenExpiresAt)

		refreshRes := rec.Result()
		wrapCloseWithErrorCheck(refreshRes.Body.Close)
		newCookies := refreshRes.Cookies()
		Expect(newCookies).To(haveAuthSessionCookies())

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("creates a user as an administrator", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `{"username": "john", "email": "john@example.com", "password": "secret123", "role": "editor"}`
		rec := authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(body))
		Expect(rec).To(HaveHTTPStatus(http.StatusCreated), "Expected 201 Created, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects duplicate user email or username", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Create initial user
		payload := `{"username": "john", "email": "john@example.com", "password": "secret", "role": "editor"}`
		_ = authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(payload))

		// Attempt with duplicate username
		payloadDuplicate := `{"username": "john", "email": "john2@example.com", "password": "secret", "role": "editor"}`
		rec1 := authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(payloadDuplicate))
		Expect(rec1).To(HaveHTTPStatus(http.StatusBadRequest), "Expected 400 for duplicate username, got %d", rec1.Code)

		// Attempt with duplicate email
		payloadDuplicateEmail := `{"username": "johnny", "email": "john@example.com", "password": "secret", "role": "editor"}`
		rec2 := authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(payloadDuplicateEmail))
		Expect(rec2).To(HaveHTTPStatus(http.StatusBadRequest), "Expected 400 for duplicate email, got %d", rec2.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects user creation with an invalid role", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `{"username": "sam", "email": "sam@example.com", "password": "secret1234", "role": "undefined"}`
		rec := authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(body))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected 400 Bad Request for invalid role, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("creates a viewer user", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `{"username": "vieweruser", "email": "viewer@example.com", "password": "secret1234", "role": "viewer"}`
		rec := authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(body))
		Expect(rec).To(HaveHTTPStatus(http.StatusCreated), "Expected 201 Created for viewer role, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("updates a user role to viewer", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Create user
		create := `{"username": "jane", "email": "jane@example.com", "password": "secretpassword", "role": "editor"}`
		resp := authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(create))
		var user map[string]interface{}
		_ = json.Unmarshal(resp.Body.Bytes(), &user)

		updatePayload := map[string]string{
			"username": "jane-updated",
			"email":    "jane-updated@example.com",
			"password": "newpassword",
			"role":     "viewer",
		}
		data, _ := json.Marshal(updatePayload)
		rec := authenticatedRequest(router, http.MethodPut, "/api/users/"+user["id"].(string), strings.NewReader(string(data)))
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK for user update, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("prevents a viewer from creating pages", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Create a viewer user
		createUserBody := `{"username": "vieweruser", "email": "viewer@example.com", "password": "viewerpass", "role": "viewer"}`
		authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(createUserBody))

		// Try to create a page as viewer
		pageBody := `{"title": "Test Page", "slug": "test-page"}`
		rec := authenticatedRequestAs(router, "vieweruser", "viewerpass", http.MethodPost, "/api/pages", strings.NewReader(pageBody))
		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden), "Expected 403 Forbidden for viewer creating page, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("prevents a viewer from uploading assets", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Create a viewer user
		createUserBody := `{"username": "vieweruser2", "email": "viewer2@example.com", "password": "viewerpass2", "role": "viewer"}`
		authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(createUserBody))

		// First create a page as admin to have a page ID
		pageBody := `{"title": "Test Page for Assets", "slug": "test-page-assets"}`
		pageResp := authenticatedRequest(router, http.MethodPost, "/api/pages", strings.NewReader(pageBody))
		var page map[string]interface{}
		_ = json.Unmarshal(pageResp.Body.Bytes(), &page)
		pageID := page["id"].(string)

		// Try to upload an asset as viewer
		rec := authenticatedRequestAs(router, "vieweruser2", "viewerpass2", http.MethodPost, "/api/pages/"+pageID+"/assets", strings.NewReader(""))
		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden), "Expected 403 Forbidden for viewer uploading asset, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("prevents a viewer from updating pages", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Create a viewer user
		createUserBody := `{"username": "vieweruser3", "email": "viewer3@example.com", "password": "viewerpass3", "role": "viewer"}`
		authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(createUserBody))

		// First create a page as admin
		pageBody := `{"title": "Test Page to Update", "slug": "test-page-update"}`
		pageResp := authenticatedRequest(router, http.MethodPost, "/api/pages", strings.NewReader(pageBody))
		var page map[string]interface{}
		_ = json.Unmarshal(pageResp.Body.Bytes(), &page)
		pageID := page["id"].(string)

		// Try to update the page as viewer
		updateBody := `{"title": "Updated Title", "slug": "updated-slug"}`
		rec := authenticatedRequestAs(router, "vieweruser3", "viewerpass3", http.MethodPut, "/api/pages/"+pageID, strings.NewReader(updateBody))
		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden), "Expected 403 Forbidden for viewer updating page, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("prevents a viewer from deleting pages", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Create a viewer user
		createUserBody := `{"username": "vieweruser4", "email": "viewer4@example.com", "password": "viewerpass4", "role": "viewer"}`
		authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(createUserBody))

		// First create a page as admin
		pageBody := `{"title": "Test Page to Delete", "slug": "test-page-delete"}`
		pageResp := authenticatedRequest(router, http.MethodPost, "/api/pages", strings.NewReader(pageBody))
		var page map[string]interface{}
		_ = json.Unmarshal(pageResp.Body.Bytes(), &page)
		pageID := page["id"].(string)

		// Try to delete the page as viewer
		rec := authenticatedRequestAs(router, "vieweruser4", "viewerpass4", http.MethodDelete, "/api/pages/"+pageID, nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden), "Expected 403 Forbidden for viewer deleting page, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("lists users for administrators", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodGet, "/api/users", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d", rec.Code)

		var users []map[string]interface{}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &users)
			Expect(err).NotTo(HaveOccurred(), "Failed to decode response: %v", err)
		}
		Expect(users).To(ContainElement(SatisfyAll(
			HaveKeyWithValue("username", "admin"),
			HaveKeyWithValue("role", "admin"),
		)))

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("updates a user as an administrator", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Create user
		create := `{"username": "jane", "email": "jane@example.com", "password": "secretpassword", "role": "editor"}`
		resp := authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(create))
		var user map[string]interface{}
		_ = json.Unmarshal(resp.Body.Bytes(), &user)

		updatePayload := map[string]string{
			"username": "jane-updated",
			"email":    "jane-updated@example.com",
			"password": "newpassword",
			"role":     "editor",
		}
		data, _ := json.Marshal(updatePayload)
		rec := authenticatedRequest(router, http.MethodPut, "/api/users/"+user["id"].(string), strings.NewReader(string(data)))
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK for user update, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("changes the current user's password", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		create := `{"username": "jane", "email": "jane@example.com", "password": "secretpassword", "role": "editor"}`
		resp := authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(create))
		var user map[string]interface{}
		_ = json.Unmarshal(resp.Body.Bytes(), &user)

		changePayload := `{"oldPassword":"secretpassword","newPassword":"newsecretpassword"}`
		rec := authenticatedRequestAs(router, "jane", "secretpassword", http.MethodPut, "/api/users/me/password", strings.NewReader(changePayload))
		Expect(rec).To(HaveHTTPStatus(http.StatusNoContent), "Expected 204 No Content for own password change, got %d - %s", rec.Code, rec.Body.String())

		loginWithOld := map[string]string{
			"identifier": "jane",
			"password":   "secretpassword",
		}
		loginWithOldBody, _ := json.Marshal(loginWithOld)
		oldReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginWithOldBody))
		oldReq.Header.Set("Content-Type", "application/json")
		oldRec := httptest.NewRecorder()
		router.ServeHTTP(oldRec, oldReq)
		Expect(oldRec).To(HaveHTTPStatus(http.StatusUnauthorized), "Expected 401 Unauthorized with old password, got %d - %s", oldRec.Code, oldRec.Body.String())

		loginWithNew := map[string]string{
			"identifier": "jane",
			"password":   "newsecretpassword",
		}
		loginWithNewBody, _ := json.Marshal(loginWithNew)
		newReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginWithNewBody))
		newReq.Header.Set("Content-Type", "application/json")
		newRec := httptest.NewRecorder()
		router.ServeHTTP(newRec, newReq)
		Expect(newRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK with new password, got %d - %s", newRec.Code, newRec.Body.String())

	})
})
