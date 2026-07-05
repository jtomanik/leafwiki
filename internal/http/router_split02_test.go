package http_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	httpinternal "github.com/perber/wiki/internal/http"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing/fstest"

	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/wiki"
)

func workspaceSyncStatusObservationFor(status map[string]any) workspaceSyncStatusObservation {
	state := workspaceSyncStatusDisabled
	if enabled, ok := status["enabled"].(bool); ok && enabled {
		state = workspaceSyncStatusEnabled
	}
	lastCommitHash, _ := status["lastCommitHash"].(string)
	return workspaceSyncStatusObservation{State: state, LastCommitHash: lastCommitHash}
}

type apiSectionNodePresence uint8

const (
	apiSectionNodeMissing apiSectionNodePresence = iota
	apiSectionNodePresent
)

type apiSectionContentSource uint8

const (
	apiSectionContentExplicit apiSectionContentSource = iota
	apiSectionContentReadmeFallback
)

type apiSectionNodeObservation struct {
	Presence    apiSectionNodePresence
	Path        tree.RoutePath
	ContentPath tree.MarkdownPath
	Source      apiSectionContentSource
}

func apiSectionNodeObservationFor(node *apiPageDTO) apiSectionNodeObservation {
	if node == nil {
		return apiSectionNodeObservation{Presence: apiSectionNodeMissing}
	}
	source := apiSectionContentExplicit
	if node.ReadmeFallback {
		source = apiSectionContentReadmeFallback
	}
	return apiSectionNodeObservation{
		Presence:    apiSectionNodePresent,
		Path:        tree.RoutePathFromString(node.Path),
		ContentPath: tree.MarkdownPathFromString(node.ContentPath),
		Source:      source,
	}
}

type frontendSubFSBootstrapState uint8

const (
	frontendSubFSBootstrapReady frontendSubFSBootstrapState = iota
	frontendSubFSBootstrapPanicked
)

type frontendSubFSBootstrapObservation struct {
	State         frontendSubFSBootstrapState
	FailedDir     string
	Err           error
	RequestedDirs []string
}

func frontendSubFSBootstrapObservationFor(failures map[string]error) frontendSubFSBootstrapObservation {
	GinkgoHelper()

	previous := httpinternal.EmbedFrontend
	httpinternal.EmbedFrontend = "true"
	defer func() {
		httpinternal.EmbedFrontend = previous
	}()

	requestedDirs := []string{}
	var failedDir string
	var failedErr error
	restoreSubFS := httpinternal.SetFrontendSubFSForTest(func(_ fs.FS, dir string) (fs.FS, error) {
		requestedDirs = append(requestedDirs, dir)
		if err, fail := failures[dir]; fail && err != nil {
			failedDir = dir
			failedErr = err
			return nil, err
		}
		if dir == "dist" {
			return fstest.MapFS{
				"index.html": &fstest.MapFile{Data: []byte("<html><head></head><body></body></html>")},
			}, nil
		}
		return fstest.MapFS{}, nil
	})
	defer restoreSubFS()

	state := frontendSubFSBootstrapReady
	func() {
		defer func() {
			if recover() != nil {
				state = frontendSubFSBootstrapPanicked
			}
		}()
		httpinternal.NewRouter(nil, httpinternal.FrontendConfig{}, httpinternal.RouterOptions{DisableRequestLog: true})
	}()

	return frontendSubFSBootstrapObservation{
		State:         state,
		FailedDir:     failedDir,
		Err:           failedErr,
		RequestedDirs: requestedDirs,
	}
}

func matchFrontendSubFSPanic(failedDir string, errMatcher types.GomegaMatcher, requestedDirs types.GomegaMatcher) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"State":         Equal(frontendSubFSBootstrapPanicked),
		"FailedDir":     Equal(failedDir),
		"Err":           errMatcher,
		"RequestedDirs": requestedDirs,
	})
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
