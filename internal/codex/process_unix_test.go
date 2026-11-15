//go:build unix

package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rusenask/bar-ai-sub-commanders/internal/opencode"
	"github.com/rusenask/bar-ai-sub-commanders/internal/tools"
)

func TestCancelStopsCodexWrapperAndChild(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\n(sleep 0.5; touch child-survived) &\ntouch started\nwait\n"
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	c := Client{Dir: dir, URL: "http://127.0.0.1:1/mcp"}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := c.Send(ctx, "1", opencode.Prompt{Text: "test"}); done <- err }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("wrapper did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancelled command succeeded")
	}
	time.Sleep(700 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, "child-survived")); !os.IsNotExist(err) {
		t.Fatalf("child survived: %v", err)
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

func TestWrapperExitDoesNotLeaveChildHoldingOutput(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\n(sleep 2; touch child-survived) &\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	c := Client{Dir: dir, URL: "http://127.0.0.1:1/mcp"}
	start := time.Now()
	if _, err := c.Send(context.Background(), "1", opencode.Prompt{Text: "test"}); err == nil {
		t.Fatal("incomplete output accepted")
	}
	if time.Since(start) > 1800*time.Millisecond {
		t.Fatal("wrapper exit left Send blocked")
	}
	time.Sleep(1200 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, "child-survived")); !os.IsNotExist(err) {
		t.Fatalf("child survived: %v", err)
	}
}
