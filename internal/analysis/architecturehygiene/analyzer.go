package architecturehygiene

import (
	"go/ast"
	"reflect"

	"github.com/perber/wiki/internal/analysis/checkerpolicy"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const doc = "checks LeafWiki architecture boundaries"

type analyzerResult struct{}

var Analyzer = &analysis.Analyzer{
	Name:       "architecturehygiene",
	Doc:        doc,
	Requires:   []*analysis.Analyzer{inspect.Analyzer},
	ResultType: reflect.TypeOf(analyzerResult{}),
	Run:        run,
}

func run(pass *analysis.Pass) (any, error) {
	ctx := checkerpolicy.NewContext(pass, ruleSet, false)
	ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	ins.Preorder([]ast.Node{
		(*ast.ImportSpec)(nil),
	}, func(node ast.Node) {
		checkDependencyDirection(ctx, node.(*ast.ImportSpec))
	})
	ctx.FinalizeDiagnostics()
	return analyzerResult{}, nil
}
