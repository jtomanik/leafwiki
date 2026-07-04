package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wikid"
)

func startWikidFrontdRuntime(parent context.Context, cfg leafwikiRuntimeConfig, daemonToken string, wikidURL string) (*wikidFrontdRuntime, error) {
	ctx, cancel := context.WithCancel(parent)
	runtime := &wikidFrontdRuntime{
		cfg:         cfg,
		daemonToken: daemonToken,
		ctx:         ctx,
		cancel:      cancel,
		supervisor:  wikid.NewSupervisor(wikid.SupervisorOptions{}),
		processes:   map[projectdaemon.RoleName]*internalRuntimeRoleProcess{},
		wikidURL:    wikidURL,
	}
	runtime.supervisor.MarkReady(projectdaemon.RoleWikid, os.Getpid(), "", false)
	if err := runtime.withProcessLock(func() error {
		if err := runtime.startWorkspacedLocked(); err != nil {
			return err
		}
		if err := runtime.startFrontdLocked(); err != nil {
			return err
		}
		runtime.publishRolesLocked()
		return nil
	}); err != nil {
		cancel()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = runtime.stop(stopCtx)
		stopCancel()
		return nil, err
	}
	return runtime, nil
}

func (s *wikidFrontdRuntime) startWorkspacedLocked() error {
	workspacedCfg := s.cfg
	workspacedCfg.Host = "127.0.0.1"
	workspacedCfg.Port = "0"
	proc, ready, err := startInternalRuntimeRoleProcessForRuntime(internalRuntimeRoleStartupConfig{
		Role:        projectdaemon.RoleWorkspaced,
		Runtime:     workspacedCfg,
		DaemonToken: s.daemonToken,
	})
	if err != nil {
		return err
	}
	s.processes[projectdaemon.RoleWorkspaced] = proc
	s.workspacedURL = ready.URL
	s.supervisor.MarkReady(projectdaemon.RoleWorkspaced, ready.PID, ready.URL, true)
	go s.monitorRoleProcess(proc)
	return nil
}

func (s *wikidFrontdRuntime) startFrontdLocked() error {
	if strings.TrimSpace(s.workspacedURL) == "" {
		return errRuntimeWorkspacedURLUnavailable
	}
	proc, ready, err := startInternalRuntimeRoleProcessForRuntime(internalRuntimeRoleStartupConfig{
		Role:          projectdaemon.RoleFrontd,
		Runtime:       s.cfg,
		WorkspacedURL: s.workspacedURL,
		WikidURL:      s.wikidURL,
		DaemonToken:   s.daemonToken,
	})
	if err != nil {
		return err
	}
	s.processes[projectdaemon.RoleFrontd] = proc
	s.supervisor.MarkReady(projectdaemon.RoleFrontd, ready.PID, ready.URL, false)
	go s.monitorRoleProcess(proc)
	return nil
}

func (s *wikidFrontdRuntime) monitorRoleProcess(proc *internalRuntimeRoleProcess) {
	err := proc.wait()
	s.mu.Lock()
	current := s.processes[proc.role]
	if s.stopping || current != proc {
		s.mu.Unlock()
		return
	}
	message := "process exited"
	if err != nil {
		message = err.Error()
	}
	restartAt, scheduled := s.supervisor.RecordCrash(proc.role, message)
	s.publishRolesLocked()
	if !scheduled {
		s.mu.Unlock()
		return
	}
	delay := time.Until(restartAt)
	s.mu.Unlock()
	if delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-s.ctx.Done():
			timer.Stop()
			return
		}
	}
	s.restartRole(proc.role)
}

func (s *wikidFrontdRuntime) restartRole(role projectdaemon.RoleName) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping || s.ctx.Err() != nil {
		return
	}
	var err error
	switch role {
	case projectdaemon.RoleWorkspaced:
		err = s.startWorkspacedLocked()
	case projectdaemon.RoleFrontd:
		err = s.startFrontdLocked()
	default:
		err = fmt.Errorf("unsupported restart role %s", role)
	}
	if err != nil {
		s.supervisor.RecordCrash(role, err.Error())
	}
	s.publishRolesLocked()
}

func (s *wikidFrontdRuntime) setRoleChangeCallback(callback func([]projectdaemon.RoleHealth)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onRolesChanged = callback
	s.publishRolesLocked()
}

func (s *wikidFrontdRuntime) withProcessLock(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn()
}

func (s *wikidFrontdRuntime) roleHealthSnapshot() []projectdaemon.RoleHealth {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]projectdaemon.RoleHealth(nil), s.roles...)
}

func (s *wikidFrontdRuntime) publishRolesLocked() {
	roles := s.supervisor.Roles()
	s.roles = roles
	if s.onRolesChanged != nil {
		s.onRolesChanged(append([]projectdaemon.RoleHealth(nil), roles...))
	}
}

func requiredRuntimeRoleHealth() []projectdaemon.RoleName {
	return []projectdaemon.RoleName{
		projectdaemon.RoleWikid,
		projectdaemon.RoleFrontd,
		projectdaemon.RoleWorkspaced,
	}
}

func startInternalRuntimeRoleProcess(startup internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
	readyFile, err := createTempFileForRuntime("", "leafwiki-runtime-ready-*.json")
	if err != nil {
		return nil, internalRuntimeRoleReady{}, fmt.Errorf("create %s ready file: %w", startup.Role, err)
	}
	readyPath := readyFile.Name()
	_ = readyFile.Close()
	_ = os.Remove(readyPath)
	startup.ReadyPath = readyPath
	startup.ParentPID = os.Getpid()

	startupPath, err := writeInternalRuntimeRoleStartupConfig(startup)
	if err != nil {
		return nil, internalRuntimeRoleReady{}, err
	}
	removeStartupConfig := true
	defer func() {
		if removeStartupConfig {
			_ = os.Remove(startupPath)
		}
	}()

	exe, err := projectDaemonExecutable()
	if err != nil {
		return nil, internalRuntimeRoleReady{}, err
	}
	cmd := exec.Command(exe, internalRuntimeRoleArgs(startupPath)...)
	cmd.Env = daemonOwnerEnv()
	configureDaemonOwnerProcessGroup(cmd)
	cleanupIO, err := configureInternalRuntimeRoleIO(cmd, startup.Runtime)
	if err != nil {
		return nil, internalRuntimeRoleReady{}, err
	}
	if err := startCommandForRuntime(cmd); err != nil {
		cleanupIO()
		return nil, internalRuntimeRoleReady{}, fmt.Errorf("start %s role process: %w", startup.Role, err)
	}
	cleanupIO()
	if cmd.Process == nil {
		return nil, internalRuntimeRoleReady{}, fmt.Errorf("start %s role process: %w", startup.Role, errRuntimeRoleMissingProcessHandle)
	}
	removeStartupConfig = false
	scheduleProjectDaemonStartupConfigCleanup(startupPath)

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	proc := &internalRuntimeRoleProcess{
		role:     startup.Role,
		pid:      cmd.Process.Pid,
		process:  cmd.Process,
		done:     done,
		waitDone: make(chan struct{}),
	}
	ready, err := waitForInternalRuntimeRoleReady(readyPath, proc, internalRuntimeRoleReadinessTimeoutForProcess)
	_ = os.Remove(readyPath)
	if err != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = proc.stop(stopCtx)
		stopCancel()
		return nil, internalRuntimeRoleReady{}, err
	}
	if ready.Role != startup.Role {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = proc.stop(stopCtx)
		stopCancel()
		return nil, internalRuntimeRoleReady{}, fmt.Errorf("%s role reported readiness for %s", startup.Role, ready.Role)
	}
	return proc, ready, nil
}

func writeInternalRuntimeRoleStartupConfig(startup internalRuntimeRoleStartupConfig) (string, error) {
	raw, err := jsonMarshalForRuntime(startup)
	if err != nil {
		return "", err
	}
	tmp, err := createTempFileForRuntime("", "leafwiki-runtime-role-*.json")
	if err != nil {
		return "", fmt.Errorf("create %s startup config: %w", startup.Role, err)
	}
	path := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(path)
		return "", err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func waitForInternalRuntimeRoleReady(path string, proc *internalRuntimeRoleProcess, timeout time.Duration) (internalRuntimeRoleReady, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	done := make(chan error, 1)
	go func() {
		done <- proc.wait()
	}()
	for {
		select {
		case err := <-done:
			if err == nil {
				err = errRuntimeRoleExitedBeforeReadiness
			}
			return internalRuntimeRoleReady{}, err
		case <-deadline.C:
			return internalRuntimeRoleReady{}, errRuntimeRoleReadinessTimeout
		case <-ticker.C:
			raw, err := os.ReadFile(path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return internalRuntimeRoleReady{}, err
			}
			if len(strings.TrimSpace(string(raw))) == 0 {
				continue
			}
			var ready internalRuntimeRoleReady
			if err := json.Unmarshal(raw, &ready); err != nil {
				return internalRuntimeRoleReady{}, err
			}
			if ready.PID <= 0 {
				return internalRuntimeRoleReady{}, errRuntimeRoleInvalidPID
			}
			return ready, nil
		}
	}
}
