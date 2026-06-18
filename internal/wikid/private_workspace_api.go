package wikid

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/perber/wiki/internal/workspaceid"
)

const PrivateWorkspacesPrefix = "/__leafwiki/workspaces"

type WorkspaceListItem struct {
	ID          string          `json:"id"`
	DisplayName string          `json:"displayName"`
	DataDir     string          `json:"-"`
	RootDir     string          `json:"-"`
	Role        GrantRole       `json:"role"`
	Status      WorkspaceStatus `json:"status"`
}

type WorkspaceListResponse struct {
	Workspaces []WorkspaceListItem `json:"workspaces"`
}

type WorkspaceStatusResponse struct {
	Workspace WorkspaceListItem `json:"workspace"`
	Status    WorkspaceStatus   `json:"status"`
}

type WorkspaceSubject struct {
	Subject string
	Role    GrantRole
}

type PrivateWorkspaceAPIOptions struct {
	Registry   *RegistryService
	Grants     *GrantStore
	Supervisor *WorkspaceSupervisor
	Subject    func(*http.Request) (WorkspaceSubject, error)
	Ensure     func(context.Context, WorkspaceRecord) (WorkspaceStatus, error)
}

func NewPrivateWorkspaceAPI(opts PrivateWorkspaceAPIOptions) http.Handler {
	return &privateWorkspaceAPI{opts: opts}
}

type privateWorkspaceAPI struct {
	opts PrivateWorkspaceAPIOptions
}

func (h *privateWorkspaceAPI) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch {
	case req.Method == http.MethodGet && req.URL.Path == PrivateWorkspacesPrefix:
		h.list(w, req)
	case strings.HasPrefix(req.URL.Path, PrivateWorkspacesPrefix+"/"):
		h.workspaceAction(w, req)
	default:
		http.NotFound(w, req)
	}
}

func (h *privateWorkspaceAPI) list(w http.ResponseWriter, req *http.Request) {
	subject, err := h.subject(req)
	if err != nil {
		http.Error(w, "resolve subject", http.StatusUnauthorized)
		return
	}
	workspaces, err := h.opts.Registry.ListWorkspaces()
	if err != nil {
		http.Error(w, "load registry", http.StatusInternalServerError)
		return
	}
	out := WorkspaceListResponse{}
	if subject.Role == GrantRoleAdmin {
		for _, workspace := range workspaces {
			out.Workspaces = append(out.Workspaces, h.item(workspace, GrantRoleAdmin))
		}
		writeJSON(w, out)
		return
	}
	grants, err := h.opts.Grants.GrantsForSubject(subject.Subject)
	if err != nil {
		http.Error(w, "load grants", http.StatusInternalServerError)
		return
	}
	grantByWorkspace := map[string]Grant{}
	for _, grant := range grants {
		grantByWorkspace[grant.WorkspaceID] = grant
	}
	for _, workspace := range workspaces {
		grant, ok := grantByWorkspace[workspace.ID]
		if !ok {
			continue
		}
		out.Workspaces = append(out.Workspaces, h.item(workspace, grant.Role))
	}
	writeJSON(w, out)
}

func (h *privateWorkspaceAPI) workspaceAction(w http.ResponseWriter, req *http.Request) {
	workspaceID, action, ok := parsePrivateWorkspacePath(req.URL.Path)
	if !ok {
		http.NotFound(w, req)
		return
	}
	workspace, grant, granted, found, err := h.authorizedWorkspace(req, workspaceID)
	if err != nil {
		http.Error(w, "authorize workspace", http.StatusInternalServerError)
		return
	}
	if !found {
		http.NotFound(w, req)
		return
	}
	if !granted {
		http.Error(w, "workspace access denied", http.StatusForbidden)
		return
	}
	switch {
	case req.Method == http.MethodGet && action == "status":
		status := h.status(workspace.ID)
		writeJSON(w, WorkspaceStatusResponse{Workspace: h.item(workspace, grant.Role), Status: status})
	case req.Method == http.MethodPost && action == "ensure":
		status, err := h.ensure(req.Context(), workspace)
		if err != nil {
			http.Error(w, "ensure workspace: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, WorkspaceStatusResponse{Workspace: h.item(workspace, grant.Role), Status: status})
	default:
		http.NotFound(w, req)
	}
}

func (h *privateWorkspaceAPI) authorizedWorkspace(req *http.Request, workspaceID string) (WorkspaceRecord, Grant, bool, bool, error) {
	workspace, ok, err := h.opts.Registry.Workspace(workspaceID)
	if err != nil {
		return WorkspaceRecord{}, Grant{}, false, false, err
	}
	if !ok {
		return WorkspaceRecord{}, Grant{}, false, false, nil
	}
	subject, err := h.subject(req)
	if err != nil {
		return WorkspaceRecord{}, Grant{}, false, true, err
	}
	if subject.Role == GrantRoleAdmin {
		return workspace, Grant{Subject: subject.Subject, WorkspaceID: workspace.ID, Role: GrantRoleAdmin}, true, true, nil
	}
	grants, err := h.opts.Grants.GrantsForSubject(subject.Subject)
	if err != nil {
		return WorkspaceRecord{}, Grant{}, false, true, err
	}
	for _, grant := range grants {
		if grant.WorkspaceID == workspace.ID {
			return workspace, grant, true, true, nil
		}
	}
	return workspace, Grant{}, false, true, nil
}

func (h *privateWorkspaceAPI) subject(req *http.Request) (WorkspaceSubject, error) {
	if h.opts.Subject == nil {
		return WorkspaceSubject{}, fmt.Errorf("subject resolver is required")
	}
	subject, err := h.opts.Subject(req)
	if err != nil {
		return WorkspaceSubject{}, err
	}
	subject.Subject = strings.TrimSpace(subject.Subject)
	if subject.Subject == "" {
		return WorkspaceSubject{}, fmt.Errorf("subject is required")
	}
	if subject.Role != "" && !subject.Role.Valid() {
		return WorkspaceSubject{}, fmt.Errorf("unknown subject role %q", subject.Role)
	}
	return subject, nil
}

func (h *privateWorkspaceAPI) item(workspace WorkspaceRecord, role GrantRole) WorkspaceListItem {
	return WorkspaceListItem{
		ID:          workspace.ID,
		DisplayName: workspace.DisplayName,
		DataDir:     workspace.DataDir,
		RootDir:     workspace.RootDir,
		Role:        role,
		Status:      h.status(workspace.ID),
	}
}

func (h *privateWorkspaceAPI) status(workspaceID string) WorkspaceStatus {
	if h.opts.Supervisor == nil {
		return WorkspaceStatus{WorkspaceID: workspaceID, State: WorkspaceStateRegistered}
	}
	return h.opts.Supervisor.Status(workspaceID)
}

func (h *privateWorkspaceAPI) ensure(ctx context.Context, workspace WorkspaceRecord) (WorkspaceStatus, error) {
	if h.opts.Ensure == nil {
		if h.opts.Supervisor != nil {
			return h.opts.Supervisor.Status(workspace.ID), nil
		}
		return WorkspaceStatus{WorkspaceID: workspace.ID, State: WorkspaceStateRegistered}, nil
	}
	return h.opts.Ensure(ctx, workspace)
}

func parsePrivateWorkspacePath(path string) (string, string, bool) {
	rest := strings.TrimPrefix(path, PrivateWorkspacesPrefix+"/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	if err := workspaceid.ValidateWorkspaceID(parts[0]); err != nil {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, "encode response", http.StatusInternalServerError)
	}
}
