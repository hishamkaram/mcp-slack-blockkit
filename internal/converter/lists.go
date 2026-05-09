package converter

import (
	"github.com/slack-go/slack"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
)

// handleList emits one or more top-level rich_text blocks representing a
// markdown list. The simple case (only paragraphs and nested lists) produces
// a single rich_text block. The complex case (a list item contains a code
// block or table — non-representable inside a rich_text_section) is split:
// the in-progress list flushes as its own rich_text block, the inner block
// emits as a separate top-level block, and a new sibling list opens with
// `Offset` set so ordered numbering continues across the split.
func (w *walker) handleList(list *ast.List) error {
	var elements []slack.RichTextElement

	flushBlock := func() {
		if len(elements) == 0 {
			return
		}
		rt := slack.NewRichTextBlock(w.nextBlockID(), elements...)
		w.blocks = append(w.blocks, rt)
		elements = nil
	}

	if err := w.flattenList(list, 0, &elements, flushBlock); err != nil {
		return err
	}
	flushBlock()
	return nil
}

// flattenList walks one list level and appends rich_text_list elements to
// out. When a list ITEM contains a non-representable child (FencedCodeBlock,
// CodeBlock, Table), the in-progress list flushes, the inner block emits as
// its own top-level block via dispatchBlock, and the next items open a new
// sibling rich_text_list with `Offset` set for ordered-list continuity.
//
// Nested lists keep the existing sibling-with-incrementing-indent pattern —
// they are not "non-representable"; rich_text_list children that are
// themselves rich_text_list elements are simply not allowed by Slack's
// schema, so we represent the nesting by emitting siblings.
func (w *walker) flattenList(list *ast.List, depth int, out *[]slack.RichTextElement, flushBlock func()) error {
	style := slack.RTEListBullet
	if list.IsOrdered() {
		style = slack.RTEListOrdered
	}

	// baseStart is the natural starting number for the first item. For
	// ordered lists, defaults to 1 unless the markdown specifies a different
	// `start` (e.g. `5.` to begin at 5). For bullet lists, irrelevant.
	baseStart := 1
	if list.IsOrdered() && list.Start > 1 {
		baseStart = list.Start
	}

	// itemsConsumed counts list items emitted across ALL sibling
	// rich_text_list elements at this depth so we can derive Offset on each
	// new sibling. For a list split as [items 1,2] + (code) + [items 3,4]:
	// after the second flush itemsConsumed=4 and the new sibling has
	// len(run)=2, giving Offset = (baseStart-1) + (4-2) = 2 → first item
	// renders as "3" (Slack: Offset=N → first number = N+1).
	itemsConsumed := 0

	var run []slack.RichTextElement
	flushRun := func() {
		if len(run) == 0 {
			return
		}
		listEl := slack.NewRichTextList(style, depth, run...)
		offset := (baseStart - 1) + (itemsConsumed - len(run))
		if offset < 0 {
			offset = 0
		}
		listEl.Offset = offset
		*out = append(*out, listEl)
		run = nil
	}

	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		li, ok := item.(*ast.ListItem)
		if !ok {
			continue
		}

		// Detect non-representable children of this item. Nested lists are
		// NOT non-representable; they handle themselves below.
		var splitInner []ast.Node
		for c := li.FirstChild(); c != nil; c = c.NextSibling() {
			switch c.(type) {
			case *ast.FencedCodeBlock, *ast.CodeBlock, *extast.Table:
				splitInner = append(splitInner, c)
			}
		}

		if len(splitInner) > 0 {
			// Split the list around the inner block(s). The item's
			// pre-split content (paragraphs / inline-only children) becomes
			// the section for THIS list position; then we flush the list
			// and the rich_text block; then dispatch each inner block as
			// its own top-level block; subsequent items open a new sibling
			// list with continued Offset.
			sec := w.renderListItemSectionExcluding(li, splitInner)
			if sec != nil {
				run = append(run, sec)
				itemsConsumed++
			} else {
				// Even if the item has no pre-split content, count it as
				// consumed so the post-split numbering reflects its position.
				itemsConsumed++
			}
			flushRun()
			flushBlock()
			for _, inner := range splitInner {
				if err := w.dispatchBlock(inner); err != nil {
					return err
				}
			}
			continue
		}

		// Normal item: render the section, accumulate.
		sec := w.renderListItemSection(li)
		if sec != nil {
			run = append(run, sec)
			itemsConsumed++
		}

		// Walk children for nested lists in document order. When we hit one,
		// flush the current run so the nested list slots in BETWEEN the
		// items above and below it (matching Slack's sibling-with-indent
		// rendering convention).
		for c := li.FirstChild(); c != nil; c = c.NextSibling() {
			if nl, ok := c.(*ast.List); ok {
				flushRun()
				if err := w.flattenList(nl, depth+1, out, flushBlock); err != nil {
					return err
				}
			}
		}
	}
	flushRun()
	return nil
}

// renderListItemSection produces the rich_text_section for one list item.
// Inline content (links, emphasis, code) is preserved via the inline
// renderer; nested lists are skipped here because they're emitted as
// siblings by the parent flattener. Task-list checkboxes prepend a literal
// "[x] " / "[ ] " text element since Slack messages have no native checkbox.
// Returns nil if the item is effectively empty (e.g. a list-item that only
// contains a nested list).
func (w *walker) renderListItemSection(li *ast.ListItem) *slack.RichTextSection {
	return w.renderListItemSectionExcluding(li, nil)
}

// renderListItemSectionExcluding is like renderListItemSection but skips
// any of the listed AST nodes when building the section content. Used by
// the split-emit path: the code blocks / tables that triggered the split
// must NOT also appear as text in the section, since they're being emitted
// as their own top-level blocks.
func (w *walker) renderListItemSectionExcluding(li *ast.ListItem, exclude []ast.Node) *slack.RichTextSection {
	var elements []slack.RichTextSectionElement

	// Task-list checkbox prefix. We deliberately do NOT include a trailing
	// space here — goldmark preserves the source space between `]` and the
	// first character as the leading space of the next Text node, so adding
	// our own would produce `[x]  done`. For the rare `- [x]done` (no source
	// space), the output is `[x]done`, which faithfully echoes the input.
	if cb, ok := findTaskCheckBox(li); ok {
		prefix := "[ ]"
		if cb.IsChecked {
			prefix = "[x]"
		}
		elements = append(elements, slack.NewRichTextSectionTextElement(prefix, nil))
	}

	skip := make(map[ast.Node]bool, len(exclude))
	for _, n := range exclude {
		skip[n] = true
	}

	// Walk block-level children of the list item, excluding nested lists
	// (handled as siblings by the parent flattener) and excluded nodes
	// (passed in by the split-emit path).
	for c := li.FirstChild(); c != nil; c = c.NextSibling() {
		if skip[c] {
			continue
		}
		if _, ok := c.(*ast.List); ok {
			continue
		}
		els := renderInlinesWithOpts(c, w.source, w.opts)
		// Insert a separating space between consecutive block children so
		// adjacent paragraphs (loose lists) don't collide on the boundary.
		if len(elements) > 0 && len(els) > 0 {
			elements = append(elements, slack.NewRichTextSectionTextElement(" ", nil))
		}
		elements = append(elements, els...)
	}

	if len(elements) == 0 {
		return nil
	}
	return slack.NewRichTextSection(elements...)
}

// findTaskCheckBox returns the TaskCheckBox node attached to the first
// paragraph (or text-block) child of li, if any. extension.TaskList places
// it as the first inline child of the item's first block child — so we
// only need to inspect each block child's FirstChild, not iterate inline
// siblings.
func findTaskCheckBox(li *ast.ListItem) (*extast.TaskCheckBox, bool) {
	for c := li.FirstChild(); c != nil; c = c.NextSibling() {
		first := c.FirstChild()
		if first == nil {
			continue
		}
		if cb, ok := first.(*extast.TaskCheckBox); ok {
			return cb, true
		}
	}
	return nil, false
}
