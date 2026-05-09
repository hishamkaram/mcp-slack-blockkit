package server

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/slack-go/slack"

	"github.com/hishamkaram/mcp-slack-blockkit/internal/validator"
)

// ValidateInput accepts either a `blocks` array directly or a full Slack
// message `payload` (which it unwraps to its `blocks` field). At least
// one must be provided. Both fields are typed `any` because the SDK's
// jsonschema-go inference can't represent a heterogeneous block array;
// permissive schema lets the unmarshal step on our side drive the
// concrete-type dispatch via slack.Blocks's UnmarshalJSON.
type ValidateInput struct {
	Blocks  any  `json:"blocks,omitempty" jsonschema:"array of Slack Block Kit block objects to validate"`
	Payload any  `json:"payload,omitempty" jsonschema:"alternative form: a full chat.postMessage payload object whose blocks field is validated"`
	Strict  bool `json:"strict,omitempty" jsonschema:"if true, also reports deprecated patterns (e.g. raw mrkdwn section where rich_text is preferred) as errors"`
}

// ValidateOutput mirrors validator.Result. Violations carry path, code,
// message, and an optional fix_hint so the calling LLM can act on each.
type ValidateOutput struct {
	Valid    bool                  `json:"valid"`
	Errors   []validator.Violation `json:"errors"`
	Warnings []validator.Violation `json:"warnings"`
}

func (s *Server) registerValidateTool() {
	mcp.AddTool(s.mcp,
		&mcp.Tool{
			Name: "validate_blockkit",
			Description: "Validate a Slack Block Kit payload against the documented " +
				"Slack constraints (per-block char limits, count limits, XOR rules, " +
				"unique block_ids, only_one_table_allowed, markdown_block 12k cap). " +
				"Returns structured violations with JSON paths and fix hints. " +
				"Strict mode additionally flags deprecated patterns. Accepts either " +
				"a `blocks` array directly or a full chat.postMessage `payload`.",
		},
		s.handleValidate,
	)
}

func (s *Server) handleValidate(_ context.Context, _ *mcp.CallToolRequest, in ValidateInput) (*mcp.CallToolResult, ValidateOutput, error) {
	blocks, err := decodeBlocksInput(in.Blocks, in.Payload)
	if err != nil {
		return errorResult("invalid input: " + err.Error()), ValidateOutput{}, nil
	}

	var v *validator.Validator
	if in.Strict {
		v = validator.NewStrict()
	} else {
		v = validator.New()
	}
	r := v.Validate(blocks)
	return nil, ValidateOutput(r), nil
}

// decodeBlocksInput unwraps either a `blocks` array or a `payload` object
// (whose `blocks` field we then read) into a typed []slack.Block. Returns
// an error if neither is present or if the JSON is malformed.
//
// Both inputs arrive from the MCP SDK as map[string]any / []any values
// (the SDK validates them against our permissive `any` schema then hands
// them off here). We re-marshal to JSON bytes and feed those into
// slack.Blocks's per-type UnmarshalJSON dispatch — the canonical path
// for converting a generic map back into typed Block values.
func decodeBlocksInput(rawBlocks, rawPayload any) ([]slack.Block, error) {
	if isNilOrEmpty(rawBlocks) && isNilOrEmpty(rawPayload) {
		return nil, errEmptyInput
	}

	target := rawBlocks
	if isNilOrEmpty(target) {
		// payload form: marshal it back to JSON and pluck out `blocks`.
		raw, err := json.Marshal(rawPayload)
		if err != nil {
			return nil, err
		}
		var wrapper struct {
			Blocks json.RawMessage `json:"blocks"`
		}
		if err := json.Unmarshal(raw, &wrapper); err != nil {
			return nil, err
		}
		// Same as the blocks-form branch below: route through slack.Blocks's
		// per-element UnmarshalJSON dispatch.
		var bs slack.Blocks
		if err := json.Unmarshal(wrapper.Blocks, &bs); err != nil {
			return nil, err
		}
		return bs.BlockSet, nil
	}

	raw, err := json.Marshal(target)
	if err != nil {
		return nil, err
	}
	// slack.Blocks's UnmarshalJSON dispatches each element to the right
	// concrete Block type by inspecting its `type` field.
	var bs slack.Blocks
	if err := json.Unmarshal(raw, &bs); err != nil {
		return nil, err
	}
	return bs.BlockSet, nil
}

// isNilOrEmpty handles the map/slice/nil tri-state from the SDK's input
// decoding. A truly-missing field arrives as nil; an explicit empty
// array arrives as []any{}; an empty object arrives as map[string]any{}.
func isNilOrEmpty(v any) bool {
	if v == nil {
		return true
	}
	switch t := v.(type) {
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}

var errEmptyInput = jsonError("provide either `blocks` or `payload` in input")

// jsonError is a tiny stand-in for fmt.Errorf so this file doesn't pull
// the fmt import for a single message. Inlined inline at call sites.
type jsonErrType string

func (e jsonErrType) Error() string { return string(e) }

func jsonError(s string) error { return jsonErrType(s) }
