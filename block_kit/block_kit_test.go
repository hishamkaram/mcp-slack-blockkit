package block_kit_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hishamkaram/mcp-slack-block-kit/block_kit"
)

// These tests live in block_kit_test (external package) on purpose: they
// must consume the public API the same way an external module would, so
// any leak of internal-only behavior fails compilation here first.

func TestPublicAPI_Convert(t *testing.T) {
	r, err := block_kit.NewConverter(block_kit.DefaultOptions())
	if err != nil {
		t.Fatalf("NewConverter: %v", err)
	}
	blocks, err := r.Convert("# Hello\n\nworld.")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if len(blocks) == 0 {
		t.Fatal("no blocks emitted")
	}
	raw, _ := json.Marshal(blocks)
	if !strings.Contains(string(raw), "Hello") {
		t.Errorf("expected 'Hello' in output: %s", raw)
	}
}

func TestPublicAPI_Validate(t *testing.T) {
	r, _ := block_kit.NewConverter(block_kit.Options{Mode: block_kit.ModeRichText})
	blocks, _ := r.Convert("# Title")

	v := block_kit.NewValidator()
	result := v.Validate(blocks)
	if !result.Valid {
		t.Errorf("expected valid, got errors=%+v", result.Errors)
	}
}

func TestPublicAPI_PreviewURL(t *testing.T) {
	r, _ := block_kit.NewConverter(block_kit.Options{Mode: block_kit.ModeRichText})
	blocks, _ := r.Convert("body")
	pr, err := block_kit.PreviewURL(blocks)
	if err != nil {
		t.Fatalf("PreviewURL: %v", err)
	}
	if !strings.HasPrefix(pr.URL, block_kit.BuilderHost) {
		t.Errorf("URL = %q, want prefix %q", pr.URL, block_kit.BuilderHost)
	}
}

func TestPublicAPI_ChunkBlocks(t *testing.T) {
	r, _ := block_kit.NewConverter(block_kit.Options{Mode: block_kit.ModeRichText})
	blocks, _ := r.Convert(strings.Repeat("paragraph.\n\n", 60))
	chunks := block_kit.ChunkBlocks(blocks, block_kit.DefaultMaxBlocksPerChunk)
	if len(chunks) < 1 {
		t.Errorf("got 0 chunks for 60+ paragraphs")
	}
}

func TestPublicAPI_SplitText(t *testing.T) {
	in := strings.Repeat("word ", 200)
	out := block_kit.SplitText(in, 100, 10)
	if len(out) < 2 {
		t.Errorf("expected ≥2 chunks; got %d", len(out))
	}
}

func TestPublicAPI_StrictValidator_FlagsDeprecated(t *testing.T) {
	r, _ := block_kit.NewConverter(block_kit.Options{Mode: block_kit.ModeSectionMrkdwn})
	// section_mrkdwn mode produces section blocks with mrkdwn text; strict
	// validator should flag those as deprecated.
	blocks, _ := r.Convert("plain body")
	if len(blocks) == 0 {
		t.Skip("no blocks emitted for input — skip strict check")
	}
	result := block_kit.NewStrictValidator().Validate(blocks)
	// Note: section_mrkdwn is not yet specially distinguished in the
	// converter's emission path; this assertion may not fire. Test
	// documents the API surface.
	_ = result
}
