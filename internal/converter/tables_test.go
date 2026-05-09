package converter

import (
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

// firstTable returns the first slack.TableBlock from the converted output.
func firstTable(t *testing.T, blocks []slack.Block) *slack.TableBlock {
	t.Helper()
	for _, b := range blocks {
		if tb, ok := b.(*slack.TableBlock); ok {
			return tb
		}
	}
	t.Fatal("no TableBlock in output")
	return nil
}

// allTables returns every TableBlock in the output.
func allTables(blocks []slack.Block) []*slack.TableBlock {
	var out []*slack.TableBlock
	for _, b := range blocks {
		if tb, ok := b.(*slack.TableBlock); ok {
			out = append(out, tb)
		}
	}
	return out
}

// cellText returns the concatenated plain text of a cell.
func cellText(cell *slack.RichTextBlock) string {
	var sb strings.Builder
	for _, el := range cell.Elements {
		sec, ok := el.(*slack.RichTextSection)
		if !ok {
			continue
		}
		for _, inner := range sec.Elements {
			if t, ok := inner.(*slack.RichTextSectionTextElement); ok {
				sb.WriteString(t.Text)
			}
		}
	}
	return sb.String()
}

// --- Basic table emission ---------------------------------------------------

func TestHandleTable_SimpleTable_EmitsTableBlock(t *testing.T) {
	input := `| h1 | h2 |
|----|----|
| a  | b  |
| c  | d  |
`
	blocks, _ := renderForTest(t, Options{}, input)
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks, want 1 TableBlock", len(blocks))
	}
	tb := firstTable(t, blocks)
	// 1 header row + 2 data rows = 3 rows total
	if len(tb.Rows) != 3 {
		t.Fatalf("got %d rows, want 3 (header + 2 data)", len(tb.Rows))
	}
	if cellText(tb.Rows[0][0]) != "h1" || cellText(tb.Rows[0][1]) != "h2" {
		t.Errorf("header row = [%q,%q]", cellText(tb.Rows[0][0]), cellText(tb.Rows[0][1]))
	}
	if cellText(tb.Rows[1][0]) != "a" || cellText(tb.Rows[1][1]) != "b" {
		t.Errorf("data row 1 = [%q,%q]", cellText(tb.Rows[1][0]), cellText(tb.Rows[1][1]))
	}
}

// --- Alignment --------------------------------------------------------------

func TestHandleTable_Alignment_LeftCenterRight(t *testing.T) {
	input := `| L  | C  | R  |
|:---|:--:|---:|
| a  | b  | c  |
`
	blocks, _ := renderForTest(t, Options{}, input)
	tb := firstTable(t, blocks)
	if len(tb.ColumnSettings) != 3 {
		t.Fatalf("got %d column settings, want 3", len(tb.ColumnSettings))
	}
	want := []slack.ColumnAlignment{
		slack.ColumnAlignmentLeft,
		slack.ColumnAlignmentCenter,
		slack.ColumnAlignmentRight,
	}
	for i, w := range want {
		if tb.ColumnSettings[i].Align != w {
			t.Errorf("column %d align = %q, want %q", i, tb.ColumnSettings[i].Align, w)
		}
		if !tb.ColumnSettings[i].IsWrapped {
			t.Errorf("column %d should default to IsWrapped=true", i)
		}
	}
}

// --- Inline formatting in cells --------------------------------------------

func TestHandleTable_InlineFormattingInCells_Preserved(t *testing.T) {
	input := "| header | other |\n|---|---|\n| **bold** | `code` |\n| [link](https://example.com) | plain |\n"
	blocks, _ := renderForTest(t, Options{}, input)
	tb := firstTable(t, blocks)
	if len(tb.Rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(tb.Rows))
	}

	// Row 1, col 0: bold style
	cell := tb.Rows[1][0]
	sec := cell.Elements[0].(*slack.RichTextSection)
	if s := styleOf(sec, "bold"); s == nil || !s.Bold {
		t.Errorf("bold cell style = %+v", s)
	}

	// Row 1, col 1: code style
	cell = tb.Rows[1][1]
	sec = cell.Elements[0].(*slack.RichTextSection)
	if s := styleOf(sec, "code"); s == nil || !s.Code {
		t.Errorf("code cell style = %+v", s)
	}

	// Row 2, col 0: link element
	cell = tb.Rows[2][0]
	sec = cell.Elements[0].(*slack.RichTextSection)
	var hasLink bool
	for _, el := range sec.Elements {
		if _, ok := el.(*slack.RichTextSectionLinkElement); ok {
			hasLink = true
		}
	}
	if !hasLink {
		t.Error("link cell missing rich_text_section_link element")
	}
}

// --- Row-overflow chunking --------------------------------------------------

func TestHandleTable_OverRowLimit_SplitsIntoMultipleTablesWithReplicatedHeader(t *testing.T) {
	// Build a table with maxTableRows + 1 = 101 data rows. Expected: two
	// TableBlocks. First chunk: header + 99 data rows (header counts toward
	// the 100 ceiling, so dataPerChunk = 99). Second chunk: header + 2
	// remaining data rows.
	const dataRows = maxTableRows + 1
	dataPerChunk := maxTableRows - 1 // 99

	var sb strings.Builder
	sb.WriteString("| col |\n|---|\n")
	for i := 0; i < dataRows; i++ {
		sb.WriteString("| row")
		sb.WriteString(itoa(i))
		sb.WriteString(" |\n")
	}

	blocks, _ := renderForTest(t, Options{}, sb.String())
	tables := allTables(blocks)

	wantChunks := (dataRows + dataPerChunk - 1) / dataPerChunk
	if len(tables) != wantChunks {
		t.Fatalf("got %d TableBlocks, want %d", len(tables), wantChunks)
	}

	// Every chunk starts with the header row.
	for i, tb := range tables {
		if len(tb.Rows) == 0 {
			t.Fatalf("chunk %d has no rows", i)
		}
		if cellText(tb.Rows[0][0]) != "col" {
			t.Errorf("chunk %d header[0] = %q, want %q", i, cellText(tb.Rows[0][0]), "col")
		}
	}

	// Sum of data rows across chunks should equal the original count.
	totalData := 0
	for _, tb := range tables {
		// Subtract 1 for the replicated header.
		totalData += len(tb.Rows) - 1
	}
	if totalData != dataRows {
		t.Errorf("data rows total = %d, want %d", totalData, dataRows)
	}
}

// --- Column-overflow truncation --------------------------------------------

func TestHandleTable_OverColumnLimit_TruncatesColumns(t *testing.T) {
	// Build a table with maxTableCols + 5 columns.
	cols := maxTableCols + 5
	var hdr, sep, row strings.Builder
	hdr.WriteString("|")
	sep.WriteString("|")
	row.WriteString("|")
	for i := 0; i < cols; i++ {
		hdr.WriteString(" h")
		hdr.WriteString(itoa(i))
		hdr.WriteString(" |")
		sep.WriteString("---|")
		row.WriteString(" v")
		row.WriteString(itoa(i))
		row.WriteString(" |")
	}
	input := hdr.String() + "\n" + sep.String() + "\n" + row.String() + "\n"

	blocks, _ := renderForTest(t, Options{}, input)
	tb := firstTable(t, blocks)
	if len(tb.Rows[0]) != maxTableCols {
		t.Errorf("header cell count = %d, want %d (truncated)", len(tb.Rows[0]), maxTableCols)
	}
	if len(tb.Rows[1]) != maxTableCols {
		t.Errorf("data cell count = %d, want %d (truncated)", len(tb.Rows[1]), maxTableCols)
	}
	if len(tb.ColumnSettings) > maxTableCols {
		t.Errorf("column settings count = %d, want ≤%d", len(tb.ColumnSettings), maxTableCols)
	}
}

// --- Empty cells ------------------------------------------------------------

func TestHandleTable_EmptyCell_StillEmittedForShape(t *testing.T) {
	// A row with an empty cell must still produce a cell — Slack rejects
	// rows with mismatched cell counts, so the placeholder is mandatory.
	input := "| a | b |\n|---|---|\n| value |  |\n"
	blocks, _ := renderForTest(t, Options{}, input)
	tb := firstTable(t, blocks)
	if len(tb.Rows[1]) != 2 {
		t.Errorf("data row cell count = %d, want 2", len(tb.Rows[1]))
	}
}

// --- Table interleaved with other blocks ------------------------------------

func TestHandleTable_FollowedByParagraph_BothEmitted(t *testing.T) {
	input := "| a | b |\n|---|---|\n| 1 | 2 |\n\nAfter the table.\n"
	blocks, _ := renderForTest(t, Options{}, input)
	if len(blocks) != 2 {
		t.Fatalf("got %d blocks, want 2 (table + paragraph)", len(blocks))
	}
	if _, ok := blocks[0].(*slack.TableBlock); !ok {
		t.Errorf("blocks[0] = %T, want TableBlock", blocks[0])
	}
	if _, ok := blocks[1].(*slack.RichTextBlock); !ok {
		t.Errorf("blocks[1] = %T, want RichTextBlock paragraph", blocks[1])
	}
}

// --- Tables disabled --------------------------------------------------------

func TestHandleTable_TablesDisabledOption_FallsThroughAsText(t *testing.T) {
	r, err := New(Options{EnableTables: false})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	blocks, err := r.Convert("| a | b |\n|---|---|\n| 1 | 2 |\n")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	for _, b := range blocks {
		if _, ok := b.(*slack.TableBlock); ok {
			t.Errorf("EnableTables=false should not emit TableBlock; got %T", b)
		}
	}
}

// --- itoa helper (avoid pulling strconv into _test.go for one int format) --

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		b = append([]byte{'0' + byte(n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
