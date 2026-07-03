package semantichygiene

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

type ruleID string

const (
	ruleSemanticStringLeak                 ruleID = "semantic.string-leak"
	ruleDirectCast                         ruleID = "semantic.direct-cast"
	ruleSemanticUncheckedConstructor       ruleID = "semantic.unchecked-constructor"
	ruleSemanticRawSignature               ruleID = "semantic.raw-signature"
	ruleSemanticRawField                   ruleID = "semantic.raw-field"
	ruleSemanticRawPrimitive               ruleID = "semantic.raw-primitive"
	ruleSemanticValidatorReturn            ruleID = "semantic.validator-return"
	ruleI18nRawProse                       ruleID = "i18n.raw-prose"
	ruleI18nRawProseSink                   ruleID = "i18n.raw-prose-sink"
	ruleI18nLocalizedErrorPassthrough      ruleID = "i18n.localized-error-passthrough"
	ruleI18nResponseStatusForward          ruleID = "i18n.response-status-forward"
	ruleI18nMessageField                   ruleID = "i18n.message-field"
	ruleI18nMessageParameter               ruleID = "i18n.message-parameter"
	ruleContractRawLiteral                 ruleID = "contract.raw-literal"
	ruleDependencyE2EProxyInternalImport   ruleID = "dependency.e2e-proxy-internal-import"
	ruleGinkgoFocus                        ruleID = "ginkgo.focus"
	ruleGinkgoPending                      ruleID = "ginkgo.pending"
	ruleGinkgoFlakeAttempts                ruleID = "ginkgo.flake-attempts"
	ruleGinkgoRestrictedDecorator          ruleID = "ginkgo.restricted-decorator"
	ruleGinkgoContainerCall                ruleID = "ginkgo.container-call"
	ruleGinkgoContainerStateInitialization ruleID = "ginkgo.container-state-initialization"
	ruleGinkgoEntrySetupValue              ruleID = "ginkgo.entry-setup-value"
	ruleGinkgoGoroutineRecover             ruleID = "ginkgo.goroutine-recover"
	ruleGinkgoBlockingReceive              ruleID = "ginkgo.blocking-receive"
	ruleGinkgoHelperFirst                  ruleID = "ginkgo.helper-first"
	ruleGinkgoGlobalStateCleanup           ruleID = "ginkgo.global-state-cleanup"
	ruleGinkgoTestName                     ruleID = "ginkgo.test-name"
	ruleGinkgoCoverageName                 ruleID = "ginkgo.coverage-name"
	ruleGinkgoTestingTInSpec               ruleID = "ginkgo.testing-t-in-spec"
	ruleGinkgoFailInSpec                   ruleID = "ginkgo.fail-in-spec"
	ruleGinkgoLinterRawIgnore              ruleID = "ginkgo-linter.raw-ignore"
	ruleGomegaErrorString                  ruleID = "gomega.err-error-string"
	ruleGomegaRawStringMatchError          ruleID = "gomega.raw-string-match-error"
	ruleGomegaErrorNilMatcher              ruleID = "gomega.error-nil-matcher"
	ruleGomegaInlineErrorSucceed           ruleID = "gomega.inline-error-succeed"
	ruleGomegaMultiReturnErrorMatcher      ruleID = "gomega.multi-return-error-matcher"
	ruleGomegaStringsContains              ruleID = "gomega.strings-contains"
	ruleGomegaStringPredicate              ruleID = "gomega.string-predicate"
	ruleGomegaRegexpMatchString            ruleID = "gomega.regexp-match-string"
	ruleGomegaErrorsIsMatcher              ruleID = "gomega.errors-is-matcher"
	ruleGomegaErrorsAsMatcher              ruleID = "gomega.errors-as-matcher"
	ruleGomegaOSIsNotExistMatcher          ruleID = "gomega.os-is-not-exist-matcher"
	ruleGomegaLenEqual                     ruleID = "gomega.len-equal"
	ruleGomegaBinaryBoolean                ruleID = "gomega.binary-boolean"
	ruleGomegaBooleanLiteral               ruleID = "gomega.boolean-literal"
	ruleGomegaCommaOKAssertion             ruleID = "gomega.comma-ok-assertion"
	ruleGomegaProxyBoolean                 ruleID = "gomega.proxy-boolean"
	ruleGomegaMapIndex                     ruleID = "gomega.map-index"
	ruleGomegaHTTPStatus                   ruleID = "gomega.http-status"
	ruleGomegaHTTPBody                     ruleID = "gomega.http-body"
	ruleGomegaRepeatedHTTPBody             ruleID = "gomega.repeated-http-body"
	ruleGomegaHTTPHeader                   ruleID = "gomega.http-header"
	ruleGomegaStructuredErrorMatcher       ruleID = "gomega.structured-error-matcher"
	ruleGomegaStructuredProtocolKey        ruleID = "gomega.structured-protocol-key"
	ruleGomegaStructuredProtocolPayload    ruleID = "gomega.structured-protocol-payload"
	ruleGomegaAsyncContext                 ruleID = "gomega.async-context"
	ruleGomegaAsyncBoolean                 ruleID = "gomega.async-boolean"
	ruleGomegaAsyncNegativeReceive         ruleID = "gomega.async-negative-receive"
	ruleGomegaAsyncBareValue               ruleID = "gomega.async-bare-value"
	ruleGomegaAsyncCallbackExpect          ruleID = "gomega.async-callback-expect"
	ruleGomegaHelperOffset                 ruleID = "gomega.helper-offset"
	ruleGinkgoTopLevelIt                   ruleID = "ginkgo.top-level-it"
	ruleGinkgoWideEntry                    ruleID = "ginkgo.wide-entry"
	ruleGomegaHelperShouldBeMatcher        ruleID = "gomega.helper-should-be-matcher"
	ruleGomegaRepeatedFieldAssertions      ruleID = "gomega.repeated-field-assertions"
	ruleGomegaCollectionIndexAssertion     ruleID = "gomega.collection-index-assertion"
	ruleGomegaEqualEmpty                   ruleID = "gomega.equal-empty"
	ruleGomegaEqualZero                    ruleID = "gomega.equal-zero"
	ruleGomegaNumericEquivalent            ruleID = "gomega.numeric-equivalent"
	ruleGomegaTimeEqual                    ruleID = "gomega.time-equal"
	ruleGomegaMatcherAsValue               ruleID = "gomega.matcher-as-value"
	ruleGomegaPositionalTransform          ruleID = "gomega.positional-transform"
	ruleGomegaPositionalCompositeAssertion ruleID = "gomega.positional-composite-assertion"
	ruleWaiverBudgetExceeded               ruleID = "waiver.budget-exceeded"
	ruleWaiverDuplicate                    ruleID = "waiver.duplicate"
	ruleWaiverMalformed                    ruleID = "waiver.malformed"
	ruleWaiverMissingExplanation           ruleID = "waiver.missing-explanation"
	ruleWaiverNonWaivableRule              ruleID = "waiver.non-waivable-rule"
	ruleWaiverStale                        ruleID = "waiver.stale"
	ruleWaiverUnknownRule                  ruleID = "waiver.unknown-rule"
)

type waiverScopeKind int

const (
	waiverScopeNone waiverScopeKind = iota
	waiverScopeNextNode
	waiverScopeCall
	waiverScopeDeclaration
)

type ruleMetadata struct {
	messagePrefix string
	waivable      bool
	scope         waiverScopeKind
}

var ruleMetadataByID = map[ruleID]ruleMetadata{
	ruleSemanticStringLeak:                 hardRule(ruleSemanticStringLeak),
	ruleDirectCast:                         hardRule(ruleDirectCast),
	ruleSemanticUncheckedConstructor:       hardRule(ruleSemanticUncheckedConstructor),
	ruleSemanticRawSignature:               hardRule(ruleSemanticRawSignature),
	ruleSemanticRawField:                   hardRule(ruleSemanticRawField),
	ruleSemanticRawPrimitive:               hardRule(ruleSemanticRawPrimitive),
	ruleSemanticValidatorReturn:            hardRule(ruleSemanticValidatorReturn),
	ruleI18nRawProse:                       hardRule(ruleI18nRawProse),
	ruleI18nRawProseSink:                   hardRule(ruleI18nRawProseSink),
	ruleI18nLocalizedErrorPassthrough:      hardRule(ruleI18nLocalizedErrorPassthrough),
	ruleI18nResponseStatusForward:          hardRule(ruleI18nResponseStatusForward),
	ruleI18nMessageField:                   hardRule(ruleI18nMessageField),
	ruleI18nMessageParameter:               hardRule(ruleI18nMessageParameter),
	ruleContractRawLiteral:                 hardRule(ruleContractRawLiteral),
	ruleDependencyE2EProxyInternalImport:   hardRule(ruleDependencyE2EProxyInternalImport),
	ruleGinkgoFocus:                        hardRule(ruleGinkgoFocus),
	ruleGinkgoPending:                      hardRule(ruleGinkgoPending),
	ruleGinkgoFlakeAttempts:                hardRule(ruleGinkgoFlakeAttempts),
	ruleGinkgoRestrictedDecorator:          hardRule(ruleGinkgoRestrictedDecorator),
	ruleGinkgoContainerCall:                hardRule(ruleGinkgoContainerCall),
	ruleGinkgoContainerStateInitialization: hardRule(ruleGinkgoContainerStateInitialization),
	ruleGinkgoEntrySetupValue:              hardRule(ruleGinkgoEntrySetupValue),
	ruleGinkgoGoroutineRecover:             hardRule(ruleGinkgoGoroutineRecover),
	ruleGinkgoBlockingReceive:              hardRule(ruleGinkgoBlockingReceive),
	ruleGinkgoHelperFirst:                  hardRule(ruleGinkgoHelperFirst),
	ruleGinkgoGlobalStateCleanup:           hardRule(ruleGinkgoGlobalStateCleanup),
	ruleGinkgoTestName:                     hardRule(ruleGinkgoTestName),
	ruleGinkgoCoverageName:                 hardRule(ruleGinkgoCoverageName),
	ruleGinkgoTestingTInSpec:               hardRule(ruleGinkgoTestingTInSpec),
	ruleGinkgoFailInSpec:                   hardRule(ruleGinkgoFailInSpec),
	ruleGinkgoLinterRawIgnore:              hardRule(ruleGinkgoLinterRawIgnore),
	ruleGomegaErrorString:                  hardRule(ruleGomegaErrorString),
	ruleGomegaRawStringMatchError:          hardRule(ruleGomegaRawStringMatchError),
	ruleGomegaErrorNilMatcher:              hardRule(ruleGomegaErrorNilMatcher),
	ruleGomegaInlineErrorSucceed:           hardRule(ruleGomegaInlineErrorSucceed),
	ruleGomegaMultiReturnErrorMatcher:      hardRule(ruleGomegaMultiReturnErrorMatcher),
	ruleGomegaStringsContains:              hardRule(ruleGomegaStringsContains),
	ruleGomegaStringPredicate:              hardRule(ruleGomegaStringPredicate),
	ruleGomegaRegexpMatchString:            hardRule(ruleGomegaRegexpMatchString),
	ruleGomegaErrorsIsMatcher:              hardRule(ruleGomegaErrorsIsMatcher),
	ruleGomegaErrorsAsMatcher:              hardRule(ruleGomegaErrorsAsMatcher),
	ruleGomegaOSIsNotExistMatcher:          hardRule(ruleGomegaOSIsNotExistMatcher),
	ruleGomegaLenEqual:                     hardRule(ruleGomegaLenEqual),
	ruleGomegaBinaryBoolean:                hardRule(ruleGomegaBinaryBoolean),
	ruleGomegaBooleanLiteral:               hardRule(ruleGomegaBooleanLiteral),
	ruleGomegaCommaOKAssertion:             hardRule(ruleGomegaCommaOKAssertion),
	ruleGomegaProxyBoolean:                 hardRule(ruleGomegaProxyBoolean),
	ruleGomegaMapIndex:                     hardRule(ruleGomegaMapIndex),
	ruleGomegaHTTPStatus:                   hardRule(ruleGomegaHTTPStatus),
	ruleGomegaHTTPBody:                     hardRule(ruleGomegaHTTPBody),
	ruleGomegaRepeatedHTTPBody:             hardRule(ruleGomegaRepeatedHTTPBody),
	ruleGomegaHTTPHeader:                   hardRule(ruleGomegaHTTPHeader),
	ruleGomegaStructuredErrorMatcher:       hardRule(ruleGomegaStructuredErrorMatcher),
	ruleGomegaStructuredProtocolKey:        hardRule(ruleGomegaStructuredProtocolKey),
	ruleGomegaStructuredProtocolPayload:    hardRule(ruleGomegaStructuredProtocolPayload),
	ruleGomegaAsyncContext:                 hardRule(ruleGomegaAsyncContext),
	ruleGomegaAsyncBoolean:                 hardRule(ruleGomegaAsyncBoolean),
	ruleGomegaAsyncNegativeReceive:         hardRule(ruleGomegaAsyncNegativeReceive),
	ruleGomegaAsyncBareValue:               hardRule(ruleGomegaAsyncBareValue),
	ruleGomegaAsyncCallbackExpect:          hardRule(ruleGomegaAsyncCallbackExpect),
	ruleGomegaHelperOffset:                 hardRule(ruleGomegaHelperOffset),
	ruleGinkgoTopLevelIt:                   waivableRule(ruleGinkgoTopLevelIt, waiverScopeCall),
	ruleGinkgoWideEntry:                    waivableRule(ruleGinkgoWideEntry, waiverScopeCall),
	ruleGomegaHelperShouldBeMatcher:        waivableRule(ruleGomegaHelperShouldBeMatcher, waiverScopeDeclaration),
	ruleGomegaRepeatedFieldAssertions:      waivableRule(ruleGomegaRepeatedFieldAssertions, waiverScopeCall),
	ruleGomegaCollectionIndexAssertion:     waivableRule(ruleGomegaCollectionIndexAssertion, waiverScopeCall),
	ruleGomegaEqualEmpty:                   waivableRule(ruleGomegaEqualEmpty, waiverScopeCall),
	ruleGomegaEqualZero:                    waivableRule(ruleGomegaEqualZero, waiverScopeCall),
	ruleGomegaNumericEquivalent:            waivableRule(ruleGomegaNumericEquivalent, waiverScopeCall),
	ruleGomegaTimeEqual:                    waivableRule(ruleGomegaTimeEqual, waiverScopeCall),
	ruleGomegaMatcherAsValue:               hardRule(ruleGomegaMatcherAsValue),
	ruleGomegaPositionalTransform:          hardRule(ruleGomegaPositionalTransform),
	ruleGomegaPositionalCompositeAssertion: hardRule(ruleGomegaPositionalCompositeAssertion),
	ruleWaiverBudgetExceeded:               hardRule(ruleWaiverBudgetExceeded),
	ruleWaiverDuplicate:                    hardRule(ruleWaiverDuplicate),
	ruleWaiverMalformed:                    hardRule(ruleWaiverMalformed),
	ruleWaiverMissingExplanation:           hardRule(ruleWaiverMissingExplanation),
	ruleWaiverNonWaivableRule:              hardRule(ruleWaiverNonWaivableRule),
	ruleWaiverStale:                        hardRule(ruleWaiverStale),
	ruleWaiverUnknownRule:                  hardRule(ruleWaiverUnknownRule),
}

const totalWaiverBudget = 10

var waiverBudgetsByRule = map[ruleID]int{
	ruleGinkgoTopLevelIt:               3,
	ruleGinkgoWideEntry:                3,
	ruleGomegaHelperShouldBeMatcher:    3,
	ruleGomegaRepeatedFieldAssertions:  3,
	ruleGomegaCollectionIndexAssertion: 3,
	ruleGomegaEqualEmpty:               3,
	ruleGomegaEqualZero:                3,
	ruleGomegaNumericEquivalent:        3,
	ruleGomegaTimeEqual:                3,
}

func hardRule(id ruleID) ruleMetadata {
	return ruleMetadata{messagePrefix: string(id), waivable: false, scope: waiverScopeNone}
}

func waivableRule(id ruleID, scope waiverScopeKind) ruleMetadata {
	return ruleMetadata{messagePrefix: string(id), waivable: true, scope: scope}
}

func metadataForRule(id ruleID) (ruleMetadata, bool) {
	metadata, ok := ruleMetadataByID[id]
	return metadata, ok
}

func allRuleMetadata() map[ruleID]ruleMetadata {
	metadata := make(map[ruleID]ruleMetadata, len(ruleMetadataByID))
	for id, value := range ruleMetadataByID {
		metadata[id] = value
	}
	return metadata
}

func waiverBudgetForRule(id ruleID) (int, bool) {
	budget, ok := waiverBudgetsByRule[id]
	return budget, ok
}

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

func (ctx *analysisContext) enclosingCall(node ast.Node) *ast.CallExpr {
	for current := node; current != nil; current = ctx.parent(current) {
		if call, ok := current.(*ast.CallExpr); ok {
			return call
		}
	}
	return nil
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

func semanticTypeNameOf(typ types.Type) (string, bool) {
	if typ == nil {
		return "", false
	}
	typ = types.Unalias(typ)
	if ptr, ok := typ.(*types.Pointer); ok {
		typ = ptr.Elem()
	}
	named, ok := typ.(*types.Named)
	if !ok {
		return "", false
	}
	name := named.Obj().Name()
	if semanticTypeNames[name] {
		return name, true
	}
	return "", false
}

func semanticExprTypeName(pass *analysis.Pass, expr ast.Expr) (string, bool) {
	return semanticTypeNameOf(pass.TypesInfo.TypeOf(expr))
}

func conversionSemanticTypeName(pass *analysis.Pass, expr ast.Expr) (string, bool) {
	if tv, ok := pass.TypesInfo.Types[expr]; ok && tv.IsType() {
		return semanticTypeNameOf(tv.Type)
	}
	return semanticTypeNameOf(pass.TypesInfo.TypeOf(expr))
}

var semanticTypeNames = map[string]bool{
	"APIKeyID":             true,
	"ActorID":              true,
	"AgentEventName":       true,
	"AgentSource":          true,
	"AgentToolName":        true,
	"AssetName":            true,
	"CommitHash":           true,
	"EntryKind":            true,
	"ErrorCode":            true,
	"FieldErrorCode":       true,
	"GrantRole":            true,
	"ImportErrorCode":      true,
	"IssueCode":            true,
	"IssueSeverity":        true,
	"MarkdownPath":         true,
	"MarkdownSourceKind":   true,
	"MCPSessionID":         true,
	"MessageID":            true,
	"NodeKind":             true,
	"PageID":               true,
	"PageVersion":          true,
	"ProviderID":           true,
	"RevisionID":           true,
	"RoutePath":            true,
	"RoleName":             true,
	"SectionEditErrorCode": true,
	"SessionID":            true,
	"SessionMode":          true,
	"SessionState":         true,
	"SessionType":          true,
	"Slug":                 true,
	"TargetKind":           true,
	"ToolDescriptionID":    true,
	"ToolID":               true,
	"ToolMessageID":        true,
	"ToolProtocolName":     true,
	"UserID":               true,
	"WebSessionID":         true,
	"WorkspaceID":          true,
	"WorkspaceSourcePath":  true,
	"WorkspaceSyncIssueID": true,
}

var semanticNameTypes = map[string]string{
	"apikeyid":             "APIKeyID",
	"actorid":              "ActorID",
	"agenteventname":       "AgentEventName",
	"agentsource":          "AgentSource",
	"agenttoolname":        "AgentToolName",
	"assetname":            "AssetName",
	"commithash":           "CommitHash",
	"currentpath":          "RoutePath",
	"entrykind":            "EntryKind",
	"errorcode":            "ErrorCode",
	"fieldcode":            "FieldErrorCode",
	"fieldvalidationcode":  "FieldErrorCode",
	"filename":             "AssetName",
	"grantrole":            "GrantRole",
	"importerrorcode":      "ImportErrorCode",
	"issuecode":            "IssueCode",
	"markdownsourcekind":   "MarkdownSourceKind",
	"mcpsessionid":         "MCPSessionID",
	"messageid":            "MessageID",
	"nodekind":             "NodeKind",
	"oldpath":              "RoutePath",
	"pagepath":             "RoutePath",
	"pageid":               "PageID",
	"pageversion":          "PageVersion",
	"providerid":           "ProviderID",
	"revisionid":           "RevisionID",
	"routepath":            "RoutePath",
	"rolename":             "RoleName",
	"sectionediterrorcode": "SectionEditErrorCode",
	"sessionid":            "SessionID",
	"sessionmode":          "SessionMode",
	"sessionstate":         "SessionState",
	"sessiontype":          "SessionType",
	"slug":                 "Slug",
	"sourcepath":           "WorkspaceSourcePath",
	"targetkind":           "TargetKind",
	"targetpath":           "RoutePath",
	"topath":               "RoutePath",
	"toolid":               "ToolID",
	"toolmessageid":        "ToolMessageID",
	"toolname":             "AgentToolName",
	"toolprotocolname":     "ToolProtocolName",
	"userid":               "UserID",
	"websessionid":         "WebSessionID",
	"workspaceid":          "WorkspaceID",
	"workspacesourcepath":  "WorkspaceSourcePath",
	"workspacesyncissueid": "WorkspaceSyncIssueID",
}

var semanticPrimitiveNames = map[string]bool{
	"depth":    true,
	"limit":    true,
	"maxbytes": true,
	"offset":   true,
}

var semanticPrimitiveContextHints = map[string][]string{
	"depth":    {"tree", "subtree", "navigation"},
	"limit":    {"search", "tag", "propert", "revision", "snapshot", "page"},
	"maxbytes": {"asset", "file", "stream", "upload", "write"},
	"offset":   {"search", "page", "revision", "snapshot"},
}

func semanticTypeForName(name string) string {
	if typ, ok := semanticTypeForCanonicalName(canonicalName(name)); ok {
		return typ
	}
	return "a semantic type"
}

func semanticPrimitiveName(name string) bool {
	_, ok := semanticPrimitiveNames[canonicalName(name)]
	return ok
}

func semanticPrimitiveNameInContext(name string, context string) bool {
	canonical := canonicalName(name)
	hints, ok := semanticPrimitiveContextHints[canonical]
	if !ok {
		return false
	}
	canonicalContext := canonicalName(context)
	return containsAnyCanonical(canonicalContext, hints)
}

func containsAnyCanonical(value string, fragments []string) bool {
	for _, fragment := range fragments {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}

func semanticTypeForFieldName(fieldName string, typeName string) (string, bool) {
	if typ, ok := semanticTypeForCanonicalName(canonicalName(fieldName)); ok {
		return typ, true
	}
	if canonicalName(fieldName) == "id" && pageIdentityContext(typeName) {
		return "PageID", true
	}
	if canonicalName(fieldName) == "hash" && commitHashContext(typeName) {
		return "CommitHash", true
	}
	return "", false
}

func semanticTypeForParamName(paramName string, funcName string) (string, bool) {
	if typ, ok := semanticTypeForCanonicalName(canonicalName(paramName)); ok {
		return typ, true
	}
	if canonicalName(paramName) == "id" {
		return semanticTypeForBareIDContext(funcName)
	}
	if canonicalName(paramName) == "hash" {
		return semanticTypeForBareHashContext(funcName)
	}
	return "", false
}

func semanticTypeForBareIDContext(name string) (string, bool) {
	canonical := canonicalName(name)
	switch {
	case strings.Contains(canonical, "apikey"):
		return "APIKeyID", true
	case strings.Contains(canonical, "workspace"):
		return "WorkspaceID", true
	case strings.Contains(canonical, "revision"):
		return "RevisionID", true
	case strings.Contains(canonical, "user") || strings.Contains(canonical, "author"):
		return "UserID", true
	case strings.Contains(canonical, "tool"):
		return "ToolID", true
	case pageIdentityContext(name):
		return "PageID", true
	case strings.HasSuffix(canonical, "byid"):
		return "PageID", true
	default:
		return "", false
	}
}

func pageIdentityContext(name string) bool {
	canonical := canonicalName(name)
	return strings.Contains(canonical, "page") ||
		strings.Contains(canonical, "node") ||
		strings.Contains(canonical, "permalink")
}

func commitHashContext(name string) bool {
	canonical := canonicalName(name)
	return strings.Contains(canonical, "commit") ||
		strings.Contains(canonical, "revision")
}

func semanticTypeForBareHashContext(name string) (string, bool) {
	if commitHashContext(name) {
		return "CommitHash", true
	}
	return "", false
}

func semanticName(name string) bool {
	_, ok := semanticTypeForCanonicalName(canonicalName(name))
	return ok
}

func semanticTypeForCanonicalName(name string) (string, bool) {
	if typ, ok := semanticNameTypes[name]; ok {
		return typ, true
	}
	if strings.HasSuffix(name, "s") {
		if typ, ok := semanticNameTypes[strings.TrimSuffix(name, "s")]; ok {
			return typ, true
		}
	}
	if strings.HasSuffix(name, "byid") || strings.HasSuffix(name, "ids") {
		return "PageID", true
	}
	return "", false
}

func canonicalName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r == '_' || r == '-' {
			continue
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

func exprName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.StarExpr:
		return exprName(e.X)
	case *ast.IndexExpr:
		return exprName(e.X)
	case *ast.IndexListExpr:
		return exprName(e.X)
	default:
		return ""
	}
}

func callName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	default:
		return ""
	}
}

func enclosingFuncName(ctx *analysisContext, node ast.Node) string {
	for current := node; current != nil; current = ctx.parent(current) {
		if fn, ok := current.(*ast.FuncDecl); ok {
			return fn.Name.Name
		}
	}
	return ""
}

func enclosingFunc(ctx *analysisContext, node ast.Node) *ast.FuncDecl {
	for current := node; current != nil; current = ctx.parent(current) {
		if fn, ok := current.(*ast.FuncDecl); ok {
			return fn
		}
	}
	return nil
}

func isStringType(pass *analysis.Pass, expr ast.Expr) bool {
	return isString(pass.TypesInfo.TypeOf(expr))
}

func isString(typ types.Type) bool {
	if typ == nil {
		return false
	}
	basic, ok := typ.Underlying().(*types.Basic)
	return ok && (basic.Kind() == types.String || basic.Kind() == types.UntypedString)
}

func primitiveCarrierTypeName(typ types.Type) (string, bool) {
	if typ == nil {
		return "", false
	}
	basic, ok := typ.Underlying().(*types.Basic)
	if !ok {
		return "", false
	}
	switch basic.Kind() {
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
		types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64:
		return basic.Name(), true
	default:
		return "", false
	}
}

func messageFieldName(name string) bool {
	return canonicalName(name) == "message"
}

func warningStringsFieldName(name string) bool {
	canonical := canonicalName(name)
	return canonical == "warning" || canonical == "warnings"
}

func isMessageBearingStructName(name string) bool {
	canonical := canonicalName(name)
	return strings.Contains(canonical, "issue") ||
		strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "warning") ||
		strings.Contains(canonical, "validation") ||
		strings.Contains(canonical, "refactor")
}

func astStructHasField(strct *ast.StructType, fieldName string) bool {
	for _, field := range strct.Fields.List {
		for _, name := range field.Names {
			if name != nil && canonicalName(name.Name) == canonicalName(fieldName) {
				return true
			}
		}
	}
	return false
}

func astStructHasContractSignal(strct *ast.StructType) bool {
	for _, field := range strct.Fields.List {
		for _, name := range field.Names {
			if name == nil {
				continue
			}
			canonical := canonicalName(name.Name)
			if canonical == "code" ||
				strings.HasSuffix(canonical, "code") ||
				canonical == "severity" ||
				canonical == "path" ||
				canonical == "pageid" {
				return true
			}
		}
	}
	return false
}

func astStructIsMessageBearing(typeName string, strct *ast.StructType) bool {
	return isMessageBearingStructName(typeName) || astStructHasContractSignal(strct)
}

func namedStructIsMessageBearing(named *types.Named, strct *types.Struct) bool {
	if named == nil || strct == nil {
		return false
	}
	if isMessageBearingStructName(named.Obj().Name()) {
		return true
	}
	for i := 0; i < strct.NumFields(); i++ {
		canonical := canonicalName(strct.Field(i).Name())
		if canonical == "code" ||
			strings.HasSuffix(canonical, "code") ||
			canonical == "severity" ||
			canonical == "path" ||
			canonical == "pageid" {
			return true
		}
	}
	return false
}

func namedStructHasMessageID(named *types.Named, strct *types.Struct) bool {
	if named == nil || strct == nil {
		return false
	}
	for i := 0; i < strct.NumFields(); i++ {
		if canonicalName(strct.Field(i).Name()) == "messageid" {
			return true
		}
	}
	return false
}

func enclosingNamedCompositeStruct(ctx *analysisContext, node ast.Node) (*types.Named, *types.Struct, bool) {
	named, strct, _, ok := enclosingNamedCompositeStructLiteral(ctx, node)
	return named, strct, ok
}

func enclosingNamedCompositeStructLiteral(ctx *analysisContext, node ast.Node) (*types.Named, *types.Struct, *ast.CompositeLit, bool) {
	for current := node; current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.CompositeLit:
			typ := ctx.pass.TypesInfo.TypeOf(n)
			if ptr, ok := typ.(*types.Pointer); ok {
				typ = ptr.Elem()
			}
			named, ok := typ.(*types.Named)
			if !ok {
				return nil, nil, nil, false
			}
			strct, ok := named.Underlying().(*types.Struct)
			if !ok {
				return nil, nil, nil, false
			}
			return named, strct, n, true
		case *ast.FuncDecl:
			return nil, nil, nil, false
		}
	}
	return nil, nil, nil, false
}

func isTestFile(filename string) bool {
	return strings.HasSuffix(filename, "_test.go")
}

func isAllowedTestDescriptionLiteral(ctx *analysisContext, lit *ast.BasicLit) bool {
	call, index, ok := directCallArg(ctx, lit)
	if !ok {
		return false
	}
	name := callName(call)
	return (index == 0 && isBDDDescriptionCall(name)) ||
		(index > 0 && isGomegaAnnotationCall(name))
}

func isStableTestContractLiteral(ctx *analysisContext, lit *ast.BasicLit, value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	if isLeafWikiTrailerProtocolLiteral(value) {
		return isTestAssertionLiteralContext(ctx, lit) ||
			isTestTrailerIndexLiteralContext(ctx, lit) ||
			isTestContractLiteralContext(ctx, lit)
	}
	return isStableMessageLikeLiteral(value) && isTestContractLiteralContext(ctx, lit)
}

func isTestAssertionLiteralContext(ctx *analysisContext, lit *ast.BasicLit) bool {
	for current := ast.Node(lit); current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.CallExpr:
			if isGomegaAssertionMethod(callName(n)) {
				return true
			}
		case *ast.FuncDecl:
			return false
		}
	}
	return false
}

func isTestTrailerIndexLiteralContext(ctx *analysisContext, lit *ast.BasicLit) bool {
	for current := ast.Node(lit); current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.IndexExpr:
			return containsNode(n.Index, lit)
		case *ast.FuncDecl:
			return false
		}
	}
	return false
}

func isTestContractLiteralContext(ctx *analysisContext, lit *ast.BasicLit) bool {
	for current := ast.Node(lit); current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.CallExpr:
			name := callName(n)
			if isBDDDescriptionCall(name) {
				return isBDDContractDataLiteral(ctx, n, lit)
			}
			if isTestAssertionMatcherContractContext(ctx, n) ||
				isTestSemanticAssertionHelper(name) ||
				isTestContractAssertionCall(ctx, n) {
				return true
			}
		case *ast.FuncDecl:
			return false
		}
	}
	return false
}

func isTestLocalizedProseContractLiteralContext(ctx *analysisContext, lit *ast.BasicLit) bool {
	for current := ast.Node(lit); current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.CallExpr:
			name := callName(n)
			if isBDDDescriptionCall(name) {
				return isBDDRenderedProseDataLiteral(ctx, n, lit)
			}
			if isTestAssertionMatcherLocalizedProseContext(ctx, n) ||
				isTestSemanticAssertionHelper(name) ||
				isTestContractAssertionCall(ctx, n) {
				return true
			}
		case *ast.KeyValueExpr:
			if isTestRenderedProseFieldLiteral(ctx, n, lit) {
				return true
			}
		case *ast.FuncDecl:
			return false
		}
	}
	return false
}

func isTestRenderedProseFieldLiteral(ctx *analysisContext, kv *ast.KeyValueExpr, lit *ast.BasicLit) bool {
	if !containsNode(kv.Value, lit) || !nameSuggestsTestRenderedProseSubject(keyName(kv.Key)) {
		return false
	}
	return isTestAssertionLiteralContext(ctx, lit)
}

func directCallArg(ctx *analysisContext, lit *ast.BasicLit) (*ast.CallExpr, int, bool) {
	call, ok := ctx.parent(lit).(*ast.CallExpr)
	if !ok {
		return nil, 0, false
	}
	index, ok := directArgIndex(call, lit)
	if !ok {
		return nil, 0, false
	}
	return call, index, true
}

func directArgIndex(call *ast.CallExpr, lit *ast.BasicLit) (int, bool) {
	for i, arg := range call.Args {
		if arg == lit {
			return i, true
		}
	}
	return 0, false
}

func isBDDContractDataLiteral(ctx *analysisContext, call *ast.CallExpr, lit *ast.BasicLit) bool {
	if !isBDDEntryCall(callName(call)) {
		return false
	}
	if keyName, ok := bddEntryDataKeyName(ctx, call, lit); ok {
		return testTableParamSuggestsContract(keyName)
	}
	index, ok := bddEntryDataArgIndex(ctx, call, lit)
	if !ok || index == 0 {
		return false
	}
	paramName, ok := bddEntryTableParamName(ctx, call, index-1)
	if !ok {
		return false
	}
	return testTableParamSuggestsContract(paramName)
}

func isBDDRenderedProseDataLiteral(ctx *analysisContext, call *ast.CallExpr, lit *ast.BasicLit) bool {
	if !isBDDEntryCall(callName(call)) {
		return false
	}
	if keyName, ok := bddEntryDataKeyName(ctx, call, lit); ok {
		return testTableParamSuggestsRenderedProseContract(keyName)
	}
	index, ok := bddEntryDataArgIndex(ctx, call, lit)
	if !ok || index == 0 {
		return false
	}
	paramName, ok := bddEntryTableParamName(ctx, call, index-1)
	if !ok {
		return false
	}
	return testTableParamSuggestsRenderedProseContract(paramName)
}

func bddEntryDataKeyName(ctx *analysisContext, call *ast.CallExpr, lit *ast.BasicLit) (string, bool) {
	key, ok := enclosingKeyValueWithin(ctx, lit, call)
	if !ok {
		return "", false
	}
	return exprName(key.Key), true
}

func enclosingKeyValueWithin(ctx *analysisContext, node ast.Node, stop ast.Node) (*ast.KeyValueExpr, bool) {
	for current := ast.Node(node); current != nil && current != stop; current = ctx.parent(current) {
		key, ok := current.(*ast.KeyValueExpr)
		if ok {
			return key, true
		}
	}
	return nil, false
}

func bddEntryDataArgIndex(ctx *analysisContext, call *ast.CallExpr, node ast.Node) (int, bool) {
	child := directChildWithin(ctx, node, call)
	if child == nil {
		return 0, false
	}
	for i, arg := range call.Args {
		if arg == child {
			return i, true
		}
	}
	return 0, false
}

func directChildWithin(ctx *analysisContext, node ast.Node, parent ast.Node) ast.Node {
	current := ast.Node(node)
	for current != nil {
		next := ctx.parent(current)
		if next == parent {
			return current
		}
		current = next
	}
	return nil
}

func isBDDEntryCall(name string) bool {
	switch name {
	case "Entry", "FEntry", "PEntry", "XEntry":
		return true
	default:
		return false
	}
}

func isBDDDescriptionCall(name string) bool {
	switch name {
	case "Describe", "Context", "When", "It", "Specify", "DescribeTable", "Entry",
		"FDescribe", "FContext", "FWhen", "FIt", "FSpecify", "FDescribeTable", "FEntry",
		"PDescribe", "PContext", "PWhen", "PIt", "PSpecify", "PDescribeTable", "PEntry",
		"XDescribe", "XContext", "XWhen", "XIt", "XSpecify", "XDescribeTable", "XEntry",
		"By", "Label", "EntryDescription":
		return true
	default:
		return false
	}
}

func bddEntryTableParamName(ctx *analysisContext, entry *ast.CallExpr, dataIndex int) (string, bool) {
	table, ok := enclosingDescribeTableCall(ctx, entry)
	if !ok {
		return "", false
	}
	body, ok := describeTableBody(table)
	if !ok || body.Type.Params == nil {
		return "", false
	}
	current := 0
	for _, field := range body.Type.Params.List {
		for _, name := range field.Names {
			if name == nil {
				continue
			}
			if current == dataIndex {
				return name.Name, true
			}
			current++
		}
	}
	return "", false
}

func enclosingDescribeTableCall(ctx *analysisContext, entry *ast.CallExpr) (*ast.CallExpr, bool) {
	for current := ctx.parent(entry); current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.CallExpr:
			if isDescribeTableCall(callName(n)) {
				return n, true
			}
		case *ast.FuncDecl:
			return nil, false
		}
	}
	return nil, false
}

func isDescribeTableCall(name string) bool {
	switch name {
	case "DescribeTable", "FDescribeTable", "PDescribeTable", "XDescribeTable":
		return true
	default:
		return false
	}
}

func describeTableBody(call *ast.CallExpr) (*ast.FuncLit, bool) {
	for _, arg := range call.Args {
		body, ok := arg.(*ast.FuncLit)
		if ok {
			return body, true
		}
	}
	return nil, false
}

func isGomegaAnnotationCall(name string) bool {
	switch name {
	case "To", "NotTo", "ToNot", "Should", "ShouldNot", "Error":
		return true
	default:
		return false
	}
}

func isTestAssertionMatcherCall(name string) bool {
	switch name {
	case "Equal", "ContainSubstring", "HavePrefix", "HaveSuffix", "HaveKeyWithValue", "HaveField", "MatchError":
		return true
	default:
		return false
	}
}

func isTestAssertionMatcherContractContext(ctx *analysisContext, matcher *ast.CallExpr) bool {
	if !isTestAssertionMatcherCall(callName(matcher)) {
		return false
	}
	if matcherCallHasStructuredProtocolKey(matcher) {
		return true
	}
	for current := ctx.parent(matcher); current != nil; current = ctx.parent(current) {
		call, ok := current.(*ast.CallExpr)
		if !ok {
			if _, ok := current.(*ast.FuncDecl); ok {
				return false
			}
			continue
		}
		if !isGomegaAssertionMethod(callName(call)) {
			continue
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		expectCall, ok := selector.X.(*ast.CallExpr)
		if !ok || !isGomegaExpectCall(callName(expectCall)) || len(expectCall.Args) == 0 {
			return false
		}
		return exprSuggestsTestContract(expectCall.Args[0])
	}
	return false
}

func isTestAssertionMatcherLocalizedProseContext(ctx *analysisContext, matcher *ast.CallExpr) bool {
	if !isTestAssertionMatcherCall(callName(matcher)) {
		return false
	}
	if matcherCallHasRenderedProseProtocolKey(matcher) {
		return true
	}
	for current := ctx.parent(matcher); current != nil; current = ctx.parent(current) {
		call, ok := current.(*ast.CallExpr)
		if !ok {
			if _, ok := current.(*ast.FuncDecl); ok {
				return false
			}
			continue
		}
		if !isGomegaAssertionMethod(callName(call)) {
			continue
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		expectCall, ok := selector.X.(*ast.CallExpr)
		if !ok || !isGomegaExpectCall(callName(expectCall)) || len(expectCall.Args) == 0 {
			return false
		}
		return exprSuggestsTestRenderedProseContract(expectCall.Args[0])
	}
	return false
}

func matcherCallHasStructuredProtocolKey(call *ast.CallExpr) bool {
	if !isMatcherNamed(call, "HaveKeyWithValue") || len(call.Args) == 0 {
		return false
	}
	key, ok := stringArgValue(call.Args[0])
	return ok && structuredProtocolKeyName(key)
}

func matcherCallHasRenderedProseProtocolKey(call *ast.CallExpr) bool {
	if !isMatcherNamed(call, "HaveKeyWithValue") || len(call.Args) == 0 {
		return false
	}
	key, ok := stringArgValue(call.Args[0])
	if !ok {
		return false
	}
	switch canonicalName(key) {
	case "error", "message":
		return true
	default:
		return false
	}
}

func stringArgValue(expr ast.Expr) (string, bool) {
	lit, ok := unparenExpr(expr).(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

func isGomegaAssertionMethod(name string) bool {
	switch name {
	case "To", "NotTo", "ToNot", "Should", "ShouldNot", "Error":
		return true
	default:
		return false
	}
}

func isGomegaExpectCall(name string) bool {
	switch name {
	case "Expect", "ExpectWithOffset", "Ω":
		return true
	default:
		return false
	}
}

func exprSuggestsTestContract(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		switch n := node.(type) {
		case *ast.Ident:
			found = nameSuggestsTestSubjectContract(n.Name)
		case *ast.SelectorExpr:
			found = nameSuggestsTestSubjectContract(n.Sel.Name)
		case *ast.CallExpr:
			found = nameSuggestsTestSubjectContract(callName(n))
		}
		return !found
	})
	return found
}

func exprSuggestsTestRenderedProseContract(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		switch n := node.(type) {
		case *ast.Ident:
			found = nameSuggestsTestRenderedProseSubject(n.Name)
		case *ast.SelectorExpr:
			found = nameSuggestsTestRenderedProseSubject(n.Sel.Name)
		case *ast.CallExpr:
			found = nameSuggestsTestRenderedProseSubject(callName(n))
		}
		return !found
	})
	return found
}

func nameSuggestsTestSubjectContract(name string) bool {
	canonical := canonicalName(name)
	if strings.Contains(canonical, "diagnostic") {
		return false
	}
	return canonical == "body" ||
		strings.Contains(canonical, "code") ||
		strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "message") ||
		strings.Contains(canonical, "messageid") ||
		strings.Contains(canonical, "toolid") ||
		strings.Contains(canonical, "issue") ||
		strings.Contains(canonical, "validation")
}

func nameSuggestsTestRenderedProseSubject(name string) bool {
	canonical := canonicalName(name)
	if strings.Contains(canonical, "diagnostic") {
		return false
	}
	return canonical == "err" ||
		canonical == "error" ||
		canonical == "message" ||
		strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "message") ||
		strings.Contains(canonical, "stderr") ||
		strings.Contains(canonical, "stdout")
}

func isTestContractAssertionCall(ctx *analysisContext, call *ast.CallExpr) bool {
	name := callName(call)
	if !strings.HasPrefix(name, "assert") && !strings.HasPrefix(name, "expect") {
		return false
	}
	return nameSuggestsTestContract(name)
}

func isTestSemanticAssertionHelper(name string) bool {
	if !strings.HasPrefix(name, "assert") && !strings.HasPrefix(name, "expect") {
		return false
	}
	return nameSuggestsTestContract(name)
}

func nameSuggestsTestContract(name string) bool {
	canonical := canonicalName(name)
	return strings.Contains(canonical, "structured") ||
		strings.Contains(canonical, "localized") ||
		strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "message") ||
		strings.Contains(canonical, "code") ||
		strings.Contains(canonical, "validation") ||
		strings.Contains(canonical, "issue") ||
		strings.Contains(canonical, "tool")
}

func semanticTypeForTestHelperParamName(paramName string, funcName string) (string, bool) {
	if typ, ok := semanticTypeForParamName(paramName, funcName); ok {
		return typ, true
	}
	canonicalParam := canonicalName(paramName)
	canonicalFunc := canonicalName(funcName)
	switch {
	case strings.Contains(canonicalParam, "messageid"):
		return "MessageID", true
	case strings.Contains(canonicalParam, "toolid") || strings.Contains(canonicalParam, "toolname"):
		return "ToolID", true
	case strings.Contains(canonicalParam, "code"):
		switch {
		case strings.Contains(canonicalFunc, "field"):
			return "FieldErrorCode", true
		case strings.Contains(canonicalFunc, "issue") || strings.Contains(canonicalFunc, "validationissue"):
			return "IssueCode", true
		default:
			return "ErrorCode", true
		}
	default:
		return "", false
	}
}

func testHelperMessageParamName(paramName string, funcName string) bool {
	if canonicalName(paramName) != "message" {
		return false
	}
	canonicalFunc := canonicalName(funcName)
	return strings.Contains(canonicalFunc, "structured") ||
		strings.Contains(canonicalFunc, "localized") ||
		strings.Contains(canonicalFunc, "error")
}

func testHelperFieldParamName(paramName string, funcName string) bool {
	if canonicalName(paramName) != "field" {
		return false
	}
	canonicalFunc := canonicalName(funcName)
	return strings.Contains(canonicalFunc, "field") ||
		strings.Contains(canonicalFunc, "validation") ||
		strings.Contains(canonicalFunc, "error")
}

func testTableParamSuggestsContract(paramName string) bool {
	if _, ok := semanticTypeForTestHelperParamName(paramName, ""); ok {
		return true
	}
	canonical := canonicalName(paramName)
	return strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "message")
}

func testTableParamSuggestsRenderedProseContract(paramName string) bool {
	canonical := canonicalName(paramName)
	if strings.Contains(canonical, "diagnostic") {
		return false
	}
	return strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "message") ||
		strings.Contains(canonical, "stderr") ||
		strings.Contains(canonical, "stdout")
}

func isGeneratedOrVendored(filename string) bool {
	return strings.Contains(filename, "/vendor/") || strings.Contains(filename, "/node_modules/")
}

func isTestOrGeneratedFile(filename string) bool {
	return isTestFile(filename) || isGeneratedOrVendored(filename)
}

func isWireDTOFile(filename string) bool {
	return strings.Contains(filename, "/internal/http/dto/")
}

func isMCPWireFile(filename string) bool {
	return strings.Contains(filename, "/internal/wiki/mcp/types.go")
}

func isMarkdownSerializationFile(filename string) bool {
	return strings.Contains(filename, "/internal/core/markdown/metadata.go") ||
		strings.Contains(filename, "/internal/core/markdown/metadata_codec.go")
}

func isTestSupportFile(filename string) bool {
	return strings.Contains(filename, "/internal/test_utils/")
}

func isTestMatcherSupportFile(filename string) bool {
	return strings.Contains(filename, "/internal/test_utils/matchers/")
}

func isPersistenceAdapterFile(filename string) bool {
	base := filepath.Base(filename)
	return strings.HasSuffix(base, "_store.go") ||
		strings.Contains(filename, "/internal/core/revision/fs_store.go")
}

func isEdgeAdapterFile(filename string) bool {
	switch {
	case strings.HasSuffix(filename, "/internal/wiki/import_adapter.go"):
		return true
	case strings.HasSuffix(filename, "/internal/core/tree/migration_adapter.go"):
		return true
	case strings.HasSuffix(filename, "/internal/workspacesync/watcher_adapter.go"):
		return true
	case strings.HasSuffix(filename, "/internal/analysis/semantichygiene/testdata/semanticcases/migration_adapter.go"):
		return true
	default:
		return false
	}
}

func isSemanticOwnerAdapterFile(ctx *analysisContext, pos token.Pos) bool {
	filename := ctx.filename(pos)
	switch ctx.pass.Pkg.Path() {
	case "github.com/perber/wiki/internal/agenthooks":
		return strings.HasSuffix(filename, "/internal/agenthooks/agenthooks.go")
	case "github.com/perber/wiki/internal/core/identity":
		return strings.HasSuffix(filename, "/internal/core/identity/semantic_types.go")
	case "github.com/perber/wiki/internal/core/tree":
		return strings.HasSuffix(filename, "/internal/core/tree/semantic_types.go")
	case "github.com/perber/wiki/internal/core/auth":
		return strings.HasSuffix(filename, "/internal/core/auth/semantic_types.go")
	case "github.com/perber/wiki/internal/core/revision":
		return strings.HasSuffix(filename, "/internal/core/revision/semantic_types.go")
	case "github.com/perber/wiki/internal/core/markdownvalidation":
		return strings.HasSuffix(filename, "/internal/core/markdownvalidation/issue_codes.go")
	case "github.com/perber/wiki/internal/workspacesync":
		return strings.HasSuffix(filename, "/internal/workspacesync/semantic_types.go")
	case "github.com/perber/wiki/internal/workspaceid":
		return strings.HasSuffix(filename, "/internal/workspaceid/validate.go")
	case "github.com/perber/wiki/internal/analysis/semantichygiene/testdata/semanticcases":
		return strings.HasSuffix(filename, "/internal/analysis/semantichygiene/testdata/semanticcases/path_adapters.go") ||
			strings.HasSuffix(filename, "/internal/analysis/semantichygiene/testdata/semanticcases/owner_adapters.go")
	default:
		return false
	}
}

var allowedSemanticOwnerAdapterFuncs = map[string]map[string]bool{
	"ActorID":                         {"UserID": true},
	"Child":                           {"RoutePath": true, "Slug": true},
	"Clean":                           {"AssetName": true, "MarkdownPath": true, "RoutePath": true, "WorkspaceSourcePath": true},
	"CleanMarkdownPath":               {"MarkdownPath": true},
	"CleanWorkspaceSourcePath":        {"WorkspaceSourcePath": true},
	"CommitID":                        {"RevisionID": true},
	"Dir":                             {"MarkdownPath": true, "WorkspaceSourcePath": true},
	"EqualFold":                       {"Slug": true},
	"Ext":                             {"MarkdownPath": true},
	"FilesystemPath":                  {"MarkdownPath": true, "RoutePath": true, "Slug": true, "WorkspaceSourcePath": true},
	"Filename":                        {"AssetName": true},
	"HashPayload":                     {"PageID": true, "Slug": true, "UserID": true},
	"HrefPath":                        {"MarkdownPath": true, "RoutePath": true},
	"HTTPHeaderValue":                 {"WorkspaceID": true},
	"IsMarkdown":                      {"MarkdownPath": true},
	"LeafSlug":                        {"RoutePath": true, "Slug": true},
	"LowerKey":                        {"RoutePath": true},
	"MarkdownContentPath":             {"MarkdownPath": true, "RoutePath": true},
	"MarkdownPagePath":                {"MarkdownPath": true, "RoutePath": true},
	"MetadataValue":                   {"PageID": true, "UserID": true},
	"NewAPIKeyIDUnchecked":            {"APIKeyID": true},
	"NewAssetNameUnchecked":           {"AssetName": true},
	"NewMarkdownPathUnchecked":        {"MarkdownPath": true},
	"NewCommitHashUnchecked":          {"CommitHash": true},
	"NewPageIDUnchecked":              {"PageID": true},
	"NewPageVersionFromTime":          {"PageVersion": true},
	"NewPageVersionUnchecked":         {"PageVersion": true},
	"NewRevisionIDUnchecked":          {"RevisionID": true},
	"NewRoutePathUnchecked":           {"RoutePath": true},
	"NewSessionIDUnchecked":           {"SessionID": true},
	"NewSlugUnchecked":                {"Slug": true},
	"NewUserIDUnchecked":              {"UserID": true},
	"NewWorkspaceSourcePathUnchecked": {"WorkspaceSourcePath": true},
	"HasMCPPrefix":                    {"AgentToolName": true},
	"Normalize":                       {"AgentEventName": true, "AgentToolName": true, "IssueCode": true, "IssueSeverity": true, "ProviderID": true},
	"ParseWorkspaceID":                {"WorkspaceID": true},
	"RoutePath":                       {"MarkdownPath": true, "RoutePath": true},
	"Scan":                            {"PageID": true, "WorkspaceID": true},
	"Segments":                        {"RoutePath": true, "Slug": true},
	"SanitizedMetadataValue":          {"AgentToolName": true},
	"SlugKey":                         {"Slug": true},
	"SourceDir":                       {"MarkdownPath": true},
	"StorageKey":                      {"WorkspaceID": true},
	"URLPathSegment":                  {"WorkspaceID": true},
	"Validate":                        {"RoutePath": true, "Slug": true, "WorkspaceID": true},
	"ValidateWorkspaceID":             {"WorkspaceID": true},
	"Value":                           {"PageID": true, "WorkspaceID": true},
	"WikiPath":                        {"RoutePath": true},
	"WithLeafSlug":                    {"RoutePath": true, "Slug": true},
	"WorkspaceSourceDirectory":        {"RoutePath": true, "WorkspaceSourcePath": true},
	"WorkspaceSourcePath":             {"RoutePath": true, "WorkspaceSourcePath": true},
}

func isAllowedSemanticOwnerAdapterFunc(ctx *analysisContext, node ast.Node, typeName string) bool {
	if !isSemanticOwnerAdapterFile(ctx, node.Pos()) {
		return false
	}
	fn := enclosingFunc(ctx, node)
	if fn == nil || !allowedSemanticOwnerAdapterFuncs[fn.Name.Name][typeName] {
		return false
	}
	return functionReturnsSemanticType(ctx, fn, typeName) ||
		functionHasSemanticReceiver(ctx, fn, typeName) ||
		functionHasSemanticParameter(ctx, fn, typeName)
}

func isAllowedStringBoundaryFile(filename string) bool {
	return isGeneratedOrVendored(filename) || isTestMatcherSupportFile(filename)
}

func isAllowedDirectCastFile(filename string) bool {
	return isGeneratedOrVendored(filename)
}

func isAllowedDirectCastContext(ctx *analysisContext, call *ast.CallExpr, typeName string) bool {
	filename := ctx.filename(call.Pos())
	if isAllowedDirectCastFile(filename) {
		return true
	}
	if isAllowedTestLiteralDirectCastContext(filename, call) {
		return true
	}
	if isAllowedFixtureDirectCastContext(filename, enclosingFuncName(ctx, call)) {
		return true
	}
	if isAllowedEdgeAdapterDirectCastContext(ctx, call) {
		return true
	}
	if isAllowedSemanticConstructorContext(ctx, call, typeName) {
		return true
	}
	if isAllowedSemanticOwnerAdapterFunc(ctx, call, typeName) {
		return true
	}
	return isConstOrTypeDefinition(ctx, call)
}

func isAllowedTestLiteralDirectCastContext(filename string, call *ast.CallExpr) bool {
	if !isRepoTestBoundaryFile(filename) || len(call.Args) != 1 {
		return false
	}
	_, ok := call.Args[0].(*ast.BasicLit)
	return ok
}

func isRepoTestBoundaryFile(filename string) bool {
	return isTestFile(filename) && !isSemanticHygienePolicyFixtureFile(filename)
}

func isSemanticHygienePolicyFixtureFile(filename string) bool {
	return strings.Contains(filename, "/internal/analysis/semantichygiene/testdata/semanticcases/")
}

func isAllowedFixtureDirectCastContext(filename string, funcName string) bool {
	if !(isTestFile(filename) || strings.Contains(filename, "/e2e/") || strings.Contains(filename, "/testdata/")) {
		return false
	}
	canonicalFuncName := canonicalName(funcName)
	return strings.HasPrefix(canonicalFuncName, "buildfixture") ||
		strings.HasPrefix(canonicalFuncName, "makefixture") ||
		strings.HasPrefix(canonicalFuncName, "newfixture") ||
		strings.HasSuffix(canonicalFuncName, "fixture")
}

func isAllowedEdgeAdapterDirectCastContext(ctx *analysisContext, call *ast.CallExpr) bool {
	if !isEdgeAdapterFile(ctx.filename(call.Pos())) {
		return false
	}
	return enclosingFuncName(ctx, call) == "SetMetadata"
}

type directCastAllowKey struct {
	packagePath string
	funcName    string
	typeName    string
}

var allowedDirectCastFunctions = map[directCastAllowKey]bool{
	{"github.com/perber/wiki/internal/analysis/semantichygiene/testdata/semanticcases", "ParsePageID", "PageID"}:                           true,
	{"github.com/perber/wiki/internal/analysis/semantichygiene/testdata/semanticcases", "ParseRoutePath", "RoutePath"}:                     true,
	{"github.com/perber/wiki/internal/analysis/semantichygiene/testdata/semanticcases", "MessageIDForCode", "MessageID"}:                   true,
	{"github.com/perber/wiki/internal/analysis/semantichygiene/testdata/semanticcases", "MessageIDForTrimmedCode", "MessageID"}:            true,
	{"github.com/perber/wiki/internal/analysis/semantichygiene/testdata/semanticcases", "MessageIDForCodeViaLocalTrim", "MessageID"}:       true,
	{"github.com/perber/wiki/internal/analysis/semantichygiene/testdata/semanticcases", "MessageIDForCodeViaNestedLocalTrim", "MessageID"}: true,
	{"github.com/perber/wiki/internal/core/shared/errors", "MessageIDForCode", "MessageID"}:                                                true,
	{"github.com/perber/wiki/internal/core/tree", "NewPageIDUnchecked", "PageID"}:                                                          true,
	{"github.com/perber/wiki/internal/core/tree", "NewRoutePathUnchecked", "RoutePath"}:                                                    true,
	{"github.com/perber/wiki/internal/core/tree", "NewSlugUnchecked", "Slug"}:                                                              true,
	{"github.com/perber/wiki/internal/core/tree", "NewWorkspaceSourcePathUnchecked", "WorkspaceSourcePath"}:                                true,
	{"github.com/perber/wiki/internal/core/tree", "ParseRoutePath", "RoutePath"}:                                                           true,
	{"github.com/perber/wiki/internal/core/tree", "ParseSlug", "Slug"}:                                                                     true,
	{"github.com/perber/wiki/internal/core/tree", "ValidateRoutePath", "RoutePath"}:                                                        true,
	{"github.com/perber/wiki/internal/wiki/mcp", "ToolDescriptionIDForTool", "ToolDescriptionID"}:                                          true,
	{"github.com/perber/wiki/internal/workspaceid", "ParseWorkspaceID", "WorkspaceID"}:                                                     true,
	{"github.com/perber/wiki/internal/workspaceid", "ValidateWorkspaceID", "WorkspaceID"}:                                                  true,
}

func isAllowedSemanticConstructorContext(ctx *analysisContext, call *ast.CallExpr, typeName string) bool {
	return isAllowedSemanticConstructorFunction(ctx, call, typeName)
}

func isAllowedSemanticConstructorFunction(ctx *analysisContext, node ast.Node, typeName string) bool {
	fn := enclosingFunc(ctx, node)
	if fn == nil || !functionReturnsSemanticType(ctx, fn, typeName) {
		return false
	}
	key := directCastAllowKey{
		packagePath: ctx.pass.Pkg.Path(),
		funcName:    fn.Name.Name,
		typeName:    typeName,
	}
	return allowedDirectCastFunctions[key]
}

func functionReturnsSemanticType(ctx *analysisContext, fn *ast.FuncDecl, typeName string) bool {
	if fn.Type.Results == nil {
		return false
	}
	for _, result := range fn.Type.Results.List {
		if typeContainsSemanticType(ctx.pass.TypesInfo.TypeOf(result.Type), typeName) {
			return true
		}
	}
	return false
}

func typeContainsSemanticType(typ types.Type, typeName string) bool {
	if resultTypeName, ok := semanticTypeNameOf(typ); ok && resultTypeName == typeName {
		return true
	}
	if typ == nil {
		return false
	}
	switch underlying := typ.Underlying().(type) {
	case *types.Slice:
		return typeContainsSemanticType(underlying.Elem(), typeName)
	case *types.Array:
		return typeContainsSemanticType(underlying.Elem(), typeName)
	default:
		return false
	}
}

func functionHasSemanticReceiver(ctx *analysisContext, fn *ast.FuncDecl, typeName string) bool {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return false
	}
	recvTypeName, ok := semanticTypeNameOf(ctx.pass.TypesInfo.TypeOf(fn.Recv.List[0].Type))
	return ok && recvTypeName == typeName
}

func functionHasSemanticParameter(ctx *analysisContext, fn *ast.FuncDecl, typeName string) bool {
	if fn.Type.Params == nil {
		return false
	}
	for _, param := range fn.Type.Params.List {
		paramTypeName, ok := semanticTypeNameOf(ctx.pass.TypesInfo.TypeOf(param.Type))
		if ok && paramTypeName == typeName {
			return true
		}
	}
	return false
}

func semanticConstructorAllowsSource(targetTypeName string, sourceTypeName string) bool {
	if targetTypeName == "" || sourceTypeName == "" {
		return false
	}
	allowedSources := map[string]map[string]bool{
		"MessageID": {
			"ErrorCode":      true,
			"FieldErrorCode": true,
			"IssueCode":      true,
		},
		"ToolDescriptionID": {
			"ToolID": true,
		},
	}
	return allowedSources[targetTypeName][sourceTypeName]
}

func isConstOrTypeDefinition(ctx *analysisContext, node ast.Node) bool {
	for current := node; current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.GenDecl:
			return n.Tok == token.CONST || n.Tok == token.TYPE
		case *ast.FuncDecl:
			return false
		}
	}
	return false
}

func inJSONCompositeLiteral(ctx *analysisContext, node ast.Node) bool {
	var keyName string
	for current := node; current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.KeyValueExpr:
			keyName = exprName(n.Key)
		case *ast.CompositeLit:
			if keyName == "" {
				return false
			}
			return compositeFieldHasWireTag(ctx, n, keyName)
		case *ast.FuncDecl:
			return false
		}
	}
	return false
}

func compositeFieldHasWireTag(ctx *analysisContext, lit *ast.CompositeLit, fieldName string) bool {
	typ := ctx.pass.TypesInfo.TypeOf(lit)
	if ptr, ok := typ.(*types.Pointer); ok {
		typ = ptr.Elem()
	}
	named, ok := typ.(*types.Named)
	if !ok {
		return false
	}
	if !isAllowedWireComposite(ctx, lit, named.Obj().Name(), named.Obj().Pos()) {
		return false
	}
	strct, ok := named.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := 0; i < strct.NumFields(); i++ {
		field := strct.Field(i)
		if field.Name() == fieldName {
			tag := strct.Tag(i)
			return strings.Contains(tag, `json:"`) ||
				strings.Contains(tag, `yaml:"`) ||
				strings.Contains(tag, `toml:"`)
		}
	}
	return false
}

func isAllowedWireComposite(ctx *analysisContext, lit *ast.CompositeLit, typeName string, typePos token.Pos) bool {
	filename := ctx.filename(lit.Pos())
	typeFilename := ctx.filename(typePos)
	return isTestOrGeneratedFile(filename) ||
		isWireDTOFile(filename) ||
		isMCPWireFile(typeFilename) ||
		isMarkdownSerializationFile(typeFilename) ||
		isDTOTypeName(typeName)
}

func isPersistenceRowTypeName(typeName string) bool {
	canonical := canonicalName(typeName)
	return strings.HasSuffix(canonical, "row") ||
		strings.HasSuffix(canonical, "record")
}

func isPersistenceRowStruct(ctx *analysisContext, spec *ast.TypeSpec) bool {
	return isPersistenceAdapterFile(ctx.filename(spec.Pos())) &&
		isPersistenceRowTypeName(spec.Name.Name)
}

func isAllowedPersistenceRowKeyValue(ctx *analysisContext, node ast.Node) bool {
	for current := node; current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.CompositeLit:
			return isPersistenceRowComposite(ctx, n)
		case *ast.FuncDecl:
			return false
		}
	}
	return false
}

func isPersistenceRowComposite(ctx *analysisContext, lit *ast.CompositeLit) bool {
	if !isPersistenceAdapterFile(ctx.filename(lit.Pos())) {
		return false
	}
	typ := ctx.pass.TypesInfo.TypeOf(lit)
	if ptr, ok := typ.(*types.Pointer); ok {
		typ = ptr.Elem()
	}
	named, ok := typ.(*types.Named)
	return ok && isPersistenceRowTypeName(named.Obj().Name())
}

func isEmptyOrRootString(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return false
	}
	return value == "" || value == "root"
}

func isAllowedSignatureFile(filename string) bool {
	return isGeneratedOrVendored(filename) ||
		isRepoTestBoundaryFile(filename) ||
		isWireDTOFile(filename) ||
		isMCPWireFile(filename) ||
		isMarkdownSerializationFile(filename) ||
		isTestSupportFile(filename)
}

func isAllowedStructFieldFile(filename string) bool {
	return isGeneratedOrVendored(filename) ||
		isTestSupportFile(filename)
}

func isTestFixtureStructName(name string) bool {
	canonical := canonicalName(name)
	return strings.Contains(canonical, "fixture") ||
		strings.Contains(canonical, "stub") ||
		strings.Contains(canonical, "mock") ||
		strings.Contains(canonical, "fake") ||
		strings.Contains(canonical, "wire") ||
		strings.Contains(canonical, "dto") ||
		strings.Contains(canonical, "request") ||
		strings.Contains(canonical, "response")
}

func isDTOTypeName(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "dto") ||
		strings.Contains(lower, "request") ||
		strings.Contains(lower, "response") ||
		strings.Contains(lower, "output") ||
		strings.Contains(lower, "payload") ||
		strings.Contains(lower, "wire")
}

func isAllowedWireStructField(ctx *analysisContext, spec *ast.TypeSpec, field *ast.Field) bool {
	filename := ctx.filename(spec.Pos())
	if !(isDTOTypeName(spec.Name.Name) ||
		isWireDTOFile(filename) ||
		isMCPWireFile(filename) ||
		isMarkdownSerializationFile(filename)) {
		return false
	}
	return fieldHasWireTag(field)
}

func fieldHasWireTag(field *ast.Field) bool {
	if field.Tag == nil {
		return false
	}
	return strings.Contains(field.Tag.Value, `json:"`) ||
		strings.Contains(field.Tag.Value, `yaml:"`) ||
		strings.Contains(field.Tag.Value, `toml:"`)
}

func isStableLiteralAllowed(ctx *analysisContext, lit *ast.BasicLit) bool {
	filename := ctx.filename(lit.Pos())
	if isTestFile(filename) ||
		strings.Contains(filename, "/internal/localization/") ||
		strings.Contains(filename, "/docs/") ||
		strings.HasSuffix(filename, ".json") {
		return true
	}
	return isConstOrTypeDefinition(ctx, lit)
}

func isLocalizedProseLiteralAllowed(ctx *analysisContext, lit *ast.BasicLit) bool {
	filename := ctx.filename(lit.Pos())
	if isTestFile(filename) ||
		isGeneratedOrVendored(filename) ||
		strings.Contains(filename, "/docs/") ||
		strings.HasSuffix(filename, ".json") {
		return true
	}
	return isConstOrTypeDefinition(ctx, lit)
}

func looksLikeLocalizedProse(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || messageIDPattern.MatchString(value) || toolIDPattern.MatchString(value) || errorCodePattern.MatchString(value) {
		return false
	}
	return strings.ContainsAny(value, " \t\n")
}

var localizedProseContractCalls = map[string]bool{
	"apiSuccessMessage":          true,
	"abortWorkspaceSyncError":    true,
	"newMessageOutput":           true,
	"newToolDescriptor":          true,
	"Render":                     true,
	"writeControlError":          true,
	"writeFrontdError":           true,
	"writePrivateWorkspaceError": true,
	"writeRuntimeError":          true,
}

var localizedProseFirstArgContractCalls = map[string]bool{
	"fail":                 true,
	"failWithoutAgentHook": true,
}

var localizedProseConstructorCalls = map[string]bool{
	"NewLocalizedError":                     true,
	"NewLocalizedErrorFromCodeWithFallback": true,
	"NewLocalizedErrorDetail":               true,
	"NewFieldErrorWithCode":                 true,
	"AddWithCode":                           true,
}

var localizedProseStrictContractCalls = map[string]bool{
	"abortWorkspaceSyncError":    true,
	"writeControlError":          true,
	"writeFrontdError":           true,
	"writePrivateWorkspaceError": true,
}

var localizedProseSignatureSinkCalls = map[string]bool{
	"writeControlError":          true,
	"writeFrontdError":           true,
	"writePrivateWorkspaceError": true,
}

func isRawLocalizedProseContractLiteral(ctx *analysisContext, lit *ast.BasicLit) bool {
	for current := ast.Node(lit); current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.CallExpr:
			if isRawLocalizedProseCallLiteral(ctx, n, lit) {
				return true
			}
		case *ast.KeyValueExpr:
			if isRawLocalizedProseFieldLiteral(ctx, n, lit) {
				return true
			}
		case *ast.FuncDecl:
			return false
		}
	}
	return false
}

func isRawLocalizedProseCallLiteral(ctx *analysisContext, call *ast.CallExpr, lit *ast.BasicLit) bool {
	name := callName(call)
	if localizedProseFirstArgContractCalls[name] {
		return callContainsFirstArg(call, lit)
	}
	if localizedProseConstructorCalls[name] {
		return callContainsArg(call, lit) && localizedProseConstructorRequiresCatalogOnly(name, ctx, call)
	}
	if localizedProseContractCalls[name] || isCLIControlProseCall(ctx, call) || isPrivateHTTPErrorProseCall(ctx, call) {
		return callContainsArg(call, lit)
	}
	return false
}

func isRawLocalizedProseFieldLiteral(ctx *analysisContext, kv *ast.KeyValueExpr, lit *ast.BasicLit) bool {
	fieldName := keyName(kv.Key)
	if !containsNode(kv.Value, lit) {
		return false
	}
	if fieldName == "message" {
		return isResponsePayloadLiteral(ctx, kv)
	}
	if messageFieldName(fieldName) {
		return isMessageFieldValueMissingMessageID(ctx, kv)
	}
	return warningStringsFieldName(fieldName) && isWarningFieldValueMissingMessageID(ctx, kv)
}

func isCLIControlProseCall(ctx *analysisContext, call *ast.CallExpr) bool {
	if !isCLIControlProseFile(ctx.filename(call.Pos())) {
		return false
	}
	packagePath, name := calleePackageAndName(ctx, call)
	if packagePath != "fmt" {
		return false
	}
	switch name {
	case "Printf", "Println":
		return true
	case "Fprint", "Fprintf", "Fprintln":
		return len(call.Args) > 0 && isCLIOutputWriterExpr(call.Args[0])
	case "Errorf":
		return isCLIControlErrorfContext(ctx, call)
	default:
		return false
	}
}

func isCLIOutputWriterExpr(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return exprName(sel.X) == "os" && (sel.Sel.Name == "Stdout" || sel.Sel.Name == "Stderr")
}

func isCLIControlErrorfContext(ctx *analysisContext, call *ast.CallExpr) bool {
	fnName := canonicalName(enclosingFuncName(ctx, call))
	return strings.Contains(fnName, "validatemcptransport") ||
		strings.Contains(fnName, "mcptransportvalidation")
}

func isCLIControlProseFile(filename string) bool {
	return strings.HasSuffix(filename, "/cmd/leafwiki/main.go") ||
		isSemanticHygienePolicyFixtureFile(filename)
}

func isPrivateHTTPErrorProseCall(ctx *analysisContext, call *ast.CallExpr) bool {
	if !isPrivateHTTPResponseFile(ctx.filename(call.Pos())) {
		return false
	}
	packagePath, name := calleePackageAndName(ctx, call)
	return packagePath == "net/http" && name == "Error"
}

func isPrivateHTTPResponseFile(filename string) bool {
	return strings.Contains(filename, "/internal/wikid/") ||
		strings.Contains(filename, "/internal/projectdaemon/") ||
		strings.Contains(filename, "/internal/frontd/") ||
		isSemanticHygienePolicyFixtureFile(filename)
}

func isStrictLocalizedProseContractLiteral(ctx *analysisContext, lit *ast.BasicLit, value string) bool {
	if isStableMessageLikeLiteral(value) {
		return false
	}
	for current := ast.Node(lit); current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.CallExpr:
			if isStrictLocalizedProseCallLiteral(ctx, n, lit) {
				return true
			}
		case *ast.FuncDecl:
			return false
		}
	}
	return false
}

func isStableMessageLikeLiteral(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" ||
		messageIDPattern.MatchString(value) ||
		toolIDPattern.MatchString(value) ||
		errorCodePattern.MatchString(value)
}

func isStrictLocalizedProseCallLiteral(ctx *analysisContext, call *ast.CallExpr, lit *ast.BasicLit) bool {
	if localizedProseStrictContractCalls[callName(call)] || isPrivateHTTPErrorProseCall(ctx, call) {
		return callContainsArg(call, lit)
	}
	return false
}

func localizedProseConstructorRequiresCatalogOnly(name string, ctx *analysisContext, call *ast.CallExpr) bool {
	switch name {
	case "NewLocalizedError", "NewLocalizedErrorFromCodeWithFallback", "NewLocalizedErrorDetail", "NewFieldErrorWithCode", "AddWithCode":
		return true
	default:
		return callHasRawStableContractArg(ctx, call)
	}
}

func callHasRawStableContractArg(ctx *analysisContext, call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		lit, ok := arg.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			continue
		}
		value, err := strconv.Unquote(lit.Value)
		if err == nil && isStableContractLiteral(ctx, lit, value) {
			return true
		}
	}
	return false
}

func callContainsArg(call *ast.CallExpr, target ast.Node) bool {
	for _, arg := range call.Args {
		if containsNode(arg, target) {
			return true
		}
	}
	return false
}

func callContainsFirstArg(call *ast.CallExpr, target ast.Node) bool {
	return len(call.Args) > 0 && containsNode(call.Args[0], target)
}

func isResponsePayloadLiteral(ctx *analysisContext, node ast.Node) bool {
	for current := node; current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.CompositeLit:
			return exprName(n.Type) == "H" || isStringKeyedMap(ctx.pass.TypesInfo.TypeOf(n))
		case *ast.FuncDecl:
			return false
		}
	}
	return false
}

func isStringKeyedMap(typ types.Type) bool {
	if typ == nil {
		return false
	}
	m, ok := types.Unalias(typ).Underlying().(*types.Map)
	return ok && isString(m.Key())
}

var (
	errorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+){1,}$`)
	toolIDPattern    = regexp.MustCompile(`^wiki_[a-z0-9_]+$`)
	messageIDPattern = regexp.MustCompile(`^(errors|validation|api|mcp|cli|shell|ui)(\.[a-z0-9]+(?:_[a-z0-9]+)*)+$`)
	trailerPattern   = regexp.MustCompile(`^LeafWiki-[A-Za-z0-9-]+(?::.*)?$`)
)

func isLeafWikiTrailerProtocolLiteral(value string) bool {
	return trailerPattern.MatchString(strings.TrimSpace(value))
}

func isStableContractLiteral(ctx *analysisContext, lit *ast.BasicLit, value string) bool {
	return toolIDPattern.MatchString(value) ||
		messageIDPattern.MatchString(value) ||
		(errorCodePattern.MatchString(value) && stableLiteralContextSuggestsContract(ctx, lit))
}

func stableLiteralContextSuggestsContract(ctx *analysisContext, lit *ast.BasicLit) bool {
	for current := ast.Node(lit); current != nil; current = ctx.parent(current) {
		matches, terminal := stableLiteralContextDecision(ctx, current, lit)
		if matches || terminal {
			return matches
		}
	}
	return false
}

func stableLiteralContextDecision(ctx *analysisContext, node ast.Node, lit *ast.BasicLit) (matches bool, terminal bool) {
	switch n := node.(type) {
	case *ast.CallExpr:
		return nameSuggestsStableContract(callName(n)), false
	case *ast.KeyValueExpr:
		return nameSuggestsStableContract(keyName(n.Key)), false
	case *ast.AssignStmt:
		return assignStmtValueNameSuggestsStableContract(n, lit), false
	case *ast.ValueSpec:
		return valueSpecNameSuggestsStableContract(n, lit), false
	case *ast.ReturnStmt:
		return nameSuggestsStableContract(enclosingFuncName(ctx, n)), false
	case *ast.FuncDecl:
		return nameSuggestsStableContract(n.Name.Name), true
	default:
		return false, false
	}
}

func assignStmtValueNameSuggestsStableContract(stmt *ast.AssignStmt, lit *ast.BasicLit) bool {
	for i, rhs := range stmt.Rhs {
		if containsNode(rhs, lit) && i < len(stmt.Lhs) {
			return nameSuggestsStableContract(exprName(stmt.Lhs[i]))
		}
	}
	return false
}

func valueSpecNameSuggestsStableContract(spec *ast.ValueSpec, lit *ast.BasicLit) bool {
	for i, value := range spec.Values {
		if containsNode(value, lit) && i < len(spec.Names) {
			return nameSuggestsStableContract(spec.Names[i].Name)
		}
	}
	return false
}

func nameSuggestsStableContract(name string) bool {
	canonical := canonicalName(name)
	return canonical == "code" ||
		strings.HasSuffix(canonical, "code") ||
		strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "messageid") ||
		strings.Contains(canonical, "toolid") ||
		strings.Contains(canonical, "issue") ||
		strings.Contains(canonical, "validation")
}

func keyName(expr ast.Expr) string {
	if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		if value, err := strconv.Unquote(lit.Value); err == nil {
			return value
		}
	}
	return exprName(expr)
}

func calleePackageAndName(ctx *analysisContext, call *ast.CallExpr) (string, string) {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		if fn, ok := ctx.pass.TypesInfo.Uses[fun].(*types.Func); ok && fn.Pkg() != nil {
			return fn.Pkg().Path(), fun.Name
		}
		return "", fun.Name
	case *ast.SelectorExpr:
		if fn, ok := ctx.pass.TypesInfo.Uses[fun.Sel].(*types.Func); ok && fn.Pkg() != nil {
			return fn.Pkg().Path(), fun.Sel.Name
		}
		return "", fun.Sel.Name
	default:
		return "", ""
	}
}

func containsNode(root ast.Node, target ast.Node) bool {
	found := false
	ast.Inspect(root, func(node ast.Node) bool {
		if node == target {
			found = true
			return false
		}
		return !found
	})
	return found
}
