package pagesave

import (
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync"
)

type pagesaveWorkspaceSyncObservation struct {
	Outcome   pagesaveWorkspaceSyncOutcome
	LastError string
}

type pagesaveWorkspaceSyncOutcome string

const (
	pagesaveWorkspaceSyncCaptured pagesaveWorkspaceSyncOutcome = "captured"
	pagesaveWorkspaceSyncPending  pagesaveWorkspaceSyncOutcome = "pending"
)

func observePagesaveWorkspaceSync(status workspacesync.SyncStatus) pagesaveWorkspaceSyncObservation {
	if status.Enabled && status.LastCommitHash != "" && status.LastError == "" {
		return pagesaveWorkspaceSyncObservation{Outcome: pagesaveWorkspaceSyncCaptured}
	}
	return pagesaveWorkspaceSyncObservation{
		Outcome:   pagesaveWorkspaceSyncPending,
		LastError: status.LastError,
	}
}

var _ = ginkgo.Describe("page save workspace sync integration", ginkgo.Label("integration"), func() {
	ginkgo.It("captures real workspace changes before link indexing observes the saved page", func() {
		dir, treeService, linkService, linkEffect := setupLinkSideEffect()
		createMarkdownPage(treeService, "Target Page", newFixtureSlug("target-page"), "Target")
		source := createMarkdownPage(treeService, "Synced Source", newFixtureSlug("synced-source"), "[Target](/target-page)")
		syncService, err := workspacesync.NewService(workspacesync.ServiceOptions{
			Enabled: true,
			DataDir: filepath.Join(dir, "sync-data"),
			RootDir: treeService.RootDir(),
			Tree:    treeService,
		})
		Expect(err).To(Succeed())
		syncEffect := NewWorkspaceSyncSideEffectWithActorLookup(syncService, nil, func(userID tree.UserID) workspacesync.Actor {
			Expect(userID).To(Equal(newFixtureUserID("alice")))
			return workspacesync.Actor{
				ID:    workspacesync.ActorIDFromUserID(userID),
				Name:  "Alice",
				Email: "alice@example.test",
			}
		})
		orchestrator := NewPageSaveOrchestrator(syncEffect, linkEffect)

		Expect(orchestrator.Run(PageSaveEvent{
			Operation: PageOperationUpdate,
			After:     source,
			UserID:    newFixtureUserID("alice"),
			Source:    PageMutationSourceMCP,
		})).To(Succeed())

		Expect(syncService.Status()).To(WithTransform(
			observePagesaveWorkspaceSync,
			Equal(pagesaveWorkspaceSyncObservation{Outcome: pagesaveWorkspaceSyncCaptured}),
		))
		status, err := linkService.GetLinkStatusForPage(source.ID, source.CalculateRoutePath())
		Expect(err).To(Succeed())
		Expect(status).To(haveSingleBrokenOutgoingLink(source.ID, tree.RoutePathFromString("/target-page")))
	})
})
