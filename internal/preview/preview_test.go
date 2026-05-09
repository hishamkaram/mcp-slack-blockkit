package preview

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

// --- Basic URL shape --------------------------------------------------------

func TestBuilderURL_StartsWithCanonicalHost(t *testing.T) {
	r, err := BuilderURL([]slack.Block{slack.NewDividerBlock()})
	if err != nil {
		t.Fatalf("BuilderURL: %v", err)
	}
	if !strings.HasPrefix(r.URL, BuilderHost) {
		t.Errorf("URL does not start with %q; got %q", BuilderHost, r.URL)
	}
	if !strings.Contains(r.URL, "#") {
		t.Errorf("URL has no fragment marker: %q", r.URL)
	}
}

func TestBuilderURL_EmptyBlocks_StillProducesValidURL(t *testing.T) {
	r, err := BuilderURL(nil)
	if err != nil {
		t.Fatalf("BuilderURL: %v", err)
	}
	if r.URL == "" {
		t.Error("URL should not be empty even for nil blocks")
	}
	// Verify the fragment decodes to {"blocks":null}.
	frag := r.URL[strings.Index(r.URL, "#")+1:]
	decoded, err := url.QueryUnescape(frag)
	if err != nil {
		t.Fatalf("fragment decode failed: %v", err)
	}
	var got struct {
		Blocks []slack.Block `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(decoded), &got); err != nil {
		t.Fatalf("payload unmarshal failed: %v\nraw=%q", err, decoded)
	}
}

// --- Round-trip invariant ---------------------------------------------------

func TestBuilderURL_RoundTripDecodesToOriginalBlocks(t *testing.T) {
	in := []slack.Block{
		slack.NewHeaderBlock(slack.NewTextBlockObject(slack.PlainTextType, "Title", false, false)),
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, "*body* text", false, false),
			nil, nil,
		),
		slack.NewDividerBlock(),
	}
	r, err := BuilderURL(in)
	if err != nil {
		t.Fatalf("BuilderURL: %v", err)
	}

	frag := r.URL[strings.Index(r.URL, "#")+1:]
	decoded, err := url.QueryUnescape(frag)
	if err != nil {
		t.Fatalf("fragment decode: %v", err)
	}
	var got struct {
		Blocks json.RawMessage `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(decoded), &got); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}

	// Re-marshal the original blocks and compare against the decoded
	// fragment; this proves the fragment is bit-for-bit our payload.
	wantJSON, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal original: %v", err)
	}
	if string(got.Blocks) != string(wantJSON) {
		t.Errorf("round-trip mismatch.\nwant=%s\ngot =%s", wantJSON, got.Blocks)
	}
}

// --- Truncated flag ---------------------------------------------------------

func TestBuilderURL_BelowBudget_NotTruncated(t *testing.T) {
	r, err := BuilderURL([]slack.Block{slack.NewDividerBlock()})
	if err != nil {
		t.Fatalf("BuilderURL: %v", err)
	}
	if r.Truncated {
		t.Errorf("tiny payload marked truncated; ByteSize=%d", r.ByteSize)
	}
}

func TestBuilderURL_OverBudget_FlagsAsTruncated(t *testing.T) {
	// Build something definitely over PracticalURLBudget.
	long := strings.Repeat("a", PracticalURLBudget*2)
	mb := slack.NewMarkdownBlock("", long)
	r, err := BuilderURL([]slack.Block{mb})
	if err != nil {
		t.Fatalf("BuilderURL: %v", err)
	}
	if !r.Truncated {
		t.Errorf("oversized payload should set Truncated=true; ByteSize=%d", r.ByteSize)
	}
}

// --- BuilderURLString convenience wrapper -----------------------------------

func TestBuilderURLString_DropsMetadata(t *testing.T) {
	url, err := BuilderURLString([]slack.Block{slack.NewDividerBlock()})
	if err != nil {
		t.Fatalf("BuilderURLString: %v", err)
	}
	if !strings.HasPrefix(url, BuilderHost) {
		t.Errorf("URL = %q, want prefix %q", url, BuilderHost)
	}
}

// --- ByteSize accuracy ------------------------------------------------------

func TestBuilderURL_ByteSizeMatchesURL(t *testing.T) {
	r, err := BuilderURL([]slack.Block{slack.NewDividerBlock()})
	if err != nil {
		t.Fatalf("BuilderURL: %v", err)
	}
	if r.ByteSize != len(r.URL) {
		t.Errorf("ByteSize=%d, len(URL)=%d", r.ByteSize, len(r.URL))
	}
}

// --- URL encoding correctness ----------------------------------------------

func TestBuilderURL_FragmentIsValidlyEscaped(t *testing.T) {
	// Block content with characters that need escaping.
	in := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, "hello /?&#=", false, false),
			nil, nil,
		),
	}
	r, err := BuilderURL(in)
	if err != nil {
		t.Fatalf("BuilderURL: %v", err)
	}
	frag := r.URL[strings.Index(r.URL, "#")+1:]
	if _, err := url.QueryUnescape(frag); err != nil {
		t.Errorf("fragment is not validly URL-encoded: %v", err)
	}
}
