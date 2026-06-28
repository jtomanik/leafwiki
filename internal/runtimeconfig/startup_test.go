package runtimeconfig

import (
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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

		err := ValidateRawConfigFlagUsage([]string{"--config"})
		var usage ConfigUsageError
		Expect(errors.As(err, &usage)).To(BeTrue())
		Expect(usage.Error()).To(Equal("--config requires a path"))
		Expect(usage.Code).To(Equal(errCodeRuntimeConfigUsage))

		Expect(ValidateRawConfigFlagUsage([]string{"--config="})).To(MatchError("--config requires a path"))
		Expect(ValidateRawConfigFlagUsage([]string{"--config", "--port"})).To(MatchError("--config requires a path"))
	})

	It("reports mixed config-mode arguments with the displayed flag form", func() {
		Expect(ConfigFlagMixError{Flag: "--port"}.Error()).To(Equal("--config cannot be combined with --port"))
		Expect(ValidateConfigModeArgs([]string{"wiki"})).To(Succeed())
		Expect(ValidateConfigModeArgs([]string{"help"})).To(MatchError("--config cannot be combined with help"))
		Expect(ValidateConfigModeArgs([]string{"--port=8080"})).To(MatchError("--config cannot be combined with --port"))
		Expect(ValidateConfigModeArgs([]string{"-p=8080"})).To(MatchError("--config cannot be combined with -p"))
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
		Expect(ApplyYAMLConfigFile(fs, "", nil)).To(MatchError("--config requires a path"))
		Expect(ApplyYAMLConfigFile(fs, "--port", nil)).To(MatchError("--config requires a path"))
		Expect(ApplyYAMLConfigFile(fs, "-p=8080", nil)).To(MatchError("--config requires a path"))
	})

	It("rejects combining config files with other visited flags", func() {
		fs := newRuntimeFlagSet()
		path := writeRuntimeConfig("host: 0.0.0.0\n")

		Expect(ApplyYAMLConfigFile(fs, path, map[string]bool{"config": true})).To(Succeed())
		Expect(ApplyYAMLConfigFile(fs, path, map[string]bool{"port": true})).To(MatchError("--config cannot be combined with --port"))
	})

	DescribeTable("rejects malformed config files",
		func(contents string, want string) {
			err := ApplyYAMLConfigPath(newRuntimeFlagSet(), map[string]bool{}, writeRuntimeConfig(contents), "test config")

			Expect(err).To(MatchError(ContainSubstring(want)))
		},
		Entry("invalid YAML", "host: [", "parse test config"),
		Entry("non-mapping root", "- host\n", "root must be a YAML mapping"),
		Entry("non-scalar key", "? [host]\n: 127.0.0.1\n", "keys must be scalar strings"),
		Entry("duplicate key", "host: 127.0.0.1\nhost: 0.0.0.0\n", "duplicate test config key"),
		Entry("unknown key", "unknown: value\n", "unknown test config key"),
		Entry("sequence value", "host:\n  - 127.0.0.1\n", "requires a non-null scalar value"),
		Entry("null value", "host: null\n", "requires a non-null scalar value"),
		Entry("invalid flag value", "disable-auth: nope\n", "invalid test config value"),
	)

	It("reports file read errors with the config source", func() {
		err := ApplyYAMLConfigPath(newRuntimeFlagSet(), map[string]bool{}, filepath.Join(GinkgoT().TempDir(), "missing.yml"), "test config")

		Expect(err).To(MatchError(ContainSubstring("read test config")))
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
		Expect(err).To(MatchError(ContainSubstring("resolve home directory")))

		_, err = DefaultDaemonServiceConfigPath()
		Expect(err).To(MatchError(ContainSubstring("resolve home directory")))

		Expect(ApplyDaemonServiceConfig(newRuntimeFlagSet(), map[string]bool{}, []string{"daemon"})).To(MatchError(ContainSubstring("resolve home directory")))
		Expect(ApplyDaemonServiceDefaults(newRuntimeFlagSet(), map[string]bool{})).To(MatchError(ContainSubstring("resolve home directory")))
	})

	It("reports daemon service config usage errors", func() {
		fs := newRuntimeFlagSet()
		Expect(ApplyDaemonServiceConfig(fs, map[string]bool{}, []string{"daemon", "extra"})).To(MatchError("leafwiki daemon does not accept additional positional arguments"))
		Expect(ApplyDaemonServiceConfig(fs, map[string]bool{"port": true}, []string{"daemon"})).To(MatchError("leafwiki daemon reads ~/.leafwiki/leafwiki.yml; move --port into the service config file"))
	})

	It("wraps the missing default daemon service config path", func() {
		home := GinkgoT().TempDir()
		GinkgoT().Setenv("HOME", home)

		err := ApplyDaemonServiceConfig(newRuntimeFlagSet(), map[string]bool{}, []string{"daemon"})

		var missing DaemonServiceConfigMissingError
		Expect(errors.As(err, &missing)).To(BeTrue())
		Expect(missing.Path).To(Equal(filepath.Join(home, ".leafwiki", "leafwiki.yml")))
		Expect(missing.Error()).To(ContainSubstring("leafwiki.yml is required"))
		Expect(errors.Is(err, missing.Unwrap())).To(BeTrue())
	})

	It("returns service config parse errors before applying defaults", func() {
		home := GinkgoT().TempDir()
		GinkgoT().Setenv("HOME", home)
		Expect(os.MkdirAll(filepath.Join(home, ".leafwiki"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(home, ".leafwiki", "leafwiki.yml"), []byte("unknown: value\n"), 0o644)).To(Succeed())

		err := ApplyDaemonServiceConfig(newRuntimeFlagSet(), map[string]bool{}, []string{"daemon"})

		Expect(err).To(MatchError(ContainSubstring("unknown service config key")))
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
		Expect(ApplyDaemonServiceDefaults(flag.NewFlagSet("missing-data-dir", flag.ContinueOnError), map[string]bool{})).To(MatchError(ContainSubstring("no such flag -data-dir")))

		fsWithoutRoot := flag.NewFlagSet("missing-root-dir", flag.ContinueOnError)
		fsWithoutRoot.SetOutput(io.Discard)
		fsWithoutRoot.String("data-dir", "", "")
		Expect(ApplyDaemonServiceDefaults(fsWithoutRoot, map[string]bool{})).To(MatchError(ContainSubstring("no such flag -root-dir")))

		fsWithoutDefault := flag.NewFlagSet("missing-default", flag.ContinueOnError)
		fsWithoutDefault.SetOutput(io.Discard)
		fsWithoutDefault.String("data-dir", "", "")
		fsWithoutDefault.String("root-dir", "", "")
		Expect(ApplyDaemonServiceDefaults(fsWithoutDefault, map[string]bool{})).To(MatchError(ContainSubstring("set service default")))

		fsWithoutLogFile := newRuntimeFlagSet()
		fsWithoutLogFile = flag.NewFlagSet("missing-log-file", flag.ContinueOnError)
		fsWithoutLogFile.SetOutput(io.Discard)
		for name := range ConfigFileFlagNames() {
			if name == "log-file" {
				continue
			}
			fsWithoutLogFile.String(name, "", "")
		}
		visited := map[string]bool{}
		for name := range ConfigFileFlagNames() {
			if name != "log-file" {
				visited[name] = true
			}
		}
		Expect(fsWithoutLogFile.Set("log-target", "file")).To(Succeed())
		Expect(ApplyDaemonServiceDefaults(fsWithoutLogFile, visited)).To(MatchError(ContainSubstring("set service default for \"log-file\"")))
	})
})

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
