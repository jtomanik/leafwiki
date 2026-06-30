package pagesave

import (
	"context"
	"errors"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = ginkgo.Describe("page save side-effect orchestration", func() {
	ginkgo.It("runs required and best-effort effects in registration order", func() {
		required := &failingRequiredEffect{}
		bestEffort := &countingSideEffect{}
		orchestrator := NewPageSaveOrchestrator(required, bestEffort)

		Expect(orchestrator.Run(PageSaveEvent{Operation: PageOperationUpdate})).To(Succeed())

		Expect(required.calls).To(Equal(1))
		Expect(bestEffort.calls).To(Equal(1))
	})

	ginkgo.It("treats nil side-effect dependencies as no-ops", func() {
		page := &tree.Page{PageNode: &tree.PageNode{ID: tree.PageIDFromString("page-1")}}
		event := PageSaveEvent{
			Operation:     PageOperationDelete,
			After:         page,
			AffectedPages: []*tree.Page{page},
		}

		NewTagsSideEffect(nil, nil).Apply(event)
		NewPropertiesSideEffect(nil, nil).Apply(event)
		searchEffect := NewSearchIndexSideEffect(nil, nil, nil)
		searchEffect.Apply(event)
		Expect(searchEffect.IndexAllPages()).To(Succeed())
	})

	ginkgo.It("uses default workspace sync actor and source when optional context is absent", func() {
		syncer := &captureWorkspaceSyncer{}
		effect := NewWorkspaceSyncSideEffect(syncer, nil)

		Expect(effect.ApplyRequired(PageSaveEvent{Operation: PageOperationRestore})).To(Succeed())

		Expect(syncer.req.Source).To(Equal(workspacesync.SourceWeb))
		Expect(syncer.req.Actor.ID).To(Equal(workspacesync.PublicEditorActor().ID))
	})

	ginkgo.It("keeps the fallback actor ID when actor lookup returns an empty ID", func() {
		syncer := &captureWorkspaceSyncer{}
		effect := NewWorkspaceSyncSideEffectWithActorLookup(syncer, nil, func(tree.UserID) workspacesync.Actor {
			return workspacesync.Actor{Name: "Resolved"}
		})

		Expect(effect.ApplyRequired(PageSaveEvent{UserID: newFixtureUserID("alice")})).To(Succeed())

		Expect(syncer.req.Actor).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ID":   Equal(workspacesync.ActorIDFromUserID(newFixtureUserID("alice"))),
			"Name": Equal("Resolved"),
		}))
	})

	ginkgo.It("returns nil for nil workspace sync services and surfaces sync failures for required effects", func() {
		nilEffect := NewWorkspaceSyncSideEffect(nil, nil)
		Expect(nilEffect.ApplyRequired(PageSaveEvent{})).To(Succeed())

		expected := errors.New("sync failed")
		failing := NewWorkspaceSyncSideEffect(&alwaysFailingWorkspaceSyncer{err: expected}, nil)
		Expect(failing.ApplyRequired(PageSaveEvent{})).To(MatchError(expected))
	})
})

type alwaysFailingWorkspaceSyncer struct {
	err error
}

func (s *alwaysFailingWorkspaceSyncer) SyncNow(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
	return workspacesync.SyncStatus{}, s.err
}
