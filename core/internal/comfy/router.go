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

func (r *Router) ExecuteAll(ctx context.Context, graph json.RawMessage, onProgress func(step, total int)) (map[string][]byte, error) {
	return r.pick(ctx).ExecuteAll(ctx, graph, onProgress)
}
