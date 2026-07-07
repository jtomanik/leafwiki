package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/frontd"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
)

func writeRuntimeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, "encode response", http.StatusInternalServerError)
	}
}

func writeRuntimeError(w http.ResponseWriter, status int, code sharederrors.ErrorCode) {
	detail := sharederrors.NewLocalizedErrorDetail(code, "", "")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(runtimeErrorResponse{
		Error: runtimeError{
			Code:      detail.Code,
			MessageID: detail.MessageID,
			Message:   detail.Message,
		},
	}); err != nil {
		http.Error(w, "encode response", http.StatusInternalServerError)
	}
}

func workspaceMCPUnavailableHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeRuntimeError(w, http.StatusServiceUnavailable, runtimeErrorCodeMCPWorkspaceUnavailable)
	})
}

func writePrivateMCPUnauthorized(w http.ResponseWriter) {
	writeRuntimeError(w, http.StatusUnauthorized, runtimeErrorCodePrivateMCPControlTokenInvalid)
}

type runtimeErrorResponse struct {
	Error runtimeError `json:"error"`
}

type runtimeError struct {
	Code      sharederrors.ErrorCode `json:"code"`
	MessageID sharederrors.MessageID `json:"messageId"`
	Message   string                 `json:"message"`
}

func runWikidFrontdOwner(parent context.Context, cfg leafwikiRuntimeConfig, ownerCfg projectdaemon.Config) error {
	controlListener, err := netListenForRuntime("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start control listener: %w", err)
	}
	defer closeBestEffort(controlListener)

	controlToken, err := randomTokenForRuntime()
	if err != nil {
		return err
	}
	hash, err := configHashForRuntime(ownerCfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	controlPlaneWiki, err := newRuntimeWikiForRuntime(cfg, ownerCfg, runtimeWikiControlPlaneOnly)
	if err != nil {
		return err
	}
	defer closeBestEffort(controlPlaneWiki)
	controlPlaneOpts, err := controlPlaneRouterOptionsForOwner(cfg, controlPlaneWiki)
	if err != nil {
		return err
	}
	controlPlaneRouter := frontd.NewRouter(controlPlaneWiki, controlPlaneOpts)
	wikidURL := "http://" + controlListener.Addr().String()
	runtime, err := startWikidFrontdRuntimeForOwner(ctx, cfg, controlToken, wikidURL)
	if err != nil {
		return err
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := runtime.stop(stopCtx); err != nil {
			slog.Default().Warn("Runtime role shutdown failed", "error", err)
		}
	}()
	controlPlaneWiki.SetRuntimeRoleHealth(requiredRuntimeRoleHealth(), runtime.roleHealthSnapshot)
	runtime.mu.Lock()
	workspacedURL := runtime.workspacedURL
	workspacedPID := 0
	if role, ok := findRuntimeRoleHealth(runtime.roles, projectdaemon.RoleWorkspaced); ok {
		workspacedPID = role.PID
	}
	runtime.mu.Unlock()
	privateMCP, err := newMCPProxyWithActorForRuntime(frontd.WorkspaceProxyOptions{
		Upstream:    workspacedURL,
		DaemonToken: controlToken,
		Actor:       wikidControlMCPActorResolver(authStorageDirForRuntime(ownerCfg.DataDir), cfg),
	})
	if err != nil {
		return err
	}

	var sessions *projectdaemon.SessionRegistry
	var agentPresence *projectdaemon.AgentPresenceRegistry
	activityChanged := func(int) {}
	if !cfg.DisableIdleShutdown {
		activityChanged = idleShutdownCallback(ctx, cancel, cfg.DaemonIdleTimeout, func() int {
			return projectDaemonActivityCount(sessions, agentPresence)
		})
	}
	notifyActivityChanged := func(int) {
		activityChanged(projectDaemonActivityCount(sessions, agentPresence))
	}
	sessions = projectdaemon.NewSessionRegistry(projectdaemon.DefaultHeartbeatTTL, notifyActivityChanged)
	agentPresence = projectdaemon.NewAgentPresenceRegistry(cfg.DaemonIdleTimeout, notifyActivityChanged)
	go sessions.RunExpiryLoop(ctx, 0)
	go agentPresence.RunExpiryLoop(ctx, 0)

	layout := wikid.GlobalLayout(ownerCfg.DataDir)
	registry := wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout)
	if _, err := registry.BootstrapHomeWorkspace(ownerCfg.DataDir, ownerCfg.RootDir); err != nil {
		return fmt.Errorf("bootstrap home workspace: %w", err)
	}
	grants := wikid.NewGrantStore(layout.DBPath)
	if err := seedRuntimeHomeGrantsForOwner(grants, cfg); err != nil {
		return err
	}
	workspaceSupervisor := wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{})
	workspaceSupervisor.MarkReady(wikid.HomeWorkspaceID, workspacedPID, workspacedURL)
	workspaceManager := newFederatedWorkspaceManagerForOwner(cfg, controlToken, wikidURL, layout, workspaceSupervisor)
	workspaceManager.MarkReady(wikid.HomeWorkspaceID, workspacedPID, workspacedURL)
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := workspaceManager.stop(stopCtx); err != nil {
			slog.Default().Warn("Workspace runtime shutdown failed", "error", err)
		}
	}()
	workspaceAPI := wikid.NewPrivateWorkspaceAPI(wikid.PrivateWorkspaceAPIOptions{
		Registry:   registry,
		Grants:     grants,
		Supervisor: workspaceSupervisor,
		Subject: func(req *http.Request) (wikid.WorkspaceSubject, error) {
			return runtimeWorkspaceSubject(req, controlPlaneWiki, cfg, grants)
		},
		Ensure: func(ctx context.Context, workspace wikid.WorkspaceRecord) (wikid.WorkspaceStatus, error) {
			return workspaceManager.Ensure(ctx, workspace)
		},
	})

	controlHandler := projectdaemon.NewControlServer(projectdaemon.ControlServerOptions{
		Token:         controlToken,
		Sessions:      sessions,
		AgentPresence: agentPresence,
		PrivateMCP:    privateMCP,
		AuthDisabled:  cfg.DisableAuth,
		Health: projectdaemon.DaemonHealth{
			SchemaVersion: projectdaemon.DescriptorSchemaVersion,
			PID:           os.Getpid(),
			DataDir:       ownerCfg.DataDir,
			RootDir:       ownerCfg.RootDir,
			ConfigHash:    hash,
		},
		VerifyAPIKey: func(key string) error {
			return verifyOwnerControlAPIKey(ownerCfg, key)
		},
	})
	controlServer := &http.Server{Addr: controlListener.Addr().String(), Handler: wikid.NewPrivateHandler(wikid.PrivateHandlerOptions{
		DaemonToken:  controlToken,
		BasePath:     cfg.BasePath,
		Control:      controlHandler,
		ControlPlane: controlPlaneRouter,
		ActorContext: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			handleWikidActorContext(w, req, controlPlaneWiki, cfg, registry, grants)
		}),
		TokenVerify:  runtimeTokenVerifyHandler(controlPlaneWiki),
		WorkspaceAPI: workspaceAPI,
	})}
	serverDone := serveWikidControlServerForOwner(controlServer, controlListener)

	desc := &projectdaemon.Descriptor{
		SchemaVersion:    projectdaemon.DescriptorSchemaVersion,
		RuntimeStack:     cfg.RuntimeStack,
		Role:             projectDaemonDescriptorRole(cfg.RuntimeStack),
		WorkspaceID:      runtimeWorkspaceSemanticID(cfg.Workspace),
		PID:              os.Getpid(),
		StartedAt:        time.Now().UTC(),
		DataDir:          ownerCfg.DataDir,
		RootDir:          ownerCfg.RootDir,
		PublicURL:        roleURL(runtime.roles, projectdaemon.RoleFrontd),
		PublicMCPEnabled: cfg.MCPTransports.HTTP,
		BasePath:         cfg.BasePath,
		ControlURL:       "http://" + controlListener.Addr().String(),
		PrivateMCPURL:    strings.TrimRight(workspacedURL, "/") + "/mcp",
		PrivateMCPToken:  controlToken,
		ConfigHash:       hash,
		IdleTimeout:      cfg.DaemonIdleTimeout.String(),
		ControlToken:     controlToken,
		Config:           ownerCfg,
		Roles:            append([]projectdaemon.RoleHealth(nil), runtime.roles...),
	}
	if desc.PublicURL == "" {
		desc.PublicURL = nativeStdioHTTPURL(cfg.Host, cfg.Port, cfg.BasePath)
	}
	descriptorPath := projectdaemon.DescriptorPath(ownerCfg.DataDir)
	if err := writeDescriptorAtomicForRuntime(descriptorPath, desc); err != nil {
		return err
	}
	globalDescriptorPath := projectdaemon.GlobalDescriptorPath(layout.RuntimeDir, projectdaemon.RoleWikid)
	if err := writeDescriptorAtomicForRuntime(globalDescriptorPath, desc); err != nil {
		return err
	}
	var descriptorMu sync.Mutex
	runtime.setRoleChangeCallback(func(roles []projectdaemon.RoleHealth) {
		updateRuntimeRoleDescriptors(&descriptorMu, desc, workspaceSupervisor, descriptorPath, globalDescriptorPath, roles)
	})
	defer removeProjectDaemonDescriptorBestEffort(descriptorPath)
	defer removeProjectDaemonDescriptorBestEffort(globalDescriptorPath)
	if !cfg.DisableIdleShutdown {
		go cancelIfNoActivityAfterStartupGrace(ctx, cancel, sessions, agentPresence, projectdaemon.DefaultHeartbeatTTL)
	}

	slog.Default().Info("Starting LeafWiki", "address", buildListenAddress(cfg.Host, cfg.Port), "data_dir", ownerCfg.DataDir)
	select {
	case <-ctx.Done():
	case err := <-serverDone:
		if err != nil {
			return err
		}
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	return shutdownWikidControlServerForRuntime(controlServer, shutdownCtx)
}

func runtimeWorkspaceSubject(req *http.Request, controlPlaneWiki *wiki.Wiki, cfg leafwikiRuntimeConfig, grants *wikid.GrantStore) (wikid.WorkspaceSubject, error) {
	user, _, err := frontdActorUser(cloneWithOriginalRequest(req), controlPlaneWiki, cfg)
	if err != nil {
		return wikid.WorkspaceSubject{}, err
	}
	if err := ensureRuntimeHomeGrantForOwner(grants, user); err != nil {
		return wikid.WorkspaceSubject{}, err
	}
	return wikid.WorkspaceSubject{
		Subject: leafwikiUserSubject(user.ID),
		Role:    wikidGrantRoleForCoreRole(user.Role),
	}, nil
}

func runtimeTokenVerifyHandler(controlPlaneWiki *wiki.Wiki) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		handleWikidTokenVerify(w, req, controlPlaneWiki)
	})
}

func verifyOwnerControlAPIKey(ownerCfg projectdaemon.Config, key string) error {
	err := verifyStdioAPIKeyFromStorage(authStorageDirForRuntime(ownerCfg.DataDir), key)
	if errors.Is(err, coreauth.ErrInvalidToken) {
		return projectdaemon.ErrInvalidAPIKey
	}
	return err
}

func serveWikidControlServer(server *http.Server, listener net.Listener) <-chan error {
	serverDone := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
			err = nil
		}
		serverDone <- err
	}()
	return serverDone
}

func updateRuntimeRoleDescriptors(descriptorMu *sync.Mutex, desc *projectdaemon.Descriptor, workspaceSupervisor *wikid.WorkspaceSupervisor, descriptorPath string, globalDescriptorPath string, roles []projectdaemon.RoleHealth) {
	descriptorMu.Lock()
	defer descriptorMu.Unlock()
	desc.Roles = append([]projectdaemon.RoleHealth(nil), roles...)
	syncHomeWorkspaceStatus(workspaceSupervisor, roles)
	if publicURL := roleURL(roles, projectdaemon.RoleFrontd); publicURL != "" {
		desc.PublicURL = publicURL
	}
	if err := writeDescriptorAtomicForRuntime(descriptorPath, desc); err != nil {
		slog.Default().Warn("Runtime role descriptor update failed", "error", err)
	}
	if err := writeDescriptorAtomicForRuntime(globalDescriptorPath, desc); err != nil {
		slog.Default().Warn("Runtime role global descriptor update failed", "error", err)
	}
}

func roleURL(roles []projectdaemon.RoleHealth, name projectdaemon.RoleName) string {
	for _, role := range roles {
		if role.Name == name {
			return role.URL
		}
	}
	return ""
}

func findRuntimeRoleHealth(roles []projectdaemon.RoleHealth, name projectdaemon.RoleName) (projectdaemon.RoleHealth, bool) {
	for _, role := range roles {
		if role.Name == name {
			return role, true
		}
	}
	return projectdaemon.RoleHealth{}, false
}

func syncHomeWorkspaceStatus(supervisor *wikid.WorkspaceSupervisor, roles []projectdaemon.RoleHealth) {
	if supervisor == nil {
		return
	}
	role, ok := findRuntimeRoleHealth(roles, projectdaemon.RoleWorkspaced)
	if !ok {
		return
	}
	status := wikid.WorkspaceStatus{
		WorkspaceID: wikid.HomeWorkspaceID,
		PID:         role.PID,
		URL:         strings.TrimSpace(role.URL),
		Error:       role.Error,
		UpdatedAt:   role.UpdatedAt,
	}
	switch role.State {
	case projectdaemon.RoleStateReady:
		status.State = wikid.WorkspaceStateRunning
	case projectdaemon.RoleStateStarting:
		status.State = wikid.WorkspaceStateStarting
	case projectdaemon.RoleStateRestarting:
		status.State = wikid.WorkspaceStateRestarting
	case projectdaemon.RoleStateCrashed, projectdaemon.RoleStateStopped:
		status.State = wikid.WorkspaceStateCrashed
	default:
		status.State = wikid.WorkspaceStateRegistered
	}
	supervisor.MarkStatus(status)
}

func seedRuntimeHomeGrants(store *wikid.GrantStore, cfg leafwikiRuntimeConfig) error {
	if cfg.DisableAuth {
		if err := store.Upsert(wikid.Grant{Subject: "user:public-editor", WorkspaceID: wikid.HomeWorkspaceID, Role: wikid.GrantRoleEditor}); err != nil {
			return fmt.Errorf("seed disabled-auth home grant: %w", err)
		}
	}
	if cfg.PublicAccess {
		if err := store.Upsert(wikid.Grant{Subject: "user:public-viewer", WorkspaceID: wikid.HomeWorkspaceID, Role: wikid.GrantRoleViewer}); err != nil {
			return fmt.Errorf("seed public home grant: %w", err)
		}
	}
	return nil
}

func ensureRuntimeHomeGrant(store *wikid.GrantStore, user *coreauth.User) error {
	if user == nil {
		return errRuntimeHomeGrantUserRequired
	}
	role := wikidGrantRoleForCoreRole(user.Role)
	if role == "" {
		return nil
	}
	return store.Upsert(wikid.Grant{Subject: leafwikiUserSubject(user.ID), WorkspaceID: wikid.HomeWorkspaceID, Role: role})
}

func wikidGrantRoleForCoreRole(role string) wikid.GrantRole {
	switch role {
	case coreauth.RoleViewer:
		return wikid.GrantRoleViewer
	case coreauth.RoleEditor:
		return wikid.GrantRoleEditor
	case coreauth.RoleAdmin:
		return wikid.GrantRoleAdmin
	default:
		return ""
	}
}

func effectiveWorkspaceGrantRole(userRole wikid.GrantRole, grantRole wikid.GrantRole) wikid.GrantRole {
	if wikidGrantRoleRank(userRole) < wikidGrantRoleRank(grantRole) {
		return userRole
	}
	return grantRole
}

func wikidGrantRoleRank(role wikid.GrantRole) int {
	switch role {
	case wikid.GrantRoleViewer:
		return 1
	case wikid.GrantRoleEditor:
		return 2
	case wikid.GrantRoleAdmin:
		return 3
	default:
		return 0
	}
}
