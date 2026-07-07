package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	wikipages "github.com/perber/wiki/internal/wiki/pages"

	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/wiki"
)

// - Explicit README.md page link stays a page when index.md exists
var _ = Describe("HTTP router", Label("integration"), func() {
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

		w := createWikiTestInstanceWithWorkspace(wiki.Workspace{ID: newFixtureWorkspaceID("default"), DataDir: dataDir, RootDir: rootDir})
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

		Expect(readmePageResp).To(matchAPIPageMapIdentity(newFixturePageID("indexed-readme-page"), tree.NodeKindPage), "README page response = %#v, want indexed README page", readmePageResp)

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

var _ = Describe("HTTP router", Label("integration"), func() {
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

		w := createWikiTestInstanceWithWorkspace(wiki.Workspace{ID: newFixtureWorkspaceID("default"), DataDir: dataDir, RootDir: rootDir})
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		root := getTreeViaAPI(router)
		Expect(root.Children).To(ContainElement(matchExplicitContentSectionNode(
			newFixtureRoutePath("docs"),
			newFixtureMarkdownPath("docs/INDEX.MD"),
		)), "docs section missing from tree: %#v", root.Children)
		Expect(root.Children).To(ContainElement(matchReadmeFallbackSectionNode(
			newFixtureRoutePath("guides"),
		)), "guides section missing from tree: %#v", root.Children)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("creates a section twin when a page route already exists", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		sectionKind := tree.NodeKindSection
		page := createPageViaAPI(router, "Sync Page", newFixtureSlug("sync"), nil, pageNodeKind())
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

var _ = Describe("HTTP router", Label("integration"), func() {
	It("returns the current path for a page permalink", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		docs := createPageViaAPI(router, "Docs", newFixtureSlug("docs"), nil, pageNodeKind())
		guide := createPageViaAPI(router, "Guide", newFixtureSlug("guide"), &docs.ID, pageNodeKind())
		archive := createPageViaAPI(router, "Archive", newFixtureSlug("archive"), nil, pageNodeKind())

		movePayload := `{"version":"` + guide.Version + `","parentId":"` + archive.ID + `"}`
		moveRec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+guide.ID+"/move", strings.NewReader(movePayload))
		Expect(moveRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on move, got %d - %s", moveRec.Code, moveRec.Body.String())

		guide = getPageByPathViaAPI(router, "archive/guide")

		updatePayload := `{"version":"` + guide.Version + `","title":"User Guide","slug":"user-guide","content":""}`
		updateRec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+guide.ID, strings.NewReader(updatePayload))
		Expect(updateRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on update, got %d - %s", updateRec.Code, updateRec.Body.String())

		target := getPermalinkTargetViaAPI(router, apiPageDTOPathSegment(guide))
		Expect(target).To(matchAPIPermalinkTarget(
			apiPageDTOID(guide),
			newFixtureSlug("user-guide"),
			newFixtureRoutePath("archive/user-guide"),
			tree.NodeKindPage,
		), "permalink target = %#v", target)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
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

		page := createPageViaAPI(router, "Public Page", newFixtureSlug("public-page"), nil, pageNodeKind())

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

var _ = Describe("HTTP router", Label("integration"), func() {
	It("moves a page to a new parent", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Create two pages a and b
		a := createPageViaAPI(router, "Section A", newFixtureSlug("section-a"), nil, pageNodeKind())
		b := createPageViaAPI(router, "Section B", newFixtureSlug("section-b"), nil, pageNodeKind())

		// Move a under b
		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+a.ID+"/move", strings.NewReader(`{"version":"`+a.Version+`","parentId":"`+b.ID+`"}`))
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected status 200, got %d", rec.Code)

		// Check if a is now a child of b
		movedParent := getPageByPathViaAPI(router, "section-b")
		Expect(movedParent.Children).To(ConsistOf(HaveField("ID", a.ID)), "Expected page to be moved under new parent")

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("returns not found when moving a missing page", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/not-found-id/move", strings.NewReader(`{"version":"missing","parentId":"root"}`))
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), "Expected status 404, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects page moves with invalid JSON", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/invalid-id/move", strings.NewReader(`this is not valid json`))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected status 400, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects page moves when the parent id is missing", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/missing-parent/move", strings.NewReader(`{"version":"missing","parentId":""}`))
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), "Expected status 404, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects page moves when the parent is missing", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		a := createPageViaAPI(router, "Section A", newFixtureSlug("section-a"), nil, pageNodeKind())

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+a.ID+"/move", strings.NewReader(`{"version":"`+a.Version+`","parentId":"not-found-id"}`))
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), "Expected status 404, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects page moves that would create a cycle", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		a := createPageViaAPI(router, "Section A", newFixtureSlug("section-a"), nil, pageNodeKind())
		b := createPageViaAPI(router, "Section B", newFixtureSlug("section-b"), &a.ID, pageNodeKind())

		// Verschiebe a → unter b
		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+b.ID+"/move", strings.NewReader(`{"version":"`+b.Version+`","parentId":"`+a.ID+`"}`))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected status 400, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects page moves when the target already has the same slug", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		a := createPageViaAPI(router, "Section A", newFixtureSlug("section-a"), nil, pageNodeKind())
		createPageViaAPI(router, "Section B", newFixtureSlug("section-b"), nil, pageNodeKind())

		// Create Conflict Page in b
		conflictPage := createPageViaAPI(router, "Section B", newFixtureSlug("section-b"), &a.ID, pageNodeKind())

		// move conflictPage under root (where section-b already exists)
		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+conflictPage.ID+"/move", strings.NewReader(`{"version":"`+conflictPage.Version+`","parentId":"root"}`))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected status 400, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("allows moving a page to its current location", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		a := createPageViaAPI(router, "Section A", newFixtureSlug("section-a"), nil, pageNodeKind())

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+a.ID+"/move", strings.NewReader(`{"version":"`+a.Version+`","parentId":"root"}`))
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected status 400, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("sorts sibling pages in the requested order", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		// Create pages
		page1 := createPageViaAPI(router, "Page 1", newFixtureSlug("page-1"), nil, pageNodeKind())
		page2 := createPageViaAPI(router, "Page 2", newFixtureSlug("page-2"), nil, pageNodeKind())
		page3 := createPageViaAPI(router, "Page 3", newFixtureSlug("page-3"), nil, pageNodeKind())
		welcomePage := getPageByPathViaAPI(router, "welcome-to-leafwiki")
		deletePageViaAPI(router, apiPageDTOPathSegment(welcomePage), welcomePage.Version, false)

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
