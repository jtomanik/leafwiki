package mcp_test

import (
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/markdown"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - MCP agent context returns canonical examples

func matchSearchResults(httpSearch map[string]any) types.GomegaMatcher {
	GinkgoHelper()

	return SatisfyAll(
		HaveKeyWithValue("count", matchJSONEqual(httpSearch["count"])),
		HaveKeyWithValue("offset", matchJSONEqual(httpSearch["offset"])),
		HaveKeyWithValue("limit", matchJSONEqual(httpSearch["limit"])),
		HaveKeyWithValue("items", matchJSONEqual(arrayFieldFromMap(httpSearch, "items"))),
		HaveKeyWithValue("tagFacets", matchJSONEqual(httpSearch["tag_facets"])),
		HaveKeyWithValue("hasMore", Equal(searchResultsHasMoreFromHTTP(httpSearch))),
	)
}

func matchMapFields(want map[string]any, fields []string) types.GomegaMatcher {
	GinkgoHelper()

	matchers := make([]types.GomegaMatcher, 0, len(fields))
	for _, field := range fields {
		matchers = append(matchers, HaveKeyWithValue(field, matchJSONEqual(want[field])))
	}
	return SatisfyAll(matchers...)
}

func searchResultsHasMoreFromHTTP(httpSearch map[string]any) bool {
	count := numericMapField(httpSearch, "count")
	offset := numericMapField(httpSearch, "offset")
	items := arrayFieldFromMap(httpSearch, "items")
	return int(offset)+len(items) < int(count)
}

func numericMapField(value map[string]any, key string) float64 {
	GinkgoHelper()
	Expect(value).To(HaveKeyWithValue(key, BeAssignableToTypeOf(float64(0))))
	return value[key].(float64)
}

func matchRestoredMetadata() types.GomegaMatcher {
	GinkgoHelper()

	return SatisfyAll(
		HaveKeyWithValue("content", "metadata revision\n"),
		WithTransform(func(page map[string]any) []string {
			return stringSliceField(page, "tags")
		}, matchStringSet([]string{"restore", "metadata"})),
		HaveKeyWithValue("properties", HaveKeyWithValue("status", "archived")),
	)
}

func matchMCPPageError(code sharederrors.ErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	GinkgoHelper()

	return SatisfyAll(
		testmatchers.HaveMCPStructuredError(code, messageID),
		HaveField("Message", Not(BeEmpty())),
	)
}

func matchHTTPPageError(code sharederrors.ErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	GinkgoHelper()

	return WithTransform(func(body string) httpPageErrorPayloadWire {
		payload := decodeJSONMap("HTTP page error", []byte(body))
		errPayload := nestedMap(payload, "error")
		return httpPageErrorPayloadFromWire("HTTP page error", errPayload)
	}, SatisfyAll(
		testmatchers.HaveStructuredError(code, messageID),
		HaveField("Message", Not(BeEmpty())),
		HaveField("Template", Not(BeEmpty())),
	))
}

func matchMCPPageVersionConflict() types.GomegaMatcher {
	GinkgoHelper()

	return matchMCPPageError(wikipages.ErrCodePageVersionConflict, sharederrors.MessageIDForCode(wikipages.ErrCodePageVersionConflict))
}

func matchHTTPPageVersionConflict() types.GomegaMatcher {
	GinkgoHelper()

	return matchHTTPPageError(wikipages.ErrCodePageVersionConflict, sharederrors.MessageIDForCode(wikipages.ErrCodePageVersionConflict))
}

func matchRestoreVolatileFields() types.GomegaMatcher {
	GinkgoHelper()

	return SatisfyAll(
		HaveKeyWithValue("version", SatisfyAll(BeAssignableToTypeOf(""), Not(BeEmpty()))),
		HaveKeyWithValue("metadata", HaveKeyWithValue("updatedAt", matchRFC3339Timestamp())),
	)
}

func matchPageState(id tree.PageID, title string, slug tree.Slug, pathValue, kind string, parentID tree.PageID) types.GomegaMatcher {
	GinkgoHelper()

	return SatisfyAll(
		HaveKeyWithValue("id", id.MetadataValue()),
		HaveKeyWithValue("title", title),
		WithTransform(pageSlugFromWire, Equal(slug)),
		HaveKeyWithValue("path", pathValue),
		HaveKeyWithValue("kind", kind),
		WithTransform(func(page map[string]any) string {
			return stringValue(page["parentId"])
		}, Equal(parentID.MetadataValue())),
	)
}

func pageSlugFromWire(page map[string]any) tree.Slug {
	GinkgoHelper()
	return tree.SlugFromString(stringValue(page["slug"]))
}

func matchChildOrder(childIDs ...tree.PageID) types.GomegaMatcher {
	GinkgoHelper()

	return HaveKeyWithValue("children", HaveExactElements(childIDMatchers(childIDs)...))
}

func matchChildrenExcludingIDs(childIDs ...tree.PageID) types.GomegaMatcher {
	GinkgoHelper()

	matchers := make([]types.GomegaMatcher, 0, len(childIDs))
	for _, childID := range childIDs {
		matchers = append(matchers, Not(ContainElement(childID.MetadataValue())))
	}
	return WithTransform(childIDsFromPage, SatisfyAll(matchers...))
}

func matchRFC3339Timestamp() types.GomegaMatcher {
	GinkgoHelper()
	return SatisfyAll(
		BeAssignableToTypeOf(""),
		WithTransform(func(value string) (time.Time, error) {
			return time.Parse(time.RFC3339, value)
		}, Not(BeZero())),
	)
}

func childIDsFromPage(page map[string]any) []string {
	GinkgoHelper()
	rawChildren := arrayFieldFromMap(page, "children")
	ids := make([]string, 0, len(rawChildren))
	for _, raw := range rawChildren {
		Expect(raw).To(BeAssignableToTypeOf(map[string]any{}))
		child := raw.(map[string]any)
		ids = append(ids, stringValue(child["id"]))
	}
	return ids
}

func childIDMatchers(childIDs []tree.PageID) []any {
	matchers := make([]any, 0, len(childIDs))
	for _, id := range childIDs {
		matchers = append(matchers, HaveKeyWithValue("id", id.MetadataValue()))
	}
	return matchers
}

func canonicalPageMarkdown(label, raw string) markdown.PageDocument {
	GinkgoHelper()

	Expect(raw).To(HavePrefix("<!-- leafwiki\n"), "%s should start with canonical LeafWiki metadata", label)
	Expect(raw).NotTo(HavePrefix("---\n"), "%s should not start with legacy YAML frontmatter", label)
	doc, _, err := markdown.ParsePageDocument(raw)
	Expect(err).NotTo(HaveOccurred(), "%s should parse with ParsePageDocument", label)
	return doc
}

func matchAssetURLResult(field string, pageID tree.PageID) types.GomegaMatcher {
	GinkgoHelper()

	prefix := "/assets/" + pageID.MetadataValue() + "/"
	return SatisfyAll(
		HaveKeyWithValue(field, SatisfyAll(HavePrefix(prefix), Not(Equal(prefix)))),
		HaveLen(1),
	)
}

func matchScopedSuccessPayload(httpPayload map[string]any, mcpMessageID wikimcp.ToolMessageID, httpMessageID sharederrors.MessageID) types.GomegaMatcher {
	GinkgoHelper()

	expected := scopedSuccessPayloadProjection{
		MCPMessageID:  mcpMessageID,
		HTTPMessageID: httpMessageID,
		Body:          httpSuccessPayloadFromWire("HTTP success payload", httpPayload).Body,
	}
	return WithTransform(func(mcpPayload map[string]any) scopedSuccessPayloadProjection {
		httpSuccess := httpSuccessPayloadFromWire("HTTP success payload", httpPayload)
		mcpSuccess := mcpSuccessPayloadFromWire("MCP success payload", mcpPayload)
		return scopedSuccessPayloadProjection{
			MCPMessageID:  mcpSuccess.MessageID,
			HTTPMessageID: httpSuccess.MessageID,
			Body:          mcpSuccess.Body,
		}
	}, Equal(expected),
	)
}

type mcpSuccessPayloadWire struct {
	MessageID wikimcp.ToolMessageID `json:"messageId"`
	Body      any
}

type httpSuccessPayloadWire struct {
	MessageID sharederrors.MessageID `json:"messageId"`
	Body      any
}

type scopedSuccessPayloadProjection struct {
	MCPMessageID  wikimcp.ToolMessageID
	HTTPMessageID sharederrors.MessageID
	Body          any
}

func mcpSuccessPayloadFromWire(label string, payload map[string]any) mcpSuccessPayloadWire {
	GinkgoHelper()
	raw, err := json.Marshal(payload)
	Expect(err).NotTo(HaveOccurred(), "%s should marshal", label)
	var decoded struct {
		MessageID wikimcp.ToolMessageID `json:"messageId"`
	}
	Expect(json.Unmarshal(raw, &decoded)).To(Succeed(), "%s should decode", label)
	return mcpSuccessPayloadWire{
		MessageID: decoded.MessageID,
		Body:      normalizeJSON(mapWithoutField(payload, newFixtureJSONPayloadField("messageId"))),
	}
}

func httpSuccessPayloadFromWire(label string, payload map[string]any) httpSuccessPayloadWire {
	GinkgoHelper()
	raw, err := json.Marshal(payload)
	Expect(err).NotTo(HaveOccurred(), "%s should marshal", label)
	var decoded struct {
		MessageID sharederrors.MessageID `json:"messageId"`
	}
	Expect(json.Unmarshal(raw, &decoded)).To(Succeed(), "%s should decode", label)
	return httpSuccessPayloadWire{
		MessageID: decoded.MessageID,
		Body:      normalizeJSON(mapWithoutField(payload, newFixtureJSONPayloadField("messageId"))),
	}
}

func matchLookupFinalID(wantID string, wantKind string) types.GomegaMatcher {
	GinkgoHelper()

	return WithTransform(finalLookupSegment, SatisfyAll(
		HaveKeyWithValue("id", wantID),
		HaveKeyWithValue("kind", wantKind),
	))
}

func matchValidationIssueCodes(wantCodes []wikivalidation.IssueCode) types.GomegaMatcher {
	GinkgoHelper()

	return WithTransform(validationIssueCodes, ContainElements(issueCodeElements(wantCodes)...))
}

func finalLookupSegment(lookup map[string]any) (map[string]any, error) {
	GinkgoHelper()
	segments := arrayFieldFromMap(lookup, "segments")
	if len(segments) == 0 {
		return nil, fmt.Errorf("lookup should include at least one segment")
	}
	final, ok := segments[len(segments)-1].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("final lookup segment should be an object")
	}
	return final, nil
}
