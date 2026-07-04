package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/localization"
	"github.com/perber/wiki/internal/locking"
	"github.com/perber/wiki/internal/projectdaemon"
)

func readHealthyProjectDaemon(ctx context.Context, descriptorPath string, ownerCfg projectdaemon.Config) (*projectdaemon.Descriptor, bool, error) {
	desc, err := projectdaemon.ReadTrustedDescriptor(descriptorPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		locksFree, lockErr := projectDaemonLocksFree(ownerCfg.DataDir, ownerCfg.RootDir)
		if lockErr != nil {
			return nil, false, lockErr
		}
		if !locksFree {
			return nil, false, fmt.Errorf("read project daemon descriptor: %w", err)
		}
		_ = projectdaemon.RemoveDescriptor(descriptorPath)
		return nil, false, nil
	}
	if desc.SchemaVersion != projectdaemon.DescriptorSchemaVersion {
		locksFree, lockErr := projectDaemonLocksFree(ownerCfg.DataDir, ownerCfg.RootDir)
		if lockErr != nil {
			return nil, false, lockErr
		}
		if !locksFree {
			return nil, false, fmt.Errorf("%w: schema version = %d, want %d while project locks are held", projectdaemon.ErrDescriptorSchemaMismatch, desc.SchemaVersion, projectdaemon.DescriptorSchemaVersion)
		}
		return desc, false, nil
	}
	if desc.DataDir != ownerCfg.DataDir || desc.RootDir != ownerCfg.RootDir {
		healthy, err := projectDaemonDescriptorHealthy(ctx, desc)
		if err != nil {
			return nil, false, err
		}
		if healthy {
			return nil, false, projectDaemonIdentityMismatch(desc, ownerCfg)
		}
		return desc, false, nil
	}
	healthy, err := projectDaemonDescriptorHealthy(ctx, desc)
	if err != nil {
		return nil, false, err
	}
	return desc, healthy, nil
}

func projectDaemonDescriptorHealthy(ctx context.Context, desc *projectdaemon.Descriptor) (bool, error) {
	if desc == nil {
		return false, nil
	}
	if desc.Role == projectdaemon.RoleWorkspaced && strings.TrimSpace(desc.PrivateMCPURL) != "" && strings.TrimSpace(desc.PrivateMCPToken) != "" {
		if !isTrustedDaemonControlURL(desc.PrivateMCPURL) {
			return false, errPrivateMCPURLUntrusted
		}
		if !processPIDAlive(desc.PID) {
			return false, nil
		}
		return workspacedPrivateMCPEndpointReachable(ctx, desc), nil
	}
	locksHeld, err := projectDaemonLocksHeld(desc.DataDir, desc.RootDir)
	if err != nil {
		return false, err
	}
	if !locksHeld {
		return false, nil
	}
	if !isTrustedDaemonControlURL(desc.ControlURL) {
		return false, errControlURLUntrusted
	}
	pingCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	health, err := projectdaemon.NewClient(desc.ControlURL, desc.ControlToken).Health(pingCtx)
	if err != nil {
		return false, fmt.Errorf("%w: %w", errControlHealthUnreachable, err)
	}
	if !daemonHealthMatchesDescriptor(desc, health) {
		return false, errControlHealthMismatch
	}
	return true, nil
}

func workspacedPrivateMCPEndpointReachable(ctx context.Context, desc *projectdaemon.Descriptor) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, strings.TrimSpace(desc.PrivateMCPURL), nil)
	if err != nil {
		return false
	}
	req.Header.Set(projectdaemon.ControlTokenHeader, strings.TrimSpace(desc.PrivateMCPToken))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return false
	}
	return resp.StatusCode < http.StatusInternalServerError
}

func processPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := processFindProcessForRuntime(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

func projectDaemonIdentityMismatch(desc *projectdaemon.Descriptor, ownerCfg projectdaemon.Config) error {
	var mismatches []projectdaemon.Mismatch
	if desc.DataDir != ownerCfg.DataDir {
		mismatches = append(mismatches, projectdaemon.Mismatch{
			Field: "data-dir",
			Want:  desc.DataDir,
			Got:   ownerCfg.DataDir,
		})
	}
	if desc.RootDir != ownerCfg.RootDir {
		mismatches = append(mismatches, projectdaemon.Mismatch{
			Field: "root-dir",
			Want:  desc.RootDir,
			Got:   ownerCfg.RootDir,
		})
	}
	return projectdaemon.NewConfigMismatchError(mismatches)
}

var projectDaemonWaitTimeout = 30 * time.Second

func waitForProjectDaemon(ctx context.Context, descriptorPath string, errorPath string, ownerCfg projectdaemon.Config, requestTransports mcpTransports) (*projectdaemon.Descriptor, error) {
	deadlineCtx, cancel := context.WithTimeout(ctx, projectDaemonWaitTimeout)
	defer cancel()
	defer removePathBestEffort(errorPath)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	var startupErr projectDaemonStartupError
	for {
		desc, healthy, err := readHealthyProjectDaemon(deadlineCtx, descriptorPath, ownerCfg)
		if err != nil {
			lastErr = err
		} else if healthy {
			if mismatches := compareProjectDaemonDescriptorForRequest(desc, ownerCfg, requestTransports); len(mismatches) > 0 {
				return nil, projectdaemon.NewConfigMismatchError(mismatches)
			}
			return desc, nil
		}
		if raw, err := os.ReadFile(errorPath); err == nil && len(raw) > 0 {
			if startupErr.RenderedMessage == "" {
				startupErr = parseProjectDaemonStartupError(raw)
			}
			if startupErr.RenderedMessage != "" && !startupErr.IsLock() {
				return nil, formatProjectDaemonStartupError(startupErr)
			}
			if startupErr.RenderedMessage != "" && startupErr.IsLock() {
				disjointRootOwner, lockErr := projectDaemonDataLockFreeRootLockHeld(ownerCfg.DataDir, ownerCfg.RootDir)
				if lockErr != nil {
					lastErr = lockErr
				} else if disjointRootOwner {
					return nil, formatProjectDaemonStartupError(startupErr)
				}
			}
		}
		select {
		case <-deadlineCtx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil, ctx.Err()
			}
			if startupErr.RenderedMessage != "" {
				return nil, formatProjectDaemonStartupError(startupErr)
			}
			if lastErr != nil {
				return nil, fmt.Errorf("%w: %w", errProjectLockedNoAttachableDaemon, lastErr)
			}
			return nil, errProjectLockedNoAttachableDaemon
		case <-ticker.C:
		}
	}
}

const (
	projectDaemonStartupErrorKindStartup = "startup"
	projectDaemonStartupErrorKindLock    = "lock"
)

type projectDaemonStartupError struct {
	Kind            string                 `json:"kind"`
	MessageID       sharederrors.MessageID `json:"messageId,omitempty"`
	RenderedMessage string                 `json:"message"`
}

func (e projectDaemonStartupError) IsLock() bool {
	return e.Kind == projectDaemonStartupErrorKindLock
}

func parseProjectDaemonStartupError(raw []byte) projectDaemonStartupError {
	trimmed := strings.TrimSpace(string(raw))
	var structured projectDaemonStartupError
	if err := json.Unmarshal(raw, &structured); err == nil && strings.TrimSpace(structured.RenderedMessage) != "" {
		structured.RenderedMessage = strings.TrimSpace(structured.RenderedMessage)
		if structured.Kind == "" {
			structured.Kind = projectDaemonStartupErrorKindStartup
		}
		if structured.MessageID == "" {
			structured.MessageID = localization.MessageIDCLIErrorProjectDaemonFailed
		}
		return structured
	}
	kind := projectDaemonStartupErrorKindStartup
	if isLegacyProjectDaemonLockStartupMessage(trimmed) {
		kind = projectDaemonStartupErrorKindLock
	}
	return projectDaemonStartupError{
		Kind:            kind,
		MessageID:       localization.MessageIDCLIErrorProjectDaemonFailed,
		RenderedMessage: trimmed,
	}
}

func formatProjectDaemonStartupError(startupErr projectDaemonStartupError) error {
	if startupErr.IsLock() {
		return fmt.Errorf("%w: %s", errProjectLockedNoAttachableDaemon, startupErr.RenderedMessage)
	}
	return fmt.Errorf("%w: %s", errProjectDaemonStartupFailed, startupErr.RenderedMessage)
}

func writeProjectDaemonStartupError(path string, err error) {
	if strings.TrimSpace(path) == "" || err == nil {
		return
	}
	kind := projectDaemonStartupErrorKindStartup
	if locking.IsLockHeld(err) {
		kind = projectDaemonStartupErrorKindLock
	}
	raw, marshalErr := jsonMarshalForRuntime(projectDaemonStartupError{
		Kind:            kind,
		MessageID:       localization.MessageIDCLIErrorProjectDaemonFailed,
		RenderedMessage: err.Error(),
	})
	if marshalErr != nil {
		raw = []byte(err.Error())
	}
	_ = os.WriteFile(path, raw, 0o600)
}
