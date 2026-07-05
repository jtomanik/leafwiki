package semantichygiene

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"strings"
)

func isGeneratedOrVendored(filename string) bool {
	return strings.Contains(filename, "/vendor/") || strings.Contains(filename, "/node_modules/")
}

func isTestOrGeneratedFile(filename string) bool {
	return isTestFile(filename) || isGeneratedOrVendored(filename)
}

func isWireDTOFile(filename string) bool {
	return strings.Contains(filename, "/internal/http/dto/")
}

func isMCPWireFile(filename string) bool {
	return strings.Contains(filename, "/internal/wiki/mcp/types.go")
}

func isMarkdownSerializationFile(filename string) bool {
	return strings.Contains(filename, "/internal/core/markdown/metadata.go") ||
		strings.Contains(filename, "/internal/core/markdown/metadata_codec.go")
}

func isTestSupportFile(filename string) bool {
	return strings.Contains(filename, "/internal/test_utils/")
}

func isTestMatcherSupportFile(filename string) bool {
	return strings.Contains(filename, "/internal/test_utils/matchers/")
}

func isPersistenceAdapterFile(filename string) bool {
	base := filepath.Base(filename)
	return strings.HasSuffix(base, "_store.go") ||
		strings.Contains(filename, "/internal/core/revision/fs_store.go")
}

func isEdgeAdapterFile(filename string) bool {
	switch {
	case strings.HasSuffix(filename, "/internal/wiki/import_adapter.go"):
		return true
	case strings.HasSuffix(filename, "/internal/core/tree/migration_adapter.go"):
		return true
	case strings.HasSuffix(filename, "/internal/workspacesync/watcher_adapter.go"):
		return true
	case strings.HasSuffix(filename, "/internal/analysis/semantichygiene/testdata/semanticcases/migration_adapter.go"):
		return true
	default:
		return false
	}
}

func isSemanticOwnerAdapterFile(ctx *analysisContext, pos token.Pos) bool {
	filename := ctx.filename(pos)
	switch ctx.pass.Pkg.Path() {
	case "github.com/perber/wiki/internal/agenthooks":
		return strings.HasSuffix(filename, "/internal/agenthooks/agenthooks.go")
	case "github.com/perber/wiki/internal/core/identity":
		return strings.HasSuffix(filename, "/internal/core/identity/semantic_types.go")
	case "github.com/perber/wiki/internal/core/tree":
		return strings.HasSuffix(filename, "/internal/core/tree/semantic_types.go")
	case "github.com/perber/wiki/internal/core/auth":
		return strings.HasSuffix(filename, "/internal/core/auth/semantic_types.go")
	case "github.com/perber/wiki/internal/core/revision":
		return strings.HasSuffix(filename, "/internal/core/revision/semantic_types.go")
	case "github.com/perber/wiki/internal/core/markdownvalidation":
		return strings.HasSuffix(filename, "/internal/core/markdownvalidation/issue_codes.go")
	case "github.com/perber/wiki/internal/workspacesync":
		return strings.HasSuffix(filename, "/internal/workspacesync/semantic_types.go")
	case "github.com/perber/wiki/internal/workspaceid":
		return strings.HasSuffix(filename, "/internal/workspaceid/validate.go")
	case "github.com/perber/wiki/internal/analysis/semantichygiene/testdata/semanticcases":
		return strings.HasSuffix(filename, "/internal/analysis/semantichygiene/testdata/semanticcases/path_adapters.go") ||
			strings.HasSuffix(filename, "/internal/analysis/semantichygiene/testdata/semanticcases/owner_adapters.go")
	default:
		return false
	}
}

var allowedSemanticOwnerAdapterFuncs = map[string]map[string]bool{
	"ActorID":                         {"UserID": true},
	"Child":                           {"RoutePath": true, "Slug": true},
	"Clean":                           {"AssetName": true, "MarkdownPath": true, "RoutePath": true, "WorkspaceSourcePath": true},
	"CleanMarkdownPath":               {"MarkdownPath": true},
	"CleanWorkspaceSourcePath":        {"WorkspaceSourcePath": true},
	"CommitID":                        {"RevisionID": true},
	"Dir":                             {"MarkdownPath": true, "WorkspaceSourcePath": true},
	"EqualFold":                       {"Slug": true},
	"Ext":                             {"MarkdownPath": true},
	"FilesystemPath":                  {"MarkdownPath": true, "RoutePath": true, "Slug": true, "WorkspaceSourcePath": true},
	"Filename":                        {"AssetName": true},
	"HashPayload":                     {"PageID": true, "Slug": true, "UserID": true},
	"HrefPath":                        {"MarkdownPath": true, "RoutePath": true},
	"HTTPHeaderValue":                 {"WorkspaceID": true},
	"IsMarkdown":                      {"MarkdownPath": true},
	"LeafSlug":                        {"RoutePath": true, "Slug": true},
	"LowerKey":                        {"RoutePath": true},
	"MarkdownContentPath":             {"MarkdownPath": true, "RoutePath": true},
	"MarkdownPagePath":                {"MarkdownPath": true, "RoutePath": true},
	"MetadataValue":                   {"PageID": true, "UserID": true},
	"NewAPIKeyIDUnchecked":            {"APIKeyID": true},
	"NewAssetNameUnchecked":           {"AssetName": true},
	"NewMarkdownPathUnchecked":        {"MarkdownPath": true},
	"NewCommitHashUnchecked":          {"CommitHash": true},
	"NewPageIDUnchecked":              {"PageID": true},
	"NewPageVersionFromTime":          {"PageVersion": true},
	"NewPageVersionUnchecked":         {"PageVersion": true},
	"NewRevisionIDUnchecked":          {"RevisionID": true},
	"NewRoutePathUnchecked":           {"RoutePath": true},
	"NewSessionIDUnchecked":           {"SessionID": true},
	"NewSlugUnchecked":                {"Slug": true},
	"NewUserIDUnchecked":              {"UserID": true},
	"NewWorkspaceSourcePathUnchecked": {"WorkspaceSourcePath": true},
	"HasMCPPrefix":                    {"AgentToolName": true},
	"Normalize":                       {"AgentEventName": true, "AgentToolName": true, "IssueCode": true, "IssueSeverity": true, "ProviderID": true},
	"ParseWorkspaceID":                {"WorkspaceID": true},
	"RoutePath":                       {"MarkdownPath": true, "RoutePath": true},
	"Scan":                            {"PageID": true, "WorkspaceID": true},
	"Segments":                        {"RoutePath": true, "Slug": true},
	"SanitizedMetadataValue":          {"AgentToolName": true},
	"SlugKey":                         {"Slug": true},
	"SourceDir":                       {"MarkdownPath": true},
	"StorageKey":                      {"WorkspaceID": true},
	"URLPathSegment":                  {"WorkspaceID": true},
	"Validate":                        {"RoutePath": true, "Slug": true, "WorkspaceID": true},
	"ValidateWorkspaceID":             {"WorkspaceID": true},
	"Value":                           {"PageID": true, "WorkspaceID": true},
	"WikiPath":                        {"RoutePath": true},
	"WithLeafSlug":                    {"RoutePath": true, "Slug": true},
	"WorkspaceSourceDirectory":        {"RoutePath": true, "WorkspaceSourcePath": true},
	"WorkspaceSourcePath":             {"RoutePath": true, "WorkspaceSourcePath": true},
}

func isAllowedSemanticOwnerAdapterFunc(ctx *analysisContext, node ast.Node, typeName string) bool {
	if !isSemanticOwnerAdapterFile(ctx, node.Pos()) {
		return false
	}
	fn := enclosingFunc(ctx, node)
	if fn == nil || !allowedSemanticOwnerAdapterFuncs[fn.Name.Name][typeName] {
		return false
	}
	return functionReturnsSemanticType(ctx, fn, typeName) ||
		functionHasSemanticReceiver(ctx, fn, typeName) ||
		functionHasSemanticParameter(ctx, fn, typeName)
}

func isAllowedStringBoundaryFile(filename string) bool {
	return isGeneratedOrVendored(filename) || isTestMatcherSupportFile(filename)
}

func isAllowedDirectCastFile(filename string) bool {
	return isGeneratedOrVendored(filename)
}

func isAllowedDirectCastContext(ctx *analysisContext, call *ast.CallExpr, typeName string) bool {
	filename := ctx.filename(call.Pos())
	if isAllowedDirectCastFile(filename) {
		return true
	}
	if isAllowedTestLiteralDirectCastContext(filename, call) {
		return true
	}
	if isAllowedFixtureDirectCastContext(filename, enclosingFuncName(ctx, call)) {
		return true
	}
	if isAllowedEdgeAdapterDirectCastContext(ctx, call) {
		return true
	}
	if isAllowedSemanticConstructorContext(ctx, call, typeName) {
		return true
	}
	if isAllowedSemanticOwnerAdapterFunc(ctx, call, typeName) {
		return true
	}
	return isConstOrTypeDefinition(ctx, call)
}

func isAllowedTestLiteralDirectCastContext(filename string, call *ast.CallExpr) bool {
	return false
}

func isRepoTestBoundaryFile(filename string) bool {
	return isTestFile(filename) && !isSemanticHygienePolicyFixtureFile(filename)
}

func isSemanticHygienePolicyFixtureFile(filename string) bool {
	return strings.Contains(filename, "/internal/analysis/semantichygiene/testdata/semanticcases/")
}

func isAllowedFixtureDirectCastContext(filename string, funcName string) bool {
	if !(isTestFile(filename) || strings.Contains(filename, "/e2e/") || strings.Contains(filename, "/testdata/")) {
		return false
	}
	return isFixtureFunctionName(funcName)
}

func isFixtureFunctionName(funcName string) bool {
	canonicalFuncName := canonicalName(funcName)
	return strings.HasPrefix(canonicalFuncName, "buildfixture") ||
		strings.HasPrefix(canonicalFuncName, "makefixture") ||
		strings.HasPrefix(canonicalFuncName, "newfixture") ||
		strings.HasSuffix(canonicalFuncName, "fixture")
}

func isAllowedEdgeAdapterDirectCastContext(ctx *analysisContext, call *ast.CallExpr) bool {
	if !isEdgeAdapterFile(ctx.filename(call.Pos())) {
		return false
	}
	return enclosingFuncName(ctx, call) == "SetMetadata"
}
