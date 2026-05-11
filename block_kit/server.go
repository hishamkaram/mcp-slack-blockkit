package block_kit

import (
	"context"

	"github.com/hishamkaram/mcp-slack-block-kit/internal/server"
)

// --- Server -----------------------------------------------------------------

// Server is the MCP server bundling the five v0.1 tools
// (convert_markdown_to_block_kit, validate_block_kit, preview_block_kit,
// lint_block_kit, split_blocks) on top of the official
// modelcontextprotocol/go-sdk. Construct one with NewServer and start it
// on the transport that matches your deployment.
type Server = server.Server

// HTTPOptions configures the HTTP and SSE transports.
//
//   - Token: optional shared-secret bearer. When non-empty, every request
//     to the HTTP/SSE listener must carry Authorization: Bearer <token>
//     or it gets a 401. Constant-time comparison.
//
// Empty options ({}) means no auth is enforced, which is appropriate for
// the default localhost bind. Set a token before binding on any
// non-loopback address.
type HTTPOptions = server.HTTPOptions

// NewServer constructs a fully-wired MCP server. version is reported back
// to MCP clients during the initialize handshake.
func NewServer(version string) (*Server, error) {
	return server.New(version)
}

// RunStdio starts the MCP server on the standard stdio transport and
// blocks until the client disconnects (or ctx is cancelled). This is the
// launch path Claude Desktop / Cursor / Continue.dev expect.
func RunStdio(ctx context.Context, s *Server) error {
	return s.RunStdio(ctx)
}

// RunHTTP starts the streamable-HTTP MCP transport (2025-03 spec) on
// addr and blocks until ctx is cancelled. Use this for remote runners,
// containerized deployments, or any client that connects over HTTP.
func RunHTTP(ctx context.Context, s *Server, addr string, opts HTTPOptions) error {
	return s.RunHTTP(ctx, addr, opts)
}

// RunSSE starts the legacy SSE MCP transport (2024-11 spec). Same shape
// as RunHTTP — useful for MCP clients that don't support streamable HTTP
// yet.
func RunSSE(ctx context.Context, s *Server, addr string, opts HTTPOptions) error {
	return s.RunSSE(ctx, addr, opts)
}
