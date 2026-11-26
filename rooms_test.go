package main

import (
	"strings"
	"testing"
	"time"
)

func TestCreateValidatesName(t *testing.T) {
	s := NewStore()
	for _, name := range []string{"", "   ", strings.Repeat("x", 61)} {
		if _, err := s.Create(name); err != errInvalidName {
			t.Errorf("Create(%q) err = %v, want errInvalidName", name, err)
		}
	}
	r, err := s.Create("  Toilet 1st floor  ")
	if err != nil || r.Name != "Toilet 1st floor" || r.ID == "" || r.Occupied {
		t.Fatalf("Create = %+v, %v", r, err)
	}
}

func TestCreateLimit(t *testing.T) {
	s := NewStore()
	for range maxRooms {
		if _, err := s.Create("room"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Create("one too many"); err != errTooMany {
		t.Fatalf("err = %v, want errTooMany", err)
	}
}

func TestSetOccupied(t *testing.T) {
	s := NewStore()
	t0 := time.Date(2026, 11, 26, 10, 42, 0, 0, time.UTC)
	s.now = func() time.Time { return t0 }
	r, _ := s.Create("Toilet")

	got, err := s.SetOccupied(r.ID, true, "Ada")
	if err != nil || !got.Occupied || got.By != "Ada" || !got.Since.Equal(t0) {
		t.Fatalf("occupy = %+v, %v", got, err)
	}

	// A second press from someone else must not reset who occupied it and since when.
	s.now = func() time.Time { return t0.Add(time.Minute) }
	got, _ = s.SetOccupied(r.ID, true, "Bob")
	if got.By != "Ada" || !got.Since.Equal(t0) {
		t.Fatalf("second occupy changed state: %+v", got)
	}

	got, _ = s.SetOccupied(r.ID, false, "Bob")
	if got.Occupied || got.By != "" || got.Since != nil {
		t.Fatalf("release = %+v", got)
	}

	if _, err := s.SetOccupied("nope", true, "Ada"); err != errNotFound {
		t.Fatalf("unknown room err = %v", err)
	}
}

func TestDelete(t *testing.T) {
	s := NewStore()
	r, _ := s.Create("Toilet")
	if err := s.Delete(r.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(r.ID); err != errNotFound {
		t.Fatalf("second delete err = %v", err)
	}
}

func TestSubscribe(t *testing.T) {
	s := NewStore()
	a, _ := s.Create("A")
	ch, unsubscribe := s.Subscribe()

	if snap := <-ch; len(snap) != 1 || snap[0].ID != a.ID {
		t.Fatalf("initial snapshot = %+v", snap)
	}

	// Several changes without reading: the subscriber only sees the newest state.
	s.Create("B")
	s.SetOccupied(a.ID, true, "Ada")
	snap := <-ch
	if len(snap) != 2 || snap[0].Name != "A" || !snap[0].Occupied || snap[1].Name != "B" {
		t.Fatalf("latest snapshot = %+v", snap)
	}
	select {
	case extra := <-ch:
		t.Fatalf("unexpected extra snapshot %+v", extra)
	default:
	}

	unsubscribe()
	s.Create("C")
	select {
	case snap := <-ch:
		t.Fatalf("got snapshot after unsubscribe: %+v", snap)
	default:
	}
}
