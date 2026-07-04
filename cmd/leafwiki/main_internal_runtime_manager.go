package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/perber/wiki/internal/localization"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
	"golang.org/x/sync/singleflight"
)

func runInternalProjectDaemon(ctx context.Context, startupPath string) error {
	raw, err := os.ReadFile(startupPath)
	if err != nil {
		return fmt.Errorf("read daemon startup config: %w", err)
	}
	_ = os.Remove(startupPath)
	var cfg leafwikiRuntimeConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("decode daemon startup config: %w", err)
	}
	err = runProjectDaemonOwner(ctx, cfg)
	if err != nil {
		writeProjectDaemonStartupError(cfg.DaemonStartupErrorPath, err)
	}
	return err
}

type wikidFrontdRuntime struct {
	mu             sync.Mutex
	cfg            leafwikiRuntimeConfig
	daemonToken    string
	ctx            context.Context
	cancel         context.CancelFunc
	supervisor     *wikid.Supervisor
	processes      map[projectdaemon.RoleName]*internalRuntimeRoleProcess
	roles          []projectdaemon.RoleHealth
	workspacedURL  string
	wikidURL       string
	stopping       bool
	onRolesChanged func([]projectdaemon.RoleHealth)
}

type internalRuntimeRoleStartupConfig struct {
	Role          projectdaemon.RoleName `json:"role"`
	Runtime       leafwikiRuntimeConfig  `json:"runtime"`
	WorkspacedURL string                 `json:"workspacedUrl,omitempty"`
	WikidURL      string                 `json:"wikidUrl,omitempty"`
	DaemonToken   string                 `json:"daemonToken"`
	ReadyPath     string                 `json:"readyPath"`
	ParentPID     int                    `json:"parentPid"`
}

type internalRuntimeRoleReady struct {
	Role    projectdaemon.RoleName `json:"role"`
	PID     int                    `json:"pid"`
	URL     string                 `json:"url,omitempty"`
	Private bool                   `json:"private,omitempty"`
}

type internalRuntimeRoleProcess struct {
	role     projectdaemon.RoleName
	pid      int
	process  *os.Process
	done     <-chan error
	waitOnce sync.Once
	waitDone chan struct{}
	waitErr  error
}

const (
	federatedWorkspaceEnsureTimeout     = 30 * time.Second
	internalRuntimeRoleReadinessTimeout = 30 * time.Second
)

type federatedWorkspaceManager struct {
	mu               sync.Mutex
	base             leafwikiRuntimeConfig
	daemonToken      string
	wikidURL         string
	layout           wikid.Layout
	supervisor       *wikid.WorkspaceSupervisor
	processes        map[workspaceid.WorkspaceID]*internalRuntimeRoleProcess
	descriptors      map[workspaceid.WorkspaceID][]string
	workspaces       map[workspaceid.WorkspaceID]wikid.WorkspaceRecord
	ensureGroup      singleflight.Group
	stopped          bool
	startRole        func(internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error)
	removeDescriptor func(string) error
	writeDescriptor  func(wikid.WorkspaceRecord, leafwikiRuntimeConfig, internalRuntimeRoleReady) error
}

func newFederatedWorkspaceManager(base leafwikiRuntimeConfig, daemonToken string, wikidURL string, layout wikid.Layout, supervisor *wikid.WorkspaceSupervisor) *federatedWorkspaceManager {
	return &federatedWorkspaceManager{
		base:             base,
		daemonToken:      daemonToken,
		wikidURL:         wikidURL,
		layout:           layout,
		supervisor:       supervisor,
		processes:        map[workspaceid.WorkspaceID]*internalRuntimeRoleProcess{},
		descriptors:      map[workspaceid.WorkspaceID][]string{},
		workspaces:       map[workspaceid.WorkspaceID]wikid.WorkspaceRecord{},
		startRole:        startInternalRuntimeRoleProcess,
		removeDescriptor: projectdaemon.RemoveDescriptor,
	}
}

func (m *federatedWorkspaceManager) MarkReady(workspaceID workspaceid.WorkspaceID, pid int, url string) {
	if m == nil || m.supervisor == nil {
		return
	}
	m.supervisor.MarkReady(workspaceID, pid, url)
}

func (m *federatedWorkspaceManager) Ensure(ctx context.Context, workspace wikid.WorkspaceRecord) (wikid.WorkspaceStatus, error) {
	if m == nil || m.supervisor == nil {
		return wikid.WorkspaceStatus{}, errWorkspaceManagerUnavailable
	}
	workspaceID := workspace.ID
	if workspaceID == "" {
		return wikid.WorkspaceStatus{}, errWorkspaceIDRequired
	}
	m.mu.Lock()
	if current := m.supervisor.Status(workspaceID); current.State == wikid.WorkspaceStateRunning {
		if proc := m.processes[workspaceID]; proc == nil || !proc.isDone() {
			m.mu.Unlock()
			return current, nil
		}
	}
	m.mu.Unlock()

	resultCh := m.ensureGroup.DoChan(workspaceID.StorageKey(), func() (any, error) {
		return m.ensureWorkspace(workspaceID, workspace)
	})
	select {
	case <-ctx.Done():
		return m.supervisor.Status(workspaceID), ctx.Err()
	case result := <-resultCh:
		return federatedEnsureResultStatus(workspaceID, result.Val, result.Err)
	}
}

func federatedEnsureResultStatus(workspaceID workspaceid.WorkspaceID, value any, resultErr error) (wikid.WorkspaceStatus, error) {
	status, ok := value.(wikid.WorkspaceStatus)
	if !ok && resultErr == nil {
		return wikid.WorkspaceStatus{}, &federatedEnsureUnexpectedResultError{WorkspaceID: workspaceID, ResultType: fmt.Sprintf("%T", value)}
	}
	return status, resultErr
}

type federatedEnsureUnexpectedResultError struct {
	WorkspaceID workspaceid.WorkspaceID
	ResultType  string
}

func (err *federatedEnsureUnexpectedResultError) Error() string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("ensure workspace %q returned unexpected result %s", err.WorkspaceID, err.ResultType)
}

func (m *federatedWorkspaceManager) ensureWorkspace(workspaceID workspaceid.WorkspaceID, workspace wikid.WorkspaceRecord) (wikid.WorkspaceStatus, error) {
	m.mu.Lock()
	if current := m.supervisor.Status(workspaceID); current.State == wikid.WorkspaceStateRunning {
		if proc := m.processes[workspaceID]; proc == nil || !proc.isDone() {
			m.mu.Unlock()
			return current, nil
		}
	}
	m.workspaces[workspaceID] = workspace
	m.supervisor.MarkStarting(workspaceID)
	cfg := m.workspaceRuntimeConfig(workspace, "0")
	m.mu.Unlock()

	proc, ready, err := m.startRole(internalRuntimeRoleStartupConfig{
		Role:        projectdaemon.RoleWorkspaced,
		Runtime:     cfg,
		DaemonToken: m.daemonToken,
		WikidURL:    m.wikidURL,
	})
	if err != nil {
		m.supervisor.RecordCrash(workspaceID, err.Error())
		return m.supervisor.Status(workspaceID), err
	}
	writeDescriptor := m.writeWorkspaceDescriptor
	if m.writeDescriptor != nil {
		writeDescriptor = m.writeDescriptor
	}
	if err := writeDescriptor(workspace, cfg, ready); err != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = proc.stop(stopCtx)
		cancel()
		m.supervisor.RecordCrash(workspaceID, err.Error())
		return m.supervisor.Status(workspaceID), err
	}

	m.mu.Lock()
	m.processes[workspaceID] = proc
	m.supervisor.MarkReady(workspaceID, ready.PID, ready.URL)
	status := m.supervisor.Status(workspaceID)
	m.mu.Unlock()
	go m.monitorWorkspaceProcess(workspaceID, proc)
	return status, nil
}

func (m *federatedWorkspaceManager) workspaceRuntimeConfig(workspace wikid.WorkspaceRecord, port string) leafwikiRuntimeConfig {
	cfg := m.base
	cfg.Workspace = wiki.Workspace{
		ID:      workspace.ID,
		DataDir: strings.TrimSpace(workspace.DataDir),
		RootDir: strings.TrimSpace(workspace.RootDir),
	}
	cfg.MarkdownLinkRootPrefix = strings.TrimSpace(workspace.MarkdownLinkRootPrefix)
	cfg.Host = "127.0.0.1"
	cfg.Port = port
	cfg.EnableWorkspaceSync = true
	return cfg
}

func (m *federatedWorkspaceManager) writeWorkspaceDescriptor(workspace wikid.WorkspaceRecord, cfg leafwikiRuntimeConfig, ready internalRuntimeRoleReady) error {
	ownerCfg, err := daemonConfigForRuntime(cfg)
	if err != nil {
		return err
	}
	hash, err := configHashForRuntime(ownerCfg)
	if err != nil {
		return err
	}
	privateMCPURL := strings.TrimRight(ready.URL, "/") + "/mcp"
	desc := &projectdaemon.Descriptor{
		SchemaVersion:   projectdaemon.DescriptorSchemaVersion,
		RuntimeStack:    cfg.RuntimeStack,
		Role:            projectdaemon.RoleWorkspaced,
		WorkspaceID:     workspace.ID,
		PID:             ready.PID,
		StartedAt:       time.Now().UTC(),
		DataDir:         ownerCfg.DataDir,
		RootDir:         ownerCfg.RootDir,
		BasePath:        cfg.BasePath,
		ControlURL:      m.wikidURL,
		PrivateMCPURL:   privateMCPURL,
		PrivateMCPToken: m.daemonToken,
		ConfigHash:      hash,
		IdleTimeout:     cfg.DaemonIdleTimeout.String(),
		ControlToken:    m.daemonToken,
		Config:          ownerCfg,
		Roles: []projectdaemon.RoleHealth{
			{Name: projectdaemon.RoleWorkspaced, State: projectdaemon.RoleStateReady, PID: ready.PID, URL: ready.URL, Private: true, UpdatedAt: time.Now().UTC()},
		},
	}
	paths := []string{
		projectdaemon.DescriptorPath(ownerCfg.DataDir),
		workspaceRuntimeDescriptorPath(m.layout.RuntimeDir, workspace.ID),
	}
	for _, path := range paths {
		if err := removeNonRegularDescriptor(path); err != nil {
			return err
		}
		if err := writeDescriptorAtomicForRuntime(path, desc); err != nil {
			return err
		}
	}
	m.mu.Lock()
	m.descriptors[workspace.ID] = paths
	m.mu.Unlock()
	return nil
}

func removeNonRegularDescriptor(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inspect descriptor target %s: %w", path, err)
	}
	if info.Mode().IsRegular() {
		return nil
	}
	if err := projectdaemon.RemoveDescriptor(path); err != nil {
		return fmt.Errorf("remove non-regular descriptor %s: %w", path, err)
	}
	return nil
}

func (m *federatedWorkspaceManager) monitorWorkspaceProcess(workspaceID workspaceid.WorkspaceID, proc *internalRuntimeRoleProcess) {
	err := proc.wait()
	m.mu.Lock()
	current := m.processes[workspaceID]
	if current != proc {
		m.mu.Unlock()
		return
	}
	delete(m.processes, workspaceID)
	descriptorPaths := append([]string(nil), m.descriptors[workspaceID]...)
	delete(m.descriptors, workspaceID)
	workspace := m.workspaces[workspaceID]
	stopped := m.stopped
	message := "process exited"
	if err != nil {
		message = err.Error()
	}
	restartAt, restart := m.supervisor.RecordCrash(workspaceID, message)
	m.mu.Unlock()
	m.removeDescriptors(descriptorPaths)
	if restart && !stopped && workspace.ID != "" {
		go m.restartWorkspaceAfter(workspace, restartAt)
	}
}

func (m *federatedWorkspaceManager) removeDescriptors(paths []string) {
	for _, path := range paths {
		if err := m.removeDescriptor(path); err != nil {
			fmt.Fprintln(os.Stderr, localization.English.Render(localization.MessageIDCLIStatusRemoveWorkspaceDescriptor, "", path, err.Error()).Message)
		}
	}
}

func (m *federatedWorkspaceManager) restartWorkspaceAfter(workspace wikid.WorkspaceRecord, restartAt time.Time) {
	delay := time.Until(restartAt)
	if delay > 0 {
		timer := time.NewTimer(delay)
		<-timer.C
	}
	m.mu.Lock()
	stopped := m.stopped
	m.mu.Unlock()
	if stopped {
		return
	}
	_, _ = m.Ensure(context.Background(), workspace)
}

func (m *federatedWorkspaceManager) stop(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	m.stopped = true
	processes := make([]*internalRuntimeRoleProcess, 0, len(m.processes))
	for _, proc := range m.processes {
		processes = append(processes, proc)
	}
	descriptors := make([]string, 0)
	for _, paths := range m.descriptors {
		descriptors = append(descriptors, paths...)
	}
	m.processes = map[workspaceid.WorkspaceID]*internalRuntimeRoleProcess{}
	m.mu.Unlock()
	var errs []error
	for _, proc := range processes {
		if err := proc.stop(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	for _, path := range descriptors {
		if err := m.removeDescriptor(path); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func workspaceRuntimeDescriptorPath(runtimeDir string, workspaceID workspaceid.WorkspaceID) string {
	return filepath.Join(runtimeDir, "workspaces", workspaceID.StorageKey()+".json")
}
