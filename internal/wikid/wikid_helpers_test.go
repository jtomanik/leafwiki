package wikid

import (
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/projectdaemon"
)

var _ = ginkgo.Describe("wikid support stores and supervisors", func() {
	ginkgo.It("replaces persisted grants when saving a complete grant document", ginkgo.Label("integration"), func() {
		layout := GlobalLayout(filepath.Join(wikidTestTempDir(), ".leafwiki"))
		_, err := NewRegistryService(NewRegistryStore(layout.DBPath), layout).BootstrapHome()
		Expect(err).To(Succeed())
		store := NewGrantStore(layout.DBPath)

		Expect(store.Upsert(Grant{Subject: "user:old", WorkspaceID: HomeWorkspaceID, Role: GrantRoleViewer})).To(Succeed())
		Expect(store.Save(GrantDocument{
			SchemaVersion: GrantSchemaVersion,
			Grants: []Grant{
				{Subject: "user:new", WorkspaceID: HomeWorkspaceID, Role: GrantRoleAdmin},
			},
		})).To(Succeed())

		doc, err := store.Load()
		Expect(err).To(Succeed())
		Expect(doc.Grants).To(Equal([]Grant{{Subject: "user:new", WorkspaceID: HomeWorkspaceID, Role: GrantRoleAdmin}}))
	})

	ginkgo.It("returns marked daemon roles as an immutable runtime snapshot", ginkgo.Label("unit"), func() {
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

	ginkgo.It("returns workspace process states ordered by workspace identity", ginkgo.Label("unit"), func() {
		now := time.Date(2026, 6, 25, 11, 0, 0, 0, time.UTC)
		supervisor := NewWorkspaceSupervisor(WorkspaceSupervisorOptions{Now: func() time.Time { return now }})

		supervisor.MarkStatus(WorkspaceStatus{WorkspaceID: mustDecodeWorkspaceID("zulu"), State: WorkspaceStateRunning, PID: 9})
		supervisor.MarkStarting(mustDecodeWorkspaceID("alpha"))
		supervisor.MarkStatus(WorkspaceStatus{})

		Expect(supervisor.Statuses()).To(HaveExactElements(
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"WorkspaceID": Equal(mustDecodeWorkspaceID("alpha")),
				"State":       Equal(WorkspaceStateStarting),
				"UpdatedAt":   BeTemporally("==", now),
			}),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"WorkspaceID": Equal(mustDecodeWorkspaceID("zulu")),
				"UpdatedAt":   BeTemporally("==", now),
			}),
		))
	})
})

var _ = ginkgo.DescribeTable("private control-plane base path joining",
	ginkgo.Label("unit"),
	func(basePath string, requestPath string, want string) {
		Expect(joinBasePathForPrivateControlPlane(basePath, requestPath)).To(Equal(want))
	},
	ginkgo.Entry("empty base with absolute path", "", "/.well-known/oauth", "/.well-known/oauth"),
	ginkgo.Entry("empty base with relative path", "", "mcp", "/mcp"),
	ginkgo.Entry("trims trailing slash before absolute path", "/workspace/", "/mcp", "/workspace/mcp"),
	ginkgo.Entry("joins trimmed base and relative path", " /workspace ", "mcp", "/workspace/mcp"),
)
