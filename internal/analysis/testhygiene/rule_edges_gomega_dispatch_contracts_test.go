package testhygiene

import (
	"go/ast"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("testhygiene Gomega policy dispatch contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("reports each Gomega assertion policy family through analyzer-style traversal", func() {
		h := newRuleHarness("/repo/internal/wiki/gomega_policy_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"time"
)

type assertion struct{}
type typedError struct{}
type WorkspaceID string
type SessionToken string
type issueReport struct{ Err error }
type protocolResult struct {
	LastError  string
	Message    string
	MessageID  string
	IsError    bool
	StatusCode int
}
type pageSummary struct {
	Title string
	Path  string
}
type sessionState struct{ ContextToken SessionToken }
type sessionResult struct{ ID string }

func (*typedError) Error() string { return "" }
func Expect(actual any, extra ...any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func (assertion) NotTo(matcher any, extra ...any) {}
func Equal(expected any) any { return nil }
func ContainSubstring(expected string) any { return nil }
func HavePrefix(expected string) any { return nil }
func HaveSuffix(expected string) any { return nil }
func MatchRegexp(expected string) any { return nil }
func MatchError(expected any) any { return nil }
func HaveOccurred() any { return nil }
func Succeed() any { return nil }
func BeNil() any { return nil }
func BeTrue() any { return nil }
func BeEquivalentTo(expected any) any { return nil }
func BeNumerically(op string, expected any) any { return nil }
func BeEmpty() any { return nil }
func Not(matcher any) any { return nil }
func And(matchers ...any) any { return nil }
func HaveField(name string, matcher any) any { return nil }
func HaveKeyWithValue(name string, matcher any) any { return nil }
func MatchFields(options any, fields map[string]any) any { return nil }
func MatchJSON(raw string) any { return nil }
func HaveHTTPBody(body string) any { return nil }
func HaveHTTPHeaderWithValue(name string, value string) any { return nil }
func WithTransform(transform any, matcher any) any { return nil }

func readOne() error { return nil }
func readMany() (string, error) { return "", nil }
func IsControlStatus(status string) bool { return status != "" }
func IsReady() bool { return true }
func FindWorkspace() (WorkspaceID, bool) { return "", false }
func statusLabel(ok bool) string {
	if ok {
		return "ready"
	}
	return "blocked"
}

func TestGomegaPolicies(
	err error,
	target error,
	recorder *httptest.ResponseRecorder,
	response *http.Response,
	request *http.Request,
	status string,
	statusCode int,
	items []string,
	raw map[string]string,
	page pageSummary,
	result protocolResult,
	state sessionState,
	session sessionResult,
) {
	var typedTarget *typedError
	_, _ = regexp.MatchString("warmup", result.Message)
	_ = Equal(0)
	_ = Equal(true)
	_ = statusLabel(IsReady())
	_ = WithTransform(os.IsNotExist, BeTrue())
	_ = WithTransform(func(page pageSummary) []string {
		return []string{page.Title, page.Path}
	}, Equal([]string{"Docs", "/docs"}))
	workspace, _ := FindWorkspace()
	_ = workspace

	Expect(err.Error()).To(ContainSubstring("boom"))
	Expect(err).To(MatchError("missing page"))
	Expect(err).To(MatchError(fmt.Errorf("missing: %w", err)))
	Expect(err).To(Equal(nil))
	Expect(err).To(HaveOccurred())
	Expect(issueReport{Err: err}).To(MatchFields(nil, map[string]any{"Err": HaveOccurred()}))
	Expect(issueReport{Err: err}).To(HaveField("Err", Not(BeNil())))
	Expect(readOne()).NotTo(HaveOccurred())
	Expect(readMany()).To(Succeed())
	Expect(strings.Contains(result.Message, "boom")).To(Equal(true))
	Expect(result.Message).To(ContainSubstring("boom"))
	Expect(result.LastError).NotTo(Equal(""))
	Expect(strings.HasPrefix(result.Message, "boom")).To(Equal(true))
	Expect(strings.HasSuffix(result.Message, "boom")).To(Equal(true))
	Expect(regexp.MatchString("boom", result.Message)).To(Equal(true))
	Expect(errors.Is(err, target)).To(Equal(true))
	Expect(errors.As(err, &typedTarget)).To(Equal(true))
	Expect(IsControlStatus(status)).To(Equal(true))
	Expect(statusCode).To(Equal(500))
	Expect(result).To(HaveField("StatusCode", Equal(http.StatusInternalServerError)))
	Expect(os.IsNotExist(err)).To(Equal(true))
	Expect(len(items)).To(Equal(0))
	Expect(len(items) == 0).To(Equal(true))
	Expect(true).To(BeTrue())
	_, ok := raw["value"]
	Expect(ok).To(BeTrue())
	ready := IsReady()
	Expect(ready).To(BeTrue())
	Expect(raw["code"]).To(Equal("ok"))
	Expect(recorder.Code).To(Equal(200))
	Expect(recorder.Body.String()).To(ContainSubstring("ok"))
	Expect(response).To(HaveHTTPBody("ok"))
	Expect(response).To(HaveHTTPBody("id"))
	Expect(response.Header.Get("X-LeafWiki")).To(Equal("workspace"))
	Expect(request).To(HaveHTTPHeaderWithValue("X-LeafWiki", "workspace"))
	Expect(statusCode).To(BeEquivalentTo(200))
	now := time.Now()
	Expect(now).To(Equal(now))
	Expect(items).NotTo(BeEmpty())
	Expect(state.ContextToken).NotTo(Equal(""))
	Expect(page.Title).To(Equal("Docs"))
	Expect(page.Path).To(Equal("/docs"))
	Expect([]string{page.Title, page.Path}).To(Equal([]string{"Docs", "/docs"}))
	Expect(items[0]).To(Equal("first"))
	Expect(result.Message).To(Equal("rendered prose"))
	Expect(result).To(HaveField("LastError", Equal("rendered prose")))
	Expect(session).To(HaveField("ID", Equal("raw-session")))
	Expect(result).To(HaveKeyWithValue("messageId", Equal("wiki.tool.failed")))
	Expect(result).To(MatchJSON(`+"`"+`{"messageId":"wiki.tool.failed"}`+"`"+`))
	Expect(result).To(MatchFields(nil, map[string]any{"IsError": BeTrue()}))
	Expect("state").To(Equal(BeTrue()))
}
`)
		for _, call := range callExpressionsInFile(h.file) {
			checkGomegaSemanticMatcher(h.ctx, call)
		}
		for _, assign := range assignmentStatementsInFile(h.file) {
			checkGomegaIgnoredSemanticBoolean(h.ctx, assign)
		}

		Expect(diagnosticRuleIDs(h.diagnosticMessages())).To(ContainElements(
			"semh:gomega.err-error-string",
			"semh:gomega.raw-string-match-error",
			"semh:gomega.error-nil-matcher",
			"semh:gomega.generic-have-occurred",
			"semh:gomega.inline-error-succeed",
			"semh:gomega.multi-return-error-matcher",
			"semh:gomega.strings-contains",
			"semh:gomega.last-error-not-empty",
			"semh:gomega.string-predicate",
			"semh:gomega.regexp-match-string",
			"semh:gomega.errors-is-matcher",
			"semh:gomega.errors-as-matcher",
			"semh:gomega.control-status-matcher",
			"semh:gomega.raw-status-code",
			"semh:gomega.os-is-not-exist-matcher",
			"semh:gomega.len-equal",
			"semh:gomega.binary-boolean",
			"semh:gomega.boolean-literal",
			"semh:gomega.comma-ok-assertion",
			"semh:gomega.ignored-semantic-boolean",
			"semh:gomega.proxy-boolean",
			"semh:gomega.map-index",
			"semh:gomega.http-status",
			"semh:gomega.http-body",
			"semh:gomega.repeated-http-body",
			"semh:gomega.http-header",
			"semh:gomega.numeric-equivalent",
			"semh:gomega.time-equal",
			"semh:gomega.non-empty-collection",
			"semh:gomega.semantic-scalar-not-empty",
			"semh:gomega.equal-empty",
			"semh:gomega.repeated-field-assertions",
			"semh:gomega.positional-composite-assertion",
			"semh:gomega.collection-index-assertion",
			"semh:gomega.structured-error-matcher",
			"semh:gomega.structured-protocol-key",
			"semh:gomega.structured-protocol-payload",
			"semh:gomega.structured-protocol-status",
			"semh:gomega.boolean-literal",
			"semh:gomega.equal-zero",
			"semh:gomega.positional-transform",
			"semh:gomega.matcher-as-value",
		))
	})
})

func callExpressionsInFile(file *ast.File) []*ast.CallExpr {
	var calls []*ast.CallExpr
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok {
			calls = append(calls, call)
		}
		return true
	})
	return calls
}

func assignmentStatementsInFile(file *ast.File) []*ast.AssignStmt {
	var statements []*ast.AssignStmt
	ast.Inspect(file, func(node ast.Node) bool {
		stmt, ok := node.(*ast.AssignStmt)
		if ok {
			statements = append(statements, stmt)
		}
		return true
	})
	return statements
}

func diagnosticRuleIDs(messages []string) []string {
	ids := make([]string, 0, len(messages))
	for _, message := range messages {
		id, _, ok := strings.Cut(message, ": ")
		if ok {
			ids = append(ids, id)
		}
	}
	return ids
}
