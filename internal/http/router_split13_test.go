package http_test

import (
	"bytes"
	"encoding/json"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	httpinternal "github.com/perber/wiki/internal/http"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	wikiassets "github.com/perber/wiki/internal/wiki/assets"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/perber/wiki/internal/wiki"
)

var _ = Describe("HTTP router", Label("integration"), func() {
	It("blocks admin middleware access when auth is disabled", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)

		// Create router with auth disabled
		router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
			PublicAccess:            false,
			InjectCodeInHeader:      "",
			AllowInsecure:           true,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			HideLinkMetadataSection: false,
			AuthDisabled:            true, // Auth is disabled
		})

		// Test POST /api/users (admin-only endpoint)
		createUserBody := `{"username": "testuser", "email": "test@example.com", "password": "password", "role": "editor"}`
		req := httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(createUserBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden), "Expected 403 Forbidden for POST /api/users when auth disabled, got %d - %s", rec.Code, rec.Body.String())

		// Test GET /api/users (admin-only endpoint)
		req = httptest.NewRequest(http.MethodGet, "/api/users", nil)
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden), "Expected 403 Forbidden for GET /api/users when auth disabled, got %d - %s", rec.Code, rec.Body.String())

		// Test DELETE /api/users/:id (admin-only endpoint)
		req = httptest.NewRequest(http.MethodDelete, "/api/users/some-user-id", nil)
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden), "Expected 403 Forbidden for DELETE /api/users/:id when auth disabled, got %d - %s", rec.Code, rec.Body.String())

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects unauthenticated requests in auth middleware", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Request ohne Token
		req := httptest.NewRequest(http.MethodPost, "/api/pages", strings.NewReader(`{"title": "Oops", "slug": "oops"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), "Expected 401 Unauthorized, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects invalid tokens in auth middleware", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		req := httptest.NewRequest(http.MethodPost, "/api/pages", strings.NewReader(`{"title": "Bad", "slug": "bad"}`))
		req.Header.Set("Authorization", "Bearer invalidtoken")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), "Expected 401 Unauthorized for invalid token, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("serves asset endpoints through the authenticated router", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Step 0: Login als Admin und Cookies holen
		loginBody := `{"identifier": "admin", "password": "admin"}`
		loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()

		router.ServeHTTP(loginRec, loginReq)
		Expect(loginRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on login, got %d - %s", loginRec.Code, loginRec.Body.String())

		credentials := readAuthenticatedSessionCredentials(loginRec)
		Expect(credentials).To(haveAuthenticatedSessionCredentials())

		addCookies := func(req *http.Request) {
			for _, c := range credentials.Cookies {
				req.AddCookie(c)
			}

			if req.Method != http.MethodGet && req.Method != http.MethodHead && req.Method != http.MethodOptions {
				req.Header.Set("X-CSRF-Token", credentials.CSRFToken)
			}
		}

		// Step 1: Create page direkt über Wiki-API
		page := createPageViaAPI(router, "Assets Page", "assets-page", nil, pageNodeKind())

		// Step 2: Upload file
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		part, err := writer.CreateFormFile("file", "testfile.txt")
		Expect(err).NotTo(HaveOccurred(), "Failed to create form file: %v", err)
		{

			_, err := part.Write([]byte("Hello, asset!"))
			Expect(err).NotTo(HaveOccurred(), "Failed to write file: %v", err)
		}
		{

			err := writer.Close()
			Expect(err).NotTo(HaveOccurred(), "Failed to close multipart writer: %v", err)
		}

		uploadReq := httptest.NewRequest(http.MethodPost, "/api/pages/"+page.ID+"/assets", body)
		uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
		addCookies(uploadReq)

		uploadRec := httptest.NewRecorder()
		router.ServeHTTP(uploadRec, uploadReq)
		Expect(uploadRec).To(HaveHTTPStatus(http.StatusCreated), "Expected 201 Created on upload, got %d - %s", uploadRec.Code, uploadRec.Body.String())

		var uploadResp map[string]string
		{
			err := json.Unmarshal(uploadRec.Body.Bytes(), &uploadResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid upload JSON: %v", err)
		}
		Expect(uploadResp).To(HaveKeyWithValue("file", Not(BeEmpty())), "Expected file field in upload response")

		// Step 3: List assets
		listReq := httptest.NewRequest(http.MethodGet, "/api/pages/"+page.ID+"/assets", nil)
		addCookies(listReq)

		listRec := httptest.NewRecorder()
		router.ServeHTTP(listRec, listReq)
		Expect(listRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on listing, got %d - %s", listRec.Code, listRec.Body.String())

		var listResp map[string][]string
		{
			err := json.Unmarshal(listRec.Body.Bytes(), &listResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid listing JSON: %v", err)
		}

		Expect(listResp).To(HaveKeyWithValue("files", ConsistOf("/assets/"+page.ID+"/testfile.txt")), "Expected file in listing, got: %v", listResp)

		// Step 4: Delete asset
		delReq := httptest.NewRequest(http.MethodDelete, "/api/pages/"+page.ID+"/assets/testfile.txt", nil)
		addCookies(delReq)

		delRec := httptest.NewRecorder()
		router.ServeHTTP(delRec, delReq)
		Expect(delRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on delete, got %d - %s", delRec.Code, delRec.Body.String())

		var deleteResp map[string]interface{}
		{
			err := json.Unmarshal(delRec.Body.Bytes(), &deleteResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid delete JSON: %v", err)
		}
		Expect(deleteResp).To(testmatchers.HaveMessageID(wikiassets.MessageIDAssetDeleteSuccess), "Expected API-scoped asset delete messageId, got: %v", deleteResp["messageId"])

		// Step 5: Verify asset is gone
		listReq2 := httptest.NewRequest(http.MethodGet, "/api/pages/"+page.ID+"/assets", nil)
		addCookies(listReq2)

		listRec2 := httptest.NewRecorder()
		router.ServeHTTP(listRec2, listReq2)
		Expect(listRec2).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on listing after delete, got %d - %s", listRec2.Code, listRec2.Body.String())

		var listResp2 map[string][]string
		{
			err := json.Unmarshal(listRec2.Body.Bytes(), &listResp2)
			Expect(err).NotTo(HaveOccurred(), "Invalid listing JSON: %v", err)
		}
		Expect(listResp2).To(HaveKeyWithValue("files", BeEmpty()), "Expected asset to be deleted, got: %v", listResp2)

	})
})

// Lets check the indexing status
var _ = Describe("HTTP router", Label("integration"), func() {
	It("returns indexing status", func() {

		// Lets call /api/search/status
		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Default Admin holen
		rec := authenticatedRequest(router, http.MethodGet, "/api/search/status", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d", rec.Code)

		var status map[string]interface{}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &status)
			Expect(err).NotTo(HaveOccurred(), "Failed to parse JSON: %v", err)
		}
		Expect(status).To(HaveKey("active"), "Expected 'active' field in response, got: %v", status)

	})
})

// uploadTestAsset is a helper function that creates a page, uploads an asset, and returns the asset URL and auth cookies.
// If needsAuth is true, it will obtain authentication cookies; otherwise it will get CSRF token only (for AuthDisabled mode).
func uploadTestAsset(router *gin.Engine, w *wiki.Wiki, content string, needsAuth bool) (assetURL string, cookies []*http.Cookie) {
	GinkgoHelper()

	pageID := ""
	if needsAuth {
		pageID = createPageViaAPI(router, "Test Page", "test-page", nil, pageNodeKind()).ID
	}

	// Prepare the file upload
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "test.txt")
	Expect(err).NotTo(HaveOccurred(), "Failed to create form file: %v", err)
	{

		_, err := part.Write([]byte(content))
		Expect(err).NotTo(HaveOccurred(), "Failed to write file: %v", err)
	}
	{

		err := writer.Close()
		Expect(err).NotTo(HaveOccurred(), "Failed to close multipart writer: %v", err)
	}

	var csrfToken string

	if needsAuth {
		// Login to get auth cookies
		loginBody := `{"identifier": "admin", "password": "admin"}`
		loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		router.ServeHTTP(loginRec, loginReq)
		Expect(loginRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on login, got %d", loginRec.Code)

		cookies = loginRec.Result().Cookies()
		csrfToken = loginRec.Header().Get("X-CSRF-Token")
		if csrfToken == "" {
			for _, c := range cookies {
				if c.Name == "leafwiki_csrf" || c.Name == "__Host-leafwiki_csrf" {
					csrfToken = c.Value
					break
				}
			}
		}
	} else {
		createBody := `{"title":"Test Page","slug":"test-page"}`
		createReq := httptest.NewRequest(http.MethodPost, "/api/pages", strings.NewReader(createBody))
		createReq.Header.Set("Content-Type", "application/json")

		// Get CSRF token only (for AuthDisabled mode)
		configReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		configRec := httptest.NewRecorder()
		router.ServeHTTP(configRec, configReq)

		cookies = configRec.Result().Cookies()
		csrfToken = configRec.Header().Get("X-CSRF-Token")
		if csrfToken == "" {
			for _, c := range cookies {
				if c.Name == "leafwiki_csrf" || c.Name == "__Host-leafwiki_csrf" {
					csrfToken = c.Value
					break
				}
			}
		}

		for _, cookie := range cookies {
			createReq.AddCookie(cookie)
		}
		createReq.Header.Set("X-CSRF-Token", csrfToken)

		createRec := httptest.NewRecorder()
		router.ServeHTTP(createRec, createReq)
		Expect(createRec).To(HaveHTTPStatus(http.StatusCreated), "Expected 201 Created on page creation, got %d - %s", createRec.Code, createRec.Body.String())

		var pageResp apiPageDTO
		{
			err := json.Unmarshal(createRec.Body.Bytes(), &pageResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid page creation JSON: %v", err)
		}

		pageID = pageResp.ID
	}

	// Upload the asset
	uploadReq := httptest.NewRequest(http.MethodPost, "/api/pages/"+pageID+"/assets", body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	for _, cookie := range cookies {
		uploadReq.AddCookie(cookie)
	}
	uploadReq.Header.Set("X-CSRF-Token", csrfToken)

	uploadRec := httptest.NewRecorder()
	router.ServeHTTP(uploadRec, uploadReq)
	Expect(uploadRec).To(HaveHTTPStatus(http.StatusCreated), "Expected 201 Created on upload, got %d - %s", uploadRec.Code, uploadRec.Body.String())

	var uploadResp struct {
		File string `json:"file"`
	}
	{
		err := json.Unmarshal(uploadRec.Body.Bytes(), &uploadResp)
		Expect(err).NotTo(HaveOccurred(), "Invalid upload JSON: %v", err)
	}
	Expect(uploadResp).To(HaveField("File", Not(BeEmpty())), "Expected file URL in upload response")

	assetURL = uploadResp.File

	return assetURL, cookies
}

type assetAccessControlScenario struct {
	publicAccess bool
	authDisabled bool
	content      string
	needsAuth    bool
	sendCookies  bool
	wantStatus   int
}

// TestAssetAccessControl tests the access control for static asset routes
var _ = DescribeTable("asset routes enforce access control", Label("integration"),
	func(tc assetAccessControlScenario) {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)

		router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
			PublicAccess:            tc.publicAccess,
			InjectCodeInHeader:      "",
			CustomStylesheet:        "",
			AllowInsecure:           true,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			HideLinkMetadataSection: false,
			AuthDisabled:            tc.authDisabled,
		})

		assetURL, cookies := uploadTestAsset(router, w, tc.content, tc.needsAuth)

		assetReq := httptest.NewRequest(http.MethodGet, assetURL, nil)
		if tc.sendCookies {
			for _, cookie := range cookies {
				assetReq.AddCookie(cookie)
			}
		}
		assetRec := httptest.NewRecorder()
		router.ServeHTTP(assetRec, assetReq)
		Expect(assetRec).To(HaveHTTPStatus(tc.wantStatus), "Expected status %d when accessing asset, got %d", tc.wantStatus, assetRec.Code)

		if tc.wantStatus == http.StatusOK {
			content := assetRec.Body.String()
			Expect(content).To(Equal(tc.content), "Expected %q, got %q", tc.content, content)

		}
	},
	Entry("rejects unauthenticated reads in private mode", assetAccessControlScenario{
		content:    "test content",
		needsAuth:  true,
		wantStatus: http.StatusUnauthorized,
	}),
	Entry("allows authenticated reads in private mode", assetAccessControlScenario{
		content:     "test content",
		needsAuth:   true,
		sendCookies: true,
		wantStatus:  http.StatusOK,
	}),
	Entry("allows unauthenticated reads in public mode", assetAccessControlScenario{
		publicAccess: true,
		content:      "test content public",
		needsAuth:    true,
		wantStatus:   http.StatusOK,
	}),
	Entry("allows unauthenticated reads when auth is disabled", assetAccessControlScenario{
		authDisabled: true,
		content:      "test content no auth",
		needsAuth:    false,
		wantStatus:   http.StatusOK,
	}),
)

var _ = Describe("HTTP router", Label("unit"), func() {
	It("build Custom Stylesheet Tag", func() {

		tag := httpinternal.BuildCustomStylesheetTag("/wiki", "/tmp/custom.css")

		expected := `<link rel="stylesheet" href="/wiki/custom.css">`
		Expect(tag).To(Equal(expected), "expected %q, got %q", expected, tag)

	})
})

var _ = Describe("HTTP router", Label("unit"), func() {
	It("omits custom stylesheet tags for an empty path", func() {

		tag := httpinternal.BuildCustomStylesheetTag("", "")
		Expect(tag).To(BeEmpty(), "expected empty tag, got %q", tag)

	})
})

var _ = Describe("HTTP router", Label("unit"), func() {
	It("injects configured markup into the document head", func() {

		html := "<html><head></head><body></body></html>"
		got := httpinternal.InjectIntoHead(html, `<link rel="stylesheet" href="/custom.css">`)
		Expect(got).To(ContainSubstring(`<link rel="stylesheet" href="/custom.css">`), "expected stylesheet link to be injected, got %q", got)

	})
})
