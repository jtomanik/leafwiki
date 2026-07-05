package semantichygiene

import (
	"go/ast"
	"strings"
)

func checkGinkgoSpecQualityCall(ctx *analysisContext, call *ast.CallExpr) {
	if !isTestFile(ctx.filename(call.Pos())) {
		return
	}
	name := callName(call)
	checkGinkgoTopLevelIt(ctx, call, name)
	checkGinkgoTestName(ctx, call, name)
	checkGinkgoCoverageName(ctx, call, name)
	checkGinkgoVagueName(ctx, call, name)
	checkGinkgoBooleanOutcomeName(ctx, call, name)
	checkGinkgoTestingTInSpec(ctx, call, name)
	checkGinkgoFailInSpec(ctx, call, name)
	checkGinkgoTaxonomyLabels(ctx, call, name)
	switch {
	case isFocusedGinkgoNodeName(name):
		ctx.report(ruleGinkgoFocus, call, ginkgoFocusDiagnostic())
	case isPendingGinkgoNodeName(name):
		ctx.report(ruleGinkgoPending, call, ginkgoPendingDiagnostic())
	}
	if isGinkgoContainerNodeName(name) {
		checkGinkgoContainerBody(ctx, call)
	}
	if isGinkgoRunnableNodeName(name) || isDescribeTableCall(name) {
		checkGinkgoDecoratorArgs(ctx, call)
	}
	if isBDDEntryCall(name) {
		checkGinkgoEntryPolicy(ctx, call)
	}
}

func checkGinkgoTopLevelIt(ctx *analysisContext, call *ast.CallExpr, name string) {
	if !isTopLevelItCandidateName(name) || !isGinkgoDSLCall(ctx, call) {
		return
	}
	if enclosingGinkgoContainerBlock(ctx, call) != nil {
		return
	}
	ctx.report(ruleGinkgoTopLevelIt, call, ginkgoTopLevelItDiagnostic())
}

func checkGinkgoTestName(ctx *analysisContext, call *ast.CallExpr, name string) {
	if !isGinkgoNameCarrier(name) || !isGinkgoDSLCall(ctx, call) || len(call.Args) == 0 {
		return
	}
	reportGinkgoTestName(ctx, call.Args[0])
	if isDescribeTableCall(name) {
		for _, arg := range call.Args[1:] {
			reportGinkgoTableEntryDescriptionName(ctx, arg)
		}
	}
}

func checkGinkgoCoverageName(ctx *analysisContext, call *ast.CallExpr, name string) {
	if !isGinkgoNameCarrier(name) || !isGinkgoDSLCall(ctx, call) || len(call.Args) == 0 {
		return
	}
	reportGinkgoCoverageName(ctx, call.Args[0])
	if isDescribeTableCall(name) {
		for _, arg := range call.Args[1:] {
			reportGinkgoTableEntryCoverageName(ctx, arg)
		}
	}
}

func checkGinkgoVagueName(ctx *analysisContext, call *ast.CallExpr, name string) {
	if !isGinkgoNameCarrier(name) || !isGinkgoDSLCall(ctx, call) || len(call.Args) == 0 {
		return
	}
	reportGinkgoVagueName(ctx, call.Args[0])
	if isDescribeTableCall(name) {
		for _, arg := range call.Args[1:] {
			reportGinkgoTableEntryVagueName(ctx, arg)
		}
	}
}

func checkGinkgoBooleanOutcomeName(ctx *analysisContext, call *ast.CallExpr, name string) {
	if !isGinkgoNameCarrier(name) || !isGinkgoDSLCall(ctx, call) || len(call.Args) == 0 {
		return
	}
	reportGinkgoBooleanOutcomeName(ctx, call.Args[0])
	if isDescribeTableCall(name) {
		for _, arg := range call.Args[1:] {
			reportGinkgoTableEntryBooleanOutcomeName(ctx, arg)
		}
	}
}

func reportGinkgoTestName(ctx *analysisContext, expr ast.Expr) {
	description, ok := ginkgoStaticDescription(expr)
	if !ok || !ginkgoDescriptionLooksMigratedTestName(description) {
		return
	}
	ctx.report(ruleGinkgoTestName, expr, ginkgoTestNameDiagnostic(description))
}

func reportGinkgoCoverageName(ctx *analysisContext, expr ast.Expr) {
	description, ok := ginkgoStaticDescription(expr)
	if !ok || !ginkgoDescriptionUsesCoverageBucket(description) {
		return
	}
	ctx.report(ruleGinkgoCoverageName, expr, ginkgoCoverageNameDiagnostic(description))
}

func reportGinkgoVagueName(ctx *analysisContext, expr ast.Expr) {
	description, ok := ginkgoStaticDescription(expr)
	if !ok || !ginkgoDescriptionIsVague(description) {
		return
	}
	ctx.report(ruleGinkgoVagueName, expr, ginkgoVagueNameDiagnostic(description))
}

func reportGinkgoBooleanOutcomeName(ctx *analysisContext, expr ast.Expr) {
	description, ok := ginkgoStaticDescription(expr)
	if !ok || !ginkgoDescriptionUsesBooleanOutcome(description) {
		return
	}
	ctx.report(ruleGinkgoBooleanOutcomeName, expr, ginkgoBooleanOutcomeNameDiagnostic(description))
}

func reportGinkgoTableEntryDescriptionName(ctx *analysisContext, expr ast.Expr) {
	description, ok := ginkgoEntryDescriptionValue(expr)
	if !ok || !ginkgoDescriptionLooksMigratedTestName(description) {
		return
	}
	ctx.report(ruleGinkgoTestName, expr, ginkgoTestNameDiagnostic(description))
}

func reportGinkgoTableEntryCoverageName(ctx *analysisContext, expr ast.Expr) {
	description, ok := ginkgoEntryDescriptionValue(expr)
	if !ok || !ginkgoDescriptionUsesCoverageBucket(description) {
		return
	}
	ctx.report(ruleGinkgoCoverageName, expr, ginkgoCoverageNameDiagnostic(description))
}

func reportGinkgoTableEntryVagueName(ctx *analysisContext, expr ast.Expr) {
	description, ok := ginkgoEntryDescriptionValue(expr)
	if !ok || !ginkgoDescriptionIsVague(description) {
		return
	}
	ctx.report(ruleGinkgoVagueName, expr, ginkgoVagueNameDiagnostic(description))
}

func reportGinkgoTableEntryBooleanOutcomeName(ctx *analysisContext, expr ast.Expr) {
	description, ok := ginkgoEntryDescriptionValue(expr)
	if !ok || !ginkgoDescriptionUsesBooleanOutcome(description) {
		return
	}
	ctx.report(ruleGinkgoBooleanOutcomeName, expr, ginkgoBooleanOutcomeNameDiagnostic(description))
}

func ginkgoDescriptionUsesCoverageBucket(description string) bool {
	fields := strings.FieldsFunc(strings.ToLower(description), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	for i, field := range fields {
		if field == "coverage" {
			return true
		}
		if ginkgoCoverageBucketTailWordHasContext(fields, i) {
			return true
		}
		if ginkgoCoverageBucketBranchesWithFillerSuffix(fields, i) {
			return true
		}
		if i == 0 && isCoverageBucketVerb(field) {
			return true
		}
		if i > 0 && isCoverageBucketBranchPhrase(fields[i-1], field) {
			return true
		}
	}
	return false
}

func ginkgoCoverageBucketBranchesWithFillerSuffix(fields []string, index int) bool {
	if fields[index] != "branches" ||
		index == len(fields)-1 ||
		!ginkgoCoverageBucketBranchPrefixHasContext(fields[:index]) {
		return false
	}
	for _, field := range fields[index+1:] {
		if !isCoverageBucketFillerSuffix(field) {
			return false
		}
	}
	return true
}

func isCoverageBucketFillerSuffix(field string) bool {
	switch field {
	case "explicit", "explicitly":
		return true
	default:
		return false
	}
}

func ginkgoCoverageBucketTailWordHasContext(fields []string, index int) bool {
	if index != len(fields)-1 {
		return false
	}
	switch fields[index] {
	case "branches":
		return index == 0 || ginkgoCoverageBucketBranchPrefixHasContext(fields[:index])
	case "edges":
		return index == 0 || ginkgoCoverageBucketEdgePrefixHasContext(fields[:index])
	default:
		return false
	}
}

func ginkgoCoverageBucketBranchPrefixHasContext(fields []string) bool {
	for i, field := range fields {
		switch field {
		case "edge", "option", "options":
			return true
		case "no":
			if i+1 < len(fields) && fields[i+1] == "op" {
				return true
			}
		case "noop":
			return true
		}
	}
	return false
}

func ginkgoCoverageBucketEdgePrefixHasContext(fields []string) bool {
	for _, field := range fields {
		if field == "deterministic" {
			return true
		}
	}
	return false
}

func isCoverageBucketVerb(field string) bool {
	switch field {
	case "cover", "covers", "covered", "covering",
		"exercise", "exercises", "exercised", "exercising":
		return true
	default:
		return false
	}
}

func isCoverageBucketBranchPhrase(previous string, current string) bool {
	return previous == "edge" && (current == "branch" || current == "branches" || current == "case" || current == "cases")
}
