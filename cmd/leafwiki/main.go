package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/perber/wiki/internal/agenthooks"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/frontd"
	"github.com/perber/wiki/internal/localization"
	"github.com/perber/wiki/internal/locking"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/runtimeconfig"
	"github.com/perber/wiki/internal/wikid"
)

const (
	runtimeErrorCodeWorkspaceGrantDenied          sharederrors.ErrorCode = "workspace_grant_denied"
	runtimeErrorCodeMCPWorkspaceUnavailable       sharederrors.ErrorCode = "mcp_workspace_unavailable"
	runtimeErrorCodePrivateMCPControlTokenInvalid sharederrors.ErrorCode = "private_mcp_control_token_invalid"
	errCodeStdioAuthAPIKeyInvalid                 sharederrors.ErrorCode = "stdio_auth_api_key_invalid"
	errCodeMCPActorContextInvalid                 sharederrors.ErrorCode = "mcp_actor_context_invalid"
)

type leafwikiUsageToken string
type leafwikiUsageMessageID string

const (
	leafwikiUsageTokenJWTSecret          leafwikiUsageToken     = "--jwt-secret"
	leafwikiUsageTokenAdminPassword      leafwikiUsageToken     = "--admin-password"
	leafwikiUsageTokenAllowInsecure      leafwikiUsageToken     = "--allow-insecure"
	leafwikiUsageTokenDataDir            leafwikiUsageToken     = "--data-dir"
	leafwikiUsageTokenRootDir            leafwikiUsageToken     = "--root-dir"
	leafwikiUsageTokenLogTarget          leafwikiUsageToken     = "--log-target"
	leafwikiUsageTokenLogFile            leafwikiUsageToken     = "--log-file"
	leafwikiUsageTokenMCP                leafwikiUsageToken     = "--mcp"
	leafwikiUsageTokenAPIKey             leafwikiUsageToken     = "--api-key"
	leafwikiUsageTokenDaemonIdleTimeout  leafwikiUsageToken     = "--daemon-idle-timeout"
	leafwikiUsageTokenConfig             leafwikiUsageToken     = "--config"
	leafwikiUsageTokenAgentHookCommand   leafwikiUsageToken     = "agent-hook"
	leafwikiUsageTokenDaemonCommand      leafwikiUsageToken     = "daemon"
	leafwikiUsageTokenRootDirEnv         leafwikiUsageToken     = "LEAFWIKI_ROOT_DIR"
	leafwikiUsageTokenLogTargetEnv       leafwikiUsageToken     = "LEAFWIKI_LOG_TARGET"
	leafwikiUsageTokenLogFileEnv         leafwikiUsageToken     = "LEAFWIKI_LOG_FILE"
	leafwikiUsageTokenMCPEnv             leafwikiUsageToken     = "LEAFWIKI_MCP"
	leafwikiUsageTokenMCPAPIKeyEnv       leafwikiUsageToken     = "LEAFWIKI_MCP_API_KEY"
	leafwikiUsageTokenEnableRevision     leafwikiUsageToken     = "--enable-revision"
	leafwikiUsageTokenWorkspaceSync      leafwikiUsageToken     = "--enable-workspace-sync"
	leafwikiUsageTokenRevisionHistory    leafwikiUsageToken     = "--max-revision-history"
	leafwikiUsageTokenEnableMCP          leafwikiUsageToken     = "--enable-mcp"
	leafwikiUsageTokenMCPStdio           leafwikiUsageToken     = "--mcp-stdio"
	leafwikiUsageTokenEnableRevisionEnv  leafwikiUsageToken     = "LEAFWIKI_ENABLE_REVISION"
	leafwikiUsageTokenWorkspaceSyncEnv   leafwikiUsageToken     = "LEAFWIKI_ENABLE_WORKSPACE_SYNC"
	leafwikiUsageTokenRevisionHistoryEnv leafwikiUsageToken     = "LEAFWIKI_MAX_REVISION_HISTORY"
	leafwikiUsageTokenRuntimeStackEnv    leafwikiUsageToken     = "LEAFWIKI_RUNTIME_STACK"
	leafwikiUsageTokenEnableMCPEnv       leafwikiUsageToken     = "LEAFWIKI_ENABLE_MCP"
	leafwikiUsageTokenMCPStdioEnv        leafwikiUsageToken     = "LEAFWIKI_MCP_STDIO"
	leafwikiUsageMessageCLIHelpUsage     leafwikiUsageMessageID = localization.MessageIDCLIHelpUsage
	leafwikiUsageMessageCLIHelpBody      leafwikiUsageMessageID = localization.MessageIDCLIHelpBody
)

type leafwikiUsageContract struct {
	MessageIDs    []leafwikiUsageMessageID
	Supported     []leafwikiUsageToken
	RemovedLegacy []leafwikiUsageToken
}

func leafwikiUsage() leafwikiUsageContract {
	return leafwikiUsageContract{
		MessageIDs: []leafwikiUsageMessageID{
			leafwikiUsageMessageCLIHelpUsage,
			leafwikiUsageMessageCLIHelpBody,
		},
		Supported: []leafwikiUsageToken{
			leafwikiUsageTokenJWTSecret,
			leafwikiUsageTokenAdminPassword,
			leafwikiUsageTokenAllowInsecure,
			leafwikiUsageTokenDataDir,
			leafwikiUsageTokenRootDir,
			leafwikiUsageTokenLogTarget,
			leafwikiUsageTokenLogFile,
			leafwikiUsageTokenMCP,
			leafwikiUsageTokenAPIKey,
			leafwikiUsageTokenDaemonIdleTimeout,
			leafwikiUsageTokenConfig,
			leafwikiUsageTokenAgentHookCommand,
			leafwikiUsageTokenDaemonCommand,
			leafwikiUsageTokenRootDirEnv,
			leafwikiUsageTokenLogTargetEnv,
			leafwikiUsageTokenLogFileEnv,
			leafwikiUsageTokenMCPEnv,
			leafwikiUsageTokenMCPAPIKeyEnv,
		},
		RemovedLegacy: []leafwikiUsageToken{
			leafwikiUsageTokenEnableRevision,
			leafwikiUsageTokenWorkspaceSync,
			leafwikiUsageTokenRevisionHistory,
			leafwikiUsageTokenEnableMCP,
			leafwikiUsageTokenMCPStdio,
			leafwikiUsageTokenEnableRevisionEnv,
			leafwikiUsageTokenWorkspaceSyncEnv,
			leafwikiUsageTokenRevisionHistoryEnv,
			leafwikiUsageTokenRuntimeStackEnv,
			leafwikiUsageTokenEnableMCPEnv,
			leafwikiUsageTokenMCPStdioEnv,
		},
	}
}

func writeUsage(w io.Writer) {
	for _, messageID := range leafwikiUsage().MessageIDs {
		rendered := localization.English.Render(string(messageID), "").Message
		if _, err := fmt.Fprintln(w, rendered); err != nil {
			panic(err)
		}
	}
}

func printUsage() {
	writeUsage(os.Stdout)
}

func setupBootstrapLogger(stderr io.Writer) {
	handler := slog.NewJSONHandler(stderr, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	})

	slog.SetDefault(slog.New(handler))
}

func setupLogger(cfg leaflogging.Config, stdout io.Writer, stderr io.Writer) (io.Closer, error) {
	logger, closer, err := leaflogging.Open(cfg, leaflogging.Streams{
		Stdout: stdout,
		Stderr: stderr,
	})
	if err != nil {
		return nil, err
	}
	slog.SetDefault(logger)
	return closer, nil
}

var (
	failOpenAgentHookProvider agenthooks.ProviderID
	leafwikiExit              = os.Exit
)

type configFlagMixError = runtimeconfig.ConfigFlagMixError
type configUsageError = runtimeconfig.ConfigUsageError
type daemonServiceConfigMissingError = runtimeconfig.DaemonServiceConfigMissingError

func fail(msg string, args ...any) {
	if failOpenAgentHookProvider != "" {
		provider := failOpenAgentHookProvider
		slog.Default().Warn("Agent hook failed open", "provider", provider, "reason", msg)
		if allowResponse := agenthooks.AllowProviderResponse(provider); len(allowResponse) > 0 {
			_, _ = os.Stdout.Write(allowResponse)
		}
		leafwikiExit(0)
	}
	slog.Default().Error(msg, args...)
	fmt.Fprintln(os.Stderr, failureMessage(msg, args...))
	leafwikiExit(1)
}

func failInvalidConfigFile(err error) {
	var mixErr configFlagMixError
	var usageErr configUsageError
	if errors.As(err, &mixErr) || errors.As(err, &usageErr) {
		failWithoutAgentHook(localization.MessageIDCLIErrorInvalidConfigFile, "error", err)
	}
	fail(localization.MessageIDCLIErrorInvalidConfigFile, "error", err)
}

func failWithoutAgentHook(msg string, args ...any) {
	slog.Default().Error(msg, args...)
	fmt.Fprintln(os.Stderr, failureMessage(msg, args...))
	leafwikiExit(1)
}

func failureMessage(msg string, args ...any) string {
	var b strings.Builder
	b.WriteString(localization.English.Render(msg, msg).Message)
	for i := 0; i+1 < len(args); i += 2 {
		b.WriteByte(' ')
		b.WriteString(fmt.Sprint(args[i]))
		b.WriteByte('=')
		b.WriteString(fmt.Sprint(args[i+1]))
	}
	return b.String()
}

type leafwikiTempFile interface {
	Name() string
	Chmod(os.FileMode) error
	Write([]byte) (int, error)
	Close() error
}

type leafwikiRuntimeLock interface {
	Release() error
}

func closeBestEffort(closer io.Closer) {
	if closer == nil {
		return
	}
	_ = closer.Close()
}

func releaseRuntimeLockBestEffort(lock leafwikiRuntimeLock) {
	if lock == nil {
		return
	}
	_ = lock.Release()
}

func removePathBestEffort(path string) {
	_ = os.Remove(path)
}

func removeProjectDaemonDescriptorBestEffort(path string) {
	_ = projectdaemon.RemoveDescriptor(path)
}

func defaultDaemonStdin() io.ReadCloser {
	return os.Stdin
}

func defaultDaemonStdout() io.Writer {
	return os.Stdout
}

func notifyRuntimeSignals(c chan<- os.Signal, sig ...os.Signal) {
	signal.Notify(c, sig...)
}

func stopRuntimeSignals(c chan<- os.Signal) {
	signal.Stop(c)
}

func shutdownHTTPServer(server *http.Server, ctx context.Context) error {
	return server.Shutdown(ctx)
}

var projectDaemonExecutable = os.Executable

var projectDaemonStartupConfigPostStartCleanupDelay = 30 * time.Second

var startInternalRuntimeRoleProcessForRuntime = startInternalRuntimeRoleProcess

var (
	createTempFileForRuntime                      = func(dir string, pattern string) (leafwikiTempFile, error) { return os.CreateTemp(dir, pattern) }
	runDaemonServiceForDispatch                   = runDaemonService
	runAgentHookCommandForDispatch                = runAgentHookCommand
	runProjectDaemonLauncherForDispatch           = runProjectDaemonLauncher
	attachOrStartRuntimeDaemonForLaunch           = attachOrStartRuntimeDaemon
	runDaemonHeartbeatForLaunch                   = runDaemonHeartbeat
	runDaemonStdioBridgeForLaunch                 = runDaemonStdioBridge
	daemonStdioActorContextForLaunch              = daemonStdioActorContext
	encodeActorContextForRuntime                  = projectdaemon.EncodeActorContext
	attachOrStartRuntimeDaemonForAgentHook        = attachOrStartRuntimeDaemon
	filepathAbsForRuntime                         = filepath.Abs
	filepathRelForRuntime                         = filepath.Rel
	userHomeDirForRuntime                         = os.UserHomeDir
	openDaemonNullDeviceForRuntime                = func() (*os.File, error) { return os.OpenFile(os.DevNull, os.O_RDWR, 0) }
	jsonMarshalForRuntime                         = json.Marshal
	resolveLoggingForRuntime                      = leaflogging.Resolve
	acquireDataDirLockForRuntime                  = func(path string) (leafwikiRuntimeLock, error) { return locking.AcquireDataDirLock(path) }
	acquireRootDirLockForRuntime                  = func(path string) (leafwikiRuntimeLock, error) { return locking.AcquireRootDirLock(path) }
	statPathForRuntime                            = os.Stat
	mkdirAllForRuntime                            = os.MkdirAll
	processFindProcessForRuntime                  = os.FindProcess
	startCommandForRuntime                        = func(cmd *exec.Cmd) error { return cmd.Start() }
	releaseProcessForRuntime                      = func(process *os.Process) error { return process.Release() }
	bridgeTransportsForRuntime                    = bridgeTransports
	newControlPlaneProxyForRuntime                = frontd.NewControlPlaneProxy
	newWorkspaceProxyForRuntime                   = frontd.NewWorkspaceProxy
	newWorkspacesAPIForRuntime                    = frontd.NewWorkspacesAPI
	newWikidWorkspaceResolverForRuntime           = frontd.NewWikidWorkspaceResolver
	frontdPublicMCPHandlerForRuntime              = frontdPublicMCPHandler
	newWikidSingleWorkspaceResolverForRuntime     = frontd.NewWikidSingleWorkspaceResolver
	defaultDaemonStdinForRuntime                  = defaultDaemonStdin
	defaultDaemonStdoutForRuntime                 = defaultDaemonStdout
	notifyRuntimeSignalsForRuntime                = notifyRuntimeSignals
	stopRuntimeSignalsForRuntime                  = stopRuntimeSignals
	shutdownInternalRuntimeHTTPServerForRuntime   = shutdownHTTPServer
	shutdownWikidControlServerForRuntime          = shutdownHTTPServer
	internalRuntimeRoleReadinessTimeoutForProcess = internalRuntimeRoleReadinessTimeout
	runWikidFrontdOwnerForProjectDaemon           = runWikidFrontdOwner
	netListenForRuntime                           = net.Listen
	randomTokenForRuntime                         = projectdaemon.RandomToken
	configHashForRuntime                          = projectdaemon.ConfigHash
	newRuntimeWikiForRuntime                      = newRuntimeWiki
	controlPlaneRouterOptionsForOwner             = controlPlaneRouterOptionsForRuntime
	startWikidFrontdRuntimeForOwner               = startWikidFrontdRuntime
	newMCPProxyWithActorForRuntime                = frontd.NewMCPProxyWithActor
	writeDescriptorAtomicForRuntime               = projectdaemon.WriteDescriptorAtomic
	registeredFederatedWorkspaceForAttach         = registeredFederatedWorkspaceForRequest
	seedRuntimeHomeGrantsForOwner                 = seedRuntimeHomeGrants
	ensureRuntimeHomeGrantForOwner                = ensureRuntimeHomeGrant
	newFederatedWorkspaceManagerForOwner          = newFederatedWorkspaceManager
	verifyFrontdAPIKeyForRuntime                  = verifyFrontdAPIKey
	verifyFrontdOAuthBearerTokenForRuntime        = verifyFrontdOAuthBearerToken
	getFrontdUserByIDForRuntime                   = getFrontdUserByID
	serveWikidControlServerForOwner               = serveWikidControlServer
	grantsForSubjectForRuntime                    = func(store *wikid.GrantStore, subject string) ([]wikid.Grant, error) {
		return store.GrantsForSubject(subject)
	}
	actorContextForWorkspaceGrantForRuntime = actorContextForWorkspaceGrant
	readHealthyProjectDaemonForAttach       = readHealthyProjectDaemon
	verifyStdioAPIKeyFromStorageForAttach   = verifyStdioAPIKeyFromStorage
	spawnProjectDaemonOwnerForAttach        = spawnProjectDaemonOwner
	waitForProjectDaemonForAttach           = waitForProjectDaemon
	registerFederatedFirstContactForAttach  = registerFederatedFirstContact
	ensureFederatedWorkspaceForAttach       = ensureFederatedWorkspace
)
