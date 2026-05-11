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
//
// When Options.PreserveMentionTokens is true, the four trusted Slack
// token shapes (`<@U…>`, `<#C…>`, `<!subteam^S…>`, `<!date^…|fallback>`)
// pass through verbatim while the rest of the input is still escaped.
// This lets legitimate Slack-emitted mentions survive when the input came
// from an upstream Slack tool result.
func (r *Renderer) emitMarkdownBlock(input string) ([]slack.Block, error) {
	if len(input) > MaxMarkdownBlockSum {
		return nil, fmt.Errorf("%w: %d chars", ErrMarkdownBlockTooLarge, len(input))
	}
	safe := input
	switch {
	case r.opts.AllowBroadcasts:
		// No escaping at all.
	case r.opts.PreserveMentionTokens:
		safe = escapePreservingTokens(input)
	default:
		safe = entityEscape(input)
	}
	// blockID on a markdown block is per Slack docs "ignored… and will not
	// be retained" — pass the empty string and let slack.NewMarkdownBlock
	// omit the field via omitempty.
	mb := slack.NewMarkdownBlock("", safe)
	return []slack.Block{mb}, nil
}

// shouldUseMarkdownBlock implements the auto-mode picker.
// Returns true when the input is a good fit for a single markdown block:
//   - Total length ≤ MaxMarkdownBlockSum (12,000)
//   - AST contains no Image nodes (markdown block doesn't render images per
//     Slack's documented supported syntax)
//   - AST contains no GFM table that exceeds Slack's row/col limits — small
//     tables render fine in the markdown block; large ones need TableBlock
//     splitting which the rich_text path provides
//   - AST contains no "non-representable in rich_text" nesting pattern
//     (code-in-quote, code-in-list, table-in-quote, table-in-list,
//     list-in-quote). Slack's markdown-block rendering of these
//     combinations is undocumented and unverified — we route them
//     through rich_text decomposition instead so the visual outcome is
//     predictable. See containsBlockInBlock for the detection rules.
//
// Returns false in any other case, falling through to decomposition.
//
// The picker walks the document once before the main converter walk; the
// cost is a few microseconds for typical inputs and avoids the alternative
// of running the full converter twice.
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
	if hasImage || hasLargeTable {
		return false
	}
	if patterns := containsBlockInBlock(root); len(patterns) > 0 {
		return false
	}
	return true
}

// Pattern names returned by containsBlockInBlock. Exported as constants so
// tests and warning emitters can match on stable strings rather than
// reformatting message text.
const (
	PatternCodeInBlockquote  = "code-in-blockquote"
	PatternCodeInList        = "code-in-list"
	PatternTableInBlockquote = "table-in-blockquote"
	PatternTableInList       = "table-in-list"
	PatternListInBlockquote  = "list-in-blockquote"
)

// containsBlockInBlock walks the AST and returns the names of any
// "non-representable in rich_text" nesting patterns present. Returns an
// empty slice when the input is fully representable.
//
// "Non-representable" means: Slack's rich_text element schema can't hold
// the inner block as a child of the outer container. Specifically:
//   - rich_text_quote.elements is inline-only — it cannot directly contain
//     rich_text_preformatted, rich_text_list, or another rich_text_quote.
//   - rich_text_section.elements (which is what rich_text_list items are)
//     is inline-only — it cannot contain rich_text_preformatted.
//
// When any of these patterns is present, the renderer falls back to a
// "split-emit" decomposition: the outer construct is broken into multiple
// adjacent top-level rich_text blocks with the inner block emitted between.
// This is the same pattern md2slack uses; see plan §"Layer 2".
func containsBlockInBlock(root ast.Node) []string {
	seen := map[string]bool{}
	walk(root, func(n ast.Node) (stop bool) {
		switch n.(type) {
		case *ast.Blockquote:
			if hasDescendant(n, isCodeBlock) {
				seen[PatternCodeInBlockquote] = true
			}
			if hasDescendant(n, isTable) {
				seen[PatternTableInBlockquote] = true
			}
			if hasDescendant(n, isList) {
				seen[PatternListInBlockquote] = true
			}
		case *ast.ListItem:
			if hasDescendant(n, isCodeBlock) {
				seen[PatternCodeInList] = true
			}
			if hasDescendant(n, isTable) {
				seen[PatternTableInList] = true
			}
		}
		return false
	})
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	// Emit in stable order so warning text is deterministic across runs.
	for _, name := range []string{
		PatternCodeInBlockquote,
		PatternCodeInList,
		PatternTableInBlockquote,
		PatternTableInList,
		PatternListInBlockquote,
	} {
		if seen[name] {
			out = append(out, name)
		}
	}
	return out
}

// hasDescendant returns true if any node in the subtree rooted at parent
// (exclusive) satisfies pred. Excludes parent itself so callers can write
// "is this Blockquote containing a code block as a *descendant*" without
// matching a Blockquote that IS a code block (which can't happen, but the
// exclusion makes the helper composable).
func hasDescendant(parent ast.Node, pred func(ast.Node) bool) bool {
	found := false
	for c := parent.FirstChild(); c != nil && !found; c = c.NextSibling() {
		walk(c, func(n ast.Node) (stop bool) {
			if pred(n) {
				found = true
				return true
			}
			return false
		})
	}
	return found
}

func isCodeBlock(n ast.Node) bool {
	switch n.(type) {
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		return true
	}
	return false
}

func isTable(n ast.Node) bool {
	_, ok := n.(*extast.Table)
	return ok
}

func isList(n ast.Node) bool {
	_, ok := n.(*ast.List)
	return ok
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
