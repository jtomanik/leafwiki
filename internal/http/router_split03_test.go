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

	"github.com/perber/wiki/internal/core/assets"
	"github.com/perber/wiki/internal/core/shared"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	wikiauth "github.com/perber/wiki/internal/wiki/auth"
)

var _ = Describe("HTTP router", Label("integration"), func() {
	It("handles disabled request logging without crashing", func() {

		logs := captureDefaultLogs()
		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
			AllowInsecure:           true,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			DisableRequestLog:       true,
		})

		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "expected 200, got %d: %s", rec.Code, rec.Body.String())
		Expect(jsonLogEntries(logs.String())).NotTo(ContainElement(matchHTTPRequestLogEntry(http.MethodGet, "/api/health", http.StatusOK)), "request log was written despite DisableRequestLog: %s", logs.String())

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("writes request logs to the default slog sink", func() {

		logs := captureDefaultLogs()
		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
			AllowInsecure:           true,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		})

		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "expected 200, got %d: %s", rec.Code, rec.Body.String())

		entries := jsonLogEntries(logs.String())
		Expect(entries).To(ContainElement(matchHTTPRequestLogEntry(http.MethodGet, "/api/health", http.StatusOK)), "request log entries = %#v", entries)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("writes recovery logs to the default slog sink", func() {

		logs := captureDefaultLogs()
		router := httpinternal.NewRouter([]httpinternal.RouteRegistrar{panicRegistrar{}}, httpinternal.FrontendConfig{}, httpinternal.RouterOptions{
			AllowInsecure:           true,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		})

		req := httptest.NewRequest(http.MethodGet, "/panic", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusInternalServerError), "expected 500, got %d: %s", rec.Code, rec.Body.String())
		Expect(jsonLogEntries(logs.String())).To(ContainElement(matchHTTPRecoveryLogEntry()), "recovery log entries = %s", logs.String())

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("returns a null current user for unauthenticated requests", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "expected 200 for unauthenticated /auth/me, got %d: %s", rec.Code, rec.Body.String())
		{

			body := strings.TrimSpace(rec.Body.String())
			Expect(body).To(Equal("null"), "expected null body for unauthenticated request, got %q", body)
		}

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("returns the authenticated current user", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodGet, "/api/auth/me", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "expected 200 for authenticated /auth/me, got %d: %s", rec.Code, rec.Body.String())

		var body map[string]any
		{
			err := json.NewDecoder(rec.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred(), "failed to decode /auth/me response: %v", err)
		}
		Expect(body).To(SatisfyAll(
			HaveKeyWithValue("username", "admin"),
			HaveKeyWithValue("role", "admin"),
		), "authenticated user response = %#v", body)

	})
})

var _ = DescribeTable("current-user responses are uncacheable", Label("integration"),
	func(authenticated bool) {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		var rec *httptest.ResponseRecorder
		if authenticated {
			rec = authenticatedRequest(router, http.MethodGet, "/api/auth/me", nil)
		} else {
			req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
			rec = httptest.NewRecorder()
			router.ServeHTTP(rec, req)
		}
		{

			cc := rec.Header().Get("Cache-Control")
			Expect(cc).To(Equal("no-store"), "expected Cache-Control: no-store, got %q", cc)
		}
		{

			p := rec.Header().Get("Pragma")
			Expect(p).To(Equal("no-cache"), "expected Pragma: no-cache, got %q", p)
		}
		{

			exp := rec.Header().Get("Expires")
			Expect(exp).NotTo(BeEmpty(), "expected Expires header to be set")
		}

	},
	Entry("unauthenticated", false),
	Entry("authenticated", true),
)

var _ = Describe("HTTP router", Label("integration"), func() {
	It("creates a page through the authenticated router", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		title := "Getting Started"
		expectedSlug := "getting-started"

		body := `{"title": "Getting Started", "slug": "getting-started"}`

		rec := authenticatedRequest(router, http.MethodPost, "/api/pages", strings.NewReader(body))
		Expect(rec).To(HaveHTTPStatus(http.StatusCreated), "Expected status 201, got %d", rec.Code)

		var resp map[string]interface{}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Invalid JSON response: %v", err)
		}
		Expect(resp).To(SatisfyAll(
			HaveKey("id"),
			HaveKeyWithValue("title", title),
			HaveKeyWithValue("slug", expectedSlug),
		), "created page response = %#v", resp)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("explains the insecure-transport requirement in the config route", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstanceWithAllowInsecure(w, false)

		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(testmatchers.HaveHTTPStructuredError(
			http.StatusBadRequest,
			wikiauth.ErrCodeAuthCookieFailed,
			sharederrors.MessageIDForCode(wikiauth.ErrCodeAuthCookieFailed),
		), "expected structured HTTPS-required config error, got status %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("explains the insecure-transport requirement during login", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstanceWithAllowInsecure(w, false)

		loginBody := `{"identifier": "admin", "password": "admin"}`
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(testmatchers.HaveHTTPStructuredError(
			http.StatusBadRequest,
			wikiauth.ErrCodeAuthCookieFailed,
			sharederrors.MessageIDForCode(wikiauth.ErrCodeAuthCookieFailed),
		), "expected structured HTTPS-required login error, got status %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects page creation when the title is missing", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `{"title": ""}`
		rec := authenticatedRequest(router, http.MethodPost, "/api/pages", strings.NewReader(body))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected 400 Bad Request for missing title, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects page creation with invalid JSON", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `this is not valid json`
		rec := authenticatedRequest(router, http.MethodPost, "/api/pages", strings.NewReader(body))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected 400 Bad Request for invalid JSON, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects page creation when the route already exists", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `{"title": "Page Exists", "slug": "page-exists"}`
		rec1 := authenticatedRequest(router, http.MethodPost, "/api/pages", strings.NewReader(body))
		Expect(rec1).To(HaveHTTPStatus(http.StatusCreated), "Expected status 201, got %d", rec1.Code)

		rec2 := authenticatedRequest(router, http.MethodPost, "/api/pages", strings.NewReader(body))
		Expect(rec2).To(HaveHTTPStatus(http.StatusBadRequest), "Expected status 400, got %d", rec2.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("returns the page tree", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodGet, "/api/tree", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected status 200, got %d", rec.Code)

		var resp map[string]interface{}
		{

			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Invalid JSON response: %v", err)
		}
		Expect(resp).To(SatisfyAll(
			HaveKeyWithValue("id", "root"),
			HaveKeyWithValue("title", "root"),
			HaveKeyWithValue("slug", "root"),
		), "tree root response = %#v", resp)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("publishes the configured maximum asset upload size", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)

		const maxAssetUploadSizeBytes shared.MaxBytes = 123456
		router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
			PublicAccess:            true,
			InjectCodeInHeader:      "",
			AllowInsecure:           true,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			HideLinkMetadataSection: false,
			MaxAssetUploadSizeBytes: maxAssetUploadSizeBytes,
		})

		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d", rec.Code)

		var resp map[string]any
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Invalid JSON response: %v", err)
		}

		Expect(resp).To(HaveKeyWithValue("maxAssetUploadSizeBytes", BeNumerically("==", maxAssetUploadSizeBytes)), "Expected maxAssetUploadSizeBytes=%d in config response, got %v", maxAssetUploadSizeBytes, resp)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("publishes whether link refactoring is enabled", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)

		router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
			PublicAccess:            true,
			InjectCodeInHeader:      "",
			AllowInsecure:           true,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			HideLinkMetadataSection: false,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			EnableLinkRefactor:      true,
		})

		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d", rec.Code)

		var resp map[string]any
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Invalid JSON response: %v", err)
		}

		Expect(resp).To(publishLinkRefactorEnabled(), "Expected enableLinkRefactor=true in config response, got %v", resp)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("publishes the markdown link root prefix", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)

		router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
			PublicAccess:            true,
			InjectCodeInHeader:      "",
			AllowInsecure:           true,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			HideLinkMetadataSection: false,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			MarkdownLinkRootPrefix:  "/docs",
		})

		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d", rec.Code)

		var resp map[string]any
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Invalid JSON response: %v", err)
		}
		Expect(resp).To(HaveKeyWithValue("markdownLinkRootPrefix", "/docs"), "Expected markdownLinkRootPrefix=/docs in %v", resp)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("reports workspace sync as disabled when no sync root is configured", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)

		router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
			PublicAccess:            true,
			InjectCodeInHeader:      "",
			AllowInsecure:           true,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			HideLinkMetadataSection: false,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		})

		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d", rec.Code)

		var resp map[string]any
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Invalid JSON response: %v", err)
		}

		Expect(resp).To(publishWorkspaceSyncDisabled(), "Expected enableWorkspaceSync=false in config response, got %v", resp)

	})
})
