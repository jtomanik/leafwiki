package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
)

func spawnProjectDaemonOwner(cfg leafwikiRuntimeConfig) (string, error) {
	ownerCfg, err := daemonOwnerRuntimeConfig(cfg)
	if err != nil {
		return "", err
	}
	errorFile, err := createTempFileForRuntime("", "leafwiki-project-daemon-*.err")
	if err != nil {
		return "", fmt.Errorf("create daemon startup error file: %w", err)
	}
	errorPath := errorFile.Name()
	_ = errorFile.Close()
	_ = os.Remove(errorPath)
	ownerCfg.DaemonStartupErrorPath = errorPath
	raw, err := jsonMarshalForRuntime(ownerCfg)
	if err != nil {
		return "", err
	}
	tmp, err := createTempFileForRuntime("", "leafwiki-project-daemon-*.json")
	if err != nil {
		return "", fmt.Errorf("create daemon startup config: %w", err)
	}
	startupPath := tmp.Name()
	removeStartupConfig := true
	defer func() {
		if removeStartupConfig {
			_ = os.Remove(startupPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	exe, err := projectDaemonExecutable()
	if err != nil {
		return "", err
	}
	args := []string{"--internal-project-daemon", startupPath}
	if os.Getenv("GO_WANT_LEAFWIKI_HELPER_PROCESS") == "1" {
		args = []string{"-test.run=TestLeafWikiSuite", "--", "--internal-project-daemon", startupPath}
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = daemonOwnerEnv()
	cleanupIO, err := configureDaemonOwnerIO(cmd, cfg)
	if err != nil {
		return "", err
	}
	defer cleanupIO()
	if err := startCommandForRuntime(cmd); err != nil {
		return "", fmt.Errorf("start project daemon: %w", err)
	}
	removeStartupConfig = false
	scheduleProjectDaemonStartupConfigCleanup(startupPath)
	if cmd.Process != nil {
		if err := releaseProcessForRuntime(cmd.Process); err != nil {
			return "", fmt.Errorf("release project daemon process: %w", err)
		}
	}
	return errorPath, nil
}

func scheduleProjectDaemonStartupConfigCleanup(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	delay := projectDaemonStartupConfigPostStartCleanupDelay
	if delay <= 0 {
		delay = 30 * time.Second
	}
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		<-timer.C
		_ = os.Remove(path)
	}()
}

func daemonOwnerRuntimeConfig(cfg leafwikiRuntimeConfig) (leafwikiRuntimeConfig, error) {
	ownerCfg, err := daemonWorkspaceRuntimeConfig(cfg)
	if err != nil {
		return leafwikiRuntimeConfig{}, err
	}
	workspace, err := globalRuntimeWorkspace()
	if err != nil {
		return leafwikiRuntimeConfig{}, err
	}
	ownerCfg.Workspace = workspace
	ownerCfg.MarkdownLinkRootPrefix = ""
	return ownerCfg, nil
}

func globalRuntimeWorkspace() (wiki.Workspace, error) {
	homeDir, err := globalRuntimeHomeDir()
	if err != nil {
		return wiki.Workspace{}, err
	}
	return wiki.NormalizeWorkspace(wiki.Workspace{
		ID:      wikid.HomeWorkspaceID,
		DataDir: homeDir,
		RootDir: filepath.Join(homeDir, "root"),
	}), nil
}

func globalRuntimeHomeDir() (string, error) {
	home, err := userHomeDirForRuntime()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	home = strings.TrimSpace(home)
	if home == "" {
		return "", errUserHomeEmpty
	}
	return filepath.Clean(filepath.Join(home, ".leafwiki")), nil
}

func configureDaemonOwnerIO(cmd *exec.Cmd, cfg leafwikiRuntimeConfig) (func(), error) {
	configureDaemonOwnerProcessGroup(cmd)
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

func configureDaemonOwnerProcessGroup(cmd *exec.Cmd) {
	attr := &syscall.SysProcAttr{}
	if setSysProcAttrBool(attr, "Setpgid", true) {
		cmd.SysProcAttr = attr
	}
}

func setSysProcAttrBool(attr *syscall.SysProcAttr, field string, value bool) bool {
	if attr == nil {
		return false
	}
	v := reflect.ValueOf(attr).Elem().FieldByName(field)
	if !v.IsValid() || !v.CanSet() || v.Kind() != reflect.Bool {
		return false
	}
	v.SetBool(value)
	return true
}

func openDaemonNullDevice() (*os.File, func(), error) {
	nullDevice, err := openDaemonNullDeviceForRuntime()
	if err != nil {
		return nil, func() {}, fmt.Errorf("open daemon null device: %w", err)
	}
	return nullDevice, func() {
		_ = nullDevice.Close()
	}, nil
}

func daemonOwnerEnv() []string {
	env := []string{}
	secretKeys := map[string]bool{
		"LEAFWIKI_MCP_API_KEY":            true,
		"LEAFWIKI_RUN_MCP_API_KEY":        true,
		"LEAFWIKI_JWT_SECRET":             true,
		"LEAFWIKI_RUN_MCP_JWT_SECRET":     true,
		"LEAFWIKI_ADMIN_PASSWORD":         true,
		"LEAFWIKI_RUN_MCP_ADMIN_PASSWORD": true,
	}
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok && secretKeys[key] {
			continue
		}
		env = append(env, entry)
	}
	return env
}
