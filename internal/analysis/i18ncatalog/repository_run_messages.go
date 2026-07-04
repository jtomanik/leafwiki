package i18ncatalog

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

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
