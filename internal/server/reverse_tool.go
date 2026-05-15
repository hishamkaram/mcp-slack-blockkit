package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hishamkaram/mcp-slack-block-kit/internal/reverse"
)

// ReverseInput accepts a blocks array (or a payload-wrapped form, same
// rules as validate) to convert back into Markdown.
type ReverseInput struct {
	Blocks  any `json:"blocks,omitempty" jsonschema:"array of Slack Block Kit blocks to convert back to markdown"`
	Payload any `json:"payload,omitempty" jsonschema:"alternative form: a full chat.postMessage payload object whose blocks field is converted"`
}

// ReverseOutput carries the reconstructed markdown plus any lossy-conversion
// warnings.
type ReverseOutput struct {
	Markdown string   `json:"markdown" jsonschema:"the reconstructed Markdown text"`
	Warnings []string `json:"warnings,omitempty" jsonschema:"non-fatal notes about constructs that could not be represented faithfully in Markdown"`
}

func (s *Server) registerReverseTool() {
	mcp.AddTool(
		s.mcp,
		&mcp.Tool{
			Name: "block_kit_to_markdown",
			Description: "Convert a Slack Block Kit payload back into Markdown — the " +
				"inverse of convert_markdown_to_block_kit. Best-effort and lossy: " +
				"Block Kit can express styling and interactive elements (buttons, " +
				"accessories, colors) with no Markdown equivalent; such constructs " +
				"are approximated and the choices reported in warnings. Accepts " +
				"either a `blocks` array or a full chat.postMessage `payload`.",
			Annotations: readOnlyToolAnnotations("Block Kit to Markdown"),
		},
		s.handleReverse,
	)
}

func (s *Server) handleReverse(_ context.Context, _ *mcp.CallToolRequest, in ReverseInput) (*mcp.CallToolResult, ReverseOutput, error) {
	blocks, err := decodeBlocksInput(in.Blocks, in.Payload)
	if err != nil {
		return errorResult("invalid input: " + err.Error()), ReverseOutput{}, nil
	}
	md, warnings, err := reverse.ToMarkdown(blocks)
	if err != nil {
		return errorResult("reverse conversion failed: " + err.Error()), ReverseOutput{}, nil
	}
	return nil, ReverseOutput{Markdown: md, Warnings: warnings}, nil
}
