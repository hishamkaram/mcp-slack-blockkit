package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// httpHarness spins up RunHTTP on an ephemeral port. The transport-pick
// closure (newTransport) lets the same harness drive both StreamableHTTP
// and SSE — keeps the two test files thin.
type httpHarness struct {
	t       *testing.T
	server  *Server
	srvCtx  context.Context
	cancel  context.CancelFunc
	wg      *sync.WaitGroup
	addr    string
	runErrM sync.Mutex
	runErr  error
}

// startHTTP starts RunHTTP on 127.0.0.1:0 and returns the resolved
// host:port string once the listener is up. The caller is responsible
// for calling stop().
func startHTTP(t *testing.T, opts HTTPOptions) *httpHarness {
	t.Helper()
	return startTransport(t, opts, func(s *Server, ctx context.Context, addr string, o HTTPOptions) error {
		return s.RunHTTP(ctx, addr, o)
	})
}

func startSSE(t *testing.T, opts HTTPOptions) *httpHarness {
	t.Helper()
	return startTransport(t, opts, func(s *Server, ctx context.Context, addr string, o HTTPOptions) error {
		return s.RunSSE(ctx, addr, o)
	})
}

func startTransport(
	t *testing.T,
	opts HTTPOptions,
	run func(*Server, context.Context, string, HTTPOptions) error,
) *httpHarness {
	t.Helper()

	srv, err := New("test")
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	// Pre-allocate the listener so the harness knows the bound port
	// before RunHTTP starts. We close it and have RunHTTP re-bind on the
	// same address — cleaner than racing on a startup signal.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	h := &httpHarness{t: t, server: srv, srvCtx: ctx, cancel: cancel, wg: wg, addr: addr}
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := run(srv, ctx, addr, opts)
		h.runErrM.Lock()
		h.runErr = err
		h.runErrM.Unlock()
	}()
	// Wait until the listener is accepting. Try a TCP dial in a tight
	// loop with a short overall budget.
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("listener never came up at %s: %v", addr, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return h
}

func (h *httpHarness) stop() {
	h.cancel()
	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		h.t.Errorf("RunHTTP did not exit after cancel")
	}
}

func (h *httpHarness) lastErr() error {
	h.runErrM.Lock()
	defer h.runErrM.Unlock()
	return h.runErr
}

// --- Streamable HTTP: happy path -------------------------------------------

func TestRunHTTP_EndToEnd_ConvertTool(t *testing.T) {
	h := startHTTP(t, HTTPOptions{})
	defer h.stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tr := &mcp.StreamableClientTransport{
		Endpoint: "http://" + h.addr,
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "http-test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, tr, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer session.Close()

	r, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "convert_markdown_to_block_kit",
		Arguments: ConvertInput{Markdown: "hello http", Mode: "rich_text"},
	})
	if err != nil {
		t.Fatalf("CallTool over HTTP: %v", err)
	}
	if r == nil || r.IsError {
		t.Fatalf("tool returned error: %+v", r)
	}
	var out ConvertOutput
	extractStructured(t, r, &out)
	body, _ := json.Marshal(out.Blocks)
	if !strings.Contains(string(body), "rich_text") {
		t.Errorf("expected rich_text in response: %s", body)
	}
}

// --- Graceful shutdown -----------------------------------------------------

func TestRunHTTP_GracefulShutdown_ExitsCleanly(t *testing.T) {
	h := startHTTP(t, HTTPOptions{})
	// Open a session so the shutdown path has to flush a live one.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "shutdown-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: "http://" + h.addr}, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer session.Close()

	start := time.Now()
	h.stop()
	elapsed := time.Since(start)
	if elapsed > 6*time.Second {
		t.Errorf("shutdown took too long: %s", elapsed)
	}
	// ctx.Err() being context.Canceled is the expected RunHTTP return.
	if err := h.lastErr(); err != nil && err != context.Canceled {
		t.Errorf("unexpected RunHTTP error: %v", err)
	}
}

// --- Concurrency: many clients against one shared *mcp.Server --------------

func TestRunHTTP_ConcurrentClients_NoRaceOnSharedServer(t *testing.T) {
	h := startHTTP(t, HTTPOptions{})
	defer h.stop()

	const n = 20
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			client := mcp.NewClient(
				&mcp.Implementation{Name: fmt.Sprintf("c%d", i), Version: "0.0.1"}, nil,
			)
			session, err := client.Connect(ctx,
				&mcp.StreamableClientTransport{Endpoint: "http://" + h.addr}, nil)
			if err != nil {
				errs <- fmt.Errorf("client %d connect: %w", i, err)
				return
			}
			defer session.Close()
			r, err := session.CallTool(ctx, &mcp.CallToolParams{
				Name:      "convert_markdown_to_block_kit",
				Arguments: ConvertInput{Markdown: fmt.Sprintf("body %d", i), Mode: "rich_text"},
			})
			if err != nil {
				errs <- fmt.Errorf("client %d call: %w", i, err)
				return
			}
			if r.IsError {
				errs <- fmt.Errorf("client %d tool error: %s", i, contentText(r))
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// --- Body cap --------------------------------------------------------------

func TestRunHTTP_BodyTooLarge_ReturnsClientError(t *testing.T) {
	h := startHTTP(t, HTTPOptions{})
	defer h.stop()

	// Build a payload bigger than httpMaxBodyBytes (1 MiB).
	big := strings.Repeat("a", httpMaxBodyBytes+2048)
	req, _ := http.NewRequest(http.MethodPost, "http://"+h.addr+"/",
		strings.NewReader(big))
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	// http.MaxBytesHandler responds with 4xx on cap breach (variable status
	// across Go versions); the protocol-handler shouldn't see the request.
	if resp.StatusCode < 400 || resp.StatusCode >= 500 {
		t.Errorf("expected 4xx for oversized body; got %d", resp.StatusCode)
	}
}

// --- Bearer auth -----------------------------------------------------------

func TestRunHTTP_NoToken_NoAuthChecked(t *testing.T) {
	h := startHTTP(t, HTTPOptions{}) // empty token = no auth
	defer h.stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "noauth", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx,
		&mcp.StreamableClientTransport{Endpoint: "http://" + h.addr}, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer session.Close()
}

func TestRunHTTP_WithToken_RequiresBearer(t *testing.T) {
	const tok = "s3cret"
	h := startHTTP(t, HTTPOptions{Token: tok})
	defer h.stop()

	// Missing Authorization → 401.
	resp, err := http.Post("http://"+h.addr+"/", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST missing-auth: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("missing auth: status = %d, want 401", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, "Bearer") {
		t.Errorf("missing auth: WWW-Authenticate = %q", got)
	}

	// Wrong token → 401.
	req, _ := http.NewRequest(http.MethodPost, "http://"+h.addr+"/",
		strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer wrong")
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST wrong-auth: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong auth: status = %d, want 401", resp.StatusCode)
	}

	// Correct token via a real MCP client.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tr := &mcp.StreamableClientTransport{
		Endpoint:   "http://" + h.addr,
		HTTPClient: &http.Client{Transport: bearerRoundTripper{rt: http.DefaultTransport, tok: tok}},
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "ok-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, tr, nil)
	if err != nil {
		t.Fatalf("client.Connect with valid token: %v", err)
	}
	defer session.Close()

	r, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "convert_markdown_to_block_kit",
		Arguments: ConvertInput{Markdown: "auth ok", Mode: "rich_text"},
	})
	if err != nil {
		t.Fatalf("CallTool with valid token: %v", err)
	}
	if r.IsError {
		t.Fatalf("tool returned error: %s", contentText(r))
	}
}

// bearerRoundTripper injects the Authorization header on every request the
// MCP client makes. The SDK's StreamableClientTransport accepts a custom
// *http.Client, which lets us tunnel auth without forking the transport.
type bearerRoundTripper struct {
	rt  http.RoundTripper
	tok string
}

func (b bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+b.tok)
	return b.rt.RoundTrip(req)
}

// --- Bearer middleware unit tests (no transport) --------------------------

func TestIsLoopbackBind(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:7777", true},
		{"localhost:7777", true},
		{"[::1]:7777", true},
		{"127.0.0.5:80", true},
		{":7777", false},
		{"0.0.0.0:7777", false},
		{"192.168.1.10:7777", false},
		{"example.com:7777", false},
	}
	for _, tc := range cases {
		t.Run(tc.addr, func(t *testing.T) {
			if got := isLoopbackBind(tc.addr); got != tc.want {
				t.Errorf("isLoopbackBind(%q) = %v, want %v", tc.addr, got, tc.want)
			}
		})
	}
}

func TestBearerAuth_ConstantTimeCompare_LengthMismatch_Rejected(t *testing.T) {
	// A length mismatch must NOT short-circuit and leak timing — but the
	// reject path is the same as a content mismatch (401). We're really
	// asserting both produce 401 here.
	handler := bearerAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), "abc12345")

	cases := []struct{ name, auth string }{
		{"too short", "Bearer abc"},
		{"too long", "Bearer abc123456789"},
		{"empty bearer", "Bearer "},
		{"malformed prefix", "Token abc12345"},
		{"missing header", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, "/", nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			rec := newResponseRecorder()
			handler.ServeHTTP(rec, req)
			if rec.code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.code)
			}
		})
	}
}

// newResponseRecorder is a minimal http.ResponseWriter stand-in so we
// don't pull in httptest just for two fields.
type responseRecorder struct {
	header http.Header
	code   int
}

func newResponseRecorder() *responseRecorder {
	return &responseRecorder{header: http.Header{}, code: http.StatusOK}
}

func (r *responseRecorder) Header() http.Header         { return r.header }
func (r *responseRecorder) Write(b []byte) (int, error) { return len(b), nil }
func (r *responseRecorder) WriteHeader(c int)           { r.code = c }
