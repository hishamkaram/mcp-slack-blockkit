package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// HTTPOptions configures the HTTP and SSE transports.
//
// Token is an optional shared-secret bearer token. When non-empty, every
// incoming request must carry Authorization: Bearer <token> or it gets a
// 401 Unauthorized response. The comparison is constant-time. When empty,
// no auth is performed — appropriate for the default localhost bind, but
// callers should set a token before binding on any non-loopback address.
type HTTPOptions struct {
	Token string
}

// Per-request hard caps on the wrapping http.Server. Hand-picked values
// rather than struct fields because the supported MCP transports
// (streamable HTTP, SSE) have a narrow protocol envelope; configuring
// them per call would invite users to set values that defeat the SDK's
// own session-management logic. Notably: we DO NOT set WriteTimeout,
// because it kills the long-lived SSE GET that carries server→client
// events. ReadHeaderTimeout + IdleTimeout cover the slowloris and
// idle-leak vectors. Body cap is 1 MiB — the largest single MCP
// request payload we expect is a markdown blob bounded by the
// converter's 256 KiB default.
const (
	httpReadHeaderTimeout = 10 * time.Second
	httpIdleTimeout       = 2 * time.Minute
	httpMaxHeaderBytes    = 1 << 16
	httpMaxBodyBytes      = 1 << 20
	httpShutdownDeadline  = 5 * time.Second
)

// RunHTTP starts the streamable-HTTP MCP transport (spec 2025-03-26) bound
// to addr and blocks until ctx is cancelled or ListenAndServe returns. The
// underlying *mcp.Server is shared across all sessions, matching the
// SDK's documented expectation for StreamableHTTPHandler.
func (s *Server) RunHTTP(ctx context.Context, addr string, opts HTTPOptions) error {
	return s.runHTTPLike(ctx, addr, opts, "http", s.buildStreamableHandler)
}

// RunSSE starts the legacy SSE MCP transport (spec 2024-11-05) bound to
// addr. Same shape as RunHTTP — useful for older MCP clients that don't
// support streamable HTTP yet.
func (s *Server) RunSSE(ctx context.Context, addr string, opts HTTPOptions) error {
	return s.runHTTPLike(ctx, addr, opts, "sse", s.buildSSEHandler)
}

// runHTTPLike is the shared start/shutdown loop. The build-handler closure
// returns the bare protocol handler (no auth, no body cap); this function
// wraps it in middleware and runs the lifecycle.
func (s *Server) runHTTPLike(
	ctx context.Context,
	addr string,
	opts HTTPOptions,
	transport string,
	buildHandler func() http.Handler,
) error {
	if addr == "" {
		return fmt.Errorf("server: %s transport requires a non-empty bind address", transport)
	}
	wrapped := http.Handler(buildHandler())
	wrapped = http.MaxBytesHandler(wrapped, httpMaxBodyBytes)
	if opts.Token != "" {
		wrapped = bearerAuth(wrapped, opts.Token)
	}

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           wrapped,
		ReadHeaderTimeout: httpReadHeaderTimeout,
		IdleTimeout:       httpIdleTimeout,
		MaxHeaderBytes:    httpMaxHeaderBytes,
		BaseContext:       func(_ net.Listener) context.Context { return ctx },
	}
	// Without this, http.Server.Shutdown won't terminate a hanging SSE
	// GET — the SDK's canonical pattern (see streamable_test.go's
	// TestStreamableServerShutdown). Close each session so its blocking
	// reader returns and the handler goroutine exits.
	httpServer.RegisterOnShutdown(func() {
		for sess := range s.mcp.Sessions() {
			_ = sess.Close()
		}
	})

	if opts.Token == "" && !isLoopbackBind(addr) {
		slog.WarnContext(ctx,
			"mcp server bound to a non-loopback address with no bearer token: "+
				"anyone who can reach this address can drive the server; "+
				"set a token (--http-token / HTTPOptions.Token) or bind to localhost",
			"transport", transport, "addr", addr)
	}

	slog.InfoContext(
		ctx, "starting mcp server",
		"transport", transport,
		"addr", addr,
		"auth_enabled", opts.Token != "",
		"version", s.version,
	)

	errc := make(chan error, 1)
	go func() {
		err := httpServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
			return
		}
		errc <- nil
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), httpShutdownDeadline)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			// Fall back to Close on shutdown deadline; we still want to
			// report ctx.Err so callers can distinguish cancellation from
			// a real listener error.
			_ = httpServer.Close()
		}
		<-errc
		return ctx.Err()
	}
}

func (s *Server) buildStreamableHandler() http.Handler {
	return mcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcp.Server { return s.mcp },
		&mcp.StreamableHTTPOptions{Logger: slog.Default()},
	)
}

func (s *Server) buildSSEHandler() http.Handler {
	return mcp.NewSSEHandler(
		func(_ *http.Request) *mcp.Server { return s.mcp },
		nil,
	)
}

// bearerAuth wraps next in a middleware that requires
// Authorization: Bearer <token>. The comparison is constant-time to
// avoid timing-side-channel leakage of the secret. On mismatch we return
// 401 with a WWW-Authenticate header (per RFC 6750) and log the attempt
// without including the supplied token value.
func bearerAuth(next http.Handler, token string) http.Handler {
	want := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := extractBearer(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			slog.WarnContext(r.Context(), "mcp http auth rejected",
				"remote", r.RemoteAddr, "path", r.URL.Path)
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcp-slack-block-kit"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLoopbackBind reports whether addr binds only to a loopback interface.
// A host of "localhost", a 127.0.0.0/8 address, or "::1" is loopback. An
// empty host (e.g. ":7777") binds every interface and is NOT loopback;
// neither is 0.0.0.0 or a routable IP. A malformed addr is treated as
// non-loopback so the warning errs on the side of caution.
func isLoopbackBind(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	switch host {
	case "":
		return false
	case "localhost":
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// extractBearer returns the token portion of a "Bearer <token>" header.
// Returns nil (zero length) when the header is missing or malformed, so a
// constant-time compare against the expected token fails.
func extractBearer(h string) []byte {
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return nil
	}
	return []byte(strings.TrimSpace(h[len(prefix):]))
}
