package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	corebranding "github.com/perber/wiki/internal/branding"
	"github.com/perber/wiki/internal/frontd"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaced"
)

func frontdWorkspaceMux(workspaceRouterProxy http.Handler, workspaceProxy http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, frontd.PublicWorkspacesPrefix+"/") {
			workspaceRouterProxy.ServeHTTP(w, req)
			return
		}
		workspaceProxy.ServeHTTP(w, req)
	})
}

func frontdWorkspaceMCPProxy(route frontd.WorkspaceRoute, actorResolver func(*http.Request) (projectdaemon.ActorContext, error)) http.Handler {
	proxy, err := newMCPProxyWithActorForRuntime(frontd.WorkspaceProxyOptions{
		Upstream:    route.Upstream,
		DaemonToken: route.DaemonToken,
		Actor: func(req *http.Request) (projectdaemon.ActorContext, error) {
			clone := req.Clone(req.Context())
			clone.Header = req.Header.Clone()
			clone.Header.Set(projectdaemon.WorkspaceIDHeader, route.WorkspaceID.HTTPHeaderValue())
			return actorResolver(clone)
		},
	})
	if err != nil {
		return workspaceMCPUnavailableHandler()
	}
	return proxy
}

func frontdMCPMux(baseMCPProxy http.Handler, workspaceMCP http.Handler) http.Handler {
	return localOnlyHTTPMCPHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/mcp" || strings.HasPrefix(req.URL.Path, "/mcp/workspaces/") {
			workspaceMCP.ServeHTTP(w, req)
			return
		}
		baseMCPProxy.ServeHTTP(w, req)
	}))
}

func frontendConfigForRuntimeStorage(storageDir string) httpinternal.FrontendConfig {
	store := corebranding.NewBrandingStore(storageDir)
	return httpinternal.FrontendConfig{
		StorageDir: storageDir,
		GetSiteName: func() string {
			cfg, err := store.Load()
			if err != nil || cfg == nil {
				return ""
			}
			return cfg.SiteName
		},
		GetFaviconFile: func() string {
			cfg, err := store.Load()
			if err != nil || cfg == nil {
				return ""
			}
			return cfg.FaviconFile
		},
	}
}

func runWorkspacedRole(parent context.Context, startup internalRuntimeRoleStartupConfig) error {
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

	w, err := newRuntimeWikiForRuntime(cfg, ownerCfg, runtimeWikiWorkspaceOnly)
	if err != nil {
		return err
	}
	defer closeBestEffort(w)

	opts, err := routerOptionsForRuntime(cfg, w, "", false, "127.0.0.1")
	if err != nil {
		return err
	}
	opts.HTTPRemoteUser = httpinternal.HTTPRemoteUserConfig{}
	router := workspaced.NewAuthenticatedRouter(w, opts, workspaced.PrivateAuthOptions{
		DaemonToken: startup.DaemonToken,
		WorkspaceID: runtimeWorkspaceSemanticID(cfg.Workspace),
	})
	mcpOpts := opts
	mcpOpts.BasePath = cfg.BasePath
	mcpOpts.MCPEnabled = true
	mcpOpts.MCPBindHost = "127.0.0.1"
	privateMCP := w.ActorContextMCPHTTPHandler(mcpOpts)
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/mcp" {
			if req.Header.Get(projectdaemon.ControlTokenHeader) != startup.DaemonToken {
				writePrivateMCPUnauthorized(w)
				return
			}
			privateMCP.ServeHTTP(w, req)
			return
		}
		router.ServeHTTP(w, req)
	})
	listener, err := netListenForRuntime("tcp", buildListenAddress("127.0.0.1", cfg.Port))
	if err != nil {
		return fmt.Errorf("start workspaced listener: %w", err)
	}
	defer closeBestEffort(listener)
	ready := internalRuntimeRoleReady{
		Role:    projectdaemon.RoleWorkspaced,
		PID:     os.Getpid(),
		URL:     "http://" + listener.Addr().String(),
		Private: true,
	}
	if err := writeInternalRuntimeRoleReady(startup.ReadyPath, ready); err != nil {
		return err
	}
	return serveInternalRuntimeHTTP(parent, projectdaemon.RoleWorkspaced, listener, handler, startup.ParentPID)
}

func serveInternalRuntimeHTTP(parent context.Context, role projectdaemon.RoleName, listener net.Listener, handler http.Handler, parentPID int) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	signals := make(chan os.Signal, 1)
	notifyRuntimeSignalsForRuntime(signals, os.Interrupt, syscall.SIGTERM)
	defer stopRuntimeSignalsForRuntime(signals)
	go func() {
		select {
		case <-signals:
			cancel()
		case <-ctx.Done():
		}
	}()
	go cancelWhenParentExits(ctx, cancel, parentPID, 2*time.Second)
	server := &http.Server{Addr: listener.Addr().String(), Handler: handler}
	done := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
			err = nil
		}
		done <- err
	}()
	select {
	case <-ctx.Done():
	case err := <-done:
		if err != nil {
			return fmt.Errorf("%s server failed: %w", role, err)
		}
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := shutdownInternalRuntimeHTTPServerForRuntime(server, shutdownCtx); err != nil {
		return err
	}
	return nil
}

func cancelWhenParentExits(ctx context.Context, cancel context.CancelFunc, parentPID int, interval time.Duration) {
	if parentPID <= 0 {
		return
	}
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !processAlive(parentPID) {
				cancel()
				return
			}
		}
	}
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func publicURLForListener(host string, listener net.Listener, basePath string) string {
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil || port == "" {
		return "http://" + listener.Addr().String() + basePath
	}
	return nativeStdioHTTPURL(host, port, basePath)
}
