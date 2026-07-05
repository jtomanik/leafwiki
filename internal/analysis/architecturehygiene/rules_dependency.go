package architecturehygiene

import (
	"go/ast"
	"strconv"
	"strings"

	"github.com/perber/wiki/internal/analysis/checkerpolicy"
)

type dependencyDirectionRule struct {
	importerPrefix        string
	forbiddenImportPrefix string
	diagnostic            string
	rule                  checkerpolicy.RuleID
}

var dependencyDirectionRules = []dependencyDirectionRule{
	{
		importerPrefix:        "github.com/perber/wiki/e2e-proxy",
		forbiddenImportPrefix: "github.com/perber/wiki/internal",
		diagnostic:            dependencyDirectionDiagnostic(),
		rule:                  ruleDependencyE2EProxyInternalImport,
	},
	{
		importerPrefix:        "github.com/perber/wiki/internal/core",
		forbiddenImportPrefix: "github.com/perber/wiki/internal/wiki",
		diagnostic:            "Core domain code must not depend on HTTP/wiki route adapters.",
		rule:                  ruleArchitectureCoreToWiki,
	},
	{
		importerPrefix:        "github.com/perber/wiki/internal/core",
		forbiddenImportPrefix: "github.com/perber/wiki/internal/http",
		diagnostic:            "Core domain code must stay transport-agnostic.",
		rule:                  ruleArchitectureCoreToHTTP,
	},
	{
		importerPrefix:        "github.com/perber/wiki/internal/core",
		forbiddenImportPrefix: "github.com/perber/wiki/internal/projectdaemon",
		diagnostic:            "Core domain code must not know daemon/runtime orchestration.",
		rule:                  ruleArchitectureCoreToProjectDaemon,
	},
	{
		importerPrefix:        "github.com/perber/wiki/internal/wiki",
		forbiddenImportPrefix: "github.com/perber/wiki/cmd/leafwiki",
		diagnostic:            "Route/domain assembly must not depend on CLI startup code.",
		rule:                  ruleArchitectureWikiToCmdLeafwiki,
	},
	{
		importerPrefix:        "github.com/perber/wiki/internal/projectdaemon",
		forbiddenImportPrefix: "github.com/perber/wiki/cmd/leafwiki",
		diagnostic:            "Daemon primitives must not depend on the executable entrypoint.",
		rule:                  ruleArchitectureProjectDaemonToCmdLeafwiki,
	},
	{
		importerPrefix:        "github.com/perber/wiki/internal/workspaced",
		forbiddenImportPrefix: "github.com/perber/wiki/internal/frontd",
		diagnostic:            "workspaced should expose workspace services without depending on the frontend proxy.",
		rule:                  ruleArchitectureWorkspacedToFrontd,
	},
}

func checkDependencyDirection(ctx *checkerpolicy.Context, spec *ast.ImportSpec) {
	if ctx.Pass().Pkg == nil {
		return
	}
	if spec.Path == nil {
		return
	}
	importPath, err := strconv.Unquote(spec.Path.Value)
	if err != nil {
		return
	}

	importerPath := ctx.Pass().Pkg.Path()
	for _, rule := range dependencyDirectionRules {
		if importPathWithin(importerPath, rule.importerPrefix) && importPathWithin(importPath, rule.forbiddenImportPrefix) {
			ctx.Report(rule.rule, spec.Path, rule.diagnostic)
		}
	}
}

func dependencyDirectionDiagnostic() string {
	return "e2e-proxy must not import LeafWiki internal packages; assert protocol semantics or define local black-box test helpers"
}

func importPathWithin(importPath string, prefix string) bool {
	prefix = strings.TrimSuffix(prefix, "/")
	return importPath == prefix || strings.HasPrefix(importPath, prefix+"/")
}
