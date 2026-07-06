package http_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fmt"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/assets"
	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/workspaceid"
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

func newFixturePageID[T ~string](raw T) tree.PageID {
	return tree.NewPageIDUnchecked(raw)
}

func newFixtureSlug[T ~string](raw T) tree.Slug {
	return tree.NewSlugUnchecked(raw)
}

func newFixtureRoutePath[T ~string](raw T) tree.RoutePath {
	return tree.NewRoutePathUnchecked(string(raw))
}

func newFixtureMarkdownPath[T ~string](raw T) tree.MarkdownPath {
	return tree.NewMarkdownPathUnchecked(string(raw))
}

func newFixtureWorkspaceID[T ~string](raw T) workspaceid.WorkspaceID {
	payload, err := json.Marshal(string(raw))
	Expect(err).To(Succeed())
	var id workspaceid.WorkspaceID
	Expect(json.Unmarshal(payload, &id)).To(Succeed())
	return id
}

func apiPageDTOID(page *apiPageDTO) tree.PageID {
	GinkgoHelper()
	return tree.PageIDFromString(page.ID)
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

func publishLinkRefactorEnabled() types.GomegaMatcher {
	return WithTransform(routerConfigObservationFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"LinkRefactor": Equal(configFeatureEnabled),
	}))
}

func publishWorkspaceSyncDisabled() types.GomegaMatcher {
	return WithTransform(routerConfigObservationFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"WorkspaceSync": Equal(configFeatureDisabled),
	}))
}

func reportWorkspaceSyncEnabledStatus() types.GomegaMatcher {
	return WithTransform(workspaceSyncStatusObservationFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"State":          Equal(workspaceSyncStatusEnabled),
		"LastCommitHash": Not(BeEmpty()),
	}))
}

func matchExplicitContentSectionNode(path tree.RoutePath, contentPath tree.MarkdownPath) types.GomegaMatcher {
	return WithTransform(apiSectionNodeObservationFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Presence":    Equal(apiSectionNodePresent),
		"Path":        Equal(path),
		"ContentPath": Equal(contentPath),
		"Source":      Equal(apiSectionContentExplicit),
	}))
}

func matchReadmeFallbackSectionNode(path tree.RoutePath) types.GomegaMatcher {
	return WithTransform(apiSectionNodeObservationFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Presence": Equal(apiSectionNodePresent),
		"Path":     Equal(path),
		"Source":   Equal(apiSectionContentReadmeFallback),
	}))
}

type configFeatureState uint8

const (
	configFeatureDisabled configFeatureState = iota
	configFeatureEnabled
)

type routerConfigObservation struct {
	LinkRefactor  configFeatureState
	WorkspaceSync configFeatureState
}

func routerConfigObservationFor(config map[string]any) routerConfigObservation {
	return routerConfigObservation{
		LinkRefactor:  configFeatureStateFor(config["enableLinkRefactor"]),
		WorkspaceSync: configFeatureStateFor(config["enableWorkspaceSync"]),
	}
}

func configFeatureStateFor(value any) configFeatureState {
	if enabled, ok := value.(bool); ok && enabled {
		return configFeatureEnabled
	}
	return configFeatureDisabled
}

type workspaceSyncStatusState uint8

const (
	workspaceSyncStatusDisabled workspaceSyncStatusState = iota
	workspaceSyncStatusEnabled
)

type workspaceSyncStatusObservation struct {
	State          workspaceSyncStatusState
	LastCommitHash string
}
