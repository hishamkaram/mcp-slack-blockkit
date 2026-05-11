package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// --- server transport flag wiring ------------------------------------------

func TestServerCmd_BothTransportFlags_ReturnsUsageError(t *testing.T) {
	_, _, err := runRoot(t, "server", "--http-addr", "127.0.0.1:0", "--sse-addr", "127.0.0.1:0")
	if err == nil {
		t.Fatal("expected error when both transport flags are set, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected 'mutually exclusive' in error; got %v", err)
	}
}

func TestServerCmd_TokenWithoutAddr_ReturnsUsageError(t *testing.T) {
	_, _, err := runRoot(t, "server", "--http-token", "abc")
	if err == nil {
		t.Fatal("expected error when --http-token has no addr, got nil")
	}
	if !strings.Contains(err.Error(), "--http-addr") {
		t.Errorf("expected error to mention --http-addr; got %v", err)
	}
}

func TestServerCmd_ResolveHTTPToken_FlagOverridesEnv(t *testing.T) {
	got := resolveHTTPToken("flag-tok", "env-tok")
	if got != "flag-tok" {
		t.Errorf("flag should win; got %q", got)
	}
	got = resolveHTTPToken("", "env-tok")
	if got != "env-tok" {
		t.Errorf("env should be used when flag empty; got %q", got)
	}
	got = resolveHTTPToken("", "")
	if got != "" {
		t.Errorf("both empty → empty; got %q", got)
	}
}

func TestServerCmd_ValidateServerFlags_TableCases(t *testing.T) {
	cases := []struct {
		name, http, sse, tok string
		wantErr              bool
	}{
		{"all empty (stdio)", "", "", "", false},
		{"http only", "127.0.0.1:7777", "", "", false},
		{"sse only", "", "127.0.0.1:7778", "", false},
		{"http + token", "127.0.0.1:7777", "", "abc", false},
		{"sse + token", "", "127.0.0.1:7778", "abc", false},
		{"both transports", "127.0.0.1:7777", "127.0.0.1:7778", "", true},
		{"token alone", "", "", "abc", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateServerFlags(tc.http, tc.sse, tc.tok)
			if (err != nil) != tc.wantErr {
				t.Errorf("err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

// runRoot is the shared test harness: build a fresh root with byte-buffer
// streams, set the args, execute, and return what was written where.
func runRoot(t *testing.T, args ...string) (stdout, stderr *bytes.Buffer, err error) {
	t.Helper()
	return runRootWithStdin(t, "", args...)
}

// runRootWithStdin is runRoot's variant that lets the test pass arbitrary
// stdin content. Used by the convert subcommand tests.
func runRootWithStdin(t *testing.T, stdinContent string, args ...string) (stdout, stderr *bytes.Buffer, err error) {
	t.Helper()
	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}
	stdin := strings.NewReader(stdinContent)

	root := newRootCmd(stderr, stdout, stdin)
	root.SetArgs(args)
	root.SetOut(stderr) // cobra's --help and error text go to stderr in this binary
	root.SetErr(stderr)
	err = root.ExecuteContext(context.Background())
	return stdout, stderr, err
}

func TestRoot_VersionFlag_PrintsVersion(t *testing.T) {
	stdout, stderr, err := runRoot(t, "--version")
	if err != nil {
		t.Fatalf("--version returned error: %v", err)
	}
	// Cobra writes --version output to its configured Out, which we set to
	// stderr in this binary so stdout stays reserved for MCP protocol output.
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "dev") {
		t.Errorf("expected default version 'dev' in output, got: stdout=%q stderr=%q",
			stdout.String(), stderr.String())
	}
	if !strings.Contains(combined, "commit") || !strings.Contains(combined, "built") {
		t.Errorf("expected commit/built metadata in version output, got: %q", combined)
	}
}

func TestRoot_HelpFlag_DoesNotWriteToStdout(t *testing.T) {
	stdout, stderr, err := runRoot(t, "--help")
	if err != nil {
		t.Fatalf("--help returned error: %v", err)
	}
	// Help text is informational; per the MCP stdio rule, stdout MUST stay
	// reserved for protocol messages. --help may legitimately go to stderr.
	if stdout.Len() != 0 {
		t.Errorf("--help wrote to stdout (must stay empty for MCP protocol channel): %q",
			stdout.String())
	}
	if !strings.Contains(stderr.String(), "convert") {
		t.Errorf("expected --help text to mention 'convert' subcommand, got: %q",
			stderr.String())
	}
	if !strings.Contains(stderr.String(), "server") {
		t.Errorf("expected --help text to mention 'server' subcommand, got: %q",
			stderr.String())
	}
}

func TestRoot_BareInvocation_DefaultsToServer(t *testing.T) {
	// The bare invocation runs the real stdio MCP server. Test stdin is a
	// closed strings.Reader, so the server detects EOF immediately and
	// exits cleanly. We assert that (a) the startup log went to stderr
	// (our injected stream), (b) nothing leaked to stdout (which is
	// reserved for MCP protocol output and must stay empty before the
	// transport opens), and (c) the run returned without error.
	stdout, stderr, err := runRoot(t /* no args */)
	if err != nil {
		t.Fatalf("bare invocation returned error: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("server wrote to stdout pre-handshake: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "starting mcp server") {
		t.Errorf("expected 'starting mcp server' log on stderr; got %q", stderr.String())
	}
}

func TestRoot_ConvertSubcommand_OutputsJSONOnStdout(t *testing.T) {
	stdout, stderr, err := runRootWithStdin(t, "hello world",
		"convert", "--mode", "rich_text")
	if err != nil {
		t.Fatalf("convert returned error: %v\nstderr=%s", err, stderr.String())
	}
	if stdout.Len() == 0 {
		t.Error("convert produced no stdout")
	}
	if !strings.Contains(stdout.String(), `"blocks"`) {
		t.Errorf("stdout should contain a `blocks` key; got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "hello world") {
		t.Errorf("stdout should contain the input text; got %q", stdout.String())
	}
}

func TestRoot_ConvertSubcommand_PreviewFlagWritesURLToStderr(t *testing.T) {
	stdout, stderr, err := runRootWithStdin(t, "test",
		"convert", "--mode", "rich_text", "--preview")
	if err != nil {
		t.Fatalf("convert returned error: %v", err)
	}
	if stdout.Len() == 0 {
		t.Error("expected stdout JSON output")
	}
	if !strings.Contains(stderr.String(), "preview:") {
		t.Errorf("expected 'preview:' line on stderr; got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "https://app.slack.com/block-kit-builder/") {
		t.Errorf("expected Builder URL on stderr; got %q", stderr.String())
	}
	// Critical: stdout must remain JSON-only even when --preview is on.
	if strings.Contains(stdout.String(), "preview:") {
		t.Errorf("preview text leaked to stdout: %q", stdout.String())
	}
}

func TestRoot_ConvertSubcommand_PrettyFlag_FormatsOutput(t *testing.T) {
	stdout, _, err := runRootWithStdin(t, "hello",
		"convert", "--mode", "rich_text", "--pretty")
	if err != nil {
		t.Fatalf("convert returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "\n  ") {
		t.Errorf("pretty output should have indentation; got %q", stdout.String())
	}
}

func TestRoot_ConvertSubcommand_EmptyInput_ProducesEmptyBlocks(t *testing.T) {
	stdout, _, err := runRootWithStdin(t, "", "convert", "--mode", "rich_text")
	if err != nil {
		t.Fatalf("convert returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), `"blocks"`) {
		t.Errorf("expected blocks key even for empty input; got %q", stdout.String())
	}
}

func TestRoot_InvalidLogLevel_ReturnsError(t *testing.T) {
	_, _, err := runRoot(t, "--log-level", "trace", "server")
	if err == nil {
		t.Fatal("expected error for invalid log level, got nil")
	}
	if !strings.Contains(err.Error(), "invalid --log-level") {
		t.Errorf("expected 'invalid --log-level' in error, got: %v", err)
	}
}

func TestConfigureLogging_AllValidLevels(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error", ""} {
		buf := &bytes.Buffer{}
		if err := configureLogging(buf, level); err != nil {
			t.Errorf("configureLogging(%q) returned error: %v", level, err)
		}
	}
}

func TestConfigureLogging_RejectsUnknownLevel(t *testing.T) {
	buf := &bytes.Buffer{}
	if err := configureLogging(buf, "verbose"); err == nil {
		t.Error("expected error for unknown level 'verbose', got nil")
	}
}

func TestResolveVersion_FormatsAllFields(t *testing.T) {
	got := resolveVersion()
	for _, want := range []string{"dev", "commit", "built", "none", "unknown"} {
		if !strings.Contains(got, want) {
			t.Errorf("resolveVersion()=%q missing expected substring %q", got, want)
		}
	}
}
