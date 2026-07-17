// Package comfy: ComfyUI client + per-GPU router; one workflow per
// process, treat as unreliable (docs/architecture.md#concurrency).
package comfy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type Client struct {
	base     string // http://host:8188
	wsURL    string
	http     *http.Client
	clientID string // correlates WS messages to our submissions
}

func New(baseURL string) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid comfy url %q", baseURL)
	}
	ws := *u
	switch u.Scheme {
	case "http":
		ws.Scheme = "ws"
	case "https":
		ws.Scheme = "wss"
	default:
		return nil, fmt.Errorf("invalid comfy url scheme %q", u.Scheme)
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, err
	}
	c := &Client{
		base:     u.String(),
		clientID: hex.EncodeToString(idBytes),
		http:     &http.Client{Timeout: 30 * time.Second},
	}
	ws.Path = "/ws"
	ws.RawQuery = "clientId=" + c.clientID
	c.wsURL = ws.String()
	return c, nil
}

func (c *Client) URL() string { return c.base }

// QueueDepth is running + pending prompts; the router picks the shallowest.
func (c *Client) QueueDepth(ctx context.Context) (int, error) {
	var q struct {
		Running []json.RawMessage `json:"queue_running"`
		Pending []json.RawMessage `json:"queue_pending"`
	}
	if err := c.getJSON(ctx, "/queue", &q); err != nil {
		return 0, err
	}
	return len(q.Running) + len(q.Pending), nil
}

type wsMsg struct {
	Type string `json:"type"`
	Data struct {
		Value            int     `json:"value"`
		Max              int     `json:"max"`
		Node             *string `json:"node"`
		PromptID         string  `json:"prompt_id"`
		ExceptionMessage string  `json:"exception_message"`
	} `json:"data"`
}

// Execute is ExecuteAll for the one-SaveImage case.
func (c *Client) Execute(ctx context.Context, graph json.RawMessage, onProgress func(step, total int)) ([]byte, error) {
	outs, err := c.ExecuteAll(ctx, graph, onProgress)
	if err != nil {
		return nil, err
	}
	for _, img := range outs {
		return img, nil
	}
	return nil, fmt.Errorf("comfy: no output images")
}

// ExecuteAll runs a graph and returns every output image keyed by
// SaveImage node ID; WS dials before submit, /history poll is the fallback.
func (c *Client) ExecuteAll(ctx context.Context, graph json.RawMessage, onProgress func(step, total int)) (map[string][]byte, error) {
	conn, _, err := websocket.Dial(ctx, c.wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("comfy ws dial: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	conn.SetReadLimit(1 << 22) // preview frames can be large

	promptID, err := c.postPrompt(ctx, graph)
	if err != nil {
		return nil, err
	}

	for {
		var msg wsMsg
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// WS died but the prompt may still be running — poll for it
			return c.pollImages(ctx, promptID)
		}
		if msg.Data.PromptID != promptID {
			continue
		}
		switch msg.Type {
		case "progress":
			if onProgress != nil && msg.Data.Max > 0 {
				onProgress(msg.Data.Value, msg.Data.Max)
			}
		case "execution_error":
			return nil, fmt.Errorf("comfy execution error: %s", msg.Data.ExceptionMessage)
		case "executing":
			if msg.Data.Node == nil { // whole graph finished
				return c.pollImages(ctx, promptID)
			}
		}
	}
}

func (c *Client) postPrompt(ctx context.Context, graph json.RawMessage) (string, error) {
	body, err := json.Marshal(map[string]any{
		"prompt":    graph,
		"client_id": c.clientID,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/prompt", strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("comfy submit: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("comfy submit: %s: %s", resp.Status, raw)
	}
	var out struct {
		PromptID string `json:"prompt_id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.PromptID == "" {
		return "", fmt.Errorf("comfy submit: unexpected response %q", raw)
	}
	return out.PromptID, nil
}

type historyEntry struct {
	Outputs map[string]struct {
		Images []struct {
			Filename  string `json:"filename"`
			Subfolder string `json:"subfolder"`
			Type      string `json:"type"`
		} `json:"images"`
	} `json:"outputs"`
}

// pollImages fetches each node's first output image once /history has it.
func (c *Client) pollImages(ctx context.Context, promptID string) (map[string][]byte, error) {
	for {
		var hist map[string]historyEntry
		if err := c.getJSON(ctx, "/history/"+promptID, &hist); err != nil {
			return nil, err
		}
		if entry, ok := hist[promptID]; ok {
			imgs := make(map[string][]byte)
			for node, out := range entry.Outputs {
				for _, img := range out.Images {
					if img.Type != "output" {
						continue
					}
					q := url.Values{
						"filename":  {img.Filename},
						"subfolder": {img.Subfolder},
						"type":      {img.Type},
					}
					data, err := c.getBytes(ctx, "/view?"+q.Encode())
					if err != nil {
						return nil, err
					}
					imgs[node] = data
					break
				}
			}
			if len(imgs) == 0 {
				return nil, fmt.Errorf("comfy history %s: no output images", promptID)
			}
			return imgs, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (c *Client) getJSON(ctx context.Context, path string, v any) error {
	raw, err := c.getBytes(ctx, path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

func (c *Client) getBytes(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("comfy GET %s: %s", path, resp.Status)
	}
	return io.ReadAll(resp.Body)
}
