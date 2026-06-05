package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

type Target string

const (
	TargetFile   Target = "file"
	TargetStderr Target = "stderr"
	TargetStdout Target = "stdout"
)

type ConfigInput struct {
	Target          string
	TargetSet       bool
	FilePath        string
	FilePathSet     bool
	LevelFromConfig string
	DataDir         string
}

type Config struct {
	Target   Target
	FilePath string
	Level    slog.Level
}

type Streams struct {
	Stdout io.Writer
	Stderr io.Writer
}

func Resolve(input ConfigInput) (Config, error) {
	target := TargetFile
	if input.TargetSet || strings.TrimSpace(input.Target) != "" {
		parsed, err := parseTarget(input.Target)
		if err != nil {
			return Config{}, err
		}
		target = parsed
	}

	if input.FilePathSet && target != TargetFile {
		return Config{}, fmt.Errorf("--log-file requires --log-target file")
	}

	level := parseLevel(input.LevelFromConfig)
	if target != TargetFile {
		return Config{Target: target, Level: level}, nil
	}

	dataDir := strings.TrimSpace(input.DataDir)
	if dataDir == "" {
		return Config{}, fmt.Errorf("data dir is required for file logging")
	}

	filePath := strings.TrimSpace(input.FilePath)
	if filePath == "" {
		filePath = filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log")
	} else if !filepath.IsAbs(filePath) {
		cleanRel := filepath.Clean(filePath)
		if !filepath.IsLocal(cleanRel) {
			return Config{}, fmt.Errorf("log file path must stay within data dir")
		}
		filePath = filepath.Join(dataDir, cleanRel)
	}

	return Config{
		Target:   target,
		FilePath: filepath.Clean(filePath),
		Level:    level,
	}, nil
}

func Open(cfg Config, streams Streams) (*slog.Logger, io.Closer, error) {
	sink, closer, err := openSink(cfg, streams)
	if err != nil {
		return nil, nil, err
	}

	return slog.New(slog.NewJSONHandler(sink, &slog.HandlerOptions{
		Level:     cfg.Level,
		AddSource: true,
	})), closer, nil
}

func parseTarget(raw string) (Target, error) {
	switch Target(strings.TrimSpace(strings.ToLower(raw))) {
	case TargetFile:
		return TargetFile, nil
	case TargetStderr:
		return TargetStderr, nil
	case TargetStdout:
		return TargetStdout, nil
	default:
		return "", fmt.Errorf("invalid log target %q (expected file, stderr, or stdout)", raw)
	}
}

func parseLevel(raw string) slog.Level {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func openSink(cfg Config, streams Streams) (io.Writer, io.Closer, error) {
	switch cfg.Target {
	case TargetFile:
		if err := os.MkdirAll(filepath.Dir(cfg.FilePath), 0o755); err != nil {
			return nil, nil, fmt.Errorf("failed to open log file: create parent directory: %w", err)
		}
		file, err := os.OpenFile(cfg.FilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open log file: %w", err)
		}
		return file, file, nil
	case TargetStderr:
		if streams.Stderr == nil {
			streams.Stderr = os.Stderr
		}
		return streams.Stderr, noopCloser{}, nil
	case TargetStdout:
		if streams.Stdout == nil {
			streams.Stdout = os.Stdout
		}
		return streams.Stdout, noopCloser{}, nil
	default:
		return nil, nil, fmt.Errorf("invalid log target %q (expected file, stderr, or stdout)", cfg.Target)
	}
}

type noopCloser struct{}

func (noopCloser) Close() error {
	return nil
}
