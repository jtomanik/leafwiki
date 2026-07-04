package semantichygiene

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const doc = "checks LeafWiki semantic value boundaries for primitive leaks"

var Analyzer = &analysis.Analyzer{
	Name:     "semantichygiene",
	Doc:      doc,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	ctx := newAnalysisContext(pass)
	ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	ins.Preorder([]ast.Node{
		(*ast.AssignStmt)(nil),
		(*ast.CallExpr)(nil),
		(*ast.FuncDecl)(nil),
		(*ast.GoStmt)(nil),
		(*ast.ImportSpec)(nil),
		(*ast.KeyValueExpr)(nil),
		(*ast.TypeSpec)(nil),
		(*ast.UnaryExpr)(nil),
		(*ast.BasicLit)(nil),
	}, func(node ast.Node) {
		switch n := node.(type) {
		case *ast.AssignStmt:
			checkGinkgoGlobalStateAssignment(ctx, n)
			checkGomegaIgnoredSemanticBoolean(ctx, n)
		case *ast.CallExpr:
			checkGinkgoSpecQualityCall(ctx, n)
			checkGinkgoGlobalStateCleanup(ctx, n)
			checkStringLeak(ctx, n)
			checkStringConversionLeak(ctx, n)
			checkDirectCast(ctx, n)
			checkUncheckedConstructorCall(ctx, n)
			checkFixtureSemanticConstructorCall(ctx, n)
			checkMessagePassthroughCall(ctx, n)
			checkGomegaSemanticMatcher(ctx, n)
			checkGomegaAsyncAssertion(ctx, n)
			checkGomegaAsyncCallback(ctx, n)
			checkErrorStringPredicate(ctx, n)
		case *ast.FuncDecl:
			checkSignature(ctx, n)
			checkValidatorReturn(ctx, n)
			checkGomegaAssertionHelperOffset(ctx, n)
			checkGinkgoHelperFirst(ctx, n)
			checkReusableAssertionHelper(ctx, n)
			checkGomegaMatcherFactorySignature(ctx, n)
		case *ast.GoStmt:
			checkGinkgoGoroutineAssertionRecovery(ctx, n)
		case *ast.ImportSpec:
			checkDependencyDirection(ctx, n)
		case *ast.KeyValueExpr:
			checkMessageFieldValue(ctx, n)
		case *ast.TypeSpec:
			checkTypeSpec(ctx, n)
		case *ast.UnaryExpr:
			checkGinkgoBlockingReceive(ctx, n)
		case *ast.BasicLit:
			checkStableLiteral(ctx, n)
			checkLocalizedProseLiteral(ctx, n)
		}
	})
	ctx.finalizeDiagnostics()
	return nil, nil
}
