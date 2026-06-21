package pagesave

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/links"
	"github.com/perber/wiki/internal/search"
	"github.com/perber/wiki/internal/workspacesync"
)

func TestNewLinkIndexSideEffect_DefaultsLogger(t *testing.T) {
	treeService := tree.NewTreeService(t.TempDir())
	store, err := links.NewLinksStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLinksStore failed: %v", err)
	}
	effect := NewLinkIndexSideEffect(links.NewLinkService(t.TempDir(), treeService, store), nil)
	if effect.log == nil {
		t.Fatal("expected default logger to be set")
	}
	if effect.log != slog.Default() {
		t.Fatal("expected slog.Default() logger")
	}
}

func TestNewSearchIndexSideEffect_DefaultsLogger(t *testing.T) {
	treeService := tree.NewTreeService(t.TempDir())
	index, err := search.NewSQLiteIndex(t.TempDir())
	if err != nil {
		t.Fatalf("NewSQLiteIndex failed: %v", err)
	}
	defer func() {
		if err := index.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	}()

	effect := NewSearchIndexSideEffect(index, treeService, nil)
	if effect.log == nil {
		t.Fatal("expected default logger to be set")
	}
	if effect.log != slog.Default() {
		t.Fatal("expected slog.Default() logger")
	}
}

func TestNewTagsSideEffect_DefaultsLogger(t *testing.T) {
	effect := NewTagsSideEffect(nil, nil)
	if effect.log == nil {
		t.Fatal("expected default logger to be set")
	}
	if effect.log != slog.Default() {
		t.Fatal("expected slog.Default() logger")
	}
}

func TestNewPropertiesSideEffect_DefaultsLogger(t *testing.T) {
	effect := NewPropertiesSideEffect(nil, nil)
	if effect.log == nil {
		t.Fatal("expected default logger to be set")
	}
	if effect.log != slog.Default() {
		t.Fatal("expected slog.Default() logger")
	}
}

func TestPageSaveOrchestrator_ReturnsRequiredEffectErrorBeforeBestEffortEffects(t *testing.T) {
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
}

func TestWorkspaceSyncSideEffect_UsesMCPSourceFromPageEvent(t *testing.T) {
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
}

func TestWorkspaceSyncSideEffect_UsesResolvedActorMetadata(t *testing.T) {
	syncer := &captureWorkspaceSyncer{}
	effect := NewWorkspaceSyncSideEffectWithActorLookup(syncer, nil, func(userID string) workspacesync.Actor {
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
}

func TestWorkspaceSyncSideEffect_AllowsRetryAfterCaptureFailure(t *testing.T) {
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
}

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
