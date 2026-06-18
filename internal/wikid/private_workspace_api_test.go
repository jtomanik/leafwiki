package wikid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateWorkspaceAPIListsGrantedWorkspaces(t *testing.T) {
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	registry := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
	home, err := registry.BootstrapHome()
	if err != nil {
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
	grants := NewGrantStore(layout.DBPath)
	if err := grants.Upsert(Grant{Subject: "user:1", WorkspaceID: home.ID, Role: GrantRoleViewer}); err != nil {
		t.Fatalf("grant home: %v", err)
	}
	if err := grants.Upsert(Grant{Subject: "user:2", WorkspaceID: alpha.ID, Role: GrantRoleViewer}); err != nil {
		t.Fatalf("grant alpha to other user: %v", err)
	}
	api := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
		Registry: registry,
		Grants:   grants,
		Subject: func(*http.Request) (WorkspaceSubject, error) {
			return WorkspaceSubject{Subject: "user:1"}, nil
		},
		Supervisor: NewWorkspaceSupervisor(WorkspaceSupervisorOptions{}),
	})

	req := httptest.NewRequest(http.MethodGet, "/__leafwiki/workspaces", nil)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, leaked := range []string{"dataDir", "rootDir", home.DataDir, home.RootDir} {
		if strings.Contains(body, leaked) {
			t.Fatalf("response leaked workspace path field %q: %s", leaked, body)
		}
	}
	var out WorkspaceListResponse
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out.Workspaces) != 1 || out.Workspaces[0].ID != home.ID || out.Workspaces[0].Role != GrantRoleViewer {
		t.Fatalf("workspaces = %#v, want only granted home", out.Workspaces)
	}
}

func TestPrivateWorkspaceAPIAdminListsAndEnsuresRegisteredWorkspacesWithoutStoredGrants(t *testing.T) {
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	registry := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
	home, err := registry.BootstrapHome()
	if err != nil {
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
	grants := NewGrantStore(layout.DBPath)
	supervisor := NewWorkspaceSupervisor(WorkspaceSupervisorOptions{})
	var ensured string
	api := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
		Registry:   registry,
		Grants:     grants,
		Supervisor: supervisor,
		Subject: func(*http.Request) (WorkspaceSubject, error) {
			return WorkspaceSubject{Subject: "user:admin", Role: GrantRoleAdmin}, nil
		},
		Ensure: func(_ context.Context, workspace WorkspaceRecord) (WorkspaceStatus, error) {
			ensured = workspace.ID
			supervisor.MarkReady(workspace.ID, 321, "http://127.0.0.1:42001")
			return supervisor.Status(workspace.ID), nil
		},
	})

	listReq := httptest.NewRequest(http.MethodGet, "/__leafwiki/workspaces", nil)
	listRec := httptest.NewRecorder()
	api.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", listRec.Code, listRec.Body.String())
	}
	var list WorkspaceListResponse
	if err := json.NewDecoder(listRec.Body).Decode(&list); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(list.Workspaces) != 2 {
		t.Fatalf("workspaces = %#v, want home and alpha", list.Workspaces)
	}
	for _, workspace := range list.Workspaces {
		if workspace.ID != home.ID && workspace.ID != alpha.ID {
			t.Fatalf("unexpected workspace in admin list: %#v", workspace)
		}
		if workspace.Role != GrantRoleAdmin {
			t.Fatalf("workspace %q role = %q, want admin", workspace.ID, workspace.Role)
		}
	}

	ensureReq := httptest.NewRequest(http.MethodPost, "/__leafwiki/workspaces/"+alpha.ID+"/ensure", nil)
	ensureRec := httptest.NewRecorder()
	api.ServeHTTP(ensureRec, ensureReq)

	if ensureRec.Code != http.StatusOK {
		t.Fatalf("ensure status = %d: %s", ensureRec.Code, ensureRec.Body.String())
	}
	if ensured != alpha.ID {
		t.Fatalf("ensured workspace = %q, want %q", ensured, alpha.ID)
	}
	stored, err := grants.GrantsForSubject("user:admin")
	if err != nil {
		t.Fatalf("GrantsForSubject failed: %v", err)
	}
	if len(stored) != 0 {
		t.Fatalf("stored admin grants = %#v, want none", stored)
	}
}

func TestPrivateWorkspaceAPIEnsureStartsGrantedWorkspace(t *testing.T) {
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	registry := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
	alpha, err := registry.RegisterWorkspace(RegisterWorkspaceRequest{
		DisplayName: "Alpha",
		DataDir:     filepath.Join(t.TempDir(), "alpha-data"),
		RootDir:     filepath.Join(t.TempDir(), "alpha-root"),
	})
	if err != nil {
		t.Fatalf("RegisterWorkspace failed: %v", err)
	}
	grants := NewGrantStore(layout.DBPath)
	if err := grants.Upsert(Grant{Subject: "user:1", WorkspaceID: alpha.ID, Role: GrantRoleEditor}); err != nil {
		t.Fatalf("grant alpha: %v", err)
	}
	supervisor := NewWorkspaceSupervisor(WorkspaceSupervisorOptions{})
	var ensured string
	api := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
		Registry:   registry,
		Grants:     grants,
		Supervisor: supervisor,
		Subject: func(*http.Request) (WorkspaceSubject, error) {
			return WorkspaceSubject{Subject: "user:1"}, nil
		},
		Ensure: func(_ context.Context, workspace WorkspaceRecord) (WorkspaceStatus, error) {
			ensured = workspace.ID
			supervisor.MarkReady(workspace.ID, 123, "http://127.0.0.1:41001")
			return supervisor.Status(workspace.ID), nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/__leafwiki/workspaces/"+alpha.ID+"/ensure", nil)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, leaked := range []string{"dataDir", "rootDir", alpha.DataDir, alpha.RootDir} {
		if strings.Contains(body, leaked) {
			t.Fatalf("response leaked workspace path field %q: %s", leaked, body)
		}
	}
	if ensured != alpha.ID {
		t.Fatalf("ensured workspace = %q, want %q", ensured, alpha.ID)
	}
	var out WorkspaceStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Status.State != WorkspaceStateRunning || out.Status.URL == "" {
		t.Fatalf("status response = %#v", out.Status)
	}
}

func TestPrivateWorkspaceAPIRejectsUngrantedWorkspace(t *testing.T) {
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	registry := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
	alpha, err := registry.RegisterWorkspace(RegisterWorkspaceRequest{
		DisplayName: "Alpha",
		DataDir:     filepath.Join(t.TempDir(), "alpha-data"),
		RootDir:     filepath.Join(t.TempDir(), "alpha-root"),
	})
	if err != nil {
		t.Fatalf("RegisterWorkspace failed: %v", err)
	}
	api := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
		Registry:   registry,
		Grants:     NewGrantStore(layout.DBPath),
		Supervisor: NewWorkspaceSupervisor(WorkspaceSupervisorOptions{}),
		Subject: func(*http.Request) (WorkspaceSubject, error) {
			return WorkspaceSubject{Subject: "user:1"}, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/__leafwiki/workspaces/"+alpha.ID+"/status", nil)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}

func TestPrivateWorkspaceAPIReturnsNotFoundForUnknownWorkspace(t *testing.T) {
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	registry := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
	if _, err := registry.BootstrapHome(); err != nil {
		t.Fatalf("BootstrapHome failed: %v", err)
	}
	api := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
		Registry:   registry,
		Grants:     NewGrantStore(layout.DBPath),
		Supervisor: NewWorkspaceSupervisor(WorkspaceSupervisorOptions{}),
		Subject: func(*http.Request) (WorkspaceSubject, error) {
			return WorkspaceSubject{Subject: "user:1"}, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/__leafwiki/workspaces/missing/status", nil)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestPrivateWorkspaceAPIRejectsInvalidWorkspaceIDBeforeLookup(t *testing.T) {
	layout := GlobalLayout(filepath.Join(t.TempDir(), ".leafwiki"))
	registry := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
	home, err := registry.BootstrapHome()
	if err != nil {
		t.Fatalf("BootstrapHome failed: %v", err)
	}
	grants := NewGrantStore(layout.DBPath)
	if err := grants.Upsert(Grant{Subject: "user:1", WorkspaceID: home.ID, Role: GrantRoleEditor}); err != nil {
		t.Fatalf("grant home: %v", err)
	}
	api := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
		Registry:   registry,
		Grants:     grants,
		Supervisor: NewWorkspaceSupervisor(WorkspaceSupervisorOptions{}),
		Subject: func(*http.Request) (WorkspaceSubject, error) {
			return WorkspaceSubject{Subject: "user:1"}, nil
		},
		Ensure: func(context.Context, WorkspaceRecord) (WorkspaceStatus, error) {
			t.Fatalf("invalid workspace ID unexpectedly reached ensure")
			return WorkspaceStatus{}, nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/__leafwiki/workspaces/%20home/ensure", nil)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for invalid workspace ID: %s", rec.Code, rec.Body.String())
	}
}
