package i18ncatalog_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	i18ncatalog "github.com/perber/wiki/internal/analysis/i18ncatalog"
)

var _ = ginkgo.Describe("i18n catalog repository edge contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("reports catalog parity edge diagnostics", func() {
		diagnostics := i18ncatalog.CatalogParityDiagnosticsForTest(
			map[string]i18ncatalog.CatalogEntryForTest{
				"ui.changed": {
					Description: "Old description",
					Other:       "Old text",
					SectionLine: 3,
				},
				"ui.stale": {
					Description: "Stale description",
					Other:       "Stale text",
					SectionLine: 9,
				},
			},
			[]i18ncatalog.MessageDefinitionForTest{
				{ID: " ", Other: "Text", Path: "/repo/internal/localization/messages.go", Line: 1},
				{ID: "ui.empty", Other: " ", Path: "/repo/internal/localization/messages.go", Line: 2},
				{ID: "ui.duplicate", Description: "Description", Other: "First", Path: "/repo/internal/localization/messages.go", Line: 3},
				{ID: "ui.duplicate", Description: "Description", Other: "Second", Path: "/repo/internal/localization/messages.go", Line: 4},
				{ID: "ui.changed", Description: "New description", Other: "New text", Path: "/repo/internal/localization/messages.go", Line: 5},
			},
		)

		Expect(diagnostics).To(ContainElements(
			matchRepositoryDiagnostic("internal/localization/messages.go", "catalog message definition has empty ID"),
			matchRepositoryDiagnostic("internal/localization/messages.go", `catalog message "ui.empty" has empty default text`),
			matchRepositoryDiagnostic("internal/localization/messages.go", `catalog message "ui.duplicate" has conflicting defaults`),
			matchRepositoryDiagnostic("internal/localization/locales/active.en.toml", `catalog message "ui.changed" description = "Old description", want "New description"`),
			matchRepositoryDiagnostic("internal/localization/locales/active.en.toml", `catalog message "ui.changed" other = "Old text", want "New text"`),
			matchRepositoryDiagnostic("internal/localization/locales/active.en.toml", `catalog has stale message "ui.stale"`),
		))
	})

	ginkgo.It("recognizes production message ID type edge shapes", func() {
		Expect(i18ncatalog.ProductionMessageIDTypeDecisionsForTest([]string{
			"errors.MessageID",
			"errors.ErrorCode",
			"factory().MessageID",
			"MessageID",
			"ToolMessageID",
			"ToolDescriptionID",
			"string",
			`"literal"`,
		}, []string{"errors"})).To(Equal([]bool{true, false, false, true, true, true, false, false}))
		Expect(observeErrorCodeMessageIDs(i18ncatalog.MessageIDsForErrorCodesForTest([]i18ncatalog.ErrorCodeForTest{
			"widget_missing",
			"widget",
			"widget_",
		}))).To(Equal(errorCodeMessageIDObservation{
			MissingCodeHasMessageID:     true,
			PlainCodeHasMessageID:       true,
			TrailingUnderscorePreserved: true,
			MessageIDCountMatchesInputs: true,
		}))
	})

	ginkgo.It("reports generated run-message states", func() {
		repoRoot := temporaryRepositoryRoot()
		writeFixtureFile(repoRoot, "internal/localization/shell/export.go", `package shell

func messages() {
	_ = []struct {
		name string
		id   string
	}{
		{"LEAFWIKI_RUN_MSG_USAGE", MessageIDShellUsage},
		{"LEAFWIKI_RUN_MSG_QUOTED", MessageIDQuoted},
	}
}
`)
		writeFixtureFile(repoRoot, "scripts/run_messages.sh", "#!/usr/bin/env bash\nLEAFWIKI_RUN_MSG_USAGE='Usage text'\nLEAFWIKI_RUN_MSG_USAGE='Usage text'\nLEAFWIKI_RUN_MSG_EXTRA='Extra text'\n")

		diagnostics := i18ncatalog.RunMessageDiagnosticsForTest(repoRoot, map[string]string{
			"shell.run.usage":   "Usage text",
			"shell.run.missing": "Missing text",
		}, map[string]string{
			"MessageIDShellUsage": "shell.run.usage",
			"MessageIDQuoted":     "shell.run.missing",
		})

		Expect(diagnostics).To(ContainElements(
			matchRepositoryDiagnostic("scripts/run_messages.sh", `generated run message "LEAFWIKI_RUN_MSG_QUOTED" is missing`),
			matchRepositoryDiagnostic("scripts/run_messages.sh", `generated run message "LEAFWIKI_RUN_MSG_USAGE" is duplicated`),
			matchRepositoryDiagnostic("scripts/run_messages.sh", `generated run message "LEAFWIKI_RUN_MSG_EXTRA" is extra`),
		))
	})

	ginkgo.It("parses catalog sections and localization definitions with constant indirection", func() {
		repoRoot := temporaryRepositoryRoot()
		catalogPath := filepath.Join(repoRoot, "internal/localization/locales/active.en.toml")
		writeFixtureFile(repoRoot, "internal/localization/locales/active.en.toml", `["ui.saved"]
description = "Saved description"
other = "Saved text"
`)
		writeFixtureFile(repoRoot, "internal/localization/messages.go", `package localization

const (
	MessageIDSaved = "ui.saved"
	DefaultSaved = "Saved text"
)

var _ = []struct {
	ID string
	Description string
	Other string
}{
	{ID: MessageIDSaved, Description: "Saved description", Other: DefaultSaved},
	{ID: missing.ID, Description: "Ignored", Other: "Ignored"},
}
`)

		entries, catalogErr := i18ncatalog.CatalogEntriesForTest(catalogPath)
		definitions, constants, definitionsErr := i18ncatalog.LocalizationDefinitionsForTest(repoRoot)

		Expect(catalogErr).To(Succeed())
		Expect(definitionsErr).To(Succeed())
		Expect(entries).To(HaveKeyWithValue("ui.saved", i18ncatalog.CatalogEntryForTest{
			Description:     "Saved description",
			Other:           "Saved text",
			SectionLine:     1,
			DescriptionLine: 2,
			OtherLine:       3,
		}))
		Expect(definitions).To(ContainElement(i18ncatalog.MessageDefinitionForTest{
			ID:          "ui.saved",
			Description: "Saved description",
			Other:       "Saved text",
			Path:        filepath.Join(repoRoot, "internal/localization/messages.go"),
			Line:        13,
		}))
		Expect(constants).To(HaveKeyWithValue("DefaultSaved", "Saved text"))
	})

	ginkgo.It("reports repository policy diagnostics from production and external files", func() {
		repoRoot := temporaryRepositoryRoot()
		productionPath := filepath.Join(repoRoot, "internal/wiki/messages.go")
		writeFixtureFile(repoRoot, "internal/wiki/messages.go", `package wiki

import "github.com/perber/wiki/internal/core/shared/errors"

type MessageID string
type ErrorCode string

const (
	MessageIDKnown MessageID = "ui.known"
	MessageIDMissing MessageID = "ui.missing"
	MessageIDOne, MessageIDTwo MessageID = "ui.one"
	ErrCodeMissing errors.ErrorCode = "widget_missing"
	SharedMissing errors.MessageID = "shared_missing"
)
`)
		writeFixtureFile(repoRoot, "internal/localization/locales/translate.pl.toml", "")
		writeFixtureFile(repoRoot, "internal/wiki/mcp/tool_descriptors.go", `package mcp

type ToolID string

const ToolSearch ToolID = "search"
`)
		writeFixtureFile(repoRoot, "e2e/search.ts", `await expect(page.getByText("Saved visible text")).toBeVisible()
await expect(page.toContainText("Short")).toBeVisible()
`)
		writeFixtureFile(repoRoot, "scripts/run.sh", `fail "raw failure"
fail_error "$LEAFWIKI_RUN_MSG_USAGE"
fail_config_argument_error "bad {{.Template}}"
`)

		catalog := map[string]string{
			"ui.known":  "Known text",
			"ui.one":    "One text",
			"ui.saved":  "Saved visible text",
			"mcp.short": "Short",
		}

		Expect(i18ncatalog.ProductionConstantDiagnosticsForTest(productionPath, catalog)).To(ContainElements(
			matchRepositoryDiagnostic("internal/wiki/messages.go", `catalog missing production message "ui.missing" for constant MessageIDMissing`),
			matchRepositoryDiagnostic("internal/wiki/messages.go", `catalog missing ErrorCode-derived message "errors.widget.missing" for constant ErrCodeMissing`),
			matchRepositoryDiagnostic("internal/wiki/messages.go", `catalog missing production message "shared_missing" for constant SharedMissing`),
		))
		Expect(i18ncatalog.TranslateCatalogDiagnosticsForTest(repoRoot)).To(ContainElement(matchRepositoryDiagnostic("internal/localization/locales/translate.pl.toml", "committed translate.* catalog files are forbidden")))
		Expect(i18ncatalog.MCPDescriptorDiagnosticsForTest(repoRoot, catalog)).To(ContainElement(matchRepositoryDiagnostic("internal/wiki/mcp/tool_descriptors.go", `catalog missing generated MCP descriptor message "mcp.tools.search.description"`)))
		Expect(i18ncatalog.E2ELocalizedProseDiagnosticsForTest(repoRoot, catalog)).To(ContainElement(matchRepositoryDiagnostic("e2e/search.ts", "E2E behavior tests must assert semantic IDs/status")))
		Expect(i18ncatalog.ShellFailureDiagnosticsForTest(repoRoot)).To(ContainElement(matchRepositoryDiagnostic("scripts/run.sh", "failure body literal must use generated catalog messages")))
	})

	ginkgo.It("deduplicates equivalent MCP description misses in the full repository check", func() {
		repoRoot := temporaryRepositoryRoot()
		writeFixtureFile(repoRoot, "internal/localization/locales/active.en.toml", "")
		writeFixtureFile(repoRoot, "internal/localization/messages.go", `package localization
`)
		writeFixtureFile(repoRoot, "internal/wiki/tool_messages.go", `package wiki

type MessageID string

const MessageIDSearchDescription MessageID = "mcp.tools.search.description"
`)
		writeFixtureFile(repoRoot, "internal/wiki/mcp/tool_descriptors.go", `package mcp

type ToolID string

const ToolSearch ToolID = "search"
`)

		diagnostics, err := i18ncatalog.CheckRepository(repoRoot)

		Expect(err).To(Succeed())
		Expect(countRepositoryDiagnosticsContaining(diagnostics, "mcp.tools.search.description")).To(Equal(1))
	})

	ginkgo.It("reports catalog misses before checking generated run-message files", func() {
		repoRoot := temporaryRepositoryRoot()
		writeFixtureFile(repoRoot, "internal/localization/shell/export.go", `package shell

func messages() {
	_ = []struct {
		name string
		id   string
	}{
		{"LEAFWIKI_RUN_MSG_USAGE", MessageIDShellUsage},
	}
}
`)
		writeFixtureFile(repoRoot, "scripts/run_messages.sh", "#!/usr/bin/env bash\n")

		diagnostics := i18ncatalog.RunMessageDiagnosticsForTest(repoRoot, map[string]string{}, map[string]string{
			"MessageIDShellUsage": "shell.run.usage",
		})

		Expect(diagnostics).To(ContainElement(matchRepositoryDiagnostic("internal/localization/shell/export.go", `catalog missing shell run message "shell.run.usage" for LEAFWIKI_RUN_MSG_USAGE`)))
	})

	ginkgo.It("returns repository setup errors before emitting policy diagnostics", func() {
		missingCatalogRoot := temporaryRepositoryRoot()

		invalidCatalogRoot := temporaryRepositoryRoot()
		writeFixtureFile(invalidCatalogRoot, "internal/localization/locales/active.en.toml", "not = [\n")

		invalidGoRoot := temporaryRepositoryRoot()
		writeFixtureFile(invalidGoRoot, "internal/localization/locales/active.en.toml", "")
		writeFixtureFile(invalidGoRoot, "internal/localization/messages.go", "package localization\nconst =\n")

		_, missingCatalogErr := i18ncatalog.CheckRepository(missingCatalogRoot)
		_, invalidCatalogErr := i18ncatalog.CheckRepository(invalidCatalogRoot)
		_, invalidGoErr := i18ncatalog.CheckRepository(invalidGoRoot)

		Expect(observeRepositoryCheckOutcomes(
			missingCatalogErr,
			invalidCatalogErr,
			invalidGoErr,
		)).To(Equal([]repositoryCheckOutcome{
			repositoryCheckReturnedError,
			repositoryCheckReturnedError,
			repositoryCheckReturnedError,
		}))
	})

	ginkgo.It("detects stale generated run-message file layout", func() {
		repoRoot := temporaryRepositoryRoot()
		writeFixtureFile(repoRoot, "internal/localization/shell/export.go", `package shell

func messages() {
	_ = []struct {
		name string
		id   string
	}{
		{"LEAFWIKI_RUN_MSG_USAGE", MessageIDShellUsage},
	}
}
`)
		writeFixtureFile(repoRoot, "scripts/run_messages.sh", "#!/usr/bin/env bash\n# stale heading\n\nLEAFWIKI_RUN_MSG_USAGE='Usage text'\n")

		diagnostics := i18ncatalog.RunMessageDiagnosticsForTest(repoRoot, map[string]string{
			"shell.run.usage": "Usage text",
		}, map[string]string{
			"MessageIDShellUsage": "shell.run.usage",
		})

		Expect(diagnostics).To(ContainElement(matchRepositoryDiagnostic("scripts/run_messages.sh", "generated run messages file layout is stale")))
	})

	ginkgo.It("parses generated run-message shell assignments across quoted values", func() {
		Expect(i18ncatalog.RunMessageAssignmentsForTest("ignored\nLEAFWIKI_RUN_MSG_SINGLE='RUN_MESSAGE_LINE_ONE\nRUN_MESSAGE_LINE_TWO'\nLEAFWIKI_RUN_MSG_DOUBLE=\"RUN_MESSAGE_LINE_ONE\nRUN_MESSAGE_LINE_TWO\"\nLEAFWIKI_RUN_MSG_PLAIN=plain\nLEAFWIKI_RUN_MSG_BROKEN\n")).To(Equal([]i18ncatalog.RunMessageAssignmentForTest{
			{Name: "LEAFWIKI_RUN_MSG_SINGLE", RawValue: "'RUN_MESSAGE_LINE_ONE\nRUN_MESSAGE_LINE_TWO'", Line: 2},
			{Name: "LEAFWIKI_RUN_MSG_DOUBLE", RawValue: "\"RUN_MESSAGE_LINE_ONE\nRUN_MESSAGE_LINE_TWO\"", Line: 4},
			{Name: "LEAFWIKI_RUN_MSG_PLAIN", RawValue: "plain", Line: 6},
		}))
	})

	ginkgo.It("parses E2E localized assertion literal edge shapes", func() {
		literals := i18ncatalog.E2ELocalizedAssertionLiteralsForTest(`
await expect(page.getByText ("Saved value")).toBeVisible()
await expect(page.toHaveText	("Line\\nBreak")).toBeVisible()
await expect(page.toContainText(` + "`Backtick literal`" + `)).toBeVisible()
await expect(page.getByText(value)).toBeVisible()
await expect(page.getByText).toBeVisible()
await expect(page.getByText("unterminated)).toBeVisible()
`)

		Expect(literals).To(Equal([]string{"Saved value", `Line\nBreak`, "Backtick literal"}))
	})

	ginkgo.It("allows shell failure bodies only when they are generated or templated", func() {
		Expect(i18ncatalog.ShellFailureBodyAllowedDecisionsForTest([]string{
			"$(generated_message)",
			"$LEAFWIKI_RUN_MSG_USAGE",
			"{{ template }}",
			"raw failure",
		})).To(Equal([]bool{true, true, true, false}))
	})

	ginkgo.It("keeps repository diagnostic fallback positions deterministic", func() {
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, filepath.Join(temporaryRepositoryRoot(), "fixture.go"), "package fixture\n", 0)
		Expect(err).To(Succeed())

		Expect(i18ncatalog.RepositoryDiagnosticFallbackPositionsForTest([]*ast.File{file})).To(Equal([]bool{true, false}))
	})

	ginkgo.It("normalizes repository diagnostic line numbers", func() {
		Expect(i18ncatalog.RepositoryDiagnosticLinesForTest([]int{-2, 0, 3})).To(Equal([]int{1, 1, 3}))
		Expect(i18ncatalog.LineForOffsetsForTest("first\nsecond\nthird", []int{-1, 0, 6, 13})).To(Equal([]int{1, 1, 2, 3}))
	})
})

type errorCodeMessageIDObservation struct {
	MissingCodeHasMessageID     bool
	PlainCodeHasMessageID       bool
	TrailingUnderscorePreserved bool
	MessageIDCountMatchesInputs bool
}

type errorCodeMessageIDValues []string

type repositoryCheckOutcome uint8

const (
	repositoryCheckReturnedDiagnostics repositoryCheckOutcome = iota
	repositoryCheckReturnedError
)

func observeRepositoryCheckOutcomes(errs ...error) []repositoryCheckOutcome {
	outcomes := make([]repositoryCheckOutcome, 0, len(errs))
	for _, err := range errs {
		outcomes = append(outcomes, observeRepositoryCheckOutcome(err))
	}
	return outcomes
}

func observeRepositoryCheckOutcome(err error) repositoryCheckOutcome {
	if err != nil {
		return repositoryCheckReturnedError
	}
	return repositoryCheckReturnedDiagnostics
}

func observeErrorCodeMessageIDs(ids errorCodeMessageIDValues) errorCodeMessageIDObservation {
	return errorCodeMessageIDObservation{
		MissingCodeHasMessageID:     len(ids) > 0 && strings.HasSuffix(ids[0], ".missing"),
		PlainCodeHasMessageID:       len(ids) > 1 && strings.HasPrefix(ids[1], "errors."),
		TrailingUnderscorePreserved: len(ids) > 2 && strings.HasSuffix(ids[2], "_"),
		MessageIDCountMatchesInputs: len(ids) == 3,
	}
}

func countRepositoryDiagnosticsContaining(diagnostics []i18ncatalog.RepositoryDiagnostic, fragment string) int {
	count := 0
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Detail, fragment) {
			count++
		}
	}
	return count
}

func temporaryRepositoryRoot() string {
	ginkgo.GinkgoHelper()

	repoRoot, err := os.MkdirTemp("", "i18ncatalog-repository-*")
	Expect(err).To(Succeed())
	ginkgo.DeferCleanup(os.RemoveAll, repoRoot)
	return repoRoot
}
