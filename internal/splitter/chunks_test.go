package splitter

import (
	"testing"

	"github.com/slack-go/slack"
)

// makeDividers returns n divider blocks. Used as filler for count-limit tests.
func makeDividers(n int) []slack.Block {
	out := make([]slack.Block, n)
	for i := range out {
		out[i] = slack.NewDividerBlock()
	}
	return out
}

// makeTable returns a minimal table block; the contents are irrelevant
// for chunker tests, only the type matters.
func makeTable() slack.Block {
	return slack.NewTableBlock("")
}

// --- Empty / trivial inputs --------------------------------------------------

func TestChunkBlocks_NilInput_ReturnsNil(t *testing.T) {
	if got := ChunkBlocks(nil, 50); got != nil {
		t.Errorf("got %v chunks for nil input, want nil", got)
	}
}

func TestChunkBlocks_EmptyInput_ReturnsNil(t *testing.T) {
	if got := ChunkBlocks([]slack.Block{}, 50); got != nil {
		t.Errorf("got %v chunks for empty input, want nil", got)
	}
}

func TestChunkBlocks_SingleBlock_OneChunk(t *testing.T) {
	in := []slack.Block{slack.NewDividerBlock()}
	got := ChunkBlocks(in, 50)
	if len(got) != 1 || len(got[0]) != 1 {
		t.Errorf("got %d chunks of len %v, want 1 chunk of 1 block",
			len(got), chunkLensB(got))
	}
}

// --- Block-count rule -------------------------------------------------------

func TestChunkBlocks_BelowLimit_OneChunk(t *testing.T) {
	in := makeDividers(50)
	got := ChunkBlocks(in, 50)
	if len(got) != 1 {
		t.Errorf("got %d chunks for 50 blocks at limit 50, want 1", len(got))
	}
	if len(got[0]) != 50 {
		t.Errorf("chunk size = %d, want 50", len(got[0]))
	}
}

func TestChunkBlocks_ExactlyOneOverLimit_TwoChunks(t *testing.T) {
	in := makeDividers(51)
	got := ChunkBlocks(in, 50)
	if len(got) != 2 {
		t.Errorf("got %d chunks for 51 blocks at limit 50, want 2", len(got))
	}
	if len(got[0]) != 50 || len(got[1]) != 1 {
		t.Errorf("chunk sizes = %v, want [50 1]", chunkLensB(got))
	}
}

func TestChunkBlocks_ManyMultiples_AllChunksFull(t *testing.T) {
	in := makeDividers(150)
	got := ChunkBlocks(in, 50)
	if len(got) != 3 {
		t.Errorf("got %d chunks, want 3", len(got))
	}
	for i, c := range got {
		if len(c) != 50 {
			t.Errorf("chunk[%d] size = %d, want 50", i, len(c))
		}
	}
}

func TestChunkBlocks_DefaultLimit_50(t *testing.T) {
	// maxPerChunk <= 0 should fall back to the Slack default.
	in := makeDividers(75)
	got := ChunkBlocks(in, 0)
	if len(got) != 2 {
		t.Errorf("default limit should be 50; got %d chunks for 75 blocks", len(got))
	}
	if len(got[0]) != 50 || len(got[1]) != 25 {
		t.Errorf("chunk sizes = %v, want [50 25]", chunkLensB(got))
	}
}

func TestChunkBlocks_NegativeLimit_FallsBackToDefault(t *testing.T) {
	in := makeDividers(60)
	got := ChunkBlocks(in, -10)
	if len(got) != 2 {
		t.Errorf("negative limit should fall back to default; got %d chunks", len(got))
	}
}

// --- Table-isolation rule ---------------------------------------------------

func TestChunkBlocks_TwoTables_SplitIntoSeparateChunks(t *testing.T) {
	in := []slack.Block{makeTable(), makeTable()}
	got := ChunkBlocks(in, 50)
	if len(got) != 2 {
		t.Errorf("two tables should split into 2 chunks, got %d", len(got))
	}
	if len(got[0]) != 1 || len(got[1]) != 1 {
		t.Errorf("table chunks should each hold exactly 1 block; got sizes %v",
			chunkLensB(got))
	}
}

func TestChunkBlocks_TableSurroundedByDividers_SecondTableSplits(t *testing.T) {
	in := []slack.Block{
		slack.NewDividerBlock(),
		makeTable(),
		slack.NewDividerBlock(),
		makeTable(),
		slack.NewDividerBlock(),
	}
	got := ChunkBlocks(in, 50)
	if len(got) != 2 {
		t.Errorf("got %d chunks, want 2 (split before second table)", len(got))
	}
	// First chunk: divider + table + divider = 3 blocks
	// Second chunk: table + divider = 2 blocks
	if len(got[0]) != 3 || len(got[1]) != 2 {
		t.Errorf("chunk sizes = %v, want [3 2]", chunkLensB(got))
	}
}

func TestChunkBlocks_ManyTables_EachInOwnChunk(t *testing.T) {
	in := []slack.Block{makeTable(), makeTable(), makeTable(), makeTable()}
	got := ChunkBlocks(in, 50)
	if len(got) != 4 {
		t.Errorf("4 tables → 4 chunks, got %d", len(got))
	}
	for i, c := range got {
		if len(c) != 1 {
			t.Errorf("chunk[%d] size = %d, want 1 (table-only chunk)", i, len(c))
		}
	}
}

// --- Combined rules ---------------------------------------------------------

func TestChunkBlocks_BothRulesInteract_RespectsBoth(t *testing.T) {
	// Build: 49 dividers + table + 5 dividers + table + dividers.
	// Expected:
	//   chunk 0: 49 dividers + 1 table = 50 blocks (count limit hits next)
	//   chunk 1: 5 dividers + table = 6 blocks (table-isolation didn't fire
	//            because previous chunk closed at count limit)
	//   wait — when did the second table arrive? After 5 dividers in chunk 1.
	//   Chunk 1 has no table when we reach the second table, so it just
	//   appends. Result: chunk 1 has 5 dividers + 1 table = 6 blocks.
	//   chunk 2: only if more blocks follow.

	in := append(makeDividers(49), makeTable())
	in = append(in, makeDividers(5)...)
	in = append(in, makeTable())

	got := ChunkBlocks(in, 50)
	if len(got) != 2 {
		t.Errorf("got %d chunks, want 2; sizes=%v", len(got), chunkLensB(got))
	}
	if len(got[0]) != 50 {
		t.Errorf("chunk[0] size = %d, want 50", len(got[0]))
	}
	if len(got[1]) != 6 {
		t.Errorf("chunk[1] size = %d, want 6", len(got[1]))
	}
}

// --- Round-trip invariant ---------------------------------------------------

func TestChunkBlocks_RoundTripPreservesOrderAndContent(t *testing.T) {
	in := []slack.Block{
		slack.NewDividerBlock(),
		makeTable(),
		slack.NewDividerBlock(),
		makeTable(),
		slack.NewHeaderBlock(slack.NewTextBlockObject("plain_text", "h", false, false)),
		makeTable(),
	}
	got := ChunkBlocks(in, 50)

	// Re-flatten and assert identical sequence.
	var flat []slack.Block
	for _, c := range got {
		flat = append(flat, c...)
	}
	if len(flat) != len(in) {
		t.Fatalf("round-trip lost blocks: in=%d, out=%d", len(in), len(flat))
	}
	for i := range in {
		if flat[i] != in[i] {
			t.Errorf("block %d differs after round-trip", i)
		}
	}
}

func chunkLensB(chunks [][]slack.Block) []int {
	out := make([]int, len(chunks))
	for i, c := range chunks {
		out[i] = len(c)
	}
	return out
}
