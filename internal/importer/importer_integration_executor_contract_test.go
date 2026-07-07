package importer_test

import (
	"log/slog"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/importer"
	"github.com/perber/wiki/internal/wiki"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

func integWorkspaceSourcePath(raw string) tree.WorkspaceSourcePath {
	return tree.WorkspaceSourcePathFromString(raw)
}

func integRoutePath(raw string) tree.RoutePath {
	return tree.RoutePathFromString(raw)
}

func integExecutorPlan(
	wikiAdapter *wiki.WikiImportAdapter,
	items ...importer.PlanItem,
) *importer.PlanResult {
	return &importer.PlanResult{
		ID:       "integration-executor-plan",
		TreeHash: wikiAdapter.TreeHash(),
		Items:    items,
		Errors:   []string{},
	}
}

func newIntegrationExecutor(
	plan *importer.PlanResult,
	sourceBasePath string,
	wikiAdapter *wiki.WikiImportAdapter,
) *importer.Executor {
	return importer.NewExecutor(
		plan,
		&importer.PlanOptions{SourceBasePath: sourceBasePath},
		0,
		wikiAdapter,
		slog.Default(),
	)
}

type integExecutionErrorState string

const (
	integExecutionErrorAbsent  integExecutionErrorState = "absent"
	integExecutionErrorPresent integExecutionErrorState = "present"
)

type integExecutionItemErrorObservation struct {
	SourcePath tree.WorkspaceSourcePath
	TargetPath tree.RoutePath
	Action     importer.ExecutionAction
	Code       importer.ImportErrorCode
	Error      integExecutionErrorState
}

func matchExecutionItemError(
	sourcePath tree.WorkspaceSourcePath,
	targetPath tree.RoutePath,
	code importer.ImportErrorCode,
) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(observeExecutionItemError, Equal(integExecutionItemErrorObservation{
		SourcePath: sourcePath,
		TargetPath: targetPath,
		Action:     importer.ExecutionActionSkipped,
		Code:       code,
		Error:      integExecutionErrorPresent,
	}))
}

func observeExecutionItemError(item importer.ExecutionItemResult) integExecutionItemErrorObservation {
	errorState := integExecutionErrorAbsent
	if item.Error != nil && *item.Error != "" {
		errorState = integExecutionErrorPresent
	}
	return integExecutionItemErrorObservation{
		SourcePath: item.SourcePath,
		TargetPath: item.TargetPath,
		Action:     item.Action,
		Code:       item.ErrorCode,
		Error:      errorState,
	}
}

var _ = ginkgo.Describe("import executor contracts through real wiki storage", ginkgo.Label("integration"), func() {
	ginkgo.It("stops before the next item when cancellation is requested", func() {
		ws := integTempDir()
		integMustWrite(ws, "Canceled.md", "# Canceled\nBody")

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		wikiAdapter := wiki.NewWikiImportAdapter(w)
		plan := integExecutorPlan(wikiAdapter, importer.PlanItem{
			SourcePath: integWorkspaceSourcePath("Canceled.md"),
			TargetPath: integRoutePath("canceled"),
			Title:      "Canceled",
			Kind:       tree.NodeKindPage,
			Action:     importer.PlanActionCreate,
		})

		result, err := newIntegrationExecutor(plan, ws, wikiAdapter).
			WithCancelCheck(func() bool { return true }).
			Execute(integFixtureUserID("system"))

		Expect(err).To(MatchError(importer.ErrImportCanceled))
		Expect(result).To(SatisfyAll(
			HaveField("ImportedCount", BeZero()),
			HaveField("SkippedCount", BeZero()),
			HaveField("Items", BeEmpty()),
			HaveField("TreeHashBefore", Not(BeEmpty())),
		))
	})

	ginkgo.It("rejects resume state without the prior tree hash", func() {
		ws := integTempDir()
		integMustWrite(ws, "Resume.md", "# Resume\nBody")

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		wikiAdapter := wiki.NewWikiImportAdapter(w)
		plan := integExecutorPlan(wikiAdapter, importer.PlanItem{
			SourcePath: integWorkspaceSourcePath("Resume.md"),
			TargetPath: integRoutePath("resume"),
			Title:      "Resume",
			Kind:       tree.NodeKindPage,
			Action:     importer.PlanActionCreate,
		})

		_, err := newIntegrationExecutor(plan, ws, wikiAdapter).
			WithResumeState(1, &importer.ExecutionResult{}).
			Execute(integFixtureUserID("system"))

		Expect(err).To(MatchError(importer.ErrImportResumeTreeHashMissing))
	})

	ginkgo.It("records missing source files after the target path is ensured", func() {
		ws := integTempDir()

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		wikiAdapter := wiki.NewWikiImportAdapter(w)
		plan := integExecutorPlan(wikiAdapter, importer.PlanItem{
			SourcePath: integWorkspaceSourcePath("Missing.md"),
			TargetPath: integRoutePath("missing"),
			Title:      "Missing",
			Kind:       tree.NodeKindPage,
			Action:     importer.PlanActionCreate,
		})

		result, err := newIntegrationExecutor(plan, ws, wikiAdapter).
			Execute(integFixtureUserID("system"))

		Expect(err).To(Succeed())
		Expect(result).To(SatisfyAll(
			HaveField("ImportedCount", BeZero()),
			HaveField("SkippedCount", Equal(1)),
			HaveField("Items", ConsistOf(matchExecutionItemError(
				integWorkspaceSourcePath("Missing.md"),
				integRoutePath("missing"),
				importer.ImportErrorCodeLoadSourceFailed,
			))),
		))
	})

	ginkgo.It("records unknown stored actions as skipped results", func() {
		ws := integTempDir()

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		wikiAdapter := wiki.NewWikiImportAdapter(w)
		plan := integExecutorPlan(wikiAdapter, importer.PlanItem{
			SourcePath: integWorkspaceSourcePath("Unknown.md"),
			TargetPath: integRoutePath("unknown"),
			Title:      "Unknown",
			Kind:       tree.NodeKindPage,
			Action:     importer.PlanAction("unknown-action"),
		})

		result, err := newIntegrationExecutor(plan, ws, wikiAdapter).
			Execute(integFixtureUserID("system"))

		Expect(err).To(Succeed())
		Expect(result).To(SatisfyAll(
			HaveField("ImportedCount", BeZero()),
			HaveField("SkippedCount", Equal(1)),
			HaveField("Items", ConsistOf(matchExecutionItemError(
				integWorkspaceSourcePath("Unknown.md"),
				integRoutePath("unknown"),
				importer.ImportErrorCodeUnknownAction,
			))),
		))
	})
})
