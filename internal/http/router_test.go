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

	"fmt"
	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/assets"
	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/importer"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/wiki"
	wikiassets "github.com/perber/wiki/internal/wiki/assets"
	wikiauth "github.com/perber/wiki/internal/wiki/auth"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
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
func httpTestTempDir() string {
	GinkgoHelper()
	dir, err := os.
		MkdirTemp("",
			"leafwiki-http-*",
		)
	Expect(err).To(Succeed())
	DeferCleanup(func() {
		Expect(
			os.RemoveAll(dir),
		).To(Succeed())
	})
	return dir
}

func wrapCloseWithErrorCheck(closer func() error) {
	GinkgoHelper()

	DeferCleanup(func() {
		{
			err := closer()
			Expect(err).NotTo(HaveOccurred(), "failed to close resource: %v", err)
		}

	})
}

func fixturePathForHTTPTests(rel string, candidates ...string) string {
	GinkgoHelper()

	wd, err := os.Getwd()
	Expect(err).NotTo(HaveOccurred(), "getwd: %v", err)

	for _, candidate := range candidates {
		abs := filepath.Join(wd, candidate, rel)
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs
		}
	}
	Fail(fmt.Sprintf("fixture path not found for %q from working directory %q", rel, wd))
	return ""
}

func createWikiTestInstance() *wiki.Wiki {
	GinkgoHelper()
	return createWikiTestInstanceWithRevisionFlag(true)
}

func createWikiTestInstanceWithRevisionFlag(_ bool) *wiki.Wiki {
	GinkgoHelper()
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		StorageDir:          httpTestTempDir(),
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	Expect(err).NotTo(HaveOccurred(), "Failed to create wiki instance: %v", err)

	return w
}

func createWikiTestInstanceWithWorkspace(workspace wiki.Workspace) *wiki.Wiki {
	GinkgoHelper()

	w, err := wiki.NewWiki(&wiki.WikiOptions{
		Workspace:           workspace,
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	Expect(err).NotTo(HaveOccurred(), "Failed to create wiki instance: %v", err)

	return w
}

func createRouterTestInstance(w *wiki.Wiki) *gin.Engine {
	GinkgoHelper()
	return createRouterTestInstanceWithMaxAssetUploadSize(w, assets.DefaultMaxUploadSizeBytes)
}

func createRouterTestInstanceWithRevision(w *wiki.Wiki) *gin.Engine {
	GinkgoHelper()
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

func createRouterTestInstanceWithMaxAssetUploadSize(w *wiki.Wiki, maxAssetUploadSizeBytes shared.MaxBytes) *gin.Engine {
	GinkgoHelper()
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

func createRouterTestInstanceWithAllowInsecure(w *wiki.Wiki, allowInsecure bool) *gin.Engine {
	GinkgoHelper()
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

type csrfTokenTransport string

const (
	csrfTokenTransportHeader csrfTokenTransport = "header"
	csrfTokenTransportCookie csrfTokenTransport = "cookie"
)

type authenticatedSessionCredentials struct {
	Cookies       []*http.Cookie
	CSRFToken     string
	CSRFTransport csrfTokenTransport
}

func readAuthenticatedSessionCredentials(rec *httptest.ResponseRecorder) authenticatedSessionCredentials {
	GinkgoHelper()

	res := rec.Result()
	wrapCloseWithErrorCheck(res.Body.Close)

	credentials := authenticatedSessionCredentials{
		Cookies: res.Cookies(),
	}
	if csrfToken := rec.Header().Get("X-CSRF-Token"); csrfToken != "" {
		credentials.CSRFToken = csrfToken
		credentials.CSRFTransport = csrfTokenTransportHeader
		return credentials
	}

	for _, c := range credentials.Cookies {
		if c.Name == "leafwiki_csrf" || c.Name == "__Host-leafwiki_csrf" {
			credentials.CSRFToken = c.Value
			credentials.CSRFTransport = csrfTokenTransportCookie
			return credentials
		}
	}

	return credentials
}

func haveCookieNamed(names ...string) types.GomegaMatcher {
	matchers := make([]types.GomegaMatcher, 0, len(names))
	for _, name := range names {
		matchers = append(matchers, Equal(name))
	}
	return ContainElement(HaveField("Name", SatisfyAny(matchers...)))
}

func haveAuthSessionCookies() types.GomegaMatcher {
	return SatisfyAll(
		haveCookieNamed("leafwiki_at", "__Host-leafwiki_at"),
		haveCookieNamed("leafwiki_rt", "__Host-leafwiki_rt"),
	)
}

func haveAuthenticatedSessionCredentials() types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Cookies", haveAuthSessionCookies()),
		HaveField("CSRFToken", Not(BeEmpty())),
		HaveField("CSRFTransport", SatisfyAny(
			Equal(csrfTokenTransportHeader),
			Equal(csrfTokenTransportCookie),
		)),
	)
}

func authenticatedRequest(router http.Handler, method, url string, body *strings.Reader) *httptest.ResponseRecorder {
	GinkgoHelper()

	loginBody := `{"identifier": "admin", "password": "admin"}`
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	Expect(loginRec).To(HaveHTTPStatus(http.StatusOK), "Failed to login: %d - %s", loginRec.Code, loginRec.Body.String())

	credentials := readAuthenticatedSessionCredentials(loginRec)
	Expect(credentials).To(haveAuthenticatedSessionCredentials())

	// Perform authenticated request
	if body == nil {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, url, body)
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range credentials.Cookies {
		req.AddCookie(cookie)
	}

	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		req.Header.Set("X-CSRF-Token", credentials.CSRFToken)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func authenticatedRequestAs(router http.Handler, username, password, method, url string, body *strings.Reader) *httptest.ResponseRecorder {
	GinkgoHelper()

	loginData := map[string]string{
		"identifier": username,
		"password":   password,
	}
	loginBodyBytes, _ := json.Marshal(loginData)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBodyBytes))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	Expect(loginRec).To(HaveHTTPStatus(http.StatusOK), "Failed to login as %s: %d - %s", username, loginRec.Code, loginRec.Body.String())

	credentials := readAuthenticatedSessionCredentials(loginRec)
	Expect(credentials).To(haveAuthenticatedSessionCredentials())

	// Perform authenticated request
	var reqBody io.Reader
	if body != nil {
		reqBody = body
	}
	req := httptest.NewRequest(method, url, reqBody)
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range credentials.Cookies {
		req.AddCookie(cookie)
	}

	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		req.Header.Set("X-CSRF-Token", credentials.CSRFToken)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func haveNoStoreHeaders() types.GomegaMatcher {
	return SatisfyAll(
		HaveHTTPHeaderWithValue("Cache-Control", "no-store"),
		HaveHTTPHeaderWithValue("Pragma", "no-cache"),
		HaveHTTPHeaderWithValue("Expires", "Thu, 01 Jan 1970 00:00:00 GMT"),
	)
}

func haveNullAPIKeyLifecycleMetadata() types.GomegaMatcher {
	return SatisfyAll(
		HaveKeyWithValue("lastUsedAt", BeNil()),
		HaveKeyWithValue("revokedAt", BeNil()),
	)
}

type apiPageDTO struct {
	ID             string                 `json:"id"`
	Title          string                 `json:"title"`
	Slug           string                 `json:"slug"`
	Content        string                 `json:"content"`
	Path           string                 `json:"path"`
	Version        string                 `json:"version"`
	Kind           tree.NodeKind          `json:"kind"`
	ContentPath    string                 `json:"contentPath"`
	ReadmeFallback bool                   `json:"readmeFallback"`
	Children       []*apiPageDTO          `json:"children"`
	Tags           []string               `json:"tags"`
	Properties     map[string]interface{} `json:"properties"`
}

type apiPermalinkTargetDTO struct {
	ID   string        `json:"id"`
	Slug string        `json:"slug"`
	Path string        `json:"path"`
	Kind tree.NodeKind `json:"kind"`
}

type apiTaggedPageSummaryDTO struct {
	Kind    tree.NodeKind `json:"kind"`
	Excerpt string        `json:"excerpt"`
}

type createPagePayloadDTO struct {
	Title    string        `json:"title"`
	Slug     string        `json:"slug"`
	ParentID string        `json:"parentId,omitempty"`
	Kind     tree.NodeKind `json:"kind,omitempty"`
}

func createPageViaAPI(router http.Handler, title, slug string, parentID *string, kind *tree.NodeKind) *apiPageDTO {
	GinkgoHelper()

	payload := createPagePayloadDTO{
		Title: title,
		Slug:  slug,
	}
	if parentID != nil {
		payload.ParentID = *parentID
	}
	if kind != nil {
		payload.Kind = *kind
	}

	body, err := json.Marshal(payload)
	Expect(err).NotTo(HaveOccurred(), "Marshal(create page payload) failed: %v", err)

	rec := authenticatedRequest(router, http.MethodPost, "/api/pages", strings.NewReader(string(body)))
	Expect(rec).To(HaveHTTPStatus(http.StatusCreated), "Expected 201 Created, got %d - %s", rec.Code, rec.Body.String())

	var page apiPageDTO
	{
		err := json.Unmarshal(rec.Body.Bytes(), &page)
		Expect(err).NotTo(HaveOccurred(), "Unmarshal(create page response) failed: %v", err)
	}

	return &page
}

func getPageByPathViaAPI(router http.Handler, path string) *apiPageDTO {
	GinkgoHelper()

	rec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path="+path, nil)
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

	var page apiPageDTO
	{
		err := json.Unmarshal(rec.Body.Bytes(), &page)
		Expect(err).NotTo(HaveOccurred(), "Unmarshal(get page by path response) failed: %v", err)
	}

	return &page
}

func getPermalinkTargetViaAPI(router http.Handler, id string) *apiPermalinkTargetDTO {
	GinkgoHelper()

	rec := authenticatedRequest(router, http.MethodGet, "/api/pages/permalink/"+id, nil)
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

	var target apiPermalinkTargetDTO
	{
		err := json.Unmarshal(rec.Body.Bytes(), &target)
		Expect(err).NotTo(HaveOccurred(), "Unmarshal(get permalink target response) failed: %v", err)
	}

	return &target
}

func getTreeViaAPI(router http.Handler) *apiPageDTO {
	GinkgoHelper()

	rec := authenticatedRequest(router, http.MethodGet, "/api/tree", nil)
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

	var node apiPageDTO
	{
		err := json.Unmarshal(rec.Body.Bytes(), &node)
		Expect(err).NotTo(HaveOccurred(), "Unmarshal(tree response) failed: %v", err)
	}

	return &node
}

func deletePageViaAPI(router http.Handler, pageID string, version string, recursive bool) {
	GinkgoHelper()

	url := "/api/pages/" + pageID + "?version=" + version
	if recursive {
		url += "&recursive=true"
	}

	rec := authenticatedRequest(router, http.MethodDelete, url, nil)
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

}

func listAssetsViaAPI(router http.Handler, pageID string) []string {
	GinkgoHelper()

	rec := authenticatedRequest(router, http.MethodGet, "/api/pages/"+pageID+"/assets", nil)
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

	var resp struct {
		Files []string `json:"files"`
	}
	{
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		Expect(err).NotTo(HaveOccurred(), "Unmarshal(list assets response) failed: %v", err)
	}

	return resp.Files
}

func uploadAssetViaAPI(router http.Handler, pageID, filename, content string) string {
	GinkgoHelper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", filename)
	Expect(err).NotTo(HaveOccurred(), "CreateFormFile failed: %v", err)
	{

		_, err := part.Write([]byte(content))
		Expect(err).NotTo(HaveOccurred(), "Write(asset payload) failed: %v", err)
	}
	{

		err := writer.Close()
		Expect(err).NotTo(HaveOccurred(), "Close(writer) failed: %v", err)
	}

	loginBody := `{"identifier": "admin", "password": "admin"}`
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	Expect(loginRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on login, got %d - %s", loginRec.Code, loginRec.Body.String())

	loginRes := loginRec.Result()
	wrapCloseWithErrorCheck(loginRes.Body.Close)

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
	Expect(csrfToken).NotTo(BeEmpty(), "Expected CSRF token after login, got none")

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/pages/"+pageID+"/assets", body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadReq.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		uploadReq.AddCookie(cookie)
	}

	uploadRec := httptest.NewRecorder()
	router.ServeHTTP(uploadRec, uploadReq)
	Expect(uploadRec).To(HaveHTTPStatus(http.StatusCreated), "Expected 201 Created on upload, got %d - %s", uploadRec.Code, uploadRec.Body.String())

	var uploadResp map[string]string
	{
		err := json.Unmarshal(uploadRec.Body.Bytes(), &uploadResp)
		Expect(err).NotTo(HaveOccurred(), "Unmarshal(upload asset response) failed: %v", err)
	}

	return uploadResp["file"]
}

func getLatestRevisionViaAPI(router http.Handler, pageID string) map[string]any {
	GinkgoHelper()

	rec := authenticatedRequest(router, http.MethodGet, "/api/pages/"+pageID+"/revisions/latest", nil)
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

	var rev map[string]any
	{
		err := json.Unmarshal(rec.Body.Bytes(), &rev)
		Expect(err).NotTo(HaveOccurred(), "Unmarshal(latest revision response) failed: %v", err)
	}

	return rev
}

func getAdminUserIDViaAPI(router http.Handler) string {
	GinkgoHelper()

	rec := authenticatedRequest(router, http.MethodGet, "/api/users", nil)
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

	var users []map[string]any
	{
		err := json.Unmarshal(rec.Body.Bytes(), &users)
		Expect(err).NotTo(HaveOccurred(), "Unmarshal(users response) failed: %v", err)
	}

	for _, user := range users {
		if role, _ := user["role"].(string); role == "admin" {
			if id, _ := user["id"].(string); id != "" {
				return id
			}
		}
	}
	Fail(fmt.Sprint("admin user not found"))
	return ""
}

func writePageMarkdownForTest(w *wiki.Wiki, page *apiPageDTO, raw string) {
	GinkgoHelper()

	pagePath := filepath.Join(w.GetRootDir(), filepath.FromSlash(page.Path)+".md")
	{
		err := os.WriteFile(pagePath, []byte(raw), 0o644)
		Expect(err).NotTo(HaveOccurred(), "WriteFile(page markdown) failed: %v", err)
	}

}

func uploadBrandingLogoViaAPI(router http.Handler, filename string, content []byte) {
	GinkgoHelper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	Expect(err).NotTo(HaveOccurred(), "CreateFormFile failed: %v", err)
	{

		_, err := part.Write(content)
		Expect(err).NotTo(HaveOccurred(), "Write(logo payload) failed: %v", err)
	}
	{

		err := writer.Close()
		Expect(err).NotTo(HaveOccurred(), "Close(writer) failed: %v", err)
	}

	loginBody := `{"identifier": "admin", "password": "admin"}`
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	Expect(loginRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on login, got %d - %s", loginRec.Code, loginRec.Body.String())

	loginRes := loginRec.Result()
	wrapCloseWithErrorCheck(loginRes.Body.Close)

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
	Expect(csrfToken).NotTo(BeEmpty(), "Expected CSRF token after login, got none")

	req := httptest.NewRequest(http.MethodPost, "/api/branding/logo", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

}

func uploadBrandingFaviconViaAPI(router http.Handler, filename string, content []byte) {
	GinkgoHelper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	Expect(err).NotTo(HaveOccurred(), "CreateFormFile failed: %v", err)
	{

		_, err := part.Write(content)
		Expect(err).NotTo(HaveOccurred(), "Write(favicon payload) failed: %v", err)
	}
	{

		err := writer.Close()
		Expect(err).NotTo(HaveOccurred(), "Close(writer) failed: %v", err)
	}

	loginBody := `{"identifier": "admin", "password": "admin"}`
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	Expect(loginRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on login, got %d - %s", loginRec.Code, loginRec.Body.String())

	loginRes := loginRec.Result()
	wrapCloseWithErrorCheck(loginRes.Body.Close)

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
	Expect(csrfToken).NotTo(BeEmpty(), "Expected CSRF token after login, got none")

	req := httptest.NewRequest(http.MethodPost, "/api/branding/favicon", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

}

func importerFixturePathForHTTPTests(rel string) string {
	GinkgoHelper()

	return fixturePathForHTTPTests(rel, "../importer/fixtures", "internal/importer/fixtures")
}

func createZipFromDir(root string) []byte {
	GinkgoHelper()

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
	Expect(err).NotTo(HaveOccurred(), "create zip from dir: %v", err)
	{

		err := zipWriter.Close()
		Expect(err).NotTo(HaveOccurred(), "close zip writer: %v", err)
	}

	return body.Bytes()
}

var _ = Describe("HTTP router", func() {
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
		Expect(logs.String()).NotTo(ContainSubstring("http request"), "request log was written despite DisableRequestLog: %s", logs.String())

	})
})

var _ = Describe("HTTP router", func() {
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
		Expect(entries).To(ContainElement(SatisfyAll(
			HaveKeyWithValue("msg", "http request"),
			HaveKeyWithValue("method", http.MethodGet),
			HaveKeyWithValue("path", "/api/health"),
			HaveKeyWithValue("status", float64(http.StatusOK)),
			HaveKey("latency"),
			HaveKey("ip"),
		)), "request log entries = %#v", entries)

	})
})

var _ = Describe("HTTP router", func() {
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
		Expect(logs.String()).To(ContainSubstring("panic route"), "recovery log did not include panic text: %s", logs.String())

	})
})

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = DescribeTable("current-user responses are uncacheable",
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
	It("explains the insecure-transport requirement in the config route", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstanceWithAllowInsecure(w, false)

		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "expected status 400, got %d", rec.Code)
		Expect(rec).To(HaveHTTPBody(ContainSubstring("--allow-insecure")), "expected response to explain allow-insecure requirement, got %s", rec.Body.String())

	})
})

var _ = Describe("HTTP router", func() {
	It("explains the insecure-transport requirement during login", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstanceWithAllowInsecure(w, false)

		loginBody := `{"identifier": "admin", "password": "admin"}`
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "expected status 400, got %d with body %s", rec.Code, rec.Body.String())
		Expect(rec).To(HaveHTTPBody(ContainSubstring("--allow-insecure")), "expected response to explain allow-insecure requirement, got %s", rec.Body.String())

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects page creation when the title is missing", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `{"title": ""}`
		rec := authenticatedRequest(router, http.MethodPost, "/api/pages", strings.NewReader(body))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected 400 Bad Request for missing title, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects page creation with invalid JSON", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `this is not valid json`
		rec := authenticatedRequest(router, http.MethodPost, "/api/pages", strings.NewReader(body))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected 400 Bad Request for invalid JSON, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

		Expect(resp).To(HaveKeyWithValue("enableLinkRefactor", BeTrue()), "Expected enableLinkRefactor=true in config response, got %v", resp)

	})
})

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

		Expect(resp).To(HaveKeyWithValue("enableWorkspaceSync", BeFalse()), "Expected enableWorkspaceSync=false in config response, got %v", resp)

	})
})

var _ = Describe("HTTP router", func() {
	It("returns workspace sync status when sync is enabled", func() {

		dataDir := httpTestTempDir()
		rootDir := filepath.Join(httpTestTempDir(), "content")
		if err := os.WriteFile(filepath.Join(rootDir, "page.md"), []byte("---\nleafwiki_id: page\nleafwiki_title: Page\n---\n# Page\n"), 0o644); err != nil {
			{
				err := os.MkdirAll(rootDir, 0o755)
				Expect(err).NotTo(HaveOccurred(), "create root dir: %v", err)
			}
			{

				err := os.WriteFile(filepath.Join(rootDir, "page.md"), []byte("---\nleafwiki_id: page\nleafwiki_title: Page\n---\n# Page\n"), 0o644)
				Expect(err).NotTo(HaveOccurred(), "write page: %v", err)
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
		Expect(err).NotTo(HaveOccurred(), "NewWiki: %v", err)

		wrapCloseWithErrorCheck(w.Close)

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
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "GET status = %d: %s", rec.Code, rec.Body.String())

		var resp map[string]any
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "decode status: %v", err)
		}
		Expect(resp).To(SatisfyAll(
			HaveKeyWithValue("enabled", BeTrue()),
			HaveKeyWithValue("lastCommitHash", Not(BeEmpty())),
		), "workspace sync status response = %#v", resp)

	})
})

var _ = Describe("HTTP router", func() {
	It("refreshes workspace sync after markdown is created on disk", func() {

		dataDir := httpTestTempDir()
		rootDir := filepath.Join(httpTestTempDir(), "content")
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
		Expect(err).NotTo(HaveOccurred(), "NewWiki: %v", err)

		wrapCloseWithErrorCheck(w.Close)
		router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
			PublicAccess:            true,
			AllowInsecure:           true,
			AuthDisabled:            true,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			EnableWorkspaceSync:     true,
		})
		{

			err := os.WriteFile(filepath.Join(rootDir, "direct.md"), []byte("---\nleafwiki_id: direct\nleafwiki_title: Direct\n---\n# Direct\n"), 0o644)
			Expect(err).NotTo(HaveOccurred(), "write direct markdown: %v", err)
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
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "POST refresh = %d: %s", rec.Code, rec.Body.String())

		pageReq := httptest.NewRequest(http.MethodGet, "/api/pages/by-path?path=direct", nil)
		pageRec := httptest.NewRecorder()
		router.ServeHTTP(pageRec, pageReq)
		Expect(pageRec).To(HaveHTTPStatus(http.StatusOK), "GET synced page = %d: %s", pageRec.Code, pageRec.Body.String())

		var page apiPageDTO
		{
			err := json.Unmarshal(pageRec.Body.Bytes(), &page)
			Expect(err).NotTo(HaveOccurred(), "decode synced page: %v", err)
		}

		Expect(page).To(SatisfyAll(
			HaveField("ID", "direct"),
			HaveField("Title", "Direct"),
		), "synced page = %#v, want direct page", page)

	})
})

var _ = Describe("HTTP router", func() {
	It("lists workspace sync snapshots when sync is enabled", func() {

		dataDir := httpTestTempDir()
		rootDir := filepath.Join(httpTestTempDir(), "content")
		{
			err := os.MkdirAll(rootDir, 0o755)
			Expect(err).NotTo(HaveOccurred(), "create root dir: %v", err)
		}
		{

			err := os.WriteFile(filepath.Join(rootDir, "page.md"), []byte("---\nleafwiki_id: page\nleafwiki_title: Page\n---\n# Page\n"), 0o644)
			Expect(err).NotTo(HaveOccurred(), "write page: %v", err)
		}

		w, err := wiki.NewWiki(&wiki.WikiOptions{
			Workspace:           wiki.Workspace{DataDir: dataDir, RootDir: rootDir},
			AdminPassword:       "admin",
			JWTSecret:           "secretkey",
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
			AuthDisabled:        true,
		})
		Expect(err).NotTo(HaveOccurred(), "NewWiki: %v", err)

		wrapCloseWithErrorCheck(w.Close)
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
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "GET snapshots = %d: %s", rec.Code, rec.Body.String())

		var resp struct {
			Snapshots  []map[string]any `json:"snapshots"`
			NextCursor string           `json:"nextCursor"`
		}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "decode snapshots: %v", err)
		}

		Expect(resp.Snapshots).To(ContainElement(HaveKeyWithValue("id", Not(BeEmpty()))), "snapshots missing commit id: %#v", resp)

	})
})

var _ = Describe("HTTP router", func() {
	It("limits workspace sync snapshots to the requested page size", func() {

		dataDir := httpTestTempDir()
		rootDir := filepath.Join(httpTestTempDir(), "content")
		{
			err := os.MkdirAll(rootDir, 0o755)
			Expect(err).NotTo(HaveOccurred(), "create root dir: %v", err)
		}
		{

			err := os.WriteFile(filepath.Join(rootDir, "page.md"), []byte("---\nleafwiki_id: page\nleafwiki_title: Page\n---\n# Page\n"), 0o644)
			Expect(err).NotTo(HaveOccurred(), "write page: %v", err)
		}

		w, err := wiki.NewWiki(&wiki.WikiOptions{
			Workspace:           wiki.Workspace{DataDir: dataDir, RootDir: rootDir},
			AdminPassword:       "admin",
			JWTSecret:           "secretkey",
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
			AuthDisabled:        true,
		})
		Expect(err).NotTo(HaveOccurred(), "NewWiki: %v", err)

		wrapCloseWithErrorCheck(w.Close)
		{
			err := os.WriteFile(filepath.Join(rootDir, "page.md"), []byte("---\nleafwiki_id: page\nleafwiki_title: Page\n---\n# Page 2\n"), 0o644)
			Expect(err).NotTo(HaveOccurred(), "write page update: %v", err)
		}
		{

			_, err := w.WorkspaceSyncRefresh(context.Background(), workspacesync.SyncRequest{
				Reason: workspacesync.ReasonExplicit,
				Source: workspacesync.SourceFilesystem,
				Actor:  workspacesync.PublicEditorActor(),
			})
			Expect(err).NotTo(HaveOccurred(), "WorkspaceSyncRefresh: %v", err)
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
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "GET snapshots limit = %d: %s", rec.Code, rec.Body.String())

		var resp struct {
			Snapshots  []map[string]any `json:"snapshots"`
			NextCursor string           `json:"nextCursor"`
		}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "decode snapshots: %v", err)
		}
		Expect(resp).To(SatisfyAll(
			HaveField("Snapshots", HaveLen(1)),
			HaveField("NextCursor", Not(BeEmpty())),
		), "first page snapshot response = %#v", resp)

		firstID := resp.Snapshots[0]["id"]

		req = httptest.NewRequest(http.MethodGet, "/api/workspace-sync/snapshots?limit=1&cursor="+resp.NextCursor, nil)
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "GET snapshots second page = %d: %s", rec.Code, rec.Body.String())

		resp = struct {
			Snapshots  []map[string]any `json:"snapshots"`
			NextCursor string           `json:"nextCursor"`
		}{}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "decode second page snapshots: %v", err)
		}
		Expect(resp.Snapshots).To(HaveExactElements(
			HaveKeyWithValue("id", Not(Equal(firstID))),
		), "second page returned same snapshot id %v: %#v", firstID, resp.Snapshots)

	})
})

var _ = Describe("HTTP router", func() {
	It("keeps workspace sync snapshot cursors stable after newer commits", func() {

		dataDir := httpTestTempDir()
		rootDir := filepath.Join(httpTestTempDir(), "content")
		{
			err := os.MkdirAll(rootDir, 0o755)
			Expect(err).NotTo(HaveOccurred(), "create root dir: %v", err)
		}
		{

			err := os.WriteFile(filepath.Join(rootDir, "page.md"), []byte("<!-- leafwiki\nversion: 1\npage:\n  id: page\n  title: Page\n-->\n\n# Page 1\n"), 0o644)
			Expect(err).NotTo(HaveOccurred(), "write page: %v", err)
		}

		w, err := wiki.NewWiki(&wiki.WikiOptions{
			Workspace:           wiki.Workspace{DataDir: dataDir, RootDir: rootDir},
			AdminPassword:       "admin",
			JWTSecret:           "secretkey",
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
			AuthDisabled:        true,
		})
		Expect(err).NotTo(HaveOccurred(), "NewWiki: %v", err)

		wrapCloseWithErrorCheck(w.Close)
		initialCommit := w.WorkspaceSyncStatus().LastCommitHash
		Expect(initialCommit).NotTo(BeZero(), "initial workspace commit is empty")
		{

			err := os.WriteFile(filepath.Join(rootDir, "page.md"), []byte("<!-- leafwiki\nversion: 1\npage:\n  id: page\n  title: Page\n-->\n\n# Page 2\n"), 0o644)
			Expect(err).NotTo(HaveOccurred(), "write page update: %v", err)
		}
		{

			_, err := w.WorkspaceSyncRefresh(context.Background(), workspacesync.SyncRequest{
				Reason: workspacesync.ReasonExplicit,
				Source: workspacesync.SourceFilesystem,
				Actor:  workspacesync.PublicEditorActor(),
			})
			Expect(err).NotTo(HaveOccurred(), "WorkspaceSyncRefresh page 2: %v", err)
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
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "GET snapshots first page = %d: %s", rec.Code, rec.Body.String())

		var resp struct {
			Snapshots  []map[string]any `json:"snapshots"`
			NextCursor string           `json:"nextCursor"`
		}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "decode first page snapshots: %v", err)
		}

		Expect(resp).To(SatisfyAll(
			HaveField("Snapshots", HaveLen(1)),
			HaveField("NextCursor", Not(BeEmpty())),
		), "first page response = %#v, want one snapshot with cursor", resp)
		firstPageID, _ := resp.Snapshots[0]["id"].(string)
		{

			err := os.WriteFile(filepath.Join(rootDir, "page.md"), []byte("<!-- leafwiki\nversion: 1\npage:\n  id: page\n  title: Page\n-->\n\n# Page 3\n"), 0o644)
			Expect(err).NotTo(HaveOccurred(), "write page newer update: %v", err)
		}
		{

			_, err := w.WorkspaceSyncRefresh(context.Background(), workspacesync.SyncRequest{
				Reason: workspacesync.ReasonExplicit,
				Source: workspacesync.SourceFilesystem,
				Actor:  workspacesync.PublicEditorActor(),
			})
			Expect(err).NotTo(HaveOccurred(), "WorkspaceSyncRefresh page 3: %v", err)
		}

		req = httptest.NewRequest(http.MethodGet, "/api/workspace-sync/snapshots?limit=1&cursor="+resp.NextCursor, nil)
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "GET snapshots second page = %d: %s", rec.Code, rec.Body.String())

		resp = struct {
			Snapshots  []map[string]any `json:"snapshots"`
			NextCursor string           `json:"nextCursor"`
		}{}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "decode second page snapshots: %v", err)
		}
		Expect(resp.Snapshots).To(HaveExactElements(HaveKeyWithValue("id", SatisfyAll(
			Not(Equal(firstPageID)),
			WithTransform(func(raw string) workspacesync.CommitHash {
				return workspacesync.CommitHashFromString(raw)
			}, Equal(initialCommit)),
		))), "second page snapshots = %#v, want original older commit %s without duplicating first page %s", resp.Snapshots, initialCommit, firstPageID)

	})
})

var _ = Describe("HTTP router", func() {
	It("allows unauthenticated workspace sync status reads in public mode", func() {

		dataDir := httpTestTempDir()
		rootDir := filepath.Join(httpTestTempDir(), "content")
		{
			err := os.MkdirAll(rootDir, 0o755)
			Expect(err).NotTo(HaveOccurred(), "create root dir: %v", err)
		}
		{

			err := os.WriteFile(filepath.Join(rootDir, "page.md"), []byte("---\nleafwiki_id: page\nleafwiki_title: Page\n---\n# Page\n"), 0o644)
			Expect(err).NotTo(HaveOccurred(), "write page: %v", err)
		}

		w, err := wiki.NewWiki(&wiki.WikiOptions{
			Workspace:           wiki.Workspace{DataDir: dataDir, RootDir: rootDir},
			AdminPassword:       "admin",
			JWTSecret:           "secretkey",
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
		})
		Expect(err).NotTo(HaveOccurred(), "NewWiki: %v", err)

		wrapCloseWithErrorCheck(w.Close)
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
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "GET public workspace status = %d: %s", rec.Code, rec.Body.String())

	})
})

var _ = Describe("HTTP router", func() {
	It("restores markdown files from a workspace sync snapshot", func() {

		dataDir := httpTestTempDir()
		rootDir := filepath.Join(httpTestTempDir(), "content")
		{
			err := os.MkdirAll(rootDir, 0o755)
			Expect(err).NotTo(HaveOccurred(), "create root dir: %v", err)
		}
		{

			err := os.WriteFile(filepath.Join(rootDir, "one.md"), []byte("---\nleafwiki_id: one\nleafwiki_title: One\n---\n# One A\n"), 0o644)
			Expect(err).NotTo(HaveOccurred(), "write one.md: %v", err)
		}

		w, err := wiki.NewWiki(&wiki.WikiOptions{
			Workspace:           wiki.Workspace{DataDir: dataDir, RootDir: rootDir},
			AdminPassword:       "admin",
			JWTSecret:           "secretkey",
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
			AuthDisabled:        true,
		})
		Expect(err).NotTo(HaveOccurred(), "NewWiki: %v", err)

		wrapCloseWithErrorCheck(w.Close)
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
		Expect(status.LastCommitHash).NotTo(BeZero(), "missing initial snapshot hash")
		{

			err := os.WriteFile(filepath.Join(rootDir, "one.md"), []byte("---\nleafwiki_id: one\nleafwiki_title: One\n---\n# One current\n"), 0o644)
			Expect(err).NotTo(HaveOccurred(), "write current one.md: %v", err)
		}
		{

			err := os.WriteFile(filepath.Join(rootDir, "two.md"), []byte("---\nleafwiki_id: two\nleafwiki_title: Two\n---\n# Two current\n"), 0o644)
			Expect(err).NotTo(HaveOccurred(), "write two.md: %v", err)
		}
		{

			err := os.WriteFile(filepath.Join(rootDir, "image.png"), []byte("png"), 0o644)
			Expect(err).NotTo(HaveOccurred(), "write image: %v", err)
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
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "POST restore = %d: %s", rec.Code, rec.Body.String())

		raw, err := os.ReadFile(filepath.Join(rootDir, "one.md"))
		Expect(err).NotTo(HaveOccurred(), "read restored one.md: %v", err)
		Expect(string(raw)).To(ContainSubstring("# One A"), "one.md was not restored: %q", string(raw))
		Expect(filepath.Join(rootDir, "two.md")).NotTo(BeAnExistingFile(), "two.md should be removed")

		raw, err = os.ReadFile(filepath.Join(rootDir, "image.png"))
		Expect(err).NotTo(HaveOccurred(), "image.png = %q, %v; want untouched png", string(raw), err)
		Expect(string(raw)).To(Equal("png"), "image.png = %q, %v; want untouched png", string(raw), err)
		snapshots, err := w.WorkspaceSyncSnapshots(context.Background(), 1)
		Expect(err).NotTo(HaveOccurred(), "WorkspaceSyncSnapshots: %v", err)
		Expect(snapshots).To(ContainElement(HaveField("Source", string(workspacesync.SourceWeb))), "snapshots after restore = %#v", snapshots)

	})
})

var _ = Describe("HTTP router", func() {
	It("returns page revision history from the git-backed store", func() {

		dataDir := filepath.Join(httpTestTempDir(), "data")
		rootDir := filepath.Join(httpTestTempDir(), "content")
		w, err := wiki.NewWiki(&wiki.WikiOptions{
			Workspace:           wiki.Workspace{DataDir: dataDir, RootDir: rootDir},
			AdminPassword:       "admin",
			JWTSecret:           "secretkey",
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
			AuthDisabled:        true,
		})
		Expect(err).NotTo(HaveOccurred(), "NewWiki: %v", err)

		wrapCloseWithErrorCheck(w.Close)
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
		Expect(configRec).To(HaveHTTPStatus(http.StatusOK), "GET config = %d: %s", configRec.Code, configRec.Body.String())

		csrfToken := configRec.Header().Get("X-CSRF-Token")
		createReq := httptest.NewRequest(http.MethodPost, "/api/pages", strings.NewReader(`{"title":"Git History","slug":"git-history"}`))
		createReq.Header.Set("Content-Type", "application/json")
		createReq.Header.Set("X-CSRF-Token", csrfToken)
		for _, cookie := range configRec.Result().Cookies() {
			createReq.AddCookie(cookie)
		}
		createRec := httptest.NewRecorder()
		router.ServeHTTP(createRec, createReq)
		Expect(createRec).To(HaveHTTPStatus(http.StatusCreated), "POST page = %d: %s", createRec.Code, createRec.Body.String())

		var page apiPageDTO
		{
			err := json.Unmarshal(createRec.Body.Bytes(), &page)
			Expect(err).NotTo(HaveOccurred(), "decode page: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/pages/"+page.ID+"/revisions", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "GET workspace revisions = %d: %s", rec.Code, rec.Body.String())

		var resp struct {
			Revisions  []map[string]any `json:"revisions"`
			NextCursor string           `json:"nextCursor"`
		}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "decode revisions: %v", err)
		}
		Expect(resp.Revisions).To(ContainElement(SatisfyAll(
			HaveKeyWithValue("id", Not(BeEmpty())),
			HaveKeyWithValue("pageId", page.ID),
		)), "unexpected workspace revision: %#v", resp.Revisions)

	})
})

var _ = Describe("HTTP router", func() {
	It("serves revision snapshots and restores them through the git backend", func() {

		dataDir := filepath.Join(httpTestTempDir(), "data")
		rootDir := filepath.Join(httpTestTempDir(), "content")
		{
			err := os.MkdirAll(rootDir, 0o755)
			Expect(err).NotTo(HaveOccurred(), "create root dir: %v", err)
		}

		previous := `---
leafwiki_id: restore-page
leafwiki_title: Restore Page
---

# Restore Page

previous content`
		{
			err := os.WriteFile(filepath.Join(rootDir, "restore-page.md"), []byte(previous), 0o644)
			Expect(err).NotTo(HaveOccurred(), "write previous markdown: %v", err)
		}

		w, err := wiki.NewWiki(&wiki.WikiOptions{
			Workspace:           wiki.Workspace{DataDir: dataDir, RootDir: rootDir},
			AdminPassword:       "admin",
			JWTSecret:           "secretkey",
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
			AuthDisabled:        true,
		})
		Expect(err).NotTo(HaveOccurred(), "NewWiki: %v", err)

		wrapCloseWithErrorCheck(w.Close)
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
		Expect(revisionsRec).To(HaveHTTPStatus(http.StatusOK), "GET revisions = %d: %s", revisionsRec.Code, revisionsRec.Body.String())

		type revisionListItem struct {
			ID string `json:"id"`
		}
		var revisionsResp struct {
			Revisions []revisionListItem `json:"revisions"`
		}
		{
			err := json.Unmarshal(revisionsRec.Body.Bytes(), &revisionsResp)
			Expect(err).NotTo(HaveOccurred(), "decode revisions: %v", err)
		}
		Expect(revisionsResp.Revisions).To(SatisfyAll(
			Not(BeEmpty()),
			HaveEach(HaveField("ID", Not(BeEmpty()))),
		), "expected initial revision with an id: %#v", revisionsResp)

		oldRevisionID := revisionsResp.Revisions[0].ID

		current := strings.Replace(previous, "previous content", "current content", 1)
		{
			err := os.WriteFile(filepath.Join(rootDir, "restore-page.md"), []byte(current), 0o644)
			Expect(err).NotTo(HaveOccurred(), "write current markdown: %v", err)
		}
		{

			_, err := w.WorkspaceSyncRefresh(context.Background(), workspacesync.SyncRequest{
				Reason: workspacesync.ReasonExplicit,
				Source: workspacesync.SourceFilesystem,
				Actor:  workspacesync.PublicEditorActor(),
			})
			Expect(err).NotTo(HaveOccurred(), "WorkspaceSyncRefresh current: %v", err)
		}

		snapshotReq := httptest.NewRequest(http.MethodGet, "/api/pages/restore-page/revisions/"+oldRevisionID, nil)
		snapshotRec := httptest.NewRecorder()
		router.ServeHTTP(snapshotRec, snapshotReq)
		Expect(snapshotRec).To(HaveHTTPStatus(http.StatusOK), "GET revision snapshot = %d: %s", snapshotRec.Code, snapshotRec.Body.String())

		var snapshot struct {
			Content string `json:"content"`
			Assets  []any  `json:"assets"`
		}
		{
			err := json.Unmarshal(snapshotRec.Body.Bytes(), &snapshot)
			Expect(err).NotTo(HaveOccurred(), "decode snapshot: %v", err)
		}

		Expect(snapshot).To(SatisfyAll(
			HaveField("Content", ContainSubstring("previous content")),
			HaveField("Assets", BeEmpty()),
		), "unexpected snapshot: %#v", snapshot)

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
		Expect(restoreRec).To(HaveHTTPStatus(http.StatusOK), "POST restore = %d: %s", restoreRec.Code, restoreRec.Body.String())

		raw, err := os.ReadFile(filepath.Join(rootDir, "restore-page.md"))
		Expect(err).NotTo(HaveOccurred(), "read restored markdown: %v", err)
		Expect(string(raw)).To(ContainSubstring("previous content"), "restored markdown = %q, want previous content", string(raw))

	})
})

var _ = Describe("HTTP router", func() {
	It("returns the frontend JSON shape for refactor previews", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)

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
		target := createPageViaAPI(router, "Target", "target", nil, pageNodeKind())
		ref := createPageViaAPI(router, "Ref", "ref", nil, pageNodeKind())

		updateBody := strings.NewReader(`{"version":"` + ref.Version + `","title":"Ref","slug":"ref","content":"[Target](/target.md)"}`)
		updateRec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+ref.ID, updateBody)
		Expect(updateRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on page update, got %d - %s", updateRec.Code, updateRec.Body.String())

		previewBody := strings.NewReader(`{"kind":"rename","title":"Target","slug":"target-renamed"}`)
		previewRec := authenticatedRequest(router, http.MethodPost, "/api/pages/"+target.ID+"/refactor/preview", previewBody)
		Expect(previewRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on refactor preview, got %d - %s", previewRec.Code, previewRec.Body.String())

		var resp map[string]any
		{
			err := json.Unmarshal(previewRec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Invalid refactor preview JSON: %v", err)
		}
		Expect(resp).To(SatisfyAll(
			HaveKey("affectedPages"),
			Not(HaveKey("Counts")),
			Not(HaveKey("AffectedPages")),
			HaveKeyWithValue("counts", SatisfyAll(
				HaveKeyWithValue("affectedPages", BeNumerically("==", 1)),
				HaveKey("matchedLinks"),
			)),
		), "refactor preview response = %#v", resp)

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects refactor previews when the feature flag is disabled", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)

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

		target := createPageViaAPI(router, "Target", "target", nil, pageNodeKind())
		previewBody := strings.NewReader(`{"kind":"rename","title":"Target","slug":"target-renamed"}`)
		previewRec := authenticatedRequest(router, http.MethodPost, "/api/pages/"+target.ID+"/refactor/preview", previewBody)
		Expect(previewRec).To(HaveHTTPStatus(http.StatusNotFound), "Expected 404 when link refactor is disabled, got %d - %s", previewRec.Code, previewRec.Body.String())

	})
})

var _ = Describe("HTTP router", func() {
	It("records refactor application through git history without legacy revisions", func() {

		w := createWikiTestInstanceWithRevisionFlag(false)
		wrapCloseWithErrorCheck(w.Close)

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

		target := createPageViaAPI(router, "Target", "target", nil, pageNodeKind())
		ref := createPageViaAPI(router, "Ref", "ref", nil, pageNodeKind())

		updateBody := strings.NewReader(`{"version":"` + ref.Version + `","title":"Ref","slug":"ref","content":"[Target](/target.md)"}`)
		updateRec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+ref.ID, updateBody)
		Expect(updateRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on page update, got %d - %s", updateRec.Code, updateRec.Body.String())

		applyBody := strings.NewReader(`{"kind":"rename","version":"` + target.Version + `","title":"Target","slug":"target-renamed","rewriteLinks":true}`)
		applyRec := authenticatedRequest(router, http.MethodPost, "/api/pages/"+target.ID+"/refactor/apply", applyBody)
		Expect(applyRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on refactor apply, got %d - %s", applyRec.Code, applyRec.Body.String())

		refPageRec := authenticatedRequest(router, http.MethodGet, "/api/pages/"+ref.ID, strings.NewReader(""))
		Expect(refPageRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on ref page fetch, got %d - %s", refPageRec.Code, refPageRec.Body.String())

		var refPage map[string]any
		{
			err := json.Unmarshal(refPageRec.Body.Bytes(), &refPage)
			Expect(err).NotTo(HaveOccurred(), "Invalid ref page JSON: %v", err)
		}
		Expect(refPage).To(HaveKeyWithValue("content", "[Target](/target-renamed.md)"), "Expected rewritten ref content, got %#v", refPage)

		revisionsRec := authenticatedRequest(router, http.MethodGet, "/api/pages/"+target.ID+"/revisions", strings.NewReader(""))
		Expect(revisionsRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on Git-backed revisions endpoint, got %d - %s", revisionsRec.Code, revisionsRec.Body.String())

		revisionsDir := filepath.Join(w.GetStorageDir(), ".leafwiki", "revisions")
		Expect(revisionsDir).NotTo(BeADirectory(), "revision storage directory should not exist")

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects asset uploads that exceed the configured size limit", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)

		router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
			PublicAccess:            false,
			InjectCodeInHeader:      "",
			AllowInsecure:           true,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			HideLinkMetadataSection: false,
			MaxAssetUploadSizeBytes: 32,
		})

		page := createPageViaAPI(router, "Asset Limit Test", "asset-limit-test", nil, pageNodeKind())

		loginBody := `{"identifier": "admin", "password": "admin"}`
		loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		router.ServeHTTP(loginRec, loginReq)
		Expect(loginRec).To(HaveHTTPStatus(http.StatusOK), "Failed to login: %d - %s", loginRec.Code, loginRec.Body.String())

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
		Expect(err).NotTo(HaveOccurred(), "Failed to create form file: %v", err)
		{

			_, err := part.Write([]byte(strings.Repeat("a", 128)))
			Expect(err).NotTo(HaveOccurred(), "Failed to write file content: %v", err)
		}
		{

			err := writer.Close()
			Expect(err).NotTo(HaveOccurred(), "Failed to close multipart writer: %v", err)
		}

		uploadReq := httptest.NewRequest(http.MethodPost, "/api/pages/"+page.ID+"/assets", body)
		uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
		uploadReq.Header.Set("X-CSRF-Token", csrfToken)
		for _, cookie := range cookies {
			uploadReq.AddCookie(cookie)
		}

		uploadRec := httptest.NewRecorder()
		router.ServeHTTP(uploadRec, uploadReq)
		Expect(uploadRec).To(HaveHTTPStatus(http.StatusRequestEntityTooLarge), "Expected 413 Request Entity Too Large, got %d - %s", uploadRec.Code, uploadRec.Body.String())

		assetDir := filepath.Join(w.GetStorageDir(), "assets", page.ID)
		entries, err := os.ReadDir(assetDir)
		if err != nil {
			if os.IsNotExist(err) {
				return
			}
			Expect(err).NotTo(HaveOccurred(), "Failed to read asset directory: %v", err)
		}
		Expect(entries).To(HaveLen(0), "Expected no files after rejected upload, got %d", len(entries))

	})
})

var _ = Describe("HTTP router", func() {
	It("suggests a slug for a page title", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstanceWithRevision(w)

		rec := authenticatedRequest(router, http.MethodGet, "/api/pages/slug-suggestion?title=NewPage", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected status 200, got %d", rec.Code)

		var resp map[string]interface{}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Invalid JSON response: %v", err)
		}
		Expect(resp).To(HaveKeyWithValue("slug", "newpage"), "slug suggestion response = %#v", resp)

	})
})

var _ = Describe("HTTP router", func() {
	It("cancels the current import plan", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		var body bytes.Buffer
		writer := multipart.NewWriter(&body)

		fileWriter, err := writer.CreateFormFile("file", "fixture-1.zip")
		Expect(err).NotTo(HaveOccurred(), "CreateFormFile failed: %v", err)

		zipFile, err := os.Open("../importer/fixtures/fixture-1.zip")
		Expect(err).NotTo(HaveOccurred(), "Open fixture zip failed: %v", err)

		wrapCloseWithErrorCheck(zipFile.Close)
		{

			_, err := io.Copy(fileWriter, zipFile)
			Expect(err).NotTo(HaveOccurred(), "Copy zip fixture failed: %v", err)
		}
		{

			err := writer.Close()
			Expect(err).NotTo(HaveOccurred(), "Close multipart writer failed: %v", err)
		}

		loginBody := `{"identifier": "admin", "password": "admin"}`
		loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		router.ServeHTTP(loginRec, loginReq)
		Expect(loginRec).To(HaveHTTPStatus(http.StatusOK), "Failed to login: %d - %s", loginRec.Code, loginRec.Body.String())

		credentials := readAuthenticatedSessionCredentials(loginRec)
		Expect(credentials).To(haveAuthenticatedSessionCredentials())

		createReq := httptest.NewRequest(http.MethodPost, "/api/import/plan", &body)
		createReq.Header.Set("Content-Type", writer.FormDataContentType())
		createReq.Header.Set("X-CSRF-Token", credentials.CSRFToken)
		for _, cookie := range credentials.Cookies {
			createReq.AddCookie(cookie)
		}

		createRec := httptest.NewRecorder()
		router.ServeHTTP(createRec, createReq)
		Expect(createRec).To(HaveHTTPStatus(http.StatusOK), "Expected status 200 when creating import plan, got %d: %s", createRec.Code, createRec.Body.String())

		cancelReq := httptest.NewRequest(http.MethodDelete, "/api/import/plan", nil)
		cancelReq.Header.Set("Content-Type", "application/json")
		cancelReq.Header.Set("X-CSRF-Token", credentials.CSRFToken)
		for _, cookie := range credentials.Cookies {
			cancelReq.AddCookie(cookie)
		}

		cancelRec := httptest.NewRecorder()
		router.ServeHTTP(cancelRec, cancelReq)
		Expect(cancelRec).To(HaveHTTPStatus(http.StatusOK), "Expected status 200 when canceling import plan, got %d: %s", cancelRec.Code, cancelRec.Body.String())
		{

			got := strings.TrimSpace(cancelRec.Body.String())
			Expect(got).To(Equal("null"), "Expected null response body when clearing import plan, got %q", got)
		}

		getRec := authenticatedRequest(router, http.MethodGet, "/api/import/plan", nil)
		Expect(getRec).To(HaveHTTPStatus(http.StatusNotFound), "Expected status 404 when fetching canceled import plan, got %d: %s", getRec.Code, getRec.Body.String())

		var resp map[string]interface{}
		{
			err := json.Unmarshal(getRec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Invalid JSON response: %v", err)
		}

		Expect(resp).To(HaveKeyWithValue("error", HaveKey("code")), "Expected structured error response after canceling import plan, got: %v", resp)

	})
})

var _ = Describe("HTTP router", func() {
	It("imports pages links and assets from an uploaded zip", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		fixtureDir := importerFixturePathForHTTPTests("link-assets-package")
		zipBytes := createZipFromDir(fixtureDir)

		loginBody := `{"identifier": "admin", "password": "admin"}`
		loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		router.ServeHTTP(loginRec, loginReq)
		Expect(loginRec).To(HaveHTTPStatus(http.StatusOK), "Failed to login: %d - %s", loginRec.Code, loginRec.Body.String())

		credentials := readAuthenticatedSessionCredentials(loginRec)
		Expect(credentials).To(haveAuthenticatedSessionCredentials())

		var planBody bytes.Buffer
		planWriter := multipart.NewWriter(&planBody)
		fileWriter, err := planWriter.CreateFormFile("file", "link-assets-package.zip")
		Expect(err).NotTo(HaveOccurred(), "CreateFormFile failed: %v", err)
		{

			_, err := fileWriter.Write(zipBytes)
			Expect(err).NotTo(HaveOccurred(), "Write zip bytes failed: %v", err)
		}
		{

			err := planWriter.Close()
			Expect(err).NotTo(HaveOccurred(), "Close multipart writer failed: %v", err)
		}

		planReq := httptest.NewRequest(http.MethodPost, "/api/import/plan", &planBody)
		planReq.Header.Set("Content-Type", planWriter.FormDataContentType())
		planReq.Header.Set("X-CSRF-Token", credentials.CSRFToken)
		for _, cookie := range credentials.Cookies {
			planReq.AddCookie(cookie)
		}

		planRec := httptest.NewRecorder()
		router.ServeHTTP(planRec, planReq)
		Expect(planRec).To(HaveHTTPStatus(http.StatusOK), "Expected status 200 when creating import plan, got %d: %s", planRec.Code, planRec.Body.String())

		var planResp struct {
			Items []map[string]any `json:"items"`
		}
		{
			err := json.Unmarshal(planRec.Body.Bytes(), &planResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid import plan response JSON: %v", err)
		}
		Expect(planResp.Items).To(HaveLen(5), "expected 5 plan items, got %d", len(planResp.Items))

		execReq := httptest.NewRequest(http.MethodPost, "/api/import/execute", strings.NewReader(""))
		execReq.Header.Set("Content-Type", "application/json")
		execReq.Header.Set("X-CSRF-Token", credentials.CSRFToken)
		for _, cookie := range credentials.Cookies {
			execReq.AddCookie(cookie)
		}

		execRec := httptest.NewRecorder()
		router.ServeHTTP(execRec, execReq)
		Expect(execRec).To(HaveHTTPStatus(http.StatusAccepted), "Expected status 202 when starting import, got %d: %s", execRec.Code, execRec.Body.String())

		var execResp struct {
			ImportedCount   int    `json:"imported_count"`
			SkippedCount    int    `json:"skipped_count"`
			ExecutionStatus string `json:"execution_status"`
			ExecutionResult *struct {
				ImportedCount int `json:"imported_count"`
				SkippedCount  int `json:"skipped_count"`
			} `json:"execution_result"`
		}
		{
			err := json.Unmarshal(execRec.Body.Bytes(), &execResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid import execute response JSON: %v", err)
		}
		Expect(execResp.ExecutionStatus).To(Equal("running"), "expected running execution status, got %q", execResp.ExecutionStatus)

		var completedResp struct {
			ExecutionStatus string `json:"execution_status"`
			ExecutionResult *struct {
				ImportedCount int `json:"imported_count"`
				SkippedCount  int `json:"skipped_count"`
			} `json:"execution_result"`
		}

		Eventually(func(g Gomega) {
			statusReq := httptest.NewRequest(http.MethodGet, "/api/import/plan", nil)
			for _, cookie := range credentials.Cookies {
				statusReq.AddCookie(cookie)
			}

			statusRec := httptest.NewRecorder()
			router.ServeHTTP(statusRec, statusReq)

			g.Expect(statusRec).To(HaveHTTPStatus(http.StatusOK), statusRec.Body.String())
			g.Expect(json.Unmarshal(statusRec.Body.Bytes(), &completedResp)).To(Succeed(), statusRec.Body.String())
			g.Expect(completedResp.ExecutionStatus).To(Equal("completed"))
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())

		Expect(completedResp).To(SatisfyAll(
			HaveField("ExecutionStatus", "completed"),
			HaveField("ExecutionResult", Not(BeNil())),
		), "expected completed execution result, got %#v", completedResp)
		Expect(completedResp.ExecutionResult).To(HaveValue(SatisfyAll(
			HaveField("ImportedCount", 4),
			HaveField("SkippedCount", 1),
		)), "unexpected execution result: %#v", completedResp.ExecutionResult)

		setupPage := getPageByPathViaAPI(router, "guides/setup")
		for _, expected := range []string{
			"[Relative MD](/reference/endpoints.md)",
			"[Absolute MD](/reference/endpoints.md)",
			"[Container](/guides)",
			"[Endpoints](/reference/endpoints.md)",
			"[API Alias](/reference/endpoints.md)",
			"![Relative Image](/assets/" + setupPage.ID + "/logo.png)",
			"[Manual](/assets/" + setupPage.ID + "/manual.pdf)",
		} {
			Expect(setupPage.Content).To(ContainSubstring(expected), "expected setup content to contain %q, got:\n%s", expected, setupPage.Content)

		}

		assets := listAssetsViaAPI(router, setupPage.ID)
		Expect(assets).To(HaveLen(2), "expected 2 uploaded assets, got %#v", assets)

		_ = getPageByPathViaAPI(router, "reference/api-1")

	})
})

var _ = Describe("HTTP router", func() {
	It("applies the configured asset upload limit during import execution", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstanceWithMaxAssetUploadSize(w, 1024)

		fixtureDir := httpTestTempDir()
		{
			err := os.MkdirAll(filepath.Join(fixtureDir, "docs"), 0o755)
			Expect(err).NotTo(HaveOccurred(), "mkdir fixture dir: %v", err)
		}
		{

			err := os.WriteFile(filepath.Join(fixtureDir, "docs", "setup.md"), []byte("# Setup\n\n[Manual](./manual.pdf)\n"), 0o644)
			Expect(err).NotTo(HaveOccurred(), "write markdown fixture: %v", err)
		}
		{

			err := os.WriteFile(filepath.Join(fixtureDir, "docs", "manual.pdf"), bytes.Repeat([]byte("a"), 2048), 0o644)
			Expect(err).NotTo(HaveOccurred(), "write oversized asset fixture: %v", err)
		}

		zipBytes := createZipFromDir(fixtureDir)

		loginBody := `{"identifier": "admin", "password": "admin"}`
		loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		router.ServeHTTP(loginRec, loginReq)
		Expect(loginRec).To(HaveHTTPStatus(http.StatusOK), "Failed to login: %d - %s", loginRec.Code, loginRec.Body.String())

		credentials := readAuthenticatedSessionCredentials(loginRec)
		Expect(credentials).To(haveAuthenticatedSessionCredentials())

		var planBody bytes.Buffer
		planWriter := multipart.NewWriter(&planBody)
		fileWriter, err := planWriter.CreateFormFile("file", "oversized-assets.zip")
		Expect(err).NotTo(HaveOccurred(), "CreateFormFile failed: %v", err)
		{

			_, err := fileWriter.Write(zipBytes)
			Expect(err).NotTo(HaveOccurred(), "Write zip bytes failed: %v", err)
		}
		{

			err := planWriter.Close()
			Expect(err).NotTo(HaveOccurred(), "Close multipart writer failed: %v", err)
		}

		planReq := httptest.NewRequest(http.MethodPost, "/api/import/plan", &planBody)
		planReq.Header.Set("Content-Type", planWriter.FormDataContentType())
		planReq.Header.Set("X-CSRF-Token", credentials.CSRFToken)
		for _, cookie := range credentials.Cookies {
			planReq.AddCookie(cookie)
		}

		planRec := httptest.NewRecorder()
		router.ServeHTTP(planRec, planReq)
		Expect(planRec).To(HaveHTTPStatus(http.StatusOK), "Expected status 200 when creating import plan, got %d: %s", planRec.Code, planRec.Body.String())

		execReq := httptest.NewRequest(http.MethodPost, "/api/import/execute", strings.NewReader(""))
		execReq.Header.Set("Content-Type", "application/json")
		execReq.Header.Set("X-CSRF-Token", credentials.CSRFToken)
		for _, cookie := range credentials.Cookies {
			execReq.AddCookie(cookie)
		}

		execRec := httptest.NewRecorder()
		router.ServeHTTP(execRec, execReq)
		Expect(execRec).To(HaveHTTPStatus(http.StatusAccepted), "Expected status 202 when starting import, got %d: %s", execRec.Code, execRec.Body.String())

		var execResp struct {
			ExecutionStatus string `json:"execution_status"`
		}
		{
			err := json.Unmarshal(execRec.Body.Bytes(), &execResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid import execute response JSON: %v", err)
		}
		Expect(execResp.ExecutionStatus).To(Equal("running"), "expected running execution status, got %q", execResp.ExecutionStatus)

		var completedResp struct {
			ExecutionStatus string `json:"execution_status"`
			ExecutionResult *struct {
				ImportedCount int `json:"imported_count"`
				SkippedCount  int `json:"skipped_count"`
				Items         []struct {
					Action    importer.ExecutionAction `json:"action"`
					ErrorCode importer.ImportErrorCode `json:"error_code"`
				} `json:"items"`
			} `json:"execution_result"`
		}

		Eventually(func(g Gomega) {
			statusReq := httptest.NewRequest(http.MethodGet, "/api/import/plan", nil)
			for _, cookie := range credentials.Cookies {
				statusReq.AddCookie(cookie)
			}

			statusRec := httptest.NewRecorder()
			router.ServeHTTP(statusRec, statusReq)

			g.Expect(statusRec).To(HaveHTTPStatus(http.StatusOK), statusRec.Body.String())
			g.Expect(json.Unmarshal(statusRec.Body.Bytes(), &completedResp)).To(Succeed(), statusRec.Body.String())
			g.Expect(completedResp.ExecutionStatus).To(Equal("completed"))
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())

		Expect(completedResp).To(SatisfyAll(
			HaveField("ExecutionStatus", "completed"),
			HaveField("ExecutionResult", Not(BeNil())),
		), "expected completed execution result, got %#v", completedResp)
		Expect(completedResp.ExecutionResult).To(HaveValue(SatisfyAll(
			HaveField("ImportedCount", BeZero()),
			HaveField("SkippedCount", 1),
			HaveField("Items", HaveExactElements(SatisfyAll(
				HaveField("Action", Equal(importer.ExecutionActionSkipped)),
				HaveField("ErrorCode", Equal(importer.ImportErrorCodeTransformContentFailed)),
			))),
		)), "unexpected execution result: %#v", completedResp.ExecutionResult)

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects slug suggestions when the title is missing", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodGet, "/api/pages/slug-suggestion", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected status 400, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("deletes a page through the authenticated router", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		page := createPageViaAPI(router, "Delete Me", "delete-me", nil, pageNodeKind())
		rec := authenticatedRequest(router, http.MethodDelete, "/api/pages/"+page.ID+"?version="+page.Version, nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d", rec.Code)

		getRec := authenticatedRequest(router, http.MethodGet, "/api/pages/"+page.ID, nil)
		Expect(getRec).To(HaveHTTPStatus(http.StatusNotFound), "Expected deleted page to return 404, got %d", getRec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("returns not found when deleting a missing page", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodDelete, "/api/pages/not-found-id", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), "Expected 404 Not Found, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects deleting a page that has children", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		parent := createPageViaAPI(router, "Parent", "parent", nil, pageNodeKind())
		createPageViaAPI(router, "Child", "child", &parent.ID, pageNodeKind())

		rec := authenticatedRequest(router, http.MethodDelete, "/api/pages/"+parent.ID+"?version="+parent.Version, nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected 400 Bad Request, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("deletes a page tree recursively when requested", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		parent := createPageViaAPI(router, "Parent", "parent", nil, pageNodeKind())
		createPageViaAPI(router, "Child", "child", &parent.ID, pageNodeKind())

		rec := authenticatedRequest(router, http.MethodDelete, "/api/pages/"+parent.ID+"?recursive=true&version="+parent.Version, nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d", rec.Code)

		getRec := authenticatedRequest(router, http.MethodGet, "/api/pages/"+parent.ID, nil)
		Expect(getRec).To(HaveHTTPStatus(http.StatusNotFound), "Expected deleted page to return 404, got %d", getRec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("updates page content through the authenticated router", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		page := createPageViaAPI(router, "Original Title", "original-title", nil, pageNodeKind())

		payload := map[string]string{
			"version": page.Version,
			"title":   "Updated Title",
			"slug":    "updated-title",
			"content": "# Updated Content\nWith **Markdown** support.",
		}
		body, _ := json.Marshal(payload)

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d", rec.Code)

		var resp map[string]interface{}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Invalid JSON response: %v", err)
		}
		Expect(resp).To(HaveKeyWithValue("title", "Updated Title"), "Expected updated title, got %q", resp["title"])
		Expect(resp).To(HaveKeyWithValue("slug", "updated-title"), "Expected updated slug, got %q", resp["slug"])
		Expect(resp).To(HaveKeyWithValue("content", "# Updated Content\nWith **Markdown** support."), "Expected updated content, got %q", resp["content"])

	})
})

var _ = Describe("HTTP router", func() {
	It("writes page tags and string properties", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		page := createPageViaAPI(router, "Original Title", "original-title", nil, pageNodeKind())

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

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

		getRec := authenticatedRequest(router, http.MethodGet, "/api/pages/"+page.ID, nil)
		Expect(getRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on get, got %d", getRec.Code)

		var fetched apiPageDTO
		{
			err := json.Unmarshal(getRec.Body.Bytes(), &fetched)
			Expect(err).NotTo(HaveOccurred(), "Invalid get response JSON: %v", err)
		}
		Expect(fetched).To(SatisfyAll(
			HaveField("Tags", Equal([]string{"react", "typescript"})),
			HaveField("Properties", SatisfyAll(
				HaveKeyWithValue("status", "published"),
				HaveKeyWithValue("author", "alice"),
			)),
		), "updated page metadata = %#v", fetched)

	})
})

var _ = Describe("HTTP router", func() {
	It("removes page tags when an empty tag list is sent", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		page := createPageViaAPI(router, "Original Title", "original-title", nil, pageNodeKind())

		firstPayload := map[string]interface{}{
			"version": page.Version,
			"title":   "Original Title",
			"slug":    "original-title",
			"content": "# Updated Content",
			"tags":    []string{"React", "TypeScript"},
		}
		firstBody, _ := json.Marshal(firstPayload)

		firstRec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(firstBody)))
		Expect(firstRec).To(HaveHTTPStatus(http.StatusOK), "Expected first update to return 200 OK, got %d - %s", firstRec.Code, firstRec.Body.String())

		var updated apiPageDTO
		{
			err := json.Unmarshal(firstRec.Body.Bytes(), &updated)
			Expect(err).NotTo(HaveOccurred(), "Invalid first update response JSON: %v", err)
		}

		secondPayload := map[string]interface{}{
			"version": updated.Version,
			"title":   updated.Title,
			"slug":    updated.Slug,
			"content": updated.Content,
			"tags":    []string{},
		}
		secondBody, _ := json.Marshal(secondPayload)

		secondRec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(secondBody)))
		Expect(secondRec).To(HaveHTTPStatus(http.StatusOK), "Expected second update to return 200 OK, got %d - %s", secondRec.Code, secondRec.Body.String())

		getRec := authenticatedRequest(router, http.MethodGet, "/api/pages/"+page.ID, nil)
		Expect(getRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on get, got %d", getRec.Code)

		var fetched apiPageDTO
		{
			err := json.Unmarshal(getRec.Body.Bytes(), &fetched)
			Expect(err).NotTo(HaveOccurred(), "Invalid get response JSON: %v", err)
		}
		Expect(fetched.Tags).To(HaveLen(0), "expected tags to be removed, got %#v", fetched.Tags)

		tagsRec := authenticatedRequest(router, http.MethodGet, "/api/tags?q=react&limit=20", nil)
		Expect(tagsRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK from tags endpoint, got %d - %s", tagsRec.Code, tagsRec.Body.String())

		var tagsResp []map[string]interface{}
		{
			err := json.Unmarshal(tagsRec.Body.Bytes(), &tagsResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid tags response JSON: %v", err)
		}

		for _, entry := range tagsResp {
			Expect(entry).NotTo(HaveKeyWithValue("tag", "react"), "expected react tag to be removed from index, got %#v", tagsResp)

		}

	})
})

var _ = Describe("HTTP router", func() {
	It("preserves omitted tags and properties while clearing explicit empty values", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		page := createPageViaAPI(router, "Metadata Preserve", "metadata-preserve", nil, pageNodeKind())

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
		firstRec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(firstBody)))
		Expect(firstRec).To(HaveHTTPStatus(http.StatusOK), "Expected first update to return 200 OK, got %d - %s", firstRec.Code, firstRec.Body.String())

		var firstUpdated apiPageDTO
		{
			err := json.Unmarshal(firstRec.Body.Bytes(), &firstUpdated)
			Expect(err).NotTo(HaveOccurred(), "Invalid first update response JSON: %v", err)
		}

		rawAfterFirstBytes, err := os.ReadFile(filepath.Join(w.GetRootDir(), "metadata-preserve.md"))
		Expect(err).NotTo(HaveOccurred(), "ReadFile first metadata update: %v", err)

		rawAfterFirst := string(rawAfterFirstBytes)
		Expect(rawAfterFirst).To(HavePrefix("<!-- leafwiki\n"), "HTTP update should write canonical LeafWiki metadata, got: %q", rawAfterFirst)
		Expect(rawAfterFirst).NotTo(HavePrefix("---\n"), "HTTP update should not write legacy YAML frontmatter, got: %q", rawAfterFirst)

		firstDoc, _, err := markdown.ParsePageDocument(rawAfterFirst)
		Expect(err).NotTo(HaveOccurred(), "ParsePageDocument first metadata update: %v", err)

		Expect(firstDoc.Metadata).To(SatisfyAll(
			HaveField("Tags", Equal([]string{"react"})),
			HaveField("Fields", HaveKeyWithValue("status", "draft")),
		), "first raw metadata = %#v", firstDoc.Metadata)

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
		metadataOnlyRec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(metadataOnlyBody)))
		Expect(metadataOnlyRec).To(HaveHTTPStatus(http.StatusOK), "Expected metadata-only update to return 200 OK, got %d - %s", metadataOnlyRec.Code, metadataOnlyRec.Body.String())

		var metadataOnlyUpdated apiPageDTO
		{
			err := json.Unmarshal(metadataOnlyRec.Body.Bytes(), &metadataOnlyUpdated)
			Expect(err).NotTo(HaveOccurred(), "Invalid metadata-only update response JSON: %v", err)
		}
		Expect(metadataOnlyUpdated).To(SatisfyAll(
			HaveField("Content", "# Metadata Preserve\n\nFirst"),
			HaveField("Tags", Equal([]string{"ready"})),
			HaveField("Properties", HaveKeyWithValue("status", "ready")),
		), "metadata-only update = %#v", metadataOnlyUpdated)

		omittedPayload := map[string]interface{}{
			"version": metadataOnlyUpdated.Version,
			"title":   metadataOnlyUpdated.Title,
			"slug":    metadataOnlyUpdated.Slug,
			"content": "# Metadata Preserve\n\nSecond",
		}
		omittedBody, _ := json.Marshal(omittedPayload)
		omittedRec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(omittedBody)))
		Expect(omittedRec).To(HaveHTTPStatus(http.StatusOK), "Expected omitted metadata update to return 200 OK, got %d - %s", omittedRec.Code, omittedRec.Body.String())

		var omittedUpdated apiPageDTO
		{
			err := json.Unmarshal(omittedRec.Body.Bytes(), &omittedUpdated)
			Expect(err).NotTo(HaveOccurred(), "Invalid omitted update response JSON: %v", err)
		}

		Expect(omittedUpdated).To(SatisfyAll(
			HaveField("Tags", Equal([]string{"ready"})),
			HaveField("Properties", HaveKeyWithValue("status", "ready")),
		), "omitted metadata update = %#v", omittedUpdated)

		clearPayload := map[string]interface{}{
			"version":    omittedUpdated.Version,
			"title":      omittedUpdated.Title,
			"slug":       omittedUpdated.Slug,
			"content":    "# Metadata Preserve\n\nThird",
			"tags":       []string{},
			"properties": map[string]string{},
		}
		clearBody, _ := json.Marshal(clearPayload)
		clearRec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(clearBody)))
		Expect(clearRec).To(HaveHTTPStatus(http.StatusOK), "Expected explicit clear update to return 200 OK, got %d - %s", clearRec.Code, clearRec.Body.String())

		var cleared apiPageDTO
		{
			err := json.Unmarshal(clearRec.Body.Bytes(), &cleared)
			Expect(err).NotTo(HaveOccurred(), "Invalid clear update response JSON: %v", err)
		}
		Expect(cleared).To(SatisfyAll(
			HaveField("Tags", BeEmpty()),
			HaveField("Properties", BeEmpty()),
		), "explicit clear update = %#v", cleared)

		rawAfterClearBytes, err := os.ReadFile(filepath.Join(w.GetRootDir(), "metadata-preserve.md"))
		Expect(err).NotTo(HaveOccurred(), "ReadFile clear metadata update: %v", err)

		rawAfterClear := string(rawAfterClearBytes)
		Expect(rawAfterClear).To(HavePrefix("<!-- leafwiki\n"), "clear update should keep canonical storage, got: %q", rawAfterClear)
		Expect(rawAfterClear).NotTo(HavePrefix("---\n"), "clear update should keep canonical storage, got: %q", rawAfterClear)
		clearDoc, _, err := markdown.ParsePageDocument(rawAfterClear)
		Expect(err).NotTo(HaveOccurred(), "ParsePageDocument clear metadata update: %v", err)

		Expect(clearDoc.Metadata).To(SatisfyAll(
			HaveField("Tags", BeEmpty()),
			HaveField("Fields", BeEmpty()),
		), "clear raw metadata = %#v", clearDoc.Metadata)

	})
})

var _ = Describe("HTTP router", func() {
	It("indexes updated page tags for the tags route", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		page := createPageViaAPI(router, "Original Title", "original-title", nil, pageNodeKind())

		payload := map[string]interface{}{
			"version": page.Version,
			"title":   "Updated Title",
			"slug":    "updated-title",
			"content": "# Updated Content",
			"tags":    []string{"react", "typescript"},
		}
		body, _ := json.Marshal(payload)

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

		tagsRec := authenticatedRequest(router, http.MethodGet, "/api/tags?q=react&limit=20", nil)
		Expect(tagsRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK from tags endpoint, got %d - %s", tagsRec.Code, tagsRec.Body.String())

		var tagsResp []map[string]interface{}
		{
			err := json.Unmarshal(tagsRec.Body.Bytes(), &tagsResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid tags response JSON: %v", err)
		}
		Expect(tagsResp).To(ContainElement(HaveKeyWithValue("tag", "react")), "expected indexed tags, got %#v", tagsResp)

	})
})

var _ = Describe("HTTP router", func() {
	It("counts tag suggestions within the selected tags", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		pageA := createPageViaAPI(router, "Page A", "page-a", nil, pageNodeKind())
		pageB := createPageViaAPI(router, "Page B", "page-b", nil, pageNodeKind())
		pageC := createPageViaAPI(router, "Page C", "page-c", nil, pageNodeKind())

		updatePageTags := func(page *apiPageDTO, title, slug string, tags []string) {
			payload := map[string]interface{}{
				"version": page.Version,
				"title":   title,
				"slug":    slug,
				"content": "# Content",
				"tags":    tags,
			}
			body, _ := json.Marshal(payload)
			rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
			Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

		}

		updatePageTags(pageA, "Page A", "page-a", []string{"react", "typescript"})
		updatePageTags(pageB, "Page B", "page-b", []string{"react", "testing"})
		updatePageTags(pageC, "Page C", "page-c", []string{"react", "typescript"})

		tagsRec := authenticatedRequest(router, http.MethodGet, "/api/tags?q=t&limit=20&selected=react", nil)
		Expect(tagsRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK from tags endpoint, got %d - %s", tagsRec.Code, tagsRec.Body.String())

		var tagsResp []map[string]interface{}
		{
			err := json.Unmarshal(tagsRec.Body.Bytes(), &tagsResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid tags response JSON: %v", err)
		}
		Expect(tagsResp).To(HaveExactElements(
			SatisfyAll(HaveKeyWithValue("tag", "typescript"), HaveKeyWithValue("count", float64(2))),
			SatisfyAll(HaveKeyWithValue("tag", "testing"), HaveKeyWithValue("count", float64(1))),
		), "expected selected-tag suggestions, got %#v", tagsResp)

	})
})

var _ = Describe("HTTP router", func() {
	It("accepts repeated selected tag parameters", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		page := createPageViaAPI(router, "Page A", "page-a", nil, pageNodeKind())

		payload := map[string]interface{}{
			"version": page.Version,
			"title":   "Page A",
			"slug":    "page-a",
			"content": "# Content",
			"tags":    []string{"react", "typescript", "testing"},
		}
		body, _ := json.Marshal(payload)
		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

		tagsRec := authenticatedRequest(router, http.MethodGet, "/api/tags?q=t&limit=20&selected=react&selected=typescript", nil)
		Expect(tagsRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK from tags endpoint, got %d - %s", tagsRec.Code, tagsRec.Body.String())

		var tagsResp []map[string]interface{}
		{
			err := json.Unmarshal(tagsRec.Body.Bytes(), &tagsResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid tags response JSON: %v", err)
		}
		Expect(tagsResp).To(HaveExactElements(
			SatisfyAll(HaveKeyWithValue("tag", "testing"), HaveKeyWithValue("count", float64(1))),
		), "expected repeated-selected-tag suggestion, got %#v", tagsResp)

	})
})

var _ = Describe("HTTP router", func() {
	It("filters search results by selected tags", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		reactPage := createPageViaAPI(router, "React Search Match", "react-search-match", nil, pageNodeKind())
		plainPage := createPageViaAPI(router, "Plain Search Match", "plain-search-match", nil, pageNodeKind())

		updatePage := func(page *apiPageDTO, title, slug, content string, tags []string) {
			payload := map[string]interface{}{
				"version": page.Version,
				"title":   title,
				"slug":    slug,
				"content": content,
				"tags":    tags,
			}
			body, _ := json.Marshal(payload)
			rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
			Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

		}

		updatePage(reactPage, "React Search Match", "react-search-match", "Body with shared search token.", []string{"react"})
		updatePage(plainPage, "Plain Search Match", "plain-search-match", "Body with shared search token.", []string{"docs"})

		rec := authenticatedRequest(router, http.MethodGet, "/api/search?q=shared%20search&tags=react", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

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
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Invalid search response JSON: %v", err)
		}
		Expect(resp).To(SatisfyAll(
			HaveField("Count", 1),
			HaveField("Items", ConsistOf(HaveField("PageID", reactPage.ID))),
			HaveField("TagFacets", ConsistOf(SatisfyAll(
				HaveField("Tag", "react"),
				HaveField("Count", 1),
			))),
		), "filtered search response = %#v", resp)

	})
})

var _ = Describe("HTTP router", func() {
	It("returns tag matches without a text query", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		reactPage := createPageViaAPI(router, "React Tag Match", "react-tag-match", nil, pageNodeKind())
		plainPage := createPageViaAPI(router, "Plain Tag Match", "plain-tag-match", nil, pageNodeKind())

		updatePage := func(page *apiPageDTO, title, slug, content string, tags []string) {
			payload := map[string]interface{}{
				"version": page.Version,
				"title":   title,
				"slug":    slug,
				"content": content,
				"tags":    tags,
			}
			body, _ := json.Marshal(payload)
			rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
			Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

		}

		updatePage(reactPage, "React Tag Match", "react-tag-match", "Body without search token.", []string{"react"})
		updatePage(plainPage, "Plain Tag Match", "plain-tag-match", "Body without search token.", []string{"docs"})

		rec := authenticatedRequest(router, http.MethodGet, "/api/search?tags=react", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

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
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Invalid search response JSON: %v", err)
		}
		Expect(resp).To(SatisfyAll(
			HaveField("Count", 1),
			HaveField("Items", ConsistOf(HaveField("PageID", reactPage.ID))),
			HaveField("TagFacets", ConsistOf(SatisfyAll(
				HaveField("Tag", "react"),
				HaveField("Count", 1),
			))),
		), "tag-only search response = %#v", resp)

	})
})

var _ = Describe("HTTP router", func() {
	It("normalizes pagination bounds for tag-only searches", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		page := createPageViaAPI(router, "React Tag Match", "react-tag-match-bounds", nil, pageNodeKind())

		payload := map[string]interface{}{
			"version": page.Version,
			"title":   "React Tag Match",
			"slug":    "react-tag-match-bounds",
			"content": "Body without search token.",
			"tags":    []string{"react"},
		}
		body, _ := json.Marshal(payload)
		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

		boundsRec := authenticatedRequest(router, http.MethodGet, "/api/search?tags=react&offset=-1&limit=0", nil)
		Expect(boundsRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", boundsRec.Code, boundsRec.Body.String())

		var resp struct {
			Count  int `json:"count"`
			Limit  int `json:"limit"`
			Offset int `json:"offset"`
			Items  []struct {
				PageID string `json:"page_id"`
			} `json:"items"`
		}
		{
			err := json.Unmarshal(boundsRec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Invalid search response JSON: %v", err)
		}
		Expect(resp).To(SatisfyAll(
			HaveField("Count", 1),
			HaveField("Offset", BeZero()),
			HaveField("Limit", 20),
			HaveField("Items", ConsistOf(HaveField("PageID", page.ID))),
		), "normalized tag search response = %#v", resp)

	})
})

var _ = Describe("HTTP router", func() {
	It("shrinks tag facets as additional filters are applied", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		type searchResponse struct {
			Count     int `json:"count"`
			TagFacets []struct {
				Tag   string `json:"tag"`
				Count int    `json:"count"`
			} `json:"tag_facets"`
		}

		updatePage := func(page *apiPageDTO, title, slug, content string, tags []string) {
			payload := map[string]interface{}{
				"version": page.Version,
				"title":   title,
				"slug":    slug,
				"content": content,
				"tags":    tags,
			}
			body, _ := json.Marshal(payload)
			rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
			Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

		}

		pageOne := createPageViaAPI(router, "Facet Alpha", "facet-alpha", nil, pageNodeKind())
		pageTwo := createPageViaAPI(router, "Facet Beta", "facet-beta", nil, pageNodeKind())
		pageThree := createPageViaAPI(router, "Facet Gamma", "facet-gamma", nil, pageNodeKind())

		updatePage(pageOne, "Facet Alpha", "facet-alpha", "Body with facet token.", []string{"alpha", "shared"})
		updatePage(pageTwo, "Facet Beta", "facet-beta", "Body with facet token.", []string{"beta", "shared"})
		updatePage(pageThree, "Facet Gamma", "facet-gamma", "Body with facet token.", []string{"alpha", "shared", "narrow"})

		baseRec := authenticatedRequest(router, http.MethodGet, "/api/search?q=facet%20token", nil)
		Expect(baseRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", baseRec.Code, baseRec.Body.String())

		var baseResp searchResponse
		{
			err := json.Unmarshal(baseRec.Body.Bytes(), &baseResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid search response JSON: %v", err)
		}

		narrowRec := authenticatedRequest(router, http.MethodGet, "/api/search?q=facet%20token&tags=alpha", nil)
		Expect(narrowRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", narrowRec.Code, narrowRec.Body.String())

		var narrowResp searchResponse
		{
			err := json.Unmarshal(narrowRec.Body.Bytes(), &narrowResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid filtered search response JSON: %v", err)
		}
		Expect(baseResp.Count).To(Equal(3), "expected 3 base results, got %d", baseResp.Count)
		Expect(narrowResp.Count).To(Equal(2), "expected 2 narrowed results, got %d", narrowResp.Count)

		baseFacets := map[string]int{}
		for _, facet := range baseResp.TagFacets {
			baseFacets[facet.Tag] = facet.Count
		}
		narrowFacets := map[string]int{}
		for _, facet := range narrowResp.TagFacets {
			narrowFacets[facet.Tag] = facet.Count
		}
		Expect(baseFacets).To(HaveLen(4), "expected 4 base facets, got %#v", baseResp.TagFacets)
		Expect(narrowFacets).To(HaveLen(3), "expected 3 narrowed facets, got %#v", narrowResp.TagFacets)
		Expect(baseFacets).To(HaveKeyWithValue("beta", 1), "expected base facets to include beta=1, got %#v", baseResp.TagFacets)
		Expect(narrowFacets).NotTo(HaveKey("beta"), "expected beta to disappear after narrowing, got %#v", narrowResp.TagFacets)

		Expect(narrowFacets).To(HaveKeyWithValue("alpha", 2), "unexpected narrowed facets: %#v", narrowResp.TagFacets)
		Expect(narrowFacets).To(HaveKeyWithValue("shared", 2), "unexpected narrowed facets: %#v", narrowResp.TagFacets)
		Expect(narrowFacets).To(HaveKeyWithValue("narrow", 1), "unexpected narrowed facets: %#v", narrowResp.TagFacets)

	})
})

var _ = Describe("HTTP router", func() {
	It("returns excerpts for pages matched by tags", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		page := createPageViaAPI(router, "Excerpt Page", "excerpt-page", nil, pageNodeKind())

		payload := map[string]interface{}{
			"version": page.Version,
			"title":   "Excerpt Page",
			"slug":    "excerpt-page",
			"content": "# Heading\n\nThis is a tagged page with useful excerpt text and a [link](/docs) inside the content.",
			"tags":    []string{"react"},
		}
		body, _ := json.Marshal(payload)

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

		pagesRec := authenticatedRequest(router, http.MethodGet, "/api/tags/pages?tags=react", nil)
		Expect(pagesRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK from tags pages endpoint, got %d - %s", pagesRec.Code, pagesRec.Body.String())

		var pagesResp []apiTaggedPageSummaryDTO
		{
			err := json.Unmarshal(pagesRec.Body.Bytes(), &pagesResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid pages response JSON: %v", err)
		}
		Expect(pagesResp).To(HaveExactElements(SatisfyAll(
			HaveField("Kind", tree.NodeKindPage),
			HaveField("Excerpt", SatisfyAll(
				Not(BeEmpty()),
				Not(ContainSubstring("#")),
				Not(ContainSubstring("[link]")),
				ContainSubstring("This is a tagged page with useful excerpt text"),
			)),
		)), "expected tagged page excerpt, got %#v", pagesResp)

	})
})

var _ = Describe("HTTP router", func() {
	It("accepts repeated tag parameters when listing tagged pages", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		pageA := createPageViaAPI(router, "Page A", "page-a", nil, pageNodeKind())
		pageB := createPageViaAPI(router, "Page B", "page-b", nil, pageNodeKind())

		updatePageTags := func(page *apiPageDTO, title, slug string, tags []string) {
			payload := map[string]interface{}{
				"version": page.Version,
				"title":   title,
				"slug":    slug,
				"content": "# Content",
				"tags":    tags,
			}
			body, _ := json.Marshal(payload)
			rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
			Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

		}

		updatePageTags(pageA, "Page A", "page-a", []string{"react", "typescript"})
		updatePageTags(pageB, "Page B", "page-b", []string{"react"})

		pagesRec := authenticatedRequest(router, http.MethodGet, "/api/tags/pages?tags=react&tags=typescript", nil)
		Expect(pagesRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK from tags pages endpoint, got %d - %s", pagesRec.Code, pagesRec.Body.String())

		var pagesResp []map[string]interface{}
		{
			err := json.Unmarshal(pagesRec.Body.Bytes(), &pagesResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid pages response JSON: %v", err)
		}
		Expect(pagesResp).To(HaveExactElements(
			HaveKeyWithValue("title", "Page A"),
		), "expected Page A, got %#v", pagesResp)

	})
})

var _ = Describe("HTTP router", func() {
	It("returns not found when updating a missing page", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `{"version":"stale-version","title":"Updated","slug":"updated","content":"New content"}`
		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/not-found-id", strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), "Expected 404 for unknown page, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("keeps a page slug when the update does not change it", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Create a page
		created := createPageViaAPI(router, "Immutable Slug", "immutable-slug", nil, pageNodeKind())

		// Update title, but reuse slug
		payload := map[string]string{
			"version": created.Version,
			"title":   "Updated Title",
			"slug":    created.Slug,
			"content": "Updated content",
		}
		body, _ := json.Marshal(payload)

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+created.ID, strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected status 200, got %d", rec.Code)

		var updated map[string]interface{}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &updated)
			Expect(err).NotTo(HaveOccurred(), "Invalid response JSON: %v", err)
		}
		Expect(updated).To(HaveKeyWithValue("slug", created.Slug), "Expected slug to remain unchanged, got: %v", updated["slug"])

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects page updates that would collide with an existing route", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		page := createPageViaAPI(router, "Original Title", "original-title", nil, pageNodeKind())
		createPageViaAPI(router, "Conflict Title", "conflict-title", nil, pageNodeKind())

		payload := map[string]string{
			"version": page.Version,
			"title":   "Conflict Title",
			"slug":    "conflict-title",
			"content": "Updated content",
		}
		body, _ := json.Marshal(payload)

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected 400 Bad Request, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects page updates with invalid JSON", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `this is not valid json`
		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/invalid-id", strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected 400 for invalid JSON, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects page updates when the title is missing", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `{"version":"required","slug":"updated","content":"New content"}`
		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/missing-title", strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected 400 for missing title, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects page updates when the slug is missing", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `{"version":"required","title":"Updated","content":"New content"}`
		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/missing-slug", strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected 400 for missing slug, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects page updates with invalid properties", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		page := createPageViaAPI(router, "Original Title", "original-title", nil, pageNodeKind())

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

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected 400 Bad Request, got %d - %s", rec.Code, rec.Body.String())

		var resp struct {
			Error  string `json:"error"`
			Fields []struct {
				Field     string `json:"field"`
				Code      string `json:"code"`
				MessageID string `json:"messageId"`
				Message   string `json:"message"`
			} `json:"fields"`
		}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Invalid validation response JSON: %v", err)
		}
		Expect(resp.Fields).To(testmatchers.ContainFieldError(
			testmatchers.ValidationFieldName("properties.leafwiki_hidden"),
			wikipages.FieldCodePagePropertyKeyReserved,
			wikipages.MessageIDPagePropertyKeyReservedPrefix,
		), "expected reserved prefix validation error, got %#v", resp.Fields)

	})
})

var _ = Describe("HTTP router", func() {
	It("returns a page by id", func() {

		dataDir := filepath.Join(httpTestTempDir(), "data")
		rootDir := filepath.Join(httpTestTempDir(), "content")
		w := createWikiTestInstanceWithWorkspace(wiki.Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir})
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Create a page
		page := createPageViaAPI(router, "Welcome", "welcome", nil, pageNodeKind())
		{
			_, err := os.Stat(filepath.Join(rootDir, "welcome.md"))
			Expect(err).NotTo(HaveOccurred(), "expected API-created page in root dir: %v", err)
		}
		{

			Expect(filepath.Join(dataDir, "root", "welcome.md")).NotTo(BeAnExistingFile(), "expected no API-created page in data dir root")
		}

		writePageMarkdownForTest(w, page, `---
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
		rec := authenticatedRequest(router, http.MethodGet, "/api/pages/"+page.ID, nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected status 200, got %d", rec.Code)

		var resp map[string]interface{}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Failed to parse JSON: %v", err)
		}
		Expect(resp).To(SatisfyAll(
			HaveKey("id"),
			HaveKeyWithValue("title", page.Title),
			HaveKeyWithValue("slug", page.Slug),
			HaveKeyWithValue("tags", HaveExactElements("alpha", "beta")),
			HaveKeyWithValue("properties", SatisfyAll(
				Not(HaveKey("priority")),
				Not(HaveKey("published")),
				Not(HaveKey("owners")),
			)),
		), "page response = %#v", resp)

	})
})

var _ = Describe("HTTP router", func() {
	It("returns not found for a missing page id", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodGet, "/api/pages/not-found-id", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), "Expected status 404, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects page reads when the id is missing", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodGet, "/api/pages/", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), "Expected status 404, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects page-by-path reads when the path is missing", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected status 400, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("returns the root section for an explicit empty path", func() {

		dataDir := httpTestTempDir()
		rootDir := filepath.Join(httpTestTempDir(), "root")
		{
			err := os.MkdirAll(rootDir, 0o755)
			Expect(err).NotTo(HaveOccurred(), "mkdir root fixture: %v", err)
		}
		{

			err := os.WriteFile(filepath.Join(rootDir, "README.md"), []byte("---\nleafwiki_id: root\nleafwiki_title: Root README\n---\n# Root README\n"), 0o644)
			Expect(err).NotTo(HaveOccurred(), "write root README: %v", err)
		}
		{

			err := os.WriteFile(filepath.Join(rootDir, "child.md"), []byte("---\nleafwiki_id: child\nleafwiki_title: Child\n---\n# Child\n"), 0o644)
			Expect(err).NotTo(HaveOccurred(), "write child: %v", err)
		}

		w := createWikiTestInstanceWithWorkspace(wiki.Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir})
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path=&kind=section", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected root path status 200, got %d - %s", rec.Code, rec.Body.String())

		var resp map[string]interface{}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "parse root response: %v", err)
		}

		Expect(resp).To(SatisfyAll(
			HaveKeyWithValue("id", "root"),
			HaveKeyWithValue("kind", "section"),
			HaveKeyWithValue("title", "Root README"),
			HaveKeyWithValue("content", ContainSubstring("Root README")),
		), "root response = %#v, want root README section", resp)

	})
})

var _ = Describe("HTTP router", func() {
	It("returns not found for a missing page path", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path=does-not-exist", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), "Expected status 404, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("omits children when page-by-path resolves to a page", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Create a standalone page (no children – adding children auto-converts it to a section)
		createPageViaAPI(router, "My Page", "my-page", nil, pageNodeKind())

		rec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path=my-page", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected status 200, got %d - %s", rec.Code, rec.Body.String())

		var resp map[string]interface{}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Failed to parse JSON: %v", err)
		}
		// Page kind (depth=0): the node must be returned with children absent or null.
		Expect(resp).To(SatisfyAll(
			HaveKeyWithValue("kind", "page"),
			HaveKeyWithValue("children", BeNil()),
		), "page-kind response = %#v", resp)

	})
})

var _ = Describe("HTTP router", func() {
	It("returns only direct children when page-by-path resolves to a section", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		sectionKind := tree.NodeKindSection

		// Create a section with a child page that itself has a grandchild
		section := createPageViaAPI(router, "My Section", "my-section", nil, &sectionKind)
		child := createPageViaAPI(router, "Child Page", "child-page", &section.ID, pageNodeKind())
		createPageViaAPI(router, "Grandchild Page", "grandchild-page", &child.ID, pageNodeKind())

		rec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path=my-section", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected status 200, got %d - %s", rec.Code, rec.Body.String())

		var resp map[string]interface{}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Failed to parse JSON: %v", err)
		}

		Expect(resp).To(HaveKeyWithValue("children", ContainElement(HaveKeyWithValue("children", BeNil()))), "Expected direct children without grandchildren for section kind (depth=1), got: %v", resp["children"])

	})
})

var _ = Describe("HTTP router", func() {
	It("uses the requested kind to distinguish same-basename pages and sections", func() {

		dataDir := httpTestTempDir()
		rootDir := filepath.Join(httpTestTempDir(), "root")
		{
			err := os.MkdirAll(filepath.Join(rootDir, "docs", "sync"), 0o755)
			Expect(err).NotTo(HaveOccurred(), "mkdir fixture: %v", err)
		}

		write := func(relPath, content string) {
			GinkgoHelper()
			{

				err := os.WriteFile(filepath.Join(rootDir, filepath.FromSlash(relPath)), []byte(content), 0o644)
				Expect(err).NotTo(HaveOccurred(), "write %s: %v", relPath, err)
			}

		}
		write("docs/index.md", "---\nleafwiki_id: docs-section\nleafwiki_title: Docs\n---\n# Docs\n")
		write("docs/sync.md", "---\nleafwiki_id: sync-page\nleafwiki_title: Sync Page\n---\n# Sync Page\n")
		write("docs/sync/index.md", "---\nleafwiki_id: sync-section\nleafwiki_title: Sync Section\n---\n# Sync Section\n")

		w := createWikiTestInstanceWithWorkspace(wiki.Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir})
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		pageRec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path=docs/sync&kind=page", nil)
		Expect(pageRec).To(HaveHTTPStatus(http.StatusOK), "Expected page status 200, got %d - %s", pageRec.Code, pageRec.Body.String())

		var pageResp map[string]interface{}
		{
			err := json.Unmarshal(pageRec.Body.Bytes(), &pageResp)
			Expect(err).NotTo(HaveOccurred(), "parse page response: %v", err)
		}

		Expect(pageResp).To(SatisfyAll(HaveKeyWithValue("id", "sync-page"), HaveKeyWithValue("kind", "page")), "page response = %#v, want sync-page page", pageResp)

		pageMarkdownPathRec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path=docs/sync.md", nil)
		Expect(pageMarkdownPathRec).To(HaveHTTPStatus(http.StatusOK), "Expected page markdown-path status 200, got %d - %s", pageMarkdownPathRec.Code, pageMarkdownPathRec.Body.String())

		var pageMarkdownPathResp map[string]interface{}
		{
			err := json.Unmarshal(pageMarkdownPathRec.Body.Bytes(), &pageMarkdownPathResp)
			Expect(err).NotTo(HaveOccurred(), "parse page markdown-path response: %v", err)
		}

		Expect(pageMarkdownPathResp).To(SatisfyAll(HaveKeyWithValue("id", "sync-page"), HaveKeyWithValue("kind", "page")), "page markdown-path response = %#v, want sync-page page", pageMarkdownPathResp)

		sectionRec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path=docs/sync&kind=section", nil)
		Expect(sectionRec).To(HaveHTTPStatus(http.StatusOK), "Expected section status 200, got %d - %s", sectionRec.Code, sectionRec.Body.String())

		var sectionResp map[string]interface{}
		{
			err := json.Unmarshal(sectionRec.Body.Bytes(), &sectionResp)
			Expect(err).NotTo(HaveOccurred(), "parse section response: %v", err)
		}

		Expect(sectionResp).To(SatisfyAll(HaveKeyWithValue("id", "sync-section"), HaveKeyWithValue("kind", "section")), "section response = %#v, want sync-section section", sectionResp)

		sectionCanonicalRec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path=docs/sync", nil)
		Expect(sectionCanonicalRec).To(HaveHTTPStatus(http.StatusOK), "Expected canonical section status 200, got %d - %s", sectionCanonicalRec.Code, sectionCanonicalRec.Body.String())

		var sectionCanonicalResp map[string]interface{}
		{
			err := json.Unmarshal(sectionCanonicalRec.Body.Bytes(), &sectionCanonicalResp)
			Expect(err).NotTo(HaveOccurred(), "parse canonical section response: %v", err)
		}

		Expect(sectionCanonicalResp).To(SatisfyAll(HaveKeyWithValue("id", "sync-section"), HaveKeyWithValue("kind", "section")), "canonical section response = %#v, want sync-section section", sectionCanonicalResp)

	})
})

// - Explicit README.md page link stays a page when index.md exists
var _ = Describe("HTTP router", func() {
	It("serves README.md as a section fallback only when no explicit page exists", func() {

		dataDir := httpTestTempDir()
		rootDir := filepath.Join(httpTestTempDir(), "root")
		{
			err := os.MkdirAll(filepath.Join(rootDir, "docs", "guides"), 0o755)
			Expect(err).NotTo(HaveOccurred(), "mkdir guides fixture: %v", err)
		}
		{

			err := os.MkdirAll(filepath.Join(rootDir, "docs", "indexed"), 0o755)
			Expect(err).NotTo(HaveOccurred(), "mkdir indexed fixture: %v", err)
		}
		{

			err := os.MkdirAll(filepath.Join(rootDir, "docs", "no-readme"), 0o755)
			Expect(err).NotTo(HaveOccurred(), "mkdir no-readme fixture: %v", err)
		}

		write := func(relPath, content string) {
			GinkgoHelper()
			{

				err := os.WriteFile(filepath.Join(rootDir, filepath.FromSlash(relPath)), []byte(content), 0o644)
				Expect(err).NotTo(HaveOccurred(), "write %s: %v", relPath, err)
			}

		}
		write("docs/index.md", "---\nleafwiki_id: docs-section\nleafwiki_title: Docs\n---\n# Docs\n")
		write("README.md", "---\nleafwiki_id: root-section\nleafwiki_title: Root\n---\n# Root\n")
		write("docs/guides/README.md", "---\nleafwiki_id: guides-section\nleafwiki_title: Guides\n---\n# Guides\n")
		write("docs/indexed/index.md", "---\nleafwiki_id: indexed-section\nleafwiki_title: Indexed\n---\n# Indexed\n")
		write("docs/indexed/README.md", "---\nleafwiki_id: indexed-readme-page\nleafwiki_title: Indexed README\n---\n# Indexed README\n")
		write("docs/no-readme/index.md", "---\nleafwiki_id: no-readme-section\nleafwiki_title: No README\n---\n# No README\n")

		w := createWikiTestInstanceWithWorkspace(wiki.Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir})
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		fallbackRec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path=docs/guides/README.md", nil)
		Expect(fallbackRec).To(HaveHTTPStatus(http.StatusOK), "Expected README fallback status 200, got %d - %s", fallbackRec.Code, fallbackRec.Body.String())

		var fallbackResp map[string]interface{}
		{
			err := json.Unmarshal(fallbackRec.Body.Bytes(), &fallbackResp)
			Expect(err).NotTo(HaveOccurred(), "parse README fallback response: %v", err)
		}

		Expect(fallbackResp).To(SatisfyAll(HaveKeyWithValue("id", "guides-section"), HaveKeyWithValue("kind", "section")), "README fallback response = %#v, want guides section", fallbackResp)

		explicitSectionRec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path=docs/guides/README.md&kind=section", nil)
		Expect(explicitSectionRec).To(HaveHTTPStatus(http.StatusOK), "Expected explicit README section fallback status 200, got %d - %s", explicitSectionRec.Code, explicitSectionRec.Body.String())

		var explicitSectionResp map[string]interface{}
		{
			err := json.Unmarshal(explicitSectionRec.Body.Bytes(), &explicitSectionResp)
			Expect(err).NotTo(HaveOccurred(), "parse explicit README section fallback response: %v", err)
		}

		Expect(explicitSectionResp).To(SatisfyAll(HaveKeyWithValue("id", "guides-section"), HaveKeyWithValue("kind", "section")), "explicit README section fallback response = %#v, want guides section", explicitSectionResp)

		readmePageRec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path=docs/indexed/README.md", nil)
		Expect(readmePageRec).To(HaveHTTPStatus(http.StatusOK), "Expected README page status 200, got %d - %s", readmePageRec.Code, readmePageRec.Body.String())

		var readmePageResp map[string]interface{}
		{
			err := json.Unmarshal(readmePageRec.Body.Bytes(), &readmePageResp)
			Expect(err).NotTo(HaveOccurred(), "parse README page response: %v", err)
		}

		Expect(readmePageResp).To(SatisfyAll(HaveKeyWithValue("id", "indexed-readme-page"), HaveKeyWithValue("kind", "page")), "README page response = %#v, want indexed README page", readmePageResp)

		inactiveExplicitSectionRec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path=docs/indexed/README.md&kind=section", nil)
		Expect(inactiveExplicitSectionRec).To(HaveHTTPStatus(http.StatusNotFound), "Expected inactive explicit README section status 404, got %d - %s", inactiveExplicitSectionRec.Code, inactiveExplicitSectionRec.Body.String())

		missingReadmeRec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path=docs/no-readme/README.md", nil)
		Expect(missingReadmeRec).To(HaveHTTPStatus(http.StatusNotFound), "Expected missing README status 404, got %d - %s", missingReadmeRec.Code, missingReadmeRec.Body.String())

		lowercaseReadmeRec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path=docs/guides/readme.md", nil)
		Expect(lowercaseReadmeRec).To(HaveHTTPStatus(http.StatusNotFound), "Expected lowercase readme.md status 404, got %d - %s", lowercaseReadmeRec.Code, lowercaseReadmeRec.Body.String())

		traversalReadmeRec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path=../README.md&kind=section", nil)
		Expect(traversalReadmeRec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected traversal README status 400, got %d - %s", traversalReadmeRec.Code, traversalReadmeRec.Body.String())

	})
})

var _ = Describe("HTTP router", func() {
	It("prefers case-insensitive index files when building tree content paths", func() {

		dataDir := httpTestTempDir()
		rootDir := filepath.Join(httpTestTempDir(), "root")
		for _, dir := range []string{"docs", "guides"} {
			{
				err := os.MkdirAll(filepath.Join(rootDir, dir), 0o755)
				Expect(err).NotTo(HaveOccurred(), "create %s dir: %v", dir, err)
			}

		}
		write := func(relPath, content string) {
			GinkgoHelper()
			{

				err := os.WriteFile(filepath.Join(rootDir, filepath.FromSlash(relPath)), []byte(content), 0o644)
				Expect(err).NotTo(HaveOccurred(), "write %s: %v", relPath, err)
			}

		}
		write("docs/INDEX.MD", "---\nleafwiki_id: docs-section\nleafwiki_title: Docs\n---\n# Docs Index\n")
		write("docs/README.md", "---\nleafwiki_id: docs-readme\nleafwiki_title: Docs README\n---\n# Docs README\n")
		write("guides/README.md", "---\nleafwiki_id: guides-section\nleafwiki_title: Guides\n---\n# Guides README\n")

		w := createWikiTestInstanceWithWorkspace(wiki.Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir})
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		root := getTreeViaAPI(router)
		Expect(root.Children).To(ContainElement(SatisfyAll(
			HaveField("Path", "docs"),
			HaveField("ContentPath", "docs/INDEX.MD"),
			HaveField("ReadmeFallback", BeFalse()),
		)), "docs section missing from tree: %#v", root.Children)
		Expect(root.Children).To(ContainElement(SatisfyAll(
			HaveField("Path", "guides"),
			HaveField("ReadmeFallback", BeTrue()),
		)), "guides section missing from tree: %#v", root.Children)

	})
})

var _ = Describe("HTTP router", func() {
	It("creates a section twin when a page route already exists", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		sectionKind := tree.NodeKindSection
		page := createPageViaAPI(router, "Sync Page", "sync", nil, pageNodeKind())
		body := `{"path":"sync","title":"Sync Section","kind":"section"}`
		rec := authenticatedRequest(router, http.MethodPost, "/api/pages/ensure", strings.NewReader(body))
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on ensure, got %d - %s", rec.Code, rec.Body.String())

		var ensured apiPageDTO
		{
			err := json.Unmarshal(rec.Body.Bytes(), &ensured)
			Expect(err).NotTo(HaveOccurred(), "Unmarshal(ensure response) failed: %v", err)
		}
		Expect(ensured).To(SatisfyAll(
			HaveField("ID", Not(Equal(page.ID))),
			HaveField("Kind", sectionKind),
		), "ensure response = %#v", ensured)

		pageRec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path=sync&kind=page", nil)
		Expect(pageRec).To(HaveHTTPStatus(http.StatusOK), "Expected page twin lookup status 200, got %d - %s", pageRec.Code, pageRec.Body.String())

		var pageTwin apiPageDTO
		{
			err := json.Unmarshal(pageRec.Body.Bytes(), &pageTwin)
			Expect(err).NotTo(HaveOccurred(), "Unmarshal(page twin response) failed: %v", err)
		}

		Expect(pageTwin).To(SatisfyAll(HaveField("ID", page.ID), HaveField("Kind", tree.NodeKindPage)), "page twin response = %#v, want original page %q", pageTwin, page.ID)

		sectionRec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path=sync&kind=section", nil)
		Expect(sectionRec).To(HaveHTTPStatus(http.StatusOK), "Expected section twin lookup status 200, got %d - %s", sectionRec.Code, sectionRec.Body.String())

		var sectionTwin apiPageDTO
		{
			err := json.Unmarshal(sectionRec.Body.Bytes(), &sectionTwin)
			Expect(err).NotTo(HaveOccurred(), "Unmarshal(section twin response) failed: %v", err)
		}

		Expect(sectionTwin).To(SatisfyAll(HaveField("ID", ensured.ID), HaveField("Kind", tree.NodeKindSection)), "section twin response = %#v, want ensured section %q", sectionTwin, ensured.ID)

	})
})

var _ = Describe("HTTP router", func() {
	It("returns the current path for a page permalink", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		docs := createPageViaAPI(router, "Docs", "docs", nil, pageNodeKind())
		guide := createPageViaAPI(router, "Guide", "guide", &docs.ID, pageNodeKind())
		archive := createPageViaAPI(router, "Archive", "archive", nil, pageNodeKind())

		movePayload := `{"version":"` + guide.Version + `","parentId":"` + archive.ID + `"}`
		moveRec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+guide.ID+"/move", strings.NewReader(movePayload))
		Expect(moveRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on move, got %d - %s", moveRec.Code, moveRec.Body.String())

		guide = getPageByPathViaAPI(router, "archive/guide")

		updatePayload := `{"version":"` + guide.Version + `","title":"User Guide","slug":"user-guide","content":""}`
		updateRec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+guide.ID, strings.NewReader(updatePayload))
		Expect(updateRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on update, got %d - %s", updateRec.Code, updateRec.Body.String())

		target := getPermalinkTargetViaAPI(router, guide.ID)
		Expect(target).To(SatisfyAll(
			HaveField("ID", guide.ID),
			HaveField("Slug", "user-guide"),
			HaveField("Path", "archive/user-guide"),
			HaveField("Kind", tree.NodeKindPage),
		), "permalink target = %#v", target)

	})
})

var _ = Describe("HTTP router", func() {
	It("allows unauthenticated permalink reads in public mode", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
			PublicAccess:            true,
			InjectCodeInHeader:      "",
			CustomStylesheet:        "",
			AllowInsecure:           true,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			HideLinkMetadataSection: false,
		})

		page := createPageViaAPI(router, "Public Page", "public-page", nil, pageNodeKind())

		req := httptest.NewRequest(http.MethodGet, "/api/pages/permalink/"+page.ID, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

		var target apiPermalinkTargetDTO
		{
			err := json.Unmarshal(rec.Body.Bytes(), &target)
			Expect(err).NotTo(HaveOccurred(), "Unmarshal(permalink response) failed: %v", err)
		}
		Expect(target).To(SatisfyAll(
			HaveField("Path", "public-page"),
			HaveField("Kind", tree.NodeKindPage),
		), "public permalink target = %#v", target)

	})
})

var _ = Describe("HTTP router", func() {
	It("moves a page to a new parent", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Create two pages a and b
		a := createPageViaAPI(router, "Section A", "section-a", nil, pageNodeKind())
		b := createPageViaAPI(router, "Section B", "section-b", nil, pageNodeKind())

		// Move a under b
		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+a.ID+"/move", strings.NewReader(`{"version":"`+a.Version+`","parentId":"`+b.ID+`"}`))
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected status 200, got %d", rec.Code)

		// Check if a is now a child of b
		movedParent := getPageByPathViaAPI(router, "section-b")
		Expect(movedParent.Children).To(ConsistOf(HaveField("ID", a.ID)), "Expected page to be moved under new parent")

	})
})

var _ = Describe("HTTP router", func() {
	It("returns not found when moving a missing page", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/not-found-id/move", strings.NewReader(`{"version":"missing","parentId":"root"}`))
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), "Expected status 404, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects page moves with invalid JSON", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/invalid-id/move", strings.NewReader(`this is not valid json`))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected status 400, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects page moves when the parent id is missing", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/missing-parent/move", strings.NewReader(`{"version":"missing","parentId":""}`))
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), "Expected status 404, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects page moves when the parent is missing", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		a := createPageViaAPI(router, "Section A", "section-a", nil, pageNodeKind())

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+a.ID+"/move", strings.NewReader(`{"version":"`+a.Version+`","parentId":"not-found-id"}`))
		fmt.Fprintf(GinkgoWriter, "Response: %s", rec.Body.String())
		fmt.Fprintf(GinkgoWriter, "Response Code: %d", rec.Code)
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), "Expected status 404, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects page moves that would create a cycle", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		a := createPageViaAPI(router, "Section A", "section-a", nil, pageNodeKind())
		b := createPageViaAPI(router, "Section B", "section-b", &a.ID, pageNodeKind())

		// Verschiebe a → unter b
		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+b.ID+"/move", strings.NewReader(`{"version":"`+b.Version+`","parentId":"`+a.ID+`"}`))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected status 400, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects page moves when the target already has the same slug", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		a := createPageViaAPI(router, "Section A", "section-a", nil, pageNodeKind())
		createPageViaAPI(router, "Section B", "section-b", nil, pageNodeKind())

		// Create Conflict Page in b
		conflictPage := createPageViaAPI(router, "Section B", "section-b", &a.ID, pageNodeKind())

		// move conflictPage under root (where section-b already exists)
		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+conflictPage.ID+"/move", strings.NewReader(`{"version":"`+conflictPage.Version+`","parentId":"root"}`))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected status 400, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("allows moving a page to its current location", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		a := createPageViaAPI(router, "Section A", "section-a", nil, pageNodeKind())

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+a.ID+"/move", strings.NewReader(`{"version":"`+a.Version+`","parentId":"root"}`))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected status 400, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("sorts sibling pages in the requested order", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Create pages
		page1 := createPageViaAPI(router, "Page 1", "page-1", nil, pageNodeKind())
		page2 := createPageViaAPI(router, "Page 2", "page-2", nil, pageNodeKind())
		page3 := createPageViaAPI(router, "Page 3", "page-3", nil, pageNodeKind())
		welcomePage := getPageByPathViaAPI(router, "welcome-to-leafwiki")
		deletePageViaAPI(router, welcomePage.ID, welcomePage.Version, false)

		// Sort pages
		payload := map[string]interface{}{
			"orderedIds": []string{page3.ID, page1.ID, page2.ID},
		}
		body, _ := json.Marshal(payload)

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/root/sort", strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected status 200, got %d", rec.Code)

		var resp map[string]interface{}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Failed to parse JSON: %v", err)
		}
		Expect(resp).To(testmatchers.HaveMessageID(wikipages.MessageIDAPIPagesSortSuccess), "Expected API-scoped success messageId, got: %v", resp["messageId"])

		root := getTreeViaAPI(router)
		Expect(root.Children).To(HaveExactElements(
			HaveField("ID", page3.ID),
			HaveField("ID", page1.ID),
			HaveField("ID", page2.ID),
		), "root children after sort = %#v", root.Children)

	})
})

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
	It("creates a user as an administrator", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `{"username": "john", "email": "john@example.com", "password": "secret123", "role": "editor"}`
		rec := authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(body))
		Expect(rec).To(HaveHTTPStatus(http.StatusCreated), "Expected 201 Created, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
	It("rejects user creation with an invalid role", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `{"username": "sam", "email": "sam@example.com", "password": "secret1234", "role": "undefined"}`
		rec := authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(body))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected 400 Bad Request for invalid role, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("creates a viewer user", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `{"username": "vieweruser", "email": "viewer@example.com", "password": "secret1234", "role": "viewer"}`
		rec := authenticatedRequest(router, http.MethodPost, "/api/users", strings.NewReader(body))
		Expect(rec).To(HaveHTTPStatus(http.StatusCreated), "Expected 201 Created for viewer role, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = DescribeTable("self-service mcp api key routes are blocked when auth is disabled",
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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

var _ = Describe("HTTP router", func() {
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
var _ = Describe("HTTP router", func() {
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
var _ = DescribeTable("asset routes enforce access control",
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

var _ = Describe("HTTP router", func() {
	It("build Custom Stylesheet Tag", func() {

		tag := httpinternal.BuildCustomStylesheetTag("/wiki", "/tmp/custom.css")

		expected := `<link rel="stylesheet" href="/wiki/custom.css">`
		Expect(tag).To(Equal(expected), "expected %q, got %q", expected, tag)

	})
})

var _ = Describe("HTTP router", func() {
	It("omits custom stylesheet tags for an empty path", func() {

		tag := httpinternal.BuildCustomStylesheetTag("", "")
		Expect(tag).To(BeEmpty(), "expected empty tag, got %q", tag)

	})
})

var _ = Describe("HTTP router", func() {
	It("injects configured markup into the document head", func() {

		html := "<html><head></head><body></body></html>"
		got := httpinternal.InjectIntoHead(html, `<link rel="stylesheet" href="/custom.css">`)
		Expect(got).To(ContainSubstring(`<link rel="stylesheet" href="/custom.css">`), "expected stylesheet link to be injected, got %q", got)

	})
})

var _ = Describe("router edge behavior", func() {
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
		storageDir := httpTestTempDir()

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

		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
	})

	It("returns the relative path error while validating a custom stylesheet", func() {
		storageDir := httpTestTempDir()
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
		storageDir := httpTestTempDir()
		missingCSSPath := filepath.Join(storageDir, "missing.css")
		router := httpinternal.NewRouter(nil, httpinternal.FrontendConfig{
			CustomStylesheetPath: missingCSSPath,
		}, httpinternal.RouterOptions{DisableRequestLog: true})

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/custom.css", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
	})

	It("returns 500 for a configured custom stylesheet that cannot be statted", func() {
		router := httpinternal.NewRouter(nil, httpinternal.FrontendConfig{
			CustomStylesheetPath: "bad\x00stylesheet.css",
		}, httpinternal.RouterOptions{DisableRequestLog: true})

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/custom.css", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusInternalServerError))
	})

	It("applies base path SPA fallback routing and index rewrites", func() {
		previous := httpinternal.EmbedFrontend
		httpinternal.EmbedFrontend = "true"
		DeferCleanup(func() {
			httpinternal.EmbedFrontend = previous
		})
		router := httpinternal.NewRouter(nil, httpinternal.FrontendConfig{
			CustomStylesheetPath: filepath.Join(httpTestTempDir(), "style.css"),
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
		Expect(outsideBasePath).To(HaveHTTPStatus(http.StatusNotFound))
		Expect(outsideBasePath).To(HaveHTTPBody("Page not found"))

		spaRoot := httptest.NewRecorder()
		router.ServeHTTP(spaRoot, httptest.NewRequest(http.MethodGet, "/wiki", nil))
		Expect(spaRoot).To(HaveHTTPStatus(http.StatusOK))
		Expect(spaRoot).To(HaveHTTPBody(SatisfyAll(
			ContainSubstring("Test Wiki"),
			ContainSubstring(`/wiki/custom.css`),
			ContainSubstring(`/wiki/branding/favicon.ico`),
			ContainSubstring(`test-injection`),
		)))

		nonGet := httptest.NewRecorder()
		router.ServeHTTP(nonGet, httptest.NewRequest(http.MethodPost, "/wiki/docs", nil))
		Expect(nonGet).To(HaveHTTPStatus(http.StatusNotFound))
		Expect(nonGet).To(HaveHTTPBody("Page not found"))
	})
})

type frontendFaviconHrefScenario struct {
	basePath    string
	faviconFile string
	want        string
}

var _ = DescribeTable("frontend favicon hrefs include the base path and branding file",
	func(tt frontendFaviconHrefScenario) {

		got := httpinternal.BuildFrontendFaviconHref(tt.basePath, tt.faviconFile)
		Expect(got).To(Equal(tt.want), "BuildFrontendFaviconHref(%q, %q) = %q, want %q", tt.basePath, tt.faviconFile, got, tt.want)

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

var _ = Describe("HTTP router", func() {
	It("serves configured custom stylesheets", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)

		customCSSPath := filepath.Join(w.GetStorageDir(), "custom.css")
		{
			err := os.WriteFile(customCSSPath, []byte("body { color: red; }"), 0644)
			Expect(err).NotTo(HaveOccurred(), "failed to create custom stylesheet: %v", err)
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
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "expected 200, got %d", rec.Code)
		{

			got := rec.Header().Get("Content-Type")
			Expect(got).To(Equal("text/css; charset=utf-8"), "expected css content-type, got %q", got)
		}
		Expect(rec).To(HaveHTTPBody(ContainSubstring("body { color: red; }")), "expected CSS body, got %q", rec.Body.String())

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects custom stylesheet paths outside the storage directory", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)

		outsideCSSPath := filepath.Join(httpTestTempDir(), "outside.css")
		{
			err := os.WriteFile(outsideCSSPath, []byte("body { color: blue; }"), 0644)
			Expect(err).NotTo(HaveOccurred(), "failed to create stylesheet outside storage dir: %v", err)
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
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), "expected 404 when stylesheet path is outside storage dir, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects custom stylesheet paths that are not css files", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)

		textFilePath := filepath.Join(w.GetStorageDir(), "custom.txt")
		{
			err := os.WriteFile(textFilePath, []byte("not css"), 0644)
			Expect(err).NotTo(HaveOccurred(), "failed to create non-css file: %v", err)
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
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), "expected 404 when stylesheet path is not a css file, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", func() {
	It("disables client caching for branding assets", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)

		router := createRouterTestInstance(w)
		uploadBrandingLogoViaAPI(router, "logo.png", []byte("logo"))

		req := httptest.NewRequest(http.MethodGet, "/branding/logo.png", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "expected 200, got %d", rec.Code)
		{

			got := rec.Header().Get("Cache-Control")
			Expect(got).To(Equal("no-store"), "expected Cache-Control no-store, got %q", got)
		}

	})
})

var _ = Describe("HTTP router", func() {
	It("disables client caching for favicon assets", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)

		EmbedFrontendOrig := httpinternal.EmbedFrontend
		httpinternal.EmbedFrontend = "true"
		DeferCleanup(func() {
			httpinternal.EmbedFrontend = EmbedFrontendOrig
		})

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
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "expected 200, got %d", rec.Code)
		{

			got := rec.Header().Get("Cache-Control")
			Expect(got).To(Equal("no-store"), "expected Cache-Control no-store, got %q", got)
		}

	})
})

var _ = Describe("HTTP router", func() {
	It("adds approval security headers to the oauth approval frontend route", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)

		embedFrontendOrig := httpinternal.EmbedFrontend
		httpinternal.EmbedFrontend = "true"
		DeferCleanup(func() {
			httpinternal.EmbedFrontend = embedFrontendOrig
		})

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
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "GET /oauth/approve = %d, want 200: %s", rec.Code, rec.Body.String())
		{

			got := rec.Header().Get("Cache-Control")
			Expect(got).To(Equal("no-store"), "approval Cache-Control = %q, want no-store", got)
		}
		{

			got := rec.Header().Get("Content-Security-Policy")
			Expect(got).To(ContainSubstring("frame-ancestors 'none'"), "approval Content-Security-Policy = %q, want frame-ancestors 'none'", got)
		}
		{

			got := rec.Header().Get("X-Frame-Options")
			Expect(got).To(Equal("DENY"), "approval X-Frame-Options = %q, want DENY", got)
		}

	})
})

var _ = Describe("HTTP router", func() {
	It("serves the configured branding favicon from the ico route", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)

		router := createRouterTestInstance(w)
		uploadBrandingFaviconViaAPI(router, "favicon.ico", []byte("custom-favicon"))

		req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "expected 200, got %d", rec.Code)
		{

			got := rec.Header().Get("Cache-Control")
			Expect(got).To(Equal("no-store"), "expected Cache-Control no-store, got %q", got)
		}
		{

			got := rec.Body.String()
			Expect(got).To(Equal("custom-favicon"), "expected custom favicon payload, got %q", got)
		}

	})
})

var _ = Describe("HTTP router", func() {
	It("falls back to the default svg favicon from the ico route", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)

		router := createRouterTestInstance(w)

		req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "expected 200, got %d", rec.Code)
		{

			got := rec.Header().Get("Cache-Control")
			Expect(got).To(Equal("no-store"), "expected Cache-Control no-store, got %q", got)
		}
		{

			got := rec.Body.String()
			Expect(got).To(ContainSubstring("<svg"), "expected default svg favicon response, got %q", got)
		}

	})
})

var _ = Describe("HTTP router", func() {
	It("omits custom stylesheet tags for whitespace-only paths", func() {

		tag := httpinternal.BuildCustomStylesheetTag("/wiki", "   ")
		Expect(tag).To(BeEmpty(), "expected empty tag for whitespace path, got %q", tag)

	})
})

var _ = DescribeTable("loopback host detection",
	func(host string, want bool) {
		{

			got := httpinternal.IsLoopbackHost(host)
			Expect(got).To(Equal(want), "IsLoopbackHost(%q) = %v, want %v", host, got, want)
		}

	},
	Entry("localhost", "localhost", true),
	Entry("localhost with whitespace and uppercase", " LOCALHOST ", true),
	Entry("i Pv 4 loopback", "127.0.0.1", true),
	Entry("i Pv 6 loopback", "::1", true),
	Entry("bracketed I Pv 6 loopback", "[::1]", true),
	Entry("non-loopback I Pv 4", "192.0.2.10", false),
	Entry("empty host", "", false),
	Entry("malformed host", "not a host", false),
)

var _ = DescribeTable("loopback remote address detection",
	func(remoteAddr string, want bool) {
		{

			got := httpinternal.IsLoopbackRemoteAddr(remoteAddr)
			Expect(got).To(Equal(want), "IsLoopbackRemoteAddr(%q) = %v, want %v", remoteAddr, got, want)
		}

	},
	Entry("i Pv 4 host port", "127.0.0.1:8080", true),
	Entry("i Pv 6 host port", "[::1]:8080", true),
	Entry("localhost host port", "localhost:8080", true),
	Entry("loopback without port", "127.0.0.1", true),
	Entry("non-loopback host port", "203.0.113.5:8080", false),
	Entry("empty remote addr", "", false),
	Entry("malformed remote addr", "not a remote addr", false),
)

var _ = Describe("HTTP router", func() {
	It("allows loopback requests through the local-only handler", func() {

		handler := httpinternal.LocalOnlyHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-Local-Only", "allowed")
			w.WriteHeader(http.StatusNoContent)
		}))

		req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
		req.RemoteAddr = "127.0.0.1:3456"
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusNoContent), "LocalOnlyHandler loopback status = %d, want %d", rec.Code, http.StatusNoContent)
		{

			got := rec.Header().Get("X-Local-Only")
			Expect(got).To(Equal("allowed"), "LocalOnlyHandler did not preserve wrapped response header, got %q", got)
		}

	})
})

var _ = Describe("HTTP router", func() {
	It("rejects non-loopback requests before they reach the wrapped handler", func() {

		handler := httpinternal.LocalOnlyHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-Local-Only", "called")
			w.WriteHeader(http.StatusNoContent)
		}))

		req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
		req.RemoteAddr = "203.0.113.5:3456"
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), "LocalOnlyHandler non-loopback status = %d, want %d", rec.Code, http.StatusNotFound)
		Expect(rec.Header()).NotTo(HaveKey("X-Local-Only"), "wrapped handler response header should not be present")

	})
})

func captureDefaultLogs() *bytes.Buffer {
	GinkgoHelper()

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	DeferCleanup(func() {
		slog.SetDefault(previous)
	})
	return &logs
}

func jsonLogEntries(logs string) []map[string]any {
	GinkgoHelper()

	entries := []map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry map[string]any
		{
			err := json.Unmarshal([]byte(line), &entry)
			Expect(err).NotTo(HaveOccurred(), "log line is not JSON: %v\n%s", err, line)
		}

		entries = append(entries, entry)
	}
	return entries
}
