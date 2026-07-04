package wikid

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"path/filepath"
	"time"

	"github.com/perber/wiki/internal/workspaceid"
)

type registryWorkspaceLookupOutcome uint8

const (
	registryWorkspaceResolved registryWorkspaceLookupOutcome = iota + 1
	registryWorkspaceAbsent
)

type registryWorkspaceLookupResult struct {
	Outcome   registryWorkspaceLookupOutcome
	Workspace WorkspaceRecord
}

func lookupRegistryWorkspace(doc RegistryDocument, workspaceID workspaceid.WorkspaceID) registryWorkspaceLookupResult {
	ginkgo.GinkgoHelper()

	workspace, ok := doc.Workspace(workspaceID)
	if ok {
		return registryWorkspaceLookupResult{Outcome: registryWorkspaceResolved, Workspace: workspace}
	}
	return registryWorkspaceLookupResult{Outcome: registryWorkspaceAbsent}
}

var _ = ginkgo.Describe("wikid workspace semantic typing", func() {
	ginkgo.It("preserves typed workspace identifiers across registry, grant, and supervisor models", func() {
		workspaceID := workspaceid.WorkspaceID("home")

		doc := RegistryDocument{
			SchemaVersion: RegistrySchemaVersion,
			Workspaces: []WorkspaceRecord{{
				ID: workspaceID,
			}},
		}
		Expect(lookupRegistryWorkspace(doc, workspaceID)).To(SatisfyAll(
			HaveField("Outcome", Equal(registryWorkspaceResolved)),
			HaveField("Workspace", HaveField("ID", Equal(workspaceID))),
		))

		grant := Grant{Subject: "user:1", WorkspaceID: workspaceID, Role: GrantRoleViewer}
		var _ workspaceid.WorkspaceID = grant.WorkspaceID

		supervisor := NewWorkspaceSupervisor(WorkspaceSupervisorOptions{})
		supervisor.MarkReady(workspaceID, 123, "http://127.0.0.1:41001")
		Expect(supervisor.Status(workspaceID)).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"WorkspaceID": Equal(workspaceID),
			"State":       Equal(WorkspaceStateRunning),
		}))
	})

	ginkgo.It("rejects registry records whose workspace ID contains whitespace before normalization", func() {
		layout := GlobalLayout(filepath.Join(wikidTestTempDir(), ".leafwiki"))
		store := NewRegistryStore(layout.DBPath)
		now := func() time.Time { return time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC) }

		_, err := store.RegisterWorkspaceWithResultAndGrants(WorkspaceRecord{
			ID:          workspaceid.WorkspaceID(" docs "),
			DisplayName: "Docs",
			DataDir:     filepath.Join(wikidTestTempDir(), "docs-data"),
			RootDir:     filepath.Join(wikidTestTempDir(), "docs-root"),
			CreatedAt:   now(),
			UpdatedAt:   now(),
		}, now, nil)
		Expect(err).To(WithTransform(workspaceid.WorkspaceIDErrorCode, Equal(workspaceid.ErrCodeWorkspaceIDWhitespace)))

		doc, loadErr := store.Load()
		Expect(loadErr).To(Succeed())
		Expect(doc.Workspaces).To(BeEmpty())
	})

	ginkgo.It("rolls back registry and grant writes when seeded grants contain whitespace workspace IDs", func() {
		layout := GlobalLayout(filepath.Join(wikidTestTempDir(), ".leafwiki"))
		store := NewRegistryStore(layout.DBPath)
		now := func() time.Time { return time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC) }

		_, err := store.RegisterWorkspaceWithResultAndGrants(WorkspaceRecord{
			ID:          workspaceid.WorkspaceID("docs"),
			DisplayName: "Docs",
			DataDir:     filepath.Join(wikidTestTempDir(), "docs-data"),
			RootDir:     filepath.Join(wikidTestTempDir(), "docs-root"),
			CreatedAt:   now(),
			UpdatedAt:   now(),
		}, now, func(registration RegisterWorkspaceResult) ([]Grant, error) {
			return []Grant{{
				Subject:     "user:agent",
				WorkspaceID: mustDecodeWorkspaceID(" " + registration.Workspace.ID.StorageKey() + " "),
				Role:        GrantRoleViewer,
			}}, nil
		})
		Expect(err).To(WithTransform(workspaceid.WorkspaceIDErrorCode, Equal(workspaceid.ErrCodeWorkspaceIDWhitespace)))

		doc, loadErr := store.Load()
		Expect(loadErr).To(Succeed())
		Expect(doc.Workspaces).To(BeEmpty())
		grants, loadErr := NewGrantStore(layout.DBPath).Load()
		Expect(loadErr).To(Succeed())
		Expect(grants.Grants).To(BeEmpty())
	})
})
