package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
)

func attachOrStartRuntimeDaemon(ctx context.Context, cfg leafwikiRuntimeConfig) (*projectdaemon.Descriptor, error) {
	requestCfg, err := daemonWorkspaceRequestConfigForRuntime(cfg)
	if err != nil {
		return nil, err
	}
	descriptorPath := projectdaemon.DescriptorPath(requestCfg.DataDir)
	return attachOrStartFederatedProjectDaemon(ctx, cfg, requestCfg, descriptorPath)
}

func attachOrStartFederatedProjectDaemon(ctx context.Context, cfg leafwikiRuntimeConfig, requestCfg projectdaemon.Config, workspaceDescriptorPath string) (*projectdaemon.Descriptor, error) {
	globalCfg, err := daemonRequestConfigForRuntime(cfg)
	if err != nil {
		return nil, err
	}
	layout := wikid.GlobalLayout(globalCfg.DataDir)
	if cfg.MCPTransports.Stdio {
		if workspace, ok, err := registeredFederatedWorkspaceForAttach(layout, requestCfg); err != nil {
			return nil, err
		} else if ok {
			directRequestCfg := requestCfg
			directRequestCfg.WorkspaceID = workspace.ID
			desc, healthy, err := readHealthyProjectDaemonForAttach(ctx, workspaceDescriptorPath, directRequestCfg)
			if err != nil {
				return nil, err
			}
			if healthy {
				if mismatches := compareProjectDaemonDescriptorForRequest(desc, directRequestCfg, cfg.MCPTransports); len(mismatches) > 0 {
					return nil, projectdaemon.NewConfigMismatchError(mismatches)
				}
				return desc, nil
			}
			if desc != nil {
				_ = projectdaemon.RemoveDescriptor(workspaceDescriptorPath)
			}
		}
	}

	globalDescriptorPath := projectdaemon.GlobalDescriptorPath(layout.RuntimeDir, projectdaemon.RoleWikid)
	globalDesc, healthy, err := readHealthyProjectDaemonForAttach(ctx, globalDescriptorPath, globalCfg)
	if err != nil {
		return nil, err
	}
	if !healthy {
		if globalDesc != nil {
			_ = projectdaemon.RemoveDescriptor(globalDescriptorPath)
		}
		if err := validateAuthStartupConfig(cfg); err != nil {
			logStartupValidationFailure(cfg.Logging, err.Error())
			return nil, err
		}
		if cfg.MCPTransports.Stdio && !cfg.DisableAuth {
			if err := verifyStdioAPIKeyFromStorageForAttach(authStorageDirForRuntime(globalCfg.DataDir), cfg.APIKey); err != nil {
				if errors.Is(err, coreauth.ErrInvalidToken) {
					return nil, fmt.Errorf("invalid native STDIO API key: %w", projectdaemon.ErrInvalidAPIKey)
				}
				return nil, fmt.Errorf("verify native STDIO API key: %w", err)
			}
		}
		errorPath, err := spawnProjectDaemonOwnerForAttach(cfg)
		if err != nil {
			return nil, err
		}
		globalDesc, err = waitForProjectDaemonForAttach(ctx, globalDescriptorPath, errorPath, globalCfg, cfg.MCPTransports)
		if err != nil {
			return nil, err
		}
	} else if mismatches := compareProjectDaemonDescriptorForRequest(globalDesc, globalCfg, cfg.MCPTransports); len(mismatches) > 0 {
		return nil, projectdaemon.NewConfigMismatchError(mismatches)
	}

	workspace, isHome, err := registerFederatedFirstContactForAttach(layout, requestCfg, cfg)
	if err != nil {
		return nil, err
	}
	if isHome {
		return globalDesc, nil
	}
	requestCfg.WorkspaceID = workspace.ID
	requestCfg.MarkdownLinkRootPrefix = workspace.MarkdownLinkRootPrefix
	shouldEnsure := cfg.DisableAuth || cfg.MCPTransports.Stdio
	if shouldEnsure {
		if err := ensureFederatedWorkspaceForAttach(ctx, globalDesc, workspace.ID, cfg); err != nil {
			return nil, err
		}
	}
	if cfg.MCPTransports.Stdio {
		return waitForProjectDaemonForAttach(ctx, workspaceDescriptorPath, "", requestCfg, cfg.MCPTransports)
	}
	return globalDesc, nil
}

func registeredFederatedWorkspaceForRequest(layout wikid.Layout, requestCfg projectdaemon.Config) (wikid.WorkspaceRecord, bool, error) {
	if filepath.Clean(requestCfg.DataDir) == filepath.Clean(layout.HomeDir) &&
		filepath.Clean(requestCfg.RootDir) == filepath.Clean(layout.HomeRootDir) {
		return wikid.WorkspaceRecord{ID: wikid.HomeWorkspaceID, DataDir: layout.HomeDir, RootDir: layout.HomeRootDir}, true, nil
	}
	registry := wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout)
	workspaces, err := registry.ListWorkspaces()
	if err != nil {
		return wikid.WorkspaceRecord{}, false, err
	}
	for _, workspace := range workspaces {
		if filepath.Clean(workspace.DataDir) == filepath.Clean(requestCfg.DataDir) &&
			filepath.Clean(workspace.RootDir) == filepath.Clean(requestCfg.RootDir) {
			return workspace, true, nil
		}
	}
	return wikid.WorkspaceRecord{}, false, nil
}

func registerFederatedFirstContact(layout wikid.Layout, requestCfg projectdaemon.Config, cfg leafwikiRuntimeConfig) (wikid.WorkspaceRecord, bool, error) {
	if requestCfg.DataDir == layout.HomeDir && requestCfg.RootDir == layout.HomeRootDir {
		return wikid.WorkspaceRecord{ID: wikid.HomeWorkspaceID, DataDir: layout.HomeDir, RootDir: layout.HomeRootDir}, true, nil
	}
	registry := wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout)
	registration, err := registry.RegisterWorkspaceWithResultAndGrants(wikid.RegisterWorkspaceRequest{
		DisplayName:            federatedWorkspaceDisplayName(requestCfg),
		DataDir:                requestCfg.DataDir,
		RootDir:                requestCfg.RootDir,
		MarkdownLinkRootPrefix: cfg.MarkdownLinkRootPrefix,
	}, func(registration wikid.RegisterWorkspaceResult) ([]wikid.Grant, error) {
		workspace := registration.Workspace
		var grants []wikid.Grant
		if cfg.DisableAuth {
			grants = append(grants, wikid.Grant{Subject: "user:public-editor", WorkspaceID: workspace.ID, Role: wikid.GrantRoleEditor})
		}
		if cfg.PublicAccess {
			grants = append(grants, wikid.Grant{Subject: "user:public-viewer", WorkspaceID: workspace.ID, Role: wikid.GrantRoleViewer})
		}
		if cfg.MCPTransports.Stdio && !cfg.DisableAuth && registration.Created {
			grant, ok, err := federatedStdioAPIKeyWorkspaceGrant(layout, cfg, workspace.ID)
			if err != nil {
				return nil, err
			}
			if ok {
				grants = append(grants, grant)
			}
		}
		return grants, nil
	})
	if err != nil {
		return wikid.WorkspaceRecord{}, false, err
	}
	return registration.Workspace, false, nil
}

func federatedStdioAPIKeyWorkspaceGrant(layout wikid.Layout, cfg leafwikiRuntimeConfig, workspaceID workspaceid.WorkspaceID) (wikid.Grant, bool, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return wikid.Grant{}, false, nil
	}
	user, err := stdioAPIKeyUserFromStorage(authStorageDirForRuntime(layout.HomeDir), apiKey)
	if err != nil {
		return wikid.Grant{}, false, fmt.Errorf("resolve native STDIO API-key grant user: %w", err)
	}
	userRole := wikidGrantRoleForCoreRole(user.Role)
	role := userRole
	if role == "" {
		return wikid.Grant{}, false, fmt.Errorf("native STDIO API-key user role %q cannot access workspaces: %w", user.Role, errNativeStdioWorkspaceAccessDenied)
	}
	return wikid.Grant{Subject: "user:" + user.ID, WorkspaceID: workspaceID, Role: role}, true, nil
}

func federatedWorkspaceDisplayName(cfg projectdaemon.Config) string {
	if base := strings.TrimSpace(filepath.Base(cfg.RootDir)); base != "" && base != "." && base != string(filepath.Separator) {
		return base
	}
	if base := strings.TrimSpace(filepath.Base(cfg.DataDir)); base != "" && base != "." && base != string(filepath.Separator) {
		return base
	}
	return "Workspace"
}

func ensureFederatedWorkspace(ctx context.Context, desc *projectdaemon.Descriptor, workspaceID workspaceid.WorkspaceID, cfg leafwikiRuntimeConfig) error {
	if desc == nil {
		return errGlobalWikidDescriptorUnavailable
	}
	path := "/__leafwiki/workspaces/" + workspaceID.URLPathSegment() + "/ensure"
	endpoint := strings.TrimRight(desc.ControlURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set(projectdaemon.ControlTokenHeader, desc.ControlToken)
	req.Header.Set("X-LeafWiki-Original-Method", http.MethodPost)
	req.Header.Set("X-LeafWiki-Original-Path", "/mcp")
	if strings.TrimSpace(cfg.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
	}
	resp, err := (&http.Client{Timeout: federatedWorkspaceEnsureTimeout}).Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("ensure workspace %q: %w", workspaceID.String(), newWikidPrivateEndpointError(path, resp.StatusCode, raw))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
