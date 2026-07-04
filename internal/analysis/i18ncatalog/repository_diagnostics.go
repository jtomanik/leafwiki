package i18ncatalog

import "strings"

type RepositoryDiagnostic struct {
	Path   string
	Line   int
	Detail string
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
