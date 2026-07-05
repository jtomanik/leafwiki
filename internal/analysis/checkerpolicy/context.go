package checkerpolicy

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

type RuleID string

const (
	RuleWaiverBudgetExceeded      RuleID = "waiver.budget-exceeded"
	RuleWaiverDuplicate           RuleID = "waiver.duplicate"
	RuleWaiverMalformed           RuleID = "waiver.malformed"
	RuleWaiverMissingExplanation  RuleID = "waiver.missing-explanation"
	RuleWaiverNonWaivableRule     RuleID = "waiver.non-waivable-rule"
	RuleWaiverStale               RuleID = "waiver.stale"
	RuleWaiverUnknownRule         RuleID = "waiver.unknown-rule"
	RuleGinkgoLinterRawIgnoreRule RuleID = "ginkgo-linter.raw-ignore"
)

type WaiverScopeKind int

const (
	WaiverScopeNone WaiverScopeKind = iota
	WaiverScopeNextNode
	WaiverScopeCall
	WaiverScopeDeclaration
)

type RuleMetadata struct {
	MessagePrefix string
	Waivable      bool
	Scope         WaiverScopeKind
}

type RuleSet struct {
	metadata          map[RuleID]RuleMetadata
	waiverBudgets     map[RuleID]int
	totalWaiverBudget int
	ginkgoRawIgnoreID RuleID
	waiverBudgetRule  RuleID
	waiverDuplicate   RuleID
	waiverMalformed   RuleID
	waiverMissing     RuleID
	waiverNonWaivable RuleID
	waiverStale       RuleID
	waiverUnknown     RuleID
}

func NewRuleSet(metadata map[RuleID]RuleMetadata, waiverBudgets map[RuleID]int, totalWaiverBudget int) RuleSet {
	copiedMetadata := make(map[RuleID]RuleMetadata, len(metadata))
	for id, value := range metadata {
		copiedMetadata[id] = value
	}
	registerInfrastructureRules(copiedMetadata)
	copiedBudgets := make(map[RuleID]int, len(waiverBudgets))
	for id, value := range waiverBudgets {
		copiedBudgets[id] = value
	}
	return RuleSet{
		metadata:          copiedMetadata,
		waiverBudgets:     copiedBudgets,
		totalWaiverBudget: totalWaiverBudget,
		ginkgoRawIgnoreID: RuleGinkgoLinterRawIgnoreRule,
		waiverBudgetRule:  RuleWaiverBudgetExceeded,
		waiverDuplicate:   RuleWaiverDuplicate,
		waiverMalformed:   RuleWaiverMalformed,
		waiverMissing:     RuleWaiverMissingExplanation,
		waiverNonWaivable: RuleWaiverNonWaivableRule,
		waiverStale:       RuleWaiverStale,
		waiverUnknown:     RuleWaiverUnknownRule,
	}
}

func registerInfrastructureRules(metadata map[RuleID]RuleMetadata) {
	for _, id := range []RuleID{
		RuleWaiverBudgetExceeded,
		RuleWaiverDuplicate,
		RuleWaiverMalformed,
		RuleWaiverMissingExplanation,
		RuleWaiverNonWaivableRule,
		RuleWaiverStale,
		RuleWaiverUnknownRule,
	} {
		metadata[id] = HardRule(id)
	}
}

func HardRule(id RuleID) RuleMetadata {
	return RuleMetadata{MessagePrefix: string(id), Waivable: false, Scope: WaiverScopeNone}
}

func WaivableRule(id RuleID, scope WaiverScopeKind) RuleMetadata {
	return RuleMetadata{MessagePrefix: string(id), Waivable: true, Scope: scope}
}

func (rules RuleSet) MetadataForRule(id RuleID) (RuleMetadata, bool) {
	metadata, ok := rules.metadata[id]
	return metadata, ok
}

func (rules RuleSet) AllRuleMetadata() map[RuleID]RuleMetadata {
	metadata := make(map[RuleID]RuleMetadata, len(rules.metadata))
	for id, value := range rules.metadata {
		metadata[id] = value
	}
	return metadata
}

func (rules RuleSet) WaiverBudgetForRule(id RuleID) (int, bool) {
	budget, ok := rules.waiverBudgets[id]
	return budget, ok
}

func (rules RuleSet) FormatDiagnosticMessage(diagnostic Diagnostic) string {
	metadata, ok := rules.MetadataForRule(diagnostic.Rule)
	if !ok {
		return "semh:" + string(diagnostic.Rule) + ": " + diagnostic.Message
	}
	return "semh:" + metadata.MessagePrefix + ": " + diagnostic.Message
}

type Diagnostic struct {
	Rule    RuleID
	Pos     token.Pos
	Node    ast.Node
	Message string
}

type ParsedWaiver struct {
	Rule        RuleID
	Pos         token.Pos
	Explanation string
	Used        bool
}

type Context struct {
	pass           *analysis.Pass
	rules          RuleSet
	processWaivers bool
	parents        map[ast.Node]ast.Node
	diagnostics    []Diagnostic
}

func NewContext(pass *analysis.Pass, rules RuleSet, processWaivers bool) *Context {
	return &Context{
		pass:           pass,
		rules:          rules,
		processWaivers: processWaivers,
		parents:        buildParentMap(pass.Files),
	}
}

func (ctx *Context) Pass() *analysis.Pass {
	return ctx.pass
}

func (ctx *Context) Parent(node ast.Node) ast.Node {
	return ctx.parents[node]
}

func (ctx *Context) Filename(pos token.Pos) string {
	return filepath.ToSlash(ctx.pass.Fset.Position(pos).Filename)
}

func (ctx *Context) Report(rule RuleID, node ast.Node, message string) {
	ctx.diagnostics = append(ctx.diagnostics, Diagnostic{
		Rule:    rule,
		Pos:     node.Pos(),
		Node:    node,
		Message: message,
	})
}

func (ctx *Context) FinalizeDiagnostics() {
	if !ctx.processWaivers {
		for _, diagnostic := range ctx.diagnostics {
			ctx.reportDiagnostic(diagnostic)
		}
		ctx.diagnostics = nil
		return
	}

	waivers, waiverDiagnostics := ctx.CollectWaivers()
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
		if waiver.Used {
			continue
		}
		if ctx.waiverIsDuplicateOfUsedNeighbor(waivers, i) {
			ctx.reportDiagnostic(Diagnostic{
				Rule:    ctx.rules.waiverDuplicate,
				Pos:     waiver.Pos,
				Message: "duplicate semh waiver for " + string(waiver.Rule) + "; one waiver can suppress one diagnostic",
			})
			continue
		}
		ctx.reportDiagnostic(Diagnostic{
			Rule:    ctx.rules.waiverStale,
			Pos:     waiver.Pos,
			Message: "semh waiver for " + string(waiver.Rule) + " did not match any diagnostic",
		})
	}
	ctx.reportWaiverBudgetDiagnostics(waivers)
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

func (ctx *Context) reportDiagnostic(diagnostic Diagnostic) {
	ctx.pass.Report(analysis.Diagnostic{
		Pos:     diagnostic.Pos,
		Message: ctx.rules.FormatDiagnosticMessage(diagnostic),
	})
}

func (ctx *Context) diagnosticSuppressedByWaiver(diagnostic Diagnostic, waivers []ParsedWaiver) bool {
	metadata, ok := ctx.rules.MetadataForRule(diagnostic.Rule)
	if !ok || !metadata.Waivable {
		return false
	}
	for i := range waivers {
		if waivers[i].Used || waivers[i].Rule != diagnostic.Rule {
			continue
		}
		if ctx.WaiverMatchesDiagnostic(waivers[i], diagnostic, metadata.Scope) {
			waivers[i].Used = true
			return true
		}
	}
	return false
}

func (ctx *Context) WaiverMatchesDiagnostic(waiver ParsedWaiver, diagnostic Diagnostic, scope WaiverScopeKind) bool {
	if ctx.Filename(waiver.Pos) != ctx.Filename(diagnostic.Pos) {
		return false
	}
	switch scope {
	case WaiverScopeNextNode:
		if diagnostic.Node == nil {
			return false
		}
		return ctx.LineAfter(waiver.Pos, diagnostic.Node.Pos())
	case WaiverScopeCall:
		return ctx.waiverMatchesEnclosingCall(waiver, diagnostic.Node)
	case WaiverScopeDeclaration:
		decl := ctx.EnclosingDeclaration(diagnostic.Node)
		if decl == nil {
			return false
		}
		return ctx.LineAfter(waiver.Pos, decl.Pos())
	default:
		return false
	}
}

func (ctx *Context) waiverMatchesEnclosingCall(waiver ParsedWaiver, node ast.Node) bool {
	for current := node; current != nil; current = ctx.Parent(current) {
		switch current.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return false
		}
		call, ok := current.(*ast.CallExpr)
		if ok && ctx.LineAfter(waiver.Pos, call.Pos()) {
			return true
		}
	}
	return false
}

func (ctx *Context) EnclosingDeclaration(node ast.Node) ast.Node {
	for current := node; current != nil; current = ctx.Parent(current) {
		switch current.(type) {
		case *ast.FuncDecl, *ast.TypeSpec, *ast.ValueSpec:
			return current
		}
	}
	return nil
}

func (ctx *Context) LineAfter(first token.Pos, second token.Pos) bool {
	return ctx.pass.Fset.Position(first).Line+1 == ctx.pass.Fset.Position(second).Line
}

func (ctx *Context) CollectWaivers() ([]ParsedWaiver, []Diagnostic) {
	var waivers []ParsedWaiver
	var diagnostics []Diagnostic
	for _, file := range ctx.pass.Files {
		for _, group := range file.Comments {
			for _, comment := range group.List {
				if !strings.HasPrefix(comment.Text, "//") {
					continue
				}
				text := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))
				if strings.HasPrefix(text, "ginkgo-linter:ignore-") {
					diagnostics = append(diagnostics, Diagnostic{
						Rule:    ctx.rules.ginkgoRawIgnoreID,
						Pos:     comment.Slash,
						Message: "raw ginkgolinter ignore comments are not allowed; fix the generic lint or use semh waivers only for waivable semantic-hygiene rules",
					})
				}
				if !strings.HasPrefix(text, "semh:") {
					if strings.Contains(text, "semh:allow") {
						diagnostics = append(diagnostics, Diagnostic{
							Rule:    ctx.rules.waiverMalformed,
							Pos:     comment.Slash,
							Message: "semh directive must be a standalone line comment",
						})
					}
					continue
				}
				if !isAllowWaiverDirective(text) {
					diagnostics = append(diagnostics, Diagnostic{
						Rule:    ctx.rules.waiverMalformed,
						Pos:     comment.Slash,
						Message: "unsupported semh directive; use semh:allow <rule-id> -- <explanation>",
					})
					continue
				}
				body := strings.TrimSpace(strings.TrimPrefix(text, "semh:allow"))
				ruleText, explanation, ok := strings.Cut(body, "--")
				if body == "" {
					diagnostics = append(diagnostics, Diagnostic{
						Rule:    ctx.rules.waiverMalformed,
						Pos:     comment.Slash,
						Message: "semh:allow waiver must name exactly one rule ID before --",
					})
					continue
				}
				if !ok || strings.TrimSpace(explanation) == "" {
					rule := ctx.rules.waiverMissing
					if strings.TrimSpace(ruleText) == "" {
						rule = ctx.rules.waiverMalformed
					}
					diagnostics = append(diagnostics, Diagnostic{
						Rule:    rule,
						Pos:     comment.Slash,
						Message: "semh:allow " + body + " must include an explanation after --",
					})
					continue
				}
				ruleName := strings.TrimSpace(ruleText)
				if ruleName == "" || strings.ContainsAny(ruleName, " \t") {
					diagnostics = append(diagnostics, Diagnostic{
						Rule:    ctx.rules.waiverMalformed,
						Pos:     comment.Slash,
						Message: "semh:allow waiver must name exactly one rule ID before --",
					})
					continue
				}
				rule := RuleID(ruleName)
				metadata, ok := ctx.rules.MetadataForRule(rule)
				if !ok {
					diagnostics = append(diagnostics, Diagnostic{
						Rule:    ctx.rules.waiverUnknown,
						Pos:     comment.Slash,
						Message: "unknown semh waiver rule " + string(rule),
					})
					continue
				}
				if !metadata.Waivable {
					diagnostics = append(diagnostics, Diagnostic{
						Rule:    ctx.rules.waiverNonWaivable,
						Pos:     comment.Slash,
						Message: "semh waiver for " + string(rule) + " cannot suppress hard diagnostics",
					})
					continue
				}
				waivers = append(waivers, ParsedWaiver{
					Rule:        rule,
					Pos:         comment.Slash,
					Explanation: strings.TrimSpace(explanation),
				})
			}
		}
	}
	return waivers, diagnostics
}

func isAllowWaiverDirective(text string) bool {
	return text == "semh:allow" || strings.HasPrefix(text, "semh:allow ")
}

func (ctx *Context) waiverIsDuplicateOfUsedNeighbor(waivers []ParsedWaiver, index int) bool {
	if index+1 >= len(waivers) {
		return false
	}
	current := waivers[index]
	next := waivers[index+1]
	return next.Used &&
		current.Rule == next.Rule &&
		ctx.Filename(current.Pos) == ctx.Filename(next.Pos) &&
		ctx.LineAfter(current.Pos, next.Pos)
}

func (ctx *Context) reportWaiverBudgetDiagnostics(waivers []ParsedWaiver) {
	totalUsed := 0
	ruleCounts := map[RuleID]int{}
	reportedRule := map[RuleID]bool{}
	totalReported := false
	for _, waiver := range waivers {
		if !waiver.Used {
			continue
		}
		totalUsed++
		ruleCounts[waiver.Rule]++
		if budget, ok := ctx.rules.WaiverBudgetForRule(waiver.Rule); ok && ruleCounts[waiver.Rule] > budget && !reportedRule[waiver.Rule] {
			ctx.reportDiagnostic(Diagnostic{
				Rule: ctx.rules.waiverBudgetRule,
				Pos:  waiver.Pos,
				Message: "waiver budget exceeded for " + string(waiver.Rule) +
					": used " + strconv.Itoa(ruleCounts[waiver.Rule]) +
					", budget " + strconv.Itoa(budget),
			})
			reportedRule[waiver.Rule] = true
		}
		if ctx.rules.totalWaiverBudget > 0 && totalUsed > ctx.rules.totalWaiverBudget && !totalReported {
			ctx.reportDiagnostic(Diagnostic{
				Rule: ctx.rules.waiverBudgetRule,
				Pos:  waiver.Pos,
				Message: "total waiver budget exceeded: used " + strconv.Itoa(totalUsed) +
					", budget " + strconv.Itoa(ctx.rules.totalWaiverBudget),
			})
			totalReported = true
		}
	}
}
