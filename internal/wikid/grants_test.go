package wikid

import (
	"fmt"
	ginkgo "github.com/onsi/ginkgo/v2"
	"path/filepath"
	"strings"
	"sync"

	"github.com/perber/wiki/internal/workspaceid"
)

var _ = ginkgo.It("TestGrantStoreRejectsUnknownRole", func() {
	t := ginkgo.GinkgoT()
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	if _, err := NewRegistryService(NewRegistryStore(layout.DBPath), layout).BootstrapHome(); err != nil {
		t.Fatalf("BootstrapHome failed: %v", err)
	}

	err := NewGrantStore(layout.DBPath).Upsert(Grant{
		Subject:     "user:1",
		WorkspaceID: HomeWorkspaceID,
		Role:        GrantRole("owner"),
	})
	if err == nil || !strings.Contains(err.Error(), `unknown grant role "owner"`) {
		t.Fatalf("Upsert error = %v, want unknown role validation", err)
	}
})

var _ = ginkgo.It("TestGrantStoreRejectsNonURLSafeWorkspaceID", func() {
	t := ginkgo.GinkgoT()
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	err := NewGrantStore(layout.DBPath).Upsert(Grant{
		Subject:     "user:1",
		WorkspaceID: "bad/id",
		Role:        GrantRoleViewer,
	})
	if err == nil || !strings.Contains(err.Error(), "workspace ID") {
		t.Fatalf("Upsert error = %v, want workspace ID validation", err)
	}
})

var _ = ginkgo.It("TestGrantStoreRejectsWorkspaceIDWhitespaceBeforeNormalization", func() {
	t := ginkgo.GinkgoT()
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	err := NewGrantStore(layout.DBPath).Upsert(Grant{
		Subject:     "user:1",
		WorkspaceID: workspaceid.WorkspaceID(" docs "),
		Role:        GrantRoleViewer,
	})
	if code := workspaceid.WorkspaceIDErrorCode(err); code != workspaceid.ErrCodeWorkspaceIDWhitespace {
		t.Fatalf("Upsert error code = %q, want %q (err=%v)", code, workspaceid.ErrCodeWorkspaceIDWhitespace, err)
	}
})

type roleCapabilitiesCase struct {
	role      GrantRole
	wantRead  bool
	wantWrite bool
	wantAdmin bool
}

var _ = ginkgo.DescribeTable("TestRoleCapabilities",
	func(tt roleCapabilitiesCase) {
		t := ginkgo.GinkgoT()
		caps, err := CapabilitiesForRole(tt.role)
		if err != nil {
			t.Fatalf("%s CapabilitiesForRole failed: %v", tt.role, err)
		}
		if caps.ReadContent != tt.wantRead || caps.WriteContent != tt.wantWrite || caps.AdministerGrants != tt.wantAdmin {
			t.Fatalf("%s capabilities = %#v", tt.role, caps)
		}
	},
	ginkgo.Entry("viewer", roleCapabilitiesCase{role: GrantRoleViewer, wantRead: true}),
	ginkgo.Entry("editor", roleCapabilitiesCase{role: GrantRoleEditor, wantRead: true, wantWrite: true}),
	ginkgo.Entry("admin", roleCapabilitiesCase{role: GrantRoleAdmin, wantRead: true, wantWrite: true, wantAdmin: true}),
)

var _ = ginkgo.It("TestGrantStoreUpsertAndListSubjectGrants", func() {
	t := ginkgo.GinkgoT()
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	registry := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
	if _, err := registry.BootstrapHome(); err != nil {
		t.Fatalf("BootstrapHome failed: %v", err)
	}
	alpha, err := registry.RegisterWorkspace(RegisterWorkspaceRequest{
		DisplayName: "Alpha",
		DataDir:     filepath.Join(t.TempDir(), "alpha-data"),
		RootDir:     filepath.Join(t.TempDir(), "alpha-root"),
	})
	if err != nil {
		t.Fatalf("RegisterWorkspace failed: %v", err)
	}
	store := NewGrantStore(layout.DBPath)

	if err := store.Upsert(Grant{Subject: " user:1 ", WorkspaceID: HomeWorkspaceID, Role: GrantRoleViewer}); err != nil {
		t.Fatalf("Upsert home grant failed: %v", err)
	}
	if err := store.Upsert(Grant{Subject: "user:1", WorkspaceID: alpha.ID, Role: GrantRoleEditor}); err != nil {
		t.Fatalf("Upsert alpha grant failed: %v", err)
	}
	if err := store.Upsert(Grant{Subject: "user:1", WorkspaceID: HomeWorkspaceID, Role: GrantRoleAdmin}); err != nil {
		t.Fatalf("Upsert home grant update failed: %v", err)
	}
	if err := store.Upsert(Grant{Subject: "user:2", WorkspaceID: HomeWorkspaceID, Role: GrantRoleViewer}); err != nil {
		t.Fatalf("Upsert other subject failed: %v", err)
	}

	grants, err := store.GrantsForSubject("user:1")
	if err != nil {
		t.Fatalf("GrantsForSubject failed: %v", err)
	}
	if len(grants) != 2 {
		t.Fatalf("grants = %#v, want two for subject", grants)
	}
	if grants[0].WorkspaceID != alpha.ID || grants[0].Role != GrantRoleEditor {
		t.Fatalf("first grant = %#v, want alpha editor", grants[0])
	}
	if grants[1].WorkspaceID != HomeWorkspaceID || grants[1].Role != GrantRoleAdmin {
		t.Fatalf("second grant = %#v, want home admin update", grants[1])
	}
})

var _ = ginkgo.It("TestGrantStoreUpsertPreservesConcurrentStoreInstanceUpdates", func() {
	t := ginkgo.GinkgoT()
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	if _, err := NewRegistryService(NewRegistryStore(layout.DBPath), layout).BootstrapHome(); err != nil {
		t.Fatalf("BootstrapHome failed: %v", err)
	}
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
		if err != nil {
			t.Fatalf("concurrent Upsert failed: %v", err)
		}
	}

	loaded, err := NewGrantStore(layout.DBPath).Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded.Grants) != count {
		t.Fatalf("grants length = %d, want %d: %#v", len(loaded.Grants), count, loaded.Grants)
	}
})

var _ = ginkgo.It("TestGrantStoreConcurrentUpsertsAcrossProcessesUseSQLiteAuthorityStore", func() {
	t := ginkgo.GinkgoT()
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	home, err := NewRegistryService(NewRegistryStore(layout.DBPath), layout).BootstrapHome()
	if err != nil {
		t.Fatalf("BootstrapHome failed: %v", err)
	}
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
				"WIKID_HELPER_ROLE":         string(GrantRoleEditor),
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent helper grant failed: %v", err)
		}
	}
	loaded, err := NewGrantStore(layout.DBPath).Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded.Grants) != count {
		t.Fatalf("grants length = %d, want %d: %#v", len(loaded.Grants), count, loaded.Grants)
	}
	assertSQLiteTableCount(t, layout.DBPath, "workspace_grants", count)
})

var _ = ginkgo.It("TestGrantStoreReplaceSubjectGrantsIsSubjectScoped", func() {
	t := ginkgo.GinkgoT()
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	registry := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
	if _, err := registry.BootstrapHome(); err != nil {
		t.Fatalf("BootstrapHome failed: %v", err)
	}
	alpha, err := registry.RegisterWorkspace(RegisterWorkspaceRequest{
		DisplayName: "Alpha",
		DataDir:     filepath.Join(t.TempDir(), "alpha-data"),
		RootDir:     filepath.Join(t.TempDir(), "alpha-root"),
	})
	if err != nil {
		t.Fatalf("RegisterWorkspace alpha failed: %v", err)
	}
	beta, err := registry.RegisterWorkspace(RegisterWorkspaceRequest{
		DisplayName: "Beta",
		DataDir:     filepath.Join(t.TempDir(), "beta-data"),
		RootDir:     filepath.Join(t.TempDir(), "beta-root"),
	})
	if err != nil {
		t.Fatalf("RegisterWorkspace beta failed: %v", err)
	}
	store := NewGrantStore(layout.DBPath)
	if err := store.Upsert(Grant{Subject: "user:public-editor", WorkspaceID: alpha.ID, Role: GrantRoleEditor}); err != nil {
		t.Fatalf("Upsert public-editor alpha failed: %v", err)
	}
	if err := store.Upsert(Grant{Subject: "user:other", WorkspaceID: beta.ID, Role: GrantRoleViewer}); err != nil {
		t.Fatalf("Upsert other beta failed: %v", err)
	}

	if err := store.ReplaceSubjectGrants("user:public-editor", []Grant{{
		WorkspaceID: HomeWorkspaceID,
		Role:        GrantRoleEditor,
	}}); err != nil {
		t.Fatalf("ReplaceSubjectGrants failed: %v", err)
	}

	publicGrants, err := store.GrantsForSubject("user:public-editor")
	if err != nil {
		t.Fatalf("GrantsForSubject public-editor failed: %v", err)
	}
	if len(publicGrants) != 1 || publicGrants[0].WorkspaceID != HomeWorkspaceID {
		t.Fatalf("public-editor grants = %#v, want only home", publicGrants)
	}
	otherGrants, err := store.GrantsForSubject("user:other")
	if err != nil {
		t.Fatalf("GrantsForSubject other failed: %v", err)
	}
	if len(otherGrants) != 1 || otherGrants[0].WorkspaceID != beta.ID {
		t.Fatalf("other grants = %#v, want preserved beta grant", otherGrants)
	}
})
