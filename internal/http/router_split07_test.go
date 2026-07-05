package http_test

import (
	"encoding/json"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/core/markdown"
)

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects slug suggestions when the title is missing", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodGet, "/api/pages/slug-suggestion", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected status 400, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("deletes a page through the authenticated router", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		page := createPageViaAPI(router, "Delete Me", "delete-me", nil, pageNodeKind())
		rec := authenticatedRequest(router, http.MethodDelete, "/api/pages/"+page.ID+"?version="+page.Version, nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d", rec.Code)

		getRec := authenticatedRequest(router, http.MethodGet, "/api/pages/"+page.ID, nil)
		Expect(getRec).To(HaveHTTPStatus(http.StatusNotFound), "Expected deleted page to return 404, got %d", getRec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("returns not found when deleting a missing page", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		rec := authenticatedRequest(router, http.MethodDelete, "/api/pages/not-found-id", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), "Expected 404 Not Found, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects deleting a page that has children", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		parent := createPageViaAPI(router, "Parent", "parent", nil, pageNodeKind())
		createPageViaAPI(router, "Child", "child", &parent.ID, pageNodeKind())

		rec := authenticatedRequest(router, http.MethodDelete, "/api/pages/"+parent.ID+"?version="+parent.Version, nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), "Expected 400 Bad Request, got %d", rec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("deletes a page tree recursively when requested", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		parent := createPageViaAPI(router, "Parent", "parent", nil, pageNodeKind())
		createPageViaAPI(router, "Child", "child", &parent.ID, pageNodeKind())

		rec := authenticatedRequest(router, http.MethodDelete, "/api/pages/"+parent.ID+"?recursive=true&version="+parent.Version, nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d", rec.Code)

		getRec := authenticatedRequest(router, http.MethodGet, "/api/pages/"+parent.ID, nil)
		Expect(getRec).To(HaveHTTPStatus(http.StatusNotFound), "Expected deleted page to return 404, got %d", getRec.Code)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("updates page content through the authenticated router", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		page := createPageViaAPI(router, "Original Title", "original-title", nil, pageNodeKind())

		payload := map[string]string{
			"version": page.Version,
			"title":   "Updated Title",
			"slug":    "updated-title",
			"content": "# Updated Content\nWith **Markdown** support.",
		}
		body, _ := json.Marshal(payload)

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d", rec.Code)

		var resp map[string]interface{}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Invalid JSON response: %v", err)
		}
		Expect(resp).To(HaveKeyWithValue("title", "Updated Title"), "Expected updated title, got %q", resp["title"])
		Expect(resp).To(HaveKeyWithValue("slug", "updated-title"), "Expected updated slug, got %q", resp["slug"])
		Expect(resp).To(HaveKeyWithValue("content", "# Updated Content\nWith **Markdown** support."), "Expected updated content, got %q", resp["content"])

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("writes page tags and string properties", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		page := createPageViaAPI(router, "Original Title", "original-title", nil, pageNodeKind())

		payload := map[string]interface{}{
			"version": page.Version,
			"title":   "Updated Title",
			"slug":    "updated-title",
			"content": "# Updated Content",
			"tags":    []string{"React", "TypeScript"},
			"properties": map[string]string{
				"status": "published",
				"author": "alice",
			},
		}
		body, _ := json.Marshal(payload)

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

		getRec := authenticatedRequest(router, http.MethodGet, "/api/pages/"+page.ID, nil)
		Expect(getRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on get, got %d", getRec.Code)

		var fetched apiPageDTO
		{
			err := json.Unmarshal(getRec.Body.Bytes(), &fetched)
			Expect(err).NotTo(HaveOccurred(), "Invalid get response JSON: %v", err)
		}
		Expect(fetched).To(SatisfyAll(
			HaveField("Tags", Equal([]string{"react", "typescript"})),
			HaveField("Properties", SatisfyAll(
				HaveKeyWithValue("status", "published"),
				HaveKeyWithValue("author", "alice"),
			)),
		), "updated page metadata = %#v", fetched)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("removes page tags when an empty tag list is sent", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		page := createPageViaAPI(router, "Original Title", "original-title", nil, pageNodeKind())

		firstPayload := map[string]interface{}{
			"version": page.Version,
			"title":   "Original Title",
			"slug":    "original-title",
			"content": "# Updated Content",
			"tags":    []string{"React", "TypeScript"},
		}
		firstBody, _ := json.Marshal(firstPayload)

		firstRec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(firstBody)))
		Expect(firstRec).To(HaveHTTPStatus(http.StatusOK), "Expected first update to return 200 OK, got %d - %s", firstRec.Code, firstRec.Body.String())

		var updated apiPageDTO
		{
			err := json.Unmarshal(firstRec.Body.Bytes(), &updated)
			Expect(err).NotTo(HaveOccurred(), "Invalid first update response JSON: %v", err)
		}

		secondPayload := map[string]interface{}{
			"version": updated.Version,
			"title":   updated.Title,
			"slug":    updated.Slug,
			"content": updated.Content,
			"tags":    []string{},
		}
		secondBody, _ := json.Marshal(secondPayload)

		secondRec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(secondBody)))
		Expect(secondRec).To(HaveHTTPStatus(http.StatusOK), "Expected second update to return 200 OK, got %d - %s", secondRec.Code, secondRec.Body.String())

		getRec := authenticatedRequest(router, http.MethodGet, "/api/pages/"+page.ID, nil)
		Expect(getRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK on get, got %d", getRec.Code)

		var fetched apiPageDTO
		{
			err := json.Unmarshal(getRec.Body.Bytes(), &fetched)
			Expect(err).NotTo(HaveOccurred(), "Invalid get response JSON: %v", err)
		}
		Expect(fetched.Tags).To(HaveLen(0), "expected tags to be removed, got %#v", fetched.Tags)

		tagsRec := authenticatedRequest(router, http.MethodGet, "/api/tags?q=react&limit=20", nil)
		Expect(tagsRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK from tags endpoint, got %d - %s", tagsRec.Code, tagsRec.Body.String())

		var tagsResp []map[string]interface{}
		{
			err := json.Unmarshal(tagsRec.Body.Bytes(), &tagsResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid tags response JSON: %v", err)
		}

		for _, entry := range tagsResp {
			Expect(entry).NotTo(HaveKeyWithValue("tag", "react"), "expected react tag to be removed from index, got %#v", tagsResp)

		}

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("preserves omitted tags and properties while clearing explicit empty values", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		page := createPageViaAPI(router, "Metadata Preserve", "metadata-preserve", nil, pageNodeKind())

		firstPayload := map[string]interface{}{
			"version": page.Version,
			"title":   page.Title,
			"slug":    page.Slug,
			"content": "# Metadata Preserve\n\nFirst",
			"tags":    []string{"React"},
			"properties": map[string]string{
				"status": "draft",
			},
		}
		firstBody, _ := json.Marshal(firstPayload)
		firstRec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(firstBody)))
		Expect(firstRec).To(HaveHTTPStatus(http.StatusOK), "Expected first update to return 200 OK, got %d - %s", firstRec.Code, firstRec.Body.String())

		var firstUpdated apiPageDTO
		{
			err := json.Unmarshal(firstRec.Body.Bytes(), &firstUpdated)
			Expect(err).NotTo(HaveOccurred(), "Invalid first update response JSON: %v", err)
		}

		rawAfterFirstBytes, err := os.ReadFile(filepath.Join(w.GetRootDir(), "metadata-preserve.md"))
		Expect(err).NotTo(HaveOccurred(), "ReadFile first metadata update: %v", err)

		rawAfterFirst := string(rawAfterFirstBytes)
		Expect(rawAfterFirst).To(HavePrefix("<!-- leafwiki\n"), "HTTP update should write canonical LeafWiki metadata, got: %q", rawAfterFirst)
		Expect(rawAfterFirst).NotTo(HavePrefix("---\n"), "HTTP update should not write legacy YAML frontmatter, got: %q", rawAfterFirst)

		firstDoc, _, err := markdown.ParsePageDocument(rawAfterFirst)
		Expect(err).NotTo(HaveOccurred(), "ParsePageDocument first metadata update: %v", err)

		Expect(firstDoc.Metadata).To(SatisfyAll(
			HaveField("Tags", Equal([]string{"react"})),
			HaveField("Fields", HaveKeyWithValue("status", "draft")),
		), "first raw metadata = %#v", firstDoc.Metadata)

		metadataOnlyPayload := map[string]interface{}{
			"version": firstUpdated.Version,
			"title":   firstUpdated.Title,
			"slug":    firstUpdated.Slug,
			"tags":    []string{"Ready"},
			"properties": map[string]string{
				"status": "ready",
			},
		}
		metadataOnlyBody, _ := json.Marshal(metadataOnlyPayload)
		metadataOnlyRec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(metadataOnlyBody)))
		Expect(metadataOnlyRec).To(HaveHTTPStatus(http.StatusOK), "Expected metadata-only update to return 200 OK, got %d - %s", metadataOnlyRec.Code, metadataOnlyRec.Body.String())

		var metadataOnlyUpdated apiPageDTO
		{
			err := json.Unmarshal(metadataOnlyRec.Body.Bytes(), &metadataOnlyUpdated)
			Expect(err).NotTo(HaveOccurred(), "Invalid metadata-only update response JSON: %v", err)
		}
		Expect(metadataOnlyUpdated).To(SatisfyAll(
			HaveField("Content", "# Metadata Preserve\n\nFirst"),
			HaveField("Tags", Equal([]string{"ready"})),
			HaveField("Properties", HaveKeyWithValue("status", "ready")),
		), "metadata-only update = %#v", metadataOnlyUpdated)

		omittedPayload := map[string]interface{}{
			"version": metadataOnlyUpdated.Version,
			"title":   metadataOnlyUpdated.Title,
			"slug":    metadataOnlyUpdated.Slug,
			"content": "# Metadata Preserve\n\nSecond",
		}
		omittedBody, _ := json.Marshal(omittedPayload)
		omittedRec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(omittedBody)))
		Expect(omittedRec).To(HaveHTTPStatus(http.StatusOK), "Expected omitted metadata update to return 200 OK, got %d - %s", omittedRec.Code, omittedRec.Body.String())

		var omittedUpdated apiPageDTO
		{
			err := json.Unmarshal(omittedRec.Body.Bytes(), &omittedUpdated)
			Expect(err).NotTo(HaveOccurred(), "Invalid omitted update response JSON: %v", err)
		}

		Expect(omittedUpdated).To(SatisfyAll(
			HaveField("Tags", Equal([]string{"ready"})),
			HaveField("Properties", HaveKeyWithValue("status", "ready")),
		), "omitted metadata update = %#v", omittedUpdated)

		clearPayload := map[string]interface{}{
			"version":    omittedUpdated.Version,
			"title":      omittedUpdated.Title,
			"slug":       omittedUpdated.Slug,
			"content":    "# Metadata Preserve\n\nThird",
			"tags":       []string{},
			"properties": map[string]string{},
		}
		clearBody, _ := json.Marshal(clearPayload)
		clearRec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(clearBody)))
		Expect(clearRec).To(HaveHTTPStatus(http.StatusOK), "Expected explicit clear update to return 200 OK, got %d - %s", clearRec.Code, clearRec.Body.String())

		var cleared apiPageDTO
		{
			err := json.Unmarshal(clearRec.Body.Bytes(), &cleared)
			Expect(err).NotTo(HaveOccurred(), "Invalid clear update response JSON: %v", err)
		}
		Expect(cleared).To(SatisfyAll(
			HaveField("Tags", BeEmpty()),
			HaveField("Properties", BeEmpty()),
		), "explicit clear update = %#v", cleared)

		rawAfterClearBytes, err := os.ReadFile(filepath.Join(w.GetRootDir(), "metadata-preserve.md"))
		Expect(err).NotTo(HaveOccurred(), "ReadFile clear metadata update: %v", err)

		rawAfterClear := string(rawAfterClearBytes)
		Expect(rawAfterClear).To(HavePrefix("<!-- leafwiki\n"), "clear update should keep canonical storage, got: %q", rawAfterClear)
		Expect(rawAfterClear).NotTo(HavePrefix("---\n"), "clear update should keep canonical storage, got: %q", rawAfterClear)
		clearDoc, _, err := markdown.ParsePageDocument(rawAfterClear)
		Expect(err).NotTo(HaveOccurred(), "ParsePageDocument clear metadata update: %v", err)

		Expect(clearDoc.Metadata).To(SatisfyAll(
			HaveField("Tags", BeEmpty()),
			HaveField("Fields", BeEmpty()),
		), "clear raw metadata = %#v", clearDoc.Metadata)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("indexes updated page tags for the tags route", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		page := createPageViaAPI(router, "Original Title", "original-title", nil, pageNodeKind())

		payload := map[string]interface{}{
			"version": page.Version,
			"title":   "Updated Title",
			"slug":    "updated-title",
			"content": "# Updated Content",
			"tags":    []string{"react", "typescript"},
		}
		body, _ := json.Marshal(payload)

		rec := authenticatedRequest(router, http.MethodPut, "/api/pages/"+page.ID, strings.NewReader(string(body)))
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK, got %d - %s", rec.Code, rec.Body.String())

		tagsRec := authenticatedRequest(router, http.MethodGet, "/api/tags?q=react&limit=20", nil)
		Expect(tagsRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK from tags endpoint, got %d - %s", tagsRec.Code, tagsRec.Body.String())

		var tagsResp []map[string]interface{}
		{
			err := json.Unmarshal(tagsRec.Body.Bytes(), &tagsResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid tags response JSON: %v", err)
		}
		Expect(tagsResp).To(ContainElement(HaveKeyWithValue("tag", "react")), "expected indexed tags, got %#v", tagsResp)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("counts tag suggestions within the selected tags", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		pageA := createPageViaAPI(router, "Page A", "page-a", nil, pageNodeKind())
		pageB := createPageViaAPI(router, "Page B", "page-b", nil, pageNodeKind())
		pageC := createPageViaAPI(router, "Page C", "page-c", nil, pageNodeKind())

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
		updatePageTags(pageB, "Page B", "page-b", []string{"react", "testing"})
		updatePageTags(pageC, "Page C", "page-c", []string{"react", "typescript"})

		tagsRec := authenticatedRequest(router, http.MethodGet, "/api/tags?q=t&limit=20&selected=react", nil)
		Expect(tagsRec).To(HaveHTTPStatus(http.StatusOK), "Expected 200 OK from tags endpoint, got %d - %s", tagsRec.Code, tagsRec.Body.String())

		var tagsResp []map[string]interface{}
		{
			err := json.Unmarshal(tagsRec.Body.Bytes(), &tagsResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid tags response JSON: %v", err)
		}
		Expect(tagsResp).To(HaveExactElements(
			SatisfyAll(HaveKeyWithValue("tag", "typescript"), HaveKeyWithValue("count", float64(2))),
			SatisfyAll(HaveKeyWithValue("tag", "testing"), HaveKeyWithValue("count", float64(1))),
		), "expected selected-tag suggestions, got %#v", tagsResp)

	})
})
