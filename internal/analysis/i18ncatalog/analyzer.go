package i18ncatalog

import (
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const doc = "checks LeafWiki i18n catalog source policy"

var Analyzer = &analysis.Analyzer{
	Name:     "i18ncatalog",
	Doc:      doc,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	ins.Preorder([]ast.Node{(*ast.CompositeLit)(nil)}, func(node ast.Node) {
		checkGinPayload(pass, node.(*ast.CompositeLit))
	})

	if shouldRunRepositoryChecks(pass) {
		root, ok := findRepositoryRoot(pass)
		if ok {
			diagnostics, err := CheckRepository(root)
			if err != nil {
				return nil, err
			}
			for _, diagnostic := range diagnostics {
				reportRepositoryDiagnostic(pass, diagnostic)
			}
		}
	}
	return nil, nil
}

func checkGinPayload(pass *analysis.Pass, lit *ast.CompositeLit) {
	if !isGinH(pass, lit.Type) {
		return
	}
	filename := pass.Fset.Position(lit.Pos()).Filename
	if !isGinPayloadPolicyFile(filename) {
		return
	}

	fields := map[string]*ast.KeyValueExpr{}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := stringLiteral(kv.Key)
		if !ok {
			continue
		}
		fields[key] = kv
	}

	if message, ok := fields["message"]; ok {
		if _, hasMessageID := fields["messageId"]; !hasMessageID {
			pass.Reportf(message.Key.Pos(), `gin.H "message" payload requires "messageId"`)
		}
	}

	errorField, ok := fields["error"]
	if !ok || ginErrorPayloadAllowed(pass, filename, fields, errorField.Value) {
		return
	}
	if _, ok := stringLiteral(errorField.Value); ok {
		pass.Reportf(errorField.Key.Pos(), `gin.H "error" string literal bypasses structured localized errors`)
		return
	}
	pass.Reportf(errorField.Key.Pos(), `gin.H "error" dynamic value must use structured localized errors`)
}

func isGinH(pass *analysis.Pass, expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	if typ := pass.TypesInfo.TypeOf(expr); typ != nil {
		if named, ok := typ.(*types.Named); ok {
			obj := named.Obj()
			return obj != nil && obj.Name() == "H" && obj.Pkg() != nil && obj.Pkg().Path() == "github.com/gin-gonic/gin"
		}
	}
	sel, ok := expr.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "H"
}

func isGinPayloadPolicyFile(filename string) bool {
	if strings.HasSuffix(filename, "_test.go") {
		return false
	}
	normalized := filepath.ToSlash(filename)
	if strings.Contains(normalized, "/internal/localization/") {
		return false
	}
	return strings.Contains(normalized, "/internal/") || strings.Contains(normalized, "/cmd/")
}

func ginErrorPayloadAllowed(pass *analysis.Pass, filename string, fields map[string]*ast.KeyValueExpr, expr ast.Expr) bool {
	if isLeafWikiLocalizedErrorDetailExpr(pass, expr) {
		return true
	}
	if _, hasFields := fields["fields"]; hasFields && strings.HasSuffix(exprSemanticName(expr), "ValidationErrorCode") {
		return true
	}
	if strings.Contains(filepath.ToSlash(filename), "/internal/wiki/oauth/") {
		name := exprSemanticName(expr)
		return strings.HasPrefix(name, "oauthError") || name == "rfcErr.ErrorField"
	}
	return false
}

func isLeafWikiLocalizedErrorDetailExpr(pass *analysis.Pass, expr ast.Expr) bool {
	if typ := pass.TypesInfo.TypeOf(expr); typ != nil {
		return isLeafWikiLocalizedErrorDetailType(typ)
	}
	return isKnownSharedErrorsLocalizedErrorDetailCall(pass, expr)
}

func isLeafWikiLocalizedErrorDetailType(typ types.Type) bool {
	named, ok := types.Unalias(typ).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil &&
		obj.Name() == "LocalizedErrorDetail" &&
		obj.Pkg() != nil &&
		obj.Pkg().Path() == "github.com/perber/wiki/internal/core/shared/errors"
}

func isKnownSharedErrorsLocalizedErrorDetailCall(pass *analysis.Pass, expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !isLocalizedErrorDetailHelperName(sel.Sel.Name) {
		return false
	}
	if obj := pass.TypesInfo.Uses[sel.Sel]; obj != nil && obj.Pkg() != nil {
		return obj.Pkg().Path() == "github.com/perber/wiki/internal/core/shared/errors"
	}
	return selectorUsesSharedErrorsImport(pass, sel)
}

func isLocalizedErrorDetailHelperName(name string) bool {
	switch name {
	case "NewLocalizedErrorDetail", "NewLocalizedErrorDetailFromCode", "LocalizedErrorDetailFromError":
		return true
	default:
		return false
	}
}

func selectorUsesSharedErrorsImport(pass *analysis.Pass, sel *ast.SelectorExpr) bool {
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	file := fileContainingExpr(pass, sel)
	if file == nil {
		return false
	}
	for _, importSpec := range file.Imports {
		importPath, ok := stringLiteral(importSpec.Path)
		if !ok || importPath != "github.com/perber/wiki/internal/core/shared/errors" {
			continue
		}
		if importSpec.Name != nil {
			return importSpec.Name.Name == ident.Name
		}
		return ident.Name == "errors"
	}
	return false
}

func fileContainingExpr(pass *analysis.Pass, expr ast.Expr) *ast.File {
	for _, file := range pass.Files {
		if file.Pos() <= expr.Pos() && expr.End() <= file.End() {
			return file
		}
	}
	return nil
}

func exprSemanticName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		if ident, ok := e.X.(*ast.Ident); ok {
			return ident.Name + "." + e.Sel.Name
		}
		return e.Sel.Name
	default:
		return ""
	}
}

func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

func shouldRunRepositoryChecks(pass *analysis.Pass) bool {
	return pass.Pkg != nil && pass.Pkg.Path() == "github.com/perber/wiki/internal/localization"
}

func findRepositoryRoot(pass *analysis.Pass) (string, bool) {
	for _, file := range pass.Files {
		filename := pass.Fset.Position(file.Package).Filename
		if filename == "" {
			continue
		}
		dir := filepath.Dir(filename)
		for {
			goMod := filepath.Join(dir, "go.mod")
			raw, err := os.ReadFile(goMod)
			if err == nil && strings.Contains(string(raw), "module github.com/perber/wiki") {
				return dir, true
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", false
}

func reportRepositoryDiagnostic(pass *analysis.Pass, diagnostic RepositoryDiagnostic) {
	raw, err := os.ReadFile(diagnostic.Path)
	if err != nil {
		pass.Report(analysis.Diagnostic{
			Pos:     repositoryDiagnosticFallbackPos(pass),
			Message: diagnostic.Path + ": " + diagnostic.Detail,
		})
		return
	}
	file := pass.Fset.AddFile(diagnostic.Path, -1, len(raw))
	file.SetLinesForContent(raw)
	line := diagnostic.Line
	if line < 1 || line > file.LineCount() {
		line = 1
	}
	pass.Report(analysis.Diagnostic{
		Pos:     file.LineStart(line),
		Message: diagnostic.Detail,
	})
}

func repositoryDiagnosticFallbackPos(pass *analysis.Pass) token.Pos {
	for _, file := range pass.Files {
		if file.Package.IsValid() {
			return file.Package
		}
		if file.Pos().IsValid() {
			return file.Pos()
		}
	}
	return token.NoPos
}
