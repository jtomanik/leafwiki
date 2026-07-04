package main

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/locking"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wikid"
)

func isLegacyProjectDaemonLockStartupMessage(msg string) bool {
	normalized := strings.ToLower(strings.TrimSpace(msg))
	return strings.Contains(normalized, "acquire data directory lock:") ||
		strings.Contains(normalized, "acquire root directory lock:") ||
		strings.Contains(normalized, "data directory is already in use") ||
		strings.Contains(normalized, "root directory is already in use")
}

func isTrustedDaemonControlURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "http" {
		return false
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func projectDaemonLocksHeld(dataDir string, rootDir string) (bool, error) {
	dataLock, err := acquireDataDirLockForRuntime(dataDir)
	if err == nil {
		_ = dataLock.Release()
		return false, nil
	}
	if !locking.IsDataDirLockHeld(err) {
		return false, err
	}
	rootLock, err := acquireRootDirLockForRuntime(rootDir)
	if err == nil {
		_ = rootLock.Release()
		return false, nil
	}
	if !locking.IsRootDirLockHeld(err) {
		return false, err
	}
	return true, nil
}

func projectDaemonLocksFree(dataDir string, rootDir string) (bool, error) {
	dataLock, err := acquireDataDirLockForRuntime(dataDir)
	if err != nil {
		if locking.IsDataDirLockHeld(err) {
			return false, nil
		}
		return false, err
	}
	defer releaseRuntimeLockBestEffort(dataLock)

	rootLock, err := acquireRootDirLockForRuntime(rootDir)
	if err != nil {
		if locking.IsRootDirLockHeld(err) {
			return false, nil
		}
		return false, err
	}
	if err := rootLock.Release(); err != nil {
		return false, err
	}
	return true, nil
}

func projectDaemonDataLockFreeRootLockHeld(dataDir string, rootDir string) (bool, error) {
	dataLock, err := acquireDataDirLockForRuntime(dataDir)
	if err != nil {
		if locking.IsDataDirLockHeld(err) {
			return false, nil
		}
		return false, err
	}
	if err := dataLock.Release(); err != nil {
		return false, err
	}

	rootLock, err := acquireRootDirLockForRuntime(rootDir)
	if err == nil {
		_ = rootLock.Release()
		return false, nil
	}
	if locking.IsRootDirLockHeld(err) {
		return true, nil
	}
	return false, err
}

func daemonHealthMatchesDescriptor(desc *projectdaemon.Descriptor, health *projectdaemon.DaemonHealth) bool {
	if desc == nil || health == nil {
		return false
	}
	return health.SchemaVersion == desc.SchemaVersion &&
		health.PID == desc.PID &&
		health.DataDir == desc.DataDir &&
		health.RootDir == desc.RootDir &&
		health.ConfigHash == desc.ConfigHash
}

func projectDaemonDescriptorRole(runtimeStack string) projectdaemon.RoleName {
	if runtimeStack == projectdaemon.RuntimeStackWikidFrontd {
		return projectdaemon.RoleWikid
	}
	return ""
}

func projectDaemonDescriptorRoles(runtimeStack string, pid int, publicAddr string, roleShells *wikidFrontdRuntime) []projectdaemon.RoleHealth {
	if runtimeStack != projectdaemon.RuntimeStackWikidFrontd {
		return nil
	}
	if roleShells != nil && len(roleShells.roles) > 0 {
		return append([]projectdaemon.RoleHealth(nil), roleShells.roles...)
	}
	now := time.Now().UTC()
	return []projectdaemon.RoleHealth{
		{Name: projectdaemon.RoleWikid, State: projectdaemon.RoleStateReady, PID: pid, UpdatedAt: now},
		{Name: projectdaemon.RoleFrontd, State: projectdaemon.RoleStateReady, PID: pid, URL: "http://" + publicAddr, UpdatedAt: now},
		{Name: projectdaemon.RoleWorkspaced, State: projectdaemon.RoleStateReady, PID: pid, Private: true, UpdatedAt: now},
	}
}

func compareProjectDaemonConfigForRequest(owner projectdaemon.Config, requested projectdaemon.Config, requestTransports mcpTransports) []projectdaemon.Mismatch {
	normalized := requested
	if requestTransports.Stdio && !requestTransports.HTTP {
		normalized.PublicMCPEnabled = owner.PublicMCPEnabled
		normalized.Host = owner.Host
		normalized.Port = owner.Port
		normalized.LogTarget = owner.LogTarget
		normalized.LogFile = owner.LogFile
		normalized.DisableRequestLog = owner.DisableRequestLog
	}
	return projectdaemon.CompareConfig(owner, normalized)
}

func compareProjectDaemonDescriptorForRequest(desc *projectdaemon.Descriptor, requested projectdaemon.Config, requestTransports mcpTransports) []projectdaemon.Mismatch {
	if desc == nil {
		return nil
	}
	normalized := requested
	if desc.Role == projectdaemon.RoleWorkspaced && requestTransports.Stdio && strings.TrimSpace(desc.PrivateMCPURL) != "" {
		normalized.Host = desc.Config.Host
		normalized.Port = desc.Config.Port
		normalized.PublicMCPEnabled = desc.Config.PublicMCPEnabled
		normalized.LogTarget = desc.Config.LogTarget
		normalized.LogFile = desc.Config.LogFile
		normalized.DisableRequestLog = desc.Config.DisableRequestLog
	}
	mismatches := compareProjectDaemonConfigForRequest(desc.Config, normalized, requestTransports)
	requestedWorkspaceID := normalized.WorkspaceID
	descriptorWorkspaceID := desc.WorkspaceID
	if requestedWorkspaceID != "" && descriptorWorkspaceID != "" && requestedWorkspaceID != descriptorWorkspaceID && !hasProjectDaemonMismatch(mismatches, "workspace-id") {
		mismatches = append(mismatches, projectdaemon.Mismatch{
			Field: "workspace-id",
			Want:  fmt.Sprint(descriptorWorkspaceID),
			Got:   fmt.Sprint(requestedWorkspaceID),
		})
	}
	return mismatches
}

func hasProjectDaemonMismatch(mismatches []projectdaemon.Mismatch, field string) bool {
	for _, mismatch := range mismatches {
		if mismatch.Field == field {
			return true
		}
	}
	return false
}

func authStorageDirForRuntime(dataDir string) string {
	return wikid.AuthStoragePaths(dataDir).AuthDir
}

func verifyStdioAPIKeyFromStorage(dataDir string, apiKey string) error {
	_, err := stdioAPIKeyUserFromStorage(dataDir, apiKey)
	return err
}

func stdioAPIKeyUserFromStorage(dataDir string, apiKey string) (*coreauth.User, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	userStore, err := coreauth.NewUserStore(dataDir)
	if err != nil {
		return nil, err
	}
	defer closeBestEffort(userStore)
	userService := coreauth.NewUserService(userStore)
	apiKeyStore, err := coreauth.NewAPIKeyStore(dataDir)
	if err != nil {
		return nil, err
	}
	apiKeyService := coreauth.NewAPIKeyService(apiKeyStore, userService)
	defer closeBestEffort(apiKeyService)
	verified, err := apiKeyService.VerifyAPIKey(apiKey)
	if err != nil {
		return nil, err
	}
	return verified.User, nil
}

func wikidControlMCPActorResolver(authDir string, cfg leafwikiRuntimeConfig) func(*http.Request) (projectdaemon.ActorContext, error) {
	return func(req *http.Request) (projectdaemon.ActorContext, error) {
		if cfg.DisableAuth {
			return actorContextForUser(&coreauth.User{ID: "public-editor", Username: "public-editor", Role: coreauth.RoleEditor}, "disabled", cfg)
		}
		token := httpBearerToken(req)
		if token == "" || !coreauth.IsAPIKeyBearer(token) {
			return projectdaemon.ActorContext{}, errNativeStdioAPIKeyRequired
		}
		user, err := stdioAPIKeyUserFromStorage(authDir, token)
		if err != nil {
			return projectdaemon.ActorContext{}, err
		}
		return actorContextForUser(user, "api_key", cfg)
	}
}
