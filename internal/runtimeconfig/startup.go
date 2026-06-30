package runtimeconfig

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	"gopkg.in/yaml.v3"
)

type ConfigFlagMixError struct {
	Flag string
}

func (err ConfigFlagMixError) Error() string {
	return "--config cannot be combined with " + err.Flag
}

type ConfigUsageError struct {
	Code            sharederrors.ErrorCode
	MessageID       sharederrors.MessageID
	Reason          ConfigUsageReason
	RenderedMessage string
}

func (err ConfigUsageError) Error() string {
	return err.RenderedMessage
}

const errCodeRuntimeConfigUsage sharederrors.ErrorCode = "runtime_config_usage"

type ConfigUsageReason string

const (
	ConfigUsageReasonConfigPathRequired     ConfigUsageReason = "config_path_required"
	ConfigUsageReasonDaemonPositionalArgs   ConfigUsageReason = "daemon_positional_args"
	ConfigUsageReasonDaemonFlagInConfigFile ConfigUsageReason = "daemon_flag_in_config_file"
)

func newConfigUsageError(reason ConfigUsageReason, message string) ConfigUsageError {
	return ConfigUsageError{
		Code:            errCodeRuntimeConfigUsage,
		MessageID:       sharederrors.MessageIDForCode(errCodeRuntimeConfigUsage),
		Reason:          reason,
		RenderedMessage: message,
	}
}

type ConfigFileErrorReason string

const (
	ConfigFileErrorReasonRead             ConfigFileErrorReason = "read"
	ConfigFileErrorReasonParse            ConfigFileErrorReason = "parse"
	ConfigFileErrorReasonRootMapping      ConfigFileErrorReason = "root_mapping"
	ConfigFileErrorReasonScalarKey        ConfigFileErrorReason = "scalar_key"
	ConfigFileErrorReasonDuplicateKey     ConfigFileErrorReason = "duplicate_key"
	ConfigFileErrorReasonUnknownKey       ConfigFileErrorReason = "unknown_key"
	ConfigFileErrorReasonScalarValue      ConfigFileErrorReason = "scalar_value"
	ConfigFileErrorReasonInvalidFlagValue ConfigFileErrorReason = "invalid_flag_value"
)

type ConfigFileError struct {
	Reason ConfigFileErrorReason
	Source string
	Path   string
	Key    string
	Err    error
}

func (err ConfigFileError) Error() string {
	switch err.Reason {
	case ConfigFileErrorReasonRead:
		return fmt.Sprintf("read %s %q: %v", err.Source, err.Path, err.Err)
	case ConfigFileErrorReasonParse:
		return fmt.Sprintf("parse %s %q: %v", err.Source, err.Path, err.Err)
	case ConfigFileErrorReasonRootMapping:
		return fmt.Sprintf("%s root must be a YAML mapping", err.Source)
	case ConfigFileErrorReasonScalarKey:
		return fmt.Sprintf("%s keys must be scalar strings", err.Source)
	case ConfigFileErrorReasonDuplicateKey:
		return fmt.Sprintf("duplicate %s key %q", err.Source, err.Key)
	case ConfigFileErrorReasonUnknownKey:
		return fmt.Sprintf("unknown %s key %q", err.Source, err.Key)
	case ConfigFileErrorReasonScalarValue:
		return fmt.Sprintf("%s key %q requires a non-null scalar value", err.Source, err.Key)
	case ConfigFileErrorReasonInvalidFlagValue:
		return fmt.Sprintf("invalid %s value for %q: %v", err.Source, err.Key, err.Err)
	default:
		return "invalid config file"
	}
}

func (err ConfigFileError) Unwrap() error {
	return err.Err
}

type DaemonServiceConfigMissingError struct {
	Path string
	Err  error
}

func (err DaemonServiceConfigMissingError) Error() string {
	return fmt.Sprintf("%s is required: %v", err.Path, err.Err)
}

func (err DaemonServiceConfigMissingError) Unwrap() error {
	return err.Err
}

func ValidateRawConfigFlagUsage(args []string) error {
	skipNext := false
	for i, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		name, hasInlineValue, ok := RawFlagName(arg)
		if !ok {
			continue
		}
		if name == "config" {
			if hasInlineValue {
				_, value, _ := strings.Cut(strings.TrimLeft(arg, "-"), "=")
				if IsInvalidBareConfigPathValue(value) {
					return newConfigUsageError(ConfigUsageReasonConfigPathRequired, "--config requires a path")
				}
			} else if i+1 >= len(args) || IsInvalidBareConfigPathValue(args[i+1]) {
				return newConfigUsageError(ConfigUsageReasonConfigPathRequired, "--config requires a path")
			}
		}
		if _, takesValue := ValueTakingFlagNames()[name]; takesValue && !hasInlineValue {
			skipNext = true
		}
	}
	return nil
}

func IsInvalidBareConfigPathValue(path string) bool {
	path = strings.TrimSpace(path)
	return path == "" || strings.HasPrefix(path, "-")
}

func RawFlagName(arg string) (string, bool, bool) {
	if !strings.HasPrefix(arg, "-") || arg == "-" {
		return "", false, false
	}
	var trimmed string
	if strings.HasPrefix(arg, "--") {
		trimmed = strings.TrimPrefix(arg, "--")
		if strings.HasPrefix(trimmed, "-") {
			return "", false, false
		}
	} else {
		trimmed = strings.TrimPrefix(arg, "-")
	}
	if trimmed == "" {
		return "", false, false
	}
	name, value, hasInlineValue := strings.Cut(trimmed, "=")
	if strings.TrimSpace(name) == "" {
		return "", false, false
	}
	_ = value
	return name, hasInlineValue, true
}

func ValueTakingFlagNames() map[string]struct{} {
	return map[string]struct{}{
		"access-token-timeout":         {},
		"admin-password":               {},
		"api-key":                      {},
		"base-path":                    {},
		"config":                       {},
		"custom-stylesheet":            {},
		"daemon-idle-timeout":          {},
		"data-dir":                     {},
		"host":                         {},
		"http-remote-user-header-name": {},
		"http-remote-user-logout-url":  {},
		"inject-code-in-header":        {},
		"internal-project-daemon":      {},
		"internal-runtime-role":        {},
		"jwt-secret":                   {},
		"log-file":                     {},
		"log-target":                   {},
		"markdown-link-root-prefix":    {},
		"max-asset-upload-size":        {},
		"mcp":                          {},
		"port":                         {},
		"refresh-token-timeout":        {},
		"root-dir":                     {},
		"trusted-proxy-ips":            {},
	}
}

func IsDaemonCommand(args []string) bool {
	return len(args) > 0 && args[0] == "daemon"
}

func IsDaemonHelpCommand(args []string) bool {
	if len(args) != 2 || args[0] != "daemon" {
		return false
	}
	return args[1] == "--help" || args[1] == "-h" || args[1] == "help"
}

func DefaultDaemonServiceDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".leafwiki"), nil
}

func DefaultDaemonServiceConfigPath() (string, error) {
	dataDir, err := DefaultDaemonServiceDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "leafwiki.yml"), nil
}

func ApplyYAMLConfigFile(fs *flag.FlagSet, configPath string, visited map[string]bool) error {
	path := strings.TrimSpace(configPath)
	if IsInvalidBareConfigPathValue(path) {
		return newConfigUsageError(ConfigUsageReasonConfigPathRequired, "--config requires a path")
	}
	for name := range visited {
		if name == "config" {
			continue
		}
		return ConfigFlagMixError{Flag: "--" + name}
	}
	return ApplyYAMLConfigPath(fs, visited, path, "--config")
}

func ApplyYAMLConfigPath(fs *flag.FlagSet, visited map[string]bool, path string, source string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ConfigFileError{Reason: ConfigFileErrorReasonRead, Source: source, Path: path, Err: err}
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return ConfigFileError{Reason: ConfigFileErrorReasonParse, Source: source, Path: path, Err: err}
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return ConfigFileError{Reason: ConfigFileErrorReasonRootMapping, Source: source, Path: path}
	}

	root := doc.Content[0]
	seen := map[string]struct{}{}
	allowed := ConfigFileFlagNames()
	for i := 0; i < len(root.Content); i += 2 {
		keyNode := root.Content[i]
		valueNode := root.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			return ConfigFileError{Reason: ConfigFileErrorReasonScalarKey, Source: source, Path: path}
		}
		key := keyNode.Value
		if _, ok := seen[key]; ok {
			return ConfigFileError{Reason: ConfigFileErrorReasonDuplicateKey, Source: source, Path: path, Key: key}
		}
		seen[key] = struct{}{}
		if _, ok := allowed[key]; !ok {
			return ConfigFileError{Reason: ConfigFileErrorReasonUnknownKey, Source: source, Path: path, Key: key}
		}
		if valueNode.Kind != yaml.ScalarNode || valueNode.Tag == "!!null" {
			return ConfigFileError{Reason: ConfigFileErrorReasonScalarValue, Source: source, Path: path, Key: key}
		}
		if err := fs.Set(key, valueNode.Value); err != nil {
			return ConfigFileError{Reason: ConfigFileErrorReasonInvalidFlagValue, Source: source, Path: path, Key: key, Err: err}
		}
		visited[key] = true
	}
	return nil
}

func ApplyDaemonServiceConfig(fs *flag.FlagSet, visited map[string]bool, args []string) error {
	if len(args) != 1 || args[0] != "daemon" {
		return newConfigUsageError(ConfigUsageReasonDaemonPositionalArgs, "leafwiki daemon does not accept additional positional arguments")
	}
	for name := range visited {
		return newConfigUsageError(ConfigUsageReasonDaemonFlagInConfigFile, fmt.Sprintf("leafwiki daemon reads ~/.leafwiki/leafwiki.yml; move --%s into the service config file", name))
	}
	path, err := DefaultDaemonServiceConfigPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return DaemonServiceConfigMissingError{Path: path, Err: err}
	}
	if err := ApplyYAMLConfigPath(fs, visited, path, "service config"); err != nil {
		return err
	}
	return ApplyDaemonServiceDefaults(fs, visited)
}

func ApplyDaemonServiceDefaults(fs *flag.FlagSet, visited map[string]bool) error {
	if !visited["data-dir"] {
		dataDir, err := DefaultDaemonServiceDataDir()
		if err != nil {
			return err
		}
		if err := fs.Set("data-dir", dataDir); err != nil {
			return err
		}
		visited["data-dir"] = true
	}
	if !visited["root-dir"] {
		dataDir := fs.Lookup("data-dir").Value.String()
		rootDir := filepath.Join(dataDir, "root")
		if err := fs.Set("root-dir", rootDir); err != nil {
			return err
		}
		visited["root-dir"] = true
	}
	defaults := map[string]string{
		"access-token-timeout":         "15m",
		"admin-password":               "",
		"api-key":                      "",
		"base-path":                    "",
		"custom-stylesheet":            "",
		"daemon-idle-timeout":          projectdaemon.DefaultIdleTimeout.String(),
		"disable-auth":                 "false",
		"disable-request-log":          "false",
		"enable-http-remote-user":      "false",
		"enable-link-refactor":         "false",
		"hide-link-metadata-section":   "false",
		"host":                         "127.0.0.1",
		"http-remote-user-header-name": "Remote-User",
		"http-remote-user-logout-url":  "",
		"inject-code-in-header":        "",
		"jwt-secret":                   "",
		"log-target":                   "file",
		"markdown-link-root-prefix":    "",
		"max-asset-upload-size":        "50MiB",
		"mcp":                          "none",
		"port":                         "8080",
		"public-access":                "false",
		"allow-insecure":               "false",
		"refresh-token-timeout":        "168h",
		"trusted-proxy-ips":            "",
	}
	for name, value := range defaults {
		if visited[name] {
			continue
		}
		if err := fs.Set(name, value); err != nil {
			return fmt.Errorf("set service default for %q: %w", name, err)
		}
		visited[name] = true
	}
	if !visited["log-file"] && strings.EqualFold(strings.TrimSpace(fs.Lookup("log-target").Value.String()), string(leaflogging.TargetFile)) {
		if err := fs.Set("log-file", ""); err != nil {
			return fmt.Errorf("set service default for %q: %w", "log-file", err)
		}
		visited["log-file"] = true
	}
	return nil
}

func ValidateConfigModeArgs(args []string) error {
	for _, arg := range args {
		if arg == "help" {
			return ConfigFlagMixError{Flag: "help"}
		}
		name, _, ok := RawFlagName(arg)
		if !ok {
			continue
		}
		return ConfigFlagMixError{Flag: configModeFlagDisplay(arg, name)}
	}
	return nil
}

func ConfigFileFlagNames() map[string]struct{} {
	return map[string]struct{}{
		"access-token-timeout":         {},
		"admin-password":               {},
		"api-key":                      {},
		"base-path":                    {},
		"custom-stylesheet":            {},
		"daemon-idle-timeout":          {},
		"data-dir":                     {},
		"disable-auth":                 {},
		"disable-request-log":          {},
		"enable-http-remote-user":      {},
		"enable-link-refactor":         {},
		"hide-link-metadata-section":   {},
		"host":                         {},
		"http-remote-user-header-name": {},
		"http-remote-user-logout-url":  {},
		"inject-code-in-header":        {},
		"jwt-secret":                   {},
		"log-file":                     {},
		"log-target":                   {},
		"markdown-link-root-prefix":    {},
		"max-asset-upload-size":        {},
		"mcp":                          {},
		"port":                         {},
		"public-access":                {},
		"allow-insecure":               {},
		"refresh-token-timeout":        {},
		"root-dir":                     {},
		"trusted-proxy-ips":            {},
	}
}

func configModeFlagDisplay(arg string, name string) string {
	if strings.HasPrefix(arg, "--") {
		return "--" + name
	}
	return strings.SplitN(arg, "=", 2)[0]
}
