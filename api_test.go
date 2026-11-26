package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// asUser skips auth and injects a logged-in user, like Auth.require does.
func asUser(name string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, name)))
	})
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRoomAPI(t *testing.T) {
	h := asUser("Ada Lovelace", appHandler(NewStore(), t.TempDir()))

	rec := do(t, h, "POST", "/api/rooms", `{"name":"Toilet"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var room Room
	json.Unmarshal(rec.Body.Bytes(), &room)

	if rec := do(t, h, "POST", "/api/rooms", `{"name":""}`); rec.Code != http.StatusBadRequest {
		t.Errorf("empty name: %d", rec.Code)
	}
	if rec := do(t, h, "POST", "/api/rooms", `not json`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad json: %d", rec.Code)
	}
	if rec := do(t, h, "POST", "/api/rooms", `{"name":"`+strings.Repeat("x", 5000)+`"}`); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("huge body: %d", rec.Code)
	}

	rec = do(t, h, "PUT", "/api/rooms/"+room.ID+"/occupied", `{"occupied":true}`)
	json.Unmarshal(rec.Body.Bytes(), &room)
	if rec.Code != http.StatusOK || !room.Occupied || room.By != "Ada Lovelace" || room.Since == nil {
		t.Fatalf("occupy: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, "PUT", "/api/rooms/nope/occupied", `{"occupied":true}`); rec.Code != http.StatusNotFound {
		t.Errorf("occupy unknown: %d", rec.Code)
	}

	if rec := do(t, h, "DELETE", "/api/rooms/"+room.ID, ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete: %d", rec.Code)
	}
	if rec := do(t, h, "DELETE", "/api/rooms/"+room.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("delete again: %d", rec.Code)
	}

	if rec := do(t, h, "GET", "/api/me", ""); !strings.Contains(rec.Body.String(), `"Ada Lovelace"`) {
		t.Errorf("me: %s", rec.Body)
	}
	if rec := do(t, h, "GET", "/api/unknown", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown api route: %d", rec.Code)
	}
}

func TestSPAFallback(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("INDEX"), 0o644)
	os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("JS"), 0o644)
	h := spa(dir)

	for path, want := range map[string]string{
		"/":                 "INDEX",
		"/rooms/abc":        "INDEX",
		"/assets/app.js":    "JS",
		"/../../etc/passwd": "invalid URL path\n", // ServeFile refuses ".." outright
		"/assets/":          "INDEX",
	} {
		if got := do(t, h, "GET", path, "").Body.String(); got != want {
			t.Errorf("GET %s = %q, want %q", path, got, want)
		}
	}
}

func TestEventsStream(t *testing.T) {
	heartbeat = 50 * time.Millisecond
	defer func() { heartbeat = 20 * time.Second }()
	store := NewStore()
	srv := httptest.NewServer(asUser("Ada", appHandler(store, t.TempDir())))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/events", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}

	lines := bufio.NewScanner(res.Body)
	next := func(prefix string) string {
		t.Helper()
		for lines.Scan() {
			if strings.HasPrefix(lines.Text(), prefix) {
				return lines.Text()
			}
		}
		t.Fatalf("stream ended while waiting for %q: %v", prefix, lines.Err())
		return ""
	}

	if got := next("data: "); got != "data: []" {
		t.Fatalf("first event = %q", got)
	}
	next(": ping")
	store.Create("Toilet")
	if got := next("data: "); !strings.Contains(got, `"name":"Toilet"`) {
		t.Fatalf("update event = %q", got)
	}
}
