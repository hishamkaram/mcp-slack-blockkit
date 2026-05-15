// Package server tests use the official SDK's in-memory transport pair
// (mcp.NewInMemoryTransports) so each test spins up a real server, sends
// real JSON-RPC requests via a real client, and asserts on real
// CallToolResult values. No subprocess, no flakiness.
package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newTestServer starts a fresh Server and returns a connected client
// session ready for CallTool. The cleanup closure stops the server and
// closes the session.
func newTestServer(t *testing.T) (*mcp.ClientSession, func()) {
	t.Helper()

	srv, err := New("test")
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	// In-memory transport pair: one end attached to the server, the other
	// to the client. No bytes ever cross a real socket.
	srvTransport, cliTransport := mcp.NewInMemoryTransports()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	go func() {
		// Run the server in the background. It exits when the transport
		// is closed (via the client session Close below).
		_ = srv.MCP().Run(ctx, srvTransport)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, cliTransport, nil)
	if err != nil {
		cancel()
		t.Fatalf("client.Connect: %v", err)
	}

	return session, func() {
		_ = session.Close()
		cancel()
	}
}

// callTool is a tiny wrapper that JSON-encodes args, calls the tool, and
// returns the result + any unmarshal'd structured output.
func callTool(t *testing.T, session *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	if r == nil {
		t.Fatalf("CallTool(%s) returned nil result", name)
	}
	return r
}

// extractStructured pulls the structured output JSON from a successful
// CallToolResult (the SDK puts it under StructuredContent or as JSON in
// the first text content block).
func extractStructured(t *testing.T, r *mcp.CallToolResult, target any) {
	t.Helper()
	if r.IsError {
		t.Fatalf("tool returned error: %s", contentText(r))
	}
	if r.StructuredContent != nil {
		raw, err := json.Marshal(r.StructuredContent)
		if err != nil {
			t.Fatalf("marshal structured content: %v", err)
		}
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatalf("unmarshal structured content: %v\nraw=%s", err, raw)
		}
		return
	}
	// Fall back to the first text content block (the SDK serializes
	// structured output as JSON text when StructuredContent is nil).
	body := contentText(r)
	if body == "" {
		t.Fatal("no content in result")
	}
	if err := json.Unmarshal([]byte(body), target); err != nil {
		t.Fatalf("unmarshal text content: %v\nbody=%s", err, body)
	}
}

func contentText(r *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range r.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(t.Text)
		}
	}
	return sb.String()
}

// --- convert_markdown_to_block_kit -------------------------------------------

// blocksJSON re-marshals out.Blocks (an `any`) so substring assertions
// can probe the wire shape regardless of whether the SDK delivered the
// payload as a slice of maps or a slice of typed values.
func blocksJSON(t *testing.T, out ConvertOutput) string {
	t.Helper()
	raw, err := json.Marshal(out.Blocks)
	if err != nil {
		t.Fatalf("marshal out.Blocks: %v", err)
	}
	return string(raw)
}

func TestConvertTool_HappyPath(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	r := callTool(t, session, "convert_markdown_to_block_kit", ConvertInput{
		Markdown: "# Title\n\nbody text here.",
		Mode:     "rich_text",
	})

	var out ConvertOutput
	extractStructured(t, r, &out)
	body := blocksJSON(t, out)
	if body == "null" || body == "[]" {
		t.Fatal("got empty blocks")
	}
	if !strings.Contains(body, "header") {
		t.Errorf("expected header block in output: %s", body)
	}
	if !strings.Contains(body, "rich_text") {
		t.Errorf("expected rich_text block in output: %s", body)
	}
}

func TestConvertTool_AutoMode_PicksMarkdownBlock(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	r := callTool(t, session, "convert_markdown_to_block_kit", ConvertInput{
		Markdown: "Just a short paragraph.",
		Mode:     "auto",
	})

	var out ConvertOutput
	extractStructured(t, r, &out)

	body := blocksJSON(t, out)
	if !strings.Contains(body, "markdown") {
		t.Errorf("auto mode should pick markdown block for short prose: %s", body)
	}
}

func TestConvertTool_MentionMap_ResolvesUsers(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	r := callTool(t, session, "convert_markdown_to_block_kit", ConvertInput{
		Markdown:   "ping @alice for review",
		Mode:       "rich_text",
		MentionMap: map[string]string{"alice": "U123ABC"},
	})

	var out ConvertOutput
	extractStructured(t, r, &out)

	body := blocksJSON(t, out)
	if !strings.Contains(body, "U123ABC") {
		t.Errorf("expected resolved user ID in output: %s", body)
	}
}

func TestConvertTool_AllowBroadcastsFalse_EscapesChannelMention(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	r := callTool(t, session, "convert_markdown_to_block_kit", ConvertInput{
		Markdown: "alert <!channel> please",
		Mode:     "rich_text",
	})

	var out ConvertOutput
	extractStructured(t, r, &out)

	body := blocksJSON(t, out)
	// Raw <!channel> must NOT survive (would broadcast in Slack).
	// JSON encoder escapes & to &, so check for both forms.
	if strings.Contains(body, `<!channel>`) && !strings.Contains(body, `&lt;`) {
		t.Errorf("raw <!channel> survived: %s", body)
	}
}

func TestConvertTool_PreserveMentionTokens_PassesUserButEscapesBroadcast(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	r := callTool(t, session, "convert_markdown_to_block_kit", ConvertInput{
		Markdown:              "Ping <@U012AB3CD> in <#C123ABC456>, then <!channel>",
		Mode:                  "rich_text",
		PreserveMentionTokens: true,
	})

	var out ConvertOutput
	extractStructured(t, r, &out)
	body := blocksJSON(t, out)

	// Typed user + channel must appear in the wire output.
	if !strings.Contains(body, `"user_id":"U012AB3CD"`) {
		t.Errorf("expected typed user element with U012AB3CD: %s", body)
	}
	if !strings.Contains(body, `"channel_id":"C123ABC456"`) {
		t.Errorf("expected typed channel element with C123ABC456: %s", body)
	}
	// <!channel> must NOT survive as a raw token. Go's encoding/json
	// HTMLEscape turns `&` into the literal sequence `&`, so the
	// escaped Block Kit text `&lt;!channel&gt;` shows up in the wire
	// body as `&lt;!channel&gt;`.
	if strings.Contains(body, "<!channel>") {
		t.Errorf("raw <!channel> must not survive with PreserveMentionTokens=true: %s", body)
	}
	// json.Marshal escapes `&` to the six-byte sequence &
	// (HTMLEscape safety), so the entity-escaped `&lt;!channel&gt;`
	// from the converter shows up in the wire body as the literal
	// twelve-byte sequence "&lt;!channel&gt;".
	if !strings.Contains(body, "\\u0026lt;!channel\\u0026gt;") {
		t.Errorf("expected escaped <!channel>: %s", body)
	}
}

func TestConvertTool_PreviewURLEnabled_ReturnsBuilderURL(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	r := callTool(t, session, "convert_markdown_to_block_kit", ConvertInput{
		Markdown:         "simple body",
		Mode:             "rich_text",
		ReturnPreviewURL: boolPtr(true),
	})

	var out ConvertOutput
	extractStructured(t, r, &out)

	if !strings.HasPrefix(out.PreviewURL, "https://app.slack.com/block-kit-builder/") {
		t.Errorf("preview URL = %q, want Block Kit Builder prefix", out.PreviewURL)
	}
}

func TestConvertTool_PreviewURLDisabled_OmitsBuilderURL(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	r := callTool(t, session, "convert_markdown_to_block_kit", ConvertInput{
		Markdown:         "simple body",
		Mode:             "rich_text",
		ReturnPreviewURL: boolPtr(false),
	})

	var out ConvertOutput
	extractStructured(t, r, &out)

	if out.PreviewURL != "" {
		t.Errorf("preview URL = %q, want empty when return_preview_url=false", out.PreviewURL)
	}
}

func TestConvertTool_PreviewURLDefault_IncludedWhenOmitted(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	// ReturnPreviewURL omitted (nil) — the default is to include the URL.
	r := callTool(t, session, "convert_markdown_to_block_kit", ConvertInput{
		Markdown: "simple body",
		Mode:     "rich_text",
	})

	var out ConvertOutput
	extractStructured(t, r, &out)

	if !strings.HasPrefix(out.PreviewURL, "https://app.slack.com/block-kit-builder/") {
		t.Errorf("preview URL = %q, want it included by default", out.PreviewURL)
	}
}

// --- validate_block_kit ------------------------------------------------------

func TestValidateTool_ValidPayload_ReturnsValidTrue(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	blocks := []any{map[string]any{"type": "divider"}}
	r := callTool(t, session, "validate_block_kit", ValidateInput{Blocks: blocks})

	var out ValidateOutput
	extractStructured(t, r, &out)
	if !out.Valid {
		t.Errorf("expected valid=true; got errors=%+v", out.Errors)
	}
}

func TestValidateTool_HeaderTooLong_ReturnsError(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	tooLong := strings.Repeat("h", 200)
	blocks := []any{map[string]any{
		"type": "header",
		"text": map[string]any{"type": "plain_text", "text": tooLong},
	}}
	r := callTool(t, session, "validate_block_kit", ValidateInput{Blocks: blocks})

	var out ValidateOutput
	extractStructured(t, r, &out)
	if out.Valid {
		t.Error("expected valid=false for >150-char header")
	}
	var foundCode bool
	for _, e := range out.Errors {
		if e.Code == "header_text_too_long" {
			foundCode = true
		}
	}
	if !foundCode {
		t.Errorf("missing header_text_too_long error; got %+v", out.Errors)
	}
}

func TestValidateTool_NoInput_ReturnsToolError(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	r := callTool(t, session, "validate_block_kit", ValidateInput{})
	if !r.IsError {
		t.Error("expected isError=true when neither blocks nor payload provided")
	}
}

// --- preview_block_kit -------------------------------------------------------

func TestPreviewTool_ReturnsBuilderURL(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	blocks := []any{map[string]any{"type": "divider"}}
	r := callTool(t, session, "preview_block_kit", PreviewInput{Blocks: blocks})

	var out PreviewOutput
	extractStructured(t, r, &out)
	if !strings.HasPrefix(out.PreviewURL, "https://app.slack.com/block-kit-builder/") {
		t.Errorf("preview URL = %q", out.PreviewURL)
	}
	if out.ByteSize == 0 {
		t.Error("ByteSize should be non-zero")
	}
}

// --- lint_block_kit ----------------------------------------------------------

func TestLintTool_NearLimitHeader_FlagsWarning(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	// 145 chars = 96% of the 150 limit
	near := strings.Repeat("h", 145)
	blocks := []any{map[string]any{
		"type": "header",
		"text": map[string]any{"type": "plain_text", "text": near},
	}}
	r := callTool(t, session, "lint_block_kit", LintInput{Blocks: blocks})
	var out LintOutput
	extractStructured(t, r, &out)
	var found bool
	for _, f := range out.Findings {
		if f.Code == "header_near_limit" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing header_near_limit finding; got %+v", out.Findings)
	}
}

func TestLintTool_HappyPath_NoFindings(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	blocks := []any{map[string]any{"type": "divider"}}
	r := callTool(t, session, "lint_block_kit", LintInput{Blocks: blocks})
	var out LintOutput
	extractStructured(t, r, &out)
	if len(out.Findings) != 0 {
		t.Errorf("expected no findings for trivial payload; got %+v", out.Findings)
	}
}

// --- split_blocks ----------------------------------------------------------

func TestSplitTool_FewBlocks_NoSplit(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	blocks := []any{
		map[string]any{"type": "divider"},
		map[string]any{"type": "divider"},
	}
	r := callTool(t, session, "split_blocks", SplitInput{Blocks: blocks})
	var out SplitOutput
	extractStructured(t, r, &out)
	if out.ChunkCount != 1 {
		t.Errorf("expected 1 chunk for 2 blocks; got %d", out.ChunkCount)
	}
}

func TestSplitTool_OverLimit_SplitsIntoChunks(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	// Build 60 dividers as []any of map[string]any.
	blocks := make([]any, 60)
	for i := range blocks {
		blocks[i] = map[string]any{"type": "divider"}
	}

	r := callTool(t, session, "split_blocks", SplitInput{Blocks: blocks})
	var out SplitOutput
	extractStructured(t, r, &out)
	if out.ChunkCount != 2 {
		t.Errorf("expected 2 chunks for 60 blocks; got %d", out.ChunkCount)
	}
}

// --- tool metadata / hygiene ------------------------------------------------

func TestTools_AllAdvertiseReadOnlyAnnotations(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) == 0 {
		t.Fatal("no tools registered")
	}
	for _, tool := range res.Tools {
		if tool.Annotations == nil {
			t.Errorf("tool %q has no annotations", tool.Name)
			continue
		}
		if !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %q should advertise readOnlyHint=true", tool.Name)
		}
		if tool.Annotations.Title == "" {
			t.Errorf("tool %q has no annotation title", tool.Name)
		}
	}
}

func TestConvertTool_InvalidSplit_ReturnsError(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	r := callTool(t, session, "convert_markdown_to_block_kit", ConvertInput{
		Markdown: "hello",
		Split:    "sideways",
	})
	if !r.IsError {
		t.Error("expected an error result for an unknown split strategy")
	}
}

func TestValidateTool_ModalSurface_Allows100Blocks(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	blocks := make([]any, 100)
	for i := range blocks {
		blocks[i] = map[string]any{"type": "divider"}
	}

	// Default (message) surface rejects 100 blocks.
	rMsg := callTool(t, session, "validate_block_kit", ValidateInput{Blocks: blocks})
	var outMsg ValidateOutput
	extractStructured(t, rMsg, &outMsg)
	if outMsg.Valid {
		t.Error("100 blocks should be invalid for the default message surface")
	}

	// Modal surface accepts 100 blocks.
	rModal := callTool(t, session, "validate_block_kit", ValidateInput{
		Blocks:  blocks,
		Surface: "modal",
	})
	var outModal ValidateOutput
	extractStructured(t, rModal, &outModal)
	if !outModal.Valid {
		t.Errorf("100 blocks should be valid for a modal; errors=%+v", outModal.Errors)
	}
}

func TestValidateTool_InvalidSurface_ReturnsError(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	r := callTool(t, session, "validate_block_kit", ValidateInput{
		Blocks:  []any{map[string]any{"type": "divider"}},
		Surface: "spaceship",
	})
	if !r.IsError {
		t.Error("expected an error result for an unknown surface")
	}
}

// --- block_kit_to_markdown ---------------------------------------------------

func TestReverseTool_RoundTrip_RecoversContent(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	// Convert markdown to blocks, then convert the blocks back.
	conv := callTool(t, session, "convert_markdown_to_block_kit", ConvertInput{
		Markdown: "Some **bold** text.\n\n- a\n- b\n",
		Mode:     "rich_text",
	})
	var convOut ConvertOutput
	extractStructured(t, conv, &convOut)

	rev := callTool(t, session, "block_kit_to_markdown", ReverseInput{Blocks: convOut.Blocks})
	var revOut ReverseOutput
	extractStructured(t, rev, &revOut)

	for _, want := range []string{"**bold**", "- a", "- b"} {
		if !strings.Contains(revOut.Markdown, want) {
			t.Errorf("reverse output missing %q; got:\n%s", want, revOut.Markdown)
		}
	}
}

func TestReverseTool_ActionsBlock_ReportsWarning(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	blocks := []any{
		map[string]any{
			"type": "actions",
			"elements": []any{
				map[string]any{
					"type": "button",
					"text": map[string]any{"type": "plain_text", "text": "Click"},
				},
			},
		},
	}
	r := callTool(t, session, "block_kit_to_markdown", ReverseInput{Blocks: blocks})
	var out ReverseOutput
	extractStructured(t, r, &out)
	if len(out.Warnings) == 0 {
		t.Error("expected a warning when reversing an actions block")
	}
}

// --- resources & prompts -----------------------------------------------------

func TestResources_Cheatsheet_ListedAndReadable(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	list, err := session.ListResources(ctx, &mcp.ListResourcesParams{})
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	var found bool
	for _, r := range list.Resources {
		if r.URI == cheatsheetURI {
			found = true
		}
	}
	if !found {
		t.Fatalf("cheat-sheet resource %q not listed", cheatsheetURI)
	}

	read, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: cheatsheetURI})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(read.Contents) == 0 || !strings.Contains(read.Contents[0].Text, "Block Kit") {
		t.Errorf("cheat-sheet content looks wrong: %+v", read.Contents)
	}
}

func TestPrompts_FormatForSlack_ListedAndRenders(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	list, err := session.ListPrompts(ctx, &mcp.ListPromptsParams{})
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	var found bool
	for _, p := range list.Prompts {
		if p.Name == formatForSlackPromptName {
			found = true
		}
	}
	if !found {
		t.Fatalf("prompt %q not listed", formatForSlackPromptName)
	}

	got, err := session.GetPrompt(ctx, &mcp.GetPromptParams{
		Name:      formatForSlackPromptName,
		Arguments: map[string]string{"text": "release notes for v2"},
	})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if len(got.Messages) == 0 {
		t.Fatal("prompt returned no messages")
	}
	tc, ok := got.Messages[0].Content.(*mcp.TextContent)
	if !ok || !strings.Contains(tc.Text, "release notes for v2") {
		t.Errorf("prompt message did not embed the argument: %+v", got.Messages[0].Content)
	}
}

func TestPrompts_FormatForSlack_MissingText_Errors(t *testing.T) {
	session, cleanup := newTestServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := session.GetPrompt(ctx, &mcp.GetPromptParams{
		Name:      formatForSlackPromptName,
		Arguments: map[string]string{},
	}); err == nil {
		t.Error("expected an error when the required 'text' argument is missing")
	}
}
