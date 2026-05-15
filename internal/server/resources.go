package server

import (
	"context"
	_ "embed"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// cheatsheetContent is the Block Kit reference served as an MCP resource.
// Embedded at build time so the binary stays single-file.
//
//go:embed cheatsheet.md
var cheatsheetContent string

// cheatsheetURI is the stable URI of the cheat-sheet resource. MCP clients
// reference resources by URI, so this must not change across releases.
const cheatsheetURI = "slackblockkit://reference/cheatsheet"

// registerResources exposes the Block Kit cheat-sheet so MCP clients can
// discover the conversion modes, supported Markdown, Slack limits, and the
// mention-safety model without guessing from tool descriptions alone.
func (s *Server) registerResources() {
	s.mcp.AddResource(
		&mcp.Resource{
			URI:   cheatsheetURI,
			Name:  "block-kit-cheatsheet",
			Title: "Slack Block Kit Cheat Sheet",
			Description: "Reference for the Markdown → Slack Block Kit conversion this server " +
				"performs: the four conversion modes, supported and unsupported Markdown, " +
				"Slack's documented block and field limits, and the mention-safety model. " +
				"Read before calling convert_markdown_to_block_kit.",
			MIMEType: "text/markdown",
		},
		s.handleCheatsheet,
	)
}

func (s *Server) handleCheatsheet(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      cheatsheetURI,
				MIMEType: "text/markdown",
				Text:     cheatsheetContent,
			},
		},
	}, nil
}
