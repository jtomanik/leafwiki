package projectdaemon

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

const (
	DescriptorSchemaVersion = 1
	DescriptorFileName      = "project-daemon.json"
	ControlTokenHeader      = "X-LeafWiki-Daemon-Token"
	ActorContextHeader      = "X-LeafWiki-Actor-Context"
	WorkspaceIDHeader       = "X-LeafWiki-Workspace-ID"
	DefaultIdleTimeout      = 10 * time.Minute
	DefaultHeartbeatTTL     = 15 * time.Second
)

type Config struct {
	RuntimeStack            string `json:"runtimeStack,omitempty"`
	WorkspaceID             string `json:"workspaceId,omitempty"`
	DataDir                 string `json:"dataDir"`
	RootDir                 string `json:"rootDir"`
	PrivateMCPURL           string `json:"privateMcpUrl,omitempty"`
	PrivateMCPToken         string `json:"privateMcpToken,omitempty"`
	AuthDisabled            bool   `json:"authDisabled"`
	PublicMCPEnabled        bool   `json:"publicMcpEnabled"`
	Host                    string `json:"host"`
	Port                    string `json:"port"`
	BasePath                string `json:"basePath"`
	MarkdownLinkRootPrefix  string `json:"markdownLinkRootPrefix"`
	PublicAccess            bool   `json:"publicAccess"`
	AllowInsecure           bool   `json:"allowInsecure"`
	AccessTokenTimeout      string `json:"accessTokenTimeout"`
	RefreshTokenTimeout     string `json:"refreshTokenTimeout"`
	InjectCodeInHeaderHash  string `json:"injectCodeInHeaderHash,omitempty"`
	CustomStylesheet        string `json:"customStylesheet"`
	LogTarget               string `json:"logTarget"`
	LogFile                 string `json:"logFile"`
	HideLinkMetadataSection bool   `json:"hideLinkMetadataSection"`
	MaxAssetUploadSizeBytes int64  `json:"maxAssetUploadSizeBytes"`
	EnableRevision          bool   `json:"enableRevision"`
	EnableWorkspaceSync     bool   `json:"enableWorkspaceSync"`
	EnableLinkRefactor      bool   `json:"enableLinkRefactor"`
	MaxRevisionHistory      int    `json:"maxRevisionHistory"`
	EnableHTTPRemoteUser    bool   `json:"enableHttpRemoteUser"`
	HTTPRemoteUserHeader    string `json:"httpRemoteUserHeader"`
	TrustedProxyIPs         string `json:"trustedProxyIps"`
	HTTPRemoteUserLogoutURL string `json:"httpRemoteUserLogoutUrl"`
	DisableRequestLog       bool   `json:"disableRequestLog"`
	DaemonIdleTimeout       string `json:"daemonIdleTimeout"`
}

type Descriptor struct {
	SchemaVersion    int          `json:"schemaVersion"`
	RuntimeStack     string       `json:"runtimeStack,omitempty"`
	Role             RoleName     `json:"role,omitempty"`
	WorkspaceID      string       `json:"workspaceId,omitempty"`
	PID              int          `json:"pid"`
	StartedAt        time.Time    `json:"startedAt"`
	DataDir          string       `json:"dataDir"`
	RootDir          string       `json:"rootDir"`
	PublicURL        string       `json:"publicUrl"`
	PublicMCPEnabled bool         `json:"publicMcpEnabled"`
	BasePath         string       `json:"basePath"`
	ControlURL       string       `json:"controlUrl"`
	PrivateMCPURL    string       `json:"privateMcpUrl,omitempty"`
	PrivateMCPToken  string       `json:"privateMcpToken,omitempty"`
	ConfigHash       string       `json:"configHash"`
	IdleTimeout      string       `json:"idleTimeout"`
	ControlToken     string       `json:"controlToken"`
	Config           Config       `json:"config"`
	Roles            []RoleHealth `json:"roles,omitempty"`
}

type Mismatch struct {
	Field string
	Want  string
	Got   string
}

func CanonicalizeProject(dataDir, rootDir string) (string, string, error) {
	canonicalData, err := canonicalPath(dataDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve data dir: %w", err)
	}
	canonicalRoot, err := canonicalPath(rootDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve root dir: %w", err)
	}
	return canonicalData, canonicalRoot, nil
}

func DescriptorPath(dataDir string) string {
	return filepath.Join(dataDir, ".leafwiki", DescriptorFileName)
}

func GlobalDescriptorPath(runtimeDir string, role RoleName) string {
	return filepath.Join(runtimeDir, string(role)+".json")
}

func ConfigHash(cfg Config) (string, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func HashSecret(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(trimmed))
	return hex.EncodeToString(sum[:])
}

func CompareConfig(owner Config, requested Config) []Mismatch {
	ownerValue := reflect.ValueOf(owner)
	requestedValue := reflect.ValueOf(requested)
	cfgType := ownerValue.Type()
	var mismatches []Mismatch
	for i := 0; i < cfgType.NumField(); i++ {
		field := cfgType.Field(i)
		if field.PkgPath != "" {
			continue
		}
		ownerField := ownerValue.Field(i).Interface()
		requestedField := requestedValue.Field(i).Interface()
		if reflect.DeepEqual(ownerField, requestedField) {
			continue
		}
		mismatches = append(mismatches, Mismatch{
			Field: configFieldName(field),
			Want:  redactedValue(field.Name, ownerField),
			Got:   redactedValue(field.Name, requestedField),
		})
	}
	return mismatches
}

func FormatConfigMismatch(mismatches []Mismatch) string {
	if len(mismatches) == 0 {
		return "project daemon config mismatch"
	}
	parts := make([]string, 0, len(mismatches))
	for _, mismatch := range mismatches {
		if mismatch.Want == "" && mismatch.Got == "" {
			parts = append(parts, mismatch.Field)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s owner=%s requested=%s", mismatch.Field, mismatch.Want, mismatch.Got))
	}
	return "project daemon config mismatch: " + strings.Join(parts, "; ")
}

func RandomToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func canonicalPath(path string) (string, error) {
	absPath, err := filepath.Abs(filepath.Clean(strings.TrimSpace(path)))
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absPath)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}

	current := absPath
	var suffix []string
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(absPath), nil
		}
		suffix = append([]string{filepath.Base(current)}, suffix...)
		current = parent
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			parts := append([]string{resolved}, suffix...)
			return filepath.Clean(filepath.Join(parts...)), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
	}
}

func configFieldName(field reflect.StructField) string {
	name := field.Name
	if tag := field.Tag.Get("json"); tag != "" {
		if tagName := strings.Split(tag, ",")[0]; tagName != "" && tagName != "-" {
			name = tagName
		}
	}
	var b strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

func redactedValue(field string, value any) string {
	switch field {
	case "InjectCodeInHeaderHash", "PrivateMCPToken":
		if fmt.Sprint(value) == "" {
			return "empty"
		}
		return "redacted"
	default:
		if fmt.Sprint(value) == "" {
			return `""`
		}
		return fmt.Sprint(value)
	}
}
