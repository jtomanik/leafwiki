package main

import (
	"path/filepath"
	"strings"

	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
)

func daemonConfigForRuntime(cfg leafwikiRuntimeConfig) (projectdaemon.Config, error) {
	dataDir, rootDir, err := projectdaemon.CanonicalizeProject(cfg.Workspace.DataDir, cfg.Workspace.RootDir)
	if err != nil {
		return projectdaemon.Config{}, err
	}
	logFile := daemonLogFileForConfig(cfg, dataDir)
	return projectdaemon.Config{
		RuntimeStack:            cfg.RuntimeStack,
		WorkspaceID:             runtimeWorkspaceSemanticID(cfg.Workspace),
		DataDir:                 dataDir,
		RootDir:                 rootDir,
		AuthDisabled:            cfg.DisableAuth,
		PublicMCPEnabled:        cfg.MCPTransports.HTTP,
		Host:                    cfg.Host,
		Port:                    cfg.Port,
		BasePath:                cfg.BasePath,
		MarkdownLinkRootPrefix:  cfg.MarkdownLinkRootPrefix,
		PublicAccess:            cfg.PublicAccess,
		AllowInsecure:           cfg.AllowInsecure,
		AccessTokenTimeout:      cfg.AccessTokenTimeout.String(),
		RefreshTokenTimeout:     cfg.RefreshTokenTimeout.String(),
		InjectCodeInHeaderHash:  projectdaemon.HashSecret(cfg.InjectCodeInHeader),
		CustomStylesheet:        cfg.CustomStylesheet,
		LogTarget:               string(cfg.Logging.Target),
		LogFile:                 logFile,
		HideLinkMetadataSection: cfg.HideLinkMetadataSection,
		MaxAssetUploadSizeBytes: cfg.MaxAssetUploadSize,
		EnableWorkspaceSync:     true,
		EnableLinkRefactor:      cfg.EnableLinkRefactor,
		EnableHTTPRemoteUser:    cfg.EnableHTTPRemoteUser,
		HTTPRemoteUserHeader:    cfg.HTTPRemoteUserHeader,
		TrustedProxyIPs:         cfg.TrustedProxyIPsRaw,
		HTTPRemoteUserLogoutURL: cfg.HTTPRemoteUserLogoutURL,
		DisableRequestLog:       cfg.DisableRequestLog,
		DaemonIdleTimeout:       cfg.DaemonIdleTimeout.String(),
	}, nil
}

func daemonRequestConfigForRuntime(cfg leafwikiRuntimeConfig) (projectdaemon.Config, error) {
	ownerCfg, err := daemonOwnerRuntimeConfig(cfg)
	if err != nil {
		return projectdaemon.Config{}, err
	}
	return daemonConfigForRuntime(ownerCfg)
}

func daemonWorkspaceRequestConfigForRuntime(cfg leafwikiRuntimeConfig) (projectdaemon.Config, error) {
	workspaceCfg, err := daemonWorkspaceRuntimeConfig(cfg)
	if err != nil {
		return projectdaemon.Config{}, err
	}
	return daemonConfigForRuntime(workspaceCfg)
}

func daemonWorkspaceRuntimeConfig(cfg leafwikiRuntimeConfig) (leafwikiRuntimeConfig, error) {
	ownerCfg := cfg
	ownerCfg.APIKey = ""
	ownerCfg.RuntimeStack = projectdaemon.RuntimeStackWikidFrontd
	ownerCfg.EnableWorkspaceSync = true
	if ownerCfg.MCPTransports.Stdio && ownerCfg.Logging.Target == leaflogging.TargetStderr {
		fileLogging, err := resolveLoggingForRuntime(leaflogging.ConfigInput{
			Target:    string(leaflogging.TargetFile),
			TargetSet: true,
			DataDir:   ownerCfg.Workspace.DataDir,
		})
		if err != nil {
			return leafwikiRuntimeConfig{}, err
		}
		fileLogging.Level = ownerCfg.Logging.Level
		ownerCfg.Logging = fileLogging
	}
	return ownerCfg, nil
}

func daemonLogFileForConfig(cfg leafwikiRuntimeConfig, canonicalDataDir string) string {
	if cfg.Logging.Target != leaflogging.TargetFile || strings.TrimSpace(cfg.Logging.FilePath) == "" {
		return cfg.Logging.FilePath
	}
	logPath := filepath.Clean(cfg.Logging.FilePath)
	absLogPath, err := filepathAbsForRuntime(logPath)
	if err != nil {
		return logPath
	}
	originalDataDir, err := filepathAbsForRuntime(filepath.Clean(cfg.Workspace.DataDir))
	if err == nil {
		if rel, ok := localRelativePath(originalDataDir, absLogPath); ok {
			return filepath.Clean(filepath.Join(canonicalDataDir, rel))
		}
	}
	if rel, ok := localRelativePath(canonicalDataDir, absLogPath); ok {
		return filepath.Clean(filepath.Join(canonicalDataDir, rel))
	}
	return filepath.Clean(absLogPath)
}

func localRelativePath(base string, target string) (string, bool) {
	rel, err := filepathRelForRuntime(filepath.Clean(base), filepath.Clean(target))
	if err != nil {
		return "", false
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}
