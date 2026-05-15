package server

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const formatForSlackPromptName = "format_for_slack"

// registerPrompts exposes reusable MCP prompt templates that guide a model
// through the common workflow of turning text into a safe Slack message.
func (s *Server) registerPrompts() {
	s.mcp.AddPrompt(
		&mcp.Prompt{
			Name:  formatForSlackPromptName,
			Title: "Format text as a Slack message",
			Description: "Guides the model to turn a piece of text or Markdown into a Slack " +
				"Block Kit message via the convert_markdown_to_block_kit tool, keeping " +
				"mention sanitization on by default.",
			Arguments: []*mcp.PromptArgument{
				{
					Name:        "text",
					Description: "The text or Markdown to format as a Slack message.",
					Required:    true,
				},
			},
		},
		handleFormatForSlackPrompt,
	)
}

func handleFormatForSlackPrompt(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	text := req.Params.Arguments["text"]
	if text == "" {
		return nil, fmt.Errorf("prompt %q requires a non-empty 'text' argument", formatForSlackPromptName)
	}
	instructions := "Convert the content below into a Slack Block Kit message by calling the " +
		"convert_markdown_to_block_kit tool with it as the `markdown` argument. Leave mention " +
		"sanitization on — do NOT set allow_broadcasts unless the user explicitly asks to ping a " +
		"channel. After converting, you may call validate_block_kit on the result to confirm it " +
		"is within Slack's limits.\n\n--- content ---\n" + text

	return &mcp.GetPromptResult{
		Description: "Format the provided text as a Slack Block Kit message.",
		Messages: []*mcp.PromptMessage{
			{
				Role:    "user",
				Content: &mcp.TextContent{Text: instructions},
			},
		},
	}, nil
}
