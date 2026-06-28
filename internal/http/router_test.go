package http_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/assets"
	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/wiki"
	wikiauth "github.com/perber/wiki/internal/wiki/auth"
	"github.com/perber/wiki/internal/workspacesync"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Explicit README.md page link stays a page when index.md exists

type panicRegistrar struct{}

func (panicRegistrar) RegisterRoutes(ctx httpinternal.RouterContext) {
	ctx.Base.GET("/panic", func(c *gin.Context) {
		panic("panic route")
	})
}

func pageNodeKind() *tree.NodeKind {
	kind := tree.NodeKindPage
	return &kind
}

type routerTestTB interface {
	Helper()
	TempDir() string
	Cleanup(func())
	Fatal(args ...any)
	Fatalf(format string, args ...any)
	Error(args ...any)
	Errorf(format string, args ...any)
	Logf(format string, args ...any)
}

func wrapCloseWithErrorCheck(closer func() error, t routerTestTB) {
	t.Helper()
	if err := closer(); err != nil {
		t.Fatalf("failed to close resource: %v", err)
	}
}

func fixturePathForHTTPTests(t routerTestTB, rel string, candidates ...string) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	for _, candidate := range candidates {
		abs := filepath.Join(wd, candidate, rel)
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs
		}
	}

	t.Fatalf("fixture path not found for %q from working directory %q", rel, wd)
	return ""
}

func createWikiTestInstance(t routerTestTB) *wiki.Wiki {
	return createWikiTestInstanceWithRevisionFlag(t, true)
}

func createWikiTestInstanceWithRevisionFlag(t routerTestTB, _ bool) *wiki.Wiki {
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		StorageDir:          t.TempDir(),
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("Failed to create wiki instance: %v", err)
	}
	return w
}

func createWikiTestInstanceWithWorkspace(t routerTestTB, workspace wiki.Workspace) *wiki.Wiki {
	t.Helper()
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		Workspace:           workspace,
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("Failed to create wiki instance: %v", err)
	}
	return w
}

func createRouterTestInstance(w *wiki.Wiki, t routerTestTB) *gin.Engine {
	return createRouterTestInstanceWithMaxAssetUploadSize(w, t, assets.DefaultMaxUploadSizeBytes)
}

func createRouterTestInstanceWithRevision(w *wiki.Wiki, t routerTestTB) *gin.Engine {
	return httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            false,
		InjectCodeInHeader:      "",
		CustomStylesheet:        "",
		AllowInsecure:           true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		HideLinkMetadataSection: false,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		EnableWorkspaceSync:     true,
	})
}

func createRouterTestInstanceWithMaxAssetUploadSize(w *wiki.Wiki, t routerTestTB, maxAssetUploadSizeBytes shared.MaxBytes) *gin.Engine {
	return httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            false,
		InjectCodeInHeader:      "",
		CustomStylesheet:        "",
		AllowInsecure:           true,
		AccessTokenTimeout:      15 * time.Minute,   // 15 minutes
		RefreshTokenTimeout:     7 * 24 * time.Hour, // 7 days
		HideLinkMetadataSection: false,
		MaxAssetUploadSizeBytes: maxAssetUploadSizeBytes,
	})
}

func createRouterTestInstanceWithAllowInsecure(w *wiki.Wiki, allowInsecure bool, t routerTestTB) *gin.Engine {
	return httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            false,
		InjectCodeInHeader:      "",
		CustomStylesheet:        "",
		AllowInsecure:           allowInsecure,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		HideLinkMetadataSection: false,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
	})
}

func authenticatedRequest(t routerTestTB, router http.Handler, method, url string, body *strings.Reader) *httptest.ResponseRecorder {
	// Login
	loginBody := `{"identifier": "admin", "password": "admin"}`
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("Failed to login: %d - %s", loginRec.Code, loginRec.Body.String())
	}

	loginRes := loginRec.Result()
	defer wrapCloseWithErrorCheck(loginRes.Body.Close, t)

	cookies := loginRes.Cookies()
	if len(cookies) == 0 {
		t.Fatalf("Expected auth cookies on login response, got none")
	}

	csrfToken := loginRec.Header().Get("X-CSRF-Token")
	if csrfToken == "" {
		for _, c := range cookies {
			if c.Name == "leafwiki_csrf" || c.Name == "__Host-leafwiki_csrf" {
				csrfToken = c.Value
				break
			}
		}
	}

	if csrfToken == "" {
		t.Fatalf("Expected CSRF token after login, got none")
	}

	// Perform authenticated request
	if body == nil {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, url, body)
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		req.Header.Set("X-CSRF-Token", csrfToken)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func authenticatedRequestAs(t routerTestTB, router http.Handler, username, password, method, url string, body *strings.Reader) *httptest.ResponseRecorder {
	// Login with specific credentials
	loginData := map[string]string{
		"identifier": username,
		"password":   password,
	}
	loginBodyBytes, _ := json.Marshal(loginData)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBodyBytes))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("Failed to login as %s: %d - %s", username, loginRec.Code, loginRec.Body.String())
	}

	loginRes := loginRec.Result()
	defer wrapCloseWithErrorCheck(loginRes.Body.Close, t)

	cookies := loginRes.Cookies()
	if len(cookies) == 0 {
		t.Fatalf("Expected auth cookies on login response, got none")
	}

	csrfToken := loginRec.Header().Get("X-CSRF-Token")
	if csrfToken == "" {
		for _, c := range cookies {
			if c.Name == "leafwiki_csrf" || c.Name == "__Host-leafwiki_csrf" {
				csrfToken = c.Value
				break
			}
		}
	}

	if csrfToken == "" {
		t.Fatalf("Expected CSRF token after login, got none")
	}

	// Perform authenticated request
	var reqBody io.Reader
	if body != nil {
		reqBody = body
	}
	req := httptest.NewRequest(method, url, reqBody)
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		req.Header.Set("X-CSRF-Token", csrfToken)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func assertNoStoreHeaders(t routerTestTB, rec *httptest.ResponseRecorder) {
	t.Helper()
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("Pragma = %q, want no-cache", got)
	}
	if got := rec.Header().Get("Expires"); got != "Thu, 01 Jan 1970 00:00:00 GMT" {
		t.Fatalf("Expires = %q, want expired header", got)
	}
}

func assertAPIKeyNullMetadata(t routerTestTB, key map[string]any) {
	t.Helper()
	for _, field := range []string{"lastUsedAt", "revokedAt"} {
		value, ok := key[field]
		if !ok {
			t.Fatalf("api key metadata missing %q: %#v", field, key)
		}
		if value != nil {
			t.Fatalf("api key metadata %q = %#v, want null", field, value)
		}
	}
}

type apiPage struct {
	ID             string                 `json:"id"`
	Title          string                 `json:"title"`
	Slug           string                 `json:"slug"`
	Content        string                 `json:"content"`
	Path           string                 `json:"path"`
	Version        string                 `json:"version"`
	Kind           tree.NodeKind          `json:"kind"`
	ContentPath    string                 `json:"contentPath"`
	ReadmeFallback bool                   `json:"readmeFallback"`
	Children       []*apiPage             `json:"children"`
	Tags           []string               `json:"tags"`
	Properties     map[string]interface{} `json:"properties"`
}

type apiPermalinkTarget struct {
	ID   string        `json:"id"`
	Slug string        `json:"slug"`
	Path string        `json:"path"`
	Kind tree.NodeKind `json:"kind"`
}

func createPageViaAPI(t routerTestTB, router http.Handler, title, slug string, parentID *string, kind *tree.NodeKind) *apiPage {
	t.Helper()

	payload := map[string]any{
		"title": title,
		"slug":  slug,
	}
	if parentID != nil {
		payload["parentId"] = *parentID
	}
	if kind != nil {
		payload["kind"] = string(*kind)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal(create page payload) failed: %v", err)
	}

	rec := authenticatedRequest(t, router, http.MethodPost, "/api/pages", strings.NewReader(string(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created, got %d - %s", rec.Code, rec.Body.String())
	}

	var page apiPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("Unmarshal(create page response) failed: %v", err)
	}

	return &page
}

func getPageByPathViaAPI(t routerTestTB, router http.Handler, path string) *apiPage {
	t.Helper()

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/by-path?path="+path, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
	}

	var page apiPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("Unmarshal(get page by path response) failed: %v", err)
	}

	return &page
}

func getPermalinkTargetViaAPI(t routerTestTB, router http.Handler, id string) *apiPermalinkTarget {
	t.Helper()

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/permalink/"+id, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
	}

	var target apiPermalinkTarget
	if err := json.Unmarshal(rec.Body.Bytes(), &target); err != nil {
		t.Fatalf("Unmarshal(get permalink target response) failed: %v", err)
	}

	return &target
}

func getTreeViaAPI(t routerTestTB, router http.Handler) *apiPage {
	t.Helper()

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/tree", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
	}

	var node apiPage
	if err := json.Unmarshal(rec.Body.Bytes(), &node); err != nil {
		t.Fatalf("Unmarshal(tree response) failed: %v", err)
	}

	return &node
}

func deletePageViaAPI(t routerTestTB, router http.Handler, pageID string, version string, recursive bool) {
	t.Helper()

	url := "/api/pages/" + pageID + "?version=" + version
	if recursive {
		url += "&recursive=true"
	}

	rec := authenticatedRequest(t, router, http.MethodDelete, url, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
	}
}

func listAssetsViaAPI(t routerTestTB, router http.Handler, pageID string) []string {
	t.Helper()

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/"+pageID+"/assets", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal(list assets response) failed: %v", err)
	}

	return resp.Files
}

func uploadAssetViaAPI(t routerTestTB, router http.Handler, pageID, filename, content string) string {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile failed: %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("Write(asset payload) failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close(writer) failed: %v", err)
	}

	loginBody := `{"identifier": "admin", "password": "admin"}`
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on login, got %d - %s", loginRec.Code, loginRec.Body.String())
	}

	loginRes := loginRec.Result()
	defer wrapCloseWithErrorCheck(loginRes.Body.Close, t)

	cookies := loginRes.Cookies()
	csrfToken := loginRec.Header().Get("X-CSRF-Token")
	if csrfToken == "" {
		for _, c := range cookies {
			if c.Name == "leafwiki_csrf" || c.Name == "__Host-leafwiki_csrf" {
				csrfToken = c.Value
				break
			}
		}
	}
	if csrfToken == "" {
		t.Fatal("Expected CSRF token after login, got none")
	}

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/pages/"+pageID+"/assets", body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadReq.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		uploadReq.AddCookie(cookie)
	}

	uploadRec := httptest.NewRecorder()
	router.ServeHTTP(uploadRec, uploadReq)

	if uploadRec.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created on upload, got %d - %s", uploadRec.Code, uploadRec.Body.String())
	}

	var uploadResp map[string]string
	if err := json.Unmarshal(uploadRec.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("Unmarshal(upload asset response) failed: %v", err)
	}

	return uploadResp["file"]
}

func getLatestRevisionViaAPI(t routerTestTB, router http.Handler, pageID string) map[string]any {
	t.Helper()

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/"+pageID+"/revisions/latest", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
	}

	var rev map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rev); err != nil {
		t.Fatalf("Unmarshal(latest revision response) failed: %v", err)
	}
	return rev
}

func getAdminUserIDViaAPI(t routerTestTB, router http.Handler) string {
	t.Helper()

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/users", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
	}

	var users []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &users); err != nil {
		t.Fatalf("Unmarshal(users response) failed: %v", err)
	}
	for _, user := range users {
		if role, _ := user["role"].(string); role == "admin" {
			if id, _ := user["id"].(string); id != "" {
				return id
			}
		}
	}
	t.Fatal("admin user not found")
	return ""
}

func writePageMarkdownForTest(t routerTestTB, w *wiki.Wiki, page *apiPage, raw string) {
	t.Helper()

	pagePath := filepath.Join(w.GetRootDir(), filepath.FromSlash(page.Path)+".md")
	if err := os.WriteFile(pagePath, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile(page markdown) failed: %v", err)
	}
}

func uploadBrandingLogoViaAPI(t routerTestTB, router http.Handler, filename string, content []byte) {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile failed: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("Write(logo payload) failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close(writer) failed: %v", err)
	}

	loginBody := `{"identifier": "admin", "password": "admin"}`
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on login, got %d - %s", loginRec.Code, loginRec.Body.String())
	}

	loginRes := loginRec.Result()
	defer wrapCloseWithErrorCheck(loginRes.Body.Close, t)

	cookies := loginRes.Cookies()
	csrfToken := loginRec.Header().Get("X-CSRF-Token")
	if csrfToken == "" {
		for _, c := range cookies {
			if c.Name == "leafwiki_csrf" || c.Name == "__Host-leafwiki_csrf" {
				csrfToken = c.Value
				break
			}
		}
	}
	if csrfToken == "" {
		t.Fatal("Expected CSRF token after login, got none")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/branding/logo", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
	}
}

func uploadBrandingFaviconViaAPI(t routerTestTB, router http.Handler, filename string, content []byte) {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile failed: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("Write(favicon payload) failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close(writer) failed: %v", err)
	}

	loginBody := `{"identifier": "admin", "password": "admin"}`
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on login, got %d - %s", loginRec.Code, loginRec.Body.String())
	}

	loginRes := loginRec.Result()
	defer wrapCloseWithErrorCheck(loginRes.Body.Close, t)

	cookies := loginRes.Cookies()
	csrfToken := loginRec.Header().Get("X-CSRF-Token")
	if csrfToken == "" {
		for _, c := range cookies {
			if c.Name == "leafwiki_csrf" || c.Name == "__Host-leafwiki_csrf" {
				csrfToken = c.Value
				break
			}
		}
	}
	if csrfToken == "" {
		t.Fatal("Expected CSRF token after login, got none")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/branding/favicon", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
	}
}

func importerFixturePathForHTTPTests(t routerTestTB, rel string) string {
	t.Helper()

	return fixturePathForHTTPTests(t, rel, "../importer/fixtures", "internal/importer/fixtures")
}

func createZipFromDir(t routerTestTB, root string) []byte {
	t.Helper()

	var body bytes.Buffer
	zipWriter := zip.NewWriter(&body)

	err := filepath.Walk(root, func(sourcePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relativePath, err := filepath.Rel(root, sourcePath)
		if err != nil {
			return err
		}

		entry, err := zipWriter.Create(filepath.ToSlash(relativePath))
		if err != nil {
			return err
		}

		raw, err := os.ReadFile(sourcePath)
		if err != nil {
			return err
		}
		_, err = entry.Write(raw)
		return err
	})
	if err != nil {
		t.Fatalf("create zip from dir: %v", err)
	}

	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}

	return body.Bytes()
}

var _ = It("TestDisableRequestLog_DoesNotCrash", func() {
	t := GinkgoT()
	logs := captureDefaultLogs(t)
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
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

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(logs.String(), "http request") {
		t.Fatalf("request log was written despite DisableRequestLog: %s", logs.String())
	}

})

var _ = It("TestRequestLogsGoToDefaultSlogSink", func() {
	t := GinkgoT()
	logs := captureDefaultLogs(t)
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		AllowInsecure:           true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	entry := findJSONLogEntry(t, logs.String(), "http request")
	for _, key := range []string{"method", "path", "status", "latency", "ip"} {
		if _, ok := entry[key]; !ok {
			t.Fatalf("http request log missing %q: %#v", key, entry)
		}
	}
	if entry["method"] != http.MethodGet {
		t.Fatalf("method = %v, want GET", entry["method"])
	}
	if entry["path"] != "/api/health" {
		t.Fatalf("path = %v, want /api/health", entry["path"])
	}
	if entry["status"] != float64(http.StatusOK) {
		t.Fatalf("status = %v, want 200", entry["status"])
	}

})

var _ = It("TestGinRecoveryLogsGoToDefaultSlogSink", func() {
	t := GinkgoT()
	logs := captureDefaultLogs(t)
	router := httpinternal.NewRouter([]httpinternal.RouteRegistrar{panicRegistrar{}}, httpinternal.FrontendConfig{}, httpinternal.RouterOptions{
		AllowInsecure:           true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logs.String(), "panic route") {
		t.Fatalf("recovery log did not include panic text: %s", logs.String())
	}

})

var _ = It("TestMeEndpoint_Unauthenticated_Returns200WithNullBody", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for unauthenticated /auth/me, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "null" {
		t.Errorf("expected null body for unauthenticated request, got %q", body)
	}

})

var _ = It("TestMeEndpoint_Authenticated_ReturnsUser", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/auth/me", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for authenticated /auth/me, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode /auth/me response: %v", err)
	}
	if body["username"] != "admin" {
		t.Errorf("expected username=admin, got %v", body["username"])
	}
	if body["role"] != "admin" {
		t.Errorf("expected role=admin, got %v", body["role"])
	}

})

var _ = DescribeTable("TestMeEndpoint_HasNoCacheHeaders",
	func(authenticated bool) {
		t := GinkgoT()
		w := createWikiTestInstance(t)
		defer wrapCloseWithErrorCheck(w.Close, t)
		router := createRouterTestInstance(w, t)

		var rec *httptest.ResponseRecorder
		if authenticated {
			rec = authenticatedRequest(t, router, http.MethodGet, "/api/auth/me", nil)
		} else {
			req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
			rec = httptest.NewRecorder()
			router.ServeHTTP(rec, req)
		}

		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("expected Cache-Control: no-store, got %q", cc)
		}
		if p := rec.Header().Get("Pragma"); p != "no-cache" {
			t.Errorf("expected Pragma: no-cache, got %q", p)
		}
		if exp := rec.Header().Get("Expires"); exp == "" {
			t.Error("expected Expires header to be set")
		}
	},
	Entry("unauthenticated", false),
	Entry("authenticated", true),
)

var _ = It("TestCreatePageEndpoint", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	title := "Getting Started"
	expectedSlug := "getting-started"

	body := `{"title": "Getting Started", "slug": "getting-started"}`

	rec := authenticatedRequest(t, router, http.MethodPost, "/api/pages", strings.NewReader(body))

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected status 201, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Invalid JSON response: %v", err)
	}

	if resp["id"] == nil {
		t.Errorf("Expected id in response, got: %v", resp)
	}

	if resp["title"] != title {
		t.Errorf("Expected title in response, got: %v", resp)
	}

	if resp["slug"] != expectedSlug {
		t.Errorf("Expected slug in response, got: %v", resp)
	}

})

var _ = It("TestConfigEndpoint_ExplainsAllowInsecureRequirementOnHTTP", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstanceWithAllowInsecure(w, false, t)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}

	if !strings.Contains(rec.Body.String(), "--allow-insecure") {
		t.Fatalf("expected response to explain allow-insecure requirement, got %s", rec.Body.String())
	}

})

var _ = It("TestLoginEndpoint_ExplainsAllowInsecureRequirementOnHTTP", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstanceWithAllowInsecure(w, false, t)

	loginBody := `{"identifier": "admin", "password": "admin"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d with body %s", rec.Code, rec.Body.String())
	}

	if !strings.Contains(rec.Body.String(), "--allow-insecure") {
		t.Fatalf("expected response to explain allow-insecure requirement, got %s", rec.Body.String())
	}

})

var _ = It("TestCreatePageEndpoint_MissingTitle", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	body := `{"title": ""}`
	rec := authenticatedRequest(t, router, http.MethodPost, "/api/pages", strings.NewReader(body))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for missing title, got %d", rec.Code)
	}

})

var _ = It("TestCreatePageEndpoint_InvalidJSON", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	body := `this is not valid json`
	rec := authenticatedRequest(t, router, http.MethodPost, "/api/pages", strings.NewReader(body))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for invalid JSON, got %d", rec.Code)
	}

})

var _ = It("TestCreatePageEndpoint_PageAlreadyExists", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	body := `{"title": "Page Exists", "slug": "page-exists"}`
	rec1 := authenticatedRequest(t, router, http.MethodPost, "/api/pages", strings.NewReader(body))

	if rec1.Code != http.StatusCreated {
		t.Fatalf("Expected status 201, got %d", rec1.Code)
	}

	rec2 := authenticatedRequest(t, router, http.MethodPost, "/api/pages", strings.NewReader(body))

	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("Expected status 400, got %d", rec2.Code)
	}

})

var _ = It("TestGetTreeEndpoint", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/tree", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}

	var resp map[string]interface{}

	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Invalid JSON response: %v", err)
	}

	if _, ok := resp["id"]; !ok {
		t.Errorf("Expected root node in response")
	}

	if resp["title"] != "root" {
		t.Errorf("Expected root node title to be 'Root', got: %v", resp)
	}

	if resp["slug"] != "root" {
		t.Errorf("Expected root node slug to be 'root', got: %v", resp)
	}

	if resp["id"] != "root" {
		t.Errorf("Expected root node id to be 'root', got: %v", resp)
	}

})

var _ = It("TestConfigEndpoint_IncludesMaxAssetUploadSizeBytes", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)

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

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Invalid JSON response: %v", err)
	}

	gotSize, ok := resp["maxAssetUploadSizeBytes"].(float64)
	if !ok {
		t.Fatalf("Expected maxAssetUploadSizeBytes in config response, got %v", resp)
	}

	if int64(gotSize) != int64(maxAssetUploadSizeBytes) {
		t.Fatalf("Expected maxAssetUploadSizeBytes=%d, got %v", maxAssetUploadSizeBytes, gotSize)
	}

})

var _ = It("TestConfigEndpoint_IncludesEnableLinkRefactor", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)

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

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Invalid JSON response: %v", err)
	}

	gotEnabled, ok := resp["enableLinkRefactor"].(bool)
	if !ok {
		t.Fatalf("Expected enableLinkRefactor in config response, got %v", resp)
	}

	if !gotEnabled {
		t.Fatalf("Expected enableLinkRefactor=true, got %v", gotEnabled)
	}

})

var _ = It("TestConfigEndpoint_IncludesMarkdownLinkRootPrefix", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)

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

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Invalid JSON response: %v", err)
	}

	if got := resp["markdownLinkRootPrefix"]; got != "/docs" {
		t.Fatalf("Expected markdownLinkRootPrefix=/docs, got %v in %v", got, resp)
	}

})

var _ = It("TestConfigEndpoint_IncludesEnableWorkspaceSyncWhenDisabled", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)

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

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Invalid JSON response: %v", err)
	}

	gotEnabled, ok := resp["enableWorkspaceSync"].(bool)
	if !ok {
		t.Fatalf("Expected enableWorkspaceSync in config response, got %v", resp)
	}

	if gotEnabled {
		t.Fatalf("Expected enableWorkspaceSync=false, got %v", gotEnabled)
	}

})

var _ = It("TestWorkspaceSyncStatusEndpoint_WhenEnabled", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "content")
	if err := os.WriteFile(filepath.Join(rootDir, "page.md"), []byte("---\nleafwiki_id: page\nleafwiki_title: Page\n---\n# Page\n"), 0o644); err != nil {
		if err := os.MkdirAll(rootDir, 0o755); err != nil {
			t.Fatalf("create root dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "page.md"), []byte("---\nleafwiki_id: page\nleafwiki_title: Page\n---\n# Page\n"), 0o644); err != nil {
			t.Fatalf("write page: %v", err)
		}
	}
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		Workspace: wiki.Workspace{
			DataDir: dataDir,
			RootDir: rootDir,
		},
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
		AuthDisabled:        true,
	})
	if err != nil {
		t.Fatalf("NewWiki: %v", err)
	}
	defer wrapCloseWithErrorCheck(w.Close, t)

	router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            true,
		AllowInsecure:           true,
		AuthDisabled:            true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		EnableWorkspaceSync:     true,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/workspace-sync/status", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if resp["enabled"] != true {
		t.Fatalf("enabled = %v, want true: %#v", resp["enabled"], resp)
	}
	if resp["lastCommitHash"] == "" {
		t.Fatalf("lastCommitHash missing: %#v", resp)
	}

})

var _ = It("TestWorkspaceSyncRefreshEndpoint_SyncsDirectMarkdownCreate", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "content")
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		Workspace: wiki.Workspace{
			DataDir: dataDir,
			RootDir: rootDir,
		},
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
		AuthDisabled:        true,
	})
	if err != nil {
		t.Fatalf("NewWiki: %v", err)
	}
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            true,
		AllowInsecure:           true,
		AuthDisabled:            true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		EnableWorkspaceSync:     true,
	})

	if err := os.WriteFile(filepath.Join(rootDir, "direct.md"), []byte("---\nleafwiki_id: direct\nleafwiki_title: Direct\n---\n# Direct\n"), 0o644); err != nil {
		t.Fatalf("write direct markdown: %v", err)
	}
	configReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	configRec := httptest.NewRecorder()
	router.ServeHTTP(configRec, configReq)
	csrfToken := configRec.Header().Get("X-CSRF-Token")
	req := httptest.NewRequest(http.MethodPost, "/api/workspace-sync/refresh", nil)
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range configRec.Result().Cookies() {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST refresh = %d: %s", rec.Code, rec.Body.String())
	}
	pageReq := httptest.NewRequest(http.MethodGet, "/api/pages/by-path?path=direct", nil)
	pageRec := httptest.NewRecorder()
	router.ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusOK {
		t.Fatalf("GET synced page = %d: %s", pageRec.Code, pageRec.Body.String())
	}
	var page apiPage
	if err := json.Unmarshal(pageRec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode synced page: %v", err)
	}
	if page.ID != "direct" || page.Title != "Direct" {
		t.Fatalf("synced page = %#v, want direct page", page)
	}

})

var _ = It("TestWorkspaceSyncSnapshotsEndpoint_WhenEnabled", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "content")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("create root dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "page.md"), []byte("---\nleafwiki_id: page\nleafwiki_title: Page\n---\n# Page\n"), 0o644); err != nil {
		t.Fatalf("write page: %v", err)
	}
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		Workspace:           wiki.Workspace{DataDir: dataDir, RootDir: rootDir},
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
		AuthDisabled:        true,
	})
	if err != nil {
		t.Fatalf("NewWiki: %v", err)
	}
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            true,
		AllowInsecure:           true,
		AuthDisabled:            true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		EnableWorkspaceSync:     true,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/workspace-sync/snapshots", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET snapshots = %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Snapshots  []map[string]any `json:"snapshots"`
		NextCursor string           `json:"nextCursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode snapshots: %v", err)
	}
	if len(resp.Snapshots) == 0 || resp.Snapshots[0]["id"] == "" {
		t.Fatalf("snapshots missing commit id: %#v", resp)
	}

})

var _ = It("TestWorkspaceSyncSnapshotsEndpoint_RespectsLimit", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "content")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("create root dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "page.md"), []byte("---\nleafwiki_id: page\nleafwiki_title: Page\n---\n# Page\n"), 0o644); err != nil {
		t.Fatalf("write page: %v", err)
	}
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		Workspace:           wiki.Workspace{DataDir: dataDir, RootDir: rootDir},
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
		AuthDisabled:        true,
	})
	if err != nil {
		t.Fatalf("NewWiki: %v", err)
	}
	defer wrapCloseWithErrorCheck(w.Close, t)
	if err := os.WriteFile(filepath.Join(rootDir, "page.md"), []byte("---\nleafwiki_id: page\nleafwiki_title: Page\n---\n# Page 2\n"), 0o644); err != nil {
		t.Fatalf("write page update: %v", err)
	}
	if _, err := w.WorkspaceSyncRefresh(context.Background(), workspacesync.SyncRequest{
		Reason: workspacesync.ReasonExplicit,
		Source: workspacesync.SourceFilesystem,
		Actor:  workspacesync.PublicEditorActor(),
	}); err != nil {
		t.Fatalf("WorkspaceSyncRefresh: %v", err)
	}
	router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            true,
		AllowInsecure:           true,
		AuthDisabled:            true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		EnableWorkspaceSync:     true,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/workspace-sync/snapshots?limit=1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET snapshots limit = %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Snapshots  []map[string]any `json:"snapshots"`
		NextCursor string           `json:"nextCursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode snapshots: %v", err)
	}
	if len(resp.Snapshots) != 1 {
		t.Fatalf("snapshot count = %d, want 1: %#v", len(resp.Snapshots), resp)
	}
	if resp.NextCursor == "" {
		t.Fatalf("next cursor is empty, want second page cursor: %#v", resp)
	}
	firstID := resp.Snapshots[0]["id"]

	req = httptest.NewRequest(http.MethodGet, "/api/workspace-sync/snapshots?limit=1&cursor="+resp.NextCursor, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET snapshots second page = %d: %s", rec.Code, rec.Body.String())
	}
	resp = struct {
		Snapshots  []map[string]any `json:"snapshots"`
		NextCursor string           `json:"nextCursor"`
	}{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode second page snapshots: %v", err)
	}
	if len(resp.Snapshots) != 1 {
		t.Fatalf("second page snapshot count = %d, want 1: %#v", len(resp.Snapshots), resp)
	}
	if resp.Snapshots[0]["id"] == firstID {
		t.Fatalf("second page returned same snapshot id %v", firstID)
	}

})

var _ = It("TestWorkspaceSyncSnapshotsEndpoint_StableCursorSurvivesNewerCommit", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "content")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("create root dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "page.md"), []byte("<!-- leafwiki\nversion: 1\npage:\n  id: page\n  title: Page\n-->\n\n# Page 1\n"), 0o644); err != nil {
		t.Fatalf("write page: %v", err)
	}
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		Workspace:           wiki.Workspace{DataDir: dataDir, RootDir: rootDir},
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
		AuthDisabled:        true,
	})
	if err != nil {
		t.Fatalf("NewWiki: %v", err)
	}
	defer wrapCloseWithErrorCheck(w.Close, t)
	initialCommit := w.WorkspaceSyncStatus().LastCommitHash
	if initialCommit == "" {
		t.Fatalf("initial workspace commit is empty")
	}
	if err := os.WriteFile(filepath.Join(rootDir, "page.md"), []byte("<!-- leafwiki\nversion: 1\npage:\n  id: page\n  title: Page\n-->\n\n# Page 2\n"), 0o644); err != nil {
		t.Fatalf("write page update: %v", err)
	}
	if _, err := w.WorkspaceSyncRefresh(context.Background(), workspacesync.SyncRequest{
		Reason: workspacesync.ReasonExplicit,
		Source: workspacesync.SourceFilesystem,
		Actor:  workspacesync.PublicEditorActor(),
	}); err != nil {
		t.Fatalf("WorkspaceSyncRefresh page 2: %v", err)
	}
	router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            true,
		AllowInsecure:           true,
		AuthDisabled:            true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		EnableWorkspaceSync:     true,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/workspace-sync/snapshots?limit=1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET snapshots first page = %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Snapshots  []map[string]any `json:"snapshots"`
		NextCursor string           `json:"nextCursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode first page snapshots: %v", err)
	}
	if len(resp.Snapshots) != 1 || resp.NextCursor == "" {
		t.Fatalf("first page response = %#v, want one snapshot with cursor", resp)
	}
	firstPageID, _ := resp.Snapshots[0]["id"].(string)

	if err := os.WriteFile(filepath.Join(rootDir, "page.md"), []byte("<!-- leafwiki\nversion: 1\npage:\n  id: page\n  title: Page\n-->\n\n# Page 3\n"), 0o644); err != nil {
		t.Fatalf("write page newer update: %v", err)
	}
	if _, err := w.WorkspaceSyncRefresh(context.Background(), workspacesync.SyncRequest{
		Reason: workspacesync.ReasonExplicit,
		Source: workspacesync.SourceFilesystem,
		Actor:  workspacesync.PublicEditorActor(),
	}); err != nil {
		t.Fatalf("WorkspaceSyncRefresh page 3: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/workspace-sync/snapshots?limit=1&cursor="+resp.NextCursor, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET snapshots second page = %d: %s", rec.Code, rec.Body.String())
	}
	resp = struct {
		Snapshots  []map[string]any `json:"snapshots"`
		NextCursor string           `json:"nextCursor"`
	}{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode second page snapshots: %v", err)
	}
	if len(resp.Snapshots) != 1 {
		t.Fatalf("second page snapshot count = %d, want 1: %#v", len(resp.Snapshots), resp)
	}
	secondPageID, _ := resp.Snapshots[0]["id"].(string)
	if secondPageID == firstPageID {
		t.Fatalf("second page duplicated first page snapshot %s after newer commit", firstPageID)
	}
	if workspacesync.CommitHashFromString(secondPageID) != initialCommit {
		t.Fatalf("second page snapshot = %s, want original older commit %s", secondPageID, initialCommit)
	}

})

var _ = It("TestWorkspaceSyncStatusEndpoint_PublicAccessAllowsUnauthenticatedRead", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "content")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("create root dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "page.md"), []byte("---\nleafwiki_id: page\nleafwiki_title: Page\n---\n# Page\n"), 0o644); err != nil {
		t.Fatalf("write page: %v", err)
	}
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		Workspace:           wiki.Workspace{DataDir: dataDir, RootDir: rootDir},
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewWiki: %v", err)
	}
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            true,
		AllowInsecure:           true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		EnableWorkspaceSync:     true,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/workspace-sync/status", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET public workspace status = %d: %s", rec.Code, rec.Body.String())
	}

})

var _ = It("TestWorkspaceSyncSnapshotRestoreEndpoint_RestoresMarkdownOnly", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "content")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("create root dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "one.md"), []byte("---\nleafwiki_id: one\nleafwiki_title: One\n---\n# One A\n"), 0o644); err != nil {
		t.Fatalf("write one.md: %v", err)
	}
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		Workspace:           wiki.Workspace{DataDir: dataDir, RootDir: rootDir},
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
		AuthDisabled:        true,
	})
	if err != nil {
		t.Fatalf("NewWiki: %v", err)
	}
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            true,
		AllowInsecure:           true,
		AuthDisabled:            true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		EnableWorkspaceSync:     true,
	})
	status := w.WorkspaceSyncStatus()
	if status.LastCommitHash == "" {
		t.Fatalf("missing initial snapshot hash")
	}

	if err := os.WriteFile(filepath.Join(rootDir, "one.md"), []byte("---\nleafwiki_id: one\nleafwiki_title: One\n---\n# One current\n"), 0o644); err != nil {
		t.Fatalf("write current one.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "two.md"), []byte("---\nleafwiki_id: two\nleafwiki_title: Two\n---\n# Two current\n"), 0o644); err != nil {
		t.Fatalf("write two.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "image.png"), []byte("png"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	configReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	configRec := httptest.NewRecorder()
	router.ServeHTTP(configRec, configReq)
	csrfToken := configRec.Header().Get("X-CSRF-Token")
	req := httptest.NewRequest(http.MethodPost, "/api/workspace-sync/snapshots/"+status.LastCommitHash.String()+"/restore", nil)
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range configRec.Result().Cookies() {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST restore = %d: %s", rec.Code, rec.Body.String())
	}
	raw, err := os.ReadFile(filepath.Join(rootDir, "one.md"))
	if err != nil {
		t.Fatalf("read restored one.md: %v", err)
	}
	if !strings.Contains(string(raw), "# One A") {
		t.Fatalf("one.md was not restored: %q", string(raw))
	}
	if _, err := os.Stat(filepath.Join(rootDir, "two.md")); !os.IsNotExist(err) {
		t.Fatalf("two.md state = %v, want removed", err)
	}
	if raw, err := os.ReadFile(filepath.Join(rootDir, "image.png")); err != nil || string(raw) != "png" {
		t.Fatalf("image.png = %q, %v; want untouched png", string(raw), err)
	}
	snapshots, err := w.WorkspaceSyncSnapshots(context.Background(), 1)
	if err != nil {
		t.Fatalf("WorkspaceSyncSnapshots: %v", err)
	}
	if len(snapshots) == 0 {
		t.Fatalf("snapshots empty after restore")
	}
	if snapshots[0].Source != string(workspacesync.SourceWeb) {
		t.Fatalf("restore snapshot source = %q, want web", snapshots[0].Source)
	}

})

var _ = It("TestWorkspaceSyncPageRevisionsEndpoint_UsesGitBackedHistory", func() {
	t := GinkgoT()
	dataDir := filepath.Join(t.TempDir(), "data")
	rootDir := filepath.Join(t.TempDir(), "content")
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		Workspace:           wiki.Workspace{DataDir: dataDir, RootDir: rootDir},
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
		AuthDisabled:        true,
	})
	if err != nil {
		t.Fatalf("NewWiki: %v", err)
	}
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            true,
		AllowInsecure:           true,
		AuthDisabled:            true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		EnableWorkspaceSync:     true,
	})
	configReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	configRec := httptest.NewRecorder()
	router.ServeHTTP(configRec, configReq)
	if configRec.Code != http.StatusOK {
		t.Fatalf("GET config = %d: %s", configRec.Code, configRec.Body.String())
	}
	csrfToken := configRec.Header().Get("X-CSRF-Token")
	createReq := httptest.NewRequest(http.MethodPost, "/api/pages", strings.NewReader(`{"title":"Git History","slug":"git-history"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range configRec.Result().Cookies() {
		createReq.AddCookie(cookie)
	}
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("POST page = %d: %s", createRec.Code, createRec.Body.String())
	}
	var page apiPage
	if err := json.Unmarshal(createRec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/pages/"+page.ID+"/revisions", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET workspace revisions = %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Revisions  []map[string]any `json:"revisions"`
		NextCursor string           `json:"nextCursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode revisions: %v", err)
	}
	if len(resp.Revisions) == 0 {
		t.Fatalf("expected at least one Git-backed revision: %#v", resp)
	}
	if resp.Revisions[0]["id"] == "" || resp.Revisions[0]["pageId"] != page.ID {
		t.Fatalf("unexpected workspace revision: %#v", resp.Revisions[0])
	}

})

var _ = It("TestWorkspaceSyncRevisionSnapshotAndRestoreEndpoint_UseGitBackend", func() {
	t := GinkgoT()
	dataDir := filepath.Join(t.TempDir(), "data")
	rootDir := filepath.Join(t.TempDir(), "content")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("create root dir: %v", err)
	}
	previous := `---
leafwiki_id: restore-page
leafwiki_title: Restore Page
---

# Restore Page

previous content`
	if err := os.WriteFile(filepath.Join(rootDir, "restore-page.md"), []byte(previous), 0o644); err != nil {
		t.Fatalf("write previous markdown: %v", err)
	}
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		Workspace:           wiki.Workspace{DataDir: dataDir, RootDir: rootDir},
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
		AuthDisabled:        true,
	})
	if err != nil {
		t.Fatalf("NewWiki: %v", err)
	}
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            true,
		AllowInsecure:           true,
		AuthDisabled:            true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		EnableWorkspaceSync:     true,
	})

	revisionsReq := httptest.NewRequest(http.MethodGet, "/api/pages/restore-page/revisions", nil)
	revisionsRec := httptest.NewRecorder()
	router.ServeHTTP(revisionsRec, revisionsReq)
	if revisionsRec.Code != http.StatusOK {
		t.Fatalf("GET revisions = %d: %s", revisionsRec.Code, revisionsRec.Body.String())
	}
	var revisionsResp struct {
		Revisions []map[string]any `json:"revisions"`
	}
	if err := json.Unmarshal(revisionsRec.Body.Bytes(), &revisionsResp); err != nil {
		t.Fatalf("decode revisions: %v", err)
	}
	if len(revisionsResp.Revisions) == 0 {
		t.Fatalf("expected initial revision: %#v", revisionsResp)
	}
	oldRevisionID, _ := revisionsResp.Revisions[0]["id"].(string)
	if oldRevisionID == "" {
		t.Fatalf("old revision id missing: %#v", revisionsResp.Revisions[0])
	}

	current := strings.Replace(previous, "previous content", "current content", 1)
	if err := os.WriteFile(filepath.Join(rootDir, "restore-page.md"), []byte(current), 0o644); err != nil {
		t.Fatalf("write current markdown: %v", err)
	}
	if _, err := w.WorkspaceSyncRefresh(context.Background(), workspacesync.SyncRequest{
		Reason: workspacesync.ReasonExplicit,
		Source: workspacesync.SourceFilesystem,
		Actor:  workspacesync.PublicEditorActor(),
	}); err != nil {
		t.Fatalf("WorkspaceSyncRefresh current: %v", err)
	}

	snapshotReq := httptest.NewRequest(http.MethodGet, "/api/pages/restore-page/revisions/"+oldRevisionID, nil)
	snapshotRec := httptest.NewRecorder()
	router.ServeHTTP(snapshotRec, snapshotReq)
	if snapshotRec.Code != http.StatusOK {
		t.Fatalf("GET revision snapshot = %d: %s", snapshotRec.Code, snapshotRec.Body.String())
	}
	var snapshot struct {
		Content string `json:"content"`
		Assets  []any  `json:"assets"`
	}
	if err := json.Unmarshal(snapshotRec.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if !strings.Contains(snapshot.Content, "previous content") || len(snapshot.Assets) != 0 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}

	configReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	configRec := httptest.NewRecorder()
	router.ServeHTTP(configRec, configReq)
	csrfToken := configRec.Header().Get("X-CSRF-Token")
	restoreReq := httptest.NewRequest(http.MethodPost, "/api/pages/restore-page/revisions/"+oldRevisionID+"/restore", nil)
	restoreReq.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range configRec.Result().Cookies() {
		restoreReq.AddCookie(cookie)
	}
	restoreRec := httptest.NewRecorder()
	router.ServeHTTP(restoreRec, restoreReq)
	if restoreRec.Code != http.StatusOK {
		t.Fatalf("POST restore = %d: %s", restoreRec.Code, restoreRec.Body.String())
	}
	raw, err := os.ReadFile(filepath.Join(rootDir, "restore-page.md"))
	if err != nil {
		t.Fatalf("read restored markdown: %v", err)
	}
	if !strings.Contains(string(raw), "previous content") {
		t.Fatalf("restored markdown = %q, want previous content", string(raw))
	}

})

var _ = It("TestRefactorPreviewEndpoint_UsesFrontendJSONShape", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)

	router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            false,
		InjectCodeInHeader:      "",
		AllowInsecure:           true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		HideLinkMetadataSection: false,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		EnableLinkRefactor:      true,
	})
	target := createPageViaAPI(t, router, "Target", "target", nil, pageNodeKind())
	ref := createPageViaAPI(t, router, "Ref", "ref", nil, pageNodeKind())

	updateBody := strings.NewReader(`{"version":"` + ref.Version + `","title":"Ref","slug":"ref","content":"[Target](/target.md)"}`)
	updateRec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+ref.ID, updateBody)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on page update, got %d - %s", updateRec.Code, updateRec.Body.String())
	}

	previewBody := strings.NewReader(`{"kind":"rename","title":"Target","slug":"target-renamed"}`)
	previewRec := authenticatedRequest(t, router, http.MethodPost, "/api/pages/"+target.ID+"/refactor/preview", previewBody)
	if previewRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on refactor preview, got %d - %s", previewRec.Code, previewRec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(previewRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Invalid refactor preview JSON: %v", err)
	}

	if _, ok := resp["counts"]; !ok {
		t.Fatalf("Expected lowercase counts in response, got %v", resp)
	}
	if _, ok := resp["affectedPages"]; !ok {
		t.Fatalf("Expected lowercase affectedPages in response, got %v", resp)
	}
	if _, ok := resp["Counts"]; ok {
		t.Fatalf("Did not expect legacy Counts key in response, got %v", resp)
	}
	if _, ok := resp["AffectedPages"]; ok {
		t.Fatalf("Did not expect legacy AffectedPages key in response, got %v", resp)
	}

	counts, ok := resp["counts"].(map[string]any)
	if !ok {
		t.Fatalf("Expected counts object, got %T", resp["counts"])
	}
	if got := counts["affectedPages"]; got != float64(1) {
		t.Fatalf("Expected counts.affectedPages=1, got %v", got)
	}
	if _, ok := counts["matchedLinks"]; !ok {
		t.Fatalf("Expected counts.matchedLinks in response, got %v", counts)
	}

})

var _ = It("TestRefactorPreviewEndpoint_IsDisabledWhenFlagIsOff", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)

	router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            false,
		InjectCodeInHeader:      "",
		AllowInsecure:           true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		HideLinkMetadataSection: false,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		EnableLinkRefactor:      false,
	})

	target := createPageViaAPI(t, router, "Target", "target", nil, pageNodeKind())
	previewBody := strings.NewReader(`{"kind":"rename","title":"Target","slug":"target-renamed"}`)
	previewRec := authenticatedRequest(t, router, http.MethodPost, "/api/pages/"+target.ID+"/refactor/preview", previewBody)
	if previewRec.Code != http.StatusNotFound {
		t.Fatalf("Expected 404 when link refactor is disabled, got %d - %s", previewRec.Code, previewRec.Body.String())
	}

})

var _ = It("TestRefactorApply_UsesGitHistoryWithoutLegacyRevisionStorage", func() {
	t := GinkgoT()
	w := createWikiTestInstanceWithRevisionFlag(t, false)
	defer wrapCloseWithErrorCheck(w.Close, t)

	router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            false,
		InjectCodeInHeader:      "",
		AllowInsecure:           true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		HideLinkMetadataSection: false,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		EnableWorkspaceSync:     true,
		EnableLinkRefactor:      true,
	})

	target := createPageViaAPI(t, router, "Target", "target", nil, pageNodeKind())
	ref := createPageViaAPI(t, router, "Ref", "ref", nil, pageNodeKind())

	updateBody := strings.NewReader(`{"version":"` + ref.Version + `","title":"Ref","slug":"ref","content":"[Target](/target.md)"}`)
	updateRec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+ref.ID, updateBody)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on page update, got %d - %s", updateRec.Code, updateRec.Body.String())
	}

	applyBody := strings.NewReader(`{"kind":"rename","version":"` + target.Version + `","title":"Target","slug":"target-renamed","rewriteLinks":true}`)
	applyRec := authenticatedRequest(t, router, http.MethodPost, "/api/pages/"+target.ID+"/refactor/apply", applyBody)
	if applyRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on refactor apply, got %d - %s", applyRec.Code, applyRec.Body.String())
	}

	refPageRec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/"+ref.ID, strings.NewReader(""))
	if refPageRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on ref page fetch, got %d - %s", refPageRec.Code, refPageRec.Body.String())
	}

	var refPage map[string]any
	if err := json.Unmarshal(refPageRec.Body.Bytes(), &refPage); err != nil {
		t.Fatalf("Invalid ref page JSON: %v", err)
	}

	if got, _ := refPage["content"].(string); got != "[Target](/target-renamed.md)" {
		t.Fatalf("Expected rewritten ref content, got %q", got)
	}

	revisionsRec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/"+target.ID+"/revisions", strings.NewReader(""))
	if revisionsRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on Git-backed revisions endpoint, got %d - %s", revisionsRec.Code, revisionsRec.Body.String())
	}

	revisionsDir := filepath.Join(w.GetStorageDir(), ".leafwiki", "revisions")
	if _, err := os.Stat(revisionsDir); !os.IsNotExist(err) {
		t.Fatalf("Expected no revision storage directory, got err=%v", err)
	}

})

var _ = It("TestUploadAssetEndpoint_RejectsFilesExceedingConfiguredLimit", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)

	router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            false,
		InjectCodeInHeader:      "",
		AllowInsecure:           true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		HideLinkMetadataSection: false,
		MaxAssetUploadSizeBytes: 32,
	})

	page := createPageViaAPI(t, router, "Asset Limit Test", "asset-limit-test", nil, pageNodeKind())

	loginBody := `{"identifier": "admin", "password": "admin"}`
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("Failed to login: %d - %s", loginRec.Code, loginRec.Body.String())
	}

	cookies := loginRec.Result().Cookies()
	csrfToken := loginRec.Header().Get("X-CSRF-Token")
	if csrfToken == "" {
		for _, c := range cookies {
			if c.Name == "leafwiki_csrf" || c.Name == "__Host-leafwiki_csrf" {
				csrfToken = c.Value
				break
			}
		}
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "large.txt")
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}
	if _, err := part.Write([]byte(strings.Repeat("a", 128))); err != nil {
		t.Fatalf("Failed to write file content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Failed to close multipart writer: %v", err)
	}

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/pages/"+page.ID+"/assets", body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadReq.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		uploadReq.AddCookie(cookie)
	}

	uploadRec := httptest.NewRecorder()
	router.ServeHTTP(uploadRec, uploadReq)

	if uploadRec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("Expected 413 Request Entity Too Large, got %d - %s", uploadRec.Code, uploadRec.Body.String())
	}

	assetDir := filepath.Join(w.GetStorageDir(), "assets", page.ID)
	entries, err := os.ReadDir(assetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("Failed to read asset directory: %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("Expected no files after rejected upload, got %d", len(entries))
	}

})

var _ = It("TestSuggestSlugEndpoint", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstanceWithRevision(w, t)

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/slug-suggestion?title=NewPage", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Invalid JSON response: %v", err)
	}

	if resp["slug"] == "" {
		t.Errorf("Expected a slug suggestion, got: %v", resp)
	}

	if resp["slug"] != "newpage" {
		t.Errorf("Expected 'newpage' as slug suggestion, got: %v", resp)
	}

})

var _ = It("TestCancelImportPlanEndpoint", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	fileWriter, err := writer.CreateFormFile("file", "fixture-1.zip")
	if err != nil {
		t.Fatalf("CreateFormFile failed: %v", err)
	}

	zipFile, err := os.Open("../importer/fixtures/fixture-1.zip")
	if err != nil {
		t.Fatalf("Open fixture zip failed: %v", err)
	}
	defer wrapCloseWithErrorCheck(zipFile.Close, t)

	if _, err := io.Copy(fileWriter, zipFile); err != nil {
		t.Fatalf("Copy zip fixture failed: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("Close multipart writer failed: %v", err)
	}

	loginBody := `{"identifier": "admin", "password": "admin"}`
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("Failed to login: %d - %s", loginRec.Code, loginRec.Body.String())
	}

	loginRes := loginRec.Result()
	defer wrapCloseWithErrorCheck(loginRes.Body.Close, t)

	cookies := loginRes.Cookies()
	if len(cookies) == 0 {
		t.Fatalf("Expected auth cookies on login response, got none")
	}

	csrfToken := loginRec.Header().Get("X-CSRF-Token")
	if csrfToken == "" {
		for _, c := range cookies {
			if c.Name == "leafwiki_csrf" || c.Name == "__Host-leafwiki_csrf" {
				csrfToken = c.Value
				break
			}
		}
	}

	if csrfToken == "" {
		t.Fatalf("Expected CSRF token after login, got none")
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/import/plan", &body)
	createReq.Header.Set("Content-Type", writer.FormDataContentType())
	createReq.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		createReq.AddCookie(cookie)
	}

	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusOK {
		t.Fatalf("Expected status 200 when creating import plan, got %d: %s", createRec.Code, createRec.Body.String())
	}

	cancelReq := httptest.NewRequest(http.MethodDelete, "/api/import/plan", nil)
	cancelReq.Header.Set("Content-Type", "application/json")
	cancelReq.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		cancelReq.AddCookie(cookie)
	}

	cancelRec := httptest.NewRecorder()
	router.ServeHTTP(cancelRec, cancelReq)

	if cancelRec.Code != http.StatusOK {
		t.Fatalf("Expected status 200 when canceling import plan, got %d: %s", cancelRec.Code, cancelRec.Body.String())
	}
	if got := strings.TrimSpace(cancelRec.Body.String()); got != "null" {
		t.Fatalf("Expected null response body when clearing import plan, got %q", got)
	}

	getRec := authenticatedRequest(t, router, http.MethodGet, "/api/import/plan", nil)
	if getRec.Code != http.StatusNotFound {
		t.Fatalf("Expected status 404 when fetching canceled import plan, got %d: %s", getRec.Code, getRec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(getRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Invalid JSON response: %v", err)
	}

	errObj, ok := resp["error"].(map[string]interface{})
	if !ok || errObj["code"] == nil {
		t.Fatalf("Expected structured error response after canceling import plan, got: %v", resp)
	}

})

var _ = It("TestImportExecuteEndpoint_WithZipUpload_ImportsPagesLinksAndAssets", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	fixtureDir := importerFixturePathForHTTPTests(t, "link-assets-package")
	zipBytes := createZipFromDir(t, fixtureDir)

	loginBody := `{"identifier": "admin", "password": "admin"}`
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("Failed to login: %d - %s", loginRec.Code, loginRec.Body.String())
	}

	loginRes := loginRec.Result()
	defer wrapCloseWithErrorCheck(loginRes.Body.Close, t)

	cookies := loginRes.Cookies()
	if len(cookies) == 0 {
		t.Fatalf("Expected auth cookies on login response, got none")
	}

	csrfToken := loginRec.Header().Get("X-CSRF-Token")
	if csrfToken == "" {
		for _, c := range cookies {
			if c.Name == "leafwiki_csrf" || c.Name == "__Host-leafwiki_csrf" {
				csrfToken = c.Value
				break
			}
		}
	}
	if csrfToken == "" {
		t.Fatalf("Expected CSRF token after login, got none")
	}

	var planBody bytes.Buffer
	planWriter := multipart.NewWriter(&planBody)
	fileWriter, err := planWriter.CreateFormFile("file", "link-assets-package.zip")
	if err != nil {
		t.Fatalf("CreateFormFile failed: %v", err)
	}
	if _, err := fileWriter.Write(zipBytes); err != nil {
		t.Fatalf("Write zip bytes failed: %v", err)
	}
	if err := planWriter.Close(); err != nil {
		t.Fatalf("Close multipart writer failed: %v", err)
	}

	planReq := httptest.NewRequest(http.MethodPost, "/api/import/plan", &planBody)
	planReq.Header.Set("Content-Type", planWriter.FormDataContentType())
	planReq.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		planReq.AddCookie(cookie)
	}

	planRec := httptest.NewRecorder()
	router.ServeHTTP(planRec, planReq)

	if planRec.Code != http.StatusOK {
		t.Fatalf("Expected status 200 when creating import plan, got %d: %s", planRec.Code, planRec.Body.String())
	}

	var planResp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(planRec.Body.Bytes(), &planResp); err != nil {
		t.Fatalf("Invalid import plan response JSON: %v", err)
	}
	if len(planResp.Items) != 5 {
		t.Fatalf("expected 5 plan items, got %d", len(planResp.Items))
	}

	execReq := httptest.NewRequest(http.MethodPost, "/api/import/execute", strings.NewReader(""))
	execReq.Header.Set("Content-Type", "application/json")
	execReq.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		execReq.AddCookie(cookie)
	}

	execRec := httptest.NewRecorder()
	router.ServeHTTP(execRec, execReq)

	if execRec.Code != http.StatusAccepted {
		t.Fatalf("Expected status 202 when starting import, got %d: %s", execRec.Code, execRec.Body.String())
	}

	var execResp struct {
		ImportedCount   int    `json:"imported_count"`
		SkippedCount    int    `json:"skipped_count"`
		ExecutionStatus string `json:"execution_status"`
		ExecutionResult *struct {
			ImportedCount int `json:"imported_count"`
			SkippedCount  int `json:"skipped_count"`
		} `json:"execution_result"`
	}
	if err := json.Unmarshal(execRec.Body.Bytes(), &execResp); err != nil {
		t.Fatalf("Invalid import execute response JSON: %v", err)
	}

	if execResp.ExecutionStatus != "running" {
		t.Fatalf("expected running execution status, got %q", execResp.ExecutionStatus)
	}

	var completedResp struct {
		ExecutionStatus string `json:"execution_status"`
		ExecutionResult *struct {
			ImportedCount int `json:"imported_count"`
			SkippedCount  int `json:"skipped_count"`
		} `json:"execution_result"`
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		statusReq := httptest.NewRequest(http.MethodGet, "/api/import/plan", nil)
		for _, cookie := range cookies {
			statusReq.AddCookie(cookie)
		}

		statusRec := httptest.NewRecorder()
		router.ServeHTTP(statusRec, statusReq)

		if statusRec.Code != http.StatusOK {
			t.Fatalf("Expected status 200 when fetching import plan, got %d: %s", statusRec.Code, statusRec.Body.String())
		}
		if err := json.Unmarshal(statusRec.Body.Bytes(), &completedResp); err != nil {
			t.Fatalf("Invalid import status response JSON: %v", err)
		}
		if completedResp.ExecutionStatus == "completed" {
			break
		}

		time.Sleep(50 * time.Millisecond)
	}

	if completedResp.ExecutionStatus != "completed" || completedResp.ExecutionResult == nil {
		t.Fatalf("expected completed execution result, got %#v", completedResp)
	}
	if completedResp.ExecutionResult.ImportedCount != 4 || completedResp.ExecutionResult.SkippedCount != 1 {
		t.Fatalf(
			"unexpected execution result: imported=%d skipped=%d",
			completedResp.ExecutionResult.ImportedCount,
			completedResp.ExecutionResult.SkippedCount,
		)
	}

	setupPage := getPageByPathViaAPI(t, router, "guides/setup")
	for _, expected := range []string{
		"[Relative MD](/reference/endpoints.md)",
		"[Absolute MD](/reference/endpoints.md)",
		"[Container](/guides)",
		"[Endpoints](/reference/endpoints.md)",
		"[API Alias](/reference/endpoints.md)",
		"![Relative Image](/assets/" + setupPage.ID + "/logo.png)",
		"[Manual](/assets/" + setupPage.ID + "/manual.pdf)",
	} {
		if !strings.Contains(setupPage.Content, expected) {
			t.Fatalf("expected setup content to contain %q, got:\n%s", expected, setupPage.Content)
		}
	}

	assets := listAssetsViaAPI(t, router, setupPage.ID)
	if len(assets) != 2 {
		t.Fatalf("expected 2 uploaded assets, got %#v", assets)
	}
	_ = getPageByPathViaAPI(t, router, "reference/api-1")

})

var _ = It("TestImportExecuteEndpoint_UsesConfiguredAssetUploadLimit", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstanceWithMaxAssetUploadSize(w, t, 1024)

	fixtureDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fixtureDir, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir fixture dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fixtureDir, "docs", "setup.md"), []byte("# Setup\n\n[Manual](./manual.pdf)\n"), 0o644); err != nil {
		t.Fatalf("write markdown fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fixtureDir, "docs", "manual.pdf"), bytes.Repeat([]byte("a"), 2048), 0o644); err != nil {
		t.Fatalf("write oversized asset fixture: %v", err)
	}

	zipBytes := createZipFromDir(t, fixtureDir)

	loginBody := `{"identifier": "admin", "password": "admin"}`
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("Failed to login: %d - %s", loginRec.Code, loginRec.Body.String())
	}

	loginRes := loginRec.Result()
	defer wrapCloseWithErrorCheck(loginRes.Body.Close, t)

	cookies := loginRes.Cookies()
	if len(cookies) == 0 {
		t.Fatalf("Expected auth cookies on login response, got none")
	}

	csrfToken := loginRec.Header().Get("X-CSRF-Token")
	if csrfToken == "" {
		for _, c := range cookies {
			if c.Name == "leafwiki_csrf" || c.Name == "__Host-leafwiki_csrf" {
				csrfToken = c.Value
				break
			}
		}
	}
	if csrfToken == "" {
		t.Fatalf("Expected CSRF token after login, got none")
	}

	var planBody bytes.Buffer
	planWriter := multipart.NewWriter(&planBody)
	fileWriter, err := planWriter.CreateFormFile("file", "oversized-assets.zip")
	if err != nil {
		t.Fatalf("CreateFormFile failed: %v", err)
	}
	if _, err := fileWriter.Write(zipBytes); err != nil {
		t.Fatalf("Write zip bytes failed: %v", err)
	}
	if err := planWriter.Close(); err != nil {
		t.Fatalf("Close multipart writer failed: %v", err)
	}

	planReq := httptest.NewRequest(http.MethodPost, "/api/import/plan", &planBody)
	planReq.Header.Set("Content-Type", planWriter.FormDataContentType())
	planReq.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		planReq.AddCookie(cookie)
	}

	planRec := httptest.NewRecorder()
	router.ServeHTTP(planRec, planReq)

	if planRec.Code != http.StatusOK {
		t.Fatalf("Expected status 200 when creating import plan, got %d: %s", planRec.Code, planRec.Body.String())
	}

	execReq := httptest.NewRequest(http.MethodPost, "/api/import/execute", strings.NewReader(""))
	execReq.Header.Set("Content-Type", "application/json")
	execReq.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		execReq.AddCookie(cookie)
	}

	execRec := httptest.NewRecorder()
	router.ServeHTTP(execRec, execReq)

	if execRec.Code != http.StatusAccepted {
		t.Fatalf("Expected status 202 when starting import, got %d: %s", execRec.Code, execRec.Body.String())
	}

	var execResp struct {
		ExecutionStatus string `json:"execution_status"`
	}
	if err := json.Unmarshal(execRec.Body.Bytes(), &execResp); err != nil {
		t.Fatalf("Invalid import execute response JSON: %v", err)
	}
	if execResp.ExecutionStatus != "running" {
		t.Fatalf("expected running execution status, got %q", execResp.ExecutionStatus)
	}

	var completedResp struct {
		ExecutionStatus string `json:"execution_status"`
		ExecutionResult *struct {
			ImportedCount int `json:"imported_count"`
			SkippedCount  int `json:"skipped_count"`
			Items         []struct {
				Error *string `json:"error"`
			} `json:"items"`
		} `json:"execution_result"`
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		statusReq := httptest.NewRequest(http.MethodGet, "/api/import/plan", nil)
		for _, cookie := range cookies {
			statusReq.AddCookie(cookie)
		}

		statusRec := httptest.NewRecorder()
		router.ServeHTTP(statusRec, statusReq)

		if statusRec.Code != http.StatusOK {
			t.Fatalf("Expected status 200 when fetching import plan, got %d: %s", statusRec.Code, statusRec.Body.String())
		}
		if err := json.Unmarshal(statusRec.Body.Bytes(), &completedResp); err != nil {
			t.Fatalf("Invalid import status response JSON: %v", err)
		}
		if completedResp.ExecutionStatus == "completed" {
			break
		}

		time.Sleep(50 * time.Millisecond)
	}

	if completedResp.ExecutionStatus != "completed" || completedResp.ExecutionResult == nil {
		t.Fatalf("expected completed execution result, got %#v", completedResp)
	}
	if completedResp.ExecutionResult.ImportedCount != 0 || completedResp.ExecutionResult.SkippedCount != 1 {
		t.Fatalf(
			"unexpected execution result: imported=%d skipped=%d",
			completedResp.ExecutionResult.ImportedCount,
			completedResp.ExecutionResult.SkippedCount,
		)
	}
	if len(completedResp.ExecutionResult.Items) != 1 || completedResp.ExecutionResult.Items[0].Error == nil || !strings.Contains(*completedResp.ExecutionResult.Items[0].Error, "file too large") {
		t.Fatalf("expected import error about configured asset limit, got %#v", completedResp.ExecutionResult.Items)
	}

})

var _ = It("TestSuggestSlugEndpoint_MissingTitle", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/slug-suggestion", nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected status 400, got %d", rec.Code)
	}

})

var _ = It("TestDeletePageEndpoint", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	page := createPageViaAPI(t, router, "Delete Me", "delete-me", nil, pageNodeKind())
	rec := authenticatedRequest(t, router, http.MethodDelete, "/api/pages/"+page.ID+"?version="+page.Version, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rec.Code)
	}

	getRec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/"+page.ID, nil)
	if getRec.Code != http.StatusNotFound {
		t.Fatalf("Expected deleted page to return 404, got %d", getRec.Code)
	}

})

var _ = It("TestDeletePageEndpoint_NotFound", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	rec := authenticatedRequest(t, router, http.MethodDelete, "/api/pages/not-found-id", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("Expected 404 Not Found, got %d", rec.Code)
	}

})

var _ = It("TestDeletePageEndpoint_HasChildren", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	parent := createPageViaAPI(t, router, "Parent", "parent", nil, pageNodeKind())
	createPageViaAPI(t, router, "Child", "child", &parent.ID, pageNodeKind())

	rec := authenticatedRequest(t, router, http.MethodDelete, "/api/pages/"+parent.ID+"?version="+parent.Version, nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 Bad Request, got %d", rec.Code)
	}

})

var _ = It("TestDeletePageEndpoint_Recursive", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	parent := createPageViaAPI(t, router, "Parent", "parent", nil, pageNodeKind())
	createPageViaAPI(t, router, "Child", "child", &parent.ID, pageNodeKind())

	rec := authenticatedRequest(t, router, http.MethodDelete, "/api/pages/"+parent.ID+"?recursive=true&version="+parent.Version, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rec.Code)
	}

	getRec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/"+parent.ID, nil)
	if getRec.Code != http.StatusNotFound {
		t.Fatalf("Expected deleted page to return 404, got %d", getRec.Code)
	}

})

var _ = It("TestUpdatePageEndpoint", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	page := createPageViaAPI(t, router, "Original Title", "original-title", nil, pageNodeKind())

	payload := map[string]string{
		"version": page.Version,
		"title":   "Updated Title",
		"slug":    "updated-title",
		"content": "# Updated Content\nWith **Markdown** support.",
	}
	body, _ := json.Marshal(payload)

	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Invalid JSON response: %v", err)
	}

	if resp["title"] != "Updated Title" {
		t.Errorf("Expected updated title, got %q", resp["title"])
	}
	if resp["slug"] != "updated-title" {
		t.Errorf("Expected updated slug, got %q", resp["slug"])
	}
	if resp["content"] != "# Updated Content\nWith **Markdown** support." {
		t.Errorf("Expected updated content, got %q", resp["content"])
	}

})

var _ = It("TestUpdatePageEndpoint_WritesTagsAndStringProperties", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	page := createPageViaAPI(t, router, "Original Title", "original-title", nil, pageNodeKind())

	payload := map[string]interface{}{
		"version": page.Version,
		"title":   "Updated Title",
		"slug":    "updated-title",
		"content": "# Updated Content",
		"tags":    []string{"React", "TypeScript"},
		"properties": map[string]string{
			"status": "published",
			"author": "alice",
		},
	}
	body, _ := json.Marshal(payload)

	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
	}

	getRec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/"+page.ID, nil)
	if getRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on get, got %d", getRec.Code)
	}

	var fetched apiPage
	if err := json.Unmarshal(getRec.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("Invalid get response JSON: %v", err)
	}

	if len(fetched.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %#v", fetched.Tags)
	}
	if fetched.Tags[0] != "react" || fetched.Tags[1] != "typescript" {
		t.Fatalf("expected lowercase normalized tags, got %#v", fetched.Tags)
	}
	if fetched.Properties["status"] != "published" {
		t.Fatalf("expected status=published, got %#v", fetched.Properties)
	}
	if fetched.Properties["author"] != "alice" {
		t.Fatalf("expected author=alice, got %#v", fetched.Properties)
	}

})

var _ = It("TestUpdatePageEndpoint_RemovesTagsWhenEmptyListIsSent", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	page := createPageViaAPI(t, router, "Original Title", "original-title", nil, pageNodeKind())

	firstPayload := map[string]interface{}{
		"version": page.Version,
		"title":   "Original Title",
		"slug":    "original-title",
		"content": "# Updated Content",
		"tags":    []string{"React", "TypeScript"},
	}
	firstBody, _ := json.Marshal(firstPayload)

	firstRec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(firstBody)))
	if firstRec.Code != http.StatusOK {
		t.Fatalf("Expected first update to return 200 OK, got %d - %s", firstRec.Code, firstRec.Body.String())
	}

	var updated apiPage
	if err := json.Unmarshal(firstRec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("Invalid first update response JSON: %v", err)
	}

	secondPayload := map[string]interface{}{
		"version": updated.Version,
		"title":   updated.Title,
		"slug":    updated.Slug,
		"content": updated.Content,
		"tags":    []string{},
	}
	secondBody, _ := json.Marshal(secondPayload)

	secondRec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(secondBody)))
	if secondRec.Code != http.StatusOK {
		t.Fatalf("Expected second update to return 200 OK, got %d - %s", secondRec.Code, secondRec.Body.String())
	}

	getRec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/"+page.ID, nil)
	if getRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on get, got %d", getRec.Code)
	}

	var fetched apiPage
	if err := json.Unmarshal(getRec.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("Invalid get response JSON: %v", err)
	}

	if len(fetched.Tags) != 0 {
		t.Fatalf("expected tags to be removed, got %#v", fetched.Tags)
	}

	tagsRec := authenticatedRequest(t, router, http.MethodGet, "/api/tags?q=react&limit=20", nil)
	if tagsRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK from tags endpoint, got %d - %s", tagsRec.Code, tagsRec.Body.String())
	}

	var tagsResp []map[string]interface{}
	if err := json.Unmarshal(tagsRec.Body.Bytes(), &tagsResp); err != nil {
		t.Fatalf("Invalid tags response JSON: %v", err)
	}

	for _, entry := range tagsResp {
		if entry["tag"] == "react" {
			t.Fatalf("expected react tag to be removed from index, got %#v", tagsResp)
		}
	}

})

var _ = It("TestUpdatePageEndpoint_PreservesTagsAndPropertiesWhenOmittedAndClearsWhenExplicitEmpty", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	page := createPageViaAPI(t, router, "Metadata Preserve", "metadata-preserve", nil, pageNodeKind())

	firstPayload := map[string]interface{}{
		"version": page.Version,
		"title":   page.Title,
		"slug":    page.Slug,
		"content": "# Metadata Preserve\n\nFirst",
		"tags":    []string{"React"},
		"properties": map[string]string{
			"status": "draft",
		},
	}
	firstBody, _ := json.Marshal(firstPayload)
	firstRec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(firstBody)))
	if firstRec.Code != http.StatusOK {
		t.Fatalf("Expected first update to return 200 OK, got %d - %s", firstRec.Code, firstRec.Body.String())
	}
	var firstUpdated apiPage
	if err := json.Unmarshal(firstRec.Body.Bytes(), &firstUpdated); err != nil {
		t.Fatalf("Invalid first update response JSON: %v", err)
	}
	rawAfterFirstBytes, err := os.ReadFile(filepath.Join(w.GetRootDir(), "metadata-preserve.md"))
	if err != nil {
		t.Fatalf("ReadFile first metadata update: %v", err)
	}
	rawAfterFirst := string(rawAfterFirstBytes)
	if !strings.HasPrefix(rawAfterFirst, "<!-- leafwiki\n") {
		t.Fatalf("HTTP update should write canonical LeafWiki metadata, got: %q", rawAfterFirst)
	}
	if strings.HasPrefix(rawAfterFirst, "---\n") {
		t.Fatalf("HTTP update should not write legacy YAML frontmatter, got: %q", rawAfterFirst)
	}
	firstDoc, _, err := markdown.ParsePageDocument(rawAfterFirst)
	if err != nil {
		t.Fatalf("ParsePageDocument first metadata update: %v", err)
	}
	if len(firstDoc.Metadata.Tags) != 1 || firstDoc.Metadata.Tags[0] != "react" {
		t.Fatalf("first raw tags = %#v, want [react]", firstDoc.Metadata.Tags)
	}
	if firstDoc.Metadata.Fields["status"] != "draft" {
		t.Fatalf("first raw fields = %#v, want status=draft", firstDoc.Metadata.Fields)
	}

	metadataOnlyPayload := map[string]interface{}{
		"version": firstUpdated.Version,
		"title":   firstUpdated.Title,
		"slug":    firstUpdated.Slug,
		"tags":    []string{"Ready"},
		"properties": map[string]string{
			"status": "ready",
		},
	}
	metadataOnlyBody, _ := json.Marshal(metadataOnlyPayload)
	metadataOnlyRec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(metadataOnlyBody)))
	if metadataOnlyRec.Code != http.StatusOK {
		t.Fatalf("Expected metadata-only update to return 200 OK, got %d - %s", metadataOnlyRec.Code, metadataOnlyRec.Body.String())
	}
	var metadataOnlyUpdated apiPage
	if err := json.Unmarshal(metadataOnlyRec.Body.Bytes(), &metadataOnlyUpdated); err != nil {
		t.Fatalf("Invalid metadata-only update response JSON: %v", err)
	}
	if metadataOnlyUpdated.Content != "# Metadata Preserve\n\nFirst" {
		t.Fatalf("expected metadata-only update to preserve body, got %q", metadataOnlyUpdated.Content)
	}
	if len(metadataOnlyUpdated.Tags) != 1 || metadataOnlyUpdated.Tags[0] != "ready" {
		t.Fatalf("expected metadata-only tags to update, got %#v", metadataOnlyUpdated.Tags)
	}
	if metadataOnlyUpdated.Properties["status"] != "ready" {
		t.Fatalf("expected metadata-only properties to update, got %#v", metadataOnlyUpdated.Properties)
	}

	omittedPayload := map[string]interface{}{
		"version": metadataOnlyUpdated.Version,
		"title":   metadataOnlyUpdated.Title,
		"slug":    metadataOnlyUpdated.Slug,
		"content": "# Metadata Preserve\n\nSecond",
	}
	omittedBody, _ := json.Marshal(omittedPayload)
	omittedRec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(omittedBody)))
	if omittedRec.Code != http.StatusOK {
		t.Fatalf("Expected omitted metadata update to return 200 OK, got %d - %s", omittedRec.Code, omittedRec.Body.String())
	}
	var omittedUpdated apiPage
	if err := json.Unmarshal(omittedRec.Body.Bytes(), &omittedUpdated); err != nil {
		t.Fatalf("Invalid omitted update response JSON: %v", err)
	}
	if len(omittedUpdated.Tags) != 1 || omittedUpdated.Tags[0] != "ready" {
		t.Fatalf("expected omitted tags to be preserved, got %#v", omittedUpdated.Tags)
	}
	if omittedUpdated.Properties["status"] != "ready" {
		t.Fatalf("expected omitted properties to be preserved, got %#v", omittedUpdated.Properties)
	}

	clearPayload := map[string]interface{}{
		"version":    omittedUpdated.Version,
		"title":      omittedUpdated.Title,
		"slug":       omittedUpdated.Slug,
		"content":    "# Metadata Preserve\n\nThird",
		"tags":       []string{},
		"properties": map[string]string{},
	}
	clearBody, _ := json.Marshal(clearPayload)
	clearRec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(clearBody)))
	if clearRec.Code != http.StatusOK {
		t.Fatalf("Expected explicit clear update to return 200 OK, got %d - %s", clearRec.Code, clearRec.Body.String())
	}
	var cleared apiPage
	if err := json.Unmarshal(clearRec.Body.Bytes(), &cleared); err != nil {
		t.Fatalf("Invalid clear update response JSON: %v", err)
	}
	if len(cleared.Tags) != 0 {
		t.Fatalf("expected explicit empty tags to clear metadata, got %#v", cleared.Tags)
	}
	if len(cleared.Properties) != 0 {
		t.Fatalf("expected explicit empty properties to clear metadata, got %#v", cleared.Properties)
	}
	rawAfterClearBytes, err := os.ReadFile(filepath.Join(w.GetRootDir(), "metadata-preserve.md"))
	if err != nil {
		t.Fatalf("ReadFile clear metadata update: %v", err)
	}
	rawAfterClear := string(rawAfterClearBytes)
	if !strings.HasPrefix(rawAfterClear, "<!-- leafwiki\n") || strings.HasPrefix(rawAfterClear, "---\n") {
		t.Fatalf("clear update should keep canonical storage, got: %q", rawAfterClear)
	}
	clearDoc, _, err := markdown.ParsePageDocument(rawAfterClear)
	if err != nil {
		t.Fatalf("ParsePageDocument clear metadata update: %v", err)
	}
	if len(clearDoc.Metadata.Tags) != 0 || len(clearDoc.Metadata.Fields) != 0 {
		t.Fatalf("clear raw metadata = tags %#v fields %#v, want both empty", clearDoc.Metadata.Tags, clearDoc.Metadata.Fields)
	}

})

var _ = It("TestUpdatePageEndpoint_IndexesTagsForTagsEndpoint", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	page := createPageViaAPI(t, router, "Original Title", "original-title", nil, pageNodeKind())

	payload := map[string]interface{}{
		"version": page.Version,
		"title":   "Updated Title",
		"slug":    "updated-title",
		"content": "# Updated Content",
		"tags":    []string{"react", "typescript"},
	}
	body, _ := json.Marshal(payload)

	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
	}

	tagsRec := authenticatedRequest(t, router, http.MethodGet, "/api/tags?q=react&limit=20", nil)
	if tagsRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK from tags endpoint, got %d - %s", tagsRec.Code, tagsRec.Body.String())
	}

	var tagsResp []map[string]interface{}
	if err := json.Unmarshal(tagsRec.Body.Bytes(), &tagsResp); err != nil {
		t.Fatalf("Invalid tags response JSON: %v", err)
	}

	if len(tagsResp) == 0 {
		t.Fatalf("expected indexed tags, got empty response")
	}
	if tagsResp[0]["tag"] != "react" {
		t.Fatalf("expected first indexed tag to be react, got %#v", tagsResp)
	}

})

var _ = It("TestGetTagsEndpoint_CountsSuggestionsWithinSelectedTags", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	pageA := createPageViaAPI(t, router, "Page A", "page-a", nil, pageNodeKind())
	pageB := createPageViaAPI(t, router, "Page B", "page-b", nil, pageNodeKind())
	pageC := createPageViaAPI(t, router, "Page C", "page-c", nil, pageNodeKind())

	updatePageTags := func(page *apiPage, title, slug string, tags []string) {
		payload := map[string]interface{}{
			"version": page.Version,
			"title":   title,
			"slug":    slug,
			"content": "# Content",
			"tags":    tags,
		}
		body, _ := json.Marshal(payload)
		rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
		}
	}

	updatePageTags(pageA, "Page A", "page-a", []string{"react", "typescript"})
	updatePageTags(pageB, "Page B", "page-b", []string{"react", "testing"})
	updatePageTags(pageC, "Page C", "page-c", []string{"react", "typescript"})

	tagsRec := authenticatedRequest(t, router, http.MethodGet, "/api/tags?q=t&limit=20&selected=react", nil)
	if tagsRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK from tags endpoint, got %d - %s", tagsRec.Code, tagsRec.Body.String())
	}

	var tagsResp []map[string]interface{}
	if err := json.Unmarshal(tagsRec.Body.Bytes(), &tagsResp); err != nil {
		t.Fatalf("Invalid tags response JSON: %v", err)
	}

	if len(tagsResp) != 2 {
		t.Fatalf("expected 2 suggestion tags, got %#v", tagsResp)
	}
	if tagsResp[0]["tag"] != "typescript" || tagsResp[0]["count"] != float64(2) {
		t.Fatalf("expected first suggestion to be typescript with count 2, got %#v", tagsResp[0])
	}
	if tagsResp[1]["tag"] != "testing" || tagsResp[1]["count"] != float64(1) {
		t.Fatalf("expected second suggestion to be testing with count 1, got %#v", tagsResp[1])
	}

})

var _ = It("TestGetTagsEndpoint_AcceptsRepeatedSelectedParams", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	page := createPageViaAPI(t, router, "Page A", "page-a", nil, pageNodeKind())

	payload := map[string]interface{}{
		"version": page.Version,
		"title":   "Page A",
		"slug":    "page-a",
		"content": "# Content",
		"tags":    []string{"react", "typescript", "testing"},
	}
	body, _ := json.Marshal(payload)
	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
	}

	tagsRec := authenticatedRequest(t, router, http.MethodGet, "/api/tags?q=t&limit=20&selected=react&selected=typescript", nil)
	if tagsRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK from tags endpoint, got %d - %s", tagsRec.Code, tagsRec.Body.String())
	}

	var tagsResp []map[string]interface{}
	if err := json.Unmarshal(tagsRec.Body.Bytes(), &tagsResp); err != nil {
		t.Fatalf("Invalid tags response JSON: %v", err)
	}

	if len(tagsResp) != 1 {
		t.Fatalf("expected 1 suggestion tag, got %#v", tagsResp)
	}
	if tagsResp[0]["tag"] != "testing" || tagsResp[0]["count"] != float64(1) {
		t.Fatalf("expected testing with count 1, got %#v", tagsResp[0])
	}

})

var _ = It("TestSearchEndpoint_FiltersResultsByTags", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	reactPage := createPageViaAPI(t, router, "React Search Match", "react-search-match", nil, pageNodeKind())
	plainPage := createPageViaAPI(t, router, "Plain Search Match", "plain-search-match", nil, pageNodeKind())

	updatePage := func(page *apiPage, title, slug, content string, tags []string) {
		payload := map[string]interface{}{
			"version": page.Version,
			"title":   title,
			"slug":    slug,
			"content": content,
			"tags":    tags,
		}
		body, _ := json.Marshal(payload)
		rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
		}
	}

	updatePage(reactPage, "React Search Match", "react-search-match", "Body with shared search token.", []string{"react"})
	updatePage(plainPage, "Plain Search Match", "plain-search-match", "Body with shared search token.", []string{"docs"})

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/search?q=shared%20search&tags=react", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Count     int `json:"count"`
		TagFacets []struct {
			Tag   string `json:"tag"`
			Count int    `json:"count"`
		} `json:"tag_facets"`
		Items []struct {
			PageID string `json:"page_id"`
			Title  string `json:"title"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Invalid search response JSON: %v", err)
	}

	if resp.Count != 1 {
		t.Fatalf("expected 1 filtered search result, got %d", resp.Count)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 filtered search item, got %d", len(resp.Items))
	}
	if resp.Items[0].PageID != reactPage.ID {
		t.Fatalf("expected filtered page %q, got %#v", reactPage.ID, resp.Items)
	}
	if len(resp.TagFacets) != 1 || resp.TagFacets[0].Tag != "react" || resp.TagFacets[0].Count != 1 {
		t.Fatalf("expected tag facets to contain only react=1, got %#v", resp.TagFacets)
	}

})

var _ = It("TestSearchEndpoint_ReturnsTagMatchesWithoutQuery", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	reactPage := createPageViaAPI(t, router, "React Tag Match", "react-tag-match", nil, pageNodeKind())
	plainPage := createPageViaAPI(t, router, "Plain Tag Match", "plain-tag-match", nil, pageNodeKind())

	updatePage := func(page *apiPage, title, slug, content string, tags []string) {
		payload := map[string]interface{}{
			"version": page.Version,
			"title":   title,
			"slug":    slug,
			"content": content,
			"tags":    tags,
		}
		body, _ := json.Marshal(payload)
		rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
		}
	}

	updatePage(reactPage, "React Tag Match", "react-tag-match", "Body without search token.", []string{"react"})
	updatePage(plainPage, "Plain Tag Match", "plain-tag-match", "Body without search token.", []string{"docs"})

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/search?tags=react", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Count     int `json:"count"`
		TagFacets []struct {
			Tag   string `json:"tag"`
			Count int    `json:"count"`
		} `json:"tag_facets"`
		Items []struct {
			PageID string `json:"page_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Invalid search response JSON: %v", err)
	}

	if resp.Count != 1 {
		t.Fatalf("expected 1 tag-only result, got %d", resp.Count)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 tag-only item, got %d", len(resp.Items))
	}
	if resp.Items[0].PageID != reactPage.ID {
		t.Fatalf("expected tag-only page %q, got %#v", reactPage.ID, resp.Items)
	}
	if len(resp.TagFacets) != 1 || resp.TagFacets[0].Tag != "react" || resp.TagFacets[0].Count != 1 {
		t.Fatalf("expected tag facets to contain only react=1, got %#v", resp.TagFacets)
	}

})

var _ = It("TestSearchEndpoint_NormalizesTagOnlyPaginationBounds", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	page := createPageViaAPI(t, router, "React Tag Match", "react-tag-match-bounds", nil, pageNodeKind())

	payload := map[string]interface{}{
		"version": page.Version,
		"title":   "React Tag Match",
		"slug":    "react-tag-match-bounds",
		"content": "Body without search token.",
		"tags":    []string{"react"},
	}
	body, _ := json.Marshal(payload)
	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
	}

	boundsRec := authenticatedRequest(t, router, http.MethodGet, "/api/search?tags=react&offset=-1&limit=0", nil)
	if boundsRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d - %s", boundsRec.Code, boundsRec.Body.String())
	}

	var resp struct {
		Count  int `json:"count"`
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
		Items  []struct {
			PageID string `json:"page_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(boundsRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Invalid search response JSON: %v", err)
	}

	if resp.Count != 1 {
		t.Fatalf("expected 1 tag-only result, got %d", resp.Count)
	}
	if resp.Offset != 0 {
		t.Fatalf("expected offset to normalize to 0, got %d", resp.Offset)
	}
	if resp.Limit != 20 {
		t.Fatalf("expected limit to normalize to 20, got %d", resp.Limit)
	}
	if len(resp.Items) != 1 || resp.Items[0].PageID != page.ID {
		t.Fatalf("expected normalized request to return page %q, got %#v", page.ID, resp.Items)
	}

})

var _ = It("TestSearchEndpoint_TagFacetsShrinkWithAdditionalFilters", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	type searchResponse struct {
		Count     int `json:"count"`
		TagFacets []struct {
			Tag   string `json:"tag"`
			Count int    `json:"count"`
		} `json:"tag_facets"`
	}

	updatePage := func(page *apiPage, title, slug, content string, tags []string) {
		payload := map[string]interface{}{
			"version": page.Version,
			"title":   title,
			"slug":    slug,
			"content": content,
			"tags":    tags,
		}
		body, _ := json.Marshal(payload)
		rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
		}
	}

	pageOne := createPageViaAPI(t, router, "Facet Alpha", "facet-alpha", nil, pageNodeKind())
	pageTwo := createPageViaAPI(t, router, "Facet Beta", "facet-beta", nil, pageNodeKind())
	pageThree := createPageViaAPI(t, router, "Facet Gamma", "facet-gamma", nil, pageNodeKind())

	updatePage(pageOne, "Facet Alpha", "facet-alpha", "Body with facet token.", []string{"alpha", "shared"})
	updatePage(pageTwo, "Facet Beta", "facet-beta", "Body with facet token.", []string{"beta", "shared"})
	updatePage(pageThree, "Facet Gamma", "facet-gamma", "Body with facet token.", []string{"alpha", "shared", "narrow"})

	baseRec := authenticatedRequest(t, router, http.MethodGet, "/api/search?q=facet%20token", nil)
	if baseRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d - %s", baseRec.Code, baseRec.Body.String())
	}

	var baseResp searchResponse
	if err := json.Unmarshal(baseRec.Body.Bytes(), &baseResp); err != nil {
		t.Fatalf("Invalid search response JSON: %v", err)
	}

	narrowRec := authenticatedRequest(t, router, http.MethodGet, "/api/search?q=facet%20token&tags=alpha", nil)
	if narrowRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d - %s", narrowRec.Code, narrowRec.Body.String())
	}

	var narrowResp searchResponse
	if err := json.Unmarshal(narrowRec.Body.Bytes(), &narrowResp); err != nil {
		t.Fatalf("Invalid filtered search response JSON: %v", err)
	}

	if baseResp.Count != 3 {
		t.Fatalf("expected 3 base results, got %d", baseResp.Count)
	}
	if narrowResp.Count != 2 {
		t.Fatalf("expected 2 narrowed results, got %d", narrowResp.Count)
	}

	baseFacets := map[string]int{}
	for _, facet := range baseResp.TagFacets {
		baseFacets[facet.Tag] = facet.Count
	}
	narrowFacets := map[string]int{}
	for _, facet := range narrowResp.TagFacets {
		narrowFacets[facet.Tag] = facet.Count
	}

	if len(baseFacets) != 4 {
		t.Fatalf("expected 4 base facets, got %#v", baseResp.TagFacets)
	}
	if len(narrowFacets) != 3 {
		t.Fatalf("expected 3 narrowed facets, got %#v", narrowResp.TagFacets)
	}
	if baseFacets["beta"] != 1 {
		t.Fatalf("expected base facets to include beta=1, got %#v", baseResp.TagFacets)
	}
	if _, ok := narrowFacets["beta"]; ok {
		t.Fatalf("expected beta to disappear after narrowing, got %#v", narrowResp.TagFacets)
	}
	if narrowFacets["alpha"] != 2 || narrowFacets["shared"] != 2 || narrowFacets["narrow"] != 1 {
		t.Fatalf("unexpected narrowed facets: %#v", narrowResp.TagFacets)
	}

})

var _ = It("TestGetPagesByTagsEndpoint_ReturnsExcerpt", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	page := createPageViaAPI(t, router, "Excerpt Page", "excerpt-page", nil, pageNodeKind())

	payload := map[string]interface{}{
		"version": page.Version,
		"title":   "Excerpt Page",
		"slug":    "excerpt-page",
		"content": "# Heading\n\nThis is a tagged page with useful excerpt text and a [link](/docs) inside the content.",
		"tags":    []string{"react"},
	}
	body, _ := json.Marshal(payload)

	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
	}

	pagesRec := authenticatedRequest(t, router, http.MethodGet, "/api/tags/pages?tags=react", nil)
	if pagesRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK from tags pages endpoint, got %d - %s", pagesRec.Code, pagesRec.Body.String())
	}

	var pagesResp []map[string]interface{}
	if err := json.Unmarshal(pagesRec.Body.Bytes(), &pagesResp); err != nil {
		t.Fatalf("Invalid pages response JSON: %v", err)
	}

	if len(pagesResp) != 1 {
		t.Fatalf("expected 1 tagged page, got %#v", pagesResp)
	}
	if pagesResp[0]["kind"] != string(tree.NodeKindPage) {
		t.Fatalf("expected tagged page kind page, got %#v", pagesResp[0]["kind"])
	}

	excerpt, _ := pagesResp[0]["excerpt"].(string)
	if excerpt == "" {
		t.Fatalf("expected excerpt to be present, got %#v", pagesResp[0])
	}
	if strings.Contains(excerpt, "#") {
		t.Fatalf("expected excerpt without markdown heading markers, got %q", excerpt)
	}
	if strings.Contains(excerpt, "[link]") {
		t.Fatalf("expected excerpt without markdown link syntax, got %q", excerpt)
	}
	if !strings.Contains(excerpt, "This is a tagged page with useful excerpt text") {
		t.Fatalf("expected excerpt to contain page text, got %q", excerpt)
	}

})

var _ = It("TestGetPagesByTagsEndpoint_AcceptsRepeatedTagsParams", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	pageA := createPageViaAPI(t, router, "Page A", "page-a", nil, pageNodeKind())
	pageB := createPageViaAPI(t, router, "Page B", "page-b", nil, pageNodeKind())

	updatePageTags := func(page *apiPage, title, slug string, tags []string) {
		payload := map[string]interface{}{
			"version": page.Version,
			"title":   title,
			"slug":    slug,
			"content": "# Content",
			"tags":    tags,
		}
		body, _ := json.Marshal(payload)
		rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
		}
	}

	updatePageTags(pageA, "Page A", "page-a", []string{"react", "typescript"})
	updatePageTags(pageB, "Page B", "page-b", []string{"react"})

	pagesRec := authenticatedRequest(t, router, http.MethodGet, "/api/tags/pages?tags=react&tags=typescript", nil)
	if pagesRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK from tags pages endpoint, got %d - %s", pagesRec.Code, pagesRec.Body.String())
	}

	var pagesResp []map[string]interface{}
	if err := json.Unmarshal(pagesRec.Body.Bytes(), &pagesResp); err != nil {
		t.Fatalf("Invalid pages response JSON: %v", err)
	}

	if len(pagesResp) != 1 {
		t.Fatalf("expected 1 tagged page, got %#v", pagesResp)
	}
	if pagesResp[0]["title"] != "Page A" {
		t.Fatalf("expected Page A, got %#v", pagesResp[0])
	}

})

var _ = It("TestUpdatePage_NotFound", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	body := `{"version":"stale-version","title":"Updated","slug":"updated","content":"New content"}`
	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/not-found-id", strings.NewReader(string(body)))
	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for unknown page, got %d", rec.Code)
	}

})

var _ = It("TestUpdatePage_SlugRemainsIfUnchanged", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	// Create a page
	created := createPageViaAPI(t, router, "Immutable Slug", "immutable-slug", nil, pageNodeKind())

	// Update title, but reuse slug
	payload := map[string]string{
		"version": created.Version,
		"title":   "Updated Title",
		"slug":    created.Slug,
		"content": "Updated content",
	}
	body, _ := json.Marshal(payload)

	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+created.ID, strings.NewReader(string(body)))

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}

	var updated map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("Invalid response JSON: %v", err)
	}

	if updated["slug"] != created.Slug {
		t.Errorf("Expected slug to remain unchanged, got: %v", updated["slug"])
	}

})

var _ = It("TestUpdatePage_PageAlreadyExists", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	page := createPageViaAPI(t, router, "Original Title", "original-title", nil, pageNodeKind())
	createPageViaAPI(t, router, "Conflict Title", "conflict-title", nil, pageNodeKind())

	payload := map[string]string{
		"version": page.Version,
		"title":   "Conflict Title",
		"slug":    "conflict-title",
		"content": "Updated content",
	}
	body, _ := json.Marshal(payload)

	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 Bad Request, got %d", rec.Code)
	}

})

var _ = It("TestUpdatePage_InvalidJSON", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	body := `this is not valid json`
	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/invalid-id", strings.NewReader(string(body)))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid JSON, got %d", rec.Code)
	}

})

var _ = It("TestUpdatePage_MissingTitle", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	body := `{"version":"required","slug":"updated","content":"New content"}`
	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/missing-title", strings.NewReader(string(body)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing title, got %d", rec.Code)
	}

})

var _ = It("TestUpdatePage_MissingSlug", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	body := `{"version":"required","title":"Updated","content":"New content"}`
	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/missing-slug", strings.NewReader(string(body)))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing slug, got %d", rec.Code)
	}

})

var _ = It("TestUpdatePage_InvalidProperties", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	page := createPageViaAPI(t, router, "Original Title", "original-title", nil, pageNodeKind())

	payload := map[string]interface{}{
		"version": page.Version,
		"title":   "Updated Title",
		"slug":    "updated-title",
		"content": "Updated content",
		"properties": map[string]string{
			"leafwiki_hidden": "forbidden",
		},
	}
	body, _ := json.Marshal(payload)

	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 Bad Request, got %d - %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Error  string `json:"error"`
		Fields []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Invalid validation response JSON: %v", err)
	}

	if resp.Error != "validation_error" {
		t.Fatalf("expected validation_error, got %q", resp.Error)
	}

	gotFields := map[string]string{}
	for _, field := range resp.Fields {
		gotFields[field.Field] = field.Message
	}

	if gotFields["properties.leafwiki_hidden"] != "Property key uses a reserved prefix" {
		t.Fatalf("expected reserved prefix validation error, got %#v", gotFields)
	}

})

var _ = It("TestGetPageEndpoint", func() {
	t := GinkgoT()
	dataDir := filepath.Join(t.TempDir(), "data")
	rootDir := filepath.Join(t.TempDir(), "content")
	w := createWikiTestInstanceWithWorkspace(t, wiki.Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir})
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	// Create a page
	page := createPageViaAPI(t, router, "Welcome", "welcome", nil, pageNodeKind())
	if _, err := os.Stat(filepath.Join(rootDir, "welcome.md")); err != nil {
		t.Fatalf("expected API-created page in root dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "root", "welcome.md")); !os.IsNotExist(err) {
		t.Fatalf("expected no API-created page in data dir root, got err=%v", err)
	}
	writePageMarkdownForTest(t, w, page, `---
leafwiki_id: `+page.ID+`
leafwiki_title: Welcome
tags:
  - alpha
  - beta
priority: 2
published: true
owners:
  - alice
  - bob
---
# Welcome
Body
`)

	// Get page
	rec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/"+page.ID, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if resp["id"] == nil {
		t.Errorf("Expected id in response, got: %v", resp)
	}

	if resp["title"] != page.Title {
		t.Errorf("Expected title in response, got: %v", resp)
	}

	if resp["slug"] != page.Slug {
		t.Errorf("Expected slug in response, got: %v", resp)
	}

	tagsValue, ok := resp["tags"].([]interface{})
	if !ok || len(tagsValue) != 2 || tagsValue[0] != "alpha" || tagsValue[1] != "beta" {
		t.Fatalf("Expected tags in response, got %#v", resp["tags"])
	}

	// Only string scalar properties are returned; numbers, booleans, and lists are excluded.
	propertiesValue, ok := resp["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected properties map in response, got %#v", resp["properties"])
	}
	if _, exists := propertiesValue["priority"]; exists {
		t.Fatalf("Numeric property must not be returned, got %#v", propertiesValue)
	}
	if _, exists := propertiesValue["published"]; exists {
		t.Fatalf("Boolean property must not be returned, got %#v", propertiesValue)
	}
	if _, exists := propertiesValue["owners"]; exists {
		t.Fatalf("List property must not be returned, got %#v", propertiesValue)
	}

})

var _ = It("TestGetPageEndpoint_NotFound", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/not-found-id", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("Expected status 404, got %d", rec.Code)
	}

})

var _ = It("TestGetPageEndpoint_MissingID", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("Expected status 404, got %d", rec.Code)
	}

})

var _ = It("TestGetPageByPathEndpoint_MissingPath", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/by-path", nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected status 400, got %d", rec.Code)
	}

})

var _ = It("TestGetPageByPathEndpoint_ExplicitEmptyPathReturnsRootSection", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "root")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("mkdir root fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "README.md"), []byte("---\nleafwiki_id: root\nleafwiki_title: Root README\n---\n# Root README\n"), 0o644); err != nil {
		t.Fatalf("write root README: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "child.md"), []byte("---\nleafwiki_id: child\nleafwiki_title: Child\n---\n# Child\n"), 0o644); err != nil {
		t.Fatalf("write child: %v", err)
	}
	w := createWikiTestInstanceWithWorkspace(t, wiki.Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir})
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/by-path?path=&kind=section", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected root path status 200, got %d - %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse root response: %v", err)
	}
	if resp["id"] != "root" || resp["kind"] != "section" || resp["title"] != "Root README" {
		t.Fatalf("root response = %#v, want root README section", resp)
	}
	if !strings.Contains(resp["content"].(string), "Root README") {
		t.Fatalf("root content = %#v, want README body", resp["content"])
	}

})

var _ = It("TestGetPageByPathEndpoint_NotFound", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/by-path?path=does-not-exist", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("Expected status 404, got %d", rec.Code)
	}

})

var _ = It("TestGetPageByPathEndpoint_PageReturnsNoChildren", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	// Create a standalone page (no children – adding children auto-converts it to a section)
	createPageViaAPI(t, router, "My Page", "my-page", nil, pageNodeKind())

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/by-path?path=my-page", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d - %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	// Page kind (depth=0): the node must be returned with children absent or null
	if resp["kind"] != "page" {
		t.Errorf("Expected kind 'page', got: %v", resp["kind"])
	}
	if children, ok := resp["children"]; ok && children != nil {
		t.Errorf("Expected no children for page kind (depth=0), got: %v", children)
	}

})

var _ = It("TestGetPageByPathEndpoint_SectionReturnsDirectChildrenOnly", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	sectionKind := tree.NodeKindSection

	// Create a section with a child page that itself has a grandchild
	section := createPageViaAPI(t, router, "My Section", "my-section", nil, &sectionKind)
	child := createPageViaAPI(t, router, "Child Page", "child-page", &section.ID, pageNodeKind())
	createPageViaAPI(t, router, "Grandchild Page", "grandchild-page", &child.ID, pageNodeKind())

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/by-path?path=my-section", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d - %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	// Section kind (depth=1): direct children must be present
	children, ok := resp["children"].([]interface{})
	if !ok || len(children) == 0 {
		t.Fatalf("Expected direct children for section kind (depth=1), got: %v", resp["children"])
	}

	// Grandchildren must be absent or null (depth=1 means children's children are not included)
	firstChild, ok := children[0].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected child to be an object, got: %v", children[0])
	}
	if grandchildren, ok := firstChild["children"]; ok && grandchildren != nil {
		t.Errorf("Expected no grandchildren for section kind (depth=1), got: %v", grandchildren)
	}

})

var _ = It("TestGetPageByPathEndpoint_KindDistinguishesSameBasenamePageAndSection", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "root")
	if err := os.MkdirAll(filepath.Join(rootDir, "docs", "sync"), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	write := func(relPath, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(rootDir, filepath.FromSlash(relPath)), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", relPath, err)
		}
	}
	write("docs/index.md", "---\nleafwiki_id: docs-section\nleafwiki_title: Docs\n---\n# Docs\n")
	write("docs/sync.md", "---\nleafwiki_id: sync-page\nleafwiki_title: Sync Page\n---\n# Sync Page\n")
	write("docs/sync/index.md", "---\nleafwiki_id: sync-section\nleafwiki_title: Sync Section\n---\n# Sync Section\n")

	w := createWikiTestInstanceWithWorkspace(t, wiki.Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir})
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	pageRec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/by-path?path=docs/sync&kind=page", nil)
	if pageRec.Code != http.StatusOK {
		t.Fatalf("Expected page status 200, got %d - %s", pageRec.Code, pageRec.Body.String())
	}
	var pageResp map[string]interface{}
	if err := json.Unmarshal(pageRec.Body.Bytes(), &pageResp); err != nil {
		t.Fatalf("parse page response: %v", err)
	}
	if pageResp["id"] != "sync-page" || pageResp["kind"] != "page" {
		t.Fatalf("page response = %#v, want sync-page page", pageResp)
	}

	pageMarkdownPathRec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/by-path?path=docs/sync.md", nil)
	if pageMarkdownPathRec.Code != http.StatusOK {
		t.Fatalf("Expected page markdown-path status 200, got %d - %s", pageMarkdownPathRec.Code, pageMarkdownPathRec.Body.String())
	}
	var pageMarkdownPathResp map[string]interface{}
	if err := json.Unmarshal(pageMarkdownPathRec.Body.Bytes(), &pageMarkdownPathResp); err != nil {
		t.Fatalf("parse page markdown-path response: %v", err)
	}
	if pageMarkdownPathResp["id"] != "sync-page" || pageMarkdownPathResp["kind"] != "page" {
		t.Fatalf("page markdown-path response = %#v, want sync-page page", pageMarkdownPathResp)
	}

	sectionRec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/by-path?path=docs/sync&kind=section", nil)
	if sectionRec.Code != http.StatusOK {
		t.Fatalf("Expected section status 200, got %d - %s", sectionRec.Code, sectionRec.Body.String())
	}
	var sectionResp map[string]interface{}
	if err := json.Unmarshal(sectionRec.Body.Bytes(), &sectionResp); err != nil {
		t.Fatalf("parse section response: %v", err)
	}
	if sectionResp["id"] != "sync-section" || sectionResp["kind"] != "section" {
		t.Fatalf("section response = %#v, want sync-section section", sectionResp)
	}

	sectionCanonicalRec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/by-path?path=docs/sync", nil)
	if sectionCanonicalRec.Code != http.StatusOK {
		t.Fatalf("Expected canonical section status 200, got %d - %s", sectionCanonicalRec.Code, sectionCanonicalRec.Body.String())
	}
	var sectionCanonicalResp map[string]interface{}
	if err := json.Unmarshal(sectionCanonicalRec.Body.Bytes(), &sectionCanonicalResp); err != nil {
		t.Fatalf("parse canonical section response: %v", err)
	}
	if sectionCanonicalResp["id"] != "sync-section" || sectionCanonicalResp["kind"] != "section" {
		t.Fatalf("canonical section response = %#v, want sync-section section", sectionCanonicalResp)
	}

})

// - Explicit README.md page link stays a page when index.md exists
var _ = It("TestGetPageByPathEndpoint_ReadmeMarkdownPathUsesFallbackOnlyWhenActive", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "root")
	if err := os.MkdirAll(filepath.Join(rootDir, "docs", "guides"), 0o755); err != nil {
		t.Fatalf("mkdir guides fixture: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(rootDir, "docs", "indexed"), 0o755); err != nil {
		t.Fatalf("mkdir indexed fixture: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(rootDir, "docs", "no-readme"), 0o755); err != nil {
		t.Fatalf("mkdir no-readme fixture: %v", err)
	}
	write := func(relPath, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(rootDir, filepath.FromSlash(relPath)), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", relPath, err)
		}
	}
	write("docs/index.md", "---\nleafwiki_id: docs-section\nleafwiki_title: Docs\n---\n# Docs\n")
	write("README.md", "---\nleafwiki_id: root-section\nleafwiki_title: Root\n---\n# Root\n")
	write("docs/guides/README.md", "---\nleafwiki_id: guides-section\nleafwiki_title: Guides\n---\n# Guides\n")
	write("docs/indexed/index.md", "---\nleafwiki_id: indexed-section\nleafwiki_title: Indexed\n---\n# Indexed\n")
	write("docs/indexed/README.md", "---\nleafwiki_id: indexed-readme-page\nleafwiki_title: Indexed README\n---\n# Indexed README\n")
	write("docs/no-readme/index.md", "---\nleafwiki_id: no-readme-section\nleafwiki_title: No README\n---\n# No README\n")

	w := createWikiTestInstanceWithWorkspace(t, wiki.Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir})
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	fallbackRec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/by-path?path=docs/guides/README.md", nil)
	if fallbackRec.Code != http.StatusOK {
		t.Fatalf("Expected README fallback status 200, got %d - %s", fallbackRec.Code, fallbackRec.Body.String())
	}
	var fallbackResp map[string]interface{}
	if err := json.Unmarshal(fallbackRec.Body.Bytes(), &fallbackResp); err != nil {
		t.Fatalf("parse README fallback response: %v", err)
	}
	if fallbackResp["id"] != "guides-section" || fallbackResp["kind"] != "section" {
		t.Fatalf("README fallback response = %#v, want guides section", fallbackResp)
	}

	explicitSectionRec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/by-path?path=docs/guides/README.md&kind=section", nil)
	if explicitSectionRec.Code != http.StatusOK {
		t.Fatalf("Expected explicit README section fallback status 200, got %d - %s", explicitSectionRec.Code, explicitSectionRec.Body.String())
	}
	var explicitSectionResp map[string]interface{}
	if err := json.Unmarshal(explicitSectionRec.Body.Bytes(), &explicitSectionResp); err != nil {
		t.Fatalf("parse explicit README section fallback response: %v", err)
	}
	if explicitSectionResp["id"] != "guides-section" || explicitSectionResp["kind"] != "section" {
		t.Fatalf("explicit README section fallback response = %#v, want guides section", explicitSectionResp)
	}

	readmePageRec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/by-path?path=docs/indexed/README.md", nil)
	if readmePageRec.Code != http.StatusOK {
		t.Fatalf("Expected README page status 200, got %d - %s", readmePageRec.Code, readmePageRec.Body.String())
	}
	var readmePageResp map[string]interface{}
	if err := json.Unmarshal(readmePageRec.Body.Bytes(), &readmePageResp); err != nil {
		t.Fatalf("parse README page response: %v", err)
	}
	if readmePageResp["id"] != "indexed-readme-page" || readmePageResp["kind"] != "page" {
		t.Fatalf("README page response = %#v, want indexed README page", readmePageResp)
	}

	inactiveExplicitSectionRec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/by-path?path=docs/indexed/README.md&kind=section", nil)
	if inactiveExplicitSectionRec.Code != http.StatusNotFound {
		t.Fatalf("Expected inactive explicit README section status 404, got %d - %s", inactiveExplicitSectionRec.Code, inactiveExplicitSectionRec.Body.String())
	}

	missingReadmeRec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/by-path?path=docs/no-readme/README.md", nil)
	if missingReadmeRec.Code != http.StatusNotFound {
		t.Fatalf("Expected missing README status 404, got %d - %s", missingReadmeRec.Code, missingReadmeRec.Body.String())
	}

	lowercaseReadmeRec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/by-path?path=docs/guides/readme.md", nil)
	if lowercaseReadmeRec.Code != http.StatusNotFound {
		t.Fatalf("Expected lowercase readme.md status 404, got %d - %s", lowercaseReadmeRec.Code, lowercaseReadmeRec.Body.String())
	}

	traversalReadmeRec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/by-path?path=../README.md&kind=section", nil)
	if traversalReadmeRec.Code != http.StatusBadRequest {
		t.Fatalf("Expected traversal README status 400, got %d - %s", traversalReadmeRec.Code, traversalReadmeRec.Body.String())
	}

})

var _ = It("TestGetTreeEndpoint_ContentPathUsesCaseInsensitiveIndexPrecedence", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "root")
	for _, dir := range []string{"docs", "guides"} {
		if err := os.MkdirAll(filepath.Join(rootDir, dir), 0o755); err != nil {
			t.Fatalf("create %s dir: %v", dir, err)
		}
	}
	write := func(relPath, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(rootDir, filepath.FromSlash(relPath)), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", relPath, err)
		}
	}
	write("docs/INDEX.MD", "---\nleafwiki_id: docs-section\nleafwiki_title: Docs\n---\n# Docs Index\n")
	write("docs/README.md", "---\nleafwiki_id: docs-readme\nleafwiki_title: Docs README\n---\n# Docs README\n")
	write("guides/README.md", "---\nleafwiki_id: guides-section\nleafwiki_title: Guides\n---\n# Guides README\n")

	w := createWikiTestInstanceWithWorkspace(t, wiki.Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir})
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	root := getTreeViaAPI(t, router)
	var docs, guides *apiPage
	for _, child := range root.Children {
		if child.Path == "docs" {
			docs = child
		}
		if child.Path == "guides" {
			guides = child
		}
	}
	if docs == nil {
		t.Fatalf("docs section missing from tree: %#v", root.Children)
	}
	if guides == nil {
		t.Fatalf("guides section missing from tree: %#v", root.Children)
	}
	if docs.ContentPath != "docs/INDEX.MD" {
		t.Fatalf("docs contentPath = %q, want docs/INDEX.MD", docs.ContentPath)
	}
	if docs.ReadmeFallback {
		t.Fatalf("docs readmeFallback = true, want false for index-backed section")
	}
	if !guides.ReadmeFallback {
		t.Fatalf("guides readmeFallback = false, want true for README-backed section")
	}

})

var _ = It("TestEnsurePageEndpoint_CreatesSectionTwinWhenPageRouteExists", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	sectionKind := tree.NodeKindSection
	page := createPageViaAPI(t, router, "Sync Page", "sync", nil, pageNodeKind())
	body := `{"path":"sync","title":"Sync Section","kind":"section"}`
	rec := authenticatedRequest(t, router, http.MethodPost, "/api/pages/ensure", strings.NewReader(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on ensure, got %d - %s", rec.Code, rec.Body.String())
	}
	var ensured apiPage
	if err := json.Unmarshal(rec.Body.Bytes(), &ensured); err != nil {
		t.Fatalf("Unmarshal(ensure response) failed: %v", err)
	}
	if ensured.ID == page.ID {
		t.Fatalf("ensure returned existing page %q instead of section twin", page.ID)
	}
	if ensured.Kind != sectionKind {
		t.Fatalf("ensure kind = %q, want section", ensured.Kind)
	}

	pageRec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/by-path?path=sync&kind=page", nil)
	if pageRec.Code != http.StatusOK {
		t.Fatalf("Expected page twin lookup status 200, got %d - %s", pageRec.Code, pageRec.Body.String())
	}
	var pageTwin apiPage
	if err := json.Unmarshal(pageRec.Body.Bytes(), &pageTwin); err != nil {
		t.Fatalf("Unmarshal(page twin response) failed: %v", err)
	}
	if pageTwin.ID != page.ID || pageTwin.Kind != tree.NodeKindPage {
		t.Fatalf("page twin response = %#v, want original page %q", pageTwin, page.ID)
	}

	sectionRec := authenticatedRequest(t, router, http.MethodGet, "/api/pages/by-path?path=sync&kind=section", nil)
	if sectionRec.Code != http.StatusOK {
		t.Fatalf("Expected section twin lookup status 200, got %d - %s", sectionRec.Code, sectionRec.Body.String())
	}
	var sectionTwin apiPage
	if err := json.Unmarshal(sectionRec.Body.Bytes(), &sectionTwin); err != nil {
		t.Fatalf("Unmarshal(section twin response) failed: %v", err)
	}
	if sectionTwin.ID != ensured.ID || sectionTwin.Kind != tree.NodeKindSection {
		t.Fatalf("section twin response = %#v, want ensured section %q", sectionTwin, ensured.ID)
	}

})

var _ = It("TestGetPagePermalinkEndpoint_ReturnsCurrentPath", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	docs := createPageViaAPI(t, router, "Docs", "docs", nil, pageNodeKind())
	guide := createPageViaAPI(t, router, "Guide", "guide", &docs.ID, pageNodeKind())
	archive := createPageViaAPI(t, router, "Archive", "archive", nil, pageNodeKind())

	movePayload := `{"version":"` + guide.Version + `","parentId":"` + archive.ID + `"}`
	moveRec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+guide.ID+"/move", strings.NewReader(movePayload))
	if moveRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on move, got %d - %s", moveRec.Code, moveRec.Body.String())
	}

	guide = getPageByPathViaAPI(t, router, "archive/guide")

	updatePayload := `{"version":"` + guide.Version + `","title":"User Guide","slug":"user-guide","content":""}`
	updateRec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+guide.ID, strings.NewReader(updatePayload))
	if updateRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on update, got %d - %s", updateRec.Code, updateRec.Body.String())
	}

	target := getPermalinkTargetViaAPI(t, router, guide.ID)
	if target.ID != guide.ID {
		t.Fatalf("expected ID %q, got %q", guide.ID, target.ID)
	}
	if target.Slug != "user-guide" {
		t.Fatalf("expected slug user-guide, got %q", target.Slug)
	}
	if target.Path != "archive/user-guide" {
		t.Fatalf("expected path archive/user-guide, got %q", target.Path)
	}
	if target.Kind != tree.NodeKindPage {
		t.Fatalf("expected kind page, got %q", target.Kind)
	}

})

var _ = It("TestGetPagePermalinkEndpoint_PublicAccessAllowsUnauthenticatedReads", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            true,
		InjectCodeInHeader:      "",
		CustomStylesheet:        "",
		AllowInsecure:           true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		HideLinkMetadataSection: false,
	})

	page := createPageViaAPI(t, router, "Public Page", "public-page", nil, pageNodeKind())

	req := httptest.NewRequest(http.MethodGet, "/api/pages/permalink/"+page.ID, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())
	}

	var target apiPermalinkTarget
	if err := json.Unmarshal(rec.Body.Bytes(), &target); err != nil {
		t.Fatalf("Unmarshal(permalink response) failed: %v", err)
	}
	if target.Path != "public-page" {
		t.Fatalf("expected path public-page, got %q", target.Path)
	}
	if target.Kind != tree.NodeKindPage {
		t.Fatalf("expected kind page, got %q", target.Kind)
	}

})

var _ = It("TestMovePageEndpoint", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	// Create two pages a and b
	a := createPageViaAPI(t, router, "Section A", "section-a", nil, pageNodeKind())
	b := createPageViaAPI(t, router, "Section B", "section-b", nil, pageNodeKind())

	// Move a under b
	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+a.ID+"/move", strings.NewReader(`{"version":"`+a.Version+`","parentId":"`+b.ID+`"}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}

	// Check if a is now a child of b
	movedParent := getPageByPathViaAPI(t, router, "section-b")
	if len(movedParent.Children) != 1 || movedParent.Children[0].ID != a.ID {
		t.Errorf("Expected page to be moved under new parent")
	}

})

var _ = It("TestMovePageEndpoint_NotFound", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/not-found-id/move", strings.NewReader(`{"version":"missing","parentId":"root"}`))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("Expected status 404, got %d", rec.Code)
	}

})

var _ = It("TestMovePageEndpoint_InvalidJSON", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/invalid-id/move", strings.NewReader(`this is not valid json`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected status 400, got %d", rec.Code)
	}

})

var _ = It("TestMovePageEndpoint_MissingParentID", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/missing-parent/move", strings.NewReader(`{"version":"missing","parentId":""}`))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("Expected status 404, got %d", rec.Code)
	}

})

var _ = It("TestMovePageEndpoint_ParentNotFound", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	a := createPageViaAPI(t, router, "Section A", "section-a", nil, pageNodeKind())

	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+a.ID+"/move", strings.NewReader(`{"version":"`+a.Version+`","parentId":"not-found-id"}`))

	t.Logf("Response: %s", rec.Body.String())
	t.Logf("Response Code: %d", rec.Code)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("Expected status 404, got %d", rec.Code)
	}

})

var _ = It("TestMovePageEndpoint_CircularReference", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	a := createPageViaAPI(t, router, "Section A", "section-a", nil, pageNodeKind())
	b := createPageViaAPI(t, router, "Section B", "section-b", &a.ID, pageNodeKind())

	// Verschiebe a → unter b
	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+b.ID+"/move", strings.NewReader(`{"version":"`+b.Version+`","parentId":"`+a.ID+`"}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected status 400, got %d", rec.Code)
	}

})

var _ = It("TestMovePage_FailsIfTargetAlreadyHasPageWithSameSlug", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	a := createPageViaAPI(t, router, "Section A", "section-a", nil, pageNodeKind())
	createPageViaAPI(t, router, "Section B", "section-b", nil, pageNodeKind())

	// Create Conflict Page in b
	conflictPage := createPageViaAPI(t, router, "Section B", "section-b", &a.ID, pageNodeKind())

	// move conflictPage under root (where section-b already exists)
	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+conflictPage.ID+"/move", strings.NewReader(`{"version":"`+conflictPage.Version+`","parentId":"root"}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected status 400, got %d", rec.Code)
	}

})

var _ = It("TestMovePage_InTheSamePlace", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	a := createPageViaAPI(t, router, "Section A", "section-a", nil, pageNodeKind())

	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/"+a.ID+"/move", strings.NewReader(`{"version":"`+a.Version+`","parentId":"root"}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected status 400, got %d", rec.Code)
	}

})

var _ = It("TestSortPagesEndpoint", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	// Create pages
	page1 := createPageViaAPI(t, router, "Page 1", "page-1", nil, pageNodeKind())
	page2 := createPageViaAPI(t, router, "Page 2", "page-2", nil, pageNodeKind())
	page3 := createPageViaAPI(t, router, "Page 3", "page-3", nil, pageNodeKind())
	welcomePage := getPageByPathViaAPI(t, router, "welcome-to-leafwiki")
	deletePageViaAPI(t, router, welcomePage.ID, welcomePage.Version, false)

	// Sort pages
	payload := map[string]interface{}{
		"orderedIds": []string{page3.ID, page1.ID, page2.ID},
	}
	body, _ := json.Marshal(payload)

	rec := authenticatedRequest(t, router, http.MethodPut, "/api/pages/root/sort", strings.NewReader(string(body)))

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if resp["message"] != "Pages sorted successfully" {
		t.Errorf("Expected success message, got: %v", resp["message"])
	}
	if resp["messageId"] != "api.pages.sort.success" {
		t.Errorf("Expected API-scoped success messageId, got: %v", resp["messageId"])
	}

	root := getTreeViaAPI(t, router)
	if len(root.Children) != 3 {
		t.Fatalf("Expected 3 children in root, got: %d", len(root.Children))
	}

	if root.Children[0].ID != page3.ID {
		t.Errorf("Expected first child to be page 3, got: %v", root.Children[0].ID)
	}
	if root.Children[1].ID != page1.ID {
		t.Errorf("Expected second child to be page 1, got: %v", root.Children[1].ID)
	}
	if root.Children[2].ID != page2.ID {
		t.Errorf("Expected third child to be page 2, got: %v", root.Children[2].ID)
	}

})

var _ = It("TestAuthLoginEndpoint", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	body := `{"identifier": "admin", "password": "admin"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for valid login, got %d", rec.Code)
	}

	res := rec.Result()
	defer wrapCloseWithErrorCheck(res.Body.Close, t)

	// Prüfen, ob Cookies gesetzt wurden
	cookies := res.Cookies()
	if len(cookies) == 0 {
		t.Fatalf("Expected auth cookies to be set on login")
	}

})

var _ = It("TestAuthLogin_InvalidCredentials", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	body := `{"identifier": "admin", "password": "wrong"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized for wrong credentials, got %d", rec.Code)
	}

})

var _ = It("TestAuthRefreshToken", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	type authResponse struct {
		AccessTokenExpiresAt int64 `json:"accessTokenExpiresAt"`
	}

	// 1) Login
	loginBody := `{"identifier": "admin", "password": "admin"}`
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on login, got %d", loginRec.Code)
	}

	var loginPayload authResponse
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginPayload); err != nil {
		t.Fatalf("Expected valid login JSON response, got error: %v", err)
	}
	if loginPayload.AccessTokenExpiresAt <= time.Now().Unix() {
		t.Fatalf("Expected login response to include a future access token expiry, got %d", loginPayload.AccessTokenExpiresAt)
	}

	loginRes := loginRec.Result()
	defer wrapCloseWithErrorCheck(loginRes.Body.Close, t)
	cookies := loginRes.Cookies()

	if len(cookies) == 0 {
		t.Fatalf("Expected auth cookies on login response, got none")
	}

	csrfToken := loginRec.Header().Get("X-CSRF-Token")
	if csrfToken == "" {
		for _, c := range cookies {
			if c.Name == "leafwiki_csrf" || c.Name == "__Host-leafwiki_csrf" {
				csrfToken = c.Value
				break
			}
		}
	}

	if csrfToken == "" {
		t.Fatalf("Expected CSRF token after login, got none")
	}

	// call refresh token endpoint with cookies from login
	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh-token", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	req.Header.Set("X-CSRF-Token", csrfToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on refresh, got %d - %s", rec.Code, rec.Body.String())
	}

	var refreshPayload authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &refreshPayload); err != nil {
		t.Fatalf("Expected valid refresh JSON response, got error: %v", err)
	}
	if refreshPayload.AccessTokenExpiresAt <= time.Now().Unix() {
		t.Fatalf("Expected refresh response to include a future access token expiry, got %d", refreshPayload.AccessTokenExpiresAt)
	}

	// optional: check if new cookies are set
	refreshRes := rec.Result()
	defer wrapCloseWithErrorCheck(refreshRes.Body.Close, t)
	newCookies := refreshRes.Cookies()
	if len(newCookies) == 0 {
		t.Fatalf("Expected new auth cookies on refresh")
	}

})

var _ = It("TestCreateUserEndpoint", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	body := `{"username": "john", "email": "john@example.com", "password": "secret123", "role": "editor"}`
	rec := authenticatedRequest(t, router, http.MethodPost, "/api/users", strings.NewReader(body))

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created, got %d", rec.Code)
	}

})

var _ = It("TestCreateUser_DuplicateEmailOrUsername", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	// Create initial user
	payload := `{"username": "john", "email": "john@example.com", "password": "secret", "role": "editor"}`
	_ = authenticatedRequest(t, router, http.MethodPost, "/api/users", strings.NewReader(payload))

	// Attempt with duplicate username
	payloadDuplicate := `{"username": "john", "email": "john2@example.com", "password": "secret", "role": "editor"}`
	rec1 := authenticatedRequest(t, router, http.MethodPost, "/api/users", strings.NewReader(payloadDuplicate))
	if rec1.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for duplicate username, got %d", rec1.Code)
	}

	// Attempt with duplicate email
	payloadDuplicateEmail := `{"username": "johnny", "email": "john@example.com", "password": "secret", "role": "editor"}`
	rec2 := authenticatedRequest(t, router, http.MethodPost, "/api/users", strings.NewReader(payloadDuplicateEmail))
	if rec2.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for duplicate email, got %d", rec2.Code)
	}

})

var _ = It("TestCreateUser_InvalidRole", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	body := `{"username": "sam", "email": "sam@example.com", "password": "secret1234", "role": "undefined"}`
	rec := authenticatedRequest(t, router, http.MethodPost, "/api/users", strings.NewReader(body))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for invalid role, got %d", rec.Code)
	}

})

var _ = It("TestCreateUser_WithViewerRole", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	body := `{"username": "vieweruser", "email": "viewer@example.com", "password": "secret1234", "role": "viewer"}`
	rec := authenticatedRequest(t, router, http.MethodPost, "/api/users", strings.NewReader(body))

	if rec.Code != http.StatusCreated {
		t.Errorf("Expected 201 Created for viewer role, got %d", rec.Code)
	}

})

var _ = It("TestUpdateUser_RoleToViewer", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	// Create user
	create := `{"username": "jane", "email": "jane@example.com", "password": "secretpassword", "role": "editor"}`
	resp := authenticatedRequest(t, router, http.MethodPost, "/api/users", strings.NewReader(create))
	var user map[string]interface{}
	_ = json.Unmarshal(resp.Body.Bytes(), &user)

	updatePayload := map[string]string{
		"username": "jane-updated",
		"email":    "jane-updated@example.com",
		"password": "newpassword",
		"role":     "viewer",
	}
	data, _ := json.Marshal(updatePayload)
	rec := authenticatedRequest(t, router, http.MethodPut, "/api/users/"+user["id"].(string), strings.NewReader(string(data)))

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for user update, got %d", rec.Code)
	}

})

var _ = It("TestViewer_CannotCreatePage", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	// Create a viewer user
	createUserBody := `{"username": "vieweruser", "email": "viewer@example.com", "password": "viewerpass", "role": "viewer"}`
	authenticatedRequest(t, router, http.MethodPost, "/api/users", strings.NewReader(createUserBody))

	// Try to create a page as viewer
	pageBody := `{"title": "Test Page", "slug": "test-page"}`
	rec := authenticatedRequestAs(t, router, "vieweruser", "viewerpass", http.MethodPost, "/api/pages", strings.NewReader(pageBody))

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden for viewer creating page, got %d", rec.Code)
	}

})

var _ = It("TestViewer_CannotUploadAsset", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	// Create a viewer user
	createUserBody := `{"username": "vieweruser2", "email": "viewer2@example.com", "password": "viewerpass2", "role": "viewer"}`
	authenticatedRequest(t, router, http.MethodPost, "/api/users", strings.NewReader(createUserBody))

	// First create a page as admin to have a page ID
	pageBody := `{"title": "Test Page for Assets", "slug": "test-page-assets"}`
	pageResp := authenticatedRequest(t, router, http.MethodPost, "/api/pages", strings.NewReader(pageBody))
	var page map[string]interface{}
	_ = json.Unmarshal(pageResp.Body.Bytes(), &page)
	pageID := page["id"].(string)

	// Try to upload an asset as viewer
	rec := authenticatedRequestAs(t, router, "vieweruser2", "viewerpass2", http.MethodPost, "/api/pages/"+pageID+"/assets", strings.NewReader(""))

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden for viewer uploading asset, got %d", rec.Code)
	}

})

var _ = It("TestViewer_CannotUpdatePage", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	// Create a viewer user
	createUserBody := `{"username": "vieweruser3", "email": "viewer3@example.com", "password": "viewerpass3", "role": "viewer"}`
	authenticatedRequest(t, router, http.MethodPost, "/api/users", strings.NewReader(createUserBody))

	// First create a page as admin
	pageBody := `{"title": "Test Page to Update", "slug": "test-page-update"}`
	pageResp := authenticatedRequest(t, router, http.MethodPost, "/api/pages", strings.NewReader(pageBody))
	var page map[string]interface{}
	_ = json.Unmarshal(pageResp.Body.Bytes(), &page)
	pageID := page["id"].(string)

	// Try to update the page as viewer
	updateBody := `{"title": "Updated Title", "slug": "updated-slug"}`
	rec := authenticatedRequestAs(t, router, "vieweruser3", "viewerpass3", http.MethodPut, "/api/pages/"+pageID, strings.NewReader(updateBody))

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden for viewer updating page, got %d", rec.Code)
	}

})

var _ = It("TestViewer_CannotDeletePage", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	// Create a viewer user
	createUserBody := `{"username": "vieweruser4", "email": "viewer4@example.com", "password": "viewerpass4", "role": "viewer"}`
	authenticatedRequest(t, router, http.MethodPost, "/api/users", strings.NewReader(createUserBody))

	// First create a page as admin
	pageBody := `{"title": "Test Page to Delete", "slug": "test-page-delete"}`
	pageResp := authenticatedRequest(t, router, http.MethodPost, "/api/pages", strings.NewReader(pageBody))
	var page map[string]interface{}
	_ = json.Unmarshal(pageResp.Body.Bytes(), &page)
	pageID := page["id"].(string)

	// Try to delete the page as viewer
	rec := authenticatedRequestAs(t, router, "vieweruser4", "viewerpass4", http.MethodDelete, "/api/pages/"+pageID, nil)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden for viewer deleting page, got %d", rec.Code)
	}

})

var _ = It("TestGetUsersEndpoint", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/users", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rec.Code)
	}

	var users []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &users); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(users) == 0 {
		t.Errorf("Expected at least one user (admin), got none")
	}

})

var _ = It("TestUpdateUserEndpoint", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	// Create user
	create := `{"username": "jane", "email": "jane@example.com", "password": "secretpassword", "role": "editor"}`
	resp := authenticatedRequest(t, router, http.MethodPost, "/api/users", strings.NewReader(create))
	var user map[string]interface{}
	_ = json.Unmarshal(resp.Body.Bytes(), &user)

	updatePayload := map[string]string{
		"username": "jane-updated",
		"email":    "jane-updated@example.com",
		"password": "newpassword",
		"role":     "editor",
	}
	data, _ := json.Marshal(updatePayload)
	rec := authenticatedRequest(t, router, http.MethodPut, "/api/users/"+user["id"].(string), strings.NewReader(string(data)))

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for user update, got %d", rec.Code)
	}

})

var _ = It("TestChangeOwnPasswordEndpoint", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	create := `{"username": "jane", "email": "jane@example.com", "password": "secretpassword", "role": "editor"}`
	resp := authenticatedRequest(t, router, http.MethodPost, "/api/users", strings.NewReader(create))
	var user map[string]interface{}
	_ = json.Unmarshal(resp.Body.Bytes(), &user)

	changePayload := `{"oldPassword":"secretpassword","newPassword":"newsecretpassword"}`
	rec := authenticatedRequestAs(t, router, "jane", "secretpassword", http.MethodPut, "/api/users/me/password", strings.NewReader(changePayload))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("Expected 204 No Content for own password change, got %d - %s", rec.Code, rec.Body.String())
	}

	loginWithOld := map[string]string{
		"identifier": "jane",
		"password":   "secretpassword",
	}
	loginWithOldBody, _ := json.Marshal(loginWithOld)
	oldReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginWithOldBody))
	oldReq.Header.Set("Content-Type", "application/json")
	oldRec := httptest.NewRecorder()
	router.ServeHTTP(oldRec, oldReq)

	if oldRec.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized with old password, got %d - %s", oldRec.Code, oldRec.Body.String())
	}

	loginWithNew := map[string]string{
		"identifier": "jane",
		"password":   "newsecretpassword",
	}
	loginWithNewBody, _ := json.Marshal(loginWithNew)
	newReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginWithNewBody))
	newReq.Header.Set("Content-Type", "application/json")
	newRec := httptest.NewRecorder()
	router.ServeHTTP(newRec, newReq)

	if newRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK with new password, got %d - %s", newRec.Code, newRec.Body.String())
	}

})

var _ = It("TestMCPAPIKeys_AdminCreatesListsAndRevokesUserKey", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	createUser := `{"username": "keyuser", "email": "keyuser@example.com", "password": "secretpassword", "role": "editor"}`
	userRec := authenticatedRequest(t, router, http.MethodPost, "/api/users", strings.NewReader(createUser))
	if userRec.Code != http.StatusCreated {
		t.Fatalf("create user = %d: %s", userRec.Code, userRec.Body.String())
	}
	var user map[string]any
	if err := json.Unmarshal(userRec.Body.Bytes(), &user); err != nil {
		t.Fatalf("decode user: %v", err)
	}
	userID := user["id"].(string)

	createKey := authenticatedRequest(t, router, http.MethodPost, "/api/users/"+userID+"/mcp-api-keys", strings.NewReader(`{"name":"CLI"}`))
	if createKey.Code != http.StatusCreated {
		t.Fatalf("create api key = %d: %s", createKey.Code, createKey.Body.String())
	}
	assertNoStoreHeaders(t, createKey)
	var created map[string]any
	if err := json.Unmarshal(createKey.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created key: %v", err)
	}
	secret, _ := created["secret"].(string)
	if !strings.HasPrefix(secret, "lwk_") {
		t.Fatalf("secret = %q, want lwk_ prefix", secret)
	}
	key := created["key"].(map[string]any)
	keyID := key["id"].(string)
	if key["userId"] != userID || key["name"] != "CLI" {
		t.Fatalf("created key metadata = %#v", key)
	}
	assertAPIKeyNullMetadata(t, key)

	listKeys := authenticatedRequest(t, router, http.MethodGet, "/api/users/"+userID+"/mcp-api-keys", nil)
	if listKeys.Code != http.StatusOK {
		t.Fatalf("list api keys = %d: %s", listKeys.Code, listKeys.Body.String())
	}
	var listed []map[string]any
	if err := json.Unmarshal(listKeys.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode listed keys: %v", err)
	}
	if len(listed) != 1 || listed[0]["id"] != keyID {
		t.Fatalf("listed keys = %#v, want created key", listed)
	}
	if _, ok := listed[0]["secret"]; ok {
		t.Fatalf("list response exposed secret: %#v", listed[0])
	}
	if _, ok := listed[0]["secretHash"]; ok {
		t.Fatalf("list response exposed secretHash: %#v", listed[0])
	}
	assertAPIKeyNullMetadata(t, listed[0])

	revoke := authenticatedRequest(t, router, http.MethodDelete, "/api/users/"+userID+"/mcp-api-keys/"+keyID, nil)
	if revoke.Code != http.StatusNoContent {
		t.Fatalf("revoke api key = %d: %s", revoke.Code, revoke.Body.String())
	}
	listAfterRevoke := authenticatedRequest(t, router, http.MethodGet, "/api/users/"+userID+"/mcp-api-keys", nil)
	var after []map[string]any
	if err := json.Unmarshal(listAfterRevoke.Body.Bytes(), &after); err != nil {
		t.Fatalf("decode keys after revoke: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("revoked key still listed: %#v", after)
	}

})

var _ = It("TestMCPAPIKeys_RoutePermissionsAndValidation", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	createEditor := `{"username": "editor-key-user", "email": "editor-key-user@example.com", "password": "secretpassword", "role": "editor"}`
	editorRec := authenticatedRequest(t, router, http.MethodPost, "/api/users", strings.NewReader(createEditor))
	var editor map[string]any
	if err := json.Unmarshal(editorRec.Body.Bytes(), &editor); err != nil {
		t.Fatalf("decode editor: %v", err)
	}
	editorID := editor["id"].(string)

	invalidName := authenticatedRequest(t, router, http.MethodPost, "/api/users/"+editorID+"/mcp-api-keys", strings.NewReader(`{"name":"   "}`))
	if invalidName.Code != http.StatusBadRequest {
		t.Fatalf("invalid name status = %d: %s", invalidName.Code, invalidName.Body.String())
	}
	var validation struct {
		Error  string `json:"error"`
		Fields []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(invalidName.Body.Bytes(), &validation); err != nil {
		t.Fatalf("decode validation: %v", err)
	}
	if validation.Error != "validation_error" || len(validation.Fields) == 0 || validation.Fields[0].Field != "name" {
		t.Fatalf("validation body = %#v", validation)
	}

	missingUser := authenticatedRequest(t, router, http.MethodPost, "/api/users/missing/mcp-api-keys", strings.NewReader(`{"name":"CLI"}`))
	if missingUser.Code != http.StatusNotFound {
		t.Fatalf("missing user create = %d: %s", missingUser.Code, missingUser.Body.String())
	}

	emptyList := authenticatedRequest(t, router, http.MethodGet, "/api/users/"+editorID+"/mcp-api-keys", nil)
	if emptyList.Code != http.StatusOK {
		t.Fatalf("empty key list = %d: %s", emptyList.Code, emptyList.Body.String())
	}
	if strings.TrimSpace(emptyList.Body.String()) != "[]" {
		t.Fatalf("empty key list body = %q, want []", emptyList.Body.String())
	}

	asEditor := authenticatedRequestAs(t, router, "editor-key-user", "secretpassword", http.MethodPost, "/api/users/missing/mcp-api-keys", strings.NewReader(`{"name":"CLI"}`))
	if asEditor.Code != http.StatusForbidden {
		t.Fatalf("non-admin administer other user = %d: %s", asEditor.Code, asEditor.Body.String())
	}

})

var _ = It("TestMCPAPIKeys_SelfServiceRequiresCurrentPasswordAndIsMCPOnly", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	createEditor := `{"username": "self-key-user", "email": "self-key-user@example.com", "password": "secretpassword", "role": "editor"}`
	authenticatedRequest(t, router, http.MethodPost, "/api/users", strings.NewReader(createEditor))

	wrongPassword := authenticatedRequestAs(t, router, "self-key-user", "secretpassword", http.MethodPost, "/api/users/me/mcp-api-keys", strings.NewReader(`{"name":"Self","currentPassword":"wrong"}`))
	if wrongPassword.Code != http.StatusBadRequest {
		t.Fatalf("wrong current password = %d: %s", wrongPassword.Code, wrongPassword.Body.String())
	}

	createKey := authenticatedRequestAs(t, router, "self-key-user", "secretpassword", http.MethodPost, "/api/users/me/mcp-api-keys", strings.NewReader(`{"name":"Self","currentPassword":"secretpassword"}`))
	if createKey.Code != http.StatusCreated {
		t.Fatalf("self create api key = %d: %s", createKey.Code, createKey.Body.String())
	}
	assertNoStoreHeaders(t, createKey)
	var created map[string]any
	if err := json.Unmarshal(createKey.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode self-created key: %v", err)
	}
	secret := created["secret"].(string)
	key := created["key"].(map[string]any)
	keyID := key["id"].(string)
	assertAPIKeyNullMetadata(t, key)

	list := authenticatedRequestAs(t, router, "self-key-user", "secretpassword", http.MethodGet, "/api/users/me/mcp-api-keys", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("self list api keys = %d: %s", list.Code, list.Body.String())
	}
	var listed []map[string]any
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode self list: %v", err)
	}
	if len(listed) != 1 || listed[0]["id"] != keyID {
		t.Fatalf("self list = %#v, want own key", listed)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("api key authenticated protected normal HTTP API = %d, want 401", rec.Code)
	}

	revoke := authenticatedRequestAs(t, router, "self-key-user", "secretpassword", http.MethodDelete, "/api/users/me/mcp-api-keys/"+keyID, nil)
	if revoke.Code != http.StatusNoContent {
		t.Fatalf("self revoke api key = %d: %s", revoke.Code, revoke.Body.String())
	}

})

var _ = It("TestMCPAPIKeys_SelfCreateRateLimited", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	createEditor := `{"username": "rate-key-user", "email": "rate-key-user@example.com", "password": "secretpassword", "role": "editor"}`
	authenticatedRequest(t, router, http.MethodPost, "/api/users", strings.NewReader(createEditor))

	for i := 0; i < 10; i++ {
		rec := authenticatedRequestAs(t, router, "rate-key-user", "secretpassword", http.MethodPost, "/api/users/me/mcp-api-keys", strings.NewReader(`{"name":"Self","currentPassword":"wrong"}`))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("wrong current password attempt %d = %d: %s", i+1, rec.Code, rec.Body.String())
		}
	}
	limited := authenticatedRequestAs(t, router, "rate-key-user", "secretpassword", http.MethodPost, "/api/users/me/mcp-api-keys", strings.NewReader(`{"name":"Self","currentPassword":"wrong"}`))
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-limited self create = %d: %s", limited.Code, limited.Body.String())
	}

})

var _ = It("TestMCPAPIKeys_RemoteUserSelfCreateDisabled", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)

	trustedProxies, err := authmw.ParseTrustedProxies("192.0.2.1")
	if err != nil {
		t.Fatalf("ParseTrustedProxies failed: %v", err)
	}
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
	if configRec.Code != http.StatusOK {
		t.Fatalf("remote-user config = %d: %s", configRec.Code, configRec.Body.String())
	}
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
	if csrfToken == "" {
		t.Fatalf("remote-user config did not issue CSRF token")
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/users/me/mcp-api-keys", nil)
	listReq.RemoteAddr = "192.0.2.1:1234"
	listReq.Header.Set("Remote-User", "admin")
	for _, cookie := range cookies {
		listReq.AddCookie(cookie)
	}
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("remote-user self list = %d: %s", listRec.Code, listRec.Body.String())
	}

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
	if createRec.Code != http.StatusForbidden {
		t.Fatalf("remote-user self create = %d: %s", createRec.Code, createRec.Body.String())
	}

})

type authDisabledSelfAPIKeyRoute struct {
	method string
	path   string
	body   string
}

var _ = DescribeTable("TestMCPAPIKeys_SelfRoutesBlockedWhenAuthDisabled",
	func(tc authDisabledSelfAPIKeyRoute) {
		t := GinkgoT()
		w := createWikiTestInstance(t)
		defer wrapCloseWithErrorCheck(w.Close, t)

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
		if configRec.Code != http.StatusOK {
			t.Fatalf("auth-disabled config = %d: %s", configRec.Code, configRec.Body.String())
		}
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
		if csrfToken == "" {
			t.Fatalf("auth-disabled config did not issue CSRF token")
		}

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

		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s auth-disabled self API-key route = %d: %s", tc.method, rec.Code, rec.Body.String())
		}
		var authErr wikiauth.AuthErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &authErr); err != nil {
			t.Fatalf("decode auth-disabled error: %v", err)
		}
		if authErr.Error.Code != wikiauth.ErrCodeAuthDisabled {
			t.Fatalf("error code = %q, want %q; body=%s", authErr.Error.Code, wikiauth.ErrCodeAuthDisabled, rec.Body.String())
		}
	},
	Entry("list", authDisabledSelfAPIKeyRoute{method: http.MethodGet, path: "/api/users/me/mcp-api-keys"}),
	Entry("create", authDisabledSelfAPIKeyRoute{method: http.MethodPost, path: "/api/users/me/mcp-api-keys", body: `{"name":"CLI","currentPassword":"admin"}`}),
	Entry("revoke", authDisabledSelfAPIKeyRoute{method: http.MethodDelete, path: "/api/users/me/mcp-api-keys/some-key"}),
)

var _ = It("TestDeleteUserEndpoint", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	// Create user
	create := `{"username": "todelete", "email": "delete@example.com", "password": "secrepassword", "role": "editor"}`
	resp := authenticatedRequest(t, router, http.MethodPost, "/api/users", strings.NewReader(create))
	var user map[string]interface{}
	_ = json.Unmarshal(resp.Body.Bytes(), &user)

	// Delete user
	rec := authenticatedRequest(t, router, http.MethodDelete, "/api/users/"+user["id"].(string), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("Expected 204 OK on delete, got %d", rec.Code)
	}

})

var _ = It("TestDeleteAdminUser_ShouldFail", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	// Get default admin
	rec := authenticatedRequest(t, router, http.MethodGet, "/api/users", nil)
	var users []map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &users)

	var adminID string
	for _, u := range users {
		if u["role"] == "admin" {
			adminID = u["id"].(string)
		}
	}

	if adminID == "" {
		t.Fatal("No admin user found")
	}

	// Attempt to delete the admin
	recDel := authenticatedRequest(t, router, http.MethodDelete, "/api/users/"+adminID, nil)
	if recDel.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 when deleting admin user, got %d", recDel.Code)
	}

})

var _ = It("TestRequireAdminMiddleware", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	// Default Admin create user should succeed
	body := `{"username": "mod", "email": "mod@example.com", "password": "secretpassword", "role": "editor"}`
	rec := authenticatedRequest(t, router, http.MethodPost, "/api/users", strings.NewReader(body))

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created by admin, got %d", rec.Code)
	}

})

var _ = It("TestRequireAdminMiddleware_BlockedWhenAuthDisabled", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)

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

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden for POST /api/users when auth disabled, got %d - %s", rec.Code, rec.Body.String())
	}

	// Test GET /api/users (admin-only endpoint)
	req = httptest.NewRequest(http.MethodGet, "/api/users", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden for GET /api/users when auth disabled, got %d - %s", rec.Code, rec.Body.String())
	}

	// Test DELETE /api/users/:id (admin-only endpoint)
	req = httptest.NewRequest(http.MethodDelete, "/api/users/some-user-id", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden for DELETE /api/users/:id when auth disabled, got %d - %s", rec.Code, rec.Body.String())
	}

})

var _ = It("TestRequireAuthMiddleware_Unauthorized", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	// Request ohne Token
	req := httptest.NewRequest(http.MethodPost, "/api/pages", strings.NewReader(`{"title": "Oops", "slug": "oops"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized, got %d", rec.Code)
	}

})

var _ = It("TestRequireAuthMiddleware_InvalidToken", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	req := httptest.NewRequest(http.MethodPost, "/api/pages", strings.NewReader(`{"title": "Bad", "slug": "bad"}`))
	req.Header.Set("Authorization", "Bearer invalidtoken")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized for invalid token, got %d", rec.Code)
	}

})

var _ = It("TestAssetEndpoints", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	// Step 0: Login als Admin und Cookies holen
	loginBody := `{"identifier": "admin", "password": "admin"}`
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()

	router.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on login, got %d - %s", loginRec.Code, loginRec.Body.String())
	}

	loginRes := loginRec.Result()
	defer wrapCloseWithErrorCheck(loginRes.Body.Close, t)

	cookies := loginRes.Cookies()
	if len(cookies) == 0 {
		t.Fatalf("Expected auth cookies after login, got none")
	}

	csrfToken := loginRec.Header().Get("X-CSRF-Token")
	if csrfToken == "" {
		for _, c := range cookies {
			if c.Name == "leafwiki_csrf" || c.Name == "__Host-leafwiki_csrf" {
				csrfToken = c.Value
				break
			}
		}
	}

	if csrfToken == "" {
		t.Fatalf("Expected CSRF token after login, got none")
	}

	addCookies := func(req *http.Request) {
		for _, c := range cookies {
			req.AddCookie(c)
		}

		if req.Method != http.MethodGet && req.Method != http.MethodHead && req.Method != http.MethodOptions {
			req.Header.Set("X-CSRF-Token", csrfToken)
		}
	}

	// Step 1: Create page direkt über Wiki-API
	page := createPageViaAPI(t, router, "Assets Page", "assets-page", nil, pageNodeKind())

	// Step 2: Upload file
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "testfile.txt")
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}
	if _, err := part.Write([]byte("Hello, asset!")); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Failed to close multipart writer: %v", err)
	}

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/pages/"+page.ID+"/assets", body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	addCookies(uploadReq)

	uploadRec := httptest.NewRecorder()
	router.ServeHTTP(uploadRec, uploadReq)

	if uploadRec.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created on upload, got %d - %s", uploadRec.Code, uploadRec.Body.String())
	}

	var uploadResp map[string]string
	if err := json.Unmarshal(uploadRec.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("Invalid upload JSON: %v", err)
	}
	if uploadResp["file"] == "" {
		t.Error("Expected file field in upload response")
	}

	// Step 3: List assets
	listReq := httptest.NewRequest(http.MethodGet, "/api/pages/"+page.ID+"/assets", nil)
	addCookies(listReq)

	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on listing, got %d - %s", listRec.Code, listRec.Body.String())
	}

	var listResp map[string][]string
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("Invalid listing JSON: %v", err)
	}
	if len(listResp["files"]) != 1 || listResp["files"][0] != "/assets/"+page.ID+"/testfile.txt" {
		t.Errorf("Expected file in listing, got: %v", listResp["files"])
	}

	// Step 4: Delete asset
	delReq := httptest.NewRequest(http.MethodDelete, "/api/pages/"+page.ID+"/assets/testfile.txt", nil)
	addCookies(delReq)

	delRec := httptest.NewRecorder()
	router.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusOK {
		t.Errorf("Expected 200 OK on delete, got %d - %s", delRec.Code, delRec.Body.String())
	}
	var deleteResp map[string]interface{}
	if err := json.Unmarshal(delRec.Body.Bytes(), &deleteResp); err != nil {
		t.Fatalf("Invalid delete JSON: %v", err)
	}
	if deleteResp["messageId"] != "api.assets.delete.success" {
		t.Errorf("Expected API-scoped asset delete messageId, got: %v", deleteResp["messageId"])
	}

	// Step 5: Verify asset is gone
	listReq2 := httptest.NewRequest(http.MethodGet, "/api/pages/"+page.ID+"/assets", nil)
	addCookies(listReq2)

	listRec2 := httptest.NewRecorder()
	router.ServeHTTP(listRec2, listReq2)

	if listRec2.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK on listing after delete, got %d - %s", listRec2.Code, listRec2.Body.String())
	}

	var listResp2 map[string][]string
	if err := json.Unmarshal(listRec2.Body.Bytes(), &listResp2); err != nil {
		t.Fatalf("Invalid listing JSON: %v", err)
	}
	if len(listResp2["files"]) != 0 {
		t.Errorf("Expected asset to be deleted, got: %v", listResp2["files"])
	}

})

// Lets check the indexing status
var _ = It("TestIndexingStatusEndpoint", func() {
	t := GinkgoT()
	// Lets call /api/search/status
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)
	router := createRouterTestInstance(w, t)

	// Default Admin holen
	rec := authenticatedRequest(t, router, http.MethodGet, "/api/search/status", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rec.Code)
	}

	var status map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if status["active"] == nil {
		t.Errorf("Expected 'active' field in response, got: %v", status)
	}

})

// uploadTestAsset is a helper function that creates a page, uploads an asset, and returns the asset URL and auth cookies.
// If needsAuth is true, it will obtain authentication cookies; otherwise it will get CSRF token only (for AuthDisabled mode).
func uploadTestAsset(t routerTestTB, router *gin.Engine, w *wiki.Wiki, content string, needsAuth bool) (assetURL string, cookies []*http.Cookie) {
	// Create a page
	pageID := ""
	if needsAuth {
		pageID = createPageViaAPI(t, router, "Test Page", "test-page", nil, pageNodeKind()).ID
	}

	// Prepare the file upload
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Failed to close multipart writer: %v", err)
	}

	var csrfToken string

	if needsAuth {
		// Login to get auth cookies
		loginBody := `{"identifier": "admin", "password": "admin"}`
		loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		router.ServeHTTP(loginRec, loginReq)

		if loginRec.Code != http.StatusOK {
			t.Fatalf("Expected 200 OK on login, got %d", loginRec.Code)
		}

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

		if createRec.Code != http.StatusCreated {
			t.Fatalf("Expected 201 Created on page creation, got %d - %s", createRec.Code, createRec.Body.String())
		}

		var pageResp apiPage
		if err := json.Unmarshal(createRec.Body.Bytes(), &pageResp); err != nil {
			t.Fatalf("Invalid page creation JSON: %v", err)
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

	if uploadRec.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created on upload, got %d - %s", uploadRec.Code, uploadRec.Body.String())
	}

	var uploadResp map[string]string
	if err := json.Unmarshal(uploadRec.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("Invalid upload JSON: %v", err)
	}

	assetURL = uploadResp["file"]
	if assetURL == "" {
		t.Fatal("Expected file URL in upload response")
	}

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
var _ = DescribeTable("TestAssetAccessControl",
	func(tc assetAccessControlScenario) {
		t := GinkgoT()
		w := createWikiTestInstance(t)
		defer wrapCloseWithErrorCheck(w.Close, t)

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

		assetURL, cookies := uploadTestAsset(t, router, w, tc.content, tc.needsAuth)

		assetReq := httptest.NewRequest(http.MethodGet, assetURL, nil)
		if tc.sendCookies {
			for _, cookie := range cookies {
				assetReq.AddCookie(cookie)
			}
		}
		assetRec := httptest.NewRecorder()
		router.ServeHTTP(assetRec, assetReq)

		if assetRec.Code != tc.wantStatus {
			t.Errorf("Expected status %d when accessing asset, got %d", tc.wantStatus, assetRec.Code)
		}

		if tc.wantStatus == http.StatusOK {
			content := assetRec.Body.String()
			if content != tc.content {
				t.Errorf("Expected %q, got %q", tc.content, content)
			}
		}
	},
	Entry("PrivateMode_UnauthenticatedAccess_Returns401", assetAccessControlScenario{
		content:    "test content",
		needsAuth:  true,
		wantStatus: http.StatusUnauthorized,
	}),
	Entry("PrivateMode_AuthenticatedAccess_Returns200", assetAccessControlScenario{
		content:     "test content",
		needsAuth:   true,
		sendCookies: true,
		wantStatus:  http.StatusOK,
	}),
	Entry("PublicAccessMode_UnauthenticatedAccess_Returns200", assetAccessControlScenario{
		publicAccess: true,
		content:      "test content public",
		needsAuth:    true,
		wantStatus:   http.StatusOK,
	}),
	Entry("AuthDisabledMode_UnauthenticatedAccess_Returns200", assetAccessControlScenario{
		authDisabled: true,
		content:      "test content no auth",
		needsAuth:    false,
		wantStatus:   http.StatusOK,
	}),
)

var _ = It("TestBuildCustomStylesheetTag", func() {
	t := GinkgoT()
	tag := httpinternal.BuildCustomStylesheetTag("/wiki", "/tmp/custom.css")

	expected := `<link rel="stylesheet" href="/wiki/custom.css">`
	if tag != expected {
		t.Fatalf("expected %q, got %q", expected, tag)
	}

})

var _ = It("TestBuildCustomStylesheetTag_EmptyPath", func() {
	t := GinkgoT()
	tag := httpinternal.BuildCustomStylesheetTag("", "")
	if tag != "" {
		t.Fatalf("expected empty tag, got %q", tag)
	}

})

var _ = It("TestInjectIntoHead", func() {
	t := GinkgoT()
	html := "<html><head></head><body></body></html>"
	got := httpinternal.InjectIntoHead(html, `<link rel="stylesheet" href="/custom.css">`)

	if !strings.Contains(got, `<link rel="stylesheet" href="/custom.css">`) {
		t.Fatalf("expected stylesheet link to be injected, got %q", got)
	}

})

var _ = Describe("router edge coverage", func() {
	It("sets Gin release mode in production", func() {
		previous := httpinternal.Environment
		httpinternal.Environment = "production"
		DeferCleanup(func() {
			httpinternal.Environment = previous
			gin.SetMode(gin.TestMode)
		})

		httpinternal.NewRouter(nil, httpinternal.FrontendConfig{}, httpinternal.RouterOptions{DisableFrontendRoutes: true})

		Expect(gin.Mode()).To(Equal(gin.ReleaseMode))
	})

	It("normalizes empty and relative custom stylesheet paths", func() {
		storageDir := GinkgoT().TempDir()

		empty, err := httpinternal.NormalizeCustomStylesheetPath(storageDir, " \t\n ")
		Expect(err).NotTo(HaveOccurred())
		Expect(empty).To(BeEmpty())

		relative, err := httpinternal.NormalizeCustomStylesheetPath(storageDir, "styles/custom.css")
		Expect(err).NotTo(HaveOccurred())
		Expect(relative).To(Equal(filepath.Join(storageDir, "styles", "custom.css")))
	})

	It("leaves HTML unchanged when injecting into a document without a head close tag", func() {
		html := "<html><body>content</body></html>"

		Expect(httpinternal.InjectIntoHead(html, `<script src="/custom.js"></script>`)).To(Equal(html))
	})

	It("panics when the embedded frontend dist filesystem cannot be opened", func() {
		previous := httpinternal.EmbedFrontend
		httpinternal.EmbedFrontend = "true"
		DeferCleanup(func() {
			httpinternal.EmbedFrontend = previous
		})
		DeferCleanup(httpinternal.SetFrontendSubFSForTest(func(_ fs.FS, _ string) (fs.FS, error) {
			return nil, errors.New("dist unavailable")
		}))

		Expect(func() {
			httpinternal.NewRouter(nil, httpinternal.FrontendConfig{}, httpinternal.RouterOptions{DisableRequestLog: true})
		}).To(PanicWith(ContainSubstring("failed to create sub FS: dist unavailable")))
	})

	It("panics when the embedded frontend static filesystem cannot be opened", func() {
		previous := httpinternal.EmbedFrontend
		httpinternal.EmbedFrontend = "true"
		DeferCleanup(func() {
			httpinternal.EmbedFrontend = previous
		})
		var requestedDirs []string
		DeferCleanup(httpinternal.SetFrontendSubFSForTest(func(_ fs.FS, dir string) (fs.FS, error) {
			requestedDirs = append(requestedDirs, dir)
			if dir == "dist" {
				return fstest.MapFS{
					"index.html": &fstest.MapFile{Data: []byte("<html><head></head><body></body></html>")},
				}, nil
			}
			return nil, errors.New("static unavailable")
		}))

		Expect(func() {
			httpinternal.NewRouter(nil, httpinternal.FrontendConfig{}, httpinternal.RouterOptions{DisableRequestLog: true})
		}).To(PanicWith(ContainSubstring("failed to create sub FS: static unavailable")))
		Expect(requestedDirs).To(Equal([]string{"dist", "dist/static"}))
	})

	It("returns 404 when the embedded SPA index cannot be read", func() {
		previous := httpinternal.EmbedFrontend
		httpinternal.EmbedFrontend = "true"
		DeferCleanup(func() {
			httpinternal.EmbedFrontend = previous
		})
		DeferCleanup(httpinternal.SetFrontendReadFileForTest(func(_ fs.FS, name string) ([]byte, error) {
			Expect(name).To(Equal("index.html"))
			return nil, errors.New("index unavailable")
		}))
		router := httpinternal.NewRouter(nil, httpinternal.FrontendConfig{}, httpinternal.RouterOptions{DisableRequestLog: true})

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs", nil))

		Expect(rec.Code).To(Equal(http.StatusNotFound))
	})

	It("returns the relative path error while validating a custom stylesheet", func() {
		storageDir := GinkgoT().TempDir()
		relErr := errors.New("relative path failed")
		DeferCleanup(httpinternal.SetCustomStylesheetRelPathForTest(func(base, path string) (string, error) {
			Expect(base).To(Equal(filepath.Clean(storageDir)))
			Expect(path).To(Equal(filepath.Join(storageDir, "style.css")))
			return "", relErr
		}))

		resolved, err := httpinternal.NormalizeCustomStylesheetPath(storageDir, "style.css")

		Expect(resolved).To(BeEmpty())
		Expect(err).To(MatchError(relErr))
	})

	It("returns 404 for a configured custom stylesheet that is missing on disk", func() {
		storageDir := GinkgoT().TempDir()
		missingCSSPath := filepath.Join(storageDir, "missing.css")
		router := httpinternal.NewRouter(nil, httpinternal.FrontendConfig{
			CustomStylesheetPath: missingCSSPath,
		}, httpinternal.RouterOptions{DisableRequestLog: true})

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/custom.css", nil))

		Expect(rec.Code).To(Equal(http.StatusNotFound))
	})

	It("returns 500 for a configured custom stylesheet that cannot be statted", func() {
		router := httpinternal.NewRouter(nil, httpinternal.FrontendConfig{
			CustomStylesheetPath: "bad\x00stylesheet.css",
		}, httpinternal.RouterOptions{DisableRequestLog: true})

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/custom.css", nil))

		Expect(rec.Code).To(Equal(http.StatusInternalServerError))
	})

	It("applies base path SPA fallback routing and index rewrites", func() {
		previous := httpinternal.EmbedFrontend
		httpinternal.EmbedFrontend = "true"
		DeferCleanup(func() {
			httpinternal.EmbedFrontend = previous
		})
		router := httpinternal.NewRouter(nil, httpinternal.FrontendConfig{
			CustomStylesheetPath: filepath.Join(GinkgoT().TempDir(), "style.css"),
			GetSiteName: func() string {
				return "Test Wiki"
			},
			GetFaviconFile: func() string {
				return "favicon.ico"
			},
		}, httpinternal.RouterOptions{
			BasePath:           "/wiki",
			InjectCodeInHeader: `<meta name="test-injection" content="ok">`,
			DisableRequestLog:  true,
		})

		outsideBasePath := httptest.NewRecorder()
		router.ServeHTTP(outsideBasePath, httptest.NewRequest(http.MethodGet, "/outside", nil))
		Expect(outsideBasePath.Code).To(Equal(http.StatusNotFound))
		Expect(outsideBasePath.Body.String()).To(Equal("Page not found"))

		spaRoot := httptest.NewRecorder()
		router.ServeHTTP(spaRoot, httptest.NewRequest(http.MethodGet, "/wiki", nil))
		Expect(spaRoot.Code).To(Equal(http.StatusOK))
		Expect(spaRoot.Body.String()).To(ContainSubstring("Test Wiki"))
		Expect(spaRoot.Body.String()).To(ContainSubstring(`/wiki/custom.css`))
		Expect(spaRoot.Body.String()).To(ContainSubstring(`/wiki/branding/favicon.ico`))
		Expect(spaRoot.Body.String()).To(ContainSubstring(`test-injection`))

		nonGet := httptest.NewRecorder()
		router.ServeHTTP(nonGet, httptest.NewRequest(http.MethodPost, "/wiki/docs", nil))
		Expect(nonGet.Code).To(Equal(http.StatusNotFound))
		Expect(nonGet.Body.String()).To(Equal("Page not found"))
	})
})

type frontendFaviconHrefScenario struct {
	basePath    string
	faviconFile string
	want        string
}

var _ = DescribeTable("TestBuildFrontendFaviconHref",
	func(tt frontendFaviconHrefScenario) {
		t := GinkgoT()

		got := httpinternal.BuildFrontendFaviconHref(tt.basePath, tt.faviconFile)
		if got != tt.want {
			t.Fatalf("BuildFrontendFaviconHref(%q, %q) = %q, want %q", tt.basePath, tt.faviconFile, got, tt.want)
		}
	},
	Entry("default favicon without base path", frontendFaviconHrefScenario{
		want:     "/favicon.svg",
		basePath: "",
	}),
	Entry("default favicon with base path", frontendFaviconHrefScenario{
		basePath: "/wiki",
		want:     "/wiki/favicon.svg",
	}),
	Entry("custom favicon without base path", frontendFaviconHrefScenario{
		faviconFile: "favicon.ico",
		want:        "/branding/favicon.ico",
	}),
	Entry("custom favicon with base path", frontendFaviconHrefScenario{
		basePath:    "/wiki",
		faviconFile: "favicon.webp",
		want:        "/wiki/branding/favicon.webp",
	}),
)

var _ = It("TestCustomStylesheetRoute", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)

	customCSSPath := filepath.Join(w.GetStorageDir(), "custom.css")
	if err := os.WriteFile(customCSSPath, []byte("body { color: red; }"), 0644); err != nil {
		t.Fatalf("failed to create custom stylesheet: %v", err)
	}

	router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            false,
		InjectCodeInHeader:      "",
		CustomStylesheet:        customCSSPath,
		AllowInsecure:           true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		HideLinkMetadataSection: false,
		AuthDisabled:            false,
	})

	req := httptest.NewRequest(http.MethodGet, "/custom.css", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if got := rec.Header().Get("Content-Type"); got != "text/css; charset=utf-8" {
		t.Fatalf("expected css content-type, got %q", got)
	}

	if !strings.Contains(rec.Body.String(), "body { color: red; }") {
		t.Fatalf("expected CSS body, got %q", rec.Body.String())
	}

})

var _ = It("TestCustomStylesheetRoute_RejectsPathOutsideStorageDir", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)

	outsideCSSPath := filepath.Join(t.TempDir(), "outside.css")
	if err := os.WriteFile(outsideCSSPath, []byte("body { color: blue; }"), 0644); err != nil {
		t.Fatalf("failed to create stylesheet outside storage dir: %v", err)
	}

	router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            false,
		InjectCodeInHeader:      "",
		CustomStylesheet:        outsideCSSPath,
		AllowInsecure:           true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		HideLinkMetadataSection: false,
		AuthDisabled:            false,
	})

	req := httptest.NewRequest(http.MethodGet, "/custom.css", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when stylesheet path is outside storage dir, got %d", rec.Code)
	}

})

var _ = It("TestCustomStylesheetRoute_RejectsNonCSSFile", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)

	textFilePath := filepath.Join(w.GetStorageDir(), "custom.txt")
	if err := os.WriteFile(textFilePath, []byte("not css"), 0644); err != nil {
		t.Fatalf("failed to create non-css file: %v", err)
	}

	router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            false,
		InjectCodeInHeader:      "",
		CustomStylesheet:        textFilePath,
		AllowInsecure:           true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		HideLinkMetadataSection: false,
		AuthDisabled:            false,
	})

	req := httptest.NewRequest(http.MethodGet, "/custom.css", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when stylesheet path is not a css file, got %d", rec.Code)
	}

})

var _ = It("TestBrandingAssetRoute_DisablesClientCache", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)

	router := createRouterTestInstance(w, t)
	uploadBrandingLogoViaAPI(t, router, "logo.png", []byte("logo"))

	req := httptest.NewRequest(http.MethodGet, "/branding/logo.png", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected Cache-Control no-store, got %q", got)
	}

})

var _ = It("TestFaviconRoute_DisablesClientCache", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)

	EmbedFrontendOrig := httpinternal.EmbedFrontend
	httpinternal.EmbedFrontend = "true"
	defer func() {
		httpinternal.EmbedFrontend = EmbedFrontendOrig
	}()

	router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		PublicAccess:            false,
		InjectCodeInHeader:      "",
		CustomStylesheet:        "",
		AllowInsecure:           true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		HideLinkMetadataSection: false,
		AuthDisabled:            false,
	})

	req := httptest.NewRequest(http.MethodGet, "/favicon.svg", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected Cache-Control no-store, got %q", got)
	}

})

var _ = It("TestOAuthApprovalFrontendRoute_HasApprovalSecurityHeaders", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)

	embedFrontendOrig := httpinternal.EmbedFrontend
	httpinternal.EmbedFrontend = "true"
	defer func() {
		httpinternal.EmbedFrontend = embedFrontendOrig
	}()

	router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
		AllowInsecure:           true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		MCPEnabled:              true,
		MCPBindHost:             "127.0.0.1",
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/oauth/approve", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /oauth/approve = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("approval Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "frame-ancestors 'none'") {
		t.Fatalf("approval Content-Security-Policy = %q, want frame-ancestors 'none'", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("approval X-Frame-Options = %q, want DENY", got)
	}

})

var _ = It("TestFaviconICORoute_ServesCustomBrandingFavicon", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)

	router := createRouterTestInstance(w, t)
	uploadBrandingFaviconViaAPI(t, router, "favicon.ico", []byte("custom-favicon"))

	req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected Cache-Control no-store, got %q", got)
	}

	if got := rec.Body.String(); got != "custom-favicon" {
		t.Fatalf("expected custom favicon payload, got %q", got)
	}

})

var _ = It("TestFaviconICORoute_FallsBackToDefaultSVG", func() {
	t := GinkgoT()
	w := createWikiTestInstance(t)
	defer wrapCloseWithErrorCheck(w.Close, t)

	router := createRouterTestInstance(w, t)

	req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected Cache-Control no-store, got %q", got)
	}

	if got := rec.Body.String(); !strings.Contains(got, "<svg") {
		t.Fatalf("expected default svg favicon response, got %q", got)
	}

})

var _ = It("TestBuildCustomStylesheetTag_WhitespacePath", func() {
	t := GinkgoT()
	tag := httpinternal.BuildCustomStylesheetTag("/wiki", "   ")
	if tag != "" {
		t.Fatalf("expected empty tag for whitespace path, got %q", tag)
	}

})

var _ = DescribeTable("IsLoopbackHost",
	func(host string, want bool) {
		t := GinkgoT()
		if got := httpinternal.IsLoopbackHost(host); got != want {
			t.Fatalf("IsLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	},
	Entry("localhost", "localhost", true),
	Entry("localhost with whitespace and uppercase", " LOCALHOST ", true),
	Entry("IPv4 loopback", "127.0.0.1", true),
	Entry("IPv6 loopback", "::1", true),
	Entry("bracketed IPv6 loopback", "[::1]", true),
	Entry("non-loopback IPv4", "192.0.2.10", false),
	Entry("empty host", "", false),
	Entry("malformed host", "not a host", false),
)

var _ = DescribeTable("IsLoopbackRemoteAddr",
	func(remoteAddr string, want bool) {
		t := GinkgoT()
		if got := httpinternal.IsLoopbackRemoteAddr(remoteAddr); got != want {
			t.Fatalf("IsLoopbackRemoteAddr(%q) = %v, want %v", remoteAddr, got, want)
		}
	},
	Entry("IPv4 host port", "127.0.0.1:8080", true),
	Entry("IPv6 host port", "[::1]:8080", true),
	Entry("localhost host port", "localhost:8080", true),
	Entry("loopback without port", "127.0.0.1", true),
	Entry("non-loopback host port", "203.0.113.5:8080", false),
	Entry("empty remote addr", "", false),
	Entry("malformed remote addr", "not a remote addr", false),
)

var _ = It("LocalOnlyHandler allows loopback requests", func() {
	t := GinkgoT()
	called := false
	handler := httpinternal.LocalOnlyHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		called = true
		w.Header().Set("X-Local-Only", "allowed")
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.RemoteAddr = "127.0.0.1:3456"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected wrapped handler to be called for loopback request")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("LocalOnlyHandler loopback status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("X-Local-Only"); got != "allowed" {
		t.Fatalf("LocalOnlyHandler did not preserve wrapped response header, got %q", got)
	}
})

var _ = It("LocalOnlyHandler rejects non-loopback requests", func() {
	t := GinkgoT()
	called := false
	handler := httpinternal.LocalOnlyHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.RemoteAddr = "203.0.113.5:3456"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("wrapped handler was called for non-loopback request")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("LocalOnlyHandler non-loopback status = %d, want %d", rec.Code, http.StatusNotFound)
	}
})

func captureDefaultLogs(t routerTestTB) *bytes.Buffer {
	t.Helper()

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})
	return &logs
}

func findJSONLogEntry(t routerTestTB, logs string, msg string) map[string]any {
	t.Helper()

	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %v\n%s", err, line)
		}
		if entry["msg"] == msg {
			return entry
		}
	}
	t.Fatalf("logs did not contain msg %q:\n%s", msg, logs)
	return nil
}
