package logging

import (
	"bytes"
	"encoding/json"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolve_DefaultsToFileUnderDataDir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	cfg, err := Resolve(ConfigInput{DataDir: dataDir})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if cfg.Target != TargetFile {
		t.Fatalf("Target = %q, want %q", cfg.Target, TargetFile)
	}
	if got, want := cfg.FilePath, filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log"); got != want {
		t.Fatalf("FilePath = %q, want %q", got, want)
	}
	if cfg.Level != slog.LevelInfo {
		t.Fatalf("Level = %v, want %v", cfg.Level, slog.LevelInfo)
	}
}

func TestResolve_RelativeLogFileResolvesUnderDataDir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	cfg, err := Resolve(ConfigInput{
		DataDir:         dataDir,
		FilePath:        "logs/custom.log",
		FilePathSet:     true,
		Target:          "file",
		TargetSet:       true,
		LevelFromConfig: "debug",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if got, want := cfg.FilePath, filepath.Join(dataDir, "logs", "custom.log"); got != want {
		t.Fatalf("FilePath = %q, want %q", got, want)
	}
	if cfg.Level != slog.LevelDebug {
		t.Fatalf("Level = %v, want %v", cfg.Level, slog.LevelDebug)
	}
}

func TestResolve_RejectsRelativeLogFileEscapingDataDir(t *testing.T) {
	_, err := Resolve(ConfigInput{
		DataDir:     filepath.Join(t.TempDir(), "data"),
		FilePath:    "../leafwiki.log",
		FilePathSet: true,
	})
	if err == nil {
		t.Fatalf("Resolve() error = nil, want traversal rejection")
	}
	if !strings.Contains(err.Error(), "log file path must stay within data dir") {
		t.Fatalf("Resolve() error = %v, want containment error", err)
	}
}

func TestResolve_AbsoluteLogFileIsUsedAsIs(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "leafwiki.log")

	cfg, err := Resolve(ConfigInput{
		DataDir:     filepath.Join(t.TempDir(), "data"),
		FilePath:    logPath,
		FilePathSet: true,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if cfg.FilePath != logPath {
		t.Fatalf("FilePath = %q, want absolute path %q", cfg.FilePath, logPath)
	}
}

func TestResolve_RejectsInvalidTargetAndNonFileTargetWithFile(t *testing.T) {
	tests := []struct {
		name  string
		input ConfigInput
		want  string
	}{
		{
			name: "invalid target",
			input: ConfigInput{
				DataDir:   t.TempDir(),
				Target:    "system",
				TargetSet: true,
			},
			want: "invalid log target",
		},
		{
			name: "stderr with file",
			input: ConfigInput{
				DataDir:     t.TempDir(),
				Target:      "stderr",
				TargetSet:   true,
				FilePath:    "custom.log",
				FilePathSet: true,
			},
			want: "--log-file requires --log-target file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Resolve(tt.input)
			if err == nil {
				t.Fatalf("Resolve() error = nil, want %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Resolve() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestOpenLogger_FileCreatesParentAndAppendsJSON(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "nested", "leafwiki.log")
	if err := os.WriteFile(logPath, []byte("previous line\n"), 0o600); !os.IsNotExist(err) {
		t.Fatalf("precondition WriteFile error = %v, want not-exist", err)
	}

	logger, closer, err := Open(Config{
		Target:   TargetFile,
		FilePath: logPath,
		Level:    slog.LevelInfo,
	}, Streams{})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer closer.Close()

	logger.Info("Starting LeafWiki", "address", "127.0.0.1:0")
	if err := closer.Close(); err != nil {
		t.Fatalf("close logger sink: %v", err)
	}

	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("stat log file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("log file mode = %v, want 0600", got)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 1 {
		t.Fatalf("log file lines = %q, want one JSON line", lines)
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("log line is not JSON: %v\n%s", err, lines[0])
	}
	for _, key := range []string{"time", "level", "msg", "source"} {
		if _, ok := entry[key]; !ok {
			t.Fatalf("log entry missing %q: %#v", key, entry)
		}
	}
	if entry["msg"] != "Starting LeafWiki" {
		t.Fatalf("msg = %v, want Starting LeafWiki", entry["msg"])
	}
}

func TestOpenLogger_AppendsExistingFile(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "leafwiki.log")
	if err := os.WriteFile(logPath, []byte("previous line\n"), 0o600); err != nil {
		t.Fatalf("write existing log file: %v", err)
	}

	logger, closer, err := Open(Config{
		Target:   TargetFile,
		FilePath: logPath,
		Level:    slog.LevelInfo,
	}, Streams{})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	logger.Info("Starting LeafWiki")
	if err := closer.Close(); err != nil {
		t.Fatalf("close logger sink: %v", err)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(raw), "previous line") || !strings.Contains(string(raw), "Starting LeafWiki") {
		t.Fatalf("log file was not appended: %q", string(raw))
	}
}

func TestOpenLogger_FileOpenFailureIsVisible(t *testing.T) {
	parentFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parentFile, []byte("file"), 0o600); err != nil {
		t.Fatalf("write parent file: %v", err)
	}

	_, _, err := Open(Config{
		Target:   TargetFile,
		FilePath: filepath.Join(parentFile, "leafwiki.log"),
		Level:    slog.LevelInfo,
	}, Streams{})
	if err == nil {
		t.Fatalf("Open() error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "failed to open log file") {
		t.Fatalf("Open() error = %v, want failed to open log file", err)
	}
}

func TestOpenLogger_StdoutStderrAndStdlibBridgeUseSelectedSink(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	logger, closer, err := Open(Config{
		Target: TargetStderr,
		Level:  slog.LevelInfo,
	}, Streams{Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer closer.Close()

	previous := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(previous)

	slog.Default().Info("slog message")
	log.Print("stdlib message")

	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	got := stderr.String()
	if !strings.Contains(got, "slog message") || !strings.Contains(got, "stdlib message") {
		t.Fatalf("stderr = %q, want slog and stdlib messages", got)
	}
}

func TestOpenLogger_StreamTargetsRespectLevel(t *testing.T) {
	var stdout bytes.Buffer

	logger, closer, err := Open(Config{
		Target: TargetStdout,
		Level:  slog.LevelError,
	}, Streams{Stdout: &stdout})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer closer.Close()

	logger.Info("info message")
	logger.Error("error message")

	got := stdout.String()
	if strings.Contains(got, "info message") {
		t.Fatalf("stdout contains filtered info message: %q", got)
	}
	if !strings.Contains(got, "error message") {
		t.Fatalf("stdout = %q, want error message", got)
	}
}
