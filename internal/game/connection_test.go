package game

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"runtime"
	"testing"
	"time"
)

func TestMessageIDsKeepPrecisionAndNumericDecisionCompatibility(t *testing.T) {
	const actionID messageID = 1794767243948000001
	data, err := json.Marshal(message{Type: "action", ID: actionID})
	if err != nil {
		t.Fatal(err)
	}
	var action message
	if err := json.Unmarshal(data, &action); err != nil || action.ID != actionID {
		t.Fatalf("action %s: %+v %v", data, action, err)
	}
	var decision message
	if err := json.Unmarshal([]byte(`{"type":"decide","id":-5}`), &decision); err != nil || decision.ID != -5 {
		t.Fatalf("decision %+v: %v", decision, err)
	}
	data, err = json.Marshal(message{Type: "decision", ID: decision.ID})
	if err != nil || string(data) != `{"type":"decision","id":-5}` {
		t.Fatalf("decision %s: %v", data, err)
	}
}

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

func TestDoDoesNotSendActionToReplacementConnection(t *testing.T) {
	s := NewServer("", slog.New(slog.NewTextHandler(io.Discard, nil)))
	a, peerA := net.Pipe()
	defer peerA.Close()
	first := tcpConn{a}
	s.attach(first)
	defer first.Close()

	// Hold writes until Do has registered its request, then replace the connection.
	s.writeMu.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := s.Do(context.Background(), 1, "set_posture", map[string]any{"posture": "spend"})
		done <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		registered := len(s.pending) == 1
		s.mu.Unlock()
		if registered {
			break
		}
		if time.Now().After(deadline) {
			s.writeMu.Unlock()
			t.Fatal("Do did not register its request")
		}
		runtime.Gosched()
	}

	b, peerB := net.Pipe()
	defer peerB.Close()
	s.attach(tcpConn{b})
	s.writeMu.Unlock()

	if err := <-done; err != ErrOffline {
		t.Fatalf("Do error = %v, want %v", err, ErrOffline)
	}
	peerB.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	var buf [1]byte
	if n, err := peerB.Read(buf[:]); n != 0 || err == nil {
		t.Fatalf("replacement connection received action byte %q, %v", buf[:n], err)
	}
}

func TestDoForRejectsConnectionReplacedAfterGuard(t *testing.T) {
	s := NewServer("", slog.New(slog.NewTextHandler(io.Discard, nil)))
	a, peerA := net.Pipe()
	defer peerA.Close()
	s.attach(tcpConn{a})
	_, _, _, oldID := s.SnapshotWithID()
	b, peerB := net.Pipe()
	defer peerB.Close()
	s.attach(tcpConn{b})
	if _, err := s.DoFor(context.Background(), oldID, 1, "set_role", nil); err != ErrOffline {
		t.Fatalf("stale authorization: %v", err)
	}
	s.TellFor(oldID, 1, "status", map[string]any{"text": "old review"})
	peerB.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	if n, err := peerB.Read(make([]byte, 1)); n != 0 || err == nil {
		t.Fatalf("action reached replacement connection: n=%d err=%v", n, err)
	}
}

func TestCanceledActionIsNotWritten(t *testing.T) {
	s := NewServer("", slog.New(slog.NewTextHandler(io.Discard, nil)))
	a, peer := net.Pipe()
	defer peer.Close()
	conn := tcpConn{a}
	s.attach(conn)
	defer conn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Do(ctx, 1, "set_role", nil); err != context.Canceled {
		t.Fatalf("canceled action: %v", err)
	}
	peer.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	var b [1]byte
	if n, err := peer.Read(b[:]); n != 0 || err == nil {
		t.Fatalf("canceled action wrote %q, %v", b[:n], err)
	}
}

func TestExplicitRejectionIsDistinguishedFromFailure(t *testing.T) {
	s := NewServer("", slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, tt := range []struct {
		msg      string
		rejected bool
	}{
		{`{"type":"result","id":"1","ok":false,"rejected":true,"error":"cooldown"}`, true},
		{`{"type":"result","id":"1","ok":false,"error":"exception"}`, false},
	} {
		ch := make(chan result, 1)
		s.pending[1] = ch
		var msg message
		if err := json.Unmarshal([]byte(tt.msg), &msg); err != nil {
			t.Fatal(err)
		}
		s.handle(msg)
		_, ok := (<-ch).err.(*RejectedError)
		if ok != tt.rejected {
			t.Fatalf("message %s classified as rejected=%v", tt.msg, ok)
		}
	}
}
