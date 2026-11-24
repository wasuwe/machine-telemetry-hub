package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wasuwe/machine-telemetry-hub/internal/hub"
	"github.com/wasuwe/machine-telemetry-hub/internal/store"
	"github.com/wasuwe/machine-telemetry-hub/internal/telemetry"
)

const (
	full     = "mth_test_full_7f3a9c2e"
	readings = "mth_test_readings_4b81d0aa"
	limited  = "mth_test_limited_c29e5f13"
	revoked  = "mth_test_revoked_0d6e8b71"
)

type fixture struct {
	h   http.Handler
	now time.Time
}

func newFixture() *fixture {
	f := &fixture{now: time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)}
	clock := func() time.Time { return f.now }
	hb := hub.New([]telemetry.Machine{{ID: "M01", Name: "Press 01", IdealCycle: time.Second}}, clock)
	st := store.New(10)
	for range 3 {
		st.Add(telemetry.Event{MachineID: "M01", Kind: telemetry.EventShot, At: f.now, Good: true, CycleTime: time.Second})
	}
	f.h = New(hb, st, clock).Handler()
	return f
}

func (f *fixture) do(method, path, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec
}

func problemOf(t *testing.T, rec *httptest.ResponseRecorder) Problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content type %q, want application/problem+json", ct)
	}
	var p Problem
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	if p.Status != rec.Code || p.Title != http.StatusText(rec.Code) || p.RequestID == "" {
		t.Fatalf("bad problem body %+v for status %d", p, rec.Code)
	}
	return p
}

func TestStatusCodes(t *testing.T) {
	cases := []struct {
		name, method, path, key string
		status                  int
		code                    string // problem code, "" for success
		header                  string // header that must be set
	}{
		{"ok machines", "GET", "/api/v1/machines", full, 200, "", "RateLimit-Remaining"},
		{"ok one machine, case-insensitive id", "GET", "/api/v1/machines/m01", full, 200, "", ""},
		{"ok readings", "GET", "/api/v1/readings?after=1&limit=5", readings, 200, "", ""},
		{"no key on readings", "GET", "/api/v1/readings", "", 401, "missing_api_key", "WWW-Authenticate"},
		{"preflight", "OPTIONS", "/api/v1/machines", "", 204, "", "Access-Control-Allow-Origin"},
		{"bad limit", "GET", "/api/v1/readings?limit=0", full, 400, "invalid_parameter", ""},
		{"bad after", "GET", "/api/v1/readings?after=abc", full, 400, "invalid_parameter", ""},
		{"no key", "GET", "/api/v1/machines", "", 401, "missing_api_key", "WWW-Authenticate"},
		{"unknown key", "GET", "/api/v1/machines", "mth_test_nope", 401, "invalid_api_key", "WWW-Authenticate"},
		{"revoked key", "GET", "/api/v1/machines", revoked, 401, "api_key_revoked", "WWW-Authenticate"},
		{"missing scope", "GET", "/api/v1/machines", readings, 403, "insufficient_scope", "WWW-Authenticate"},
		{"unknown machine", "GET", "/api/v1/machines/M99", full, 404, "machine_not_found", ""},
		{"unknown path", "GET", "/api/v1/nope", full, 404, "not_found", ""},
		{"write method", "POST", "/api/v1/machines", full, 405, "method_not_allowed", "Allow"},
		{"test endpoint 503", "GET", "/api/v1/status/503", "", 503, "test_503", "Retry-After"},
		{"test endpoint unsupported", "GET", "/api/v1/status/418", "", 400, "invalid_parameter", ""},
		{"test endpoint 204", "GET", "/api/v1/status/204", "", 204, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture()
			rec := f.do(c.method, c.path, c.key)
			if rec.Code != c.status {
				t.Fatalf("status %d, want %d; body %s", rec.Code, c.status, rec.Body)
			}
			if c.header != "" && rec.Header().Get(c.header) == "" {
				t.Errorf("missing header %s", c.header)
			}
			if rec.Header().Get("X-Request-Id") == "" {
				t.Error("missing X-Request-Id")
			}
			if c.code != "" {
				if p := problemOf(t, rec); p.Code != c.code {
					t.Errorf("code %q, want %q", p.Code, c.code)
				}
			}
		})
	}
}

func TestXAPIKeyHeader(t *testing.T) {
	f := newFixture()
	req := httptest.NewRequest("GET", "/api/v1/readings", nil)
	req.Header.Set("X-API-Key", readings)
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestReadingsBody(t *testing.T) {
	rec := newFixture().do("GET", "/api/v1/readings?after=1", full)
	var body struct {
		Data []store.Reading `json:"data"`
		Meta struct {
			Total     int64 `json:"total"`
			NextAfter int64 `json:"nextAfter"`
		} `json:"meta"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 2 || body.Meta.Total != 3 || body.Meta.NextAfter != 3 {
		t.Fatalf("unexpected body %+v", body)
	}
}

func TestRateLimit(t *testing.T) {
	f := newFixture()
	for i := 1; i <= 5; i++ {
		rec := f.do("GET", "/api/v1/machines", limited)
		if rec.Code != 200 {
			t.Fatalf("request %d: status %d", i, rec.Code)
		}
		if got, want := rec.Header().Get("RateLimit-Remaining"), strings.TrimSpace(string(rune('0'+5-i))); got != want {
			t.Fatalf("request %d: remaining %q, want %q", i, got, want)
		}
	}

	f.now = f.now.Add(20 * time.Second)
	rec := f.do("GET", "/api/v1/machines", limited)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("6th request: status %d, want 429", rec.Code)
	}
	if ra := rec.Header().Get("Retry-After"); ra != "40" {
		t.Fatalf("Retry-After %q, want 40", ra)
	}
	problemOf(t, rec)

	f.now = f.now.Add(40 * time.Second) // new window
	if rec := f.do("GET", "/api/v1/machines", limited); rec.Code != 200 {
		t.Fatalf("after reset: status %d", rec.Code)
	}
}
