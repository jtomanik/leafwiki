package frontd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
)

type wikidWorkspaceStatusResponse struct {
	Workspace struct {
		ID string `json:"id"`
	} `json:"workspace"`
	Status struct {
		WorkspaceID string `json:"workspaceId"`
		State       string `json:"state"`
		URL         string `json:"url"`
	} `json:"status"`
}

type wikidWorkspaceListResponse struct {
	Workspaces []struct {
		ID string `json:"id"`
	} `json:"workspaces"`
}

var newFrontdRequestWithContext = http.NewRequestWithContext

func NewWikidSingleWorkspaceResolver(wikidURL string, daemonToken string) (func(*http.Request) (workspaceid.WorkspaceID, error), error) {
	upstream, err := url.Parse(strings.TrimSpace(wikidURL))
	if err != nil || upstream.Scheme == "" || upstream.Host == "" {
		return nil, fmt.Errorf("invalid wikid upstream %q", wikidURL)
	}
	daemonToken = strings.TrimSpace(daemonToken)
	if daemonToken == "" {
		return nil, fmt.Errorf("daemon token is required")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	baseURL := strings.TrimRight(upstream.String(), "/")
	return func(source *http.Request) (workspaceid.WorkspaceID, error) {
		endpoint := baseURL + "/__leafwiki/workspaces"
		ctx := contextForRequest(source)
		req, err := newFrontdRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return "", err
		}
		if source != nil {
			req.Header = source.Header.Clone()
			preserveOriginalRequestHeaders(req, source)
		}
		req.Header.Set(projectdaemon.ControlTokenHeader, daemonToken)
		req.Header.Del(projectdaemon.ActorContextHeader)
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusOK:
		case http.StatusForbidden, http.StatusUnauthorized:
			return "", ErrWorkspaceForbidden
		default:
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			msg := strings.TrimSpace(string(raw))
			if msg == "" {
				msg = resp.Status
			}
			return "", fmt.Errorf("list workspaces failed: %s", msg)
		}
		var out wikidWorkspaceListResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return "", fmt.Errorf("decode workspace list response: %w", err)
		}
		switch len(out.Workspaces) {
		case 0:
			return "", ErrWorkspaceForbidden
		case 1:
			typedWorkspaceID, err := workspaceid.ParseWorkspaceID(out.Workspaces[0].ID)
			if err != nil {
				return "", ErrWorkspaceNotFound
			}
			return typedWorkspaceID, nil
		default:
			return "", ErrWorkspaceAmbiguous
		}
	}, nil
}

func NewWikidWorkspaceResolver(wikidURL string, daemonToken string) (func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error), error) {
	upstream, err := url.Parse(strings.TrimSpace(wikidURL))
	if err != nil || upstream.Scheme == "" || upstream.Host == "" {
		return nil, fmt.Errorf("invalid wikid upstream %q", wikidURL)
	}
	daemonToken = strings.TrimSpace(daemonToken)
	if daemonToken == "" {
		return nil, fmt.Errorf("daemon token is required")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	baseURL := strings.TrimRight(upstream.String(), "/")
	return func(source *http.Request, workspaceID workspaceid.WorkspaceID) (WorkspaceRoute, error) {
		if err := workspaceID.Validate(); err != nil {
			return WorkspaceRoute{}, ErrWorkspaceNotFound
		}
		endpoint := baseURL + "/__leafwiki/workspaces/" + workspaceID.URLPathSegment() + "/ensure"
		ctx := contextForRequest(source)
		req, err := newFrontdRequestWithContext(ctx, http.MethodPost, endpoint, nil)
		if err != nil {
			return WorkspaceRoute{}, err
		}
		if source != nil {
			req.Header = source.Header.Clone()
			preserveOriginalRequestHeaders(req, source)
		}
		req.Header.Set(projectdaemon.ControlTokenHeader, daemonToken)
		req.Header.Del(projectdaemon.ActorContextHeader)
		resp, err := client.Do(req)
		if err != nil {
			return WorkspaceRoute{}, err
		}
		defer resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusOK:
		case http.StatusNotFound:
			return WorkspaceRoute{}, ErrWorkspaceNotFound
		case http.StatusForbidden:
			return WorkspaceRoute{}, ErrWorkspaceForbidden
		default:
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			msg := strings.TrimSpace(string(raw))
			if msg == "" {
				msg = resp.Status
			}
			return WorkspaceRoute{}, fmt.Errorf("ensure workspace %q failed: %s", workspaceID.String(), msg)
		}
		var out wikidWorkspaceStatusResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return WorkspaceRoute{}, fmt.Errorf("decode workspace ensure response: %w", err)
		}
		var routeID workspaceid.WorkspaceID
		if out.Status.WorkspaceID != "" {
			parsed, err := workspaceid.ParseWorkspaceID(out.Status.WorkspaceID)
			if err != nil {
				return WorkspaceRoute{}, fmt.Errorf("decode workspace ensure response ID: %w", err)
			}
			routeID = parsed
		}
		if routeID == "" {
			if out.Workspace.ID != "" {
				parsed, err := workspaceid.ParseWorkspaceID(out.Workspace.ID)
				if err != nil {
					return WorkspaceRoute{}, fmt.Errorf("decode workspace ensure response ID: %w", err)
				}
				routeID = parsed
			}
		}
		if routeID == "" {
			routeID = workspaceID
		}
		if !strings.EqualFold(strings.TrimSpace(out.Status.State), "running") || strings.TrimSpace(out.Status.URL) == "" {
			return WorkspaceRoute{}, fmt.Errorf("workspace %q is not running", workspaceID.String())
		}
		upstreamURL := strings.TrimRight(out.Status.URL, "/")
		return WorkspaceRoute{
			WorkspaceID:   routeID,
			Upstream:      upstreamURL,
			DaemonToken:   daemonToken,
			PrivateMCPURL: upstreamURL + "/mcp",
		}, nil
	}, nil
}

func contextForRequest(req *http.Request) context.Context {
	if req == nil {
		return context.Background()
	}
	return req.Context()
}

func preserveOriginalRequestHeaders(target *http.Request, source *http.Request) {
	if target == nil || source == nil {
		return
	}
	path := ""
	if source.URL != nil {
		path = source.URL.Path
	}
	setOriginalRequestHeaders(target, source.Method, path, source.RemoteAddr)
}

func setOriginalRequestHeaders(target *http.Request, method string, path string, remoteAddr string) {
	if target == nil {
		return
	}
	target.Header.Del("X-LeafWiki-Original-Method")
	target.Header.Del("X-LeafWiki-Original-Path")
	target.Header.Del("X-LeafWiki-Original-Remote-Addr")
	if strings.TrimSpace(method) != "" {
		target.Header.Set("X-LeafWiki-Original-Method", method)
	}
	if strings.TrimSpace(path) != "" {
		target.Header.Set("X-LeafWiki-Original-Path", path)
	}
	if strings.TrimSpace(remoteAddr) != "" {
		target.Header.Set("X-LeafWiki-Original-Remote-Addr", remoteAddr)
	}
}
