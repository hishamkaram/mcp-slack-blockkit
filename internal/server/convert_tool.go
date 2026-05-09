package server

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/slack-go/slack"

	"github.com/hishamkaram/mcp-slack-blockkit/internal/converter"
	"github.com/hishamkaram/mcp-slack-blockkit/internal/preview"
	"github.com/hishamkaram/mcp-slack-blockkit/internal/splitter"
)

// ConvertInput is the schema for the convert_markdown_to_blockkit tool.
// Field tags are read by the SDK's jsonschema generator at registration
// time.
type ConvertInput struct {
	Markdown         string            `json:"markdown" jsonschema:"the markdown text to convert to Slack Block Kit JSON"`
	Mode             string            `json:"mode,omitempty" jsonschema:"conversion strategy: auto (default), rich_text, markdown_block, or section_mrkdwn"`
	AllowBroadcasts  bool              `json:"allow_broadcasts,omitempty" jsonschema:"if true, raw <!channel>/<!here>/<@U…> in input pass through unchanged (default false: entity-escaped for safety)"`
	MentionMap       map[string]string `json:"mention_map,omitempty" jsonschema:"map of bare @handle to Slack ID (U… user, C… channel, S… usergroup); resolved to typed mention elements"`
	ReturnPreviewURL bool              `json:"return_preview_url,omitempty" jsonschema:"if true (default), include the Block Kit Builder preview URL in the response"`
	Split            string            `json:"split,omitempty" jsonschema:"split strategy: none (default), or both — chunks the result on the >50-block axis"`
	BlockIDPrefix    string            `json:"block_id_prefix,omitempty" jsonschema:"optional prefix for generated block_id values; empty means no block_id is set"`
}

// ConvertOutput is the schema for the convert_markdown_to_blockkit response.
// Blocks/Chunks are typed `any` rather than `json.RawMessage`: the
// jsonschema-go inference treats RawMessage as []byte (integer array)
// and rejects the nested object payload at validation time. Using `any`
// gives us a permissive schema while still encoding the slack.Block
// values correctly via their per-type MarshalJSON.
type ConvertOutput struct {
	Blocks      any      `json:"blocks" jsonschema:"the converted Slack Block Kit blocks array"`
	Chunks      []any    `json:"chunks,omitempty" jsonschema:"when split is enabled and the conversion produces more than max_blocks_per_chunk blocks, the result is returned as one block-array per chunk"`
	ChunkCount  int      `json:"chunk_count,omitempty" jsonschema:"number of chunks; 1 when split was a no-op or disabled"`
	PreviewURL  string   `json:"preview_url,omitempty" jsonschema:"single-click Block Kit Builder URL for visual QA"`
	PreviewSize int      `json:"preview_byte_size,omitempty" jsonschema:"byte length of the preview URL; URLs above 8KB may be unreliable"`
	Warnings    []string `json:"warnings,omitempty" jsonschema:"non-fatal notes (e.g. fallback paths taken)"`
}

func (s *Server) registerConvertTool() {
	mcp.AddTool(
		s.mcp,
		&mcp.Tool{
			Name: "convert_markdown_to_blockkit",
			Description: "Convert markdown into Slack Block Kit JSON. Auto mode picks " +
				"between a single Slack `markdown` block (Feb 2025, ≤12k chars, no " +
				"images, no oversized tables) and full deterministic decomposition " +
				"into rich_text / section / header / image / divider blocks. " +
				"Mention sanitization is on by default (raw <!channel>/<!here>/<@U…> " +
				"are entity-escaped). Pass mention_map for safe @handle resolution. " +
				"Optional preview_url returns a Block Kit Builder link for visual QA.",
		},
		s.handleConvert,
	)
}

func (s *Server) handleConvert(_ context.Context, _ *mcp.CallToolRequest, in ConvertInput) (*mcp.CallToolResult, ConvertOutput, error) {
	opts, err := convertInputToOptions(in)
	if err != nil {
		return errorResult("invalid input: " + err.Error()), ConvertOutput{}, nil
	}

	r, err := converter.New(opts)
	if err != nil {
		return errorResult("converter init failed: " + err.Error()), ConvertOutput{}, nil
	}

	blocks, err := r.Convert(in.Markdown)
	if err != nil {
		return errorResult("conversion failed: " + err.Error()), ConvertOutput{}, nil
	}

	out := ConvertOutput{
		// Assign the typed []slack.Block directly; each block's MarshalJSON
		// produces the correct wire shape when the SDK serializes the
		// response. `any` keeps the inferred schema permissive.
		Blocks: blocks,
	}

	// Optional split into chunks. Only the "both" / "blocks" strategies
	// fire today; "paragraphs" splitting is handled inside the converter.
	if in.Split == "both" || in.Split == "blocks" {
		chunks := splitter.ChunkBlocks(blocks, splitter.DefaultMaxBlocksPerChunk)
		if len(chunks) > 1 {
			out.Chunks = make([]any, len(chunks))
			for i, c := range chunks {
				out.Chunks[i] = c
			}
			out.ChunkCount = len(chunks)
		} else {
			out.ChunkCount = 1
		}
	}

	// Preview URL is always produced. The schema documents
	// `return_preview_url` as opt-in (default true), but Go can't
	// distinguish a missing JSON bool from an explicit false on a
	// non-pointer field — so we'd need to switch the input type to
	// *bool to support strict opt-out. Until a caller asks for that,
	// always-on is the simpler contract.
	if pr, err := preview.BuilderURL(blocks); err == nil {
		out.PreviewURL = pr.URL
		out.PreviewSize = pr.ByteSize
		if pr.Truncated {
			out.Warnings = append(out.Warnings,
				fmt.Sprintf("preview URL is %d bytes; may exceed practical browser/Slack limits (~8KB)", pr.ByteSize))
		}
	}

	return nil, out, nil
}

// convertInputToOptions translates the MCP tool's input struct into a
// converter.Options. Empty Mode defaults to auto (the converter's
// DefaultOptions choice). Invalid Mode values are surfaced as errors at
// the converter.New stage.
func convertInputToOptions(in ConvertInput) (converter.Options, error) {
	opts := converter.DefaultOptions()
	if in.Mode != "" {
		opts.Mode = converter.Mode(in.Mode)
	}
	opts.AllowBroadcasts = in.AllowBroadcasts
	if len(in.MentionMap) > 0 {
		opts.MentionMap = in.MentionMap
	}
	if in.BlockIDPrefix != "" {
		opts.BlockIDPrefix = in.BlockIDPrefix
	}
	return opts, nil
}

// errorResult builds an MCP CallToolResult with isError: true. Per the
// MCP spec, tool-level failures (input that parses but can't be
// processed) use this shape rather than a JSON-RPC transport error.
func errorResult(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: message}},
	}
}

// ensure the slack import stays referenced even if a future refactor
// removes its use here — keeps this adapter self-contained for tests.
var _ = slack.MarkdownType
