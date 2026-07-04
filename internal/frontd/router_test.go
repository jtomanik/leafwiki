package frontd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/assets"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/wiki"
)

type publicRuntimeRouteCase struct {
	method     string
	path       string
	wantStatus int
}

var _ = Describe("public runtime router", Label("integration"), func() {
	DescribeTable("serves public runtime routes",
		func(tc publicRuntimeRouteCase) {
			w := newTestWiki()
			DeferCleanup(w.Close)
			router := NewRouter(w, httpinternal.RouterOptions{
				PublicAccess:            true,
				AllowInsecure:           true,
				AuthDisabled:            true,
				AccessTokenTimeout:      15 * time.Minute,
				RefreshTokenTimeout:     7 * 24 * time.Hour,
				MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			})

			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			Expect(rec).To(HaveHTTPStatus(tc.wantStatus))
		},
		Entry("serves public config", publicRuntimeRouteCase{method: http.MethodGet, path: "/api/config", wantStatus: http.StatusOK}),
		Entry("serves the current-user endpoint", publicRuntimeRouteCase{method: http.MethodGet, path: "/api/auth/me", wantStatus: http.StatusOK}),
		Entry("serves branding", publicRuntimeRouteCase{method: http.MethodGet, path: "/api/branding", wantStatus: http.StatusOK}),
	)
})

func newTestWiki() *wiki.Wiki {
	GinkgoHelper()
	storageDir, err := os.MkdirTemp("", "leafwiki-frontd-*")
	Expect(err).To(Succeed())
	DeferCleanup(os.RemoveAll, storageDir)

	w, err := wiki.NewWiki(&wiki.WikiOptions{
		StorageDir:          storageDir,
		AdminPassword:       "admin",
		JWTSecret:           "secret",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
		AuthDisabled:        true,
	})
	Expect(err).To(Succeed())
	return w
}
