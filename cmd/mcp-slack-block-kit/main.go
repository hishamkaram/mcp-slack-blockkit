package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Build-time version info, populated via -ldflags by GoReleaser.
// Defaults are used for `go install` builds; we recover the module
// version from runtime/debug.ReadBuildInfo when these are unset.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// logLevel is bound by the persistent --log-level flag.
var logLevel string

func main() {
	if err := newRootCmd(os.Stderr, os.Stdout, os.Stdin).Execute(); err != nil {
		// cobra already printed the error to stderr; just exit non-zero.
		os.Exit(1)
	}
}

// newRootCmd constructs the cobra root command. Streams are injected so tests
// can substitute pipes without touching os.Stdout / os.Stderr / os.Stdin.
func newRootCmd(stderr io.Writer, stdout io.Writer, stdin io.Reader) *cobra.Command {
	root := &cobra.Command{
		Use:   "mcp-slack-block-kit",
		Short: "MCP server that converts markdown into Slack Block Kit JSON",
		Long: "mcp-slack-block-kit is an MCP server (and CLI) that converts " +
			"markdown into valid Slack Block Kit JSON. " +
			"Run with no arguments to start the stdio MCP server, or use the " +
			"`convert` subcommand for one-shot CLI conversion.",
		Version:       resolveVersion(),
		SilenceUsage:  true,
		SilenceErrors: false,
		// Configure logging once, before any subcommand runs.
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return configureLogging(stderr, logLevel)
		},
	}

	root.SetOut(stderr)
	root.SetErr(stderr)

	root.PersistentFlags().StringVar(
		&logLevel, "log-level", "info",
		"log level (debug, info, warn, error)",
	)

	server := newServerCmd(stderr, stdout, stdin)
	convert := newConvertCmd(stderr, stdout, stdin)
	root.AddCommand(server, convert)

	// Default behavior: invoking the bare binary runs the MCP server.
	// This matches Claude Desktop's stdio launcher convention, which
	// passes no subcommand. See README for the config snippet.
	root.RunE = server.RunE

	return root
}

// resolveVersion returns the human-readable version string, falling back to
// the module version that `go install` stamps into the binary when the
// -ldflags vars are still at their defaults.
func resolveVersion() string {
	v := version
	if v == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			v = info.Main.Version
		}
	}
	return fmt.Sprintf("%s (commit %s, built %s)", v, commit, date)
}

// configureLogging installs the default slog logger. The MCP stdio transport
// reserves stdout for protocol messages, so logs MUST go to stderr.
// See https://modelcontextprotocol.io/specification (transports).
func configureLogging(stderr io.Writer, level string) error {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "info", "":
		lvl = slog.LevelInfo
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		return fmt.Errorf("invalid --log-level %q (want debug|info|warn|error)", level)
	}
	handler := slog.NewJSONHandler(stderr, &slog.HandlerOptions{Level: lvl})
	slog.SetDefault(slog.New(handler))
	return nil
}
