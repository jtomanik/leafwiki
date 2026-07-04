package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wikid"
)

func runProjectDaemonOwner(parent context.Context, cfg leafwikiRuntimeConfig) error {
	ownerCfg, err := daemonConfigForRuntime(cfg)
	if err != nil {
		return err
	}
	dataDirMissingBeforeLock := false
	if _, err := statPathForRuntime(ownerCfg.DataDir); os.IsNotExist(err) {
		dataDirMissingBeforeLock = true
	}
	dataLock, err := acquireDataDirLockForRuntime(ownerCfg.DataDir)
	if err != nil {
		return fmt.Errorf("acquire data directory lock: %w", err)
	}
	defer releaseRuntimeLockBestEffort(dataLock)

	logCloser, err := setupLogger(cfg.Logging, os.Stdout, os.Stderr)
	if err != nil {
		return fmt.Errorf("invalid logging configuration: %w", err)
	}
	defer closeBestEffort(logCloser)

	if cfg.DisableAuth {
		slog.Default().Warn("Authentication disabled. Wiki is publicly accessible without authentication.")
	}
	if cfg.AllowInsecure {
		slog.Default().Warn("allow-insecure enabled. Auth cookies may be transmitted over plain HTTP (INSECURE).")
	}
	if cfg.EnableHTTPRemoteUser {
		slog.Default().Info("Reverse-proxy authentication enabled",
			"header", cfg.HTTPRemoteUserHeader,
			"trusted_proxies", cfg.TrustedProxyIPsRaw,
		)
	}
	if dataDirMissingBeforeLock {
		slog.Default().Info("Data directory created", "path", cfg.Workspace.DataDir)
	}
	if _, err := statPathForRuntime(ownerCfg.DataDir); os.IsNotExist(err) {
		if err := mkdirAllForRuntime(ownerCfg.DataDir, 0o755); err != nil {
			return fmt.Errorf("create data directory: %w", err)
		}
	}
	if _, err := statPathForRuntime(ownerCfg.RootDir); os.IsNotExist(err) {
		if err := mkdirAllForRuntime(ownerCfg.RootDir, 0o755); err != nil {
			return fmt.Errorf("create root directory: %w", err)
		}
	}
	rootLock, err := acquireRootDirLockForRuntime(ownerCfg.RootDir)
	if err != nil {
		return fmt.Errorf("acquire root directory lock: %w", err)
	}
	defer releaseRuntimeLockBestEffort(rootLock)

	if err := wikid.CleanupLegacyAuthDBs(ownerCfg.DataDir); err != nil {
		return fmt.Errorf("cleanup legacy auth DBs: %w", err)
	}
	authPaths := wikid.AuthStoragePaths(ownerCfg.DataDir)
	if err := os.MkdirAll(authPaths.AuthDir, 0o755); err != nil {
		return fmt.Errorf("create wikid auth dir: %w", err)
	}
	if err := os.MkdirAll(authPaths.OAuthDir, 0o755); err != nil {
		return fmt.Errorf("create wikid oauth dir: %w", err)
	}
	return runWikidFrontdOwnerForProjectDaemon(parent, cfg, ownerCfg)
}

func waitForFirstProjectDaemonSession(ctx context.Context, sessions *projectdaemon.SessionRegistry, interval time.Duration) error {
	if interval <= 0 {
		interval = 25 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if sessions.SeenSession() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func waitForFirstProjectDaemonActivity(ctx context.Context, sessions *projectdaemon.SessionRegistry, presence *projectdaemon.AgentPresenceRegistry, interval time.Duration) error {
	if interval <= 0 {
		interval = 25 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		seen, _ := projectDaemonSeenActivityCount(sessions, presence)
		if seen {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func idleShutdownCallback(ctx context.Context, cancel context.CancelFunc, idleTimeout time.Duration, currentCount func() int) func(count int) {
	var mu sync.Mutex
	var timer *time.Timer
	lastCount := -1
	return func(count int) {
		mu.Lock()
		defer mu.Unlock()
		if currentCount != nil && currentCount() != count {
			return
		}
		if count == lastCount {
			return
		}
		lastCount = count
		if timer != nil {
			timer.Stop()
			timer = nil
		}
		if count > 0 {
			return
		}
		if idleTimeout == 0 {
			if currentCount == nil || currentCount() == 0 {
				cancel()
			}
			return
		}
		timer = time.AfterFunc(idleTimeout, func() {
			if ctx.Err() == nil && (currentCount == nil || currentCount() == 0) {
				cancel()
			}
		})
	}
}

func projectDaemonActivityCount(sessions *projectdaemon.SessionRegistry, presence *projectdaemon.AgentPresenceRegistry) int {
	count := 0
	if sessions != nil {
		count += sessions.Count()
	}
	if presence != nil {
		count += presence.Count()
	}
	return count
}

func projectDaemonSeenActivityCount(sessions *projectdaemon.SessionRegistry, presence *projectdaemon.AgentPresenceRegistry) (bool, int) {
	seen := false
	count := 0
	if sessions != nil {
		sessionSeen, sessionCount := sessions.SeenSessionCount()
		seen = seen || sessionSeen
		count += sessionCount
	}
	if presence != nil {
		presenceSeen, presenceCount := presence.SeenPresenceCount()
		seen = seen || presenceSeen
		count += presenceCount
	}
	return seen, count
}

func cancelIfNoSessionAfterStartupGrace(ctx context.Context, cancel context.CancelFunc, sessions *projectdaemon.SessionRegistry, grace time.Duration) {
	if grace <= 0 {
		grace = projectdaemon.DefaultHeartbeatTTL
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		seen, _ := sessions.SeenSessionCount()
		if seen {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-timer.C:
			seen, count := sessions.SeenSessionCount()
			if !seen && count == 0 {
				cancel()
			}
			return
		}
	}
}

func cancelIfNoActivityAfterStartupGrace(ctx context.Context, cancel context.CancelFunc, sessions *projectdaemon.SessionRegistry, presence *projectdaemon.AgentPresenceRegistry, grace time.Duration) {
	if grace <= 0 {
		grace = projectdaemon.DefaultHeartbeatTTL
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		seen, _ := projectDaemonSeenActivityCount(sessions, presence)
		if seen {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-timer.C:
			seen, count := projectDaemonSeenActivityCount(sessions, presence)
			if !seen && count == 0 {
				cancel()
			}
			return
		}
	}
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

type lockedWriter struct {
	io.Writer
	mu sync.Mutex
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Writer.Write(p)
}

// CLI > ENV > default(flag)
