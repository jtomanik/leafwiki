package wikid

import (
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = ginkgo.Describe("wikid helper coverage", func() {
	ginkgo.It("GrantStore.Save replaces persisted grants", func() {
		t := ginkgo.GinkgoT()
		layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
		if _, err := NewRegistryService(NewRegistryStore(layout.DBPath), layout).BootstrapHome(); err != nil {
			t.Fatalf("BootstrapHome failed: %v", err)
		}
		store := NewGrantStore(layout.DBPath)

		Expect(store.Upsert(Grant{Subject: "user:old", WorkspaceID: HomeWorkspaceID, Role: GrantRoleViewer})).To(Succeed())
		Expect(store.Save(GrantDocument{
			SchemaVersion: GrantSchemaVersion,
			Grants: []Grant{
				{Subject: "user:new", WorkspaceID: HomeWorkspaceID, Role: GrantRoleAdmin},
			},
		})).To(Succeed())

		doc, err := store.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(doc.Grants).To(Equal([]Grant{{Subject: "user:new", WorkspaceID: HomeWorkspaceID, Role: GrantRoleAdmin}}))
	})

	ginkgo.It("Supervisor.Roles returns a snapshot of marked roles", func() {
		now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
		supervisor := NewSupervisor(SupervisorOptions{Now: func() time.Time { return now }})

		supervisor.MarkReady(projectdaemon.RoleFrontd, 101, "http://127.0.0.1:8080", true)
		supervisor.MarkReady(projectdaemon.RoleWorkspaced, 202, "http://127.0.0.1:9090", false)

		Expect(supervisor.Roles()).To(ConsistOf(
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Name":      Equal(projectdaemon.RoleFrontd),
				"PID":       Equal(101),
				"UpdatedAt": BeTemporally("==", now),
			}),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Name": Equal(projectdaemon.RoleWorkspaced),
				"URL":  Equal("http://127.0.0.1:9090"),
			}),
		))
	})

	ginkgo.It("WorkspaceSupervisor marks starting/status entries and returns statuses ordered by workspace ID", func() {
		now := time.Date(2026, 6, 25, 11, 0, 0, 0, time.UTC)
		supervisor := NewWorkspaceSupervisor(WorkspaceSupervisorOptions{Now: func() time.Time { return now }})

		supervisor.MarkStatus(WorkspaceStatus{WorkspaceID: workspaceid.WorkspaceID("zulu"), State: WorkspaceStateRunning, PID: 9})
		supervisor.MarkStarting(workspaceid.WorkspaceID("alpha"))
		supervisor.MarkStatus(WorkspaceStatus{})

		Expect(supervisor.Statuses()).To(HaveExactElements(
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"WorkspaceID": Equal(workspaceid.WorkspaceID("alpha")),
				"State":       Equal(WorkspaceStateStarting),
				"UpdatedAt":   BeTemporally("==", now),
			}),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"WorkspaceID": Equal(workspaceid.WorkspaceID("zulu")),
				"UpdatedAt":   BeTemporally("==", now),
			}),
		))
	})
})

var _ = ginkgo.DescribeTable("joinBasePathForPrivateControlPlane",
	func(basePath string, requestPath string, want string) {
		Expect(joinBasePathForPrivateControlPlane(basePath, requestPath)).To(Equal(want))
	},
	ginkgo.Entry("empty base with absolute path", "", "/.well-known/oauth", "/.well-known/oauth"),
	ginkgo.Entry("empty base with relative path", "", "mcp", "/mcp"),
	ginkgo.Entry("trims trailing slash before absolute path", "/workspace/", "/mcp", "/workspace/mcp"),
	ginkgo.Entry("joins trimmed base and relative path", " /workspace ", "mcp", "/workspace/mcp"),
)
