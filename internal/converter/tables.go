package converter

import (
	"github.com/slack-go/slack"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
)

// Slack TableBlock limits per
// https://docs.slack.dev/reference/block-kit/blocks/table-block/. Verified
// in research.md §3 — exceed these and chat.postMessage rejects the payload.
const (
	maxTableRows = 100 // header + data rows per single TableBlock
	maxTableCols = 20  // total columns per row
)

// handleTable emits one or more slack.TableBlock from a GFM table. When the
// table has more than maxTableRows data rows, we split into multiple
// TableBlocks with the header row replicated on each, and the splitter
// (separate package, step 12) later enforces Slack's only_one_table_allowed
// rule by breaking each TableBlock into its own outgoing message chunk.
//
// Columns beyond maxTableCols are truncated silently — the lint tool (step 18)
// surfaces this as a warning so users don't lose data quietly forever.
func (w *walker) handleTable(t *extast.Table) error {
	header, rows := w.collectTableRows(t)
	if len(header) == 0 && len(rows) == 0 {
		return nil
	}

	// Truncate columns beyond Slack's per-row limit.
	if len(header) > maxTableCols {
		header = header[:maxTableCols]
	}
	for i := range rows {
		if len(rows[i]) > maxTableCols {
			rows[i] = rows[i][:maxTableCols]
		}
	}

	colSettings := buildColumnSettings(t.Alignments)
	if len(colSettings) > maxTableCols {
		colSettings = colSettings[:maxTableCols]
	}

	// Slack TableBlock holds header + data rows in the same Rows slice.
	// We chunk data rows by maxTableRows-1 (header row counts toward the
	// 100-row ceiling) so the header is replicated on every chunk.
	dataPerChunk := maxTableRows - 1
	if len(header) == 0 {
		dataPerChunk = maxTableRows
	}
	if dataPerChunk < 1 {
		dataPerChunk = 1
	}

	if len(rows) == 0 {
		// Header-only table (rare but valid). Emit one TableBlock with header.
		tb := slack.NewTableBlock(w.nextBlockID())
		if len(colSettings) > 0 {
			tb.WithColumnSettings(colSettings...)
		}
		tb.AddRow(header...)
		w.blocks = append(w.blocks, tb)
		return nil
	}

	for offset := 0; offset < len(rows); offset += dataPerChunk {
		tb := slack.NewTableBlock(w.nextBlockID())
		if len(colSettings) > 0 {
			tb.WithColumnSettings(colSettings...)
		}
		if len(header) > 0 {
			tb.AddRow(header...)
		}
		end := offset + dataPerChunk
		if end > len(rows) {
			end = len(rows)
		}
		for _, r := range rows[offset:end] {
			tb.AddRow(r...)
		}
		w.blocks = append(w.blocks, tb)
	}
	return nil
}

// collectTableRows walks a Table node and returns its header cells and data
// rows, each cell rendered as a single-section rich_text_block (Slack's
// TableBlock cell shape).
func (w *walker) collectTableRows(t *extast.Table) ([]*slack.RichTextBlock, [][]*slack.RichTextBlock) {
	var (
		header []*slack.RichTextBlock
		rows   [][]*slack.RichTextBlock
	)
	for c := t.FirstChild(); c != nil; c = c.NextSibling() {
		switch n := c.(type) {
		case *extast.TableHeader:
			header = w.renderRowCells(n)
		case *extast.TableRow:
			rows = append(rows, w.renderRowCells(n))
		}
	}
	return header, rows
}

// renderRowCells iterates a TableRow's (or TableHeader's) cell children,
// rendering each as a rich_text_block containing one rich_text_section
// with the cell's inline content. Empty cells become an empty section so
// every row has the same shape — Slack rejects rows with mismatched cell
// counts.
func (w *walker) renderRowCells(parent ast.Node) []*slack.RichTextBlock {
	var cells []*slack.RichTextBlock
	for c := parent.FirstChild(); c != nil; c = c.NextSibling() {
		cell, ok := c.(*extast.TableCell)
		if !ok {
			continue
		}
		elements := renderInlinesWithOpts(cell, w.source, w.opts)
		if len(elements) == 0 {
			// Slack rejects a row with mismatched cell counts; an empty
			// rich_text_section is the safe placeholder.
			elements = []slack.RichTextSectionElement{
				slack.NewRichTextSectionTextElement("", nil),
			}
		}
		cells = append(cells, slack.NewRichTextBlock("", slack.NewRichTextSection(elements...)))
	}
	return cells
}

// buildColumnSettings translates goldmark per-column alignments into Slack
// ColumnSetting values. We default IsWrapped=true so long cells reflow
// rather than overflow horizontally — research.md §4 documents this
// preference, and it matches how md2slack ships table rendering.
func buildColumnSettings(alignments []extast.Alignment) []slack.ColumnSetting {
	out := make([]slack.ColumnSetting, len(alignments))
	for i, a := range alignments {
		out[i] = slack.ColumnSetting{
			Align:     mapAlignment(a),
			IsWrapped: true,
		}
	}
	return out
}

// mapAlignment translates goldmark's Alignment enum into Slack's ColumnAlignment.
// AlignNone maps to "left" (Slack's default visual alignment) so we don't
// emit an empty alignment string that some clients would reject.
func mapAlignment(a extast.Alignment) slack.ColumnAlignment {
	switch a {
	case extast.AlignLeft:
		return slack.ColumnAlignmentLeft
	case extast.AlignRight:
		return slack.ColumnAlignmentRight
	case extast.AlignCenter:
		return slack.ColumnAlignmentCenter
	default:
		return slack.ColumnAlignmentLeft
	}
}
