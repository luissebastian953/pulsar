package nltag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DeepSeekDecomposer decomposes free text via DeepSeek's OpenAI-compatible
// chat completions API. DeepSeek has no json_schema mode, so output is
// constrained with JSON mode (response_format json_object) — which requires
// the word "json" plus an example in the prompt, or the model can emit
// endless whitespace. The same vocabulary rule applies as everywhere in
// nltag: the LLM decomposes, it never emits tags.
type DeepSeekDecomposer struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

// NewDeepSeekDecomposer builds a decomposer. baseURL is for tests or
// self-hosted OpenAI-compatible endpoints; empty means the real API. An
// empty model defaults to deepseek-v4-flash (deepseek-chat is discontinued
// as of 2026-07-24).
func NewDeepSeekDecomposer(apiKey, model, baseURL string) *DeepSeekDecomposer {
	if model == "" {
		model = "deepseek-v4-flash"
	}
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	return &DeepSeekDecomposer{
		apiKey:  apiKey,
		model:   model,
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// JSON mode contract: name the format and show an example (see docstring).
const deepseekJSONSuffix = `

Respond with a JSON object of exactly this shape:
{"concepts": ["long hair", "silver hair", "ponytail"]}`

func (d *DeepSeekDecomposer) Decompose(ctx context.Context, text string) ([]string, error) {
	body, err := json.Marshal(map[string]any{
		"model": d.model,
		"messages": []map[string]string{
			{"role": "system", "content": decomposeSystem + deepseekJSONSuffix},
			{"role": "user", "content": text},
		},
		"response_format": map[string]string{"type": "json_object"},
		"max_tokens":      2048,
		"temperature":     0,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		d.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.apiKey)

	resp, err := d.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deepseek: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("deepseek: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("deepseek: %s: %.300s", resp.Status, raw)
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("deepseek: unexpected response %.300q: %w", raw, err)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("deepseek: no choices in response")
	}
	choice := parsed.Choices[0]
	if choice.FinishReason == "length" {
		return nil, fmt.Errorf("deepseek: output truncated (finish_reason length)")
	}

	// thinking-mode models may fence the JSON despite json_object mode
	content := strings.TrimSpace(choice.Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")

	var out struct {
		Concepts []string `json:"concepts"`
	}
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return nil, fmt.Errorf("deepseek: unexpected output %.200q: %w", content, err)
	}
	return out.Concepts, nil
}
