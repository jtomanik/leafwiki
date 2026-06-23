package wikid

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/perber/wiki/internal/workspaceid"
	_ "modernc.org/sqlite"
)

func TestRegistryServiceBootstrapsHomeWorkspace(t *testing.T) {
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	service := NewRegistryService(NewRegistryStore(layout.DBPath), layout)

	home, err := service.BootstrapHome()
	if err != nil {
		t.Fatalf("BootstrapHome failed: %v", err)
	}
	if home.ID != "home" {
		t.Fatalf("home ID = %q, want home", home.ID)
	}
	if home.DataDir != layout.HomeDir {
		t.Fatalf("home data dir = %q, want %q", home.DataDir, layout.HomeDir)
	}
	if home.RootDir != layout.HomeRootDir {
		t.Fatalf("home root dir = %q, want %q", home.RootDir, layout.HomeRootDir)
	}

	loaded, err := NewRegistryStore(layout.DBPath).Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", loaded.SchemaVersion)
	}
	if got, ok := loaded.Workspace("home"); !ok || got.RootDir != layout.HomeRootDir {
		t.Fatalf("loaded home = %#v, ok=%v", got, ok)
	}
	info, err := os.Stat(layout.DBPath)
	if err != nil {
		t.Fatalf("stat registry: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("registry mode = %04o, want 0600", got)
	}
}

func TestRegistryServiceRegistersStableNonHomeWorkspaceIDs(t *testing.T) {
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	service := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
	rootDir := filepath.Join(t.TempDir(), "Docs Root")
	dataDir := filepath.Join(t.TempDir(), "Docs Data")

	first, err := service.RegisterWorkspace(RegisterWorkspaceRequest{
		DisplayName: "Docs",
		DataDir:     dataDir,
		RootDir:     rootDir,
	})
	if err != nil {
		t.Fatalf("RegisterWorkspace failed: %v", err)
	}
	if first.ID == "" || strings.ContainsAny(first.ID.String(), " /_") {
		t.Fatalf("workspace ID = %q, want URL-safe slug", first.ID)
	}
	if !strings.HasPrefix(first.ID.String(), "docs-") {
		t.Fatalf("workspace ID = %q, want display-name slug prefix", first.ID)
	}

	renamed, err := service.RegisterWorkspace(RegisterWorkspaceRequest{
		DisplayName: "Product Docs",
		DataDir:     dataDir,
		RootDir:     rootDir,
	})
	if err != nil {
		t.Fatalf("RegisterWorkspace renamed failed: %v", err)
	}
	if renamed.ID != first.ID {
		t.Fatalf("renamed ID = %q, want stable %q", renamed.ID, first.ID)
	}
	if renamed.DisplayName != "Product Docs" {
		t.Fatalf("renamed display name = %q", renamed.DisplayName)
	}

	second, err := service.RegisterWorkspace(RegisterWorkspaceRequest{
		DisplayName: "Docs",
		DataDir:     filepath.Join(t.TempDir(), "Other Data"),
		RootDir:     filepath.Join(t.TempDir(), "Other Root"),
	})
	if err != nil {
		t.Fatalf("RegisterWorkspace duplicate display failed: %v", err)
	}
	if second.ID == first.ID {
		t.Fatalf("duplicate display name reused ID %q", second.ID)
	}

	loaded, err := NewRegistryStore(layout.DBPath).Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if _, ok := loaded.Workspace(first.ID); !ok {
		t.Fatalf("registry did not persist first workspace %#v", loaded.Workspaces)
	}
	if _, ok := loaded.Workspace(second.ID); !ok {
		t.Fatalf("registry did not persist second workspace %#v", loaded.Workspaces)
	}
}

func TestRegistryServicePersistsNormalizedMarkdownLinkRootPrefix(t *testing.T) {
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	service := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
	rootDir := filepath.Join(t.TempDir(), "Docs Root")
	dataDir := filepath.Join(t.TempDir(), "Docs Data")

	first, err := service.RegisterWorkspace(RegisterWorkspaceRequest{
		DisplayName:            "Docs",
		DataDir:                dataDir,
		RootDir:                rootDir,
		MarkdownLinkRootPrefix: "docs/",
	})
	if err != nil {
		t.Fatalf("RegisterWorkspace failed: %v", err)
	}
	if first.MarkdownLinkRootPrefix != "/docs" {
		t.Fatalf("markdown link root prefix = %q, want /docs", first.MarkdownLinkRootPrefix)
	}

	renamed, err := service.RegisterWorkspace(RegisterWorkspaceRequest{
		DisplayName:            "Docs Renamed",
		DataDir:                dataDir,
		RootDir:                rootDir,
		MarkdownLinkRootPrefix: "/handbook/",
	})
	if err != nil {
		t.Fatalf("RegisterWorkspace renamed failed: %v", err)
	}
	if renamed.ID != first.ID {
		t.Fatalf("renamed ID = %q, want stable %q", renamed.ID, first.ID)
	}
	if renamed.MarkdownLinkRootPrefix != "/handbook" {
		t.Fatalf("updated markdown link root prefix = %q, want /handbook", renamed.MarkdownLinkRootPrefix)
	}

	loaded, err := NewRegistryStore(layout.DBPath).Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	persisted, ok := loaded.Workspace(first.ID)
	if !ok {
		t.Fatalf("workspace %q not persisted", first.ID)
	}
	if persisted.MarkdownLinkRootPrefix != "/handbook" {
		t.Fatalf("persisted markdown link root prefix = %q, want /handbook", persisted.MarkdownLinkRootPrefix)
	}
}

func TestRegistryDocumentWorkspaceDoesNotTrimLookupID(t *testing.T) {
	doc := NewRegistryDocument()
	doc.Workspaces = append(doc.Workspaces, WorkspaceRecord{
		ID:          HomeWorkspaceID,
		DisplayName: "Home",
		DataDir:     filepath.Join(t.TempDir(), "home-data"),
		RootDir:     filepath.Join(t.TempDir(), "home-root"),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	})

	leadingWhitespaceID := decodeWorkspaceIDForTest(t, " "+HomeWorkspaceID.StorageKey())
	if _, ok := doc.Workspace(leadingWhitespaceID); ok {
		t.Fatalf("workspace lookup with leading whitespace resolved %q", HomeWorkspaceID)
	}
	trailingWhitespaceID := decodeWorkspaceIDForTest(t, HomeWorkspaceID.StorageKey()+" ")
	if _, ok := doc.Workspace(trailingWhitespaceID); ok {
		t.Fatalf("workspace lookup with trailing whitespace resolved %q", HomeWorkspaceID)
	}
	if _, ok := doc.Workspace(HomeWorkspaceID); !ok {
		t.Fatalf("workspace lookup did not resolve exact ID %q", HomeWorkspaceID)
	}
}

func TestRegistryServiceRejectsConflictingWorkspaceLocations(t *testing.T) {
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	service := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
	rootDir := filepath.Join(t.TempDir(), "Docs Root")
	dataDir := filepath.Join(t.TempDir(), "Docs Data")

	if _, err := service.RegisterWorkspace(RegisterWorkspaceRequest{
		DisplayName: "Docs",
		DataDir:     dataDir,
		RootDir:     rootDir,
	}); err != nil {
		t.Fatalf("RegisterWorkspace failed: %v", err)
	}
	if _, err := service.RegisterWorkspace(RegisterWorkspaceRequest{
		DisplayName: "Other Data",
		DataDir:     filepath.Join(t.TempDir(), "Other Data"),
		RootDir:     rootDir,
	}); err == nil || !strings.Contains(err.Error(), "root directory is already in use") {
		t.Fatalf("same root error = %v, want root directory conflict", err)
	}
	if _, err := service.RegisterWorkspace(RegisterWorkspaceRequest{
		DisplayName: "Other Root",
		DataDir:     dataDir,
		RootDir:     filepath.Join(t.TempDir(), "Other Root"),
	}); err == nil || !strings.Contains(err.Error(), "data directory is already in use") {
		t.Fatalf("same data error = %v, want data directory conflict", err)
	}
}

func TestRegistryStoreRejectsNonURLSafeWorkspaceIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wikid.db")
	now := time.Now().UTC()
	doc := NewRegistryDocument()
	doc.Workspaces = append(doc.Workspaces, WorkspaceRecord{
		ID:          "bad/id",
		DisplayName: "Bad",
		DataDir:     filepath.Join(t.TempDir(), "bad-data"),
		RootDir:     filepath.Join(t.TempDir(), "bad-root"),
		CreatedAt:   now,
		UpdatedAt:   now,
	})

	err := NewRegistryStore(path).Save(doc)
	if err == nil || !strings.Contains(err.Error(), "workspace ID") {
		t.Fatalf("Save error = %v, want workspace ID validation", err)
	}
}

func TestRegistryStoreUpdateSerializesAcrossStoreInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wikid.db")
	firstStore := NewRegistryStore(path)
	secondStore := NewRegistryStore(path)
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)

	go func() {
		_, err := firstStore.Update(func(doc RegistryDocument) (RegistryDocument, error) {
			close(firstEntered)
			<-releaseFirst
			doc.Workspaces = append(doc.Workspaces, testWorkspaceRecord("first"))
			return doc, nil
		})
		firstDone <- err
	}()

	<-firstEntered
	secondDone := make(chan error, 1)
	go func() {
		_, err := secondStore.Update(func(doc RegistryDocument) (RegistryDocument, error) {
			doc.Workspaces = append(doc.Workspaces, testWorkspaceRecord("second"))
			return doc, nil
		})
		secondDone <- err
	}()

	secondCompleted := false
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second update failed: %v", err)
		}
		secondCompleted = true
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first update failed: %v", err)
	}
	if !secondCompleted {
		if err := <-secondDone; err != nil {
			t.Fatalf("second update failed: %v", err)
		}
	}

	loaded, err := NewRegistryStore(path).Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if _, ok := loaded.Workspace("first"); !ok {
		t.Fatalf("registry missing first workspace after concurrent updates: %#v", loaded.Workspaces)
	}
	if _, ok := loaded.Workspace("second"); !ok {
		t.Fatalf("registry missing second workspace after concurrent updates: %#v", loaded.Workspaces)
	}
}

func TestRegistryStoreRegisterWorkspaceAndGrantSeedingRollsBackTogether(t *testing.T) {
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	store := NewRegistryStore(layout.DBPath)
	service := NewRegistryService(store, layout)

	_, err := service.RegisterWorkspaceWithResultAndGrants(RegisterWorkspaceRequest{
		DisplayName: "Docs",
		DataDir:     filepath.Join(t.TempDir(), "docs-data"),
		RootDir:     filepath.Join(t.TempDir(), "docs-root"),
	}, func(registration RegisterWorkspaceResult) ([]Grant, error) {
		return []Grant{{
			Subject:     "user:agent",
			WorkspaceID: registration.Workspace.ID,
			Role:        GrantRole("owner"),
		}}, nil
	})
	if err == nil || !strings.Contains(err.Error(), `unknown grant role "owner"`) {
		t.Fatalf("RegisterWorkspaceWithResultAndGrants error = %v, want grant validation", err)
	}

	doc, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("Load failed: %v", loadErr)
	}
	if len(doc.Workspaces) != 0 {
		t.Fatalf("workspaces after rolled-back registration = %#v, want none", doc.Workspaces)
	}
	grants, loadErr := NewGrantStore(layout.DBPath).Load()
	if loadErr != nil {
		t.Fatalf("Load grants failed: %v", loadErr)
	}
	if len(grants.Grants) != 0 {
		t.Fatalf("grants after rolled-back registration = %#v, want none", grants.Grants)
	}
}

func TestRegistryServiceConcurrentRegistrationAcrossProcessesUsesSQLiteAuthorityStore(t *testing.T) {
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	dataDir := filepath.Join(t.TempDir(), "shared-data")
	rootDir := filepath.Join(t.TempDir(), "shared-root")
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
				"WIKID_HELPER_HOME":         layout.HomeDir,
				"WIKID_HELPER_DISPLAY_NAME": fmt.Sprintf("Docs %02d", i),
				"WIKID_HELPER_DATA_DIR":     dataDir,
				"WIKID_HELPER_ROOT_DIR":     rootDir,
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent helper registration failed: %v", err)
		}
	}

	service := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
	workspaces, err := service.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces failed: %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("workspaces = %#v, want one registered workspace", workspaces)
	}
	assertSQLiteTableCount(t, layout.DBPath, "workspaces", 1)
}

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

func decodeWorkspaceIDForTest(t *testing.T, raw string) workspaceid.WorkspaceID {
	t.Helper()
	payload, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("Marshal workspace ID fixture: %v", err)
	}
	var id workspaceid.WorkspaceID
	if err := json.Unmarshal(payload, &id); err != nil {
		t.Fatalf("Unmarshal workspace ID fixture: %v", err)
	}
	return id
}

func TestRegistryServiceListsWorkspacesInStableOrder(t *testing.T) {
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	service := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
	if _, err := service.RegisterWorkspace(RegisterWorkspaceRequest{
		DisplayName: "Zulu",
		DataDir:     filepath.Join(t.TempDir(), "z-data"),
		RootDir:     filepath.Join(t.TempDir(), "z-root"),
	}); err != nil {
		t.Fatalf("register zulu: %v", err)
	}
	if _, err := service.BootstrapHome(); err != nil {
		t.Fatalf("bootstrap home: %v", err)
	}
	if _, err := service.RegisterWorkspace(RegisterWorkspaceRequest{
		DisplayName: "Alpha",
		DataDir:     filepath.Join(t.TempDir(), "a-data"),
		RootDir:     filepath.Join(t.TempDir(), "a-root"),
	}); err != nil {
		t.Fatalf("register alpha: %v", err)
	}

	workspaces, err := service.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces failed: %v", err)
	}
	got := []string{}
	for _, workspace := range workspaces {
		got = append(got, workspace.ID.String())
	}
	if len(got) != 3 || got[0] != "home" || !strings.HasPrefix(got[1], "alpha-") || !strings.HasPrefix(got[2], "zulu-") {
		t.Fatalf("workspace order = %#v", got)
	}
}

func runWikidStoreHelper(env map[string]string) error {
	args := []string{"-test.run=TestWikidStoreHelperProcess", "--"}
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

func TestWikidStoreHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_WIKID_STORE_HELPER") != "1" {
		return
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
			t.Fatalf("RegisterWorkspace failed: %v", err)
		}
	case "grant":
		store := NewGrantStore(layout.DBPath)
		workspaceID, err := workspaceid.ParseWorkspaceID(os.Getenv("WIKID_HELPER_WORKSPACE_ID"))
		if err != nil {
			t.Fatalf("ParseWorkspaceID failed: %v", err)
		}
		if err := store.Upsert(Grant{
			Subject:     os.Getenv("WIKID_HELPER_SUBJECT"),
			WorkspaceID: workspaceID,
			Role:        GrantRole(os.Getenv("WIKID_HELPER_ROLE")),
		}); err != nil {
			t.Fatalf("GrantStore.Upsert failed: %v", err)
		}
	default:
		t.Fatalf("unknown helper op %q", os.Getenv("WIKID_HELPER_OP"))
	}
	os.Exit(0)
}

func assertSQLiteTableCount(t *testing.T, dbPath string, table string, want int) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite %s: %v", dbPath, err)
	}
	defer db.Close()
	var got int
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	if err := db.QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("query %s count: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}
