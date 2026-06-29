package semantichygiene

import (
	"go/ast"
	"strconv"
	"strings"
)

type dependencyDirectionRule struct {
	importerPath           string
	forbiddenImportPrefix string
	diagnostic            string
}

var dependencyDirectionRules = []dependencyDirectionRule{
	{
		importerPath:           "github.com/perber/wiki/e2e-proxy",
		forbiddenImportPrefix: "github.com/perber/wiki/internal/",
		diagnostic:            dependencyDirectionDiagnostic(),
	},
}

func checkDependencyDirection(ctx *analysisContext, spec *ast.ImportSpec) {
	if ctx.pass.Pkg == nil {
		return
	}
	if spec.Path == nil {
		return
	}
	importPath, err := strconv.Unquote(spec.Path.Value)
	if err != nil {
		return
	}

	importerPath := ctx.pass.Pkg.Path()
	for _, rule := range dependencyDirectionRules {
		if importerPath == rule.importerPath && strings.HasPrefix(importPath, rule.forbiddenImportPrefix) {
			ctx.pass.Reportf(spec.Path.Pos(), "%s", rule.diagnostic)
		}
	}
}
