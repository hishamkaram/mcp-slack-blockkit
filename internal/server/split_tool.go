package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hishamkaram/mcp-slack-blockkit/internal/splitter"
)

// SplitInput accepts a blocks array (or payload-wrapped form) plus an
// optional max_blocks_per_chunk override. The default matches Slack's
// 50-block per-message ceiling.
type SplitInput struct {
	Blocks            any `json:"blocks,omitempty" jsonschema:"array of Slack Block Kit blocks to chunk"`
	Payload           any `json:"payload,omitempty" jsonschema:"alternative form: a full chat.postMessage payload object"`
	MaxBlocksPerChunk int `json:"max_blocks_per_chunk,omitempty" jsonschema:"per-chunk block ceiling (default 50, Slack's per-message limit)"`
}

// SplitOutput returns one chunk per Slack-API-compliant message. ChunkCount
// is len(Chunks) — included for caller-side pagination convenience.
type SplitOutput struct {
	Chunks     []any `json:"chunks" jsonschema:"the input blocks array split into one or more message-sized chunks"`
	ChunkCount int   `json:"chunk_count" jsonschema:"len(chunks)"`
}

func (s *Server) registerSplitTool() {
	mcp.AddTool(
		s.mcp,
		&mcp.Tool{
			Name: "split_blocks",
			Description: "Split a Block Kit payload into one or more Slack-API-" +
				"compliant chunks. Enforces the 50-block per-message limit and the " +
				"only_one_table_allowed rule (a TableBlock always opens a new chunk " +
				"when the current chunk already contains a table). Returns the " +
				"original blocks unchanged when they fit in one chunk.",
		},
		s.handleSplit,
	)
}

func (s *Server) handleSplit(_ context.Context, _ *mcp.CallToolRequest, in SplitInput) (*mcp.CallToolResult, SplitOutput, error) {
	blocks, err := decodeBlocksInput(in.Blocks, in.Payload)
	if err != nil {
		return errorResult("invalid input: " + err.Error()), SplitOutput{}, nil
	}

	chunks := splitter.ChunkBlocks(blocks, in.MaxBlocksPerChunk)

	out := SplitOutput{ChunkCount: len(chunks)}
	if len(chunks) > 0 {
		out.Chunks = make([]any, len(chunks))
		for i, c := range chunks {
			out.Chunks[i] = c
		}
	}
	return nil, out, nil
}
