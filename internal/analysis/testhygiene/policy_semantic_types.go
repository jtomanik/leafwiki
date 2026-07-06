package testhygiene

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

func isMessageBearingStructName(name string) bool {
	canonical := canonicalName(name)
	return strings.Contains(canonical, "issue") ||
		strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "warning") ||
		strings.Contains(canonical, "validation") ||
		strings.Contains(canonical, "refactor")
}
