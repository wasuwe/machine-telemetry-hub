package web

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wasuwe/machine-telemetry-hub/internal/hub"
	"github.com/wasuwe/machine-telemetry-hub/internal/store"
	"github.com/wasuwe/machine-telemetry-hub/internal/telemetry"
)

func newTestServer(t *testing.T) (*httptest.Server, *hub.Hub) {
	t.Helper()
	h := hub.New([]telemetry.Machine{{ID: "M01", Name: "Press 01", IdealCycle: time.Second}}, time.Now)
	srv := httptest.NewServer(NewHandler(h, store.New(10)))
	t.Cleanup(srv.Close)
	return srv, h
}

func TestDashboardIsServed(t *testing.T) {
	srv, _ := newTestServer(t)
	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), "Machine Telemetry Hub") {
		t.Fatalf("status %d, body %.80q", res.StatusCode, body)
	}
}

func TestMachinesAPI(t *testing.T) {
	srv, _ := newTestServer(t)
	res, err := http.Get(srv.URL + "/api/machines")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	var snap hub.Snapshot
	if err := json.NewDecoder(res.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Machines) != 1 || snap.Machines[0].ID != "M01" {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
}

func TestEventsStream(t *testing.T) {
	srv, h := newTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}

	r := bufio.NewReader(res.Body)
	first := readData(t, r)
	if len(first.Machines) != 1 || first.Summary.Total != 0 {
		t.Fatalf("unexpected first event: %+v", first)
	}

	// A broadcast after a new shot must reach the stream.
	h.Apply(telemetry.Event{MachineID: "M01", Kind: telemetry.EventShot, Good: true, At: time.Now()})
	h.Broadcast()
	if next := readData(t, r); next.Summary.Total != 1 {
		t.Fatalf("expected total 1, got %+v", next.Summary)
	}

	// Closing the client must unsubscribe it from the hub.
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for h.Subscribers() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber was not removed after client disconnect")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func readData(t *testing.T, r *bufio.Reader) hub.Snapshot {
	t.Helper()
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			var snap hub.Snapshot
			if err := json.Unmarshal([]byte(data), &snap); err != nil {
				t.Fatal(err)
			}
			return snap
		}
	}
}

func TestReadingsAPI(t *testing.T) {
	h := hub.New([]telemetry.Machine{{ID: "M01", Name: "Press 01", IdealCycle: time.Second}}, time.Now)
	st := store.New(10)
	for range 3 {
		st.Add(telemetry.Event{MachineID: "M01", Kind: telemetry.EventShot, At: time.Now(), Good: true, CycleTime: time.Second})
	}
	srv := httptest.NewServer(NewHandler(h, st))
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/api/readings?after=1")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Total int64           `json:"total"`
		Rows  []store.Reading `json:"rows"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 3 || len(body.Rows) != 2 || body.Rows[0].ID != 2 {
		t.Fatalf("got total %d, rows %+v", body.Total, body.Rows)
	}
}
