package mcp

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/workspacesync"
)

func TestRefreshWorkspaceSyncPropagatesHardSyncErrors(t *testing.T) {
	routes := &Routes{
		workspaceSyncRefresh: func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
			return workspacesync.SyncStatus{Enabled: true}, errors.New("capture failed")
		},
	}
	actor := toolActor{ID: "editor", User: &auth.User{ID: "editor", Username: "editor", Role: auth.RoleEditor}}

	_, err := routes.refreshWorkspaceSync(context.Background(), actor, refreshInput{})
	if err == nil || !strings.Contains(err.Error(), "capture failed") {
		t.Fatalf("refreshWorkspaceSync error = %v, want hard sync error", err)
	}
}

func TestRefreshWorkspaceSyncReturnsValidationStatusWithoutToolError(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "content")
	dataDir := filepath.Join(t.TempDir(), "data")
	routes := &Routes{
		workspaceRootDir: rootDir,
		workspaceDataDir: dataDir,
		workspaceSyncRefresh: func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
			return workspacesync.SyncStatus{
				Enabled: true,
				ValidationErrors: []workspacesync.ValidationError{
					{
						Path:    filepath.Join(rootDir, "a.md"),
						Message: "duplicate leafwiki_id in " + filepath.Join(dataDir, ".leafwiki", "scan"),
					},
				},
			}, nil
		},
	}
	actor := toolActor{ID: "editor", User: &auth.User{ID: "editor", Username: "editor", Role: auth.RoleEditor}}

	out, err := routes.refreshWorkspaceSync(context.Background(), actor, refreshInput{})
	if err != nil {
		t.Fatalf("refreshWorkspaceSync returned error for validation-only failure: %v", err)
	}
	validation := out.Validation
	if validation == nil || validation.Summary.Errors != 1 {
		t.Fatalf("validation = %#v, want one validation error", validation)
	}
	if len(validation.Issues) != 1 || validation.Issues[0].Path != "<root-dir>/a.md" || !strings.Contains(validation.Issues[0].Message, "<data-dir>/.leafwiki/scan") {
		t.Fatalf("validation issues = %#v, want redacted root/data paths", validation.Issues)
	}
	status, ok := out.SyncStatus.(map[string]any)
	if !ok {
		t.Fatalf("syncStatus has type %T: %#v", out.SyncStatus, out.SyncStatus)
	}
	if lastErrorDetail, ok := status["lastErrorDetail"].(*sharederrors.LocalizedErrorDetail); ok && lastErrorDetail != nil {
		t.Fatalf("syncStatus = %#v, did not want validation status modeled as tool error", status)
	}
	validationErrors, ok := status["validationErrorDetails"].([]workspacesync.ValidationError)
	if !ok || len(validationErrors) != 1 {
		t.Fatalf("syncStatus validationErrorDetails = %#v, want one validation error", status["validationErrorDetails"])
	}
	if validationErrors[0].Path != "<root-dir>/a.md" || !strings.Contains(validationErrors[0].Message, "<data-dir>/.leafwiki/scan") {
		t.Fatalf("syncStatus validationErrorDetails = %#v, want redacted root/data paths", validationErrors)
	}
}

func TestRefreshWorkspaceSyncPropagatesHardErrorsEvenWithValidationStatus(t *testing.T) {
	routes := &Routes{
		workspaceSyncRefresh: func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
			return workspacesync.SyncStatus{
				Enabled: true,
				ValidationErrors: []workspacesync.ValidationError{
					{Path: "a.md", Message: "duplicate leafwiki_id"},
				},
			}, errors.New("search rebuild failed")
		},
	}
	actor := toolActor{ID: "editor", User: &auth.User{ID: "editor", Username: "editor", Role: auth.RoleEditor}}

	_, err := routes.refreshWorkspaceSync(context.Background(), actor, refreshInput{})
	if err == nil || !strings.Contains(err.Error(), "search rebuild failed") {
		t.Fatalf("refreshWorkspaceSync error = %v, want hard sync error", err)
	}
}
