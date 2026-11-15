//go:build unix

package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rusenask/bar-ai-sub-commanders/internal/opencode"
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
