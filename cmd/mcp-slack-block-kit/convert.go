package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/hishamkaram/mcp-slack-block-kit/internal/converter"
	"github.com/hishamkaram/mcp-slack-block-kit/internal/preview"
)

// newConvertCmd builds the `convert` subcommand: read markdown from stdin,
// write Block Kit JSON to stdout. The implementation reuses the same
// internal/converter that powers the MCP convert_markdown_to_block_kit
// tool, so CLI and MCP outputs match bit-for-bit on identical input.
//
// Stream contract:
//   - stdin → markdown input (read until EOF; bounded by --max-input-bytes)
//   - stdout → ONLY the Block Kit JSON (a single line of `{"blocks": [...]}`),
//     suitable for piping into `jq` or `chat.postMessage`.
//   - stderr → logs (slog), --preview URL when requested, error messages.
//
// This separation lets shell pipelines work cleanly:
//
//	cat doc.md | mcp-slack-block-kit convert --preview > payload.json
func newConvertCmd(stderr io.Writer, stdout io.Writer, stdin io.Reader) *cobra.Command {
	var (
		mode            string
		previewFlag     bool
		allowBroadcasts bool
		blockIDPrefix   string
		maxInputBytes   int
		pretty          bool
	)

	cmd := &cobra.Command{
		Use:   "convert",
		Short: "Convert markdown on stdin to Block Kit JSON on stdout",
		Long: "Reads markdown from stdin, writes a Slack Block Kit JSON " +
			"payload to stdout. Useful for offline testing without an MCP " +
			"client. With --preview, also writes the Block Kit Builder URL " +
			"to stderr (stdout stays JSON-only for piping).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			input, err := io.ReadAll(stdin)
			if err != nil {
				return fmt.Errorf("read stdin: %w", err)
			}

			opts := converter.DefaultOptions()
			if mode != "" {
				opts.Mode = converter.Mode(mode)
			}
			opts.AllowBroadcasts = allowBroadcasts
			if blockIDPrefix != "" {
				opts.BlockIDPrefix = blockIDPrefix
			}
			if maxInputBytes > 0 {
				opts.MaxInputBytes = maxInputBytes
			}

			r, err := converter.New(opts)
			if err != nil {
				return fmt.Errorf("converter init: %w", err)
			}

			blocks, err := r.Convert(string(input))
			if err != nil {
				return fmt.Errorf("convert: %w", err)
			}

			payload := struct {
				Blocks any `json:"blocks"`
			}{Blocks: blocks}

			var encoded []byte
			if pretty {
				encoded, err = json.MarshalIndent(payload, "", "  ")
			} else {
				encoded, err = json.Marshal(payload)
			}
			if err != nil {
				return fmt.Errorf("marshal: %w", err)
			}

			if _, err := stdout.Write(encoded); err != nil {
				return fmt.Errorf("write stdout: %w", err)
			}
			// Trailing newline so terminal output isn't glued to the prompt.
			if _, err := stdout.Write([]byte{'\n'}); err != nil {
				return fmt.Errorf("write stdout: %w", err)
			}

			if previewFlag {
				pr, err := preview.BuilderURL(blocks)
				if err == nil {
					// stderr writes can fail if the descriptor is closed
					// (rare); surface as a non-fatal log via slog so we
					// don't swallow the error silently.
					if _, werr := fmt.Fprintf(stderr, "preview: %s\n", pr.URL); werr != nil {
						return fmt.Errorf("write preview to stderr: %w", werr)
					}
					if pr.Truncated {
						if _, werr := fmt.Fprintf(stderr, "note: preview URL is %d bytes; may be unreliable above ~8KB\n", pr.ByteSize); werr != nil {
							return fmt.Errorf("write preview note to stderr: %w", werr)
						}
					}
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "auto",
		"conversion mode: auto, rich_text, markdown_block, section_mrkdwn")
	cmd.Flags().BoolVar(&previewFlag, "preview", false,
		"emit Block Kit Builder preview URL on stderr alongside JSON output")
	cmd.Flags().BoolVar(&allowBroadcasts, "allow-broadcasts", false,
		"allow raw <!channel>/<!here>/<@U…> in input to pass through unescaped (DEFAULT FALSE for safety)")
	cmd.Flags().StringVar(&blockIDPrefix, "block-id-prefix", "",
		"optional prefix for generated block_id values")
	cmd.Flags().IntVar(&maxInputBytes, "max-input-bytes", 0,
		"maximum markdown input size in bytes (0 = use the converter default of 256 KiB)")
	cmd.Flags().BoolVar(&pretty, "pretty", false,
		"pretty-print the JSON output (2-space indent)")
	return cmd
}
