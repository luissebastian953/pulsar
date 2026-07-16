package nltag

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// ClaudeDecomposer decomposes free text into visual concepts with the Claude
// API. Structured outputs pin the response to a JSON schema, and the system
// prompt forbids emitting tags — the tag DB decides vocabulary, never the
// LLM (.claude/skills/tag-grounding/).
type ClaudeDecomposer struct {
	client anthropic.Client
	model  anthropic.Model
}

// NewClaudeDecomposer builds a decomposer. baseURL is for tests; empty means
// the real API. An empty model defaults to claude-opus-4-8.
func NewClaudeDecomposer(apiKey, model, baseURL string) *ClaudeDecomposer {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithRequestTimeout(60 * time.Second),
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if model == "" {
		model = "claude-opus-4-8"
	}
	return &ClaudeDecomposer{
		client: anthropic.NewClient(opts...),
		model:  anthropic.Model(model),
	}
}

const decomposeSystem = `You decompose a description of an anime illustration into short visual concepts.

Rules:
- Each concept is one atomic visual element: subject count, character or franchise names, physical attributes, clothing, pose, expression, action, objects, setting, composition, art style.
- Use short lowercase noun phrases of 1-4 words.
- Do NOT output Danbooru tags — output plain descriptive phrases; a separate system maps them to real tags.
- Split compound attributes: "long silver hair" becomes "long hair" and "silver hair".
- Keep character and franchise names exactly as written.
- Only describe what the text says. Do not invent details, and do not add quality or meta concepts.`

var conceptsSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"concepts": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "The atomic visual concepts, in the order they appear in the text.",
		},
	},
	"required":             []string{"concepts"},
	"additionalProperties": false,
}

func (d *ClaudeDecomposer) Decompose(ctx context.Context, text string) ([]string, error) {
	resp, err := d.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     d.model,
		MaxTokens: 2048,
		System:    []anthropic.TextBlockParam{{Text: decomposeSystem}},
		OutputConfig: anthropic.OutputConfigParam{
			Format: anthropic.JSONOutputFormatParam{Schema: conceptsSchema},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(text)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("claude: %w", err)
	}
	if resp.StopReason == anthropic.StopReasonRefusal {
		return nil, fmt.Errorf("claude declined the request")
	}

	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			var out struct {
				Concepts []string `json:"concepts"`
			}
			if err := json.Unmarshal([]byte(tb.Text), &out); err != nil {
				return nil, fmt.Errorf("claude: unexpected output %.200q: %w", tb.Text, err)
			}
			return out.Concepts, nil
		}
	}
	return nil, fmt.Errorf("claude: no text block in response (stop_reason %s)", resp.StopReason)
}
