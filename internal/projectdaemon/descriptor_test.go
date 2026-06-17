package projectdaemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteDescriptorAtomicUses0600AndOmitsSessionAPIKey(t *testing.T) {
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
}

func TestDescriptorRoundTripIncludesRuntimeStackAndRoleHealth(t *testing.T) {
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
}

func TestReadTrustedDescriptorRejectsGroupReadableFile(t *testing.T) {
	dataDir := t.TempDir()
	path := DescriptorPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create descriptor dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"schemaVersion":1}`), 0o644); err != nil {
		t.Fatalf("write descriptor: %v", err)
	}

	if _, err := ReadTrustedDescriptor(path); err == nil || !strings.Contains(err.Error(), "want 0600") {
		t.Fatalf("ReadTrustedDescriptor error = %v, want 0600 rejection", err)
	}
}

func TestCompareConfigReportsRedactedMismatch(t *testing.T) {
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
}
