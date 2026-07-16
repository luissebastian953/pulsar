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

func fakeDeepSeek(t *testing.T, status int, body string) (*httptest.Server, *http.Request, *[]byte) {
	t.Helper()
	var gotReq http.Request
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = *r
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &gotReq, &captured
}

func deepseekResponse(finishReason, content string) string {
	return fmt.Sprintf(`{"id":"c1","object":"chat.completion","model":"deepseek-v4-flash",`+
		`"choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":%q}],`+
		`"usage":{"prompt_tokens":10,"completion_tokens":10}}`, content, finishReason)
}

func TestDeepSeekDecomposer(t *testing.T) {
	srv, gotReq, captured := fakeDeepSeek(t, 200,
		deepseekResponse("stop", `{"concepts":["silver hair","ponytail"]}`))

	d := NewDeepSeekDecomposer("test-key", "", srv.URL)
	concepts, err := d.Decompose(context.Background(), "a girl with silver hair in a ponytail")
	if err != nil {
		t.Fatal(err)
	}
	if len(concepts) != 2 || concepts[0] != "silver hair" || concepts[1] != "ponytail" {
		t.Errorf("concepts = %v", concepts)
	}

	if gotReq.URL.Path != "/chat/completions" {
		t.Errorf("path = %s", gotReq.URL.Path)
	}
	if auth := gotReq.Header.Get("Authorization"); auth != "Bearer test-key" {
		t.Errorf("auth = %q", auth)
	}
	req := string(*captured)
	for _, want := range []string{
		`"model":"deepseek-v4-flash"`, // current default; deepseek-chat is discontinued
		`"response_format":{"type":"json_object"}`,
		"Do NOT output Danbooru tags",       // vocabulary belongs to the tag DB
		"JSON object of exactly this shape", // json-mode contract: word + example
	} {
		if !strings.Contains(req, want) {
			t.Errorf("request missing %q:\n%.600s", want, req)
		}
	}
}

func TestDeepSeekDecomposerFencedOutput(t *testing.T) {
	srv, _, _ := fakeDeepSeek(t, 200,
		deepseekResponse("stop", "```json\n{\"concepts\":[\"sitting\"]}\n```"))
	d := NewDeepSeekDecomposer("k", "", srv.URL)
	concepts, err := d.Decompose(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(concepts) != 1 || concepts[0] != "sitting" {
		t.Errorf("concepts = %v", concepts)
	}
}

func TestDeepSeekDecomposerErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"http error", 401, `{"error":{"message":"invalid api key"}}`, "401"},
		{"no choices", 200, `{"choices":[]}`, "no choices"},
		{"truncated", 200, deepseekResponse("length", `{"concepts":["a`), "truncated"},
		{"garbage content", 200, deepseekResponse("stop", "not json"), "unexpected output"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _, _ := fakeDeepSeek(t, tc.status, tc.body)
			d := NewDeepSeekDecomposer("k", "", srv.URL)
			_, err := d.Decompose(context.Background(), "x")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}
