package splitter

import "github.com/slack-go/slack"

// DefaultMaxBlocksPerChunk is the per-message ceiling enforced by Slack
// (50 for `chat.postMessage`). Sourced from the Block Kit overview, see
// research.md §3.
const DefaultMaxBlocksPerChunk = 50

// ChunkBlocks splits a flat list of blocks into one or more message-sized
// chunks. Two rules drive the chunk boundaries:
//
//  1. **Block count**: a chunk holds at most maxPerChunk blocks. When
//     adding the next block would exceed the limit, a new chunk opens.
//
//  2. **Table isolation**: Slack rejects messages containing more than one
//     `table` block (`only_one_table_allowed`). When the next block is a
//     TableBlock and the current chunk already contains a TableBlock, we
//     open a new chunk before adding it.
//
// Returns nil for nil/empty input. maxPerChunk ≤ 0 falls back to the
// Slack default (DefaultMaxBlocksPerChunk).
//
// Pure function — no allocation beyond the result slice and its inner
// chunks. Safe to call from any goroutine.
func ChunkBlocks(blocks []slack.Block, maxPerChunk int) [][]slack.Block {
	if len(blocks) == 0 {
		return nil
	}
	if maxPerChunk <= 0 {
		maxPerChunk = DefaultMaxBlocksPerChunk
	}

	var (
		chunks       [][]slack.Block
		current      []slack.Block
		currentTable bool
	)
	flush := func() {
		if len(current) == 0 {
			return
		}
		chunks = append(chunks, current)
		current = nil
		currentTable = false
	}

	for _, b := range blocks {
		_, isTable := b.(*slack.TableBlock)

		// Rule 2: a TableBlock incoming when the chunk already has one →
		// flush before appending.
		if isTable && currentTable {
			flush()
		}
		// Rule 1: count limit reached → flush before appending.
		if len(current) >= maxPerChunk {
			flush()
		}

		current = append(current, b)
		if isTable {
			currentTable = true
		}
	}
	flush()
	return chunks
}
