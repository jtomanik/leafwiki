package semantichygiene

import (
	"go/ast"
	"go/token"
	"go/types"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/tools/go/analysis"
)

var _ = ginkgo.Describe("semantichygiene string leak contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("reports semantic string escapes across ordinary expression shapes", func() {
		h := newRuleHarness("/repo/internal/wiki/page.go", "github.com/perber/wiki/internal/wiki", `package wiki

type WorkspaceID string
type Plain string

func (id WorkspaceID) String() string { return string(id) }
func (p Plain) String() string { return string(p) }
func acceptRaw(raw string) {}

func leak(id WorkspaceID, plain Plain, raw map[string]string) string {
	local := id.String()
	_ = local
	raw[id.String()] = "value"
	_ = map[string]string{id.String(): "value"}
	_ = struct{ Name string }{Name: id.String()}
	_ = id.String() == "workspace"
	_ = id.String() == ""
	acceptRaw(id.String())
	_ = plain.String()
	return id.String()
}

func castLeak(id WorkspaceID) string {
	raw := string(id)
	_ = raw
	return string(id)
}
`)
		for _, call := range h.findCalls("String") {
			checkStringLeak(h.ctx, call)
		}
		for _, call := range h.findCalls("string") {
			checkStringConversionLeak(h.ctx, call)
		}

		Expect(h.diagnosticMessages()).To(ContainElements(
			ContainSubstring("semantic value WorkspaceID converted to string into local local"),
			ContainSubstring("semantic value WorkspaceID converted to string into local map index"),
			ContainSubstring("semantic value WorkspaceID converted to string into local map key"),
			ContainSubstring("semantic value WorkspaceID converted to string for semantic field Name"),
			ContainSubstring("semantic value WorkspaceID converted to string for comparison"),
			ContainSubstring("semantic value WorkspaceID converted to string before internal call acceptRaw"),
			ContainSubstring("semantic value WorkspaceID returned as string from internal function leak"),
			ContainSubstring("semantic value WorkspaceID converted to string into local raw"),
			ContainSubstring("semantic value WorkspaceID returned as string from internal function castLeak"),
		))
		Expect(h.diagnosticMessages()).NotTo(ContainElement(ContainSubstring("Plain")))
	})

	ginkgo.It("keeps string boundary helpers narrow around unsupported AST shapes", func() {
		h := newRuleHarness("/repo/internal/wiki/page.go", "github.com/perber/wiki/internal/wiki", `package wiki

type WorkspaceID string

func acceptRaw(raw string) {}
func leak(id WorkspaceID) {
	acceptRaw(id.String())
}
func (id WorkspaceID) String() string { return string(id) }
`)
		call := h.findCall("String")
		emptyParents := &analysisContext{parents: map[ast.Node]ast.Node{}, pass: h.ctx.pass}

		Expect(observePolicyHelperDecisions(
			isAllowedTestStringCall(h.ctx, h.findCall("acceptRaw")),
			isAllowedTerminalCallBoundary(emptyParents, h.findCall("acceptRaw")),
			isAllowedSemanticConstructorTransform(h.ctx, call, "WorkspaceID"),
			isAllowedAdapterStringReturn(h.ctx, call),
			isAllowedErrorInterfaceStringReturn(h.ctx, call),
			isAllowedAdapterStringKeyValue(h.ctx, call),
		)).To(Equal([]policyHelperDecision{
			policyHelperRejected,
			policyHelperRejected,
			policyHelperRejected,
			policyHelperRejected,
			policyHelperRejected,
			policyHelperRejected,
		}))
	})

	ginkgo.It("allows explicit terminal and test string boundaries without hiding internal leaks", func() {
		production := newRuleHarness("/repo/internal/wiki/page.go", "github.com/perber/wiki/internal/wiki", `package wiki

import "fmt"

type WorkspaceID string

func (id WorkspaceID) String() string { return string(id) }

func logID(id WorkspaceID) error {
	fmt.Println("workspace " + (id.String()))
	err := fmt.Errorf("%s", id.String())
	var wrapped error = fmt.Errorf("%s", id.String())
	_ = wrapped
	return err
}
`)
		for _, call := range production.findCalls("String") {
			checkStringLeak(production.ctx, call)
		}

		Expect(production.diagnosticMessages()).To(BeEmpty())

		testFile := newRuleHarness("/repo/internal/wiki/page_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

import "strings"

type WorkspaceID string
type testLogger interface { Log(args ...any) }

func (id WorkspaceID) String() string { return string(id) }
func assertWorkspace(raw string) {}

func exerciseTestBoundary(t testLogger, id WorkspaceID) {
	_ = strings.TrimSpace(id.String())
	assertWorkspace(id.String())
	t.Log(id.String())
}
`)
		for _, call := range testFile.findCalls("String") {
			checkStringLeak(testFile.ctx, call)
		}

		Expect(testFile.diagnosticMessages()).To(BeEmpty())
	})

	ginkgo.It("allows commit hashes to cross the go-git hash boundary", func() {
		plumbingPkg := types.NewPackage("github.com/go-git/go-git/v6/plumbing", "plumbing")
		newHashName := &ast.Ident{Name: "NewHash"}
		otherName := &ast.Ident{Name: "Other"}
		newHashCall := &ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent("plumbing"), Sel: newHashName}}
		otherCall := &ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent("plumbing"), Sel: otherName}}
		returnStatement := &ast.ReturnStmt{}
		ctx := &analysisContext{
			pass: &analysis.Pass{TypesInfo: &types.Info{Uses: map[*ast.Ident]types.Object{
				newHashName: types.NewFunc(token.NoPos, plumbingPkg, "NewHash", nil),
				otherName:   types.NewFunc(token.NoPos, plumbingPkg, "Other", nil),
			}}},
			parents: map[ast.Node]ast.Node{
				newHashCall: returnStatement,
				otherCall:   returnStatement,
			},
		}

		Expect(observePolicyHelperDecisions(
			isAllowedExternalSemanticStringBoundary(ctx, newHashCall, "CommitHash"),
			isAllowedExternalSemanticStringBoundary(ctx, otherCall, "CommitHash"),
			isAllowedExternalSemanticStringBoundary(ctx, newHashCall, "WorkspaceID"),
		)).To(Equal([]policyHelperDecision{
			policyHelperAccepted,
			policyHelperRejected,
			policyHelperRejected,
		}))
	})

	ginkgo.It("allows gin route parameter values only in test composite fixtures", func() {
		fileSet := token.NewFileSet()
		testFile := fileSet.AddFile("/repo/internal/wiki/page_test.go", -1, 100)
		productionFile := fileSet.AddFile("/repo/internal/wiki/page.go", -1, 100)
		ginPackage := types.NewPackage("github.com/gin-gonic/gin", "gin")
		ginParam := types.NewNamed(types.NewTypeName(token.NoPos, ginPackage, "Param", nil), types.NewStruct(nil, nil), nil)
		acceptedComposite := &ast.CompositeLit{Type: ast.NewIdent("Param")}
		acceptedComposite.Type.(*ast.Ident).NamePos = testFile.Pos(1)
		accepted := &ast.KeyValueExpr{
			Key:   &ast.Ident{Name: "Value", NamePos: testFile.Pos(2)},
			Value: &ast.BasicLit{Kind: token.STRING, Value: `"slug"`, ValuePos: testFile.Pos(3)},
		}
		wrongKey := &ast.KeyValueExpr{
			Key:   &ast.Ident{Name: "Key", NamePos: testFile.Pos(4)},
			Value: &ast.BasicLit{Kind: token.STRING, Value: `"slug"`, ValuePos: testFile.Pos(5)},
		}
		productionComposite := &ast.CompositeLit{Type: ast.NewIdent("Param")}
		productionComposite.Type.(*ast.Ident).NamePos = productionFile.Pos(1)
		production := &ast.KeyValueExpr{
			Key:   &ast.Ident{Name: "Value", NamePos: productionFile.Pos(2)},
			Value: &ast.BasicLit{Kind: token.STRING, Value: `"slug"`, ValuePos: productionFile.Pos(3)},
		}
		acceptedComposite.Elts = []ast.Expr{accepted, wrongKey}
		productionComposite.Elts = []ast.Expr{production}
		ctx := &analysisContext{
			pass: &analysis.Pass{Fset: fileSet, TypesInfo: &types.Info{Types: map[ast.Expr]types.TypeAndValue{
				acceptedComposite:   {Type: ginParam},
				productionComposite: {Type: ginParam},
			}}},
			parents: map[ast.Node]ast.Node{
				accepted:   acceptedComposite,
				wrongKey:   acceptedComposite,
				production: productionComposite,
			},
		}

		Expect(observePolicyHelperDecisions(
			isAllowedGinRouteParamKeyValue(ctx, accepted),
			isAllowedGinRouteParamKeyValue(ctx, wrongKey),
			isAllowedGinRouteParamKeyValue(ctx, production),
		)).To(Equal([]policyHelperDecision{
			policyHelperAccepted,
			policyHelperRejected,
			policyHelperRejected,
		}))
	})
})
