package testhygiene

import (
	"go/ast"
	"reflect"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const doc = "checks LeafWiki test semantics and BDD hygiene"

type analyzerResult struct{}

var Analyzer = &analysis.Analyzer{
	Name:       "testhygiene",
	Doc:        doc,
	Requires:   []*analysis.Analyzer{inspect.Analyzer},
	ResultType: reflect.TypeOf(analyzerResult{}),
	Run:        run,
}

func run(pass *analysis.Pass) (any, error) {
	ctx := newAnalysisContext(pass)
	ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	ins.Preorder([]ast.Node{
		(*ast.AssignStmt)(nil),
		(*ast.CallExpr)(nil),
		(*ast.FuncDecl)(nil),
		(*ast.GoStmt)(nil),
		(*ast.UnaryExpr)(nil),
	}, func(node ast.Node) {
		switch n := node.(type) {
		case *ast.AssignStmt:
			checkGinkgoGlobalStateAssignment(ctx, n)
			checkGomegaIgnoredSemanticBoolean(ctx, n)
		case *ast.CallExpr:
			checkGinkgoSpecQualityCall(ctx, n)
			checkGinkgoGlobalStateCleanup(ctx, n)
			checkGomegaSemanticMatcher(ctx, n)
			checkGomegaAsyncAssertion(ctx, n)
			checkGomegaAsyncCallback(ctx, n)
			checkErrorStringPredicate(ctx, n)
		case *ast.FuncDecl:
			checkGomegaAssertionHelperOffset(ctx, n)
			checkGinkgoHelperFirst(ctx, n)
			checkReusableAssertionHelper(ctx, n)
			checkGomegaMatcherFactorySignature(ctx, n)
		case *ast.GoStmt:
			checkGinkgoGoroutineAssertionRecovery(ctx, n)
		case *ast.UnaryExpr:
			checkGinkgoBlockingReceive(ctx, n)
		}
	})
	checkGinkgoMissingTaxonomyLabels(ctx)
	ctx.finalizeDiagnostics()
	return analyzerResult{}, nil
}
