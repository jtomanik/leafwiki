package mcp

import (
	"context"
	"errors"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = Describe("Workspace sync tool helpers", Label("integration"), func() {
	It("propagates hard sync errors", func() {
		expected := errors.New("capture failed")
		routes := &Routes{
			workspaceSyncRefresh: func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
				return workspacesync.SyncStatus{Enabled: true}, expected
			},
		}
		actor := toolActor{ID: "editor", User: &auth.User{ID: "editor", Username: "editor", Role: auth.RoleEditor}}

		_, err := routes.refreshWorkspaceSync(context.Background(), actor, refreshInput{})
		Expect(err).To(MatchError(expected))
	})

	It("returns validation status without modeling it as a tool error", func() {
		rootDir := filepath.Join(mcpTestTempDir(), "content")
		dataDir := filepath.Join(mcpTestTempDir(), "data")
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
		Expect(err).To(Succeed())
		validation := out.Validation
		Expect(validation).NotTo(BeNil())
		Expect(*validation).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Summary": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Errors": Equal(1),
			}),
			"Issues": HaveExactElements(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Path":    Equal("<root-dir>/a.md"),
				"Message": ContainSubstring("<data-dir>/.leafwiki/scan"),
			})),
		}))

		Expect(out.SyncStatus).To(SatisfyAll(
			HaveKeyWithValue("lastErrorDetail", BeNil()),
			HaveKeyWithValue("validationErrorDetails", HaveExactElements(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Path":    Equal("<root-dir>/a.md"),
				"Message": ContainSubstring("<data-dir>/.leafwiki/scan"),
			}))),
		))
	})

	It("propagates hard errors even when validation status is present", func() {
		expected := errors.New("search rebuild failed")
		routes := &Routes{
			workspaceSyncRefresh: func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
				return workspacesync.SyncStatus{
					Enabled: true,
					ValidationErrors: []workspacesync.ValidationError{
						{Path: "a.md", Message: "duplicate leafwiki_id"},
					},
				}, expected
			},
		}
		actor := toolActor{ID: "editor", User: &auth.User{ID: "editor", Username: "editor", Role: auth.RoleEditor}}

		_, err := routes.refreshWorkspaceSync(context.Background(), actor, refreshInput{})
		Expect(err).To(MatchError(expected))
	})
})
