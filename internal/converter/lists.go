package converter

import (
	"github.com/slack-go/slack"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
)

// handleList emits a rich_text block containing one or more rich_text_list
// elements. Slack rejects nested rich_text_list children, so nested markdown
// lists are flattened into sibling rich_text_list elements with incrementing
// indent values: a 2-level nested list becomes (outer items @indent=0,
// nested items @indent=1, remaining outer items @indent=0).
func (w *walker) handleList(list *ast.List) error {
	var elements []slack.RichTextElement
	w.flattenList(list, 0, &elements)
	if len(elements) == 0 {
		return nil
	}
	rt := slack.NewRichTextBlock(w.nextBlockID(), elements...)
	w.blocks = append(w.blocks, rt)
	return nil
}

// flattenList walks one list level and appends rich_text_list elements to out.
// It maintains a "run" of consecutive same-depth items and flushes the run
// each time a nested list is encountered, so document-order is preserved
// across the parent → nested → parent transitions Slack uses for nesting.
func (w *walker) flattenList(list *ast.List, depth int, out *[]slack.RichTextElement) {
	style := slack.RTEListBullet
	if list.IsOrdered() {
		style = slack.RTEListOrdered
	}

	// offset is set only on the first emitted sibling for an ordered list
	// with a non-default Start (e.g. `3.` to begin numbering at 3). Slack's
	// `offset` is relative: 0 means "start at 1 / the marker's natural number."
	var firstOffset int
	if list.IsOrdered() && list.Start > 1 {
		firstOffset = list.Start - 1
	}
	firstFlushDone := false

	var run []slack.RichTextElement
	flush := func() {
		if len(run) == 0 {
			return
		}
		listEl := slack.NewRichTextList(style, depth, run...)
		if !firstFlushDone {
			listEl.Offset = firstOffset
			firstFlushDone = true
		}
		*out = append(*out, listEl)
		run = nil
	}

	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		li, ok := item.(*ast.ListItem)
		if !ok {
			continue
		}

		sec := w.renderListItemSection(li)
		if sec != nil {
			run = append(run, sec)
		}

		// Walk children for nested lists in document order. When we hit one,
		// flush the current run so the nested list slots in BETWEEN the
		// items above and below it (matching Slack's sibling-with-indent
		// rendering convention).
		for c := li.FirstChild(); c != nil; c = c.NextSibling() {
			if nl, ok := c.(*ast.List); ok {
				flush()
				w.flattenList(nl, depth+1, out)
			}
		}
	}
	flush()
}

// renderListItemSection produces the rich_text_section for one list item.
// Inline content (links, emphasis, code) is preserved via the inline
// renderer; nested lists are skipped here because they're emitted as
// siblings by the parent flattener. Task-list checkboxes prepend a literal
// "[x] " / "[ ] " text element since Slack messages have no native checkbox.
// Returns nil if the item is effectively empty (e.g. a list-item that only
// contains a nested list).
func (w *walker) renderListItemSection(li *ast.ListItem) *slack.RichTextSection {
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

	// Walk block-level children of the list item, excluding nested lists.
	// A list item typically has a single Paragraph child holding the inline
	// content; loose-list items may have multiple block children. We feed
	// each non-list child through the inline renderer and concatenate the
	// element streams.
	for c := li.FirstChild(); c != nil; c = c.NextSibling() {
		if _, ok := c.(*ast.List); ok {
			continue
		}
		els := renderInlinesWithOpts(c, w.source, w.opts)
		// Insert a separating space between consecutive block children so
		// adjacent paragraphs don't collide on the boundary.
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
