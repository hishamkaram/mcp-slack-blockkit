package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hishamkaram/mcp-slack-block-kit/internal/preview"
)

// PreviewInput accepts a blocks array (or a payload-wrapped form, same
// rules as validate) and returns a Block Kit Builder URL.
type PreviewInput struct {
	Blocks  any    `json:"blocks,omitempty" jsonschema:"array of Slack Block Kit blocks to preview"`
	Payload any    `json:"payload,omitempty" jsonschema:"alternative form: a full chat.postMessage payload object"`
	TeamID  string `json:"team_id,omitempty" jsonschema:"optional team ID for workspace-scoped builder URLs (currently informational; standard URL is workspace-agnostic)"`
}

// PreviewOutput carries the URL plus byte-size and a truncated flag for
// callers that want to surface size warnings.
type PreviewOutput struct {
	PreviewURL string `json:"preview_url" jsonschema:"single-click Block Kit Builder URL"`
	ByteSize   int    `json:"byte_size" jsonschema:"length of the produced URL in bytes"`
	Truncated  bool   `json:"truncated" jsonschema:"true when the URL exceeds ~8KB practical limit"`
}

func (s *Server) registerPreviewTool() {
	mcp.AddTool(
		s.mcp,
		&mcp.Tool{
			Name: "preview_block_kit",
			Description: "Generate a Block Kit Builder URL for the given blocks. The " +
				"returned URL opens Slack's own visual builder with the payload " +
				"pre-loaded — useful for one-click visual QA from an LLM workflow. " +
				"No Slack credentials required; the URL is shareable across " +
				"workspaces. Reports byte_size and a truncated flag for payloads " +
				"that exceed the ~8KB practical browser limit.",
		},
		s.handlePreview,
	)
}

func (s *Server) handlePreview(_ context.Context, _ *mcp.CallToolRequest, in PreviewInput) (*mcp.CallToolResult, PreviewOutput, error) {
	blocks, err := decodeBlocksInput(in.Blocks, in.Payload)
	if err != nil {
		return errorResult("invalid input: " + err.Error()), PreviewOutput{}, nil
	}
	r, err := preview.BuilderURL(blocks)
	if err != nil {
		return errorResult("preview generation failed: " + err.Error()), PreviewOutput{}, nil
	}
	return nil, PreviewOutput{
		PreviewURL: r.URL,
		ByteSize:   r.ByteSize,
		Truncated:  r.Truncated,
	}, nil
}
