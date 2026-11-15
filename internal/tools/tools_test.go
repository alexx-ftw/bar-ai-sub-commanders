package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rusenask/bar-ai-sub-commanders/internal/game"
)

func TestMCPTimeoutBlocksFollowingAction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := game.NewServer("", slog.New(slog.NewTextHandler(io.Discard, nil)))
	listener, err := game.ListenTCP(ctx, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go g.Serve(ctx, listener)
	conn, err := net.Dial("tcp", strings.TrimPrefix(listener.Addr(), "tcp "))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	deadline := time.Now().Add(time.Second)
	for {
		_, _, online := g.Snapshot()
		if online {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("game did not connect")
		}
		time.Sleep(time.Millisecond)
	}
	oldTimeout := game.AnswerTimeout
	game.AnswerTimeout = 100 * time.Millisecond
	defer func() { game.AnswerTimeout = oldTimeout }()
	var stopped atomic.Bool
	guard := func(context.Context, int) (uint64, error) {
		if stopped.Load() {
			return 0, errors.New("stopped")
		}
		_, _, _, id := g.SnapshotWithID()
		return id, nil
	}
	server := httptest.NewServer(Handler(g, guard, func(context.Context, int, error) { stopped.Store(true) }))
	defer server.Close()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	result := make(chan *mcp.CallToolResult, 1)
	failures := make(chan error, 1)
	go func() {
		r, e := client.CallTool(ctx, &mcp.CallToolParams{Name: "set_autonomy", Arguments: map[string]any{"sub_id": 1, "autonomy": "full"}})
		result <- r
		failures <- e
	}()
	reader := bufio.NewReader(conn)
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := reader.ReadBytes('\n'); err != nil {
		t.Fatalf("first action: %v", err)
	}
	first, err := <-result, <-failures
	if err != nil || first == nil || !first.IsError || !stopped.Load() {
		t.Fatalf("first result: %+v %v", first, err)
	}
	second, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "set_role", Arguments: map[string]any{"sub_id": 1, "role": "army"}})
	if err != nil || second == nil || !second.IsError {
		t.Fatalf("second result: %+v %v", second, err)
	}
	conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if _, err := reader.ReadBytes('\n'); err == nil {
		t.Fatal("second action reached the game")
	}
}

func TestFailureRevokesFollowingGameCalls(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var stopped atomic.Bool
	var called atomic.Int32
	c := &guardedCalls{
		guard: func(context.Context, int) (uint64, error) {
			if stopped.Load() {
				return 0, errors.New("stopped")
			}
			return 1, nil
		},
		call: func(context.Context, uint64, int, string, map[string]any) (json.RawMessage, error) {
			called.Add(1)
			close(entered)
			<-release
			return nil, errors.New("timeout")
		},
		onFailure: func(context.Context, int, error) { stopped.Store(true) },
	}
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { _, err := c.Do(context.Background(), 1, "add_zone", nil); first <- err }()
	<-entered
	go func() { _, err := c.Do(context.Background(), 1, "add_zone", nil); second <- err }()
	close(release)
	if err := <-first; err == nil || err.Error() != "timeout" {
		t.Fatalf("first call: %v", err)
	}
	if err := <-second; err == nil || err.Error() != "stopped" {
		t.Fatalf("second call: %v", err)
	}
	if n := called.Load(); n != 1 {
		t.Fatalf("sent %d actions", n)
	}
}

func TestFailedReadOnlyToolDoesNotStopReview(t *testing.T) {
	var stopped bool
	c := &guardedCalls{
		guard: func(context.Context, int) (uint64, error) { return 1, nil },
		call: func(context.Context, uint64, int, string, map[string]any) (json.RawMessage, error) {
			return nil, errors.New("timeout")
		},
		onFailure: func(context.Context, int, error) { stopped = true },
	}
	for _, op := range []string{"terrain", "map_view", "build_options", "structures"} {
		_, _ = c.Do(context.Background(), 1, op, nil)
		if stopped {
			t.Fatalf("read-only %s stopped the review", op)
		}
	}
}
