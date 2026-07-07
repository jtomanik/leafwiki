package pagesave

import (
	"context"
	"errors"
	"log/slog"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/links"
	"github.com/perber/wiki/internal/search"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = ginkgo.Describe("page save side effects", func() {
	ginkgo.Describe("constructor defaults", func() {
		ginkgo.It("assigns the default logger to link indexing", ginkgo.Label("integration"), func() {
			dir := tempPagesaveDir()
			treeService := tree.NewTreeService(dir)
			store, err := links.NewLinksStore(dir)
			Expect(err).To(Succeed())
			ginkgo.DeferCleanup(func() {
				Expect(store.Close()).To(Succeed())
			})

			effect := NewLinkIndexSideEffect(links.NewLinkService(dir, treeService, store), nil)

			Expect(effect.log).To(BeIdenticalTo(slog.Default()))
		})

		ginkgo.It("assigns the default logger to search indexing", ginkgo.Label("integration"), func() {
			dir := tempPagesaveDir()
			treeService := tree.NewTreeService(dir)
			index, err := search.NewSQLiteIndex(dir)
			Expect(err).To(Succeed())
			ginkgo.DeferCleanup(func() {
				Expect(index.Close()).To(Succeed())
			})

			effect := NewSearchIndexSideEffect(index, treeService, nil)

			Expect(effect.log).To(BeIdenticalTo(slog.Default()))
		})

		ginkgo.It("assigns the default logger to tag indexing", ginkgo.Label("unit"), func() {
			effect := NewTagsSideEffect(nil, nil)

			Expect(effect.log).To(BeIdenticalTo(slog.Default()))
		})

		ginkgo.It("assigns the default logger to property indexing", ginkgo.Label("unit"), func() {
			effect := NewPropertiesSideEffect(nil, nil)

			Expect(effect.log).To(BeIdenticalTo(slog.Default()))
		})
	})

	ginkgo.Describe("link indexing", ginkgo.Label("integration"), func() {
		ginkgo.It("records outgoing markdown links for created pages", func() {
			_, treeService, linkService, effect := setupLinkSideEffect()
			source := createMarkdownPage(treeService, "Source Page", newFixtureSlug("source-page"), "[Target](/target-page)")

			effect.Apply(PageSaveEvent{
				Operation: PageOperationCreate,
				After:     source,
			})

			status, err := linkService.GetLinkStatusForPage(source.ID, source.CalculateRoutePath())
			Expect(err).To(Succeed())
			Expect(status).To(haveSingleBrokenOutgoingLink(source.ID, tree.RoutePathFromString("/target-page")))
		})
	})

	ginkgo.Describe("orchestration", ginkgo.Label("unit"), func() {
		ginkgo.It("returns required effect errors before running best-effort effects", func() {
			expected := errors.New("required sync failed")
			required := &failingRequiredEffect{err: expected}
			bestEffort := &countingSideEffect{}
			orchestrator := NewPageSaveOrchestrator(required, bestEffort)

			Expect(orchestrator.Run(PageSaveEvent{Operation: PageOperationUpdate})).To(MatchError(expected))
			Expect(required.calls).To(Equal(1))
			Expect(bestEffort.calls).To(BeZero())
		})
	})

	ginkgo.Describe("orchestration with real side effects", ginkgo.Label("integration"), func() {
		ginkgo.It("runs required workspace sync before best-effort link indexing", func() {
			_, treeService, linkService, linkEffect := setupLinkSideEffect()
			source := createMarkdownPage(treeService, "Source Page", newFixtureSlug("source-page"), "[Target](/target-page)")
			syncService, err := workspacesync.NewService(workspacesync.ServiceOptions{Enabled: false})
			Expect(err).To(Succeed())
			syncEffect := NewWorkspaceSyncSideEffect(syncService, nil)
			orchestrator := NewPageSaveOrchestrator(syncEffect, linkEffect)

			Expect(orchestrator.Run(PageSaveEvent{
				Operation: PageOperationUpdate,
				After:     source,
				UserID:    newFixtureUserID("alice"),
				Source:    PageMutationSourceMCP,
			})).To(Succeed())

			status, err := linkService.GetLinkStatusForPage(source.ID, source.CalculateRoutePath())
			Expect(err).To(Succeed())
			Expect(status).To(haveSingleBrokenOutgoingLink(source.ID, tree.RoutePathFromString("/target-page")))
		})
	})

	ginkgo.Describe("workspace sync", ginkgo.Label("unit"), func() {
		ginkgo.It("maps MCP page mutations to MCP sync source and actor metadata", func() {
			syncer := &captureWorkspaceSyncer{}
			effect := NewWorkspaceSyncSideEffect(syncer, nil)

			Expect(effect.ApplyRequired(PageSaveEvent{
				Operation: PageOperationCreate,
				UserID:    newFixtureUserID("alice"),
				Source:    PageMutationSourceMCP,
			})).To(Succeed())

			Expect(syncer.req).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Source": Equal(workspacesync.SourceMCP),
				"Actor": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"ID": Equal(workspacesync.ActorIDFromUserID(newFixtureUserID("alice"))),
				}),
			}))
		})

		ginkgo.It("uses resolved actor metadata for workspace sync requests", func() {
			syncer := &captureWorkspaceSyncer{}
			effect := NewWorkspaceSyncSideEffectWithActorLookup(syncer, nil, func(userID tree.UserID) workspacesync.Actor {
				Expect(userID).To(Equal(newFixtureUserID("alice")))
				return workspacesync.Actor{ID: "alice", Name: "Alice", Email: "alice@example.test"}
			})

			Expect(effect.ApplyRequired(PageSaveEvent{
				Operation: PageOperationUpdate,
				UserID:    newFixtureUserID("alice"),
				Source:    PageMutationSourceWeb,
			})).To(Succeed())

			Expect(syncer.req.Actor).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ID":    Equal(workspacesync.ActorIDFromUserID(newFixtureUserID("alice"))),
				"Name":  Equal("Alice"),
				"Email": Equal("alice@example.test"),
			}))
		})

		ginkgo.It("allows a required sync retry after a capture failure", func() {
			expected := errors.New("git capture failed")
			syncer := &retryWorkspaceSyncer{err: expected}
			effect := NewWorkspaceSyncSideEffect(syncer, nil)
			event := PageSaveEvent{Operation: PageOperationUpdate, UserID: newFixtureUserID("alice")}

			Expect(effect.ApplyRequired(event)).To(MatchError(expected))
			Expect(effect.ApplyRequired(event)).To(Succeed())
			Expect(syncer.calls).To(Equal(2))
		})

		ginkgo.It("logs best-effort sync failures without surfacing them", func() {
			expected := errors.New("sync failed")
			syncer := &retryWorkspaceSyncer{err: expected}
			effect := NewWorkspaceSyncSideEffect(syncer, nil)

			Expect(func() {
				effect.Apply(PageSaveEvent{
					Operation: PageOperationUpdate,
					UserID:    newFixtureUserID("alice"),
					Source:    PageMutationSourceWeb,
				})
			}).NotTo(Panic())

			Expect(syncer.calls).To(Equal(1))
		})
	})
})

type failingRequiredEffect struct {
	err   error
	calls int
}

func (e *failingRequiredEffect) Apply(PageSaveEvent) {}

func (e *failingRequiredEffect) ApplyRequired(PageSaveEvent) error {
	e.calls++
	return e.err
}

type countingSideEffect struct {
	calls int
}

func (e *countingSideEffect) Apply(PageSaveEvent) {
	e.calls++
}

type captureWorkspaceSyncer struct {
	req workspacesync.SyncRequest
}

func (s *captureWorkspaceSyncer) SyncNow(_ context.Context, req workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
	s.req = req
	return workspacesync.SyncStatus{}, nil
}

type retryWorkspaceSyncer struct {
	calls int
	err   error
}

func (s *retryWorkspaceSyncer) SyncNow(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
	s.calls++
	if s.calls == 1 {
		return workspacesync.SyncStatus{}, s.err
	}
	return workspacesync.SyncStatus{}, nil
}

func haveSingleBrokenOutgoingLink(fromPageID tree.PageID, targetPath tree.RoutePath) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Outgoings":       BeEmpty(),
		"BrokenOutgoings": ConsistOf(matchUnresolvedOutgoingLink(fromPageID, targetPath)),
		"Counts": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Outgoings":       BeZero(),
			"BrokenOutgoings": Equal(1),
		}),
	}))
}

func matchUnresolvedOutgoingLink(fromPageID tree.PageID, targetPath tree.RoutePath) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"FromPageID": Equal(fromPageID),
		"ToPath":     Equal(targetPath),
		"ToPageID":   BeEmpty(),
	})
}
