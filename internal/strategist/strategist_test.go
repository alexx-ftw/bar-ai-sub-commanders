package strategist

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/rusenask/bar-ai-sub-commanders/internal/game"
)

type uncertainTestError struct{}

func (uncertainTestError) Error() string   { return "uncertain" }
func (uncertainTestError) Uncertain() bool { return true }

func TestStatelessReviewGetsContextWithoutRepeatingFirstMapRequest(t *testing.T) {
	info := &game.Info{Map: "test", Player: "p", Faction: "cortex"}
	sub := game.Sub{ID: 3, Role: "army"}
	prompt := report(info, &game.State{}, sub, &subState{}, "routine", false, true, "")
	if !strings.Contains(prompt, "You command sub-commander #3") || strings.Contains(prompt, "This is your first report") {
		t.Fatalf("stateless report: %s", prompt)
	}
}

func TestUncertainReviewWaitsForPlayerToToggleAI(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(Config{Interval: 30 * time.Second, Urgent: 8 * time.Second, MaxInFlight: 1}, game.NewServer("127.0.0.1:0", log), nil, log)
	state := &game.State{Frame: 100, Subs: []game.Sub{{ID: 1, AI: true}}}
	_, started := s.schedule(state, func(game.Sub, string) {})
	if started != 1 {
		t.Fatal("initial review did not start")
	}
	ss := s.subs[1]
	s.fail(state.Subs[0], ss, state.Frame, uncertainTestError{})
	ss.busy = false
	s.pace.done()
	state.Frame += seconds(10 * time.Minute)
	_, started = s.schedule(state, func(game.Sub, string) {})
	if started != 0 {
		t.Fatal("retried an uncertain turn")
	}
	state.Subs[0].AI = false
	s.schedule(state, func(game.Sub, string) {})
	state.Subs[0].AI = true
	_, started = s.schedule(state, func(game.Sub, string) {})
	if started != 1 {
		t.Fatal("did not resume after the player toggled AI")
	}
}

func TestUncertainReviewSeesFastToggleBetweenSnapshots(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(Config{Interval: 30 * time.Second, Urgent: 8 * time.Second, MaxInFlight: 1}, game.NewServer("", log), nil, log)
	state := &game.State{Frame: 100, Subs: []game.Sub{{ID: 1, AI: true, AIRevision: 1}}}
	s.schedule(state, func(game.Sub, string) {})
	ss := s.subs[1]
	s.fail(state.Subs[0], ss, state.Frame, uncertainTestError{})
	ss.busy = false
	s.pace.done()
	state.Frame += seconds(10 * time.Minute)
	state.Subs[0].AIRevision += 2 // off and on, both between snapshots
	_, started := s.schedule(state, func(game.Sub, string) {})
	if started != 1 {
		t.Fatal("missed the player's off/on reset")
	}
}

func TestFailedReviewsBackOff(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(Config{Interval: 30 * time.Second, Urgent: 8 * time.Second}, game.NewServer("127.0.0.1:0", log), nil, log)
	sub := game.Sub{ID: 1, Name: "Economy 1", AI: true}

	frame := 1000
	if s.due(sub, frame) == "" {
		t.Fatal("a new sub-commander should be reviewed right away")
	}
	ss := s.subs[1]
	ss.busy = false
	s.fail(sub, ss, frame, errors.New("opencode down"))
	if why := s.due(sub, frame+30); why != "" {
		t.Fatalf("reviewed again 1 s after a failure: %q", why)
	}
	if why := s.due(sub, frame+seconds(5*time.Second)); why == "" {
		t.Fatal("not retried after the first backoff")
	}

	// repeated failures wait longer, capped at 2 minutes
	for range 10 {
		ss.busy = false
		s.fail(sub, ss, frame, errors.New("opencode down"))
	}
	ss.busy = false
	if why := s.due(sub, frame+seconds(90*time.Second)); why != "" {
		t.Fatalf("retried within 90 s after many failures: %q", why)
	}
	if why := s.due(sub, frame+seconds(2*time.Minute)); why == "" {
		t.Fatal("backoff should be capped at 2 minutes")
	}
}

func TestSingleReviewSlotRotatesBetweenBusySubcommanders(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(Config{Interval: 30 * time.Second, Urgent: 8 * time.Second, MaxInFlight: 1}, game.NewServer("127.0.0.1:0", log), nil, log)
	state := &game.State{Subs: []game.Sub{{ID: 1, AI: true, Threats: 1}, {ID: 2, AI: true, Threats: 1}, {ID: 3, AI: true, Threats: 1}}}
	for i := 0; i < 9; i++ {
		state.Frame += seconds(65 * time.Second)
		var chosen []int
		_, started := s.schedule(state, func(sub game.Sub, _ string) { chosen = append(chosen, sub.ID) })
		if started != 1 || len(chosen) != 1 || chosen[0] != i%3+1 {
			t.Fatalf("turn %d: started %d, selected %v", i, started, chosen)
		}
		ss := s.subs[chosen[0]]
		ss.busy = false
		ss.reports++
		ss.lastThreats = 1
		s.pace.done()
	}
}

func TestPaceLine(t *testing.T) {
	line := paceLine(8*60*framesPerSecond, game.Economy{MetalIncome: 20, EnergyIncome: 300})
	if !strings.Contains(line, "Pace: metal income 20.0/s, energy 300/s at 8 min; strong players' 1v1s at 8 min: metal ") {
		t.Errorf("pace line %q", line)
	}
	if paceLine(60*framesPerSecond, game.Economy{}) != "" {
		t.Error("no pace in the first two minutes")
	}
}

func TestParseOps(t *testing.T) {
	ops, err := parseOps("Here you go:\n```json\n[{\"op\":\"confirm\",\"id\":2},{\"op\":\"create\",\"text\":\"x\",\"stage\":\"mid\"}]\n```")
	if err != nil || len(ops) != 2 || ops[0].Op != "confirm" || ops[1].Stage != "mid" {
		t.Fatalf("%v %+v", err, ops)
	}
	if _, err := parseOps("nothing to add"); err == nil {
		t.Error("no array is an error (the book stays as it was)")
	}
	if stageOf(30*60*5) != "opening" || stageOf(30*60*12) != "mid" || stageOf(30*60*40) != "late" {
		t.Error("stages")
	}
}
