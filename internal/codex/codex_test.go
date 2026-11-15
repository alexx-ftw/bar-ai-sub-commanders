package codex

import (
	"errors"
	"testing"
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
