package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rusenask/bar-ai-sub-commanders/internal/opencode"
	"github.com/rusenask/bar-ai-sub-commanders/internal/tools"
)

func TestUncertainFailureAfterToolCall(t *testing.T) {
	base := errors.New("interrupted")
	var marker interface{ Uncertain() bool }
	if err := withUncertainty([]byte(`{"type":"item.started","item":{"type":"mcp_tool_call"}}`), base); !errors.As(err, &marker) || !marker.Uncertain() {
		t.Fatalf("tool call failure must be uncertain: %v", err)
	}
	if err := withUncertainty([]byte(`{"type":"turn.started"}`), base); err != base {
		t.Fatalf("failure before a tool call: %v", err)
	}
}

func TestParseReply(t *testing.T) {
	data := []byte(`{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"set_posture","status":"completed"}}
{"type":"item.completed","item":{"type":"agent_message","text":"Resources are being spent."}}
{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10}}`)
	got, err := parseReply(data)
	if err != nil || got.Text != "Resources are being spent." || len(got.Tools) != 1 || got.Tools[0].Tool != "set_posture" || got.Tokens.Cache.Read != 40 {
		t.Fatalf("reply = %+v, error = %v", got, err)
	}
	for _, input := range []string{`{"type":"turn.failed","error":{"message":"bad"}}`, `{"type":"turn.completed"}`, `invalid`} {
		if _, err := parseReply([]byte(input)); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestCompletedTurnWithFailedToolIsNotAutomaticallyUncertain(t *testing.T) {
	for _, tool := range []string{
		`{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"add_zone","status":"failed"}}`,
		`{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"add_zone","status":"completed","result":{"is_error":true}}}`,
		`{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"add_zone","status":"completed","result":{"isError":true}}}`,
	} {
		data := tool + "\n" + `{"type":"item.completed","item":{"type":"agent_message","text":"done"}}` + "\n" + `{"type":"turn.completed"}`
		_, err := parseReply([]byte(data))
		if err != nil {
			t.Fatalf("completed turn rejected: %v", err)
		}
	}
}

func TestSendInvokesCodexWithIsolatedConfigAndPrompt(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "codex")
	script := `#!/bin/sh
printf '%s\n' "$@" > args
printf '%s' "$SCBRIDGE_REVIEW_TOKEN" > token
cat > prompt
printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}' '{"type":"turn.completed"}'
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	c := Client{Dir: dir, URL: "http://127.0.0.1:27879/mcp", Model: "gpt-6-luna", Instructions: "instruction"}
	reply, err := c.Send(tools.WithReviewToken(context.Background(), "test-token"), "1", opencode.Prompt{Variant: "medium", Text: "report"})
	if err != nil || reply.Text != "ok" {
		t.Fatalf("reply = %+v, error = %v", reply, err)
	}
	args, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"--ignore-user-config", "--ephemeral", "read-only", "mcp_servers.bar.url=", "mcp_servers.bar.default_tools_approval_mode=\"approve\"", "mcp_servers.bar.bearer_token_env_var=\"SCBRIDGE_REVIEW_TOKEN\"", "gpt-6-luna", "model_reasoning_effort=\"medium\""} {
		if !strings.Contains(string(args), expected) {
			t.Errorf("missing %q in %s", expected, args)
		}
	}
	token, err := os.ReadFile(filepath.Join(dir, "token"))
	if err != nil || string(token) != "test-token" {
		t.Fatalf("token = %q, error = %v", token, err)
	}
	prompt, err := os.ReadFile(filepath.Join(dir, "prompt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(prompt) != "instruction\n\nreport" {
		t.Errorf("prompt = %q", prompt)
	}
}
