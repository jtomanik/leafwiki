package semantichygiene

import (
	"go/ast"
	"go/token"
	"strconv"
)

func checkStableLiteral(ctx *analysisContext, lit *ast.BasicLit) {
	if lit.Kind != token.STRING || isStableLiteralAllowed(ctx, lit) {
		return
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil || !isStableContractLiteral(ctx, lit, value) {
		return
	}
	ctx.pass.Reportf(lit.Pos(), "%s", stableLiteralDiagnostic(value))
}

func checkLocalizedProseLiteral(ctx *analysisContext, lit *ast.BasicLit) {
	if lit.Kind != token.STRING || isLocalizedProseLiteralAllowed(ctx, lit) {
		return
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil || !looksLikeLocalizedProse(value) || !isRawLocalizedProseContractLiteral(ctx, lit) {
		return
	}
	ctx.pass.Reportf(lit.Pos(), "%s", rawLocalizedProseDiagnostic(value))
}
