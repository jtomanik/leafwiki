package semantichygiene

import (
	"go/ast"
	"go/token"
	"path/filepath"

	"github.com/perber/wiki/internal/analysis/checkerpolicy"
	"golang.org/x/tools/go/analysis"
)

type semanticDiagnostic struct {
	rule    ruleID
	pos     token.Pos
	node    ast.Node
	message string
}

type analysisContext struct {
	pass        *analysis.Pass
	policy      *checkerpolicy.Context
	parents     map[ast.Node]ast.Node
	diagnostics []semanticDiagnostic
}

func newAnalysisContext(pass *analysis.Pass) *analysisContext {
	return &analysisContext{
		pass:    pass,
		policy:  checkerpolicy.NewContext(pass, semanticRuleSet(), false),
		parents: buildParentMap(pass.Files),
	}
}

func buildParentMap(files []*ast.File) map[ast.Node]ast.Node {
	parents := map[ast.Node]ast.Node{}
	for _, file := range files {
		var stack []ast.Node
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				stack = stack[:len(stack)-1]
				return false
			}
			if len(stack) > 0 {
				parents[node] = stack[len(stack)-1]
			}
			stack = append(stack, node)
			return true
		})
	}
	return parents
}

func (ctx *analysisContext) parent(node ast.Node) ast.Node {
	return ctx.parents[node]
}

func (ctx *analysisContext) report(rule ruleID, node ast.Node, message string) {
	ctx.diagnostics = append(ctx.diagnostics, semanticDiagnostic{
		rule:    rule,
		pos:     node.Pos(),
		node:    node,
		message: message,
	})
}

func (ctx *analysisContext) finalizeDiagnostics() {
	for _, diagnostic := range ctx.diagnostics {
		ctx.policy.Report(checkerpolicy.RuleID(diagnostic.rule), diagnostic.node, diagnostic.message)
	}
	ctx.diagnostics = nil
	ctx.policy.FinalizeDiagnostics()
}

func formatDiagnosticMessage(diagnostic semanticDiagnostic) string {
	metadata, ok := metadataForRule(diagnostic.rule)
	if !ok {
		return "semh:" + string(diagnostic.rule) + ": " + diagnostic.message
	}
	return "semh:" + metadata.messagePrefix + ": " + diagnostic.message
}

func (ctx *analysisContext) filename(pos token.Pos) string {
	return filepath.ToSlash(ctx.pass.Fset.Position(pos).Filename)
}
