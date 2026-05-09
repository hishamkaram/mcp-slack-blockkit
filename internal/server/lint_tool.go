package server

import (
	"context"
	"fmt"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/slack-go/slack"

	"github.com/hishamkaram/mcp-slack-block-kit/internal/validator"
)

// LintInput accepts blocks/payload (same shape as validate) plus a
// thresholds object that controls the near-limit warning trigger.
type LintInput struct {
	Blocks     any        `json:"blocks,omitempty" jsonschema:"array of Slack Block Kit blocks to lint"`
	Payload    any        `json:"payload,omitempty" jsonschema:"alternative form: a full chat.postMessage payload object"`
	Thresholds Thresholds `json:"thresholds,omitempty" jsonschema:"thresholds for near-limit warnings (default 90% of each Slack constraint)"`
}

// Thresholds expresses the percentage of a constraint that triggers a
// near-limit lint finding. 90 means "warn when content reaches 90% of the
// Slack-documented maximum." Zero means "use defaults"; values >100 are
// silently clamped to 100.
type Thresholds struct {
	TextPct    int `json:"text_pct,omitempty" jsonschema:"percentage of section.text 3000-char limit that triggers a near-limit warning (default 90)"`
	HeaderPct  int `json:"header_pct,omitempty" jsonschema:"percentage of header.text 150-char limit (default 90)"`
	ActionsPct int `json:"actions_pct,omitempty" jsonschema:"percentage of actions.elements 25-element limit (default 90)"`
	BlocksPct  int `json:"blocks_pct,omitempty" jsonschema:"percentage of 50-blocks-per-message limit (default 90)"`
}

// LintOutput carries findings only — there's no pass/fail concept for
// lint; everything is advisory. Callers use the severity field on each
// Finding to decide whether to surface it as an error or warning.
type LintOutput struct {
	Findings []Finding `json:"findings"`
}

// Finding is the lint counterpart of validator.Violation. Severity is
// always "warning" for lint output (lint never errors); use validate for
// hard errors.
type Finding struct {
	Severity string `json:"severity"` // always "warning" from lint
	Path     string `json:"path"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	FixHint  string `json:"fix_hint,omitempty"`
}

func (s *Server) registerLintTool() {
	mcp.AddTool(
		s.mcp,
		&mcp.Tool{
			Name: "lint_block_kit",
			Description: "Lint a Slack Block Kit payload for near-limit content, " +
				"deprecated patterns, and accessibility gaps. Always advisory — " +
				"never returns errors, only warnings — so it's safe to call on a " +
				"payload that is technically valid but might benefit from " +
				"adjustment. Configurable thresholds (default 90% of each Slack " +
				"limit). Use validate_block_kit for hard correctness checks.",
		},
		s.handleLint,
	)
}

func (s *Server) handleLint(_ context.Context, _ *mcp.CallToolRequest, in LintInput) (*mcp.CallToolResult, LintOutput, error) {
	blocks, err := decodeBlocksInput(in.Blocks, in.Payload)
	if err != nil {
		return errorResult("invalid input: " + err.Error()), LintOutput{}, nil
	}

	t := normalizeThresholds(in.Thresholds)
	out := LintOutput{}

	// 1. Surface every validator warning as-is (e.g. missing alt_text).
	r := validator.New().Validate(blocks)
	for _, w := range r.Warnings {
		out.Findings = append(out.Findings, Finding{
			Severity: string(w.Severity),
			Path:     w.Path,
			Code:     w.Code,
			Message:  w.Message,
			FixHint:  w.FixHint,
		})
	}

	// 2. Near-limit checks at the cross-block level.
	if pct := percentOf(len(blocks), validator.MaxBlocksPerMessage); pct >= t.BlocksPct {
		out.Findings = append(out.Findings, Finding{
			Severity: "warning",
			Path:     "blocks",
			Code:     "blocks_near_limit",
			Message: fmt.Sprintf("%d blocks (%d%% of Slack's %d-block per-message limit)",
				len(blocks), pct, validator.MaxBlocksPerMessage),
			FixHint: "consider splitting into multiple messages with split_blocks",
		})
	}

	// 3. Per-block near-limit checks.
	for i, b := range blocks {
		path := "blocks[" + strconv.Itoa(i) + "]"
		switch tb := b.(type) {
		case *slack.SectionBlock:
			if tb.Text != nil {
				if pct := percentOf(len(tb.Text.Text), validator.MaxSectionTextChars); pct >= t.TextPct {
					out.Findings = append(out.Findings, Finding{
						Severity: "warning",
						Path:     path + ".text.text",
						Code:     "section_text_near_limit",
						Message: fmt.Sprintf("%d/%d chars (%d%%)",
							len(tb.Text.Text), validator.MaxSectionTextChars, pct),
						FixHint: "consider splitting at a paragraph boundary",
					})
				}
			}
		case *slack.HeaderBlock:
			if tb.Text != nil {
				if pct := percentOf(len(tb.Text.Text), validator.MaxHeaderTextChars); pct >= t.HeaderPct {
					out.Findings = append(out.Findings, Finding{
						Severity: "warning",
						Path:     path + ".text.text",
						Code:     "header_near_limit",
						Message: fmt.Sprintf("%d/%d chars (%d%%)",
							len(tb.Text.Text), validator.MaxHeaderTextChars, pct),
						FixHint: "shorten heading or split into a section",
					})
				}
			}
		case *slack.ActionBlock:
			if tb.Elements != nil {
				n := len(tb.Elements.ElementSet)
				if pct := percentOf(n, validator.MaxActionsElements); pct >= t.ActionsPct {
					out.Findings = append(out.Findings, Finding{
						Severity: "warning",
						Path:     path + ".elements",
						Code:     "actions_near_limit",
						Message: fmt.Sprintf("%d/%d elements (%d%%)",
							n, validator.MaxActionsElements, pct),
						FixHint: "split into multiple actions blocks",
					})
				}
			}
		}
	}

	return nil, out, nil
}

// normalizeThresholds applies defaults for any zero-valued field and
// clamps each percent to [0, 100].
func normalizeThresholds(t Thresholds) Thresholds {
	defaults := Thresholds{TextPct: 90, HeaderPct: 90, ActionsPct: 90, BlocksPct: 90}
	if t.TextPct == 0 {
		t.TextPct = defaults.TextPct
	}
	if t.HeaderPct == 0 {
		t.HeaderPct = defaults.HeaderPct
	}
	if t.ActionsPct == 0 {
		t.ActionsPct = defaults.ActionsPct
	}
	if t.BlocksPct == 0 {
		t.BlocksPct = defaults.BlocksPct
	}
	clamp := func(v int) int {
		if v < 0 {
			return 0
		}
		if v > 100 {
			return 100
		}
		return v
	}
	t.TextPct = clamp(t.TextPct)
	t.HeaderPct = clamp(t.HeaderPct)
	t.ActionsPct = clamp(t.ActionsPct)
	t.BlocksPct = clamp(t.BlocksPct)
	return t
}

// percentOf returns 100*current/limit, integer-truncated. Returns 0 when
// limit is 0 to avoid division-by-zero.
func percentOf(current, limit int) int {
	if limit <= 0 {
		return 0
	}
	return (current * 100) / limit
}
