package pagesave

import (
	"context"
	"errors"
	ginkgo "github.com/onsi/ginkgo/v2"
	"log/slog"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/links"
	"github.com/perber/wiki/internal/search"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = ginkgo.It("TestNewLinkIndexSideEffect_DefaultsLogger", func() {
	t := ginkgo.GinkgoT()
	treeService := tree.NewTreeService(t.TempDir())
	store, err := links.NewLinksStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLinksStore failed: %v", err)
	}
	ginkgo.DeferCleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("LinksStore.Close: %v", err)
		}
	})
	effect := NewLinkIndexSideEffect(links.NewLinkService(t.TempDir(), treeService, store), nil)
	if effect.log == nil {
		t.Fatal("expected default logger to be set")
	}
	if effect.log != slog.Default() {
		t.Fatal("expected slog.Default() logger")
	}

})

var _ = ginkgo.It("LinkIndexSideEffect Apply create records outgoing markdown links", func() {
	t := ginkgo.GinkgoT()
	dir := t.TempDir()
	treeService := tree.NewTreeService(dir)
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	store, err := links.NewLinksStore(dir)
	if err != nil {
		t.Fatalf("NewLinksStore failed: %v", err)
	}
	ginkgo.DeferCleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("LinksStore.Close: %v", err)
		}
	})
	linkService := links.NewLinkService(dir, treeService, store)
	effect := NewLinkIndexSideEffect(linkService, nil)

	source := createPageWithContent(t, treeService, "Source Page", "source-page", "[Target](/target-page)")

	effect.Apply(PageSaveEvent{
		Operation: PageOperationCreate,
		After:     source,
	})

	outgoing, err := linkService.GetOutgoingLinksForPage(source.ID)
	if err != nil {
		t.Fatalf("GetOutgoingLinksForPage: %v", err)
	}
	if outgoing.Count != 1 {
		t.Fatalf("outgoing count = %d, want 1", outgoing.Count)
	}
	if outgoing.Outgoings[0].FromPageID != source.ID || outgoing.Outgoings[0].ToPath != "/target-page" || !outgoing.Outgoings[0].Broken {
		t.Fatalf("outgoing link = %#v, want broken /target-page record from %q", outgoing.Outgoings[0], source.ID)
	}
})

var _ = ginkgo.It("TestNewSearchIndexSideEffect_DefaultsLogger", func() {
	t := ginkgo.GinkgoT()
	treeService := tree.NewTreeService(t.TempDir())
	index, err := search.NewSQLiteIndex(t.TempDir())
	if err != nil {
		t.Fatalf("NewSQLiteIndex failed: %v", err)
	}
	ginkgo.DeferCleanup(func() {
		if err := index.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	effect := NewSearchIndexSideEffect(index, treeService, nil)
	if effect.log == nil {
		t.Fatal("expected default logger to be set")
	}
	if effect.log != slog.Default() {
		t.Fatal("expected slog.Default() logger")
	}

})

var _ = ginkgo.It("TestNewTagsSideEffect_DefaultsLogger", func() {
	t := ginkgo.GinkgoT()
	effect := NewTagsSideEffect(nil, nil)
	if effect.log == nil {
		t.Fatal("expected default logger to be set")
	}
	if effect.log != slog.Default() {
		t.Fatal("expected slog.Default() logger")
	}

})

var _ = ginkgo.It("TestNewPropertiesSideEffect_DefaultsLogger", func() {
	t := ginkgo.GinkgoT()
	effect := NewPropertiesSideEffect(nil, nil)
	if effect.log == nil {
		t.Fatal("expected default logger to be set")
	}
	if effect.log != slog.Default() {
		t.Fatal("expected slog.Default() logger")
	}

})

var _ = ginkgo.It("TestPageSaveOrchestrator_ReturnsRequiredEffectErrorBeforeBestEffortEffects", func() {
	t := ginkgo.GinkgoT()
	expected := errors.New("required sync failed")
	required := &failingRequiredEffect{err: expected}
	bestEffort := &countingSideEffect{}

	orchestrator := NewPageSaveOrchestrator(required, bestEffort)

	err := orchestrator.Run(PageSaveEvent{Operation: PageOperationUpdate})
	if !errors.Is(err, expected) {
		t.Fatalf("expected required effect error, got %v", err)
	}
	if required.calls != 1 {
		t.Fatalf("expected required effect to be called once, got %d", required.calls)
	}
	if bestEffort.calls != 0 {
		t.Fatalf("expected best-effort effect to be skipped after required failure, got %d calls", bestEffort.calls)
	}

})

var _ = ginkgo.It("TestWorkspaceSyncSideEffect_UsesMCPSourceFromPageEvent", func() {
	t := ginkgo.GinkgoT()
	syncer := &captureWorkspaceSyncer{}
	effect := NewWorkspaceSyncSideEffect(syncer, nil)

	if err := effect.ApplyRequired(PageSaveEvent{
		Operation: PageOperationCreate,
		UserID:    "alice",
		Source:    PageMutationSourceMCP,
	}); err != nil {
		t.Fatalf("ApplyRequired: %v", err)
	}

	if syncer.req.Source != workspacesync.SourceMCP {
		t.Fatalf("sync source = %q, want mcp", syncer.req.Source)
	}
	if syncer.req.Actor.ID != "alice" {
		t.Fatalf("sync actor = %q, want alice", syncer.req.Actor.ID)
	}

})

var _ = ginkgo.It("TestWorkspaceSyncSideEffect_UsesResolvedActorMetadata", func() {
	t := ginkgo.GinkgoT()
	syncer := &captureWorkspaceSyncer{}
	effect := NewWorkspaceSyncSideEffectWithActorLookup(syncer, nil, func(userID tree.UserID) workspacesync.Actor {
		if userID != "alice" {
			t.Fatalf("actor lookup userID = %q, want alice", userID)
		}
		return workspacesync.Actor{ID: "alice", Name: "Alice", Email: "alice@example.test"}
	})

	if err := effect.ApplyRequired(PageSaveEvent{
		Operation: PageOperationUpdate,
		UserID:    "alice",
		Source:    PageMutationSourceWeb,
	}); err != nil {
		t.Fatalf("ApplyRequired: %v", err)
	}

	if syncer.req.Actor.ID != "alice" || syncer.req.Actor.Name != "Alice" || syncer.req.Actor.Email != "alice@example.test" {
		t.Fatalf("sync actor = %#v, want resolved Alice metadata", syncer.req.Actor)
	}

})

var _ = ginkgo.It("TestWorkspaceSyncSideEffect_AllowsRetryAfterCaptureFailure", func() {
	t := ginkgo.GinkgoT()
	expected := errors.New("git capture failed")
	syncer := &retryWorkspaceSyncer{err: expected}
	effect := NewWorkspaceSyncSideEffect(syncer, nil)
	event := PageSaveEvent{Operation: PageOperationUpdate, UserID: "alice"}

	if err := effect.ApplyRequired(event); !errors.Is(err, expected) {
		t.Fatalf("first ApplyRequired error = %v, want %v", err, expected)
	}
	if err := effect.ApplyRequired(event); err != nil {
		t.Fatalf("retry ApplyRequired: %v", err)
	}
	if syncer.calls != 2 {
		t.Fatalf("sync calls = %d, want failed call plus retry", syncer.calls)
	}

})

var _ = ginkgo.It("WorkspaceSyncSideEffect Apply logs and swallows sync errors", func() {
	t := ginkgo.GinkgoT()
	expected := errors.New("sync failed")
	syncer := &retryWorkspaceSyncer{err: expected}
	effect := NewWorkspaceSyncSideEffect(syncer, nil)

	effect.Apply(PageSaveEvent{
		Operation: PageOperationUpdate,
		UserID:    "alice",
		Source:    PageMutationSourceWeb,
	})

	if syncer.calls != 1 {
		t.Fatalf("sync calls = %d, want 1", syncer.calls)
	}
	if syncer.err != expected {
		t.Fatalf("syncer err = %v, want %v", syncer.err, expected)
	}
})

type failingRequiredEffect struct {
	err   error
	calls int
}

func (e *failingRequiredEffect) Apply(PageSaveEvent) {
	t := e
	_ = t
}

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
