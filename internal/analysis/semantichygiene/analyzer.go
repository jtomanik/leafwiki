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
		(*ast.CallExpr)(nil),
		(*ast.FuncDecl)(nil),
		(*ast.TypeSpec)(nil),
		(*ast.BasicLit)(nil),
	}, func(node ast.Node) {
		switch n := node.(type) {
		case *ast.CallExpr:
			checkStringLeak(ctx, n)
			checkStringConversionLeak(ctx, n)
			checkDirectCast(ctx, n)
		case *ast.FuncDecl:
			checkSignature(ctx, n)
			checkValidatorReturn(ctx, n)
		case *ast.TypeSpec:
			checkStructFields(ctx, n)
		case *ast.BasicLit:
			checkStableLiteral(ctx, n)
			checkLocalizedProseLiteral(ctx, n)
		}
	})
	return nil, nil
}
