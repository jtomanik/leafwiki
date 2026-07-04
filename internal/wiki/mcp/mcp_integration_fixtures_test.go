package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/wiki"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - MCP agent context returns canonical examples

func newLocalMCPTestWiki(_ bool) *wiki.Wiki {
	GinkgoHelper()

	w, _ := newLocalMCPTestWikiWithStorage()
	return w
}

func newLocalMCPTestWikiWithStorage() (*wiki.Wiki, string) {
	GinkgoHelper()

	return newLocalMCPTestWikiWithOptionsAndStorage(wiki.WikiOptions{
		AuthDisabled: true,
	})
}

func newLocalMCPTestWikiWithOptions(opts wiki.WikiOptions) *wiki.Wiki {
	GinkgoHelper()

	w, _ := newLocalMCPTestWikiWithOptionsAndStorage(opts)
	return w
}

func newLocalMCPTestWikiWithOptionsAndStorage(opts wiki.WikiOptions) (*wiki.Wiki, string) {
	GinkgoHelper()

	storageDir := filepath.Join(mcpIntegrationTempDir(), "data")
	rootDir := filepath.Join(mcpIntegrationTempDir(), "content")
	if opts.Workspace.ID == "" {
		opts.Workspace.ID = "default"
	}
	if opts.Workspace.DataDir == "" {
		opts.Workspace.DataDir = storageDir
	}
	if opts.Workspace.RootDir == "" {
		opts.Workspace.RootDir = rootDir
	}
	if opts.AdminPassword == "" {
		opts.AdminPassword = "admin"
	}
	if opts.JWTSecret == "" {
		opts.JWTSecret = "secretkey"
	}
	if opts.AccessTokenTimeout == 0 {
		opts.AccessTokenTimeout = 15 * time.Minute
	}
	if opts.RefreshTokenTimeout == 0 {
		opts.RefreshTokenTimeout = 7 * 24 * time.Hour
	}
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		Workspace:           opts.Workspace,
		AdminPassword:       opts.AdminPassword,
		JWTSecret:           opts.JWTSecret,
		AccessTokenTimeout:  opts.AccessTokenTimeout,
		RefreshTokenTimeout: opts.RefreshTokenTimeout,
		AuthDisabled:        opts.AuthDisabled,
	})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		Expect(w.Close()).To(Succeed())
	})
	return w, storageDir
}

func mcpIntegrationTempDir() string {
	GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-mcp-integration-*")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(os.RemoveAll, dir)
	return dir
}

func newLocalMCPTestRouter(w *wiki.Wiki, opts httpinternal.RouterOptions) http.Handler {
	if opts.MCPEnabled && opts.MCPBindHost == "" {
		opts.MCPBindHost = "127.0.0.1"
	}
	return httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), opts)
}

func connectLocalMCP(handler http.Handler, path string) *sdkmcp.ClientSession {
	GinkgoHelper()

	server := httptest.NewServer(handler)
	DeferCleanup(server.Close)

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "leafwiki-test", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), &sdkmcp.StreamableClientTransport{
		Endpoint:             server.URL + path,
		HTTPClient:           server.Client(),
		DisableStandaloneSSE: true,
	}, nil)
	Expect(err).NotTo(HaveOccurred(), "MCP client should connect")
	DeferCleanup(func() { _ = session.Close() })
	return session
}

func listAllToolNames(session *sdkmcp.ClientSession) []string {
	GinkgoHelper()

	tools := listAllTools(session)
	var names []string
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func listAllTools(session *sdkmcp.ClientSession) []*sdkmcp.Tool {
	GinkgoHelper()

	var tools []*sdkmcp.Tool
	cursor := ""
	for {
		result, err := session.ListTools(context.Background(), &sdkmcp.ListToolsParams{Cursor: cursor})
		Expect(err).NotTo(HaveOccurred(), "ListTools should return a page")
		for _, tool := range result.Tools {
			tools = append(tools, tool)
		}
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})
	return tools
}
