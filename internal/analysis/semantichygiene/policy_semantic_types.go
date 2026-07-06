package semantichygiene

import (
	"go/ast"
	"go/types"
	"golang.org/x/tools/go/analysis"
	"strings"
)

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
	if canonicalName(fieldName) == "id" {
		return semanticTypeForBareIDContext(typeName)
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
