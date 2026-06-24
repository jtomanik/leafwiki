package wikid

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/workspaceid"
)

const PrivateWorkspacesPrefix = "/__leafwiki/workspaces"

const (
	ErrCodeWorkspaceGrantDenied         sharederrors.ErrorCode = "workspace_grant_denied"
	errCodePrivateUnauthorized          sharederrors.ErrorCode = "private_unauthorized"
	errCodePrivateSubjectResolveFailed  sharederrors.ErrorCode = "private_subject_resolve_failed"
	errCodePrivateRegistryLoadFailed    sharederrors.ErrorCode = "private_registry_load_failed"
	errCodePrivateGrantsLoadFailed      sharederrors.ErrorCode = "private_grants_load_failed"
	errCodePrivateWorkspaceAuthFailed   sharederrors.ErrorCode = "private_workspace_auth_failed"
	errCodePrivateWorkspaceEnsureFailed sharederrors.ErrorCode = "private_workspace_ensure_failed"
	errCodePrivateEncodeResponseFailed  sharederrors.ErrorCode = "private_encode_response_failed"
)

type WorkspaceListItem struct {
	ID                     workspaceid.WorkspaceID `json:"id"`
	DisplayName            string                  `json:"displayName"`
	MarkdownLinkRootPrefix string                  `json:"markdownLinkRootPrefix,omitempty"`
	DataDir                string                  `json:"-"`
	RootDir                string                  `json:"-"`
	Role                   GrantRole               `json:"role"`
	Status                 WorkspaceStatus         `json:"status"`
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
		writePrivateWorkspaceError(w, http.StatusUnauthorized, errCodePrivateSubjectResolveFailed)
		return
	}
	workspaces, err := h.opts.Registry.ListWorkspaces()
	if err != nil {
		writePrivateWorkspaceError(w, http.StatusInternalServerError, errCodePrivateRegistryLoadFailed)
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
		writePrivateWorkspaceError(w, http.StatusInternalServerError, errCodePrivateGrantsLoadFailed)
		return
	}
	grantByWorkspace := map[workspaceid.WorkspaceID]Grant{}
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
		writePrivateWorkspaceError(w, http.StatusInternalServerError, errCodePrivateWorkspaceAuthFailed)
		return
	}
	if !found {
		http.NotFound(w, req)
		return
	}
	if !granted {
		writePrivateWorkspaceError(w, http.StatusForbidden, ErrCodeWorkspaceGrantDenied)
		return
	}
	switch {
	case req.Method == http.MethodGet && action == "status":
		status := h.status(workspace.ID)
		writeJSON(w, WorkspaceStatusResponse{Workspace: h.item(workspace, grant.Role), Status: status})
	case req.Method == http.MethodPost && action == "ensure":
		status, err := h.ensure(req.Context(), workspace)
		if err != nil {
			writePrivateWorkspaceError(w, http.StatusInternalServerError, errCodePrivateWorkspaceEnsureFailed)
			return
		}
		writeJSON(w, WorkspaceStatusResponse{Workspace: h.item(workspace, grant.Role), Status: status})
	default:
		http.NotFound(w, req)
	}
}

func (h *privateWorkspaceAPI) authorizedWorkspace(req *http.Request, workspaceID workspaceid.WorkspaceID) (WorkspaceRecord, Grant, bool, bool, error) {
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
		ID:                     workspace.ID,
		DisplayName:            workspace.DisplayName,
		MarkdownLinkRootPrefix: workspace.MarkdownLinkRootPrefix,
		DataDir:                workspace.DataDir,
		RootDir:                workspace.RootDir,
		Role:                   role,
		Status:                 h.status(workspace.ID),
	}
}

func (h *privateWorkspaceAPI) status(workspaceID workspaceid.WorkspaceID) WorkspaceStatus {
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

func parsePrivateWorkspacePath(path string) (workspaceid.WorkspaceID, string, bool) {
	rest := strings.TrimPrefix(path, PrivateWorkspacesPrefix+"/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	workspaceID, err := workspaceid.ValidateWorkspaceID(parts[0])
	if err != nil {
		return "", "", false
	}
	return workspaceID, parts[1], true
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		writePrivateWorkspaceError(w, http.StatusInternalServerError, errCodePrivateEncodeResponseFailed)
	}
}

func writePrivateWorkspaceError(w http.ResponseWriter, status int, code sharederrors.ErrorCode) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	detail := sharederrors.NewLocalizedErrorDetailFromCode(code)
	_ = json.NewEncoder(w).Encode(privateWorkspaceErrorResponse{
		Error: privateWorkspaceError{
			Code:      code,
			MessageID: detail.MessageID,
			Message:   detail.Message,
		},
	})
}

type privateWorkspaceErrorResponse struct {
	Error privateWorkspaceError `json:"error"`
}

type privateWorkspaceError struct {
	Code      sharederrors.ErrorCode `json:"code"`
	MessageID sharederrors.MessageID `json:"messageId"`
	Message   string                 `json:"message"`
}
