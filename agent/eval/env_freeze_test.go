package eval

import (
	"testing"
	"time"
)

func TestFrozenTimeParsesAnInstant(t *testing.T) {
	spec := EnvSpec{FreezeTime: "2026-08-01T12:30:00Z"}
	frozen, ok, err := spec.FrozenTime()
	if err != nil || !ok {
		t.Fatalf("expected a frozen instant, got ok=%v err=%v", ok, err)
	}
	if !frozen.Equal(time.Date(2026, 8, 1, 12, 30, 0, 0, time.UTC)) {
		t.Fatalf("unexpected instant %s", frozen)
	}
}

func TestFrozenTimeIsAbsentByDefault(t *testing.T) {
	_, ok, err := EnvSpec{}.FrozenTime()
	if err != nil || ok {
		t.Fatalf("an unset clock must read as absent, got ok=%v err=%v", ok, err)
	}
}

// A malformed instant must fail loudly. Falling back to the wall clock would
// produce a run that silently is not the reproducible one that was asked for.
func TestFrozenTimeRejectsMalformedInstant(t *testing.T) {
	for _, value := range []string{"2026-08-01", "yesterday", "1754000000"} {
		if _, _, err := (EnvSpec{FreezeTime: value}).FrozenTime(); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}

// Freezing the clock without pinning the data would leave the questions fixed
// while the rows they ask about slide forward on the next boot.
func TestFreezingTheClockPinsTheDataToTheSameDay(t *testing.T) {
	anchor, err := (EnvSpec{FreezeTime: "2026-08-01T12:30:00Z"}).EffectiveDataAnchor()
	if err != nil {
		t.Fatal(err)
	}
	if anchor != "2026-08-01" {
		t.Fatalf("expected the frozen day as the anchor, got %q", anchor)
	}
}

// An explicit anchor still wins: resuming a run pins the anchor that run began
// on, which may differ from the day the clock is frozen at.
func TestExplicitAnchorOutranksTheFrozenDay(t *testing.T) {
	anchor, err := (EnvSpec{FreezeTime: "2026-08-02T00:00:00Z", PinDataAnchor: "2026-08-01"}).EffectiveDataAnchor()
	if err != nil {
		t.Fatal(err)
	}
	if anchor != "2026-08-01" {
		t.Fatalf("expected the explicit anchor to win, got %q", anchor)
	}
}

func TestNoAnchorWithoutAFrozenClock(t *testing.T) {
	anchor, err := (EnvSpec{}).EffectiveDataAnchor()
	if err != nil || anchor != "" {
		t.Fatalf("expected no anchor, got %q err=%v", anchor, err)
	}
}
