package wikid

import (
	"fmt"
	"path/filepath"
	"sync"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/workspaceid"
)

type roleCapabilitiesCase struct {
	role      GrantRole
	wantRead  bool
	wantWrite bool
	wantAdmin bool
}

func newBootstrappedGrantStore() (Layout, WorkspaceRecord, *GrantStore) {
	ginkgo.GinkgoHelper()

	layout := GlobalLayout(filepath.Join(wikidTestTempDir(), ".leafwiki"))
	home, err := NewRegistryService(NewRegistryStore(layout.DBPath), layout).BootstrapHome()
	Expect(err).To(Succeed())
	return layout, home, NewGrantStore(layout.DBPath)
}

func registeredWorkspace(registry *RegistryService, displayName string) WorkspaceRecord {
	ginkgo.GinkgoHelper()

	workspace, err := registry.RegisterWorkspace(RegisterWorkspaceRequest{
		DisplayName: displayName,
		DataDir:     filepath.Join(wikidTestTempDir(), displayName+"-data"),
		RootDir:     filepath.Join(wikidTestTempDir(), displayName+"-root"),
	})
	Expect(err).To(Succeed())
	return workspace
}

var _ = ginkgo.Describe("grant store validation", func() {
	ginkgo.It("rejects grants whose role is unknown", func() {
		_, _, store := newBootstrappedGrantStore()

		err := store.Upsert(Grant{
			Subject:     "user:1",
			WorkspaceID: HomeWorkspaceID,
			Role:        GrantRole("owner"),
		})
		Expect(err).To(MatchError(ErrUnknownGrantRole))
	})

	ginkgo.It("rejects grants whose workspace ID is not URL safe", func() {
		_, _, store := newBootstrappedGrantStore()

		err := store.Upsert(Grant{
			Subject:     "user:1",
			WorkspaceID: "bad/id",
			Role:        GrantRoleViewer,
		})
		Expect(err).To(WithTransform(workspaceid.WorkspaceIDErrorCode, Equal(workspaceid.ErrCodeWorkspaceIDInvalid)))
	})

	ginkgo.It("rejects grants whose workspace ID contains whitespace before normalization", func() {
		_, _, store := newBootstrappedGrantStore()

		err := store.Upsert(Grant{
			Subject:     "user:1",
			WorkspaceID: workspaceid.WorkspaceID(" docs "),
			Role:        GrantRoleViewer,
		})
		Expect(err).To(WithTransform(workspaceid.WorkspaceIDErrorCode, Equal(workspaceid.ErrCodeWorkspaceIDWhitespace)))
	})
})

var _ = ginkgo.DescribeTable("grant role capabilities",
	func(tt roleCapabilitiesCase) {
		caps, err := CapabilitiesForRole(tt.role)
		Expect(err).To(Succeed())
		Expect(caps).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ReadContent":      Equal(tt.wantRead),
			"WriteContent":     Equal(tt.wantWrite),
			"AdministerGrants": Equal(tt.wantAdmin),
		}))
	},
	ginkgo.Entry("allows viewers to read content", roleCapabilitiesCase{role: GrantRoleViewer, wantRead: true}),
	ginkgo.Entry("allows editors to read and write content", roleCapabilitiesCase{role: GrantRoleEditor, wantRead: true, wantWrite: true}),
	ginkgo.Entry("allows administrators to read, write, and manage grants", roleCapabilitiesCase{role: GrantRoleAdmin, wantRead: true, wantWrite: true, wantAdmin: true}),
)

var _ = ginkgo.Describe("grant persistence", func() {
	ginkgo.It("upserts grants and lists only the requested subject in stable workspace order", func() {
		layout, _, _ := newBootstrappedGrantStore()
		registry := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
		alpha := registeredWorkspace(registry, "Alpha")
		store := NewGrantStore(layout.DBPath)

		Expect(store.Upsert(Grant{Subject: " user:1 ", WorkspaceID: HomeWorkspaceID, Role: GrantRoleViewer})).To(Succeed())
		Expect(store.Upsert(Grant{Subject: "user:1", WorkspaceID: alpha.ID, Role: GrantRoleEditor})).To(Succeed())
		Expect(store.Upsert(Grant{Subject: "user:1", WorkspaceID: HomeWorkspaceID, Role: GrantRoleAdmin})).To(Succeed())
		Expect(store.Upsert(Grant{Subject: "user:2", WorkspaceID: HomeWorkspaceID, Role: GrantRoleViewer})).To(Succeed())

		grants, err := store.GrantsForSubject("user:1")
		Expect(err).To(Succeed())
		Expect(grants).To(HaveExactElements(
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"WorkspaceID": Equal(alpha.ID),
				"Role":        Equal(GrantRoleEditor),
			}),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"WorkspaceID": Equal(HomeWorkspaceID),
				"Role":        Equal(GrantRoleAdmin),
			}),
		))
	})

	ginkgo.It("preserves concurrent updates from independent store instances", func() {
		layout, _, _ := newBootstrappedGrantStore()
		const count = 32
		var wg sync.WaitGroup
		errs := make(chan error, count)

		for i := 0; i < count; i++ {
			i := i
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs <- NewGrantStore(layout.DBPath).Upsert(Grant{
					Subject:     fmt.Sprintf("user:%02d", i),
					WorkspaceID: HomeWorkspaceID,
					Role:        GrantRoleEditor,
				})
			}()
		}

		wg.Wait()
		close(errs)
		for err := range errs {
			Expect(err).To(Succeed())
		}

		loaded, err := NewGrantStore(layout.DBPath).Load()
		Expect(err).To(Succeed())
		Expect(loaded.Grants).To(HaveLen(count))
	})

	ginkgo.It("uses SQLite as the authority store for concurrent helper-process grants", func() {
		layout, home, _ := newBootstrappedGrantStore()
		const count = 16
		var wg sync.WaitGroup
		errs := make(chan error, count)
		for i := 0; i < count; i++ {
			i := i
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs <- runWikidStoreHelper(map[string]string{
					"WIKID_HELPER_OP":           "grant",
					"WIKID_HELPER_HOME":         layout.HomeDir,
					"WIKID_HELPER_SUBJECT":      fmt.Sprintf("user:%02d", i),
					"WIKID_HELPER_WORKSPACE_ID": home.ID.StorageKey(),
					"WIKID_HELPER_ROLE":         grantRoleEditorValue,
				})
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			Expect(err).To(Succeed())
		}

		loaded, err := NewGrantStore(layout.DBPath).Load()
		Expect(err).To(Succeed())
		Expect(loaded.Grants).To(HaveLen(count))
	})

	ginkgo.It("replaces only grants for the requested subject", func() {
		layout, _, _ := newBootstrappedGrantStore()
		registry := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
		alpha := registeredWorkspace(registry, "Alpha")
		beta := registeredWorkspace(registry, "Beta")
		store := NewGrantStore(layout.DBPath)
		Expect(store.Upsert(Grant{Subject: "user:public-editor", WorkspaceID: alpha.ID, Role: GrantRoleEditor})).To(Succeed())
		Expect(store.Upsert(Grant{Subject: "user:other", WorkspaceID: beta.ID, Role: GrantRoleViewer})).To(Succeed())

		Expect(store.ReplaceSubjectGrants("user:public-editor", []Grant{{
			WorkspaceID: HomeWorkspaceID,
			Role:        GrantRoleEditor,
		}})).To(Succeed())

		publicGrants, err := store.GrantsForSubject("user:public-editor")
		Expect(err).To(Succeed())
		Expect(publicGrants).To(HaveExactElements(HaveField("WorkspaceID", Equal(HomeWorkspaceID))))
		otherGrants, err := store.GrantsForSubject("user:other")
		Expect(err).To(Succeed())
		Expect(otherGrants).To(HaveExactElements(HaveField("WorkspaceID", Equal(beta.ID))))
	})
})
