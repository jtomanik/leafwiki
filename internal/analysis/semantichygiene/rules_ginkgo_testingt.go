package semantichygiene

import (
	"go/ast"
	"go/types"
	"strings"
)

func checkGinkgoTestingTInSpec(ctx *analysisContext, call *ast.CallExpr, name string) {
	if !isGinkgoSubjectBodyNodeName(name) || !isGinkgoDSLCall(ctx, call) {
		return
	}
	body, ok := firstFuncLitArg(call)
	if !ok {
		return
	}
	ast.Inspect(body.Body, func(node ast.Node) bool {
		if node == nil {
			return false
		}
		if nested, ok := node.(*ast.FuncLit); ok && nested != body {
			return false
		}
		candidate, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if name, ok := ginkgoTestingTAssertion(ctx, candidate); ok {
			ctx.report(ruleGinkgoTestingTInSpec, candidate, ginkgoTestingTAssertionDiagnostic(name))
			return true
		}
		if name, ok := ginkgoTestingTAdapterMethod(ctx, candidate); ok {
			ctx.report(ruleGinkgoTestingTInSpec, candidate, ginkgoTestingTInSpecDiagnostic(name))
			return true
		}
		name, ok := ginkgoTestingTAdapter(ctx, candidate)
		if !ok {
			return true
		}
		ctx.report(ruleGinkgoTestingTInSpec, candidate, ginkgoTestingTInSpecDiagnostic(name))
		return true
	})
}

func checkGinkgoFailInSpec(ctx *analysisContext, call *ast.CallExpr, name string) {
	if !isGinkgoSubjectBodyNodeName(name) || !isGinkgoDSLCall(ctx, call) {
		return
	}
	body, ok := firstFuncLitArg(call)
	if !ok {
		return
	}
	ast.Inspect(body.Body, func(node ast.Node) bool {
		if node == nil {
			return false
		}
		candidate, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isGinkgoFailCall(ctx, candidate) {
			ctx.report(ruleGinkgoFailInSpec, candidate, ginkgoFailInSpecDiagnostic())
			return true
		}
		if name, ok := ginkgoFailureHelperCall(ctx, candidate); ok {
			ctx.report(ruleGinkgoFailInSpec, candidate, ginkgoFailureHelperInSpecDiagnostic(name))
			return true
		}
		if name, ok := ginkgoHiddenFailHelperCall(ctx, candidate); ok {
			ctx.report(ruleGinkgoFailInSpec, candidate, ginkgoHiddenFailHelperInSpecDiagnostic(name))
			return true
		}
		return true
	})
}

func isGinkgoFailCall(ctx *analysisContext, call *ast.CallExpr) bool {
	packagePath, name := calleePackageAndName(ctx, call)
	return packagePath == "github.com/onsi/ginkgo/v2" && name == "Fail"
}

func ginkgoFailureHelperCall(ctx *analysisContext, call *ast.CallExpr) (string, bool) {
	if _, ok := unparenExpr(call.Fun).(*ast.Ident); !ok {
		return "", false
	}
	_, name := calleePackageAndName(ctx, call)
	if !isFailureHelperName(name) {
		return "", false
	}
	return name, true
}

func isFailureHelperName(name string) bool {
	return hasFailureHelperPrefix(name, "fail") || hasFailureHelperPrefix(name, "fatal")
}

func ginkgoHiddenFailHelperCall(ctx *analysisContext, call *ast.CallExpr) (string, bool) {
	if _, ok := unparenExpr(call.Fun).(*ast.Ident); !ok {
		return "", false
	}
	fn := localFuncDeclForCall(ctx, call)
	if fn == nil ||
		fn.Body == nil ||
		fn.Name == nil ||
		!funcHasGinkgoHelperCall(fn.Body) ||
		!funcBodyContainsGinkgoFail(ctx, fn.Body) {
		return "", false
	}
	return fn.Name.Name, true
}

func localFuncDeclForCall(ctx *analysisContext, call *ast.CallExpr) *ast.FuncDecl {
	target := calledFunctionObject(ctx, call)
	if target == nil || target.Pkg() == nil || target.Pkg().Path() != ctx.pass.Pkg.Path() {
		return nil
	}
	for _, file := range ctx.pass.Files {
		var found *ast.FuncDecl
		ast.Inspect(file, func(node ast.Node) bool {
			if found != nil || node == nil {
				return false
			}
			fn, ok := node.(*ast.FuncDecl)
			if !ok || fn.Name == nil {
				return true
			}
			if ctx.pass.TypesInfo.ObjectOf(fn.Name) == target {
				found = fn
				return false
			}
			return true
		})
		if found != nil {
			return found
		}
	}
	return nil
}

func funcBodyContainsGinkgoFail(ctx *analysisContext, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if ok && isGinkgoFailCall(ctx, call) {
			found = true
			return false
		}
		return true
	})
	return found
}

func hasFailureHelperPrefix(name string, prefix string) bool {
	lower := strings.ToLower(name)
	if lower == prefix || lower == prefix+"f" || lower == prefix+"now" {
		return true
	}
	if !strings.HasPrefix(lower, prefix) || len(name) <= len(prefix) {
		return false
	}
	next := name[len(prefix)]
	return next >= 'A' && next <= 'Z'
}

func ginkgoTestingTAssertion(ctx *analysisContext, call *ast.CallExpr) (string, bool) {
	selector, ok := unparenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || !isTestingTFailureMethod(selector.Sel.Name) {
		return "", false
	}
	receiverType := ctx.pass.TypesInfo.TypeOf(selector.X)
	if isTestingTType(receiverType) {
		return "testing.T." + selector.Sel.Name, true
	}
	if isTestingTLikeAdapterType(receiverType) {
		return "testing.T-like." + selector.Sel.Name, true
	}
	return "", false
}

func ginkgoTestingTAdapterMethod(ctx *analysisContext, call *ast.CallExpr) (string, bool) {
	selector, ok := unparenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || !isTestingTFixtureMethod(selector.Sel.Name) {
		return "", false
	}
	receiverType := ctx.pass.TypesInfo.TypeOf(selector.X)
	if isTestingTType(receiverType) {
		return "testing.T." + selector.Sel.Name, true
	}
	if isTestingTLikeAdapterType(receiverType) {
		return "testing.T-like." + selector.Sel.Name, true
	}
	return "", false
}

func isTestingTFixtureMethod(name string) bool {
	switch name {
	case "Cleanup", "Helper", "Log", "Logf", "Name", "Setenv", "Skip", "Skipf", "SkipNow", "TempDir":
		return true
	default:
		return false
	}
}

func isTestingTLikeAdapterType(typ types.Type) bool {
	if typ == nil || isTestingTType(typ) {
		return false
	}
	methods := map[string]struct{}{}
	collectTestingTLikeMethods(types.Unalias(typ), methods)
	if testingTLikeMethodsHaveStrongSignal(methods) {
		return true
	}
	named := namedType(typ)
	return named != nil && strings.Contains(strings.ToLower(named.Obj().Name()), "testt") && len(methods) >= 1
}

func testingTLikeMethodsHaveStrongSignal(methods map[string]struct{}) bool {
	for _, name := range []string{
		"Fatalf",
		"Fatal",
		"Errorf",
		"Helper",
		"Cleanup",
		"Setenv",
		"Skip",
		"Skipf",
		"SkipNow",
		"TempDir",
	} {
		if _, ok := methods[name]; ok {
			return true
		}
	}
	return false
}

func collectTestingTLikeMethods(typ types.Type, methods map[string]struct{}) {
	if typ == nil {
		return
	}
	methodSet := types.NewMethodSet(typ)
	for i := 0; i < methodSet.Len(); i++ {
		name := methodSet.At(i).Obj().Name()
		if isTestingTFailureMethod(name) || isTestingTFixtureMethod(name) {
			methods[name] = struct{}{}
		}
	}
	if _, ok := typ.(*types.Pointer); ok {
		return
	}
	if named, ok := typ.(*types.Named); ok {
		collectTestingTLikeMethods(types.NewPointer(named), methods)
	}
}

func isTestingTFailureMethod(name string) bool {
	switch name {
	case "Fatalf", "Fatal", "Errorf", "Error":
		return true
	default:
		return false
	}
}

func isTestingTType(typ types.Type) bool {
	typ = types.Unalias(typ)
	if ptr, ok := typ.(*types.Pointer); ok {
		typ = ptr.Elem()
	}
	named, ok := typ.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == "testing" && named.Obj().Name() == "T"
}

func ginkgoTestingTAdapter(ctx *analysisContext, call *ast.CallExpr) (string, bool) {
	packagePath, name := calleePackageAndName(ctx, call)
	if packagePath != "github.com/onsi/ginkgo/v2" {
		return "", false
	}
	switch name {
	case "GinkgoT", "GinkgoTB":
		return name, true
	default:
		return "", false
	}
}
