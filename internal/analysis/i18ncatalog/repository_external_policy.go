package i18ncatalog

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

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
