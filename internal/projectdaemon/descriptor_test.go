package projectdaemon

import (
	"errors"
	ginkgo "github.com/onsi/ginkgo/v2"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var _ = ginkgo.It("TestWriteDescriptorAtomicUses0600AndOmitsSessionAPIKey", func() {
	t := ginkgo.GinkgoT()
	dataDir := t.TempDir()
	desc := &Descriptor{
		SchemaVersion:    DescriptorSchemaVersion,
		PID:              1234,
		StartedAt:        time.Now().UTC(),
		DataDir:          dataDir,
		RootDir:          filepath.Join(t.TempDir(), "root"),
		PublicURL:        "http://127.0.0.1:8080",
		PublicMCPEnabled: false,
		BasePath:         "",
		ControlURL:       "http://127.0.0.1:12345",
		ConfigHash:       "hash",
		IdleTimeout:      "10m0s",
		ControlToken:     "control-token",
		Config: Config{
			DataDir:      dataDir,
			RootDir:      filepath.Join(t.TempDir(), "root"),
			AuthDisabled: false,
		},
	}
	path := DescriptorPath(dataDir)

	if err := WriteDescriptorAtomic(path, desc); err != nil {
		t.Fatalf("WriteDescriptorAtomic failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat descriptor: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("descriptor mode = %v, want 0600", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read descriptor: %v", err)
	}
	for _, secret := range []string{"lwk_secret", "jwt-secret", "admin-password", HashSecret("jwt-secret"), HashSecret("admin-password"), "jwtSecretHash", "adminPasswordHash"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("descriptor leaked secret %q:\n%s", secret, raw)
		}
	}
	loaded, err := ReadDescriptor(path)
	if err != nil {
		t.Fatalf("ReadDescriptor failed: %v", err)
	}
	if loaded.ControlToken != "control-token" || loaded.Config.AuthDisabled {
		t.Fatalf("descriptor round trip lost fields: %#v", loaded)
	}

})

var _ = ginkgo.It("TestDescriptorRoundTripIncludesRuntimeStackAndRoleHealth", func() {
	t := ginkgo.GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "root")
	now := time.Now().UTC().Truncate(time.Second)
	desc := &Descriptor{
		SchemaVersion:    DescriptorSchemaVersion,
		RuntimeStack:     RuntimeStackWikidFrontd,
		Role:             RoleWikid,
		PID:              1234,
		StartedAt:        now,
		DataDir:          dataDir,
		RootDir:          rootDir,
		PublicURL:        "http://127.0.0.1:8080",
		PublicMCPEnabled: true,
		BasePath:         "",
		ControlURL:       "http://127.0.0.1:12345",
		ConfigHash:       "hash",
		IdleTimeout:      "10m0s",
		ControlToken:     "control-token",
		Config: Config{
			DataDir:      dataDir,
			RootDir:      rootDir,
			AuthDisabled: true,
			RuntimeStack: RuntimeStackWikidFrontd,
		},
		Roles: []RoleHealth{
			{Name: RoleWikid, State: RoleStateReady, PID: 1234, UpdatedAt: now},
			{Name: RoleFrontd, State: RoleStateReady, PID: 2345, URL: "http://127.0.0.1:8080", UpdatedAt: now},
			{Name: RoleWorkspaced, State: RoleStateReady, PID: 3456, URL: "http://127.0.0.1:43111", Private: true, UpdatedAt: now},
		},
	}

	path := DescriptorPath(dataDir)
	if err := WriteDescriptorAtomic(path, desc); err != nil {
		t.Fatalf("WriteDescriptorAtomic failed: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read descriptor: %v", err)
	}
	for _, expected := range []string{`"runtimeStack": "wikid-frontd"`, `"role": "wikid"`, `"name": "frontd"`, `"name": "workspaced"`, `"private": true`} {
		if !strings.Contains(string(raw), expected) {
			t.Fatalf("descriptor missing %s:\n%s", expected, raw)
		}
	}
	if strings.Contains(string(raw), "session-api-key") {
		t.Fatalf("descriptor leaked session API key material:\n%s", raw)
	}

	loaded, err := ReadTrustedDescriptor(path)
	if err != nil {
		t.Fatalf("ReadTrustedDescriptor failed: %v", err)
	}
	if loaded.RuntimeStack != RuntimeStackWikidFrontd || loaded.Role != RoleWikid {
		t.Fatalf("runtime metadata = %q/%q, want %q/%q", loaded.RuntimeStack, loaded.Role, RuntimeStackWikidFrontd, RoleWikid)
	}
	if len(loaded.Roles) != 3 || loaded.Roles[2].Name != RoleWorkspaced || !loaded.Roles[2].Private {
		t.Fatalf("role health round trip = %#v", loaded.Roles)
	}

})

var _ = ginkgo.It("TestDescriptorRoundTripIncludesWorkspaceIdentityAndPrivateMCPAttach", func() {
	t := ginkgo.GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "root")
	desc := &Descriptor{
		SchemaVersion:    DescriptorSchemaVersion,
		RuntimeStack:     RuntimeStackWikidFrontd,
		Role:             RoleWorkspaced,
		WorkspaceID:      "home",
		PID:              1234,
		StartedAt:        time.Now().UTC().Truncate(time.Second),
		DataDir:          dataDir,
		RootDir:          rootDir,
		PublicURL:        "http://127.0.0.1:8080",
		PublicMCPEnabled: true,
		BasePath:         "",
		ControlURL:       "http://127.0.0.1:12345",
		PrivateMCPURL:    "http://127.0.0.1:23456/mcp",
		PrivateMCPToken:  "super-secret-token",
		ConfigHash:       "hash",
		IdleTimeout:      "10m0s",
		ControlToken:     "control-token",
		Config: Config{
			RuntimeStack:    RuntimeStackWikidFrontd,
			WorkspaceID:     "home",
			DataDir:         dataDir,
			RootDir:         rootDir,
			PrivateMCPURL:   "http://127.0.0.1:23456/mcp",
			PrivateMCPToken: "super-secret-token",
		},
	}

	path := DescriptorPath(dataDir)
	if err := WriteDescriptorAtomic(path, desc); err != nil {
		t.Fatalf("WriteDescriptorAtomic failed: %v", err)
	}

	loaded, err := ReadTrustedDescriptor(path)
	if err != nil {
		t.Fatalf("ReadTrustedDescriptor failed: %v", err)
	}
	if loaded.WorkspaceID != "home" || loaded.Config.WorkspaceID != "home" {
		t.Fatalf("workspace IDs = %q/%q, want home/home", loaded.WorkspaceID, loaded.Config.WorkspaceID)
	}
	if loaded.PrivateMCPURL != desc.PrivateMCPURL || loaded.PrivateMCPToken != desc.PrivateMCPToken {
		t.Fatalf("private MCP fields = %q/%q", loaded.PrivateMCPURL, loaded.PrivateMCPToken)
	}

	requested := loaded.Config
	requested.PrivateMCPToken = "different-secret"
	message := FormatConfigMismatch(CompareConfig(loaded.Config, requested))
	if strings.Contains(message, "super-secret-token") || strings.Contains(message, "different-secret") {
		t.Fatalf("mismatch leaked private MCP token: %s", message)
	}
	if !strings.Contains(message, "private-mcp-token") || !strings.Contains(message, "redacted") {
		t.Fatalf("mismatch message = %q, want redacted private MCP token field", message)
	}

})

var _ = ginkgo.It("TestGlobalDescriptorPath", func() {
	t := ginkgo.GinkgoT()
	runtimeDir := filepath.Join(t.TempDir(), ".leafwiki", "runtime")

	got := GlobalDescriptorPath(runtimeDir, RoleWikid)

	if got != filepath.Join(runtimeDir, "wikid.json") {
		t.Fatalf("GlobalDescriptorPath = %q", got)
	}

})

var _ = ginkgo.It("RemoveDescriptor is idempotent for missing and existing descriptor files", func() {
	t := ginkgo.GinkgoT()
	path := DescriptorPath(t.TempDir())
	if err := RemoveDescriptor(path); err != nil {
		t.Fatalf("RemoveDescriptor missing file failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create descriptor dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"schemaVersion":1}`), 0o600); err != nil {
		t.Fatalf("write descriptor: %v", err)
	}
	if err := RemoveDescriptor(path); err != nil {
		t.Fatalf("RemoveDescriptor existing file failed: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("descriptor stat err = %v, want not exist", err)
	}
})

var _ = ginkgo.It("ConfigHash is deterministic and changes with canonical config values", func() {
	t := ginkgo.GinkgoT()
	base := Config{DataDir: "/data", RootDir: "/root", Port: "8080"}
	first, err := ConfigHash(base)
	if err != nil {
		t.Fatalf("ConfigHash failed: %v", err)
	}
	second, err := ConfigHash(base)
	if err != nil {
		t.Fatalf("ConfigHash repeat failed: %v", err)
	}
	changed, err := ConfigHash(Config{DataDir: "/data", RootDir: "/root", Port: "8081"})
	if err != nil {
		t.Fatalf("ConfigHash changed failed: %v", err)
	}

	if first == "" || len(first) != 64 || first != second {
		t.Fatalf("ConfigHash deterministic value = %q/%q, want matching sha256 hex", first, second)
	}
	if changed == first {
		t.Fatalf("ConfigHash did not change after config value changed")
	}
})

var _ = ginkgo.It("TestReadTrustedDescriptorRejectsGroupReadableFile", func() {
	t := ginkgo.GinkgoT()
	dataDir := t.TempDir()
	path := DescriptorPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create descriptor dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"schemaVersion":1}`), 0o644); err != nil {
		t.Fatalf("write descriptor: %v", err)
	}

	if _, err := ReadTrustedDescriptor(path); err == nil || !errors.Is(err, errDescriptorModeMismatch) {
		t.Fatalf("ReadTrustedDescriptor error = %v, want 0600 rejection", err)
	}

})

var _ = ginkgo.It("TestCompareConfigReportsRedactedMismatch", func() {
	t := ginkgo.GinkgoT()
	owner := Config{
		DataDir:                "/data",
		RootDir:                "/root",
		Port:                   "8080",
		InjectCodeInHeaderHash: HashSecret("owner-code"),
	}
	requested := owner
	requested.Port = "8081"
	requested.InjectCodeInHeaderHash = HashSecret("requested-code")

	message := FormatConfigMismatch(CompareConfig(owner, requested))

	if !strings.Contains(message, "port") || !strings.Contains(message, "8080") || !strings.Contains(message, "8081") {
		t.Fatalf("mismatch message = %q, want port values", message)
	}
	for _, secret := range []string{"owner-code", "requested-code"} {
		if strings.Contains(message, secret) {
			t.Fatalf("mismatch leaked raw secret %q: %s", secret, message)
		}
	}
	if !strings.Contains(message, "inject-code-in-header-hash") || !strings.Contains(message, "redacted") {
		t.Fatalf("mismatch message = %q, want redacted hash field", message)
	}

})
