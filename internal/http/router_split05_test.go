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
	"strings"
	"time"

	"github.com/perber/wiki/internal/core/assets"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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
