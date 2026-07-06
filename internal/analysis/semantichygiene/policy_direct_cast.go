package semantichygiene

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

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
	return containsAnyCanonical(canonicalName(name), testFixtureStructNameFragments)
}

var testFixtureStructNameFragments = []string{
	"dto",
	"fake",
	"fixture",
	"mock",
	"request",
	"response",
	"stub",
	"wire",
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
