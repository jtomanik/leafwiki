package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"github.com/perber/wiki/internal/agenthooks"
	"github.com/perber/wiki/internal/localization"
	"github.com/perber/wiki/internal/runtimeconfig"
)

func normalizeAgentHookRawArgs(args []string) []string {
	if len(args) >= 2 && args[0] == "agent-hook" {
		normalized := make([]string, 0, len(args))
		normalized = append(normalized, args[2:]...)
		normalized = append(normalized, args[0], args[1])
		return normalized
	}
	return args
}

type removedEnvironmentVariableError struct {
	Name string
}

func (err removedEnvironmentVariableError) Error() string {
	return fmt.Sprintf("unknown environment variable: %s", err.Name)
}

type cliMessageID string

type cliRenderedMessageError struct {
	MessageID cliMessageID
	Message   string
}

func (err cliRenderedMessageError) Error() string {
	return err.Message
}

func newCLIRenderedMessageError(messageID cliMessageID) cliRenderedMessageError {
	return cliRenderedMessageError{
		MessageID: messageID,
		Message:   localization.English.Render(string(messageID), "").Message,
	}
}

func rejectRemovedLeafWikiEnv() error {
	for _, name := range []string{
		"LEAFWIKI_RUNTIME_STACK",
		"LEAFWIKI_ENABLE_REVISION",
		"LEAFWIKI_ENABLE_WORKSPACE_SYNC",
		"LEAFWIKI_MAX_REVISION_HISTORY",
		"LEAFWIKI_ENABLE_MCP",
		"LEAFWIKI_MCP_STDIO",
	} {
		if _, ok := os.LookupEnv(name); ok {
			return removedEnvironmentVariableError{Name: name}
		}
	}
	return nil
}

func agentHookProviderFromArgs(args []string) (agenthooks.ProviderID, bool) {
	for i, arg := range args {
		if arg != "agent-hook" {
			continue
		}
		if len(args) > i+1 {
			return agenthooks.ProviderIDFromString(args[i+1]), true
		}
		return agenthooks.ProviderUnknown, true
	}
	return "", false
}

func agentHookProviderFromRawArgs(args []string) (agenthooks.ProviderID, bool) {
	skipNext := false
	for i, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if name, hasInlineValue, ok := rawFlagName(arg); ok {
			if _, takesValue := valueTakingFlagNames()[name]; takesValue && !hasInlineValue {
				skipNext = true
			}
			continue
		}
		if arg != "agent-hook" {
			continue
		}
		if len(args) > i+1 {
			return agenthooks.ProviderIDFromString(args[i+1]), true
		}
		return agenthooks.ProviderUnknown, true
	}
	return "", false
}

func rawFlagName(arg string) (string, bool, bool) {
	return runtimeconfig.RawFlagName(arg)
}

func rawArgsContainFlag(args []string, flagName string) bool {
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		name, hasInlineValue, ok := rawFlagName(arg)
		if ok {
			if _, takesValue := valueTakingFlagNames()[name]; takesValue && !hasInlineValue {
				skipNext = true
			}
		}
		if ok && name == flagName {
			return true
		}
	}
	return false
}

func validateRawConfigFlagUsage(args []string) error {
	return runtimeconfig.ValidateRawConfigFlagUsage(args)
}

func isInvalidBareConfigPathValue(path string) bool {
	return runtimeconfig.IsInvalidBareConfigPathValue(path)
}

func valueTakingFlagNames() map[string]struct{} {
	return runtimeconfig.ValueTakingFlagNames()
}

func isAgentHookCommand(args []string) bool {
	return len(args) > 0 && args[0] == "agent-hook"
}

func isDaemonCommand(args []string) bool {
	return runtimeconfig.IsDaemonCommand(args)
}

func isDaemonHelpCommand(args []string) bool {
	return runtimeconfig.IsDaemonHelpCommand(args)
}

func shouldPrintUsage(args []string) bool {
	if len(args) == 1 && args[0] == "help" {
		return true
	}

	fs := flag.NewFlagSet("leafwiki-help-check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerFlags(fs)
	return fs.Parse(args) == flag.ErrHelp
}

func buildListenAddress(host, port string) string {
	return net.JoinHostPort(host, port)
}

func defaultDaemonServiceDataDir() (string, error) {
	return runtimeconfig.DefaultDaemonServiceDataDir()
}

func defaultDaemonServiceConfigPath() (string, error) {
	return runtimeconfig.DefaultDaemonServiceConfigPath()
}

func newNativeStdioJSONFilter(stdin io.ReadCloser, stdout io.Writer) (io.ReadCloser, <-chan error) {
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- filterNativeStdioJSON(stdin, writer, stdout)
	}()
	return reader, done
}

func filterNativeStdioJSON(stdin io.Reader, forward *io.PipeWriter, stdout io.Writer) error {
	defer closeBestEffort(forward)

	reader := bufio.NewReader(stdin)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			frame := bytes.TrimSpace(line)
			if len(frame) > 0 {
				if !json.Valid(frame) {
					if _, err := io.WriteString(stdout, `{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"Parse error"}}`+"\n"); err != nil {
						_ = forward.CloseWithError(err)
						return err
					}
				} else {
					if _, err := forward.Write(line); err != nil {
						return err
					}
					if line[len(line)-1] != '\n' {
						if _, err := forward.Write([]byte("\n")); err != nil {
							return err
						}
					}
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			_ = forward.CloseWithError(readErr)
			return readErr
		}
	}
}

func isCleanNativeStdioClose(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
		return true
	}
	return strings.Contains(err.Error(), "server is closing: EOF")
}

func nativeStdioHTTPURL(host, port, basePath string) string {
	return "http://" + net.JoinHostPort(host, port) + basePath
}
