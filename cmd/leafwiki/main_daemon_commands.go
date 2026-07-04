package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/perber/wiki/internal/agenthooks"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
)

func runProjectDaemonLauncher(parent context.Context, cfg leafwikiRuntimeConfig) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	var closeStdin sync.Once
	signals := make(chan os.Signal, 1)
	notifyRuntimeSignalsForRuntime(signals, os.Interrupt, syscall.SIGTERM)
	defer stopRuntimeSignalsForRuntime(signals)
	go func() {
		select {
		case <-signals:
			cancel()
			if cfg.MCPTransports.Stdio {
				closeStdin.Do(func() {
					_ = os.Stdin.Close()
				})
			}
		case <-ctx.Done():
		}
	}()
	desc, err := attachOrStartRuntimeDaemonForLaunch(ctx, cfg)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil
		}
		return err
	}
	client := projectdaemon.NewClient(desc.ControlURL, desc.ControlToken)
	if cfg.MCPTransports.Stdio && !cfg.DisableAuth {
		if err := client.VerifyStdioAuth(ctx, cfg.APIKey); err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			if !projectdaemon.IsControlStatus(err, http.StatusUnauthorized) {
				return fmt.Errorf("verify native STDIO API key: %w", err)
			}
			return fmt.Errorf("invalid native STDIO API key: %w", projectdaemon.ErrInvalidAPIKey)
		}
	}
	handle, err := client.RegisterSession(ctx)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil
		}
		return fmt.Errorf("register project daemon session: %w", err)
	}
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	defer stopHeartbeat()
	heartbeatErr := make(chan error, 1)
	go func() {
		heartbeatErr <- runDaemonHeartbeatForLaunch(heartbeatCtx, client, handle.ID, 2*time.Second)
	}()
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = client.ReleaseSession(releaseCtx, handle.ID)
	}()

	if cfg.MCPTransports.Stdio {
		bridgeCtx, stopBridge := context.WithCancel(ctx)
		defer stopBridge()
		bridgeCfg := daemonStdioBridgeConfig(desc, cfg)
		if strings.TrimSpace(desc.PrivateMCPURL) != "" {
			actorContext, err := daemonStdioActorContextForLaunch(ctx, desc, cfg)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
					return nil
				}
				return err
			}
			bridgeCfg.ActorContext = actorContext
		}
		bridgeErr := make(chan error, 1)
		go func() {
			bridgeErr <- runDaemonStdioBridgeForLaunch(bridgeCtx, bridgeCfg)
		}()
		select {
		case err := <-bridgeErr:
			return err
		case err := <-heartbeatErr:
			stopBridge()
			if err == nil || errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("project daemon heartbeat failed: %w", err)
		case <-ctx.Done():
			stopBridge()
			return nil
		}
	}
	if err := waitForForegroundSession(ctx, heartbeatErr); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func runDaemonService(parent context.Context, cfg leafwikiRuntimeConfig) error {
	cfg.RuntimeStack = projectdaemon.RuntimeStackWikidFrontd
	cfg.DisableIdleShutdown = true
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
	return runProjectDaemonOwner(ctx, cfg)
}

func runAgentHookCommand(parent context.Context, cfg leafwikiRuntimeConfig, provider agenthooks.ProviderID, stdin io.Reader, stdout io.Writer) (err error) {
	allowResponse := agenthooks.AllowProviderResponse(provider)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("agent hook panic: %v", recovered)
		}
		if len(allowResponse) > 0 {
			if _, writeErr := stdout.Write(allowResponse); writeErr != nil && err == nil {
				err = fmt.Errorf("write hook allow response: %w", writeErr)
			}
		}
	}()

	raw, err := io.ReadAll(io.LimitReader(stdin, agentHookMaxPayloadBytes+1))
	if err != nil {
		return fmt.Errorf("read hook payload: %w", err)
	}
	if len(raw) > agentHookMaxPayloadBytes {
		return fmt.Errorf("hook payload exceeds %d bytes", agentHookMaxPayloadBytes)
	}
	event, ok := agenthooks.Normalize(provider, raw, time.Now().UTC())
	if !ok {
		return nil
	}

	cfg.DetachDaemonOwnerIO = true
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	desc, err := attachOrStartRuntimeDaemonForAgentHook(ctx, cfg)
	if err != nil {
		return err
	}
	client := projectdaemon.NewClient(desc.ControlURL, desc.ControlToken)
	if err := client.RecordAgentPresence(ctx, event); err != nil {
		return fmt.Errorf("record agent presence: %w", err)
	}
	return nil
}

func daemonStdioBridgeConfig(desc *projectdaemon.Descriptor, cfg leafwikiRuntimeConfig) daemonStdioBridge {
	endpointURL := strings.TrimRight(desc.ControlURL, "/") + "/mcp"
	controlToken := desc.ControlToken
	if strings.TrimSpace(desc.PrivateMCPURL) != "" && strings.TrimSpace(desc.PrivateMCPToken) != "" {
		endpointURL = strings.TrimSpace(desc.PrivateMCPURL)
		controlToken = strings.TrimSpace(desc.PrivateMCPToken)
	}
	return daemonStdioBridge{
		EndpointURL:      endpointURL,
		ControlToken:     controlToken,
		AuthControlURL:   strings.TrimSpace(desc.ControlURL),
		AuthControlToken: strings.TrimSpace(desc.ControlToken),
		WorkspaceID:      desc.WorkspaceID,
		APIKey:           cfg.APIKey,
		Stdin:            os.Stdin,
		Stdout:           os.Stdout,
		Stderr:           os.Stderr,
	}
}

func daemonStdioActorContext(ctx context.Context, desc *projectdaemon.Descriptor, cfg leafwikiRuntimeConfig) (string, error) {
	source := &http.Request{
		Method: http.MethodPost,
		URL:    &url.URL{Path: "/mcp"},
		Header: http.Header{},
	}
	if strings.TrimSpace(cfg.APIKey) != "" {
		source.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
	}
	if desc.WorkspaceID != "" {
		source.Header.Set(projectdaemon.WorkspaceIDHeader, desc.WorkspaceID.HTTPHeaderValue())
	}
	out := struct {
		Actor projectdaemon.ActorContext `json:"actor"`
	}{}
	if err := callWikidPrivateEndpoint(ctx, desc.ControlURL, desc.ControlToken, "/__leafwiki/actor-context", source, &out); err != nil {
		if isWikidPrivateAuthFailure(err) {
			return "", fmt.Errorf("unauthorized native STDIO API key: %w", err)
		}
		return "", fmt.Errorf("resolve native STDIO actor context: %w", err)
	}
	encoded, err := encodeActorContextForRuntime(out.Actor)
	if err != nil {
		return "", fmt.Errorf("encode native STDIO actor context: %w", err)
	}
	return encoded, nil
}

func validateAuthStartupConfig(cfg leafwikiRuntimeConfig) error {
	if cfg.DisableAuth {
		return nil
	}
	if cfg.JWTSecret == "" {
		return errAuthJWTSecretRequired
	}
	if cfg.AdminPassword == "" {
		return errAuthAdminPasswordRequired
	}
	return nil
}

func logStartupValidationFailure(cfg leaflogging.Config, msg string) {
	if cfg.Target != leaflogging.TargetFile {
		return
	}
	logger, closer, err := leaflogging.Open(cfg, leaflogging.Streams{Stdout: os.Stdout, Stderr: os.Stderr})
	if err != nil {
		return
	}
	defer closeBestEffort(closer)
	logger.Error(msg)
}
