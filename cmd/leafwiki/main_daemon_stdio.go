package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
)

func runDaemonHeartbeat(ctx context.Context, client *projectdaemon.Client, sessionID projectdaemon.SessionID, interval time.Duration) error {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := client.HeartbeatSession(ctx, sessionID); err != nil {
				return err
			}
		}
	}
}

type daemonStdioBridge struct {
	EndpointURL      string
	ControlToken     string
	AuthControlURL   string
	AuthControlToken string
	WorkspaceID      workspaceid.WorkspaceID
	APIKey           string
	ActorContext     string
	Stdin            io.ReadCloser
	Stdout           io.Writer
	Stderr           io.Writer
}

func runDaemonStdioBridge(parent context.Context, cfg daemonStdioBridge) error {
	if cfg.Stdin == nil {
		cfg.Stdin = defaultDaemonStdinForRuntime()
	}
	if cfg.Stdout == nil {
		cfg.Stdout = defaultDaemonStdoutForRuntime()
	}
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
			closeStdin.Do(func() {
				_ = cfg.Stdin.Close()
			})
		case <-ctx.Done():
			closeStdin.Do(func() {
				_ = cfg.Stdin.Close()
			})
		}
	}()
	protocolStdout := &lockedWriter{Writer: cfg.Stdout}
	filteredStdin, filterDone := newNativeStdioJSONFilter(cfg.Stdin, protocolStdout)
	stdioTransport := &sdkmcp.IOTransport{
		Reader: filteredStdin,
		Writer: nopWriteCloser{Writer: protocolStdout},
	}
	httpTransport := &sdkmcp.StreamableClientTransport{
		Endpoint:             cfg.EndpointURL,
		HTTPClient:           daemonStdioBridgeHTTPClient(cfg),
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}
	err := bridgeTransportsForRuntime(ctx, stdioTransport, httpTransport)
	cancel()
	select {
	case filterErr := <-filterDone:
		if filterErr != nil && !isCleanNativeStdioClose(filterErr) {
			return fmt.Errorf("MCP STDIO input failed: %w", filterErr)
		}
	default:
	}
	if !isCleanNativeStdioClose(err) {
		return fmt.Errorf("MCP STDIO failed: %w", err)
	}
	return nil
}

func daemonStdioBridgeHTTPClient(cfg daemonStdioBridge) *http.Client {
	return &http.Client{
		Transport: daemonStdioBridgeHTTPTransport(cfg, &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}),
	}
}

func daemonStdioBridgeHTTPTransport(cfg daemonStdioBridge, base http.RoundTripper) http.RoundTripper {
	actorContext := cfg.ActorContext
	if strings.TrimSpace(cfg.APIKey) != "" &&
		strings.TrimSpace(cfg.AuthControlURL) != "" &&
		strings.TrimSpace(cfg.AuthControlToken) != "" {
		actorContext = ""
	}
	authTransport := projectdaemon.AuthRoundTripper{
		Base:         base,
		ControlToken: cfg.ControlToken,
		BearerToken:  cfg.APIKey,
		ActorContext: actorContext,
	}
	if strings.TrimSpace(cfg.APIKey) == "" ||
		strings.TrimSpace(cfg.AuthControlURL) == "" ||
		strings.TrimSpace(cfg.AuthControlToken) == "" {
		return authTransport
	}
	return stdioActorContextRoundTripper{
		Base:             authTransport,
		AuthControlURL:   cfg.AuthControlURL,
		AuthControlToken: cfg.AuthControlToken,
		WorkspaceID:      cfg.WorkspaceID,
		APIKey:           cfg.APIKey,
	}
}

type stdioActorContextRoundTripper struct {
	Base             http.RoundTripper
	AuthControlURL   string
	AuthControlToken string
	WorkspaceID      workspaceid.WorkspaceID
	APIKey           string
}

func (rt stdioActorContextRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	actorContext, err := rt.actorContext(req)
	if err != nil {
		return nil, err
	}
	next := rt.Base
	if next == nil {
		next = http.DefaultTransport
	}
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Header.Set(projectdaemon.ActorContextHeader, actorContext)
	return next.RoundTrip(clone)
}

func (rt stdioActorContextRoundTripper) actorContext(req *http.Request) (string, error) {
	source := &http.Request{
		Method: http.MethodPost,
		URL:    &url.URL{Path: "/mcp"},
		Header: http.Header{},
	}
	if req != nil {
		source = req.Clone(req.Context())
		source.Header = req.Header.Clone()
	}
	source.Header.Set("Authorization", "Bearer "+strings.TrimSpace(rt.APIKey))
	if rt.WorkspaceID != "" {
		source.Header.Set(projectdaemon.WorkspaceIDHeader, rt.WorkspaceID.HTTPHeaderValue())
	}
	out := struct {
		Actor projectdaemon.ActorContext `json:"actor"`
	}{}
	if err := callWikidPrivateEndpoint(source.Context(), rt.AuthControlURL, rt.AuthControlToken, "/__leafwiki/actor-context", source, &out); err != nil {
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

func bridgeTransports(ctx context.Context, left sdkmcp.Transport, right sdkmcp.Transport) error {
	leftConn, err := left.Connect(ctx)
	if err != nil {
		return err
	}
	defer closeBestEffort(leftConn)
	rightConn, err := right.Connect(ctx)
	if err != nil {
		return err
	}
	defer closeBestEffort(rightConn)

	type pumpResult struct {
		fromLeft  bool
		forwarded int
		err       error
	}
	errs := make(chan pumpResult, 2)
	pump := func(from sdkmcp.Connection, to sdkmcp.Connection, fromLeft bool) {
		forwarded := 0
		for {
			msg, err := from.Read(ctx)
			if err != nil {
				errs <- pumpResult{fromLeft: fromLeft, forwarded: forwarded, err: err}
				return
			}
			if err := to.Write(ctx, msg); err != nil {
				errs <- pumpResult{fromLeft: fromLeft, forwarded: forwarded, err: err}
				return
			}
			forwarded++
		}
	}
	go pump(leftConn, rightConn, true)
	go pump(rightConn, leftConn, false)
	result := <-errs
	err = result.err
	if result.fromLeft && isCleanNativeStdioClose(result.err) {
		if result.forwarded == 0 {
			_ = leftConn.Close()
			_ = rightConn.Close()
			return nil
		}
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case responseResult := <-errs:
			err = responseResult.err
		case <-timer.C:
			err = nil
		case <-ctx.Done():
			err = ctx.Err()
		}
	}
	_ = leftConn.Close()
	_ = rightConn.Close()
	return err
}

func waitForForegroundSession(ctx context.Context, heartbeatErr <-chan error) error {
	signals := make(chan os.Signal, 1)
	notifyRuntimeSignalsForRuntime(signals, os.Interrupt, syscall.SIGTERM)
	defer stopRuntimeSignalsForRuntime(signals)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-heartbeatErr:
		if err == nil || errors.Is(err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("project daemon heartbeat failed: %w", err)
	case <-signals:
		return nil
	}
}
