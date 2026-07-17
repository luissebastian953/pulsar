package comfy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
)

// Router routes each job to the shallowest ComfyUI queue (one per GPU).
type Router struct {
	clients []*Client
}

func NewRouter(clients []*Client) (*Router, error) {
	if len(clients) == 0 {
		return nil, fmt.Errorf("router needs at least one comfy client")
	}
	return &Router{clients: clients}, nil
}

func (r *Router) Size() int { return len(r.clients) }

func (r *Router) pick(ctx context.Context) *Client {
	best := r.clients[0]
	bestDepth := -1
	for _, c := range r.clients {
		depth, err := c.QueueDepth(ctx)
		if err != nil {
			log.Printf("comfy %s: queue depth: %v", c.URL(), err)
			continue
		}
		if bestDepth == -1 || depth < bestDepth {
			best, bestDepth = c, depth
		}
	}
	return best
}

func (r *Router) Execute(ctx context.Context, graph json.RawMessage, onProgress func(step, total int)) ([]byte, error) {
	return r.pick(ctx).Execute(ctx, graph, onProgress)
}

// ExecuteAll routes to one process; a non-nil input is uploaded to that
// same process first (upload/job affinity).
func (r *Router) ExecuteAll(ctx context.Context, graph json.RawMessage, input *Input, onProgress func(step, total int)) (map[string][]byte, error) {
	c := r.pick(ctx)
	if input != nil {
		if err := c.UploadImage(ctx, input.Name, input.Data); err != nil {
			return nil, err
		}
	}
	return c.ExecuteAll(ctx, graph, onProgress)
}
