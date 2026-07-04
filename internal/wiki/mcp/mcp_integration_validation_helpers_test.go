package mcp_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - MCP agent context returns canonical examples

func issueCodeElements(codes []wikivalidation.IssueCode) []any {
	out := make([]any, 0, len(codes))
	for _, code := range codes {
		out = append(out, code)
	}
	return out
}

type httpPageErrorPayloadWire struct {
	Code      sharederrors.ErrorCode `json:"code"`
	MessageID sharederrors.MessageID `json:"messageId"`
	Message   string                 `json:"message"`
	Template  string                 `json:"template"`
}

func httpPageErrorPayloadFromWire(label string, payload map[string]any) httpPageErrorPayloadWire {
	GinkgoHelper()

	raw, err := json.Marshal(payload)
	Expect(err).NotTo(HaveOccurred(), "%s error payload should marshal", label)
	var decoded httpPageErrorPayloadWire
	Expect(json.Unmarshal(raw, &decoded)).To(Succeed(), "%s error payload should decode", label)
	return decoded
}

func normalizeRestorePayload(page map[string]any) map[string]any {
	GinkgoHelper()

	normalizedValue := normalizeJSON(page)
	Expect(normalizedValue).To(BeAssignableToTypeOf(map[string]any{}), "restore payload should normalize to object")
	normalized := normalizedValue.(map[string]any)
	delete(normalized, "version")
	if metadata, ok := normalized["metadata"].(map[string]any); ok {
		delete(metadata, "updatedAt")
	}
	return normalized
}

func readPageMarkdownByRoutePath(rootDir, routePath string) string {
	GinkgoHelper()

	path := filepath.Join(append([]string{rootDir}, strings.Split(routePath, "/")...)...) + ".md"
	raw, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred(), "read markdown page %s", path)
	return string(raw)
}

func stringValue(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func matchJSONEqual(want any) types.GomegaMatcher {
	GinkgoHelper()

	normalizedWant := normalizeJSON(want)
	return WithTransform(func(got any) any {
		return normalizeJSON(got)
	}, Equal(normalizedWant))
}

func mapWithoutField(payload map[string]any, field string) map[string]any {
	out := make(map[string]any, len(payload))
	for key, value := range payload {
		if key == field {
			continue
		}
		out[key] = value
	}
	return out
}

func normalizeJSON(value any) any {
	GinkgoHelper()

	raw, err := json.Marshal(value)
	Expect(err).NotTo(HaveOccurred())
	return decodeJSONValue("normalize JSON", raw)
}

func decodeJSONValue(label string, raw []byte) any {
	GinkgoHelper()

	var out any
	Expect(json.Unmarshal(raw, &out)).To(Succeed(), "decode %s", label)
	return out
}

func issueHTTPCSRF(router http.Handler) (string, []*http.Cookie) {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	token := rec.Header().Get("X-CSRF-Token")
	result := rec.Result()
	cookies := result.Cookies()
	Expect(result.Body.Close()).To(Succeed())
	if token == "" {
		for _, cookie := range cookies {
			if cookie.Name == "leafwiki_csrf" || cookie.Name == "__Host-leafwiki_csrf" {
				token = cookie.Value
				break
			}
		}
	}
	Expect(token).NotTo(BeEmpty())
	return token, cookies
}

func nestedMap(value map[string]any, key string) map[string]any {
	GinkgoHelper()

	Expect(value).To(HaveKeyWithValue(key, BeAssignableToTypeOf(map[string]any{})))
	return value[key].(map[string]any)
}

func stringField(value map[string]any, key string) string {
	GinkgoHelper()

	Expect(value).To(HaveKeyWithValue(key, SatisfyAll(BeAssignableToTypeOf(""), Not(BeEmpty()))))
	return value[key].(string)
}

func arrayField(value map[string]any, key string) []any {
	GinkgoHelper()

	return arrayFieldFromMap(value, key)
}

func arrayFieldFromMap(value map[string]any, key string) []any {
	GinkgoHelper()

	Expect(value).To(HaveKeyWithValue(key, BeAssignableToTypeOf([]any{})))
	return value[key].([]any)
}

func stringSliceField(value map[string]any, key string) []string {
	GinkgoHelper()

	raw := arrayField(value, key)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		Expect(item).To(BeAssignableToTypeOf(""))
		out = append(out, item.(string))
	}
	return out
}

func changedPathsFromContext(output map[string]any) map[string]bool {
	GinkgoHelper()

	return changedPathsFromChanges(arrayField(output, "changesSincePreviousContext"))
}

func changedPathsFromChanges(changes []any) map[string]bool {
	GinkgoHelper()

	paths := map[string]bool{}
	for _, raw := range changes {
		Expect(raw).To(BeAssignableToTypeOf(map[string]any{}))
		change := raw.(map[string]any)
		for _, value := range arrayFieldFromMap(change, "changedPaths") {
			Expect(value).To(BeAssignableToTypeOf(""))
			paths[value.(string)] = true
		}
	}
	return paths
}

type validationIssueWire struct {
	Code wikivalidation.IssueCode `json:"code"`
	Path string                   `json:"path"`
}

func validationIssueFromWire(item map[string]any) validationIssueWire {
	GinkgoHelper()

	raw, err := json.Marshal(item)
	Expect(err).NotTo(HaveOccurred(), "validation issue should marshal")
	var decoded validationIssueWire
	Expect(json.Unmarshal(raw, &decoded)).To(Succeed(), "validation issue should decode")
	return decoded
}

func validationIssueByCode(output map[string]any, wantCode wikivalidation.IssueCode) map[string]any {
	GinkgoHelper()

	issues := arrayField(output, "issues")
	Expect(issues).To(ContainElement(Satisfy(func(issue any) bool {
		item, ok := issue.(map[string]any)
		return ok && validationIssueFromWire(item).Code == wantCode
	})))
	matches := []map[string]any{}
	for _, issue := range issues {
		Expect(issue).To(BeAssignableToTypeOf(map[string]any{}))
		item := issue.(map[string]any)
		if validationIssueFromWire(item).Code == wantCode {
			matches = append(matches, item)
		}
	}
	return matches[0]
}

func assertNoValidationIssuePath(output map[string]any, unwantedPath string) {
	GinkgoHelper()

	issues := arrayField(output, "issues")
	paths := []string{}
	for _, issue := range issues {
		Expect(issue).To(BeAssignableToTypeOf(map[string]any{}))
		paths = append(paths, validationIssueFromWire(issue.(map[string]any)).Path)
	}
	Expect(paths).NotTo(ContainElement(unwantedPath))
}

func matchValidationIssueCodesAbsent(absentCodes []wikivalidation.IssueCode) types.GomegaMatcher {
	GinkgoHelper()

	matchers := make([]types.GomegaMatcher, 0, len(absentCodes))
	for _, code := range absentCodes {
		matchers = append(matchers, Not(ContainElement(code)))
	}
	return WithTransform(validationIssueCodes, SatisfyAll(matchers...))
}

func validationIssueCodes(output map[string]any) ([]wikivalidation.IssueCode, error) {
	rawIssues, ok := output["issues"].([]any)
	if !ok {
		return nil, fmt.Errorf("validation output should expose issues as an array")
	}
	codes := make([]wikivalidation.IssueCode, 0, len(rawIssues))
	for _, rawIssue := range rawIssues {
		issue, ok := rawIssue.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("validation issue should be an object")
		}
		payload, err := json.Marshal(issue)
		if err != nil {
			return nil, fmt.Errorf("marshal validation issue: %w", err)
		}
		var decoded validationIssueWire
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return nil, fmt.Errorf("decode validation issue: %w", err)
		}
		codes = append(codes, decoded.Code)
	}
	return codes, nil
}

func validationIssueCodeCount(output map[string]any, wantCode wikivalidation.IssueCode, wantPath string) int {
	GinkgoHelper()

	issues := arrayField(output, "issues")
	count := 0
	for _, issue := range issues {
		Expect(issue).To(BeAssignableToTypeOf(map[string]any{}))
		decoded := validationIssueFromWire(issue.(map[string]any))
		if decoded.Code == wantCode && decoded.Path == wantPath {
			count++
		}
	}
	return count
}

func assertRecentChangesIncludePath(output map[string]any, wantPath string) {
	GinkgoHelper()

	Expect(changedPathsFromChanges(arrayField(output, "recentChanges"))).To(HaveKey(wantPath))
}
