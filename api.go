package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

var heartbeat = 20 * time.Second

// newHandler wires all routes. Everything except /healthz and /auth/* needs a session.
func newHandler(store *Store, auth *Auth, staticDir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /auth/login", auth.login)
	mux.HandleFunc("GET /auth/callback", auth.callback)
	mux.HandleFunc("POST /auth/logout", auth.logout)
	mux.Handle("/", auth.require(appHandler(store, staticDir)))
	return http.NewCrossOriginProtection().Handler(mux)
}

func appHandler(store *Store, staticDir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"name": userName(r)})
	})
	mux.HandleFunc("GET /api/events", events(store))
	mux.HandleFunc("POST /api/rooms", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		if !readJSON(w, r, &body) {
			return
		}
		room, err := store.Create(body.Name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, room)
	})
	mux.HandleFunc("DELETE /api/rooms/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := store.Delete(r.PathValue("id")); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PUT /api/rooms/{id}/occupied", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Occupied bool `json:"occupied"`
		}
		if !readJSON(w, r, &body) {
			return
		}
		room, err := store.SetOccupied(r.PathValue("id"), body.Occupied, userName(r))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, room)
	})
	mux.HandleFunc("/api/", http.NotFound)
	mux.Handle("/", spa(staticDir))
	return mux
}

// events streams the full room list on connect and after every change.
func events(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		updates, unsubscribe := store.Subscribe()
		defer unsubscribe()
		tick := time.NewTicker(heartbeat)
		defer tick.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case rooms := <-updates:
				b, _ := json.Marshal(rooms)
				fmt.Fprintf(w, "event: rooms\ndata: %s\n\n", b)
			case <-tick.C:
				fmt.Fprint(w, ": ping\n\n") // keeps proxies and gateways from closing an idle stream
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}
}

// spa serves the built UI and falls back to index.html for client-side routes like /rooms/abc.
func spa(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		files.ServeHTTP(w, r)
	})
}

func userName(r *http.Request) string {
	name, _ := r.Context().Value(userKey{}).(string)
	return name
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(v)
	if err != nil {
		var tooBig *http.MaxBytesError
		status := http.StatusBadRequest
		if errors.As(err, &tooBig) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, "invalid request body", status)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
