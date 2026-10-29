package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestReadRoutesAndAuth(t *testing.T) {
	s := &Server{Mode: "demo"}
	h := s.Handler()
	for _, path := range []string{"/session", "/roles", "/shifts", "/sources", "/sources/WB-26", "/assets", "/assets/cip-log", "/assets/cip-policy", "/assets/mid-tanks", "/production/fillers", "/production/aseptic-tanks", "/production/unit-states", "/production/changeover-matrix", "/plans", "/plans/current", "/limits/schema", "/supply/farms", "/supply/demand-plan", "/supply/trend", "/quality/active-event", "/assistant/templates", "/graph", "/site/layout", "/trace/backward?lotId=FG-1013-07", "/trace/forward?rtId=RT-04&date=2026-10-13", "/trace/explain?resource=Bay%202&time=14.50", "/trace/handoffs?lotId=FG-1013-07", "/lots/locations?ids=FG-1013-05", "/holds/summary"} {
		t.Run(path, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/v1"+path, nil)
			r.Header.Set("X-Demo-Role", "planner")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 200 || !json.Valid(w.Body.Bytes()) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	for _, role := range []string{"", "admin"} {
		r := httptest.NewRequest("GET", "/api/v1/session", nil)
		r.Header.Set("X-Demo-Role", role)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	}
}

func TestPublicMeta(t *testing.T) {
	s := &Server{Mode: "demo"}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/meta", nil))
	var meta map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &meta); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || meta["schemaVersion"] != "1" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestGraphEndpoints(t *testing.T) {
	s := &Server{Mode: "demo"}
	h := s.Handler()

	// Subgraph valid
	r := httptest.NewRequest("GET", "/api/v1/graph/subgraph?node=rt-04&depth=2", nil)
	r.Header.Set("X-Demo-Role", "planner")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var res map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	nodes, ok := res["nodes"].([]any)
	if !ok || len(nodes) == 0 {
		t.Fatalf("expected non-empty nodes, got %v", res["nodes"])
	}

	// Subgraph not found
	r = httptest.NewRequest("GET", "/api/v1/graph/subgraph?node=NONEXISTENT_NODE&depth=1", nil)
	r.Header.Set("X-Demo-Role", "planner")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}

	// Subgraph invalid depth
	r = httptest.NewRequest("GET", "/api/v1/graph/subgraph?node=RT-04&depth=abc", nil)
	r.Header.Set("X-Demo-Role", "planner")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 422 {
		t.Fatalf("expected 422, got %d", w.Code)
	}
}
