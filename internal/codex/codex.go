// Package codex runs game reviews with the locally authenticated Codex CLI.
package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/rusenask/bar-ai-sub-commanders/internal/opencode"
	"github.com/rusenask/bar-ai-sub-commanders/internal/tools"
)

type Client struct {
	Dir          string
	URL          string
	Model        string
	Instructions string
	next         atomic.Uint64
}

type uncertainError struct{ err error }

func (e uncertainError) Error() string   { return e.err.Error() }
func (e uncertainError) Unwrap() error   { return e.err }
func (e uncertainError) Uncertain() bool { return true }

func withUncertainty(data []byte, err error) error {
	if err == nil {
		return nil
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		var event struct {
			Type string `json:"type"`
			Item struct {
				Type string `json:"type"`
			} `json:"item"`
		}
		if json.Unmarshal(line, &event) == nil && event.Type == "item.started" && event.Item.Type == "mcp_tool_call" {
			return uncertainError{err}
		}
	}
	return err
}

func (*Client) Stateless() bool { return true }
func (c *Client) CreateSession(context.Context, string) (string, error) {
	return strconv.FormatUint(c.next.Add(1), 10), nil
}
func (*Client) Abort(context.Context, string) error { return nil }

func (c *Client) Send(ctx context.Context, _ string, p opencode.Prompt) (*opencode.Reply, error) {
	args := []string{"exec", "--ignore-user-config", "--ephemeral", "--skip-git-repo-check", "--sandbox", "read-only", "--json", "-c", `approval_policy="never"`, "-c", "mcp_servers.bar.url=" + strconv.Quote(c.URL), "-c", `mcp_servers.bar.default_tools_approval_mode="approve"`}
	token := tools.ReviewToken(ctx)
	if token != "" {
		args = append(args, "-c", `mcp_servers.bar.bearer_token_env_var="SCBRIDGE_REVIEW_TOKEN"`)
	}
	if c.Model != "" {
		args = append(args, "--model", c.Model)
	}
	if p.Variant != "" {
		args = append(args, "-c", "model_reasoning_effort="+strconv.Quote(p.Variant))
	}
	args = append(args, "-")
	cmd := exec.CommandContext(ctx, "codex", args...)
	if err := isolateProcess(cmd); err != nil {
		return nil, err
	}
	cmd.Env = append(os.Environ(), "SCBRIDGE_REVIEW_TOKEN="+token)
	cmd.Dir = c.Dir
	cmd.Stdin = strings.NewReader(c.Instructions + "\n\n" + p.Text)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	cleanupProcess(cmd)
	if err != nil {
		return nil, withUncertainty(stdout.Bytes(), fmt.Errorf("codex: %w: %s", err, strings.TrimSpace(stderr.String())))
	}
	reply, err := parseReply(stdout.Bytes())
	return reply, withUncertainty(stdout.Bytes(), err)
}

func parseReply(data []byte) (*opencode.Reply, error) {
	var reply opencode.Reply
	completed := false
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var event struct {
			Type string `json:"type"`
			Item struct {
				Type   string `json:"type"`
				Text   string `json:"text"`
				Server string `json:"server"`
				Tool   string `json:"tool"`
				Status string `json:"status"`
			} `json:"item"`
			Usage struct {
				Input  float64 `json:"input_tokens"`
				Cached float64 `json:"cached_input_tokens"`
				Output float64 `json:"output_tokens"`
			} `json:"usage"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, fmt.Errorf("codex output: %w", err)
		}
		switch event.Type {
		case "turn.failed":
			return nil, fmt.Errorf("codex turn failed: %s", event.Error.Message)
		case "turn.completed":
			completed = true
			reply.Tokens.Input = event.Usage.Input
			reply.Tokens.Cache.Read = event.Usage.Cached
			reply.Tokens.Output = event.Usage.Output
		case "item.completed":
			switch event.Item.Type {
			case "agent_message":
				reply.Text = event.Item.Text
			case "mcp_tool_call":
				reply.Tools = append(reply.Tools, opencode.ToolCall{Tool: event.Item.Tool, Status: event.Item.Status})
			}
		}
	}
	if !completed || strings.TrimSpace(reply.Text) == "" {
		return nil, fmt.Errorf("codex returned no final message")
	}
	return &reply, nil
}
