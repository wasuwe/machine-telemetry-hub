// Package web serves the dashboard, a JSON API, and a Server-Sent Events stream.
package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wasuwe/machine-telemetry-hub/internal/api"
	"github.com/wasuwe/machine-telemetry-hub/internal/hub"
	"github.com/wasuwe/machine-telemetry-hub/internal/store"
)

//go:embed static
var staticFiles embed.FS

// NewHandler wires all routes.
//
//	GET /              dashboard (static files embedded in the binary)
//	GET /api/machines  current snapshot as JSON
//	GET /api/readings  stored rows with id > ?after (at most ?limit, default 50)
//	GET /api/demo-keys the public demo keys for /api/v1 (shown in the API tab)
//	    /api/v1/...    public REST API, needs an API key (see package api)
//	GET /events        live snapshots via Server-Sent Events
//	GET /healthz       liveness probe
func NewHandler(h *hub.Hub, st *store.Store) http.Handler {
	static, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err) // only fails if the embed path is wrong: a programming error
	}

	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(static))
	mux.HandleFunc("GET /api/machines", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, h.Snapshot())
	})
	mux.HandleFunc("GET /api/readings", func(w http.ResponseWriter, r *http.Request) {
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if err != nil || limit < 1 || limit > 200 {
			limit = 50
		}
		writeJSON(w, map[string]any{"total": st.Total(), "rows": st.After(after, limit)})
	})
	mux.HandleFunc("GET /api/demo-keys", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, api.DemoKeys)
	})
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) { serveEvents(w, r, h) })
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	// The v1 API answers every method itself (405 as problem+json, OPTIONS for CORS),
	// so it sits in front of the mux instead of inside it, where it would clash with "GET /".
	v1 := api.New(h, st, time.Now).Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1" || strings.HasPrefix(r.URL.Path, "/api/v1/") {
			v1.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("encode json", "err", err)
	}
}

func serveEvents(w http.ResponseWriter, r *http.Request, h *hub.Hub) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	updates, cancel := h.Subscribe()
	defer cancel()

	// Send the current state right away so the page renders without waiting.
	if writeEvent(w, h.Snapshot()) != nil {
		return
	}
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case snap, ok := <-updates:
			if !ok {
				return
			}
			if writeEvent(w, snap) != nil {
				return // client went away
			}
			flusher.Flush()
		}
	}
}

func writeEvent(w http.ResponseWriter, snap hub.Snapshot) error {
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}
