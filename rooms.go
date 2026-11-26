package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	maxRooms    = 100
	maxRoomName = 60
)

var (
	errNotFound    = errors.New("room not found")
	errInvalidName = errors.New("room name must be 1-60 characters")
	errTooMany     = errors.New("too many rooms")
)

type Room struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Occupied bool       `json:"occupied"`
	By       string     `json:"by,omitempty"`
	Since    *time.Time `json:"since,omitempty"`
	created  time.Time
}

// Store keeps all rooms in memory and pushes a full snapshot to every subscriber on each change.
type Store struct {
	mu    sync.Mutex
	rooms map[string]*Room
	subs  map[chan []Room]struct{}
	now   func() time.Time
}

func NewStore() *Store {
	return &Store{rooms: map[string]*Room{}, subs: map[chan []Room]struct{}{}, now: time.Now}
}

func (s *Store) Create(name string) (Room, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxRoomName {
		return Room{}, errInvalidName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.rooms) >= maxRooms {
		return Room{}, errTooMany
	}
	id := randomID(4)
	for s.rooms[id] != nil {
		id = randomID(4)
	}
	r := &Room{ID: id, Name: name, created: s.now()}
	s.rooms[r.ID] = r
	s.publish()
	return *r, nil
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rooms[id]; !ok {
		return errNotFound
	}
	delete(s.rooms, id)
	s.publish()
	return nil
}

func (s *Store) SetOccupied(id string, occupied bool, by string) (Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rooms[id]
	if !ok {
		return Room{}, errNotFound
	}
	if r.Occupied == occupied {
		return *r, nil // keep the original "since" when someone presses twice
	}
	r.Occupied = occupied
	r.By, r.Since = "", nil
	if occupied {
		now := s.now()
		r.By, r.Since = by, &now
	}
	s.publish()
	return *r, nil
}

// Subscribe returns a channel that immediately holds the current snapshot.
// Call the returned func to unsubscribe.
func (s *Store) Subscribe() (<-chan []Room, func()) {
	ch := make(chan []Room, 1)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	ch <- s.snapshot()
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}

func (s *Store) snapshot() []Room {
	list := make([]Room, 0, len(s.rooms))
	for _, r := range s.rooms {
		list = append(list, *r)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].created.Equal(list[j].created) {
			return list[i].ID < list[j].ID
		}
		return list[i].created.Before(list[j].created)
	})
	return list
}

// publish must be called with s.mu held. A slow subscriber only ever gets the latest snapshot.
func (s *Store) publish() {
	snap := s.snapshot()
	for ch := range s.subs {
		select {
		case <-ch:
		default:
		}
		ch <- snap
	}
}

func randomID(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
