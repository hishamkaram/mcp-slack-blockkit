package main

import (
	"errors"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/hishamkaram/mcp-slack-block-kit/internal/server"
)

// httpTokenEnv is the environment-variable fallback for --http-token. The
// flag wins when both are set.
//
//nolint:gosec // env-var NAME, not a credential value
const httpTokenEnv = "MCPSBK_HTTP_TOKEN"

// newServerCmd builds the `server` subcommand. Default behavior (no flags)
// is stdio, matching today's bare-binary launch path. Setting --http-addr
// or --sse-addr (mutually exclusive) switches the transport. Optional
// --http-token guards the HTTP/SSE listener with bearer-token auth.
//
// Flag values are bound to closure-local variables (not package globals)
// so repeated newServerCmd calls in tests start from a clean slate.
func newServerCmd(_ io.Writer, _ io.Writer, _ io.Reader) *cobra.Command {
	var (
		httpAddr  string
		sseAddr   string
		httpToken string
	)
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run the MCP server (stdio by default; --http-addr or --sse-addr for HTTP transports)",
		Long: "Starts the Model Context Protocol server. With no flags " +
			"reads JSON-RPC from stdin and writes responses to stdout " +
			"(the default Claude Desktop / Cursor launch path). With " +
			"--http-addr, serves the 2025-03 streamable-HTTP transport on " +
			"the given address. With --sse-addr, serves the 2024-11 SSE " +
			"transport. Use --http-token (or the " + httpTokenEnv + " env " +
			"variable) to require Authorization: Bearer <token> on every " +
			"incoming request. All listeners log to stderr; stdout is " +
			"reserved for the protocol on the stdio transport.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			token := resolveHTTPToken(httpToken, os.Getenv(httpTokenEnv))
			if err := validateServerFlags(httpAddr, sseAddr, token); err != nil {
				return err
			}
			s, err := server.New(resolveVersion())
			if err != nil {
				return err
			}
			switch {
			case httpAddr != "":
				return s.RunHTTP(cmd.Context(), httpAddr, server.HTTPOptions{Token: token})
			case sseAddr != "":
				return s.RunSSE(cmd.Context(), sseAddr, server.HTTPOptions{Token: token})
			default:
				return s.RunStdio(cmd.Context())
			}
		},
	}
	cmd.Flags().StringVar(&httpAddr, "http-addr", "",
		"bind address for the streamable-HTTP MCP transport (e.g. 127.0.0.1:7777); empty = stdio")
	cmd.Flags().StringVar(&sseAddr, "sse-addr", "",
		"bind address for the legacy SSE MCP transport (e.g. 127.0.0.1:7778); empty = stdio")
	cmd.Flags().StringVar(&httpToken, "http-token", "",
		"optional bearer token required on incoming HTTP/SSE requests; falls back to env "+httpTokenEnv)
	return cmd
}

// resolveHTTPToken picks the bearer token from the flag value when set, or
// the env-var fallback otherwise. The flag wins to keep CLI invocation
// authoritative — a stale env var shouldn't override an explicit override.
func resolveHTTPToken(flag, env string) string {
	if flag != "" {
		return flag
	}
	return env
}

// validateServerFlags enforces the two usage invariants from the plan: at
// most one transport flag set; token only meaningful with one of them.
func validateServerFlags(httpAddr, sseAddr, token string) error {
	if httpAddr != "" && sseAddr != "" {
		return errors.New("--http-addr and --sse-addr are mutually exclusive")
	}
	if token != "" && httpAddr == "" && sseAddr == "" {
		return errors.New("--http-token requires --http-addr or --sse-addr")
	}
	return nil
}
