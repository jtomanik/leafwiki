package runtimeconfig

import (
	"time"

	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/wiki"
)

type LeafWikiRuntimeConfig struct {
	Workspace               wiki.Workspace
	Host                    string
	Port                    string
	AdminPassword           string
	JWTSecret               string
	PublicAccess            bool
	AllowInsecure           bool
	InjectCodeInHeader      string
	CustomStylesheet        string
	Logging                 leaflogging.Config
	DisableAuth             bool
	HideLinkMetadataSection bool
	AccessTokenTimeout      time.Duration
	RefreshTokenTimeout     time.Duration
	BasePath                string
	MaxAssetUploadSize      int64
	EnableWorkspaceSync     bool
	EnableLinkRefactor      bool
	MCPTransports           MCPTransports
	APIKey                  string
	EnableHTTPRemoteUser    bool
	HTTPRemoteUserHeader    string
	TrustedProxyIPsRaw      string
	HTTPRemoteUserLogoutURL string
	DisableRequestLog       bool
	DaemonIdleTimeout       time.Duration
	DisableIdleShutdown     bool
	DaemonStartupErrorPath  string
	DetachDaemonOwnerIO     bool
	MarkdownLinkRootPrefix  string
	RuntimeStack            string
}
