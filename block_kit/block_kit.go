// Package block_kit is the public Go API for mcp-slack-block-kit.
//
// External Go consumers import this single package to use the converter,
// validator, splitter, and preview engines without pulling the MCP
// server dependencies. The internal/ packages are deliberately
// non-importable; this facade is the supported surface and follows
// semver across releases.
//
// Quickstart:
//
//	package main
//
//	import (
//	    "fmt"
//	    "encoding/json"
//	    "github.com/hishamkaram/mcp-slack-block-kit/block_kit"
//	)
//
//	func main() {
//	    r, err := block_kit.NewConverter(block_kit.DefaultOptions())
//	    if err != nil { panic(err) }
//
//	    blocks, err := r.Convert("# Hello\n\nbody **bold** text.")
//	    if err != nil { panic(err) }
//
//	    out, _ := json.Marshal(blocks)
//	    fmt.Println(string(out))
//
//	    // Validate before sending to chat.postMessage:
//	    result := block_kit.NewValidator().Validate(blocks)
//	    if !result.Valid {
//	        for _, e := range result.Errors {
//	            fmt.Println(e.Path, e.Code, e.Message)
//	        }
//	    }
//
//	    // Visual QA via Block Kit Builder:
//	    pr, _ := block_kit.PreviewURL(blocks)
//	    fmt.Println("preview:", pr.URL)
//	}
//
// Embedding the MCP server in your own binary:
//
//	srv, _ := block_kit.NewServer("v1.2.3")
//	// Stdio (default for Claude Desktop / Cursor launches):
//	_ = block_kit.RunStdio(ctx, srv)
//	// Streamable HTTP for HTTP-based MCP clients:
//	_ = block_kit.RunHTTP(ctx, srv, "127.0.0.1:7777", block_kit.HTTPOptions{})
//	// With bearer-token auth:
//	_ = block_kit.RunHTTP(ctx, srv, "127.0.0.1:7777", block_kit.HTTPOptions{Token: "s3cret"})
package block_kit

import (
	"github.com/slack-go/slack"

	"github.com/hishamkaram/mcp-slack-block-kit/internal/converter"
	"github.com/hishamkaram/mcp-slack-block-kit/internal/preview"
	"github.com/hishamkaram/mcp-slack-block-kit/internal/reverse"
	"github.com/hishamkaram/mcp-slack-block-kit/internal/splitter"
	"github.com/hishamkaram/mcp-slack-block-kit/internal/validator"
)

// --- Converter --------------------------------------------------------------

// Converter is the markdown → Block Kit conversion engine. Construct one per
// process and reuse across requests; safe for concurrent use.
type Converter = converter.Renderer

// Options configures a Converter. Use DefaultOptions for the recommended
// defaults and override individual fields as needed.
type Options = converter.Options

// Mode picks the overall conversion strategy. See the Mode constants.
type Mode = converter.Mode

// Conversion mode constants. ModeAuto is the recommended default — it
// chooses between a single Slack `markdown` block (Feb 2025) and
// deterministic decomposition based on input characteristics.
const (
	ModeAuto          = converter.ModeAuto
	ModeRichText      = converter.ModeRichText
	ModeMarkdownBlock = converter.ModeMarkdownBlock
	ModeSectionMrkdwn = converter.ModeSectionMrkdwn
)

// Slack-defined ceilings, exposed for callers that want to reference the
// same constants as the converter.
const (
	MaxBlocksPerMessage    = converter.MaxBlocksPerMessage
	MaxSectionTextChars    = converter.MaxSectionTextChars
	MaxHeaderTextChars     = converter.MaxHeaderTextChars
	MaxMarkdownBlockSum    = converter.MaxMarkdownBlockSum
	MaxBlockIDChars        = converter.MaxBlockIDChars
	DefaultMaxInputBytes   = converter.DefaultMaxInputBytes
	DefaultMaxNestingDepth = converter.DefaultMaxNestingDepth
)

// ErrInputTooLarge is returned when input exceeds Options.MaxInputBytes.
var ErrInputTooLarge = converter.ErrInputTooLarge

// ErrInputTooDeeplyNested is returned when the parsed markdown AST nests
// deeper than Options.MaxNestingDepth.
var ErrInputTooDeeplyNested = converter.ErrInputTooDeeplyNested

// ErrMarkdownBlockTooLarge is returned when input is requested to be
// emitted as a single Slack markdown block but exceeds the 12,000-char cap.
var ErrMarkdownBlockTooLarge = converter.ErrMarkdownBlockTooLarge

// DefaultOptions returns Options with the recommended defaults applied.
func DefaultOptions() Options { return converter.DefaultOptions() }

// NewConverter constructs a Converter with the given options.
//
// The returned Converter exposes both Convert (returning blocks + error
// only) and ConvertWithWarnings (additionally returning a slice of
// human-readable warnings explaining mode-fallback decisions, e.g.
// "auto mode routed to rich_text decomposition because input contains
// code-in-blockquote"). Callers integrating with an LLM should prefer
// ConvertWithWarnings so warnings can be surfaced back to the model.
func NewConverter(opts Options) (*Converter, error) { return converter.New(opts) }

// --- Validator --------------------------------------------------------------

// Validator runs the full Slack constraint suite against a block list.
type Validator = validator.Validator

// ValidationResult is the outcome of a validation call.
type ValidationResult = validator.Result

// Violation describes one validation failure.
type Violation = validator.Violation

// Severity classifies a Violation. Errors invalidate the payload;
// Warnings indicate usage that may be problematic but is technically valid.
type Severity = validator.Severity

const (
	SeverityError   = validator.SeverityError
	SeverityWarning = validator.SeverityWarning
)

// Surface identifies the Slack surface a payload targets. It sets the
// block-count ceiling: messages allow 50 blocks, modals and App Home tabs
// allow 100. Pass one to Validator.ValidateForSurface.
type Surface = validator.Surface

const (
	SurfaceMessage = validator.SurfaceMessage
	SurfaceModal   = validator.SurfaceModal
	SurfaceHomeTab = validator.SurfaceHomeTab
)

// NewValidator returns a Validator that reports only Slack-published
// constraint violations as errors.
func NewValidator() *Validator { return validator.New() }

// NewStrictValidator returns a Validator that additionally reports
// deprecated patterns as errors (e.g. raw mrkdwn section where rich_text
// is now strongly preferred).
func NewStrictValidator() *Validator { return validator.NewStrict() }

// --- Splitter ---------------------------------------------------------------

// SplitText breaks an oversized text payload into chunks no larger than
// maxChars, preferring paragraph > sentence > word boundaries.
func SplitText(s string, maxChars, safetyMargin int) []string {
	return splitter.SplitText(s, maxChars, safetyMargin)
}

// ChunkBlocks splits a flat block list into one or more message-sized
// chunks, enforcing the 50-block per-message ceiling and the
// only_one_table_allowed rule.
func ChunkBlocks(blocks []slack.Block, maxPerChunk int) [][]slack.Block {
	return splitter.ChunkBlocks(blocks, maxPerChunk)
}

// DefaultMaxBlocksPerChunk is the per-message ceiling enforced by Slack.
const DefaultMaxBlocksPerChunk = splitter.DefaultMaxBlocksPerChunk

// --- Preview ----------------------------------------------------------------

// PreviewResult carries a Block Kit Builder URL and metadata.
type PreviewResult = preview.Result

// PreviewURL produces a Block Kit Builder URL for the given blocks.
func PreviewURL(blocks []slack.Block) (PreviewResult, error) {
	return preview.BuilderURL(blocks)
}

// PreviewURLString is a convenience wrapper that returns just the URL string.
func PreviewURLString(blocks []slack.Block) (string, error) {
	return preview.BuilderURLString(blocks)
}

// BuilderHost is the canonical Block Kit Builder URL prefix.
const BuilderHost = preview.BuilderHost

// --- Reverse (Block Kit → Markdown) -----------------------------------------

// BlockKitToMarkdown converts a Slack Block Kit block list back into
// Markdown — the inverse of Converter.Convert. The conversion is
// best-effort and lossy: Block Kit can express styling and interactive
// elements with no Markdown equivalent. The returned warnings slice names
// every construct that could not be represented faithfully.
func BlockKitToMarkdown(blocks []slack.Block) (markdown string, warnings []string, err error) {
	return reverse.ToMarkdown(blocks)
}
