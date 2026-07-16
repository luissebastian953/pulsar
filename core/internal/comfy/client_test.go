package comfy_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"pulsar/core/internal/comfy"
	"pulsar/core/internal/comfy/comfytest"
)

func TestExecute(t *testing.T) {
	fake := comfytest.New(t)
	c, err := comfy.New(fake.URL())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	graph := json.RawMessage(`{"5":{"class_type":"KSampler","inputs":{"seed":7}}}`)
	var steps []int
	img, err := c.Execute(ctx, graph, func(step, total int) {
		if total != fake.Steps {
			t.Errorf("total = %d, want %d", total, fake.Steps)
		}
		steps = append(steps, step)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(img, fake.PNG) {
		t.Error("image bytes do not round-trip")
	}
	if len(steps) != fake.Steps {
		t.Errorf("got %d progress callbacks, want %d", len(steps), fake.Steps)
	}
	if !bytes.Equal(fake.LastGraph(), graph) {
		t.Errorf("submitted graph mangled: %s", fake.LastGraph())
	}
}

func TestExecuteError(t *testing.T) {
	fake := comfytest.New(t)
	fake.FailExecution = true
	c, err := comfy.New(fake.URL())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = c.Execute(ctx, json.RawMessage(`{}`), nil)
	if err == nil || !strings.Contains(err.Error(), "fake execution failure") {
		t.Fatalf("want execution error, got %v", err)
	}
}

func TestQueueDepth(t *testing.T) {
	fake := comfytest.New(t)
	c, err := comfy.New(fake.URL())
	if err != nil {
		t.Fatal(err)
	}
	depth, err := c.QueueDepth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if depth != 0 {
		t.Errorf("depth = %d, want 0", depth)
	}
}

func TestNewRejectsBadURL(t *testing.T) {
	for _, u := range []string{"", "not a url", "ftp://x"} {
		if _, err := comfy.New(u); err == nil {
			t.Errorf("comfy.New(%q) should fail", u)
		}
	}
}
