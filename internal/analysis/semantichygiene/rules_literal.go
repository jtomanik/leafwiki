package semantichygiene

import (
	"go/ast"
	"go/token"
	"strconv"
)

func checkStableLiteral(ctx *analysisContext, lit *ast.BasicLit) {
	if lit.Kind != token.STRING {
		return
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return
	}
	if isTestFile(ctx.filename(lit.Pos())) {
		if isAllowedTestDescriptionLiteral(ctx, lit) ||
			!isStableTestContractLiteral(ctx, lit, value) {
			return
		}
		ctx.pass.Reportf(lit.Pos(), "%s", testStableLiteralDiagnostic(value))
		return
	}
	if isStableLiteralAllowed(ctx, lit) || !isStableContractLiteral(ctx, lit, value) {
		return
	}
	ctx.pass.Reportf(lit.Pos(), "%s", stableLiteralDiagnostic(value))
}

func checkLocalizedProseLiteral(ctx *analysisContext, lit *ast.BasicLit) {
	if lit.Kind != token.STRING {
		return
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return
	}
	if isTestFile(ctx.filename(lit.Pos())) {
		if isAllowedTestDescriptionLiteral(ctx, lit) ||
			!looksLikeLocalizedProse(value) ||
			!isTestLocalizedProseContractLiteralContext(ctx, lit) {
			return
		}
		ctx.pass.Reportf(lit.Pos(), "%s", testRawLocalizedProseDiagnostic(value))
		return
	}
	if isLocalizedProseLiteralAllowed(ctx, lit) || !isRawLocalizedProseContractLiteral(ctx, lit) {
		return
	}
	if !looksLikeLocalizedProse(value) && !isStrictLocalizedProseContractLiteral(ctx, lit, value) {
		return
	}
	ctx.pass.Reportf(lit.Pos(), "%s", rawLocalizedProseDiagnostic(value))
}
