package runtimeconfig

import (
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

var (
	errSetRootDirDefaultFailed = errors.New("set root-dir default failed")
	errSetServiceDefaultFailed = errors.New("set service default failed")
	errSetLogFileDefaultFailed = errors.New("set log-file default failed")
)

func matchRuntimeConfigUsageError() types.GomegaMatcher {
	return WithTransform(func(err error) ConfigUsageError {
		var usage ConfigUsageError
		_ = errors.As(err, &usage)
		return usage
	}, testmatchers.HaveStructuredError(errCodeRuntimeConfigUsage, sharederrors.MessageIDForCode(errCodeRuntimeConfigUsage)))
}

func matchConfigFileError(reason ConfigFileErrorReason, key string) types.GomegaMatcher {
	fields := gstruct.Fields{
		"Reason": Equal(reason),
	}
	if key != "" {
		fields["Key"] = Equal(key)
	}
	return WithTransform(func(err error) ConfigFileError {
		var configErr ConfigFileError
		_ = errors.As(err, &configErr)
		return configErr
	}, gstruct.MatchFields(gstruct.IgnoreExtras, fields))
}

func matchDaemonServiceConfigMissing(path string) types.GomegaMatcher {
	return WithTransform(func(err error) DaemonServiceConfigMissingError {
		var missing DaemonServiceConfigMissingError
		_ = errors.As(err, &missing)
		return missing
	}, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Path": Equal(path),
		"Err":  MatchError(os.ErrNotExist),
	}))
}

var _ = Describe("startup config validation", func() {
	DescribeTable("parses raw flag names",
		func(arg string, wantName string, wantInline bool, wantOK bool) {
			name, inline, ok := RawFlagName(arg)

			Expect(name).To(Equal(wantName))
			Expect(inline).To(Equal(wantInline))
			Expect(ok).To(Equal(wantOK))
		},
		Entry("positional", "leafwiki.yml", "", false, false),
		Entry("bare dash", "-", "", false, false),
		Entry("long flag", "--config", "config", false, true),
		Entry("long flag with inline value", "--config=leafwiki.yml", "config", true, true),
		Entry("rejects triple dash", "---config", "", false, false),
		Entry("rejects empty long flag", "--", "", false, false),
		Entry("rejects empty short flag", "-=value", "", false, false),
		Entry("short flag", "-p=8080", "p", true, true),
	)

	DescribeTable("identifies invalid bare config path values",
		func(value string, want bool) {
			Expect(IsInvalidBareConfigPathValue(value)).To(Equal(want))
		},
		Entry("empty", "", true),
		Entry("whitespace", " \t ", true),
		Entry("looks like another flag", "--port", true),
		Entry("path", "leafwiki.yml", false),
	)

	It("validates raw --config usage before flag parsing", func() {
		Expect(ValidateRawConfigFlagUsage([]string{"serve", "--host", "--config", "leafwiki.yml"})).To(Succeed())
		Expect(ValidateRawConfigFlagUsage([]string{"--config=leafwiki.yml"})).To(Succeed())

		Expect(ValidateRawConfigFlagUsage([]string{"--config"})).To(matchRuntimeConfigUsageError())
		Expect(ValidateRawConfigFlagUsage([]string{"--config="})).To(matchRuntimeConfigUsageError())
		Expect(ValidateRawConfigFlagUsage([]string{"--config", "--port"})).To(matchRuntimeConfigUsageError())
	})

	It("reports mixed config-mode arguments with the displayed flag form", func() {
		Expect(ConfigFlagMixError{Flag: "--port"}.Flag).To(Equal("--port"))
		Expect(ValidateConfigModeArgs([]string{"wiki"})).To(Succeed())
		Expect(ValidateConfigModeArgs([]string{"help"})).To(MatchError(ConfigFlagMixError{Flag: "help"}))
		Expect(ValidateConfigModeArgs([]string{"--port=8080"})).To(MatchError(ConfigFlagMixError{Flag: "--port"}))
		Expect(ValidateConfigModeArgs([]string{"-p=8080"})).To(MatchError(ConfigFlagMixError{Flag: "-p"}))
	})

	It("classifies daemon commands and help invocations", func() {
		Expect(IsDaemonCommand([]string{"daemon"})).To(BeTrue())
		Expect(IsDaemonCommand([]string{"serve"})).To(BeFalse())
		Expect(IsDaemonHelpCommand([]string{"daemon", "--help"})).To(BeTrue())
		Expect(IsDaemonHelpCommand([]string{"daemon", "-h"})).To(BeTrue())
		Expect(IsDaemonHelpCommand([]string{"daemon", "help"})).To(BeTrue())
		Expect(IsDaemonHelpCommand([]string{"daemon"})).To(BeFalse())
		Expect(IsDaemonHelpCommand([]string{"serve", "help"})).To(BeFalse())
	})

	It("exposes the supported config and value-taking flag names", func() {
		Expect(ValueTakingFlagNames()).To(HaveKey("config"))
		Expect(ValueTakingFlagNames()).To(HaveKey("port"))
		Expect(ConfigFileFlagNames()).To(HaveKey("disable-auth"))
		Expect(ConfigFileFlagNames()).NotTo(HaveKey("config"))
	})
})

var _ = Describe("YAML startup config", func() {
	It("applies scalar YAML config values into a flag set", func() {
		fs := newRuntimeFlagSet()
		visited := map[string]bool{}
		path := writeRuntimeConfig("host: 0.0.0.0\nport: \"9090\"\ndisable-auth: true\nmcp: http,stdio\n")

		Expect(ApplyYAMLConfigFile(fs, path, visited)).To(Succeed())

		Expect(fs.Lookup("host").Value.String()).To(Equal("0.0.0.0"))
		Expect(fs.Lookup("port").Value.String()).To(Equal("9090"))
		Expect(fs.Lookup("disable-auth").Value.String()).To(Equal("true"))
		Expect(fs.Lookup("mcp").Value.String()).To(Equal("http,stdio"))
		Expect(visited).To(SatisfyAll(
			HaveKey("host"),
			HaveKey("port"),
			HaveKey("disable-auth"),
			HaveKey("mcp"),
		))
	})

	It("rejects invalid --config path usage before reading the file", func() {
		fs := newRuntimeFlagSet()
		Expect(ApplyYAMLConfigFile(fs, "", nil)).To(matchRuntimeConfigUsageError())
		Expect(ApplyYAMLConfigFile(fs, "--port", nil)).To(matchRuntimeConfigUsageError())
		Expect(ApplyYAMLConfigFile(fs, "-p=8080", nil)).To(matchRuntimeConfigUsageError())
	})

	It("rejects combining config files with other visited flags", func() {
		fs := newRuntimeFlagSet()
		path := writeRuntimeConfig("host: 0.0.0.0\n")

		Expect(ApplyYAMLConfigFile(fs, path, map[string]bool{"config": true})).To(Succeed())
		Expect(ApplyYAMLConfigFile(fs, path, map[string]bool{"port": true})).To(MatchError(ConfigFlagMixError{Flag: "--port"}))
	})

	type configFileValidationCase struct {
		contents string
		reason   ConfigFileErrorReason
		key      string
	}

	DescribeTable("rejects malformed config files",
		func(tc configFileValidationCase) {
			err := ApplyYAMLConfigPath(newRuntimeFlagSet(), map[string]bool{}, writeRuntimeConfig(tc.contents), "test config")

			Expect(err).To(matchConfigFileError(tc.reason, tc.key))
		},
		Entry("invalid YAML", configFileValidationCase{contents: "host: [", reason: ConfigFileErrorReasonParse}),
		Entry("non-mapping root", configFileValidationCase{contents: "- host\n", reason: ConfigFileErrorReasonRootMapping}),
		Entry("non-scalar key", configFileValidationCase{contents: "? [host]\n: 127.0.0.1\n", reason: ConfigFileErrorReasonScalarKey}),
		Entry("duplicate key", configFileValidationCase{contents: "host: 127.0.0.1\nhost: 0.0.0.0\n", reason: ConfigFileErrorReasonDuplicateKey, key: "host"}),
		Entry("unknown key", configFileValidationCase{contents: "unknown: value\n", reason: ConfigFileErrorReasonUnknownKey, key: "unknown"}),
		Entry("sequence value", configFileValidationCase{contents: "host:\n  - 127.0.0.1\n", reason: ConfigFileErrorReasonScalarValue, key: "host"}),
		Entry("null value", configFileValidationCase{contents: "host: null\n", reason: ConfigFileErrorReasonScalarValue, key: "host"}),
		Entry("invalid flag value", configFileValidationCase{contents: "disable-auth: nope\n", reason: ConfigFileErrorReasonInvalidFlagValue, key: "disable-auth"}),
	)

	It("reports file read errors with the config source", func() {
		err := ApplyYAMLConfigPath(newRuntimeFlagSet(), map[string]bool{}, filepath.Join(GinkgoT().TempDir(), "missing.yml"), "test config")

		Expect(err).To(matchConfigFileError(ConfigFileErrorReasonRead, ""))
	})
})

var _ = Describe("daemon service startup config", func() {
	It("resolves default daemon paths from HOME", func() {
		home := GinkgoT().TempDir()
		GinkgoT().Setenv("HOME", home)

		dataDir, err := DefaultDaemonServiceDataDir()
		Expect(err).NotTo(HaveOccurred())
		Expect(dataDir).To(Equal(filepath.Join(home, ".leafwiki")))

		configPath, err := DefaultDaemonServiceConfigPath()
		Expect(err).NotTo(HaveOccurred())
		Expect(configPath).To(Equal(filepath.Join(home, ".leafwiki", "leafwiki.yml")))
	})

	It("returns home resolution errors when daemon defaults require HOME", func() {
		GinkgoT().Setenv("HOME", "")

		_, err := DefaultDaemonServiceDataDir()
		Expect(err).To(HaveOccurred())

		_, err = DefaultDaemonServiceConfigPath()
		Expect(err).To(HaveOccurred())

		Expect(ApplyDaemonServiceConfig(newRuntimeFlagSet(), map[string]bool{}, []string{"daemon"})).To(HaveOccurred())
		Expect(ApplyDaemonServiceDefaults(newRuntimeFlagSet(), map[string]bool{})).To(HaveOccurred())
	})

	It("reports daemon service config usage errors", func() {
		fs := newRuntimeFlagSet()
		Expect(ApplyDaemonServiceConfig(fs, map[string]bool{}, []string{"daemon", "extra"})).To(matchRuntimeConfigUsageError())
		Expect(ApplyDaemonServiceConfig(fs, map[string]bool{"port": true}, []string{"daemon"})).To(matchRuntimeConfigUsageError())
	})

	It("wraps the missing default daemon service config path", func() {
		home := GinkgoT().TempDir()
		GinkgoT().Setenv("HOME", home)

		err := ApplyDaemonServiceConfig(newRuntimeFlagSet(), map[string]bool{}, []string{"daemon"})

		Expect(err).To(matchDaemonServiceConfigMissing(filepath.Join(home, ".leafwiki", "leafwiki.yml")))
	})

	It("returns service config parse errors before applying defaults", func() {
		home := GinkgoT().TempDir()
		GinkgoT().Setenv("HOME", home)
		Expect(os.MkdirAll(filepath.Join(home, ".leafwiki"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(home, ".leafwiki", "leafwiki.yml"), []byte("unknown: value\n"), 0o644)).To(Succeed())

		err := ApplyDaemonServiceConfig(newRuntimeFlagSet(), map[string]bool{}, []string{"daemon"})

		Expect(err).To(HaveOccurred())
	})

	It("applies daemon service config and fills daemon defaults", func() {
		home := GinkgoT().TempDir()
		GinkgoT().Setenv("HOME", home)
		Expect(os.MkdirAll(filepath.Join(home, ".leafwiki"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(home, ".leafwiki", "leafwiki.yml"), []byte("port: \"9090\"\nlog-target: stderr\n"), 0o644)).To(Succeed())
		fs := newRuntimeFlagSet()
		visited := map[string]bool{}

		Expect(ApplyDaemonServiceConfig(fs, visited, []string{"daemon"})).To(Succeed())

		Expect(fs.Lookup("port").Value.String()).To(Equal("9090"))
		Expect(fs.Lookup("data-dir").Value.String()).To(Equal(filepath.Join(home, ".leafwiki")))
		Expect(fs.Lookup("root-dir").Value.String()).To(Equal(filepath.Join(home, ".leafwiki", "root")))
		Expect(fs.Lookup("host").Value.String()).To(Equal("127.0.0.1"))
		Expect(fs.Lookup("log-target").Value.String()).To(Equal("stderr"))
		Expect(visited).To(HaveKey("root-dir"))
		Expect(visited).NotTo(HaveKey("log-file"))
	})

	It("preserves visited daemon defaults and sets log-file for file logging", func() {
		fs := newRuntimeFlagSet()
		Expect(fs.Set("data-dir", "/custom/data")).To(Succeed())
		Expect(fs.Set("root-dir", "/custom/root")).To(Succeed())
		Expect(fs.Set("log-target", "file")).To(Succeed())
		visited := map[string]bool{
			"data-dir":   true,
			"root-dir":   true,
			"log-target": true,
		}

		Expect(ApplyDaemonServiceDefaults(fs, visited)).To(Succeed())

		Expect(fs.Lookup("data-dir").Value.String()).To(Equal("/custom/data"))
		Expect(fs.Lookup("root-dir").Value.String()).To(Equal("/custom/root"))
		Expect(fs.Lookup("log-file").Value.String()).To(BeEmpty())
		Expect(visited).To(HaveKey("log-file"))
	})

	It("returns flag-set errors while applying daemon defaults", func() {
		Expect(ApplyDaemonServiceDefaults(flag.NewFlagSet("missing-data-dir", flag.ContinueOnError), map[string]bool{})).To(HaveOccurred())

		fsWithoutRoot := flag.NewFlagSet("failing-root-dir", flag.ContinueOnError)
		fsWithoutRoot.SetOutput(io.Discard)
		fsWithoutRoot.String("data-dir", "/data", "")
		fsWithoutRoot.Var(failingRuntimeFlagValue{err: errSetRootDirDefaultFailed}, "root-dir", "")
		Expect(ApplyDaemonServiceDefaults(fsWithoutRoot, map[string]bool{"data-dir": true})).To(MatchError(errSetRootDirDefaultFailed))

		fsWithoutDefault := flag.NewFlagSet("missing-default", flag.ContinueOnError)
		fsWithoutDefault.SetOutput(io.Discard)
		registerRuntimeConfigFlags(fsWithoutDefault, map[string]error{"host": errSetServiceDefaultFailed})
		Expect(ApplyDaemonServiceDefaults(fsWithoutDefault, map[string]bool{"data-dir": true, "root-dir": true})).To(MatchError(errSetServiceDefaultFailed))

		fsWithoutLogFile := flag.NewFlagSet("missing-log-file", flag.ContinueOnError)
		fsWithoutLogFile.SetOutput(io.Discard)
		registerRuntimeConfigFlags(fsWithoutLogFile, map[string]error{"log-file": errSetLogFileDefaultFailed})
		visited := map[string]bool{}
		for name := range ConfigFileFlagNames() {
			if name != "log-file" {
				visited[name] = true
			}
		}
		Expect(fsWithoutLogFile.Set("log-target", "file")).To(Succeed())
		Expect(ApplyDaemonServiceDefaults(fsWithoutLogFile, visited)).To(MatchError(errSetLogFileDefaultFailed))
	})
})

type failingRuntimeFlagValue struct {
	err error
}

func (value failingRuntimeFlagValue) String() string {
	return ""
}

func (value failingRuntimeFlagValue) Set(string) error {
	return value.err
}

func registerRuntimeConfigFlags(fs *flag.FlagSet, failing map[string]error) {
	GinkgoHelper()

	for name := range ConfigFileFlagNames() {
		if err, ok := failing[name]; ok {
			fs.Var(failingRuntimeFlagValue{err: err}, name, "")
			continue
		}
		fs.String(name, "", "")
	}
}

func newRuntimeFlagSet() *flag.FlagSet {
	GinkgoHelper()

	fs := flag.NewFlagSet("runtimeconfig-test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	boolFlags := map[string]struct{}{
		"allow-insecure":             {},
		"disable-auth":               {},
		"disable-request-log":        {},
		"enable-http-remote-user":    {},
		"enable-link-refactor":       {},
		"hide-link-metadata-section": {},
		"public-access":              {},
	}
	for name := range ConfigFileFlagNames() {
		if _, ok := boolFlags[name]; ok {
			fs.Bool(name, false, "")
			continue
		}
		fs.String(name, "", "")
	}
	return fs
}

func writeRuntimeConfig(contents string) string {
	GinkgoHelper()

	path := filepath.Join(GinkgoT().TempDir(), "leafwiki.yml")
	Expect(os.WriteFile(path, []byte(contents), 0o644)).To(Succeed())
	return path
}
