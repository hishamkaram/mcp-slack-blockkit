package converter

import (
	"fmt"

	"github.com/slack-go/slack"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
)

// ErrMarkdownBlockTooLarge is returned when input is requested to be emitted
// as a single Slack `markdown` block but exceeds the 12,000-char cumulative
// limit. Callers can decide to split or fall back to deterministic
// decomposition.
var ErrMarkdownBlockTooLarge = fmt.Errorf("input exceeds Slack markdown-block %d-char limit", MaxMarkdownBlockSum)

// emitMarkdownBlock packages the input as a single slack.MarkdownBlock.
// The text content is entity-escaped (via the same sanitizer used in
// renderInlines) unless Options.AllowBroadcasts is true, so a literal
// `<!channel>` in AI-generated input can't broadcast through this path.
// Real markdown syntax (autolinks like `<https://...>`, fenced code blocks)
// is content-only — Slack's markdown-block parser interprets the escaped
// text and renders it correctly because mrkdwn parsers run after the
// entity-decode step on Slack's side.
func (r *Renderer) emitMarkdownBlock(input string) ([]slack.Block, error) {
	if len(input) > MaxMarkdownBlockSum {
		return nil, fmt.Errorf("%w: %d chars", ErrMarkdownBlockTooLarge, len(input))
	}
	safe := input
	if !r.opts.AllowBroadcasts {
		safe = entityEscape(input)
	}
	// blockID on a markdown block is per Slack docs "ignored… and will not
	// be retained" — pass the empty string and let slack.NewMarkdownBlock
	// omit the field via omitempty.
	mb := slack.NewMarkdownBlock("", safe)
	return []slack.Block{mb}, nil
}

// shouldUseMarkdownBlock implements the auto-mode picker from research §4.
// Returns true when the input is a good fit for a single markdown block:
//   - Total length ≤ MaxMarkdownBlockSum (12,000)
//   - AST contains no Image nodes (markdown block doesn't render images per
//     Slack's documented supported syntax)
//   - AST contains no GFM table that exceeds Slack's row/col limits — small
//     tables render fine in the markdown block; large ones need TableBlock
//     splitting which the rich_text path provides
//
// Returns false in any other case, falling through to decomposition.
//
// The picker walks the document a single time before the main converter
// walk; the cost is a few microseconds for typical inputs and avoids the
// alternative of running the full converter twice.
func (r *Renderer) shouldUseMarkdownBlock(input string, root ast.Node) bool {
	if len(input) > MaxMarkdownBlockSum {
		return false
	}
	hasImage := false
	hasLargeTable := false
	walk(root, func(n ast.Node) (stop bool) {
		switch t := n.(type) {
		case *ast.Image:
			hasImage = true
			return true
		case *extast.Table:
			// Count rows and check the widest row's columns. We don't
			// need the per-cell content, just the shape.
			rows := 0
			cols := 0
			for c := t.FirstChild(); c != nil; c = c.NextSibling() {
				switch row := c.(type) {
				case *extast.TableHeader:
					rows++
					cols = max(cols, countCells(row))
				case *extast.TableRow:
					rows++
					cols = max(cols, countCells(row))
				}
			}
			if rows > maxTableRows || cols > maxTableCols {
				hasLargeTable = true
				return true
			}
		}
		return false
	})
	return !hasImage && !hasLargeTable
}

func countCells(row ast.Node) int {
	n := 0
	for c := row.FirstChild(); c != nil; c = c.NextSibling() {
		if _, ok := c.(*extast.TableCell); ok {
			n++
		}
	}
	return n
}
