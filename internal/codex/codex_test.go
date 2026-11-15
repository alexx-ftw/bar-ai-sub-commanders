package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rusenask/bar-ai-sub-commanders/internal/opencode"
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

func TestSendInvokesCodexWithIsolatedConfigAndPrompt(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "codex")
	script := `#!/bin/sh
printf '%s\n' "$@" > args
cat > prompt
printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}' '{"type":"turn.completed"}'
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	c := Client{Binary: binary, Dir: dir, URL: "http://127.0.0.1:27879/mcp", Model: "gpt-6-luna", Instructions: "instruction"}
	reply, err := c.Send(context.Background(), "1", opencode.Prompt{Variant: "medium", Text: "report"})
	if err != nil || reply.Text != "ok" {
		t.Fatalf("reply = %+v, error = %v", reply, err)
	}
	args, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"--ignore-user-config", "--ephemeral", "read-only", "mcp_servers.bar.url=", "mcp_servers.bar.default_tools_approval_mode=\"approve\"", "gpt-6-luna", "model_reasoning_effort=\"medium\""} {
		if !strings.Contains(string(args), expected) {
			t.Errorf("missing %q in %s", expected, args)
		}
	}
	prompt, err := os.ReadFile(filepath.Join(dir, "prompt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(prompt) != "instruction\n\nreport" {
		t.Errorf("prompt = %q", prompt)
	}
}
