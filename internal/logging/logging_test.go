package logging

import (
	"bytes"
	"encoding/json"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
)

const (
	testLogStartupMessage = "Starting LeafWiki"
	testLogPreviousLine   = "previous line"
	testLogSlogMessage    = "slog message"
	testLogStdlibMessage  = "stdlib message"
	testLogInfoMessage    = "info message"
	testLogErrorMessage   = "error message"
)

func matchResolvedConfig(target Target, filePath types.GomegaMatcher, level slog.Level) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Target":   Equal(target),
		"FilePath": filePath,
		"Level":    Equal(level),
	})
}

func parseLogRecords(raw string) []map[string]any {
	GinkgoHelper()

	lines := strings.Split(strings.TrimSpace(raw), "\n")
	records := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var entry map[string]any
		Expect(json.Unmarshal([]byte(line), &entry)).To(Succeed())
		records = append(records, entry)
	}
	return records
}

var _ = Describe("logging configuration", func() {
	It("defaults file logging to the LeafWiki log path under the data directory", func() {
		dataDir := filepath.Join(loggingTempDir(), "data")

		cfg, err := Resolve(ConfigInput{DataDir: dataDir})

		Expect(err).NotTo(HaveOccurred())
		Expect(cfg).To(matchResolvedConfig(
			TargetFile,
			Equal(filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log")),
			slog.LevelInfo,
		))
	})

	It("resolves relative log file paths under the data directory", func() {
		dataDir := filepath.Join(loggingTempDir(), "data")

		cfg, err := Resolve(ConfigInput{
			DataDir:         dataDir,
			FilePath:        "logs/custom.log",
			FilePathSet:     true,
			Target:          "file",
			TargetSet:       true,
			LevelFromConfig: "debug",
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(cfg).To(matchResolvedConfig(TargetFile, Equal(filepath.Join(dataDir, "logs", "custom.log")), slog.LevelDebug))
	})

	It("rejects relative log file paths that escape the data directory", func() {
		_, err := Resolve(ConfigInput{
			DataDir:     filepath.Join(loggingTempDir(), "data"),
			FilePath:    "../leafwiki.log",
			FilePathSet: true,
		})

		Expect(err).To(MatchError(ErrLogFilePathOutsideDataDir))
	})

	It("preserves absolute log file paths", func() {
		logPath := filepath.Join(loggingTempDir(), "leafwiki.log")

		cfg, err := Resolve(ConfigInput{
			DataDir:     filepath.Join(loggingTempDir(), "data"),
			FilePath:    logPath,
			FilePathSet: true,
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.FilePath).To(Equal(logPath))
	})
})

type resolveErrorCase struct {
	input ConfigInput
	want  types.GomegaMatcher
}

var _ = DescribeTable("logging target validation",
	func(tc resolveErrorCase) {
		_, err := Resolve(tc.input)

		Expect(err).To(tc.want)
	},
	Entry("rejects an unknown log target", resolveErrorCase{
		input: ConfigInput{
			DataDir:   "data",
			Target:    "system",
			TargetSet: true,
		},
		want: MatchError(ErrInvalidLogTarget),
	}),
	Entry("rejects log file paths for stderr logging", resolveErrorCase{
		input: ConfigInput{
			DataDir:     "data",
			Target:      "stderr",
			TargetSet:   true,
			FilePath:    "custom.log",
			FilePathSet: true,
		},
		want: MatchError(ErrLogFileRequiresFileTarget),
	}),
)

var _ = Describe("opening loggers", func() {
	It("creates parent directories and appends JSON records to file logs", func() {
		logPath := filepath.Join(loggingTempDir(), "nested", "leafwiki.log")
		err := os.WriteFile(logPath, []byte("previous line\n"), 0o600)
		Expect(err).To(MatchError(os.ErrNotExist))

		logger, closer, err := Open(Config{
			Target:   TargetFile,
			FilePath: logPath,
			Level:    slog.LevelInfo,
		}, Streams{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			_ = closer.Close()
		})

		logger.Info(testLogStartupMessage, "address", "127.0.0.1:0")
		Expect(closer.Close()).To(Succeed())

		info, err := os.Stat(logPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))

		raw, err := os.ReadFile(logPath)
		Expect(err).NotTo(HaveOccurred())
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		Expect(lines).To(HaveLen(1))
		var entry map[string]any
		Expect(json.Unmarshal([]byte(lines[0]), &entry)).To(Succeed())
		for _, key := range []string{"time", "level", "msg", "source"} {
			Expect(entry).To(HaveKey(key))
		}
		Expect(entry).To(HaveKeyWithValue("msg", testLogStartupMessage))
	})

	It("appends JSON records to existing file logs", func() {
		logPath := filepath.Join(loggingTempDir(), "leafwiki.log")
		Expect(os.WriteFile(logPath, []byte(testLogPreviousLine+"\n"), 0o600)).To(Succeed())

		logger, closer, err := Open(Config{
			Target:   TargetFile,
			FilePath: logPath,
			Level:    slog.LevelInfo,
		}, Streams{})
		Expect(err).NotTo(HaveOccurred())
		logger.Info(testLogStartupMessage)
		Expect(closer.Close()).To(Succeed())

		raw, err := os.ReadFile(logPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(SatisfyAll(
			ContainSubstring(testLogPreviousLine),
			ContainSubstring(testLogStartupMessage),
		))
	})

	It("returns visible errors when file log parent creation cannot proceed", func() {
		parentFile := filepath.Join(loggingTempDir(), "not-a-directory")
		Expect(os.WriteFile(parentFile, []byte("file"), 0o600)).To(Succeed())

		_, _, err := Open(Config{
			Target:   TargetFile,
			FilePath: filepath.Join(parentFile, "leafwiki.log"),
			Level:    slog.LevelInfo,
		}, Streams{})

		Expect(err).To(MatchError(ErrOpenLogFile))
	})

	It("returns file open errors after parent creation succeeds", func() {
		logPath := loggingTempDir()

		_, _, err := Open(Config{
			Target:   TargetFile,
			FilePath: logPath,
			Level:    slog.LevelInfo,
		}, Streams{})

		Expect(err).To(MatchError(ErrOpenLogFile))
	})

	It("routes slog and stdlib log output to the selected stderr sink", func() {
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		logger, closer, err := Open(Config{
			Target: TargetStderr,
			Level:  slog.LevelInfo,
		}, Streams{Stdout: &stdout, Stderr: &stderr})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			_ = closer.Close()
		})

		previous := slog.Default()
		slog.SetDefault(logger)
		DeferCleanup(func() {
			slog.SetDefault(previous)
		})

		slog.Default().Info(testLogSlogMessage)
		log.Print(testLogStdlibMessage)

		Expect(stdout.String()).To(BeEmpty())
		Expect(parseLogRecords(stderr.String())).To(ConsistOf(
			HaveKeyWithValue("msg", testLogSlogMessage),
			HaveKeyWithValue("msg", testLogStdlibMessage),
		))
	})

	It("filters stream log records below the configured level", func() {
		var stdout bytes.Buffer

		logger, closer, err := Open(Config{
			Target: TargetStdout,
			Level:  slog.LevelError,
		}, Streams{Stdout: &stdout})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			_ = closer.Close()
		})

		logger.Info(testLogInfoMessage)
		logger.Error(testLogErrorMessage)

		Expect(parseLogRecords(stdout.String())).To(ConsistOf(HaveKeyWithValue("msg", testLogErrorMessage)))
	})
})

var _ = Describe("logging fallback behavior", func() {
	It("rejects blank data dir when file logging is required", func() {
		_, err := Resolve(ConfigInput{Target: "file", TargetSet: true, DataDir: " \t\n "})

		Expect(err).To(MatchError(ErrLogDataDirRequired))
	})

	It("trims and case-normalizes stream targets", func() {
		cfg, err := Resolve(ConfigInput{Target: " StDeRr ", TargetSet: true})

		Expect(err).NotTo(HaveOccurred())
		Expect(cfg).To(matchResolvedConfig(TargetStderr, BeEmpty(), slog.LevelInfo))
	})

	It("trims and case-normalizes warning level config", func() {
		cfg, err := Resolve(ConfigInput{Target: "stdout", TargetSet: true, LevelFromConfig: " WARN "})

		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Level).To(Equal(slog.LevelWarn))
	})

	It("trims and case-normalizes error level config", func() {
		cfg, err := Resolve(ConfigInput{Target: "stdout", TargetSet: true, LevelFromConfig: " ERROR "})

		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Level).To(Equal(slog.LevelError))
	})

	It("falls back to info level for unknown level config", func() {
		cfg, err := Resolve(ConfigInput{Target: "stdout", TargetSet: true, LevelFromConfig: "verbose"})

		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Level).To(Equal(slog.LevelInfo))
	})

	It("rejects an invalid already-resolved target in Open", func() {
		_, _, err := Open(Config{Target: Target("system"), Level: slog.LevelInfo}, Streams{})

		Expect(err).To(MatchError(ErrInvalidLogTarget))
	})

	It("uses default stdout writer for nil stdout stream", func() {
		logger, closer, err := Open(Config{Target: TargetStdout, Level: slog.LevelInfo}, Streams{})

		Expect(err).NotTo(HaveOccurred())
		Expect(logger).NotTo(BeNil())
		Expect(closer.Close()).To(Succeed())
	})

	It("uses default stderr writer for nil stderr stream", func() {
		logger, closer, err := Open(Config{Target: TargetStderr, Level: slog.LevelInfo}, Streams{})

		Expect(err).NotTo(HaveOccurred())
		Expect(logger).NotTo(BeNil())
		Expect(closer.Close()).To(Succeed())
	})
})

func loggingTempDir() string {
	GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-logging-*")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(os.RemoveAll, dir)
	return dir
}
