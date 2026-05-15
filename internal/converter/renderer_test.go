package converter

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

// --- Options.validate / DefaultOptions ----------------------------------------

func TestDefaultOptions_ProducesValidConfig(t *testing.T) {
	opts := DefaultOptions()
	if err := opts.validate(); err != nil {
		t.Fatalf("DefaultOptions() failed validation: %v", err)
	}
	if opts.Mode != ModeAuto {
		t.Errorf("default Mode=%q, want %q", opts.Mode, ModeAuto)
	}
	if opts.MaxBlocksPerChunk != MaxBlocksPerMessage {
		t.Errorf("default MaxBlocksPerChunk=%d, want %d", opts.MaxBlocksPerChunk, MaxBlocksPerMessage)
	}
	if opts.ParagraphCharLimit != MaxSectionTextChars {
		t.Errorf("default ParagraphCharLimit=%d, want %d", opts.ParagraphCharLimit, MaxSectionTextChars)
	}
	if opts.MaxInputBytes != DefaultMaxInputBytes {
		t.Errorf("default MaxInputBytes=%d, want %d", opts.MaxInputBytes, DefaultMaxInputBytes)
	}
	if opts.AllowBroadcasts {
		t.Error("default AllowBroadcasts=true, want false (security default)")
	}
	if !opts.EnableTables {
		t.Error("default EnableTables=false, want true")
	}
	if opts.EmitStandaloneLinkAsButton {
		t.Error("default EmitStandaloneLinkAsButton=true, want false")
	}
}

func TestOptions_Validate(t *testing.T) {
	cases := []struct {
		name    string
		in      Options
		wantErr string // substring; empty = expect no error
		check   func(t *testing.T, o Options)
	}{
		{
			name: "zero value defaults to auto mode",
			in:   Options{},
			check: func(t *testing.T, o Options) {
				if o.Mode != ModeAuto {
					t.Errorf("Mode=%q, want %q", o.Mode, ModeAuto)
				}
				if o.MaxBlocksPerChunk != MaxBlocksPerMessage {
					t.Errorf("MaxBlocksPerChunk=%d, want %d", o.MaxBlocksPerChunk, MaxBlocksPerMessage)
				}
				if o.ParagraphCharLimit != MaxSectionTextChars {
					t.Errorf("ParagraphCharLimit=%d, want %d", o.ParagraphCharLimit, MaxSectionTextChars)
				}
				if o.MaxInputBytes != DefaultMaxInputBytes {
					t.Errorf("MaxInputBytes=%d, want %d", o.MaxInputBytes, DefaultMaxInputBytes)
				}
			},
		},
		{name: "invalid mode rejected", in: Options{Mode: "bogus"}, wantErr: "invalid Mode"},
		{name: "rich_text mode accepted", in: Options{Mode: ModeRichText}},
		{name: "markdown_block mode accepted", in: Options{Mode: ModeMarkdownBlock}},
		{name: "section_mrkdwn mode accepted", in: Options{Mode: ModeSectionMrkdwn}},
		{
			name:    "MaxBlocksPerChunk over Slack limit rejected",
			in:      Options{MaxBlocksPerChunk: MaxBlocksPerMessage + 1},
			wantErr: "MaxBlocksPerChunk",
		},
		{
			name:    "MaxBlocksPerChunk negative rejected",
			in:      Options{MaxBlocksPerChunk: -1},
			wantErr: "MaxBlocksPerChunk",
		},
		{
			name: "MaxBlocksPerChunk=1 (minimum) accepted",
			in:   Options{MaxBlocksPerChunk: 1},
			check: func(t *testing.T, o Options) {
				if o.MaxBlocksPerChunk != 1 {
					t.Errorf("MaxBlocksPerChunk=%d, want 1", o.MaxBlocksPerChunk)
				}
			},
		},
		{
			name:    "ParagraphCharLimit over Slack limit rejected",
			in:      Options{ParagraphCharLimit: MaxSectionTextChars + 1},
			wantErr: "ParagraphCharLimit",
		},
		{
			name:    "ParagraphCharLimit negative rejected",
			in:      Options{ParagraphCharLimit: -100},
			wantErr: "ParagraphCharLimit",
		},
		{
			name:    "MaxInputBytes negative rejected",
			in:      Options{MaxInputBytes: -1},
			wantErr: "MaxInputBytes",
		},
		{
			name:    "BlockIDPrefix too long rejected",
			in:      Options{BlockIDPrefix: strings.Repeat("x", MaxBlockIDChars)},
			wantErr: "BlockIDPrefix",
		},
		{
			name: "BlockIDPrefix at safe length accepted",
			in:   Options{BlockIDPrefix: strings.Repeat("x", MaxBlockIDChars-8)},
			check: func(t *testing.T, o Options) {
				if len(o.BlockIDPrefix) != MaxBlockIDChars-8 {
					t.Errorf("BlockIDPrefix length=%d, want %d", len(o.BlockIDPrefix), MaxBlockIDChars-8)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.in
			err := opts.validate()
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, opts)
			}
		})
	}
}

// --- Renderer construction ---------------------------------------------------

func TestNew_DefaultOptions_Succeeds(t *testing.T) {
	r, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("New(DefaultOptions()) failed: %v", err)
	}
	if r == nil {
		t.Fatal("New returned nil renderer")
	}
	got := r.Options()
	if got.Mode != ModeAuto {
		t.Errorf("Renderer.Options().Mode=%q, want %q", got.Mode, ModeAuto)
	}
}

func TestNew_RejectsInvalidOptions(t *testing.T) {
	_, err := New(Options{Mode: "bogus"})
	if err == nil {
		t.Fatal("New(invalid) returned nil error")
	}
	if !strings.Contains(err.Error(), "invalid Mode") {
		t.Errorf("error %q does not mention invalid mode", err)
	}
}

func TestNew_TablesDisabled_StillBuildsParser(t *testing.T) {
	r, err := New(Options{EnableTables: false})
	if err != nil {
		t.Fatalf("New with EnableTables=false failed: %v", err)
	}
	// Smoke: parse something with a table — it should not crash even though
	// the table extension is off (it just won't produce a TableBlock).
	blocks, err := r.Convert("| a | b |\n|---|---|\n| 1 | 2 |")
	if err != nil {
		t.Fatalf("Convert with EnableTables=false errored: %v", err)
	}
	if blocks == nil {
		t.Error("Convert returned nil blocks slice")
	}
}

// --- Convert (skeleton placeholder behavior) ---------------------------------

func TestConvert_EmptyInput_ReturnsEmptySlice(t *testing.T) {
	r, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	blocks, err := r.Convert("")
	if err != nil {
		t.Fatalf("Convert(\"\") errored: %v", err)
	}
	if blocks == nil {
		t.Error("Convert(\"\") returned nil; want non-nil empty slice")
	}
	if len(blocks) != 0 {
		t.Errorf("Convert(\"\") returned %d blocks; want 0", len(blocks))
	}
}

func TestConvert_ProducesValidBlockKitJSON(t *testing.T) {
	// Use ModeRichText explicitly: DefaultOptions uses ModeAuto, which
	// would route this short paragraph through the markdown_block path.
	// Auto-mode shape is asserted in markdown_block_test.go.
	r, err := New(Options{Mode: ModeRichText})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	blocks, err := r.Convert("hello world")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if len(blocks) == 0 {
		t.Fatal("Convert returned no blocks for non-empty input")
	}
	payload, err := json.Marshal(slack.Blocks{BlockSet: blocks})
	if err != nil {
		t.Fatalf("marshal blocks: %v", err)
	}
	if !strings.Contains(string(payload), "rich_text") {
		t.Errorf("expected rich_text block for paragraph; got %s", payload)
	}
	if !strings.Contains(string(payload), "hello world") {
		t.Errorf("expected input text in output; got %s", payload)
	}
}

func TestConvert_RespectsMaxInputBytes(t *testing.T) {
	r, err := New(Options{MaxInputBytes: 64})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tooBig := strings.Repeat("x", 65)
	_, err = r.Convert(tooBig)
	if err == nil {
		t.Fatal("Convert accepted oversized input")
	}
	if !errors.Is(err, ErrInputTooLarge) {
		t.Errorf("error %v is not ErrInputTooLarge", err)
	}
}

func TestConvert_DefaultMaxInputBytes_EnforcesCeiling(t *testing.T) {
	r, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Default is 256 KiB; build something just over.
	tooBig := strings.Repeat("a", DefaultMaxInputBytes+1)
	_, err = r.Convert(tooBig)
	if !errors.Is(err, ErrInputTooLarge) {
		t.Errorf("expected ErrInputTooLarge for input > 256 KiB, got %v", err)
	}
}

func TestConvert_DeeplyNestedInput_Rejected(t *testing.T) {
	r, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// 400 nested blockquotes — small in bytes, pathological in depth.
	deep := strings.Repeat("> ", 400) + "boom"
	_, err = r.Convert(deep)
	if err == nil {
		t.Fatal("Convert accepted pathologically deep input")
	}
	if !errors.Is(err, ErrInputTooDeeplyNested) {
		t.Errorf("error %v is not ErrInputTooDeeplyNested", err)
	}
}

func TestConvert_ModestNesting_Accepted(t *testing.T) {
	r, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// A few levels of nested lists/quotes is well within the depth limit.
	input := "> quote\n>\n> - a\n>   - b\n>     - c\n"
	if _, err := r.Convert(input); err != nil {
		t.Errorf("modestly nested input rejected: %v", err)
	}
}

func TestConvert_MaxNestingDepthZero_DefaultsTo100(t *testing.T) {
	opts := DefaultOptions()
	opts.MaxNestingDepth = 0 // should normalize to DefaultMaxNestingDepth
	r, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := r.Options().MaxNestingDepth; got != DefaultMaxNestingDepth {
		t.Errorf("MaxNestingDepth=%d, want default %d", got, DefaultMaxNestingDepth)
	}
}

func TestRenderer_OptionsCopy_NotAlias(t *testing.T) {
	// Defensive: Options() should return a value-copy so callers can't mutate
	// the renderer's internal state.
	r, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := r.Options()
	got.Mode = ModeRichText
	if r.Options().Mode != ModeAuto {
		t.Error("mutating Options() return value affected internal state")
	}
}
