package i18ncatalog_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	goTypes "go/types"
	"os"
	"path/filepath"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	gomegaTypes "github.com/onsi/gomega/types"
	i18ncatalog "github.com/perber/wiki/internal/analysis/i18ncatalog"
	"golang.org/x/tools/go/analysis"
	analysischeck "golang.org/x/tools/go/analysis/analysistest"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

type analysisFixtureFailures struct{}

func (analysisFixtureFailures) Errorf(format string, args ...any) {
	ginkgo.GinkgoHelper()
	ginkgo.Fail(fmt.Sprintf(format, args...))
}

var _ = ginkgo.Describe("i18n catalog analyzer fixtures", ginkgo.Label("unit"), func() {
	ginkgo.DescribeTable("Go payload policies",
		func(pkg string) {
			analysischeck.Run(analysisFixtureFailures{}, analysischeck.TestData(), i18ncatalog.Analyzer, pkg)
		},
		ginkgo.Entry("gin payloads", "github.com/perber/wiki/internal/analysis/i18ncatalog/testdata/gincases"),
		ginkgo.Entry("OAuth compatibility payloads", "github.com/perber/wiki/internal/wiki/oauth"),
	)
})

var _ = ginkgo.Describe("i18n catalog repository checks", ginkgo.Label("unit"), func() {
	ginkgo.It("reports every static policy class migrated from check-i18n-catalog", func() {
		repoRoot := writeRepositoryPolicyFixture()

		diagnostics, err := i18ncatalog.CheckRepository(repoRoot)

		Expect(err).To(Succeed())
		Expect(diagnostics).To(SatisfyAll(
			ContainElement(matchRepositoryDiagnostic("internal/localization/messages.go", `catalog missing message "ui.missing"`)),
			ContainElement(matchRepositoryDiagnostic("internal/localization/locales/active.en.toml", `catalog message "ui.changed"`)),
			ContainElement(matchRepositoryDiagnostic("internal/localization/locales/active.en.toml", `catalog has stale message "ui.stale"`)),
			ContainElement(matchRepositoryDiagnostic("internal/localization/locales/translate.fr.toml", "committed translate.* catalog files are forbidden")),
			ContainElement(matchRepositoryDiagnostic("internal/localization/locales/fr/translate.active.toml", "committed translate.* catalog files are forbidden")),
			ContainElement(matchRepositoryDiagnostic("internal/wiki/mcp/tool_descriptors.go", `catalog missing generated MCP descriptor message "mcp.tools.wiki_move_page.description"`)),
			ContainElement(matchRepositoryDiagnostic("internal/core/errors.go", `catalog missing ErrorCode-derived message "errors.widget.missing"`)),
			ContainElement(matchRepositoryDiagnostic("internal/core/errors.go", `catalog missing production message "warnings.link_rewrite.unsupported_syntax" for constant rewriteWarningUnsupportedSyntax`)),
			ContainElement(matchRepositoryDiagnostic("internal/core/non_alias_messages.go", `catalog missing production message "warnings.non_alias.missing" for constant nonAliasSharedErrorMessage`)),
			ContainElement(matchRepositoryDiagnostic("scripts/run_messages.sh", `generated run message "LEAFWIKI_RUN_MSG_USAGE" is stale`)),
			ContainElement(matchRepositoryDiagnostic("e2e/page.spec.ts", "E2E behavior tests must assert semantic IDs/status")),
			ContainElement(matchRepositoryDiagnostic("scripts/run.sh", "failure body literal must use generated catalog messages")),
		))
		Expect(countDiagnosticsContaining(diagnostics, `mcp.tools.wiki_move_page.description`)).To(Equal(1))
	})

	ginkgo.It("emits repository diagnostics once from the localization analyzer package", func() {
		repoRoot := writeRepositoryPolicyFixture()

		anchorDiagnostics, err := runAnalyzerOnFixturePackage(repoRoot, "internal/localization/messages.go", "github.com/perber/wiki/internal/localization")
		Expect(err).To(Succeed())
		Expect(anchorDiagnostics).To(ContainElement(matchRepositoryDiagnostic("internal/localization/locales/active.en.toml", `catalog message "ui.changed"`)))
		Expect(anchorDiagnostics).To(ContainElement(matchRepositoryDiagnostic("e2e/page.spec.ts", "E2E behavior tests must assert semantic IDs/status")))

		otherPackageDiagnostics, err := runAnalyzerOnFixturePackage(repoRoot, "internal/core/errors.go", "github.com/perber/wiki/internal/core")
		Expect(err).To(Succeed())
		Expect(otherPackageDiagnostics).NotTo(ContainElement(matchRepositoryDiagnostic("internal/localization/locales/active.en.toml", "catalog")))
		Expect(otherPackageDiagnostics).NotTo(ContainElement(matchRepositoryDiagnostic("e2e/page.spec.ts", "E2E behavior tests")))
	})

	ginkgo.It("skips repository diagnostics when no LeafWiki module root is visible", func() {
		repoRoot, err := os.MkdirTemp("", "i18ncatalog-repository-*")
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(func() {
			ginkgo.GinkgoHelper()
			Expect(os.RemoveAll(repoRoot)).To(Succeed())
		})
		writeFixtureFile(repoRoot, "internal/localization/messages.go", "package localization\n")

		diagnostics, err := runAnalyzerOnFixturePackage(repoRoot, "internal/localization/messages.go", "github.com/perber/wiki/internal/localization")

		Expect(err).To(Succeed())
		Expect(diagnostics).To(BeEmpty())
	})

	ginkgo.It("returns repository setup errors from the localization analyzer package", func() {
		repoRoot := temporaryRepositoryRoot()
		writeFixtureFile(repoRoot, "go.mod", "module github.com/perber/wiki\n\ngo 1.25.0\n")
		writeFixtureFile(repoRoot, "internal/localization/messages.go", "package localization\n")

		diagnostics, err := runAnalyzerOnFixturePackage(repoRoot, "internal/localization/messages.go", "github.com/perber/wiki/internal/localization")

		Expect(diagnostics).To(BeEmpty())
		Expect(observeRepositoryCheckOutcome(err)).To(Equal(repositoryCheckReturnedError))
	})

	ginkgo.It("reports missing repository files through analyzer diagnostics", func() {
		repoRoot := writeRepositoryPolicyFixture()
		Expect(os.Remove(filepath.Join(repoRoot, "scripts/run_messages.sh"))).To(Succeed())

		diagnostics, err := runAnalyzerOnFixturePackage(repoRoot, "internal/localization/messages.go", "github.com/perber/wiki/internal/localization")

		Expect(err).To(Succeed())
		Expect(diagnostics).To(ContainElement(matchRepositoryDiagnostic("internal/localization/messages.go", "scripts/run_messages.sh: generated run messages file is missing")))
	})
})

func matchRepositoryDiagnostic(pathSuffix string, messageSubstring string) gomegaTypes.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Path":   Satisfy(func(path string) bool { return strings.HasSuffix(path, filepath.FromSlash(pathSuffix)) }),
		"Detail": ContainSubstring(messageSubstring),
	})
}

func countDiagnosticsContaining(diagnostics []i18ncatalog.RepositoryDiagnostic, messageSubstring string) int {
	count := 0
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Detail, messageSubstring) {
			count++
		}
	}
	return count
}

func runAnalyzerOnFixturePackage(repoRoot string, rel string, pkgPath string) ([]i18ncatalog.RepositoryDiagnostic, error) {
	ginkgo.GinkgoHelper()

	fileSet := token.NewFileSet()
	path := filepath.Join(repoRoot, filepath.FromSlash(rel))
	file, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		return nil, err
	}
	var diagnostics []i18ncatalog.RepositoryDiagnostic
	pass := &analysis.Pass{
		Analyzer: i18ncatalog.Analyzer,
		Fset:     fileSet,
		Files:    []*ast.File{file},
		Pkg:      goTypes.NewPackage(pkgPath, filepath.Base(filepath.Dir(path))),
		TypesInfo: &goTypes.Info{
			Types: map[ast.Expr]goTypes.TypeAndValue{},
		},
		ResultOf: map[*analysis.Analyzer]any{
			inspect.Analyzer: inspector.New([]*ast.File{file}),
		},
		Report: func(diagnostic analysis.Diagnostic) {
			position := fileSet.Position(diagnostic.Pos)
			diagnostics = append(diagnostics, i18ncatalog.RepositoryDiagnostic{
				Path:   position.Filename,
				Line:   position.Line,
				Detail: diagnostic.Message,
			})
		},
	}
	_, err = i18ncatalog.Analyzer.Run(pass)
	return diagnostics, err
}

func writeRepositoryPolicyFixture() string {
	ginkgo.GinkgoHelper()

	repoRoot := ginkgo.GinkgoT().TempDir()
	writeFixtureFile(repoRoot, "go.mod", "module github.com/perber/wiki\n\ngo 1.25.0\n")
	writeFixtureFile(repoRoot, "internal/localization/messages.go", `package localization

import "github.com/nicksnyder/go-i18n/v2/i18n"

const (
	MessageIDAPIExample = "api.example.success"
	MessageIDMissing    = "ui.missing"
	MessageIDChanged    = "ui.changed"
	MessageIDShellUsage = "shell.run.usage"
)

var registryMessages = []*i18n.Message{
	{ID: MessageIDAPIExample, Description: "Example success.", Other: "Catalog text"},
	{ID: MessageIDMissing, Description: "Missing entry.", Other: "Missing text"},
	{ID: MessageIDChanged, Description: "Changed entry.", Other: "Changed text"},
	{ID: MessageIDShellUsage, Description: "Shell usage.", Other: "Usage text"},
}
`)
	writeFixtureFile(repoRoot, "internal/localization/shell/export.go", `package shell

import "github.com/perber/wiki/internal/localization"

func messages() {
	_ = []struct {
		name string
		id   string
	}{
		{"LEAFWIKI_RUN_MSG_USAGE", localization.MessageIDShellUsage},
	}
}
`)
	writeFixtureFile(repoRoot, "internal/localization/locales/active.en.toml", `["api.example.success"]
description = "Example success."
other = "Catalog text"

["ui.changed"]
description = "Changed entry."
other = "Old text"

["ui.stale"]
description = "Stale entry."
other = "Stale text"

["shell.run.usage"]
description = "Shell usage."
other = "Usage text"
`)
	writeFixtureFile(repoRoot, "internal/localization/locales/translate.fr.toml", `["api.example.success"]
other = "Texte"
`)
	writeFixtureFile(repoRoot, "internal/localization/locales/fr/translate.active.toml", `["api.example.success"]
other = "Texte"
`)
	writeFixtureFile(repoRoot, "internal/wiki/mcp/tool_descriptors.go", `package mcp

type ToolID string
type ToolDescriptionID string

const (
	ToolMovePage            ToolID            = "wiki_move_page"
	ToolDescriptionMovePage ToolDescriptionID = "mcp.tools.wiki_move_page.description"
)
`)
	writeFixtureFile(repoRoot, "internal/core/errors.go", `package core

import sharederrors "github.com/perber/wiki/internal/core/shared/errors"

const ErrCodeWidgetMissing sharederrors.ErrorCode = "widget_missing"

const rewriteWarningUnsupportedSyntax sharederrors.MessageID = "warnings.link_rewrite.unsupported_syntax"
`)
	writeFixtureFile(repoRoot, "internal/core/non_alias_messages.go", `package core

import "github.com/perber/wiki/internal/core/shared/errors"

const nonAliasSharedErrorMessage errors.MessageID = "warnings.non_alias.missing"
`)
	writeFixtureFile(repoRoot, "scripts/run_messages.sh", "#!/usr/bin/env bash\nLEAFWIKI_RUN_MSG_USAGE='Old usage'\n# LEAFWIKI_RUN_MSG_USAGE='Usage text'\n")
	writeFixtureFile(repoRoot, "scripts/run.sh", "#!/usr/bin/env bash\nfail \"Raw failure\"\n")
	writeFixtureFile(repoRoot, "e2e/page.spec.ts", "await expect(page.getByText('Catalog text')).toBeVisible();\n")
	return repoRoot
}

func writeFixtureFile(repoRoot string, rel string, contents string) {
	ginkgo.GinkgoHelper()

	path := filepath.Join(repoRoot, filepath.FromSlash(rel))
	Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
	Expect(os.WriteFile(path, []byte(contents), 0o644)).To(Succeed())
}
