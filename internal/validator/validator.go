// Package validator validates Slack Block Kit payloads against the
// constraints documented at https://docs.slack.dev/reference/block-kit/.
// It returns structured Violation values keyed by JSON path so callers
// (the validate_blockkit MCP tool, the lint_blockkit MCP tool, the
// converter's pre-flight check) can surface actionable errors back to
// the LLM or human user.
//
// Hand-rolled rather than go-playground/validator with shadow structs:
// the constraint set is small enough (about 30 rules across 10 block
// types) that the indirection of mirror types + struct tags adds more
// reading cost than the validator engine saves. Research.md §5
// recommended the hybrid path; in practice the hand-rolled approach is
// ~150 lines for the same coverage with no external dep.
package validator

import (
	"fmt"
	"strconv"

	"github.com/slack-go/slack"
)

// Slack-defined limits. These are exported so the converter and the
// lint tool can reference the same constants. Sourced from research.md §3.
const (
	MaxBlocksPerMessage  = 50
	MaxSectionTextChars  = 3000
	MaxSectionFieldsLen  = 2000
	MaxSectionFieldCount = 10
	MaxHeaderTextChars   = 150
	MaxBlockIDChars      = 255
	MaxButtonTextChars   = 75
	MaxButtonValueChars  = 2000
	MaxButtonURLChars    = 3000
	MaxImageAltTextChars = 2000
	MaxImageURLChars     = 3000
	MaxActionsElements   = 25
	MaxMarkdownTotal     = 12000
)

// Severity classifies a violation. Errors must be fixed before send;
// Warnings indicate usage that may be problematic but is technically
// valid (deprecated patterns, near-limit content).
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Violation describes one validation failure with enough detail for an
// LLM or developer to fix it. Path uses dot/bracket notation to point at
// the offending field (e.g. `blocks[3].text.text`). FixHint is an
// optional human-readable suggestion.
type Violation struct {
	Severity Severity `json:"severity"`
	Path     string   `json:"path"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	FixHint  string   `json:"fix_hint,omitempty"`
}

// Result carries the outcome of one validation call. Valid is true iff
// Errors is empty (warnings do not invalidate the payload).
type Result struct {
	Valid    bool        `json:"valid"`
	Errors   []Violation `json:"errors"`
	Warnings []Violation `json:"warnings"`
}

// Validator is the entry point. New() returns a ready-to-use validator;
// it carries no per-call state, so a single instance can be shared across
// goroutines.
type Validator struct {
	strict bool
}

// New returns a Validator that reports only Slack-published constraint
// violations as errors.
func New() *Validator {
	return &Validator{}
}

// NewStrict returns a Validator that additionally reports deprecated
// patterns (e.g. raw `mrkdwn` where `rich_text` is now strongly
// preferred per Slack docs) as errors rather than warnings.
func NewStrict() *Validator {
	return &Validator{strict: true}
}

// Validate runs the full constraint suite over the block list and returns
// a structured Result.
func (v *Validator) Validate(blocks []slack.Block) Result {
	var errs, warns []Violation

	add := func(sev Severity, path, code, msg, fix string) {
		violation := Violation{Severity: sev, Path: path, Code: code, Message: msg, FixHint: fix}
		if sev == SeverityError {
			errs = append(errs, violation)
		} else {
			warns = append(warns, violation)
		}
	}

	// Rule 1 (cross-block): max blocks per message.
	if len(blocks) > MaxBlocksPerMessage {
		add(SeverityError, "blocks", "blocks_per_message_exceeded",
			fmt.Sprintf("message has %d blocks; Slack maximum is %d", len(blocks), MaxBlocksPerMessage),
			"split the payload into multiple messages, or use the split_blocks tool")
	}

	// Rule 2 (cross-block): block_id uniqueness within the message.
	seenIDs := map[string]int{}
	for i, b := range blocks {
		id := b.ID()
		if id == "" {
			continue
		}
		if prev, ok := seenIDs[id]; ok {
			add(SeverityError, blocksPath(i)+".block_id", "duplicate_block_id",
				fmt.Sprintf("block_id %q is reused (first seen at blocks[%d])", id, prev),
				"give each block a distinct block_id, or omit it entirely")
		} else {
			seenIDs[id] = i
		}
	}

	// Rule 3 (cross-block): only one TableBlock per message
	// (Slack's only_one_table_allowed). The chunker enforces this on
	// the converter side, but a directly-constructed payload might
	// still violate.
	tableCount := 0
	for i, b := range blocks {
		if _, isTable := b.(*slack.TableBlock); isTable {
			tableCount++
			if tableCount > 1 {
				add(SeverityError, blocksPath(i), "multiple_tables",
					"more than one TableBlock in the same message",
					"split the payload — each chunk should contain at most one TableBlock")
				break
			}
		}
	}

	// Rule 4 (cross-block): markdown_block cumulative text limit.
	mdTotal := 0
	for _, b := range blocks {
		if mb, ok := b.(*slack.MarkdownBlock); ok {
			mdTotal += len(mb.Text)
		}
	}
	if mdTotal > MaxMarkdownTotal {
		add(SeverityError, "blocks", "markdown_block_total_exceeded",
			fmt.Sprintf("cumulative markdown_block text is %d chars; Slack limit is %d", mdTotal, MaxMarkdownTotal),
			"shorten the markdown blocks or split into multiple messages")
	}

	// Per-block validation.
	for i, b := range blocks {
		v.validateBlock(b, blocksPath(i), &errs, &warns)
	}

	return Result{
		Valid:    len(errs) == 0,
		Errors:   errs,
		Warnings: warns,
	}
}

func (v *Validator) validateBlock(b slack.Block, path string, errs, warns *[]Violation) {
	add := func(sev Severity, p, code, msg, fix string) {
		violation := Violation{Severity: sev, Path: p, Code: code, Message: msg, FixHint: fix}
		if sev == SeverityError {
			*errs = append(*errs, violation)
		} else {
			*warns = append(*warns, violation)
		}
	}

	if id := b.ID(); len(id) > MaxBlockIDChars {
		add(SeverityError, path+".block_id", "block_id_too_long",
			fmt.Sprintf("block_id is %d chars; max %d", len(id), MaxBlockIDChars),
			"shorten the block_id or omit it")
	}

	switch t := b.(type) {
	case *slack.SectionBlock:
		v.validateSection(t, path, add)
	case *slack.HeaderBlock:
		v.validateHeader(t, path, add)
	case *slack.ImageBlock:
		v.validateImage(t, path, add)
	case *slack.ActionBlock:
		v.validateActions(t, path, add)
	case *slack.MarkdownBlock:
		// Per-block markdown text length is enforced by the cumulative
		// rule above; nothing additional to check per-block.
	case *slack.DividerBlock, *slack.RichTextBlock, *slack.TableBlock,
		*slack.ContextBlock, *slack.FileBlock, *slack.VideoBlock:
		// These have constraints we surface via their nested objects
		// (RichTextBlock has no documented per-string limit per §3).
	}
}

func (v *Validator) validateSection(s *slack.SectionBlock, path string, add func(Severity, string, string, string, string)) {
	hasText := s.Text != nil && s.Text.Text != ""
	hasFields := len(s.Fields) > 0

	// XOR rule: must have text or fields, exclusive use is fine but at
	// least one is required.
	if !hasText && !hasFields {
		add(SeverityError, path, "section_empty",
			"section block has neither text nor fields",
			"set Text or Fields (or both)")
	}

	if s.Text != nil {
		if l := len(s.Text.Text); l > MaxSectionTextChars {
			add(SeverityError, path+".text.text", "section_text_too_long",
				fmt.Sprintf("section text is %d chars; max %d", l, MaxSectionTextChars),
				"split the text or use the split_blocks tool")
		}
		if v.strict && s.Text.Type == slack.MarkdownType {
			add(SeverityError, path+".text.type", "deprecated_mrkdwn_section",
				"strict mode rejects mrkdwn-only section text in favor of rich_text",
				"use a rich_text block, or set strict=false to allow")
		}
	}

	if len(s.Fields) > MaxSectionFieldCount {
		add(SeverityError, path+".fields", "too_many_fields",
			fmt.Sprintf("section has %d fields; max %d", len(s.Fields), MaxSectionFieldCount),
			"reduce the number of fields")
	}
	for j, f := range s.Fields {
		if f == nil {
			continue
		}
		if l := len(f.Text); l > MaxSectionFieldsLen {
			add(SeverityError,
				path+".fields["+strconv.Itoa(j)+"].text",
				"section_field_too_long",
				fmt.Sprintf("section field %d is %d chars; max %d", j, l, MaxSectionFieldsLen),
				"shorten the field text")
		}
	}
}

func (v *Validator) validateHeader(h *slack.HeaderBlock, path string, add func(Severity, string, string, string, string)) {
	if h.Text == nil {
		add(SeverityError, path+".text", "header_missing_text", "header block has no text", "set Text")
		return
	}
	if h.Text.Type != slack.PlainTextType {
		add(SeverityError, path+".text.type", "header_must_be_plain_text",
			fmt.Sprintf("header text type is %q; must be %q", h.Text.Type, slack.PlainTextType),
			"convert to a plain_text text object")
	}
	if l := len(h.Text.Text); l > MaxHeaderTextChars {
		add(SeverityError, path+".text.text", "header_text_too_long",
			fmt.Sprintf("header text is %d chars; max %d", l, MaxHeaderTextChars),
			"shorten the heading or fall back to a bold section.mrkdwn block")
	}
	if h.Text.Text == "" {
		add(SeverityError, path+".text.text", "header_text_empty",
			"header text is empty", "provide non-empty heading text")
	}
}

func (v *Validator) validateImage(img *slack.ImageBlock, path string, add func(Severity, string, string, string, string)) {
	if img.ImageURL == "" && img.SlackFile == nil {
		add(SeverityError, path+".image_url", "image_missing_source",
			"image block has no image_url or slack_file",
			"set image_url or slack_file")
	}
	if img.AltText == "" {
		add(SeverityWarning, path+".alt_text", "image_missing_alt_text",
			"image alt_text is empty",
			"add descriptive alt_text for accessibility")
	}
	if l := len(img.AltText); l > MaxImageAltTextChars {
		add(SeverityError, path+".alt_text", "alt_text_too_long",
			fmt.Sprintf("alt_text is %d chars; max %d", l, MaxImageAltTextChars),
			"shorten the alt text")
	}
	if l := len(img.ImageURL); l > MaxImageURLChars {
		add(SeverityError, path+".image_url", "image_url_too_long",
			fmt.Sprintf("image_url is %d chars; max %d", l, MaxImageURLChars),
			"use a shorter URL or upload the image to Slack as a file")
	}
}

func (v *Validator) validateActions(a *slack.ActionBlock, path string, add func(Severity, string, string, string, string)) {
	if a.Elements == nil {
		return
	}
	if n := len(a.Elements.ElementSet); n > MaxActionsElements {
		add(SeverityError, path+".elements", "too_many_actions",
			fmt.Sprintf("actions block has %d elements; max %d", n, MaxActionsElements),
			"split the actions across multiple actions blocks")
	}
}

// blocksPath returns the JSON path for blocks[i].
func blocksPath(i int) string {
	return "blocks[" + strconv.Itoa(i) + "]"
}
