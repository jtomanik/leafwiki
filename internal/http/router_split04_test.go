package http_test

import (
	"context"
	"encoding/json"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/perber/wiki/internal/core/assets"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = Describe("HTTP router", Label("integration"), func() {
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
		Expect(resp).To(reportWorkspaceSyncEnabledStatus(), "workspace sync status response = %#v", resp)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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
