package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/perber/wiki/internal/localization"
	"github.com/perber/wiki/internal/locking"
	"github.com/perber/wiki/internal/projectdaemon"
)

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO rejects second process with same root dir", ginkgo.Label("e2e"), func() {
		stdinReader, stdinWriter := io.Pipe()
		defer closeBestEffort(stdinWriter)
		baseDir := leafwikiTempDir()
		rootDir := filepath.Join(baseDir, "content")
		firstPort := freeTCPPort()
		first := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", filepath.Join(baseDir, "data-a"),
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", firstPort,
			"--log-target", "stderr",
		}, nil, stdinReader)
		waitForLeafwikiReady(first, firstPort)
		_ = waitForProjectDaemonDescriptor(filepath.Join(baseDir, "data-a"))

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", filepath.Join(baseDir, "data-b"),
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", freeTCPPort(),
			"--log-target", "stderr",
		}, nil, 12*time.Second)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("expected second process with same root dir to exit non-zero"))
		Expect(err).NotTo(MatchError(context.DeadlineExceeded), fmt.Sprintf("second process did not exit; expected root directory lock rejection\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorLeafWikiStartupFailed)), fmt.Sprintf("stderr = %q, want startup failure log entry", stderr))

		Expect(stdinWriter.Close()).To(Succeed(), fmt.Sprintf("close stdin writer: %v", err))
		first.waitForExit()

	})
})

var _ = ginkgo.Describe("concurrent project daemon startup", func() {
	ginkgo.It("attaches to winning owner after spawn error", ginkgo.Label("integration"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("canonicalize project: %v", err))

		ownerCfg := projectdaemon.Config{
			DataDir:           canonicalData,
			RootDir:           canonicalRoot,
			AuthDisabled:      true,
			PublicMCPEnabled:  false,
			Host:              "127.0.0.1",
			Port:              "8080",
			DaemonIdleTimeout: "10m0s",
		}
		hash, err := projectdaemon.ConfigHash(ownerCfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("ConfigHash: %v", err))

		dataLock, err := locking.AcquireDataDirLock(canonicalData)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire fake owner data lock: %v", err))

		defer releaseRuntimeLockBestEffort(dataLock)
		rootLock, err := locking.AcquireRootDirLock(canonicalRoot)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire fake owner root lock: %v", err))

		defer releaseRuntimeLockBestEffort(rootLock)
		token := "control-token"
		control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Header.Get(projectdaemon.ControlTokenHeader) != token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if req.Method == http.MethodGet && req.URL.Path == "/health" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(projectdaemon.DaemonHealth{
					OK:            true,
					SchemaVersion: projectdaemon.DescriptorSchemaVersion,
					PID:           os.Getpid(),
					DataDir:       canonicalData,
					RootDir:       canonicalRoot,
					ConfigHash:    hash,
				})
				return
			}
			http.NotFound(w, req)
		}))
		ginkgo.DeferCleanup(control.Close)
		descriptorPath := projectdaemon.DescriptorPath(canonicalData)
		errorPath := filepath.Join(leafwikiTempDir(), "startup.err")
		Expect(os.WriteFile(errorPath, []byte("acquire data directory lock: data directory is already in use"), 0o600)).To(Succeed(), fmt.Sprintf("write startup error: %v", err))
		go func() {
			time.Sleep(2200 * time.Millisecond)
			_ = projectdaemon.WriteDescriptorAtomic(descriptorPath, &projectdaemon.Descriptor{
				SchemaVersion:    projectdaemon.DescriptorSchemaVersion,
				PID:              os.Getpid(),
				StartedAt:        time.Now().UTC(),
				DataDir:          canonicalData,
				RootDir:          canonicalRoot,
				PublicURL:        "http://127.0.0.1:8080",
				PublicMCPEnabled: false,
				ControlURL:       control.URL,
				ConfigHash:       hash,
				IdleTimeout:      "10m0s",
				ControlToken:     token,
				Config:           ownerCfg,
			})
		}()

		desc, err := waitForProjectDaemon(context.Background(), descriptorPath, errorPath, ownerCfg, mcpTransports{Stdio: true})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("waitForProjectDaemon should attach to winning owner after startup error, got %v", err))
		Expect(desc.ControlURL).To(Equal(control.URL), fmt.Sprintf("attached descriptor control URL = %q, want %q", desc.ControlURL, control.URL))

	})
})

var _ = ginkgo.Describe("concurrent project daemon startup", func() {
	ginkgo.It("handles structured lock error", ginkgo.Label("integration"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("canonicalize project: %v", err))

		ownerCfg := projectdaemon.Config{
			DataDir:           canonicalData,
			RootDir:           canonicalRoot,
			AuthDisabled:      true,
			Host:              "127.0.0.1",
			Port:              "8080",
			DaemonIdleTimeout: "10m0s",
		}
		hash, err := projectdaemon.ConfigHash(ownerCfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("ConfigHash: %v", err))

		dataLock, err := locking.AcquireDataDirLock(canonicalData)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire fake owner data lock: %v", err))

		defer releaseRuntimeLockBestEffort(dataLock)
		rootLock, err := locking.AcquireRootDirLock(canonicalRoot)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire fake owner root lock: %v", err))

		defer releaseRuntimeLockBestEffort(rootLock)
		token := "control-token"
		control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Header.Get(projectdaemon.ControlTokenHeader) != token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if req.Method == http.MethodGet && req.URL.Path == "/health" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(projectdaemon.DaemonHealth{
					OK:            true,
					SchemaVersion: projectdaemon.DescriptorSchemaVersion,
					PID:           os.Getpid(),
					DataDir:       canonicalData,
					RootDir:       canonicalRoot,
					ConfigHash:    hash,
				})
				return
			}
			http.NotFound(w, req)
		}))
		ginkgo.DeferCleanup(control.Close)
		descriptorPath := projectdaemon.DescriptorPath(canonicalData)
		errorPath := filepath.Join(leafwikiTempDir(), "startup.err")
		rawErr, err := json.Marshal(projectDaemonStartupError{
			Kind:            projectDaemonStartupErrorKindLock,
			RenderedMessage: "acquire data directory lock: data directory is already in use",
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("marshal startup error: %v", err))

		Expect(os.WriteFile(errorPath, rawErr, 0o600)).To(Succeed(), fmt.Sprintf("write startup error: %v", err))
		go func() {
			time.Sleep(2200 * time.Millisecond)
			_ = projectdaemon.WriteDescriptorAtomic(descriptorPath, &projectdaemon.Descriptor{
				SchemaVersion:    projectdaemon.DescriptorSchemaVersion,
				PID:              os.Getpid(),
				StartedAt:        time.Now().UTC(),
				DataDir:          canonicalData,
				RootDir:          canonicalRoot,
				PublicURL:        "http://127.0.0.1:8080",
				PublicMCPEnabled: false,
				ControlURL:       control.URL,
				ConfigHash:       hash,
				IdleTimeout:      "10m0s",
				ControlToken:     token,
				Config:           ownerCfg,
			})
		}()

		desc, err := waitForProjectDaemon(context.Background(), descriptorPath, errorPath, ownerCfg, mcpTransports{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("waitForProjectDaemon should attach after structured lock startup error, got %v", err))
		Expect(desc.ControlURL).To(Equal(control.URL), fmt.Sprintf("attached descriptor control URL = %q, want %q", desc.ControlURL, control.URL))

	})
})

var _ = ginkgo.Describe("project daemon startup", func() {
	ginkgo.It("reports non lock startup error directly", ginkgo.Label("integration"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("canonicalize project: %v", err))

		ownerCfg := projectdaemon.Config{
			DataDir:           canonicalData,
			RootDir:           canonicalRoot,
			AuthDisabled:      true,
			Host:              "127.0.0.1",
			Port:              "8080",
			DaemonIdleTimeout: "10m0s",
		}
		errorPath := filepath.Join(leafwikiTempDir(), "startup.err")
		Expect(os.WriteFile(errorPath, []byte("start HTTP listener: listen tcp 127.0.0.1:8080: bind: address already in use"), 0o600)).To(Succeed(), fmt.Sprintf("write startup error: %v", err))
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		_, err = waitForProjectDaemon(ctx, projectdaemon.DescriptorPath(canonicalData), errorPath, ownerCfg, mcpTransports{})
		Expect(err).To(MatchError(errProjectDaemonStartupFailed))
		Expect(err).NotTo(MatchError(errProjectLockedNoAttachableDaemon))

	})
})

var _ = ginkgo.Describe("project daemon startup", func() {
	ginkgo.It("reports structured non lock startup error directly", ginkgo.Label("integration"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("canonicalize project: %v", err))

		ownerCfg := projectdaemon.Config{
			DataDir:           canonicalData,
			RootDir:           canonicalRoot,
			AuthDisabled:      true,
			Host:              "127.0.0.1",
			Port:              "8080",
			DaemonIdleTimeout: "10m0s",
		}
		errorPath := filepath.Join(leafwikiTempDir(), "startup.err")
		rawErr, err := json.Marshal(projectDaemonStartupError{
			Kind:            projectDaemonStartupErrorKindStartup,
			RenderedMessage: "start HTTP listener: listen tcp 127.0.0.1:8080: bind: address already in use",
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("marshal startup error: %v", err))

		Expect(os.WriteFile(errorPath, rawErr, 0o600)).To(Succeed(), fmt.Sprintf("write startup error: %v", err))
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		_, err = waitForProjectDaemon(ctx, projectdaemon.DescriptorPath(canonicalData), errorPath, ownerCfg, mcpTransports{})
		Expect(err).To(MatchError(errProjectDaemonStartupFailed))
		Expect(err).NotTo(MatchError(errProjectLockedNoAttachableDaemon))

	})
})

var _ = ginkgo.Describe("STDIO attach daemon config comparison", func() {
	ginkgo.It("ignores owner logging settings", ginkgo.Label("unit"), func() {
		owner := projectdaemon.Config{
			DataDir:           "/tmp/leafwiki-data",
			RootDir:           "/tmp/leafwiki-root",
			AuthDisabled:      true,
			Host:              "127.0.0.1",
			Port:              "8080",
			LogTarget:         "stderr",
			LogFile:           "",
			DisableRequestLog: false,
			DaemonIdleTimeout: "10m0s",
		}
		requested := owner
		requested.LogTarget = "file"
		requested.LogFile = "/tmp/leafwiki-data/.leafwiki/logs/leafwiki.log"
		requested.DisableRequestLog = true

		mismatches := compareProjectDaemonConfigForRequest(owner, requested, mcpTransports{Stdio: true})
		Expect(mismatches).To(BeEmpty(), fmt.Sprintf("mismatches = %#v, want STDIO-only attach to ignore owner logging settings", mismatches))

	})
})

var _ = ginkgo.Describe("plain server daemon config comparison", func() {
	ginkgo.It("preserves public MCP mismatch", ginkgo.Label("unit"), func() {
		owner := projectdaemon.Config{
			DataDir:          "/tmp/leafwiki-data",
			RootDir:          "/tmp/leafwiki-root",
			AuthDisabled:     true,
			PublicMCPEnabled: true,
			Host:             "127.0.0.1",
			Port:             "8080",
		}
		requested := owner
		requested.PublicMCPEnabled = false

		mismatches := compareProjectDaemonConfigForRequest(owner, requested, mcpTransports{})
		Expect(mismatches).To(ConsistOf(HaveField("Field", Equal("public-mcp-enabled"))), fmt.Sprintf("mismatches = %#v, want public MCP mismatch for plain server startup", mismatches))

	})
})

var _ = ginkgo.Describe("daemon runtime configuration", func() {
	ginkgo.It("includes workspace sync", ginkgo.Label("unit"), func() {
		baseDir := leafwikiTempDir()
		cfg := testRuntimeConfig(
			filepath.Join(baseDir, "data"),
			filepath.Join(baseDir, "content"),
			"8080",
			mcpTransports{},
			true,
		)
		cfg.EnableWorkspaceSync = true

		daemonCfg, err := daemonConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemon config: %v", err))
		Expect(daemonCfg.EnableWorkspaceSync).To(BeTrue(), fmt.Sprintf("EnableWorkspaceSync = false, want true"))

	})
})

var _ = ginkgo.Describe("daemon owner runtime configuration", func() {
	ginkgo.It("for wikidfrontd forces workspace sync", ginkgo.Label("unit"), func() {
		baseDir := leafwikiTempDir()
		cfg := testRuntimeConfig(
			filepath.Join(baseDir, "data"),
			filepath.Join(baseDir, "content"),
			"8080",
			mcpTransports{},
			true,
		)
		cfg.RuntimeStack = projectdaemon.RuntimeStackWikidFrontd
		cfg.EnableWorkspaceSync = false

		ownerCfg, err := daemonOwnerRuntimeConfig(cfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemonOwnerRuntimeConfig failed: %v", err))

		daemonCfg, err := daemonConfigForRuntime(ownerCfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemonConfigForRuntime failed: %v", err))
		Expect(ownerCfg.EnableWorkspaceSync).To(BeTrue(), fmt.Sprintf("owner workspace sync = false, want true"))
		Expect(daemonCfg.EnableWorkspaceSync).To(BeTrue(), fmt.Sprintf("daemon workspace sync = false, want true"))

	})
})

var _ = ginkgo.Describe("project daemon request comparison", func() {
	// Plantrace evidence: TestCompareProjectDaemonConfigForRequestCoversDaemonRelevantFields.
	ginkgo.It("reports daemon relevant descriptor fields", ginkgo.Label("unit"), func() {
		owner := completeDaemonCompareConfig()
		tests := []struct {
			name  string
			field string
			mut   func(*projectdaemon.Config)
		}{
			{name: "data dir", field: "data-dir", mut: func(cfg *projectdaemon.Config) { cfg.DataDir = "/tmp/other-data" }},
			{name: "root dir", field: "root-dir", mut: func(cfg *projectdaemon.Config) { cfg.RootDir = "/tmp/other-root" }},
			{name: "auth mode", field: "auth-disabled", mut: func(cfg *projectdaemon.Config) { cfg.AuthDisabled = !cfg.AuthDisabled }},
			{name: "public MCP", field: "public-mcp-enabled", mut: func(cfg *projectdaemon.Config) { cfg.PublicMCPEnabled = !cfg.PublicMCPEnabled }},
			{name: "host", field: "host", mut: func(cfg *projectdaemon.Config) { cfg.Host = "127.0.0.2" }},
			{name: "port", field: "port", mut: func(cfg *projectdaemon.Config) { cfg.Port = "9090" }},
			{name: "base path", field: "base-path", mut: func(cfg *projectdaemon.Config) { cfg.BasePath = "/docs" }},
			{name: "markdown link root prefix", field: "markdown-link-root-prefix", mut: func(cfg *projectdaemon.Config) { cfg.MarkdownLinkRootPrefix = "/docs" }},
			{name: "public access", field: "public-access", mut: func(cfg *projectdaemon.Config) { cfg.PublicAccess = !cfg.PublicAccess }},
			{name: "allow insecure", field: "allow-insecure", mut: func(cfg *projectdaemon.Config) { cfg.AllowInsecure = !cfg.AllowInsecure }},
			{name: "access token timeout", field: "access-token-timeout", mut: func(cfg *projectdaemon.Config) { cfg.AccessTokenTimeout = "2h0m0s" }},
			{name: "refresh token timeout", field: "refresh-token-timeout", mut: func(cfg *projectdaemon.Config) { cfg.RefreshTokenTimeout = "720h0m0s" }},
			{name: "injected header hash", field: "inject-code-in-header-hash", mut: func(cfg *projectdaemon.Config) { cfg.InjectCodeInHeaderHash = "other-hash" }},
			{name: "custom stylesheet", field: "custom-stylesheet", mut: func(cfg *projectdaemon.Config) { cfg.CustomStylesheet = "/tmp/custom.css" }},
			{name: "log target", field: "log-target", mut: func(cfg *projectdaemon.Config) { cfg.LogTarget = "file" }},
			{name: "log file", field: "log-file", mut: func(cfg *projectdaemon.Config) { cfg.LogFile = "/tmp/leafwiki.log" }},
			{name: "hide metadata", field: "hide-link-metadata-section", mut: func(cfg *projectdaemon.Config) { cfg.HideLinkMetadataSection = !cfg.HideLinkMetadataSection }},
			{name: "upload size", field: "max-asset-upload-size-bytes", mut: func(cfg *projectdaemon.Config) { cfg.MaxAssetUploadSizeBytes = 99 }},
			{name: "link refactor", field: "enable-link-refactor", mut: func(cfg *projectdaemon.Config) { cfg.EnableLinkRefactor = !cfg.EnableLinkRefactor }},
			{name: "remote user enabled", field: "enable-http-remote-user", mut: func(cfg *projectdaemon.Config) { cfg.EnableHTTPRemoteUser = !cfg.EnableHTTPRemoteUser }},
			{name: "remote user header", field: "http-remote-user-header", mut: func(cfg *projectdaemon.Config) { cfg.HTTPRemoteUserHeader = "X-User" }},
			{name: "trusted proxies", field: "trusted-proxy-ips", mut: func(cfg *projectdaemon.Config) { cfg.TrustedProxyIPs = "127.0.0.1/32" }},
			{name: "remote user logout", field: "http-remote-user-logout-url", mut: func(cfg *projectdaemon.Config) { cfg.HTTPRemoteUserLogoutURL = "https://example.test/logout" }},
			{name: "request log", field: "disable-request-log", mut: func(cfg *projectdaemon.Config) { cfg.DisableRequestLog = !cfg.DisableRequestLog }},
			{name: "idle timeout", field: "daemon-idle-timeout", mut: func(cfg *projectdaemon.Config) { cfg.DaemonIdleTimeout = "1m0s" }},
		}

		for _, tt := range tests {
			func() {
				_ = tt.name
				requested := owner
				tt.mut(&requested)

				mismatches := compareProjectDaemonConfigForRequest(owner, requested, mcpTransports{HTTP: true})
				Expect(mismatches).To(ConsistOf(HaveField("Field", Equal(tt.field))), fmt.Sprintf("mismatches = %#v, want one %q mismatch", mismatches, tt.field))

			}()
		}

	})
})

var _ = ginkgo.Describe("STDIO attach daemon config comparison", func() {
	ginkgo.It("documents ignored fields", ginkgo.Label("unit"), func() {
		owner := completeDaemonCompareConfig()
		requested := owner
		requested.PublicMCPEnabled = !owner.PublicMCPEnabled
		requested.Host = "0.0.0.0"
		requested.LogTarget = "file"
		requested.LogFile = "/tmp/leafwiki.log"
		requested.DisableRequestLog = !owner.DisableRequestLog

		mismatches := compareProjectDaemonConfigForRequest(owner, requested, mcpTransports{Stdio: true})
		Expect(mismatches).To(BeEmpty(), fmt.Sprintf("mismatches = %#v, want STDIO-only attach to inherit public MCP/logging/request-log settings", mismatches))

	})
})

var _ = ginkgo.Describe("project daemon descriptor comparison", func() {
	ginkgo.It("checks top level workspace ID", ginkgo.Label("unit"), func() {
		requested := completeDaemonCompareConfig()
		requested.WorkspaceID = "beta"
		descriptorConfig := requested
		desc := &projectdaemon.Descriptor{
			Role:            projectdaemon.RoleWorkspaced,
			WorkspaceID:     "alpha",
			PrivateMCPURL:   "http://127.0.0.1:1/mcp",
			PrivateMCPToken: "token",
			Config:          descriptorConfig,
		}

		mismatches := compareProjectDaemonDescriptorForRequest(desc, requested, mcpTransports{Stdio: true})
		Expect(mismatches).To(ConsistOf(SatisfyAll(
			HaveField("Field", Equal("workspace-id")),
			HaveField("Want", Equal("alpha")),
			HaveField("Got", Equal("beta")),
		)), fmt.Sprintf("mismatches = %#v, want top-level workspace-id mismatch alpha -> beta", mismatches))

	})
})
