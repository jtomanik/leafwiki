package workspaced

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/assets"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/wiki"
)

var _ = ginkgo.Describe("workspaced routers", func() {
	ginkgo.DescribeTable("TestRouterExcludesPublicIdentityAndGlobalRoutes",
		func(method string, path string) {
			w := newTestWiki()
			ginkgo.DeferCleanup(func() {
				Expect(w.Close()).To(Succeed())
			})
			router := NewRouter(w, httpinternal.RouterOptions{
				PublicAccess:            true,
				AllowInsecure:           true,
				AuthDisabled:            true,
				AccessTokenTimeout:      15 * time.Minute,
				RefreshTokenTimeout:     7 * 24 * time.Hour,
				MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			})

			rec := request(router, method, path)
			Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), rec.Body.String())
		},
		ginkgo.Entry("config", http.MethodGet, "/api/config"),
		ginkgo.Entry("auth me", http.MethodGet, "/api/auth/me"),
		ginkgo.Entry("login", http.MethodPost, "/api/auth/login"),
		ginkgo.Entry("users", http.MethodGet, "/api/users"),
		ginkgo.Entry("branding", http.MethodGet, "/api/branding"),
		ginkgo.Entry("oauth token", http.MethodPost, "/oauth/token"),
		ginkgo.Entry("oauth metadata", http.MethodGet, "/.well-known/oauth-protected-resource/mcp"),
	)

	ginkgo.It("TestRouterExcludesPublicIdentityAndGlobalRoutes keeps workspace routes available", func() {
		w := newTestWiki()
		ginkgo.DeferCleanup(func() {
			Expect(w.Close()).To(Succeed())
		})
		router := NewRouter(w, httpinternal.RouterOptions{
			PublicAccess:            true,
			AllowInsecure:           true,
			AuthDisabled:            true,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		})

		rec := request(router, http.MethodGet, "/api/tree")
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	})

	ginkgo.DescribeTable("TestRoutersDoNotExposeEmbeddedFrontendRoutes",
		func(routerFactory func(*wiki.Wiki) http.Handler, path string) {
			embedFrontendOrig := httpinternal.EmbedFrontend
			httpinternal.EmbedFrontend = "true"
			ginkgo.DeferCleanup(func() {
				httpinternal.EmbedFrontend = embedFrontendOrig
			})
			stylesheetDir := ginkgo.GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(stylesheetDir, "custom.css"), []byte("body { color: red; }\n"), 0o644)).To(Succeed())
			ginkgo.GinkgoT().Chdir(stylesheetDir)

			w := newTestWiki()
			ginkgo.DeferCleanup(func() {
				Expect(w.Close()).To(Succeed())
			})
			router := routerFactory(w)

			rec := request(router, http.MethodGet, path)
			Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), rec.Body.String())
		},
		ginkgo.Entry("workspaced custom stylesheet", workspacedRouterFactory, "/custom.css"),
		ginkgo.Entry("workspaced favicon", workspacedRouterFactory, "/favicon.svg"),
		ginkgo.Entry("workspaced static asset", workspacedRouterFactory, "/static/index-DYW7NERi.js"),
		ginkgo.Entry("workspaced spa route", workspacedRouterFactory, "/workspace-spa-route"),
		ginkgo.Entry("authenticated workspaced custom stylesheet", authenticatedWorkspacedRouterFactory, "/custom.css"),
		ginkgo.Entry("authenticated workspaced favicon", authenticatedWorkspacedRouterFactory, "/favicon.svg"),
		ginkgo.Entry("authenticated workspaced static asset", authenticatedWorkspacedRouterFactory, "/static/index-DYW7NERi.js"),
		ginkgo.Entry("authenticated workspaced spa route", authenticatedWorkspacedRouterFactory, "/workspace-spa-route"),
	)
})

func newTestWiki() *wiki.Wiki {
	ginkgo.GinkgoHelper()
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		StorageDir:          ginkgo.GinkgoT().TempDir(),
		AdminPassword:       "admin",
		JWTSecret:           "secret",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
		AuthDisabled:        true,
	})
	Expect(err).NotTo(HaveOccurred())
	return w
}

func workspacedRouterOptions() httpinternal.RouterOptions {
	ginkgo.GinkgoHelper()
	return httpinternal.RouterOptions{
		PublicAccess:            true,
		AllowInsecure:           true,
		AuthDisabled:            true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
	}
}

func workspacedRouterFactory(w *wiki.Wiki) http.Handler {
	ginkgo.GinkgoHelper()
	opts := workspacedRouterOptions()
	opts.CustomStylesheet = "custom.css"
	return NewRouter(w, opts)
}

func authenticatedWorkspacedRouterFactory(w *wiki.Wiki) http.Handler {
	ginkgo.GinkgoHelper()
	opts := workspacedRouterOptions()
	opts.CustomStylesheet = "custom.css"
	return NewAuthenticatedRouter(w, opts, PrivateAuthOptions{
		DaemonToken: "private-token",
		WorkspaceID: "current",
		Now:         func() time.Time { return time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC) },
	})
}

func request(router http.Handler, method, path string) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	req := httptest.NewRequest(method, path, strings.NewReader(""))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
