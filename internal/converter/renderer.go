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
		extensions = append(extensions,
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

// Convert turns markdown into a flat slice of Slack blocks. The slice is
// always non-nil but may be empty for whitespace-only input.
//
// Mode dispatch:
//   - ModeMarkdownBlock: emit a single slack.MarkdownBlock with the raw
//     markdown text. Returns ErrMarkdownBlockTooLarge if input >12,000 chars.
//   - ModeAuto: peek at the AST to decide between markdown_block (short
//     LLM-style outputs with no images and no oversized tables) and full
//     decomposition. Falls through to decomposition when the picker says no.
//   - ModeRichText / ModeSectionMrkdwn: always run the full decomposition
//     walker. (ModeSectionMrkdwn doesn't yet emit section blocks for
//     paragraphs — the v0.1 walker produces rich_text. A future step can
//     add a section.mrkdwn-only path if downstream consumers need it.)
//
// All paths share the same MaxInputBytes ceiling (configurable via Options).
func (r *Renderer) Convert(input string) ([]slack.Block, error) {
	if r.opts.MaxInputBytes > 0 && len(input) > r.opts.MaxInputBytes {
		return nil, fmt.Errorf("%w: %d bytes (limit %d)",
			ErrInputTooLarge, len(input), r.opts.MaxInputBytes)
	}

	// Empty / whitespace-only input: every mode collapses to an empty
	// block list. Without this short-circuit ModeAuto would emit an
	// empty markdown block, which Slack rejects.
	if strings.TrimSpace(input) == "" {
		return []slack.Block{}, nil
	}

	// ModeMarkdownBlock skips the AST walk entirely — we hand the raw
	// (sanitized) input to Slack's markdown block parser.
	if r.opts.Mode == ModeMarkdownBlock {
		return r.emitMarkdownBlock(input)
	}

	src := []byte(input)
	root := r.gm.Parser().Parse(text.NewReader(src))
	if root == nil {
		return nil, fmt.Errorf("converter: goldmark returned nil AST for input of %d bytes", len(input))
	}

	if r.opts.Mode == ModeAuto && r.shouldUseMarkdownBlock(input, root) {
		return r.emitMarkdownBlock(input)
	}

	w := &walker{
		opts:   r.opts,
		source: src,
		blocks: make([]slack.Block, 0, 4),
	}
	if err := w.walkDocument(root); err != nil {
		return nil, err
	}
	return w.blocks, nil
}
