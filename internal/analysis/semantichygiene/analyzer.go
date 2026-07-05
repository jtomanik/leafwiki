package semantichygiene

import (
	"go/ast"
	"reflect"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const doc = "checks LeafWiki semantic value boundaries for primitive leaks"

type analyzerResult struct{}

var Analyzer = &analysis.Analyzer{
	Name:       "semantichygiene",
	Doc:        doc,
	Requires:   []*analysis.Analyzer{inspect.Analyzer},
	ResultType: reflect.TypeOf(analyzerResult{}),
	Run:        run,
}

func run(pass *analysis.Pass) (any, error) {
	ctx := newAnalysisContext(pass)
	ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	ins.Preorder([]ast.Node{
		(*ast.CallExpr)(nil),
		(*ast.FuncDecl)(nil),
		(*ast.KeyValueExpr)(nil),
		(*ast.TypeSpec)(nil),
		(*ast.BasicLit)(nil),
	}, func(node ast.Node) {
		switch n := node.(type) {
		case *ast.CallExpr:
			checkStringLeak(ctx, n)
			checkStringConversionLeak(ctx, n)
			checkDirectCast(ctx, n)
			checkUncheckedConstructorCall(ctx, n)
			checkFixtureSemanticConstructorCall(ctx, n)
			checkMessagePassthroughCall(ctx, n)
		case *ast.FuncDecl:
			checkSignature(ctx, n)
			checkValidatorReturn(ctx, n)
		case *ast.KeyValueExpr:
			checkMessageFieldValue(ctx, n)
		case *ast.TypeSpec:
			checkTypeSpec(ctx, n)
		case *ast.BasicLit:
			checkStableLiteral(ctx, n)
			checkLocalizedProseLiteral(ctx, n)
			checkTestRawSemanticLiteral(ctx, n)
		}
	})
	ctx.finalizeDiagnostics()
	return analyzerResult{}, nil
}
