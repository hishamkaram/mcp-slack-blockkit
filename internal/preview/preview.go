// Package preview generates Slack Block Kit Builder preview URLs from a
// blocks array. The Builder URL is the highest-leverage UX win for AI
// workflows: a single click takes a human reviewer from a converted JSON
// payload to a live visual rendering of the message in Slack's own
// builder, without any workspace credentials.
//
// URL format (verified against docs.slack.dev/block-kit/, May 2026):
//
//	https://app.slack.com/block-kit-builder/#<URL_ENCODED_JSON>
//
// The URL-encoded value is the JSON object {"blocks": [...]}. The legacy
// alias https://api.slack.com/tools/block-kit-builder accepts the same
// encoding, but app.slack.com is the canonical host today.
package preview

import (
	"encoding/json"
	"net/url"

	"github.com/slack-go/slack"
)

// BuilderHost is the canonical Block Kit Builder URL. Exported so tests
// and downstream consumers can reference one source of truth.
const BuilderHost = "https://app.slack.com/block-kit-builder/"

// PracticalURLBudget is the soft cap above which a Builder URL becomes
// fragile. Slack does not document a hard ceiling, but URLs above ~8 KB
// hit browser, copy-paste, and Slack-internal limits. Result.Truncated
// reports whether the produced URL exceeds this budget so callers can
// surface the concern (e.g. lint mode warns; the convert tool returns
// the URL anyway).
const PracticalURLBudget = 8 * 1024

// Result carries the produced URL plus metadata for callers that want to
// surface size warnings or include the byte-size in MCP tool responses.
type Result struct {
	URL       string `json:"url"`
	ByteSize  int    `json:"byte_size"`
	Truncated bool   `json:"truncated"` // true when ByteSize > PracticalURLBudget
}

// BuilderURL marshals the given blocks into a {"blocks": [...]} payload,
// URL-encodes the JSON, and returns the assembled Builder URL plus
// metadata. Returns an error only on JSON marshaling failure (which
// should not happen for well-formed slack.Block values).
func BuilderURL(blocks []slack.Block) (Result, error) {
	payload := struct {
		Blocks []slack.Block `json:"blocks"`
	}{Blocks: blocks}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}

	// url.QueryEscape is the standard Go RFC 3986 application/x-www-form-
	// urlencoded escape — equivalent to JS encodeURIComponent for the
	// characters Slack's parser cares about.
	url := BuilderHost + "#" + url.QueryEscape(string(encoded))

	return Result{
		URL:       url,
		ByteSize:  len(url),
		Truncated: len(url) > PracticalURLBudget,
	}, nil
}

// BuilderURLString is a convenience wrapper that returns just the URL
// string, dropping the metadata. Use BuilderURL when the byte_size or
// truncation signal matters (e.g. MCP tool responses, lint warnings).
func BuilderURLString(blocks []slack.Block) (string, error) {
	r, err := BuilderURL(blocks)
	if err != nil {
		return "", err
	}
	return r.URL, nil
}
