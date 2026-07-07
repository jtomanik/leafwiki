package semantichygiene

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

func checkRenderedErrorPresencePredicate(ctx *analysisContext, call *ast.CallExpr) {
	if !isTestFile(ctx.filename(call.Pos())) ||
		!isErrorMethodCall(ctx, call) {
		return
	}
	binary, ok := enclosingBinaryComparison(ctx, call)
	if !ok ||
		!isRenderedErrorPresenceComparison(binary, call) ||
		!isRenderedErrorPresenceHelperContext(ctx, call) {
		return
	}
	ctx.report(ruleI18nRenderedErrorPresence, call, renderedErrorPresencePredicateDiagnostic())
}

func isErrorMethodCall(ctx *analysisContext, call *ast.CallExpr) bool {
	if len(call.Args) != 0 || callName(call) != "Error" {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return expressionImplementsError(ctx.pass.TypesInfo.TypeOf(selector.X))
}

func expressionImplementsError(typ types.Type) bool {
	if typ == nil {
		return false
	}
	errorInterface, ok := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if !ok {
		return false
	}
	if types.Implements(typ, errorInterface) {
		return true
	}
	if _, ok := typ.(*types.Pointer); ok {
		return false
	}
	if _, ok := typ.Underlying().(*types.Interface); ok {
		return false
	}
	return types.Implements(types.NewPointer(typ), errorInterface)
}

func enclosingBinaryComparison(ctx *analysisContext, call *ast.CallExpr) (*ast.BinaryExpr, bool) {
	for current := ast.Node(call); current != nil; {
		parent := ctx.parent(current)
		if paren, ok := parent.(*ast.ParenExpr); ok {
			current = paren
			continue
		}
		binary, ok := parent.(*ast.BinaryExpr)
		return binary, ok
	}
	return nil, false
}

func isRenderedErrorPresenceComparison(binary *ast.BinaryExpr, call *ast.CallExpr) bool {
	if binary.Op != token.EQL && binary.Op != token.NEQ {
		return false
	}
	return containsNode(binary.X, call) && isEmptyStringLiteral(binary.Y) ||
		containsNode(binary.Y, call) && isEmptyStringLiteral(binary.X)
}

func isEmptyStringLiteral(expr ast.Expr) bool {
	lit, ok := unparenExpr(expr).(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	value, err := strconv.Unquote(lit.Value)
	return err == nil && value == ""
}

func isRenderedErrorPresenceHelperContext(ctx *analysisContext, call *ast.CallExpr) bool {
	fn := enclosingFunc(ctx, call)
	if fn == nil || fn.Name == nil || isGoTestEntrypointName(fn.Name.Name) {
		return false
	}
	if funcReturnsGomegaMatcher(fn) ||
		gomegaRenderedOutputMatcherFactoryName(fn.Name.Name) ||
		renderedErrorPresenceHelperName(fn.Name.Name) {
		return true
	}
	return renderedErrorPresenceHelperName(receiverTypeName(fn))
}

func renderedErrorPresenceHelperName(name string) bool {
	canonical := canonicalName(name)
	if !strings.Contains(canonical, "error") {
		return false
	}
	return containsAnyCanonical(canonical, renderedErrorPresenceHelperFragments)
}

func receiverTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	return exprName(fn.Recv.List[0].Type)
}

var renderedErrorPresenceHelperFragments = []string{
	"contract",
	"localized",
	"match",
	"matcher",
	"message",
	"presence",
	"rendered",
	"state",
	"validation",
}
