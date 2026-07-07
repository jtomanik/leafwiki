package http_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	wikipages "github.com/perber/wiki/internal/wiki/pages"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/wiki"
)

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects page updates with invalid JSON", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `this is not valid json`
		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/invalid-id", strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected 400 for invalid JSON, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects page updates when the title is missing", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `{"version":"required","slug":"updated","content":"New content"}`
		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/missing-title", strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected 400 for missing title, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects page updates when the slug is missing", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `{"version":"required","title":"Updated","content":"New content"}`
		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/missing-slug", strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected 400 for missing slug, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects page updates with invalid properties", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		page := createPageViaAPI(router, "Original Title", newFixtureSlug("original-title"), nil, pageNodeKind())

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

var _ = Describe("HTTP router", Label("integration"), func() {
	It("returns a page by id", func() {

		dataDir := filepath.Join(httpTestTempDir(), "data")
		rootDir := filepath.Join(httpTestTempDir(), "content")
		w := createWikiTestInstanceWithWorkspace(wiki.Workspace{ID: newFixtureWorkspaceID("default"), DataDir: dataDir, RootDir: rootDir})
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Create a page
		page := createPageViaAPI(router, "Welcome", newFixtureSlug("welcome"), nil, pageNodeKind())
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

var _ = Describe("HTTP router", Label("integration"), func() {
	It("returns not found for a missing page id", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodGet, "/api/pages/not-found-id", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), "Expected status 404, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects page reads when the id is missing", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodGet, "/api/pages/", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), "Expected status 404, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects page-by-path reads when the path is missing", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected status 400, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
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

		w := createWikiTestInstanceWithWorkspace(wiki.Workspace{ID: newFixtureWorkspaceID("default"), DataDir: dataDir, RootDir: rootDir})
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

var _ = Describe("HTTP router", Label("integration"), func() {
	It("returns not found for a missing page path", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path=does-not-exist", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), "Expected status 404, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("omits children when page-by-path resolves to a page", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Create a standalone page (no children – adding children auto-converts it to a section)
		createPageViaAPI(router, "My Page", newFixtureSlug("my-page"), nil, pageNodeKind())

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

var _ = Describe("HTTP router", Label("integration"), func() {
	It("returns only direct children when page-by-path resolves to a section", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		sectionKind := tree.NodeKindSection

		// Create a section with a child page that itself has a grandchild
		section := createPageViaAPI(router, "My Section", newFixtureSlug("my-section"), nil, &sectionKind)
		child := createPageViaAPI(router, "Child Page", newFixtureSlug("child-page"), &section.ID, pageNodeKind())
		createPageViaAPI(router, "Grandchild Page", newFixtureSlug("grandchild-page"), &child.ID, pageNodeKind())

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

var _ = Describe("HTTP router", Label("integration"), func() {
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

		w := createWikiTestInstanceWithWorkspace(wiki.Workspace{ID: newFixtureWorkspaceID("default"), DataDir: dataDir, RootDir: rootDir})
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		pageRec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path=docs/sync&kind=page", nil)
		Expect(pageRec).To(HaveHTTPStatus(http.StatusOK), "Expected page status 200, got %d - %s", pageRec.Code, pageRec.Body.String())

		var pageResp map[string]interface{}
		{
			err := json.Unmarshal(pageRec.Body.Bytes(), &pageResp)
			Expect(err).NotTo(HaveOccurred(), "parse page response: %v", err)
		}

		Expect(pageResp).To(matchAPIPageMapIdentity(newFixturePageID("sync-page"), tree.NodeKindPage), "page response = %#v, want sync-page page", pageResp)

		pageMarkdownPathRec := authenticatedRequest(router, http.MethodGet, "/api/pages/by-path?path=docs/sync.md", nil)
		Expect(pageMarkdownPathRec).To(HaveHTTPStatus(http.StatusOK), "Expected page markdown-path status 200, got %d - %s", pageMarkdownPathRec.Code, pageMarkdownPathRec.Body.String())

		var pageMarkdownPathResp map[string]interface{}
		{
			err := json.Unmarshal(pageMarkdownPathRec.Body.Bytes(), &pageMarkdownPathResp)
			Expect(err).NotTo(HaveOccurred(), "parse page markdown-path response: %v", err)
		}

		Expect(pageMarkdownPathResp).To(matchAPIPageMapIdentity(newFixturePageID("sync-page"), tree.NodeKindPage), "page markdown-path response = %#v, want sync-page page", pageMarkdownPathResp)

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
