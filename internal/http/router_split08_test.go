package http_test

import (
	"encoding/json"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"net/http"
	"strings"

	"github.com/perber/wiki/internal/core/tree"
)

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
	It("returns not found when updating a missing page", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		body := `{"version":"stale-version","title":"Updated","slug":"updated","content":"New content"}`
		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/not-found-id", strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), "Expected 404 for unknown page, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
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

var _ = Describe("HTTP router", Label("integration"), func() {
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
