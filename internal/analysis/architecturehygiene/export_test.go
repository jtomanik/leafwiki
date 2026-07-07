package architecturehygiene

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/perber/wiki/internal/analysis/checkerpolicy"
	"golang.org/x/tools/go/analysis"
)

func DependencyDirectionDiagnosticsForTest(importerPath string, importLiteral *string) []string {
	var pkg *types.Package
	if importerPath != "" {
		pkg = types.NewPackage(importerPath, "fixture")
	}
	var reported []string
	pass := &analysis.Pass{
		Fset: token.NewFileSet(),
		Pkg:  pkg,
		Report: func(diagnostic analysis.Diagnostic) {
			reported = append(reported, diagnostic.Message)
		},
	}
	ctx := checkerpolicy.NewContext(pass, ruleSet, false)
	spec := &ast.ImportSpec{}
	if importLiteral != nil {
		spec.Path = &ast.BasicLit{Kind: token.STRING, Value: *importLiteral}
	}
	checkDependencyDirection(ctx, spec)
	ctx.FinalizeDiagnostics()
	return reported
}
