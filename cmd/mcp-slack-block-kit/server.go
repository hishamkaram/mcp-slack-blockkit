package main

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/hishamkaram/mcp-slack-block-kit/internal/server"
)

// newServerCmd builds the `server` subcommand. Starts the stdio MCP server
// with the five v0.1 tools (convert, validate, preview, lint, split)
// registered on top of the official modelcontextprotocol/go-sdk.
func newServerCmd(_ io.Writer, _ io.Writer, _ io.Reader) *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "Run the stdio MCP server (default when no subcommand is given)",
		Long: "Starts the Model Context Protocol server on stdio. " +
			"Reads JSON-RPC requests from stdin, writes responses to stdout, " +
			"and writes structured logs to stderr. Five tools are exposed: " +
			"convert_markdown_to_block_kit, validate_block_kit, preview_block_kit, " +
			"lint_block_kit, split_blocks.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := server.New(resolveVersion())
			if err != nil {
				return err
			}
			return s.RunStdio(cmd.Context())
		},
	}
}
