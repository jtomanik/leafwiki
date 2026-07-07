package http_test

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	httpinternal "github.com/perber/wiki/internal/http"

	"github.com/gin-gonic/gin"
	"github.com/perber/wiki/internal/core/assets"
)

var _ = Describe("router frontend bootstrap and asset behavior", func() {
	It("sets Gin release mode in production", Label("integration"), func() {
		previous := httpinternal.Environment
		httpinternal.Environment = "production"
		DeferCleanup(func() {
			httpinternal.Environment = previous
			gin.SetMode(gin.TestMode)
		})

		httpinternal.NewRouter(nil, httpinternal.FrontendConfig{}, httpinternal.RouterOptions{DisableFrontendRoutes: true})

		Expect(gin.Mode()).To(Equal(gin.ReleaseMode))
	})

	It("normalizes empty and relative custom stylesheet paths", Label("unit"), func() {
		storageDir := httpTestTempDir()

		empty, err := httpinternal.NormalizeCustomStylesheetPath(storageDir, " \t\n ")
		Expect(err).NotTo(HaveOccurred())
		Expect(empty).To(BeEmpty())

		relative, err := httpinternal.NormalizeCustomStylesheetPath(storageDir, "styles/custom.css")
		Expect(err).NotTo(HaveOccurred())
		Expect(relative).To(Equal(filepath.Join(storageDir, "styles", "custom.css")))
	})

	It("leaves HTML unchanged when injecting into a document without a head close tag", Label("unit"), func() {
		html := "<html><body>content</body></html>"

		Expect(httpinternal.InjectIntoHead(html, `<script src="/custom.js"></script>`)).To(Equal(html))
	})

	It("panics when the embedded frontend dist filesystem cannot be opened", Label("integration"), func() {
		distUnavailableErr := errors.New("dist unavailable")

		Expect(frontendSubFSBootstrapObservationFor(map[string]error{
			"dist": distUnavailableErr,
		})).To(matchFrontendSubFSPanic("dist", Equal(distUnavailableErr), Equal([]string{"dist"})))
	})

	It("panics when the embedded frontend static filesystem cannot be opened", Label("integration"), func() {
		staticUnavailableErr := errors.New("static unavailable")

		Expect(frontendSubFSBootstrapObservationFor(map[string]error{
			"dist/static": staticUnavailableErr,
		})).To(matchFrontendSubFSPanic("dist/static", Equal(staticUnavailableErr), Equal([]string{"dist", "dist/static"})))
	})

	It("returns 404 when the embedded SPA index cannot be read", Label("integration"), func() {
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

	It("returns the relative path error while validating a custom stylesheet", Label("unit"), func() {
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

	It("returns 404 for a configured custom stylesheet that is missing on disk", Label("integration"), func() {
		storageDir := httpTestTempDir()
		missingCSSPath := filepath.Join(storageDir, "missing.css")
		router := httpinternal.NewRouter(nil, httpinternal.FrontendConfig{
			CustomStylesheetPath: missingCSSPath,
		}, httpinternal.RouterOptions{DisableRequestLog: true})

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/custom.css", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
	})

	It("returns 500 for a configured custom stylesheet that cannot be statted", Label("integration"), func() {
		router := httpinternal.NewRouter(nil, httpinternal.FrontendConfig{
			CustomStylesheetPath: "bad\x00stylesheet.css",
		}, httpinternal.RouterOptions{DisableRequestLog: true})

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/custom.css", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusInternalServerError))
	})

	It("applies base path SPA fallback routing and index rewrites", Label("integration"), func() {
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

var _ = DescribeTable("frontend favicon hrefs include the base path and branding file", Label("unit"),
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

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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
