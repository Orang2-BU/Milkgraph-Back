package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

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
