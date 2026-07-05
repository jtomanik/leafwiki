package testhygiene

import "go/ast"

var ginkgoPrimaryTaxonomyLabels = []string{"unit", "integration", "e2e"}

func checkGinkgoTaxonomyLabels(ctx *analysisContext, call *ast.CallExpr, name string) {
	if !isGinkgoNameCarrier(name) || !isGinkgoDSLCall(ctx, call) {
		return
	}

	directLabels, directConflict := checkGinkgoDirectTaxonomyLabels(ctx, call)
	if !ginkgoCallRepresentsRunnableSpec(name) {
		return
	}

	effectiveLabels := taxonomyLabelSet{}
	for _, ancestor := range enclosingGinkgoTaxonomyCalls(ctx, call) {
		effectiveLabels.addAll(ginkgoDirectPrimaryTaxonomyLabels(ancestor))
	}
	effectiveLabels.addAll(directLabels.labels)

	if effectiveLabels.len() > 1 && !directConflict {
		ctx.report(ruleGinkgoTaxonomyMultipleLabels, call, ginkgoTaxonomyMultipleLabelsDiagnostic(effectiveLabels.labels))
	}
}

func checkGinkgoDirectTaxonomyLabels(ctx *analysisContext, call *ast.CallExpr) (taxonomyLabelSet, bool) {
	labels := taxonomyLabelSet{}
	for _, decorator := range ginkgoLabelDecorators(call) {
		for _, arg := range decorator.Args {
			label, ok := stringLiteralValue(arg)
			if !ok {
				ctx.report(ruleGinkgoTaxonomyDynamicLabel, arg, ginkgoTaxonomyDynamicLabelDiagnostic())
				continue
			}
			if !isGinkgoPrimaryTaxonomyLabel(label) {
				ctx.report(ruleGinkgoTaxonomyUnknownLabel, arg, ginkgoTaxonomyUnknownLabelDiagnostic(label))
				continue
			}
			labels.add(label)
		}
	}
	if labels.len() > 1 {
		ctx.report(ruleGinkgoTaxonomyMultipleLabels, call, ginkgoTaxonomyMultipleLabelsDiagnostic(labels.labels))
		return labels, true
	}
	return labels, false
}

func checkGinkgoMissingTaxonomyLabels(ctx *analysisContext) {
	for _, file := range ctx.pass.Files {
		if !isTestFile(ctx.filename(file.Pos())) {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := callName(call)
			if !ginkgoCallRepresentsRunnableSpec(name) || !isGinkgoDSLCall(ctx, call) {
				return true
			}
			if ginkgoCallHasPrimaryTaxonomyLabel(ctx, call) || ginkgoCallHasAnyTaxonomyLabelAttempt(ctx, call) {
				return true
			}
			ctx.report(ruleGinkgoTaxonomyMissingLabel, call, ginkgoTaxonomyMissingLabelDiagnostic())
			return true
		})
	}
}

func ginkgoCallHasPrimaryTaxonomyLabel(ctx *analysisContext, call *ast.CallExpr) bool {
	for _, ancestor := range enclosingGinkgoTaxonomyCalls(ctx, call) {
		if len(ginkgoDirectPrimaryTaxonomyLabels(ancestor)) > 0 {
			return true
		}
	}
	return len(ginkgoDirectPrimaryTaxonomyLabels(call)) > 0
}

func ginkgoCallHasAnyTaxonomyLabelAttempt(ctx *analysisContext, call *ast.CallExpr) bool {
	for _, ancestor := range enclosingGinkgoTaxonomyCalls(ctx, call) {
		if len(ginkgoLabelDecorators(ancestor)) > 0 {
			return true
		}
	}
	return len(ginkgoLabelDecorators(call)) > 0
}

func ginkgoDirectPrimaryTaxonomyLabels(call *ast.CallExpr) []string {
	labels := taxonomyLabelSet{}
	for _, decorator := range ginkgoLabelDecorators(call) {
		for _, arg := range decorator.Args {
			label, ok := stringLiteralValue(arg)
			if ok && isGinkgoPrimaryTaxonomyLabel(label) {
				labels.add(label)
			}
		}
	}
	return labels.labels
}

func enclosingGinkgoTaxonomyCalls(ctx *analysisContext, call *ast.CallExpr) []*ast.CallExpr {
	var calls []*ast.CallExpr
	for current := ctx.parent(call); current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.FuncDecl:
			return reverseGinkgoCalls(calls)
		case *ast.CallExpr:
			name := callName(n)
			if !isGinkgoDSLCall(ctx, n) {
				continue
			}
			if isGinkgoContainerNodeName(name) || isDescribeTableCall(name) {
				calls = append(calls, n)
			}
		}
	}
	return reverseGinkgoCalls(calls)
}

func reverseGinkgoCalls(calls []*ast.CallExpr) []*ast.CallExpr {
	for left, right := 0, len(calls)-1; left < right; left, right = left+1, right-1 {
		calls[left], calls[right] = calls[right], calls[left]
	}
	return calls
}

func ginkgoLabelDecorators(call *ast.CallExpr) []*ast.CallExpr {
	var decorators []*ast.CallExpr
	for _, arg := range call.Args {
		decorator, ok := unparenExpr(arg).(*ast.CallExpr)
		if !ok || callName(decorator) != "Label" {
			continue
		}
		decorators = append(decorators, decorator)
	}
	return decorators
}

func ginkgoCallRepresentsRunnableSpec(name string) bool {
	return isGinkgoSpecNodeName(name) || isBDDEntryCall(name)
}

func isGinkgoPrimaryTaxonomyLabel(label string) bool {
	for _, allowed := range ginkgoPrimaryTaxonomyLabels {
		if label == allowed {
			return true
		}
	}
	return false
}

type taxonomyLabelSet struct {
	labels []string
	seen   map[string]bool
}

func (set *taxonomyLabelSet) add(label string) {
	if set.seen == nil {
		set.seen = map[string]bool{}
	}
	if set.seen[label] {
		return
	}
	set.seen[label] = true
	set.labels = append(set.labels, label)
}

func (set *taxonomyLabelSet) addAll(labels []string) {
	for _, label := range labels {
		set.add(label)
	}
}

func (set taxonomyLabelSet) len() int {
	return len(set.labels)
}
