package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/perber/wiki/internal/frontd"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
)

func writeInternalRuntimeRoleReady(path string, ready internalRuntimeRoleReady) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	raw, err := jsonMarshalForRuntime(ready)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func internalRuntimeRoleArgs(startupPath string) []string {
	args := []string{"--internal-runtime-role", startupPath}
	if os.Getenv("GO_WANT_LEAFWIKI_HELPER_PROCESS") == "1" {
		args = []string{"-test.run=TestLeafWikiSuite", "--", "--internal-runtime-role", startupPath}
	}
	return args
}

func configureInternalRuntimeRoleIO(cmd *exec.Cmd, cfg leafwikiRuntimeConfig) (func(), error) {
	if !cfg.MCPTransports.Stdio && !cfg.DetachDaemonOwnerIO {
		cmd.Stdin = nil
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return func() {}, nil
	}
	nullDevice, closeNullDevice, err := openDaemonNullDevice()
	if err != nil {
		return nil, err
	}
	cmd.Stdin = nullDevice
	cmd.Stdout = nullDevice
	cmd.Stderr = nullDevice
	return closeNullDevice, nil
}

func (s *wikidFrontdRuntime) stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.stopping = true
	if s.cancel != nil {
		s.cancel()
	}
	processes := make([]*internalRuntimeRoleProcess, 0, len(s.processes))
	for _, proc := range s.processes {
		processes = append(processes, proc)
	}
	s.mu.Unlock()
	var joined error
	for _, proc := range processes {
		if err := proc.stop(ctx); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

func (p *internalRuntimeRoleProcess) wait() error {
	if p == nil {
		return nil
	}
	p.waitOnce.Do(func() {
		p.waitErr = <-p.done
		close(p.waitDone)
	})
	<-p.waitDone
	return p.waitErr
}

func (p *internalRuntimeRoleProcess) isDone() bool {
	if p == nil {
		return true
	}
	select {
	case <-p.waitDone:
		return true
	default:
		return false
	}
}

func (p *internalRuntimeRoleProcess) stop(ctx context.Context) error {
	if p == nil || p.process == nil {
		return nil
	}
	if p.isDone() {
		return p.wait()
	}
	_ = p.process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() {
		done <- p.wait()
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = p.process.Kill()
		select {
		case err := <-done:
			return errors.Join(ctx.Err(), err)
		case <-time.After(2 * time.Second):
			return ctx.Err()
		}
	}
}

func runInternalRuntimeRole(parent context.Context, startupPath string) error {
	raw, err := os.ReadFile(startupPath)
	if err != nil {
		if role, ok := parseInternalRuntimeRoleName(startupPath); ok {
			return runInternalRuntimeRoleName(parent, role)
		}
		return fmt.Errorf("read runtime role startup config: %w", err)
	}
	_ = os.Remove(startupPath)
	var startup internalRuntimeRoleStartupConfig
	if err := json.Unmarshal(raw, &startup); err != nil {
		return fmt.Errorf("decode runtime role startup config: %w", err)
	}
	switch startup.Role {
	case projectdaemon.RoleWikid, projectdaemon.RoleFrontd, projectdaemon.RoleWorkspaced:
	default:
		return fmt.Errorf("%w: %s", errUnsupportedRuntimeRole, startup.Role)
	}
	switch startup.Role {
	case projectdaemon.RoleFrontd:
		return runFrontdRole(parent, startup)
	case projectdaemon.RoleWorkspaced:
		return runWorkspacedRole(parent, startup)
	default:
		return waitForInternalRuntimeRoleSignal(parent)
	}
}

func runInternalRuntimeRoleName(parent context.Context, role projectdaemon.RoleName) error {
	switch role {
	case projectdaemon.RoleWikid, projectdaemon.RoleFrontd, projectdaemon.RoleWorkspaced:
		return waitForInternalRuntimeRoleSignal(parent)
	default:
		return errUnsupportedRuntimeRole
	}
}

func parseInternalRuntimeRoleName(raw string) (projectdaemon.RoleName, bool) {
	switch strings.TrimSpace(raw) {
	case "wikid":
		return projectdaemon.RoleWikid, true
	case "frontd":
		return projectdaemon.RoleFrontd, true
	case "workspaced":
		return projectdaemon.RoleWorkspaced, true
	default:
		return "", false
	}
}

func waitForInternalRuntimeRoleSignal(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	signals := make(chan os.Signal, 1)
	notifyRuntimeSignalsForRuntime(signals, os.Interrupt, syscall.SIGTERM)
	defer stopRuntimeSignalsForRuntime(signals)

	select {
	case <-ctx.Done():
		return nil
	case <-signals:
		return nil
	}
}

func runFrontdRole(parent context.Context, startup internalRuntimeRoleStartupConfig) error {
	cfg := startup.Runtime
	ownerCfg, err := daemonConfigForRuntime(cfg)
	if err != nil {
		return err
	}
	logCloser, err := setupLogger(cfg.Logging, os.Stdout, os.Stderr)
	if err != nil {
		return fmt.Errorf("invalid logging configuration: %w", err)
	}
	defer closeBestEffort(logCloser)

	opts, err := routerOptionsForRuntimeWithUserService(cfg, nil, cfg.BasePath, false, cfg.Host)
	if err != nil {
		return err
	}
	opts.HTTPRemoteUser = httpinternal.HTTPRemoteUserConfig{}
	publicRouter := httpinternal.NewRouter(nil, frontendConfigForRuntimeStorage(ownerCfg.DataDir), opts)
	controlPlaneProxy, err := newControlPlaneProxyForRuntime(startup.WikidURL, startup.DaemonToken)
	if err != nil {
		return err
	}
	workspaceProxy, err := newWorkspaceProxyForRuntime(frontd.WorkspaceProxyOptions{
		Upstream:    startup.WorkspacedURL,
		DaemonToken: startup.DaemonToken,
		Actor:       wikidActorResolver(startup.WikidURL, startup.DaemonToken),
	})
	if err != nil {
		return err
	}
	workspacesAPI, err := newWorkspacesAPIForRuntime(startup.WikidURL, startup.DaemonToken)
	if err != nil {
		return err
	}
	workspaceResolver, err := newWikidWorkspaceResolverForRuntime(startup.WikidURL, startup.DaemonToken)
	if err != nil {
		return err
	}
	actorResolver := wikidActorResolver(startup.WikidURL, startup.DaemonToken)
	workspaceRouterProxy := frontd.NewWorkspaceRouterProxy(frontd.WorkspaceRouterProxyOptions{
		Resolve: workspaceResolver,
		Actor: func(req *http.Request, workspaceID workspaceid.WorkspaceID) (projectdaemon.ActorContext, error) {
			clone := req.Clone(req.Context())
			clone.Header = req.Header.Clone()
			clone.Header.Set(projectdaemon.WorkspaceIDHeader, workspaceID.HTTPHeaderValue())
			return actorResolver(clone)
		},
	})
	workspaceMux := frontdWorkspaceMux(workspaceRouterProxy, workspaceProxy)
	var mcpProxy http.Handler
	if cfg.MCPTransports.HTTP {
		baseMCPProxy, err := frontdPublicMCPHandlerForRuntime(cfg, startup.WorkspacedURL, startup.DaemonToken, startup.WikidURL)
		if err != nil {
			return err
		}
		rootMCPWorkspaceResolver, err := newWikidSingleWorkspaceResolverForRuntime(startup.WikidURL, startup.DaemonToken)
		if err != nil {
			return err
		}
		mcpSessions := frontd.NewMCPSessionBindings()
		workspaceMCP := frontd.NewWorkspaceMCPHandler(frontd.WorkspaceMCPHandlerOptions{
			Sessions:    mcpSessions,
			Resolve:     workspaceResolver,
			ResolveRoot: rootMCPWorkspaceResolver,
			Proxy: func(route frontd.WorkspaceRoute) http.Handler {
				return frontdWorkspaceMCPProxy(route, actorResolver)
			},
		})
		workspaceMCP = frontdMCPBearerAuthHandler(cfg, startup.WikidURL, startup.DaemonToken, workspaceMCP)
		mcpProxy = frontdMCPMux(baseMCPProxy, workspaceMCP)
	}
	handler := frontd.NewIngressHandler(publicRouter, frontd.IngressOptions{
		BasePath:     cfg.BasePath,
		Workspace:    workspaceMux,
		Workspaces:   workspacesAPI,
		MCP:          mcpProxy,
		ControlPlane: controlPlaneProxy,
	})
	listener, err := netListenForRuntime("tcp", buildListenAddress(cfg.Host, cfg.Port))
	if err != nil {
		return fmt.Errorf("start frontd listener: %w", err)
	}
	defer closeBestEffort(listener)
	ready := internalRuntimeRoleReady{
		Role: projectdaemon.RoleFrontd,
		PID:  os.Getpid(),
		URL:  publicURLForListener(cfg.Host, listener, cfg.BasePath),
	}
	if err := writeInternalRuntimeRoleReady(startup.ReadyPath, ready); err != nil {
		return err
	}
	return serveInternalRuntimeHTTP(parent, projectdaemon.RoleFrontd, listener, handler, startup.ParentPID)
}
