// Package web serves the dashboard, a JSON API, and a Server-Sent Events stream.
package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"

	"machine-telemetry-hub/internal/hub"
)

//go:embed static
var staticFiles embed.FS

// NewHandler wires all routes.
//
//	GET /              dashboard (static files embedded in the binary)
//	GET /api/machines  current snapshot as JSON
//	GET /events        live snapshots via Server-Sent Events
//	GET /healthz       liveness probe
func NewHandler(h *hub.Hub) http.Handler {
	static, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err) // only fails if the embed path is wrong: a programming error
	}

	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(static))
	mux.HandleFunc("GET /api/machines", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(w).Encode(h.Snapshot()); err != nil {
			slog.Warn("encode snapshot", "err", err)
		}
	})
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) { serveEvents(w, r, h) })
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	return mux
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
