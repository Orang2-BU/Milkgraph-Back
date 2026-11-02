package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMasterTopologyRoute(t *testing.T) {
	s := &Server{Mode: "demo"}
	r := httptest.NewRequest("GET", "/api/v1/master/topology", nil)
	r.Header.Set("X-Demo-Role", "planner")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var topology struct {
		Kind  string              `json:"kind"`
		Nodes []map[string]string `json:"nodes"`
		Edges []map[string]string `json:"edges"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &topology); err != nil {
		t.Fatal(err)
	}
	if topology.Kind != "route_capability" || len(topology.Nodes) != 73 || len(topology.Edges) != 118 {
		t.Fatalf("unexpected topology: kind=%q nodes=%d edges=%d", topology.Kind, len(topology.Nodes), len(topology.Edges))
	}
	found := false
	for _, edge := range topology.Edges {
		if edge["via_asset_id"] != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("dedicated pipe route resource missing")
	}
}

func TestMasterCatalogRoutes(t *testing.T) {
	s := &Server{Mode: "demo"}
	h := s.Handler()
	for _, path := range []string{"/master", "/master/assets", "/master/connections", "/master/products", "/master/finished-skus", "/master/lines", "/master/rules"} {
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

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/master", nil)
	r.Header.Set("X-Demo-Role", "planner")
	h.ServeHTTP(w, r)
	var summary struct {
		Tables                 map[string]int `json:"tables"`
		NumericalPlanningReady bool           `json:"numericalPlanningReady"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Tables["asset_configuration"] != 73 || summary.Tables["asset_connections"] != 118 || summary.Tables["line_finished_sku_compatibility"] != 116 {
		t.Fatalf("unexpected master counts: %#v", summary.Tables)
	}
	if summary.NumericalPlanningReady {
		t.Fatal("placeholder numeric data must not be planning-ready")
	}

	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/master", nil))
	if w.Code != 401 {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestMasterChangeoverClassifyEndpoint(t *testing.T) {
	s := &Server{Mode: "demo"}
	h := s.Handler()

	body := strings.NewReader(`{"from":"UHT-CH-1000-LOCAL","to":"UHT-ST-1000-LOCAL"}`)
	r := httptest.NewRequest("POST", "/api/v1/master/changeover/classify", body)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Demo-Role", "planner")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var res struct {
		RuleID      string `json:"ruleId"`
		Activity    string `json:"activity"`
		RequiresCIP bool   `json:"requiresCip"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.RuleID != "VARIANT" || res.Activity != "Variant Changeover" || !res.RequiresCIP {
		t.Fatalf("unexpected classification: %#v", res)
	}
}
