// Package api serves the public, key-protected REST API under /api/v1.
//
// It follows common HTTP conventions so client code written against it
// carries over to real APIs:
//
//   - API keys in "Authorization: Bearer <key>" (or "X-API-Key: <key>")
//   - errors as RFC 9457 problem details (application/problem+json)
//   - 401 with WWW-Authenticate, 403 for a missing scope, 405 with Allow,
//     429 with Retry-After, and RateLimit-* headers on every authenticated call
//   - CORS, so the API can be called from a page on another origin
//
// The keys are demo keys, published in the dashboard's API tab.
package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wasuwe/machine-telemetry-hub/internal/hub"
	"github.com/wasuwe/machine-telemetry-hub/internal/store"
)

// Scopes a key can hold.
const (
	ScopeMachines = "machines:read"
	ScopeReadings = "readings:read"
)

// Key is one API key and what it may do.
type Key struct {
	Key       string   `json:"key"`
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	PerMinute int      `json:"perMinute"` // requests allowed per minute
	Revoked   bool     `json:"revoked"`
}

// DemoKeys are public test keys. Each one shows a different response.
var DemoKeys = []Key{
	{Key: "mth_test_full_7f3a9c2e", Name: "Full access", Scopes: []string{ScopeMachines, ScopeReadings}, PerMinute: 120},
	{Key: "mth_test_readings_4b81d0aa", Name: "Readings only", Scopes: []string{ScopeReadings}, PerMinute: 120},
	{Key: "mth_test_limited_c29e5f13", Name: "Low rate limit", Scopes: []string{ScopeMachines, ScopeReadings}, PerMinute: 5},
	{Key: "mth_test_revoked_0d6e8b71", Name: "Revoked", Scopes: []string{ScopeMachines, ScopeReadings}, PerMinute: 120, Revoked: true},
}

// Codes the /api/v1/status/{code} test endpoint can return.
var testCodes = map[int]bool{200: true, 204: true, 400: true, 401: true, 403: true, 404: true, 409: true, 422: true, 429: true, 500: true, 502: true, 503: true, 504: true}

// Server holds the API's dependencies and rate-limit state.
type Server struct {
	hub   *hub.Hub
	store *store.Store
	keys  map[string]Key
	now   func() time.Time

	mu      sync.Mutex
	windows map[string]*window // per key: fixed one-minute window
}

type window struct {
	start time.Time
	count int
}

// New creates the API with the demo keys. now is injectable for tests.
func New(h *hub.Hub, st *store.Store, now func() time.Time) *Server {
	s := &Server{hub: h, store: st, now: now, keys: map[string]Key{}, windows: map[string]*window{}}
	for _, k := range DemoKeys {
		s.keys[k.Key] = k
	}
	return s
}

// route describes one endpoint: the scope it needs and its handler.
type route struct {
	scope   string // "" = no key needed
	handler func(w http.ResponseWriter, r *http.Request)
}

// Handler returns the API. Mount it at "/api/v1/".
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	add := func(pattern string, rt route) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) { s.serve(w, r, rt) })
	}
	add("/api/v1/machines", route{ScopeMachines, s.listMachines})
	add("/api/v1/machines/{id}", route{ScopeMachines, s.getMachine})
	add("/api/v1/readings", route{ScopeReadings, s.listReadings})
	add("/api/v1/status/{code}", route{"", s.testStatus})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		start(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		problem(w, r, http.StatusNotFound, "not_found", "No endpoint at "+r.URL.Path+".")
	})
	return mux
}

// serve runs the checks every endpoint shares, in this order:
// CORS preflight → method (405) → key (401) → scope (403) → rate limit (429) → handler.
func (s *Server) serve(w http.ResponseWriter, r *http.Request, rt route) {
	start(w)
	switch r.Method {
	case http.MethodGet, http.MethodHead:
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent) // CORS preflight
		return
	default:
		w.Header().Set("Allow", "GET, HEAD, OPTIONS")
		problem(w, r, http.StatusMethodNotAllowed, "method_not_allowed", r.Method+" is not supported here. This API is read-only: use GET.")
		return
	}

	if rt.scope != "" {
		key, ok := s.authenticate(w, r)
		if !ok {
			return
		}
		if !hasScope(key, rt.scope) {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="telemetryhub", error="insufficient_scope", scope=%q`, rt.scope))
			problem(w, r, http.StatusForbidden, "insufficient_scope", fmt.Sprintf("The key %q does not have the %s scope.", key.Name, rt.scope))
			return
		}
		if !s.allow(w, r, key) {
			return
		}
	}
	rt.handler(w, r)
}

// authenticate reads the key and writes a 401 if it is missing, unknown or revoked.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (Key, bool) {
	raw := r.Header.Get("X-API-Key")
	if auth := r.Header.Get("Authorization"); raw == "" && auth != "" {
		scheme, token, _ := strings.Cut(auth, " ")
		if strings.EqualFold(scheme, "Bearer") {
			raw = strings.TrimSpace(token)
		}
	}
	if raw == "" {
		w.Header().Set("WWW-Authenticate", `Bearer realm="telemetryhub"`)
		problem(w, r, http.StatusUnauthorized, "missing_api_key", `Send your key in the "Authorization: Bearer <key>" header.`)
		return Key{}, false
	}
	key, ok := s.keys[raw]
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer realm="telemetryhub", error="invalid_token"`)
		problem(w, r, http.StatusUnauthorized, "invalid_api_key", "This API key does not exist.")
		return Key{}, false
	}
	if key.Revoked {
		w.Header().Set("WWW-Authenticate", `Bearer realm="telemetryhub", error="invalid_token", error_description="revoked"`)
		problem(w, r, http.StatusUnauthorized, "api_key_revoked", "This API key was revoked. Create a new one.")
		return Key{}, false
	}
	return key, true
}

// allow counts the request against the key's per-minute limit, sets the
// RateLimit-* headers, and writes a 429 when the limit is used up.
func (s *Server) allow(w http.ResponseWriter, r *http.Request, key Key) bool {
	now := s.now()
	s.mu.Lock()
	win := s.windows[key.Key]
	if win == nil || now.Sub(win.start) >= time.Minute {
		win = &window{start: now}
		s.windows[key.Key] = win
	}
	win.count++
	count := win.count
	reset := int(win.start.Add(time.Minute).Sub(now).Seconds() + 0.999) // round up
	s.mu.Unlock()

	h := w.Header()
	h.Set("RateLimit-Limit", strconv.Itoa(key.PerMinute))
	h.Set("RateLimit-Remaining", strconv.Itoa(max(0, key.PerMinute-count)))
	h.Set("RateLimit-Reset", strconv.Itoa(reset))
	if count > key.PerMinute {
		h.Set("Retry-After", strconv.Itoa(reset))
		problem(w, r, http.StatusTooManyRequests, "rate_limited", fmt.Sprintf("This key allows %d requests per minute. Try again in %d s.", key.PerMinute, reset))
		return false
	}
	return true
}

func hasScope(k Key, scope string) bool {
	for _, s := range k.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// ---- endpoints ----

func (s *Server) listMachines(w http.ResponseWriter, r *http.Request) {
	snap := s.hub.Snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"data": snap.Machines,
		"meta": map[string]any{"at": snap.At, "count": len(snap.Machines), "summary": snap.Summary},
	})
}

func (s *Server) getMachine(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	snap := s.hub.Snapshot()
	for _, m := range snap.Machines {
		if strings.EqualFold(m.ID, id) {
			writeJSON(w, http.StatusOK, map[string]any{"data": m, "meta": map[string]any{"at": snap.At}})
			return
		}
	}
	problem(w, r, http.StatusNotFound, "machine_not_found", fmt.Sprintf("There is no machine %q. IDs run from M01 to M%02d.", id, len(snap.Machines)))
}

func (s *Server) listReadings(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	after, err := intParam(q.Get("after"), 0)
	if err != nil || after < 0 {
		problem(w, r, http.StatusBadRequest, "invalid_parameter", `"after" must be a whole number of 0 or more.`)
		return
	}
	limit, err := intParam(q.Get("limit"), 50)
	if err != nil || limit < 1 || limit > 200 {
		problem(w, r, http.StatusBadRequest, "invalid_parameter", `"limit" must be a whole number from 1 to 200.`)
		return
	}
	rows := s.store.After(int64(after), limit)
	next := int64(after)
	if len(rows) > 0 {
		next = rows[len(rows)-1].ID
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": rows,
		"meta": map[string]any{"total": s.store.Total(), "after": after, "limit": limit, "nextAfter": next},
	})
}

// testStatus returns the requested status code, so clients can test their error handling.
func (s *Server) testStatus(w http.ResponseWriter, r *http.Request) {
	code, err := strconv.Atoi(r.PathValue("code"))
	if err != nil || !testCodes[code] {
		problem(w, r, http.StatusBadRequest, "invalid_parameter", "Supported codes: 200, 204, 400, 401, 403, 404, 409, 422, 429, 500, 502, 503, 504.")
		return
	}
	switch {
	case code == http.StatusNoContent:
		w.WriteHeader(code)
	case code < 300:
		writeJSON(w, code, map[string]any{"data": map[string]any{"status": code, "message": "Test response."}})
	default:
		if code == http.StatusTooManyRequests || code == http.StatusServiceUnavailable {
			w.Header().Set("Retry-After", "30")
		}
		if code == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", `Bearer realm="telemetryhub"`)
		}
		problem(w, r, code, "test_"+strconv.Itoa(code), "Test response from /api/v1/status. Nothing is wrong with the API.")
	}
}

// ---- helpers ----

func intParam(v string, def int) (int, error) {
	if v == "" {
		return def, nil
	}
	return strconv.Atoi(v)
}

// start sets the headers every response gets.
func start(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Authorization, X-API-Key")
	h.Set("Access-Control-Expose-Headers", "RateLimit-Limit, RateLimit-Remaining, RateLimit-Reset, Retry-After, X-Request-Id, WWW-Authenticate, Allow")
	h.Set("Access-Control-Max-Age", "600")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Request-Id", requestID())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("api: encode", "err", err)
	}
}

// Problem is an RFC 9457 problem details body, plus a machine-readable code.
type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail"`
	Instance  string `json:"instance"`
	Code      string `json:"code"`
	RequestID string `json:"requestId"`
}

func problem(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	p := Problem{
		Type:      "about:blank", // RFC 9457: the title is then the HTTP status text
		Title:     http.StatusText(status),
		Status:    status,
		Detail:    detail,
		Instance:  r.URL.RequestURI(),
		Code:      code,
		RequestID: w.Header().Get("X-Request-Id"),
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(p); err != nil {
		slog.Warn("api: encode problem", "err", err)
	}
}

func requestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "req_" + hex.EncodeToString(b[:])
}
