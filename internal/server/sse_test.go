package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestRunSSE_EndToEnd_ConvertTool validates the legacy 2024-11 SSE
// transport with a real MCP client. Heavy concurrency, shutdown, and
// body-cap coverage live on the streamable-HTTP side because both
// transports go through the same wrapping middleware in
// internal/server/http.go::runHTTPLike — the auth and body limits are
// asserted there.
func TestRunSSE_EndToEnd_ConvertTool(t *testing.T) {
	h := startSSE(t, HTTPOptions{})
	defer h.stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tr := &mcp.SSEClientTransport{Endpoint: "http://" + h.addr}
	client := mcp.NewClient(&mcp.Implementation{Name: "sse-test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, tr, nil)
	if err != nil {
		t.Fatalf("client.Connect (SSE): %v", err)
	}
	defer session.Close()

	r, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "convert_markdown_to_block_kit",
		Arguments: ConvertInput{Markdown: "hello sse", Mode: "rich_text"},
	})
	if err != nil {
		t.Fatalf("CallTool over SSE: %v", err)
	}
	if r.IsError {
		t.Fatalf("tool returned error: %s", contentText(r))
	}
}

// TestRunSSE_WithToken_MissingHeader_Rejected covers the auth path for
// the SSE handler — the wrapping middleware is shared with HTTP, so a
// single check is enough.
func TestRunSSE_WithToken_MissingHeader_Rejected(t *testing.T) {
	h := startSSE(t, HTTPOptions{Token: "ssetok"})
	defer h.stop()

	resp, err := http.Get("http://" + h.addr + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, "Bearer") {
		t.Errorf("WWW-Authenticate = %q", got)
	}
}
