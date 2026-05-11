package block_kit_test

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hishamkaram/mcp-slack-block-kit/block_kit"
)

// These tests live in block_kit_test (external package) so any leak of
// internal-only behavior fails compilation here. They exercise the
// minimum public surface a library consumer would touch: construct,
// pick a transport, hit a tool.

func TestPublicAPI_NewServer_Construct(t *testing.T) {
	s, err := block_kit.NewServer("public-test")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if s == nil {
		t.Fatal("server is nil")
	}
}

func TestPublicAPI_Server_RunStdio_ViaInMemoryTransport(t *testing.T) {
	// Smoke test the public RunStdio function. We feed the server an
	// in-memory transport from the SDK and confirm a real tool call
	// resolves end-to-end, all through the public API.
	s, err := block_kit.NewServer("public-test")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srvTransport, cliTransport := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_ = s.MCP().Run(ctx, srvTransport)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "public-test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, cliTransport, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer session.Close()

	r, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "convert_markdown_to_block_kit",
		Arguments: map[string]any{"markdown": "hello public api", "mode": "rich_text"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if r.IsError {
		t.Fatalf("tool returned error: %+v", r)
	}
}

func TestPublicAPI_Server_RunStdio_PublicWrapper(t *testing.T) {
	// The public RunStdio wrapper just delegates to (*Server).RunStdio.
	// Verify it's wired by passing an already-cancelled context — RunStdio
	// must observe ctx.Done() and return without panicking.
	s, err := block_kit.NewServer("public-test")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before invocation
	done := make(chan error, 1)
	go func() {
		done <- block_kit.RunStdio(ctx, s)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("RunStdio did not return on cancelled context")
	}
}

func TestPublicAPI_Server_RunSSE_HappyPath(t *testing.T) {
	s, err := block_kit.NewServer("public-test")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- block_kit.RunSSE(ctx, s, addr, block_kit.HTTPOptions{})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("listener never came up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	tr := &mcp.SSEClientTransport{Endpoint: "http://" + addr}
	client := mcp.NewClient(&mcp.Implementation{Name: "public-sse-client", Version: "0.0.1"}, nil)
	callCtx, cancelCall := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCall()
	session, err := client.Connect(callCtx, tr, nil)
	if err != nil {
		t.Fatalf("Connect SSE: %v", err)
	}
	defer session.Close()
	r, err := session.CallTool(callCtx, &mcp.CallToolParams{
		Name:      "convert_markdown_to_block_kit",
		Arguments: map[string]any{"markdown": "hello sse public", "mode": "rich_text"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if r.IsError {
		t.Fatalf("tool error: %+v", r)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("RunSSE did not exit after cancel")
	}
}

func TestPublicAPI_Server_RunHTTP_HappyPath(t *testing.T) {
	s, err := block_kit.NewServer("public-test")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- block_kit.RunHTTP(ctx, s, addr, block_kit.HTTPOptions{})
	}()

	// Wait for the listener to come up.
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("listener never came up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	tr := &mcp.StreamableClientTransport{Endpoint: "http://" + addr}
	client := mcp.NewClient(&mcp.Implementation{Name: "public-http-client", Version: "0.0.1"}, nil)
	callCtx, cancelCall := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCall()
	session, err := client.Connect(callCtx, tr, nil)
	if err != nil {
		t.Fatalf("Connect HTTP: %v", err)
	}
	defer session.Close()

	r, err := session.CallTool(callCtx, &mcp.CallToolParams{
		Name:      "convert_markdown_to_block_kit",
		Arguments: map[string]any{"markdown": "hello public http", "mode": "rich_text"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if r.IsError {
		t.Fatalf("tool returned error: %+v", r)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("RunHTTP did not exit after cancel")
	}
}

func TestPublicAPI_Server_RunHTTP_WithToken_RejectsUnauthorized(t *testing.T) {
	s, err := block_kit.NewServer("public-test")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- block_kit.RunHTTP(ctx, s, addr, block_kit.HTTPOptions{Token: "tok"})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("listener never came up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	resp, err := http.Post("http://"+addr+"/", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("RunHTTP did not exit after cancel")
	}
}
