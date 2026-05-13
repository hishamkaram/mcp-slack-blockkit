package converter

import (
	"fmt"
	"strings"

	"github.com/slack-go/slack"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// Renderer converts markdown into Slack Block Kit blocks. It holds a
// preconfigured goldmark.Markdown and is safe to reuse across requests
// (goldmark documents the parser as reusable). Construct one per process.
type Renderer struct {
	opts Options
	gm   goldmark.Markdown
}

// New constructs a Renderer with the given options. Pass DefaultOptions()
// for the recommended defaults.
func New(opts Options) (*Renderer, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	extensions := []goldmark.Extender{}
	if opts.EnableTables {
		// extension.GFM bundles tables, strikethrough, linkify, and task lists
		// — exactly the four GFM features research.md §2 calls out as required.
		extensions = append(extensions, extension.GFM)
	} else {
		// Without GFM, still pick up strike/linkify/task lists so we don't
		// silently drop them when the caller only wants tables disabled.
		extensions = append(
			extensions,
			extension.Strikethrough,
			extension.Linkify,
			extension.TaskList,
		)
	}

	gm := goldmark.New(
		goldmark.WithExtensions(extensions...),
		goldmark.WithParserOptions(
			// Source-position attributes let us produce useful error messages
			// in lint/validate output that point back at the original markdown.
			parser.WithAttribute(),
		),
	)

	return &Renderer{opts: opts, gm: gm}, nil
}

// Options returns a copy of the Renderer's options. Useful for tests and
// for the MCP `validate` tool, which surfaces config back to the caller.
func (r *Renderer) Options() Options {
	return r.opts
}

// Convert turns markdown into a flat slice of Slack blocks. Convenience
// wrapper around ConvertWithWarnings that drops the warnings slice — use
// ConvertWithWarnings when you want to surface fallback notes to callers
// (e.g. "input contains code-in-blockquote; using rich_text decomposition").
func (r *Renderer) Convert(input string) ([]slack.Block, error) {
	blocks, _, err := r.ConvertWithWarnings(input)
	return blocks, err
}

// ConvertWithWarnings is the full API. Same blocks output as Convert plus
// a slice of human-readable warnings explaining mode-fallback decisions
// the caller should know about. Warnings are NEVER errors — every warning
// path still produces a valid Block Kit payload.
//
// Mode dispatch:
//   - ModeMarkdownBlock: emit a single slack.MarkdownBlock with the raw
//     markdown text. Returns ErrMarkdownBlockTooLarge if input >12,000 chars.
//   - ModeAuto: peek at the AST to decide between markdown_block (short
//     LLM-style outputs with no images, no oversized tables, and no
//     non-representable nesting patterns) and full rich_text decomposition.
//     When the picker falls through specifically because of a nested-block
//     pattern, ConvertWithWarnings emits one warning naming the patterns,
//     so callers can flag the visual-fidelity tradeoff.
//   - ModeRichText / ModeSectionMrkdwn: always run the full decomposition
//     walker. No warnings are emitted in these modes — the user explicitly
//     opted in to the decomposition path.
//
// All paths share the same MaxInputBytes ceiling (configurable via Options).
func (r *Renderer) ConvertWithWarnings(input string) ([]slack.Block, []string, error) {
	if r.opts.MaxInputBytes > 0 && len(input) > r.opts.MaxInputBytes {
		return nil, nil, fmt.Errorf("%w: %d bytes (limit %d)",
			ErrInputTooLarge, len(input), r.opts.MaxInputBytes)
	}

	// Empty / whitespace-only input: every mode collapses to an empty
	// block list. Without this short-circuit ModeAuto would emit an
	// empty markdown block, which Slack rejects.
	if strings.TrimSpace(input) == "" {
		return []slack.Block{}, nil, nil
	}

	// Pre-parse rewrite: convert Slack mrkdwn URL-form `<URL|label>` into
	// CommonMark `[label](URL)` so both modes can handle it via the
	// regular Link path. Idempotent on inputs that don't contain the
	// pattern. See internal/converter/slack_mrkdwn_links.go.
	input = rewriteSlackURLForms(input)

	// Parse once, then dispatch on mode. Previously ModeMarkdownBlock
	// short-circuited before the parse; now the markdown_block emitter is
	// AST-driven and needs the AST, so the parse runs unconditionally.
	src := []byte(input)
	root := r.gm.Parser().Parse(text.NewReader(src))
	if root == nil {
		return nil, nil, fmt.Errorf("converter: goldmark returned nil AST for input of %d bytes", len(input))
	}

	if r.opts.Mode == ModeMarkdownBlock {
		blocks, err := r.emitMarkdownBlock(root, src)
		return blocks, nil, err
	}

	var warnings []string
	if r.opts.Mode == ModeAuto {
		if r.shouldUseMarkdownBlock(input, root) {
			blocks, err := r.emitMarkdownBlock(root, src)
			return blocks, nil, err
		}
		// Picker said no. If the reason was a nested-block pattern (the
		// new gating layer), emit one warning naming the patterns so the
		// caller knows visual fidelity dropped relative to a markdown_block
		// rendering. Other reasons (image, large table, oversized input)
		// don't warn — those are documented decomposition paths.
		if patterns := containsBlockInBlock(root); len(patterns) > 0 {
			warnings = append(warnings, formatNestedPatternWarning(patterns))
		}
	}

	w := &walker{
		opts:   r.opts,
		source: src,
		blocks: make([]slack.Block, 0, 4),
	}
	if err := w.walkDocument(root); err != nil {
		return nil, nil, err
	}
	return w.blocks, warnings, nil
}

// formatNestedPatternWarning produces a single, deterministic warning
// string naming each detected non-representable-in-rich_text nesting
// pattern. Stable text so test fixtures can match exactly.
func formatNestedPatternWarning(patterns []string) string {
	return "auto mode routed to rich_text decomposition because input contains " +
		strings.Join(patterns, ", ") +
		"; the rich_text schema can't embed those constructs, so the " +
		"output uses adjacent top-level blocks (visual rendering may " +
		"differ from CommonMark embedded form). Use mode=markdown_block " +
		"to delegate rendering to Slack's parser instead."
}
