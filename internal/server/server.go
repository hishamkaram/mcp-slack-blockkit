// Package server wires the converter, validator, splitter, and preview
// engines into the official Model Context Protocol Go SDK and exposes
// the five v0.1 tools described in research.md §B.5:
//
//   - convert_markdown_to_block_kit
//   - validate_block_kit
//   - preview_block_kit
//   - lint_block_kit
//   - split_blocks
//
// The server is intentionally thin: each tool handler is a small adapter
// that translates between the MCP wire types and the corresponding
// internal package. All real logic lives in internal/converter,
// internal/validator, internal/splitter, and internal/preview. This
// package contains no markdown parsing or block construction of its own.
package server

import (
	"context"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hishamkaram/mcp-slack-block-kit/internal/converter"
)

// Implementation metadata reported to MCP clients on initialize.
const (
	ServerName = "mcp-slack-block-kit"
)

// Server bundles the per-process state shared by every tool handler.
// One instance per running process; concurrent tool calls share this
// state but each handler holds its own request-scoped data.
type Server struct {
	mcp       *mcp.Server
	converter *converter.Renderer
	version   string
}

// New constructs a fully-wired Server with all five tools registered.
// The converter is built with DefaultOptions; per-call options are
// derived from each tool's input parameters.
func New(version string) (*Server, error) {
	conv, err := converter.New(converter.DefaultOptions())
	if err != nil {
		return nil, err
	}

	mcpServer := mcp.NewServer(
		&mcp.Implementation{Name: ServerName, Version: version},
		nil,
	)

	s := &Server{
		mcp:       mcpServer,
		converter: conv,
		version:   version,
	}
	s.registerTools()
	s.registerResources()
	s.registerPrompts()
	return s, nil
}

// MCP returns the underlying mcp.Server. Exported for tests that need to
// drive the server via NewInMemoryTransports.
func (s *Server) MCP() *mcp.Server {
	return s.mcp
}

// RunStdio starts the MCP server on the standard stdio transport and
// blocks until the client disconnects (or ctx is cancelled). This is the
// default operation mode — Claude Desktop / Cursor / Continue.dev all
// launch the binary and speak JSON-RPC over stdio.
func (s *Server) RunStdio(ctx context.Context) error {
	slog.InfoContext(
		ctx, "starting mcp server",
		"transport", "stdio",
		"version", s.version,
	)
	return s.mcp.Run(ctx, &mcp.StdioTransport{})
}

func (s *Server) registerTools() {
	s.registerConvertTool()
	s.registerValidateTool()
	s.registerPreviewTool()
	s.registerLintTool()
	s.registerSplitTool()
	s.registerReverseTool()
}

// readOnlyToolAnnotations returns the MCP tool annotations shared by every
// tool this server exposes. All of them are pure functions: they read the
// caller's input and return a result without touching any external system
// or mutating state. Surfacing ReadOnlyHint lets MCP clients reason about
// the tool (e.g. auto-approve, cache, retry) without guessing.
func readOnlyToolAnnotations(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		Title:         title,
		ReadOnlyHint:  true,
		OpenWorldHint: boolPtr(false),
	}
}

func boolPtr(b bool) *bool { return &b }
