package nltag

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeClaude(t *testing.T, body string) (*httptest.Server, *[]byte) {
	t.Helper()
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &captured
}

func claudeResponse(stopReason, text string) string {
	content := "[]"
	if text != "" {
		content = fmt.Sprintf(`[{"type":"text","text":%q}]`, text)
	}
	return fmt.Sprintf(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-4-8",`+
		`"content":%s,"stop_reason":%q,"stop_sequence":null,`+
		`"usage":{"input_tokens":10,"output_tokens":10}}`, content, stopReason)
}

func TestClaudeDecomposer(t *testing.T) {
	srv, captured := fakeClaude(t, claudeResponse("end_turn", `{"concepts":["silver hair","ponytail"]}`))

	d := NewClaudeDecomposer("test-key", "", srv.URL)
	concepts, err := d.Decompose(context.Background(), "a girl with silver hair in a ponytail")
	if err != nil {
		t.Fatal(err)
	}
	if len(concepts) != 2 || concepts[0] != "silver hair" || concepts[1] != "ponytail" {
		t.Errorf("concepts = %v", concepts)
	}

	req := string(*captured)
	for _, want := range []string{
		`"model":"claude-opus-4-8"`,   // skill-mandated default
		"output_config",               // structured outputs pin the schema
		"Do NOT output Danbooru tags", // vocabulary belongs to the tag DB
	} {
		if !strings.Contains(req, want) {
			t.Errorf("request missing %q:\n%.500s", want, req)
		}
	}
}

func TestClaudeDecomposerRefusal(t *testing.T) {
	srv, _ := fakeClaude(t, claudeResponse("refusal", ""))
	d := NewClaudeDecomposer("test-key", "", srv.URL)
	if _, err := d.Decompose(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "declined") {
		t.Fatalf("want refusal error, got %v", err)
	}
}

func TestClaudeDecomposerGarbageOutput(t *testing.T) {
	srv, _ := fakeClaude(t, claudeResponse("end_turn", "not json"))
	d := NewClaudeDecomposer("test-key", "", srv.URL)
	if _, err := d.Decompose(context.Background(), "x"); err == nil {
		t.Fatal("want parse error")
	}
}
