package projectdaemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/perber/wiki/internal/agenthooks"
)

type SessionHandle struct {
	ID string `json:"id"`
}

type DaemonHealth struct {
	OK            bool   `json:"ok"`
	SchemaVersion int    `json:"schemaVersion"`
	PID           int    `json:"pid"`
	DataDir       string `json:"dataDir"`
	RootDir       string `json:"rootDir"`
	ConfigHash    string `json:"configHash"`
}

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type ControlHTTPError struct {
	StatusCode int
	Message    string
}

func (e *ControlHTTPError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) == "" {
		return fmt.Sprintf("daemon control request failed: status %d", e.StatusCode)
	}
	return fmt.Sprintf("daemon control request failed: %s", e.Message)
}

func IsControlStatus(err error, status int) bool {
	var controlErr *ControlHTTPError
	return errors.As(err, &controlErr) && controlErr.StatusCode == status
}

func NewClient(controlURL string, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(controlURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Health(ctx)
	return err
}

func (c *Client) Health(ctx context.Context) (*DaemonHealth, error) {
	var out DaemonHealth
	if err := c.doJSON(ctx, http.MethodGet, "/health", nil, &out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("daemon health check failed")
	}
	return &out, nil
}

func (c *Client) RegisterSession(ctx context.Context) (*SessionHandle, error) {
	var out SessionHandle
	if err := c.doJSON(ctx, http.MethodPost, "/sessions", map[string]string{}, &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.ID) == "" {
		return nil, fmt.Errorf("daemon returned empty session id")
	}
	return &out, nil
}

func (c *Client) HeartbeatSession(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodPost, "/sessions/"+id+"/heartbeat", map[string]string{}, nil)
}

func (c *Client) ReleaseSession(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/sessions/"+id, nil, nil)
}

func (c *Client) VerifyStdioAuth(ctx context.Context, apiKey string) error {
	return c.doJSON(ctx, http.MethodPost, "/stdio-auth/verify", map[string]string{"apiKey": apiKey}, nil)
}

func (c *Client) RecordAgentPresence(ctx context.Context, event agenthooks.Event) error {
	return c.doJSON(ctx, http.MethodPost, "/agent-presence/events", event, nil)
}

func (c *Client) ListAgentPresence(ctx context.Context) ([]AgentPresenceSession, error) {
	var out []AgentPresenceSession
	if err := c.doJSON(ctx, http.MethodGet, "/agent-presence", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, in any, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set(ControlTokenHeader, c.token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = resp.Status
		}
		return &ControlHTTPError{StatusCode: resp.StatusCode, Message: msg}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return err
	}
	return nil
}

type AuthRoundTripper struct {
	Base         http.RoundTripper
	ControlToken string
	BearerToken  string
	ActorContext string
}

func (rt AuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	next := rt.Base
	if next == nil {
		next = http.DefaultTransport
	}
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Header.Set(ControlTokenHeader, rt.ControlToken)
	if strings.TrimSpace(rt.BearerToken) != "" {
		clone.Header.Set("Authorization", "Bearer "+strings.TrimSpace(rt.BearerToken))
	}
	if strings.TrimSpace(rt.ActorContext) != "" {
		clone.Header.Set(ActorContextHeader, strings.TrimSpace(rt.ActorContext))
	}
	return next.RoundTrip(clone)
}
