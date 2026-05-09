// Package converter turns markdown into Slack Block Kit JSON. It exposes a
// Renderer wired around yuin/goldmark; downstream callers (the MCP tool
// handlers and the `convert` CLI subcommand) construct one Renderer per
// process and reuse it across requests.
package converter

import (
	"errors"
	"fmt"
)

// Mode selects the overall conversion strategy.
//
//   - ModeAuto picks per-input: short LLM-style outputs (≤ 12,000 chars total,
//     no images, no actions) emit a single `markdown` block; richer inputs
//     fall through to deterministic decomposition into rich_text/section/etc.
//   - ModeRichText always emits the deterministic decomposition, preferring
//     `rich_text` for body content. Tables become `slack.TableBlock`.
//   - ModeMarkdownBlock always emits a single Slack `markdown` block (Feb 2025).
//     Errors if input exceeds 12,000 chars.
//   - ModeSectionMrkdwn always emits `section` blocks with `mrkdwn` text.
//     Useful for downstream consumers that need the older shape.
type Mode string

const (
	ModeAuto          Mode = "auto"
	ModeRichText      Mode = "rich_text"
	ModeMarkdownBlock Mode = "markdown_block"
	ModeSectionMrkdwn Mode = "section_mrkdwn"
)

// Slack-defined ceilings, surfaced here as exported constants so tests and
// downstream callers can reference them without re-deriving the numbers.
// Sources are in research.md §3.
const (
	MaxBlocksPerMessage  = 50
	MaxSectionTextChars  = 3000
	MaxHeaderTextChars   = 150
	MaxMarkdownBlockSum  = 12000
	MaxBlockIDChars      = 255
	DefaultMaxInputBytes = 256 * 1024 // 256 KiB
)

// ErrInputTooLarge is returned when input exceeds Options.MaxInputBytes.
// We expose it as a sentinel so MCP handlers can map it to a structured error.
var ErrInputTooLarge = errors.New("input exceeds configured maximum size")

// Options configures a Renderer. All fields have sane defaults via DefaultOptions;
// callers typically start with that and tweak individual fields.
type Options struct {
	// Mode picks the overall conversion strategy. See the Mode constants.
	Mode Mode

	// BlockIDPrefix is prepended to every generated block_id. Default empty
	// (block_ids are assigned sequentially with no prefix). Useful when the
	// calling app wants to namespace its own action handlers.
	BlockIDPrefix string

	// EmitStandaloneLinkAsButton controls whether a paragraph containing only
	// a single bare link is rewritten as an `actions` block with a button.
	// Default false: a standalone link stays as a link element. (md2slack's
	// default is true; we explicitly disable because it's rarely what an MCP
	// caller wants.)
	EmitStandaloneLinkAsButton bool

	// MaxBlocksPerChunk caps blocks-per-output-message before the splitter
	// breaks the result into chunks. Default 50 (Slack's per-message limit).
	MaxBlocksPerChunk int

	// ParagraphCharLimit caps the per-element character count for section
	// text and rich_text_section runs before the paragraph splitter breaks
	// them. Default 3000 (Slack's section.text limit).
	ParagraphCharLimit int

	// MaxInputBytes bounds the markdown input. Zero means "use the default
	// of 256 KiB" — there is intentionally no way to disable bounding from
	// Options alone, since unbounded input is a memory-exhaustion vector
	// when the server accepts LLM-generated text. Callers that genuinely
	// need a larger ceiling should set an explicit value.
	MaxInputBytes int

	// AllowBroadcasts disables mention-sanitization. When false (default),
	// raw `<!channel>` / `<!here>` / `<@U…>` / `<#C…>` text in markdown is
	// HTML-entity-escaped so it doesn't broadcast. When true, those strings
	// pass through verbatim.
	AllowBroadcasts bool

	// MentionMap resolves bare `@handle` text to Slack user IDs (e.g.
	// {"alice": "U123ABC"}). When set, matching `@handle` substrings are
	// rendered as `user` rich_text elements. Unset entries fall through to
	// the sanitization rules (escaped unless AllowBroadcasts is true).
	MentionMap map[string]string

	// EnableTables controls whether the GFM tables extension is honored.
	// Default true. Set false to make tables fall through as raw text.
	EnableTables bool
}

// DefaultOptions returns Options with the recommended defaults applied.
func DefaultOptions() Options {
	return Options{
		Mode:                       ModeAuto,
		BlockIDPrefix:              "",
		EmitStandaloneLinkAsButton: false,
		MaxBlocksPerChunk:          MaxBlocksPerMessage,
		ParagraphCharLimit:         MaxSectionTextChars,
		MaxInputBytes:              DefaultMaxInputBytes,
		AllowBroadcasts:            false,
		MentionMap:                 nil,
		EnableTables:               true,
	}
}

// validate fills in zero-value fields with defaults and rejects clearly
// invalid combinations. Called once by New(); callers do not need to invoke
// it themselves.
func (o *Options) validate() error {
	if o.Mode == "" {
		o.Mode = ModeAuto
	}
	switch o.Mode {
	case ModeAuto, ModeRichText, ModeMarkdownBlock, ModeSectionMrkdwn:
		// ok
	default:
		return fmt.Errorf("converter: invalid Mode %q (want auto|rich_text|markdown_block|section_mrkdwn)", o.Mode)
	}
	if o.MaxBlocksPerChunk == 0 {
		o.MaxBlocksPerChunk = MaxBlocksPerMessage
	}
	if o.MaxBlocksPerChunk < 1 || o.MaxBlocksPerChunk > MaxBlocksPerMessage {
		return fmt.Errorf("converter: MaxBlocksPerChunk=%d out of range [1, %d]",
			o.MaxBlocksPerChunk, MaxBlocksPerMessage)
	}
	if o.ParagraphCharLimit == 0 {
		o.ParagraphCharLimit = MaxSectionTextChars
	}
	if o.ParagraphCharLimit < 1 || o.ParagraphCharLimit > MaxSectionTextChars {
		return fmt.Errorf("converter: ParagraphCharLimit=%d out of range [1, %d]",
			o.ParagraphCharLimit, MaxSectionTextChars)
	}
	if o.MaxInputBytes < 0 {
		return fmt.Errorf("converter: MaxInputBytes=%d cannot be negative", o.MaxInputBytes)
	}
	if o.MaxInputBytes == 0 {
		// Caller explicitly opted out of bounding via DefaultOptions(); leave
		// at 0 only if they passed Options{} with everything zero — which we
		// detect by Mode having been empty above. Otherwise default it.
		o.MaxInputBytes = DefaultMaxInputBytes
	}
	if len(o.BlockIDPrefix) > MaxBlockIDChars-8 {
		// Reserve 8 chars for the sequence suffix we append.
		return fmt.Errorf("converter: BlockIDPrefix length %d leaves no room for sequence suffix",
			len(o.BlockIDPrefix))
	}
	return nil
}
