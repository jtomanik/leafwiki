package wikid

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/workspaceid"
	_ "modernc.org/sqlite"
)

type registryServiceFixture struct {
	layout  Layout
	service *RegistryService
}

func newRegistryServiceFixture() registryServiceFixture {
	ginkgo.GinkgoHelper()
	layout := GlobalLayout(filepath.Join(wikidTestTempDir(), ".leafwiki"))
	return registryServiceFixture{
		layout:  layout,
		service: NewRegistryService(NewRegistryStore(layout.DBPath), layout),
	}
}

func (fixture registryServiceFixture) registerWorkspace(displayName string, dataDir string, rootDir string, markdownLinkRootPrefix string) WorkspaceRecord {
	ginkgo.GinkgoHelper()
	workspace, err := fixture.service.RegisterWorkspace(RegisterWorkspaceRequest{
		DisplayName:            displayName,
		DataDir:                dataDir,
		RootDir:                rootDir,
		MarkdownLinkRootPrefix: markdownLinkRootPrefix,
	})
	Expect(err).To(Succeed())
	return workspace
}

func expectRegistryTableCount(dbPath string, table string, want int) {
	ginkgo.GinkgoHelper()
	db, err := sql.Open("sqlite", dbPath)
	Expect(err).To(Succeed())
	defer func() {
		Expect(db.Close()).To(Succeed())
	}()
	var got int
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	Expect(db.QueryRow(query).Scan(&got)).To(Succeed())
	Expect(got).To(Equal(want))
}

var _ = ginkgo.Describe("wikid registry", func() {
	ginkgo.It("bootstraps the home workspace into the secured registry database", func() {
		fixture := newRegistryServiceFixture()

		home, err := fixture.service.BootstrapHome()

		Expect(err).To(Succeed())
		Expect(home).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ID":      Equal(HomeWorkspaceID),
			"DataDir": Equal(fixture.layout.HomeDir),
			"RootDir": Equal(fixture.layout.HomeRootDir),
		}))
		loaded, err := NewRegistryStore(fixture.layout.DBPath).Load()
		Expect(err).To(Succeed())
		Expect(loaded).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"SchemaVersion": Equal(RegistrySchemaVersion),
			"Workspaces": ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ID":      Equal(HomeWorkspaceID),
				"RootDir": Equal(fixture.layout.HomeRootDir),
			})),
		}))
		info, err := os.Stat(fixture.layout.DBPath)
		Expect(err).To(Succeed())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
	})

	ginkgo.It("keeps non-home workspace IDs stable across renamed registrations", func() {
		fixture := newRegistryServiceFixture()
		rootDir := filepath.Join(wikidTestTempDir(), "Docs Root")
		dataDir := filepath.Join(wikidTestTempDir(), "Docs Data")

		first := fixture.registerWorkspace("Docs", dataDir, rootDir, "")
		renamed := fixture.registerWorkspace("Product Docs", dataDir, rootDir, "")
		second := fixture.registerWorkspace("Docs", filepath.Join(wikidTestTempDir(), "Other Data"), filepath.Join(wikidTestTempDir(), "Other Root"), "")

		Expect(first.ID.Validate()).To(Succeed())
		Expect(first.ID).To(WithTransform(func(id workspaceid.WorkspaceID) string {
			return id.StorageKey()
		}, HavePrefix("docs-")))
		Expect(renamed).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ID":          Equal(first.ID),
			"DisplayName": Equal("Product Docs"),
		}))
		Expect(second.ID).NotTo(Equal(first.ID))
		loaded, err := NewRegistryStore(fixture.layout.DBPath).Load()
		Expect(err).To(Succeed())
		Expect(loaded.Workspaces).To(SatisfyAll(
			ContainElement(HaveField("ID", Equal(first.ID))),
			ContainElement(HaveField("ID", Equal(second.ID))),
		))
	})

	ginkgo.It("persists normalized markdown link root prefixes across workspace updates", func() {
		fixture := newRegistryServiceFixture()
		rootDir := filepath.Join(wikidTestTempDir(), "Docs Root")
		dataDir := filepath.Join(wikidTestTempDir(), "Docs Data")

		first := fixture.registerWorkspace("Docs", dataDir, rootDir, "docs/")
		renamed := fixture.registerWorkspace("Docs Renamed", dataDir, rootDir, "/handbook/")

		Expect(first.MarkdownLinkRootPrefix).To(Equal("/docs"))
		Expect(renamed).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ID":                     Equal(first.ID),
			"MarkdownLinkRootPrefix": Equal("/handbook"),
		}))
		loaded, err := NewRegistryStore(fixture.layout.DBPath).Load()
		Expect(err).To(Succeed())
		Expect(loaded.Workspaces).To(ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ID":                     Equal(first.ID),
			"MarkdownLinkRootPrefix": Equal("/handbook"),
		})))
	})

	ginkgo.It("requires exact workspace ID matches when looking up registry records", func() {
		doc := NewRegistryDocument()
		doc.Workspaces = append(doc.Workspaces, WorkspaceRecord{
			ID:          HomeWorkspaceID,
			DisplayName: "Home",
			DataDir:     filepath.Join(wikidTestTempDir(), "home-data"),
			RootDir:     filepath.Join(wikidTestTempDir(), "home-root"),
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		})

		Expect(doc.Workspaces).To(SatisfyAll(
			Not(ContainElement(HaveField("ID", Equal(mustDecodeWorkspaceID(" "+HomeWorkspaceID.StorageKey()))))),
			Not(ContainElement(HaveField("ID", Equal(mustDecodeWorkspaceID(HomeWorkspaceID.StorageKey()+" "))))),
			ContainElement(HaveField("ID", Equal(HomeWorkspaceID))),
		))
	})

	ginkgo.It("rejects workspace registrations that reuse data or root directories", func() {
		fixture := newRegistryServiceFixture()
		rootDir := filepath.Join(wikidTestTempDir(), "Docs Root")
		dataDir := filepath.Join(wikidTestTempDir(), "Docs Data")
		fixture.registerWorkspace("Docs", dataDir, rootDir, "")

		_, sameRootErr := fixture.service.RegisterWorkspace(RegisterWorkspaceRequest{
			DisplayName: "Other Data",
			DataDir:     filepath.Join(wikidTestTempDir(), "Other Data"),
			RootDir:     rootDir,
		})
		_, sameDataErr := fixture.service.RegisterWorkspace(RegisterWorkspaceRequest{
			DisplayName: "Other Root",
			DataDir:     dataDir,
			RootDir:     filepath.Join(wikidTestTempDir(), "Other Root"),
		})

		Expect(sameRootErr).To(MatchError(ErrWorkspaceRootDirAlreadyInUse))
		Expect(sameDataErr).To(MatchError(ErrWorkspaceDataDirAlreadyInUse))
	})

	ginkgo.It("rejects registry documents with invalid workspace ID values", func() {
		path := filepath.Join(wikidTestTempDir(), "wikid.db")
		now := time.Now().UTC()
		doc := NewRegistryDocument()
		doc.Workspaces = append(doc.Workspaces, WorkspaceRecord{
			ID:          "bad/id",
			DisplayName: "Bad",
			DataDir:     filepath.Join(wikidTestTempDir(), "bad-data"),
			RootDir:     filepath.Join(wikidTestTempDir(), "bad-root"),
			CreatedAt:   now,
			UpdatedAt:   now,
		})

		err := NewRegistryStore(path).Save(doc)

		Expect(err).To(WithTransform(workspaceid.WorkspaceIDErrorCode, Equal(workspaceid.ErrCodeWorkspaceIDInvalid)))
	})

	ginkgo.It("serializes registry updates across store instances", func() {
		path := filepath.Join(wikidTestTempDir(), "wikid.db")
		firstStore := NewRegistryStore(path)
		secondStore := NewRegistryStore(path)
		firstEntered := make(chan struct{})
		releaseFirst := make(chan struct{})
		firstDone := make(chan error, 1)

		go func() {
			defer ginkgo.GinkgoRecover()
			_, err := firstStore.Update(func(doc RegistryDocument) (RegistryDocument, error) {
				close(firstEntered)
				Eventually(releaseFirst).Should(BeClosed())
				doc.Workspaces = append(doc.Workspaces, testWorkspaceRecord("first"))
				return doc, nil
			})
			firstDone <- err
		}()

		Eventually(firstEntered).Should(BeClosed())
		secondDone := make(chan error, 1)
		go func() {
			defer ginkgo.GinkgoRecover()
			_, err := secondStore.Update(func(doc RegistryDocument) (RegistryDocument, error) {
				doc.Workspaces = append(doc.Workspaces, testWorkspaceRecord("second"))
				return doc, nil
			})
			secondDone <- err
		}()

		Consistently(secondDone).WithTimeout(50 * time.Millisecond).ShouldNot(Receive())
		close(releaseFirst)
		Eventually(firstDone).Should(Receive(Succeed()))
		Eventually(secondDone).Should(Receive(Succeed()))

		loaded, err := NewRegistryStore(path).Load()
		Expect(err).To(Succeed())
		Expect(loaded.Workspaces).To(SatisfyAll(
			ContainElement(HaveField("ID", Equal(mustDecodeWorkspaceID("first")))),
			ContainElement(HaveField("ID", Equal(mustDecodeWorkspaceID("second")))),
		))
	})

	ginkgo.It("rolls back workspace registration and seeded grants together", func() {
		fixture := newRegistryServiceFixture()
		store := NewRegistryStore(fixture.layout.DBPath)
		service := NewRegistryService(store, fixture.layout)

		_, err := service.RegisterWorkspaceWithResultAndGrants(RegisterWorkspaceRequest{
			DisplayName: "Docs",
			DataDir:     filepath.Join(wikidTestTempDir(), "docs-data"),
			RootDir:     filepath.Join(wikidTestTempDir(), "docs-root"),
		}, func(registration RegisterWorkspaceResult) ([]Grant, error) {
			return []Grant{{
				Subject:     "user:agent",
				WorkspaceID: registration.Workspace.ID,
				Role:        GrantRole("owner"),
			}}, nil
		})

		Expect(err).To(MatchError(ErrUnknownGrantRole))
		doc, loadErr := store.Load()
		Expect(loadErr).To(Succeed())
		Expect(doc.Workspaces).To(BeEmpty())
		grants, loadErr := NewGrantStore(fixture.layout.DBPath).Load()
		Expect(loadErr).To(Succeed())
		Expect(grants.Grants).To(BeEmpty())
	})

	ginkgo.It("uses the SQLite authority store for concurrent registrations across processes", func() {
		fixture := newRegistryServiceFixture()
		dataDir := filepath.Join(wikidTestTempDir(), "shared-data")
		rootDir := filepath.Join(wikidTestTempDir(), "shared-root")
		const count = 8

		var wg sync.WaitGroup
		errs := make(chan error, count)
		for i := 0; i < count; i++ {
			i := i
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs <- runWikidStoreHelper(map[string]string{
					"WIKID_HELPER_OP":           "register",
					"WIKID_HELPER_HOME":         fixture.layout.HomeDir,
					"WIKID_HELPER_DISPLAY_NAME": fmt.Sprintf("Docs %02d", i),
					"WIKID_HELPER_DATA_DIR":     dataDir,
					"WIKID_HELPER_ROOT_DIR":     rootDir,
				})
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			Expect(err).To(Succeed())
		}

		service := NewRegistryService(NewRegistryStore(fixture.layout.DBPath), fixture.layout)
		workspaces, err := service.ListWorkspaces()
		Expect(err).To(Succeed())
		Expect(workspaces).To(HaveLen(1))
		expectRegistryTableCount(fixture.layout.DBPath, "workspaces", 1)
	})

	ginkgo.It("lists workspaces with home first and named workspaces in slug order", func() {
		fixture := newRegistryServiceFixture()
		fixture.registerWorkspace("Zulu", filepath.Join(wikidTestTempDir(), "z-data"), filepath.Join(wikidTestTempDir(), "z-root"), "")
		_, err := fixture.service.BootstrapHome()
		Expect(err).To(Succeed())
		fixture.registerWorkspace("Alpha", filepath.Join(wikidTestTempDir(), "a-data"), filepath.Join(wikidTestTempDir(), "a-root"), "")

		workspaces, err := fixture.service.ListWorkspaces()

		Expect(err).To(Succeed())
		Expect(workspaces).To(HaveExactElements(
			HaveField("ID", Equal(HomeWorkspaceID)),
			HaveField("ID", WithTransform(func(id workspaceid.WorkspaceID) string {
				return id.StorageKey()
			}, HavePrefix("alpha-"))),
			HaveField("ID", WithTransform(func(id workspaceid.WorkspaceID) string {
				return id.StorageKey()
			}, HavePrefix("zulu-"))),
		))
	})
})

func testWorkspaceRecord(id string) WorkspaceRecord {
	now := time.Now().UTC()
	workspaceID, err := workspaceid.ParseWorkspaceID(id)
	if err != nil {
		panic(err)
	}
	return WorkspaceRecord{
		ID:          workspaceID,
		DisplayName: id,
		DataDir:     filepath.Join(os.TempDir(), id+"-data"),
		RootDir:     filepath.Join(os.TempDir(), id+"-root"),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func runWikidStoreHelper(env map[string]string) error {
	args := []string{"-test.run=TestWikidSuite", "--"}
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), "GO_WANT_WIKID_STORE_HELPER=1")
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

func runWikidStoreHelperProcessForTest() bool {
	if os.Getenv("GO_WANT_WIKID_STORE_HELPER") != "1" {
		return false
	}
	layout := GlobalLayout(os.Getenv("WIKID_HELPER_HOME"))
	switch os.Getenv("WIKID_HELPER_OP") {
	case "register":
		service := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
		if _, err := service.RegisterWorkspace(RegisterWorkspaceRequest{
			DisplayName: os.Getenv("WIKID_HELPER_DISPLAY_NAME"),
			DataDir:     os.Getenv("WIKID_HELPER_DATA_DIR"),
			RootDir:     os.Getenv("WIKID_HELPER_ROOT_DIR"),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "RegisterWorkspace failed: %v\n", err)
			os.Exit(1)
		}
	case "grant":
		store := NewGrantStore(layout.DBPath)
		workspaceID, err := workspaceid.ParseWorkspaceID(os.Getenv("WIKID_HELPER_WORKSPACE_ID"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "ParseWorkspaceID failed: %v\n", err)
			os.Exit(1)
		}
		role, err := ParseGrantRole(os.Getenv("WIKID_HELPER_ROLE"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "ParseGrantRole failed: %v\n", err)
			os.Exit(1)
		}
		if err := store.Upsert(Grant{
			Subject:     os.Getenv("WIKID_HELPER_SUBJECT"),
			WorkspaceID: workspaceID,
			Role:        role,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "GrantStore.Upsert failed: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown helper op %q\n", os.Getenv("WIKID_HELPER_OP"))
		os.Exit(1)
	}
	os.Exit(0)
	return true
}
