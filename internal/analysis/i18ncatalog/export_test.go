package i18ncatalog

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

type RunMessagePairForTest struct {
	Name string
	ID   string
}

type RunMessageAssignmentForTest struct {
	Name     string
	RawValue string
	Line     int
}

type CatalogEntryForTest struct {
	Description     string
	Other           string
	SectionLine     int
	DescriptionLine int
	OtherLine       int
}

type MessageDefinitionForTest struct {
	ID          string
	Description string
	Other       string
	Path        string
	Line        int
}

func GinPayloadPolicyDecisionsForTest(paths []string) []bool {
	decisions := make([]bool, 0, len(paths))
	for _, path := range paths {
		decisions = append(decisions, isGinPayloadPolicyFile(path))
	}
	return decisions
}

func LocalizedErrorDetailHelperDecisionsForTest(names []string) []bool {
	decisions := make([]bool, 0, len(names))
	for _, name := range names {
		decisions = append(decisions, isLocalizedErrorDetailHelperName(name))
	}
	return decisions
}

func SharedErrorsLocalizedErrorDetailFallbackForTest(importSource string, expressionSource string, includeFile bool) bool {
	fileSet := token.NewFileSet()
	source := "package fixture\n" + importSource + "\nvar _ = " + expressionSource + "\n"
	file, err := parser.ParseFile(fileSet, "fixture.go", source, 0)
	if err != nil {
		panic(err)
	}
	expr := firstValueExpression(file)
	pass := &analysis.Pass{
		Fset: fileSet,
		TypesInfo: &types.Info{
			Types: map[ast.Expr]types.TypeAndValue{},
			Uses:  map[*ast.Ident]types.Object{},
		},
	}
	if includeFile {
		pass.Files = []*ast.File{file}
	}
	return isKnownSharedErrorsLocalizedErrorDetailCall(pass, expr)
}

func LeafWikiLocalizedErrorDetailTypeDecisionsForTest() []bool {
	sharedErrors := types.NewPackage("github.com/perber/wiki/internal/core/shared/errors", "errors")
	otherPackage := types.NewPackage("github.com/perber/wiki/internal/other", "other")
	return []bool{
		isLeafWikiLocalizedErrorDetailType(types.NewNamed(types.NewTypeName(token.NoPos, sharedErrors, "LocalizedErrorDetail", nil), nil, nil)),
		isLeafWikiLocalizedErrorDetailType(types.NewNamed(types.NewTypeName(token.NoPos, otherPackage, "LocalizedErrorDetail", nil), nil, nil)),
		isLeafWikiLocalizedErrorDetailType(types.Typ[types.String]),
	}
}

func ExpressionSemanticNamesForTest(expressions []string) []string {
	names := make([]string, 0, len(expressions))
	for _, source := range expressions {
		expr, err := parser.ParseExpr(source)
		if err != nil {
			panic(err)
		}
		names = append(names, exprSemanticName(expr))
	}
	return names
}

func DefaultImportNamesForTest(importPaths []string) []string {
	names := make([]string, 0, len(importPaths))
	for _, importPath := range importPaths {
		names = append(names, defaultImportName(importPath))
	}
	return names
}

func FirstDifferentLinesForTest(pairs [][2]string) []int {
	lines := make([]int, 0, len(pairs))
	for _, pair := range pairs {
		lines = append(lines, firstDifferentLine(pair[0], pair[1]))
	}
	return lines
}

func GeneratedRunMessagesContentForTest(pairs []RunMessagePairForTest, values map[string]string) string {
	runPairs := make([]runMessagePair, 0, len(pairs))
	for _, pair := range pairs {
		runPairs = append(runPairs, runMessagePair{Name: pair.Name, ID: pair.ID})
	}
	return generatedRunMessagesContent(runPairs, values)
}

func RunMessageAssignmentsForTest(content string) []RunMessageAssignmentForTest {
	assignments := runMessageAssignments(content)
	out := make([]RunMessageAssignmentForTest, 0, len(assignments))
	for _, assignment := range assignments {
		out = append(out, RunMessageAssignmentForTest{
			Name:     assignment.Name,
			RawValue: assignment.RawValue,
			Line:     assignment.Line,
		})
	}
	return out
}

func CatalogEntriesForTest(path string) (map[string]CatalogEntryForTest, error) {
	entries, err := parseCatalog(path)
	if err != nil {
		return nil, err
	}
	out := make(map[string]CatalogEntryForTest, len(entries))
	for id, entry := range entries {
		out[id] = CatalogEntryForTest{
			Description:     entry.Description,
			Other:           entry.Other,
			SectionLine:     entry.SectionLine,
			DescriptionLine: entry.DescriptionLine,
			OtherLine:       entry.OtherLine,
		}
	}
	return out, nil
}

func LocalizationDefinitionsForTest(repoRoot string) ([]MessageDefinitionForTest, map[string]string, error) {
	definitions, constants, err := extractLocalizationDefinitions(repoRoot)
	if err != nil {
		return nil, nil, err
	}
	out := make([]MessageDefinitionForTest, 0, len(definitions))
	for _, definition := range definitions {
		out = append(out, MessageDefinitionForTest{
			ID:          definition.ID,
			Description: definition.Description,
			Other:       definition.Other,
			Path:        definition.Path,
			Line:        definition.Line,
		})
	}
	return out, constants, nil
}

func ProductionConstantDiagnosticsForTest(path string, catalogValues map[string]string) []RepositoryDiagnostic {
	return checkProductionConstants(path, catalogEntriesForTest(catalogValues))
}

func TranslateCatalogDiagnosticsForTest(repoRoot string) []RepositoryDiagnostic {
	return checkTranslateCatalogs(repoRoot)
}

func MCPDescriptorDiagnosticsForTest(repoRoot string, catalogValues map[string]string) []RepositoryDiagnostic {
	return checkMCPDescriptorMessages(repoRoot, catalogEntriesForTest(catalogValues))
}

func E2ELocalizedProseDiagnosticsForTest(repoRoot string, catalogValues map[string]string) []RepositoryDiagnostic {
	return checkE2ELocalizedProse(repoRoot, catalogEntriesForTest(catalogValues))
}

func ShellFailureDiagnosticsForTest(repoRoot string) []RepositoryDiagnostic {
	return checkShellFailureBodies(repoRoot)
}

func CatalogParityDiagnosticsForTest(catalog map[string]CatalogEntryForTest, definitions []MessageDefinitionForTest) []RepositoryDiagnostic {
	entries := make(map[string]catalogEntry, len(catalog))
	for id, entry := range catalog {
		entries[id] = catalogEntry{
			ID:              id,
			Description:     entry.Description,
			Other:           entry.Other,
			SectionLine:     entry.SectionLine,
			DescriptionLine: entry.DescriptionLine,
			OtherLine:       entry.OtherLine,
		}
	}
	messageDefinitions := make([]messageDefinition, 0, len(definitions))
	for _, definition := range definitions {
		messageDefinitions = append(messageDefinitions, messageDefinition{
			ID:          definition.ID,
			Description: definition.Description,
			Other:       definition.Other,
			Path:        definition.Path,
			Line:        definition.Line,
		})
	}
	return checkCatalogParity("/repo/internal/localization/locales/active.en.toml", entries, messageDefinitions)
}

func catalogEntriesForTest(values map[string]string) map[string]catalogEntry {
	catalog := make(map[string]catalogEntry, len(values))
	for id, value := range values {
		catalog[id] = catalogEntry{ID: id, Other: value}
	}
	return catalog
}

func ProductionMessageIDTypeDecisionsForTest(expressions []string, aliases []string) []bool {
	aliasSet := map[string]struct{}{}
	for _, alias := range aliases {
		aliasSet[alias] = struct{}{}
	}
	decisions := make([]bool, 0, len(expressions))
	for _, source := range expressions {
		expr, err := parser.ParseExpr(source)
		if err != nil {
			panic(err)
		}
		decisions = append(decisions, isProductionMessageIDType(expr, aliasSet))
	}
	return decisions
}

type ErrorCodeForTest string

func MessageIDsForErrorCodesForTest(codes []ErrorCodeForTest) []string {
	ids := make([]string, 0, len(codes))
	for _, code := range codes {
		ids = append(ids, messageIDForErrorCode(string(code)))
	}
	return ids
}

func RunMessageDiagnosticsForTest(repoRoot string, catalogValues map[string]string, constants map[string]string) []RepositoryDiagnostic {
	catalog := make(map[string]catalogEntry, len(catalogValues))
	for id, value := range catalogValues {
		catalog[id] = catalogEntry{ID: id, Other: value}
	}
	return checkRunMessages(repoRoot, catalog, constants)
}

func E2ELocalizedAssertionLiteralsForTest(content string) []string {
	assertions := e2eLocalizedAssertions(content)
	literals := make([]string, 0, len(assertions))
	for _, assertion := range assertions {
		literals = append(literals, assertion.literal)
	}
	return literals
}

func ShellFailureBodyAllowedDecisionsForTest(values []string) []bool {
	decisions := make([]bool, 0, len(values))
	for _, value := range values {
		decisions = append(decisions, shellFailureBodyAllowed(value))
	}
	return decisions
}

func RepositoryDiagnosticFallbackPositionsForTest(files []*ast.File) []bool {
	pass := &analysis.Pass{Fset: token.NewFileSet(), Files: files}
	first := repositoryDiagnosticFallbackPos(pass)
	pass.Files = nil
	second := repositoryDiagnosticFallbackPos(pass)
	return []bool{first.IsValid(), second.IsValid()}
}

func RepositoryDiagnosticLinesForTest(lines []int) []int {
	out := make([]int, 0, len(lines))
	for _, line := range lines {
		out = append(out, repositoryDiagnostic("/repo/file.go", line, "detail").Line)
	}
	return out
}

func LineForOffsetsForTest(content string, offsets []int) []int {
	lines := make([]int, 0, len(offsets))
	for _, offset := range offsets {
		lines = append(lines, lineForOffset(content, offset))
	}
	return lines
}

func firstValueExpression(file *ast.File) ast.Expr {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			values, ok := spec.(*ast.ValueSpec)
			if ok && len(values.Values) > 0 {
				return values.Values[0]
			}
		}
	}
	panic("fixture expression not found")
}
