package game

import (
	"io"
	"log/slog"
	"net"
	"testing"
)

func TestReplacementClearsStateAndRejectsOldMessages(t *testing.T) {
	s := NewServer("", slog.New(slog.NewTextHandler(io.Discard, nil)))
	a, peerA := net.Pipe()
	defer peerA.Close()
	first := tcpConn{a}
	s.attach(first)
	s.handleFrom(first, message{Type: "hello", Game: &Info{Map: "old"}})
	s.handleFrom(first, message{Type: "state", State: &State{Frame: 123}})
	_, _, _, oldID := s.SnapshotWithID()
	pending := make(chan result, 1)
	s.mu.Lock()
	s.pending[1] = pending
	s.mu.Unlock()
	b, peerB := net.Pipe()
	defer peerB.Close()
	second := tcpConn{b}
	s.attach(second)
	defer second.Close()
	info, state, online, newID := s.SnapshotWithID()
	if !online || info != nil || state != nil || oldID == newID {
		t.Fatalf("replacement snapshot: %+v %+v %v %d %d", info, state, online, oldID, newID)
	}
	if got := <-pending; got.err != ErrOffline {
		t.Fatalf("pending request: %v", got.err)
	}
	s.handleFrom(first, message{Type: "hello", Game: &Info{Map: "stale"}})
	info, _, _, _ = s.SnapshotWithID()
	if info != nil {
		t.Fatalf("accepted old connection: %+v", info)
	}
	s.handleFrom(second, message{Type: "hello", Game: &Info{Map: "new"}})
	info, _, _, _ = s.SnapshotWithID()
	if info == nil || info.Map != "new" {
		t.Fatalf("new connection: %+v", info)
	}
}
