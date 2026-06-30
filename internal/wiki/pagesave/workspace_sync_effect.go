package pagesave

import (
	"context"
	"log/slog"
	"strings"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync"
)

type workspaceSyncer interface {
	SyncNow(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error)
}

type WorkspaceSyncSideEffect struct {
	svc          workspaceSyncer
	log          *slog.Logger
	actorForUser func(tree.UserID) workspacesync.Actor
}

func NewWorkspaceSyncSideEffect(svc workspaceSyncer, log *slog.Logger) *WorkspaceSyncSideEffect {
	return NewWorkspaceSyncSideEffectWithActorLookup(svc, log, nil)
}

func NewWorkspaceSyncSideEffectWithActorLookup(svc workspaceSyncer, log *slog.Logger, actorForUser func(tree.UserID) workspacesync.Actor) *WorkspaceSyncSideEffect {
	if log == nil {
		log = slog.Default()
	}
	return &WorkspaceSyncSideEffect{svc: svc, log: log, actorForUser: actorForUser}
}

func (e *WorkspaceSyncSideEffect) Apply(event PageSaveEvent) {
	if err := e.ApplyRequired(event); err != nil {
		e.log.Warn("failed to sync workspace after page mutation", "operation", event.Operation, "error", err)
	}
}

func (e *WorkspaceSyncSideEffect) ApplyRequired(event PageSaveEvent) error {
	if e.svc == nil {
		return nil
	}
	actorID := workspacesync.ActorIDFromUserID(event.UserID)
	if actorID == "" {
		actorID = workspacesync.PublicEditorActor().ID
	}
	actor := workspacesync.Actor{ID: actorID}
	if e.actorForUser != nil {
		resolved := e.actorForUser(event.UserID)
		if workspacesync.ActorIDIsEmpty(resolved.ID) {
			resolved.ID = actorID
		}
		actor = resolved
	}
	if _, err := e.svc.SyncNow(context.Background(), workspacesync.SyncRequest{
		Reason: workspacesync.ReasonWebWrite,
		Source: workspaceSyncSourceForPageEvent(event.Source),
		Actor:  actor,
	}); err != nil {
		return err
	}
	return nil
}

func workspaceSyncSourceForPageEvent(source string) workspacesync.Source {
	switch strings.TrimSpace(source) {
	case PageMutationSourceMCP:
		return workspacesync.SourceMCP
	default:
		return workspacesync.SourceWeb
	}
}
