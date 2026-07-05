package semantichygiene

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go/ast"
	"go/token"
	"go/types"
	"golang.org/x/tools/go/analysis"
)

var _ = ginkgo.Describe("semantichygiene rule branches", ginkgo.Label("unit"), func() {
	ginkgo.It("reports LastError assertions that only prove non-empty rendered text", func() {
		h := newRuleHarness("/repo/internal/workspacesync/service_test.go", "github.com/perber/wiki/internal/workspacesync", `package workspacesync

type SyncStatus struct {
	LastError string
}

type GomegaMatcher interface{}
type assertion struct{}
type matcher interface {
	Match(any) (bool, error)
}
type matcherBuilder struct{}
type gcustomPackage struct{}

func Expect(actual any) assertion { return assertion{} }
func (assertion) NotTo(matcher any, extras ...any) {}
func BeEmpty() any { return nil }
var gcustom gcustomPackage
func (gcustomPackage) MakeMatcher(fn any) matcherBuilder { return matcherBuilder{} }
func (matcherBuilder) WithMessage(message string) GomegaMatcher { return nil }

func TestSyncStatus() {
	status := SyncStatus{LastError: "writeback failed"}
	Expect(status.LastError).NotTo(BeEmpty())
}

func matchWatcherFactoryFailureStatus() GomegaMatcher {
	return gcustom.MakeMatcher(func(status SyncStatus) (bool, error) {
		return status.LastError != "", nil
	}).WithMessage("report watcher factory failure status")
}

func matchStoppedWatcherError(lastError matcher) GomegaMatcher {
	return gcustom.MakeMatcher(func(status SyncStatus) (bool, error) {
		return lastError.Match(status.LastError)
	}).WithMessage("report stopped watcher error")
}
`)

		checkGomegaSemanticMatcher(h.ctx, h.findCall("NotTo"))
		checkGomegaMatcherFactorySignature(h.ctx, h.findFunc("matchWatcherFactoryFailureStatus"))
		checkGomegaMatcherFactorySignature(h.ctx, h.findFunc("matchStoppedWatcherError"))

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.last-error-not-empty: assert specific LastError semantics instead of only checking for non-empty rendered text",
			"semh:gomega.last-error-not-empty: assert specific LastError semantics instead of only checking for non-empty rendered text",
			"semh:gomega.last-error-not-empty: assert specific LastError semantics instead of only checking for non-empty rendered text",
		))
	})

	ginkgo.It("ignores string leak and conversion early exit cases", func() {
		h := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
type PlainID string
func (id PlainID) String() string { return string(id) }
func use(id PlainID, raw int) {
	_ = id.String()
	_ = string(raw)
}
`)
		checkStringLeak(h.ctx, h.findCall("String"))
		checkStringConversionLeak(h.ctx, h.findCall("string"))
		Expect(h.diagnostics).To(BeEmpty())

		allowed := newRuleHarness("/repo/vendor/example/page.go", "example.com/p", `package p
type PageID string
func (id PageID) String() string { return string(id) }
func use(id PageID) { _ = id.String() }
`)
		checkStringLeak(allowed.ctx, allowed.findCall("String"))
		Expect(allowed.diagnostics).To(BeEmpty())
	})

	ginkgo.It("reports string assignment key-value and return diagnostics", func() {
		h := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
type PageID string
func (id PageID) String() string { return string(id) }
type domainRecord struct { PageID string }
func use(id PageID, values map[string]string) string {
	values[id.String()] = "seen"
	var local string
	local = id.String()
	record := domainRecord{PageID: id.String()}
	_, _ = local, record
	return id.String()
}
`)
		for _, call := range h.findCalls("String") {
			if call.Pos() == h.findFunc("String").Body.List[0].(*ast.ReturnStmt).Results[0].Pos() {
				continue
			}
			checkStringLeak(h.ctx, call)
		}
		Expect(h.diagnosticMessages()).To(ContainElements(
			ContainSubstring("map index"),
			ContainSubstring("local local"),
			ContainSubstring("semantic field PageID"),
			ContainSubstring("returned as string"),
		))
	})

	ginkgo.It("reports string value specs and direct assignment helper edges", func() {
		h := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
type PageID string
func (id PageID) String() string { return string(id) }
func use(id PageID) {
	var value = id.String()
	_ = value
}
`)
		checkStringLeak(h.ctx, h.findCall("String"))
		Expect(h.diagnosticMessages()).To(ContainElement(ContainSubstring("local value")))

		vendor := newRuleHarness("/repo/vendor/example/page.go", "example.com/p", `package p
type PageID string
func (id PageID) String() string { return string(id) }
func use(id PageID) {
	var value = id.String()
	_ = value
}
`)
		checkStringLeak(vendor.ctx, vendor.findCall("String"))
		Expect(vendor.diagnostics).To(BeEmpty())

		expr := &ast.Ident{Name: "expr"}
		ctx := h.ctx
		ctx.pass.Report = func(diagnostic analysis.Diagnostic) {
			h.diagnostics = append(h.diagnostics, diagnostic)
		}
		checkStringAssignment(ctx, expr, "PageID", &ast.AssignStmt{
			Lhs: []ast.Expr{&ast.Ident{Name: "local"}},
			Rhs: []ast.Expr{&ast.Ident{Name: "other"}},
		})
		checkStringAssignment(ctx, expr, "PageID", &ast.AssignStmt{
			Lhs: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `"field"`}},
			Rhs: []ast.Expr{expr},
		})
		checkStringAssignment(ctx, expr, "PageID", &ast.AssignStmt{
			Lhs: []ast.Expr{&ast.IndexExpr{X: expr, Index: &ast.BasicLit{Kind: token.STRING, Value: `"key"`}}},
			Rhs: []ast.Expr{&ast.Ident{Name: "other"}},
		})
		checkStringKeyValue(ctx, expr, "PageID", &ast.KeyValueExpr{
			Key:   &ast.BasicLit{Kind: token.STRING, Value: `"`},
			Value: expr,
		})
		checkStringValueSpec(vendor.ctx, vendor.findCall("String"), "PageID", &ast.ValueSpec{})
		checkStringValueSpec(ctx, expr, "PageID", &ast.ValueSpec{
			Values: []ast.Expr{&ast.Ident{Name: "other"}, expr},
			Names:  []*ast.Ident{{Name: "first"}},
		})
		checkStringAssignment(vendor.ctx, vendor.findCall("String"), "PageID", &ast.AssignStmt{})
		Expect(h.diagnosticMessages()).To(ContainElement(ContainSubstring("map index")))

		vendorAssignment := newRuleHarness("/repo/vendor/example/page.go", "example.com/p", `package p
type PageID string
func (id PageID) String() string { return string(id) }
func use(id PageID) {
	var value string
	value = id.String()
	_ = value
}
`)
		checkStringLeak(vendorAssignment.ctx, vendorAssignment.findCall("String"))
		Expect(vendorAssignment.diagnostics).To(BeEmpty())

		vendorReturn := newRuleHarness("/repo/vendor/example/page.go", "example.com/p", `package p
type PageID string
func (id PageID) String() string { return string(id) }
func use(id PageID) string {
	return id.String()
}
`)
		checkStringReturn(vendorReturn.ctx, vendorReturn.findCall("String"), "PageID")
		Expect(vendorReturn.diagnostics).To(BeEmpty())

		ignoredExpression := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
type PageID string
func (id PageID) String() string { return string(id) }
func use(id PageID) {
	id.String()
}
`)
		checkStringLeak(ignoredExpression.ctx, ignoredExpression.findCall("String"))
		Expect(ignoredExpression.diagnostics).To(BeEmpty())
	})

	ginkgo.It("recognizes terminal string call boundaries", func() {
		h := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
import (
	"fmt"
	"log"
	"log/slog"
	"net/url"
)
type PageID string
func (id PageID) String() string { return string(id) }
func use(id PageID) {
	fmt.Println(id.String())
	log.Printf("%s", id.String())
	slog.Info("page", "id", id.String())
	url.PathEscape(id.String())
				}
	`)
		for _, name := range []string{"Println", "Printf", "Info", "PathEscape"} {
			Expect(decisionFor(isAllowedTerminalStringCall(h.ctx, h.findCall(name)))).To(Equal(ruleBranchAccepted))
			Expect(decisionFor(isAllowedTerminalCallBoundary(h.ctx, h.findCall(name)))).To(Equal(ruleBranchAccepted))
		}

		gitHashBoundary := newRuleHarness("/repo/internal/workspacesync/gitrevisions/semantic_types.go", "github.com/perber/wiki/internal/workspacesync/gitrevisions", `package gitrevisions
type CommitHash string
type Hash struct{}
type plumbingPackage struct{}
func (plumbingPackage) NewHash(string) Hash { return Hash{} }
var plumbing plumbingPackage
func (hash CommitHash) String() string { return string(hash) }
func PlumbingHashFromCommitHash(hash CommitHash) Hash {
	return plumbing.NewHash(hash.String())
}
`)
		newHash := gitHashBoundary.findCall("NewHash").Fun.(*ast.SelectorExpr).Sel
		gitHashBoundary.ctx.pass.TypesInfo.Uses[newHash] = types.NewFunc(token.NoPos, types.NewPackage("github.com/go-git/go-git/v6/plumbing", "plumbing"), "NewHash", nil)
		checkStringLeak(gitHashBoundary.ctx, gitHashBoundary.findCall("String"))
		Expect(gitHashBoundary.diagnostics).To(BeEmpty())

		nested := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
import "fmt"
type PageID string
func (id PageID) String() string { return string(id) }
func use(id PageID) string { return wrap(fmt.Sprint(id.String())) }
func wrap(value string) string { return value }
`)
		Expect(decisionFor(isAllowedTerminalStringCall(nested.ctx, nested.findCall("Sprint")))).To(Equal(ruleBranchRejected))
		checkStringLeak(nested.ctx, nested.findCall("String"))
		Expect(nested.diagnosticMessages()).To(ContainElement(ContainSubstring("before internal call Sprint")))

		otherTerminals := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
import (
	"log"
	"log/slog"
)
func use() {
	log.Output(1, "x")
	slog.SetDefault(nil)
}
`)
		Expect(decisionFor(isAllowedTerminalStringCall(otherTerminals.ctx, otherTerminals.findCall("Output")))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(isAllowedTerminalStringCall(otherTerminals.ctx, otherTerminals.findCall("SetDefault")))).To(Equal(ruleBranchRejected))

		stmt := &ast.CallExpr{Fun: &ast.Ident{Name: "terminal"}}
		binary := &ast.BinaryExpr{Op: token.LSS}
		manualCtx := &analysisContext{parents: map[ast.Node]ast.Node{stmt: binary}}
		Expect(decisionFor(isAllowedTerminalCallBoundary(manualCtx, stmt))).To(Equal(ruleBranchRejected))
		manualCtx.parents[stmt] = &ast.ParenExpr{}
		manualCtx.parents[manualCtx.parents[stmt]] = &ast.ExprStmt{}
		Expect(decisionFor(isAllowedTerminalCallBoundary(manualCtx, stmt))).To(Equal(ruleBranchAccepted))
		manualCtx.parents = map[ast.Node]ast.Node{stmt: &ast.BinaryExpr{Op: token.ADD}}
		manualCtx.parents[manualCtx.parents[stmt]] = &ast.ReturnStmt{}
		Expect(decisionFor(isAllowedTerminalCallBoundary(manualCtx, stmt))).To(Equal(ruleBranchAccepted))
		manualCtx.parents[stmt] = &ast.CallExpr{}
		Expect(decisionFor(isAllowedTerminalCallBoundary(manualCtx, stmt))).To(Equal(ruleBranchRejected))
		manualCtx.parents[stmt] = &ast.FuncDecl{}
		Expect(decisionFor(isAllowedTerminalCallBoundary(manualCtx, stmt))).To(Equal(ruleBranchRejected))
		manualCtx.parents[stmt] = &ast.IfStmt{}
		Expect(decisionFor(isAllowedTerminalCallBoundary(manualCtx, stmt))).To(Equal(ruleBranchRejected))
		manualCtx.pass = h.ctx.pass
		manualCtx.parents[stmt] = &ast.KeyValueExpr{}
		Expect(decisionFor(isAllowedTerminalCallBoundary(manualCtx, stmt))).To(Equal(ruleBranchRejected))
		manualCtx.parents = map[ast.Node]ast.Node{}
		Expect(decisionFor(isAllowedTerminalCallBoundary(manualCtx, stmt))).To(Equal(ruleBranchRejected))

		testCalls := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p
import (
	"net/http"
	"path/filepath"
	"testing"
)
func TestCalls(t *testing.T) {
	t.Helper()
	t.Cleanup(func(){})
	_ = filepath.Join("a", "b")
	_, _ = http.NewRequest("GET", "/", nil)
	_ = append([]string{}, "x")
	assertEqual("x")
	requireEqual("x")
}
func assertEqual(value string) {}
func requireEqual(value string) {}
`)
		Expect(decisionFor(isAllowedTestStringCall(testCalls.ctx, testCalls.findCall("Join")))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isAllowedTestStringCall(testCalls.ctx, testCalls.findCall("NewRequest")))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isAllowedTestStringCall(testCalls.ctx, testCalls.findCall("append")))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isAllowedTestStringCall(testCalls.ctx, testCalls.findCall("assertEqual")))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isAllowedTestStringCall(testCalls.ctx, testCalls.findCall("requireEqual")))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isAllowedTestAssertionCall(testCalls.findCall("Cleanup")))).To(Equal(ruleBranchRejected))

		comparison := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p
type PageID string
func (id PageID) String() string { return string(id) }
func compare(id PageID, raw string) bool {
	return raw == id.String()
}
`)
		var binaryExpr *ast.BinaryExpr
		ast.Inspect(comparison.file, func(node ast.Node) bool {
			if binaryExpr != nil {
				return false
			}
			if expr, ok := node.(*ast.BinaryExpr); ok {
				binaryExpr = expr
				return false
			}
			return true
		})
		Expect(binaryExpr).NotTo(BeNil())
		Expect(decisionFor(isAllowedSerializedTestComparison(comparison.ctx, comparison.findCall("String"), binaryExpr))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(isAllowedSerializedTestComparison(comparison.ctx, &ast.BasicLit{Kind: token.STRING, Value: `"other"`, ValuePos: comparison.file.Package}, binaryExpr))).To(Equal(ruleBranchRejected))

		transform := newRuleHarness("/repo/internal/analysis/semantichygiene/testdata/semanticcases/message_constructor.go", "github.com/perber/wiki/internal/analysis/semantichygiene/testdata/semanticcases", `package semanticcases
import "strings"
type ErrorCode string
type MessageID string
func MessageIDForCode(code ErrorCode) MessageID {
	_ = strings.Contains(string(code), "x")
	return ""
}
`)
		Expect(decisionFor(isAllowedSemanticConstructorTransform(transform.ctx, transform.findCall("Contains"), "ErrorCode"))).To(Equal(ruleBranchRejected))

		adapterReturn := newRuleHarness("/repo/internal/wiki/import_adapter.go", "example.com/p", `package p
type PageID string
func (id PageID) String() string { return string(id) }
func Other(id PageID) string { return id.String() }
`)
		Expect(decisionFor(isAllowedAdapterStringReturn(adapterReturn.ctx, adapterReturn.findCall("String")))).To(Equal(ruleBranchRejected))
	})
})
