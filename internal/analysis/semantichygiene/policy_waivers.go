package semantichygiene

import (
	"go/ast"
	"go/token"
	"golang.org/x/tools/go/analysis"
	"path/filepath"
	"strconv"
	"strings"
)

type semanticDiagnostic struct {
	rule    ruleID
	pos     token.Pos
	node    ast.Node
	message string
}

type parsedWaiver struct {
	rule        ruleID
	pos         token.Pos
	explanation string
	used        bool
}

type analysisContext struct {
	pass        *analysis.Pass
	parents     map[ast.Node]ast.Node
	diagnostics []semanticDiagnostic
}

func newAnalysisContext(pass *analysis.Pass) *analysisContext {
	return &analysisContext{pass: pass, parents: buildParentMap(pass.Files)}
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
	waivers, waiverDiagnostics := ctx.collectWaivers()
	for _, diagnostic := range waiverDiagnostics {
		ctx.reportDiagnostic(diagnostic)
	}
	for _, diagnostic := range ctx.diagnostics {
		if ctx.diagnosticSuppressedByWaiver(diagnostic, waivers) {
			continue
		}
		ctx.reportDiagnostic(diagnostic)
	}
	ctx.diagnostics = nil
	for i, waiver := range waivers {
		if waiver.used {
			continue
		}
		if ctx.waiverIsDuplicateOfUsedNeighbor(waivers, i) {
			ctx.reportDiagnostic(semanticDiagnostic{
				rule:    ruleWaiverDuplicate,
				pos:     waiver.pos,
				message: "duplicate semh waiver for " + string(waiver.rule) + "; one waiver can suppress one diagnostic",
			})
			continue
		}
		ctx.reportDiagnostic(semanticDiagnostic{
			rule:    ruleWaiverStale,
			pos:     waiver.pos,
			message: "semh waiver for " + string(waiver.rule) + " did not match any diagnostic",
		})
	}
	ctx.reportWaiverBudgetDiagnostics(waivers)
}

func (ctx *analysisContext) waiverIsDuplicateOfUsedNeighbor(waivers []parsedWaiver, index int) bool {
	if index+1 >= len(waivers) {
		return false
	}
	current := waivers[index]
	next := waivers[index+1]
	return next.used &&
		current.rule == next.rule &&
		ctx.filename(current.pos) == ctx.filename(next.pos) &&
		ctx.lineAfter(current.pos, next.pos)
}

func (ctx *analysisContext) reportDiagnostic(diagnostic semanticDiagnostic) {
	ctx.pass.Report(analysis.Diagnostic{
		Pos:     diagnostic.pos,
		Message: formatDiagnosticMessage(diagnostic),
	})
}

func (ctx *analysisContext) diagnosticSuppressedByWaiver(diagnostic semanticDiagnostic, waivers []parsedWaiver) bool {
	metadata, ok := metadataForRule(diagnostic.rule)
	if !ok || !metadata.waivable {
		return false
	}
	for i := range waivers {
		if waivers[i].used || waivers[i].rule != diagnostic.rule {
			continue
		}
		if ctx.waiverMatchesDiagnostic(waivers[i], diagnostic, metadata.scope) {
			waivers[i].used = true
			return true
		}
	}
	return false
}

func (ctx *analysisContext) waiverMatchesDiagnostic(waiver parsedWaiver, diagnostic semanticDiagnostic, scope waiverScopeKind) bool {
	if ctx.filename(waiver.pos) != ctx.filename(diagnostic.pos) {
		return false
	}
	switch scope {
	case waiverScopeNextNode:
		if diagnostic.node == nil {
			return false
		}
		return ctx.lineAfter(waiver.pos, diagnostic.node.Pos())
	case waiverScopeCall:
		return ctx.waiverMatchesEnclosingCall(waiver, diagnostic.node)
	case waiverScopeDeclaration:
		decl := ctx.enclosingDeclaration(diagnostic.node)
		if decl == nil {
			return false
		}
		return ctx.lineAfter(waiver.pos, decl.Pos())
	default:
		return false
	}
}

func (ctx *analysisContext) waiverMatchesEnclosingCall(waiver parsedWaiver, node ast.Node) bool {
	for current := node; current != nil; current = ctx.parent(current) {
		switch current.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return false
		}
		call, ok := current.(*ast.CallExpr)
		if ok && ctx.lineAfter(waiver.pos, call.Pos()) {
			return true
		}
	}
	return false
}

func (ctx *analysisContext) enclosingDeclaration(node ast.Node) ast.Node {
	for current := node; current != nil; current = ctx.parent(current) {
		switch current.(type) {
		case *ast.FuncDecl, *ast.TypeSpec, *ast.ValueSpec:
			return current
		}
	}
	return nil
}

func (ctx *analysisContext) lineAfter(first token.Pos, second token.Pos) bool {
	return ctx.pass.Fset.Position(first).Line+1 == ctx.pass.Fset.Position(second).Line
}

func (ctx *analysisContext) reportWaiverBudgetDiagnostics(waivers []parsedWaiver) {
	totalUsed := 0
	ruleCounts := map[ruleID]int{}
	reportedRule := map[ruleID]bool{}
	totalReported := false
	for _, waiver := range waivers {
		if !waiver.used {
			continue
		}
		totalUsed++
		ruleCounts[waiver.rule]++
		if budget, ok := waiverBudgetForRule(waiver.rule); ok && ruleCounts[waiver.rule] > budget && !reportedRule[waiver.rule] {
			ctx.reportDiagnostic(semanticDiagnostic{
				rule: ruleWaiverBudgetExceeded,
				pos:  waiver.pos,
				message: "waiver budget exceeded for " + string(waiver.rule) +
					": used " + strconv.Itoa(ruleCounts[waiver.rule]) +
					", budget " + strconv.Itoa(budget),
			})
			reportedRule[waiver.rule] = true
		}
		if totalUsed > totalWaiverBudget && !totalReported {
			ctx.reportDiagnostic(semanticDiagnostic{
				rule: ruleWaiverBudgetExceeded,
				pos:  waiver.pos,
				message: "total waiver budget exceeded: used " + strconv.Itoa(totalUsed) +
					", budget " + strconv.Itoa(totalWaiverBudget),
			})
			totalReported = true
		}
	}
}

func (ctx *analysisContext) collectWaivers() ([]parsedWaiver, []semanticDiagnostic) {
	var waivers []parsedWaiver
	var diagnostics []semanticDiagnostic
	for _, file := range ctx.pass.Files {
		for _, group := range file.Comments {
			for _, comment := range group.List {
				if !strings.HasPrefix(comment.Text, "//") {
					continue
				}
				text := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))
				if isGinkgoLinterRawIgnoreDirective(text) {
					diagnostics = append(diagnostics, semanticDiagnostic{
						rule:    ruleGinkgoLinterRawIgnore,
						pos:     comment.Slash,
						message: ginkgoLinterRawIgnoreDiagnostic(),
					})
				}
				if !strings.HasPrefix(text, "semh:") {
					if strings.Contains(text, "semh:allow") {
						diagnostics = append(diagnostics, semanticDiagnostic{
							rule:    ruleWaiverMalformed,
							pos:     comment.Slash,
							message: "semh directive must be a standalone line comment",
						})
					}
					continue
				}
				if !isAllowWaiverDirective(text) {
					diagnostics = append(diagnostics, semanticDiagnostic{
						rule:    ruleWaiverMalformed,
						pos:     comment.Slash,
						message: "unsupported semh directive; use semh:allow <rule-id> -- <explanation>",
					})
					continue
				}
				body := strings.TrimSpace(strings.TrimPrefix(text, "semh:allow"))
				ruleText, explanation, ok := strings.Cut(body, "--")
				if !ok || strings.TrimSpace(explanation) == "" {
					rule := ruleWaiverMissingExplanation
					if strings.TrimSpace(ruleText) == "" {
						rule = ruleWaiverMalformed
					}
					diagnostics = append(diagnostics, semanticDiagnostic{
						rule:    rule,
						pos:     comment.Slash,
						message: "semh:allow " + body + " must include an explanation after --",
					})
					continue
				}
				ruleName := strings.TrimSpace(ruleText)
				if ruleName == "" || strings.ContainsAny(ruleName, " \t") {
					diagnostics = append(diagnostics, semanticDiagnostic{
						rule:    ruleWaiverMalformed,
						pos:     comment.Slash,
						message: "semh:allow waiver must name exactly one rule ID before --",
					})
					continue
				}
				rule := ruleID(ruleName)
				metadata, ok := metadataForRule(rule)
				if !ok {
					diagnostics = append(diagnostics, semanticDiagnostic{
						rule:    ruleWaiverUnknownRule,
						pos:     comment.Slash,
						message: "unknown semh waiver rule " + string(rule),
					})
					continue
				}
				if !metadata.waivable {
					diagnostics = append(diagnostics, semanticDiagnostic{
						rule:    ruleWaiverNonWaivableRule,
						pos:     comment.Slash,
						message: "semh waiver for " + string(rule) + " cannot suppress hard diagnostics",
					})
					continue
				}
				waivers = append(waivers, parsedWaiver{
					rule:        rule,
					pos:         comment.Slash,
					explanation: strings.TrimSpace(explanation),
				})
			}
		}
	}
	return waivers, diagnostics
}

func isAllowWaiverDirective(text string) bool {
	return text == "semh:allow" || strings.HasPrefix(text, "semh:allow ")
}

func isGinkgoLinterRawIgnoreDirective(text string) bool {
	return strings.HasPrefix(text, "ginkgo-linter:ignore-")
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
