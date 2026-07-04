package i18ncatalog

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

type RepositoryDiagnostic struct {
	Path   string
	Line   int
	Detail string
}

type catalogEntry struct {
	ID              string
	Description     string
	Other           string
	SectionLine     int
	DescriptionLine int
	OtherLine       int
}

type messageDefinition struct {
	ID          string
	Description string
	Other       string
	Path        string
	Line        int
}

type runMessagePair struct {
	Name string
	ID   string
	Path string
	Line int
}

func CheckRepository(root string) ([]RepositoryDiagnostic, error) {
	repoRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	catalogPath := filepath.Join(repoRoot, "internal/localization/locales/active.en.toml")
	catalog, err := parseCatalog(catalogPath)
	if err != nil {
		return nil, err
	}
	definitions, localizationConstants, err := extractLocalizationDefinitions(repoRoot)
	if err != nil {
		return nil, err
	}

	var diagnostics []RepositoryDiagnostic
	diagnostics = append(diagnostics, checkCatalogParity(catalogPath, catalog, definitions)...)
	diagnostics = append(diagnostics, checkTranslateCatalogs(repoRoot)...)
	diagnostics = append(diagnostics, checkProductionMessageObligations(repoRoot, catalog)...)
	diagnostics = append(diagnostics, checkMCPDescriptorMessages(repoRoot, catalog)...)
	diagnostics = append(diagnostics, checkRunMessages(repoRoot, catalog, localizationConstants)...)
	diagnostics = append(diagnostics, checkE2ELocalizedProse(repoRoot, catalog)...)
	diagnostics = append(diagnostics, checkShellFailureBodies(repoRoot)...)
	sort.SliceStable(diagnostics, func(i, j int) bool {
		if diagnostics[i].Path != diagnostics[j].Path {
			return diagnostics[i].Path < diagnostics[j].Path
		}
		if diagnostics[i].Line != diagnostics[j].Line {
			return diagnostics[i].Line < diagnostics[j].Line
		}
		return diagnostics[i].Detail < diagnostics[j].Detail
	})
	diagnostics = deduplicateMCPDescriptionMissingCatalogDiagnostics(diagnostics)
	return diagnostics, nil
}

func deduplicateMCPDescriptionMissingCatalogDiagnostics(diagnostics []RepositoryDiagnostic) []RepositoryDiagnostic {
	seen := map[string]struct{}{}
	out := diagnostics[:0]
	for _, diagnostic := range diagnostics {
		id, ok := missingMCPDescriptionCatalogID(diagnostic.Detail)
		if ok {
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
		}
		out = append(out, diagnostic)
	}
	return out
}

func missingMCPDescriptionCatalogID(detail string) (string, bool) {
	for _, prefix := range []string{
		`catalog missing generated MCP descriptor message "`,
		`catalog missing production message "`,
	} {
		if id, ok := strings.CutPrefix(detail, prefix); ok {
			id, _, ok = strings.Cut(id, `"`)
			return id, ok && strings.HasPrefix(id, "mcp.tools.") && strings.HasSuffix(id, ".description")
		}
	}
	return "", false
}

func checkCatalogParity(catalogPath string, catalog map[string]catalogEntry, definitions []messageDefinition) []RepositoryDiagnostic {
	var diagnostics []RepositoryDiagnostic
	defined := map[string]messageDefinition{}
	for _, definition := range definitions {
		if strings.TrimSpace(definition.ID) == "" {
			diagnostics = append(diagnostics, repositoryDiagnostic(definition.Path, definition.Line, "catalog message definition has empty ID"))
			continue
		}
		if strings.TrimSpace(definition.Other) == "" {
			diagnostics = append(diagnostics, repositoryDiagnostic(definition.Path, definition.Line, fmt.Sprintf(`catalog message "%s" has empty default text`, definition.ID)))
			continue
		}
		if previous, ok := defined[definition.ID]; ok {
			if previous.Other != definition.Other {
				diagnostics = append(diagnostics, repositoryDiagnostic(definition.Path, definition.Line, fmt.Sprintf(`catalog message "%s" has conflicting defaults`, definition.ID)))
			}
			continue
		}
		defined[definition.ID] = definition

		entry, ok := catalog[definition.ID]
		if !ok {
			diagnostics = append(diagnostics, repositoryDiagnostic(definition.Path, definition.Line, fmt.Sprintf(`catalog missing message "%s" in internal/localization/locales/active.en.toml`, definition.ID)))
			continue
		}
		if entry.Description != definition.Description {
			diagnostics = append(diagnostics, repositoryDiagnostic(catalogPath, entryLine(entry.DescriptionLine, entry.SectionLine), fmt.Sprintf(`catalog message "%s" description = %q, want %q`, definition.ID, entry.Description, definition.Description)))
		}
		if entry.Other != definition.Other {
			diagnostics = append(diagnostics, repositoryDiagnostic(catalogPath, entryLine(entry.OtherLine, entry.SectionLine), fmt.Sprintf(`catalog message "%s" other = %q, want %q`, definition.ID, entry.Other, definition.Other)))
		}
	}
	for id, entry := range catalog {
		if _, ok := defined[id]; !ok {
			diagnostics = append(diagnostics, repositoryDiagnostic(catalogPath, entry.SectionLine, fmt.Sprintf(`catalog has stale message "%s" with no localization definition`, id)))
		}
	}
	return diagnostics
}

func entryLine(fieldLine int, fallback int) int {
	if fieldLine > 0 {
		return fieldLine
	}
	return fallback
}

func checkTranslateCatalogs(repoRoot string) []RepositoryDiagnostic {
	localesDir := filepath.Join(repoRoot, "internal/localization/locales")
	var diagnostics []RepositoryDiagnostic
	_ = filepath.WalkDir(localesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasPrefix(d.Name(), "translate.") {
			return nil
		}
		diagnostics = append(diagnostics, repositoryDiagnostic(path, 1, "committed translate.* catalog files are forbidden during the English-only phase"))
		return nil
	})
	return diagnostics
}

func checkProductionMessageObligations(repoRoot string, catalog map[string]catalogEntry) []RepositoryDiagnostic {
	var diagnostics []RepositoryDiagnostic
	for _, rootName := range []string{"internal", "cmd"} {
		root := filepath.Join(repoRoot, rootName)
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case "analysis", "localization":
					return filepath.SkipDir
				default:
					return nil
				}
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			diagnostics = append(diagnostics, checkProductionConstants(path, catalog)...)
			return nil
		})
	}
	return diagnostics
}

func checkProductionConstants(path string, catalog map[string]catalogEntry) []RepositoryDiagnostic {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		return nil
	}
	sharedErrorsAliases := importAliasesForPath(file, "github.com/perber/wiki/internal/core/shared/errors")
	var diagnostics []RepositoryDiagnostic
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			values, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, name := range values.Names {
				value, ok := stringValueForIndex(values.Values, index, nil)
				if !ok {
					continue
				}
				line := fileSet.Position(name.Pos()).Line
				if isProductionMessageConstant(name.Name) || isProductionMessageIDType(values.Type, sharedErrorsAliases) {
					if _, ok := catalog[value]; !ok {
						diagnostics = append(diagnostics, repositoryDiagnostic(path, line, fmt.Sprintf(`catalog missing production message "%s" for constant %s`, value, name.Name)))
					}
				}
				if isErrorCodeConstant(name.Name) && isErrorCodeType(values.Type) {
					messageID := messageIDForErrorCode(value)
					if _, ok := catalog[messageID]; !ok {
						diagnostics = append(diagnostics, repositoryDiagnostic(path, line, fmt.Sprintf(`catalog missing ErrorCode-derived message "%s" for constant %s`, messageID, name.Name)))
					}
				}
			}
		}
	}
	return diagnostics
}

func isProductionMessageConstant(name string) bool {
	return strings.HasPrefix(name, "MessageID") ||
		strings.HasPrefix(name, "ToolMessage") ||
		strings.HasPrefix(name, "ToolDescription")
}

func isProductionMessageIDType(expr ast.Expr, sharedErrorsAliases map[string]struct{}) bool {
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		ident, ok := e.X.(*ast.Ident)
		if !ok || e.Sel.Name != "MessageID" {
			return false
		}
		_, ok = sharedErrorsAliases[ident.Name]
		return ok
	case *ast.Ident:
		switch e.Name {
		case "MessageID", "ToolMessageID", "ToolDescriptionID":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func importAliasesForPath(file *ast.File, importPath string) map[string]struct{} {
	aliases := map[string]struct{}{}
	for _, importSpec := range file.Imports {
		value, ok := stringLiteral(importSpec.Path)
		if !ok || value != importPath {
			continue
		}
		if importSpec.Name != nil {
			if importSpec.Name.Name != "_" && importSpec.Name.Name != "." {
				aliases[importSpec.Name.Name] = struct{}{}
			}
			continue
		}
		aliases[defaultImportName(importPath)] = struct{}{}
	}
	return aliases
}

func defaultImportName(importPath string) string {
	_, name, ok := strings.Cut(filepath.ToSlash(importPath), "/")
	for ok {
		_, name, ok = strings.Cut(name, "/")
	}
	if name == "" {
		return importPath
	}
	return name
}

func isErrorCodeConstant(name string) bool {
	return strings.HasPrefix(name, "ErrCode") ||
		strings.HasPrefix(name, "errCode") ||
		strings.HasPrefix(name, "runtimeErrorCode")
}

func isErrorCodeType(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "ErrorCode"
}

func messageIDForErrorCode(code string) string {
	head, tail, ok := strings.Cut(strings.TrimSpace(code), "_")
	if !ok || strings.TrimSpace(tail) == "" {
		return "errors." + code
	}
	return "errors." + head + "." + tail
}

func checkMCPDescriptorMessages(repoRoot string, catalog map[string]catalogEntry) []RepositoryDiagnostic {
	path := filepath.Join(repoRoot, "internal/wiki/mcp/tool_descriptors.go")
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		return nil
	}
	var diagnostics []RepositoryDiagnostic
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			values, ok := spec.(*ast.ValueSpec)
			if !ok || !isToolIDType(values.Type) {
				continue
			}
			for index, name := range values.Names {
				value, ok := stringValueForIndex(values.Values, index, nil)
				if !ok {
					continue
				}
				messageID := "mcp.tools." + value + ".description"
				if _, ok := catalog[messageID]; !ok {
					diagnostics = append(diagnostics, repositoryDiagnostic(path, fileSet.Position(name.Pos()).Line, fmt.Sprintf(`catalog missing generated MCP descriptor message "%s" for tool %s`, messageID, name.Name)))
				}
			}
		}
	}
	return diagnostics
}

func isToolIDType(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "ToolID"
}

func checkRunMessages(repoRoot string, catalog map[string]catalogEntry, constants map[string]string) []RepositoryDiagnostic {
	pairs := extractRunMessagePairs(repoRoot, constants)
	if len(pairs) == 0 {
		return nil
	}
	runMessagesPath := filepath.Join(repoRoot, "scripts/run_messages.sh")
	raw, err := os.ReadFile(runMessagesPath)
	if err != nil {
		return []RepositoryDiagnostic{repositoryDiagnostic(runMessagesPath, 1, "generated run messages file is missing")}
	}
	actual := string(raw)
	assignments := runMessageAssignments(actual)
	assignmentsByName := map[string][]runMessageAssignment{}
	for _, assignment := range assignments {
		assignmentsByName[assignment.Name] = append(assignmentsByName[assignment.Name], assignment)
	}

	expectedValues := map[string]string{}
	var diagnostics []RepositoryDiagnostic
	for _, pair := range pairs {
		entry, ok := catalog[pair.ID]
		if !ok {
			diagnostics = append(diagnostics, repositoryDiagnostic(pair.Path, pair.Line, fmt.Sprintf(`catalog missing shell run message "%s" for %s`, pair.ID, pair.Name)))
			continue
		}
		expectedValues[pair.Name] = "'" + shellSingleQuote(entry.Other) + "'"
	}
	if len(diagnostics) > 0 {
		return diagnostics
	}

	expectedNames := map[string]struct{}{}
	for _, pair := range pairs {
		expectedNames[pair.Name] = struct{}{}
		assignmentsForName := assignmentsByName[pair.Name]
		switch len(assignmentsForName) {
		case 0:
			diagnostics = append(diagnostics, repositoryDiagnostic(runMessagesPath, 1, fmt.Sprintf(`generated run message "%s" is missing`, pair.Name)))
		case 1:
			if assignmentsForName[0].RawValue != expectedValues[pair.Name] {
				diagnostics = append(diagnostics, repositoryDiagnostic(runMessagesPath, assignmentsForName[0].Line, fmt.Sprintf(`generated run message "%s" is stale for catalog message "%s"`, pair.Name, pair.ID)))
			}
		default:
			diagnostics = append(diagnostics, repositoryDiagnostic(runMessagesPath, assignmentsForName[1].Line, fmt.Sprintf(`generated run message "%s" is duplicated`, pair.Name)))
		}
	}
	for _, assignment := range assignments {
		if _, ok := expectedNames[assignment.Name]; !ok {
			diagnostics = append(diagnostics, repositoryDiagnostic(runMessagesPath, assignment.Line, fmt.Sprintf(`generated run message "%s" is extra`, assignment.Name)))
		}
	}
	if len(diagnostics) > 0 {
		return diagnostics
	}

	expectedFile := generatedRunMessagesContent(pairs, expectedValues)
	if actual != expectedFile {
		diagnostics = append(diagnostics, repositoryDiagnostic(runMessagesPath, firstDifferentLine(actual, expectedFile), "generated run messages file layout is stale; regenerate from localization catalog"))
	}
	return diagnostics
}

type runMessageAssignment struct {
	Name     string
	RawValue string
	Line     int
}

func runMessageAssignments(content string) []runMessageAssignment {
	var assignments []runMessageAssignment
	offset := 0
	lineNumber := 1
	for offset < len(content) {
		lineStart := offset
		lineEnd := strings.IndexByte(content[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(content)
		} else {
			lineEnd += lineStart
		}
		line := content[lineStart:lineEnd]
		if strings.HasPrefix(line, "LEAFWIKI_RUN_MSG_") {
			nameEnd := strings.IndexByte(line, '=')
			if nameEnd > 0 {
				valueStart := lineStart + nameEnd + 1
				valueEnd := shellAssignmentEnd(content, valueStart)
				assignments = append(assignments, runMessageAssignment{
					Name:     line[:nameEnd],
					RawValue: content[valueStart:valueEnd],
					Line:     lineNumber,
				})
				nextOffset := valueEnd
				if nextOffset < len(content) && content[nextOffset] == '\n' {
					nextOffset++
				}
				lineNumber += strings.Count(content[offset:nextOffset], "\n")
				offset = nextOffset
				continue
			}
		}
		if lineEnd >= len(content) {
			break
		}
		offset = lineEnd + 1
		lineNumber++
	}
	return assignments
}

func shellAssignmentEnd(content string, start int) int {
	inSingleQuote := false
	inDoubleQuote := false
	for index := start; index < len(content); index++ {
		switch content[index] {
		case '\'':
			if !inDoubleQuote {
				inSingleQuote = !inSingleQuote
			}
		case '"':
			if !inSingleQuote {
				inDoubleQuote = !inDoubleQuote
			}
		case '\n':
			if !inSingleQuote && !inDoubleQuote {
				return index
			}
		}
	}
	return len(content)
}

func generatedRunMessagesContent(pairs []runMessagePair, values map[string]string) string {
	var out strings.Builder
	out.WriteString("#!/usr/bin/env bash\n")
	out.WriteString("# Generated from internal/localization/locales/active.en.toml.\n\n")
	for _, pair := range pairs {
		out.WriteString(pair.Name)
		out.WriteString("=")
		out.WriteString(values[pair.Name])
		out.WriteString("\n")
	}
	return out.String()
}

func extractRunMessagePairs(repoRoot string, constants map[string]string) []runMessagePair {
	path := filepath.Join(repoRoot, "internal/localization/shell/export.go")
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		return nil
	}
	var pairs []runMessagePair
	ast.Inspect(file, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok || len(lit.Elts) != 2 {
			return true
		}
		name, ok := stringLiteral(lit.Elts[0])
		if !ok || !strings.HasPrefix(name, "LEAFWIKI_RUN_MSG_") {
			return true
		}
		id, ok := resolveStringExpr(lit.Elts[1], constants)
		if !ok {
			return true
		}
		pairs = append(pairs, runMessagePair{
			Name: name,
			ID:   id,
			Path: path,
			Line: fileSet.Position(lit.Pos()).Line,
		})
		return true
	})
	return pairs
}

func shellSingleQuote(value string) string {
	return strings.ReplaceAll(value, "'", "'\"'\"'")
}

func checkE2ELocalizedProse(repoRoot string, catalog map[string]catalogEntry) []RepositoryDiagnostic {
	proseIDs := localizedProseIDs(catalog)
	if len(proseIDs) == 0 {
		return nil
	}
	var diagnostics []RepositoryDiagnostic
	_ = filepath.WalkDir(filepath.Join(repoRoot, "e2e"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".ts") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		content := string(raw)
		for _, assertion := range e2eLocalizedAssertions(content) {
			if id, ok := proseIDs[assertion.literal]; ok {
				diagnostics = append(diagnostics, repositoryDiagnostic(path, lineForOffset(content, assertion.offset), fmt.Sprintf("E2E behavior tests must assert semantic IDs/status instead of migrated localized prose from catalog message %s", id)))
			}
		}
		return nil
	})
	return diagnostics
}

type e2eLiteralAssertion struct {
	literal string
	offset  int
}

func e2eLocalizedAssertions(content string) []e2eLiteralAssertion {
	var assertions []e2eLiteralAssertion
	for _, call := range []string{"getByText", "toHaveText", "toContainText"} {
		searchFrom := 0
		for {
			index := strings.Index(content[searchFrom:], call)
			if index < 0 {
				break
			}
			callStart := searchFrom + index
			open := callStart + len(call)
			for open < len(content) && isHorizontalSpace(content[open]) {
				open++
			}
			if open >= len(content) || content[open] != '(' {
				searchFrom = callStart + len(call)
				continue
			}
			quote := open + 1
			for quote < len(content) && isHorizontalSpace(content[quote]) {
				quote++
			}
			if quote >= len(content) || !isTSQuote(content[quote]) {
				searchFrom = callStart + len(call)
				continue
			}
			literal, end, ok := readTSLiteral(content, quote)
			if ok {
				assertions = append(assertions, e2eLiteralAssertion{literal: literal, offset: quote + 1})
				searchFrom = end + 1
				continue
			}
			searchFrom = quote + 1
		}
	}
	return assertions
}

func isHorizontalSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

func isTSQuote(b byte) bool {
	return b == '\'' || b == '"' || b == '`'
}

func readTSLiteral(content string, quote int) (string, int, bool) {
	quoteByte := content[quote]
	var out strings.Builder
	escaped := false
	for index := quote + 1; index < len(content); index++ {
		b := content[index]
		if escaped {
			out.WriteByte(b)
			escaped = false
			continue
		}
		if b == '\\' {
			escaped = true
			continue
		}
		if b == quoteByte {
			return out.String(), index, true
		}
		out.WriteByte(b)
	}
	return "", 0, false
}

func localizedProseIDs(catalog map[string]catalogEntry) map[string]string {
	out := map[string]string{}
	for id, entry := range catalog {
		if !hasLocalizedProsePrefix(id) || strings.Contains(entry.Other, "{{") || len(entry.Other) < 12 {
			continue
		}
		out[entry.Other] = id
	}
	return out
}

func hasLocalizedProsePrefix(id string) bool {
	return strings.HasPrefix(id, "api.") ||
		strings.HasPrefix(id, "errors.") ||
		strings.HasPrefix(id, "mcp.") ||
		strings.HasPrefix(id, "validation.") ||
		strings.HasPrefix(id, "ui.")
}

func checkShellFailureBodies(repoRoot string) []RepositoryDiagnostic {
	path := filepath.Join(repoRoot, "scripts/run.sh")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	content := string(raw)
	callPattern := regexp.MustCompile(`\b(fail|fail_error|fail_config_argument_error)\s+"((?:\\.|[^"\\])*)"`)
	var diagnostics []RepositoryDiagnostic
	for _, match := range callPattern.FindAllStringSubmatchIndex(content, -1) {
		rawValue := content[match[4]:match[5]]
		value, err := strconv.Unquote(`"` + rawValue + `"`)
		if err != nil || shellFailureBodyAllowed(value) {
			continue
		}
		diagnostics = append(diagnostics, repositoryDiagnostic(path, lineForOffset(content, match[4]), "failure body literal must use generated catalog messages"))
	}
	return diagnostics
}

func shellFailureBodyAllowed(value string) bool {
	return strings.HasPrefix(value, "$(") || strings.Contains(value, "$") || strings.Contains(value, "{{")
}

func parseCatalog(path string) (map[string]catalogEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var parsed map[string]struct {
		Description string `toml:"description"`
		Other       string `toml:"other"`
	}
	if err := toml.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	entries := make(map[string]catalogEntry, len(parsed))
	for id, entry := range parsed {
		entries[id] = catalogEntry{ID: id, Description: entry.Description, Other: entry.Other}
	}
	currentID := ""
	for lineNumber, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[\"") && strings.HasSuffix(trimmed, "\"]") {
			unquoted, err := strconv.Unquote(trimmed[1 : len(trimmed)-1])
			if err == nil {
				currentID = unquoted
				entry := entries[currentID]
				entry.SectionLine = lineNumber + 1
				entries[currentID] = entry
			}
			continue
		}
		if currentID == "" {
			continue
		}
		entry := entries[currentID]
		switch {
		case strings.HasPrefix(trimmed, "description"):
			entry.DescriptionLine = lineNumber + 1
		case strings.HasPrefix(trimmed, "other"):
			entry.OtherLine = lineNumber + 1
		}
		entries[currentID] = entry
	}
	return entries, nil
}

func extractLocalizationDefinitions(repoRoot string) ([]messageDefinition, map[string]string, error) {
	localizationDir := filepath.Join(repoRoot, "internal/localization")
	files, err := parseGoFiles(localizationDir)
	if err != nil {
		return nil, nil, err
	}
	constants := map[string]string{}
	for _, parsed := range files {
		for _, decl := range parsed.file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				values, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for index, name := range values.Names {
					if value, ok := stringValueForIndex(values.Values, index, constants); ok {
						constants[name.Name] = value
					}
				}
			}
		}
	}
	var definitions []messageDefinition
	for _, parsed := range files {
		ast.Inspect(parsed.file, func(node ast.Node) bool {
			lit, ok := node.(*ast.CompositeLit)
			if !ok || !isI18nMessageDefinitionLiteral(lit) {
				return true
			}
			fields := messageLiteralFields(lit, constants)
			id := fields["ID"]
			if id == "" {
				return true
			}
			definitions = append(definitions, messageDefinition{
				ID:          id,
				Description: fields["Description"],
				Other:       fields["Other"],
				Path:        parsed.path,
				Line:        parsed.fileSet.Position(lit.Pos()).Line,
			})
			return true
		})
	}
	return definitions, constants, nil
}

type parsedGoFile struct {
	path    string
	fileSet *token.FileSet
	file    *ast.File
}

func parseGoFiles(dir string) ([]parsedGoFile, error) {
	var files []parsedGoFile
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		files = append(files, parsedGoFile{path: path, fileSet: fileSet, file: file})
		return nil
	})
	return files, err
}

func isI18nMessageDefinitionLiteral(lit *ast.CompositeLit) bool {
	if lit.Type != nil {
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Message" {
			return false
		}
	}
	fields := map[string]struct{}{}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		fields[key.Name] = struct{}{}
	}
	_, hasID := fields["ID"]
	_, hasDescription := fields["Description"]
	_, hasOther := fields["Other"]
	return hasID && hasDescription && hasOther
}

func messageLiteralFields(lit *ast.CompositeLit, constants map[string]string) map[string]string {
	fields := map[string]string{}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		value, ok := resolveStringExpr(kv.Value, constants)
		if !ok {
			continue
		}
		fields[key.Name] = value
	}
	return fields
}

func stringValueForIndex(values []ast.Expr, index int, constants map[string]string) (string, bool) {
	if len(values) == 0 {
		return "", false
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return resolveStringExpr(values[index], constants)
}

func resolveStringExpr(expr ast.Expr, constants map[string]string) (string, bool) {
	if value, ok := stringLiteral(expr); ok {
		return value, true
	}
	switch e := expr.(type) {
	case *ast.Ident:
		value, ok := constants[e.Name]
		return value, ok
	case *ast.SelectorExpr:
		value, ok := constants[e.Sel.Name]
		return value, ok
	default:
		return "", false
	}
}

func repositoryDiagnostic(path string, line int, detail string) RepositoryDiagnostic {
	if line < 1 {
		line = 1
	}
	return RepositoryDiagnostic{Path: path, Line: line, Detail: detail}
}

func lineForOffset(content string, offset int) int {
	if offset < 0 {
		return 1
	}
	return strings.Count(content[:offset], "\n") + 1
}

func firstDifferentLine(actual string, expected string) int {
	line := 1
	limit := min(len(actual), len(expected))
	for index := range limit {
		if actual[index] != expected[index] {
			return line
		}
		if actual[index] == '\n' {
			line++
		}
	}
	return line
}
